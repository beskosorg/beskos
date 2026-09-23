package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/assistant"
)

// ask runs Ask in a goroutine and returns a channel with the result.
func ask(c *assistantConfirms, ctx context.Context, id assistant.Identity, req assistant.ConfirmRequest) <-chan bool {
	out := make(chan bool, 1)
	go func() {
		ok, _ := c.Ask(ctx, id, req)
		out <- ok
	}()
	// Gives Ask time to register itself before someone decides.
	for i := 0; i < 100; i++ {
		c.mu.Lock()
		_, registered := c.pending[req.ID]
		c.mu.Unlock()
		if registered {
			break
		}
		time.Sleep(time.Millisecond)
	}
	return out
}

func testIdent() assistant.Identity {
	return assistant.Identity{TenantID: uuid.New(), UserID: uuid.New()}
}

func TestConfirmApprovalReachesTheWaitingRun(t *testing.T) {
	c := newAssistantConfirms()
	id := testIdent()
	req := assistant.ConfirmRequest{ID: uuid.New(), Tool: "mail.send"}
	done := ask(c, context.Background(), id, req)

	if !c.decide(req.ID, id.TenantID, id.UserID, true) {
		t.Fatal("decide did not find the waiting action")
	}
	if !<-done {
		t.Error("the approval did not reach execution")
	}
}

// INVARIANT (§6.2/law 5.1): the approval belongs to whoever owns the action. Another user — even
// from the same tenant — does not approve someone else's send.
func TestConfirmRejectsAnotherUser(t *testing.T) {
	c := newAssistantConfirms()
	id := testIdent()
	req := assistant.ConfirmRequest{ID: uuid.New(), Tool: "mail.send"}
	done := ask(c, context.Background(), id, req)

	if c.decide(req.ID, id.TenantID, uuid.New(), true) {
		t.Error("a colleague approved someone else's send")
	}
	if c.decide(req.ID, uuid.New(), id.UserID, true) {
		t.Error("another tenant approved an action that isn't theirs")
	}
	// The action is still waiting: the unrelated attempts neither decided nor consumed it.
	if !c.decide(req.ID, id.TenantID, id.UserID, true) {
		t.Fatal("the action was consumed by an unrelated attempt")
	}
	if !<-done {
		t.Error("the legitimate approval did not go through")
	}
}

// Approving twice doesn't send twice: the second decision finds nothing.
func TestConfirmIsSingleUse(t *testing.T) {
	c := newAssistantConfirms()
	id := testIdent()
	req := assistant.ConfirmRequest{ID: uuid.New(), Tool: "mail.send"}
	done := ask(c, context.Background(), id, req)

	if !c.decide(req.ID, id.TenantID, id.UserID, true) {
		t.Fatal("the first decision should have gone through")
	}
	<-done
	if c.decide(req.ID, id.TenantID, id.UserID, true) {
		t.Error("the second approval went through — that would send the same email twice")
	}
}

// The user closed the tab: the wait ends in REJECTION, never in approval by default.
func TestConfirmCancelledContextIsNotApproval(t *testing.T) {
	c := newAssistantConfirms()
	ctx, cancel := context.WithCancel(context.Background())
	req := assistant.ConfirmRequest{ID: uuid.New(), Tool: "mail.send"}
	done := ask(c, ctx, testIdent(), req)

	cancel()
	select {
	case ok := <-done:
		if ok {
			t.Error("a cancelled context was treated as an approval")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ask did not react to the cancellation")
	}
}

// A decided action stops occupying memory — otherwise a panel left open would accumulate waits.
func TestConfirmCleansUpAfterDecision(t *testing.T) {
	c := newAssistantConfirms()
	id := testIdent()
	req := assistant.ConfirmRequest{ID: uuid.New(), Tool: "mail.send"}
	done := ask(c, context.Background(), id, req)
	c.decide(req.ID, id.TenantID, id.UserID, false)
	<-done

	for i := 0; i < 100; i++ {
		c.mu.Lock()
		n := len(c.pending)
		c.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Error("the decided action stayed registered")
}
