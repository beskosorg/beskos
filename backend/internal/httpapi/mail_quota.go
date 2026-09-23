package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Daily send limit per plan, plus metering (tenant_mail_sent). Counts RECIPIENTS (the way SES
// bills). Three send paths go through here: outbox (creation + worker), the assistant
// (SendEmail), and direct JMAP EmailSubmission/set (intercepted in the proxy).

// mailSendAllowed reports whether the tenant can still send to n recipients today. Limit 0 = no
// cap. A read error does NOT block (fail-open: a commercial quota, not a security barrier — the
// same rationale as the AI quota).
func (s *Server) mailSendAllowed(ctx context.Context, tenantID uuid.UUID, n int) bool {
	limit := s.tenantPlanByID(ctx, tenantID).MaxDailySend
	if limit <= 0 {
		return true
	}
	sent, err := s.q.MailSentToday(ctx, tenantID)
	if err != nil {
		log.Printf("mail quota: count: %v", err)
		return true
	}
	return int(sent)+n <= limit
}

// recordMailSent adds n recipients to today's counter (best-effort).
func (s *Server) recordMailSent(ctx context.Context, tenantID uuid.UUID, n int) {
	if n <= 0 {
		return
	}
	if err := s.q.IncrementMailSent(ctx, db.IncrementMailSentParams{TenantID: tenantID, N: int32(n)}); err != nil {
		log.Printf("mail quota: increment: %v", err)
	}
}

// jmapSubmissionRcpts counts the recipients of the EmailSubmission/set calls in a JMAP body. 0 =
// the request doesn't submit any e-mail. A submission with no explicit envelope counts as 1 (Stalwart
// derives it from the Email).
func jmapSubmissionRcpts(body []byte) int {
	var req struct {
		MethodCalls []json.RawMessage `json:"methodCalls"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return 0
	}
	total := 0
	for _, call := range req.MethodCalls {
		var parts []json.RawMessage
		if json.Unmarshal(call, &parts) != nil || len(parts) < 2 {
			continue
		}
		var name string
		if json.Unmarshal(parts[0], &name) != nil || name != "EmailSubmission/set" {
			continue
		}
		var args struct {
			Create map[string]struct {
				Envelope *struct {
					RcptTo []json.RawMessage `json:"rcptTo"`
				} `json:"envelope"`
			} `json:"create"`
		}
		if json.Unmarshal(parts[1], &args) != nil {
			continue
		}
		for _, c := range args.Create {
			if c.Envelope != nil && len(c.Envelope.RcptTo) > 0 {
				total += len(c.Envelope.RcptTo)
			} else {
				total++
			}
		}
	}
	return total
}

// jmapMailQuota intercepts JMAP POSTs: a request with EmailSubmission/set above the daily limit
// gets a 429 before reaching Stalwart; below it, it counts and proceeds. The count is optimistic
// (recorded on entry; a submission rejected by Stalwart still counts) — an accepted and documented
// approximation.
func (s *Server) jmapMailQuota(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.Contains(r.Header.Get("Content-Type"), "json") {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		if n := jmapSubmissionRcpts(body); n > 0 {
			mu := mailUserFrom(r.Context())
			// The account gate goes BEFORE the daily quota: an organisation whose trial ran out is
			// not over its limit, and telling them so would send them looking for the wrong thing.
			if s.accountState(r.Context(), mu.TenantID).Blocked() {
				writeErr(w, http.StatusPaymentRequired, "account.sending_blocked")
				return
			}
			if !s.mailSendAllowed(r.Context(), mu.TenantID, n) {
				writeErr(w, http.StatusTooManyRequests, "mail.daily_limit_reached")
				return
			}
			s.recordMailSent(r.Context(), mu.TenantID, n)
		}
		next.ServeHTTP(w, r)
	})
}
