// Package reqauth carries the authenticated mail user through a request's context. It exists so
// that both httpapi (which resolves the session and sets it) and ee (whose routes run behind the
// same authenticated router group but cannot import httpapi, which imports ee) can agree on the
// same context key without a circular import.
package reqauth

import (
	"context"

	"github.com/beskosorg/beskos/backend/internal/db"
)

type ctxKey string

const mailUserKey ctxKey = "mailUser"

// WithMailUser attaches the authenticated user to ctx.
func WithMailUser(ctx context.Context, mu db.MailUser) context.Context {
	return context.WithValue(ctx, mailUserKey, mu)
}

// MailUserFrom reads the user WithMailUser attached. It panics if none was set, the same
// contract httpapi's own session/JMAP-proxy middleware already relies on: every route behind
// that middleware chain is guaranteed one.
func MailUserFrom(ctx context.Context) db.MailUser {
	return ctx.Value(mailUserKey).(db.MailUser)
}
