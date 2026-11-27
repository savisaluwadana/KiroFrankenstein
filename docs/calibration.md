# Calibration report: Black Friday Meltdown

Task 7.16, Requirements 36.1 to 36.10, 3.9 and 34.9. Engine version 2 (unchanged by calibration).

> Synthetic simulation output under the stated assumptions. Not a forecast or guarantee of real-world results.

## 1. Purpose and the one rule that governs it

The flagship Scenario was calibrated so that a manager's decisions have operational and financial trade-offs, and so that both inventory decisions and staffing decisions matter at different moments of the five days.

**No Assumption was changed to make any manager win.** The Rule_Based_Manager, a Human Manager, the recorded demo plan and every other plan in this report are measured by the same harness under the same Seeds. No Duel outcome, no Demo_Path outcome and no relative score of a Human and an AI is a calibration target. The only two gates (section 6) look at the no-Command run and at the Rule_Based_Manager run against it. The 20-Seed results (section 8) are reported as observed, without a target. The decision logic of the Rule_Based_Manager was not touched.

## 2. Method and how to rerun

Everything below comes from one reproducible harness (`backend/internal/app/calibration`), driven either by the CLI or by the tests. It runs the flagship Scenario under Seeds 1..20 for a set of arms, validates every Command with the same `sim.Validate` as the product (an invalid Command is skipped and counted, never altered), and reads the per-Tick Fulfilment_Constraint_Report to record in which Ticks orders were stock-bound (open because on-hand stock was short) or capacity-bound (open because packing capacity ran out).

```
cd backend
go run ./cmd/parallel-sim calibrate --scenario scenarios/black-friday.json --seeds 1..20      # every table in this report
go test ./internal/app/calibration -run 'TestCalibrationGates|TestCalibrationObservations' -v  # gates and observations
go test ./internal/app/demoplan ./internal/sim -run 'TestDemo|TestGoldenFlagshipHash|TestFlagship'
```

Arms (all human-style plans are fixed lists of timed Commands; none is optimised, none was searched for to produce a result):

| Arm | Definition |
|---|---|
| no commands | No Command is ever submitted. |
| rule-based AI | `internal/policy/rulebased` decides at every Decision_Window, under the Scenario's Command_Budget and validator. |
| recorded demo human plan | The Demo_Commands of section 9 at the Demo_Fork_Tick. |
| inventory-only (restock 4 SKUs) | At the Demo_Fork_Tick and every 12 Ticks after it (40, 52, 64, 76, 88, 100): one backup-supplier order per SKU of 400, 750, 750, 450, 300, 300 units. 300 units is about 12 hours of normal demand per SKU (25 orders per SKU per hour); the larger quantities cover the hours that overlap the surge. |
| staffing-only (8 temps) / (overtime 3 h in the surge) | 8 temporary workers hired at Tick 40 (ready at Tick 46), or 3 overtime hours per worker from Tick 48 to Tick 72, and nothing else. |
| both (restock + 8 temps) / (restock + overtime) | The two lines above combined. |
| wrong lever | 30 temporary workers and 4 overtime hours per worker from the Demo_Fork_Tick to the horizon, with no restock: staffing when stock is the limit. |

The lever sweeps (section 7) vary the number of temporary workers, the Tick at which they are hired and the Tick at which restocking starts, on top of the restock plan.

## 3. Diagnosis: why a lever could not matter (before calibration)

Engine version 2, Seed 2026, before any change (tables in section 5):

1. **Stock ran out before anything else happened.** Opening stock was 2,800 units for demand of about 100 orders per Tick (25 per SKU). The speaker was out of stock at Tick 19, every SKU at Tick 40 (no-command run). The surge (Tick 48), the staff absence (Tick 54) and the primary-supplier outage (Tick 60) all arrived at an empty warehouse, so they changed nothing in the no-command run.
2. **Packing capacity never bound.** 20 regular workers at 8 units per hour give 160 units per hour. The busiest normal Tick shipped 142 units. After Tick 40 nothing could ship at all. Result: the no-command run had capacity-bound orders in only 2 of 20 Seeds (a mean of less than one Tick) and none at Seed 2026, and in the family of fixed plans below, adding temporary workers or overtime only added wages (`staffing-only` lost $16,000 against `no commands`, and `both` earned $16,000 less than `inventory-only` with identical service level and backlog). Staffing could never matter.
3. **The demo plan therefore held a single purchase order**, forked at Tick 20 (the first speaker stock-out at Tick 19), 28 Ticks before the first scripted Disruption.
4. The Profit_Floor and Profit_Target (-$20,000, $60,000) were provisional and sat far from any observed outcome: the best fixed plan earned $129,000.

Parameters examined and **left unchanged** because the data did not call for a change: surge magnitude (x2.5 for 24 Ticks gives about 250 orders per hour against 112 units of capacity, which is a real bottleneck once stock exists), base arrivals, conversion, prices and unit costs (every SKU has a positive unit margin at both suppliers after the $4.00 freight; for example headphones earn $25.99 per unit from the primary supplier and $19.99 from the backup), supplier lead times and cost premiums, wages, cancel threshold, service credit, temp onboarding delay, overtime cap, temp cap, opening cash, command budget and cadence, Score_Weights, Disruption_Script onsets and durations.

## 4. Changes made

Four Assumption groups changed. Each row records the path, previous value, new value and reason. Nothing else in the Scenario changed except rationale wording as listed.

| # | Path | Previous | New | Reason (operational) |
|---|---|---|---|---|
| 1 | `skus.headphones.openingUnits` | 700 | 1400 | Stock must last into the surge. The warehouse starts the five days stocked for about 56 hours of average normal demand (25 orders per SKU per hour), not 28. With the old values a stock-out at Tick 19 made every later event irrelevant (section 3). Doubling every SKU keeps the mix and puts the first stock-out at Tick 38 (speaker), the others at Ticks 55 (headphones), 61 (watch band) and 64 (charger) once the surge drains them, so both stock and packing capacity are live questions from Tick 38 on. |
| 2 | `skus.charger.openingUnits` | 900 | 1800 | Same reason. |
| 3 | `skus.speaker.openingUnits` | 400 | 800 | Same reason. The speaker is the first SKU to run out, which is what makes the first crisis visible before the surge. |
| 4 | `skus.watchband.openingUnits` | 800 | 1600 | Same reason. |
| 5 | `workforce.regularWorkers` | 20 | 14 | Regular packing capacity of 160 units per hour could not be exceeded by a normal-hour load of about 100 orders, so staffing could never matter. At 14 workers (112 units per hour) the average normal hour is still served (about 89% utilisation), evening peaks and the surge (about 250 orders per hour, up to 450 at the evening peak) exceed capacity, and the absence (-30% of regular workers, 4 of 14 workers, so 80 units per hour) deepens the gap. Capacity-bound orders appear at the surge onset only when stock exists to ship, which is why change 1 to 4 and change 5 go together. In exploratory runs (not part of the committed harness) 16 workers gave a small lever, where a restock alone already reached 100% service level and staffing changed profit only; 14 workers also moves service level (88.1% without staffing against 100% with 8 temporary workers, section 5). 14 was chosen as the largest value tried that shows an effect on service level. The choice came from the staffing sweep, not from any manager's result. |
| 6 | `profitFloorCents` | -2,000,000 (-$20,000, provisional) | 1,000,000 ($10,000) | See section 10. The no-command profit is $17,784 for every Seed; the floor is that figure rounded down to a round number, so doing nothing earns almost no financial points. |
| 7 | `profitTargetCents` | 6,000,000 ($60,000, provisional) | 20,000,000 ($200,000) | See section 10. A round number above the best profit observed by any fixed plan (the harness maximum is $181,905). It is never the profit of any manager. |

