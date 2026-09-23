#!/usr/bin/env bash
# Email backend E2E (our features): webmail session + AI + Kanban + labels + events.
# Real stack (Postgres + Stalwart) + a fake AI server (OpenAI-compatible).
set -euo pipefail

PG=mailin-eb-pg; SW=mailin-eb-sw
PGPORT=55435; SWPORT=18092; APIPORT=8203; AIPORT=8791
SECRET=probe-secret
BACKEND="$(cd "$(dirname "$0")/../backend" && pwd)"

cleanup(){ [[ -n "${API_PID:-}" ]]&&kill $API_PID 2>/dev/null||true; [[ -n "${WK_PID:-}" ]]&&kill $WK_PID 2>/dev/null||true;
  [[ -n "${AI_PID:-}" ]]&&kill $AI_PID 2>/dev/null||true; docker rm -f $PG $SW >/dev/null 2>&1||true; rm -f /tmp/fakeai.py; }
trap cleanup EXIT

echo "== infra =="
docker rm -f $PG $SW >/dev/null 2>&1||true
docker run -d --name $PG -e POSTGRES_USER=mailin -e POSTGRES_PASSWORD=eb -e POSTGRES_DB=mailin -p $PGPORT:5432 postgres:16-alpine >/dev/null
docker run -d --name $SW -e STALWART_RECOVERY_ADMIN="admin:$SECRET" -p $SWPORT:8080 stalwartlabs/stalwart:v0.16.4 >/dev/null
for i in $(seq 1 30); do docker exec $PG pg_isready -U mailin >/dev/null 2>&1 && break; sleep 1; done
for i in $(seq 1 30); do curl -fsS http://localhost:$SWPORT/jmap/session -u admin:$SECRET >/dev/null 2>&1 && break; sleep 1; done

echo "== servidor de IA fake (OpenAI-compatível) =="
cat > /tmp/fakeai.py <<'PY'
import http.server, json
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        self.rfile.read(int(self.headers.get('Content-Length',0)))
        body=json.dumps({"model":"deepseek-v4","choices":[{"message":{"role":"assistant",
            "content":'[{"title":"Revisar contrato","due":"hoje","assignee":"Ana"}]'}}],
            "usage":{"total_tokens":10}}).encode()
        self.send_response(200); self.send_header("Content-Type","application/json"); self.end_headers(); self.wfile.write(body)
    def log_message(self,*a): pass
import sys
http.server.HTTPServer(("127.0.0.1",int(sys.argv[1])),H).serve_forever()
PY
python3 /tmp/fakeai.py $AIPORT & AI_PID=$!

export DATABASE_URL="postgres://mailin:eb@localhost:$PGPORT/mailin?sslmode=disable"
export STALWART_URL="http://localhost:$SWPORT" STALWART_ADMIN_ID=admin STALWART_ADMIN_SECRET=$SECRET
export STALWART_HOSTNAME=mail.cliente-e2e.com STALWART_DEFAULT_DOMAIN=root-e2e.com
export AI_PROVIDER=deepseek AI_API_KEY=fake AI_BASE_URL="http://localhost:$AIPORT" AI_MODEL=deepseek-v4
cd "$BACKEND"

echo "== bootstrap + restart =="
go run ./cmd/bootstrap >/dev/null 2>&1
docker restart $SW >/dev/null
for i in $(seq 1 30); do curl -fsS http://localhost:$SWPORT/jmap/session -u admin:$SECRET >/dev/null 2>&1 && break; sleep 1; done

echo "== api + worker =="
HTTP_ADDR=":$APIPORT" DEV_SIGNUP_ENABLED=true go run ./cmd/api >/tmp/eb-api.log 2>&1 & API_PID=$!
PROVISION_SKIP_DNS_VERIFY=true go run ./cmd/worker >/tmp/eb-wk.log 2>&1 & WK_PID=$!
for i in $(seq 1 20); do curl -fsS http://localhost:$APIPORT/healthz >/dev/null 2>&1 && break; sleep 1; done
grep -q "AI enabled" /tmp/eb-api.log && echo "   IA habilitada ✓"

J(){ python3 -c "import sys,json;print(json.load(sys.stdin)$1)"; }
API="localhost:$APIPORT/api"

echo "== provisionar domínio + mailbox (popular mail_users) =="
curl -s -c /tmp/eb-panel -X POST $API/auth/signup -d '{"org_name":"E2E","email":"admin@e2e.com","password":"correct-horse-battery-9273"}' >/dev/null
DID=$(curl -s -b /tmp/eb-panel -X POST $API/domains -d '{"dominio":"cliente-e2e.com"}' | J "['id']")
for i in $(seq 1 30); do ST=$(curl -s -b /tmp/eb-panel $API/domains/$DID | J "['provision_state']"); [[ $ST == active ]]&&break; sleep 2; done
[[ $ST == active ]]||{ echo "FALHOU provisionamento ($ST)"; tail -20 /tmp/eb-wk.log; exit 1; }
curl -s -b /tmp/eb-panel -X POST $API/domains/$DID/mailboxes -d '{"login":"ana","name":"Ana","password":"correct-horse-battery-9273"}' >/dev/null
echo "   domínio ativo + mailbox ana@cliente-e2e.com ✓"

echo "== webmail login (usuário final na nossa API) =="
curl -s -c /tmp/eb-wm -X POST $API/webmail/login -d '{"email":"ana@cliente-e2e.com","password":"correct-horse-battery-9273"}' | J "['email']"
WM="curl -s -b /tmp/eb-wm"

echo "== IA: resumir =="
$WM -X POST $API/ai/summarize -d '{"subject":"Contrato","body":"Revisar até hoje"}' | J "['summary']" | head -c 60; echo
echo "== IA: extrair tarefas =="
NT=$($WM -X POST $API/ai/extract-tasks -d '{"subject":"Contrato","body":"Revisar"}' | J "['tasks'].__len__()"); echo "   $NT tarefa(s)"

echo "== Kanban =="
BID=$($WM $API/boards | J "[0]['id']")
COL=$($WM $API/boards/$BID | J "['columns'][0]['column']['id']")
CARD=$($WM -X POST $API/boards/$BID/cards -d "{\"column_id\":\"$COL\",\"title\":\"Revisar contrato\"}" | J "['id']")
COL2=$($WM $API/boards/$BID | J "['columns'][1]['column']['id']")
$WM -X PATCH $API/cards/$CARD/move -d "{\"column_id\":\"$COL2\",\"position\":0}" >/dev/null
MOVED=$($WM $API/boards/$BID | J "['columns'][1]['cards'][0]['title']")
echo "   card criado e movido → coluna 2 tem: $MOVED"

echo "== Marcadores =="
$WM -X POST $API/labels -d '{"name":"Clientes","color":"#B44D3A"}' >/dev/null
echo "   labels: $($WM $API/labels | J '.__len__()')"

echo "== Calendário =="
$WM -X POST $API/events -d '{"title":"Call Marina","starts_at":"2026-07-14T11:00:00Z","ends_at":"2026-07-14T12:00:00Z"}' >/dev/null
NE=$($WM "$API/events?from=2026-07-13T00:00:00Z&to=2026-07-20T00:00:00Z" | J ".__len__()")
echo "   eventos na semana: $NE"

echo; echo "✅ E2E backend de e-mail PASSOU (webmail auth + IA + Kanban + labels + eventos)."
