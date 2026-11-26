---
inclusion: always
---

# Simulation invariants

These invariants are non-negotiable. Each one must be covered by an automated test (property-based where noted). A change that breaks one is a bug, even if the UI looks right.

## Model of record

- The **input log** is the source of truth: world seed, scenario with assumptions, commands, and injected disruptions, each tagged with the branch and the effective tick.
- Everything else (state, outcomes, ledger, KPIs, snapshots, hashes) is **derived** by the deterministic engine and can be rebuilt from the input log.
- Snapshots and projections are caches. A snapshot is trusted only if its stored state hash equals the hash of the replayed state for that tick. A snapshot holds the complete state: pending event queue entries with the insertion sequence counter, every identifier counter and the full internal state of every random stream.
- A forked child reads the parent's inputs only up to the fork tick; parent inputs and advancement after the fork are never visible to the child.

## Invariants

| ID | Invariant | Test type |
|---|---|---|
| INV-1 Determinism | Same scenario, seed and input log produce the same state hash at every tick, across runs, processes and amd64/arm64 | Property + golden hash |
| INV-2 Replay equivalence | Replaying from tick 0 and resuming from any valid snapshot produce identical hashes | Property |
| INV-3 Immutability | Recorded inputs are never updated or deleted. Rewind is a read; changing the past is a fork | Property + DB constraint |
| INV-4 Fork independence | Appending inputs to a child never changes any parent hash, and the reverse; siblings never affect each other | Property |
| INV-5 Fork fidelity | A child forked at tick N has the parent's tick-N state hash | Property |
| INV-6 Ledger balance | After every tick, total debits equal total credits | Property |
| INV-7 Cash reconciliation | Cash balance equals opening cash plus the sum of all cash ledger entries | Property |
| INV-8 Inventory conservation | Per SKU: opening + received − shipped − written off = on hand; on hand ≥ 0 | Property |
| INV-9 Order conservation | Orders created = open + shipped + cancelled | Property |
| INV-10 Calculated figures | Every money figure shown by the API is a sum of ledger entries or a documented formula over them | Example + contract test |
| INV-11 Parity | In a duel, both managers receive decision windows at the same ticks, the same observation fields, the same command budget and the same lead times | Property |
| INV-12 Exogenous fairness | In a duel, potential-customer arrivals (with their random draws) and disruptions are identical in both branches, regardless of decisions; decisions change only how arrivals convert to orders | Property |
| INV-13 LLM independence | The engine, replay and rule-based AI run with no network and no LLM configuration | Integration test |
| INV-14 Validation gate | No command reaches the engine without passing the deterministic validator | Property |
| INV-15 Hash scope | The state hash covers simulation state only; world/branch IDs, labels, manager kind, wall-clock timestamps, idempotency keys, rationale, model names and engine version string are excluded | Property |
| INV-16 Ensemble determinism | Identical ensemble invocations write byte-identical reports regardless of execution order; a fixed replay under its original seed reproduces the original hash with zero skipped commands; invalid replayed commands are skipped with a reason code, never altered | Property |
| INV-17 Comparison parity | Every comparison (duel, diff, ensemble pair) uses one comparison tick and identical horizon, decision-window ticks, command budget and lead times; comparisons across worlds or engine versions are rejected or flagged | Property |
| INV-18 Presentation independence | Playback speed, stepping, pausing, run-to advancement and the number of ticks per advance request never change any state hash; the engine never reads manager kind or provenance | Property |
| INV-19 Valuation and balance identity | Inventory balance equals the sum of per-SKU moving-average inventory values, a SKU with zero units has zero value, and cash plus inventory equals opening equity plus net profit at every tick | Property |

## Coding rules that protect the invariants

- No floating point in `internal/sim` state transitions. Money in cents (`int64`), rates in basis points.
- Rounding: integer division rounds half to even through one shared helper; no ad hoc rounding.
- No map iteration where order affects output. Use sorted slices.
- No `time.Now`, `math/rand`, `os`, `net`, or goroutines in `internal/sim`.
- Each random stream is keyed by name and derived from the world seed. Exogenous streams (demand arrivals, disruption severity) must not consume draws that depend on manager decisions: the number of draws per tick is fixed regardless of commands.
- Time model: hybrid discrete-event. All state changes are events on an integer sim-minute clock; observations, hashes, snapshots, metrics and decision windows exist only at hourly tick boundaries.
- Metric definitions (net revenue, variable cost, contribution, net profit, net cash flow, service level, fill rate, backlog) live in one pure calculator and match the glossary of the MVP requirements.
- Inventory valuation: per-SKU moving weighted average cost in cents; shipping the last units on hand takes the full remaining value, otherwise value × q / u rounded half to even. Revenue and payment are recognised at shipment; cancellations create no ledger entry; no post-horizon simulation.
- Results are synthetic. API responses with metrics, CLI reports and results screens carry the disclaimer.
- Commands submitted in the decision window at tick N take effect at tick N+1. Effective tick is stored with each input.
- The engine version is recorded with each world. Replays with a different engine version are flagged, not silently compared.
