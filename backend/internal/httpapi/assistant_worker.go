package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/assistant"
	"github.com/beskosorg/beskos/backend/internal/db"
)

// assistantConfirmPoll is how often the worker checks whether the decision has arrived. A task
// sitting there waiting for approval isn't in a hurry; polling every second would only fill up the
// log.
const assistantConfirmPoll = 2 * time.Second

// assistantTaskConfirmWait is how long a background task waits for a decision before giving up.
// Much more generous than the synchronous run's wait: here the person isn't looking at the screen,
// and the promise from §3.6 is that the task notifies and waits. Once the deadline passes, it's a
// REJECTION — never an approval.
const assistantTaskConfirmWait = 24 * time.Hour

// AssistantTaskWorker runs background tasks. It lives in `mailin-worker` (§7.5), but is assembled
// here because it needs exactly the same dependencies as the synchronous engine — and having two
// assemblies of the same engine would mean two risk surfaces drifting apart.
type AssistantTaskWorker struct {
	q      *db.Queries
	srv    *Server
	engine *assistant.Engine
}

// NewAssistantTaskWorker assembles the executor. It receives an already-configured Server: mailbox,
// Drive, and task access all come from it, with the same ownership and tenant checks.
func NewAssistantTaskWorker(s *Server) *AssistantTaskWorker {
	return &AssistantTaskWorker{q: s.q, srv: s, engine: s.agent}
}

// Tick claims a task and runs it. Returns false when there was nothing to do, so the caller can
// wait instead of spinning idle.
func (w *AssistantTaskWorker) Tick(ctx context.Context) bool {
	if w.engine == nil || w.srv.ai == nil {
		return false
	}
	lease := pgtype.Interval{Microseconds: int64(assistant.TaskLease / time.Microsecond), Valid: true}
	row, err := w.q.ClaimAssistantTask(ctx, lease)
	if err != nil {
		// No tasks in the queue is the normal case and isn't an error worth logging.
		return false
	}
	w.run(ctx, row)
	return true
}

// run executes a claimed task.
func (w *AssistantTaskWorker) run(ctx context.Context, row db.AssistantTask) {
	id := assistant.Identity{TenantID: row.TenantID, UserID: row.MailUserID}
	if mu, err := w.q.GetMailUserInTenant(ctx, db.GetMailUserInTenantParams{ID: row.MailUserID, TenantID: row.TenantID}); err == nil {
		id.Email, id.Language = mu.Email, mu.Language
	}

	ag, ok := w.srv.assistantAgentByKey(ctx, row.TenantID, row.Agent)
	if !ok {
		// The agent disappeared (deleted, or disabled) between enqueuing and running. The task fails
		// with a translatable code instead of running with a profile nobody chose.
		w.finish(ctx, row.ID, assistant.TaskResult{Status: assistant.TaskFailed, ErrorCode: "assistant.unknown_agent"})
		return
	}

	history, err := w.history(ctx, row)
	if err != nil {
		log.Printf("assistant-task: history for %s: %v", row.ID, err)
	}

	// The worker's engine needs its own Confirmer: the server's is in-memory and lives in another
	// process. This one writes to the database and waits there — that's what makes the wait durable
	// (§6.3).
	engine := w.engine.WithConfirmer(&taskConfirmer{q: w.q, taskID: row.ID})

	res := assistant.RunTask(ctx, engine, taskStore{q: w.q}, taskEffects{q: w.q, taskID: row.ID}, assistant.Task{
		ID: row.ID, Identity: id, Agent: ag, Message: row.Message,
		ConversationID: row.ConversationID, History: history,
	})

	// The response goes into the conversation, like a normal turn's would: clicking the
	// notification opens the conversation where the task ran, with the result in view (§3.6).
	w.appendAnswer(ctx, row, res)
	w.notify(ctx, row, res)
}

// history reconstructs the conversation's prior thread.
func (w *AssistantTaskWorker) history(ctx context.Context, row db.AssistantTask) ([]assistant.Turn, error) {
	msgs, err := w.q.ListAssistantMessages(ctx, db.ListAssistantMessagesParams{
		ConversationID: row.ConversationID, TenantID: row.TenantID,
		MailUserID: row.MailUserID, Lim: assistantHistoryTurns,
	})
	if err != nil {
		return nil, err
	}
	out := make([]assistant.Turn, 0, len(msgs))
	for _, m := range msgs {
		// The message that ORIGINATED the task is already in the thread; resending it and having
		// RunTask process it again would give the model the same request twice.
		if m.Role == "user" && m.Content == row.Message {
			continue
		}
		out = append(out, assistant.Turn{Role: m.Role, Content: m.Content})
	}
	return out, nil
}

func (w *AssistantTaskWorker) finish(ctx context.Context, id uuid.UUID, res assistant.TaskResult) {
	if err := (taskStore{q: w.q}).Finish(ctx, id, res); err != nil {
		log.Printf("assistant-task: finish %s: %v", id, err)
	}
}

// appendAnswer saves the task's response into the conversation.
func (w *AssistantTaskWorker) appendAnswer(ctx context.Context, row db.AssistantTask, res assistant.TaskResult) {
	content := res.Answer
	stopped := res.Stopped
	if res.Status == assistant.TaskCancelled {
		stopped = assistant.TaskCancelled
	}
	if content == "" && stopped == "" && res.ErrorCode != "" {
		stopped = res.ErrorCode
	}
	if _, err := w.q.AppendAssistantMessage(ctx, db.AppendAssistantMessageParams{
		TenantID: row.TenantID, ConversationID: row.ConversationID, Role: "assistant",
		Content: content, Steps: assistant.TaskSteps(res.Steps), Stopped: stopped,
		RunID: pgtype.UUID{Bytes: res.RunID, Valid: res.RunID != uuid.UUID{}}, Agent: row.Agent,
	}); err != nil {
		log.Printf("assistant-task: append answer for %s: %v", row.ID, err)
	}
	if err := w.q.TouchAssistantConversation(ctx, db.TouchAssistantConversationParams{
		ID: row.ConversationID, TenantID: row.TenantID, MailUserID: row.MailUserID,
	}); err != nil {
		log.Printf("assistant-task: touch conversation for %s: %v", row.ID, err)
	}
}

