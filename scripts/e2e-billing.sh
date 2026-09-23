#!/usr/bin/env bash
# Billing + quota E2E: mailbox quota (402), signed + idempotent Stripe webhook,
# plan change alters the limit, storage usage accounted, invalid signature.
set -euo pipefail
PG=bl-pg; SW=bl-sw; MO=bl-minio
PGPORT=55440; SWPORT=18095; MOPORT=19001; APIPORT=8207
SECRET=probe-secret; WHSEC=whsec_test
BACKEND="$(cd "$(dirname "$0")/../backend" && pwd)"
cleanup(){ [[ -n "${API_PID:-}" ]]&&kill $API_PID 2>/dev/null||true; [[ -n "${WK_PID:-}" ]]&&kill $WK_PID 2>/dev/null||true;
  docker rm -f $PG $SW $MO >/dev/null 2>&1||true; }
trap cleanup EXIT

docker rm -f $PG $SW $MO >/dev/null 2>&1||true
docker run -d --name $PG -e POSTGRES_USER=mailin -e POSTGRES_PASSWORD=b -e POSTGRES_DB=mailin -p $PGPORT:5432 postgres:16-alpine >/dev/null
docker run -d --name $SW -e STALWART_RECOVERY_ADMIN="admin:$SECRET" -p $SWPORT:8080 stalwartlabs/stalwart:v0.16.4 >/dev/null
docker run -d --name $MO -e MINIO_ROOT_USER=mailin -e MINIO_ROOT_PASSWORD=mailin123 -p $MOPORT:9000 quay.io/minio/minio server /data >/dev/null
for i in $(seq 1 30); do docker exec $PG pg_isready -U mailin >/dev/null 2>&1 && break; sleep 1; done
for i in $(seq 1 30); do curl -fsS http://localhost:$SWPORT/jmap/session -u admin:$SECRET >/dev/null 2>&1 && break; sleep 1; done
for i in $(seq 1 30); do curl -fsS http://localhost:$MOPORT/minio/health/live >/dev/null 2>&1 && break; sleep 1; done

export DATABASE_URL="postgres://mailin:b@localhost:$PGPORT/mailin?sslmode=disable"
export STALWART_URL="http://localhost:$SWPORT" STALWART_ADMIN_ID=admin STALWART_ADMIN_SECRET=$SECRET
export STALWART_HOSTNAME=mail.bl.com STALWART_DEFAULT_DOMAIN=root-bl.com
export MINIO_ENDPOINT="localhost:$MOPORT" MINIO_ACCESS_KEY=mailin MINIO_SECRET_KEY=mailin123 MINIO_BUCKET=mailin
export BILLING_PROVIDER=stripe STRIPE_WEBHOOK_SECRET=$WHSEC
cd "$BACKEND"
go run ./cmd/bootstrap >/dev/null 2>&1; docker restart $SW >/dev/null
for i in $(seq 1 30); do curl -fsS http://localhost:$SWPORT/jmap/session -u admin:$SECRET >/dev/null 2>&1 && break; sleep 1; done
HTTP_ADDR=":$APIPORT" DEV_SIGNUP_ENABLED=true go run ./cmd/api >/tmp/bl-api.log 2>&1 & API_PID=$!
PROVISION_SKIP_DNS_VERIFY=true go run ./cmd/worker >/tmp/bl-wk.log 2>&1 & WK_PID=$!
for i in $(seq 1 20); do curl -fsS localhost:$APIPORT/healthz >/dev/null 2>&1 && break; sleep 1; done

API="localhost:$APIPORT/api"; PW=correct-horse-battery-9273
J(){ python3 -c "import sys,json;print(json.load(sys.stdin)$1)"; }
code(){ curl -s -o /dev/null -w "%{http_code}" "$@"; }
pass(){ echo "   ✓ $1"; }; fail(){ echo "   ✗ $1"; exit 1; }

echo "== plano inicial (trial) =="
T=$(curl -s -c /tmp/bl-o -X POST $API/auth/signup -d '{"org_name":"Org","email":"o@b.com","password":"'$PW'"}' | J "['tenant_id']")
[[ $(curl -s -b /tmp/bl-o $API/usage | J "['plan']['id']") == trial ]]&&pass "tenant começa no trial"||fail "plano inicial"
[[ $(curl -s -b /tmp/bl-o $API/billing | J "['plans'].__len__()") == 4 ]]&&pass "catálogo com 4 planos"||fail "catálogo"

