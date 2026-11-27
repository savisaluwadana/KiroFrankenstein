# KiroFrankenstein

A new project repository for the **Kiroween 2026** hackathon, **Frankenstein** category.

## Current status

**Concept approved: Parallel.** *Reality has no undo button. We built one.* A playable, forkable simulation of an e-commerce business in a Black Friday crisis, where a human and an AI manager compete under identical conditions.

**M0 to M6 are implemented and tested, except as noted below:** the vertical slice, the complete engine and ledger (engine version 2, flagship hash `f6f41425...fac80` after the M6 calibration), PostgreSQL persistence with snapshots, replay and engine-version mismatch flags, the studio with an SSE progress stream, ledger drill-down, command forms and disruption injection (M4), the rule-based and LLM AI managers with the Human vs AI duel view (M5), and in M6 the branch diff ("What changed?", `POST /api/v1/diffs`), the assumptions endpoint and panel ("How this is modelled", `GET /api/v1/worlds/{id}/assumptions`, with an explicit reveal of surprises that already happened), the CLI ensemble runner (`parallel-sim ensemble`), the flagship calibration (`parallel-sim calibrate`, `docs/calibration.md`) and LLM calls made outside the service lock. Not complete: task 5.7 is partial (decorative scene interpolation `scene/interpolate.ts` is not built; label collision handling and readability are done). M7 and later are not started. The CI matrix (amd64 and arm64 runners) has not run on GitHub yet; the golden tests were run locally on arm64 and under linux/amd64 emulation in Docker (see below). **Live LLM provider verification: PENDING** (see below). The Go module requires Go 1.25. Steering lives in `.kiro/steering/` and the spec (requirements, design, tasks) in `.kiro/specs/parallel-simulation-mvp/`; the studio redesign is described in `docs/ux-redesign.md`.

## Run locally

Prerequisites: Go 1.25+ (go.mod requires 1.25.0) and Node 20+.

1. API (port 8080, in-memory store, worlds are lost on restart):
   `cd backend && WEB_ORIGIN=http://localhost:3100 go run ./cmd/parallel-api`
2. Web app (port 3100; `NEXT_PUBLIC_API_BASE_URL` defaults to `http://localhost:8080/api/v1`):
   `cd web && npm install && npm run dev`
3. Open http://localhost:3100 and click "Start Black Friday demo".

Headless run:

```
cd backend && go run ./cmd/parallel-sim run --scenario scenarios/black-friday.json --seed 2026
```

Output at the time of writing: engine version 2, tick 120, state hash `f6f414259c1cbbc7f76dd0c0d2b129cc34d6e98e906868dfea7d68787fffac80`, net profit 1778400 cents, service level 4427 bps. (Before the flagship calibration of task 7.16 the same command printed hash `4c6d0dd08cf7c1c8c27991355f734705356aced57fe060556e0d038ad03ab99e`, net profit -4342800 cents and service level 2214 bps; the Scenario data changed, the engine version did not. Engine version 1 produced hash `96e7d8d94bc1b3d0e81598272df984b0e0afe86c8e848bc34275781b513bce08`; version 2 adds the per-tick severity draw and the disruption role field. See `docs/calibration.md` for every calibration change.)

The calibration tables are reproducible with `cd backend && go run ./cmd/parallel-sim calibrate --scenario scenarios/black-friday.json --seeds 1..20`.

Tests:

- `cd backend && go test ./...`
- Ensembles (CLI only, up to 50 seeds per arm, no network or LLM): `cd backend && go run ./cmd/parallel-sim ensemble --scenario scenarios/black-friday.json --seeds 1..20 --arm fixed:scenarios/demo-duel-human.json --arm rulebased --out /tmp/ens` writes `/tmp/ens.json` and `/tmp/ens.csv`; the committed 20-seed report is `backend/scenarios/golden/ensemble-flagship-20.{json,csv}` and CI compares it byte for byte. `parallel-sim calibrate --scenario scenarios/black-friday.json --seeds 1..20` prints the calibration tables.
- `cd web && npx tsc --noEmit && npx eslint . && npx vitest --run`
- `cd web && npm run e2e` runs the real-browser suite (Playwright, chromium, desktop 1440x900 and phone 390x844) against the real Go API and a production build of the web app. It starts both servers itself on ports 18080 and 3199 and writes screenshots to `docs/screenshots/`. One-time setup: `npx playwright install chromium`.
  - Specs: `journey`, `metrics`, `setup-and-perf` (studio), `commands` (every management command through its card, checked against export, metrics and ledger), `ledger-sse` (ledger drill-down sums, SSE stream, polling fallback, injected surprise), `diff` (the demo plan in a forked future, then "What changed?" checked against `POST /diffs` and the branch export), `assumptions` (values, ranges and the surprise reveal checked against the API), `duel` (Human vs Rule-based AI to the horizon, every scoreboard number checked against `GET /duels/{id}/score` and branch metrics) and `duel-llm-stub`.
  - `duel-llm-stub.spec.ts` checks the LLM path against a **stub provider**: a local OpenAI-compatible server on `127.0.0.1` (random port) and a second API instance started by the spec with `LLM_BASE_URL=http://127.0.0.1:<port>/v1`, `LLM_API_KEY=stub-key-not-secret`, `LLM_MODEL=stub-model` (all fake). The web build is pointed at that API with a Playwright route rewrite. It covers valid proposals, the `invalid_json` and `http_error` fallbacks, escaped rationale, usage counters, that the key appears in no response, DOM or console output, and that reloading makes zero provider calls.
  - **Live LLM verification is pending:** no runtime API key is configured, so no test has called a real provider (OpenRouter or other).

