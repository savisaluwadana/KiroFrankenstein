# Benchmarks

Early benchmark report (Requirement 29.8 to 29.11). Every figure below comes from the raw output in the last section. Correctness checks (validation, invariants, canonical encoding, state hashing) stay enabled in the measured build. Results are for synthetic simulation only.

## Status of each budget

| Criterion | Budget | Status |
|---|---|---|
| 29.1 Flagship replay, Tick 0 to 120, Sim_CLI-equivalent in-process | 500 ms | **Measured (early proxy, in-process engine loop)**: median 87.4 ms, p95 98.3 ms. Met. |
| 29.2 50-Seed Paired_Experiment | 60 s | **Measured (task 7.15, real Ensemble_Runner, engine version 2, calibrated flagship)**: median 3.27 s at the default concurrency (GOMAXPROCS 10), p95 3.38 s; median 13.00 s at concurrency 1. Target 60 s: met, with the default-concurrency median at about 5.5% of the budget and the sequential median at about 21.7%. See "50-Seed paired ensemble (2026-11-27)" below. |
| 29.3 State read p95 300 ms | 300 ms (p95, demo host) | **Measured (task 8.2, laptop, not the demo host)**: horizon read warm median 1.47 ms, cold-service median 46.3 ms; rewind read of Tick 60 warm median 14.0 ms, cold 35.9 ms (worst p95 47.9 ms). Met by about 252 ms. |
| 29.4 Fork 500 ms | 500 ms (demo host) | **Measured (task 8.2, laptop)**: median 1.71 ms, p95 1.77 ms. Met by about 498 ms. |
| 29.5 Diff 1 s | 1 s (demo host) | **Measured (task 8.2, laptop)**: warm median 62.5 ms, cold-service median 242.2 ms (p95 253.9 ms). Met by about 746 ms (cold). |
| 29.6 Advance to horizon 1 s | 1 s (demo host) | **Measured (task 8.2, laptop)**: one request from Tick 60 to the horizon median 316.6 ms (p95 402.2 ms); Tick 0 to the horizon through 4 requests median 308.0 ms (p95 349.0 ms). Met by about 598 ms (worst p95). |
| 29.7 Tick_Frames range 1 s | 1 s (demo host) | **Met after the M7 fix (laptop)**: 120 frames cold median 114.6 ms, worst of 5 means 120.6 ms (met by about 879 ms); warm median 0.68 ms. **Earlier miss, kept on record (task 8.2)**: cold median 1881.3 ms, p95 1906.9 ms, missed by about 881 ms. See "M7 fix: cold frame range read" below. |

Note: the figures below were measured with engine version 1 (`sim.EngineVersion` = 1, hash `96e7d8d9...`). Engine version 2 (task 3.5) adds one disruption-stream draw per Tick and one encoded byte per active disruption; the numbers predate it and have not been re-measured.

Calibration note (task 7.16): the figures in this document predate the flagship calibration. Calibration changed Scenario data only (opening stock doubled, regular workers 20 to 14; see `docs/calibration.md`), so the engine version stays 2 and the golden hashes changed because the Scenario changed. A single-replay re-measurement after calibration (`cd backend && go test ./internal/sim -run '^$' -bench BenchmarkFlagshipReplay -benchtime 10x -count 3`, same machine as below, Go 1.25.5 darwin/arm64, Apple M5, engine version 2, dirty working tree, nothing else running) gave 102.3 ms, 103.2 ms and 104.5 ms per replay, about 682 MB and 45,800 allocations per replay. That is about 10% slower than the 93.3 ms recorded for engine version 2 in `docs/kiro-build-log.md` (about 550 MB, 32,340 allocations), not "unchanged within noise": the calibrated Scenario ships more orders, so each replay posts more ledger entries. The 500 ms budget of criterion 29.1 is still met (about 20% of it). The 100-replay proxy and the 50-Seed ensemble were not re-run here (task 7.15 owns the ensemble measurement).

No budget was missed, so there is no shortfall to record. The 29.1 measurement is of the engine loop (`Init`, 120 `Advance` calls, one final `Hash`) inside a Go benchmark, not of the `parallel-sim` process, so process start-up and JSON output are not included.

## Environment

| Item | Value |
|---|---|
| Hardware model | MacBook Pro, model identifier Mac17,2 |
| CPU | Apple M5 (10 cores: 4 performance, 6 efficiency), `GOMAXPROCS` 10 (benchmark suffix `-10`) |
| Memory | 16 GB (17179869184 bytes) |
| OS | macOS 26.2 (build 25C56), darwin/arm64 |
| Go version | go1.25.5 darwin/arm64 |
| Engine version | `sim.EngineVersion` = 1 |
| Commit measured | `ddc7c1dc5a000f16d5cc523814872e3eb3b6869c` with a **dirty working tree** (uncommitted and untracked files, including `backend/internal/sim/bench_test.go`, which contains the benchmarks) |

