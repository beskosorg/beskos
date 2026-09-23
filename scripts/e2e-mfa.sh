#!/usr/bin/env bash
# MFA security E2E: TOTP + SMS enrollment, 2-step login, rate limit, single-use recovery.
set -euo pipefail
PG=mfa-pg; PGPORT=55438; APIPORT=8205
BACKEND="$(cd "$(dirname "$0")/../backend" && pwd)"
cleanup(){ [[ -n "${API_PID:-}" ]]&&kill $API_PID 2>/dev/null||true; docker rm -f $PG >/dev/null 2>&1||true; }
trap cleanup EXIT

docker rm -f $PG >/dev/null 2>&1||true
docker run -d --name $PG -e POSTGRES_USER=mailin -e POSTGRES_PASSWORD=m -e POSTGRES_DB=mailin -p $PGPORT:5432 postgres:16-alpine >/dev/null
for i in $(seq 1 30); do docker exec $PG pg_isready -U mailin >/dev/null 2>&1 && break; sleep 1; done
export DATABASE_URL="postgres://mailin:m@localhost:$PGPORT/mailin?sslmode=disable" SMS_PROVIDER=log
cd "$BACKEND"; HTTP_ADDR=":$APIPORT" DEV_SIGNUP_ENABLED=true go run ./cmd/api >/tmp/mfa-api.log 2>&1 & API_PID=$!
for i in $(seq 1 20); do curl -fsS localhost:$APIPORT/healthz >/dev/null 2>&1 && break; sleep 1; done
API="localhost:$APIPORT/api"; PW=correct-horse-battery-9273
J(){ python3 -c "import sys,json;print(json.load(sys.stdin)$1)"; }
code(){ curl -s -o /dev/null -w "%{http_code}" "$@"; }
pass(){ echo "   ✓ $1"; }; fail(){ echo "   ✗ $1"; exit 1; }
TOTP(){ python3 -c "
import hmac,hashlib,base64,struct,time,sys
s=sys.argv[1]; key=base64.b32decode(s+'='*((8-len(s)%8)%8))
c=int(time.time())//30; h=hmac.new(key,struct.pack('>Q',c),hashlib.sha1).digest()
o=h[-1]&0x0f; n=(struct.unpack('>I',h[o:o+4])[0]&0x7fffffff)%1000000; print('%06d'%n)" "$1"; }
lastsms(){ grep -o 'Beskos: [0-9]\{6\}' /tmp/mfa-api.log | tail -1 | grep -o '[0-9]\{6\}'; }

echo "== signup + estado MFA =="
curl -s -c /tmp/o -X POST $API/auth/signup -d '{"org_name":"Org","email":"o@x.com","password":"'$PW'"}' >/dev/null
[[ $(curl -s -b /tmp/o $API/me | J "['mfa']['totp']") == False ]]&&pass "MFA começa desligado"||fail "mfa on?"

echo "== enrollment TOTP =="
SECRET=$(curl -s -b /tmp/o -X POST $API/mfa/totp/start | J "['secret']")
[[ -n $SECRET ]]&&pass "totp/start devolve segredo"||fail "sem segredo"
[[ $(code -b /tmp/o -X POST $API/mfa/totp/enable -d '{"code":"000000"}') == 400 ]]&&pass "código errado não ativa (400)"||fail "ativou com código errado!"
RESP=$(curl -s -b /tmp/o -X POST $API/mfa/totp/enable -d "{\"code\":\"$(TOTP $SECRET)\"}")
NREC=$(echo "$RESP" | J "['recovery_codes'].__len__()"); [[ $NREC == 10 ]]&&pass "TOTP ativado + 10 códigos de recuperação"||fail "recovery=$NREC"
REC=$(echo "$RESP" | J "['recovery_codes'][0]")
[[ $(curl -s -b /tmp/o $API/me | J "['mfa']['totp']") == True ]]&&pass "/me reflete TOTP ativo"||fail "me totp off"

echo "== login em 2 passos (TOTP) =="
LOGIN=$(curl -s -c /tmp/l -X POST $API/auth/login -d '{"email":"o@x.com","password":"'$PW'"}')
[[ $(echo "$LOGIN" | J "['mfa_required']") == True ]]&&pass "login exige 2º fator"||fail "login sem MFA!"
[[ $(code -b /tmp/l $API/me) == 401 ]]&&pass "sem 2º fator não há sessão (401)"||fail "sessão sem MFA!"
CH=$(echo "$LOGIN" | J "['challenge']")
[[ $(code -c /tmp/s -X POST $API/auth/mfa/verify -d "{\"challenge\":\"$CH\",\"code\":\"000000\"}") == 401 ]]&&pass "código 2FA errado (401)"||fail "aceitou errado!"
curl -s -c /tmp/s -X POST $API/auth/mfa/verify -d "{\"challenge\":\"$CH\",\"code\":\"$(TOTP $SECRET)\"}" >/dev/null
[[ $(code -b /tmp/s $API/me) == 200 ]]&&pass "verify com TOTP emite sessão"||fail "sem sessão após verify"

echo "== rate limit (brute force) =="
CH=$(curl -s -X POST $API/auth/login -d '{"email":"o@x.com","password":"'$PW'"}' | J "['challenge']")
for i in $(seq 1 5); do curl -s -X POST $API/auth/mfa/verify -d "{\"challenge\":\"$CH\",\"code\":\"111111\"}" >/dev/null; done
[[ $(code -X POST $API/auth/mfa/verify -d "{\"challenge\":\"$CH\",\"code\":\"111111\"}") == 429 ]]&&pass "6ª tentativa bloqueada (429)"||fail "sem rate limit!"

echo "== código de recuperação (uso único) =="
CH=$(curl -s -X POST $API/auth/login -d '{"email":"o@x.com","password":"'$PW'"}' | J "['challenge']")
[[ $(code -X POST $API/auth/mfa/verify -d "{\"challenge\":\"$CH\",\"code\":\"$REC\"}") == 200 ]]&&pass "login com código de recuperação"||fail "recovery falhou"
CH=$(curl -s -X POST $API/auth/login -d '{"email":"o@x.com","password":"'$PW'"}' | J "['challenge']")
[[ $(code -X POST $API/auth/mfa/verify -d "{\"challenge\":\"$CH\",\"code\":\"$REC\"}") == 401 ]]&&pass "recovery reusado é rejeitado (uso único)"||fail "recovery reusável!"

echo "== enrollment + login SMS =="
CH=$(curl -s -b /tmp/o -X POST $API/mfa/sms/start -d '{"phone":"+5511999999999"}' | J "['challenge']")
SMSCODE=$(lastsms); [[ -n $SMSCODE ]]&&pass "SMS de verificação enviado (log)"||fail "sem SMS"
curl -s -b /tmp/o -X POST $API/mfa/sms/enable -d "{\"challenge\":\"$CH\",\"code\":\"$SMSCODE\"}" >/dev/null
[[ $(curl -s -b /tmp/o $API/me | J "['mfa']['sms']") == True ]]&&pass "SMS ativado"||fail "sms off"
LOGIN=$(curl -s -X POST $API/auth/login -d '{"email":"o@x.com","password":"'$PW'"}')
CH=$(echo "$LOGIN" | J "['challenge']"); SMSCODE=$(lastsms)
[[ $(code -X POST $API/auth/mfa/verify -d "{\"challenge\":\"$CH\",\"code\":\"$SMSCODE\"}") == 200 ]]&&pass "login com código SMS"||fail "login SMS falhou"

echo; echo "✅ E2E MFA PASSOU — TOTP, SMS, recovery único, login 2 passos, rate-limit."
