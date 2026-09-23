// Package ai is Beskos's AI layer. The Provider interface abstracts the model/vendor,
// so that switching (any OpenAI-compatible provider) is just configuration — the
// rest of the code depends on `Provider`, never on a specific SDK (law §1.1/2.5).
//
// The API key lives ONLY in the backend, via the environment (law §3.5/§10). Nothing here leaks to the frontend.
package ai

import (
	"context"
	"encoding/json"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	// RoleTool carries the RESULT of a tool back to the model. The content is always
	// untrusted data — never an instruction (§6.1 of the assistant design).
	RoleTool Role = "tool"
)

type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
	// ToolCalls is populated on assistant messages that request tools.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID links a RoleTool message to the call that originated it.
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// Tool describes a tool to the model. Parameters is a JSON Schema of type "object".
// The real catalog lives in the assistant package; here it only passes the description to the provider.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ToolCall is a request from the model to invoke a tool. Arguments is the raw JSON exactly as the
// model produced it — validating it before use is the dispatcher's responsibility.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Request is the generic completion request. Zero-valued fields use the provider's defaults.
type Request struct {
	Messages    []Message
	Temperature float64
	MaxTokens   int
	// Tools, when non-empty, lets the model request tools instead of only replying with text.
	Tools []Tool
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type Response struct {
	Text  string
	Model string
	Usage Usage
	// ToolCalls is non-empty when the model wants to invoke tools instead of replying.
	ToolCalls []ToolCall
	// FinishReason is the provider's stop reason (e.g., "stop", "tool_calls", "length").
	FinishReason string
}

// Provider is the contract every LLM vendor implements. Keep it minimal and stable:
// adding a new model = implementing this interface, without touching the consumers.
type Provider interface {
	// Name identifies the provider (e.g., "deepseek") for logs/metrics.
	Name() string
	// Complete performs a synchronous completion. ctx carries timeout/cancellation.
	Complete(ctx context.Context, req Request) (Response, error)
}

// StreamingProvider is optional: providers that support token-by-token streaming implement it.
// Consumers do a type assertion when they want streaming; the base interface does not require it.
type StreamingProvider interface {
	Provider
	Stream(ctx context.Context, req Request, onDelta func(string)) (Response, error)
}
