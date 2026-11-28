<div align="center">

# Parallel

**Reality has no undo button. We built one.**

A playable, forkable simulation of an e-commerce business in a Black Friday crisis.
Rewind to any hour, fork an alternative reality, and compete against an AI manager under identical conditions.

[![CI](https://github.com/savisaluwadana/KiroFrankenstein/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/savisaluwadana/KiroFrankenstein/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](backend/go.mod)
[![Next.js](https://img.shields.io/badge/Next.js-16-black?logo=nextdotjs)](web/package.json)

![The studio at the first crisis: an isometric warehouse with alerts for a demand surge, a stock-out and a packing bottleneck](docs/screenshots/desktop-demo-path-04-crisis.png)

</div>

> Synthetic simulation output under the stated assumptions. Not a forecast or guarantee of real-world results.

Built for the **Kiroween 2026** hackathon, **Frankenstein** category, with Kiro. See [Built with Kiro](#built-with-kiro).

## Contents

[What Parallel is](#what-parallel-is) · [The problem](#the-problem) · [Walkthrough](#product-walkthrough) · [How it works](#how-it-works) · [Quick start](#quick-start) · [Configuration](#configuring-the-ai-manager-openrouter) · [Deployment](#deployment) · [Testing](#testing-and-verification-status) · [Benchmarks](#benchmarks) · [Limitations](#known-limitations) · [Status](#project-status) · [Repository layout](#repository-structure) · [Credits](#acknowledgements)

## What Parallel is

Parallel runs a small online shop for five simulated days (120 hours): one warehouse, four products, customers who place orders, suppliers who deliver, and staff who pack parcels. You play the manager. Every hour the simulation computes what happened: orders, stock, packing, cash, wages.

Three things make it different from a spreadsheet or a game with scripted outcomes:

- **Rewind and fork.** Rewinding is only a read of the past. To change the past, you fork: a new reality that shares history up to a chosen hour, then goes its own way. The original is never altered.
- **Everything is calculated.** Profit, service level and backlog are sums over a double-entry ledger. Click a profit figure and you see the ledger lines behind it.
- **A fair duel.** You and an AI manager each run a reality from the same hour. Customers, supplier failures and staff shortages arrive identically in both. You both see the same information and follow the same rules.

The flagship scenario, **Black Friday Meltdown**, has a demand surge, a supplier failure and a staff shortage. Nothing in it is scripted to favour anyone: the results you see come from running the model.

## The problem

Operations teams, students and people evaluating AI agents often ask "what happens if I do X now?" about a system where today's decision compounds over days. The common tools answer it only partly:

| Approach | What it does well | Where it falls short for this question |
|---|---|---|
| Spreadsheets | Flexible, familiar | Steady-state arithmetic. Queues, stock-outs and delays that feed back on each other are hard to express, and a changed cell has no recorded history to rewind or fork |
| Static case studies | Good for teaching concepts | One fixed story, no way to try a different decision or see its consequences |
| Forecasting dashboards | Summarise what happened or what a model expects | They report numbers, they don't let you act hour by hour and see consequences accumulate |
| A language model asked "what if?" | Fluent, quick | Produces plausible text, not a computed result. The same question can give different answers, and nothing is auditable |

Parallel is an executable model: the same inputs always produce the same outcome, every figure traces to a ledger line, and an AI manager is held to the same rules as a human. The language model, where used, only proposes decisions. It never computes results.

This is a teaching and exploration tool built on synthetic assumptions. It does not predict real businesses.

## Product walkthrough

The demo path below is the one recorded in [docs/demo-script.md](docs/demo-script.md). Figures in captions were observed in a real run (seed 2026, engine version 2) and change if assumptions change.

### 1. Landing

Start the shift from a short introduction. A mobile layout is supported.

![Landing page](docs/screenshots/desktop-demo-path-01-landing.png)

### 2. Set up the business (optional)

Every business assumption can be adjusted within documented bounds. Overrides are recorded with the world.

![Setup page with custom assumptions](docs/screenshots/desktop-demo-path-02-setup-custom.png)

### 3. Start the world

The warehouse runs hour by hour. The HUD shows time, cash, profit so far, on-time share and waiting orders. Every figure comes from the server's state for the displayed hour.

![Studio at the start of the run](docs/screenshots/desktop-demo-path-03-start.png)

### 4. The crisis

Demand surges, Speakers run out and the packing line cannot keep up. Alert cards say what happened, why, and what is affected. At this hour (Saturday 12:00 AM) profit is $50,757.48 with 527 orders waiting.

![Crisis alerts in the studio](docs/screenshots/desktop-demo-path-04-crisis.png)

### 5. Decide

At a decision window you choose up to five management moves (order stock, hire temporary workers, and others). Moves take effect one hour later.

![Decision card](docs/screenshots/desktop-demo-path-05-decision.png)

### 6. Fork a reality

Rewind to Friday 4:00 PM and choose "Explore another future". History before that hour stays identical; the new reality diverges after it.

![Fork sequence](docs/screenshots/desktop-demo-path-06-fork.png)

### 7. Compare futures

Two realities side by side at the same hour, with the observed differences and a "What changed?" inspector. Observed differences are not claims of cause.

![Original path next to the alternative future, with observed differences](docs/screenshots/desktop-demo-path-07-compare.png)

### 8. Duel the AI manager

Same customers, same surprises, same rules. The AI's decisions are listed with its rationale, labelled "AI rationale (not verified)".

![Human vs AI duel](docs/screenshots/desktop-demo-path-08-duel.png)

### 9. Results and the ledger

The scoreboard shows a 100-point composite next to the raw metrics. Every money figure opens the ledger lines that sum to it.

![Duel results](docs/screenshots/desktop-demo-path-09-results.png)
![Ledger drill-down](docs/screenshots/desktop-demo-path-09b-ledger.png)

In the recorded run, a deliberately minimal human plan (two moves, then passive) scored 26.53 and the rule-based AI scored 56.83. That is one observed run of one plan against one policy. It does not show that AI managers are better in general.

### On a phone

<p>
<img src="docs/screenshots/mobile-demo-path-03-start.png" alt="Studio on a phone at the start of the run" width="240">
<img src="docs/screenshots/mobile-demo-path-05-decision.png" alt="Decision card on a phone" width="240">
<img src="docs/screenshots/mobile-demo-path-09-results.png" alt="Duel results on a phone" width="240">
</p>

Engineering Mode (a toggle in the studio) reveals ticks, state hashes and other technical detail that the default interface hides: [screenshot](docs/screenshots/desktop-10-engineering-mode.png).

## How it works

### In plain terms

1. A **scenario** describes the business: products, prices, suppliers, staff, and a script of surprises. A **seed** fixes the random numbers.
2. The **engine** advances the business one simulated hour at a time and records everything in a ledger.
3. Your decisions and any injected surprises are **inputs** appended to a log. The log, plus the scenario and seed, is the only thing that is stored as truth. All state is rebuilt from it.
4. A **fork** is a new branch that reads its parent's inputs up to the fork hour and then has its own.
5. The same scenario, seed and inputs always give the same **state hash**, so a result can be re-computed and checked.

### System architecture

A modular monolith: one Go API binary, one CLI, one Next.js app, one PostgreSQL database.

```mermaid
flowchart LR
  subgraph Browser
    UI["Next.js app<br/>React, Zustand, React Flow"]
    PIXI["PixiJS studio scene"]
    UI --- PIXI
  end
  subgraph Server["parallel-api (Go, Chi)"]
    HTTP["httpapi<br/>REST /api/v1 + SSE"]
    APP["app<br/>worlds, branches, duels, diffs"]
    SIM["sim<br/>pure engine"]
    POL["policy<br/>rule-based and LLM adapter"]
    HTTP --> APP --> SIM
    APP --> POL
  end
  CLI["parallel-sim CLI<br/>run, verify, ensemble, calibrate, llm-check"]
  DB[("PostgreSQL 16<br/>pgx, Goose")]
  LLM["Optional LLM provider<br/>OpenAI-compatible"]
  UI -- "JSON, SSE" --> HTTP
  APP -- "ports" --> DB
  POL -. "optional, server side only" .-> LLM
  CLI --> SIM
  CLI --> POL
```

<details>
<summary>Plain ASCII version of the diagram</summary>

```
Browser (Next.js, PixiJS) --JSON/SSE--> parallel-api (Go, Chi)
                                         httpapi -> app -> sim (pure engine)
                                                     |  \-> policy (rule-based, LLM adapter) ..> LLM provider (optional)
                                                     \-> PostgreSQL 16 (pgx, Goose)
parallel-sim CLI ------------------------------------> sim, policy   (no network needed)
```
</details>

The web app only talks to `/api/v1` and never computes results. In production the web app is hosted on Vercel and the API in a container ([deployment](#deployment)).

More detail, including package dependencies, sequence and data model diagrams: [docs/architecture.md](docs/architecture.md).

### The simulation engine

The engine (`backend/internal/sim`) is a pure Go package using only an allowlist of standard library packages. An architecture test fails if it imports anything else (no `time`, `math/rand`, `os`, `net`, `sync`).

- **Hybrid discrete-event time.** An event queue on an integer sim-minute clock, ordered by `(At, Priority, Seq)`. Hourly ticks are the only boundary for observations, hashes, snapshots, metrics and decision windows. Recurring hourly processes are events at fixed minute offsets (demand at minute 5, fulfilment 30, cancellations 55, accruals 58 of each tick).
- **Integer arithmetic only.** Money is `int64` cents; rates are integer basis points. There is no floating point in state transitions, because fused multiply-add can change results between CPU architectures. Division rounds half to even through one helper.
- **Own random streams.** A SplitMix64 generator implemented in the repo, with independent named streams (`demand`, `disruption`, `supplier`, `workforce`) derived from the seed. A decision in one area never shifts draws in another, and exogenous streams draw a fixed number of values per tick regardless of decisions.
- **No map-order dependence.** Anything that affects output iterates slices or sorted keys.
- **State hash per tick.** SHA-256 over a canonical, fixed-field-order encoding of the simulation state. Identifiers, labels, manager kind, timestamps, rationale and the engine version string are excluded.
- **Engine version.** Any change to offsets, priorities, draw counts or the state encoding requires a new engine version (currently 2). Replays across versions are flagged, not silently compared.

### Event sourcing and reality branching

```mermaid
flowchart LR
  subgraph Trunk["Original path (branch 1)"]
    T0["tick 0"] --> T40["tick 40<br/>fork point"] --> T120["tick 120"]
  end
  T40 --> C40["tick 40<br/>same hash"] --> C120["tick 120<br/>different inputs, different hash"]
```

- The **input log** (append-only; a database trigger rejects UPDATE, DELETE and TRUNCATE) holds commands and injected disruptions with their branch, effective tick and manager provenance. The world's seed and scenario are stored once.
- A **fork** creates a child branch that references its parent. The child reads the parent's inputs only up to the fork tick; later parent inputs are never visible to it, and the reverse.
- **Rewind** is a read of a past tick. It never changes anything.
- **Snapshots** are caches of full engine state (including the event queue, id counters and random stream states). A snapshot is used only if its stored hash equals the hash of the replayed state at that tick. Any state can be rebuilt by replay from tick 0.
- **Golden hashes** pin the flagship run, the demo fork, the duel branches and a 20-seed ensemble report. They are checked on arm64 and on amd64 in CI, and locally under linux/amd64 emulation.

### Financial model

A double-entry ledger with a fixed chart of **10 accounts**: cash, inventory, opening equity, revenue, cost of goods sold, labour, freight, marketing and overhead expense, and service-credit expense. Every entry balances.

- Inventory is valued at **per-SKU moving weighted average cost** in integer cents. Shipping the last units takes the full remaining value; otherwise value × quantity ÷ units, rounded half to even.
- **Revenue and payment are recognised at shipment.** Cancelled orders create no ledger entry. Wages are paid in the hour earned. There are no receivables, payables, tax or post-horizon simulation.
- Metrics live in one pure calculator: **net profit**, **contribution**, **net cash flow**, **service level** (share of due orders shipped on time), **fill rate**, and **backlog**.
- Identities are tested as properties: debits equal credits every tick; cash equals opening cash plus cash entries; inventory balance equals the sum of per-SKU values; cash plus inventory equals opening equity plus net profit.

### AI manager architecture

> The AI proposes decisions. Deterministic software executes them.

```mermaid
flowchart LR
  OBS["Observation<br/>(same fields a human sees)"] --> MGR{"Manager"}
  MGR -->|"rule-based"| RB["Reorder-point and capacity rules<br/>pure, deterministic"]
  MGR -->|"optional"| LLM["LLM adapter<br/>OpenAI-compatible endpoint"]
  RB --> PROP["Proposal<br/>(commands + rationale)"]
  LLM --> PROP
  LLM -. "timeout, http error, bad JSON,<br/>schema violation, budget" .-> RB
  PROP --> VAL["Deterministic validator<br/>5-command budget, cash and stock rules"]
  VAL --> LOG[("Input log<br/>with provenance")]
  LOG --> ENG["Engine"]
```

- Both managers see an **Observation** and return a proposal. A proposal goes through the **same validator and the same 5-command budget** as a human's commands. The AI cannot touch state directly.
- The **rule-based manager** (`internal/policy/rulebased`) is an operations-research baseline: reorder points and capacity rules. It needs no network and no configuration.
- The optional **LLM manager** (`internal/policy/llm`) calls an OpenAI-compatible chat completions endpoint. OpenRouter is the initial documented provider. The model id is always configured through `LLM_MODEL`; none is built in, so the LLM opponent is unavailable until `LLM_BASE_URL`, `LLM_API_KEY` and `LLM_MODEL` are all set. Output is parsed as strict JSON; the rationale is shown as escaped text labelled "AI rationale (not verified)".
- **Fallback.** If the call times out, returns an HTTP error, invalid JSON, a schema violation, or a budget is exhausted, the rule-based manager takes the window. The decision is recorded with provenance `ai_rule_based_fallback` and the reason. Other provenance values are `human`, `ai_rule_based`, `ai_llm`.
- **Limits.** At most one provider call per window per branch, 40 per world, 2000 calls and 5,000,000 tokens per UTC day server-wide, 4 requests in flight, a 32 KiB prompt cap, and a token reservation before each call. See [the table below](#configuring-the-ai-manager-openrouter).
- **Replay never calls the provider.** Accepted proposals are recorded as inputs; replay uses only those. Each window also stores a decision record (proposed commands, accepted count, rejected commands with reasons, provenance, model, fallback reason, token usage).
- The provider call runs outside the service lock, one in flight per window. When the result returns, the service revalidates that the branch has not changed and discards the answer (HTTP 409, problem `decision-stale`) if it has.

### Human vs AI fairness

| Constraint | How it is enforced |
|---|---|
| Same information | Both managers receive the same Observation fields |
| Same rules | Same decision-window ticks, same command budget (5), same lead times, same validator |
| Same world | Customer arrivals (with their random draws) and disruptions are identical in both realities. Decisions only change how arrivals convert to orders (common random numbers) |
| Lockstep | Both branches advance together. The AI decides when its window opens, but its decision is hidden until the human ends their window |
| Scoring independent of the opponent | Composite score in hundredths of a point (0 to 10000): 50% financial, 30% fill rate, 20% on-time service level (weights in basis points). The financial part is normalised against the scenario's profit floor and target, never against the other manager |
| Ties | Scores within 10 hundredths (0.1 points) are a tie |

Raw metrics are always shown next to the score. Property tests cover parity and exogenous fairness.

### Multi-seed experiments

`parallel-sim ensemble` runs the same scenario over many seeds for two or more arms: a **fixed command replay** (a recorded plan that cannot adapt) and an **adaptive policy** (the rule-based manager). Seeds are paired, so each seed compares arms under identical randomness.

- Invalid replayed commands are skipped with a reason code and never altered.
- Reports are deterministic JSON and CSV, byte-identical for identical invocations.
- Capped at 50 seeds per invocation. Ensembles never call a paid LLM or the network.
- The committed 20-seed flagship report (`backend/scenarios/golden/ensemble-flagship-20.{json,csv}`) is reproduced byte for byte in tests. **Observed** in that report: the fixed plan has a median composite score of 26.60 and the rule-based policy 61.46; the policy scored higher in 20 of 20 seeds. The fixed plan cannot react and the policy can, so this is not a claim about real warehouses or about AI in general.

### Branch diff inspector

"What changed?" (`POST /api/v1/diffs`) compares two branches of one world at one tick: the first hour their state hashes differ, which inputs exist in only one branch, and the differences in finances, service, backlog and stock. It reports **observed differences** only. To attribute an effect to a decision, fork and change only that decision.

### Security model

No accounts. Three separate credentials (details in [docs/security-model.md](docs/security-model.md)):

| Credential | Grants |
|---|---|
| World, branch or duel ID (random UUIDv4) | **Read** access. A link is a read-only copy |
| Write token (256-bit, returned once on creation, stored server-side only as a SHA-256 hash) | **Changing** that world: advance, commands, fork, surprises |
| Optional LLM access code (`LLM_ACCESS_CODE`) | Starting an LLM-backed opponent |

Other controls: strict JSON decoding, 256 KiB bodies, per-IP rate and creation limits, an append-only input log enforced by database triggers, and a runtime database role without DDL rights. The app has no hard monetary spending cap, only call and token limits; set a credit limit on the provider key.

## Quick start

### Prerequisites

- Go 1.25+ (`backend/go.mod` requires 1.25.0)
- Node.js 22 (the version CI uses). The fresh-clone check for this README ran on Node 25.2.1 and Go 1.25.5.
- Docker, optional: only for PostgreSQL and the container image.

Ports used: API **8080**, web app **3100**. Ports 3000 and 8000 are avoided because other tools commonly use them. Change the API port with `PORT`, and keep `WEB_ORIGIN` equal to the web app's exact origin.

### Run with the in-memory store

Worlds are kept in memory and lost when the API stops.

```sh
# terminal 1
cd backend && WEB_ORIGIN=http://localhost:3100 go run ./cmd/parallel-api

# terminal 2
cd web && npm ci && npm run dev
```

Open <http://localhost:3100> and choose to begin your shift. Check the API at <http://localhost:8080/api/v1/health> (returns `{"status":"ok","engineVersion":"2"}`).

For a production build instead of the dev server: `cd web && npm run build && npx next start -p 3100`. `NEXT_PUBLIC_API_BASE_URL` (default `http://localhost:8080/api/v1`) is read at **build** time, so set it before `npm run build` if the API is elsewhere.

Create a demo world from the command line: `curl -X POST localhost:8080/api/v1/demo -H 'Content-Type: application/json' -d '{}'`.

### Headless CLI

```sh
cd backend
go run ./cmd/parallel-sim run --scenario scenarios/black-friday.json --seed 2026
go run ./cmd/parallel-sim verify --scenario scenarios/black-friday.json --seed 2026 \
  --inputs scenarios/demo-fork-inputs.json --expect $(cat scenarios/golden/demo-fork.hash)
go run ./cmd/parallel-sim ensemble --scenario scenarios/black-friday.json --seeds 1..3 \
  --arm fixed:scenarios/demo-duel-human.json --arm rulebased --out /tmp/ens
```

Expected output: `run` prints engine version 2, tick 120, state hash `f6f414259c1cbbc7f76dd0c0d2b129cc34d6e98e906868dfea7d68787fffac80`, net profit 1778400 cents, service level 4427 bps. `verify` prints state hash `9786f539...daca8` and `MATCH`. `ensemble` prints a per-seed table and writes `/tmp/ens.json` and `/tmp/ens.csv`. The calibration tables: `go run ./cmd/parallel-sim calibrate --scenario scenarios/black-friday.json --seeds 1..20` ([docs/calibration.md](docs/calibration.md)).

### Run with PostgreSQL (Docker Compose)

Without `DATABASE_URL` the API uses memory. With it, worlds persist and a restarted API rebuilds any branch by replay.

```sh
DB_PORT=55432 docker compose up -d --wait db      # DB_PORT defaults to 5432
cd backend
MIGRATE_DATABASE_URL='postgres://parallel_migrator:migrator_dev_password@127.0.0.1:55432/parallel?sslmode=disable' \
  go run ./cmd/parallel-api migrate up
DATABASE_URL='postgres://parallel_app:app_dev_password@127.0.0.1:55432/parallel?sslmode=disable' \
  WEB_ORIGIN=http://localhost:3100 go run ./cmd/parallel-api
```

- The compose file mounts `docker/initdb/`, which creates the roles `parallel_migrator` (owner) and `parallel_app` (runtime: no DDL, no UPDATE or DELETE on the input log) and the databases `parallel` and `parallel_test` on first start. The passwords are development-only values.
- Pick another `DB_PORT` if you already run PostgreSQL on 5432 (a Homebrew PostgreSQL can shadow the container). To keep a second copy separate, use `docker compose -p <name>`.
- If the schema is missing the API exits and tells you to run `migrate up`. `GET /readyz` checks the database.

### Docker image (API)

```sh
docker build -t parallel-api ./backend
scripts/check-image-secrets.sh parallel-api      # fails if secrets or model ids are baked in
docker run --rm -p 8080:8080 -e WEB_ORIGIN=http://localhost:3100 parallel-api
```

The image is a static binary on distroless, running as non-root, with migrations and the flagship scenario embedded. Without `DATABASE_URL` it uses the in-memory store. Run migrations with `docker run --rm -e MIGRATE_DATABASE_URL=... parallel-api migrate up`.

### Configuring the AI manager (OpenRouter)

The rule-based AI needs no configuration. The LLM AI is optional and appears only when all three of `LLM_BASE_URL`, `LLM_API_KEY` and `LLM_MODEL` are set. Pick a model id from the provider's catalogue (OpenRouter: <https://openrouter.ai/models>); the project hardcodes none. Providers bill usage; it is separate from Kiro credits.

```sh
cp .env.example .env        # then set LLM_API_KEY and LLM_MODEL
```

`.env.example` also contains `DATABASE_URL` and `MIGRATE_DATABASE_URL`. If `DATABASE_URL` is set the API requires that PostgreSQL. To run in memory, delete those two lines, or `unset DATABASE_URL MIGRATE_DATABASE_URL` after loading the file. The API reads the process environment only; it does not load `.env` itself:

```sh
cd backend
set -a; source ../.env; set +a
go run ./cmd/parallel-api
```

Never commit `.env` or a key. The key and endpoint stay on the server: they are never sent to the browser, returned by the API or logged.

| Variable | Meaning | Default and bounds |
|---|---|---|
| `LLM_BASE_URL` | OpenAI-compatible base URL | `https://openrouter.ai/api/v1` in `.env.example` |
| `LLM_API_KEY` | Provider API key | none |
| `LLM_MODEL` | Model id from the provider catalogue | none (required) |
| `LLM_REQUEST_TIMEOUT_SECONDS` | Real-time limit per call | 20 (1..120) |
| `LLM_MAX_CALLS_PER_WORLD` | Provider calls per world | 40 (1..200) |
| `LLM_MAX_TOKENS_PER_CALL` | Sent as `max_tokens` | 1024 (64..8192) |
| `LLM_MAX_TOTAL_TOKENS_PER_WORLD` | Reported tokens per world | 200000 (1000..5000000) |
| `LLM_MAX_PROMPT_BYTES` | Largest prompt sent | 32768 (1024..262144) |
| `LLM_GLOBAL_MAX_CALLS` / `LLM_GLOBAL_MAX_TOKENS` | Server-wide, per UTC day | 2000 / 5000000 |
| `LLM_MAX_CONCURRENT_REQUESTS` | Provider requests in flight | 4 (1..64) |
| `LLM_ACCESS_CODE` | Optional secret needed to start an LLM opponent (header `X-Parallel-LLM-Code`) | unset |
| `LLM_DUELS_PER_DAY_PER_IP` | LLM-backed starts per client IP per 24 h | 5 (1..1000) |

Before each call the server reserves an estimate (prompt bytes ÷ 3, rounded up, plus `max_tokens`) against the world and global budgets, then settles with provider-reported usage. If the reservation does not fit, no request is made and the window falls back with `budget_exhausted`. Global usage is rebuilt from stored decision records after a restart.

Verify a provider with the cheap check (at most one paid request, only with the confirm flag):

```sh
cd backend && set -a; source ../.env; set +a
go run ./cmd/parallel-sim llm-check                       # config and model listing only
go run ./cmd/parallel-sim llm-check --confirm-paid-call   # one real completion
```

Without credentials it prints `LIVE VERIFICATION PENDING` and exits 4. Also check `GET /api/v1/managers`, which lists the LLM AI as available and names the model (never the key).

> **Live provider verification: PENDING.** No provider credentials were available during development. Every LLM code path is tested against stub providers only. See [docs/live-llm-verification.md](docs/live-llm-verification.md).

## Deployment

The intended shape: the Next.js app on Vercel, the API image on a container host that keeps one long-lived process (SSE streams), and managed PostgreSQL 16 with TLS and backups. The browser calls the API origin directly, so the API's `WEB_ORIGIN` must equal the exact web URL. Migrations run as a separate release step with the owner role, never at API start. The full procedure, variables, probes and rollback are in [docs/deploy.md](docs/deploy.md); templates (untested) are in [docs/deploy-templates/](docs/deploy-templates/README.md).

| | Status |
|---|---|
| Image builds, passes the secrets check, runs and passes its health check | Verified locally |
| `scripts/smoke-prod.sh` against a local container with PostgreSQL | Verified locally |
| Web build with an https API origin and its CSP | Verified locally |
| A real hosting platform, TLS and HSTS through a real proxy, a Vercel build, managed PostgreSQL | **Not verified** |
| Public deployment | **Not deployed** |

## Testing and verification status

Commands:

```sh
# Go (PostgreSQL tests need PARALLEL_TEST_DATABASE_URL, see docs/ci.md and below)
cd backend && go vet ./... && go test ./... -count=1
cd backend && go test -race ./internal/app ./internal/httpapi ./internal/policy/... -run 'TestLLM|TestAI|TestDuel|TestConcurrent|TestNoLLMConfiguration' -count=1

# Goldens (also run under linux/amd64 in CI)
cd backend && go test ./internal/sim -run TestGoldenFlagshipHash -count=1
cd backend && go test ./internal/app -run TestGoldenDuelHashes -count=1
cd backend && go test ./internal/app/ensemble -run TestGoldenEnsembleReport -count=1

# Web
cd web && npx tsc --noEmit && npx eslint . && npx vitest --run
cd web && npx playwright install chromium && npm run e2e      # starts its own API and web servers on ports 18080 and 3199

# Everything CI runs, locally
scripts/ci-local.sh        # see docs/ci.md for flags
```

PostgreSQL store tests run only when `PARALLEL_TEST_DATABASE_URL` (runtime role) is set; `TestInputLogImmutable` also needs `PARALLEL_TEST_MIGRATE_DATABASE_URL` (owner role). Both point at the `parallel_test` database created by the compose bootstrap, after `migrate up` against it.

Latest recorded results:

| Check | Result | Date |
|---|---|---|
| Go tests including PostgreSQL, 0 skips | pass | 2026-11-28 (CI, below; and locally in M7 checkpoint) |
| Go race tests (concurrency-sensitive packages) | pass | 2026-11-28 |
| Golden hashes: flagship, demo duel, 20-seed ensemble report | pass natively on arm64 and under linux/amd64 emulation in Docker; pass on both CI runners | 2026-11-28 |
| Web: vitest | **275 tests** pass | 2026-11-28 |
| Web: Playwright (Chromium; desktop 1440x900 and phone 390x844) | **24 passed, 10 skipped**, 0 failed. The skips are per-project guards (desktop-only specs skip on the phone project) | 2026-11-28 |
| `govulncheck`, `staticcheck`, `npm audit --audit-level=high` | clean at the M7 checkpoint | 2026-11-28 |
| Fresh clone, following this README | see below | 2026-11-28 |

**GitHub Actions.** Last fully green runs: 46065036957 (commit `cbd0d68`) and 46100197654 (commit `d4f140b`), each with all six jobs successful (go amd64, go arm64, openapi lint, web, docker build, e2e). The first GitHub run, 46060356207 (`6636bee`), failed in `web` because the npm lockfile was out of sync; that was fixed in `cbd0d68`. Later runs 46106909199 (`2ce9c05`) and 46125120786 (`d387c6d`) were cancelled in the `e2e` job (the other five jobs succeeded); the run for `e666480` (46126448247) was still in progress when this was written. Check the badge above for current status. The CI badge reflects the latest run on `main`, which may be in progress or failing. [docs/ci.md](docs/ci.md) lists the jobs.

**Fresh-clone check.** On 2026-11-28 a fresh `git clone` (no `node_modules`, no `.env`) was used to run: `go build ./cmd/...`; the API with the in-memory store (`/api/v1/health`, `POST /api/v1/demo`); `npm ci`, `npm run build`, `npx next start` (`GET /` and `/briefing` returned 200, the CSP allowed the API origin, CORS allowed the web origin); Docker Compose PostgreSQL with a separate project name and `DB_PORT`, `migrate up` to version 7, the API on that database, `/readyz` ready, a demo world created; `docker build`, the secrets check, `docker run` (health check `healthy`); and the CLI commands `run`, `verify` (MATCH) and `ensemble --seeds 1..3`. Not run in that check: `npm run dev`, the Playwright suite, a browser session.

Notable test types: determinism and replay-equivalence properties (`pgregory.net/rapid`), fork independence and fidelity, ledger and inventory identities, duel parity and exogenous fairness, an architecture test on `internal/sim` imports, and a database test that the input log rejects UPDATE, DELETE and TRUNCATE even for the table owner.

## Benchmarks

Method and raw output: [docs/benchmarks.md](docs/benchmarks.md). Hardware: MacBook Pro, **Apple M5** (10 cores), 16 GB, macOS 26.2, Go 1.25.5. This is a laptop, **not the demo host**. Budgets are targets until measured on the host that serves the demo. Correctness checks stayed enabled in every measured run. Results are for synthetic simulation.

| Measure | Budget | Measured |
|---|---|---|
| Flagship replay, tick 0 to 120 (in-process engine loop) | 500 ms | median 93 ms after the engine v2 change; 102 to 105 ms after the flagship calibration |
| 50-seed paired ensemble (100 full replays plus policy) | 60 s | median 3.27 s at the default concurrency (10); 13.0 s at concurrency 1 |
| Cold 120-frame `tick-frames` read | 1 s | 115 ms median after a fix (1881 ms before; the earlier miss is kept on record) |
| State read, fork, diff, advance to horizon | 300 ms to 1 s | all within budget on the laptop (fork 1.7 ms, diff 242 ms cold, advance to horizon about 317 ms) |

The in-process replay figure excludes process start-up and JSON output. Each replay allocates about 550 MB; it was not profiled because the budget is met.

## Known limitations

- **No authentication beyond the capability model.** An ID is a read link; a write token is held in the creating tab's `sessionStorage` (losing the tab loses write access). IDs can appear in browser history and proxy logs.
- **Synthetic, uncalibrated assumptions.** Results are not predictions or guarantees.
- **The rule-based AI overspends on labour.** It reaches full service level but schedules overtime and temporary workers for the backlog and never lowers overtime, so it underperforms some human-style plans in the calibration harness. Reported in [docs/calibration.md](docs/calibration.md), not tuned.
- **Supply-limited flagship.** Fixed plans give the same net profit for every seed (for example $17,784 with no commands, $21,611.61 for the demo plan); only service level and score vary by seed.
- **The fork transition runs 1.5 s**, against a 600 ms requirement. Accepted deviation. The **duel result reveal animation was not built**.
- **Live LLM verification is pending.** Only stub providers have been used.
- **Mobile layout.** On a phone the bottom sheet hides most of the warehouse and one label is clipped.
- **Rate limits and counters are in memory**, per process. They reset on restart and are not shared between replicas. `MAX_WORLDS` applies only to the memory store.
- **LLM spending** has no monetary cap in the app, and a provider may bill more than it reports.
- **Connection poolers.** `pgxpool` caches prepared statements; transaction-mode poolers may break that and were not tested. Use a direct connection string ([docs/deploy.md](docs/deploy.md)).
- **CI vs local.** Local runs were on one arm64 Mac; GitHub results are summarised above. Browser testing is headless Chromium only; other browsers and real touch devices are untested.
- **Engine version rule.** Changing offsets, priorities, draw counts or the state encoding needs a new engine version and regenerated goldens ([CONTRIBUTING.md](CONTRIBUTING.md)).

## Project status

The specification milestones M0 to M7 are implemented and tested: the vertical slice, the full engine and ledger, PostgreSQL persistence with snapshots and replay, the studio with SSE, the rule-based and LLM managers and the duel, the branch diff, ensembles and calibration, and production hardening. This documentation pass is M8. The repository is under a feature freeze. Not done: a public deployment, the demo video, the Devpost submission, live LLM verification, the duel reveal animation. Nothing beyond what is in [the task list](.kiro/specs/parallel-simulation-mvp/tasks.md) is promised; stretch items there (for example read-only share links as a separate capability, extra disruption kinds) are not built unless the task list says so.

### Hackathon checklist

- [x] Public GitHub repository
- [x] Open-source LICENSE (MIT)
- [x] Root `.kiro/` directory, tracked
- [x] Select and approve a product concept
- [x] Working software, verified locally (see above)
- [x] Actual Kiro workflows documented in [the build log](docs/kiro-build-log.md)
- [ ] Deployed and tested public application (not deployed)
- [ ] Public demo video under three minutes (not recorded)
- [ ] Devpost submission (not submitted)

### Built with Kiro

The steering files (`.kiro/steering/`), requirements, design and task list (`.kiro/specs/parallel-simulation-mvp/`) were produced with Kiro's spec workflow, and the implementation was executed task by task with Kiro agents from that list. The [Kiro build log](docs/kiro-build-log.md) records each session, including what was reviewed or corrected, test results and known issues. For example, the property test for snapshot resume found a real engine bug (a state clone that aliased reference data), which was fixed. The initial planning scaffolding before the first logged session is not claimed as Kiro output. Kiro credit use was not measured. Further evidence: [Kiro usage evidence](docs/hackathon/kiro-evidence.md).

## Repository structure

```
.
├── .kiro/                    steering and the spec (requirements, design, tasks)
├── .github/workflows/ci.yml  CI: go (amd64, arm64), openapi lint, web, docker, e2e
├── backend/                  Go module
│   ├── cmd/parallel-api/     HTTP server (wiring, config, migrate, healthcheck)
│   ├── cmd/parallel-sim/     CLI: run, verify, ensemble, calibrate, llm-check
│   ├── api/openapi.yaml      API contract (source of truth)
│   ├── internal/sim/         pure engine, ledger, metrics, score, hash
│   ├── internal/policy/      rule-based manager, LLM adapter
│   ├── internal/app/         use cases: worlds, branches, duels, diffs, ensemble, calibration
│   ├── internal/store/       memory and PostgreSQL repositories
│   ├── internal/httpapi/     Chi routes, SSE, problem+json
│   ├── migrations/           Goose SQL (00001 to 00007)
│   └── scenarios/            black-friday.json, demo inputs, golden files
├── web/                      Next.js app: app/, features/, stores/, lib/api/, e2e/
├── docker/initdb/            development database bootstrap
├── docs/                     architecture, deploy, security, benchmarks, calibration, ci, demo script, build log, screenshots
├── scripts/                  ci-local, smoke-prod, check-image-secrets, demo-numbers, check-docs
├── docker-compose.yml        PostgreSQL only
└── LICENSE                   MIT
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).

## License

MIT. Copyright (c) 2026 Savi Saluwadana. See [LICENSE](LICENSE).

## Acknowledgements

- Fonts, bundled unmodified under the SIL Open Font License 1.1 from Fontsource: Fraunces, Inter and IBM Plex Mono. Licence texts: [web/app/fonts/LICENSES.md](web/app/fonts/LICENSES.md).
- Libraries: [PixiJS](https://pixijs.com) (warehouse scene), [React Flow](https://reactflow.dev) (`@xyflow/react`, branch graph), [Zustand](https://zustand-demo.pmnd.rs), [Next.js](https://nextjs.org), [Tailwind CSS](https://tailwindcss.com), [Lucide](https://lucide.dev), [chi](https://github.com/go-chi/chi), [pgx](https://github.com/jackc/pgx), [goose](https://github.com/pressly/goose), [rapid](https://pgregory.net/rapid) (property-based tests) and [Playwright](https://playwright.dev).
- [OpenRouter](https://openrouter.ai) is the initial documented LLM provider; it is optional.
