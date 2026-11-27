# Implementation Plan: Parallel simulation MVP

## Overview

This plan converts `requirements.md` (revision 4) and `design.md` into small, dependency-ordered coding tasks across milestones M0 to M8, followed by a separate, optional, budget-gated stretch section. Implementation language: Go 1.23+ (backend, standard library only in `internal/sim`) and TypeScript strict (Next.js `web/`), as fixed by `design.md` and `tech.md`.

Credit budget: 4000 Kiro credits in total. Planned milestones total 3500 (M0 150, M1 900, M2 600, M3 350, M4 500, M5 400, M6 350, M7 300, M8 150). The 500-credit emergency reserve is not allocated to any task. Stretch work starts only if credits remain after M8 without touching the reserve.

Build order rules:

- M1 is the vertical slice. It ends with a Next.js studio connected to the real engine over an in-memory store: one configurable business, working simulation, the scripted supplier_outage, rewind, independent forks, authoritative financial metrics and a basic interactive PixiJS view with playback.
- The early benchmark (`docs/benchmarks.md`) runs in M1 as soon as the engine can replay the flagship, before any requirement beyond the slice starts (Req 29.9).
- Flagship calibration (`docs/calibration.md`) runs in M6, after the simulation and financial modules (M1, M2), the Rule_Based_Manager (M5) and the ensemble benchmark exist. Calibration gates apply only to the no-Command run and the Rule_Based_Manager run and are never tuned for AI superiority (Req 36.6).
- Priority polish (Req 38) starts only after the MVP acceptance checkpoint in M7.

Task field conventions: `_Requirements_` lists criterion numbers from `requirements.md`; **Dependencies** lists task IDs that must be complete; **Deliverables** names files; **Verify** gives non-watch commands; **Acceptance** states what must be true. Property tests use `pgregory.net/rapid`, are named `TestProp*`, run at least 100 iterations and carry the tag comment `// Feature: parallel-simulation-mvp, Property N: <text>`. Only sub-tasks marked `*` are optional. Core invariant tests are required.

## Tasks

- [x] 1. M0 — Repository and development environment (~150 credits)
  - [x] 1.1 Create the Go module skeleton
    - _Requirements: project constraints (minimal footprint), 26.1_
    - Dependencies: none
    - Deliverables: `backend/go.mod` (`github.com/savisaluwadana/KiroFrankenstein/backend`, go 1.23) with pinned `github.com/go-chi/chi/v5`, `github.com/jackc/pgx/v5`, `github.com/pressly/goose/v3`, `pgregory.net/rapid`; stub `backend/cmd/parallel-api/main.go`, `backend/cmd/parallel-sim/main.go`; package `doc.go` files for `internal/sim`, `internal/policy/rulebased`, `internal/policy/llm`, `internal/app`, `internal/store/memory`, `internal/store/postgres`, `internal/httpapi`
    - Verify: `cd backend && go build ./... && go vet ./...`
    - Acceptance: module builds and vets clean; directory layout matches `structure.md`; each dependency is justified in a comment in `go.mod` or the README stack table.
  - [x] 1.2 Add the `internal/sim` architecture import test
    - _Requirements: 6.1, 19.5 (INV-13 purity), `tech.md` quality gate_
    - Dependencies: 1.1
    - Deliverables: `backend/internal/archtest/archtest_test.go` using `go/parser` to list imports of every non-test file under `internal/sim` and fail on any import outside an explicit standard-library allowlist (no `time`, `math/rand`, `os`, `net`, `sync` goroutine helpers)
    - Verify: `cd backend && go test ./internal/archtest/...`
    - Acceptance: test passes on the skeleton and fails when a forbidden import such as `time` is added to a file in `internal/sim` (verified by a table case over a temporary source string).
  - [x] 1.3 Create the Next.js web skeleton
    - _Requirements: project constraints, 32.12_
    - Dependencies: 1.1
    - Deliverables: `web/` App Router app with TypeScript strict and `noImplicitAny`, ESLint, Tailwind, shadcn/ui base, Vitest config, pinned `pixi.js@8`, `@xyflow/react`, `zustand`; folders `features/{world-view,timeline,branches,decisions,duel,ledger}`, `lib/api`, `stores`; one trivial Vitest test
    - Verify: `cd web && npx tsc --noEmit && npx eslint . && npx vitest --run`
    - Acceptance: all three commands exit 0; `package.json` has exact versions; no `any` in source.
  - [x] 1.4 Add Docker Compose for PostgreSQL and `.env.example`
    - _Requirements: 20.17, 28.1, `security.md` secrets rule_
    - Dependencies: 1.1
    - Deliverables: `docker-compose.yml` (PostgreSQL 16 service `db` only), `.env.example` with `DATABASE_URL` placeholder, `LLM_BASE_URL=https://openrouter.ai/api/v1`, empty `LLM_API_KEY=`, empty `LLM_MODEL=`, commented `LLM_MAX_CALLS_PER_WORLD=40`; `.gitignore` covers `.env`
    - Verify: `docker compose config --quiet && docker compose up -d db && docker compose ps db`
    - Acceptance: database container reports healthy; `.env.example` contains no real secret and no model identifier.
  - [x] 1.5 Add CI for amd64 and arm64
    - _Requirements: 6.4 (cross-architecture), `tech.md` quality gates_
    - Dependencies: 1.2, 1.3, 1.4
    - Deliverables: `.github/workflows/ci.yml` with a Go job matrix on `ubuntu-latest` (amd64) and `ubuntu-24.04-arm` (arm64) running `go vet`, `staticcheck`, `govulncheck`, `go test ./...`; a web job running `tsc --noEmit`, ESLint, `vitest --run`, `npm audit --audit-level=high`
    - Verify: `cd backend && go vet ./... && go test ./... && go run honnef.co/go/tools/cmd/staticcheck@latest ./... && go run golang.org/x/vuln/cmd/govulncheck@latest ./...`
    - Acceptance: local equivalents pass; workflow YAML lists both architectures for the Go job.
  - [x] 1.6 Checkpoint — M0 complete
    - Ensure all tests pass, ask the user if questions arise.

