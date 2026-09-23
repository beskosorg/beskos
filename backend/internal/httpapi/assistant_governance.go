package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/assistant"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/rbac"
)

// assistantQuotaLimit reads the token ceiling for the 6h window. Configurable per environment
// (§9.2) with a generous default equal for everyone; an invalid value falls back to the default
// instead of disabling the quota.
func assistantQuotaLimit() int {
	if v := os.Getenv("ASSISTANT_QUOTA_TOKENS_6H"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return assistant.DefaultQuotaTokens
}

// assistantQuotaStatus computes the user's quota in the sliding window. The ceiling comes from
// the tenant's PLAN when set (ai_tokens_per_window > 0); otherwise from the product's env/default.
func (s *Server) assistantQuotaStatus(ctx context.Context, id assistant.Identity, now time.Time) assistant.QuotaStatus {
	limit := assistantQuotaLimit()
	if p := s.tenantPlanByID(ctx, id.TenantID); p.AITokensPerWindow > 0 {
		limit = p.AITokensPerWindow
	}
	since := now.Add(-assistant.QuotaWindow)
	row, err := s.q.SumAssistantUsageWindow(ctx, db.SumAssistantUsageWindowParams{
		MailUserID: id.UserID, Since: since,
	})
	if err != nil {
		// Failing to sum must not BLOCK the user: the quota is a commercial limit, not a security
		// barrier. A read error lets it through (the step ceiling from §9.1 still protects against
		// actual runaway usage). Fail-open here is the opposite of the confirmation step, and on purpose.
		log.Printf("assistant: quota sum: %v", err)
		return assistant.QuotaStatus{Limit: limit, Remaining: limit}
	}
	// MIN(created_at) comes back as interface{} (sqlc doesn't infer the type of an aggregate). A type
	// assertion resolves it; NULL (empty window) is left at the zero value, which QuotaOf treats as
	// "nothing expires".
	var oldest time.Time
	if t, ok := row.Oldest.(time.Time); ok {
		oldest = t
	}
	return assistant.QuotaOf(limit, int(row.Total), oldest, now)
}

// recordAssistantUsage records the tokens for a turn. No cancellation: the spend already happened
// and must count, even if the user closed the tab.
func (s *Server) recordAssistantUsage(ctx context.Context, id assistant.Identity, tokens int) {
	if tokens <= 0 {
		return
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.q.RecordAssistantUsage(saveCtx, db.RecordAssistantUsageParams{
		TenantID: id.TenantID, MailUserID: id.UserID, Tokens: int32(tokens),
	}); err != nil {
		log.Printf("assistant: record usage: %v", err)
	}
}

// handleAssistantUsage returns the quota state: how much is left and when it renews (§9.2). The
// screen displays it; the warning as it nears the end is the frontend's job, from here on.
func (s *Server) handleAssistantUsage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id := assistant.Identity{TenantID: mu.TenantID, UserID: mu.ID}
	writeJSON(w, http.StatusOK, s.assistantQuotaStatus(r.Context(), id, time.Now()))
}

// --- Visible audit trail (§6.4) ---

type toolCallJSON struct {
	ID         uuid.UUID       `json:"id"`
	Agent      string          `json:"agent"`
	Tool       string          `json:"tool"`
	Args       json.RawMessage `json:"args"`
	Result     string          `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	DurationMs int32           `json:"duration_ms"`
	CreatedAt  time.Time       `json:"created_at"`
}

// handleAssistantAudit lists the tool calls made for the user themself (§6.4). Readable by them,
// not just by the administrator: it's what answers "why was this email sent?".
func (s *Server) handleAssistantAudit(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	limit := int32(100)
	rows, err := s.q.ListAssistantToolCallsByUser(r.Context(), db.ListAssistantToolCallsByUserParams{
		TenantID: mu.TenantID, MailUserID: mu.ID, Lim: limit,
	})
	if err != nil {
		log.Printf("assistant: audit list: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	items := make([]toolCallJSON, 0, len(rows))
	for _, c := range rows {
		args := c.Args
		if len(args) == 0 || !json.Valid(args) {
			args = json.RawMessage("{}")
		}
		items = append(items, toolCallJSON{
			ID: c.ID, Agent: c.Agent, Tool: c.Tool, Args: args, Result: c.Result,
			Error: c.Error, DurationMs: c.DurationMs, CreatedAt: c.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// --- Folder exclusion (§6.6) ---

// assistantExcludedFolders returns the set of folders out of the assistant's reach. Used by the
// Drive search to filter what the assistant sees (§6.6).
func (s *Server) assistantExcludedFolders(ctx context.Context, tenant uuid.UUID) map[string]bool {
	rows, err := s.q.ListExcludedFolders(ctx, tenant)
	if err != nil {
		log.Printf("assistant: excluded folders: %v", err)
		return nil
	}
	out := make(map[string]bool, len(rows))
	for _, f := range rows {
		out[f.String()] = true
	}
	return out
}

// handleAssistantExcludedFolders lists the excluded folders.
func (s *Server) handleAssistantExcludedFolders(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListExcludedFolders(r.Context(), mu.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	ids := make([]string, 0, len(rows))
	for _, f := range rows {
		ids = append(ids, f.String())
	}
	writeJSON(w, http.StatusOK, map[string]any{"folder_ids": ids})
}

type excludeFolderReq struct {
	FolderID string `json:"folder_id"`
}

func (s *Server) handleAssistantExcludeFolder(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req excludeFolderReq
	if !decode(w, r, &req) {
		return
	}
	fid, err := uuid.Parse(req.FolderID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	// The folder must exist IN THIS tenant — an id from outside is never accepted.
	if _, err := s.q.GetFolderInTenant(r.Context(), db.GetFolderInTenantParams{ID: fid, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return
	}
	if err := s.q.AddExcludedFolder(r.Context(), db.AddExcludedFolderParams{
		TenantID: mu.TenantID, FolderID: fid,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAssistantUnexcludeFolder(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	fid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	if err := s.q.RemoveExcludedFolder(r.Context(), db.RemoveExcludedFolderParams{
		TenantID: mu.TenantID, FolderID: fid,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// registerAssistantGovernance registers the §9/§6.4/§6.6 routes. Called from the Router.
func (s *Server) registerAssistantGovernance(r chi.Router) {
	r.Get("/assistant/usage", s.handleAssistantUsage)
	r.Get("/assistant/audit", s.handleAssistantAudit)
	// Folder exclusion is organization-level scope control (§6.6): administration.
	r.Get("/assistant/excluded-folders", s.handleAssistantExcludedFolders)
	r.With(s.requirePerm(rbac.PermAssistantManage)).Get("/assistant/tools", s.handleAssistantToolsGet)
	r.With(s.requirePerm(rbac.PermAssistantManage)).Post("/assistant/tools/{tool}", s.handleAssistantToolSet)
	r.With(s.requirePerm(rbac.PermAssistantManage)).Post("/assistant/excluded-folders", s.handleAssistantExcludeFolder)
	r.With(s.requirePerm(rbac.PermAssistantManage)).Delete("/assistant/excluded-folders/{id}", s.handleAssistantUnexcludeFolder)
}

// --- Tools disabled by the organization (fine-grained governance) ---

// disabledAssistantTools reads the tools the organization has disabled. A read failure returns
// empty: the ceiling is an additional restriction, and losing it doesn't open anything that the
// other filters (disabled apps, agent permissions) weren't already controlling.
func (s *Server) disabledAssistantTools(ctx context.Context, tenant uuid.UUID) []string {
	tools, err := s.q.ListDisabledAssistantTools(ctx, tenant)
	if err != nil {
		return nil
	}
	return tools
}

// handleAssistantToolsGet lists the catalog and what is disabled.
func (s *Server) handleAssistantToolsGet(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"tools":    assistant.AllTools(),
		"disabled": orEmpty(s.disabledAssistantTools(r.Context(), mu.TenantID)),
	})
}

// handleAssistantToolSet turns ONE tool on/off for the entire organization.
func (s *Server) handleAssistantToolSet(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	tool := chi.URLParam(r, "tool")
	known := false
	for _, t := range assistant.AllTools() {
		if t == tool {
			known = true
			break
		}
	}
	if !known {
		writeErr(w, http.StatusBadRequest, "assistant.unknown_tool")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.q.SetAssistantToolEnabled(r.Context(), db.SetAssistantToolEnabledParams{
		TenantID: mu.TenantID, Tool: tool, Enabled: req.Enabled,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tool": tool, "enabled": req.Enabled})
}
