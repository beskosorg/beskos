# Copies the reverse proxy's certificate for the mail hostname into Stalwart. See sync.sh for why
# this exists at all: on a single host the proxy owns ports 80 and 443, so Stalwart cannot renew
# its own certificate over ACME.
FROM alpine:3.20

RUN apk add --no-cache bash curl jq openssl coreutils

COPY sync.sh /usr/local/bin/sync.sh
RUN chmod +x /usr/local/bin/sync.sh

ENTRYPOINT ["/usr/local/bin/sync.sh"]
