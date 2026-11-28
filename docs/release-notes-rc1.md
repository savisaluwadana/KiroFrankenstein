# Parallel v0.1.0-rc.1 (release candidate)

Verified at commit `0d1a192` ("docs: M8 build log, task reconciliation and real CI history"), 2026-11-28. Tag: `v0.1.0-rc.1`.

All results are from a synthetic simulation. They are not predictions about real businesses.

## Summary

Parallel is a playable, forkable simulation of an e-commerce warehouse. You run the business hour by hour, inject disruptions, rewind to any past hour, fork an alternative reality, and compete against an AI manager under identical external conditions. Every money figure is a sum of ledger entries, and identical inputs reproduce identical state hashes.

This candidate is feature-frozen. Two optional polish items are not complete (see Known limitations). It has not been deployed, the demo video is not recorded, and the live LLM path has not been run against a real provider.

## What is included

- **M0 to M1, vertical slice:** Go simulation core (integer cents, basis points, project-owned PRNG streams, hybrid discrete-event clock, canonical SHA-256 state hash), in-memory store, REST API with OpenAPI 3.1, PixiJS warehouse diorama with playback (1x, 4x, 16x, stepping).
- **M2, full engine and finance:** double-entry ledger (10 accounts), moving-average inventory valuation, purchasing, workforce, disruptions, command validator, the Black Friday Meltdown flagship scenario.
- **M3, persistence and branching:** append-only input log in PostgreSQL (UPDATE, DELETE and TRUNCATE rejected), snapshots verified by hash, replay, rewind, fork, SSE tick progress.
- **M4, studio:** timeline, branch tree (React Flow), decision windows and command builder, ledger drill-down.
- **M5, duel:** human versus AI manager from one tick, with a rule-based AI and an optional LLM AI (OpenAI-compatible endpoint, call and token limits, rule-based fallback), parity and exogenous-fairness guarantees, 100-point duel score.
- **M6, comparison:** branch diff, CLI multi-seed ensembles (`parallel-sim ensemble`, up to 50 seeds per arm), flagship calibration report (`docs/calibration.md`), golden hashes and a golden 20-seed ensemble report.
- **M7, hardening:** write-token capability model, HTTP hardening, API benchmarks (`docs/benchmarks.md`), Playwright demo path, local production rehearsal.
- **M8, documentation:** README, architecture notes, security model, demo script (checked by `TestDemoScriptMatchesPlan`), hackathon material in `docs/hackathon/`, docs link checker.

## Verification

| Where | Run or command | Commit | Date | Result |
|---|---|---|---|---|
| GitHub Actions | run 46133835646 | `0d1a192` | 2026-11-28 | success; six jobs: openapi lint, go (amd64), go (arm64), web, docker build, e2e (playwright, chromium) |
| GitHub Actions | run 46126448247 | `e666480` | 2026-11-28 | failed in e2e only: the fork-sequence Skip click raced the overlay's own 1.5 s end on a slow runner. Fixed by "test(e2e): end the fork sequence with Escape" |
| Local | `scripts/ci-local.sh` with Docker and e2e | `0d1a192` | 2026-11-28 | all 22 steps passed |
| Local | vitest | `0d1a192` | 2026-11-28 | 276 tests in 36 files |
| Local | Playwright | `0d1a192` | 2026-11-28 | 24 passed, 10 skipped (desktop-only specs skipped on the mobile project), 0 failed, 6.6 min |
| Local | frame stats (headless Chromium) | `0d1a192` | 2026-11-28 | 60.0 fps, p95 16.7 ms, max 16.8 ms, 0 frames over 33 ms |
| Local, linux/amd64 emulation | `TestGoldenFlagshipHash`, `TestGoldenEnsembleReport`, `TestGoldenDuelHashes`, `TestDemoScriptMatchesPlan` | `0d1a192` | 2026-11-28 | all ok |
| Local | demo numbers vs `docs/demo-script.md` | `0d1a192` | 2026-11-28 | match (see below) |
| Local | README Quick start re-run | `0d1a192` | 2026-11-28 | OK: API health ok, engineVersion 2; web `/` and `/briefing` return 200 |
| Local | `parallel-sim llm-check` | `0d1a192` | 2026-11-28 | exit 4, PENDING (no credentials) |
| Local | `scripts/check-docs.sh` | `0d1a192` | 2026-11-28 | ok (22 files) |

