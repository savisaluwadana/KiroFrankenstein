# Kiro build log

**Purpose:** Maintain an accurate record of **actual** Kiro-assisted engineering for the hackathon submission.

> The initial steering, requirements, design, and tasks were prepared as repository scaffolding outside Kiro. Do not represent them as generated or executed by Kiro. Start logging only after using Kiro itself.
>
> **Provenance of the first commits.** `git log --reverse --oneline` starts with `8cb6215` (initial commit), `2a282fa` (MIT license), `5c456a6` and `9abdaf5`, all dated 2026-11-10. `5c456a6` added steering, an "incident-stitcher" spec and an earlier build log; `9abdaf5` removed that concept because it was not approved. These four commits were scaffolding for a concept that no longer exists and are not Kiro evidence. The Parallel steering and spec in `.kiro/` are Kiro work from 2026-11-26 (the entries below); the first implementation commits are `ddc7c1d` (M0 scaffolding) and `d44bf1b` (M1 vertical slice), also 2026-11-26.

## Build sessions

### 2026-11-26 — Parallel concept approved; steering and requirements spec

- **Kiro surface/version:** Kiro CLI (spec workflow, requirements-first)
- **Goal:** Turn the approved "Parallel" concept into Kiro steering and an MVP requirements document before any code.
- **Inputs:** User concept brief (tagline, 10 principles, stack, scope exclusions), `.kiro/steering/hackathon.md`, existing README.
- **What Kiro actually did:**
  - Created steering: `.kiro/steering/product.md`, `tech.md`, `structure.md`, `simulation-invariants.md`, `security.md`, `ui-design-system.md`.
  - Updated `.kiro/steering/hackathon.md` to record that the concept was approved.
  - Created `.kiro/specs/parallel-simulation-mvp/requirements.md` (24 MVP requirements, 6 stretch requirements, EARS acceptance criteria, risks and open questions) and `.config.kiro`.
  - Proposed simplifications: integer-only engine arithmetic for cross-architecture determinism, input log as the record with derived state, turn-based duel decision windows for parity, named random streams for fair duels, GSAP deferred to stretch, SSE for progress only.
- **What I reviewed or corrected:** Pending user review of steering and requirements.
- **Code/tests produced:** None. No application code was written in this session.
- **Commands and verification outcomes:** Spec format validation of `requirements.md` passed.
- **Learning / next action:** Review requirements and open questions; then generate `design.md` (module boundaries, API contracts, data model, correctness properties) and `tasks.md`.

#### 2026-11-26 (later) — Requirements revised from user feedback

- **Kiro surface/version:** Kiro CLI (spec workflow, requirements-first, revision pass)
- **Goal:** Apply the user's targeted refinements to the MVP requirements before design.
- **What Kiro actually did:**
  - Revised `.kiro/specs/parallel-simulation-mvp/requirements.md` (now 32 MVP requirements, 1 priority-polish requirement, 5 stretch requirements, renumbering table): promoted a minimal branch diff and a minimal CLI multi-seed ensemble (fixed replay vs adaptive heuristic, skipped-command handling, paired seeds, distributions and win/tie counts) to the MVP; added a hybrid discrete-event time model, precise financial and service metric definitions, canonical state hash scope, comparison parity, assumptions transparency and a synthetic-results disclaimer; kept the staffing advisor, extra disruptions and read-only sharing as stretch with server-side read-only enforcement; moved fork and duel-reveal animations to a priority-polish tier.
  - Recorded contradictions found and resolved (price-dependent demand vs exogenous fairness, decision-window resume rule, GSAP and motion-duration conflicts, count-up reveals, hash scope vs fork fidelity, assumption exposure vs hidden disruption script, ensemble performance budget) and updated open questions and the property-test candidate list.
  - Made minimal consistent updates to steering: `product.md`, `tech.md`, `simulation-invariants.md` (INV-12 wording, new INV-15 to INV-17), `security.md`, `ui-design-system.md`.
- **What I reviewed or corrected:** Pending user review of the revised requirements.
- **Code/tests produced:** None. No application code, design or tasks were written.
- **Commands and verification outcomes:** Spec format validation of `requirements.md` passed.
- **Learning / next action:** Resolve the open questions (score weights and tie tolerance, performance budgets, flagship calibration gates), then generate `design.md`.

#### 2026-11-26 (later) — Second requirements revision: final constraints

