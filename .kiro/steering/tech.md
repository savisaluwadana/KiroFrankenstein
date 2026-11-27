---
inclusion: always
---

# Technology steering

## Stack

| Layer | Choice | Notes |
|---|---|---|
| Simulation core | Go 1.25+, standard library only | Pure package. No I/O, no wall clock, no global RNG, no goroutines |
| HTTP API | Go + Chi v5 | JSON REST under `/api/v1`, SSE for run progress |
| Persistence | PostgreSQL 16, pgx v5, Goose migrations | Append-only input log, snapshots, derived projections |
| Frontend | Next.js (App Router) + TypeScript (strict) | Client-rendered simulation views |
| Rendering | PixiJS v8 | Warehouse diorama only |
| UI chrome | Tailwind CSS + shadcn/ui (restyled per `ui-design-system.md`) | |
| Client state | Zustand | View state only; never a source of simulation truth |
| Branch graph | React Flow | Branch tree view |
| Motion | CSS transitions, PixiJS ticker; GSAP optional | Fork transition and duel reveal are priority polish after the MVP; see below |
| Local infra | Docker Compose for PostgreSQL only | No orchestration platforms |

## Decisions and simplifications

- **Architecture:** modular monolith. One Go binary (`parallel-api`) plus one headless CLI (`parallel-sim`). One Next.js app.
- **SSE, not WebSockets.** Server-to-client streaming is one-way (tick progress). Client actions use plain POST. SSE reconnects with `Last-Event-ID` = last tick received. Polling is the documented fallback if a proxy breaks SSE.
- **GSAP is optional.** PixiJS has its own ticker for in-canvas interpolation; CSS transitions cover UI chrome. The fork transition and duel reveal are priority polish built after the MVP passes; they may use CSS, the PixiJS ticker or GSAP. No animation ever displays a value that is not already authoritative (no numeric count-ups).
- **Engine time model: hybrid discrete-event.** An event queue on an integer sim-minute clock ordered by `(sim_minute, priority, sequence)`. Hourly ticks are the only boundary for observations, state hashes, snapshots, metrics and decision windows (every 4 ticks in the flagship). Recurring hourly processes are events at fixed minute offsets per engine version.
- **Ensembles are CLI-only in the MVP.** `parallel-sim ensemble` runs up to 50 seeds per arm, rule-based policy or fixed replay only (no LLM), and writes deterministic JSON and CSV.
- **React Flow is kept** because the branch tree is a core visible feature; replace with a custom SVG only if bundle size or styling becomes a blocker.
- **The LLM is optional.** The AI manager has a deterministic rule-based policy (`internal/policy/rulebased`). An LLM policy is an adapter behind the same interface, using a configurable external provider through an OpenAI-compatible chat completions endpoint (`LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL`). OpenRouter is the initial configured provider; no model identifier is built in, so the LLM opponent is unavailable until all three variables are set. Calls are capped per decision window and per world. Without an API key everything except the LLM opponent works. Runtime LLM usage is billed by that provider and is independent of Kiro development credits. Recorded LLM proposals are inputs, so replay never calls the LLM. Inputs record manager provenance (`human`, `ai_rule_based`, `ai_llm`, `ai_rule_based_fallback`).
- **Playback is presentation only.** The engine computes ticks at its own pace; the web app paces the display of authoritative tick frames (1x, 4x, 16x = 2, 8, 32 ticks per second), plus manual stepping and fast-forward to the next decision window or disruption onset. Playback speed, stepping, fast-forward and pausing never change a state hash.
- **Duel score.** A 100-point composite held as integer hundredths (0 to 10000): financial 50 %, fill rate 30 %, on-time service level 20 % by default (weights in basis points summing to 10000). The financial component is normalized against scenario profit floor and target, never against the opponent. Tie tolerance 10 hundredths (0.1 points). Raw metrics are always shown next to the score.
- **Performance budgets are targets until measured.** Replay and ensemble benchmarks run early; hardware, scenario size and method are recorded in `docs/benchmarks.md`. Never fabricate a measurement or disable correctness checks to meet a target.
- **Ledger scope.** A fixed chart of accounts for this business only; revenue and payment at shipment (cancellations need no refund); per-SKU moving weighted average inventory cost in integer cents; wages paid in the tick earned; no receivables, payables, tax or post-horizon simulation.
- **Vertical slice first.** The slice may use an in-memory repository behind the same ports as the PostgreSQL store; PostgreSQL follows in the MVP core.
- **API contract:** an OpenAPI 3.1 document in `backend/api/openapi.yaml` is the source of truth. TypeScript client types are generated from it. Errors use RFC 9457 `application/problem+json`.

## Determinism rules (Go)

- Money: `int64` minor units (cents). Rates and percentages: integer basis points or fixed-point `int64` with an explicit scale. No `float32`/`float64` in state transitions; Go may fuse multiply-add on arm64, which breaks cross-architecture reproducibility.
- Randomness: a project-owned PRNG (for example PCG or SplitMix64 implemented in the repo) seeded from the world seed. Independent named streams (demand, supplier, workforce) are derived from the seed, so a decision in one area never shifts random draws in another (common random numbers).
- Ordering: never depend on map iteration order. Iterate sorted keys or slices. Event queue order is `(sim_minute, priority, sequence)`.
- Time: sim time is an integer count of simulated minutes. `time.Now()` is forbidden in `internal/sim`.
- Serialization for hashing: canonical encoding with fixed field order; SHA-256 state hash per tick.

## Quality gates

- Go: `go test ./...`, `go vet`, `staticcheck`, `govulncheck`. Property-based tests use `pgregory.net/rapid`.
- Architecture test: a Go test that fails if `internal/sim` imports anything outside an allowlist of standard library packages.
- Frontend: `tsc --noEmit`, ESLint, Vitest (`vitest --run`), Playwright for the demo path.
- Golden test: the recorded Black Friday duel replays to a committed final state hash.
- Golden ensemble test: the committed 20-seed paired ensemble report for the flagship duel is reproduced byte for byte.

## Commands (expected; finalize in design)

- `docker compose up -d db`
- `cd backend && go run ./cmd/parallel-api`
- `cd backend && go run ./cmd/parallel-sim run --scenario scenarios/black-friday.json --seed 2026`
- `cd backend && go run ./cmd/parallel-sim ensemble --scenario scenarios/black-friday.json --seeds 1..20 --arm fixed:scenarios/demo-human.json --arm rulebased` (flags finalized in design)
- `cd web && npm run dev` (run manually in a terminal; long-running)
