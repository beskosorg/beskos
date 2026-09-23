package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// streamChunk is one piece of the OpenAI-format SSE wire. Text arrives in `delta.content`; a
// tool call arrives FRAGMENTED across `delta.tool_calls`, with the index saying which call each
// fragment belongs to — that is why there is an accumulator instead of a plain copy.
type streamChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content   string           `json:"content"`
			ToolCalls []streamCallFrag `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
}

type streamCallFrag struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Stream performs a token-by-token completion. onDelta receives each piece of TEXT as it arrives;
// tool calls are not streamed mid-flight because a half name or half arguments tell nobody
// anything — only the finished call is useful information.
//
// The returned Response is equivalent to Complete's: the caller does not need to know which one
// was used.
func (d *Client) Stream(ctx context.Context, req Request, onDelta func(string)) (Response, error) {
	resp, err := d.stream(ctx, req, onDelta)
	d.health.record(ctx, d.Name(), d.model, err)
	return resp, err
}

func (d *Client) stream(ctx context.Context, req Request, onDelta func(string)) (Response, error) {
	payload, err := json.Marshal(chatRequest{
		Model:       d.model,
		Messages:    toWire(req.Messages),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      true,
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
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+d.apiKey)

	resp, err := d.http.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("%s: %w", d.Name(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return Response{}, fmt.Errorf("%s HTTP %d: %s", d.Name(), resp.StatusCode, string(body))
	}
	return readSSE(resp.Body, onDelta)
}

// readSSE consumes the wire and rebuilds the response. Isolated from the HTTP call to be
// testable without a network.
func readSSE(body io.Reader, onDelta func(string)) (Response, error) {
	var (
		out   Response
		text  strings.Builder
		calls = map[int]*ToolCall{}
		order []int
	)
	sc := bufio.NewScanner(body)
	// An SSE line can be large (tool arguments in a single chunk). The Scanner's default is
	// 64 KiB, and overflowing mid-line would leave the response silently truncated.
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var ch streamChunk
		if err := json.Unmarshal([]byte(data), &ch); err != nil {
			// An unreadable chunk does not discard what already arrived: the stream continues.
			continue
		}
		if ch.Model != "" {
			out.Model = ch.Model
		}
		if ch.Usage != nil {
			out.Usage = *ch.Usage
		}
		if len(ch.Choices) == 0 {
			continue
		}
		c := ch.Choices[0]
		if c.FinishReason != "" {
			out.FinishReason = c.FinishReason
		}
		if c.Delta.Content != "" {
			text.WriteString(c.Delta.Content)
			if onDelta != nil {
				onDelta(c.Delta.Content)
			}
		}
		for _, frag := range c.Delta.ToolCalls {
			acc, ok := calls[frag.Index]
			if !ok {
				acc = &ToolCall{}
				calls[frag.Index] = acc
				order = append(order, frag.Index)
			}
			if frag.ID != "" {
				acc.ID = frag.ID
			}
			if frag.Function.Name != "" {
				acc.Name = frag.Function.Name
			}
			acc.Arguments += frag.Function.Arguments
		}
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("stream: %w", err)
	}

	out.Text = text.String()
	for _, idx := range order {
		tc := *calls[idx]
		// Reverts the tool name from the wire format (mail_search) back to the domain (mail.search).
		tc.Name = domainToolName(tc.Name)
		out.ToolCalls = append(out.ToolCalls, tc)
	}
	return out, nil
}
