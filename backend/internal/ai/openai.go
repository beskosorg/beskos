package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// The OpenAI-compatible API (DeepSeek included) REQUIRES a function name to match
// ^[a-zA-Z0-9_-]+$ — no dots. Domain tool names use a dot (mail.search), so they get swapped for
// an underscore when sending and reverted on receipt. No tool name uses an underscore, so the swap
// is a safe bijection — and it lives ONLY in this translation layer, so the rest of the code keeps
// seeing mail.search.
func wireToolName(s string) string   { return strings.ReplaceAll(s, ".", "_") }
func domainToolName(s string) string { return strings.ReplaceAll(s, "_", ".") }

// Client implements Provider over the OpenAI-compatible chat API
// (POST {baseURL}/chat/completions). That protocol is the one nearly every vendor and local
// runtime speaks -- OpenAI and Azure, DeepSeek, Mistral, Groq, Together, OpenRouter, TensorX,
// Ollama, vLLM, llama.cpp, LM Studio -- so pointing Beskos at a different model is an address and
// a key, not another client.
//
// It used to be called DeepSeek, which was never true of the protocol and stopped being true of
// the deployment: production has run on TensorX for months while every log line and metric still
// said "deepseek".
type Client struct {
	apiKey  string
	baseURL string
	model   string
	// thinking is DeepSeek's own request field, and the one thing here that is NOT part of the
	// common protocol. A strict endpoint rejects an unknown body parameter outright -- OpenAI
	// answers 400 -- so it is only ever sent to an endpoint declared to understand it. Without
	// this the generic support would compile, pass its tests, and fail against the first real
	// provider anyone configured.
	thinking bool
	// reasoning: when the endpoint understands `thinking`, whether to let the model think before
	// answering. Off by default -- v4 emits its reasoning BEFORE the text, and in streaming the
	// user watches nothing happen for seconds.
	reasoning bool
	http      *http.Client
	health    health
	// name identifies WHO serves the model, for logs and metrics. The same protocol reaches a
	// vendor's cloud or a process on this machine, and the difference between them is the
	// jurisdiction the customer's content is processed in.
	name string
}

// ClientOptions is what a deployment actually chooses. Only APIKey and BaseURL are required, and
// BaseURL is filled from the preset when the provider is a known name (config.go).
type ClientOptions struct {
	Name     string
	APIKey   string
	BaseURL  string
	Model    string
	Thinking bool // the endpoint understands DeepSeek's `thinking` field
}

func NewClient(o ClientOptions) *Client {
	return &Client{
		apiKey:    o.APIKey,
		baseURL:   strings.TrimRight(o.BaseURL, "/"),
		model:     o.Model,
		thinking:  o.Thinking,
		reasoning: os.Getenv("AI_REASONING") == "on",
		name:      o.Name,
		http:      &http.Client{Timeout: 60 * time.Second},
	}
}

func (d *Client) thinkingParam() *thinkingOpt {
	if !d.thinking || d.reasoning {
		return nil
	}
	return &thinkingOpt{Type: "disabled"}
}

func (d *Client) Name() string {
	if d.name == "" {
		return "openai-compatible"
	}
	return d.name
}

// OpenAI-compatible wire format. The wire* types exist so the domain (Message, Tool, ToolCall)
// stays independent of any one provider's format: switching providers means writing another
// translation, not changing the consumers.
type chatRequest struct {
	Model       string       `json:"model"`
	Messages    []wireMsg    `json:"messages"`
	Temperature float64      `json:"temperature,omitempty"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Stream      bool         `json:"stream"`
	Tools       []wireTool   `json:"tools,omitempty"`
	Thinking    *thinkingOpt `json:"thinking,omitempty"`
}

// thinkingOpt controls reasoning for DeepSeek v4 models ({"type":"disabled"} = no thinking).
type thinkingOpt struct {
	Type string `json:"type"`
}

type wireMsg struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []wireCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type wireTool struct {
	Type     string   `json:"type"` // always "function"
	Function wireFunc `json:"function"`
}

type wireFunc struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type wireCall struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"` // always "function"
	Function wireCallFun `json:"function"`
}

type wireCallFun struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      wireMsg `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// toWire translates the domain into the OpenAI format. The tool name travels in "name" on
// result messages because some compatible providers still require it.
func toWire(msgs []Message) []wireMsg {
	out := make([]wireMsg, 0, len(msgs))
	for _, m := range msgs {
		w := wireMsg{Role: string(m.Role), Content: m.Content, ToolCallID: m.ToolCallID}
		for _, c := range m.ToolCalls {
			w.ToolCalls = append(w.ToolCalls, wireCall{
				ID: c.ID, Type: "function",
				Function: wireCallFun{Name: wireToolName(c.Name), Arguments: c.Arguments},
			})
		}
		out = append(out, w)
	}
	return out
}

func toolsToWire(tools []Tool) []wireTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]wireTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, wireTool{
			Type:     "function",
			Function: wireFunc{Name: wireToolName(t.Name), Description: t.Description, Parameters: t.Parameters},
		})
	}
	return out
}

func fromWireCalls(calls []wireCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(calls))
	for _, c := range calls {
		out = append(out, ToolCall{ID: c.ID, Name: domainToolName(c.Function.Name), Arguments: c.Function.Arguments})
	}
	return out
}

// Complete records every outcome, so a failure is never only a status code in the access log.
func (d *Client) Complete(ctx context.Context, req Request) (Response, error) {
	resp, err := d.complete(ctx, req)
	d.health.record(ctx, d.Name(), d.model, err)
	return resp, err
}

func (d *Client) complete(ctx context.Context, req Request) (Response, error) {
	payload, err := json.Marshal(chatRequest{
		Model:       d.model,
		Messages:    toWire(req.Messages),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      false,
		Tools:       toolsToWire(req.Tools),
		Thinking:    d.thinkingParam(),
	})
	if err != nil {
		return Response{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+d.apiKey)

	resp, err := d.http.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("%s: %w", d.Name(), err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return Response{}, fmt.Errorf("%s: invalid response (HTTP %d): %s", d.Name(), resp.StatusCode, string(body))
	}
	if resp.StatusCode != http.StatusOK {
		msg := string(body)
		if cr.Error != nil {
			msg = cr.Error.Message
		}
		return Response{}, fmt.Errorf("%s HTTP %d: %s", d.Name(), resp.StatusCode, msg)
	}
	if len(cr.Choices) == 0 {
		return Response{}, fmt.Errorf("%s: response has no choices", d.Name())
	}
	return Response{
		Text:         cr.Choices[0].Message.Content,
		Model:        cr.Model,
		Usage:        cr.Usage,
		ToolCalls:    fromWireCalls(cr.Choices[0].Message.ToolCalls),
		FinishReason: cr.Choices[0].FinishReason,
	}, nil
}
