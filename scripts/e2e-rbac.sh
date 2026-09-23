#!/usr/bin/env bash
# RBAC security E2E: roles, permissions, invites, last-owner, isolation between tenants.
set -euo pipefail
PG=rbac-pg; PGPORT=55437; APIPORT=8204
BACKEND="$(cd "$(dirname "$0")/../backend" && pwd)"
cleanup(){ [[ -n "${API_PID:-}" ]]&&kill $API_PID 2>/dev/null||true; docker rm -f $PG >/dev/null 2>&1||true; }
trap cleanup EXIT

docker rm -f $PG >/dev/null 2>&1||true
docker run -d --name $PG -e POSTGRES_USER=mailin -e POSTGRES_PASSWORD=r -e POSTGRES_DB=mailin -p $PGPORT:5432 postgres:16-alpine >/dev/null
for i in $(seq 1 30); do docker exec $PG pg_isready -U mailin >/dev/null 2>&1 && break; sleep 1; done

export DATABASE_URL="postgres://mailin:r@localhost:$PGPORT/mailin?sslmode=disable"
cd "$BACKEND"; HTTP_ADDR=":$APIPORT" DEV_SIGNUP_ENABLED=true go run ./cmd/api >/tmp/rbac-api.log 2>&1 & API_PID=$!
for i in $(seq 1 20); do curl -fsS localhost:$APIPORT/healthz >/dev/null 2>&1 && break; sleep 1; done
API="localhost:$APIPORT/api"; PW=correct-horse-battery-9273
J(){ python3 -c "import sys,json;print(json.load(sys.stdin)$1)"; }
code(){ curl -s -o /dev/null -w "%{http_code}" "$@"; }
pass(){ echo "   ✓ $1"; }
fail(){ echo "   ✗ $1"; exit 1; }

echo "== owner (signup) =="
curl -s -c /tmp/owner -X POST $API/auth/signup -d '{"org_name":"Org A","email":"owner@a.com","password":"'$PW'"}' >/dev/null
ROLE=$(curl -s -b /tmp/owner $API/me | J "['role']"); [[ $ROLE == owner ]]&&pass "signup becomes owner"||fail "role=$ROLE"
NPERM=$(curl -s -b /tmp/owner $API/me | J "['permissions'].__len__()"); [[ $NPERM -ge 8 ]]&&pass "owner has all permissions ($NPERM)"||fail "perms=$NPERM"

echo "== invite + accept =="
TOKEN=$(curl -s -b /tmp/owner -X POST $API/invites -d '{"email":"member@a.com","role":"member"}' | J "['token']")
[[ -n $TOKEN ]]&&pass "owner creates invite"||fail "no token"
EMAIL=$(curl -s $API/invites/$TOKEN | J "['email']"); [[ $EMAIL == member@a.com ]]&&pass "public invite resolvable"||fail "get invite"
curl -s -c /tmp/member -X POST $API/auth/accept 2>/dev/null >/dev/null || true
curl -s -c /tmp/member -X POST $API/invites/accept -d '{"token":"'$TOKEN'","name":"Member","password":"'$PW'"}' >/dev/null
MROLE=$(curl -s -b /tmp/member $API/me | J "['role']"); [[ $MROLE == member ]]&&pass "accept logs member in"||fail "role=$MROLE"

echo "== permissions (member is read-only) =="
[[ $(code -b /tmp/member -X POST $API/domains -d '{"dominio":"x.com"}') == 403 ]]&&pass "member does NOT create a domain (403)"||fail "member created a domain!"
[[ $(code -b /tmp/member $API/domains) == 200 ]]&&pass "member sees domains (200)"||fail "member cannot see domains"
[[ $(code -b /tmp/member $API/members) == 403 ]]&&pass "member does NOT list members (403)"||fail "member listed members!"
[[ $(code -b /tmp/member -X POST $API/invites -d '{"email":"x@a.com","role":"member"}') == 403 ]]&&pass "member does NOT invite (403)"||fail "member invited!"

echo "== privilege escalation =="
MID=$(curl -s -b /tmp/owner $API/members | python3 -c "import sys,json;print([m['id'] for m in json.load(sys.stdin) if m['role']=='member'][0])")
curl -s -b /tmp/owner -X PATCH $API/members/$MID/role -d '{"role":"admin"}' >/dev/null
[[ $(curl -s -b /tmp/member $API/me | J "['role']") == admin ]]&&pass "owner promotes member->admin"||fail "promotion failed"
[[ $(code -b /tmp/member -X POST $API/invites -d '{"email":"y@a.com","role":"owner"}') == 403 ]]&&pass "admin does NOT invite as owner (anti-escalation)"||fail "admin created an owner!"
[[ $(code -b /tmp/member -X POST $API/invites -d '{"email":"y@a.com","role":"member"}') == 201 ]]&&pass "admin invites member (201)"||fail "admin failed to invite"
# internal_admin is not assignable
[[ $(code -b /tmp/owner -X POST $API/invites -d '{"email":"z@a.com","role":"internal_admin"}') == 400 ]]&&pass "internal_admin is not assignable (400)"||fail "internal_admin assigned!"

echo "== last owner protected =="
OID=$(curl -s -b /tmp/owner $API/members | python3 -c "import sys,json;print([m['id'] for m in json.load(sys.stdin) if m['role']=='owner'][0])")
[[ $(code -b /tmp/owner -X PATCH $API/members/$OID/role -d '{"role":"admin"}') == 409 ]]&&pass "does not demote the sole owner (409)"||fail "demoted the last owner!"
[[ $(code -b /tmp/owner -X DELETE $API/members/$OID) == 409 ]]&&pass "does not remove the sole owner (409)"||fail "removed the last owner!"

echo "== isolation between tenants =="
curl -s -c /tmp/owner2 -X POST $API/auth/signup -d '{"org_name":"Org B","email":"owner@b.com","password":"'$PW'"}' >/dev/null
# owner A tries to remove a member of A that does not exist in B: use MID (from A) from B's session
[[ $(code -b /tmp/owner2 -X DELETE $API/members/$MID) == 404 ]]&&pass "tenant B cannot see tenant A's member (404)"||fail "leaked across tenants!"
[[ $(code -b /tmp/owner2 -X PATCH $API/members/$MID/role -d '{"role":"member"}') == 404 ]]&&pass "tenant B cannot change tenant A's member (404)"||fail "changed across tenants!"

echo; echo "✅ E2E RBAC PASSED -- roles, permissions, invites, anti-escalation, last-owner, isolation."
