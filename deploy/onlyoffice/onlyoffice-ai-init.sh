#!/bin/sh
# Injects the "Beskos AI" config into the Document Server's default.json before startup, so the
# AI plugin shows up PRE-CONFIGURED (server mode: uses our proxy, no pasting a key or picking a
# model). Fails safe: if the injection goes wrong, the DS starts up anyway.
python3 - "${AI_PROXY_URL:-http://localhost:5173/api/ai/proxy}" <<'PY' || echo "ai init: skipped"
import json, sys
path = "/etc/onlyoffice/documentserver/default.json"
proxy = sys.argv[1]
try:
    with open(path) as f:
        cfg = json.load(f)
    model = {"name": "Beskos AI", "id": "deepseek-chat", "provider": "Beskos AI", "capabilities": 321}
    cfg["aiSettings"] = {
        "version": 4,
        "proxy": proxy,
        "timeout": "5m",
        "customProviders": {},
        "allowedCorsOrigins": ["http://localhost:8082", "http://localhost:5173", "http://localhost:8000"],
        "providers": {"Beskos AI": {"name": "Beskos AI", "url": "https://api.deepseek.com/v1", "key": "server", "models": [model]}},
        "models": [model],
        "actions": {
            "Chat": {"model": "deepseek-chat"},
            "Summarization": {"model": "deepseek-chat"},
            "Translation": {"model": "deepseek-chat"},
            "TextAnalyze": {"model": "deepseek-chat"},
        },
    }
    with open(path, "w") as f:
        json.dump(cfg, f)
    print("ai init: Beskos AI injected (proxy=%s)" % proxy)
except Exception as e:
    print("ai init: failed: %s" % e)
    sys.exit(1)
PY

# Beskos brand in the editor header: flips the client's branding switches. See the script header
# for the license stance (attribution kept) and DIVERGENCES.md.
sh /scripts/onlyoffice-branding-patch.sh || echo "branding patch: skipped"

exec /app/ds/run-document-server.sh "$@"