## Method

- Scenario: `backend/scenarios/black-friday.json`, "Black Friday Meltdown", 4 SKUs (headphones, charger, speaker, watchband), horizon 120 Ticks.
- Commands: none. Disruptions: those scheduled in the scenario.
- Per-run counts for Seed 2026 (from `TestFlagshipReplayCounts`): 11385 outcome journal entries returned by `Advance`, 3041 ledger entries in the final State, and an Event_Queue insertion sequence counter of 726 at the horizon. "Events per run" is reported as these three counts; the number of events dispatched by the engine was not counted separately. The final state hash for Seed 2026 was `96e7d8d94bc1b3d0e81598272df984b0e0afe86c8e848bc34275781b513bce08`.
- `BenchmarkFlagshipReplay`: one full replay with Seed 2026 per iteration, followed by one `sim.Hash` of the final State (Requirement 29.1).
- `BenchmarkHundredReplays`: per iteration, Seeds 1 to 50 with 2 runs per Seed, run sequentially (100 replays plus 100 final hashes). Early proxy for 29.2 (Requirement 29.9). It does not include the Ensemble_Runner, paired-arm policy code or report writing.
- Command: `cd backend && go test ./internal/sim -run '^$' -bench 'BenchmarkFlagshipReplay|BenchmarkHundredReplays' -benchtime 10x -count 10`
- Iterations: Go runs one calibration iteration before each `-benchtime 10x` sample (these are discarded and act as warm-up, 1 per sample, 10 in total per benchmark). Each of the 10 samples is the mean ns/op over 10 measured iterations, so 100 measured iterations per benchmark. The statistics below are computed over the 10 sample values.
- Statistics: median is the mean of the 5th and 6th sorted samples. p95 is linear interpolation at rank 0.95 × (n − 1) over the 10 sorted samples, so it is dominated by the slowest samples and is only indicative.
- The machine was otherwise idle; nothing else heavy ran concurrently. The whole run took 1006.9 s wall clock.

## Results

| Benchmark | Min | Median | p95 | Max | Bytes/op | Allocs/op |
|---|---|---|---|---|---|---|
| FlagshipReplay | 85.33 ms | 87.42 ms | 98.33 ms | 105.08 ms | about 534.1 MB | about 32,158 |
| HundredReplays (100 replays) | 8.872 s | 9.050 s | 9.230 s | 9.288 s | about 53.07 GB | about 3,214,100 |

Comparison with targets:

- Flagship replay: median 87.4 ms against the 500 ms target, about 17% of the budget. The first sample (105 ms) is the slowest and looks like a cold start.
- 100 sequential replays: median 9.05 s against the 60 s ensemble budget, about 15% of the budget. A real 50-Seed paired ensemble runs about the same 100 replays plus policy code, so this proxy suggests headroom, but criterion 2 stays "target, not yet measured".

Observation, not acted on: each replay allocates about 534 MB for 120 Ticks (about 4.5 MB per Tick), which points to per-Tick State copying or encoding that grows with the ledger and Settled orders. It was not profiled because both targets are met. If later requirements add ledger size or the Ensemble_Runner misses 60 s, profile first with `go test ./internal/sim -run '^$' -bench BenchmarkFlagshipReplay -cpuprofile cpu.out -memprofile mem.out`.

## Raw output

