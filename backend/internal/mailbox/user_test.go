package mailbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewUserClientSendsBasicAuthHeader(t *testing.T) {
	var gotAuth, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	u := NewUserClient(srv.URL, "dGVzdDpwYXNz")
	if _, err := u.do(context.Background(), "/jmap/session", map[string]any{"x": 1}); err != nil {
		t.Fatalf("do: %v", err)
	}
	if gotAuth != "Basic dGVzdDpwYXNz" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Basic dGVzdDpwYXNz")
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type header = %q, want application/json for a POST body", gotContentType)
	}
}

func TestUserClientDoUsesGETWithoutBody(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	u := NewUserClient(srv.URL, "creds")
	if _, err := u.do(context.Background(), "/jmap/session", nil); err != nil {
		t.Fatalf("do: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET for a nil body", gotMethod)
	}
}

func TestUserClientDoReturnsErrorOnNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	u := NewUserClient(srv.URL, "creds")
	_, err := u.do(context.Background(), "/jmap/session", nil)
	if err == nil {
		t.Fatal("do returned no error for a 403 response")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error does not mention the status code: %v", err)
	}
}

func TestAccountIDResolvesMailAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"primaryAccounts": map[string]string{
				"urn:ietf:params:jmap:mail": "acc-1",
			},
		})
	}))
	defer srv.Close()

	u := NewUserClient(srv.URL, "creds")
	id, err := u.accountID(context.Background())
	if err != nil {
		t.Fatalf("accountID: %v", err)
	}
	if id != "acc-1" {
		t.Errorf("accountID = %q, want acc-1", id)
	}
}

func TestAccountIDErrorsWhenNoMailAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"primaryAccounts": map[string]string{
				"urn:ietf:params:jmap:calendars": "acc-1",
			},
		})
	}))
	defer srv.Close()

	u := NewUserClient(srv.URL, "creds")
	if _, err := u.accountID(context.Background()); err == nil {
		t.Fatal("accountID succeeded even though the session has no mail account")
	}
}

func TestInboxIDFindsInboxRole(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"methodResponses": []any{
				[]any{"Mailbox/get", map[string]any{
					"list": []any{
						map[string]any{"id": "box-drafts", "role": "drafts"},
						map[string]any{"id": "box-inbox", "role": "inbox"},
					},
				}, "m"},
			},
		})
	}))
	defer srv.Close()

	u := NewUserClient(srv.URL, "creds")
	id, err := u.inboxID(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("inboxID: %v", err)
	}
	if id != "box-inbox" {
		t.Errorf("inboxID = %q, want box-inbox", id)
	}
}

func TestInboxIDErrorsWhenNoInboxRole(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"methodResponses": []any{
				[]any{"Mailbox/get", map[string]any{"list": []any{}}, "m"},
			},
		})
	}))
	defer srv.Close()

	u := NewUserClient(srv.URL, "creds")
	if _, err := u.inboxID(context.Background(), "acc-1"); err == nil {
		t.Fatal("inboxID succeeded even though no mailbox has the inbox role")
	}
}

// TestSubmitEmailRejectsOnNotCreated verifies the caller sees an error when Stalwart refuses the
// submission (notCreated), rather than SubmitEmail reporting success on a rejected send.
func TestSubmitEmailRejectsOnNotCreated(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/jmap/session", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"primaryAccounts": map[string]string{"urn:ietf:params:jmap:mail": "acc-1"},
		})
	})
	mux.HandleFunc("/jmap/", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			MethodCalls []json.RawMessage `json:"methodCalls"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		var call []json.RawMessage
		json.Unmarshal(body.MethodCalls[0], &call)
		var name string
		json.Unmarshal(call[0], &name)

		switch name {
		case "Mailbox/get":
			json.NewEncoder(w).Encode(map[string]any{
				"methodResponses": []any{
					[]any{"Mailbox/get", map[string]any{"list": []any{}}, "m"},
				},
			})
		case "EmailSubmission/set":
			json.NewEncoder(w).Encode(map[string]any{
				"methodResponses": []any{
					[]any{"EmailSubmission/set", map[string]any{
						"notCreated": map[string]any{"sub": map[string]any{"type": "forbidden"}},
					}, "s"},
				},
			})
		default:
			t.Fatalf("unexpected JMAP method call: %q", name)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	u := NewUserClient(srv.URL, "creds")
	err := u.SubmitEmail(context.Background(), "email-1", "identity-1", "me@example.com", []string{"you@example.com"})
	if err == nil {
		t.Fatal("SubmitEmail returned no error for a notCreated/forbidden response")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Errorf("error does not indicate the submission was rejected: %v", err)
	}
}