PostgreSQL store tests (`backend/internal/store/postgres`) run the shared repository contract suite and `TestRestartRebuild` (a cold service over the same database must rebuild every branch with hashes equal to the stored `tick_summaries`). They are skipped unless `PARALLEL_TEST_DATABASE_URL` is set. A Homebrew PostgreSQL on port 5432 can shadow the container, so publish the container on another port with `DB_PORT`:

```
DB_PORT=55432 docker compose up -d --wait db
cd backend
MIGRATE_DATABASE_URL='postgres://parallel_migrator:migrator_dev_password@127.0.0.1:55432/parallel_test?sslmode=disable' go run ./cmd/parallel-api migrate up
PARALLEL_TEST_DATABASE_URL='postgres://parallel_app:app_dev_password@127.0.0.1:55432/parallel_test?sslmode=disable' go test ./internal/store/... -count=1
```

`TestInputLogImmutable` also needs `PARALLEL_TEST_MIGRATE_DATABASE_URL` (the `parallel_migrator` owner URL above, same value as `MIGRATE_DATABASE_URL`) and is skipped without it. It connects as the table owner to prove the triggers reject UPDATE, DELETE and TRUNCATE on `input_log` even for the owner, then checks that the runtime role is denied by permissions. Input log rows cannot be deleted, so each run leaves a few uniquely identified fixture rows in the test database.

```
PARALLEL_TEST_DATABASE_URL='postgres://parallel_app:app_dev_password@127.0.0.1:55432/parallel_test?sslmode=disable' \
PARALLEL_TEST_MIGRATE_DATABASE_URL='postgres://parallel_migrator:migrator_dev_password@127.0.0.1:55432/parallel_test?sslmode=disable' \
go test ./internal/store/postgres -run TestInputLogImmutable -count=1 -v
```

The tests connect as the runtime role `parallel_app` (no DDL, no UPDATE or DELETE on `input_log`). They use fresh random UUIDs per run and never truncate, so they can be re-run against the same database.

### Running the API with PostgreSQL

Without `DATABASE_URL` the API keeps worlds in memory and loses them on exit. With it, worlds, branches and the input log persist, and a restarted API rebuilds any branch by replay (snapshots speed this up). `DATABASE_URL` is the runtime role (no DDL); the schema is applied separately with the owner role. The URL is never logged.

```
DB_PORT=55432 docker compose up -d --wait db
cd backend
MIGRATE_DATABASE_URL='postgres://parallel_migrator:migrator_dev_password@127.0.0.1:55432/parallel?sslmode=disable' go run ./cmd/parallel-api migrate up
DATABASE_URL='postgres://parallel_app:app_dev_password@127.0.0.1:55432/parallel?sslmode=disable' go run ./cmd/parallel-api
```

If the schema is missing the server exits with an error telling you to run `migrate up`. The `tick_summaries` table is a derived cache written on each advance; its hashes are checked against replayed hashes and never used as the source of state.

### Configuring the AI manager (OpenRouter)

The rule-based AI manager needs no configuration. The LLM AI manager is optional and appears only when `LLM_BASE_URL`, `LLM_API_KEY` and `LLM_MODEL` are all set. The project hardcodes no model: pick a model id from the provider's catalogue (for OpenRouter, https://openrouter.ai/models) and set it yourself. Providers bill runtime usage; it is independent of Kiro credits.

| Variable | Meaning | Default and bounds |
|---|---|---|
| `LLM_BASE_URL` | OpenAI-compatible chat completions base URL | `https://openrouter.ai/api/v1` (set in `.env.example`) |
| `LLM_API_KEY` | Provider API key | none |
| `LLM_MODEL` | Model id from the provider catalogue | none (required) |
| `LLM_REQUEST_TIMEOUT_SECONDS` | Real-time limit per provider call | 20, range 1..120 |
| `LLM_MAX_CALLS_PER_WORLD` | Provider calls allowed per world | 40, range 1..200 |
| `LLM_MAX_TOKENS_PER_CALL` | Sent as `max_tokens` | 1024, range 64..8192 |
| `LLM_MAX_TOTAL_TOKENS_PER_WORLD` | Provider-reported token budget per world | 200000, range 1000..5000000 |
| `LLM_HTTP_REFERER` | Optional OpenRouter attribution header | empty |
| `LLM_APP_TITLE` | Optional OpenRouter attribution header | empty |

