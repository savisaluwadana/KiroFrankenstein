# Demo script (under 3 minutes)

Presenter's script for the Parallel video. Every number below comes from a real run of the default path (seed 2026, engine version 2, no LLM configured) by the Playwright test `web/e2e/demo-path.spec.ts`, and was cross-checked with `scripts/demo-numbers.sh` against a fresh API. The numbers are **observed** under the synthetic assumptions of the flagship scenario. If an assumption, the scenario or the engine version changes, they change, and this file must be updated (the numbers and the fork plan come from `web/e2e/fixtures/demo-plan.json` and `docs/calibration.md`).

> Synthetic simulation output under the stated assumptions. Not a forecast or guarantee of real-world results.

## Demo plan (copied from `web/e2e/fixtures/demo-plan.json`)

The block below is machine-checked by `TestDemoScriptMatchesPlan` (`backend/internal/app/demoplan/docs_test.go`) against the fixture and `demoplan.Derive`. The test also re-runs the flagship in process and checks that the profits at ticks 48 and 120, the orders waiting at tick 48, both state hashes at tick 120, the duel scores and the duel figures quoted in this file are the ones the engine computes.

```demo-plan
forkTick: 40
commands:
- place_purchase_order sku=speaker role=backup qty=339
- hire_temp_workers qty=3
```

- Seed: 2026. Scenario: Black Friday Meltdown (120 simulated hours, Thursday 12:00 AM to Tuesday 12:00 AM on the clock).
- Crisis tick 38 (Friday, 2:00 PM): first hour with a stock-bound order or an active disruption in the no-command run.
- Demo fork tick 40 (Friday, 4:00 PM): the first scheduled decision window at or after the crisis tick whose backup order arrives before the horizon.
- Demo commands, placed at tick 40 and effective at tick 41 (Friday, 5:00 PM):
  1. `place_purchase_order`: speaker (SKU index 2), backup supplier, quantity 339.
  2. `hire_temp_workers`: quantity 3.
- Nothing in this plan states who wins or what the profit will be.

## Before you record

Open three terminals. Do not use ports that other things on your machine already use.

```bash
# terminal 1: API on 8080, in-memory store (worlds are lost when it stops), no LLM variables set
cd backend && WEB_ORIGIN=http://localhost:3100 go run ./cmd/parallel-api

# terminal 2: web app on 3100 (NEXT_PUBLIC_API_BASE_URL defaults to http://localhost:8080/api/v1)
cd web && npm run dev          # or: npm run build && npx next start -p 3100 for a smoother recording

# terminal 3: for the technical moments, from the repository root
cd backend
```

Optional PostgreSQL: with `docker compose up -d db`, the migration step and `DATABASE_URL` in `README.md` ("Persistence" section), worlds survive an API restart and are rebuilt by replay. The script below does not need it.

Dry run (about 1 s, no browser): `scripts/demo-numbers.sh` (or `scripts/demo-numbers.sh http://localhost:8080/api/v1`) creates its own worlds and prints the numbers quoted below.

Reset: stop the API with Ctrl-C and start it again. Memory-store worlds are gone, so the next "Start Black Friday demo" is a clean world. Reload the browser tab.

Fallback if the API restarts mid-recording: the in-memory worlds are lost and the open page shows an error. Do not try to repair it. Go to `http://localhost:3100/briefing` and start again from step 1; the demo is deterministic, so the same click path gives the same numbers.

## Script

| Time | Click | Say | Real numbers on screen |
|---|---|---|---|
| 0:00 to 0:10 | Landing page, then "Begin your shift" | "Reality has no undo button. We built one. This is a real warehouse simulation, running a Black Friday week hour by hour." | Starting cash $100,000.00 |
| 0:10 to 0:20 | "Start Black Friday demo", then set "Pause automatically at" to "surprises only", speed "Fast", press Play | "Everything on screen is computed by a deterministic engine. Nothing is scripted." | Playback pauses at Saturday, 12:00 AM (tick 48) |
| 0:20 to 0:30 | Show the alert cards | "Demand is surging, Speakers are out of stock, and the packing line can't keep up." | Cards: "Orders are surging across the store", "Waiting for inventory: Speakers", "Waiting for packing capacity". Profit so far +$50,757.48, orders waiting 527, delivered on time 100.0% |
| 0:30 to 0:40 | Drag the time strip back to Friday, 4:00 PM (tick 40), then "Explore another future" | "Rewinding is just a read. Changing the past is a fork: everything before this hour stays exactly as it happened." | Fork sequence "Same history up to Friday, 4:00 PM" |
| 0:40 to 0:55 | In the fork: order 339 Speakers from the backup supplier, hire 3 temporary workers, "Lock in decisions", Continue, Play | "I'll order from the backup supplier and add three temporary workers." | Moves "take effect at Friday, 5:00 PM" |
| 0:55 to 1:10 | "Compare futures", then "What changed?" | "Same history, different decisions. Here is what the engine observed. It does not say this decision caused that result." | At Saturday, 12:00 AM: original path profit $50,757.48, orders waiting 527; forked reality profit $50,525.00, orders waiting 479; on time 100.0% in both. First visible difference: Friday, 5:00 PM. The timed crisis-to-compare segment takes 29 to 30 s in the automated test, including a 2 s wait before each action |
| 1:10 to 1:25 | (Optional, cut if short) play both to Tuesday 12:00 AM | "Over the full week the two realities have diverged." | At the horizon: original profit $17,784.00 with 44.3% delivered on time; forked profit $21,611.61 with 47.0% on time; orders waiting 4,788 in both. Observed, not predicted |
| 1:25 to 1:55 | Briefing, "Challenge the AI manager" | "Now a duel against the rule-based AI. Same customers, same surprises, same rules for both of us. No language model is involved." | Hours 0 to 40 pass with no moves from you; the AI's moves are listed with "AI rationale (not verified)" |
| 1:55 to 2:15 | At Friday, 4:00 PM place the same two moves, then play to the end of the week | "I make the same two moves. The AI makes its own." | Scoreboard "Provisional" until the week ends |
| 2:15 to 2:35 | Scoreboard, then click "Profit so far" on your business and open the ledger | "Every figure is a sum of ledger entries." | Final score, see below. Ledger: Profit so far $21,611.61 from 6,179 entries |
| 2:35 to 2:50 | Say the disclaimer | "These numbers are synthetic, computed under assumptions you can inspect and change. They are not a forecast." | Footer disclaimer visible |