Wording-only changes (no value changed): `demand.baseArrivalsPerTick` rationale now says "per SKU" (it always was per SKU in the engine); `skus.*.openingUnits` rationale now says "about 56 hours of average normal-profile demand, enough to reach the Black Friday surge but not to serve it"; `workforce.regularWorkers` rationale describes the 112 units per hour; the two profit rationales say "calibrated by observation (docs/calibration.md)". Every rationale still carries the label "synthetic, illustrative" (Requirement 3.6).

**One demo-plan rule refinement** (not an Assumption): the hire count of `demoplan.Derive` was `ceil(peak capacity-bound units / units per worker hour)`, which asks for enough workers to clear a whole backlog within one hour. It is now `ceil(peak / (units per worker hour x shortest promise window))`, enough extra capacity to work the largest capacity-bound backlog off within one delivery promise (24 Ticks), the time after which its orders become late. With the calibrated Scenario the old rule gave 50 workers (capped at 30); the new rule gives 3. The sizing is a documented, principled rule; it was not chosen from any outcome.

**Engine and golden consequences.** These are Scenario data changes, not engine changes: `sim.EngineVersion` stays 2 and the state encoding is unchanged, so the sim encode goldens did not move. The golden hashes change because the Scenario changed, and were regenerated in the same change set: `scenarios/golden/no-command.hash`, `demo-fork.hash` and `.ticks`, `demo-duel.hashes`, `demo-fork-inputs.json`, `demo-duel-human.json`, `demo-duel-ai.json`, `web/e2e/fixtures/demo-plan.json` and the README hash line. Tests that pinned Scenario-dependent literals were updated deliberately, not weakened: opening stock 1400 and 14 regular workers in `TestInitFlagship`, the "calibrated by observation" label in `TestFlagshipScenario`, the demo human final hash in the ensemble tests, and the demo plan test (the flagship plan now holds two Commands, a test with 200 regular workers covers the no-staffing path).

| Hash | Before | After |
|---|---|---|
| Seed 2026 no-Command final hash | `4c6d0dd08cf7c1c8c27991355f734705356aced57fe060556e0d038ad03ab99e` | `f6f414259c1cbbc7f76dd0c0d2b129cc34d6e98e906868dfea7d68787fffac80` |
| Demo fork, final | `a51c195a82713024b615292cc5491a29f8af57d35633b8bd706af9abaefbe2a5` | `9786f5397548d09290ed9b5f122172eb009546118d1d1b9d8d9d31015b8daca8` |
| Demo duel, human final | `a51c195a82713024b615292cc5491a29f8af57d35633b8bd706af9abaefbe2a5` | `9786f5397548d09290ed9b5f122172eb009546118d1d1b9d8d9d31015b8daca8` |
| Demo duel, AI final | `e9f1929610cc0414bb5e5a589bddbc62b1d19fdeba394b625b4bdf0ce6a284aa` | `647520ec85d34b01687fbd146634ce056c63454f0fe4e4a948dc5058d82c1f41` |

## 5. Before and after, same arms, Seeds 1..20

Figures are means over the Seeds with min..max in brackets. "Seeds stock-bound" and "Seeds cap-bound" count Seeds with at least one stock-bound or capacity-bound order; "Cap-bound ticks" is the mean number of Ticks with one. "Cmds ok/skipped" counts accepted and validator-rejected Commands per run. Composite is in points (0 to 100). The Profit_Floor and Profit_Target differ between the two tables (section 10), so the composite columns are not comparable across tables; the profit, service level and constraint columns are.

The "before" tables apply the same arm definitions to the previous Scenario (the harness then derives its Demo_Fork_Tick as 20, so its restock starts at Tick 20 and its hire is at Tick 40).

### Before (previous Scenario)

| Arm | Net profit mean (min..max) | Service level | Fill rate | Backlog orders | Cancelled | Labor | Composite mean (min..max) | Seeds stock-bound | Seeds cap-bound | Cap-bound ticks | Cmds ok/skipped |
|---|---|---|---|---|---|---|---|---|---|---|---|
| no commands | -$43428 (-$43428..-$43428) | 22.2% | 22.2% | 4799 | 7989 | $52800 | 11.12 (10.78..11.50) | 20/20 | 2/20 | 0 | 0/0 |
| rule-based AI | $68008 ($27225..$92681) | 37.2% | 83.7% | 1733 | 1878 | $81295 | 78.86 (56.33..89.73) | 20/20 | 20/20 | 27 | 43/0 |
| recorded demo human plan | -$34296 (-$34296..-$34296) | 24.7% | 24.7% | 4799 | 7674 | $52800 | 12.37 (11.98..12.80) | 20/20 | 20/20 | 1 | 1/0 |
| inventory-only (restock 4 SKUs) | $128985 ($128773..$129029) | 100.0% | 100.0% | 996 | 0 | $52800 | 100.00 (100.00..100.00) | 20/20 | 20/20 | 69 | 24/0 |
| staffing-only (8 temps) | -$59428 (-$59428..-$59428) | 22.2% | 22.2% | 4799 | 7989 | $68800 | 11.12 (10.78..11.50) | 20/20 | 2/20 | 0 | 1/0 |
| staffing-only (overtime 3 h in the surge) | -$90948 (-$90948..-$90948) | 22.2% | 22.2% | 4799 | 7989 | $100320 | 11.12 (10.78..11.50) | 20/20 | 2/20 | 0 | 2/0 |
| both (restock + 8 temps) | $112985 ($112774..$113029) | 100.0% | 100.0% | 996 | 0 | $68800 | 100.00 (100.00..100.00) | 20/20 | 20/20 | 37 | 25/0 |
| both (restock + overtime 3 h in the surge) | $81465 ($81254..$81509) | 100.0% | 100.0% | 996 | 0 | $100320 | 100.00 (100.00..100.00) | 20/20 | 20/20 | 12 | 26/0 |
| wrong lever (30 temps + 4 h overtime, no restock) | -$382428 (-$382428..-$382428) | 22.2% | 22.2% | 4799 | 7989 | $391800 | 11.12 (10.78..11.50) | 20/20 | 2/20 | 0 | 2/0 |

Evidence at Seed 2026 (Ticks at which the Fulfilment_Constraint_Report lists a stock-bound or capacity-bound order):

