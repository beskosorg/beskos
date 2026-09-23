package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Web Push: desktop notifications delivered by the server when Beskos is closed. Encrypts the
// payload for each subscription (RFC 8291) and sends it to the browser's push service. Off if the
// VAPID keys aren't configured — in that case only the with-tab-open notifications apply.
type webPush struct {
	publicKey  string
	privateKey string
	subject    string
}

// EnablePush turns on Web Push with the VAPID key pair (generated once, stored in env).
func (s *Server) EnablePush(publicKey, privateKey, subject string) {
	s.push = &webPush{publicKey: publicKey, privateKey: privateKey, subject: subject}
}

// handlePushVapid returns the public VAPID key, which the browser needs to subscribe.
func (s *Server) handlePushVapid(w http.ResponseWriter, _ *http.Request) {
	if s.push == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "publicKey": s.push.publicKey})
}

type pushSubBody struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// handlePushSubscribe stores the browser's subscription for this user.
func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var b pushSubBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Endpoint == "" || b.Keys.P256dh == "" || b.Keys.Auth == "" {
		writeErr(w, http.StatusBadRequest, "push.invalid")
		return
	}
	if err := s.q.UpsertPushSubscription(r.Context(), db.UpsertPushSubscriptionParams{
		TenantID: mu.TenantID, UserID: mu.ID, Endpoint: b.Endpoint, P256dh: b.Keys.P256dh, Auth: b.Keys.Auth,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "push.save_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handlePushUnsubscribe removes the subscription (the browser turned off push or revoked the permission).
func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Endpoint == "" {
		writeErr(w, http.StatusBadRequest, "push.invalid")
		return
	}
	if err := s.q.DeletePushSubscriptionByEndpoint(r.Context(), b.Endpoint); err != nil {
		writeErr(w, http.StatusInternalServerError, "push.delete_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// pushPayload is what the service worker receives and shows as a notification.
type pushPayload struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Link  string `json:"link"`
}

// sendWebPush delivers a notification to all of a user's subscriptions. Best-effort: a dead
// subscription (404/410) is removed; the other errors are just logged. Doesn't block the caller.
func (s *Server) sendWebPush(ctx context.Context, userID uuid.UUID, p pushPayload) {
	if s.push == nil {
		return
	}
	// Do Not Disturb: doesn't send a desktop push (the in-app bell notification is still recorded).
	if u, err := s.q.GetUserByID(ctx, userID); err == nil && u.Presence == "dnd" {
		return
	}
	subs, err := s.q.ListPushSubscriptionsByUser(ctx, userID)
	if err != nil {
		log.Printf("push: list subs failed: %v", err)
		return
	}
	if len(subs) == 0 {
		return
	}
	msg, err := json.Marshal(p)
	if err != nil {
		return
	}
	for _, sub := range subs {
		resp, err := webpush.SendNotification(msg, &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
		}, &webpush.Options{
			Subscriber:      s.push.subject,
			VAPIDPublicKey:  s.push.publicKey,
			VAPIDPrivateKey: s.push.privateKey,
			TTL:             60,
		})
		if err != nil {
			log.Printf("push: send failed: %v", err)
			continue
		}
		// 404/410: the subscription expired in the push service — clean it up so it isn't retried.
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			if derr := s.q.DeletePushSubscriptionByEndpoint(ctx, sub.Endpoint); derr != nil {
				log.Printf("push: prune dead sub failed: %v", derr)
			}
		}
		resp.Body.Close()
	}
}
