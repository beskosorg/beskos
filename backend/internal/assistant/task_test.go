package assistant

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/ai"
)

// memEffects is an in-memory effect log — the same role as the assistant_task_effects table,
// but without a database.
type memEffects struct {
	mu   sync.Mutex
	done map[string]string
}

func newMemEffects() *memEffects { return &memEffects{done: map[string]string{}} }

func (m *memEffects) Seen(_ context.Context, fp string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.done[fp]
	return s, ok, nil
}

func (m *memEffects) Record(_ context.Context, fp, summary string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.done[fp] = summary
	return nil
}

// memStore collects the progress and the outcome, and answers the cancellation request.
type memStore struct {
	mu       sync.Mutex
	progress [][]Step
	result   TaskResult
	cancel   bool
	// cancelAfter triggers cancellation after N steps.
	cancelAfter int
}

func (m *memStore) Progress(_ context.Context, _ uuid.UUID, _ string, steps []Step) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]Step, len(steps))
	copy(cp, steps)
	m.progress = append(m.progress, cp)
	if m.cancelAfter > 0 && len(m.progress) >= m.cancelAfter {
		m.cancel = true
	}
	return nil
}

func (m *memStore) Cancelled(context.Context, uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cancel, nil
}

func (m *memStore) Finish(_ context.Context, _ uuid.UUID, res TaskResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.result = res
	return nil
}

func testTask(ag Agent, msg string) Task {
	return Task{ID: uuid.New(), Identity: testIdentity(), Agent: ag, Message: msg}
}

// INVARIANT (law 5.3, §7.5): a resume does NOT resend the same e-mail. The task restarts the
// cycle from scratch; the effect guard recognises the send already done and doesn't repeat it.
func TestResumeDoesNotSendTwice(t *testing.T) {
	box := &recordingBox{}
	effects := newMemEffects()
	ag, _ := Builtin(AgentGeneral)
	script := []ai.Response{sendCall(sendArgs), textResp("enviado")}

	// First pass: the e-mail goes out.
	eng := NewEngine(&fakeProvider{script: script}, mailRegistry(box), &memAudit{}, 0).
		WithConfirmer(&scriptedConfirmer{approve: true})
	res := RunTask(context.Background(), eng, &memStore{}, effects, testTask(ag, "envia a fatura"))
	if res.Status != TaskDone {
		t.Fatalf("first pass: status = %q", res.Status)
	}
	if len(box.sent) != 1 {
		t.Fatalf("sends in the first pass = %d, want 1", len(box.sent))
	}

	// The server went down; the task goes back to the queue and reruns with the SAME effect log.
	eng2 := NewEngine(&fakeProvider{script: script}, mailRegistry(box), &memAudit{}, 0).
		WithConfirmer(&scriptedConfirmer{approve: true})
	res2 := RunTask(context.Background(), eng2, &memStore{}, effects, testTask(ag, "envia a fatura"))
	if res2.Status != TaskDone {
		t.Fatalf("resume: status = %q", res2.Status)
	}
	if len(box.sent) != 1 {
		t.Fatalf("THE RESUME RESENT: sends = %d, want to stay at 1", len(box.sent))
	}
}

// Without an effect log (synchronous run), the repeat happens — and it should: there it is a new
// action from the user, not a machine resume. This test exists to make clear that the guard is
// deliberately limited to tasks, not an accident.
func TestWithoutEffectLogARerunActuallyReruns(t *testing.T) {
	box := &recordingBox{}
	ag, _ := Builtin(AgentGeneral)
	for i := 0; i < 2; i++ {
		eng := NewEngine(&fakeProvider{script: []ai.Response{sendCall(sendArgs), textResp("ok")}},
			mailRegistry(box), &memAudit{}, 0).WithConfirmer(&scriptedConfirmer{approve: true})
		if _, err := eng.Run(context.Background(), testIdentity(), ag, "envia"); err != nil {
			t.Fatalf("Run: %v", err)
		}
	}
	if len(box.sent) != 2 {
		t.Errorf("sends = %d, want 2 — without an effect log there is no deduplication", len(box.sent))
	}
}

// Two DIFFERENT sends in the same task both go out. The guard prevents repetition, not
// plurality — if it did, a "reply to these three people" task would only send one.
func TestDifferentWritesInTheSameTaskAllHappen(t *testing.T) {
	box := &recordingBox{}
	ag, _ := Builtin(AgentGeneral)
	two := ai.Response{ToolCalls: []ai.ToolCall{
		{ID: "s1", Name: ToolMailSend, Arguments: sendArgs},
		{ID: "s2", Name: ToolMailSend, Arguments: `{"to":["bruno@exemplo.pt"],"text":"Olá"}`},
	}, FinishReason: "tool_calls"}
	eng := NewEngine(&fakeProvider{script: []ai.Response{two, textResp("feito")}},
		mailRegistry(box), &memAudit{}, 0).WithConfirmer(&scriptedConfirmer{approve: true})

	RunTask(context.Background(), eng, &memStore{}, newMemEffects(), testTask(ag, "responde aos dois"))
	if len(box.sent) != 2 {
		t.Errorf("sends = %d, want 2 — these are different requests", len(box.sent))
	}
}

