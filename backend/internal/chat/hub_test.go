package chat

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// newTestHub builds a Hub with no live Redis connection. That's safe here because every test in
// this file only exercises the in-memory connection registry (register/unregister/deliver) --
// none of it touches h.rdb.
func newTestHub() *Hub {
	return &Hub{conns: map[uuid.UUID]map[*conn]struct{}{}}
}

func TestRegisterUnregisterTracksConnections(t *testing.T) {
	h := newTestHub()
	uid := uuid.New()
	c := &conn{send: make(chan []byte, 1)}

	if h.hasConns(uid) {
		t.Fatal("hasConns is true before any connection registered")
	}
	h.register(uid, c)
	if !h.hasConns(uid) {
		t.Fatal("hasConns is false after registering a connection")
	}
	h.unregister(uid, c)
	if h.hasConns(uid) {
		t.Error("hasConns is still true after the only connection unregistered")
	}
}

func TestUnregisterClosesSendChannel(t *testing.T) {
	h := newTestHub()
	uid := uuid.New()
	c := &conn{send: make(chan []byte, 1)}
	h.register(uid, c)
	h.unregister(uid, c)

	_, ok := <-c.send
	if ok {
		t.Error("unregister should close the connection's send channel")
	}
}

func TestUnregisterUnknownConnDoesNotPanic(t *testing.T) {
	h := newTestHub()
	// Unregistering a connection that was never registered (e.g. a duplicate cleanup call on a
	// slow disconnect) must be a no-op, not a panic on a nil map entry.
	h.unregister(uuid.New(), &conn{send: make(chan []byte, 1)})
}

func TestDeliverRoutesOnlyToRecipients(t *testing.T) {
	h := newTestHub()
	recipient := uuid.New()
	bystander := uuid.New()
	cRecipient := &conn{send: make(chan []byte, 1)}
	cBystander := &conn{send: make(chan []byte, 1)}
	h.register(recipient, cRecipient)
	h.register(bystander, cBystander)

	h.deliver(Event{
		Type:       "message",
		ChannelID:  "chan-1",
		Recipients: []string{recipient.String()},
		Payload:    json.RawMessage(`{"text":"hi"}`),
	})

	select {
	case msg := <-cRecipient.send:
		var got map[string]any
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("delivered payload is not valid JSON: %v", err)
		}
		if got["type"] != "message" || got["channel_id"] != "chan-1" {
			t.Errorf("delivered envelope = %+v, want type=message channel_id=chan-1", got)
		}
	default:
		t.Error("the intended recipient received nothing")
	}

	select {
	case <-cBystander.send:
		t.Error("a connection outside Recipients received the event")
	default:
	}
}

func TestDeliverSkipsInvalidRecipientID(t *testing.T) {
	h := newTestHub()
	// A malformed recipient id must not crash delivery to the valid ones alongside it.
	good := uuid.New()
	cGood := &conn{send: make(chan []byte, 1)}
	h.register(good, cGood)

	h.deliver(Event{
		Type:       "typing",
		Recipients: []string{"not-a-uuid", good.String()},
	})

	select {
	case <-cGood.send:
	default:
		t.Error("the valid recipient after a malformed one received nothing")
	}
}

func TestDeliverDropsOnFullBuffer(t *testing.T) {
	h := newTestHub()
	uid := uuid.New()
	c := &conn{send: make(chan []byte, 1)}
	h.register(uid, c)
	c.send <- []byte("already queued")

	// deliver must not block when the recipient's buffer is full (a slow client is dropped, not
	// allowed to stall delivery to everyone else). If it blocked, this goroutine would never
	// signal and the test would time out instead of failing fast.
	done := make(chan struct{})
	go func() {
		h.deliver(Event{Recipients: []string{uid.String()}})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("deliver blocked on a full recipient buffer instead of dropping the event")
	}
}
