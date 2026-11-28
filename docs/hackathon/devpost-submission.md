# Devpost submission draft: Parallel

Every claim below is traceable to the repository. Sources are named in brackets. Fields in `{{...}}` are placeholders the team fills in; see `submission-checklist.md`.

## Title

Parallel

## Tagline

Reality has no undo button. We built one.

## Elevator pitch (≤ 200 characters)

> Rewind a simulated Black Friday warehouse to any hour, fork a new future, and duel an AI manager under identical conditions. Every number is computed and traceable to the ledger.

(Character count: 178.)

## Links

- Code: https://github.com/savisaluwadana/KiroFrankenstein
- Live demo: `{{DEMO_URL}}`. Not deployed yet; see "What's next".
- Demo video: `{{VIDEO_URL}}`
- Category: Frankenstein

## Honest status (read first)

- **Results are synthetic.** Parallel is a simulation under stated, adjustable assumptions. Its outputs are not predictions or guarantees about any real business. The app shows this disclaimer on every results view.
- **Live LLM verification is pending.** No provider credentials were available. The LLM manager is tested only with stub providers [`docs/kiro-build-log.md`, `docs/live-llm-verification.md`]. The rule-based AI and everything else run without any LLM.
- **Not deployed to a public host.** Deployment artifacts (Dockerfile, `web/vercel.json`, smoke tests, a local production rehearsal) exist, but no real platform, managed PostgreSQL or Vercel build has been verified [`docs/deploy.md`, tasks.md M7 notes].
- **Calibration is not against real data.** The flagship scenario uses transparent synthetic assumptions [`docs/calibration.md`].

## Inspiration

Peak-season plans usually live in spreadsheets. A spreadsheet can't tell you what happens if your main supplier fails on Friday afternoon, because the consequences pile up hour after hour: stock runs out, orders wait, late credits accrue. Operations courses have the same problem. A case study is static, and a "beer game" exercise can't be rewound.

We asked what it would take to give a business an undo button. The answer is to record every input (the seed, the decisions, the disruptions) and recompute everything else. Then rewinding is a read, and changing the past is a fork [`.kiro/steering/product.md`].

## What it does

Parallel is a playable, forkable simulation of one e-commerce warehouse. The flagship scenario, **Black Friday Meltdown**, runs 120 simulated hours (Thursday 12:00 AM to Tuesday 12:00 AM on the clock). Demand surges, the main supplier goes down, and part of the regular team is absent [`docs/demo-script.md`; the in-app alert cards show the same].

You can:

- **Watch the warehouse run** as an isometric scene drawn from the server's state for the displayed hour. Playback speed, pausing and stepping never change a result.
- **Rewind to any hour and fork.** Everything before the fork hour stays exactly as it happened. The branches are independent.
- **Make decisions** in decision windows: order stock from a normal or backup supplier, hire temporary workers, schedule overtime, change a price, or run marketing. A command takes effect one hour later.
- **Inject surprises** (disruptions).
- **Compare futures.** The "What changed?" view lists observed differences and says plainly that they are not proof of cause.
- **Duel a rule-based AI manager** that faces the same customers, the same surprises and the same rules. A 100-point score is built from profit (50%), orders fully shipped (30%) and on-time delivery (20%) [`docs/demo-script.md`; scoreboard screenshot].
- **Click any money figure** and see the ledger entries that add up to it.
- **Prove replay.** The CLI (`parallel-sim verify`) recomputes a run from the recorded inputs and prints `MATCH` if the final state hash equals the committed one.

An optional LLM manager can be configured through an OpenAI-compatible endpoint (OpenRouter is the documented provider). Its output is validated by the same deterministic validator as a human's moves, and any failure falls back to the rule-based AI with the reason recorded.

One recorded run, for scale (seed 2026, synthetic): a human who made only two moves and then stayed passive scored 26.53 points against the rule-based AI's 56.83, and the scoreboard said "AI won". That describes this run only. It does not show that AI managers are better in general [`docs/demo-script.md`].