Demo numbers confirmed: fork tick 40; profit at tick 48 $50,757.48 (original) and $50,525.00 (fork); at tick 120 $17,784.00 with 44.3% service level (original) against $21,611.61 with 47.0% (fork); state hashes `f6f41425…` and `9786f539…`; duel score 26.53 against 56.83; AI profit $35,957.50; 6,179 ledger entries.

Not done: no live LLM call was made, nothing was deployed to paid infrastructure, the Devpost form is not submitted and the demo video is not recorded.

## Known limitations

**Medium**

- The fork transition runs 1.5 s against the 600 ms requirement (task 8.6, accepted deviation).
- The duel result reveal is not built (task 8.7, skipped under feature freeze). The scoreboard shows final values at once.
- The rule-based AI overspends on labour and underperforms some human-style plans in the calibrated scenario. This is recorded in `docs/calibration.md` and was not tuned away.
- No hard monetary cap on LLM spending in the app, only call and token limits. Set a credit limit at the provider.
- Live LLM verification is pending (`parallel-sim llm-check` exits 4; only stub providers were tested).

**Low**

- On phones, the decision sheet covers the diorama.
- Per-IP limits are held in memory and reset on restart.
- Connection pooler caveat for PostgreSQL deployments (see `docs/deploy.md`).
- Benchmarks come from one laptop (Apple M5), not the demo host (`docs/benchmarks.md`).
- The flagship is supply-limited, so fixed-plan profits are seed-independent.
- No accounts: a world ID is a read-only link and the write token is the only mutation control.

## Task reconciliation

Source: `.kiro/specs/parallel-simulation-mvp/tasks.md`.

| Group | Count | State |
|---|---|---|
| Required sub-tasks in sections 1 to 9 | 129 | 127 done, 2 open (8.6 accepted deviation, 8.7 skipped) |
| Optional property tests in sections 1 to 9 | 8 | skipped (2.8, 2.28, 2.29, 3.10, 7.3, 7.5, 7.13, 7.14) |
| Stretch required items (section 10) | 5 | not started (10.1, 10.3, 10.4, 10.6, 10.7) |
| Stretch optional property tests | 3 | not started (10.2, 10.5, 10.8) |

The 127 of 129 figure assumes the orchestrator ticks 9.3 and 9.4 in `tasks.md`.

## Manual steps remaining

1. Record the demo video (under three minutes), following `docs/demo-script.md` and `docs/hackathon/`.
2. Optional: deploy (`docs/deploy.md`). This needs your approval and paid infrastructure.
3. Optional: configure a real provider key (`LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL`), run `parallel-sim llm-check --confirm-paid-call`, and play one live LLM duel (`docs/live-llm-verification.md`).
4. Fill the Devpost placeholders (`docs/hackathon/`), re-check the official rules and deadline.
5. Submit to Devpost after approval.

## How to verify this release candidate

```sh
git checkout v0.1.0-rc.1
gh run view 46133835646                    # GitHub Actions: success, six jobs
scripts/ci-local.sh                        # needs Docker; see docs/ci.md for flags
cd backend && go test ./... -count=1
cd backend && go run ./cmd/parallel-sim run --scenario scenarios/black-friday.json --seed 2026   # state hash f6f41425…
cd backend && go test ./internal/sim -run TestGoldenFlagshipHash -count=1
cd backend && go test ./internal/app -run 'TestGoldenDuelHashes|TestDemoScriptMatchesPlan' -count=1
cd backend && go test ./internal/app/ensemble -run TestGoldenEnsembleReport -count=1
cd web && npx tsc --noEmit && npx eslint . && npx vitest --run
bash scripts/check-docs.sh
```
