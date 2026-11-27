#!/bin/sh
# Production smoke test.
# Usage: scripts/smoke-prod.sh <api-origin> <web-origin>
#   e.g. scripts/smoke-prod.sh https://api.example.com https://parallel.example.com
# Read-only except for ONE small demo world created through POST /api/v1/demo
# (worlds are never deleted by this script). Exits non-zero on the first failed
# check group; all failures are listed. Needs curl only (no jq).
set -u
[ $# -eq 2 ] || { echo "usage: $0 <api-origin> <web-origin>" >&2; exit 2; }
API="${1%/}"; WEB="${2%/}"
FAILS=0
ok()   { echo "ok   $*"; }
bad()  { echo "FAIL $*" >&2; FAILS=$((FAILS+1)); }
TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
CT="--connect-timeout 5 --max-time 30"

status() { curl -s $CT -o /dev/null -w '%{http_code}' "$@"; }
# check_status <name> <expected> <curl args...>
check_status() { n=$1; e=$2; shift 2; g=$(status "$@"); [ "$g" = "$e" ] && ok "$n ($g)" || bad "$n: want $e got $g"; }
hdr() { # hdr <file> <name> -> value (case-insensitive, trimmed)
  grep -i "^$2:" "$1" | head -1 | cut -d: -f2- | tr -d '\r' | sed 's/^ *//;s/ *$//'; }

echo "API=$API WEB=$WEB (creates one small demo world)"

# 1. Probes and health
check_status "GET /healthz" 200 "$API/healthz"
check_status "GET /readyz" 200 "$API/readyz"
curl -s $CT "$API/api/v1/health" > "$TMP/health"
grep -q '"status":"ok"' "$TMP/health" && grep -q '"engineVersion":"[^"]*"' "$TMP/health" \
  && ok "GET /api/v1/health $(cat "$TMP/health")" || bad "GET /api/v1/health: $(cat "$TMP/health")"

# 2. CORS: exact origin allowed, foreign origin not
curl -s $CT -D "$TMP/pf" -o /dev/null -X OPTIONS "$API/api/v1/health" \
  -H "Origin: $WEB" -H 'Access-Control-Request-Method: POST' -H 'Access-Control-Request-Headers: content-type'
[ "$(hdr "$TMP/pf" access-control-allow-origin)" = "$WEB" ] \
  && ok "CORS preflight allows exactly $WEB" || bad "CORS preflight allow-origin is '$(hdr "$TMP/pf" access-control-allow-origin)', want $WEB"
curl -s $CT -D "$TMP/pf2" -o /dev/null -X OPTIONS "$API/api/v1/health" \
  -H 'Origin: https://evil.example' -H 'Access-Control-Request-Method: POST'
[ -z "$(hdr "$TMP/pf2" access-control-allow-origin)" ] \
  && ok "CORS preflight refuses https://evil.example" || bad "CORS preflight allowed evil.example: '$(hdr "$TMP/pf2" access-control-allow-origin)'"

# 3. API security headers
curl -s $CT -D "$TMP/h" -o /dev/null "$API/api/v1/health"
[ "$(hdr "$TMP/h" x-content-type-options)" = "nosniff" ] && ok "API X-Content-Type-Options" || bad "API X-Content-Type-Options missing"
[ -n "$(hdr "$TMP/h" referrer-policy)" ] && ok "API Referrer-Policy" || bad "API Referrer-Policy missing"
[ -n "$(hdr "$TMP/h" content-security-policy)" ] && ok "API Content-Security-Policy" || bad "API CSP missing"
case "$API" in https://*)
  curl -s $CT -D "$TMP/h2" -o /dev/null "$API/api/v1/health"
  [ -n "$(hdr "$TMP/h2" strict-transport-security)" ] && ok "API Strict-Transport-Security" || bad "API HSTS missing (ENV=production, TLS or X-Forwarded-Proto https needed)";;
esac
hdr "$TMP/h" content-security-policy | grep -q 'unsafe-eval' && bad "API CSP contains unsafe-eval" || ok "API CSP has no unsafe-eval"

# 4. Managers and LLM state (informational)
curl -s $CT "$API/api/v1/managers" > "$TMP/mgr"
if grep -q '"kind":"ai_rule_based"' "$TMP/mgr"; then
  if grep -q '"kind":"ai_llm"[^}]*"available":true' "$TMP/mgr"; then ok "managers: rule-based available, LLM available"
  else ok "managers: rule-based available, LLM unavailable (not configured or not available)"; fi
else bad "GET /api/v1/managers: $(head -c 200 "$TMP/mgr")"; fi

# 5. Demo world: create, advance to first decision window, read state, stream
curl -s $CT -X POST "$API/api/v1/demo" -H 'Content-Type: application/json' -d '{}' > "$TMP/demo"
WID=$(grep -o '"world":{"id":"[^"]*"' "$TMP/demo" | head -1 | sed 's/.*"id":"//;s/"//')
BID=$(grep -o '"rootBranch":{"id":"[^"]*"' "$TMP/demo" | head -1 | sed 's/.*"id":"//;s/"//')
if [ -n "$WID" ] && [ -n "$BID" ]; then
  ok "POST /api/v1/demo created world $WID"
  curl -s $CT -X POST "$API/api/v1/branches/$BID/advance" -H 'Content-Type: application/json' -d '{"runToTick":4}' > "$TMP/adv"
  grep -q '"headTick":4' "$TMP/adv" && grep -q '"stateHash":"[0-9a-f]\{64\}"' "$TMP/adv" \
    && ok "advance to tick 4 (first decision window)" || bad "advance: $(head -c 300 "$TMP/adv")"
  curl -s $CT "$API/api/v1/branches/$BID/state?tick=4" > "$TMP/st"
  grep -q '"tick":4\|"headTick":4' "$TMP/st" && ok "GET state at tick 4" || bad "GET state: $(head -c 300 "$TMP/st")"
  # SSE: connect briefly, expect a tick event
  curl -s -N --connect-timeout 5 --max-time 3 -H "Origin: $WEB" "$API/api/v1/branches/$BID/progress" > "$TMP/sse" 2>/dev/null
  grep -q '^event: tick' "$TMP/sse" && ok "SSE progress stream delivers 'event: tick'" || bad "SSE: no 'event: tick' within 3 s ($(head -c 120 "$TMP/sse"))"
else
  bad "POST /api/v1/demo: $(head -c 300 "$TMP/demo")"
fi

# 6. Web app
curl -s $CT -D "$TMP/w" -o /dev/null "$WEB/"
WS=$(head -1 "$TMP/w" | awk '{print $2}')
[ "$WS" = "200" ] && ok "GET $WEB/ ($WS)" || bad "GET $WEB/: status $WS"
CSP=$(hdr "$TMP/w" content-security-policy)
if [ -z "$CSP" ]; then bad "web Content-Security-Policy missing (production build required)"
else
  ok "web Content-Security-Policy present"
  echo "$CSP" | grep -q 'unsafe-eval' && bad "web CSP contains unsafe-eval" || ok "web CSP has no unsafe-eval"
  echo "$CSP" | grep -q "connect-src[^;]*$API" && ok "web CSP connect-src names the API origin" || bad "web CSP connect-src does not include $API (rebuild with NEXT_PUBLIC_API_BASE_URL)"
fi
[ "$(hdr "$TMP/w" x-content-type-options)" = "nosniff" ] && ok "web X-Content-Type-Options" || bad "web X-Content-Type-Options missing"

if [ "$FAILS" -ne 0 ]; then echo "smoke test FAILED ($FAILS)" >&2; exit 1; fi
echo "smoke test passed"