- **Kiro surface/version:** Kiro CLI (spec workflow, requirements-first, revision pass)
- **Goal:** Apply the user's final constraints (demo pacing, vertical slice, financial policies, rule-based vs LLM manager, calibration integrity, early studio UI, credit budget) to the approved requirements.
- **What Kiro actually did:**
  - Revised `.kiro/specs/parallel-simulation-mvp/requirements.md` in place (now 37 MVP requirements, 1 priority-polish requirement, 5 stretch requirements): added a vertical-slice tier and build order, a project-constraints section (credit budget), playback/stepping/auto-pause (33), a 60-second crisis-and-fork demo path (34), operational visualization of bottlenecks, interventions and divergence (35), transparent flagship calibration with no human-vs-AI gate (36, replacing 3.7 and 3.8), and financial policies with moving-average inventory cost, revenue at shipment, no refunds on cancellation, hourly wages and end-of-horizon treatment (37).
  - Renamed the Heuristic_Manager to Rule_Based_Manager, added Manager_Provenance (including recorded LLM fallback) and absent-API-key behaviour; added fork-opened decision windows, Run_To_Tick advancement, Tick_Frames and new performance budgets; recorded the contradictions resolved in this revision and updated open questions and property-test candidates.
  - Made minimal consistent steering updates: `product.md`, `tech.md`, `structure.md`, `simulation-invariants.md` (INV-18, INV-19), `security.md`, `ui-design-system.md`, `hackathon.md`.
- **What I reviewed or corrected:** Pending user review of the revised requirements.
- **Code/tests produced:** None. No application code, design or tasks were written.
- **Commands and verification outcomes:** Spec format validation of `requirements.md` passed.
- **Learning / next action:** Confirm the new open questions (playback rates, Demo_Path details, temporary wage policy, LLM provider), then generate `design.md`.

#### 2026-11-26 (later) — Design document generated

- **Kiro surface/version:** Kiro CLI (spec workflow, requirements-first, design phase)
- **Goal:** Turn the approved MVP requirements and the steering set into a design document, before any tasks or application code.
- **Inputs:** `.kiro/specs/parallel-simulation-mvp/requirements.md` (approved in principle) and `.kiro/steering/` (`product.md`, `tech.md`, `structure.md`, `simulation-invariants.md`, `security.md`, `ui-design-system.md`, `hackathon.md`).
- **What Kiro actually did:**
  - Created `.kiro/specs/parallel-simulation-mvp/design.md`: modular-monolith architecture with dependency direction and package boundaries; concrete Go signatures for the pure core (`State`, `Event`, `EventQueue`, `Command`, `Observation`, `Init`/`StepTick`, `Hash`/`Encode`/`Decode`, `Validate`, `ComputeMetrics`, `Manager` interface, app ports); simulation algorithms (demand/conversion, order lifecycle, fulfilment capacity, purchase orders and supplier failure, workforce, rule-based reorder-point/capacity logic); event-ordering rules with a fixed within-tick minute-offset table for engine version 1; randomness strategy (project-owned SplitMix64, named streams, common random numbers, draw-count independence); double-entry ledger design with per-event posting rules, moving-average cost and metric formulas; snapshot verify-by-hash and fork-as-input-prefix algorithms; canonical state hashing and excluded fields; concrete PostgreSQL DDL (Goose up/down, append-only `input_log` trigger, snapshots, tick summaries) with the in-memory slice store behind the same ports; OpenAPI endpoint set, idempotency, limits and SSE semantics; AI provider abstraction (rule-based + LLM adapter, strict JSON, validator gate, timeout/fallback provenance); frontend studio architecture; a 33-property Correctness Properties section traceable to requirement criteria and invariants INV-1..INV-19; error-handling map to problem+json; a testing strategy (rapid property tests, golden amd64/arm64 hash, architecture import test, contract tests, Playwright demo timing); and dependency-ordered milestones (vertical slice first, then MVP core, polish, stretch) with rough Kiro-credit estimates inside the 4000/500-reserve budget.
  - Ran the acceptance-criteria testing prework to classify every criterion (PROPERTY/EXAMPLE/EDGE_CASE/INTEGRATION/SMOKE) and consolidate redundant properties before writing the Correctness Properties section.
  - Recorded resolutions for open questions 1, 2, 7, 8, 9 and 11 in the design, and listed the remaining risks.
- **What I reviewed or corrected:** Pending user review of the design.
- **Code/tests produced:** None. No application code or tasks were written in this session.
- **Commands and verification outcomes:** Spec format validation of `design.md` passed with no issues.
- **Learning / next action:** On approval, generate `tasks.md` ordered to build the vertical slice first.

#### 2026-11-26 (later) — Third requirements revision: user resolutions of open questions

