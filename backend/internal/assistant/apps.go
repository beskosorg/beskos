package assistant

// App gating: turning off an app in the organization cuts the assistant's access to that app's
// tools and, if there is one, to the built-in agent that depends on it. It's the same decision as
// the rail — a disabled app doesn't exist for anyone, not even for the AI (customer request).

// AppTools maps each rail app to the assistant tools it enables. Apps with no tools (chat, today,
// meet) are not included.
var AppTools = map[string][]string{
	"mail":     {ToolMailSearch, ToolMailRead, ToolMailDraft, ToolMailSend},
	"contacts": {ToolContactsSearch, ToolContactsCreate},
	"cal":      {ToolCalendarCreate},
	"files":    {ToolDriveSearch, ToolDocsCreate, ToolDocsRead, ToolDocsEdit, ToolDocsShare},
	"kanban":   {ToolTasksCreate},
	"wiki":     {ToolWikiSearch, ToolWikiRead, ToolWikiCreatePage, ToolWikiEditPage},
	"notes":    {ToolNotesSearch, ToolNotesRead, ToolNotesCreate, ToolNotesEdit},
}

// AppAgent maps an app to the BUILT-IN agent that depends on it: turning off the app removes the
// agent from routing (e.g. without Files, there's no Documents agent; without Mail, no Inbox one).
var AppAgent = map[string]string{
	"files": AgentDocs,
	"mail":  AgentInbox,
	"wiki":  AgentKnowledge,
}

// AllowedTools filters an agent's tool list, removing the ones from disabled apps.
func AllowedTools(tools []string, disabledApps []string) []string {
	if len(disabledApps) == 0 {
		return tools
	}
	off := map[string]bool{}
	for _, app := range disabledApps {
		for _, t := range AppTools[app] {
			off[t] = true
		}
	}
	if len(off) == 0 {
		return tools
	}
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		if !off[t] {
			out = append(out, t)
		}
	}
	return out
}

// WithoutTools removes from the list the tools the organization has turned off. It is a CAP: it
// only removes, never adds — not even for agents created by the customer, which are already
// validated against the catalog. It serves fine-grained cuts ("the AI never sends e-mail") without
// touching what each agent is (§3.2, decision 3).
func WithoutTools(tools []string, blocked []string) []string {
	if len(blocked) == 0 || len(tools) == 0 {
		return tools
	}
	off := make(map[string]bool, len(blocked))
	for _, b := range blocked {
		off[b] = true
	}
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		if !off[t] {
			out = append(out, t)
		}
	}
	return out
}

// AllTools is the complete catalog, so governance can list what there is to turn off.
func AllTools() []string {
	return []string{
		ToolMailSearch, ToolMailRead, ToolMailDraft, ToolMailSend,
		ToolContactsSearch, ToolContactsCreate, ToolContactsShare,
		ToolDriveSearch, ToolDocsCreate, ToolDocsRead, ToolDocsEdit, ToolDocsShare,
		ToolTasksCreate, ToolCalendarCreate,
		ToolWikiSearch, ToolWikiRead, ToolWikiCreatePage, ToolWikiEditPage,
	}
}

// AgentAvailable reports whether a built-in agent remains available given the disabled apps.
func AgentAvailable(agentKey string, disabledApps []string) bool {
	for app, ag := range AppAgent {
		if ag != agentKey {
			continue
		}
		for _, d := range disabledApps {
			if d == app {
				return false
			}
		}
	}
	return true
}
