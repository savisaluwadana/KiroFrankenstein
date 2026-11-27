#!/usr/bin/env bash
# Local equivalent of .github/workflows/ci.yml: same commands, same order, per job.
# Prints a PASS/FAIL line per step and a summary; exits non-zero if any step failed.
#
#   bash scripts/ci-local.sh                 # everything except e2e and docker
#   CI_LOCAL_E2E=1 CI_LOCAL_DOCKER=1 bash scripts/ci-local.sh
#
# PostgreSQL: the compose db service on DB_PORT (default 55432 here; compose default is 5432).
# Differences from CI: one architecture (this machine's), the web schema check compares against the
# working-tree file instead of `git diff` (so uncommitted edits are judged as they stand).
set -u
cd "$(dirname "$0")/.."
ROOT=$PWD
export DB_PORT="${DB_PORT:-55432}"
PGURL_HOST="localhost:${DB_PORT}"
export MIGRATE_DATABASE_URL="${MIGRATE_DATABASE_URL:-postgres://parallel_migrator:migrator_dev_password@${PGURL_HOST}/parallel_test?sslmode=disable}"
export PARALLEL_TEST_DATABASE_URL="${PARALLEL_TEST_DATABASE_URL:-postgres://parallel_app:app_dev_password@${PGURL_HOST}/parallel_test?sslmode=disable}"
export PARALLEL_TEST_MIGRATE_DATABASE_URL="${PARALLEL_TEST_MIGRATE_DATABASE_URL:-$MIGRATE_DATABASE_URL}"

RESULTS=()
FAILED=0
head_() { printf '\n==== %s ====\n' "$1"; }
step() { # step "name" command...  (runs in a subshell from $ROOT)
  local name=$1; shift
  printf -- '--- %s\n' "$name"
  if ( cd "$ROOT" && "$@" ); then
    RESULTS+=("PASS  $name")
  else
    RESULTS+=("FAIL  $name"); FAILED=1
  fi
}
skip() { RESULTS+=("SKIP  $1 ($2)"); }

pg_must_run() {
  set -o pipefail
  ( cd backend
    go test ./internal/store/postgres -v -count=1 | tee /tmp/pg-store.txt
    go test ./internal/httpapi -run TestPostgresRestartSmoke -v -count=1 | tee /tmp/pg-http.txt )
  if grep -E -- '--- SKIP|^SKIP' /tmp/pg-store.txt /tmp/pg-http.txt; then echo "PostgreSQL tests were skipped"; return 1; fi
  grep -q -- '--- PASS: TestInputLogImmutable' /tmp/pg-store.txt && grep -q -- '--- PASS: TestPostgresRestartSmoke' /tmp/pg-http.txt
}
golden_flagship() {
  cd backend
  go test ./internal/sim -run TestGoldenFlagshipHash -count=1 &&
  go run ./cmd/parallel-sim verify --scenario scenarios/black-friday.json --seed 2026 --inputs scenarios/demo-fork-inputs.json --expect "$(cat scenarios/golden/demo-fork.hash)"
}
golden_duel() {
  cd backend
  go test ./internal/app -run TestGoldenDuelHashes -count=1 &&
  go run ./cmd/parallel-sim verify --scenario scenarios/black-friday.json --seed 2026 --inputs scenarios/demo-duel-human.json --expect "$(awk '$1=="human" && $2=="final" {print $3}' scenarios/golden/demo-duel.hashes)" &&
  go run ./cmd/parallel-sim verify --scenario scenarios/black-friday.json --seed 2026 --inputs scenarios/demo-duel-ai.json --expect "$(awk '$1=="ai" && $2=="final" {print $3}' scenarios/golden/demo-duel.hashes)"
}
golden_ensemble() {
  cd backend
  go test ./internal/app/ensemble -run TestGoldenEnsembleReport -count=1 &&
  go run ./cmd/parallel-sim ensemble --scenario scenarios/black-friday.json --seeds 1..20 --arm fixed:scenarios/demo-duel-human.json --arm rulebased --out /tmp/ens-local &&
  cmp /tmp/ens-local.json scenarios/golden/ensemble-flagship-20.json &&
  cmp /tmp/ens-local.csv scenarios/golden/ensemble-flagship-20.csv
}
web_schema_in_sync() {
  cd web
  npx openapi-typescript ../backend/api/openapi.yaml -o /tmp/schema-local.d.ts >/dev/null &&
  diff -q /tmp/schema-local.d.ts lib/api/schema.d.ts
}

head_ "go job (PostgreSQL service = compose db on port $DB_PORT)"
step "postgres up + bootstrap (scripts/db-bootstrap.sh)" sh scripts/db-bootstrap.sh
step "postgres migrate up (parallel_test)" bash -c 'cd backend && go run ./cmd/parallel-api migrate up'
step "go vet" bash -c 'cd backend && go vet ./...'
step "staticcheck v0.8.2" bash -c 'cd backend && go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...'
step "govulncheck v1.10.0" bash -c 'cd backend && go run golang.org/x/vuln/cmd/govulncheck@v1.10.0 ./...'
step "go test ./... (PostgreSQL enabled)" bash -c 'cd backend && go test ./...'
step "postgres tests must run, not skip" pg_must_run
step "race tests" bash -c "cd backend && go test -race ./internal/app -run 'TestAIConcurrency|TestLLM|TestAIWindow|TestDuel|TestConcurrent' -count=1"
step "golden flagship hash" bash -c "$(declare -f golden_flagship); golden_flagship"
step "golden demo duel hashes" bash -c "$(declare -f golden_duel); golden_duel"
step "golden flagship ensemble report" bash -c "$(declare -f golden_ensemble); golden_ensemble"

head_ "openapi job"
step "redocly lint 2.84.0" npx --yes @redocly/cli@2.84.0 lint backend/api/openapi.yaml

head_ "web job"
step "npm ci" bash -c 'cd web && npm ci'
step "generated API types in sync with openapi.yaml" bash -c "$(declare -f web_schema_in_sync); web_schema_in_sync"
step "tsc --noEmit" bash -c 'cd web && npx tsc --noEmit'
step "eslint" bash -c 'cd web && npx eslint .'
step "vitest --run" bash -c 'cd web && npx vitest --run'
step "next build" bash -c 'cd web && npm run build'
step "npm audit --audit-level=high" bash -c 'cd web && npm audit --audit-level=high --registry=https://registry.npmjs.org'

head_ "docker job"
if [ "${CI_LOCAL_DOCKER:-0}" = 1 ] && [ -f backend/Dockerfile ]; then
  step "docker build ./backend" docker build -t parallel-api ./backend
  step "check-image-secrets" sh scripts/check-image-secrets.sh parallel-api
else
  skip "docker build + check-image-secrets" "set CI_LOCAL_DOCKER=1"
fi

head_ "e2e job"
if [ "${CI_LOCAL_E2E:-0}" = 1 ]; then
  step "playwright (npm run e2e)" bash -c 'cd web && npm run e2e'
else
  skip "playwright e2e" "set CI_LOCAL_E2E=1"
fi

head_ "summary"
printf '%s\n' "${RESULTS[@]}"
[ "$FAILED" = 0 ] && echo "ALL STEPS PASSED" || echo "SOME STEPS FAILED"
exit "$FAILED"
