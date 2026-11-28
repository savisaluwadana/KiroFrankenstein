#!/usr/bin/env bash
# Reproduces the headline numbers of the flagship demo (docs/demo-script.md) from a running API, using only curl and jq.
# Usage: scripts/demo-numbers.sh [API_BASE]     default http://localhost:8080/api/v1
# Needs a fresh API with the default flagship scenario (seed 2026) and no LLM. It creates its own worlds; nothing is deleted.
# Output is a plain list of numbers. Synthetic simulation output under the stated assumptions; not a forecast.
set -euo pipefail

API="${1:-http://localhost:8080/api/v1}"
J='content-type: application/json'
TOKEN=""   # write token of the world being changed; set after each world is created
post() { local body="${2:-}"; [ -n "$body" ] || body='{}'; local h=(-H "$J"); [ -z "$TOKEN" ] || h+=(-H "X-Parallel-Write-Token: $TOKEN"); curl -fsS -X POST "${h[@]}" "$API$1" -d "$body"; }
get() { curl -fsS "$API$1"; }
money() { awk -v c="$1" 'BEGIN{s=(c<0)?"-":""; if(c<0)c=-c; printf "%s$%d.%02d", s, int(c/100), c%100}'; }
pct() { awk -v b="$1" 'BEGIN{printf "%.1f%%", b/100}'; }

# Runs a branch to `to`, ending every decision window with no commands. Prints the head reached.
run_to() {
  local b="$1" to="$2" head
  for _ in $(seq 1 80); do
    head=$(post "/branches/$b/advance" "{\"runToTick\":$to}" | jq -r .headTick)
    if [ "$head" -ge "$to" ]; then echo "$head"; return; fi
    post "/branches/$b/decision-window/end" "{\"windowTick\":$head}" >/dev/null
  done
  echo "did not reach $to" >&2; exit 1
}
metric() { get "/branches/$1/metrics?tick=$2" | jq -r ".metrics | [.netProfitCents,.serviceLevelBps,.backlogOrders,.cashCents] | @tsv"; }
show() { # label branch tick
  read -r np sl bl cash < <(metric "$2" "$3")
  printf '  %-34s profit %s | on time %s | orders waiting %s | cash %s\n' "$1" "$(money "$np")" "$(pct "$sl")" "$bl" "$(money "$cash")"
}

echo "== Reality fork (seed 2026) =="
demo=$(post /demo)
TOKEN=$(echo "$demo" | jq -r .writeToken)
root=$(echo "$demo" | jq -r .rootBranch.id)
fork=$(echo "$demo" | jq -r .plan.forkTick)
crisis=$(echo "$demo" | jq -r .plan.crisisTick)
cmds=$(echo "$demo" | jq -c '[.plan.commands[] | if .kind=="place_purchase_order" then {kind,sku:.skuIndex,role,qty} else {kind,qty} end]')
echo "  plan: crisis tick $crisis, fork tick $fork, commands $cmds"
first=$(post "/branches/$root/advance" '{"runToTick":120}' | jq -r .headTick)
echo "  first surprise pause (head after run-to-horizon in surprise mode): tick $first"
post "/branches/$root/decision-window/end" "{\"windowTick\":$first}" >/dev/null
# Rewind is a read; fork at the plan's tick (the root already ran past it).
child=$(post "/branches/$root/fork" "{\"atTick\":$fork,\"label\":\"demo fork\"}" | jq -r .id)
post "/branches/$child/commands" "{\"windowTick\":$fork,\"commands\":$cmds}" | jq -c '{accepted:(.accepted|length), rejected:(.rejected|length)}'
run_to "$child" "$first" >/dev/null
show "original path @ tick $first" "$root" "$first"
show "forked reality @ tick $first" "$child" "$first"
run_to "$root" 120 >/dev/null
run_to "$child" 120 >/dev/null
show "original path @ tick 120" "$root" 120
show "forked reality @ tick 120" "$child" 120
echo "  state hash @120 original: $(get "/branches/$root/state?tick=120" | jq -r .stateHash)"
echo "  state hash @120 forked:   $(get "/branches/$child/state?tick=120" | jq -r .stateHash)"
dbody=$(jq -nc --arg a "$root" --arg b "$child" '{branchA:$a,branchB:$b}')
dtick=$(post /diffs "$dbody" | jq -r .divergence.tick)
echo "  first divergence tick: $dtick"

echo "== Duel: human plays the same two moves at tick $fork, then ends every window; rule-based AI =="
TOKEN=""
duelresp=$(post /duels '{"forkTick":0}')
TOKEN=$(echo "$duelresp" | jq -r .writeToken)
duel=$(echo "$duelresp" | jq -r .duel.id)
post "/duels/$duel/decision-window/end" '{"windowTick":0}' >/dev/null
post "/duels/$duel/advance" "{\"runToTick\":$fork}" >/dev/null
st=$(get "/duels/$duel" | jq -r '.status.humanHeadTick')
[ "$st" = "$fork" ] || { echo "duel head is $st, expected $fork" >&2; exit 1; }
post "/duels/$duel/commands" "{\"windowTick\":$fork,\"commands\":$cmds}" | jq -c '{accepted:(.accepted|length), rejected:(.rejected|length)}'
for _ in $(seq 1 80); do
  status=$(get "/duels/$duel" | jq -c .status)
  [ "$(echo "$status" | jq -r .finished)" = "true" ] && break
  [ "$(echo "$status" | jq -r .windowOpen)" = "true" ] && post "/duels/$duel/decision-window/end" "{\"windowTick\":$(echo "$status" | jq -r .humanHeadTick)}" >/dev/null
  post "/duels/$duel/advance" '{"runToTick":120}' >/dev/null || true
done
score=$(get "/duels/$duel/score")
echo "$score" | jq -r '"  comparison tick \(.comparisonTick), provisional=\(.provisional), outcome=\(.outcome)"'
echo "$score" | jq -r '"  human: \(.human.score.compositeHundredths/100) points, profit cents \(.human.metrics.netProfitCents), on time bps \(.human.metrics.serviceLevelBps), waiting \(.human.metrics.backlogOrders), cancelled \(.human.cancelledOrders)"'
echo "$score" | jq -r '"  AI:    \(.ai.score.compositeHundredths/100) points, profit cents \(.ai.metrics.netProfitCents), on time bps \(.ai.metrics.serviceLevelBps), waiting \(.ai.metrics.backlogOrders), cancelled \(.ai.cancelledOrders)"'
hb=$(echo "$score" | jq -r .human.branchId)
echo "  ledger Profit so far (human): $(get "/branches/$hb/ledger/summary?tick=120" | jq -r '.figures[]|select(.figure=="net_profit")|"\(.sumCents) cents from \(.entryCount) entries"')"
echo "Synthetic simulation output under the stated assumptions. Not a forecast or guarantee of real-world results."
