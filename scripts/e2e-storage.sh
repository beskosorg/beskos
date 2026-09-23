#!/usr/bin/env bash
# Storage E2E (real MinIO): upload, dedup by hash, versioning, download, share, delete.
set -euo pipefail
PG=st-pg; SW=st-sw; MO=st-minio
PGPORT=55439; SWPORT=18094; MOPORT=19000; APIPORT=8206
SECRET=probe-secret
BACKEND="$(cd "$(dirname "$0")/../backend" && pwd)"
cleanup(){ [[ -n "${API_PID:-}" ]]&&kill $API_PID 2>/dev/null||true; [[ -n "${WK_PID:-}" ]]&&kill $WK_PID 2>/dev/null||true;
  docker rm -f $PG $SW $MO >/dev/null 2>&1||true; rm -f /tmp/st-a.bin /tmp/st-dl.bin; }
trap cleanup EXIT

echo "== infra (Postgres + Stalwart + MinIO) =="
docker rm -f $PG $SW $MO >/dev/null 2>&1||true
docker run -d --name $PG -e POSTGRES_USER=mailin -e POSTGRES_PASSWORD=s -e POSTGRES_DB=mailin -p $PGPORT:5432 postgres:16-alpine >/dev/null
docker run -d --name $SW -e STALWART_RECOVERY_ADMIN="admin:$SECRET" -p $SWPORT:8080 stalwartlabs/stalwart:v0.16.4 >/dev/null
docker run -d --name $MO -e MINIO_ROOT_USER=mailin -e MINIO_ROOT_PASSWORD=mailin123 -p $MOPORT:9000 quay.io/minio/minio server /data >/dev/null
for i in $(seq 1 30); do docker exec $PG pg_isready -U mailin >/dev/null 2>&1 && break; sleep 1; done
for i in $(seq 1 30); do curl -fsS http://localhost:$SWPORT/jmap/session -u admin:$SECRET >/dev/null 2>&1 && break; sleep 1; done
for i in $(seq 1 30); do curl -fsS http://localhost:$MOPORT/minio/health/live >/dev/null 2>&1 && break; sleep 1; done

export DATABASE_URL="postgres://mailin:s@localhost:$PGPORT/mailin?sslmode=disable"
export STALWART_URL="http://localhost:$SWPORT" STALWART_ADMIN_ID=admin STALWART_ADMIN_SECRET=$SECRET
export STALWART_HOSTNAME=mail.st-demo.com STALWART_DEFAULT_DOMAIN=root-st.com
export MINIO_ENDPOINT="localhost:$MOPORT" MINIO_ACCESS_KEY=mailin MINIO_SECRET_KEY=mailin123 MINIO_BUCKET=mailin
cd "$BACKEND"
go run ./cmd/bootstrap >/dev/null 2>&1; docker restart $SW >/dev/null
for i in $(seq 1 30); do curl -fsS http://localhost:$SWPORT/jmap/session -u admin:$SECRET >/dev/null 2>&1 && break; sleep 1; done
HTTP_ADDR=":$APIPORT" DEV_SIGNUP_ENABLED=true go run ./cmd/api >/tmp/st-api.log 2>&1 & API_PID=$!
PROVISION_SKIP_DNS_VERIFY=true go run ./cmd/worker >/tmp/st-wk.log 2>&1 & WK_PID=$!
for i in $(seq 1 20); do curl -fsS localhost:$APIPORT/healthz >/dev/null 2>&1 && break; sleep 1; done
grep -q "storage enabled" /tmp/st-api.log && echo "   storage habilitado ✓"

API="localhost:$APIPORT/api"; PW=correct-horse-battery-9273
J(){ python3 -c "import sys,json;print(json.load(sys.stdin)$1)"; }
code(){ curl -s -o /dev/null -w "%{http_code}" "$@"; }
pass(){ echo "   ✓ $1"; }; fail(){ echo "   ✗ $1"; exit 1; }