- **Kiro surface/version:** Kiro CLI (spec workflow, requirements-first, revision pass)
- **Goal:** Apply the user's resolutions on duel scoring, playback, the flagship demo fork, temporary workforce, the LLM provider, snapshot and fork correctness, and performance benchmarking.
- **What Kiro actually did:**
  - Revised `.kiro/specs/parallel-simulation-mvp/requirements.md` in place with stable numbering (criteria appended only): a 100-point Composite_Score in integer hundredths with default weights 50/30/20 % and opponent-independent normalization (1.8, 1.9, 3.9, 21.10 to 21.15); fast-forward to the next decision window or disruption onset (33.16, 33.17); a calibrated Demo_Fork_Tick and Demo_Commands with no scripted outcome (34.6 to 34.9, 36.10); temporary worker onboarding (9.8, 9.9); OpenRouter as initial provider, no default model, per-world LLM call budget and strict schema fallback (20.13 to 20.18); complete snapshots, fork prefix immutability and same-minute and tick-boundary ordering (5.8, 5.9, 14.6, 15.8, 17.7, 18.7, 28.6); and benchmark reporting in `docs/benchmarks.md` with budgets kept as targets until measured (29.8 to 29.11). Marked open questions 3, 5, 9, 10, 11 and 12 resolved.
  - Made minimal steering updates: `product.md`, `tech.md`, `structure.md`, `security.md`, `simulation-invariants.md`.
- **What I reviewed or corrected:** Pending user review. `design.md` predates this revision and needs a matching update (composite score, LLM configuration, ordering, snapshot contents).
- **Code/tests produced:** None. No application code, design changes or tasks were written.
- **Commands and verification outcomes:** Spec format validation of `requirements.md` passed with no issues.
- **Learning / next action:** Update `design.md` to revision 4, then generate `tasks.md`.

#### 2026-11-26 (later) — Design revised to match requirements revision 4

- **Kiro surface/version:** Kiro CLI (spec workflow, requirements-first, design revision pass)
- **Goal:** Bring `design.md` in line with the revision-4 requirements, before any tasks or application code.
- **Inputs:** `.kiro/specs/parallel-simulation-mvp/requirements.md` (revision 4) and the steering set.
- **What Kiro actually did:**
  - Updated `.kiro/specs/parallel-simulation-mvp/design.md` in place: replaced the stale Composite_Score formula with the 100-point integer-hundredths score (financial clamp against Profit_Floor/Profit_Target, fill-rate and service-level components, default 5000/3000/2000 bps weights, Tie_Tolerance 10 hundredths, opponent-independent, MulDiv/RoundHalfEven) and added a `ComputeScore` signature and a corrected Scenario JSON example; made the `State` struct snapshot-complete (Event_Queue sequence counter, order/PO id counters, full SplitMix64 stream state); separated same-minute disruption expiries from activations and added a numbered tick-boundary sequence (Req 5.8, 5.9); spelled out the fork input-prefix query predicate and parent-append invisibility (Req 15.8); rewrote the LLM section for the provider-independent OpenAI-compatible adapter (OpenRouter initial, no default model, per-window/per-world budgets, 20 s timeout, strict structured-output validation, recorded fallback reasons, distinct provenance); added a calibration-derived Demo_Path subsection with no asserted outcome; expanded playback to stepping and fast-forward; added Correctness Properties 34–41 (score bounds/competitor-independence/monotonicity, fork prefix immutability, snapshot completeness/resume, same-minute ordering, temp-worker wage/capacity, demo reproducibility); refreshed the early-benchmark testing strategy; and replaced the milestones with the M0–M8 structure (≤ 3500 planned, 500 reserve, stretch listed separately) for task generation. Updated open-question resolutions and the remaining risks.
  - Ran the acceptance-criteria testing prework for the new/updated criteria before writing the added properties.
- **What I reviewed or corrected:** Pending user review of the revised design.
- **Code/tests produced:** None. No application code or tasks were written in this session.
- **Commands and verification outcomes:** Spec format validation of `design.md` passed with no issues.
- **Learning / next action:** On approval, generate `tasks.md` ordered to build M0 then the M1 vertical slice first.

#### 2026-11-26 (later) — Implementation plan (tasks.md) generated