| Arm (Seed 2026) | Stock-bound Ticks (first) | Capacity-bound Ticks (first, peak units @ tick) | Max open-order queue |
|---|---|---|---|
| no commands | 19-120 (19) | none (-1, 0 @ 0) | 8459 |
| rule-based AI | 19-101, 112-120 (19) | 49-50, 57-58, 79-85, 102-112 (49, 7374 @ 102) | 7642 |
| recorded demo human plan | 19-28, 33-120 (19) | 29 (29, 90 @ 29) | 8459 |
| inventory-only (restock 4 SKUs) | 19-28, 36-40, 113-120 (19) | 29, 41-49, 54-113 (29, 3251 @ 71) | 3251 |
| staffing-only (8 temps) | 19-120 (19) | none (-1, 0 @ 0) | 8459 |
| staffing-only (overtime 3 h in the surge) | 19-120 (19) | none (-1, 0 @ 0) | 8459 |
| both (restock + 8 temps) | 19-28, 36-40, 88, 102-120 (19) | 29, 41-47, 55-85 (29, 2129 @ 71) | 2129 |
| both (restock + overtime 3 h in the surge) | 19-28, 36-40, 88, 102-120 (19) | 29, 41-48, 92-94 (29, 120 @ 45) | 1069 |
| wrong lever (30 temps + 4 h overtime, no restock) | 19-120 (19) | none (-1, 0 @ 0) | 8459 |

### After (calibrated Scenario)

| Arm | Net profit mean (min..max) | Service level | Fill rate | Backlog orders | Cancelled | Labor | Composite mean (min..max) | Seeds stock-bound | Seeds cap-bound | Cap-bound ticks | Cmds ok/skipped |
|---|---|---|---|---|---|---|---|---|---|---|---|
| no commands | $17784 ($17784..$17784) | 44.4% | 44.4% | 4799 | 5189 | $36960 | 24.29 (23.60..25.05) | 20/20 | 20/20 | 40 | 0/0 |
| rule-based AI | $50394 ($30212..$70714) | 100.0% | 100.0% | 811 | 0 | $184906 | 60.62 (55.32..65.98) | 20/20 | 20/20 | 26 | 36/0 |
| recorded demo human plan | $21612 ($21612..$21612) | 47.1% | 47.1% | 4799 | 4850 | $42960 | 26.64 (25.90..27.44) | 20/20 | 20/20 | 39 | 2/0 |
| inventory-only (restock 4 SKUs) | $107356 ($94585..$116059) | 88.1% | 96.1% | 3071 | 411 | $36960 | 72.09 (65.46..77.10) | 20/20 | 20/20 | 100 | 24/0 |
| staffing-only (8 temps) | $1784 ($1784..$1784) | 44.4% | 44.4% | 4799 | 5189 | $52960 | 22.24 (21.54..23.00) | 20/20 | 20/20 | 28 | 1/0 |
| staffing-only (overtime 3 h in the surge) | -$15480 (-$15480..-$15480) | 44.4% | 44.4% | 4799 | 5189 | $70224 | 22.24 (21.54..23.00) | 20/20 | 20/20 | 28 | 2/0 |
| both (restock + 8 temps) | $157913 ($154673..$162690) | 100.0% | 100.0% | 138 | 0 | $52960 | 88.92 (88.07..90.18) | 20/20 | 20/20 | 77 | 25/0 |
| both (restock + overtime 3 h in the surge) | $134392 ($129220..$136641) | 100.0% | 100.0% | 357 | 0 | $70224 | 82.73 (81.38..83.32) | 20/20 | 20/20 | 81 | 26/0 |
| wrong lever (30 temps + 4 h overtime, no restock) | -$190056 (-$190056..-$190056) | 44.4% | 44.4% | 4799 | 5189 | $244800 | 22.24 (21.54..23.00) | 20/20 | 20/20 | 22 | 2/0 |

Evidence at Seed 2026:

| Arm (Seed 2026) | Stock-bound Ticks (first) | Capacity-bound Ticks (first, peak units @ tick) | Max open-order queue |
|---|---|---|---|
| no commands | 38-120 (38) | 10-27, 34-37, 43-49, 54-63 (10, 395 @ 60) | 7293 |
| rule-based AI | 36-72, 77-80, 82-101, 103-120 (36) | 10-21, 69, 73-79, 81, 102 (10, 4383 @ 73) | 4689 |
| recorded demo human plan | 38-48, 52-120 (38) | 10-27, 34-37, 43-51, 54-61 (10, 415 @ 49) | 7293 |
| inventory-only (restock 4 SKUs) | 38-48 (38) | 10-27, 34-37, 43-120 (10, 4137 @ 71) | 4137 |
| staffing-only (8 temps) | 38-120 (38) | 10-27, 34-37, 43-46, 57 (10, 349 @ 23) | 7293 |
| staffing-only (overtime 3 h in the surge) | 38-120 (38) | 10-27, 34-37, 43-48 (10, 349 @ 23) | 7293 |
| both (restock + 8 temps) | 38-48, 51-60, 106-108, 115-120 (38) | 10-27, 34-37, 43-46, 49-50, 55-102, 109 (10, 2895 @ 71) | 2895 |
| both (restock + overtime 3 h in the surge) | 38-48, 50-60, 67-72 (38) | 10-27, 34-37, 43-49, 61-66, 73-120 (10, 891 @ 73) | 973 |
| wrong lever (30 temps + 4 h overtime, no restock) | 38-120 (38) | 10-27, 34-37 (10, 349 @ 23) | 7293 |

What the tables show, as observed:

- **No-command run, before:** stock-bound from Tick 19, capacity-bound in only 2 of 20 Seeds (none at Seed 2026). **After:** the first stock-out with open orders is at Tick 38; capacity-bound orders appear in all 20 Seeds, including after the surge onset (Ticks 43 to 49 and 54 to 63 at Seed 2026).
- **Inventory is decisive.** Restocking lifts mean net profit from $17,784 to $107,356 and the service level from 44.4% to 88.1%, on every Seed (TestCalibrationObservations).
- **Staffing is decisive once stock exists, and worthless without it.** On top of the restock, 8 temporary workers lift the service level from 88.1% to 100% and net profit from $107,356 to $157,913, with the open backlog at the horizon falling from 3,071 to 138 orders. Without the restock, the same 8 temporary workers (`staffing-only`) cost $16,000 and change nothing else. Before calibration, staffing was never worth anything.
- **The levers are complements, not substitutes.** `staffing-only` never beat `inventory-only` on any Seed in any metric (profit 0 of 20, composite 0 of 20, service level 0 of 20), and `inventory-only` beat `staffing-only` on all 20. This is an observed fact, not a target. The trade-offs between the levers are in how much and when (section 7), and in how the two compete for the same hours of the plan.
- **Overtime and temporary workers differ.** Overtime of 3 h in the surge starts at once and can be switched off, but costs 1.5 times the wage on every regular worker (3 hours for 14 workers is 14 x 3 x $33 = $1,386 per hour); 8 temporary workers cost $200 per hour for the rest of the horizon (there is no release Command) and need 6 Ticks to become useful. In this run 8 temporary workers were cheaper (`both (restock + 8 temps)` $157,913 against $134,392 with overtime) and gave the same service level.
- **The wrong lever is expensive:** 30 temporary workers plus 4 overtime hours with no restock lost $190,056, against $17,784 for doing nothing.

