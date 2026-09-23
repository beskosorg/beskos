package assistant

import (
	"context"
	"strings"
	"testing"

	"github.com/beskosorg/beskos/backend/internal/ai"
)

// INVARIANT (§6.5): forbidden categories are refused even if the model classifies them as normal
// facts. The server's safety net has the last word — the model alone is not trusted.
func TestForbiddenCategoriesAreRefusedEvenIfModelAcceptsThem(t *testing.T) {
	cases := map[string]string{
		"the wifi password is abc123":            refuseCredential,
		"my API token is sk-12345":               refuseCredential,
		"the card number is 4111 1111 1111 1111": refuseFinancial,
		"IBAN PT50000201231234567890154":         refuseFinancial,
		// reHealth only matches Portuguese words, so these two inputs must stay Portuguese: lang:ok
		"tenho uma consulta sobre a minha depressão": refuseHealth,
		"sou do partido X":                           refuseHealth,
	}
	for text, wantReason := range cases {
		// The model accepts everything as a fact in the default category. It's the net that refuses it.
		facts, refusals := parseExtraction(`{"facts":[{"fact":"` + text + `","category":"preferência"}],"forbidden":[]}`)
		if len(facts) != 0 {
			t.Errorf("%q was saved despite being forbidden: %+v", text, facts)
		}
		if len(refusals) != 1 || refusals[0].Reason != wantReason {
			t.Errorf("%q: refusal = %+v, want reason %q", text, refusals, wantReason)
		}
	}
}

// When it refuses, it SAYS SO (§6.5): the refusal comes back with a fact and a reason, for the
// assistant to show instead of silently ignoring it.
func TestRefusalCarriesAReason(t *testing.T) {
	_, refusals := parseExtraction(`{"facts":[],"forbidden":["my password is 1234"]}`)
	if len(refusals) != 1 {
		t.Fatalf("refusals = %d, want 1", len(refusals))
	}
	if refusals[0].Reason == "" || refusals[0].Text == "" {
		t.Error("the refusal has neither a reason nor text — the user wouldn't know why")
	}
}

// Normal facts pass through, with the category validated.
func TestNormalFactsArePreserved(t *testing.T) {
	facts, refusals := parseExtraction(`{"facts":[{"fact":"lives in Belo Horizonte","category":"local"},{"fact":"signs off as Ana","category":"preferência"}],"forbidden":[]}`) // lang:ok
	if len(refusals) != 0 {
		t.Errorf("refused normal facts: %+v", refusals)
	}
	if len(facts) != 2 || facts[0].Text != "lives in Belo Horizonte" || facts[0].Category != "local" {
		t.Errorf("facts = %+v", facts)
	}
	// An invalid category falls back to the default instead of being refused: the fact is fine,
	// only the label came in wrong.
	f2, _ := parseExtraction(`{"facts":[{"fact":"prefers short meetings","category":"made-up"}],"forbidden":[]}`)
	if len(f2) != 1 || f2[0].Category != "preferência" {
		t.Errorf("invalid category = %+v, want it to fall back to the default", f2)
	}
}

// INVARIANT (§8.2): extraction reads ONLY the user message. Never tool results, never the
// assistant's own reply. This is the defense against permanent memory poisoning.
func TestExtractionReadsOnlyTheUserMessage(t *testing.T) {
	p := &fakeProvider{script: []ai.Response{textResp(`{"facts":[],"forbidden":[]}`)}}
	ExtractMemory(context.Background(), p, "I live in Belo Horizonte")

	if len(p.lastMsgs) != 2 {
		t.Fatalf("messages = %d, want only the system message and the user message", len(p.lastMsgs))
	}
	if p.lastMsgs[1].Role != ai.RoleUser || p.lastMsgs[1].Content != "I live in Belo Horizonte" {
		t.Errorf("extraction saw something other than the user message: %+v", p.lastMsgs[1])
	}
	// And it doesn't offer tools: extraction doesn't act, it only reads.
	if len(p.lastTools) != 0 {
		t.Error("extraction offered tools to the model")
	}
}

// INVARIANT (§6.5): memory enters the prompt as CONTEXT, never as instructions. The frame has to
// say so, so the model doesn't treat a fact as an order.
func TestMemoryEntersAsContextNotInstructions(t *testing.T) {
	block := MemoryContext([]Fact{{Text: "lives in Belo Horizonte"}})
	if !strings.Contains(block, "lives in Belo Horizonte") {
		t.Error("the fact didn't make it into the block")
	}
	low := strings.ToLower(block)
	if !strings.Contains(low, "context") {
		t.Error("the block doesn't mark itself as context")
	}
	if !strings.Contains(low, "never grant tools") && !strings.Contains(low, "never") {
		t.Error("the block doesn't warn that facts don't grant tools or approve actions")
	}
	// No facts, no block: the prompt doesn't get padded with an empty frame.
	if MemoryContext(nil) != "" {
		t.Error("MemoryContext of empty should be empty")
	}
}

// Memory reaches the system prompt in a real run, and as context.
func TestMemoryIsInjectedIntoTheSystemPrompt(t *testing.T) {
	p := &fakeProvider{script: []ai.Response{textResp("hello")}}
	eng := NewEngine(p, mailRegistry(stubBox{}), nil, 0)
	ag, _ := Builtin(AgentGeneral)

	eng.RunStream(context.Background(), testIdentity(), ag, RunOptions{
		Message: "what time are the meetings?",
		Memory:  []Fact{{Text: "lives in Belo Horizonte", Category: "local"}},
	}, nil)

	if len(p.lastMsgs) == 0 || p.lastMsgs[0].Role != ai.RoleSystem {
		t.Fatal("no system prompt")
	}
	if !strings.Contains(p.lastMsgs[0].Content, "lives in Belo Horizonte") {
		t.Error("memory was not injected into the system prompt")
	}
}

// An unreadable extraction response saves nothing and doesn't blow up: memory is an extra.
func TestBadExtractionYieldsNothing(t *testing.T) {
	facts, refusals := parseExtraction("sorry, I didn't understand")
	if len(facts) != 0 || len(refusals) != 0 {
		t.Errorf("an unreadable response should yield nothing: %+v %+v", facts, refusals)
	}
	// But JSON wrapped in text or a code fence is still read.
	f, _ := parseExtraction("Sure! ```json\n{\"facts\":[{\"fact\":\"lives in Porto\",\"category\":\"local\"}],\"forbidden\":[]}\n``` there")
	if len(f) != 1 || f[0].Text != "lives in Porto" {
		t.Errorf("wrapped JSON was not read: %+v", f)
	}
}