- **Kiro surface/version:** Kiro CLI (spec workflow, requirements-first, task phase)
- **Goal:** Turn the approved requirements (revision 4) and design into a dependency-ordered implementation plan, before any application code.
- **Inputs:** `.kiro/specs/parallel-simulation-mvp/requirements.md`, `design.md` and the steering set.
- **What Kiro actually did:**
  - Created `.kiro/specs/parallel-simulation-mvp/tasks.md` with milestones M0 to M8 and per-milestone credit estimates (150, 900, 600, 350, 500, 400, 350, 300, 150; 3500 planned, 500 reserve untouched), followed by a separate, optional, budget-gated stretch section (Requirements 39 to 43).
  - Each task lists requirements covered, dependencies, deliverables, non-watch verification commands and acceptance criteria. M1 builds the vertical slice end to end (engine, in-memory store, API, PixiJS studio with playback) and runs the early benchmark into `docs/benchmarks.md` as soon as the engine replays. Calibration into `docs/calibration.md` is in M6 after the ensemble benchmark, with gates only on the no-Command and rule-based runs.
  - Mapped all 41 design properties to their own sub-tasks next to the code they test; core invariant tests are required and 11 non-core test sub-tasks are marked optional. Added milestone checkpoints, an MVP acceptance gate before polish, and a task dependency graph.
- **What I reviewed or corrected:** Pending user review of the task plan.
- **Code/tests produced:** None. No application code was written in this session.
- **Commands and verification outcomes:** Spec format validation of `tasks.md` passed with no issues.
- **Learning / next action:** Review `tasks.md`, then start task 1.1.

#### 2026-11-26 — M0 + M1 implementation (vertical slice)

- **Kiro surface/version:** Kiro IDE agent orchestrating spec-task-execution subagents
- **Goal:** Implement milestones M0 (scaffolding) and M1 (vertical slice: engine, in-memory store, API, PixiJS studio with playback) from `tasks.md`.
- **Inputs:** `.kiro/specs/parallel-simulation-mvp/tasks.md`, `design.md`, and the steering set.
- **What Kiro actually did:**
  - Executed tasks 1.1 to 1.6 and 2.1 to 2.55, except the optional tasks 2.8, 2.28 and 2.29, which were intentionally skipped.
  - One subagent run was interrupted by a network failure during task 2.33 and was re-run.
- **What I reviewed or corrected:** Verified the Docker image pull manually. Fixed a staticcheck unused-field finding.
- **Code/tests produced:** Backend (`backend/`), web app (`web/`), docs. Committed as `feat: M1 vertical slice (engine, API, studio, tests)`.
- **Commands and verification outcomes:**
  - `go vet`, `go test ./...`, `tsc --noEmit`, `eslint`, `vitest` (54 tests) and `next build` passed.
  - Live acceptance: demo plan crisisTick 19 and forkTick 20. Parent hashes for ticks 0..20 were unchanged after the fork and after both branches advanced. The child hash at tick 20 equals the parent's. A fresh-world replay gave identical hashes for 121 ticks. The `parallel-sim` final hash equals the API parent hash at tick 120.
- **Known issues:** `OrderBook.Settled` and the scenario fields `openingCashCents` and `demand.baseConversionBps` deviate from the plan. The outage supplier role is stored in `ActiveDisruption.MagnitudeBps` (tech debt). SSE is not in the slice. The benchmark numbers in `docs/benchmarks.md` are an in-process proxy. The PixiJS view has not been manually browser-tested by the agent. The demo-reproducibility property test takes about 60 s.
- **Learning / next action:** Start M2.

### 2026-11-27 — M2 + M3 implementation

- **Kiro surface/version:** Kiro IDE agent orchestrating spec-task-execution subagents
- **Goal:** Implement M2 (complete engine and ledger) and M3 (PostgreSQL persistence, snapshots, replay, engine-version flags) from `tasks.md`.
- **Inputs:** `.kiro/specs/parallel-simulation-mvp/tasks.md`, `design.md`, steering files.
- **What Kiro actually did:**
  - Executed tasks 3.1 to 3.17 except the optional 3.10, and tasks 4.1 to 4.9.
  - One subagent run was interrupted ("input stream interrupted") on task 3.15 and was re-run.
  - The property test in 4.6 (snapshot resume equals replay) genuinely found an engine bug: `State.clone` aliased reference data between states. It was fixed.
- **What I reviewed or corrected:** Reconciled the M4 tasks with the approved UI redesign; recorded the pgx and x/text vulnerabilities without bumping the Go directive.
- **Commands and verification outcomes (checkpoint 4.9):**
  - `go vet` clean. `go test ./... -count=1` passed in every package with tests, against the real PostgreSQL container (no SKIPs; `internal/store/postgres` 10.6 s, `internal/app` 55.6 s, `internal/app/demoplan` 77.9 s, `internal/sim` 30.8 s). `go test -race` on the concurrency and snapshot tests passed.
  - staticcheck clean. `govulncheck`: 3 reachable vulnerabilities (x/text v0.25.0 twice, pgx v5.7.5); the fixes need `go` 1.25, so they were not applied.
  - Web: `tsc`, `eslint`, vitest (21 files, 170 tests) and `next build` passed. Playwright e2e on the real backend passed 8 of 8.
  - Seed 2026 hash `4c6d0dd0...ab99e` unchanged; the demo-fork golden verify printed MATCH (`a51c195a...e2a5`).
  - Flagship replay benchmark: median 93.3 ms/op over 5 runs (500 ms target), about 550 MB and 32,340 allocs per op.