```
goos: darwin
goarch: arm64
pkg: github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim
cpu: Apple M5
BenchmarkFlagshipReplay-10    	      10	 105080275 ns/op	534099030 B/op	   32155 allocs/op
BenchmarkFlagshipReplay-10    	      10	  88770604 ns/op	534099691 B/op	   32162 allocs/op
BenchmarkFlagshipReplay-10    	      10	  88299546 ns/op	534099049 B/op	   32155 allocs/op
BenchmarkFlagshipReplay-10    	      10	  88092138 ns/op	534099428 B/op	   32159 allocs/op
BenchmarkFlagshipReplay-10    	      10	  86751496 ns/op	534099556 B/op	   32160 allocs/op
BenchmarkFlagshipReplay-10    	      10	  85330250 ns/op	534099222 B/op	   32157 allocs/op
BenchmarkFlagshipReplay-10    	      10	  85692004 ns/op	534099515 B/op	   32159 allocs/op
BenchmarkFlagshipReplay-10    	      10	  85774196 ns/op	534099598 B/op	   32160 allocs/op
BenchmarkFlagshipReplay-10    	      10	  90077512 ns/op	534099280 B/op	   32157 allocs/op
BenchmarkFlagshipReplay-10    	      10	  85708596 ns/op	534099385 B/op	   32158 allocs/op
BenchmarkHundredReplays-10    	      10	8871966454 ns/op	53068445364 B/op	 3214271 allocs/op
BenchmarkHundredReplays-10    	      10	9075828200 ns/op	53068434774 B/op	 3214156 allocs/op
BenchmarkHundredReplays-10    	      10	8964523838 ns/op	53068423340 B/op	 3214037 allocs/op
BenchmarkHundredReplays-10    	      10	9000542383 ns/op	53068438755 B/op	 3214190 allocs/op
BenchmarkHundredReplays-10    	      10	9287593646 ns/op	53068425758 B/op	 3214059 allocs/op
BenchmarkHundredReplays-10    	      10	9023263108 ns/op	53068427968 B/op	 3214078 allocs/op
BenchmarkHundredReplays-10    	      10	9160051650 ns/op	53068426587 B/op	 3214069 allocs/op
BenchmarkHundredReplays-10    	      10	9118812867 ns/op	53068430432 B/op	 3214110 allocs/op
BenchmarkHundredReplays-10    	      10	9147978812 ns/op	53068427713 B/op	 3214081 allocs/op
BenchmarkHundredReplays-10    	      10	8954695283 ns/op	53068437507 B/op	 3214180 allocs/op
PASS
ok  	github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim	1006.927s
```

Counts run (`go test ./internal/sim -run TestFlagshipReplayCounts -v -count=1`):

```
bench_test.go:55: seed=2026 ticks=120 outcomes=11385 ledger_entries=3041 event_seq=726 hash=96e7d8d94bc1b3d0e81598272df984b0e0afe86c8e848bc34275781b513bce08
```


## Warehouse canvas frame times (browser, measured)

Method: `web/e2e/setup-and-perf.spec.ts` starts the demo, sets "Pause automatically at: surprises only", selects Quick (4x, 8 ticks per second), presses Play and records `requestAnimationFrame` deltas for 5 s. Headless Chromium 159 (Playwright 1.65.1) on an Apple Silicon Mac (macOS), production build, device scale factor 1 (desktop) and 2.625 (Pixel 7 emulation). The numbers measure the page's frame cadence while the scene animates and the API streams ticks; they are not a GPU profile, and headless Chromium may not be vsync-limited like a visible window. One run each, not averaged.

| Project | Frames in 5 s | Mean ms | p50 | p95 | p99 | Max | Frames over 33 ms |
|---|---|---|---|---|---|---|---|
| desktop 1440x900 | 301 | 16.67 | 16.7 | 16.7 | 16.8 | 16.8 | 0 |
| mobile 390x844 (emulated) | 302 | 16.67 | 16.7 | 16.8 | 16.8 | 16.8 | 0 |

Earlier runs the same day varied: desktop 300 frames, mean 16.83 ms, p99 33.3 ms, max 33.4 ms; emulated phone 297 frames, mean 16.89 ms, p99 33.3 ms, max 50.1 ms (2 frames over 33 ms). So occasional single dropped frames occur; the table shows the final run. Emulated phone CPU throttling was not applied, so real phones will be slower.

## Post-M2/M3 re-measure (2026-11-27)

Engine version 2 plus the `State.clone` isolation fix, same machine (Apple M5, darwin/arm64, Go go1.25.5), idle, dirty working tree before the M3 commit. Command: `cd backend && go test ./internal/sim -run '^$' -bench BenchmarkFlagshipReplay -benchtime 10x -count 5 -benchmem`. Five runs: 101.6, 91.7, 92.3, 100.0 and 93.3 ms/op (median 93.3 ms, max 101.6 ms). Allocation per op: about 550.6 MB and about 32,340 allocs. Against the 500 ms target (29.1) this is met in-process (about 5x headroom). The engine-loop proxy excludes process start-up and JSON output. The roughly 550 MB per replay is high; it is churn from state cloning and has not been profiled or optimized. The 100-replay benchmark was not re-run, so the 29.2 proxy above still reflects engine version 1.

## 50-Seed paired ensemble (2026-11-27)

Task 7.15, Requirements 29.2 and 29.8 to 29.11. Results are for synthetic simulation only.

