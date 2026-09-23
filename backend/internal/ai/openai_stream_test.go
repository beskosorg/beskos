package ai

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// The text arrives in chunks and has to be delivered as it arrives AND accumulated at the end:
// the panel shows the chunks, but whoever persists the conversation needs the whole reply.
func TestReadSSEAccumulatesText(t *testing.T) {
	fio := strings.Join([]string{
		`data: {"model":"deepseek-chat","choices":[{"delta":{"content":"Hello"}}]}`,
		`data: {"choices":[{"delta":{"content":", world"}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"total_tokens":7}}`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n"

	var got []string
	resp, err := readSSE(strings.NewReader(fio), func(s string) { got = append(got, s) })
	if err != nil {
		t.Fatalf("readSSE: %v", err)
	}
	if resp.Text != "Hello, world" {
		t.Errorf("Text = %q, want %q", resp.Text, "Hello, world")
	}
	if strings.Join(got, "|") != "Hello|, world" {
		t.Errorf("deltas = %v, want two chunks in order", got)
	}
	if resp.FinishReason != "stop" || resp.Model != "deepseek-chat" || resp.Usage.TotalTokens != 7 {
		t.Errorf("lost metadata: %+v", resp)
	}
}

// A tool call arrives fragmented: the name in one chunk, the arguments in pieces. If reassembly
// fails, the engine dispatches a tool with truncated JSON — and the error would show up as
// "invalid arguments", far from the actual cause.
func TestReadSSEReassemblesToolCalls(t *testing.T) {
	fio := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"mail.search","arguments":"{\"que"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ry\":\"fatura\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"c2","function":{"name":"drive.search","arguments":"{}"}}]}}]}`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n"

	resp, err := readSSE(strings.NewReader(fio), nil)
	if err != nil {
		t.Fatalf("readSSE: %v", err)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("calls = %d, want 2", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "c1" || resp.ToolCalls[0].Name != "mail.search" {
		t.Errorf("first call = %+v", resp.ToolCalls[0])
	}
	if resp.ToolCalls[0].Arguments != `{"query":"fatura"}` {
		t.Errorf("arguments = %q, want the reassembled JSON", resp.ToolCalls[0].Arguments)
	}
	// The order matches the indexes as they appeared — the engine dispatches in that order.
	if resp.ToolCalls[1].Name != "drive.search" {
		t.Errorf("second call = %+v", resp.ToolCalls[1])
	}
}

// An unreadable chunk must not discard what already arrived: losing the whole reply over one
// broken line would be worse than losing just that line.
func TestReadSSESkipsBadChunks(t *testing.T) {
	fio := "data: {this is not json}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"
	resp, err := readSSE(strings.NewReader(fio), nil)
	if err != nil {
		t.Fatalf("readSSE: %v", err)
	}
	if resp.Text != "ok" {
		t.Errorf("Text = %q, want %q", resp.Text, "ok")
	}
}

// DeepSeek implements the streaming interface. The interface was declared with no provider
// fulfilling it; this test is what stops that from silently happening again.
func TestDeepSeekIsStreamingProvider(t *testing.T) {
	var _ StreamingProvider = NewClient(ClientOptions{APIKey: "k"})
}

// REGRESSION: the OpenAI-compatible API rejects function names with a dot (^[a-zA-Z0-9_-]+$).
// Domain names use a dot (mail.search); they have to go out as underscore on the wire and come
// back as a dot on receipt. The fake provider in the other tests did not validate this — which is
// how the bug reached production.
func TestToolNamesMatchOpenAIPattern(t *testing.T) {
	pat := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	names := []string{"mail.search", "mail.read", "docs.create", "tasks.create", "drive.search"}
	for _, n := range names {
		w := wireToolName(n)
		if !pat.MatchString(w) {
			t.Errorf("wire name %q (from %q) does not match the OpenAI pattern", w, n)
		}
		if got := domainToolName(w); got != n {
			t.Errorf("round trip failed: %q -> %q -> %q", n, w, got)
		}
	}
}

// The tool schemas and tool calls sent to the provider already carry the name in the valid
// format (no dot).
func TestWireToolsAndCallsUseSanitisedNames(t *testing.T) {
	tools := toolsToWire([]Tool{{Name: "mail.send", Parameters: json.RawMessage(`{}`)}})
	if len(tools) != 1 || tools[0].Function.Name != "mail_send" {
		t.Fatalf("schema sent with invalid name: %+v", tools)
	}
	msgs := toWire([]Message{{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "docs.create", Arguments: "{}"}}}})
	if msgs[0].ToolCalls[0].Function.Name != "docs_create" {
		t.Errorf("call in history sent with invalid name: %q", msgs[0].ToolCalls[0].Function.Name)
	}
	// And a call received from the provider goes back to the domain.
	got := fromWireCalls([]wireCall{{ID: "c1", Function: wireCallFun{Name: "mail_search", Arguments: "{}"}}})
	if got[0].Name != "mail.search" {
		t.Errorf("received call = %q, want mail.search", got[0].Name)
	}
}
