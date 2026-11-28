# Screenshot selection

All files are in `docs/screenshots/` (checked to exist). Images were produced by the Playwright specs, not by hand. Every file in the table below was opened and checked by the author of this file.

**Numbers caveat.** The screenshots come from capture runs of several e2e specs, which are not all the recorded demo plan of `docs/demo-script.md`. Each duel scoreboard image was opened and its numbers read; they come from three different runs and must never be mixed:

| Image | Spec that captured it | What the human did | You vs Rule-based AI points | Profit so far (you) |
|---|---|---|---|---|
| `desktop-demo-path-09-results.png` | `demo-path.spec.ts` (desktop) | The two demo moves at Friday, 4:00 PM (339 Speakers, 3 temporary workers), then nothing | 26.53 vs 56.83 | $21,611.61 |
| `desktop-16-duel-scoreboard.png`, `mobile-16-duel-scoreboard.png` | `duel.spec.ts` | One order of 60 Chargers from the backup supplier at the first hour, then nothing | 24.51 vs 56.83 | $18,113.40 |
| `mobile-demo-path-09-results.png` | `demo-path.spec.ts` (phone) | No moves at all | 24.18 vs 56.83 | $17,784.00 |

Only the 26.53 run is the one in `docs/demo-script.md` (and `TestDemoScriptMatchesPlan` re-computes it). The rule-based AI side is the same in all three (56.83, $35,957.50). `desktop-18-diff.png` compares a different fork hour (Friday, 4:00 PM, read at Saturday, 12:00 PM) from the script's Saturday 12:00 AM figures. Do not mix numbers from a screenshot into narration or the Devpost text without looking at that image. All screenshots show the synthetic-results footer (the landing image shows "Synthetic simulation. Not a forecast.").

## Devpost gallery (8, in recommended order)

| # | File | Viewed | What it shows | Caption |
|---|---|---|---|---|
| 1 | `desktop-demo-path-01-landing.png` | yes | Landing page with the tagline and "Begin your shift" | Reality has no undo button. We built one. |
| 2 | `desktop-05-studio-crisis.png` | yes | Studio at Thursday 12:00 PM: isometric warehouse, "Waiting for packing capacity" card with "Hire temporary staff" and "Schedule overtime", HUD (cash, profit so far, on time, orders waiting) | The crisis shows up in the warehouse. Every figure comes from the engine's state for this hour. |
| 3 | `desktop-07-fork-sequence.png` | yes | Fork transition: a "Shared history" line splitting into the original path and "Alternative future 1 (from Thursday 4:00 AM)" | Rewind to any hour and fork. History before that hour is unchanged. |
| 4 | `desktop-08-comparison.png` | yes | Two warehouses side by side, "Observed differences" table, note that a different outcome is not proof of cause | Two futures from the same hour, compared without claiming cause. |
| 5 | `desktop-18-diff.png` | yes | "What changed between these two futures": first different hour, the different decisions (339 Speakers from the backup supplier, 3 temporary workers), financial, service, backlog and stock differences | The diff lists what was observed and which decisions differ. |
| 6 | `desktop-demo-path-09-results.png` | yes | You versus the rule-based AI after the demo plan: both warehouses, scoreboard 26.53 vs 56.83 points, score breakdown (financial 50%, orders fully shipped 30%, on time 20%), observed differences | A duel under identical customers and surprises: 26.53 vs 56.83 points in this one run. The screen says it doesn't show which approach is better in general. |
| 7 | `desktop-12-ledger-drilldown.png` | yes | "Where this number comes from": ledger entries that add up to "Profit so far" | Click a profit figure and see the ledger entries behind it. |
| 8 | `desktop-10-engineering-mode.png` | yes | Engineering Mode: tick, state hash, engine version and the metrics JSON | Engineering Mode exposes the state hash that replay must reproduce. |

Optional replacement for #8: `desktop-19-assumptions.png` (viewed): "How this is modelled" panel with each assumption's value and range, and a control to reveal only surprises that already happened. Use it if you want to show that assumptions are adjustable and the future stays hidden.

## README (suggested, not applied)

The README is being rewritten by another worker, so this file does not edit it. Suggested hero set, in order: `desktop-demo-path-01-landing.png`, `desktop-05-studio-crisis.png`, `desktop-08-comparison.png`, `desktop-demo-path-09-results.png`, `desktop-12-ledger-drilldown.png`. Reference them with relative paths, for example `docs/screenshots/desktop-08-comparison.png`.

## Mobile (not viewed)

`mobile-*.png` files exist (for example `mobile-05-studio-crisis.png`, `mobile-16-duel-scoreboard.png`). The build log notes defects on the phone layout: the bottom sheet hides most of the warehouse and a label is clipped. We did not select any for Devpost.

## Known issues with specific files

- `desktop-demo-path-*` and `desktop-0x-*` files overlap; use one family consistently.
- The build log records that the first demo-path screenshot used to catch the intro fade (nearly black). Commit `2ce9c05` (message: "wait for intro animation before screenshots; ... refresh screenshots") targeted that, and the landing file viewed here is not black.
- `scripts/check-image-secrets.sh` exists for container images only; screenshots contain a world ID and branch ID in Engineering Mode (`desktop-10`). These are demo IDs from a local run and the images show no tokens, but check before publishing if you ever deploy the same world.