// The fingerprint doesn't depend on key order or whitespace: the model emits the same request in
// different shapes between runs, and two shapes of the same send must not count as two sends.
func TestFingerprintIgnoresKeyOrderAndWhitespace(t *testing.T) {
	a := effectFingerprint(ToolMailSend, json.RawMessage(`{"to":["ana@x.pt"],"subject":"Oi","text":"corpo"}`))
	b := effectFingerprint(ToolMailSend, json.RawMessage("{ \"text\":\"corpo\",\n \"subject\" : \"Oi\", \"to\": [\"ana@x.pt\"] }"))
	if a != b {
		t.Error("the same message written a different way produced a different fingerprint")
	}
	// But a different recipient is a different effect.
	c := effectFingerprint(ToolMailSend, json.RawMessage(`{"to":["bruno@x.pt"],"subject":"Oi","text":"corpo"}`))
	if a == c {
		t.Error("different recipients produced the same fingerprint")
	}
	// And the same payload through another tool likewise.
	d := effectFingerprint(ToolMailDraft, json.RawMessage(`{"to":["ana@x.pt"],"subject":"Oi","text":"corpo"}`))
	if a == d {
		t.Error("different tools produced the same fingerprint")
	}
}

// Progress is saved on EVERY step. Without this there is no resume and no progress bar — and the
// promise from §3.6 ("the bubble shows progress even when closed") would have nothing to draw on.
func TestTaskSavesProgressEveryStep(t *testing.T) {
	store := &memStore{}
	ag, _ := Builtin(AgentGeneral)
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		toolCallResp(ToolMailSearch, `{"query":"a"}`),
		toolCallResp(ToolMailSearch, `{"query":"b"}`),
		textResp("pronto"),
	}}, mailRegistry(stubBox{}), &memAudit{}, 0)

	RunTask(context.Background(), eng, store, newMemEffects(), testTask(ag, "procura"))
	if len(store.progress) != 2 {
		t.Fatalf("progress records = %d, want one per step (2)", len(store.progress))
	}
	if len(store.progress[0]) != 1 || len(store.progress[1]) != 2 {
		t.Errorf("progress is not cumulative: %v", store.progress)
	}
	if store.result.Status != TaskDone || store.result.Answer != "pronto" {
		t.Errorf("outcome = %+v", store.result)
	}
}

// Cancelling stops the task — and what was already done STAYS recorded (§7.5). A cancelled task
// without a history would leave no one able to tell what happened before it stopped.
func TestCancelStopsAndKeepsWhatWasDone(t *testing.T) {
	store := &memStore{cancelAfter: 1}
	ag, _ := Builtin(AgentGeneral)
	// The model would keep requesting tools indefinitely; cancellation is what stops this.
	eng := NewEngine(&fakeProvider{script: []ai.Response{toolCallResp(ToolMailSearch, `{"query":"x"}`)}},
		mailRegistry(stubBox{}), &memAudit{}, 0)

	res := RunTask(context.Background(), eng, store, newMemEffects(), testTask(ag, "organiza tudo"))
	if res.Status != TaskCancelled {
		t.Errorf("status = %q, want %q", res.Status, TaskCancelled)
	}
	if len(res.Steps) == 0 {
		t.Error("the cancelled task saved nothing of what it had already done")
	}
	if store.result.Status != TaskCancelled {
		t.Errorf("recorded outcome = %q", store.result.Status)
	}
}

// A cancelled task must not keep producing effects after stopping.
func TestCancelledTaskStopsCallingTools(t *testing.T) {
	box := &recordingBox{}
	store := &memStore{cancelAfter: 1}
	ag, _ := Builtin(AgentGeneral)
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		toolCallResp(ToolMailSearch, `{"query":"x"}`),
		sendCall(sendArgs), // if the cycle continued, this would go out
	}}, mailRegistry(box), &memAudit{}, 0).WithConfirmer(&scriptedConfirmer{approve: true})

	RunTask(context.Background(), eng, store, newMemEffects(), testTask(ag, "organiza e envia"))
	if len(box.sent) != 0 {
		t.Errorf("MAIL WENT OUT AFTER CANCELLATION: %+v", box.sent)
	}
}
