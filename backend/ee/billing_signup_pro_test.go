//go:build pro

package ee

import "testing"

func TestDomainRe(t *testing.T) {
	ok := []string{"acme.com", "acme.co.uk", "a-b.pt", "sub.dominio.pl", "x1.io"}
	for _, d := range ok {
		if !domainRe.MatchString(d) {
			t.Errorf("%q should be accepted", d)
		}
	}
	bad := []string{"", "acme", "acme.", ".com", "-acme.com", "acme-.com", "ac me.com", "ACME.com", "http://acme.com"}
	for _, d := range bad {
		if domainRe.MatchString(d) {
			t.Errorf("%q should not be accepted", d)
		}
	}
}

func TestLoginRe(t *testing.T) {
	for _, l := range []string{"dener", "ana.costa", "j_p", "a1", "x-y"} {
		if !loginRe.MatchString(l) {
			t.Errorf("%q should be accepted", l)
		}
	}
	for _, l := range []string{"", ".ana", "ana.", "an a", "Ana", "a@b", "ç"} { // lang:ok
		if loginRe.MatchString(l) {
			t.Errorf("%q should not be accepted", l)
		}
	}
}

func TestAtoiOr(t *testing.T) {
	cases := map[string]int{"": 14, "7": 7, "30": 30, "abc": 14, "1x": 14, "0": 0}
	for in, want := range cases {
		if got := atoiOr(in, 14); got != want {
			t.Errorf("atoiOr(%q) = %d, want %d", in, got, want)
		}
	}
}