## How we built it

**Spec-driven, with Kiro.** We used Kiro to write steering files and a requirements → design → tasks spec (43 requirements, 41 correctness properties, 155 task checkboxes), then had delegated spec-task-execution subagents implement the tasks milestone by milestone [`docs/kiro-build-log.md`, `docs/hackathon/kiro-evidence.md`].

**The engine** is a pure Go package (`backend/internal/sim`) with no I/O, no wall clock, no global randomness and no goroutines. An architecture test fails if it imports anything outside a standard-library allowlist. Money is `int64` cents and rates are basis points, so there is no floating point in state transitions. Randomness is a project-owned SplitMix64 generator with named streams, so a decision in one area never shifts the random draws of another. A discrete-event queue runs on an integer minute clock, and every hour the engine emits a SHA-256 state hash.

**Event sourcing.** The append-only input log is the source of truth. A PostgreSQL trigger rejects UPDATE and DELETE on it. A fork reads the parent's inputs only up to the fork hour. Snapshots are caches that are trusted only if their stored hash equals the replayed hash.

**A double-entry ledger** (integer cents, per-SKU moving-average inventory cost) makes every figure a sum of entries.

**AI managers** implement one small interface that returns proposals. They never touch state. Every command passes `sim.Validate`.

**Web app.** Next.js and TypeScript (strict) with a PixiJS isometric warehouse, Zustand for view state, React Flow for the branch view, SSE for progress with a polling fallback, and an OpenAPI 3.1 contract from which the TypeScript types are generated.

**Verification.** Property-based tests (`pgregory.net/rapid`) tagged to the design's correctness properties, golden state hashes, a 20-seed golden ensemble report reproduced byte for byte, PostgreSQL concurrency tests, Vitest, and Playwright end to end tests including a timed demo path.

## Challenges we ran into

Each item is from the engineering record (`docs/kiro-build-log.md`, tasks.md notes, `docs/calibration.md`, `docs/ux-redesign.md`).

- **Reproducibility across CPU architectures.** Go can fuse multiply-add on arm64, which would break hashes, so the engine uses integer arithmetic only. The golden hashes were checked natively on arm64, under linux/amd64 emulation in Docker, and in a GitHub Actions run whose `go (amd64)` and `go (arm64)` jobs both passed (run 46100197654 on commit `d4f140b`, checked with `gh run view` on the day this file was written; `docs/ci.md` still says the workflow has not run and is out of date).
- **A state-copy bug found by a property test.** The "snapshot resume equals full replay" property test found that `State.clone` shared reference data between states. It was fixed in the M3 commit (`30ef0d1`).
- **Two database bugs found by PostgreSQL concurrency tests.** A `CHECK (BETWEEN 0 AND 1)` on the LLM call counter made a decision record insert fail after a stale answer, so a window's commands were stored without their decision record. Separately, two services recording the same window's decision collided on a unique constraint; the loser now adopts the winner's record. Migration `00006` relaxed the check.
- **Slow LLM calls under a global lock.** The LLM decider used to hold the service mutex during the provider call. It now runs single-flight per window: the lock is taken to read state and reserve the budget, released for the call, and re-taken after revalidating the branch version. A stale answer is discarded.
- **Calibration found that staffing didn't matter.** In the original scenario a stock-out at hour 19 made every later event irrelevant. We doubled opening stock and lowered the regular team size so that staffing and supply decisions matter. Every change is published in `docs/calibration.md`. The scenario was not tuned so that the AI or the human wins, and the rule-based AI still overspends on labor there.
- **A browser crash from SSE.** The Playwright end to end run exposed React error #185: per-frame SSE updates exceeded React's nested-update limit. Frames are now batched into the store at most every 50 ms.
- **CSP vs PixiJS.** The production Content-Security-Policy forbids `unsafe-eval`, which stopped PixiJS from creating its canvas. The scene now imports `pixi.js/unsafe-eval`.
- **A 16x slower first read.** A benchmark showed the cold 120-frame read replayed once per frame (1881 ms). A range read with one replay brought the median to 114.6 ms on one Apple M5 laptop.

