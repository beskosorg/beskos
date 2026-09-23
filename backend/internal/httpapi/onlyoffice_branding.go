package httpapi

// Beskos branding in the office editor's header and "About" dialog.
//
// The Document Server Community only honors these parameters with the startup branding patch
// (deploy/onlyoffice-branding-patch.sh). Without the patch, the editor silently drops them and the
// header keeps the original brand — the app still works all the same.
//
// The images are URLs served by the Document Server ITSELF (the same patch copies them into its
// webroot). Don't use a data: URI here: the DS's api.js passes logo.{image,imageDark,imageLight}
// in the editor iframe's query string, and three percent-encoded data URIs exceed nginx's request
// line limit — the editor opens with "414 Request-URI Too Large".
//
// Attribution (AGPL v3 §5 and §13, plus Ascensio System SIA's trademark policy): the "About"
// dialog keeps showing "Powered by / Ascensio System SIA / onlyoffice.com" — the Document Server
// itself reveals it when we send `customer` — and we add, in `customer.info`, the modified-version
// notice, with the date and the address of the modified source. None of this is optional: we serve
// the editor over the network, so we owe the notice and the source offer to whoever uses it.

// brandLogoPath is where the startup patch leaves the logos inside the Document Server.
const brandLogoPath = "/web-apps/apps/common/main/resources/img/beskos/"

// officeModifiedOn is the Document Server's modification date, required by the AGPL's additional
// terms ("prominent notices ... including the date of such modification"). Always update it
// whenever the branding patch changes.
const officeModifiedOn = "2026-07-27"

const officeUpstreamNotice = "Based on ONLYOFFICE® Docs Community Edition, by Ascensio System SIA. " +
	"Version modified by Beskos (interface and branding) on " + officeModifiedOn + " — this is not the " +
	"original software and is not endorsed by Ascensio System SIA. License: GNU AGPL v3."

// applyBranding adds the Beskos brand to the editor config's customization object. Receives the
// map already built by the handler, to avoid duplicating the config construction in two places.
func (o *onlyoffice) applyBranding(customization map[string]any) {
	light := o.publicURL + brandLogoPath + "wordmark-light.svg"
	dark := o.publicURL + brandLogoPath + "wordmark-dark.svg"

	customization["logo"] = map[string]any{
		"image":      light,
		"imageLight": light,
		"imageDark":  dark,
		"url":        o.brandSiteURL(),
	}
	// "About" is where the legal notice lives, so it's turned on EXPLICITLY. With the patch,
	// `canBrandingExt` becomes true and the Document Server stops forcing it on its own — without
	// this line, all it would take is someone setting `about: false` in the customization for the
	// attribution to disappear from the UI.
	customization["about"] = true
	customization["customer"] = map[string]any{
		"name":     "Beskos",
		"logo":     light,
		"logoDark": dark,
		"www":      o.brandSiteURL(),
		"info":     o.attributionNotice(),
	}
}

// attributionNotice is the text shown in "About". Adds the modified-source offer (§13) when
// ONLYOFFICE_SOURCE_URL is configured; without it, the notice still goes out, but incomplete —
// which is why the config refuses to start without the address in production.
func (o *onlyoffice) attributionNotice() string {
	if o.sourceURL == "" {
		return officeUpstreamNotice
	}
	return officeUpstreamNotice + " Source of the modifications: " + o.sourceURL
}

// brandSiteURL is what the editor's logo links to. Its caller (cmd/api's main) already resolves
// the fallback chain (PUBLIC_URL, then BRAND_URL); an empty result here just omits the link.
func (o *onlyoffice) brandSiteURL() string { return o.brandURL }
