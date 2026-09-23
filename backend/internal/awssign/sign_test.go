package awssign

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSigningRetainsProviderGoldenSignature(t *testing.T) {
	body := []byte(`{"EmailIdentity":"acme.com"}`)
	req, err := http.NewRequest(http.MethodPost, "https://email.eu-west-1.amazonaws.com/v2/email/identities", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	SignRequest(req, body, "eu-west-1", "ses", "AKIDEXAMPLE", "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
		time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))
	const signature = "Signature=bb65686bf9e2a8f321f1dacce892c5e14a0ce99dd37187adee7cd49dce4b6e4b"
	if !strings.HasSuffix(req.Header.Get("Authorization"), signature) {
		t.Fatalf("signature changed during extraction: %s", req.Header.Get("Authorization"))
	}
}

func TestSMSSigningUsesSNSServiceScope(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	req, err := http.NewRequest(http.MethodPost, "https://sns.eu-west-1.amazonaws.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	SignRequest(req, []byte("Action=Publish&Message=123456"), "eu-west-1", "sns", "example-key", "example-secret", now)
	before := req.Header.Get("Authorization")
	if !strings.Contains(before, "/eu-west-1/sns/aws4_request") {
		t.Fatalf("incorrect SMS scope: %s", before)
	}
	SignRequest(req, []byte("Action=Publish&Message=654321"), "eu-west-1", "sns", "example-key", "example-secret", now)
	if before == req.Header.Get("Authorization") {
		t.Fatal("signature did not bind the message body")
	}
}