## 6. Gates

Only two gates exist (Requirements 36.1, 36.2, 36.6), both in `TestCalibrationGates` (`backend/internal/app/calibration/calibration_test.go`). No gate compares a Human Manager with an AI Manager, and none requires any arm to win.

| Gate | Seed | Result |
|---|---|---|
| 36.1: the no-Command run has a Tick at which a SKU has zero on-hand stock and open orders | 2026 (default) | **Pass.** First such Tick: 38 (speaker). The no-command run is stock-bound for at least one SKU in all 20 Seeds. |
| 36.2: the Rule_Based_Manager run has a higher Composite_Score than the no-Command run | 2026 (default) | **Pass.** 56.83 against 24.18 points (5683 and 2418 hundredths). The Rule_Based_Manager is also above the no-Command run on each of Seeds 1..20 (section 8). |

Informational checks (never gates, recorded by `TestCalibrationObservations`, run with `-v`): capacity-bound orders occur in the no-command run at or after the surge onset on 20 of 20 Seeds; `staffing-only` versus `inventory-only` and the other pairs, by Seed and metric, are the counts quoted in section 5.

## 7. Lever sweeps (restocked warehouse), Seeds 1..20

Each row adds one change to the restock plan: the number of temporary workers hired at Tick 40, the Tick at which 8 temporary workers are hired, or the Tick at which the restock schedule starts (always with 8 temporary workers at Tick 40). Rows named "cheap restock" order from the cheaper primary supplier whenever the order is received before the primary outage begins at Tick 60 (otherwise from the backup supplier).

| Arm | Net profit mean (min..max) | Service level | Fill rate | Backlog orders | Cancelled | Labor | Composite mean (min..max) | Seeds stock-bound | Seeds cap-bound | Cap-bound ticks | Cmds ok/skipped |
|---|---|---|---|---|---|---|---|---|---|---|---|
| restock + 0 temps | $107356 ($94585..$116059) | 88.1% | 96.1% | 3071 | 411 | $36960 | 72.09 (65.46..77.10) | 20/20 | 20/20 | 100 | 24/0 |
| restock + 3 temps at tick 40 | $137486 ($135561..$139873) | 100.0% | 100.0% | 1775 | 0 | $42960 | 83.55 (83.04..84.18) | 20/20 | 20/20 | 99 | 25/0 |
| restock + 8 temps at tick 40 | $157913 ($154673..$162690) | 100.0% | 100.0% | 138 | 0 | $52960 | 88.92 (88.07..90.18) | 20/20 | 20/20 | 77 | 25/0 |
| restock + 16 temps at tick 40 | $141989 ($138752..$146753) | 100.0% | 100.0% | 138 | 0 | $68960 | 84.73 (83.88..85.99) | 20/20 | 20/20 | 53 | 25/0 |
| restock + 24 temps at tick 40 | $125999 ($122759..$130771) | 100.0% | 100.0% | 138 | 0 | $84960 | 80.52 (79.68..81.78) | 20/20 | 20/20 | 42 | 25/0 |
| restock + 30 temps at tick 40 | $113999 ($110759..$118771) | 100.0% | 100.0% | 138 | 0 | $96960 | 77.36 (76.52..78.62) | 20/20 | 20/20 | 38 | 25/0 |
| restock + 8 temps at tick 28 | $155513 ($152273..$160290) | 100.0% | 100.0% | 138 | 0 | $55360 | 88.29 (87.44..89.55) | 20/20 | 20/20 | 69 | 25/0 |
| restock + 8 temps at tick 40 | $157913 ($154673..$162690) | 100.0% | 100.0% | 138 | 0 | $52960 | 88.92 (88.07..90.18) | 20/20 | 20/20 | 77 | 25/0 |
| restock + 8 temps at tick 48 | $159505 ($156261..$164280) | 100.0% | 100.0% | 138 | 0 | $51360 | 89.34 (88.49..90.60) | 20/20 | 20/20 | 85 | 25/0 |
| restock + 8 temps at tick 56 | $160900 ($157797..$163421) | 100.0% | 100.0% | 143 | 0 | $49760 | 89.71 (88.90..90.38) | 20/20 | 20/20 | 93 | 25/0 |
| restock + 8 temps at tick 64 | $158322 ($152690..$161144) | 100.0% | 100.0% | 291 | 0 | $48160 | 89.03 (87.55..89.78) | 20/20 | 20/20 | 100 | 25/0 |
| restock + 8 temps at tick 72 | $148492 ($146338..$150949) | 100.0% | 100.0% | 795 | 0 | $46560 | 86.44 (85.88..87.09) | 20/20 | 20/20 | 100 | 25/0 |
| cheap restock from tick 24 (primary when it arrives before the outage) + 8 temps at tick 40 | $176365 ($172690..$181905) | 100.0% | 100.0% | 138 | 0 | $52960 | 93.78 (92.82..95.24) | 20/20 | 20/20 | 77 | 25/0 |
| cheap restock from tick 28 (primary when it arrives before the outage) + 8 temps at tick 40 | $165040 ($161692..$169993) | 100.0% | 100.0% | 138 | 0 | $52960 | 90.79 (89.92..92.10) | 20/20 | 20/20 | 78 | 25/0 |
| restock from tick 28 + 8 temps at tick 40 | $157513 ($154214..$162388) | 100.0% | 100.0% | 138 | 0 | $52960 | 88.81 (87.95..90.10) | 20/20 | 20/20 | 83 | 25/0 |
| restock from tick 52 + 8 temps at tick 40 | $149320 ($146115..$153523) | 100.0% | 100.0% | 439 | 0 | $52960 | 86.66 (85.82..87.77) | 20/20 | 20/20 | 79 | 21/0 |
| restock from tick 64 + 8 temps at tick 40 | $120495 ($115291..$124930) | 94.9% | 99.9% | 2051 | 1 | $52960 | 78.05 (75.40..79.92) | 20/20 | 20/20 | 73 | 17/0 |

Observed trade-offs (no row was selected, tuned or removed after seeing it):

- **Staffing level:** 0, 3, 8, 16, 24 and 30 temporary workers earn $107,356, $137,486, $157,913, $141,989, $125,999 and $113,999. Too few temporary workers leave service level and backlog on the table, too many cost $200 per hour each for no benefit (service level is already 100% at 3, and the open backlog stops falling at 8).
- **Staffing time:** 8 temporary workers hired at Tick 28, 40, 48, 56, 64 and 72 earn $155,513, $157,913, $159,505, $160,900, $158,322 and $148,492. Waiting saves wages until the 6-Tick onboarding delay lets the queue grow (backlog 143 at Tick 56, 291 at Tick 64, 795 at Tick 72).
- **Restock time:** restocking from Tick 28, 40, 52 and 64 earns $157,513, $157,913, $149,320 and $120,495, and from Tick 64 the service level falls to 94.9%. Restocking too late costs lost orders, not just money.
- **Supplier choice:** ordering from the cheaper primary supplier while it still arrives before the outage earned $165,040 (from Tick 28) and $176,365 (from Tick 24) against $157,913, but it needs the order to be placed 10 to 14 Ticks before the first crisis (Tick 38), when the stock-out has not yet happened.