- **Known issues:** the vulnerabilities above; high per-replay allocation; SSE (5.1) and the diff view are not built.
- **Learning / next action:** Finish the remaining M4 tasks (SSE, ledger drill-down, assumptions endpoint, disruption injection), then M5.

### 2026-11-27 — M4/M5 implementation

- **Kiro surface/version:** Kiro IDE agent orchestrating spec-task-execution subagents
- **Goal:** Finish the M4 studio work and implement M5 (rule-based and LLM AI managers, Human vs AI duel) from `tasks.md`.
- **Inputs:** `.kiro/specs/parallel-simulation-mvp/tasks.md`, `design.md`, steering files.
- **What Kiro actually did:**
  - Executed tasks 5.1, 5.2, 5.3, 5.5, 5.6 and 6.1 to 6.11 (6.12 is this checkpoint). Tasks 5.4 and 5.7 were not completed and remain open.
  - A label-collision fix in the studio scene (`labelLayout.ts`) and the security patch (commit `a102b29`: pgx and x/text upgrades, Go 1.25 floor, pinned CI tool versions).
  - Checkpoint 6.12: ran every check below, gofmt'd `internal/policy/rulebased/rulebased_test.go` (the only fix needed), reconciled `tasks.md`, and added the README section on configuring the AI manager.
- **Interruptions:** none recorded in this session.
- **What I reviewed or corrected:** The orchestrator opened the duel screenshot and checked it; it kept 5.4 and 5.7 unticked and recorded what remains for each.
- **Commands and verification outcomes (checkpoint 6.12):**
  - `govulncheck@v1.10.0 ./...`: "No vulnerabilities found". staticcheck v0.8.2: no output. `gofmt -l`: only `rulebased_test.go`, fixed. `npm audit --audit-level=high`: 0 vulnerabilities. `go vet` clean.
  - Test database at migration version 4. `go test ./... -count=1` passed in every package, 476 passing test lines and 0 failures, 0 SKIPs (Postgres tests ran): `cmd/parallel-sim` 1.6 s, `internal/app` 82.6 s, `internal/app/demoplan` 82.4 s, `internal/archtest` 2.1 s, `internal/httpapi` 14.5 s, `internal/policy/llm` 3.8 s, `internal/policy/rulebased` 4.6 s, `internal/sim` 35.3 s, `internal/sim/inputsfile` 4.2 s, `internal/simtest` 8.3 s, `internal/store/memory` 4.3 s, `internal/store/postgres` 12.4 s.
  - `go test -race`: `internal/app` (`TestLLM|TestAIWindow|TestDuel|TestConcurrent|TestNoLLMConfiguration`) 36.1 s, `internal/httpapi` 64.8 s, `internal/policy/llm` 1.8 s and `internal/policy/rulebased` 11.3 s all passed. `TestNoLLMConfiguration` passed with the three LLM variables unset (2.3 s).
  - Determinism: seed 2026 hash `4c6d0dd0...ab99e`, engine version 2 (no change under `backend/internal/sim`); demo-fork verify MATCH (`a51c195a...e2a5`); demo-duel human (`a51c195a...e2a5`) and AI (`e9f19296...284aa`) verifies MATCH.
  - Web: `tsc` clean, `eslint` clean, vitest 27 files and 222 tests passed, `next build` passed. Playwright e2e passed 16 passed, 0 failed, 6 skipped in 4.7 minutes immediately before this checkpoint; it was not re-run because no web file changed.
- **Known issues:** tasks 5.4 and 5.7 are open; live LLM provider verification is PENDING (stub providers only); LLM deciders hold the service mutex during provider calls; the Branch_Tree has no AI provenance labels; rule-based rationale is jargon-heavy; SSE event ids are shared across a World's Branches.
- **Learning / next action:** Finish 5.4 and 5.7, then M6 (branch diff, ensembles).

### 2026-11-27 — Pre-M6 fixes and M6 implementation (checkpoint 7.18)

