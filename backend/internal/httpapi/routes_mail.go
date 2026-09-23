package httpapi

import (
	"github.com/go-chi/chi/v5"

	"github.com/beskosorg/beskos/backend/internal/rbac"
)

// Routes that act on the mailbox itself rather than on the organisation around it: deferring a
// message, holding one back before it goes, canned replies, and bringing a mailbox in from the
// provider the customer is leaving.
//
// Split out of server.go when that file reached its 600-line limit (law 1.4). The route table there
// stays a table; this is one subject.
func (s *Server) mountMail(r chi.Router) {
	r.Post("/mail/snooze", s.handleSnoozeEmail)         // snooze an email (the worker returns it to the inbox)
	r.Post("/mail/outbox", s.handleCreateOutbox)        // agenda o envio (undo-send / agendado)
	r.Get("/mail/outbox", s.handleListOutbox)           // emails not yet sent
	r.Delete("/mail/outbox/{id}", s.handleCancelOutbox) // cancela um envio pendente
	r.Get("/mail/templates", s.handleListTemplates)     // templates de resposta (canned)
	r.Post("/mail/templates", s.handleCreateTemplate)
	r.Patch("/mail/templates/{id}", s.handleUpdateTemplate)
	r.Delete("/mail/templates/{id}", s.handleDeleteTemplate)

	// Moving in from the old provider. These need no permission: the only account they can read
	// from is the one whose password the caller just typed, and the only one they write to is their
	// own. /check copies nothing -- it exists so a wrong password is found on the form.
	r.Post("/me/migration/check", s.handleCheckMigrationSource)
	r.Post("/me/migration", s.handleCreateMigration)
	// From a file on the customer's own disk -- the mail no IMAP login can reach.
	r.Post("/me/migration/file", s.handleUploadMigrationFile)
	r.Get("/me/migration", s.handleGetMigration)
	// The offer made on first sign-in, answered once per person rather than once per device.
	r.Post("/me/migration/dismiss", s.handleDismissMigrationPrompt)
	r.Delete("/me/migration/{id}", s.handleCancelMigration)
	// The organisation-wide view is a different thing: whoever manages mailboxes follows the move
	// for everybody.
	r.With(s.requirePerm(rbac.PermMailboxManage)).Get("/migrations", s.handleListTenantMigrations)
}
