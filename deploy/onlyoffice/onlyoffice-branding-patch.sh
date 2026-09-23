#!/bin/sh
# Document Server branding patch (Beskos).
#
# WHY: in Docs Community, the server reports the license with `branding: false` and
# `customization: false`, and the client discards `editorConfig.customization.logo` and
# `.customer` without warning. The patch flips both switches ON THE CLIENT, so the editor
# header shows the Beskos brand instead of ONLYOFFICE's.
#
# WHERE: web-apps/apps/<editor>/main/app/controller/Main.js — from Docs 9.4 onward,
# web-apps ships WITHOUT minification, so there are two readable lines per editor:
#
#   this.appOptions.canBrandingExt = params.asc_getCanBranding() && (...)   -> layout/loaderLogo
#   this.appOptions.canBranding    = params.asc_getCustomization()          -> logo + "About"
#
# LICENSE (AGPL v3, section 5 + Ascensio System SIA's trademark policy):
#   - the modification is allowed and is recorded in deploy/DIVERGENCES.md, with a date;
#   - Beskos serves this editor over the network, so §13 requires offering the modified
#     source: that's what this file is, and "About" links to it;
#   - attribution is NOT removed. The "About" dialog still shows the
#     "Powered by / Ascensio System SIA / onlyoffice.com" block (id-about-licensor-short, which
#     setLicInfo itself reveals when we send `customer`), and the backend adds the modified
#     version note in customization.customer.info;
#   - what we remove is THEIR brand from our header — which is exactly what the trademark
#     policy requires from anyone publishing a modified version: your own identity, no imitating.
#
# ALWAYS fails safe: if something goes wrong, the Document Server starts up anyway, just
# without branding. Idempotent — reapplying doesn't duplicate anything.

WEBAPPS=/var/www/onlyoffice/documentserver/web-apps/apps
BRANDDIR="$WEBAPPS/common/main/resources/img/beskos"

MARKER=beskos-branding-patch
PATCHED=0
SKIPPED=0

# The logo images live INSIDE the Document Server, not as a data: URI. The DS's api.js puts
# customization.logo.{image,imageDark,imageLight} in the editor iframe's QUERY STRING — three
# percent-encoded data URIs blow the request-line limit and nginx answers 414. Served from here,
# they're three short URLs on the editor's own origin (no CORS, no dependency on the app being exposed).
if [ -d /scripts/brand ]; then
	mkdir -p "$BRANDDIR" && cp /scripts/brand/*.svg "$BRANDDIR/" 2>/dev/null &&
		echo "branding patch: logos copied to $BRANDDIR" ||
		echo "branding patch: failed to copy logos — the header ends up without an image"

fi

for app in documenteditor spreadsheeteditor presentationeditor pdfeditor visioeditor; do
	f="$WEBAPPS/$app/main/app/controller/Main.js"
	[ -f "$f" ] || continue

	if grep -q "$MARKER" "$f" 2>/dev/null; then
		SKIPPED=$((SKIPPED + 1))
		continue
	fi

	# Only the startup value changes; the rest of the expression (the `&& customization is an
	# object` part) stays, so branding still depends on us actually sending a customization object.
	sed -i \
		-e "s|\(this\.appOptions\.canBrandingExt *= *\)params\.asc_getCanBranding()|\1true /* $MARKER */|" \
		-e "s|\(this\.appOptions\.canBranding *= *\)params\.asc_getCustomization()|\1true /* $MARKER */|" \
		"$f" 2>/dev/null || continue

	if ! grep -q "$MARKER" "$f" 2>/dev/null; then
		echo "branding patch: $app — pattern not found (newer DS version?), skipped"
		continue
	fi

	# The DS's nginx serves with gzip_static: without regenerating the .gz, the browser gets the original.
	if [ -f "$f.gz" ]; then
		gzip -9 -c "$f" > "$f.gz" 2>/dev/null || echo "branding patch: $app — failed to regenerate the .gz"
	fi
	PATCHED=$((PATCHED + 1))
done

# Beskos palette: appends our variables to the END of each editor's app.css (wins by order, at
# equal specificity). See the beskos-theme.css header for why it's not a themes.json theme.
# Idempotent via the same marker.
if [ -f /scripts/brand/beskos-theme.css ]; then
	THEMED=0
	for app in documenteditor spreadsheeteditor presentationeditor pdfeditor visioeditor; do
		c="$WEBAPPS/$app/main/resources/css/app.css"
		[ -f "$c" ] || continue
		grep -q "$MARKER" "$c" 2>/dev/null && continue
		{
			printf '\n/* %s */\n' "$MARKER"
			cat /scripts/brand/beskos-theme.css
		} >> "$c" 2>/dev/null || continue
		[ -f "$c.gz" ] && gzip -9 -c "$c" > "$c.gz" 2>/dev/null
		THEMED=$((THEMED + 1))
	done
	echo "branding patch: palette applied to $THEMED editor(s)"
fi

echo "branding patch: $PATCHED editor(s) modified, $SKIPPED already applied"