- **Kiro surface/version:** IDE spec task execution (sub-agents orchestrated from the spec).
- **Goal:** Fix the LLM-under-lock issue, build the branch diff, the assumptions endpoint, the ensemble runner and the flagship calibration, then run the M6 checkpoint.
- **What Kiro actually did:** delivered tasks 5.4 (assumptions endpoint and panel) and 7.1 to 7.17 (diff service and panel, ensemble runner, report, benchmark, calibration, golden 20-seed report). In the checkpoint pass it ran every check below, fixed the stale wording in `web/e2e/duel-llm-stub.spec.ts`, added `web/e2e/diff.spec.ts` and `web/e2e/assumptions.spec.ts`, and reconciled `tasks.md`, the README and this log.
- **What I reviewed or corrected:** screenshots `desktop-18-diff`, `mobile-18-diff` and `desktop-19-assumptions` were opened. Defects fixed in the web code: on a phone the metric explanations were squeezed into a column a few characters wide (now a full-width row under each figure); the timeline divergence label "≠ Differ" was clipped (now "≠", the full text stays in the aria-label); the studio asked the parent branch for metrics at hours it had not simulated, which logged a 409 per tick in the browser console (now skipped). Not fixed, cosmetic: "0.0 points" wraps to two lines in the narrow phone column. A stray `calibration.go-E` backup file (from a macOS `sed -i`) was deleted. The README's `.env` instructions were wrong for a copied `.env.example` (it contains `DATABASE_URL`, which makes the server require that PostgreSQL); the README now says so.
- **Commands and verification outcomes (checkpoint 7.18):**
  - `govulncheck@v1.10.0 ./...`: "No vulnerabilities found" (the pinned tools switch to the go1.26.11 toolchain automatically). `staticcheck@v0.8.2 ./...`: exit 0, no findings. `gofmt -l`: nothing. `go vet ./...`: clean. `npm audit --audit-level=high`: 0 vulnerabilities.
  - `go test ./... -count=1 -v` with PostgreSQL (container on port 55432, migration version 4): all packages ok, 553 passing test lines, 0 failures, 0 SKIPs. Per package: `cmd/parallel-sim` 3.9 s, `internal/app` 172.4 s, `app/calibration` 71.9 s, `app/demoplan` 162.9 s, `app/ensemble` 217.9 s, `archtest` 2.5 s, `httpapi` 28.8 s, `policy/llm` 3.6 s, `policy/rulebased` 7.2 s, `sim` 63.4 s, `sim/inputsfile` 3.1 s, `simtest` 7.6 s, `store/memory` 3.0 s, `store/postgres` 13.9 s. (These times were measured while a Docker image pull ran in parallel.)
  - `go test -race`: `internal/app` (AI concurrency, LLM, window, duel, diff, concurrent) 16.2 s ok; `app/ensemble` (`TestReport|TestRunner|TestGolden`) 27.9 s ok; `httpapi` 82.5 s ok; `policy/llm` 2.6 s ok; `policy/rulebased` 11.6 s ok. `TestNoLLMConfiguration` with the three LLM variables unset: pass (2.4 s). `TestAIConcurrencyProposalBudget` and `TestLLMBudget` (call cap, token cap, per-world counter, concurrent windows) pass.
  - Determinism: `parallel-sim run --scenario scenarios/black-friday.json --seed 2026` printed engine version 2, state hash `f6f41425...fac80`, net profit 1,778,400 cents, service level 4427 bps, equal to `golden/no-command.hash` and the README. `verify` MATCH for demo-fork, demo-duel-human and demo-duel-ai (`9786f539...daca8`, `9786f539...daca8`, `647520ec...c1f41`). The 20-seed ensemble CLI output matched `ensemble-flagship-20.json` and `.csv` byte for byte (`cmp`).
  - Cross-architecture: this machine is arm64. In `golang:1.25` under `--platform linux/amd64` (x86_64, go1.25.14, emulated), `TestGoldenFlagshipHash`, the demo-fork `verify` (MATCH), `TestGoldenEnsembleReport` and `TestGoldenDuelHashes` all passed. This is a local emulation result only; the GitHub Actions matrix (ubuntu-latest and ubuntu-24.04-arm) had not been executed at that date (it has since; see the M8 entry). A first attempt failed only because the golden test reads the repo-root README and the container had mounted `backend/` alone.
  - Web: `tsc` clean, `eslint` clean, vitest 32 files and 246 tests passed. Playwright full run (`npm run e2e`, includes `next build`): 19 passed, 0 failed, 7 skipped (desktop-only specs on the phone project) in 5.1 minutes. `duel-llm-stub.spec.ts` was the one known failure and now passes.
  - README OpenRouter procedure followed with a stub provider and a fake key: `/managers` listed `ai_llm` available with model `stub-readme-model`; the server log held the model name and no key or base URL; the temporary `.env` was deleted.
  - Retained rapid failure files: `TestPropReplayEquivalence` and `TestPropSnapshotResume` (regression seeds of bugs fixed earlier) and `TestPropScenarioRoundTrip` (a tracked seed from 2026-11-26, also a regression seed); rapid replays each on every run and all pass. The stale `TestPropHashMetadataIndependence` seed was already deleted in the working tree.
