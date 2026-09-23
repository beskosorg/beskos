package assistant

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/beskosorg/beskos/backend/internal/ai"
)

// failingProvider always fails — used to prove that doubt falls back to the weakest agent.
type failingProvider struct{}

func (failingProvider) Name() string { return "boom" }
func (failingProvider) Complete(context.Context, ai.Request) (ai.Response, error) {
	return ai.Response{}, errors.New("model unavailable")
}

// allBuiltins are the built-in agents as routing sees them.
func allBuiltins() []AgentChoice {
	out := make([]AgentChoice, 0, 3)
	for _, k := range BuiltinKeys() {
		ag, _ := Builtin(k)
		out = append(out, AgentChoice{Key: ag.Key, Purpose: ag.Purpose})
	}
	return out
}

func TestRouterPicksTheNamedAgent(t *testing.T) {
	for _, want := range []string{AgentInbox, AgentDocs, AgentGeneral} {
		r := NewRouter(&fakeProvider{script: []ai.Response{textResp(want)}})
		if got := r.Pick(context.Background(), "do something", allBuiltins()); got != want {
			t.Errorf("Pick = %q, want %q", got, want)
		}
	}
}

// The model is messy: it returns punctuation, uppercase, quotes. That can't turn into a silent
// fallback, or routing would never work and nobody would notice.
func TestRouterToleratesUntidyAnswers(t *testing.T) {
	for _, raw := range []string{"Inbox", " inbox ", "inbox.", `"inbox"`} {
		r := NewRouter(&fakeProvider{script: []ai.Response{textResp(raw)}})
		if got := r.Pick(context.Background(), "summarize yesterday's emails", allBuiltins()); got != AgentInbox {
			t.Errorf("Pick(%q) = %q, want %q", raw, got, AgentInbox)
		}
	}
}

// WHEN IN DOUBT, the one with the LEAST write power (§3.2). Model error, made-up response, empty
// response, empty message: everything falls back to the general assistant, never to the most
// capable one.
func TestRouterFallsBackToTheWeakestAgent(t *testing.T) {
	cases := map[string]*Router{
		"model error":    NewRouter(failingProvider{}),
		"made-up agent":  NewRouter(&fakeProvider{script: []ai.Response{textResp("superagent")}}),
		"empty response": NewRouter(&fakeProvider{script: []ai.Response{textResp("")}}),
		"no provider":    NewRouter(nil),
		"nil router":     func() *Router { var r *Router; return r }(),
	}
	for name, r := range cases {
		if got := r.Pick(context.Background(), "something ambiguous", allBuiltins()); got != AgentGeneral {
			t.Errorf("%s: Pick = %q, want %q", name, got, AgentGeneral)
		}
	}
	// An empty message isn't worth spending a model call on.
	r := NewRouter(&fakeProvider{script: []ai.Response{textResp(AgentDocs)}})
	if got := r.Pick(context.Background(), "   ", allBuiltins()); got != AgentGeneral {
		t.Errorf("empty message: Pick = %q, want %q", got, AgentGeneral)
	}
}

// INVARIANT (§3.2): routing SELECTS from among what was already allowed — it never adds an agent
// outside the list. A model that returns "docs" when "docs" is not available can't grant access
// to it.
func TestRouterNeverPicksAnUnavailableAgent(t *testing.T) {
	r := NewRouter(&fakeProvider{script: []ai.Response{textResp(AgentDocs)}})
	only := []AgentChoice{{Key: AgentGeneral}, {Key: AgentInbox}}
	if got := r.Pick(context.Background(), "make a presentation", only); got != AgentGeneral {
		t.Errorf("Pick = %q — picked an agent that wasn't available", got)
	}
}