The ledger line is for the human business of the duel (net profit 2,161,161 cents, 6,179 entries), which equals the forked reality's profit above: the same plan from the same hour gives the same state.

## Duel result of the recorded run

Human plays only the two demo moves at tick 40 and ends every other window with no moves. Scored at tick 120 (Tuesday, 12:00 AM):

| | You | Rule-based AI |
|---|---|---|
| Points (of 100) | 26.53 | 56.83 |
| Profit so far | $21,611.61 | $35,957.50 |
| Orders delivered on time | 47.0% | 100.0% |
| Orders waiting | 4,788 | 798 |
| Orders cancelled | 4,921 | 0 |

Outcome shown by the scoreboard: "AI won". This is **one observed run under synthetic assumptions**. It describes this plan against this policy only; it does not show that an AI manager is better in general, and it changes if any assumption changes or if you play differently. In this run the human plan is deliberately minimal (two moves, then passive). The video must not claim otherwise. If you play more actively, say so and read the numbers from the screen instead of from this table.

## Technical moments (terminal, for the judges)

Run from `backend/`.

1. Replay proves the result is computed: the forked run's horizon hash.

   ```bash
   go run ./cmd/parallel-sim verify --scenario scenarios/black-friday.json --seed 2026 \
     --inputs scenarios/demo-fork-inputs.json --expect $(cat scenarios/golden/demo-fork.hash)
   ```

   Observed output: `state_hash: 9786f5397548d09290ed9b5f122172eb009546118d1d1b9d8d9d31015b8daca8`, `net_profit_cents: 2161161`, `service_level_bps: 4695`, then `MATCH`. The browser run reaches the same hash for the forked Branch at tick 120 (the e2e test asserts it against an independent second run).

   Say: "Same seed, same recorded inputs, same state hash. Replay is exact."

2. The original path without commands: `go run ./cmd/parallel-sim run --scenario scenarios/black-friday.json --seed 2026` prints `state_hash: f6f414259c1cbbc7f76dd0c0d2b129cc34d6e98e906868dfea7d68787fffac80`, `net_profit_cents: 1778400`, `service_level_bps: 4427`.

3. 20-seed paired ensemble (CLI only, no LLM, no network):

   ```bash
   go run ./cmd/parallel-sim ensemble --scenario scenarios/black-friday.json --seeds 1..20 \
     --arm fixed:scenarios/demo-duel-human.json --arm rulebased --out /tmp/ens
   cmp /tmp/ens.json scenarios/golden/ensemble-flagship-20.json && echo IDENTICAL
   ```

   Observed (the committed report `backend/scenarios/golden/ensemble-flagship-20.json`, reproduced byte for byte in the run used to write this file):
   - Arm 0, a fixed recorded plan that cannot adapt: composite score median 26.60 (min 25.90, max 27.44); net profit $21,611.61 in every seed; service level 45.7% to 48.8%.
   - Arm 1, the rule-based adaptive policy: composite score median 61.46 (min 55.32, max 65.98); net profit median $53,561.84 (min $30,212.20, max $70,713.93); service level 100.0% in all 20 seeds.
   - Paired by seed, arm 1 scored higher in 20 of 20 seeds, 0 ties, 0 for arm 0. Paired score difference: median 34.75 points, min 28.40, max 40.08.

   Say: "This is observed under these synthetic assumptions with one fixed plan as the comparison arm. A fixed plan cannot react to each seed and the policy can. It is not a claim about real warehouses or about AI in general." The report states this asymmetry itself.

## Automated check of this script

`cd web && npm run e2e` runs `e2e/demo-path.spec.ts`: a custom-assumptions test (steps 1 to 3) and the default path (steps 3 to 9). It asserts that the crisis, fork and compare segment finishes in under 60 s of real time (measured 28.8 to 29.9 s in the recorded runs), that figures on screen equal API responses, that the forked horizon hash equals a second independent run, that the two realities differ in profit and service level, and it never asserts a profit value, the sign of a difference, or a winner. The measured numbers of the last run are in `web/e2e/.artifacts/demo-numbers.json`.