echo "== provisionar mailbox ana =="
curl -s -c /tmp/st-panel -X POST $API/auth/signup -d '{"org_name":"St","email":"admin@st.com","password":"'$PW'"}' >/dev/null
DID=$(curl -s -b /tmp/st-panel -X POST $API/domains -d '{"dominio":"st-demo.com"}' | J "['id']")
for i in $(seq 1 30); do ST=$(curl -s -b /tmp/st-panel $API/domains/$DID | J "['provision_state']"); [[ $ST == active ]]&&break; sleep 2; done
curl -s -b /tmp/st-panel -X POST $API/domains/$DID/mailboxes -d '{"login":"ana","name":"Ana","password":"'$PW'"}' >/dev/null
curl -s -c /tmp/st-wm -X POST $API/webmail/login -d '{"email":"ana@st-demo.com","password":"'$PW'"}' >/dev/null
WM="curl -s -b /tmp/st-wm"

echo "== upload + dedup + versionamento =="
head -c 100000 /dev/urandom > /tmp/st-a.bin
UP1=$($WM -F "file=@/tmp/st-a.bin;filename=relatorio.pdf" $API/files)
KEY1=$(echo "$UP1" | J "['blob_key']"); V1=$(echo "$UP1" | J "['versao']")
[[ -n $KEY1 && $V1 == 1 ]]&&pass "upload 1 (versao 1, blob=$KEY1)"||fail "upload1 $UP1"
UP2=$($WM -F "file=@/tmp/st-a.bin;filename=relatorio.pdf" $API/files)
KEY2=$(echo "$UP2" | J "['blob_key']"); V2=$(echo "$UP2" | J "['versao']")
[[ $KEY2 == $KEY1 ]]&&pass "dedup: conteúdo idêntico reusa o mesmo blob"||fail "dedup falhou ($KEY2)"
[[ $V2 == 2 ]]&&pass "versionamento: mesmo nome vira versao 2"||fail "versao=$V2"
FID=$(echo "$UP2" | J "['id']")

echo "== list + download (integridade) =="
[[ $($WM $API/files | J ".__len__()") == 2 ]]&&pass "lista 2 arquivos"||fail "list"
$WM $API/files/$FID/download -o /tmp/st-dl.bin
[[ $(shasum -a256 /tmp/st-a.bin|cut -d' ' -f1) == $(shasum -a256 /tmp/st-dl.bin|cut -d' ' -f1) ]]&&pass "download bate byte-a-byte"||fail "download corrompido"

echo "== compartilhamento =="
$WM -X POST $API/files/$FID/share -d '{"principal":"colega@st-demo.com","nivel":"leitor"}' >/dev/null
[[ $($WM $API/files/$FID/shares | J ".__len__()") == 1 ]]&&pass "compartilhamento criado"||fail "share"
[[ $(code -b /tmp/st-wm -X POST $API/files/$FID/share -d '{"principal":"x","nivel":"invalido"}') == 400 ]]&&pass "nível inválido rejeitado (400)"||fail "aceitou nível inválido"

echo "== delete + limpeza de blob por refcount =="
$WM -X DELETE $API/files/$FID >/dev/null
[[ $(code -b /tmp/st-wm $API/files/$FID/download) == 404 ]]&&pass "arquivo removido (404)"||fail "não removeu"
# blob still exists (version 1 points to it) → downloading v1 works
FID1=$($WM $API/files | J "[0]['id']")
[[ $(code -b /tmp/st-wm $API/files/$FID1/download) == 200 ]]&&pass "blob preservado enquanto outra versão referencia"||fail "blob removido cedo demais"

echo "== isolamento: sem sessão de webmail =="
[[ $(code $API/files) == 401 ]]&&pass "sem sessão não acessa arquivos (401)"||fail "vazou sem sessão"

echo; echo "✅ E2E STORAGE PASSOU — upload, dedup, versionamento, download íntegro, share, refcount, auth."
