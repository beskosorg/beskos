package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/wiki"
)

// Wiki real-time collaboration (Block 2 B10, user decision: relay in GO, no new container). The
// server does NOT understand Yjs: it relays the binary y-websocket protocol messages to the peers
// in the same room (page) — the clients themselves answer each other's sync requests. The
// persisted source of truth remains wiki_pages.body_json (autosave via UpdatePage), with
// RAG/versions/render untouched; this is only the "live" layer.
//
// Seed rule (anti-duplication): the 1st frame the server sends is {"type":"hello","peers":N}.
// peers==0 → the client seeds the Y.Doc from body_json; peers>0 → it starts EMPTY and syncs from
// the peers. Seeding twice would duplicate the content (independent Yjs docs merged together).
//
// Scale: rooms are in-process — the deploy runs ONE instance of the api. If there's ever more
// than one, add Redis fan-out (the chat.Hub pattern) without changing the client protocol.

type collabPeer struct {
	send chan collabFrame
}

type collabFrame struct {
	kind websocket.MessageType
	data []byte
}

type collabRooms struct {
	mu    sync.Mutex
	rooms map[uuid.UUID]map[*collabPeer]bool
}

var wikiCollab = &collabRooms{rooms: map[uuid.UUID]map[*collabPeer]bool{}}

// join registers the peer and returns how many peers were ALREADY in the room (for the seed rule).
func (c *collabRooms) join(page uuid.UUID, p *collabPeer) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	room := c.rooms[page]
	if room == nil {
		room = map[*collabPeer]bool{}
		c.rooms[page] = room
	}
	n := len(room)
	room[p] = true
	return n
}

func (c *collabRooms) leave(page uuid.UUID, p *collabPeer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if room := c.rooms[page]; room != nil {
		delete(room, p)
		if len(room) == 0 {
			delete(c.rooms, page)
		}
	}
	close(p.send)
}

// broadcast delivers the message to every OTHER peer in the room; a slow peer (full buffer) drops
// the frame — the Yjs protocol resyncs on its own (state vectors), never corrupting anything.
func (c *collabRooms) broadcast(page uuid.UUID, from *collabPeer, f collabFrame) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for p := range c.rooms[page] {
		if p == from {
			continue
		}
		select {
		case p.send <- f:
		default:
		}
	}
}

// handleWikiCollabWS connects an editor to the page's room. Requires a session plus
// canAccess>=editor: a read-only user cannot join collaborative editing.
func (s *Server) handleWikiCollabWS(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	pid, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	role, err := s.wiki.PageRole(r.Context(), mu.TenantID, mu.ID, pid)
	if err != nil || role < wiki.RoleViewer {
		writeErr(w, http.StatusNotFound, "wiki.not_found")
		return
	}
	if role < wiki.RoleEditor {
		writeErr(w, http.StatusForbidden, "wiki.forbidden")
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: originHostPatterns(s.corsOrigins)})
	if err != nil {
		return
	}
	defer ws.CloseNow()
	// Yjs updates can be large (pastes, though not base64 images — text only); 4 MiB cap.
	ws.SetReadLimit(4 << 20)

	rctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	peer := &collabPeer{send: make(chan collabFrame, 256)}
	existing := wikiCollab.join(pid, peer)
	defer wikiCollab.leave(pid, peer)

	// Writer: the only goroutine that writes to this socket.
	go func() {
		for {
			select {
			case <-rctx.Done():
				return
			case f, okc := <-peer.send:
				if !okc {
					return
				}
				wctx, wcancel := context.WithTimeout(rctx, 10*time.Second)
				werr := ws.Write(wctx, f.kind, f.data)
				wcancel()
				if werr != nil {
					cancel()
					return
				}
			}
		}
	}()

	// 1st frame: hello with the peer count (client seed rule).
	hello, _ := json.Marshal(map[string]any{"type": "hello", "peers": existing})
	peer.send <- collabFrame{kind: websocket.MessageText, data: hello}

	// Reader: relays everything (Yjs sync/awareness are opaque to us) to the rest of the room.
	for {
		kind, data, rerr := ws.Read(rctx)
		if rerr != nil {
			return
		}
		wikiCollab.broadcast(pid, peer, collabFrame{kind: kind, data: data})
	}
}