## Accomplishments that we're proud of

- Replay is checkable. `parallel-sim verify` reproduces the committed final hash of the forked demo run, and the committed 20-seed ensemble report is reproduced byte for byte.
- 33 of the 41 design properties are tagged in the test code (33 test files); the other 8 are covered by example tests or were left optional [see `kiro-evidence.md`].
- The fork and compare path is measured: the automated test's crisis → fork → compare segment took 28.8 to 29.9 s against a 60 s requirement [`docs/demo-script.md`].
- An honest scoreboard. It says "one simulated week under the stated assumptions" and shows raw metrics beside the score.
- No LLM is needed for anything except the LLM opponent. A test runs the full path with the LLM variables unset.
- Honest records: the build log lists what was not verified, and the calibration report states where the rule-based AI loses.

## What we learned

- **Make the record the product.** Treating the input log as the only truth made rewind, fork, replay and cross-process checks fall out of one idea. It also made bugs findable: the snapshot property test only works because replay is exact.
- **Property tests earn their cost.** The `State.clone` aliasing bug was not visible in example tests.
- **Concurrency bugs live at the seams.** The check-constraint and unique-race bugs only appeared against a real PostgreSQL under concurrent windows, not in the in-memory store.
- **Calibrating a simulation is a modelling task.** The first scenario made a key decision pointless, and we only noticed by running the baseline runs.
- **Say what a number does not show.** The duel result is one run against one policy. The UI and the docs say so, and the diff view uses "observed differences", not "caused by".
- **Don't trust the docs after you change the code.** Our CI doc lagged the real GitHub run; reconciling status files after each milestone took repeated passes.

## What's next for Parallel

From the documented limitations and roadmap only:

- Verify the LLM opponent with a real provider (the procedure is in `docs/live-llm-verification.md`; `parallel-sim llm-check`).
- Deploy to real hosts (container host, managed PostgreSQL with TLS, Vercel) and run `scripts/smoke-prod.sh` there. This has not happened.
- Bring the fork transition (now 1.5 s) inside its 600 ms target, and build the duel result reveal (task 8.7 is open).
- Reduce the rule-based AI's labor overspending, or document alternatives. This is recorded in `docs/calibration.md` and is not tuned.
- Optional stretch items in the spec (larger ensembles, per-tick diff view, staffing advisor, more disruption kinds, read-only share links).

Calibration against real business data is **not** claimed and is not planned in the current spec.

## Built with

| Area | Technology (version from `go.mod` / `package.json`) |
|---|---|
| Backend | Go 1.25, Chi v5.3.0 |
| Database | PostgreSQL 16, pgx v5.9.2, Goose v3.24.3 |
| Property testing | `pgregory.net/rapid` v1.3.0 |
| Frontend | Next.js 16.4.6, React 19.3.1, TypeScript 5.9.3 (strict) |
| Rendering | PixiJS 8.23.0 |
| UI | Tailwind CSS 4.3.5; shadcn/ui-style setup (`web/components.json`, `class-variance-authority`, `clsx`, `tailwind-merge`, `lucide-react`) |
| State and graph | Zustand 5.1.0, React Flow (`@xyflow/react` 13.0.3) |
| Streaming | Server-Sent Events with polling fallback |
| API contract | OpenAPI 3.1, `openapi-typescript` 7.13.0 |
| Testing | Vitest 5.1.2, Playwright 1.65.1, rapid |
| Infrastructure | Docker, Docker Compose (PostgreSQL), GitHub Actions |
| AI adapter | OpenAI-compatible chat completions client (OpenRouter documented; no model identifier built in) |
| Built with | Kiro (steering, specs, delegated task execution) |

Note: the repository has `components.json` for shadcn/ui, but `web/components/ui` does not exist, so we describe the UI as "shadcn/ui-style configuration" and do not claim the stock shadcn components.

## Team

`{{TEAM_NAMES}}`
