package assistant

import "testing"

// Turning off an app has to cut its tools from the assistant — that's what the customer asked
// for, and what separates "disappears from the menu" from "the AI truly can't do it anymore".
func TestAllowedToolsDropsDisabledApps(t *testing.T) {
	tools := []string{ToolMailSend, ToolContactsSearch, ToolCalendarCreate, ToolTasksCreate, ToolDocsCreate}

	// Nothing disabled → everything stays.
	if got := AllowedTools(tools, nil); len(got) != len(tools) {
		t.Fatalf("with no apps disabled, expected everything: %v", got)
	}

	// Contacts and Calendar disabled → their tools go away; the rest stays.
	got := AllowedTools(tools, []string{"contacts", "cal"})
	has := map[string]bool{}
	for _, g := range got {
		has[g] = true
	}
	if has[ToolContactsSearch] || has[ToolCalendarCreate] {
		t.Errorf("tools from a disabled app remained: %v", got)
	}
	if !has[ToolMailSend] || !has[ToolTasksCreate] || !has[ToolDocsCreate] {
		t.Errorf("removed tools from ENABLED apps: %v", got)
	}
}

// The Documents agent lives off Files: without that app, the agent drops out of routing.
func TestAgentAvailableFollowsApps(t *testing.T) {
	if !AgentAvailable(AgentDocs, nil) {
		t.Error("with no apps disabled, the docs agent should be available")
	}
	if AgentAvailable(AgentDocs, []string{"files"}) {
		t.Error("with Files disabled, the docs agent should NOT be available")
	}
	if !AgentAvailable(AgentGeneral, []string{"files"}) {
		t.Error("the general agent doesn't depend on a single app")
	}
}

// The tool cap is a restriction: it removes, never adds. A mistake in the opposite direction here
// would give the AI a capability the organization deliberately turned off.
func TestWithoutToolsSoRemove(t *testing.T) {
	tools := []string{ToolMailSearch, ToolMailSend, ToolWikiRead}
	got := WithoutTools(tools, []string{ToolMailSend})
	if len(got) != 2 {
		t.Fatalf("expected 2 tools, got %d (%v)", len(got), got)
	}
	for _, g := range got {
		if g == ToolMailSend {
			t.Error("the disabled tool remained in the list")
		}
	}
	// Disabling something the agent doesn't have must not add it.
	got = WithoutTools([]string{ToolWikiRead}, []string{ToolMailSend, ToolDocsEdit})
	if len(got) != 1 || got[0] != ToolWikiRead {
		t.Errorf("list changed improperly: %v", got)
	}
	// With nothing blocked, the list comes back intact.
	if len(WithoutTools(tools, nil)) != len(tools) {
		t.Error("with no blocks, the list has to stay the same")
	}
}

// Turning off the Notes app has to take the note tools away from the assistant too. The admin
// toggle says "the AI assistant loses access"; a tool that survived it would make that a lie, and
// the person who switched the app off would have no way of knowing.
func TestNotesAppGatesItsTools(t *testing.T) {
	all := []string{ToolNotesSearch, ToolNotesRead, ToolNotesCreate, ToolNotesEdit, ToolMailSearch}
	got := AllowedTools(all, []string{"notes"})
	if len(got) != 1 || got[0] != ToolMailSearch {
		t.Errorf("with Notes off, %v survived; only the mail tool should have", got)
	}
}

// And with the app on, nothing is removed.
func TestNotesToolsSurviveWhenTheAppIsOn(t *testing.T) {
	all := []string{ToolNotesSearch, ToolNotesEdit}
	if got := AllowedTools(all, []string{"wiki"}); len(got) != 2 {
		t.Errorf("switching the wiki off must not remove note tools: %v", got)
	}
}
