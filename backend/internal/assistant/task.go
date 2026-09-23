package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// States of a background task (§3.6). They are the same in the server, the worker and the
// interface — hence living here, rather than in each of them with the risk of diverging by a
// letter.
const (
	TaskQueued    = "queued"
	TaskRunning   = "running"
	TaskWaiting   = "awaiting_confirmation"
	TaskDone      = "done"
	TaskFailed    = "failed"
	TaskCancelled = "cancelled"
)

// TaskLease is how long a worker's lease is valid for. Once it elapses without renewal, the task
// goes back to the queue: that is how it recovers from a worker that died midway (§7.5).
//
// Deliberately generous. An agentic run can spend minutes on a single step — especially waiting
// on a human confirmation — and stealing a live task from a worker that is still working would be
// worse than waiting a bit longer for one that actually died.
const TaskLease = 15 * time.Minute

// TaskStore is what the executor needs from persistence. The real implementation lives in the
// worker; the test passes a double. The engine still never talks to the database directly (§5).
type TaskStore interface {
	// Progress records the steps done so far and the current status. Called on EVERY step: this
	// is what allows resuming and what the bubble displays.
	Progress(ctx context.Context, taskID uuid.UUID, status string, steps []Step) error
	// Cancelled reports whether cancellation was requested. Checked between steps.
	Cancelled(ctx context.Context, taskID uuid.UUID) (bool, error)
	// Finish closes the task.
	Finish(ctx context.Context, taskID uuid.UUID, res TaskResult) error
}

// TaskResult is the outcome of a task.
type TaskResult struct {
	Status    string
	Answer    string
	Stopped   string
	ErrorCode string
	Steps     []Step
	RunID     uuid.UUID
}

// Task is the work to execute.
type Task struct {
	ID             uuid.UUID
	Identity       Identity
	Agent          Agent
	Message        string
	ConversationID uuid.UUID
	History        []Turn
}

// RunTask executes a background task from start to finish.
//
// Three things set this apart from a synchronous run, and all three come from §7.5:
//
//  1. progress is recorded on every step, so the task can resume after a restart;
//  2. write tools go through the effect log, so a resume doesn't repeat what already happened
//     (law 5.3);
//  3. cancellation is checked between steps — what was already done stays recorded, it isn't
//     lost.
//
// Never returns an error for a model failure: a task that fails is a task with status 'failed'
// and an i18n code, not an exception nobody catches.
func RunTask(ctx context.Context, eng *Engine, store TaskStore, effects EffectLog, t Task) TaskResult {
	var seen []Step

	// Cancellation cancels the context, which stops the model call midway if needed. The check
	// runs between steps: cancelling is a request, and the effect lands on the next step.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cancelled := false

	opts := RunOptions{
		Message:        t.Message,
		History:        t.History,
		ConversationID: t.ConversationID,
		Effects:        effects,
		OnStep: func(steps []Step) {
			seen = steps
			if err := store.Progress(runCtx, t.ID, TaskRunning, steps); err != nil {
				// Losing the progress record doesn't throw away the work already done: the task
				// continues, and in the worst case a resume repeats a step the effect guard
				// already recognises.
				return
			}
			if stop, err := store.Cancelled(context.WithoutCancel(runCtx), t.ID); err == nil && stop {
				cancelled = true
				cancel()
			}
		},
	}

	run, err := eng.RunStream(runCtx, t.Identity, t.Agent, opts, nil)
	steps := run.Steps
	if len(steps) == 0 {
		steps = seen
	}

	res := TaskResult{Steps: steps, RunID: run.ID}
	switch {
	case cancelled:
		// What it already did stays recorded (§7.5). A cancelled task is not a task without a
		// history.
		res.Status = TaskCancelled
	case err != nil:
		res.Status = TaskFailed
		res.ErrorCode = "assistant.failed"
	default:
		res.Status = TaskDone
		res.Answer = run.Answer
		res.Stopped = run.Stopped
	}
	if ferr := store.Finish(context.WithoutCancel(ctx), t.ID, res); ferr != nil {
		return res
	}
	return res
}

// TaskSteps serializes the steps to save. An error returns an empty list instead of failing: the
// progress record is just information, and losing it cannot bring the task down.
func TaskSteps(steps []Step) json.RawMessage {
	if len(steps) == 0 {
		return json.RawMessage("[]")
	}
	out, err := json.Marshal(steps)
	if err != nil {
		return json.RawMessage("[]")
	}
	return out
}

// ParseTaskSteps reads the saved steps.
func ParseTaskSteps(raw json.RawMessage) ([]Step, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out []Step
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("assistant: task steps: %w", err)
	}
	return out, nil
}