## 8. Observed 20-Seed results (no target)

Per-Seed results of the three arms Requirement 36.7 asks for. "Composite" is in points. These are observations; no target outcome applies to any row.

Summary over Seeds 1..20: no commands, mean 24.29 points (23.60 to 25.05); rule-based AI, mean 60.62 (55.32 to 65.98); recorded demo human plan, mean 26.64 (25.90 to 27.44). The Rule_Based_Manager is above the no-command run on all 20 Seeds. Because the recorded demo plan is one purchase order and a 3-worker hire, the comparison of that plan with the Rule_Based_Manager says nothing about human skill. The human-style plans `inventory-only` and `both` scored 72.09 and 88.92 mean points in the same harness, above the Rule_Based_Manager, and the section 5 table shows why: the Rule_Based_Manager reached 100% service level but spent $184,906 on labor (against $52,960 for `both (restock + 8 temps)`) because it schedules overtime and temporary workers for the backlog and never lowers overtime afterwards. That is recorded here as observed, not changed.

### No commands

| Seed | Net profit | Service level | Fill rate | Backlog | Cancelled | Composite | First stock-bound tick | First cap-bound tick | Cap-bound ticks |
|---|---|---|---|---|---|---|---|---|---|
| 1 | $17784 | 43.3% | 43.3% | 4773 | 5514 | 23.74 | 38 | 9 | 40 |
| 2 | $17784 | 44.4% | 44.4% | 4836 | 5212 | 24.25 | 38 | 9 | 39 |
| 3 | $17784 | 44.0% | 44.0% | 4819 | 5280 | 24.06 | 39 | 9 | 40 |
| 4 | $17784 | 45.0% | 45.0% | 4753 | 5027 | 24.56 | 38 | 10 | 40 |
| 5 | $17784 | 44.9% | 44.9% | 4793 | 5045 | 24.54 | 39 | 10 | 42 |
| 6 | $17784 | 44.8% | 44.8% | 4763 | 5114 | 24.46 | 38 | 10 | 39 |
| 7 | $17784 | 45.1% | 45.1% | 4786 | 4997 | 24.60 | 38 | 11 | 40 |
| 8 | $17784 | 43.0% | 43.0% | 4867 | 5621 | 23.60 | 39 | 10 | 42 |
| 9 | $17784 | 45.0% | 45.0% | 4782 | 5033 | 24.55 | 38 | 10 | 40 |
| 10 | $17784 | 45.3% | 45.3% | 4810 | 4954 | 24.71 | 39 | 10 | 41 |
| 11 | $17784 | 44.8% | 44.8% | 4831 | 5033 | 24.48 | 38 | 10 | 39 |
| 12 | $17784 | 46.0% | 46.0% | 4833 | 4806 | 25.05 | 40 | 9 | 41 |
| 13 | $17784 | 44.3% | 44.3% | 4760 | 5253 | 24.22 | 38 | 10 | 41 |
| 14 | $17784 | 45.0% | 45.0% | 4890 | 5014 | 24.56 | 37 | 10 | 39 |
| 15 | $17784 | 45.0% | 45.0% | 4752 | 5035 | 24.59 | 39 | 10 | 41 |
| 16 | $17784 | 44.0% | 44.0% | 4793 | 5323 | 24.06 | 38 | 9 | 41 |
| 17 | $17784 | 43.7% | 43.7% | 4833 | 5373 | 23.92 | 39 | 9 | 41 |
| 18 | $17784 | 44.0% | 44.0% | 4742 | 5325 | 24.08 | 38 | 10 | 40 |
| 19 | $17784 | 43.6% | 43.6% | 4759 | 5458 | 23.88 | 39 | 10 | 41 |
| 20 | $17784 | 43.9% | 43.9% | 4811 | 5377 | 24.01 | 39 | 10 | 41 |

### Rule-based AI

| Seed | Net profit | Service level | Fill rate | Backlog | Cancelled | Composite | First stock-bound tick | First cap-bound tick | Cap-bound ticks |
|---|---|---|---|---|---|---|---|---|---|
| 1 | $35859 | 100.0% | 100.0% | 916 | 0 | 56.80 | 36 | 9 | 25 |
| 2 | $61452 | 100.0% | 100.0% | 713 | 0 | 63.54 | 37 | 9 | 30 |
| 3 | $34921 | 100.0% | 100.0% | 1001 | 0 | 56.56 | 37 | 9 | 24 |
| 4 | $30212 | 100.0% | 100.0% | 845 | 0 | 55.32 | 36 | 10 | 22 |
| 5 | $50136 | 100.0% | 100.0% | 980 | 0 | 60.56 | 37 | 10 | 28 |
| 6 | $59001 | 100.0% | 100.0% | 723 | 0 | 62.90 | 36 | 10 | 28 |
| 7 | $55796 | 100.0% | 100.0% | 971 | 0 | 62.05 | 37 | 11 | 25 |
| 8 | $70714 | 100.0% | 100.0% | 544 | 0 | 65.98 | 38 | 10 | 29 |
| 9 | $51653 | 100.0% | 100.0% | 774 | 0 | 60.96 | 36 | 10 | 27 |
| 10 | $55001 | 100.0% | 100.0% | 848 | 0 | 61.84 | 37 | 10 | 26 |
| 11 | $33887 | 100.0% | 100.0% | 805 | 0 | 56.28 | 37 | 10 | 22 |
| 12 | $45260 | 100.0% | 100.0% | 966 | 0 | 59.28 | 38 | 9 | 28 |
| 13 | $61508 | 100.0% | 100.0% | 647 | 0 | 63.56 | 36 | 10 | 27 |
| 14 | $59103 | 100.0% | 100.0% | 850 | 0 | 62.92 | 36 | 10 | 28 |
| 15 | $56132 | 100.0% | 100.0% | 619 | 0 | 62.14 | 36 | 10 | 28 |
| 16 | $34537 | 100.0% | 100.0% | 797 | 0 | 56.46 | 36 | 9 | 25 |
| 17 | $36986 | 100.0% | 100.0% | 951 | 0 | 57.10 | 37 | 9 | 25 |
| 18 | $62182 | 100.0% | 100.0% | 600 | 0 | 63.73 | 37 | 10 | 28 |
| 19 | $59968 | 100.0% | 100.0% | 719 | 0 | 63.15 | 36 | 10 | 27 |
| 20 | $53562 | 100.0% | 100.0% | 952 | 0 | 61.46 | 37 | 10 | 29 |

### Recorded demo human plan

