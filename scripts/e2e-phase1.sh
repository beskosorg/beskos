#!/usr/bin/env bash
# E2E: creates a customer end to end (domain → provisioning → mailbox).
# Brings up real Postgres + Stalwart, bootstraps, runs API + worker, and exercises the API.
set -euo pipefail

PG=mailin-e2e-pg
SW=mailin-e2e-sw
PGPORT=55434
SWPORT=18090
APIPORT=8202
ADMIN_SECRET=probe-secret
BACKEND="$(cd "$(dirname "$0")/../backend" && pwd)"

cleanup() {
  [[ -n "${API_PID:-}" ]] && kill "$API_PID" 2>/dev/null || true
  [[ -n "${WORKER_PID:-}" ]] && kill "$WORKER_PID" 2>/dev/null || true
  docker rm -f "$PG" "$SW" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "== subindo Postgres e Stalwart =="
docker rm -f "$PG" "$SW" >/dev/null 2>&1 || true
docker run -d --name "$PG" -e POSTGRES_USER=mailin -e POSTGRES_PASSWORD=e2e -e POSTGRES_DB=mailin -p $PGPORT:5432 postgres:16-alpine >/dev/null
docker run -d --name "$SW" -e STALWART_RECOVERY_ADMIN="admin:$ADMIN_SECRET" -p $SWPORT:8080 stalwartlabs/stalwart:v0.16.4 >/dev/null

for i in $(seq 1 30); do docker exec "$PG" pg_isready -U mailin >/dev/null 2>&1 && break; sleep 1; done
for i in $(seq 1 30); do curl -fsS "http://localhost:$SWPORT/jmap/session" -u "admin:$ADMIN_SECRET" >/dev/null 2>&1 && break; sleep 1; done
echo "   ok"

export DATABASE_URL="postgres://mailin:e2e@localhost:$PGPORT/mailin?sslmode=disable"
export STALWART_URL="http://localhost:$SWPORT" STALWART_ADMIN_ID=admin STALWART_ADMIN_SECRET=$ADMIN_SECRET
export STALWART_HOSTNAME=mail.e2e.test STALWART_DEFAULT_DOMAIN=e2e-root.test

echo "== bootstrap do Stalwart + restart =="
cd "$BACKEND"
go run ./cmd/bootstrap
docker restart "$SW" >/dev/null
for i in $(seq 1 30); do curl -fsS "http://localhost:$SWPORT/jmap/session" -u "admin:$ADMIN_SECRET" >/dev/null 2>&1 && break; sleep 1; done

echo "== subindo API + worker =="
HTTP_ADDR=":$APIPORT" DEV_SIGNUP_ENABLED=true go run ./cmd/api > /tmp/e2e-api.log 2>&1 &
API_PID=$!
PROVISION_SKIP_DNS_VERIFY=true go run ./cmd/worker > /tmp/e2e-worker.log 2>&1 &
WORKER_PID=$!
for i in $(seq 1 20); do curl -fsS "http://localhost:$APIPORT/healthz" >/dev/null 2>&1 && break; sleep 1; done

echo "== signup =="
curl -s -c /tmp/e2e-cj -X POST "localhost:$APIPORT/api/auth/signup" \
  -d '{"org_name":"E2E Corp","email":"admin@e2e.test","password":"correct-horse-battery-9273"}' >/dev/null

echo "== criar domínio =="
DID=$(curl -s -b /tmp/e2e-cj -X POST "localhost:$APIPORT/api/domains" -d '{"dominio":"cliente-e2e.test"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['id'])")
echo "   domínio id=$DID"

echo "== aguardando provisionamento chegar em 'active' =="
STATE=""
for i in $(seq 1 30); do
  STATE=$(curl -s -b /tmp/e2e-cj "localhost:$APIPORT/api/domains/$DID" | python3 -c "import sys,json;print(json.load(sys.stdin)['provision_state'])")
  echo "   [$i] estado=$STATE"
  [[ "$STATE" == "active" ]] && break
  sleep 2
done
[[ "$STATE" == "active" ]] || { echo "FALHOU: não chegou a active"; echo "--worker--"; tail -20 /tmp/e2e-worker.log; exit 1; }

echo "== registros DNS expostos ao cliente =="
curl -s -b /tmp/e2e-cj "localhost:$APIPORT/api/domains/$DID" | python3 -c "
import sys,json; d=json.load(sys.stdin)
recs=d['dns_records']; print(f'   {len(recs)} registros; tipos:', sorted(set(r['type'] for r in recs)))
assert any(r['type']=='MX' for r in recs), 'faltou MX'"

echo "== criar mailbox no domínio ativo =="
curl -s -b /tmp/e2e-cj -X POST "localhost:$APIPORT/api/domains/$DID/mailboxes" \
  -d '{"login":"joao","name":"João","password":"correct-horse-battery-9273","quota_gb":5}' \
  | python3 -c "import sys,json;d=json.load(sys.stdin);print('   mailbox criada:', d['email']); assert d['email']=='joao@cliente-e2e.test'"

echo
echo "✅ E2E Fase 1 PASSOU: cliente criado de ponta a ponta (domínio provisionado + mailbox)."
