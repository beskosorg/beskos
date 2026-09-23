package httpapi

import (
	"strings"
	"testing"
)

// The brand only reaches the editor if `logo` and `customer` make it into the config; and the
// attribution to Ascensio System SIA must always travel, because it's what satisfies the AGPL in "About".
func TestApplyBrandingSetsLogoAndAttribution(t *testing.T) {
	o := &onlyoffice{publicURL: "http://localhost:8082"}
	c := map[string]any{"uiTheme": "theme-light"}
	o.applyBranding(c)

	logo, ok := c["logo"].(map[string]any)
	if !ok {
		t.Fatal("customization.logo missing — the header would keep the original brand")
	}
	// Short URL, served by the Document Server itself. A data: URI here blows up the editor
	// iframe's query string (nginx 414) — that's how it broke the first time.
	for _, k := range []string{"image", "imageLight", "imageDark"} {
		v, _ := logo[k].(string)
		if !strings.HasPrefix(v, "http://localhost:8082"+brandLogoPath) {
			t.Errorf("logo.%s should be a URL on the Document Server, got %q", k, v)
		}
		if len(v) > 200 {
			t.Errorf("logo.%s has %d chars — it goes into the iframe's query string, it must be short", k, len(v))
		}
	}

	customer, ok := c["customer"].(map[string]any)
	if !ok {
		t.Fatal("customization.customer missing — without it, About doesn't show our identification")
	}
	if customer["name"] != "Beskos" {
		t.Errorf("customer.name = %v, wanted Beskos", customer["name"])
	}
	info, _ := customer["info"].(string)
	for _, want := range []string{"Ascensio System SIA", "ONLYOFFICE", "AGPL", officeModifiedOn} {
		if !strings.Contains(info, want) {
			t.Errorf("customer.info doesn't mention %q: %q", want, info)
		}
	}
	// Without "About" visible there's no legal notice in the UI — and with the patch the DS no
	// longer forces it on its own. It must always be turned on.
	if c["about"] != true {
		t.Error("customization.about must be true: it's where the attribution is visible to the user")
	}
	if c["uiTheme"] != "theme-light" {
		t.Error("applyBranding must not touch the other customization parameters")
	}
}

// With ONLYOFFICE_SOURCE_URL configured, the notice includes the source offer (AGPL §13).
func TestAttributionNoticeIncludesSourceURL(t *testing.T) {
	const src = "https://github.com/beskos/docs-server"
	o := &onlyoffice{sourceURL: src}
	if got := o.attributionNotice(); !strings.Contains(got, src) {
		t.Errorf("notice missing the modified source's address: %q", got)
	}
	if got := (&onlyoffice{}).attributionNotice(); strings.Contains(got, "Source of the modifications") {
		t.Error("without sourceURL the notice must not promise an address we don't have")
	}
}
