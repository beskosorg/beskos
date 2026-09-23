package sms

import "testing"

func TestPublishForm(t *testing.T) {
	f := publishForm("+351912345678", "Seu código: 123456", "")
	if f.Get("Action") != "Publish" || f.Get("PhoneNumber") != "+351912345678" {
		t.Fatalf("base fields wrong: %v", f)
	}
	if f.Get("MessageAttributes.entry.1.Value.StringValue") != "Transactional" {
		t.Errorf("MFA SMS must be Transactional")
	}
	if f.Get("MessageAttributes.entry.2.Name") != "" {
		t.Errorf("sender id must be absent when empty")
	}
	f2 := publishForm("+351912345678", "x", "Beskos")
	if f2.Get("MessageAttributes.entry.2.Value.StringValue") != "Beskos" {
		t.Errorf("sender id missing")
	}
}