| Seed | Net profit | Service level | Fill rate | Backlog | Cancelled | Composite | First stock-bound tick | First cap-bound tick | Cap-bound ticks |
|---|---|---|---|---|---|---|---|---|---|
| 1 | $21612 | 46.0% | 46.0% | 4773 | 5175 | 26.06 | 38 | 9 | 39 |
| 2 | $21612 | 47.0% | 47.0% | 4836 | 4873 | 26.60 | 38 | 9 | 39 |
| 3 | $21612 | 46.6% | 46.6% | 4819 | 4941 | 26.40 | 39 | 9 | 39 |
| 4 | $21612 | 47.7% | 47.7% | 4753 | 4688 | 26.92 | 38 | 10 | 40 |
| 5 | $21612 | 47.7% | 47.7% | 4793 | 4706 | 26.90 | 39 | 10 | 41 |
| 6 | $21612 | 47.5% | 47.5% | 4763 | 4775 | 26.82 | 38 | 10 | 38 |
| 7 | $21612 | 47.8% | 47.8% | 4786 | 4658 | 26.98 | 38 | 11 | 39 |
| 8 | $21612 | 45.7% | 45.7% | 4867 | 5282 | 25.90 | 39 | 10 | 41 |
| 9 | $21612 | 47.7% | 47.7% | 4782 | 4694 | 26.92 | 38 | 10 | 39 |
| 10 | $21612 | 48.0% | 48.0% | 4810 | 4615 | 27.09 | 39 | 10 | 40 |
| 11 | $21612 | 47.5% | 47.5% | 4831 | 4694 | 26.85 | 38 | 10 | 40 |
| 12 | $21612 | 48.7% | 48.7% | 4833 | 4467 | 27.44 | 40 | 9 | 40 |
| 13 | $21612 | 47.0% | 47.0% | 4760 | 4914 | 26.57 | 38 | 10 | 39 |
| 14 | $21612 | 47.7% | 47.7% | 4890 | 4675 | 26.93 | 37 | 10 | 38 |
| 15 | $21612 | 47.8% | 47.8% | 4752 | 4696 | 26.96 | 39 | 10 | 38 |
| 16 | $21612 | 46.6% | 46.6% | 4793 | 4984 | 26.40 | 38 | 9 | 40 |
| 17 | $21612 | 46.3% | 46.3% | 4833 | 5034 | 26.25 | 39 | 9 | 39 |
| 18 | $21612 | 46.7% | 46.7% | 4742 | 4986 | 26.41 | 38 | 10 | 39 |
| 19 | $21612 | 46.3% | 46.3% | 4759 | 5119 | 26.20 | 39 | 10 | 39 |
| 20 | $21612 | 46.5% | 46.5% | 4811 | 5038 | 26.34 | 39 | 10 | 39 |

## 9. Crisis_Tick, Demo_Fork_Tick and Demo_Commands

| Item | Previous Scenario | Calibrated Scenario |
|---|---|---|
| Crisis_Tick (lowest Tick with an active Disruption or a stock-bound order, no-command run, Seed 2026) | 19 | **38** |
| Demo_Fork_Tick (lowest scheduled Decision_Window at or after the Crisis_Tick whose backup order is received before the horizon) | 20 | **40** |
| Demo_Commands | one backup purchase order, 315 speakers | **backup purchase order of 339 speakers (SKU index 2) and `hire_temp_workers` 3** |

Derivation (printed with the plan in `web/e2e/fixtures/demo-plan.json`):

1. Crisis_Tick 38: the speaker's stock ran out with open orders at Tick 38 in the no-command run. The first scripted Disruption is at Tick 48.
2. Demo_Fork_Tick 40: the first scheduled Decision_Window (cadence 4) at or after Tick 38. A backup order placed there arrives at Tick 48, before the horizon.
3. Purchase order, 339 speakers from the backup supplier: the speaker has the earliest stock-bound report after the fork Tick (Tick 41, 153 units). Quantity 339 = 153 stock-bound units + 186 expected units (926 units demanded in 40 Ticks, scaled to the 8-Tick backup lead time, rounded up). It costs 339 x $57.00 = $19,323 against $221,852 of cash at Tick 40.
4. Staffing, `hire_temp_workers` 3: the no-command run reports capacity-bound orders after Tick 40 (peak 395 units at Tick 60), so a staffing Command is proposed. 3 = ceil(395 / (8 units per worker hour x 24 Ticks)). Temporary workers rather than overtime, because overtime scales only the regular workers and its hours are capped. The 3 workers are ready at Tick 46, before the surge onset at Tick 48.
5. Both Commands pass the Command_Validator at Tick 40 (`TestDemoPlanFlagshipHasPurchaseOrderAndStaffing`).

**Why Tick 40 is a meaningful moment (Requirement 34.9 and the selection reasoning).** The first crisis is visible (a SKU is out of stock with orders waiting, and capacity is already binding in the evening queues), the surge is 8 Ticks away, so both a backup order (8 Ticks) and a temporary hire (6 Ticks) can arrive in time, and the Demo_Path then plays from Tick 40 to the first later Disruption onset (Tick 48) and rewinds, so the choice is visible within a few seconds. At the previous fork (Tick 20) the surge was 28 Ticks away and no staffing Command could be justified. The rule for the Crisis_Tick and the Demo_Fork_Tick (Glossary) was not changed, because it produced a sensible tick on the calibrated Scenario. The fork is derived from a no-command preview run, which is correct for a scripted demonstration; a player cannot see the Disruption_Script. **No financial outcome and no winner is a calibration target:** the demo plan is a small, rule-derived plan, and its results (section 8) are what they are.

## 10. Profit_Floor and Profit_Target

| | Value | Rationale |
|---|---|---|
| Profit_Floor | $10,000 (1,000,000 cents) | The no-command net profit is $17,784 on every Seed (all revenue is the opening stock, so it does not depend on the Seed). Rounded down to a round number: doing nothing earns about 4% of the financial component. Selling the opening stock is itself profitable, so a floor below zero (the earlier -$20,000) would reward a manager who does nothing. |
| Profit_Target | $200,000 (20,000,000 cents) | A round number above the highest net profit any fixed plan reached in the harness over Seeds 1..20 ($181,905, `cheap restock from tick 24`). It is not the profit of any manager and not what the Rule_Based_Manager achieves ($50,394 mean). Below the target the financial component scales linearly. |

Both numbers come from the observed distribution of the no-command run and of the fixed plans, in a way that does not depend on any manager's result.

## 11. Assumptions of the calibrated Scenario

Value, unit, bounds and rationale of every Assumption in `backend/scenarios/black-friday.json` (`assumptionMeta`). The embedded copy served by the API (`backend/scenarios/embed.go`) is the same file.

