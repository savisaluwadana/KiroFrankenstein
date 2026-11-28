# CI

Workflow: `.github/workflows/ci.yml`. Local equivalent: `scripts/ci-local.sh`.

## GitHub Actions history (`gh run list --branch main`, checked 2026-11-28)

| Run id | Commit | State | Per-job result |
|---|---|---|---|
| 46060356207 | `6636bee` | failure | `web` failed (`npm ci`: lockfile out of sync); `e2e` skipped; the other four succeeded. This was the first run on GitHub. |
| 46065036957 | `cbd0d68` | success | all six jobs succeeded (lockfile regenerated with npm 10) |
| 46098817923 | `a2f6119` | cancelled | `e2e` cancelled; the other five succeeded |
| 46100197654 | `d4f140b` | success | all six jobs succeeded |
| 46106909199 | `2ce9c05` | cancelled | `e2e` cancelled after a slow runner exceeded its time budget; the other five succeeded. `669f640` added fail-fast, traces and a longer e2e budget. |
| 46125120786 | `d387c6d` | cancelled | `e2e` cancelled; the other five succeeded. The cause was not read from the logs; a newer push (`e666480`) followed and the workflow cancels in-progress runs per ref. |
| 46126448247 | `e666480` | in progress when checked | `openapi lint` and `docker build` succeeded; `go (amd64)`, `go (arm64)` and `web` still running; `e2e` not started |

The last two green runs are 46065036957 and 46100197654. No run for a later commit had completed successfully when this was written; run `gh run list --branch main` for current results. The check table below is the 2026-11-27 local pre-push record (macOS arm64); its GitHub column was written before any run existed and is kept as history.

## Jobs

| Job | Runs on | What it does |
|---|---|---|
| `go (amd64)`, `go (arm64)` | `ubuntu-latest`, `ubuntu-24.04-arm`, each with a `postgres:16` service container | bootstrap roles and databases (`scripts/db-bootstrap.sh` in `PGHOST` mode), `parallel-api migrate up`, vet, staticcheck v0.8.2, govulncheck v1.10.0, `go test ./...` with `PARALLEL_TEST_*` URLs set, a step that fails if any PostgreSQL test skipped, race tests, three golden checks (flagship hash, duel hashes, 20-seed ensemble, byte for byte) |
| `openapi lint` | `ubuntu-latest` | `@redocly/cli@2.84.0 lint backend/api/openapi.yaml` |
| `web` | `ubuntu-latest` | `npm ci`, `npm run gen:api` then `git diff --exit-code web/lib/api/schema.d.ts`, tsc, eslint, vitest, `next build`, `npm audit --audit-level=high` |
| `docker build` | `ubuntu-latest` | `docker build ./backend` (no push), `scripts/check-image-secrets.sh`; steps are skipped if the files are absent |
| `e2e` | `ubuntu-latest` (amd64 only), `needs` all of the above, 30 min timeout | `npx playwright install --with-deps chromium`, `npm run e2e` (includes the stub-LLM duel spec; the API runs in memory; no real LLM provider) |

Other settings: `permissions: contents: read`; concurrency group per ref with `cancel-in-progress`; Go and npm module caches through `setup-go` / `setup-node`. Third-party actions use major tags (`checkout@v4`, `setup-go@v5`, `setup-node@v4`, `upload-artifact@v4`), not commit SHAs, because the SHAs could not be verified. After the first green run, resolve each tag to its SHA and pin it.

## Check table (2026-11-27 local pre-push record)

GitHub column: NOT YET RUN for every row (written 2026-11-27, before the first GitHub run; see the history above). Local column: second full run of `CI_LOCAL_DOCKER=1 bash scripts/ci-local.sh` on 2026-11-27 (macOS arm64), output in `/tmp/ci-local.txt`.

| Check | GitHub CI | Local |
|---|---|---|
| actionlint on the workflow | NOT YET RUN | PASS (no findings) |
| postgres bootstrap, migrate up | NOT YET RUN | PASS |
| go vet | NOT YET RUN | PASS |
| staticcheck v0.8.2 | NOT YET RUN | **FAIL**: `cmd/parallel-api/config.go:35:2: field llmConfigured is unused (U1000)` (new untracked file from the deploy work; not touched here) |
| govulncheck v1.10.0 | NOT YET RUN | PASS (no vulnerabilities) |
| `go test ./...` with PostgreSQL URLs | NOT YET RUN | PASS |
| PostgreSQL tests ran, none skipped | NOT YET RUN | PASS |
| race tests (`./internal/app`) | NOT YET RUN | PASS |
| golden flagship / duel / ensemble (arm64) | NOT YET RUN | PASS |
| golden flagship / duel / ensemble (linux/amd64 emulation in Docker) | NOT YET RUN | PASS (`uname -m` x86_64, all three go tests and `verify`/`ensemble` plus `cmp`) |
| redocly lint 2.84.0 | NOT YET RUN | PASS |
| web: npm ci, tsc, eslint, vitest, next build | NOT YET RUN | PASS |
| web: `npm audit --audit-level=high` | NOT YET RUN | PASS with `--registry=https://registry.npmjs.org` (the local default registry mirror does not implement audit) |
| web: generated schema in sync with openapi.yaml | NOT YET RUN | **FAIL** in the working tree: `openapi.yaml` has uncommitted edits (+125 lines of generated types) and `web/lib/api/schema.d.ts` was not regenerated. The committed HEAD pair is in sync (checked). Fix: `cd web && npm run gen:api` and commit both files. |
| docker build, check-image-secrets | NOT YET RUN | PASS |
| Playwright e2e (chromium) | NOT YET RUN | not run in this pass (`CI_LOCAL_E2E=1` to run it) |

## Local reproduction

```sh
docker compose up -d --wait db            # or: DB_PORT=55432 docker compose up -d --wait db
DB_PORT=55432 bash scripts/ci-local.sh    # Go, openapi, web jobs; PASS/FAIL per step
CI_LOCAL_DOCKER=1 CI_LOCAL_E2E=1 DB_PORT=55432 bash scripts/ci-local.sh   # adds docker and Playwright
docker run --rm -v "$PWD":/repo -w /repo rhysd/actionlint:latest          # lint the workflows
```

Do not run the script while another process uses the same `parallel_test` database: the PostgreSQL decision tests assert global usage counters and fail when rows from a concurrent run are present.

Golden checks under linux/amd64 emulation (what the amd64 runner executes):

```sh
docker run --rm --platform linux/amd64 -v "$PWD":/src -w /src/backend golang:1.25 sh -c 'go test ./internal/sim -run TestGoldenFlagshipHash -count=1 && go test ./internal/app -run TestGoldenDuelHashes -count=1 && go test ./internal/app/ensemble -run TestGoldenEnsembleReport -count=1'
```

## How to see CI runs

Nothing is pushed or triggered by this repository's tooling. When you decide to:

```sh
git push origin main        # or push a branch and open a PR
gh run list --limit 5
gh run watch                # pick the run
gh run view --log-failed    # on failure
```

The first run did need a fix (the npm lockfile); the arm64 PostgreSQL service container and Playwright browser install have since run on GitHub (see the history).