Benchmark: `BenchmarkPairedEnsemble50` in `backend/internal/app/ensemble/bench_test.go`. It calls `ensemble.Run` once per iteration with two arms (the fixed demo plan `fixed:scenarios/demo-duel-human.json` and the `rulebased` adaptive arm) under Seeds 1..50, which is 100 full replays plus the rule-based policy and the per-run validation, state hash, metrics and score. Validation, invariant checks, canonical encoding and state hashing are all enabled; nothing was disabled. Two sub-benchmarks: `default-gomaxprocs10` (Config.Concurrency 0, the documented default, which means GOMAXPROCS = 10) and `concurrency-1` (sequential). Once per sub-benchmark, outside the timed region, the benchmark builds the report JSON and checks that it is non-empty and that there are 100 runs. Report JSON: 52,029 bytes, SHA-256 `03f92139caf1d7c0f8fc32f199e7b0360c8696818767409ef08a0280c7e924c0` (identical in every sub-benchmark and every sample, as determinism requires). Report building is not inside the timed loop.

| Item | Value |
|---|---|
| Hardware model | MacBook Pro, model identifier Mac17,2 (`sysctl hw.model`) |
| CPU | Apple M5, 10 logical CPUs (`hw.ncpu` 10), `GOMAXPROCS` 10 |
| Memory | 17179869184 bytes (16 GB) |
| OS | macOS 26.2 (build 25C56), darwin/arm64 |
| Go version | go1.25.5 darwin/arm64 |
| Engine version | `sim.EngineVersion` = 2 |
| Scenario | `backend/scenarios/black-friday.json`, "Black Friday Meltdown" (calibrated, task 7.16); canonical scenario hash `1a56aa3aee635ea8164f0379f3e184474d37949a2f67c6a6f8784416e0e7a337` (the hash recorded in `demo-duel-human.json`, which the loader checks); file SHA-256 `bf1fcedf4f559f4fb31bad2373542eaac869bd19250cae2c667ae68e414d503a` |
| SKU count | 4 |
| Horizon | 120 Ticks |
| Seeds | 1..50 (50 Seeds, 2 arms, 100 runs per ensemble) |
| Events / ledger entries per run | not measured in this task; the single-replay counts in the section above (Seed 2026, uncalibrated) no longer apply after calibration |
| Concurrency | 10 workers (default) and 1 worker |
| Commit | `744607ac0edfce2c1d35130057de100f5d02670b` with a **dirty working tree** (114 modified or untracked paths at the time, including the new `bench_test.go`) |
| Command | `cd backend && go test ./internal/app/ensemble -run '^$' -bench BenchmarkPairedEnsemble50 -benchtime 3x -count 5` |
| Warm-up | Go runs one calibration iteration (b.N = 1) before each sample: 1 per sample, 5 per sub-benchmark, discarded. |
| Measured iterations | 3 per sample, 5 samples per sub-benchmark, so 15 measured ensembles each. Each sample is the mean ns/op over its 3 iterations. |
| Wall clock | 326.145 s for the whole command. The machine was otherwise idle. |

Statistics over the 5 sample values (median is the 3rd sorted sample; p95 is linear interpolation at rank 0.95 × 4 = 3.8, so with 5 samples it is only indicative):

| Sub-benchmark | Min | Median | p95 | Max | Bytes/op | Allocs/op |
|---|---|---|---|---|---|---|
| default concurrency (10 workers) | 3.170 s | 3.274 s | 3.383 s | 3.388 s | about 80.89 GB | about 7,056,900 |
| concurrency 1 | 12.774 s | 12.999 s | 13.365 s | 13.428 s | about 80.89 GB | about 7,060,600 |

Comparison with the Requirement 29.2 target of 60 s:

- Default concurrency: median 3.27 s, p95 3.38 s. Target met, by 56.7 s (the median is about 5.5% of the budget).
- Concurrency 1: median 13.00 s, p95 13.37 s. Target met, by 47.0 s (about 21.7% of the budget), so the target holds on a single core. Speed-up from parallelism is about 4.0x on 10 cores (4 performance and 6 efficiency), which is below linear. Allocation is about 809 MB per run (80.89 GB over 100 runs), and every worker allocates at that rate, which is a plausible limit on scaling (memory bandwidth and GC), but this was not profiled.
- Sequential cost per replay is about 130 ms (13.0 s over 100 runs), about 27% over the 102 ms single-replay figure after calibration (see the calibration note above); the difference is the rule-based policy, per-run validation, metrics and score.
- Because the target is met no profile was taken. The known hot spot (per-Tick State copying, now about 809 MB per run) is unchanged and was not optimized. No code outside the benchmark file was changed.

Raw output (benchmark log lines for the one-off report check removed from this excerpt only to save space; they all read `report sha256=03f92139caf1d7c0f8fc32f199e7b0360c8696818767409ef08a0280c7e924c0 bytes=52029 runs=100`; the complete file was `/tmp/ens-bench.txt`):

