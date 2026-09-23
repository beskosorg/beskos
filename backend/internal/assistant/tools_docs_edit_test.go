package assistant

import (
	"context"
	"sync"
	"testing"

	"github.com/beskosorg/beskos/backend/internal/ai"
	"github.com/beskosorg/beskos/backend/internal/office"
)

// fakeFiles is an in-memory Drive: it keeps the content and version of each file, and counts
// writes so a refusal can be asserted to have written NOTHING.
type fakeFiles struct {
	mu       sync.Mutex
	version  int32
	editable bool
	edits    int
	// bumpBefore simulates someone else saving between the read and the write: bumps the version once.
	bumpBefore bool
}

func (f *fakeFiles) read(_ context.Context, _ Identity, fileID string) (FileContent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return FileContent{ID: fileID, Name: "report.docx", Kind: office.KindDocument,
		Text: "current content", Version: f.version, Hash: "h", Editable: f.editable}, nil
}

func (f *fakeFiles) edit(_ context.Context, _ Identity, fileID string, _ office.Plan, expect int32) (DriveHit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.bumpBefore {
		f.version++
		f.bumpBefore = false
	}
	if f.version != expect {
		return DriveHit{}, errConflict
	}
	f.version++
	f.edits++
	return DriveHit{ID: fileID, Name: "report.docx", Kind: "file", Version: f.version}, nil
}

var errConflict = &conflictError{}

type conflictError struct{}

func (*conflictError) Error() string { return "the file changed since you read it" }

func editRegistry(f *fakeFiles) *Registry {
	return BuildRegistry(Deps{DocsRead: f.read, DocsEdit: f.edit})
}

func readCall(fileID string) ai.Response {
	return ai.Response{ToolCalls: []ai.ToolCall{{ID: "r1", Name: ToolDocsRead, Arguments: `{"file_id":"` + fileID + `"}`}}, FinishReason: "tool_calls"}
}

func editCall(fileID string) ai.Response {
	return ai.Response{ToolCalls: []ai.ToolCall{{ID: "e1", Name: ToolDocsEdit,
		Arguments: `{"file_id":"` + fileID + `","kind":"document","summary":"added a section","blocks":[{"text":"new"}]}`}}, FinishReason: "tool_calls"}
}

// INVARIANT (§5.1, rule 1): docs.edit requires the file to have been read IN THIS RUN. Without a
// read, the model would overwrite content it never saw — hence the edit is refused.
func TestEditRequiresReadInThisRun(t *testing.T) {
	f := &fakeFiles{version: 3, editable: true}
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		editCall("file-1"), // edits WITHOUT reading first
		textResp("no luck"),
	}}, editRegistry(f), &memAudit{}, 0)
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "change the report")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if f.edits != 0 {
		t.Fatal("EDITED WITHOUT READING FIRST — rule 1 of §5.1 was violated")
	}
	if run.Steps[0].ErrorCode == "" {
		t.Error("editing without reading should have failed the tool")
	}
}

// Read first, in the same run, the edit succeeds — and saves a new version.
func TestEditAfterReadSucceeds(t *testing.T) {
	f := &fakeFiles{version: 3, editable: true}
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		readCall("file-1"),
		editCall("file-1"),
		textResp("added the section"),
	}}, editRegistry(f), &memAudit{}, 0)
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "add a section to the report")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if f.edits != 1 {
		t.Fatalf("edits = %d, want 1", f.edits)
	}
	if f.version != 4 {
		t.Errorf("version = %d, want 4 (bumped by one)", f.version)
	}
	// RULE 5: the card says what changed, not "document updated".
	var editStep *Step
	for i := range run.Steps {
		if run.Steps[i].Tool == ToolDocsEdit {
			editStep = &run.Steps[i]
		}
	}
	if editStep == nil || editStep.Summary != "added a section" {
		t.Errorf("the edit card doesn't describe the change: %+v", editStep)
	}
}

// INVARIANT (§5.1, rule 3): if the file changed between the read and the write, the edit is
// refused — the work of whoever saved in the meantime is not lost.
func TestEditRefusedWhenFileChangedSinceRead(t *testing.T) {
	f := &fakeFiles{version: 3, editable: true, bumpBefore: true} // someone saves between read and edit
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		readCall("file-1"),
		editCall("file-1"),
		textResp("couldn't, it changed in the meantime"),
	}}, editRegistry(f), &memAudit{}, 0)
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "change the report")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if f.edits != 0 {
		t.Fatal("EDITED OVER A CONCURRENT CHANGE — rule 3 of §5.1 was violated")
	}
	var editStep *Step
	for i := range run.Steps {
		if run.Steps[i].Tool == ToolDocsEdit {
			editStep = &run.Steps[i]
		}
	}
	if editStep == nil || editStep.ErrorCode == "" {
		t.Error("editing over a stale version should have failed")
	}
}

// Reading one file does NOT authorise editing ANOTHER. The mark is per file, not a global
// "already read something".
func TestReadingOneFileDoesNotAuthoriseEditingAnother(t *testing.T) {
	f := &fakeFiles{version: 1, editable: true}
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		readCall("file-A"),
		editCall("file-B"), // read A, tries to edit B
		textResp("no"),
	}}, editRegistry(f), &memAudit{}, 0)
	ag, _ := Builtin(AgentGeneral)

	eng.Run(context.Background(), testIdentity(), ag, "edit B")
	if f.edits != 0 {
		t.Fatal("edited a file that was never read in this run")
	}
}

// The read mark is PER RUN: a read in one run does not allow editing in another. Without this,
// yesterday's read would authorise today's write.
func TestReadMarkDoesNotLeakBetweenRuns(t *testing.T) {
	f := &fakeFiles{version: 1, editable: true}
	ag, _ := Builtin(AgentGeneral)

	// Run 1: only reads.
	eng1 := NewEngine(&fakeProvider{script: []ai.Response{readCall("file-1"), textResp("read")}},
		editRegistry(f), &memAudit{}, 0)
	eng1.Run(context.Background(), testIdentity(), ag, "read")

	// Run 2: tries to edit without reading. Must be refused.
	eng2 := NewEngine(&fakeProvider{script: []ai.Response{editCall("file-1"), textResp("tried")}},
		editRegistry(f), &memAudit{}, 0)
	eng2.Run(context.Background(), testIdentity(), ag, "edit")

	if f.edits != 0 {
		t.Fatal("a read from another run authorised this edit")
	}
}

// docs.edit does not ask for confirmation: the guard is the new version and the history, not
// confirmation (§5.1).
func TestEditDoesNotAskForConfirmation(t *testing.T) {
	f := &fakeFiles{version: 1, editable: true}
	conf := &scriptedConfirmer{approve: false}
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		readCall("file-1"), editCall("file-1"), textResp("done"),
	}}, editRegistry(f), &memAudit{}, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	eng.Run(context.Background(), testIdentity(), ag, "edit")
	if len(conf.asked) != 0 {
		t.Error("docs.edit asked for confirmation")
	}
	if f.edits != 1 {
		t.Error("docs.edit did not save even though it needed no confirmation")
	}
}