- **Known issues:** live LLM provider verification is PENDING (stub providers only); task 5.7 is partial (no `scene/interpolate.ts`); the CI matrix had not run on GitHub at that date (it has since; see the M8 entry); the rule-based AI overspends on labor and underperforms some human-style plans in the calibrated scenario (recorded in `docs/calibration.md`, not tuned); Kiro credit use for M6 was not measured.
- **Learning / next action:** M7 (HTTP hardening, API benchmarks, the Playwright demo path, deployment).

### 2026-11-28 — M7 production readiness (final checkpoint)

- **Kiro surface/version:** Kiro agent (spec task execution subagent, IDE).
- **Goal:** M7 final checkpoint (tasks 5.8, 8.5, 8.8): fix the one benchmark miss, run every gate, check deploy readiness, reconcile the spec, commit.
- **What Kiro actually did:** added `BranchService.FramesRange` (one replay per range) and used it in the tick-frames handler and the SSE catch-up, with tests (`backend/internal/app/frames_range_test.go`); ran the security and build checks, `scripts/ci-local.sh` with Docker and e2e, amd64 goldens in a container, `scripts/demo-numbers.sh`, the container, smoke and https-origin build checks; updated docs and tasks.md.
- **Real numbers:** cold 120-frame `tick-frames` read 1881 ms to 114.6 ms median (max 120.6 ms), warm 0.69 ms (Apple M5). `go vet`, gofmt, staticcheck v0.8.2, govulncheck v1.10.0 and `npm audit` clean. `scripts/ci-local.sh`: all steps pass; vitest 35 files / 267 tests; Playwright 22 passed, 10 skipped, 0 failed. Goldens match on arm64 and linux/amd64 emulation. Demo headline numbers in `docs/demo-script.md` match `scripts/demo-numbers.sh`. Smoke test (with SSE) passes against a container on PostgreSQL.
- **Honest notes:** credit use was not measured. The GitHub CI had not run at that date (superseded by the M8 entry). No real LLM provider call was made (live verification PENDING). Nothing was deployed; real platform, TLS/HSTS path, Vercel build, managed PostgreSQL are unverified. The cause of the faster warm read was not isolated. The fork transition (1.5 s) misses the 600 ms bound and the duel reveal was skipped.
- **Learning / next action:** M8 documentation and submission.

### 2026-11-28 — M8 release preparation

- **Kiro surface/version:** Kiro IDE agent orchestrating sub-agents (spec task execution and general sub-agents).
- **Goal:** Get the repository submission-ready: make GitHub CI green, add the write-token capability model and LLM access limits, rehearse a production-like deployment locally, finish the README and hackathon material, and reconcile `tasks.md`.
- **Inputs:** `.kiro/specs/parallel-simulation-mvp/tasks.md` (M8, tasks 9.1 to 9.4), `design.md`, steering (`security.md`, `tech.md`), the M7 notes, and the GitHub Actions logs.
- **What Kiro actually did** (commits after `98169a5`, from `git log --oneline`):
  - `6636bee` docs: reconciled the task status files after M7.
  - `cbd0d68` fix(web): regenerated `web/package-lock.json` with npm 10 so `npm ci` works on GitHub.
  - `a2f6119` feat(security): write-token capability model (migration `00007_world_write_token.sql`, `backend/internal/httpapi/writeauth.go`, tests, read-only banner in the studio), optional LLM access code, per-IP cap on LLM-backed starts, and `docs/security-model.md`.
  - `d4f140b` docs(deploy): local production rehearsal (TLS PostgreSQL, HTTPS proxy, SSE through it, API restart, SIGKILL and database outage recovery; `scripts/rehearse-prod.sh`, `web/rehearsal/https.spec.ts`) and a fix to `scripts/smoke-prod.sh`, which now sends the write token.
  - `2ce9c05` test(e2e): wait for the intro animation before screenshots; `scripts/demo-numbers.sh` sends the write token; screenshots refreshed.
  - `669f640` ci(e2e): fail fast with traces, a longer e2e time budget, a more robust intro wait.
  - `d387c6d` docs: final README, `docs/architecture.md`, contributing guide, security policy, `docs/hackathon/` material and the docs link checker `scripts/check-docs.sh`.
  - `e666480` test(docs): `TestDemoScriptMatchesPlan` demo-script consistency test; screenshots captioned with their own runs.
  - Uncommitted at the time of writing (the orchestrator commits it): this entry, the task reconciliation in `tasks.md`, and the CI-status corrections in `README.md`, `docs/ci.md` and `docs/hackathon/`.