The API reads these from the process environment only; it does not load `.env` itself. Either export them in the shell or load an untracked `.env` (copied from `.env.example`, never committed). Note that `.env.example` also contains `DATABASE_URL`: once it is set the server requires that PostgreSQL (and exits with "cannot connect to PostgreSQL" if it is not running there). To run in memory, delete the `DATABASE_URL` and `MIGRATE_DATABASE_URL` lines from your `.env`, or run `unset DATABASE_URL MIGRATE_DATABASE_URL` after sourcing it. This procedure was followed with a stub provider and a fake key: `GET /api/v1/managers` listed `ai_llm` as available with the configured model, and the server log named the model but never the key or the URL.

```
cp .env.example .env     # then edit LLM_API_KEY and LLM_MODEL
cd backend
set -a; source ../.env; set +a
go run ./cmd/parallel-api
```

**Live provider verification: PENDING** (only a local stub provider has been exercised). An out-of-range numeric value stops the server at start-up with an error that names the variable and never prints the key.

Where keys live: only in the API server's environment. The key and endpoint are never sent to the web app, never returned by the API and never logged. The web app talks only to `/api/v1`.

How to verify it is on:

- `GET /api/v1/managers` lists "LLM AI" as available and names the model (never the key or URL). Without configuration it lists LLM AI as unavailable.
- In the studio, the "Challenge the AI manager" dialog offers "LLM AI".
- `GET /api/v1/worlds/{id}` returns `llmAvailable`, `llmModel`, `llmCallsMade`, `llmCallsRemaining`, `llmTokensUsed` and `llmTokensRemaining`.

Failures and budgets: at most one provider call per decision window per branch. If the call times out, returns an HTTP error, returns invalid JSON, violates the proposal schema, or the call or token budget is exhausted, the rule-based manager takes the window and the decision is labelled "Rule-based AI (LLM fallback: <reason>)" (`timeout`, `http_error`, `invalid_json`, `schema_violation`, `budget_exhausted`). Proposals pass the same validator as human commands. The rationale is shown as escaped plain text, labelled "AI rationale (not verified)".

Replay never calls the provider: accepted LLM proposals are recorded as inputs, and replay and verification use only those. With no LLM variables set, everything else works (`TestNoLLMConfiguration`).

Tests use a stub provider only: Go `httptest` servers in `internal/policy/llm` and `internal/app`, and a local OpenAI-compatible stub in the Playwright spec `duel-llm-stub.spec.ts` (fake key and model).

**Live provider verification: PENDING — no runtime API key was configured during development.**

### Known limitations

- No authentication; worlds are protected only by unguessable IDs.
- Assumptions are synthetic and uncalibrated.
- Results are not predictions or guarantees.
- The LLM manager has only been tested against a stub provider (see below). While it waits for a provider response it holds the service lock, so other requests wait up to the request timeout.
- The Branch tree shows manager kind only, without AI provenance labels.
- The studio is browser-tested with Playwright in headless Chromium only (see `docs/ux-redesign.md`, Verification results); other browsers and real touch devices are untested.

## First steps

1. Review the official [Kiroween 2026 rules](https://kiroween.devpost.com/rules).
2. Research, compare, and select an original, feasible product concept.
3. **Get explicit approval of the chosen concept before generating code, product-specific steering, architecture, or specs.**
4. Clone this repository and open it in Kiro IDE or Kiro CLI.
5. After approval, use **Kiro** to create and refine steering, specs, design, tasks, and implementation. Record genuine usage in [the Kiro build log](docs/kiro-build-log.md).
6. Run tests, deploy the functional demo, record a video under three minutes, and submit on Devpost.

## Repo setup

- Public GitHub repository
- [MIT License](LICENSE)
- [Hackathon-specific Kiro steering](.kiro/steering/hackathon.md)
- [Kiro build log](docs/kiro-build-log.md)

This is **planning scaffolding** created before the first Kiro build session. It is not evidence that the application has already been developed with Kiro.

## Hackathon checklist

- [x] Public GitHub repository
- [x] Open-source LICENSE
- [x] Root `.kiro/` directory
- [x] Select and approve a product concept
- [ ] Develop working software using Kiro during the eligible submission period
- [ ] Demonstrate and document actual Kiro workflows
- [ ] Deploy and test a public application
- [ ] Publish a public demo video and complete Devpost submission

## License

MIT, Copyright (c) 2026 Savi Saluwadana.