// notify tells the requester. The link leads to the CONVERSATION where the task ran: a
// notification that just says "finished" forces a search for where, and the link is what makes it
// useful (§3.6).
func (w *AssistantTaskWorker) notify(ctx context.Context, row db.AssistantTask, res assistant.TaskResult) {
	if res.Status == assistant.TaskCancelled {
		// Whoever cancelled it already knows they did; notifying them would be noise.
		return
	}
	args, err := json.Marshal(map[string]string{"task": row.Message, "status": res.Status})
	if err != nil {
		args = json.RawMessage("{}")
	}
	if _, err := w.q.CreateNotification(ctx, db.CreateNotificationParams{
		TenantID: row.TenantID, UserID: row.MailUserID, Kind: "assistant_task",
		// The title and body are translated by the frontend via server.notif.<kind> (law 4.6); the
		// text here is just the English fallback.
		Title: "Assistant task finished", Body: row.Message,
		Link: fmt.Sprintf("/mail?assistant=%s", row.ConversationID),
		Args: args,
	}); err != nil {
		log.Printf("assistant-task: notify for %s: %v", row.ID, err)
	}
}

// --- Persistence implementations ---

// taskStore saves progress and the outcome.
type taskStore struct{ q *db.Queries }

func (t taskStore) Progress(ctx context.Context, id uuid.UUID, status string, steps []assistant.Step) error {
	return t.q.SaveAssistantTaskProgress(ctx, db.SaveAssistantTaskProgressParams{
		ID: id, Status: status, Steps: assistant.TaskSteps(steps),
	})
}

// Cancelled renews the lease at the same time it reads the flag: the worker proves it's alive
// exactly when it checks whether it should stop, so a task that's working is never mistaken for
// abandoned.
func (t taskStore) Cancelled(ctx context.Context, id uuid.UUID) (bool, error) {
	row, err := t.q.HeartbeatAssistantTask(ctx, id)
	if err != nil {
		return false, err
	}
	return row.CancelRequested, nil
}

func (t taskStore) Finish(ctx context.Context, id uuid.UUID, res assistant.TaskResult) error {
	return t.q.FinishAssistantTask(ctx, db.FinishAssistantTaskParams{
		ID: id, Status: res.Status, Answer: res.Answer, ErrorCode: res.ErrorCode,
		Stopped: res.Stopped, Steps: assistant.TaskSteps(res.Steps),
		RunID: pgtype.UUID{Bytes: res.RunID, Valid: res.RunID != uuid.UUID{}},
	})
}

// taskEffects is the idempotency guard for resumption (law 5.3).
type taskEffects struct {
	q      *db.Queries
	taskID uuid.UUID
}

func (t taskEffects) Seen(ctx context.Context, fingerprint string) (string, bool, error) {
	summary, err := t.q.GetAssistantTaskEffect(ctx, db.GetAssistantTaskEffectParams{
		TaskID: t.taskID, Fingerprint: fingerprint,
	})
	if err != nil {
		// Not found is the normal case: it just hasn't happened yet.
		return "", false, nil
	}
	return summary, true, nil
}

func (t taskEffects) Record(ctx context.Context, fingerprint, summary string) error {
	return t.q.RecordAssistantTaskEffect(ctx, db.RecordAssistantTaskEffectParams{
		TaskID: t.taskID, Fingerprint: fingerprint, Summary: summary,
	})
}

// taskConfirmer writes the pending action to the database and polls for the decision. It's the
// DURABLE wait promised in §6.3: the task stays "awaiting confirmation" and remains there after a
// restart, because the state lives in the database, not in a process's memory.
type taskConfirmer struct {
	q      *db.Queries
	taskID uuid.UUID
}

func (c *taskConfirmer) Ask(ctx context.Context, id assistant.Identity, req assistant.ConfirmRequest) (bool, error) {
	args := req.Args
	if len(args) == 0 || !json.Valid(args) {
		args = json.RawMessage("{}")
	}
	row, err := c.q.CreateAssistantPendingAction(ctx, db.CreateAssistantPendingActionParams{
		TaskID: c.taskID, TenantID: id.TenantID, MailUserID: id.UserID, Tool: req.Tool, Args: args,
	})
	if err != nil {
		// Failing to RECORD the request means failing to ask. And not asking means not approving.
		return false, fmt.Errorf("pending action: %w", err)
	}
	if err := c.q.SaveAssistantTaskProgress(ctx, db.SaveAssistantTaskProgressParams{
		ID: c.taskID, Status: assistant.TaskWaiting, Steps: json.RawMessage("[]"),
	}); err != nil {
		log.Printf("assistant-task: mark waiting %s: %v", c.taskID, err)
	}

	deadline := time.NewTimer(assistantTaskConfirmWait)
	defer deadline.Stop()
	tick := time.NewTicker(assistantConfirmPoll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-deadline.C:
			// The deadline ran out with no response. That's a "no".
			return false, nil
		case <-tick.C:
			got, err := c.q.GetAssistantPendingAction(ctx, row.ID)
			if err != nil {
				continue
			}
			switch got.Decision {
			case "approved":
				return true, nil
			case "rejected":
				return false, nil
			}
		}
	}
}