```
goos: darwin
goarch: arm64
pkg: github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/ensemble
cpu: Apple M5
BenchmarkPairedEnsemble50/default-gomaxprocs10-10         	       3	3260865750 ns/op	80890649629 B/op	 7056943 allocs/op
BenchmarkPairedEnsemble50/default-gomaxprocs10-10         	       3	3273548445 ns/op	80890647525 B/op	 7056902 allocs/op
BenchmarkPairedEnsemble50/default-gomaxprocs10-10         	       3	3387977375 ns/op	80890642000 B/op	 7056866 allocs/op
BenchmarkPairedEnsemble50/default-gomaxprocs10-10         	       3	3169546875 ns/op	80890649157 B/op	 7056965 allocs/op
BenchmarkPairedEnsemble50/default-gomaxprocs10-10         	       3	3360497389 ns/op	80890637808 B/op	 7056917 allocs/op
BenchmarkPairedEnsemble50/concurrency-1-10                	       3	13114814861 ns/op	80891589053 B/op	 7060596 allocs/op
BenchmarkPairedEnsemble50/concurrency-1-10                	       3	13427794597 ns/op	80891582128 B/op	 7060521 allocs/op
BenchmarkPairedEnsemble50/concurrency-1-10                	       3	12780291569 ns/op	80891596650 B/op	 7060695 allocs/op
BenchmarkPairedEnsemble50/concurrency-1-10                	       3	12999385347 ns/op	80891592626 B/op	 7060611 allocs/op
BenchmarkPairedEnsemble50/concurrency-1-10                	       3	12773738861 ns/op	80891604221 B/op	 7060744 allocs/op
PASS
ok  	github.com/savisaluwadana/KiroFrankenstein/backend/internal/app/ensemble	326.145s
```

## API operation benchmarks (Postgres) (2026-11-28)

Task 8.2, Requirements 29.3 to 29.7, 29.8, 29.10, 29.11. Results are for synthetic simulation only. The 29.3 to 29.7 budgets are stated for "the demo host", which is not chosen yet, and these numbers come from a developer laptop. Treat them as indicative until a demo host exists; they do not show the budgets are met on that host.

Benchmarks: `backend/internal/httpapi/bench_test.go`, package `httpapi_test`. Each request goes through the real Chi router and middleware, `BranchService`, `postgres.Store` (PostgreSQL 16) and the engine, using `httptest` (no TCP, so network and TLS time are not included, and SSE is not involved). The measured build keeps every check on: command validation, invariants, snapshot verification, projections, canonical encoding and state hashing. Only the per-IP request-rate and World-creation limiters are raised in the harness, because setup creates many Worlds.

