# Document Server with the Beskos brand.
#
# In dev, compose mounts the scripts into the official image and the patch runs at startup. In
# production, CapRover deploys BY IMAGE and doesn't mount anything from the repository — so the
# modification has to live inside the image. That's what this Dockerfile does.
#
# The patch is applied at BUILD time (immutable, and the build fails loudly if a future Document
# Server version changes the patterns, instead of the problem only surfacing in production). The
# entrypoint calls it again as a safety net: it's idempotent and reports "already applied".
#
# LICENSE: the image produced here is a modified version of ONLYOFFICE® Docs Community Edition,
# by Ascensio System SIA, under AGPL v3. The corresponding source is public:
# https://github.com/denerFernandes/beskos-docs-server — and that's what the api's
# ONLYOFFICE_SOURCE_URL points to, which the editor shows in "About". When changing the base
# version, update the pinned commits in that repository's README.

# Pinned by digest (Docs 9.4.0-129, Ubuntu 24.04) — the patch depends on the NON-minified
# web-apps, which only exists from 9.4 onward.
FROM onlyoffice/documentserver@sha256:e3da62a847b9a5d51a11f73cfea1d9c13c3be3809614490d4edddcf01dcf919b

COPY brand/ /scripts/brand/
COPY onlyoffice-branding-patch.sh onlyoffice-ai-init.sh /scripts/

# Applies the brand and verifies it actually took: without this check, a pattern that stopped
# matching would go unnoticed and the image would ship to production with the ONLYOFFICE brand.
RUN sh /scripts/onlyoffice-branding-patch.sh \
	&& grep -q beskos-branding-patch /var/www/onlyoffice/documentserver/web-apps/apps/documenteditor/main/app/controller/Main.js \
	&& grep -q beskos-branding-patch /var/www/onlyoffice/documentserver/web-apps/apps/documenteditor/main/resources/css/app.css \
	&& test -f /var/www/onlyoffice/documentserver/web-apps/apps/common/main/resources/img/beskos/wordmark-light.svg

# Injects the AI config (needs the envs, so only at startup) and then starts the Document Server.
ENTRYPOINT ["/bin/sh", "/scripts/onlyoffice-ai-init.sh"]
