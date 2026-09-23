package mailbox

// Handing a whole mailbox back, as a file somebody can open.
//
// Article 28(3)(g) of the GDPR gives the customer a choice at the end: delete the data, OR return
// it. A product that only implements the first half is not offering a choice, so this is not a
// nice-to-have -- it is the other half of the delete button.
//
// The format is mbox, because it is the one every mail client on earth can import and because it is
// plain text: a customer can read it with `less` in ten years, with no software from us. Each
// message is written EXACTLY as it was received, headers and all -- an export that reformats is an
// export that loses the evidence somebody is exporting for.
//
// STREAMED, NEVER ASSEMBLED. A mailbox is measured in gigabytes; building one in memory to send it
// would take the server down on the day a customer with real volume finally leaves.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// exportPage is how many message ids are asked for at a time. Large enough that a big mailbox is
// not thousands of round trips, small enough that the query never times out.
const exportPage = 200

// ExportMbox writes every message in the account to w, in mbox format, oldest first.
//
// Returns the number of messages written. A message that cannot be downloaded is skipped rather
// than aborting the export: a single unreadable blob must not cost somebody the other 40,000
// messages, and the count lets the caller report the difference honestly.
func (u *UserClient) ExportMbox(ctx context.Context, w io.Writer) (int, error) {
	acc, err := u.accountID(ctx)
	if err != nil {
		return 0, err
	}
	out := bufio.NewWriterSize(w, 64*1024)
	written := 0
	for pos := 0; ; pos += exportPage {
		ids, total, err := u.emailPage(ctx, acc, pos)
		if err != nil {
			return written, err
		}
		if len(ids) == 0 {
			break
		}
		for _, m := range ids {
			raw, err := u.downloadBlob(ctx, acc, m.BlobID)
			if err != nil {
				continue // skipped, not fatal: see the doc comment
			}
			if err := writeMboxMessage(out, m, raw); err != nil {
				return written, err
			}
			written++
		}
		if pos+len(ids) >= total {
			break
		}
	}
	return written, out.Flush()
}

type exportMsg struct {
	ID         string    `json:"id"`
	BlobID     string    `json:"blobId"`
	ReceivedAt time.Time `json:"receivedAt"`
	From       []struct {
		Email string `json:"email"`
	} `json:"from"`
}

// emailPage asks for one page of messages across the WHOLE account -- no mailbox filter, so drafts,
// sent, archive and anything the person made themselves all come out. An export that quietly
// covered only the inbox would be worse than none: it looks complete.
func (u *UserClient) emailPage(ctx context.Context, acc string, pos int) ([]exportMsg, int, error) {
	resp, err := u.jmap(ctx, []any{
		[]any{"Email/query", map[string]any{
			"accountId": acc, "position": pos, "limit": exportPage,
			"sort":           []any{map[string]any{"property": "receivedAt", "isAscending": true}},
			"calculateTotal": true,
		}, "q"},
		[]any{"Email/get", map[string]any{
			"accountId":  acc,
			"#ids":       map[string]any{"resultOf": "q", "name": "Email/query", "path": "/ids"},
			"properties": []string{"id", "blobId", "receivedAt", "from"},
		}, "g"},
	})
	if err != nil || len(resp) < 2 {
		return nil, 0, err
	}
	var q struct {
		Total int `json:"total"`
	}
	_ = json.Unmarshal(resp[0], &q)
	var g struct {
		List []exportMsg `json:"list"`
	}
	if err := json.Unmarshal(resp[1], &g); err != nil {
		return nil, 0, err
	}
	return g.List, q.Total, nil
}

// downloadBlob fetches one message exactly as it arrived.
func (u *UserClient) downloadBlob(ctx context.Context, acc, blobID string) ([]byte, error) {
	url := fmt.Sprintf("%s/jmap/download/%s/%s/message.eml?accept=message/rfc822",
		strings.TrimRight(u.base, "/"), acc, blobID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Basic "+u.basic)
	resp, err := u.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", blobID, resp.StatusCode)
	}
	// Capped so a corrupt blobId claiming a huge length cannot pull the server over.
	return io.ReadAll(io.LimitReader(resp.Body, 256<<20))
}

// writeMboxMessage writes one message in mbox format.
//
// The "From " separator line is what divides messages in this format, so any line in the body that
// begins with "From " has to be escaped -- otherwise a message quoting an email header splits into
// two, and the reader sees a message that nobody sent. This is the oldest bug in mail archiving and
// it is one line to avoid.
func writeMboxMessage(w *bufio.Writer, m exportMsg, raw []byte) error {
	sender := "MAILER-DAEMON"
	if len(m.From) > 0 && m.From[0].Email != "" {
		sender = m.From[0].Email
	}
	when := m.ReceivedAt
	if when.IsZero() {
		when = time.Unix(0, 0).UTC()
	}
	if _, err := fmt.Fprintf(w, "From %s %s\n", sender, when.UTC().Format(time.ANSIC)); err != nil {
		return err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "From ") {
			if _, err := w.WriteString(">"); err != nil {
				return err
			}
		}
		if _, err := w.WriteString(line + "\n"); err != nil {
			return err
		}
	}
	_, err := w.WriteString("\n")
	return err
}
