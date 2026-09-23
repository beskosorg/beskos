package httpapi

// Handing a deleted mailbox back before it is destroyed.
//
// This is the "or return" of Article 28(3)(g), and the reason the recovery window exists at all: an
// admin who deleted the wrong person, or a company that has to produce an ex-employee's
// correspondence for a court, has thirty days to get the data out in a form nobody needs our
// software to read.
//
// WHY ONLY A DELETED MAILBOX. Reading somebody's mail requires their credentials, and a live
// mailbox's credentials belong to them -- minting a new one would lock the owner out to satisfy an
// export. A deleted mailbox has none left to destroy: suspension already removed them, and the way
// back is a password reset in either case. So the temporary credential minted here costs the owner
// nothing that was not already gone, and it is thrown away the moment the export ends.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"

	"github.com/beskosorg/beskos/backend/internal/mailbox"
)

// handleExportMailbox streams the mailbox as an mbox file.
func (s *Server) handleExportMailbox(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	target, ok := s.ownedMailbox(w, r)
	if !ok {
		return
	}
	if target.Status != "deleted" {
		// A live mailbox belongs to its owner; taking a copy of it behind their back is not an
		// administrative convenience, it is surveillance. Suspend or delete it first -- which is a
		// decision somebody has to make and be recorded making.
		writeErr(w, http.StatusConflict, "mailbox.export_requires_deleted")
		return
	}
	if s.sw == nil || target.StalwartAccountID == nil {
		writeErr(w, http.StatusServiceUnavailable, "mailbox.export_unavailable")
		return
	}

	pw, err := tempPassword()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "mailbox.export_failed")
		return
	}
	if err := s.sw.SetAccountPassword(r.Context(), *target.StalwartAccountID, pw); err != nil {
		log.Printf("mailbox export %s: minting a credential failed: %v", target.Email, err)
		writeErr(w, http.StatusBadGateway, "mailbox.export_failed")
		return
	}
	// Take it away again whatever happens, including a panic on the way out. A mailbox left with a
	// working password because an export failed halfway is an ex-employee who can read their mail
	// again, and nobody would ever know.
	defer func() {
		if err := s.sw.SuspendAccount(context.Background(), *target.StalwartAccountID); err != nil {
			log.Printf("mailbox export %s: FAILED TO REMOVE the temporary credential: %v — "+
				"that mailbox can be logged into until this is fixed by hand", target.Email, err)
		}
	}()

	// The parked account answers to a different address now, and the credential is the address.
	parked := parkedLocal(target) + "@" + domainOf(target.Email)
	basic := base64.StdEncoding.EncodeToString([]byte(parked + ":" + pw))

	w.Header().Set("Content-Type", "application/mbox")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", target.Email+".mbox"))
	n, err := mailbox.NewUserClient(s.sw.BaseURL(), basic).ExportMbox(r.Context(), w)
	if err != nil {
		// The status line and part of the body are already sent, so this cannot become an HTTP error.
		// Saying it in the log and in the file itself is the only honest option left: a truncated
		// export that looks complete is the worst possible outcome for the thing it is used for.
		log.Printf("mailbox export %s: stopped after %d messages: %v", target.Email, n, err)
		fmt.Fprintf(w, "\n\n; INCOMPLETE EXPORT: stopped after %d messages (%v)\n", n, err)
		return
	}
	log.Printf("mailbox export %s: %d messages, requested by %s", target.Email, n, u.Email)
}

// tempPassword is long and random enough to pass Stalwart's strength check and to be worthless to
// guess in the seconds it exists.
func tempPassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "Xp7!" + base64.RawURLEncoding.EncodeToString(b), nil
}