- **What I reviewed or corrected:** The orchestrator read the actual failed GitHub log of run 46060356207 and diagnosed the cause as an `npm ci` lockfile mismatch (fixed in `cbd0d68`). It diagnosed the cancelled `e2e` job of run 46106909199 as a time-budget overrun on a slow runner (addressed in `669f640`). It found that `docs/ci.md`, the README and earlier log entries still said CI had never run, and had them corrected in the working tree (uncommitted, listed above). It did not label `5c456a6` and `9abdaf5` as Kiro work.
- **Code/tests produced:** the commits above.
- **Commands and verification outcomes:**
  - GitHub Actions (`gh run list --branch main`, `gh run view <id> --json jobs`, checked 2026-11-28; full table in `docs/ci.md`):
    - 46060356207 (`6636bee`): failure. `web` failed (`npm ci`, lockfile out of sync) and `e2e` was skipped; `docker build`, `go (amd64)`, `go (arm64)` and `openapi lint` succeeded. This was the first run on GitHub.
    - 46065036957 (`cbd0d68`): success, all six jobs.
    - 46098817923 (`a2f6119`): cancelled; `e2e` cancelled, the other five jobs succeeded.
    - 46100197654 (`d4f140b`): success, all six jobs.
    - 46106909199 (`2ce9c05`): cancelled; `e2e` cancelled, the other five succeeded.
    - 46125120786 (`d387c6d`): cancelled; `e2e` cancelled, the other five succeeded. The cause was not read from the logs; a newer push followed and the workflow cancels in-progress runs per ref.
    - 46126448247 (`e666480`): in progress when checked (`openapi lint` and `docker build` succeeded; `go` and `web` running; `e2e` not started). Its outcome is not recorded here.
  - Local release suite: `scripts/ci-local.sh` passed all 22 steps; Playwright 24 passed, 10 skipped (per-project guards), 0 failed; vitest 275 tests; Go tests with PostgreSQL passed; race tests passed; golden hashes matched natively on arm64 and under linux/amd64 emulation in Docker; the demo headline numbers in `docs/demo-script.md` matched `scripts/demo-numbers.sh`.
  - `parallel-sim llm-check` exits 4 (live verification PENDING) because no provider variables are set.
- **Known issues:** live LLM provider verification is PENDING (no credentials); nothing was deployed to paid infrastructure; no public demo exists; the demo video is not recorded; Kiro credit use was not measured; the fork transition (1.5 s against the 600 ms bound) is an accepted deviation and the duel result reveal was skipped; the latest GitHub run (`e666480`) had not finished when checked.
- **Learning / next action:** final checkpoint (task 9.4) by the orchestrator, then the manual steps: record the video and submit to Devpost.

Use this template after each real Kiro session:

### YYYY-MM-DD — session title

- **Kiro surface/version:** IDE / CLI / Web / Crew
- **Goal:**
- **Inputs:** relevant spec, prompt, or steering file
- **What Kiro actually did:**
- **What I reviewed or corrected:**
- **Code/tests produced:** commit or PR links
- **Commands and verification outcomes:**
- **Learning / next action:**

## Evidence checklist

- [x] First genuine Kiro spec review/revision (2026-11-26 entries: requirements, design, tasks)
- [x] First actual Kiro implementation commit (`d44bf1b`, M1 vertical slice; M0 scaffolding is `ddc7c1d`)
- [x] Demonstrated tests and build (see the M7 and M8 entries; GitHub runs 46065036957 and 46100197654 passed all six jobs)
- [ ] Hook / custom agent / MCP usage documented if actually used (not used: `.kiro/` has steering and specs only; the work used Kiro's spec workflow and sub-agent task execution, documented in the entries above)
- [ ] Working public demo and test instructions (test instructions are in the README; no public demo: nothing is deployed)
- [ ] Submission write-up and public demo video (write-up drafted in `docs/hackathon/`; demo video not recorded)

## Links

- Project repository: https://github.com/savisaluwadana/KiroFrankenstein
- Hackathon rules: https://kiroween.devpost.com/rules
