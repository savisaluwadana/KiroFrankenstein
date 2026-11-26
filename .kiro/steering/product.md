---
inclusion: always
---

# Product: Parallel

**Tagline:** Reality has no undo button. We built one.

Parallel is a playable, configurable and forkable simulation of a real business. A user loads a business, runs its operations hour by hour, injects disruptions, rewinds to any past hour, forks alternative realities from that hour, makes management decisions, and competes against an AI manager facing identical external conditions.

Status: concept approved 2026-11-26. Specification in progress (`.kiro/specs/parallel-simulation-mvp/`).

## Target users

| User | Problem today | What Parallel gives them |
|---|---|---|
| E-commerce operations leads and founders | Peak-season plans live in spreadsheets that cannot answer "what if the supplier fails on Friday?" with consequences that compound over time | An executable model where each decision has calculated financial and service effects |
| Operations / supply-chain educators and students | Case studies are static; "beer game" style exercises are hard to run and impossible to rewind | A replayable, forkable exercise with an AI sparring partner |
| Teams evaluating AI decision agents | No fair, reproducible arena to compare an AI agent's judgment with a human's | Identical seeded conditions, identical constraints, calculated scores |

## Initial domain and flagship scenario

- Domain: e-commerce operations and crisis management. One business, one warehouse, a small SKU catalog.
- Flagship scenario, **Black Friday Meltdown**: a 5-day peak (Thursday 00:00 to Monday 23:59, 120 simulated hours). Demand spikes far above forecast, the primary supplier fails, and a workforce shortage hits the warehouse. A human manager and an AI manager each run their own branch from the same fork point under the same seeded external conditions. The scoreboard shows who protected profit and customers better and why, with every number traceable to ledger entries.

## The winning demo (target under 3 minutes)

Steps 1 and 2 (the crisis and a reality fork) must fit in the first 60 seconds, using accelerated playback (1x, 4x, 16x), manual stepping and automatic pausing at decision windows or disruption onsets. Playback is presentation only and never changes results.

1. Load Black Friday Meltdown. The warehouse diorama runs; orders flow, stock drains.
2. The first crisis hits. Rewind to the calibrated fork tick (the first scheduled decision window at or after the first crisis tick), fork a reality, order from the backup supplier and, where the calibration run shows a capacity bottleneck, add staff. Show the branch tree. No financial outcome or winner is scripted.
3. Start a duel: human vs AI manager from the same tick. Supplier failure and staff shortage arrive identically in both realities.
4. Split-screen comparison: profit, service level, backlog. Click a profit figure and drill into the ledger lines that produce it.
5. Replay the AI branch and show its state hash matches, proving the result is computed and reproducible.

## Non-negotiable product principles

1. AI proposes decisions. Deterministic software executes them.
2. Every world must be genuinely executable.
3. Every financial result must be calculated, never fabricated.
4. Historical events must be immutable.
5. Identical recorded inputs must produce identical outcomes.
6. Forked branches must be independent.
7. All business assumptions must be adjustable.
8. The simulation core must operate without an LLM.
9. Human and AI managers must have comparable constraints.
10. The UI must visualize authoritative simulation state.
11. Simulation results are synthetic and are never presented as real-world predictions or guarantees.

Build priority, in order: vertical slice (one configurable business, working simulation, one operational disruption, rewind and independent branching, authoritative financial metrics, basic interactive PixiJS studio with playback) → rest of the MVP core (including the minimal branch diff, CLI multi-seed ensemble and flagship calibration after the simulation and financial modules exist) → flagship polish → priority visual polish (fork transition, duel reveal) → stretch. The studio UI starts in the vertical slice so it is never last-minute. After the slice, a complete, polished flagship scenario comes before additional features.

Calibration integrity: the flagship uses transparent synthetic assumptions; it is never tuned to make the AI (or the human) win, and every calibration change is published in `docs/calibration.md`.

## Frankenstein combination (each part is load-bearing)

| Technology idea | Role in Parallel | Why it is essential |
|---|---|---|
| Discrete-event simulation | Engine that executes the business | Without it nothing is executable |
| Event sourcing | Immutable input log; replay, rewind, fork | Undo and forking come from here |
| Game engine rendering (PixiJS) | Warehouse diorama driven by real state | Makes compound effects visible and playable |
| Financial modelling | Double-entry ledger in integer cents | Results are calculated and auditable |
| Operations research | Reorder-point and capacity rules in the rule-based AI manager | A deterministic, explainable baseline AI |
| Agentic AI (LLM, optional) | LLM AI manager on a configurable external provider; proposes validated commands with rationale | A competitor that reasons; never touches state directly; not needed to run anything else |
| Chaos engineering | Scheduled and injected disruptions | Turns a model into a crisis-management game |

## Out of scope (explicit)

Kubernetes, Kafka, microservices, vector databases, multiplayer, full 3D engine, multiple industries, user accounts in the MVP, real payment or real store integrations.

## Product vocabulary (use consistently)

World, Scenario, Assumptions, Disruption script, Branch, Fork, Tick, Sim time, Input log, Command, Disruption, Decision window, Observation, Manager (Human, Rule-based AI, LLM AI), Duel, Ledger, State hash, Snapshot, Branch diff, Ensemble, Arm (fixed replay, adaptive policy), Skipped command, Net profit, Contribution, Service level.