| Item | Value |
|---|---|
| Hardware, CPU, memory | MacBook Pro Mac17,2, Apple M5 (10 cores), 16 GB (17179869184 bytes); `GOMAXPROCS` 10 |
| OS, Go | macOS 26.2, darwin/arm64, go1.25.5 |
| Engine version | `sim.EngineVersion` = 2 |
| Scenario | `backend/scenarios/black-friday.json`, "Black Friday Meltdown" (calibrated), 4 SKUs, horizon 120 Ticks; canonical scenario hash `1a56aa3aee635ea8164f0379f3e184474d37949a2f67c6a6f8784416e0e7a337`; demo plan Seed 2026 (the `/demo` endpoint). Event count per run not measured. |
| PostgreSQL | 16.16 in Docker (`postgres:16`, compose service `db`) on the same machine, published as `127.0.0.1:55432`; database `parallel_test`, runtime role `parallel_app`; migrated to version 6 with the `parallel_migrator` role first |
| Commit | `3dc50289ff6285e93d138b6bf0ff7d2198f22e6c` with a **dirty working tree** (about 124 changed or untracked paths, including the new `bench_test.go`) |
| Machine state | Quiet apart from an idle `parallel-api` dev process (about 0% CPU) and the idle Postgres container |
| Command | `cd backend && PARALLEL_TEST_DATABASE_URL=postgres://parallel_app:app_dev_password@127.0.0.1:55432/parallel_test?sslmode=disable go test ./internal/httpapi -run '^$' -bench . -benchtime 20x -count 5` (the task's command with the local URL and port; one adjustment: the task text shows `parallel:parallel@localhost:5432`). Wall clock 373.7 s. |
| Iterations | Go runs one calibration iteration (b.N = 1) before each sample, discarded. Each benchmark also performs one untimed request first (warm-up and response check). 20 measured iterations per sample, 5 samples, so 100 measured requests per benchmark. |
| Statistics | Each sample is the mean over its 20 iterations. Median is the 3rd sorted sample; p95 is linear interpolation at rank 3.8 of the 5 sample means. It is not a per-request p95, so it understates tail latency; treat as indicative. |

What each benchmark measures:

- `StateRead`: `GET /branches/{id}/state?tick=T` for a root Branch at the horizon, T = 120 ("horizon") and T = 60 (a rewind read). "warm": one service instance, head cached. "cold": a new `BranchService` per iteration over the same database (service construction is untimed), as after a restart.
- `Fork`: `POST /branches/{id}/fork` at Tick 60 of a Branch at the horizon. A World allows 32 Branches, so a fresh World is prepared (untimed) every 30 forks.
- `Diff`: `POST /diffs` of the parent and a fork child (forked at the plan fork Tick, one demo command, advanced to the horizon), `comparisonTick` 120. Warm and cold as above.
- `Advance`: a fresh demo World each iteration (creation untimed). `from-zero`: Tick 0 to the horizon through 4 requests (the root pauses at disruption onsets, so the requests resume after each pause; the 4 requests are timed together). `last-segment`: the setup advances to Tick 60, which is where the last pause ends; the timed part is the single request from Tick 60 to the horizon, which has no open Decision_Window in between (the 29.6 wording). Persistence of inputs, projections and snapshots is included.
- `TickFrames`: `GET /branches/{id}/tick-frames?from=1&to=120` (120 frames, 29.7), warm and cold.

Results (milliseconds, over the 5 sample means):

| Benchmark | Min | Median | p95 | Max | Budget | Verdict |
|---|---|---|---|---|---|---|
| StateRead horizon warm | 1.37 | 1.47 | 1.57 | 1.59 | 300 ms p95 (29.3) | measured 1.57 vs 300: met by 298.4 |
| StateRead horizon cold | 45.15 | 46.26 | 47.94 | 48.13 | 300 ms p95 (29.3) | measured 47.94 vs 300: met by 252.1 |
| StateRead rewind Tick 60 warm | 13.53 | 14.00 | 16.78 | 17.07 | 300 ms p95 (29.3) | measured 16.78 vs 300: met by 283.2 |
| StateRead rewind Tick 60 cold | 34.55 | 35.90 | 40.97 | 42.19 | 300 ms p95 (29.3) | measured 40.97 vs 300: met by 259.0 |
| Fork at Tick 60 | 1.59 | 1.71 | 1.77 | 1.77 | 500 ms (29.4) | measured 1.77 vs 500: met by 498.2 |
| Diff at horizon warm | 60.52 | 62.48 | 66.61 | 67.52 | 1000 ms (29.5) | measured 66.61 vs 1000: met by 933.4 |
| Diff at horizon cold | 231.55 | 242.18 | 253.86 | 255.88 | 1000 ms (29.5) | measured 253.86 vs 1000: met by 746.1 |
| Advance Tick 60 to horizon (one request) | 267.59 | 316.63 | 402.18 | 420.17 | 1000 ms (29.6) | measured 402.18 vs 1000: met by 597.8 |
| Advance Tick 0 to horizon (4 requests) | 297.50 | 308.03 | 348.99 | 353.48 | 1000 ms (29.6, stricter than the wording) | measured 348.99 vs 1000: met by 651.0 |
| TickFrames 120 warm | 21.36 | 26.38 | 26.53 | 26.54 | 1000 ms (29.7) | measured 26.53 vs 1000: met by 973.5 |
| TickFrames 120 cold | 1852.78 | 1881.29 | 1906.89 | 1912.57 | 1000 ms (29.7) | **measured 1906.89 vs 1000: missed by 906.9** (median 1881.3, missed by 881.3) |

29.3 is a 95th percentile over requests; the figures above are percentile-of-means, so 29.3 is only indicatively met. No other budget is within a factor of two of its limit except the cold Tick_Frames read.

Shortfall and hot spot (29.11): the cold 120-frame read misses 1 s. A CPU profile of `BenchmarkAPITickFrames/cold` (`-cpuprofile`, 5 iterations; most of the run is untimed setup) shows the handler time dominated by `(*BranchService).FrameAt` (2.63 s cumulative) which calls `rebuild` (2.40 s), which runs `sim.Advance` (2.37 s) with `sim.BuildTickFrame` (1.89 s) and `sim.Hash` (1.79 s). `listTickFrames` calls `FrameAt` once per Tick and `FrameAt` rebuilds from the stored inputs for any Tick not in the cached head, so a cold range read appears to replay the Branch once per frame (up to 120 replays, 7,260 `Advance` steps) instead of once for the range. Reading the code and the profile agree on this; I did not count the replays directly. A fix (rebuild once per range, or cache the rebuilt frames) changes `internal/app` and `internal/httpapi`, so it was not done in this task (outside the allowed files, and not trivially safe). All determinism, invariant and validation checks stayed enabled. The warm read (head cached) takes about 26 ms; I did not measure whether a second read after a cold one is warm, so how often the cold case occurs in the demo is not established.

Raw output (`/tmp/api-bench.txt`):

```
goos: darwin
goarch: arm64
pkg: github.com/savisaluwadana/KiroFrankenstein/backend/internal/httpapi
cpu: Apple M5
BenchmarkAPIStateRead/horizon-warm-10         	      20	   1589746 ns/op
BenchmarkAPIStateRead/horizon-warm-10         	      20	   1465019 ns/op
BenchmarkAPIStateRead/horizon-warm-10         	      20	   1453435 ns/op
BenchmarkAPIStateRead/horizon-warm-10         	      20	   1369533 ns/op
BenchmarkAPIStateRead/horizon-warm-10         	      20	   1489340 ns/op
BenchmarkAPIStateRead/horizon-cold-10         	      20	  47205125 ns/op
BenchmarkAPIStateRead/horizon-cold-10         	      20	  45147460 ns/op
BenchmarkAPIStateRead/horizon-cold-10         	      20	  46264204 ns/op
BenchmarkAPIStateRead/horizon-cold-10         	      20	  48128231 ns/op
BenchmarkAPIStateRead/horizon-cold-10         	      20	  45179158 ns/op
BenchmarkAPIStateRead/rewind-60-warm-10       	      20	  13999225 ns/op
BenchmarkAPIStateRead/rewind-60-warm-10       	      20	  13684846 ns/op
BenchmarkAPIStateRead/rewind-60-warm-10       	      20	  17074538 ns/op
BenchmarkAPIStateRead/rewind-60-warm-10       	      20	  15580850 ns/op
BenchmarkAPIStateRead/rewind-60-warm-10       	      20	  13531731 ns/op
BenchmarkAPIStateRead/rewind-60-cold-10       	      20	  34656269 ns/op
BenchmarkAPIStateRead/rewind-60-cold-10       	      20	  42194235 ns/op
BenchmarkAPIStateRead/rewind-60-cold-10       	      20	  34547988 ns/op
BenchmarkAPIStateRead/rewind-60-cold-10       	      20	  35900910 ns/op
BenchmarkAPIStateRead/rewind-60-cold-10       	      20	  36057058 ns/op
BenchmarkAPIFork-10                           	      20	   1706938 ns/op
BenchmarkAPIFork-10                           	      20	   1738265 ns/op
BenchmarkAPIFork-10                           	      20	   1591750 ns/op
BenchmarkAPIFork-10                           	      20	   1774435 ns/op
BenchmarkAPIFork-10                           	      20	   1656369 ns/op
BenchmarkAPIDiff/warm-10                      	      20	  62945665 ns/op
BenchmarkAPIDiff/warm-10                      	      20	  60518077 ns/op
BenchmarkAPIDiff/warm-10                      	      20	  62478040 ns/op
BenchmarkAPIDiff/warm-10                      	      20	  61580990 ns/op
BenchmarkAPIDiff/warm-10                      	      20	  67521315 ns/op
BenchmarkAPIDiff/cold-10                      	      20	 231551098 ns/op
BenchmarkAPIDiff/cold-10                      	      20	 242178658 ns/op
BenchmarkAPIDiff/cold-10                      	      20	 245763287 ns/op
BenchmarkAPIDiff/cold-10                      	      20	 255879936 ns/op
BenchmarkAPIDiff/cold-10                      	      20	 240327556 ns/op
BenchmarkAPIAdvance/from-zero-10              	      20	 308027923 ns/op	         4.000 requests/op
BenchmarkAPIAdvance/from-zero-10              	      20	 298680481 ns/op	         4.000 requests/op
BenchmarkAPIAdvance/from-zero-10              	      20	 297497881 ns/op	         4.000 requests/op
BenchmarkAPIAdvance/from-zero-10              	      20	 331040942 ns/op	         4.000 requests/op
BenchmarkAPIAdvance/from-zero-10              	      20	 353481506 ns/op	         4.000 requests/op
BenchmarkAPIAdvance/last-segment-10           	      20	 288914977 ns/op	        60.00 start-tick
BenchmarkAPIAdvance/last-segment-10           	      20	 330206340 ns/op	        60.00 start-tick
BenchmarkAPIAdvance/last-segment-10           	      20	 316634736 ns/op	        60.00 start-tick
BenchmarkAPIAdvance/last-segment-10           	      20	 420169054 ns/op	        60.00 start-tick
BenchmarkAPIAdvance/last-segment-10           	      20	 267591810 ns/op	        60.00 start-tick
BenchmarkAPITickFrames/warm-10                	      20	  26541233 ns/op
BenchmarkAPITickFrames/warm-10                	      20	  22795181 ns/op
BenchmarkAPITickFrames/warm-10                	      20	  26384685 ns/op
BenchmarkAPITickFrames/warm-10                	      20	  21364535 ns/op
BenchmarkAPITickFrames/warm-10                	      20	  26465633 ns/op
BenchmarkAPITickFrames/cold-10                	      20	1881290106 ns/op
BenchmarkAPITickFrames/cold-10                	      20	1912567679 ns/op
BenchmarkAPITickFrames/cold-10                	      20	1884172769 ns/op
BenchmarkAPITickFrames/cold-10                	      20	1852776908 ns/op
BenchmarkAPITickFrames/cold-10                	      20	1870692813 ns/op
PASS
ok  	github.com/savisaluwadana/KiroFrankenstein/backend/internal/httpapi	373.708s
```

## M7 fix: cold frame range read (2026-11-28)

The 8.2 miss above (cold 120-frame `tick-frames` read, 1881 ms median against 1 s) was fixed in the M7 final checkpoint. The earlier numbers and analysis above stay as recorded.

Change: `BranchService.FramesRange(ctx, id, from, to)` (`backend/internal/app/branch_service.go`) returns the frames from the head cache when it holds all of them, else recomputes the whole range with ONE replay from the best trusted snapshot at or before `from-1` (else from Init), through the same `replayTo` that `FrameAt` uses. `listTickFrames` and the SSE catch-up (`drain`) call it instead of `FrameAt` once per tick. No engine code, state or hash changed; frames are the engine's own output. Tests (`backend/internal/app/frames_range_test.go`): a cold range equals `FrameAt` hash and tick for every tick in six ranges; the `ReplayStart` hook shows at most one replay per range, resuming at snapshot 24 for range 30..40; the same holds on a fork child across the ancestor's snapshot; a range beyond the head fails. I did not verify what caused the faster warm read (26 ms to 0.68 ms); the warm path now makes one lock acquisition and one cache walk instead of 120, which may explain part of it.

Command (same hardware and method as the 8.2 section: Apple M5, 10 cores, 16 GB, macOS, Go 1.25, PostgreSQL 16 on Docker port 55432):

`cd backend && PARALLEL_TEST_DATABASE_URL=postgres://parallel_app:app_dev_password@127.0.0.1:55432/parallel_test?sslmode=disable go test ./internal/httpapi -run '^$' -bench 'TickFrames' -benchtime 20x -count 5`

| Case | Runs (ms/op) | Median | Budget | Result |
|---|---|---|---|---|
| TickFrames 120 warm | 0.685, 0.681, 0.685, 0.663, 0.706 | 0.685 | 1000 ms (29.7) | met |
| TickFrames 120 cold | 114.58, 120.63, 112.74, 115.56, 110.23 | 114.58 (max 120.63) | 1000 ms (29.7) | met by about 879 ms |

Same caveat as before: a developer laptop, not the demo host; percentile-of-means over 5 samples.

Raw output:

```
BenchmarkAPITickFrames/warm-10         	      20	    685171 ns/op
BenchmarkAPITickFrames/warm-10         	      20	    681377 ns/op
BenchmarkAPITickFrames/warm-10         	      20	    684619 ns/op
BenchmarkAPITickFrames/warm-10         	      20	    663090 ns/op
BenchmarkAPITickFrames/warm-10         	      20	    705831 ns/op
BenchmarkAPITickFrames/cold-10         	      20	 114580865 ns/op
BenchmarkAPITickFrames/cold-10         	      20	 120631413 ns/op
BenchmarkAPITickFrames/cold-10         	      20	 112736996 ns/op
BenchmarkAPITickFrames/cold-10         	      20	 115561608 ns/op
BenchmarkAPITickFrames/cold-10         	      20	 110230323 ns/op
```

## Live provider run

Not run. Live provider verification is PENDING: no credentials were available during development. Fill this table only from the report of `parallel-sim llm-check --confirm-paid-call` (procedure: `docs/live-llm-verification.md`).

| Field | Value |
|---|---|
| Date (UTC), provider host, model id | not run |
| Completion latency (ms), HTTP status | not run |
| Prompt, completion, total tokens (provider-reported) | not run |
| Reserved estimate | not run |