| Path | Value | Unit | Bounds | Rationale |
|---|---|---|---|---|
| `horizonTicks` | 120 | ticks | 24 to 336 | synthetic, illustrative: five-day peak, Thursday 00:00 to Monday 23:59 |
| `decisionCadenceTicks` | 4 | ticks | 1 to 24 | synthetic, illustrative: a decision window every four hours |
| `commandBudget` | 5 | commands | 1 to 50 | synthetic, illustrative: small per-window command budget |
| `snapshotIntervalTicks` | 24 | ticks | 1 to 120 | synthetic, illustrative: one snapshot per simulated day |
| `tieToleranceHundredths` | 10 | hundredths | 0 to 10000 | synthetic, illustrative: a duel within 0.1 points is a tie |
| `scoreWeights.financialWeightBps` | 5000 | bps | 0 to 10000 | synthetic, illustrative: financial result is half the composite score |
| `scoreWeights.fulfilmentWeightBps` | 3000 | bps | 0 to 10000 | synthetic, illustrative: fill rate is 30% of the composite score |
| `scoreWeights.onTimeWeightBps` | 2000 | bps | 0 to 10000 | synthetic, illustrative: on-time service level is 20% of the composite score |
| `openingCashCents` | 10000000 cents ($100,000.00) | cents | 0 to 1000000000 | synthetic, illustrative; uncalibrated: $100,000 of opening cash |
| `profitFloorCents` | 1000000 cents ($10,000.00) | cents | -100000000 to 100000000 | synthetic, illustrative; calibrated by observation (docs/calibration.md): $10,000, the no-command profit of $17,784 rounded down to a round number, so doing nothing earns almost no financial points |
| `profitTargetCents` | 20000000 cents ($200,000.00) | cents | -100000000 to 100000000 | synthetic, illustrative; calibrated by observation (docs/calibration.md): $200,000, a round number above the best fixed-plan profit observed over Seeds 1 to 20 ($181,905), never the profit of any manager |
| `workforce.regularWorkers` | 14 | workers | 1 to 200 | synthetic, illustrative: 14 regular pickers give 112 units per hour, a little above the average normal-hour load of about 100 orders and below the evening peaks, the demand surge and the absence window |
| `workforce.unitsPerWorkerHour` | 8 | units/hour | 1 to 60 | synthetic, illustrative: eight picked-and-packed units per worker-hour |
| `workforce.maxOvertimePerWorker` | 4 | hours | 0 to 8 | synthetic, illustrative: up to four overtime hours per worker per day |
| `workforce.maxTempWorkers` | 30 | workers | 0 to 200 | synthetic, illustrative: temporary headcount cap |
| `workforce.tempOnboardingTicks` | 6 | ticks | 0 to 48 | synthetic, illustrative: temps add no capacity for six hours but are paid |
| `wages.regularPerHourCents` | 2200 cents ($22.00) | cents | 500 to 10000 | synthetic, illustrative: $22.00 per hour regular wage |
| `wages.overtimePerHourCents` | 3300 cents ($33.00) | cents | 500 to 15000 | synthetic, illustrative: 1.5 times the regular wage |
| `wages.tempPerHourCents` | 2500 cents ($25.00) | cents | 500 to 10000 | synthetic, illustrative: temp wage slightly above regular to attract short-notice labour |
| `costs.outboundShipCentsPerUnit` | 400 cents ($4.00) | cents | 0 to 5000 | synthetic, illustrative: $4.00 outbound freight per unit |
| `costs.expediteFeeCents` | 5000 cents ($50.00) | cents | 0 to 100000 | synthetic, illustrative: $50.00 flat fee to expedite a purchase order |
| `costs.expediteReductionTicks` | 4 | ticks | 0 to 24 | synthetic, illustrative: expediting shortens lead time by four hours |
| `costs.hourlyOverheadCents` | 30000 cents ($300.00) | cents | 0 to 500000 | synthetic, illustrative: $300.00 per hour rent, utilities and marketing |
| `costs.serviceCreditPerLateOrderCents` | 500 cents ($5.00) | cents | 0 to 10000 | synthetic, illustrative: $5.00 credit for each late order |
| `demand.baseArrivalsPerTick` | 100 | arrivals/tick | 1 to 2000 | synthetic, illustrative: about 100 potential customers per hour for each SKU at profile 100% |
| `demand.baseConversionBps` | 2500 | bps | 0 to 10000 | synthetic, illustrative; uncalibrated: 25% of potential customers buy at the reference price with no marketing |
| `demand.priceElasticityBps` | 1500 | bps | 0 to 20000 | synthetic, illustrative: 15% conversion change per 100% price change |
| `demand.marketingEffectBps` | 300 | bps | 0 to 10000 | synthetic, illustrative: 3% conversion uplift per $100 of hourly marketing spend on a SKU |
| `demand.cancelThresholdTicks` | 48 | ticks | 1 to 240 | synthetic, illustrative: open orders older than 48 hours are cancelled |
| `demand.noiseBps` | 2000 | bps | 0 to 10000 | synthetic, illustrative: potential arrivals vary uniformly by plus or minus 20% around their expected value; 0 gives exact arrivals |
| `demand.marketingSpendUnitCents` | 10000 cents ($100.00) | cents | 100 to 1000000 | synthetic, illustrative: $100 of hourly marketing spend on a SKU is one uplift step of demand.marketingEffectBps |
| `demand.maxMarketingSpendCentsPerHour` | 50000 cents ($500.00) | cents | 0 to 10000000 | synthetic, illustrative: at most $500 of marketing spend per hour on one SKU |
| `pricing.minPriceBps` | 5000 | bps | 100 to 10000 | synthetic, illustrative: a price may not go below 50% of the reference price |
| `pricing.maxPriceBps` | 20000 | bps | 10000 to 100000 | synthetic, illustrative: a price may not go above 200% of the reference price |
| `demand.hourlyProfile` | 10000 | bps | 0 to 50000 | synthetic, illustrative: basis points of base per hour of day, averaging 10000, evening peak |
| `skus.headphones.referencePriceCents` | 5999 cents ($59.99) | cents | 100 to 100000 | synthetic, illustrative: list price in a plausible consumer-electronics accessory range |
| `skus.headphones.openingUnits` | 1400 | units | 0 to 100000 | synthetic, illustrative: about 56 hours of average normal-profile demand, enough to reach the Black Friday surge but not to serve it |
| `skus.headphones.openingUnitCostCents` | 3000 cents ($30.00) | cents | 1 to 100000 | synthetic, illustrative: equals the primary supplier unit cost |
| `skus.headphones.promiseTicks` | 24 | ticks | 1 to 240 | synthetic, illustrative: delivery promise window |
| `suppliers.headphones.primary.unitCostCents` | 3000 cents ($30.00) | cents | 1 to 100000 | synthetic, illustrative: cheaper supplier with a long lead time |
| `suppliers.headphones.primary.leadTimeTicks` | 24 | ticks | 1 to 240 | synthetic, illustrative: cheaper supplier with a long lead time |
| `suppliers.headphones.backup.unitCostCents` | 3600 cents ($36.00) | cents | 1 to 100000 | synthetic, illustrative: pricier supplier with a short lead time |
| `suppliers.headphones.backup.leadTimeTicks` | 8 | ticks | 1 to 240 | synthetic, illustrative: pricier supplier with a short lead time |
| `skus.charger.referencePriceCents` | 1999 cents ($19.99) | cents | 100 to 100000 | synthetic, illustrative: list price in a plausible consumer-electronics accessory range |
| `skus.charger.openingUnits` | 1800 | units | 0 to 100000 | synthetic, illustrative: about 56 hours of average normal-profile demand, enough to reach the Black Friday surge but not to serve it |
| `skus.charger.openingUnitCostCents` | 800 cents ($8.00) | cents | 1 to 100000 | synthetic, illustrative: equals the primary supplier unit cost |
| `skus.charger.promiseTicks` | 24 | ticks | 1 to 240 | synthetic, illustrative: delivery promise window |
| `suppliers.charger.primary.unitCostCents` | 800 cents ($8.00) | cents | 1 to 100000 | synthetic, illustrative: cheaper supplier with a long lead time |
| `suppliers.charger.primary.leadTimeTicks` | 24 | ticks | 1 to 240 | synthetic, illustrative: cheaper supplier with a long lead time |
| `suppliers.charger.backup.unitCostCents` | 1050 cents ($10.50) | cents | 1 to 100000 | synthetic, illustrative: pricier supplier with a short lead time |
| `suppliers.charger.backup.leadTimeTicks` | 8 | ticks | 1 to 240 | synthetic, illustrative: pricier supplier with a short lead time |
| `skus.speaker.referencePriceCents` | 8999 cents ($89.99) | cents | 100 to 100000 | synthetic, illustrative: list price in a plausible consumer-electronics accessory range |
| `skus.speaker.openingUnits` | 800 | units | 0 to 100000 | synthetic, illustrative: about 56 hours of average normal-profile demand, enough to reach the Black Friday surge but not to serve it |
| `skus.speaker.openingUnitCostCents` | 4800 cents ($48.00) | cents | 1 to 100000 | synthetic, illustrative: equals the primary supplier unit cost |
| `skus.speaker.promiseTicks` | 48 | ticks | 1 to 240 | synthetic, illustrative: delivery promise window |
| `suppliers.speaker.primary.unitCostCents` | 4800 cents ($48.00) | cents | 1 to 100000 | synthetic, illustrative: cheaper supplier with a long lead time |
| `suppliers.speaker.primary.leadTimeTicks` | 24 | ticks | 1 to 240 | synthetic, illustrative: cheaper supplier with a long lead time |
| `suppliers.speaker.backup.unitCostCents` | 5700 cents ($57.00) | cents | 1 to 100000 | synthetic, illustrative: pricier supplier with a short lead time |
| `suppliers.speaker.backup.leadTimeTicks` | 8 | ticks | 1 to 240 | synthetic, illustrative: pricier supplier with a short lead time |
| `skus.watchband.referencePriceCents` | 1499 cents ($14.99) | cents | 100 to 100000 | synthetic, illustrative: list price in a plausible consumer-electronics accessory range |
| `skus.watchband.openingUnits` | 1600 | units | 0 to 100000 | synthetic, illustrative: about 56 hours of average normal-profile demand, enough to reach the Black Friday surge but not to serve it |
| `skus.watchband.openingUnitCostCents` | 500 cents ($5.00) | cents | 1 to 100000 | synthetic, illustrative: equals the primary supplier unit cost |
| `skus.watchband.promiseTicks` | 24 | ticks | 1 to 240 | synthetic, illustrative: delivery promise window |
| `suppliers.watchband.primary.unitCostCents` | 500 cents ($5.00) | cents | 1 to 100000 | synthetic, illustrative: cheaper supplier with a long lead time |
| `suppliers.watchband.primary.leadTimeTicks` | 24 | ticks | 1 to 240 | synthetic, illustrative: cheaper supplier with a long lead time |
| `suppliers.watchband.backup.unitCostCents` | 700 cents ($7.00) | cents | 1 to 100000 | synthetic, illustrative: pricier supplier with a short lead time |
| `suppliers.watchband.backup.leadTimeTicks` | 8 | ticks | 1 to 240 | synthetic, illustrative: pricier supplier with a short lead time |

