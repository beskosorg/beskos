//go:build pro

package ee

import (
	"strings"
	"testing"

	"github.com/beskosorg/beskos/backend/internal/assistant"
)

// INVARIANT (§3.2): an agent created by the client can never have more tools than the ceiling —
// validated ON THE SERVER, not in the UI. And the refusal is explicit: silently filtering would
// give the person an agent different from the one they think they created.
func TestClientAgentCannotExceedTheToolCeiling(t *testing.T) {
	req := agentWriteReq{Name: "Sales", Tools: []string{"mail.search", "drive.delete_everything"}}
	if code := req.validate(); code != "assistant.agent_tool_not_allowed" {
		t.Errorf("validate = %q, want the refusal for the tool outside the ceiling", code)
	}
}

// Whatever is within the ceiling passes — otherwise the validation would just be a blanket "no".
func TestClientAgentAcceptsToolsWithinTheCeiling(t *testing.T) {
	req := agentWriteReq{Name: "Sales", Tools: assistant.MaxTools()}
	if code := req.validate(); code != "" {
		t.Errorf("validate = %q, want to accept the full ceiling", code)
	}
}

func TestClientAgentNameIsRequiredAndBounded(t *testing.T) {
	cases := map[string]agentWriteReq{
		"empty name":     {Name: "   "},
		"oversized name": {Name: strings.Repeat("a", agentNameMax+1)},
	}
	for name, req := range cases {
		if code := req.validate(); code != "assistant.agent_name_invalid" {
			t.Errorf("%s: validate = %q", name, code)
		}
	}
	// Huge instructions get paid for on every turn — hence the ceiling.
	long := agentWriteReq{Name: "ok", Instructions: strings.Repeat("x", agentInstructionsMax+1)}
	if code := long.validate(); code != "assistant.agent_too_long" {
		t.Errorf("long instructions: validate = %q", code)
	}
}

func TestAgentWriteErrorMapsDuplicateName(t *testing.T) {
	if got := agentWriteError(&fakeDBError{msg: "duplicate key value violates unique constraint \"idx_assistant_agents_name\""}); got != "assistant.agent_name_taken" {
		t.Errorf("agentWriteError = %q, want assistant.agent_name_taken", got)
	}
	if got := agentWriteError(&fakeDBError{msg: "connection reset"}); got != "assistant.failed" {
		t.Errorf("agentWriteError = %q, want the generic fallback", got)
	}
}

type fakeDBError struct{ msg string }

func (e *fakeDBError) Error() string { return e.msg }
