//go:build pro

package ee

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/reqauth"
)

func brandingRequest(t *testing.T, body updateBrandingReq) *http.Request {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPatch, "/api/branding", bytes.NewReader(raw))
	return r.WithContext(reqauth.WithMailUser(r.Context(), db.MailUser{}))
}

func TestUpdateBrandingRejectsInvalidColor(t *testing.T) {
	w := httptest.NewRecorder()
	UpdateBranding(w, brandingRequest(t, updateBrandingReq{Color: "not-a-color"}), Deps{})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestUpdateBrandingRejectsOversizedLogo(t *testing.T) {
	huge := "data:image/png;base64," + string(make([]byte, maxLogoBytes+1))
	w := httptest.NewRecorder()
	UpdateBranding(w, brandingRequest(t, updateBrandingReq{Logo: huge}), Deps{})
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestUpdateBrandingRejectsNonImageLogo(t *testing.T) {
	w := httptest.NewRecorder()
	UpdateBranding(w, brandingRequest(t, updateBrandingReq{Logo: "not a data uri"}), Deps{})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestIsHexColor(t *testing.T) {
	valid := []string{"#fff", "#FFFFFF", "#a1b2c3"}
	invalid := []string{"", "fff", "#ff", "#gggggg", "#12345"}
	for _, c := range valid {
		if !isHexColor(c) {
			t.Errorf("isHexColor(%q) = false, want true", c)
		}
	}
	for _, c := range invalid {
		if isHexColor(c) {
			t.Errorf("isHexColor(%q) = true, want false", c)
		}
	}
}
