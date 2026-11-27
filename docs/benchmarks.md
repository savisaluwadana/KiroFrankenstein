# Benchmarks

Early benchmark report (Requirement 29.8 to 29.11). Every figure below comes from the raw output in the last section. Correctness checks (validation, invariants, canonical encoding, state hashing) stay enabled in the measured build. Results are for synthetic simulation only.

## Status of each budget

| Criterion | Budget | Status |
|---|---|---|
| 29.1 Flagship replay, Tick 0 to 120, Sim_CLI-equivalent in-process | 500 ms | **Measured (early proxy, in-process engine loop)**: median 87.4 ms, p95 98.3 ms. Met. |
| 29.2 50-Seed Paired_Experiment | 60 s | **target, not yet measured.** Early proxy only (100 sequential replays): median 9.05 s, p95 9.23 s. The real Ensemble_Runner measurement is still required before Requirement 36 starts (29.9). |
| 29.3 State read p95 300 ms | | target, not yet measured |
| 29.4 Fork 500 ms | | target, not yet measured |
| 29.5 Diff 1 s | | target, not yet measured |
| 29.6 Advance to horizon 1 s | | target, not yet measured |
| 29.7 Tick_Frames range 1 s | | target, not yet measured |

Note: the figures below were measured with engine version 1 (`sim.EngineVersion` = 1, hash `96e7d8d9...`). Engine version 2 (task 3.5) adds one disruption-stream draw per Tick and one encoded byte per active disruption; the numbers predate it and have not been re-measured.

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
