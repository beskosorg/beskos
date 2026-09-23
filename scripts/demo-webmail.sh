#!/usr/bin/env bash
# Brings up the full stack and leaves the React webmail (wired to the real backend) ready for
# screenshots. Provisions domain+mailboxes, seeds emails, runs API+worker+fake AI+Vite.
set -euo pipefail
PG=demo-pg; SW=demo-sw
PGPORT=55436; SWPORT=18093; AIPORT=8792; VITEPORT=5199
SECRET=probe-secret
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; BACKEND="$ROOT/backend"; FRONT="$ROOT/frontend"

cleanup(){ for p in API_PID WK_PID AI_PID VITE_PID; do [[ -n "${!p:-}" ]]&&kill ${!p} 2>/dev/null||true; done
  docker rm -f $PG $SW >/dev/null 2>&1||true; rm -f /tmp/fakeai.py /tmp/demo-send.mjs; }
trap cleanup EXIT

echo "== infra =="
docker rm -f $PG $SW >/dev/null 2>&1||true
docker run -d --name $PG -e POSTGRES_USER=mailin -e POSTGRES_PASSWORD=d -e POSTGRES_DB=mailin -p $PGPORT:5432 postgres:16-alpine >/dev/null
docker run -d --name $SW -e STALWART_RECOVERY_ADMIN="admin:$SECRET" -p $SWPORT:8080 stalwartlabs/stalwart:v0.16.4 >/dev/null
for i in $(seq 1 30); do docker exec $PG pg_isready -U mailin >/dev/null 2>&1 && break; sleep 1; done
for i in $(seq 1 30); do curl -fsS http://localhost:$SWPORT/jmap/session -u admin:$SECRET >/dev/null 2>&1 && break; sleep 1; done

# Context-aware fake AI (summary/briefing = sentence; extraction = JSON)
cat > /tmp/fakeai.py <<'PY'
import http.server,json,sys
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        raw=self.rfile.read(int(self.headers.get('Content-Length',0))).decode()
        if "array JSON" in raw: c='[{"title":"Revisar Contrato Aurora v3","due":"hoje","assignee":"Ana"},{"title":"Responder Banco Carvalho","due":"15/07","assignee":"Ana"}]'
        elif "resumindo o dia" in raw: c="Bom dia, Ana. Você tem 2 e-mails aguardando resposta — a proposta da Marina e a fatura do Banco Carvalho. Comece pela proposta, que precisa seguir para assinatura esta semana."
        else: c="Marina enviou o contrato revisado com ajustes na cláusula de SLA e quer seguir para assinatura ainda esta semana. Ação sugerida: revisar o anexo e responder."
        b=json.dumps({"model":"deepseek-v4","choices":[{"message":{"role":"assistant","content":c}}],"usage":{"total_tokens":20}}).encode()
        self.send_response(200);self.send_header("Content-Type","application/json");self.end_headers();self.wfile.write(b)
    def log_message(self,*a):pass
http.server.HTTPServer(("127.0.0.1",int(sys.argv[1])),H).serve_forever()
PY
python3 /tmp/fakeai.py $AIPORT & AI_PID=$!

export DATABASE_URL="postgres://mailin:d@localhost:$PGPORT/mailin?sslmode=disable"
export STALWART_URL="http://localhost:$SWPORT" STALWART_ADMIN_ID=admin STALWART_ADMIN_SECRET=$SECRET
export STALWART_HOSTNAME=mail.aurora-demo.com STALWART_DEFAULT_DOMAIN=root-demo.com
export AI_PROVIDER=deepseek AI_API_KEY=fake AI_BASE_URL="http://localhost:$AIPORT" AI_MODEL=deepseek-v4
cd "$BACKEND"
echo "== bootstrap + restart =="; go run ./cmd/bootstrap >/dev/null 2>&1; docker restart $SW >/dev/null
for i in $(seq 1 30); do curl -fsS http://localhost:$SWPORT/jmap/session -u admin:$SECRET >/dev/null 2>&1 && break; sleep 1; done
echo "== api(:8000) + worker =="
HTTP_ADDR=":8000" DEV_SIGNUP_ENABLED=true go run ./cmd/api >/tmp/demo-api.log 2>&1 & API_PID=$!
PROVISION_SKIP_DNS_VERIFY=true go run ./cmd/worker >/tmp/demo-wk.log 2>&1 & WK_PID=$!
for i in $(seq 1 20); do curl -fsS localhost:8000/healthz >/dev/null 2>&1 && break; sleep 1; done