// INVARIANT (§3.2): the decision comes ONLY from the user message. The Router doesn't receive tool
// results — it has no way to — and doesn't expose tools to the model in the routing step. If
// someone ever adds a parameter to it, this test is what raises the alarm.
func TestRouterSeesOnlyTheUserMessage(t *testing.T) {
	p := &fakeProvider{script: []ai.Response{textResp(AgentInbox)}}
	r := NewRouter(p)
	msg := "summarize yesterday's emails"
	r.Pick(context.Background(), msg, allBuiltins())

	if len(p.lastTools) != 0 {
		t.Errorf("the routing step offered tools to the model: %v", p.lastTools)
	}
	if len(p.lastMsgs) != 2 {
		t.Fatalf("messages = %d, want only the system message and the user message", len(p.lastMsgs))
	}
	if p.lastMsgs[1].Role != ai.RoleUser || p.lastMsgs[1].Content != msg {
		t.Errorf("what was routed is not the user message: %+v", p.lastMsgs[1])
	}
	// The signature is the defense: an extra parameter here would be a door for content read
	// from elsewhere.
	pick := reflect.TypeOf(r.Pick)
	if pick.NumIn() != 3 || pick.In(1).Kind() != reflect.String || pick.In(2) != reflect.TypeOf([]AgentChoice{}) {
		t.Errorf("Pick's signature changed (%v) — it can only receive the context, the message and the agent list", pick)
	}
	// And an AgentChoice only carries the key and the purpose: the instructions and the tools don't
	// enter the routing prompt, so there's no door there either for content read from elsewhere.
	if n := reflect.TypeOf(AgentChoice{}).NumField(); n != 2 {
		t.Errorf("AgentChoice has %d fields — it can only carry the key and the purpose", n)
	}
}

// THE CORE CRITERION: the mail agent can't touch Drive. It's not a prompt instruction — it's the
// permission list, validated server-side on every call.
func TestInboxAgentCannotTouchDrive(t *testing.T) {
	inbox, ok := Builtin(AgentInbox)
	if !ok {
		t.Fatal("the mail agent doesn't exist")
	}
	if allows(inbox, ToolDriveSearch) {
		t.Error("the mail agent has drive.search in its permission list")
	}
	// And the symmetric case: the docs agent doesn't read the inbox.
	docs, _ := Builtin(AgentDocs)
	for _, mailTool := range []string{ToolMailSearch, ToolMailRead, ToolMailDraft, ToolMailSend} {
		if allows(docs, mailTool) {
			t.Errorf("the docs agent has %s in its permission list", mailTool)
		}
	}
}

// No built-in agent has more tools than the general one: routing never takes a person to a
// profile with more power than the one they started with.
func TestNoBuiltinExceedsTheGeneralAgent(t *testing.T) {
	general, _ := Builtin(AgentGeneral)
	for _, key := range BuiltinKeys() {
		ag, _ := Builtin(key)
		for _, tool := range ag.Tools {
			if !allows(general, tool) {
				t.Errorf("agent %s has %s, which the general one doesn't", key, tool)
			}
		}
	}
}

// A run receives the agent ALREADY chosen. There's no switching mid-run: the engine doesn't know
// about the Router, and that's what prevents an escalation triggered by content read mid-run (§3.2).
func TestEngineDoesNotKnowTheRouter(t *testing.T) {
	eng := NewEngine(&fakeProvider{script: []ai.Response{textResp("ok")}}, mailRegistry(stubBox{}), nil, 0)
	et := reflect.TypeOf(*eng)
	for i := 0; i < et.NumField(); i++ {
		if et.Field(i).Type == reflect.TypeOf(&Router{}) {
			t.Error("the engine has a Router — routing has to stay out of the loop")
		}
	}
}

// The model sometimes swaps separators in the tool name ("wiki.edit.page"). The correction resolves
// to the canonical name ONLY within the agent's own list — it never grants access to a tool
// outside that list.
func TestCanonicalToolNameToleratesSeparators(t *testing.T) {
	ag := Agent{Tools: []string{"wiki.edit_page", "mail.send"}}
	if got := canonicalToolName(ag, "wiki.edit.page"); got != "wiki.edit_page" {
		t.Errorf("want wiki.edit_page, got %q", got)
	}
	if got := canonicalToolName(ag, "wiki_edit_page"); got != "wiki.edit_page" {
		t.Errorf("want wiki.edit_page, got %q", got)
	}
	if got := canonicalToolName(ag, "mail.send"); got != "mail.send" {
		t.Errorf("exact name must pass through, got %q", got)
	}
	if got := canonicalToolName(ag, "drive.search"); got != "drive.search" {
		t.Errorf("a tool outside the list can't be 'corrected': got %q", got)
	}
}
