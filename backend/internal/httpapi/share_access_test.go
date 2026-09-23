package httpapi

import (
	"testing"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// shareAccess decides access by the BEST of the direct share and the organization share. It's the
// piece that makes "anyone in the organization" a real access, not just a label — hence the test.
func TestShareAccess(t *testing.T) {
	sh := func(principal, level string) db.StorageShare {
		return db.StorageShare{Principal: principal, Level: level}
	}
	cases := []struct {
		name               string
		shares             []db.StorageShare
		email              string
		wantHas, wantWrite bool
	}{
		{"no shares", nil, "ana@x.pt", false, false},
		{"direct reader share", []db.StorageShare{sh("ana@x.pt", "leitor")}, "ana@x.pt", true, false},
		{"direct editor share", []db.StorageShare{sh("ana@x.pt", "editor")}, "ana@x.pt", true, true},
		{"organization reader share grants access to anyone", []db.StorageShare{sh(orgSharePrincipal, "leitor")}, "bob@x.pt", true, false},
		{"organization editor share", []db.StorageShare{sh(orgSharePrincipal, "editor")}, "bob@x.pt", true, true},
		{"direct editor beats organization reader", []db.StorageShare{sh(orgSharePrincipal, "leitor"), sh("ana@x.pt", "editor")}, "ana@x.pt", true, true},
		{"another person's share doesn't grant me access", []db.StorageShare{sh("other@x.pt", "editor")}, "ana@x.pt", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			write, has := shareAccess(c.shares, c.email)
			if has != c.wantHas || write != c.wantWrite {
				t.Errorf("shareAccess = (write=%v, has=%v), want (write=%v, has=%v)", write, has, c.wantWrite, c.wantHas)
			}
		})
	}
}