- [x] 2. M1 — Working end-to-end vertical slice (~900 credits)
  - [x] 2.1 Implement integer money and the shared rounding helpers
    - _Requirements: 6.1, 10.1, 10.16, 37.9_
    - Dependencies: 1.6
    - Deliverables: `backend/internal/sim/money.go` (`Cents`, `Bps`, `RoundHalfEven`, `MulDiv` with 128-bit intermediates via `math/bits`), table-driven `money_test.go`
    - Verify: `cd backend && go test ./internal/sim -run 'TestRoundHalfEven|TestMulDiv'`
    - Acceptance: ties round to even for positive and negative values; `MulDiv` is exact at the maximum Assumption bounds without overflow; no `float32`/`float64` appears in `internal/sim`.
  - [x] 2.2 Implement the SplitMix64 random streams
    - _Requirements: 6.2, 14.6 (full stream state)_
    - Dependencies: 1.6
    - Deliverables: `backend/internal/sim/rng.go` (`Seed`, `StreamName`, `Stream`, `NewStream(seed, name)` seeded with `seed XOR fnv64a(name)`, `Next`, unbiased `IntN`, `StreamState`), `rng_test.go` with fixed reference vectors
    - Verify: `cd backend && go test ./internal/sim -run TestStream`
    - Acceptance: reference vectors match; streams with different names diverge; restoring a `StreamState` reproduces the following draws exactly.
  - [x] 2.3 Implement the sim clock and the Event_Queue
    - _Requirements: 5.1, 5.2, 5.3_
    - Dependencies: 2.1
    - Deliverables: `backend/internal/sim/clock.go` (`SimMinute`, `Tick`, `Boundary`), `eventqueue.go` (binary heap ordered by `(At, Priority, Seq)`, insertion sequence counter), `eventqueue_test.go`
    - Verify: `cd backend && go test ./internal/sim -run 'TestClock|TestEventQueue'`
    - Acceptance: pops follow `(At, Priority, Seq)` for shuffled pushes; equal `At` and `Priority` pop in insertion order.
  - [x] 2.4 Implement the Scenario model and strict parser
    - _Requirements: 1.1, 1.2, 1.5_
    - Dependencies: 2.1
    - Deliverables: `backend/internal/sim/scenario.go` (Scenario, Assumption metadata, Disruption_Script types per the design JSON), `scenario_parse.go` using `json.Decoder.DisallowUnknownFields`, JSON-path error reporting, schema version check; `scenario_parse_test.go`
    - Verify: `cd backend && go test ./internal/sim -run TestScenarioParse`
    - Acceptance: unknown, missing and wrong-type fields each produce an error naming the JSON path of the first offending field; an unsupported `schemaVersion` returns the unsupported-version error.
  - [x] 2.5 Implement the Scenario_Validator
    - _Requirements: 1.3, 1.4, 1.6, 1.7, 1.8, 1.9, 4.4, 4.5_
    - Dependencies: 2.4
    - Deliverables: `backend/internal/sim/scenario_validate.go` (bounds, units, cents and bps integer rules, metadata completeness, Score_Weights sum, Profit_Target > Profit_Floor, Tie_Tolerance 0..10000, Assumption overrides applied then validated), `scenario_validate_test.go`
    - Verify: `cd backend && go test ./internal/sim -run TestScenarioValidate`
    - Acceptance: each rule has a failing table case whose error names the Assumption, value and allowed range (or the Score_Weights and their sum).
  - [x] 2.6 Implement the canonical Scenario_Printer
    - _Requirements: 2.1_
    - Dependencies: 2.5
    - Deliverables: `backend/internal/sim/scenario_print.go` (fixed field order, no whitespace variation), `scenario_print_test.go`
    - Verify: `cd backend && go test ./internal/sim -run TestScenarioPrint`
    - Acceptance: printing the same value twice is byte-identical; output parses with the strict parser.
  - [x] 2.7 Property test: Scenario round trip
    - **Property 1: Scenario round trip**
    - **Validates: Requirements 2.2**
    - Dependencies: 2.6
    - Deliverables: `backend/internal/sim/scenario_roundtrip_prop_test.go` (`TestPropScenarioRoundTrip`) with a generator of valid Scenario values within bounds
    - Verify: `cd backend && go test ./internal/sim -run TestPropScenarioRoundTrip`
  - [ ]* 2.8 Property test: Scenario print idempotence
    - **Property 2: Scenario print idempotence**
    - **Validates: Requirements 2.3**
    - Dependencies: 2.6
    - Deliverables: `backend/internal/sim/scenario_idempotence_prop_test.go` (`TestPropScenarioPrintIdempotence`)
    - Verify: `cd backend && go test ./internal/sim -run TestPropScenarioPrintIdempotence`
  - [x] 2.9 Author the Flagship_Scenario with synthetic, uncalibrated Assumptions
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.9, 9.9_
    - Dependencies: 2.6
    - Deliverables: `backend/scenarios/black-friday.json` (120 Ticks from Thursday 00:00, 4 SKUs, primary and backup supplier per SKU, cadence 4, budget 5, weights 5000/3000/2000, Tie_Tolerance 10, provisional Profit_Floor/Profit_Target marked "synthetic, illustrative; uncalibrated", temp onboarding delay and temp wage Assumptions, demand_surge, supplier_outage and workforce_absence after Tick 24); `backend/internal/sim/flagship_test.go`
    - Verify: `cd backend && go test ./internal/sim -run TestFlagshipScenario`
    - Acceptance: the file passes the Scenario_Validator; the test asserts the counts, cadence, budget, weights and onset Ticks above; every rationale note is labelled "synthetic, illustrative" or cites a source.
  - [x] 2.10 Implement the State model and canonical Encode, Decode and Hash
    - _Requirements: 6.5, 12.1, 12.2, 12.3, 14.6, 28.6_
    - Dependencies: 2.2, 2.3, 2.5
    - Deliverables: `backend/internal/sim/state.go` (snapshot-complete `State` per the design: inventory with values, order book and `OrderSeq`, purchase orders and `POSeq`, workforce cohorts, active Disruptions, prices and marketing spend, pending events with `EventSeq`, Ledger, full stream states), `encode.go` (fixed field order, sorted collections, fixed-width big-endian integers), `hash.go` (SHA-256), `encode_test.go`
    - Verify: `cd backend && go test ./internal/sim -run 'TestEncode|TestHash'`
    - Acceptance: the encoding contains no identifier, label, provenance, timestamp, idempotency key, rationale, model name or engine version string; a golden encoding of a small fixed State is committed and matches.
  - [x] 2.11 Property test: State canonical encoding round trip
    - **Property 4: State canonical encoding round trip**
    - **Validates: Requirements 12.5**
    - Dependencies: 2.10
    - Deliverables: `backend/internal/sim/encode_prop_test.go` (`TestPropStateEncodingRoundTrip`) with a State generator
    - Verify: `cd backend && go test ./internal/sim -run TestPropStateEncodingRoundTrip`
  - [x] 2.12 Implement the Ledger with the fixed chart of accounts
    - _Requirements: 10.1, 10.2, 10.12, 10.13, 37.1_
    - Dependencies: 2.10
    - Deliverables: `backend/internal/sim/ledger.go` (the 10 accounts only, `LedgerEntry` with Tick, Sim_Time, description, cause reference and ≥ 2 lines; a post helper that rejects unbalanced entries; running balances; opening entry builder), `ledger_test.go`
    - Verify: `cd backend && go test ./internal/sim -run TestLedger`
    - Acceptance: the opening entry debits Cash and Inventory (Σ opening units × Opening_Unit_Cost) and credits Opening_Equity by the total; posting an unbalanced entry is impossible through the API of the package.
  - [x] 2.13 Implement Init, StepTick, recurring events, demand arrivals, conversion and cancellation
    - _Requirements: 5.2, 5.5, 7.1, 7.2, 7.3, 7.4, 7.5, 37.3_
    - Dependencies: 2.12
    - Deliverables: `backend/internal/sim/engine.go` (`Init`, `StepTick`, recurring events at the engine-version-1 offsets from the design table), `demand.go` (fixed draws per SKU per Tick from the demand stream, deterministic conversion via `MulDiv`), `orders.go` (order creation with promised ship-by, cancellation at the threshold with an outcome-journal entry and no Ledger_Entry), `engine_demand_test.go`
    - Verify: `cd backend && go test ./internal/sim -run 'TestInit|TestDemand|TestCancel'`
    - Acceptance: a fixed-seed example produces a committed arrival and order count; raising the price lowers converted orders without changing the demand stream state after the Tick.
  - [x] 2.14 Implement fulfilment capacity, shipping and shipment postings
    - _Requirements: 9.1, 9.2, 9.3, 9.4, 9.7, 10.3, 10.4, 10.5, 10.11, 37.2, 37.4, 37.5, 37.7, 37.8_
    - Dependencies: 2.13
    - Deliverables: `backend/internal/sim/fulfilment.go` (capacity, ship-in-full in `(promisedBy, creationSeq)` order, Fulfilment_Constraint_Report split stock-bound/capacity-bound), `valuation.go` (Moving_Average_Cost with last-unit remainder), shipment postings (Revenue/Cash, COGS/Inventory, Freight/Cash, Service_Credit/Cash for late orders), `fulfilment_test.go`, `valuation_test.go`
    - Verify: `cd backend && go test ./internal/sim -run 'TestFulfilment|TestValuation'`
    - Acceptance: worked examples cover q < u rounding half to even, q = u taking the full remaining value, a late shipment posting a service credit, and a report listing both stock-bound and capacity-bound orders.
  - [x] 2.15 Implement purchase orders, receipts and hourly accruals
    - _Requirements: 8.1, 8.2, 8.5, 8.6, 8.7, 8.8, 10.6, 10.8, 10.9, 10.10, 37.6, 37.12_
    - Dependencies: 2.14
    - Deliverables: `backend/internal/sim/purchasing.go` (PO ids from `POSeq`, receipt events at effective Tick + lead time, Inventory_Value increase at Supplier_Unit_Cost), `accruals.go` (regular wage, marketing and overhead entries paid in the Tick; overtime and temp wage lines present with zero hours until M2 commands), `purchasing_test.go`
    - Verify: `cd backend && go test ./internal/sim -run 'TestPurchasing|TestAccruals'`
    - Acceptance: a receipt debits Inventory and credits Cash by unit cost × quantity; written-off units are always zero; wages appear as separate lines per Tick.
  - [x] 2.16 Implement the scripted supplier_outage and same-Sim_Time ordering
    - _Requirements: 5.7, 5.8, 8.3, 16.1, 16.4_
    - Dependencies: 2.15
    - Deliverables: `backend/internal/sim/disruptions.go` (activation and expiry events, script order before injected order, receipt delay to outage end + lead time), priority assignments in `engine.go`, `disruptions_test.go`
    - Verify: `cd backend && go test ./internal/sim -run TestSupplierOutage`
    - Acceptance: a receipt scheduled inside the outage moves to outage end + lead time; at one Sim_Time the order is Commands, expiries, activations, then other events.
  - [x] 2.17 Implement the tick-boundary sequence, Decision_Windows and the horizon stop
    - _Requirements: 5.4, 5.6, 5.9, 6.5, 6.6, 18.1, 37.15_
    - Dependencies: 2.16
    - Deliverables: `backend/internal/sim/boundary.go` (fix State, hash, metrics hook, Fulfilment_Constraint_Report, Tick_Frame data, Decision_Window detection for cadence multiples and Disruption onsets, horizon-reached error), `boundary_test.go`
    - Verify: `cd backend && go test ./internal/sim -run 'TestBoundary|TestHorizon'`
    - Acceptance: the flagship opens windows at every multiple of 4 and at each onset; no event at or after N × 60 runs while a window is open; stepping past Tick 120 returns the horizon-reached error.
  - [x] 2.18 Implement the Metrics_Calculator and the End_Of_Horizon_Statement
    - _Requirements: 11.1, 11.2, 11.3, 11.4, 11.5, 11.6, 11.7, 11.8, 37.16, 37.17, 37.18, 37.19, 37.20_
    - Dependencies: 2.17
    - Deliverables: `backend/internal/sim/metrics.go` (`ComputeMetrics`, `EndOfHorizonStatement`), `metrics_test.go`
    - Verify: `cd backend && go test ./internal/sim -run 'TestMetrics|TestEndOfHorizon'`
    - Acceptance: zero Orders_Due yields 10000 bps for Service_Level and Fill_Rate; an end-of-horizon example reports open POs as commitments, backlog without revenue and zero accrued wages.
  - [x] 2.19 Implement Observation, the slice Commands and the Command_Validator
    - _Requirements: 13.3, 17.1, 17.2, 17.3, 17.5, 17.6, 17.7, 18.6, 18.7, 5.7_
    - Dependencies: 2.18
    - Deliverables: `backend/internal/sim/observation.go` (`Observe` with documented sort orders, no future script, draws or seed), `commands.go` (closed Command_Set types; `set_price` and `place_purchase_order` applied at N × 60 with priority 0 in input sequence order; other kinds return `not_implemented` until M2), `validate.go` (sequential validation with committed Cash and consumed budget, reason codes), `validate_test.go`
    - Verify: `cd backend && go test ./internal/sim -run 'TestObserve|TestValidate|TestCommandApply'`
    - Acceptance: a rejected Command returns a reason code and message and is unchanged; the Observation at Tick N omits Disruption_Script entries with onset after N.
  - [x] 2.20 Create the shared rapid generators package
    - _Requirements: supports 6.3, 7.2, 8.6, 10.14 and the other slice properties_
    - Dependencies: 2.19
    - Deliverables: `backend/internal/simtest/gen.go` (Scenario overrides within bounds starting from the flagship, Seeds, validator-accepted Input logs at Decision_Window Ticks, a `RunToHorizon` helper returning per-Tick hashes and States)
    - Verify: `cd backend && go vet ./internal/simtest/... && go test ./internal/simtest/...`
    - Acceptance: generated Input logs pass the Command_Validator; the architecture test still passes because `simtest` lives outside `internal/sim`.
  - [x] 2.21 Property test: determinism at every Tick (required)
    - **Property 5: Determinism at every tick** (INV-1)
    - **Validates: Requirements 6.3**
    - Dependencies: 2.20
    - Deliverables: `backend/internal/sim/determinism_prop_test.go` (`TestPropDeterminism`)
    - Verify: `cd backend && go test ./internal/sim -run TestPropDeterminism`
  - [x] 2.22 Property test: same-Sim_Time event ordering (required)
    - **Property 39: Same-Sim_Time event ordering** (INV-1)
    - **Validates: Requirements 5.7, 5.8**
    - Dependencies: 2.20
    - Deliverables: `backend/internal/sim/ordering_prop_test.go` (`TestPropSameMinuteOrdering`)
    - Verify: `cd backend && go test ./internal/sim -run TestPropSameMinuteOrdering`
  - [x] 2.23 Property test: demand draw count independent of Commands (required)
    - **Property 12: Demand draw count independent of commands** (INV-12)
    - **Validates: Requirements 7.2**
    - Dependencies: 2.20
    - Deliverables: `backend/internal/sim/drawcount_prop_test.go` (`TestPropDemandDrawCount`)
    - Verify: `cd backend && go test ./internal/sim -run TestPropDemandDrawCount`
  - [x] 2.24 Property test: order conservation (required)
    - **Property 13: Order conservation** (INV-9)
    - **Validates: Requirements 7.6**
    - Dependencies: 2.20
    - Deliverables: `backend/internal/sim/orders_prop_test.go` (`TestPropOrderConservation`)
    - Verify: `cd backend && go test ./internal/sim -run TestPropOrderConservation`
  - [x] 2.25 Property test: inventory conservation and non-negative stock (required)
    - **Property 14: Inventory conservation and non-negative stock** (INV-8)
    - **Validates: Requirements 8.6, 8.7**
    - Dependencies: 2.20
    - Deliverables: `backend/internal/sim/inventory_prop_test.go` (`TestPropInventoryConservation`)
    - Verify: `cd backend && go test ./internal/sim -run TestPropInventoryConservation`
  - [x] 2.26 Property test: ledger balance (required)
    - **Property 15: Ledger balance** (INV-6)
    - **Validates: Requirements 10.14**
    - Dependencies: 2.20
    - Deliverables: `backend/internal/sim/ledger_balance_prop_test.go` (`TestPropLedgerBalance`)
    - Verify: `cd backend && go test ./internal/sim -run TestPropLedgerBalance`
  - [x] 2.27 Property test: cash reconciliation (required)
    - **Property 16: Cash reconciliation** (INV-7)
    - **Validates: Requirements 10.15**
    - Dependencies: 2.20
    - Deliverables: `backend/internal/sim/cash_prop_test.go` (`TestPropCashReconciliation`)
    - Verify: `cd backend && go test ./internal/sim -run TestPropCashReconciliation`
  - [ ]* 2.28 Property test: profit consistency
    - **Property 17: Profit consistency** (INV-10)
    - **Validates: Requirements 11.7**
    - Dependencies: 2.20
    - Deliverables: `backend/internal/sim/profit_prop_test.go` (`TestPropProfitConsistency`)
    - Verify: `cd backend && go test ./internal/sim -run TestPropProfitConsistency`
  - [ ]* 2.29 Property test: accrual-to-cash reconciliation
    - **Property 18: Accrual-to-cash reconciliation** (INV-10)
    - **Validates: Requirements 11.8**
    - Dependencies: 2.20
    - Deliverables: `backend/internal/sim/accrual_prop_test.go` (`TestPropAccrualToCash`)
    - Verify: `cd backend && go test ./internal/sim -run TestPropAccrualToCash`
  - [x] 2.30 Property test: valuation consistency and no residual value (required)
    - **Property 19: Valuation consistency and no residual value** (INV-19)
    - **Validates: Requirements 37.10, 37.11**
    - Dependencies: 2.20
    - Deliverables: `backend/internal/sim/valuation_prop_test.go` (`TestPropValuation`)
    - Verify: `cd backend && go test ./internal/sim -run TestPropValuation`
  - [x] 2.31 Property test: balance identity (required)
    - **Property 20: Balance identity** (INV-19)
    - **Validates: Requirements 37.21**
    - Dependencies: 2.20
    - Deliverables: `backend/internal/sim/identity_prop_test.go` (`TestPropBalanceIdentity`)
    - Verify: `cd backend && go test ./internal/sim -run TestPropBalanceIdentity`
  - [x] 2.32 Implement `parallel-sim run` for a Scenario and Seed
    - _Requirements: 30.1 (without an Inputs_File), 30.4, 31.6_
    - Dependencies: 2.19
    - Deliverables: `backend/cmd/parallel-sim/main.go` and `run.go` (flags `--scenario`, `--seed`; prints final State_Hash hex, Net_Profit in cents, Service_Level in bps and the Disclaimer), `run_test.go`
    - Verify: `cd backend && go test ./cmd/parallel-sim/... && go run ./cmd/parallel-sim run --scenario scenarios/black-friday.json --seed 2026`
    - Acceptance: two runs print the same hash; output contains the Disclaimer text verbatim; the binary opens no network connection and reads no database variable.
  - [x] 2.33 Run the early benchmark and create `docs/benchmarks.md`
    - _Requirements: 29.1, 29.2 (early proxy), 29.8, 29.9, 29.10, 29.11_
    - Dependencies: 2.21, 2.24, 2.25, 2.26, 2.27, 2.32
    - Deliverables: `backend/internal/sim/bench_test.go` (`BenchmarkFlagshipReplay`, `BenchmarkHundredReplays` = 2 runs × Seeds 1..50, sequential, all invariant checks enabled); `docs/benchmarks.md` recording hardware model, CPU, memory, OS, Go version, engine version, scenario name, SKU count, horizon, events per run, Seeds, warm-up and measured iterations, median and p95, commit id, and raw output
    - Verify: `cd backend && go test ./internal/sim -run '^$' -bench 'BenchmarkFlagshipReplay|BenchmarkHundredReplays' -benchtime 10x -count 10`
    - Acceptance: every number in `docs/benchmarks.md` is copied from the actual command output; a missed budget is recorded with the shortfall; budgets 29.3 to 29.7 and criterion 29.2 itself are labelled "target, not yet measured".
  - [x] 2.34 Define the app ports and the in-memory store with a shared contract suite
    - _Requirements: 13.1, 13.2, 28.1 (ports only)_
    - Dependencies: 2.19
    - Deliverables: `backend/internal/app/ports.go` (`WorldRepo`, `BranchRepo`, `InputRepo`, `SnapshotRepo`), `backend/internal/store/memory/store.go` (append-only Inputs, no update or delete method), `backend/internal/store/storetest/contract.go` (reusable contract suite), `backend/internal/store/memory/store_test.go`
    - Verify: `cd backend && go test ./internal/store/...`
    - Acceptance: the contract suite passes against the memory store and covers append order, per-Branch sequence numbers, idempotency-key lookup and the absence of mutation operations.
  - [x] 2.35 Implement the Branch_Service for worlds, advance, commands and rewind reads
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 13.1, 13.3, 13.4, 13.5, 14.1, 14.2, 14.5, 17.4, 18.2, 18.3, 33.7_
    - Dependencies: 2.34
    - Deliverables: `backend/internal/app/branch_service.go` (create World with canonical Scenario, Seed and engine version; advance with pause at Decision_Windows and optional Run_To_Tick that ends intervening Human windows with no Commands and stops at the next Disruption-onset window; submit Commands through `sim.Validate` only; end Decision_Window; rewind read of State, Observation, metrics, hash and Tick_Frame), `branch_service_test.go`
    - Verify: `cd backend && go test ./internal/app -run TestBranchService`
    - Acceptance: an Input with effective Tick ≤ Head_Tick returns the history-is-immutable error; a rewind read leaves Head_Tick and the Input_Log unchanged; a request for a Tick above Head_Tick returns not-yet-simulated.
  - [x] 2.36 Property test: validation gate and validator determinism (required)
    - **Property 22: Validation gate and validator determinism** (INV-14)
    - **Validates: Requirements 17.4, 17.5, 17.6**
    - Dependencies: 2.35
    - Deliverables: `backend/internal/app/validation_gate_prop_test.go` (`TestPropValidationGate`)
    - Verify: `cd backend && go test ./internal/app -run TestPropValidationGate`
  - [x] 2.37 Property test: Input idempotency key (required)
    - **Property 11: Input idempotency key** (INV-3)
    - **Validates: Requirements 13.4**
    - Dependencies: 2.35
    - Deliverables: `backend/internal/app/idempotency_prop_test.go` (`TestPropIdempotencyKey`)
    - Verify: `cd backend && go test ./internal/app -run TestPropIdempotencyKey`
  - [x] 2.38 Property test: hash metadata independence (required)
    - **Property 6: Hash metadata independence** (INV-15; also verifies the manager-blind engine of 36.8)
    - **Validates: Requirements 12.4**
    - Dependencies: 2.35
    - Deliverables: `backend/internal/app/metadata_prop_test.go` (`TestPropHashMetadataIndependence`)
    - Verify: `cd backend && go test ./internal/app -run TestPropHashMetadataIndependence`
  - [x] 2.39 Property test: presentation independence (required)
    - **Property 7: Presentation independence** (INV-18)
    - **Validates: Requirements 33.10**
    - Dependencies: 2.35
    - Deliverables: `backend/internal/app/presentation_prop_test.go` (`TestPropPresentationIndependence`: random advance granularity, Run_To_Tick use and pause points yield identical hashes)
    - Verify: `cd backend && go test ./internal/app -run TestPropPresentationIndependence`
  - [x] 2.40 Implement forking with the ancestor input-prefix walk
    - _Requirements: 15.1, 15.2, 15.5, 15.6, 15.7, 15.8_
    - Dependencies: 2.35
    - Deliverables: fork operation in `backend/internal/app/branch_service.go` and `inputs_resolve.go` (child rows plus each ancestor's rows with `effective_tick ≤ min(upTo, forkTick)`; no copying), Decision_Window opened on the child at the fork Tick, `fork_test.go`
    - Verify: `cd backend && go test ./internal/app -run TestFork`
    - Acceptance: the 33rd Branch is rejected with branch-limit; a fork Tick above Head_Tick is rejected with not-yet-simulated; parent Inputs are never copied.
  - [x] 2.41 Property test: fork fidelity (required)
    - **Property 9: Fork fidelity** (INV-5)
    - **Validates: Requirements 15.3**
    - Dependencies: 2.40
    - Deliverables: `backend/internal/app/fork_fidelity_prop_test.go` (`TestPropForkFidelity`)
    - Verify: `cd backend && go test ./internal/app -run TestPropForkFidelity`
  - [x] 2.42 Property test: fork independence (required)
    - **Property 10: Fork independence** (INV-4)
    - **Validates: Requirements 15.4**
    - Dependencies: 2.40
    - Deliverables: `backend/internal/app/fork_independence_prop_test.go` (`TestPropForkIndependence`, parent, child and sibling directions)
    - Verify: `cd backend && go test ./internal/app -run TestPropForkIndependence`
  - [x] 2.43 Property test: fork prefix immutability after parent advances (required)
    - **Property 37: Fork prefix immutability after parent advances** (INV-3, INV-4)
    - **Validates: Requirements 15.8**
    - Dependencies: 2.40
    - Deliverables: `backend/internal/app/fork_prefix_prop_test.go` (`TestPropForkPrefixImmutability`)
    - Verify: `cd backend && go test ./internal/app -run TestPropForkPrefixImmutability`
  - [x] 2.44 Implement the Demo_Path plan derivation
    - _Requirements: 34.6, 34.7_
    - Dependencies: 2.40
    - Deliverables: `backend/internal/app/demoplan/demoplan.go` (pure: Crisis_Tick, Demo_Fork_Tick and, for the slice, the single backup-supplier `place_purchase_order` for the SKU with the earliest stock-bound report after the fork Tick, with quantity rule and reason strings), `demoplan_test.go` that regenerates and compares the committed fixture `web/e2e/fixtures/demo-plan.json`
    - Verify: `cd backend && go test ./internal/app/demoplan/...`
    - Acceptance: the Demo_Command passes `sim.Validate` at the Demo_Fork_Tick; the fixture matches the computation; no financial outcome is asserted.
  - [x] 2.45 Property test: demo reproducibility (required)
    - **Property 41: Demo reproducibility** (INV-1)
    - **Validates: Requirements 34.3**
    - Dependencies: 2.44
    - Deliverables: `backend/internal/app/demoplan/repro_prop_test.go` (`TestPropDemoReproducibility`)
    - Verify: `cd backend && go test ./internal/app/demoplan -run TestPropDemoReproducibility`
  - [x] 2.46 Write the OpenAPI 3.1 document for the slice endpoints
    - _Requirements: 11.9, 26.1, 26.2 (create World, read World, fork, advance, submit Commands, end Decision_Window, read Tick_Frames, read State), 26.3, 26.7, 26.8_
    - Dependencies: 2.40
    - Deliverables: `backend/api/openapi.yaml` (schemas for World, Branch, Tick_Frame, Metrics with formula and Ledger accounts in each description, money as cents with ISO 4217 code, Disclaimer field, problem+json errors with the design's error types)
    - Verify: `npx --yes @redocly/cli@latest lint backend/api/openapi.yaml`
    - Acceptance: lint passes with no errors; every metric field documents its formula and accounts.
  - [x] 2.47 Implement the HTTP API for the slice and wire `parallel-api`
    - _Requirements: 26.2 (slice operations), 26.3, 26.4, 26.5, 26.6, 26.7, 26.8_
    - Dependencies: 2.46
    - Deliverables: `backend/internal/httpapi/router.go` (Chi, `/api/v1`, CORS from `WEB_ORIGIN`), `decode.go` (strict JSON, 256 KiB cap), `problem.go` (typed error → problem+json, no stack traces), `worlds.go`, `branches.go`; `backend/cmd/parallel-api/main.go` wiring the memory store; `httpapi_test.go` using `httptest`
    - Verify: `cd backend && go test ./internal/httpapi/... && go build ./cmd/parallel-api`
    - Acceptance: unknown field → 400, 257 KiB body → 413, stale `expectedHeadTick` → 409, history-immutable → 409 problem+json; metric responses carry the Disclaimer. The user starts the server manually with `cd backend && go run ./cmd/parallel-api`.
  - [x] 2.48 Generate the TypeScript API client
    - _Requirements: 26.1, 32.12_
    - Dependencies: 2.47
    - Deliverables: `web/lib/api/schema.d.ts` generated with a pinned `openapi-typescript`, `web/lib/api/client.ts` (typed fetch wrapper, problem+json parsing), `npm run gen:api` script, `client.test.ts`
    - Verify: `cd web && npm run gen:api && npx tsc --noEmit && npx vitest --run lib/api`
    - Acceptance: regenerating produces no diff; the client exposes only server values and never computes metrics.
  - [x] 2.49 Implement the playback controller and view-state stores
    - _Requirements: 33.1, 33.2, 33.3, 33.4, 33.5, 33.6, 33.11, 33.12, 33.15, 33.16, 33.17, 32.2_
    - Dependencies: 2.48
    - Deliverables: `web/stores/playback.ts` (Zustand: speeds 1x/4x/16x = 2/8/32 Ticks per second, display only received Tick_Frames, syncing state, Auto_Pause_Mode default "every decision window", step ±1 Tick, fast-forward to next Decision_Window or Disruption onset with the advance fallbacks, request advance at Head_Tick), `web/stores/playback.test.ts` with fake timers
    - Verify: `cd web && npx vitest --run stores && npx tsc --noEmit`
    - Acceptance: tests show playback never displays a Tick without its frame, pauses at windows or onsets per mode, and stepping back never sends an advance request.
  - [x] 2.50 Build the basic PixiJS World_View
    - _Requirements: 32.1, 32.2, 32.15, 35.1, 35.2, 35.3, 35.6, 35.8, 35.9_
    - Dependencies: 2.49
    - Deliverables: `web/features/world-view/WorldView.tsx` and `scene/*.ts` (per-SKU stock, on-shift workers, order queue, inbound POs, disruption highlight with Ticks since onset, stock-bound and capacity-bound markers with counts, intervention markers with provenance label, dimmed syncing state, reduced-motion instant updates), `WorldViewSummary.tsx` (accessible text summary), `worldViewModel.test.ts` for the frame-to-scene mapping
    - Verify: `cd web && npx vitest --run features/world-view && npx tsc --noEmit`
    - Acceptance: every rendered figure and marker comes from the Tick_Frame in the test fixtures; the text summary lists bottlenecks, Disruptions and interventions.
  - [x] 2.51 Build the HUD and the Timeline
    - _Requirements: 31.5, 32.3, 32.4, 35.4, 35.5_
    - Dependencies: 2.49
    - Deliverables: `web/features/timeline/Timeline.tsx` (horizon ticks, Disruption, Decision_Window, fork and Input markers by provenance; selecting a Tick reads State without advancing), `web/features/world-view/Hud.tsx` (sim clock, Cash, Net_Profit, Service_Level, Backlog, parent comparison with differences for forked Branches, Disclaimer), Vitest tests
    - Verify: `cd web && npx vitest --run features/timeline features/world-view/Hud && npx tsc --noEmit`
    - Acceptance: HUD differences are computed only as display subtraction of two API values for the same Tick and are labelled as such; selecting a Tick issues a read, never an advance.
  - [x] 2.52 Build the Branch_Tree and the fork control
    - _Requirements: 32.5, 32.6_
    - Dependencies: 2.49
    - Deliverables: `web/features/branches/BranchTree.tsx` (React Flow; label, Manager kind, fork Tick, Head_Tick, distinct color and line pattern), `ForkButton.tsx`, Vitest tests
    - Verify: `cd web && npx vitest --run features/branches && npx tsc --noEmit`
    - Acceptance: pressing fork sends one fork request for the displayed Branch and Tick and the new Branch appears from the API response.
  - [x] 2.53 Build the slice Decision_Panel
    - _Requirements: 32.7, 32.8, 13.3_
    - Dependencies: 2.49
    - Deliverables: `web/features/decisions/DecisionPanel.tsx` (Observation summary, remaining budget, forms for `place_purchase_order` and `set_price`, rejection messages next to the Command, "End decision window" button), Vitest tests
    - Verify: `cd web && npx vitest --run features/decisions && npx tsc --noEmit`
    - Acceptance: submitted Commands show effective Tick N+1 from the response; a 422 rejection shows its message next to the form.
  - [x] 2.54 Assemble the world page and the "Start Black Friday demo" entry
    - _Requirements: 33.13, 34.2, 34.4, 32.4, 32.6_
    - Dependencies: 2.50, 2.51, 2.52, 2.53
    - Deliverables: `web/app/page.tsx` (one-action demo start with the default Seed), `web/app/worlds/[worldId]/page.tsx` (layout per `ui-design-system.md`: diorama centre, Timeline bottom, Branch_Tree left, context panel right, HUD top), `web/features/world-view/PlaybackControls.tsx` with Space, Left/Right, Shift+Left/Right and Shift+D bindings, integration test with a mocked API
    - Verify: `cd web && npx vitest --run && npx tsc --noEmit && npx eslint . && npm run build`
    - Acceptance: the mocked integration test runs create → play → pause at window → rewind → fork → submit the demo Command → end window → play to horizon. Manual check by the user: `cd backend && go run ./cmd/parallel-api` and `cd web && npm run dev` in separate terminals.
  - [x] 2.55 Checkpoint — vertical slice complete
    - Run `cd backend && go test ./... && go vet ./...` and `cd web && npx tsc --noEmit && npx eslint . && npx vitest --run`. Ensure all tests pass, ask the user if questions arise.

- [x] 3. M2 — Complete simulation and financial engine (~600 credits)
  - [x] 3.1 Implement `expedite_purchase_order`
    - _Requirements: 8.4, 10.7, 17.1, 17.2_
    - Dependencies: 2.55
    - Deliverables: expedite handling in `backend/internal/sim/commands.go` and `purchasing.go`, validator rule for an existing open PO, `expedite_test.go`
    - Verify: `cd backend && go test ./internal/sim -run TestExpedite`
    - Acceptance: remaining lead time drops by the expedite reduction; the fee posts Freight_Expense against Cash; an unknown PO id is rejected with a reason code.
  - [x] 3.2 Implement `schedule_overtime` and `hire_temp_workers`
    - _Requirements: 9.5, 9.6, 9.8, 9.9, 37.13, 37.14, 17.2_
    - Dependencies: 3.1
    - Deliverables: workforce cohorts and overtime schedule in `backend/internal/sim/workforce.go`, Command handling and validator bounds (max overtime per worker, max temp workers, Cash), wage lines in `accruals.go`, `workforce_test.go`
    - Verify: `cd backend && go test ./internal/sim -run TestWorkforce`
    - Acceptance: overtime is capped per worker; temp workers are paid from the effective Tick, including onboarding, and add capacity only after onboarding.
  - [x] 3.3 Property test: temp worker onboarding wages and zero capacity
    - **Property 40: Temp worker onboarding wages and zero capacity**
    - **Validates: Requirements 9.8, 37.14**
    - Dependencies: 3.2
    - Deliverables: `backend/internal/sim/onboarding_prop_test.go` (`TestPropTempOnboarding`)
    - Verify: `cd backend && go test ./internal/sim -run TestPropTempOnboarding`
  - [x] 3.4 Implement `set_marketing_spend`
    - _Requirements: 7.3, 10.9, 17.2_
    - Dependencies: 3.2
    - Deliverables: marketing spend in `backend/internal/sim/commands.go` and conversion effect in `demand.go`, `marketing_test.go`
    - Verify: `cd backend && go test ./internal/sim -run TestMarketing`
    - Acceptance: higher spend raises conversion of the same Potential_Arrivals; the demand stream state after the Tick is unchanged; spend posts Marketing_Expense per Tick.
  - [x] 3.5 Implement demand_surge, workforce_absence and injected Disruptions in the engine
    - _Requirements: 5.8, 7.1, 9.4, 16.1, 16.4, 16.5_
    - Dependencies: 3.4
    - Deliverables: remaining kinds in `backend/internal/sim/disruptions.go` (surge multiplies arrivals; absence reduces regular workers with half-to-even; severity draws from the disruption stream at a fixed count), injected Disruption Inputs ordered after script entries, `disruption_kinds_test.go`
    - Verify: `cd backend && go test ./internal/sim -run 'TestDemandSurge|TestWorkforceAbsence|TestInjectedDisruption'`
    - Acceptance: the flagship runs all three scripted Disruptions; every onset opens a Decision_Window; the disruption stream draw count per Tick is fixed.
  - [x] 3.6 Implement the Disruption_Injector and its endpoint
    - _Requirements: 16.2, 16.3, 13.1, 26.2 (inject Disruption)_
    - Dependencies: 3.5
    - Deliverables: `backend/internal/app/disruptions.go`, `POST /branches/{id}/disruptions` in `backend/internal/httpapi/branches.go` and `backend/api/openapi.yaml`, tests
    - Verify: `cd backend && go test ./internal/app -run TestInjectDisruption && go test ./internal/httpapi/... && npx --yes @redocly/cli@latest lint backend/api/openapi.yaml`
    - Acceptance: an out-of-bounds magnitude or duration is rejected with the allowed range; a start Tick ≤ Head_Tick is rejected; accepted injections append one Input.
  - [x] 3.7 Implement the Composite_Score calculator
    - _Requirements: 21.5, 21.7, 21.8, 21.10, 21.15_
    - Dependencies: 3.5
    - Deliverables: `backend/internal/sim/score.go` (`ComputeScore`: Financial clamp against Profit_Floor/Profit_Target via `MulDiv`, Fill_Rate and Service_Level components, weighted `RoundHalfEven(..., 10000)`, tie helper), `score_test.go` with worked examples at both clamp edges and a tie at exactly 10 hundredths
    - Verify: `cd backend && go test ./internal/sim -run TestScore`
    - Acceptance: worked examples match hand-computed hundredths; a difference of 10 is a tie and 11 is a win.
  - [x] 3.8 Property test: Composite_Score bounds (required)
    - **Property 34: Composite_Score bounds** (INV-17)
    - **Validates: Requirements 21.11**
    - Dependencies: 3.7
    - Deliverables: `backend/internal/sim/score_bounds_prop_test.go` (`TestPropScoreBounds`)
    - Verify: `cd backend && go test ./internal/sim -run TestPropScoreBounds`
  - [x] 3.9 Property test: Composite_Score competitor independence (required)
    - **Property 35: Composite_Score competitor independence** (INV-17)
    - **Validates: Requirements 21.12**
    - Dependencies: 3.7
    - Deliverables: `backend/internal/sim/score_independence_prop_test.go` (`TestPropScoreCompetitorIndependence`)
    - Verify: `cd backend && go test ./internal/sim -run TestPropScoreCompetitorIndependence`
  - [ ]* 3.10 Property test: Composite_Score monotonicity
    - **Property 36: Composite_Score monotonicity** (INV-17)
    - **Validates: Requirements 21.13**
    - Dependencies: 3.7
    - Deliverables: `backend/internal/sim/score_monotonic_prop_test.go` (`TestPropScoreMonotonicity`)
    - Verify: `cd backend && go test ./internal/sim -run TestPropScoreMonotonicity`
  - [x] 3.11 Implement the Inputs_File parser and printer and the export endpoint
    - _Requirements: 30.6, 30.8, 26.2 (export Inputs)_
    - Dependencies: 3.6
    - Deliverables: `backend/internal/sim/inputsfile/inputsfile.go` (strict JSON with Scenario hash, Seed, engine version, Inputs with provenance and optional rationale; fixed field order), `GET /branches/{id}/export` in `backend/internal/httpapi/export.go` and `openapi.yaml`, tests
    - Verify: `cd backend && go test ./internal/sim/inputsfile/... ./internal/httpapi/...`
    - Acceptance: malformed files report the JSON path of the first offending field; export of a Branch includes inherited ancestor Inputs up to each fork Tick.
  - [x] 3.12 Property test: Inputs_File round trip (required)
    - **Property 3: Inputs file round trip**
    - **Validates: Requirements 30.7**
    - Dependencies: 3.11
    - Deliverables: `backend/internal/sim/inputsfile/roundtrip_prop_test.go` (`TestPropInputsFileRoundTrip`)
    - Verify: `cd backend && go test ./internal/sim/inputsfile -run TestPropInputsFileRoundTrip`
  - [x] 3.13 Extend `parallel-sim` with `--inputs` and `verify`
    - _Requirements: 30.1, 30.2, 30.3, 30.4_
    - Dependencies: 3.11
    - Deliverables: `backend/cmd/parallel-sim/run.go` (`--inputs`), `verify.go` (exit 0 on match, 1 on mismatch, 2 on engine-version mismatch with message), `verify_test.go`
    - Verify: `cd backend && go test ./cmd/parallel-sim/...`
    - Acceptance: the three exit codes are asserted by tests that execute the command in-process.
  - [x] 3.14 Add the State, Ledger, journal and End_Of_Horizon_Statement read endpoints
    - _Requirements: 26.2 (read Ledger_Entries, outcome journal, End_Of_Horizon_Statement), 10.13, 11.9, 26.8_
    - Dependencies: 3.11
    - Deliverables: `backend/internal/httpapi/reads.go`, schema additions in `openapi.yaml`, regenerated `web/lib/api/schema.d.ts`, tests
    - Verify: `cd backend && go test ./internal/httpapi/... && npx --yes @redocly/cli@latest lint backend/api/openapi.yaml && cd ../web && npm run gen:api && npx tsc --noEmit`
    - Acceptance: every Ledger_Entry returned carries its cause reference; responses with metrics carry the Disclaimer.
  - [x] 3.15 Extend the Demo_Path plan with the staffing Command
    - _Requirements: 34.6, 34.7, 34.9_
    - Dependencies: 3.5
    - Deliverables: staffing rule in `backend/internal/app/demoplan/demoplan.go` (add `hire_temp_workers` or `schedule_overtime` only when the no-Command run reports capacity-bound orders after the Demo_Fork_Tick, with the reason), regenerated `web/e2e/fixtures/demo-plan.json`
    - Verify: `cd backend && go test ./internal/app/demoplan/...`
    - Acceptance: both Demo_Commands pass `sim.Validate` at the Demo_Fork_Tick in one window; the fixture change is visible in the diff.
  - [x] 3.16 Add the golden flagship hash test
    - _Requirements: 6.4_
    - Dependencies: 3.13, 3.15
    - Deliverables: `backend/scenarios/demo-fork-inputs.json` (Demo_Commands as an Inputs_File), `backend/scenarios/golden/demo-fork.hash`, `backend/internal/sim/golden_test.go` (`TestGoldenFlagshipHash`), CI step running it on both architectures
    - Verify: `cd backend && go test ./internal/sim -run TestGoldenFlagshipHash && go run ./cmd/parallel-sim verify --scenario scenarios/black-friday.json --seed 2026 --inputs scenarios/demo-fork-inputs.json --expect "$(cat scenarios/golden/demo-fork.hash)"`
    - Acceptance: the test passes locally and in both CI architecture jobs; the golden file changes only with an engine version bump noted in the commit message.
  - [x] 3.17 Checkpoint — M2 complete
    - Run `cd backend && go test ./... && go vet ./...` and `cd web && npx tsc --noEmit && npx vitest --run`. Ensure all tests pass, ask the user if questions arise.

- [ ] 4. M3 — Persistence, replay and branching (~350 credits)
  - [-] 4.1 Write the Goose migrations
    - _Requirements: 28.1, 28.2, 28.5_
    - Dependencies: 3.17
    - Deliverables: `backend/migrations/00001_init.sql` (tables `worlds`, `branches`, `input_log` with UNIQUE constraints and the append-only trigger, `snapshots`, `tick_summaries`; full down section), `backend/cmd/parallel-api/migrate.go` (or a `migrate` subcommand) that runs Goose with a separate migration role URL
    - Verify: `docker compose up -d db && cd backend && go run ./cmd/parallel-api migrate up && go run ./cmd/parallel-api migrate down && go run ./cmd/parallel-api migrate up`
    - Acceptance: up, down and up again succeed; the runtime role has no DDL grant (documented in the migration comment and checked by a grant statement).
  - [ ] 4.2 Implement the PostgreSQL repositories
    - _Requirements: 28.1, 13.1, 13.2, 13.4_
    - Dependencies: 4.1
    - Deliverables: `backend/internal/store/postgres/{worlds,branches,inputs,snapshots}.go` with pgx parameterized queries only, `store_test.go` running the shared contract suite when `PARALLEL_TEST_DATABASE_URL` is set
    - Verify: `docker compose up -d db && cd backend && PARALLEL_TEST_DATABASE_URL=postgres://parallel:parallel@localhost:5432/parallel_test?sslmode=disable go test ./internal/store/...`
    - Acceptance: the contract suite passes against both stores; the uint64 Seed round-trips through `bigint`; no SQL string is built by concatenation.
  - [ ] 4.3 Integration test: Input_Log rejects UPDATE and DELETE (required)
    - _Requirements: 28.5, 13.2 (INV-3)_
    - Dependencies: 4.2
    - Deliverables: `backend/internal/store/postgres/immutability_test.go` (`TestInputLogImmutable`)
    - Verify: `cd backend && PARALLEL_TEST_DATABASE_URL=postgres://parallel:parallel@localhost:5432/parallel_test?sslmode=disable go test ./internal/store/postgres -run TestInputLogImmutable`
    - Acceptance: both statements fail with the trigger's error and the row is unchanged.
  - [ ] 4.4 Implement snapshots with verify-by-hash and self-healing replay
    - _Requirements: 14.3, 14.4, 28.3, 28.6_
    - Dependencies: 4.2
    - Deliverables: `backend/internal/app/snapshots.go` (save complete canonical State every `snapshotIntervalTicks`; load latest at or before T, decode, re-hash, resume on match; on mismatch discard, replay from Tick 0 and log a snapshot-mismatch warning), `snapshots_test.go` with a corrupted-snapshot case
    - Verify: `cd backend && go test ./internal/app -run TestSnapshots`
    - Acceptance: a tampered snapshot blob is discarded and the served State hash equals the replayed hash.
  - [ ] 4.5 Property test: replay equivalence from Snapshots (required)
    - **Property 8: Replay equivalence from snapshots** (INV-2)
    - **Validates: Requirements 14.3**
    - Dependencies: 4.4
    - Deliverables: `backend/internal/app/replay_prop_test.go` (`TestPropReplayEquivalence`)
    - Verify: `cd backend && go test ./internal/app -run TestPropReplayEquivalence`
  - [ ] 4.6 Property test: Snapshot completeness and resume equivalence (required)
    - **Property 38: Snapshot completeness and resume equivalence** (INV-2)
    - **Validates: Requirements 14.6, 28.6**
    - Dependencies: 4.4
    - Deliverables: `backend/internal/app/snapshot_resume_prop_test.go` (`TestPropSnapshotResume`: decode at Tick N, apply a random Input suffix, compare every hash, PO id and stream draw with the uninterrupted run)
    - Verify: `cd backend && go test ./internal/app -run TestPropSnapshotResume`
  - [ ] 4.7 Wire PostgreSQL into `parallel-api` and test restart rebuild
    - _Requirements: 28.1, 28.4_
    - Dependencies: 4.4
    - Deliverables: store selection in `backend/cmd/parallel-api/main.go` (PostgreSQL when `DATABASE_URL` is set, memory otherwise), `tick_summaries` projection writes in the Branch_Service, `backend/internal/store/postgres/restart_test.go` (new service instance over the same database rebuilds every Branch with equal stored hashes)
    - Verify: `cd backend && PARALLEL_TEST_DATABASE_URL=postgres://parallel:parallel@localhost:5432/parallel_test?sslmode=disable go test ./internal/store/postgres -run TestRestartRebuild`
    - Acceptance: rebuilt hashes equal the stored hashes at every computed Tick; `DATABASE_URL` is never logged.
  - [ ] 4.8 Record the engine version and flag mismatches
    - _Requirements: 4.3, 22.4_
    - Dependencies: 4.7
    - Deliverables: `sim.EngineVersion` constant, mismatch warning attached to State, score and diff responses in `backend/internal/app/branch_service.go` and `httpapi`, test
    - Verify: `cd backend && go test ./internal/app -run TestEngineVersionMismatch`
    - Acceptance: a World stored with a different version returns every read with an engine-version-mismatch warning.
  - [ ] 4.9 Checkpoint — M3 complete
    - Run `cd backend && go test ./... && PARALLEL_TEST_DATABASE_URL=postgres://parallel:parallel@localhost:5432/parallel_test?sslmode=disable go test ./internal/store/...`. Ensure all tests pass, ask the user if questions arise.

- [ ] 5. M4 — Premium interactive simulation studio (~500 credits)
  - [ ] 5.1 Implement the Progress_Stream (SSE)
    - _Requirements: 27.1, 27.2, 27.3, 27.4, 27.5_
    - Dependencies: 4.9
    - Deliverables: `backend/internal/httpapi/sse.go` (`GET /branches/{id}/progress`; `id` = Tick; tick, decision_window and completed events; `Last-Event-ID` resume; 4 connections per client IP → 429), `openapi.yaml` entry, `sse_test.go`
    - Verify: `cd backend && go test ./internal/httpapi -run TestProgressStream`
    - Acceptance: reconnecting with `Last-Event-ID: 10` yields Tick 11 first; the fifth connection from one IP gets 429.
  - [ ] 5.2 Consume the Progress_Stream in the Web_App with a polling fallback
    - _Requirements: 27.1, 27.3, 33.3, 33.4_
    - Dependencies: 5.1
    - Deliverables: `web/lib/api/progress.ts` (EventSource with resume and fallback to `GET /tick-frames` polling), integration into `web/stores/playback.ts`, Vitest tests with a mocked EventSource
    - Verify: `cd web && npx vitest --run lib/api stores && npx tsc --noEmit`
    - Acceptance: when the stream errors, frames continue through polling with no skipped Tick.
  - [ ] 5.3 Build the Ledger_Panel drill-down
    - _Requirements: 32.9, 31.5, 10.13, 11.9_
    - Dependencies: 5.1
    - Deliverables: ledger filter parameters (account, Tick range) in `backend/internal/httpapi/reads.go` and `openapi.yaml`; `web/features/ledger/LedgerPanel.tsx` (P&L by account; selecting a money figure lists the Ledger_Entries whose lines sum to it, with cause links; Disclaimer); tests on both sides
    - Verify: `cd backend && go test ./internal/httpapi/... && cd ../web && npm run gen:api && npx vitest --run features/ledger && npx tsc --noEmit`
    - Acceptance: a backend test asserts that the returned lines sum exactly to the reported figure; the panel shows that sum from the API, not a client recomputation.
  - [ ] 5.4 Build the Assumptions endpoint, Assumptions_Editor and Assumptions_Panel
    - _Requirements: 31.1, 31.2, 31.3, 31.4, 32.13, 4.4_
    - Dependencies: 5.3
    - Deliverables: `GET /worlds/{id}/assumptions` (value, unit, default, min, max, rationale, overridden flag) in `backend/internal/httpapi/worlds.go` and `openapi.yaml`; `web/features/assumptions/AssumptionsEditor.tsx` (overrides sent with World creation; Disruption_Script hidden until "Reveal disruption script"), `AssumptionsPanel.tsx` (one action from World_View, later Duel view and Diff_Panel); tests
    - Verify: `cd backend && go test ./internal/httpapi/... && cd ../web && npm run gen:api && npx vitest --run features/assumptions && npx tsc --noEmit`
    - Acceptance: out-of-bounds overrides surface the validator's message; while Head_Tick < horizon only script entries with onset ≤ the displayed Tick are shown.
  - [ ] 5.5 Complete the Decision_Panel for all Command kinds
    - _Requirements: 32.7, 32.8, 17.1, 17.3_
    - Dependencies: 5.3
    - Deliverables: forms for `expedite_purchase_order`, `schedule_overtime`, `hire_temp_workers`, `set_marketing_spend` in `web/features/decisions/`, client-side bounds hints taken from Assumptions (server remains the validator), tests
    - Verify: `cd web && npx vitest --run features/decisions && npx tsc --noEmit`
    - Acceptance: each kind round-trips through a mocked API; rejection messages appear next to the rejected Command.
  - [ ] 5.6 Add Disruption injection to the studio
    - _Requirements: 16.2, 16.3_
    - Dependencies: 5.5
    - Deliverables: `web/features/decisions/InjectDisruption.tsx` (kind, start Tick > Head_Tick, duration, magnitude within Scenario bounds), tests
    - Verify: `cd web && npx vitest --run features/decisions && npx tsc --noEmit`
    - Acceptance: an out-of-range request shows the server's allowed range; an accepted injection appears as a Timeline marker after reload.
  - [ ] 5.7 Apply the studio visual design, interpolation and full Timeline markers
    - _Requirements: 32.3, 32.12, 32.15, 35.3, 35.4, 35.6_
    - Dependencies: 5.4, 5.6
    - Deliverables: restyled components per `ui-design-system.md`; decorative position interpolation between two authoritative Tick_Frames in `web/features/world-view/scene/interpolate.ts` (positions only, never numbers; disabled under reduced motion); provenance-distinguished Input markers; tests
    - Verify: `cd web && npx vitest --run && npx tsc --noEmit && npx eslint . && npm run build`
    - Acceptance: a test asserts that every rendered number equals the value of the displayed Tick_Frame at all interpolation steps.
  - [ ] 5.8 Checkpoint — M4 complete
    - Run `cd backend && go test ./...` and `cd web && npx tsc --noEmit && npx eslint . && npx vitest --run`. Ensure all tests pass, ask the user if questions arise.

- [ ] 6. M5 — LLM manager and human-versus-AI arena (~400 credits)
  - [ ] 6.1 Implement the Rule_Based_Manager
    - _Requirements: 19.1, 19.2, 19.3, 19.4, 19.5_
    - Dependencies: 5.8
    - Deliverables: `backend/internal/policy/policy.go` (`Manager`, `Proposal`), `backend/internal/policy/rulebased/rulebased.go` (reorder point with safety stock, backup supplier during an observed outage, overtime then temp workers for a capacity gap, budget truncation, rationale citing Observation values, rule-based label), `rulebased_test.go`
    - Verify: `cd backend && go test ./internal/policy/rulebased/... && go test ./internal/archtest/...`
    - Acceptance: the package imports only `internal/sim` and the standard library; table cases show a reorder below the reorder point and backup-supplier use during an outage.
  - [ ] 6.2 Property test: Rule_Based_Manager determinism
    - **Property 23: Rule-based manager determinism**
    - **Validates: Requirements 19.2**
    - Dependencies: 6.1
    - Deliverables: `backend/internal/policy/rulebased/determinism_prop_test.go` (`TestPropRuleBasedDeterminism`)
    - Verify: `cd backend && go test ./internal/policy/rulebased -run TestPropRuleBasedDeterminism`
  - [ ] 6.3 Run AI Managers at Decision_Windows and record provenance
    - _Requirements: 18.3, 19.6, 20.18, 33.8_
    - Dependencies: 6.1
    - Deliverables: AI window loop in `backend/internal/app/branch_service.go` (propose from the same Observation as a Human, validate each proposal, append with `ai_rule_based` provenance and rationale, record per-window provenance even with zero Commands, continue through Run_To_Tick), `ai_window_test.go`
    - Verify: `cd backend && go test ./internal/app -run TestAIWindow`
    - Acceptance: Human and AI Observations for the same Branch and Tick are byte-equal in canonical form; a window with no AI Commands still reports its provenance.
  - [ ] 6.4 Implement the LLM configuration and provider-independent adapter
    - _Requirements: 20.1, 20.10, 20.13, 20.15, 20.17_
    - Dependencies: 5.8
    - Deliverables: `backend/internal/policy/llm/config.go` (present only when `LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL` are all non-empty; `LLM_MAX_CALLS_PER_WORLD` default 40, bounds 1..200; no default model), `client.go` (OpenAI-compatible chat completions request with Observation, Command_Set schema and remaining budget; length-limited, labelled user strings; 20 s timeout), `schema.go` (strict proposal decode), tests with an `httptest` stub provider, `envexample_test.go` asserting `.env.example` has the OpenRouter base URL and empty key and model
    - Verify: `cd backend && go test ./internal/policy/llm/...`
    - Acceptance: a source search test finds no model identifier literal in the package; unknown fields, unknown kinds, missing parameters and over-budget proposal counts are rejected before validation; the key never appears in logs or errors.
  - [ ] 6.5 Implement LLM fallback and call budgets
    - _Requirements: 20.2, 20.3, 20.4, 20.5, 20.6, 20.7, 20.14, 20.16_
    - Dependencies: 6.3, 6.4
    - Deliverables: LLM handling in `backend/internal/app/ai_manager.go` (one request per window per Branch; per-World counter; fallback to Rule_Based_Manager with provenance `ai_rule_based_fallback` and reason `timeout`, `http_error`, `invalid_json`, `schema_violation` or `budget_exhausted`; `ai_llm` Inputs with model name and rationale; replay uses recorded Inputs only), World read fields for calls made and remaining budget, `ai_manager_test.go` with the stub provider
    - Verify: `cd backend && go test ./internal/app -run 'TestLLMFallback|TestLLMBudget|TestLLMReplayNoCall'`
    - Acceptance: each of the five reasons is produced by a stub scenario and recorded; replaying an LLM Branch makes zero HTTP requests to the stub.
  - [ ] 6.6 Add `/managers` and the LLM independence integration test
    - _Requirements: 19.5, 20.8, 20.9, 26.2 (list AI Manager kinds)_
    - Dependencies: 6.5
    - Deliverables: `GET /managers` in `backend/internal/httpapi/managers.go` and `openapi.yaml`; `backend/internal/app/llm_independence_test.go` (`TestNoLLMConfiguration`: with all LLM variables unset and an HTTP transport that fails any request, create, advance, fork, rewind, replay and duel with the Rule_Based_Manager)
    - Verify: `cd backend && env -u LLM_BASE_URL -u LLM_API_KEY -u LLM_MODEL go test ./internal/app -run TestNoLLMConfiguration && go test ./internal/httpapi -run TestManagers`
    - Acceptance: `/managers` lists LLM AI as unavailable and Rule-based AI as available; no request leaves the process (INV-13).
  - [ ] 6.7 Implement the Duel_Service
    - _Requirements: 21.1, 21.2, 21.3, 21.4, 21.5, 21.6, 21.7, 21.8, 21.9, 21.15, 22.1, 22.2, 33.9_
    - Dependencies: 6.5
    - Deliverables: `backend/internal/app/duel_service.go` (two sibling forks, lockstep advance to the next window, wait for both managers, Disruption injection into both, Run_To_Tick lockstep, score at one Comparison_Tick with provisional label before horizon, winner or draw by Tie_Tolerance), `duel_service_test.go`
    - Verify: `cd backend && go test ./internal/app -run TestDuel`
    - Acceptance: neither Branch advances while the other's window is open; scores report raw metrics, components, weights, Profit_Floor, Profit_Target and Tie_Tolerance.
  - [ ] 6.8 Property test: duel parity and exogenous fairness (required)
    - **Property 21: Duel parity and exogenous fairness** (INV-11, INV-12)
    - **Validates: Requirements 18.4, 18.5**
    - Dependencies: 6.7
    - Deliverables: `backend/internal/app/duel_parity_prop_test.go` (`TestPropDuelParity`: random Human Commands versus Rule_Based_Manager; identical window Ticks, budget, Command_Set, lead times, Potential_Arrivals, demand and disruption stream draws and Disruptions at every Tick)
    - Verify: `cd backend && go test ./internal/app -run TestPropDuelParity`
  - [ ] 6.9 Add the Duel endpoints
    - _Requirements: 26.2 (create Duel, advance Duel, read Duel score), 26.8, 22.2_
    - Dependencies: 6.7
    - Deliverables: `backend/internal/httpapi/duels.go` (`POST /duels`, `POST /duels/{id}/advance`, `GET /duels/{id}/score`), `openapi.yaml`, regenerated `web/lib/api/schema.d.ts`, tests
    - Verify: `cd backend && go test ./internal/httpapi -run TestDuels && npx --yes @redocly/cli@latest lint backend/api/openapi.yaml && cd ../web && npm run gen:api && npx tsc --noEmit`
    - Acceptance: score responses carry the Disclaimer and the provisional flag before the horizon.
  - [ ] 6.10 Record the demo duel Inputs_File and golden Branch hashes
    - _Requirements: 30.5, 6.4_
    - Dependencies: 6.7
    - Deliverables: `backend/scenarios/demo-duel-human.json`, `backend/scenarios/demo-duel-ai.json` (Inputs_Files from a scripted duel test using the Demo_Commands for the human and the Rule_Based_Manager for the AI), `backend/scenarios/golden/demo-duel.hashes`, `backend/internal/app/golden_duel_test.go` (`TestGoldenDuelHashes`), CI step on both architectures
    - Verify: `cd backend && go test ./internal/app -run TestGoldenDuelHashes`
    - Acceptance: both Branch hashes reproduce via `parallel-sim verify`; the test asserts no winner.
  - [ ] 6.11 Build the Duel view
    - _Requirements: 32.10, 32.11, 33.14, 21.14, 20.11, 20.12, 31.2, 31.5_
    - Dependencies: 6.9
    - Deliverables: `web/app/duels/[duelId]/page.tsx`, `web/features/duel/DuelView.tsx` (two World_Views side by side at one displayed Tick, shared Timeline, scoreboard with points to two decimals, weighted components, weights and raw metrics for both Branches, Disclaimer, Assumptions_Panel link), provenance labels "Rule-based AI", "LLM AI (<model>)", "Rule-based AI (LLM fallback: <reason>)", rationale as escaped plain text labelled "AI rationale (not verified)", Branch_Tree AI labels; tests
    - Verify: `cd web && npx vitest --run features/duel features/branches && npx tsc --noEmit && npx eslint .`
    - Acceptance: a test renders a rationale containing `<script>` and markdown links as literal text; both views always show the same Tick.
  - [ ] 6.12 Checkpoint — M5 complete
    - Run `cd backend && go test ./...` and `cd web && npx tsc --noEmit && npx vitest --run`. Ensure all tests pass, ask the user if questions arise.

- [ ] 7. M6 — Branch comparison and ensemble experiments (~350 credits)
  - [ ] 7.1 Implement the Branch_Diff_Service
    - _Requirements: 23.1, 23.2, 23.3, 23.4, 23.5, 23.6, 23.7, 23.8, 22.3_
    - Dependencies: 6.12
    - Deliverables: `backend/internal/app/diff_service.go` (Comparison_Tick rule, First_Divergence_Tick by per-Tick hash comparison, differing top-level State components and per-SKU entries, first differing outcome-journal event per Branch, Inputs present in one Branch only, metric differences, causality statement, incomparable-branches error across Worlds), `diff_service_test.go`
    - Verify: `cd backend && go test ./internal/app -run TestDiff`
    - Acceptance: a demo fork versus parent diff reports the Demo_Commands as differing Inputs and a First_Divergence_Tick ≥ their effective Tick; the causality statement appears verbatim.
  - [ ] 7.2 Property test: diff identity
    - **Property 24: Diff identity**
    - **Validates: Requirements 23.9**
    - Dependencies: 7.1
    - Deliverables: `backend/internal/app/diff_identity_prop_test.go` (`TestPropDiffIdentity`)
    - Verify: `cd backend && go test ./internal/app -run TestPropDiffIdentity`
  - [ ]* 7.3 Property test: diff symmetry
    - **Property 25: Diff symmetry**
    - **Validates: Requirements 23.10**
    - Dependencies: 7.1
    - Deliverables: `backend/internal/app/diff_symmetry_prop_test.go` (`TestPropDiffSymmetry`)
    - Verify: `cd backend && go test ./internal/app -run TestPropDiffSymmetry`
  - [ ] 7.4 Property test: diff input determinism
    - **Property 26: Diff input determinism** (INV-1)
    - **Validates: Requirements 23.11**
    - Dependencies: 7.1
    - Deliverables: `backend/internal/app/diff_input_prop_test.go` (`TestPropDiffInputDeterminism`)
    - Verify: `cd backend && go test ./internal/app -run TestPropDiffInputDeterminism`
  - [ ]* 7.5 Property test: diff no early divergence
    - **Property 27: Diff no early divergence**
    - **Validates: Requirements 23.12**
    - Dependencies: 7.1
    - Deliverables: `backend/internal/app/diff_early_prop_test.go` (`TestPropDiffNoEarlyDivergence`)
    - Verify: `cd backend && go test ./internal/app -run TestPropDiffNoEarlyDivergence`
  - [ ] 7.6 Add the diff endpoint, the Diff_Panel and the divergence marker
    - _Requirements: 23.13, 32.14, 35.7, 31.2, 31.5, 26.2 (diff two Branches), 26.8_
    - Dependencies: 7.1
    - Deliverables: `POST /diffs` in `backend/internal/httpapi/diffs.go` and `openapi.yaml`; `web/features/diff/DiffPanel.tsx` (First_Divergence_Tick as a Timeline link, differing Inputs with Tick links, metric differences, causality statement, Disclaimer, Assumptions_Panel link); Branch_Tree two-Branch selection; divergence marker on the Timeline; tests
    - Verify: `cd backend && go test ./internal/httpapi -run TestDiffs && cd ../web && npm run gen:api && npx vitest --run features/diff features/timeline && npx tsc --noEmit`
    - Acceptance: the panel renders only server-reported differences.
  - [ ] 7.7 Implement the Ensemble_Runner arms and Seed handling
    - _Requirements: 24.1, 24.2, 24.3, 24.4, 24.5, 24.6, 24.7, 24.8, 24.9, 24.10, 24.11, 24.17, 22.5, 22.6, 22.7, 25.1, 25.4_
    - Dependencies: 6.12
    - Deliverables: `backend/internal/app/ensemble/runner.go` (explicit Seed list or base + count, 1..50, duplicates rejected before any run; Fixed_Replay with validation at the preceding window and Skipped_Commands with reason codes, parameters never altered; Adaptive_Evaluation with optional prefix Inputs_File; LLM arm rejected; Scenario hash and engine version mismatch rejected; memory store only), `backend/cmd/parallel-sim/ensemble.go` (`--scenario`, `--seeds`, `--arm fixed:<file>|rulebased[@startTick]`, `--prefix`, `--tie-tolerance`, `--out`), tests
    - Verify: `cd backend && go test ./internal/app/ensemble/... ./cmd/parallel-sim/...`
    - Acceptance: 51 Seeds and duplicate Seeds fail before simulating; an `llm` arm fails with unsupported-arm; the runner opens no database or network connection.
  - [ ] 7.8 Property test: fixed-replay round trip under the original Seed (required)
    - **Property 28: Fixed-replay round trip under original seed** (INV-16)
    - **Validates: Requirements 24.15**
    - Dependencies: 7.7
    - Deliverables: `backend/internal/app/ensemble/replay_roundtrip_prop_test.go` (`TestPropFixedReplayRoundTrip`)
    - Verify: `cd backend && go test ./internal/app/ensemble -run TestPropFixedReplayRoundTrip`
  - [ ] 7.9 Property test: ensemble parity (required)
    - **Property 30: Ensemble parity** (INV-17)
    - **Validates: Requirements 22.5**
    - Dependencies: 7.7
    - Deliverables: `backend/internal/app/ensemble/parity_prop_test.go` (`TestPropEnsembleParity`)
    - Verify: `cd backend && go test ./internal/app/ensemble -run TestPropEnsembleParity`
  - [ ] 7.10 Implement Ensemble_Report statistics and JSON/CSV output
    - _Requirements: 24.12, 24.13, 24.14, 25.2, 25.3, 25.5, 25.6, 25.7, 25.8, 25.9, 25.10, 31.6_
    - Dependencies: 7.7
    - Deliverables: `backend/internal/app/ensemble/report.go` (per-run rows in ascending Seed order, Skipped_Commands, arm labels and the human-versus-AI asymmetry statement, min/p10/median/mean/p90/max by nearest rank and half-to-even mean, per-Seed score differences, win/tie counts, header with Scenario hash, engine version, horizon, Comparison_Tick, Tie_Tolerance, Seed count, Disclaimer; canonical JSON and fixed-column CSV), `report_test.go`
    - Verify: `cd backend && go test ./internal/app/ensemble -run TestReport`
    - Acceptance: a hand-computed 5-Seed example matches every statistic; running with concurrency 1 and 8 writes identical bytes.
  - [ ] 7.11 Property test: ensemble determinism under any execution order (required)
    - **Property 29: Ensemble determinism under any execution order** (INV-16)
    - **Validates: Requirements 24.16**
    - Dependencies: 7.10
    - Deliverables: `backend/internal/app/ensemble/determinism_prop_test.go` (`TestPropEnsembleDeterminism`)
    - Verify: `cd backend && go test ./internal/app/ensemble -run TestPropEnsembleDeterminism`
  - [ ] 7.12 Property test: win/tie count conservation
    - **Property 31: Win/tie count conservation**
    - **Validates: Requirements 25.11**
    - Dependencies: 7.10
    - Deliverables: `backend/internal/app/ensemble/counts_prop_test.go` (`TestPropWinTieConservation`)
    - Verify: `cd backend && go test ./internal/app/ensemble -run TestPropWinTieConservation`
  - [ ]* 7.13 Property test: arm symmetry
    - **Property 32: Arm symmetry**
    - **Validates: Requirements 25.12**
    - Dependencies: 7.10
    - Deliverables: `backend/internal/app/ensemble/arm_symmetry_prop_test.go` (`TestPropArmSymmetry`)
    - Verify: `cd backend && go test ./internal/app/ensemble -run TestPropArmSymmetry`
  - [ ]* 7.14 Property test: paired identity
    - **Property 33: Paired identity**
    - **Validates: Requirements 25.13**
    - Dependencies: 7.10
    - Deliverables: `backend/internal/app/ensemble/identity_prop_test.go` (`TestPropPairedIdentity`)
    - Verify: `cd backend && go test ./internal/app/ensemble -run TestPropPairedIdentity`
  - [ ] 7.15 Measure the 50-Seed paired ensemble benchmark
    - _Requirements: 29.2, 29.8, 29.9, 29.10, 29.11_
    - Dependencies: 7.11
    - Deliverables: `backend/internal/app/ensemble/bench_test.go` (`BenchmarkPairedEnsemble50`: fixed demo plan versus Rule_Based_Manager, Seeds 1..50), updated `docs/benchmarks.md` entry with the full metadata of 29.8
    - Verify: `cd backend && go test ./internal/app/ensemble -run '^$' -bench BenchmarkPairedEnsemble50 -benchtime 3x -count 5`
    - Acceptance: the recorded figures are copied from this run; the 29.2 label changes from "target, not yet measured" to the measured value or the measured shortfall; invariant checks remain enabled.
  - [ ] 7.16 Calibrate the Flagship_Scenario and write `docs/calibration.md`
    - _Requirements: 36.1, 36.2, 36.3, 36.4, 36.5, 36.6, 36.7, 36.8, 36.9, 36.10, 3.9, 34.9_
    - Dependencies: 7.15
    - Deliverables: `backend/internal/app/calibration/calibration_test.go` (`TestCalibrationGates`: stockout with open orders in the no-Command run; Rule_Based_Manager Composite_Score above the no-Command run, default Seed); `backend/cmd/parallel-sim/calibrate.go` or a test helper that prints Composite_Scores of the no-Command run, the Rule_Based_Manager run and the recorded human demo plan over Seeds 1..20; final Profit_Floor and Profit_Target in `black-friday.json`; regenerated `demo-plan.json` and golden files if values move; `docs/calibration.md` (every Assumption with value, unit, bounds and rationale; gates, Seeds and results; every change with previous value, new value and reason; Crisis_Tick, Demo_Fork_Tick, Demo_Commands with reasons; Profit_Floor and Profit_Target rationale; statement that no Demo_Path or Duel outcome is a calibration target; Disclaimer)
    - Verify: `cd backend && go test ./internal/app/calibration/... ./internal/app/demoplan/... ./internal/sim -run 'TestCalibrationGates|TestGoldenFlagshipHash' && go test ./...`
    - Acceptance: gates exist only on the no-Command and Rule_Based_Manager runs; no Assumption is changed to make the AI or the human win; the 20-Seed scores are reported as observed results without a target; any engine or scenario change updates the golden hashes in the same commit with the reason recorded.
  - [ ] 7.17 Commit the golden 20-Seed Ensemble_Report
    - _Requirements: 25.14_
    - Dependencies: 7.16
    - Deliverables: `backend/scenarios/golden/ensemble-flagship-20.json` and `.csv` (demo human Inputs_File as fixed plan versus Rule_Based_Manager, Seeds 1..20), `backend/internal/app/ensemble/golden_test.go` (`TestGoldenEnsembleReport`)
    - Verify: `cd backend && go test ./internal/app/ensemble -run TestGoldenEnsembleReport && go run ./cmd/parallel-sim ensemble --scenario scenarios/black-friday.json --seeds 1..20 --arm fixed:scenarios/demo-duel-human.json --arm rulebased --out /tmp/ens && cmp /tmp/ens.json scenarios/golden/ensemble-flagship-20.json`
    - Acceptance: the report reproduces byte for byte on amd64 and arm64 in CI.
  - [ ] 7.18 Checkpoint — M6 complete
    - Run `cd backend && go test ./...` and `cd web && npx tsc --noEmit && npx vitest --run`. Ensure all tests pass, ask the user if questions arise.

- [ ] 8. M7 — Integration, visual polish, testing and deployment (~300 credits)
  - [ ] 8.1 Harden the HTTP boundary
    - _Requirements: 26.3, 26.4, 26.5, 15.5, 27.5, `security.md` HTTP and resource-limit rules_
    - Dependencies: 7.18
    - Deliverables: `backend/internal/httpapi/middleware.go` (CSP without `unsafe-eval` in production, `X-Content-Type-Options`, `Referrer-Policy`, CORS to `WEB_ORIGIN`, per-IP rate limit, names limited to 64 printable characters), `middleware_test.go`; matching headers in `web/next.config.ts`
    - Verify: `cd backend && go test ./internal/httpapi -run 'TestSecurityHeaders|TestRateLimit|TestNameValidation' && cd ../web && npm run build`
    - Acceptance: tests assert each header, a 429 after the rate limit, and a 400 for a 65-character or non-printable name.
  - [ ] 8.2 Add API performance benchmarks and record them
    - _Requirements: 29.3, 29.4, 29.5, 29.6, 29.7, 29.8, 29.10, 29.11_
    - Dependencies: 7.18
    - Deliverables: `backend/internal/httpapi/bench_test.go` (State read, fork, diff at horizon, advance to horizon, 120-Tick Tick_Frames over the PostgreSQL store), updated `docs/benchmarks.md`
    - Verify: `cd backend && PARALLEL_TEST_DATABASE_URL=postgres://parallel:parallel@localhost:5432/parallel_test?sslmode=disable go test ./internal/httpapi -run '^$' -bench . -benchtime 20x -count 5`
    - Acceptance: each budget shows a measured value with method and hardware, or remains "target, not yet measured" with the reason; no correctness check is disabled.
  - [ ] 8.3 Add the Playwright Demo_Path end-to-end test
    - _Requirements: 34.1, 34.2, 34.4, 34.5, 34.7, 34.8, 33.6, 33.7_
    - Dependencies: 8.1
    - Deliverables: `web/playwright.config.ts` (starts the API with the memory store and no LLM variables, and the built web app, via `webServer`), `web/e2e/demo-path.spec.ts` (uses `web/e2e/fixtures/demo-plan.json`; waits 2 s before each action; "disruption onsets only" mode; rewind, fork, submit Demo_Commands, play both Branches to the horizon; asserts total time < 60 s, horizon hash equals a second run, HUD values equal API responses; asserts no Net_Profit value, no difference sign and no winner)
    - Verify: `cd web && npx playwright install chromium && npx playwright test e2e/demo-path.spec.ts`
    - Acceptance: the test passes headless and finishes under 60 s of real time.
  - [ ] 8.4 Add deployment artifacts
    - _Requirements: 28.2, 20.10, `security.md` secrets and migration-role rules_
    - Dependencies: 8.1
    - Deliverables: `backend/Dockerfile` (multi-stage, non-root, `parallel-api` binary and migrations), a separate migration entry point using the migration role, `web/Dockerfile` or static-host config, `docs/deploy.md` with environment variables (placeholders only)
    - Verify: `docker build -t parallel-api ./backend && docker build -t parallel-web ./web`
    - Acceptance: images build; no secret or model identifier is baked in; the runtime role lacks DDL rights. Deploying to the demo host is a manual step for the user.
  - [ ] 8.5 Checkpoint — MVP acceptance gate before polish
    - Run `cd backend && go test ./... && go vet ./...`, the PostgreSQL suite, `cd web && npx tsc --noEmit && npx eslint . && npx vitest --run && npx playwright test`. Polish starts only when every MVP test passes. Ensure all tests pass, ask the user if questions arise.
  - [ ] 8.6 Implement the fork split transition
    - _Requirements: 38.1, 38.3, 38.4_
    - Dependencies: 8.5
    - Deliverables: `web/features/branches/ForkTransition.tsx` (CSS or PixiJS ticker; position, opacity and scale only; ≤ 600 ms; final state under reduced motion), tests
    - Verify: `cd web && npx vitest --run features/branches && npx tsc --noEmit`
    - Acceptance: tests assert the duration bound, the reduced-motion path and that no numeric text changes during the animation.
  - [ ] 8.7 Implement the duel result reveal
    - _Requirements: 38.2, 38.3, 38.4_
    - Dependencies: 8.5
    - Deliverables: `web/features/duel/ScoreReveal.tsx` (≤ 1200 ms; numbers rendered at final API values from the first frame; reduced-motion final state), tests
    - Verify: `cd web && npx vitest --run features/duel && npx tsc --noEmit`
    - Acceptance: tests assert that every number shown at every animation frame equals the final API value.
  - [ ] 8.8 Checkpoint — M7 complete
    - Run the full backend, web and Playwright suites. Ensure all tests pass, ask the user if questions arise.

- [ ] 9. M8 — Documentation and hackathon submission (~150 credits)
  - [ ] 9.1 Write the README setup, run and limitations sections
    - _Requirements: 31.6 (Disclaimer), `security.md` known limitations, `hackathon.md` setup instructions_
    - Dependencies: 8.8
    - Deliverables: `README.md` (stack, prerequisites, `docker compose up -d db`, migrations, `go run ./cmd/parallel-api`, `npm run dev`, CLI `run`/`verify`/`ensemble` examples, optional LLM configuration through OpenRouter with no default model, known limitations: no authentication, unguessable IDs, provider sees the Observation, human can read the script file; Disclaimer; MIT license)
    - Verify: `npx --yes markdownlint-cli2 README.md && cd backend && go run ./cmd/parallel-sim run --scenario scenarios/black-friday.json --seed 2026`
    - Acceptance: every command in the README exits 0 on a fresh clone with Docker, Go and Node installed (long-running servers are started manually by the user).
  - [ ] 9.2 Write the demo script and a consistency test
    - _Requirements: 34.9, 36.10_
    - Dependencies: 8.8
    - Deliverables: `docs/demo-script.md` (steps under 3 minutes; Demo_Fork_Tick and Demo_Commands copied from `demo-plan.json`; no scripted winner), `backend/internal/app/demoplan/docs_test.go` asserting the script's fork Tick and Commands equal the fixture
    - Verify: `cd backend && go test ./internal/app/demoplan -run TestDemoScriptMatchesPlan`
    - Acceptance: the test fails when the fixture and the script disagree.
  - [ ] 9.3 Finalize the Kiro build log and evidence checklist
    - _Requirements: `hackathon.md` logging rule_
    - Dependencies: 9.1, 9.2
    - Deliverables: `docs/kiro-build-log.md` entries for each real implementation session with commits and verification outcomes; evidence checklist ticked only for evidence that exists
    - Verify: `npx --yes markdownlint-cli2 docs/kiro-build-log.md && git log --oneline | head -n 20`
    - Acceptance: every logged claim maps to a commit or command output; nothing done outside Kiro is labelled as Kiro work.
  - [ ] 9.4 Final checkpoint — submission ready
    - Run the full backend, web and Playwright suites. Recording the video and submitting to Devpost are manual steps for the user. Ensure all tests pass, ask the user if questions arise.

- [ ] 10. Stretch — optional, budget-gated (start only after 9.4 and only with credits left outside the 500-credit reserve)
  - [ ] 10.1 Large ensembles and distribution views
    - _Requirements: 39.1, 39.2_
    - Dependencies: 9.4
    - Deliverables: `--large` flag in `backend/cmd/parallel-sim/ensemble.go` raising the Seed cap to 1000; `web/features/ensemble/` histogram and paired-difference chart reading an Ensemble_Report file; tests
    - Verify: `cd backend && go test ./internal/app/ensemble/... && cd ../web && npx vitest --run features/ensemble && npx tsc --noEmit`
    - Acceptance: 1000 Seeds accepted with the flag and rejected without it; charts plot only report values.
  - [ ]* 10.2 Property test: large ensemble determinism
    - Requirement-level property (no design property number)
    - **Validates: Requirements 39.3**
    - Dependencies: 10.1
    - Verify: `cd backend && go test ./internal/app/ensemble -run TestPropLargeEnsembleDeterminism`
  - [ ] 10.3 Per-tick branch diff visualization
    - _Requirements: 40.1, 40.2_
    - Dependencies: 10.1
    - Deliverables: per-tick series in `backend/internal/app/diff_service.go`, `GET` endpoint and OpenAPI entry, chart in `web/features/diff/` with Input markers and the causality statement; tests
    - Verify: `cd backend && go test ./internal/app -run TestPerTickDiff && cd ../web && npx vitest --run features/diff && npx tsc --noEmit`
    - Acceptance: series values equal per-Tick metrics of each Branch.
  - [ ] 10.4 Optimization-based Staffing_Advisor
    - _Requirements: 41.1, 41.2, 41.3, 41.5, 41.6, 41.7_
    - Dependencies: 9.4
    - Deliverables: `backend/internal/policy/advisor/advisor.go` (pure, Observation-only forecast, bounded exact search with optimality label or "best found, not proven optimal"), recommendation endpoint and Decision_Panel display; tests
    - Verify: `cd backend && go test ./internal/policy/advisor/... ./internal/archtest/...`
    - Acceptance: recommendations pass `sim.Validate`; labels match the requirement text.
  - [ ]* 10.5 Property tests: advisor no-privileged-information and optimality check
    - Requirement-level properties (no design property number)
    - **Validates: Requirements 41.4, 41.8**
    - Dependencies: 10.4
    - Verify: `cd backend && go test ./internal/policy/advisor -run 'TestPropAdvisorNoPrivilegedInfo|TestPropAdvisorOptimality'`
  - [ ] 10.6 Additional Disruption kinds
    - _Requirements: 42.1, 42.2_
    - Dependencies: 10.3
    - Deliverables: `carrier_delay` and `payment_outage` in `backend/internal/sim/disruptions.go` behind Scenario enablement, bumped engine version, tests; the golden files stay unchanged for Scenarios that do not enable the kinds
    - Verify: `cd backend && go test ./internal/sim/... -run 'TestCarrierDelay|TestPaymentOutage|TestGolden'`
    - Acceptance: core properties of M1 still pass with the new kinds in generators.
  - [ ] 10.7 Shareable read-only replays
    - _Requirements: 43.1, 43.2, 43.3, 43.5_
    - Dependencies: 10.6
    - Deliverables: share-link table migration, `POST /worlds/{id}/share`, server-side 403 on every mutation route, World id omitted from responses; tests
    - Verify: `cd backend && go test ./internal/httpapi -run TestReadOnlyLink`
    - Acceptance: every mutation endpoint returns 403 through a share link.
  - [ ]* 10.8 Property test: read-only enforcement on every mutation
    - Requirement-level property (no design property number)
    - **Validates: Requirements 43.4**
    - Dependencies: 10.7
    - Verify: `cd backend && go test ./internal/httpapi -run TestPropReadOnlyEnforcement`

## Notes

- Optional sub-tasks are marked `*`: 2.8, 2.28, 2.29, 3.10, 7.3, 7.5, 7.13, 7.14, 10.2, 10.5, 10.8. They can be skipped to save credits. Every other test sub-task is required, including determinism (P5, P39), replay equivalence (P8, P38), fork fidelity, independence and prefix immutability (P9, P10, P37), ledger balance (P15), cash reconciliation (P16), inventory and order conservation (P13, P14), valuation and balance identity (P19, P20), validation gate (P22), duel parity and exogenous fairness (P21), draw-count independence (P12), presentation independence (P7), metadata independence (P6), the architecture import test, the Input_Log immutability test, the LLM independence test and the golden hash and ensemble tests.
- All 41 design properties have a sub-task: P1 2.7, P2 2.8, P3 3.12, P4 2.11, P5 2.21, P6 2.38, P7 2.39, P8 4.5, P9 2.41, P10 2.42, P11 2.37, P12 2.23, P13 2.24, P14 2.25, P15 2.26, P16 2.27, P17 2.28, P18 2.29, P19 2.30, P20 2.31, P21 6.8, P22 2.36, P23 6.2, P24 7.2, P25 7.3, P26 7.4, P27 7.5, P28 7.8, P29 7.11, P30 7.9, P31 7.12, P32 7.13, P33 7.14, P34 3.8, P35 3.9, P36 3.10, P37 2.43, P38 4.6, P39 2.22, P40 3.3, P41 2.45.
- Credit plan: M0 150, M1 900, M2 600, M3 350, M4 500, M5 400, M6 350, M7 300, M8 150, total 3500. The 500-credit reserve is not planned. If a milestone overruns, cut optional sub-tasks first, then stretch, never the required invariant tests.
- Requirements 37.13 and 37.14 are partly built in 2.15 (wage lines with zero overtime and temp hours) and completed in 3.2 when the staffing Commands exist. Requirement 6.4 is first covered in 3.16 with the demo-fork Inputs and extended in 6.10 with the demo duel Inputs.
- Manual steps outside these tasks: starting long-running servers (`cd backend && go run ./cmd/parallel-api`, `cd web && npm run dev`), deploying to the demo host, recording the demo video and submitting to Devpost.
- Results are synthetic. Every API response with metrics, every CLI report, `docs/calibration.md` and every results screen carry the Disclaimer.

### M0/M1 implementation notes

- Optional tests skipped (left unchecked): 2.8, 2.28, 2.29. Parent task 2 is auto-marked complete by tooling despite them.
- The early benchmark took about 17 minutes wall clock.
- Deviations from plan:
  - `OrderBook.Settled` was added to `State`.
  - Scenario fields `openingCashCents` and `demand.baseConversionBps` were added.
  - The supplier role for an outage was stored in `ActiveDisruption.MagnitudeBps` (tech debt). Fixed in 3.5: `ActiveDisruption.Role` (encoded) and `sim.EngineVersion` = 2. The Seed 2026 no-command hash changed from `96e7d8d9...bce08` to `4c6d0dd0...ab99e`.
  - SSE is not in the slice.
  - Local ports: web 3100, API 8080 (3000 and 8000 are used by other local apps).
  - The demo-reproducibility property test takes about 60 s.
  - The numbers in `docs/benchmarks.md` are an in-process proxy.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["1.2", "1.3", "1.4"] },
    { "id": 2, "tasks": ["1.5"] },
    { "id": 3, "tasks": ["1.6"] },
    { "id": 4, "tasks": ["2.1", "2.2"] },
    { "id": 5, "tasks": ["2.3", "2.4"] },
    { "id": 6, "tasks": ["2.5"] },
    { "id": 7, "tasks": ["2.6"] },
    { "id": 8, "tasks": ["2.7", "2.8", "2.9"] },
    { "id": 9, "tasks": ["2.10"] },
    { "id": 10, "tasks": ["2.11", "2.12"] },
    { "id": 11, "tasks": ["2.13"] },
    { "id": 12, "tasks": ["2.14"] },
    { "id": 13, "tasks": ["2.15"] },
    { "id": 14, "tasks": ["2.16"] },
    { "id": 15, "tasks": ["2.17"] },
    { "id": 16, "tasks": ["2.18"] },
    { "id": 17, "tasks": ["2.19"] },
    { "id": 18, "tasks": ["2.20", "2.32", "2.34"] },
    { "id": 19, "tasks": ["2.21", "2.22", "2.23", "2.24", "2.25", "2.26", "2.27", "2.28", "2.29", "2.30", "2.31", "2.35"] },
    { "id": 20, "tasks": ["2.33", "2.36", "2.37", "2.38", "2.39"] },
    { "id": 21, "tasks": ["2.40"] },
    { "id": 22, "tasks": ["2.41", "2.42", "2.43", "2.44", "2.46"] },
    { "id": 23, "tasks": ["2.45", "2.47"] },
    { "id": 24, "tasks": ["2.48"] },
    { "id": 25, "tasks": ["2.49"] },
    { "id": 26, "tasks": ["2.50", "2.51", "2.52", "2.53"] },
    { "id": 27, "tasks": ["2.54"] },
    { "id": 28, "tasks": ["2.55"] },
    { "id": 29, "tasks": ["3.1"] },
    { "id": 30, "tasks": ["3.2"] },
    { "id": 31, "tasks": ["3.3", "3.4"] },
    { "id": 32, "tasks": ["3.5"] },
    { "id": 33, "tasks": ["3.6", "3.7", "3.15"] },
    { "id": 34, "tasks": ["3.8", "3.9", "3.10", "3.11"] },
    { "id": 35, "tasks": ["3.12", "3.13"] },
    { "id": 36, "tasks": ["3.14", "3.16"] },
    { "id": 37, "tasks": ["3.17"] },
    { "id": 38, "tasks": ["4.1"] },
    { "id": 39, "tasks": ["4.2"] },
    { "id": 40, "tasks": ["4.3", "4.4"] },
    { "id": 41, "tasks": ["4.5", "4.6", "4.7"] },
    { "id": 42, "tasks": ["4.8"] },
    { "id": 43, "tasks": ["4.9"] },
    { "id": 44, "tasks": ["5.1"] },
    { "id": 45, "tasks": ["5.2", "5.3"] },
    { "id": 46, "tasks": ["5.4", "5.5"] },
    { "id": 47, "tasks": ["5.6"] },
    { "id": 48, "tasks": ["5.7"] },
    { "id": 49, "tasks": ["5.8"] },
    { "id": 50, "tasks": ["6.1", "6.4"] },
    { "id": 51, "tasks": ["6.2", "6.3"] },
    { "id": 52, "tasks": ["6.5"] },
    { "id": 53, "tasks": ["6.6", "6.7"] },
    { "id": 54, "tasks": ["6.8", "6.9", "6.10"] },
    { "id": 55, "tasks": ["6.11"] },
    { "id": 56, "tasks": ["6.12"] },
    { "id": 57, "tasks": ["7.1", "7.7"] },
    { "id": 58, "tasks": ["7.2", "7.3", "7.4", "7.5", "7.6", "7.8", "7.9", "7.10"] },
    { "id": 59, "tasks": ["7.11", "7.12", "7.13", "7.14"] },
    { "id": 60, "tasks": ["7.15"] },
    { "id": 61, "tasks": ["7.16"] },
    { "id": 62, "tasks": ["7.17"] },
    { "id": 63, "tasks": ["7.18"] },
    { "id": 64, "tasks": ["8.1", "8.2"] },
    { "id": 65, "tasks": ["8.3", "8.4"] },
    { "id": 66, "tasks": ["8.5"] },
    { "id": 67, "tasks": ["8.6", "8.7"] },
    { "id": 68, "tasks": ["8.8"] },
    { "id": 69, "tasks": ["9.1", "9.2"] },
    { "id": 70, "tasks": ["9.3"] },
    { "id": 71, "tasks": ["9.4"] },
    { "id": 72, "tasks": ["10.1", "10.4"] },
    { "id": 73, "tasks": ["10.2", "10.3", "10.5"] },
    { "id": 74, "tasks": ["10.6"] },
    { "id": 75, "tasks": ["10.7"] },
    { "id": 76, "tasks": ["10.8"] }
  ]
}
```