Disruption_Script (onsets are after Tick 24, Requirement 3.3; unchanged by calibration):

| Kind | Onset Tick | Duration (Ticks) | Magnitude (bps) or target |
|---|---|---|---|
| demand_surge | 48 | 24 | 25000 |
| workforce_absence | 54 | 12 | 3000 |
| supplier_outage | 60 | 18 | primary |

## 12. Known limitations

- **Synthetic and uncalibrated to any real business.** Every number is an illustration. Nothing here is a forecast.
- **Low variance across Seeds.** When demand exceeds supply for most of the horizon, the Seed changes little: the no-command net profit is identical on all 20 Seeds, and fixed plans vary by about 2% to 10% around their mean profit. A 20-Seed ensemble therefore shows small intervals.
- **Cash does not bind.** Opening cash is $100,000 but revenue is received at shipment, so cash at Tick 40 is $221,852 and no plan in this report had a Command rejected for cash (0 skipped Commands). An order of 1,500 units of every SKU at Tick 40 ($166,000) is still affordable. The trade-offs are in timing and quantity, not in a cash limit.
- **No release of temporary workers.** Temporary workers are paid to the horizon; only overtime can be switched off. This makes the hire time and count a real cost trade-off, as intended by the Command set, but it also hurts any manager that over-hires.
- **Staffing alone cannot beat restocking alone.** The levers are complements here. If a lever that works on its own is wanted, stock would have to be abundant, which removes the first crisis.
- **Capacity is tight on normal days.** With 14 regular workers the Fulfilment_Constraint_Report shows capacity-bound orders from Tick 9 or 10 on the first morning ramp and in each evening peak of the no-command run, with small queues that clear overnight.
- **The Rule_Based_Manager overspends on labor and underestimates unit demand.** Its demand estimate shares the base arrivals equally between SKUs (about 7 orders per SKU per hour) while the engine draws base arrivals per SKU (25 orders per SKU per hour); its backlog term corrects this. It never reduces overtime. Neither point was changed (tuning the AI is out of scope for calibration); both are visible in section 8.
- **Presentation of weekdays.** The Scenario's clock starts Thursday 00:00, so the scripted surge at Tick 48 falls on Saturday 00:00 in the clock. Requirement 3.3 only requires onsets after Tick 24; the onsets were not moved.
- **Benchmarks.** `docs/benchmarks.md` carries a note: a single flagship replay measured about 103 ms after calibration (about 10% slower than before).

## 13. Rerun checklist

1. `cd backend && go run ./cmd/parallel-sim calibrate --scenario scenarios/black-friday.json --seeds 1..20`.
2. After any Scenario or engine change: `UPDATE_GOLDEN=1 go test ./internal/app/demoplan -run TestDemoForkInputsFile`, `UPDATE_DEMOPLAN=1 go test ./internal/app/demoplan -run TestDemoPlanMatchesFixture`, `UPDATE_GOLDEN=1 go test ./internal/sim -run TestGoldenFlagshipHash`, `UPDATE_GOLDEN=1 go test ./internal/app -run TestGoldenDuelHashes`, update the README hash line, then `go test ./...` and `cd web && npm run e2e`. Record the previous and new values here with the reason (Requirement 34.9).
3. Never add a gate that compares a Human Manager with an AI Manager.

Synthetic simulation output under the stated assumptions. Not a forecast or guarantee of real-world results.
