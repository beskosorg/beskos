package notify

import (
	"strings"
	"testing"
	"time"
)

func TestBuildHasBothParts(t *testing.T) {
	m := AccountCreated("pt-BR", "ana@acme.pt", "acme.pt", "https://app.beskos.com/mail")
	if !strings.Contains(m.HTML, "<!doctype html>") {
		t.Error("missing the HTML part")
	}
	if !strings.Contains(m.HTML, "ana@acme.pt") || !strings.Contains(m.Text, "ana@acme.pt") {
		t.Error("the address has to appear in both parts")
	}
	if !strings.Contains(m.Text, "https://app.beskos.com/mail") {
		t.Error("the text version has to carry the link (there is no button to click)")
	}
}

func TestLanguage(t *testing.T) {
	if s := AccountCreated("en", "a@b.pt", "b.pt", "").Subject; !strings.Contains(s, "ready") {
		t.Errorf("English subject = %q", s)
	}
	if s := AccountCreated("pt-BR", "a@b.pt", "b.pt", "").Subject; !strings.Contains(s, "pronta") {
		t.Errorf("Portuguese subject = %q", s)
	}
	// An empty or unknown language falls back to Portuguese (the product's default language).
	if s := AccountCreated("", "a@b.pt", "b.pt", "").Subject; !strings.Contains(s, "pronta") {
		t.Errorf("default subject = %q", s)
	}
}

func TestEscaping(t *testing.T) {
	// A value containing HTML must not inject markup into the notice.
	m := NewLogin("pt-BR", "a@b.pt", "1.2.3.4", "<script>x</script>", "Chrome", time.Now(), "")
	if strings.Contains(m.HTML, "<script>") {
		t.Error("value not escaped in HTML")
	}
}

func TestNewLoginUnknownLocation(t *testing.T) {
	m := NewLogin("pt-BR", "a@b.pt", "1.2.3.4", "", "Chrome", time.Now(), "")
	if !strings.Contains(m.Text, "desconhecida") {
		t.Error("with no location, it has to say so instead of leaving it blank")
	}
}