echo "== cota de caixas (trial = 3) =="
DID=$(curl -s -b /tmp/bl-o -X POST $API/domains -d '{"dominio":"bl-demo.com"}' | J "['id']")
for i in $(seq 1 30); do ST=$(curl -s -b /tmp/bl-o $API/domains/$DID | J "['provision_state']"); [[ $ST == active ]]&&break; sleep 2; done
# postmaster (auto) = 1; create ana(2), bob(3)
curl -s -b /tmp/bl-o -X POST $API/domains/$DID/mailboxes -d '{"login":"ana","name":"Ana","password":"'$PW'"}' >/dev/null
curl -s -b /tmp/bl-o -X POST $API/domains/$DID/mailboxes -d '{"login":"bob","name":"Bob","password":"'$PW'"}' >/dev/null
[[ $(curl -s -b /tmp/bl-o $API/usage | J "['usage']['mailboxes']") == 3 ]]&&pass "3 caixas (postmaster+ana+bob)"||fail "contagem caixas"
[[ $(code -b /tmp/bl-o -X POST $API/domains/$DID/mailboxes -d '{"login":"carol","name":"Carol","password":"'$PW'"}') == 402 ]]&&pass "4ª caixa barrada pela cota (402)"||fail "cota não barrou!"

echo "== webhook Stripe assinado (upgrade p/ business) + idempotência =="
PAYLOAD=$(printf '{"id":"evt_1","type":"checkout.session.completed","data":{"object":{"client_reference_id":"%s","subscription":"sub_1","metadata":{"plan":"business","tenant_id":"%s"}}}}' "$T" "$T")
SIG=$(python3 -c "
import hmac,hashlib,time,sys
ts=int(time.time()); p=sys.argv[1]; s=sys.argv[2]
m=hmac.new(s.encode(),f'{ts}.{p}'.encode(),hashlib.sha256).hexdigest()
print(f't={ts},v1={m}')" "$PAYLOAD" "$WHSEC")
R1=$(curl -s -X POST $API/billing/webhook -H "Stripe-Signature: $SIG" -d "$PAYLOAD" | J "['status']")
[[ $R1 == applied ]]&&pass "webhook aplicado"||fail "webhook não aplicou ($R1)"
[[ $(curl -s -b /tmp/bl-o $API/usage | J "['plan']['id']") == business ]]&&pass "plano virou business"||fail "plano não mudou"
R2=$(curl -s -X POST $API/billing/webhook -H "Stripe-Signature: $SIG" -d "$PAYLOAD" | J "['status']")
[[ $R2 == duplicate ]]&&pass "webhook repetido é idempotente (duplicate)"||fail "reprocessou! ($R2)"
[[ $(code -X POST $API/billing/webhook -H "Stripe-Signature: t=1,v1=deadbeef" -d "$PAYLOAD") == 400 ]]&&pass "assinatura inválida rejeitada (400)"||fail "aceitou assinatura falsa!"

echo "== nova cota permite mais caixas =="
[[ $(code -b /tmp/bl-o -X POST $API/domains/$DID/mailboxes -d '{"login":"carol","name":"Carol","password":"'$PW'"}') == 201 ]]&&pass "business permite a 4ª caixa (201)"||fail "ainda barrado"

echo "== uso de armazenamento contabilizado =="
curl -s -c /tmp/bl-wm -X POST $API/webmail/login -d '{"email":"ana@bl-demo.com","password":"'$PW'"}' >/dev/null
head -c 50000 /dev/urandom > /tmp/bl.bin
curl -s -b /tmp/bl-wm -F "file=@/tmp/bl.bin;filename=a.bin" $API/files >/dev/null
[[ $(curl -s -b /tmp/bl-o $API/usage | J "['usage']['storage_bytes']") == 50000 ]]&&pass "uso de disco reflete o upload (50000 bytes)"||fail "uso não contabilizou"; rm -f /tmp/bl.bin

echo; echo "✅ E2E BILLING+COTAS PASSOU — cota de caixas, webhook assinado idempotente, mudança de plano, uso de disco."
