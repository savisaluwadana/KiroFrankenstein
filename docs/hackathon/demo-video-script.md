# Demo video script (target 2:55, hard limit 3:00)

Derived from `docs/demo-script.md`. Every number below is copied from that file (seed 2026, engine version 2, no LLM configured). If the scenario, an assumption or the engine version changes, re-run `scripts/demo-numbers.sh` and update both files. If you play differently from the script, read the numbers off the screen instead.

On-screen disclaimer, always visible in the footer: "Synthetic simulation output under the stated assumptions. Not a forecast or guarantee of real-world results."

## Timeline

| Time | Screen | Action | Narration | Numbers shown |
|---|---|---|---|---|
| 0:00 to 0:08 | Landing page | Click "Begin your shift" | "Reality has no undo button. We built one. This is a simulated warehouse running a Black Friday week, hour by hour." | Starting cash $100,000.00 |
| 0:08 to 0:20 | Briefing, then studio | "Start Black Friday demo"; set "Pause automatically at" to "surprises only"; speed "Fast"; Play | "Everything on screen is computed by a deterministic engine. Nothing is scripted." | Pauses at Saturday, 12:00 AM (tick 48) |
| 0:20 to 0:30 | Alert cards | Point at the three cards | "Demand is surging, Speakers are out of stock, and the packing line can't keep up." | Cards: "Orders are surging across the store", "Waiting for inventory: Speakers", "Waiting for packing capacity". Profit so far +$50,757.48; orders waiting 527; on time 100.0% |
| 0:30 to 0:40 | Time strip | Drag back to Friday, 4:00 PM (tick 40); click "Explore another future" | "Rewinding is just a read. Changing the past is a fork: everything before this hour stays exactly as it happened." | Fork sequence: "Same history up to Friday, 4:00 PM" |
| 0:40 to 0:55 | Fork studio | Order 339 Speakers from the backup supplier; hire 3 temporary workers; "Lock in decisions"; Continue; Play | "I'll order from the backup supplier and add three temporary workers." | "take effect at Friday, 5:00 PM" |
| 0:55 to 1:10 | Compare futures | "Compare futures", then "What changed?" | "Same history, different decisions. Here is what the engine observed. It does not say this decision caused that result." | At Saturday, 12:00 AM: original profit $50,757.48, orders waiting 527; fork profit $50,525.00, orders waiting 479; on time 100.0% in both; first visible difference Friday, 5:00 PM |
| 1:10 to 1:35 | Briefing → duel | "Challenge the AI manager"; at Friday, 4:00 PM place the same two moves; Play to the end of the week | "Now a duel against the rule-based AI. Same customers, same surprises, same rules for both of us. No language model is involved." | AI moves listed with "AI rationale (not verified)" |
| 1:35 to 1:55 | Scoreboard | Show the scoreboard | "In this one run my minimal plan scored 26.53 and the rule-based AI scored 56.83. That is one run under synthetic assumptions. It does not say AI is better in general." | You 26.53 vs AI 56.83 points of 100; profit $21,611.61 vs $35,957.50; on time 47.0% vs 100.0%; orders waiting 4,788 vs 798; cancelled 4,921 vs 0 |
| 1:55 to 2:10 | Ledger drill-down | Click "Profit so far" on your business | "Every figure is a sum of ledger entries." | $21,611.61 from 6,179 entries |
| 2:10 to 2:25 | Terminal (in `backend/`) | Run the `verify` command below | "Same seed, same recorded inputs, same state hash. Replay is exact." | `state_hash: 9786f539...daca8`, `net_profit_cents: 2161161`, `service_level_bps: 4695`, then `MATCH` |
| 2:25 to 2:38 | Editor: `.kiro/` | Show `.kiro/steering/`, `.kiro/specs/parallel-simulation-mvp/`, `docs/kiro-build-log.md` | "We built this spec-first with Kiro: steering files, then requirements, design and tasks, then task execution. The log says what Kiro did and what it did not." | 43 requirements, 41 design properties, tasks.md checkboxes 130 of 155 checked (`kiro-evidence.md`) |
| 2:38 to 2:55 | Studio footer | Hold on the disclaimer | "These numbers are synthetic, computed under assumptions you can inspect and change. They are not a forecast. Parallel: reality has no undo button." | Footer disclaimer visible |

The optional full-week comparison in `docs/demo-script.md` (1:10 to 1:25 there) is cut for time.

Terminal command for 2:10 (from `backend/`):

```bash
go run ./cmd/parallel-sim verify --scenario scenarios/black-friday.json --seed 2026 \
  --inputs scenarios/demo-fork-inputs.json --expect $(cat scenarios/golden/demo-fork.hash)
```

