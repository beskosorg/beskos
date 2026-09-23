package notify

import (
	"strings"
	"testing"
)

func TestMFACodeFollowsTheRecipientLanguage(t *testing.T) {
	pt := MFACode("pt-BR", "123456")
	if !strings.Contains(pt, "123456") || strings.Contains(pt, "access code") {
		t.Errorf("pt-BR message = %q, want the Portuguese wording with the code", pt)
	}
	en := MFACode("en", "123456")
	if !strings.Contains(en, "123456") || !strings.Contains(en, "access code") {
		t.Errorf("en message = %q, want the English wording with the code", en)
	}
}

func TestMFAEnrollCodeFollowsTheRecipientLanguage(t *testing.T) {
	pt := MFAEnrollCode("pt-BR", "654321")
	if !strings.Contains(pt, "654321") || strings.Contains(pt, "verification code") {
		t.Errorf("pt-BR message = %q, want the Portuguese wording with the code", pt)
	}
	en := MFAEnrollCode("en", "654321")
	if !strings.Contains(en, "654321") || !strings.Contains(en, "verification code") {
		t.Errorf("en message = %q, want the English wording with the code", en)
	}
}
