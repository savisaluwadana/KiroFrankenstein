# Why Parallel fits the Frankenstein category

## Category text and its source

`.kiro/steering/hackathon.md` records the intent: "Frankenstein theme combines seemingly incompatible technologies into one unexpectedly useful application; integrations should be essential, not cosmetic."

**Notes on the official rules.** `https://kiroween.devpost.com/rules` and `https://kiroween.devpost.com/` returned HTTP 403 to our fetch tool, so we could not read the 2026 rules text directly. A web search returned this category wording from the previous year's page (`kiroween-2025.devpost.com`, a different year): "Stitch together a chimera of technologies into one app. Bring together seemingly incompatible elements to build something unexpectedly powerful." The 2026 wording may differ. **The user must re-read the live 2026 rules page and the category description before submitting.**

## The chimera

| Stitched-together idea | Why it looks incompatible with the others | Concrete mechanism in the repo |
|---|---|---|
| Discrete-event simulation | Needs mutable, evolving state | `backend/internal/sim` (`engine.go` `StepTick`, `eventqueue.go`) on an integer-minute clock |
| Event sourcing | Wants immutable history, not mutable state | Append-only `input_log` (`backend/migrations/00001_init.sql`: UPDATE/DELETE trigger raises), fork in `backend/internal/app/fork.go`, snapshots in `app/snapshots.go` |
| Double-entry accounting | Finance wants audited integers, games want floating, fluid numbers | `sim/ledger.go`, `valuation.go`, `metrics.go`; money in `int64` cents; API ledger summary `httpapi/ledger_summary.go`; UI `web/features/ledger` |
| Game-engine rendering (PixiJS) | Real-time animation vs. a deterministic hourly model | `web/features/world-view/scene/*`: draws only server frames; `interpolate.ts` moves decorative positions between ticks with a test |
| Operations research | Reorder-point and capacity rules vs. an AI "manager" | `backend/internal/policy/rulebased` (pure) |
| Agentic AI (optional LLM) | Non-deterministic text generator vs. exact replay | `backend/internal/policy/llm` adapter; proposals pass `sim.Validate` and are recorded as inputs, so replay never calls the LLM |
| Chaos engineering | Fault injection is a DevOps practice, not a game mechanic | `sim/disruptions.go`, `app/disruptions.go`; scheduled and injected supplier outages, demand surges, staff absences |

## Why each integration is essential

1. **Event sourcing gives rewind and fork.** The state at any hour is recomputed from the recorded inputs. A child branch reads the parent's inputs only up to the fork hour (`app/fork.go`; property tests `fork_fidelity_prop_test.go`, `fork_independence_prop_test.go`, `fork_prefix_prop_test.go`). Without it there is no "undo".
2. **The discrete-event engine makes the world executable.** Orders, purchase orders, packing capacity and wages are events with consequences, so a decision compounds over time. Without it Parallel would be a slideshow of precomputed numbers.
3. **The ledger makes every number calculated.** A money figure in the UI opens the entries that sum to it (`web/features/ledger`, `GET /branches/{id}/ledger/summary`). The balance and valuation invariants are property tests (`ledger_balance_prop_test.go`, `valuation_prop_test.go`, `cash_prop_test.go`).
4. **Integer determinism makes event sourcing work.** Replay only reproduces results if arithmetic is exact. The engine is integer only, with a project-owned PRNG (`sim/rng.go`) and a canonical SHA-256 state hash (`sim/hash.go`, `encode.go`). `internal/archtest` fails if `sim` imports anything outside a standard-library allowlist.
5. **PixiJS renders authoritative frames.** The diorama draws what the server computed for the displayed hour (stock stacks, workers, waiting orders, disruption highlights). It exists so a person can see compounding effects. It never computes a result (`structure.md`: "It never computes simulation results").
6. **The AI proposes and deterministic code executes.** `policy.Manager.Propose` returns commands only (`backend/internal/policy/policy.go`). The same `sim.Validate` gate used for humans runs in `app/branch_service.go` (`SubmitCommands`). That is what lets a duel be fair: same observation fields, same budget, same decision windows (property test `duel_parity_prop_test.go`).
7. **Chaos disruptions create the crisis.** Without scheduled outages, surges and absences there is nothing to rewind from. The disruption script is hidden by default and revealed only through an explicit control or after the horizon.

## Is any piece cosmetic?

Test: remove each piece and ask whether the product still works.

- Remove PixiJS: the numbers and API still work, but the product loses its "see the crisis" purpose. This is the part closest to presentation, and we do not claim otherwise. It is still tied to state: each stack and label is derived from a frame (`deriveSceneEvents.ts`, `sceneModel.ts`).
- Remove the LLM: nothing breaks. It is optional by design and verified only with stubs. We present it as an extension of the manager interface, not as a load-bearing part (`TestNoLLMConfiguration`).
- Remove any of the other five: rewind/fork, execution, auditable money, fair comparison or crisis generation disappears.

## Honest limits of the claim

- The "seemingly incompatible" framing is ours. Judges decide whether it fits.
- Live LLM verification is pending and nothing is deployed, so the "working demo" is a local run with documented commands (`docs/demo-script.md`).
- Results are synthetic. We make no claim of real-world accuracy.