Alternate technical shot (not in the timeline): the 20-seed ensemble `cmp` showing `IDENTICAL`, from `docs/demo-script.md` "Technical moments". Only add it if there is time. If you do, quote the numbers from there and keep the sentence about the fixed-plan asymmetry.

## Shot list

1. Landing page (desktop-demo-path-01-landing).
2. Studio with alert cards (crisis).
3. Fork sequence animation (shared history, then branching lines).
4. Fork studio with the two moves.
5. Compare view and "What changed?" panel.
6. Duel view with both warehouses, then scoreboard. If you show a still, use `desktop-demo-path-09-results.png` (26.53 vs 56.83, the numbers in the timeline above). `desktop-16-duel-scoreboard.png` is a different run (24.51 vs 56.83, human ordered only 60 Chargers); see `screenshots.md`.
7. Ledger drill-down panel.
8. Terminal showing `MATCH`.
9. Editor showing `.kiro/` and the build log (10 to 15 s).
10. Footer disclaimer.

## What to show for Kiro (10 to 15 s)

Open the file tree at `.kiro/steering/` (7 files) and `.kiro/specs/parallel-simulation-mvp/` (`requirements.md`, `design.md`, `tasks.md`). Scroll `tasks.md` so checked boxes and a "Reconciliation" note are visible. Open `docs/kiro-build-log.md` at the provenance line. Do **not** show or mention hooks or MCP (there are none).

## Recording tips

- Resolution 1920×1080 (the screenshots were captured at 1440 px wide; check legibility of the small footer text). Dark theme as designed. Close other windows and notifications.
- Launch, from `docs/demo-script.md` "Before you record" (three terminals):
  1. `cd backend && WEB_ORIGIN=http://localhost:3100 go run ./cmd/parallel-api` (in-memory store, no LLM variables set)
  2. `cd web && npm run dev` (or `npm run build && npx next start -p 3100` for a smoother recording)
  3. `cd backend` for the technical moments.
- Dry run without a browser: `scripts/demo-numbers.sh` prints the numbers quoted above. Note it needs a **fresh** API.
- Reset: stop the API with Ctrl-C and restart it; in-memory worlds are gone. Reload the tab. If the API restarts mid-recording, go to `http://localhost:3100/briefing` and start again; the demo is deterministic.
- Timing: the automated crisis → fork → compare segment measured 28.8 to 29.9 s in the recorded e2e runs (requirement: under 60 s), including a 2 s wait before each action. By hand, practise the click path; the narration above fits roughly 55 s for steps up to the compare view.
- Keep the video under 3 minutes and public (a secondary summary of the 2025 rules says judges aren't required to watch past three minutes; confirm the 2026 wording yourself). Upload to YouTube, Vimeo or similar and set it to public or unlisted-accessible; confirm in a private window.

## Claims we can make

- The warehouse state on screen is computed by a deterministic engine from a recorded input log.
- Rewind is a read; a fork keeps all history before the fork hour; branches are independent (property tests exist).
- The same seed and inputs reproduce the same state hash. `parallel-sim verify` prints `MATCH` for the committed hash.
- The golden hashes matched on arm64, under linux/amd64 emulation, and in the passing GitHub Actions run 46100197654 (jobs `go (amd64)` and `go (arm64)`).
- Every money figure opens ledger entries that add up to it.
- In the recorded duel run, the rule-based AI scored 56.83 and the minimal human plan 26.53, with the figures above.
- The rule-based AI and the whole engine need no LLM or network.
- The 20-seed paired ensemble report is reproduced byte for byte (numbers in `docs/demo-script.md`).
- The project was built spec-first with Kiro (steering, spec, delegated task execution), and the build log notes which parts were scaffolding outside Kiro.

## Claims we must NOT make

- That AI managers beat humans in general. The result is one run, and the human plan was deliberately minimal. In the ensemble, one arm is a fixed plan that cannot adapt.
- That the LLM opponent works against a real provider. Live verification is pending (stub providers only).
- That Parallel is deployed or publicly available. It runs locally. Do not show a "live demo" URL unless the user deploys it.
- That results predict real-world outcomes or that the model matches any real business. Calibration is synthetic, not against real data.
- That the winner or any profit value was scripted or tuned for the AI or the human (we may say calibration is documented; do not claim neutrality beyond that).
- That the fork transition meets its 600 ms target (it runs 1.5 s) or that a duel reveal animation exists (it does not).
- That hooks, MCP or any Kiro feature beyond steering, specs and delegated task execution was used.
- That Kiro generated the first four commits (2026-11-10 scaffolding) or that we know how many Kiro credits were used.
- That a fork "caused" a difference. The product says "observed differences".
- Any benchmark number other than as "on one Apple M5 laptop" (`docs/benchmarks.md`).
