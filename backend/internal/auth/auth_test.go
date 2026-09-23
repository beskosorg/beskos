package auth

import "testing"

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" || hash == "correct horse battery staple" {
		t.Fatalf("hash looks unhashed: %q", hash)
	}
	if !CheckPassword(hash, "correct horse battery staple") {
		t.Error("CheckPassword rejected the correct password")
	}
	if CheckPassword(hash, "wrong password") {
		t.Error("CheckPassword accepted a wrong password")
	}
}

func TestHashPasswordIsSalted(t *testing.T) {
	h1, err := HashPassword("same input")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	h2, err := HashPassword("same input")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if h1 == h2 {
		t.Error("two hashes of the same password are identical -- bcrypt should salt each call")
	}
	if !CheckPassword(h1, "same input") || !CheckPassword(h2, "same input") {
		t.Error("both salted hashes should still validate the original password")
	}
}

func TestCheckPasswordRejectsGarbageHash(t *testing.T) {
	if CheckPassword("not-a-bcrypt-hash", "anything") {
		t.Error("CheckPassword accepted a malformed hash instead of failing closed")
	}
}

func TestNewSessionTokenIsUniqueAndURLSafe(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		tok, err := NewSessionToken()
		if err != nil {
			t.Fatalf("NewSessionToken: %v", err)
		}
		if len(tok) == 0 {
			t.Fatal("NewSessionToken returned an empty token")
		}
		for _, r := range tok {
			if r == '+' || r == '/' || r == '=' {
				t.Fatalf("token contains a non-URL-safe base64 character: %q in %q", r, tok)
			}
		}
		if seen[tok] {
			t.Fatalf("NewSessionToken produced a duplicate token: %q", tok)
		}
		seen[tok] = true
	}
}
