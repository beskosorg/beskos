#!/bin/bash
# Hands Caddy's certificate for the mail hostname to Stalwart.
#
# Stalwart obtains its own certificate over ACME, which needs port 80 or 443 -- and on a single
# host the reverse proxy has both. So Stalwart cannot renew, and a certificate that expires stops
# inbound mail outright wherever the MTA-STS policy is at `enforce`: the sender does not fall back
# to plaintext, it refuses to deliver. Caddy already holds a valid certificate for that same name,
# renewed on its own schedule. This copies it across whenever Caddy's is the newer one.
#
# Two things it deliberately does NOT do quietly (laws 2.8 and 2.9): it counts consecutive
# failures and shouts past a threshold instead of logging one line an hour that nobody reads, and
# it shouts when the certificate Stalwart is actually serving gets close to expiry -- because
# "nothing to copy yet" and "this has been broken for a month" otherwise look identical.

set -uo pipefail

: "${STALWART_URL:=http://stalwart:8080}"
: "${STALWART_ADMIN_ID:=admin}"
: "${STALWART_ADMIN_SECRET:?STALWART_ADMIN_SECRET is missing}"
: "${CERT_DOMAIN:?CERT_DOMAIN is missing}"
: "${CERT_DIR:=/caddy/certificates}"
: "${INTERVAL_SECONDS:=43200}"
: "${WARN_DAYS:=21}"
: "${FAIL_THRESHOLD:=3}"

log() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) mailcert: $*"; }

jmap() { # $1 = method, $2 = arguments as JSON
  curl -sS --max-time 30 -u "${STALWART_ADMIN_ID}:${STALWART_ADMIN_SECRET}" \
    -H "Content-Type: application/json" -X POST "${STALWART_URL}/jmap" \
    -d "{\"using\":[\"urn:ietf:params:jmap:core\"],\"methodCalls\":[[\"$1\",$2,\"c\"]]}"
}

epoch_of() { date -u -d "$(openssl x509 -enddate -noout -in "$1" | cut -d= -f2)" +%s 2>/dev/null; }

sync_once() {
  local crt key stored_id stored_after file_after left resp
  crt=$(find "$CERT_DIR" -type f -path "*/${CERT_DOMAIN}/${CERT_DOMAIN}.crt" 2>/dev/null | head -1)
  key="${crt%.crt}.key"
  [ -n "$crt" ] && [ -r "$crt" ] && [ -r "$key" ] || { log "no certificate for ${CERT_DOMAIN} under ${CERT_DIR} -- is the proxy serving that name?"; return 1; }

  resp=$(jmap "x:Certificate/get" "{}") || { log "cannot read certificates from Stalwart"; return 1; }
  stored_id=$(echo "$resp" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p' | head -1)
  stored_after=$(echo "$resp" | sed -n 's/.*"notValidAfter":"\([^"]*\)".*/\1/p' | head -1)
  [ -n "$stored_id" ] || { log "Stalwart holds no certificate object to update"; return 1; }

  file_after=$(epoch_of "$crt")
  stored_epoch=$(date -u -d "$stored_after" +%s 2>/dev/null || echo 0)
  left=$(( (stored_epoch - $(date -u +%s)) / 86400 ))

  if [ "${file_after:-0}" -gt "$((stored_epoch + 3600))" ]; then
    jq -n --rawfile crt "$crt" --rawfile key "$key" --arg id "$stored_id" \
      '{using:["urn:ietf:params:jmap:core"],methodCalls:[["x:Certificate/set",
        {update:{($id):{certificate:{"@type":"Text",value:$crt},privateKey:{"@type":"Text",secret:$key}}}},"c"]]}' \
      > /tmp/set.json || { log "cannot build the update"; return 1; }
    resp=$(curl -sS --max-time 30 -u "${STALWART_ADMIN_ID}:${STALWART_ADMIN_SECRET}" \
      -H "Content-Type: application/json" -X POST "${STALWART_URL}/jmap" -d @/tmp/set.json)
    rm -f /tmp/set.json
    case "$resp" in
      *'"updated"'*) log "installed a newer certificate for ${CERT_DOMAIN}; RESTART Stalwart to serve it" ;;
      *) log "Stalwart refused the update: $(echo "$resp" | head -c 300)"; return 1 ;;
    esac
    return 0
  fi

  # Nothing to copy. That is the normal case, and also what a broken proxy looks like, so say how
  # long the certificate Stalwart is serving has left rather than only that there was no work.
  if [ "$left" -lt "$WARN_DAYS" ]; then
    log "ATTENTION: the certificate Stalwart serves for ${CERT_DOMAIN} expires in ${left} days and the proxy has nothing newer. Inbound mail stops when it expires, because the MTA-STS policy is at enforce."
  else
    log "up to date (${left} days left on ${CERT_DOMAIN})"
  fi
  return 0
}

log "started -- ${CERT_DOMAIN}, every $((INTERVAL_SECONDS / 3600))h"
consecutive=0
while :; do
  if sync_once; then
    if [ "$consecutive" -ge "$FAIL_THRESHOLD" ]; then
      log "RECOVERED after ${consecutive} consecutive failures"
    fi
    consecutive=0
  else
    consecutive=$((consecutive + 1))
    if [ "$consecutive" -ge "$FAIL_THRESHOLD" ]; then
      log "ESCALATION: ${consecutive} consecutive failures. Nothing is renewing the mail certificate; when it expires inbound mail stops."
    fi
  fi
  sleep "$INTERVAL_SECONDS"
done