J(){ python3 -c "import sys,json;print(json.load(sys.stdin)$1)"; }
PW=correct-horse-battery-9273
echo "== provisionar aurora-demo.com + mailboxes =="
curl -s -c /tmp/demo-panel -X POST localhost:8000/api/auth/signup -d '{"org_name":"Aurora","email":"admin@aurora.com","password":"'$PW'"}' >/dev/null
DID=$(curl -s -b /tmp/demo-panel -X POST localhost:8000/api/domains -d '{"dominio":"aurora-demo.com"}' | J "['id']")
for i in $(seq 1 30); do ST=$(curl -s -b /tmp/demo-panel localhost:8000/api/domains/$DID | J "['provision_state']"); [[ $ST == active ]]&&break; sleep 2; done
for u in "ana:Ana Ribeiro" "marina:Marina Alves" "banco:Banco Carvalho"; do
  L=${u%%:*}; N=${u#*:}
  curl -s -b /tmp/demo-panel -X POST localhost:8000/api/domains/$DID/mailboxes -d "{\"login\":\"$L\",\"name\":\"$N\",\"password\":\"$PW\"}" >/dev/null
done
echo "   ativo + ana/marina/banco criadas"

echo "== semear e-mails para ana =="
cat > /tmp/demo-send.mjs <<'MJS'
const SW=process.env.SW, PW="correct-horse-battery-9273";
async function jmap(auth,using,calls){const r=await fetch(SW+"/jmap/",{method:"POST",headers:{Authorization:auth,"Content-Type":"application/json"},body:JSON.stringify({using:["urn:ietf:params:jmap:core",...using],methodCalls:calls})});return (await r.json()).methodResponses.map(m=>m[1]);}
async function send(from,to,subject,body){const auth="Basic "+Buffer.from(from+":"+PW).toString("base64");
  const s=await (await fetch(SW+"/jmap/session",{headers:{Authorization:auth}})).json();const acc=s.primaryAccounts["urn:ietf:params:jmap:mail"];
  const M=["urn:ietf:params:jmap:mail"],S=["urn:ietf:params:jmap:mail","urn:ietf:params:jmap:submission"];
  const [bx]=await jmap(auth,M,[["Mailbox/get",{accountId:acc,ids:null},"0"]]);const drafts=(bx.list.find(b=>b.role==="drafts")||bx.list[0]).id;
  const [id]=await jmap(auth,S,[["Identity/get",{accountId:acc,ids:null},"0"]]);const identityId=id.list[0].id;
  await jmap(auth,S,[["Email/set",{accountId:acc,create:{d:{mailboxIds:{[drafts]:true},keywords:{$draft:true},from:[{email:from}],to:[{email:to}],subject,bodyStructure:{type:"text/plain",partId:"p"},bodyValues:{p:{value:body}}}}},"1"],["EmailSubmission/set",{accountId:acc,onSuccessDestroyEmail:["#s"],create:{s:{emailId:"#d",identityId,envelope:{mailFrom:{email:from},rcptTo:[{email:to}]}}}},"2"]]);}
await send("marina@aurora-demo.com","ana@aurora-demo.com","Proposta revisada — Contrato Aurora","Oi Ana,\n\nAnexei a versão revisada do contrato com os ajustes que combinamos. Podemos seguir para assinatura ainda esta semana?\n\nAbraço,\nMarina");
await send("banco@aurora-demo.com","ana@aurora-demo.com","Sua fatura de julho está disponível","Olá,\n\nSua fatura de julho, R$ 2.480,00, já está disponível. Vencimento 15/07.");
console.log("SENT");
MJS
SW="http://localhost:$SWPORT" node /tmp/demo-send.mjs

echo "== vite dev (:$VITEPORT) =="
cd "$FRONT"
STALWART_URL="http://localhost:$SWPORT" npx vite --port $VITEPORT >/tmp/demo-vite.log 2>&1 & VITE_PID=$!
# vite's /api proxy points to :8000 (vite.config default)
for i in $(seq 1 25); do curl -fsS localhost:$VITEPORT/ >/dev/null 2>&1 && break; sleep 1; done
echo "   webmail em http://localhost:$VITEPORT/mail  (ana@aurora-demo.com / $PW)"

echo "== screenshots =="
cat > "$FRONT/_shot.mjs" <<'EOF'
import pptr from "puppeteer-core";
const B="http://localhost:5199",O=process.env.O;
const b=await pptr.launch({executablePath:"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",headless:"new",args:["--no-sandbox"]});
const p=await b.newPage();const errs=[];p.on("pageerror",e=>errs.push(""+e));p.on("console",m=>{if(m.type()==="error")errs.push("console:"+m.text());});
await p.setViewport({width:1480,height:920,deviceScaleFactor:1.5});
const S=ms=>new Promise(r=>setTimeout(r,ms));
await p.goto(B+"/mail",{waitUntil:"networkidle0"});
await p.type('input[type="email"]',"ana@aurora-demo.com");await p.type('input[type="password"]',"correct-horse-battery-9273");
await p.click(".send");await S(3500);
await p.screenshot({path:O+"/w-today.png"});
const app=async(i,name,ms)=>{try{await p.$$eval(".apprail .app",(els,ix)=>els[ix].click(),i);await S(ms);await p.screenshot({path:O+"/w-"+name+".png"});}catch(e){errs.push(name+":"+e.message);}};
await app(1,"mail",1500); try{await p.click(".row");await S(2500);await p.screenshot({path:O+"/w-mail.png"});}catch(e){errs.push("openmail:"+e.message);}
await app(2,"kanban",2000);
await app(3,"cal",1500);
console.log("PAGE_ERRORS",errs.length?errs:"none");
await b.close();
EOF
O="$1" node "$FRONT/_shot.mjs"; rm -f "$FRONT/_shot.mjs"
echo "OK screenshots em $1"
