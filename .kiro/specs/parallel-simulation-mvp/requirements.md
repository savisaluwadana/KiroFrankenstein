# Requirements Document

## Introduction

Parallel is a playable, configurable and forkable simulation of an e-commerce business under crisis. This specification covers the hackathon MVP: one business archetype (a single-warehouse online retailer), one flagship scenario (Black Friday Meltdown, 120 simulated hours), a deterministic hybrid discrete-event engine, an immutable input log with replay, rewind and fork, a lightweight double-entry ledger specific to the simulated business with precisely defined financial and service metrics, disruption injection, a human-versus-AI duel with enforced parity, a minimal branch diff, a minimal multi-seed ensemble CLI, and a PixiJS simulation studio that renders authoritative state with accelerated playback, manual stepping and automatic pausing at decision windows.

Every result Parallel produces is synthetic output of a model under stated Assumptions. Parallel never presents results as forecasts or guarantees of real-world outcomes (Requirement 31).

### Scope tiers and build order

| Tier | Scope | Gate |
|---|---|---|
| 1. Vertical slice | The criteria listed in the vertical slice table below | Built first, end to end (engine, API, Web_App), before any secondary capability. |
| 2. MVP core | Every remaining criterion of Requirements 1 to 37 | Built only after the vertical slice passes its acceptance tests. Flagship calibration (Requirement 36) runs only after the simulation and financial modules (Requirements 5 to 12 and 37) are complete. |
| 3. Priority polish | 38 [Polish] | Built only after every MVP acceptance test passes. |
| 4. Stretch | 39 to 43 [Stretch] | Built only after Requirement 38 is done. |

A complete, polished Flagship_Scenario takes precedence over additional features in tiers 2 to 4. The studio UI (World_View, Timeline, Branch_Tree, Playback_Controls) is part of the vertical slice, so UI work starts early and grows with each tier instead of being built last.

#### Vertical slice

| Slice capability | Requirements and criteria in the slice |
|---|---|
| One configurable business | 1, 2, 3.1 to 3.6 (Flagship_Scenario business with synthetic, uncalibrated Assumptions), 4 |
| Working simulation | 5, 6.1 to 6.3, 6.5, 6.6, 7, 8.1 to 8.3, 8.5 to 8.8, 9.1 to 9.4, 9.7, 12, 29.8 to 29.10 (first benchmark) |
| One operational disruption | 16.1 and 16.4, for the scripted supplier_outage only |
| Commands needed for the fork demonstration | 13.1, 13.3, 13.5, 17 for place_purchase_order and set_price, 18.1, 18.2, 18.6, 18.7 |
| Rewind and independent reality branching | 13.2, 14.1, 14.2, 14.5, 15 |
| Authoritative financial metrics | 10, 11.1 to 11.8, 37 |
| Basic interactive PixiJS visualization | 26.2 (create World, read World, fork, advance, submit Commands, end Decision_Window, read Tick_Frames), 26.3, 31.5, 32.1 to 32.7, 32.12, 32.15, 33.1 to 33.7, 33.10 to 33.13, 33.15 to 33.17, 34.1 to 34.8, 35.1 to 35.6, 35.8, 35.9 |

In the slice, the Demo_Fork_Tick is computed by the same rule from the uncalibrated Flagship_Scenario, and the Demo_Commands contain only the backup-supplier place_purchase_order; the staffing Command joins when criteria 9.5 and 9.6 are built, and criterion 34.9 applies once the Calibration_Report exists.

Slice simplification: the slice may persist Worlds, Branches and Inputs in an in-memory repository that implements the same repository ports as the PostgreSQL store and offers no update or delete operation on Inputs (13.2). PostgreSQL persistence (Requirement 28) follows in the MVP core. Every other capability (remaining Commands and Disruption kinds, Duels, AI Managers, diff, ensembles, Progress_Stream, PostgreSQL, golden tests, calibration) is secondary and starts after the slice.

### Project constraints (non-functional, not runtime behaviour)

- **Kiro credit budget.** Development has 4000 Kiro credits in total, of which 500 are an emergency reserve, leaving 3500 for planned tasks. Kiro development credits are independent of runtime LLM API usage, which is billed by the configured external provider.
- **Task shape.** Implementation tasks are small, independently testable and ordered by dependency, so that a task can be verified on its own and the plan can stop at any tier boundary with a working product.
- **Minimal footprint.** No infrastructure, dependency or abstraction beyond the stack in `tech.md` unless a requirement needs it; each added dependency is justified in the design.

### Renumbering from the previous revision

| Previous | Current |
|---|---|
| 1 to 32 | 1 to 32 (3.7 and 3.8 moved to 36.1 and 36.2) |
| (new) playback, stepping and automatic pausing | 33 |
| (new) 60-second demo path | 34 |
| (new) operational visualization | 35 |
| (new) flagship calibration | 36 |
| (new) financial policies and end-of-horizon treatment | 37 |
| 33 [Polish] | 38 [Polish] |
| 34 to 38 [Stretch] | 39 to 43 [Stretch] |

Criterion-level changes: previous 8.6 split into 8.6 and 8.7, new 8.8; previous 10.9 split into 10.9 and 10.10, so previous 10.10 to 10.15 are now 10.11 to 10.16; previous 20.3 split into 20.3 and 20.4, so previous 20.4 to 20.6 are now 20.5 to 20.7. New criteria: 9.7, 15.7, 19.6, 20.8 to 20.12, 21.9, 29.6, 29.7.

Revision 4 keeps every existing number. Criteria are only appended: 1.8, 1.9, 3.9, 5.8, 5.9, 9.8, 9.9, 14.6, 15.8, 17.7, 18.7, 20.13 to 20.18, 21.10 to 21.15, 28.6, 29.8 to 29.11, 33.16, 33.17, 34.6 to 34.9, 36.10. Criteria 3.7 and 3.8 stay unused because their earlier content moved to 36.1 and 36.2. Criteria reworded in place: 5.7, 20.1, 20.3, 21.5, 21.6, 25.9, 33.1, 33.10, 33.13, 34.3 and the Glossary entries State, Composite_Score, Tie_Tolerance, LLM_Configuration, Demo_Path and Snapshot.

Steering references: `product.md`, `tech.md`, `structure.md`, `simulation-invariants.md`, `security.md`, `ui-design-system.md` in `.kiro/steering/`. Invariant IDs (INV-n) refer to `simulation-invariants.md`.

### Risks and simplifications

- **Cross-architecture determinism.** Go may fuse floating-point multiply-add on arm64, so the demo laptop (Apple silicon) and the server (amd64) could diverge. Simplification: no floating point in the engine; integer cents and basis points only (Requirements 6 and 10).
- **Time model.** A pure fixed-step engine hides intra-hour ordering; a pure discrete-event engine makes observation, hashing and decision points irregular. Decision: a documented hybrid. All state changes are events on an integer Sim_Time minute clock; hourly Tick_Boundaries are the only points where Observations, State_Hashes, Snapshots, metrics and Decision_Windows exist (Requirement 5).
- **"Event sourcing" scope.** Storing every derived outcome as an event would multiply storage and complicate forks. Simplification: the input log (seed, scenario, commands, disruptions) is the record; outcomes are derived and rebuildable. Derived outcomes are cached as projections and snapshots, verified by state hash.
- **Duel fairness.** Wall-clock pressure is unequal between a human and software. Simplification: duels are turn-based at decision windows; sim time is paused while managers decide, so latency is irrelevant and parity is defined in sim time.
- **Randomness coupling.** A shared random stream would let a decision change future demand draws, making the duel unfair. Simplification: independent named random streams derived from the seed; exogenous demand and disruption streams consume a fixed number of draws per Tick regardless of decisions (common random numbers). Price and marketing change how many Potential_Arrivals convert to orders, never the draws themselves (Requirement 7).
- **Human knowledge of the script.** A human can read the scenario file and learn the Disruption_Script; the AI sees only the Observation. Simplification: parity is guaranteed for information the system provides. The Disruption_Script is hidden in the UI by default (Requirement 31). This is documented as a known limitation.
- **Ensemble asymmetry.** A human cannot be re-played adaptively under a new seed. Human-versus-AI ensembles therefore compare the human's recorded plan (Fixed_Replay, non-adaptive) with the AI policy (Adaptive_Evaluation), and the report states this asymmetry. A symmetric AI-fixed versus human-fixed comparison is also offered (Requirement 25).
- **Causality in diffs.** Two branches can differ in several decisions at once. A branch diff reports observed differences with exact tick and input references; it does not attribute cause. Attribution requires a counterfactual fork that changes one decision (Requirement 23).
- **Rewind semantics.** Rewind is a read of a past tick; changing the past is always a fork. No history is truncated.
- **Frontend scope.** SSE carries tick progress only; all reads are REST, so a polling fallback is trivial. React Flow is kept for the branch tree. Fork and duel-reveal animations are priority polish after the MVP; they may use CSS, the PixiJS ticker or GSAP, and never animate a number through non-authoritative intermediate values (Requirement 38).
- **LLM dependence.** The AI manager ships with a deterministic rule-based policy (Rule_Based_Manager). The LLM policy (LLM_Manager) is optional, uses a configurable external OpenAI-compatible provider, is validated like human input, and is recorded, so replay never calls the LLM. Without an API key every feature except the LLM opponent works. Ensembles use only the rule-based policy or fixed replays, so they stay offline and cost nothing.
- **Playback versus computation.** Playback speed is presentation only. The engine computes authoritative Ticks at its own pace (a full flagship run within 500 ms, Requirement 29); the Web_App paces the display of already computed Ticks (Requirement 33). Faster or slower playback, stepping and pausing never change any State_Hash.
- **Decision-window density versus a 60-second demo.** A 4-Tick cadence gives 30 Decision_Windows over 120 Ticks, too many to end by hand in 60 seconds. Simplification: an Auto_Pause_Mode "disruption onsets only" lets the human run to the next Disruption onset, ending intervening Human Decision_Windows with no Commands (an explicit choice, identical to pressing "end window" with no Commands). AI Managers still decide at every window, so parity is unchanged (Requirement 33).
- **Vertical slice first.** The slice uses an in-memory repository behind the same ports as PostgreSQL, so the end-to-end path works before persistence, Duels or AI exist. Risk: the in-memory store hides SQL issues until Requirement 28; mitigated by sharing repository contract tests between both stores.
- **Inventory valuation.** Primary and backup suppliers have different unit costs, so a single standard cost would need a purchase-price-variance account. Decision: per-SKU moving weighted average cost in integer cents, with the last unit absorbing the rounding remainder, so supplier choices show up in Cost_Of_Goods_Sold and inventory value is exactly zero when stock is zero (Requirement 37).
- **Calibration integrity.** Tuning the flagship until the AI wins would fabricate the result. Simplification: calibration gates apply only to the no-Command run and the Rule_Based_Manager run, never to human-versus-AI outcomes, and every calibration change is published with its reason (Requirement 36).
- **Model fidelity.** The business model is intentionally small (4 SKUs, 2 suppliers per SKU, 1 warehouse, 1 workforce pool). Fidelity comes from compounding interactions, not entity count.
- **Ensemble cost.** 50 seeds with 2 arms is 100 full runs. The single-run replay target is 500 ms so a 50-seed paired ensemble fits in 60 seconds (Requirement 29). Both figures are targets until measured: the benchmark runs early, its method and hardware are published in `docs/benchmarks.md`, and a missed target is reported as measured instead of being met by weakening correctness (29.8 to 29.11). Larger ensembles are stretch.
- **Score normalization.** Normalizing a score against the other competitor's result would let one manager's play change the other's score. Decision: every Composite_Score component is normalized against Scenario-defined reference values only (Requirement 21.10 to 21.15).
- **LLM cost and drift.** An unbounded or unconfigured LLM manager could spend money or silently use an unverified model. Decision: no default model; the LLM_Manager is unavailable until all of `LLM_BASE_URL`, `LLM_API_KEY` and `LLM_MODEL` are set; calls are capped per Decision_Window and per World; every failure falls back to the Rule_Based_Manager with a recorded reason (Requirement 20).

### Contradictions resolved in revision 2

1. Previous Requirement 15.5 required identical demand arrivals regardless of decisions, while previous Requirement 6.1 made demand depend on price and marketing. Resolved: Potential_Arrivals and their random draws are exogenous and identical; realized orders differ only through deterministic conversion (Requirements 7.1 to 7.3, 18.5).
2. Previous Requirement 15.2 resumed a Branch when the Manager submitted Commands, which conflicted with a Command_Budget of 5 per window. Resolved: only ending the Decision_Window resumes advancement (Requirement 18.2).
3. Previous Requirement 29 required GSAP while `tech.md` deferred GSAP; the UI steering capped motion at 240 ms while the fork animation allowed 600 ms. Resolved: the polish requirement is technology-neutral and the steering allows the two signature moments a longer duration (now Requirement 38).
4. A count-up scoreboard reveal would display non-authoritative intermediate numbers, contradicting Requirement 32.12. Resolved: the reveal animates position and opacity only; numbers appear at their final values (now Requirement 38.3).
5. Fork fidelity (INV-5) cannot hold if the State_Hash covers Branch identifiers. Resolved: the State_Hash scope excludes identifiers and metadata (Requirement 12).
6. Exposing every Assumption conflicts with hiding future Disruptions from managers. Resolved: the Disruption_Script is separate from Assumptions, hidden by default and excluded from the Observation (Requirements 18.6, 31.3, 31.4).
7. Previous performance budget of 2 seconds per replay made a 50-seed paired ensemble take up to 200 seconds. Resolved: the per-replay budget is 500 ms and the ensemble budget is 60 seconds, both to be confirmed by a benchmark (open question 5).

### Contradictions resolved in revision 3

1. The previous build order required the Flagship_Scenario to be complete and calibrated before any other tier, while calibration needs the simulation and financial modules to exist and the user requires a vertical slice first. Resolved: the slice uses the flagship business with synthetic, uncalibrated Assumptions; calibration (Requirement 36) runs in the MVP core after Requirements 5 to 12 and 37 are complete.
2. Previous criterion 3.8 (heuristic beats the no-Command run) could be read as tuning the scenario for AI success. Resolved: the gate is kept only as a sanity check against doing nothing (36.2); no gate compares human and AI outcomes (36.6), and the Calibration_Report publishes every change (36.4, 36.5).
3. The 60-second demo, accelerated playback and the 500 ms replay budget could be read as the engine running "at playback speed". Resolved: the engine computes Ticks ahead of display; playback paces the display of authoritative Tick_Frames only and cannot change any State_Hash (33.3, 33.10). At 16x (32 Ticks per second), 120 Ticks display in 3.75 seconds, leaving most of the 60 seconds for the presenter's actions (Requirement 34).
4. Automatic pausing at every Decision_Window (30 windows in the flagship) conflicted with a 60-second demo. Resolved: the "disruption onsets only" Auto_Pause_Mode and run-to advancement (33.6 to 33.8).
5. The previous Glossary stated "the MVP models no refunds" while the user asked for a refund policy. Resolved: revenue and payment are both recognised at shipment, so a cancelled order was never paid and needs no refund; the MVP models no returns. Late shipments are compensated through service credits (Requirement 37).
6. Previous criteria 10.4 and 10.6 valued receipts and shipments at one standard cost, which cannot represent a more expensive backup supplier without a variance account. Resolved: moving weighted average cost per SKU (Requirement 37); opening inventory uses an opening unit cost Assumption.
7. "Basic PixiJS visualization early" conflicted with the previous tiering that placed all UI after the full MVP engine. Resolved: the World_View, Timeline, Branch_Tree and Playback_Controls are in the vertical slice; only the signature animations stay in priority polish.
8. The name "Heuristic_Manager" did not clearly separate the rule-based and LLM AI. Resolved: renamed Rule_Based_Manager; Inputs record Manager_Provenance, and an LLM fallback is recorded as `ai_rule_based_fallback` (Requirement 20).

### Contradictions resolved in revision 4

1. The previous Composite_Score added cents of Net_Profit to weighted service terms, so its scale depended on business size and the default Tie_Tolerance of 0 cents was arbitrary. Resolved: a 100-point score held in integer hundredths (0 to 10000), with financial performance 50 %, Fill_Rate 30 % and Service_Level 20 % by default, each component normalized against Scenario reference values only, and a Tie_Tolerance of 10 hundredths (0.1 points) (Glossary, 1.8, 3.9, 21.5, 21.10 to 21.15).
2. The previous Demo_Path fixed the demonstration to one place_purchase_order at an unspecified earlier Tick, while the user requires the fork Tick to come from calibration. Resolved: the Demo_Fork_Tick and the demo Commands (a backup-supplier purchase order and, where the calibration run shows a capacity bottleneck, a staffing Command) are derived from the calibration run and published; the demo test asserts reproducibility only, never a financial outcome or a winner (34.6 to 34.9, 36.10).
3. The previous State definition listed "Random_Stream positions", which is not enough to resume a stream whose state is more than a counter, and omitted the Event_Queue insertion sequence counter and the purchase-order identifier counter, so a resumed Snapshot could order or number later events differently. Resolved: State includes the full internal state of every Random_Stream and every counter that affects later events (Glossary State, 14.6, 28.6).
4. Criterion 5.7 placed Commands first at a Sim_Time but did not order Disruption activation, recurring events or the Tick_Boundary steps (hash, Observation, Decision_Window, validation). Resolved: 5.7 is clarified and 5.8, 5.9, 17.7 and 18.7 define the full order.
5. The previous LLM_Configuration could be read as allowing a default model. Resolved: no model identifier is built in; OpenRouter is the documented initial provider, configured through the same three variables (20.13 to 20.18).

#### Revision 4 limitations

- Open question 6 (calibration gates and Seed set) is unchanged; the Demo_Fork_Tick rule in 34.6 depends on the calibration run of 36.1.
- `design.md` predates revision 4. Its composite-score formula, LLM configuration and open-question resolutions need a matching update before tasks are generated.

### Open questions (to resolve in design)

1. Scenario and Inputs_File format: JSON (assumed). JSON avoids a YAML dependency and has a strict decoder in the standard library.
2. Snapshot interval: every 24 ticks is assumed; confirm against the replay budget in Requirement 29.
3. **Resolved (revision 4).** Composite score weights and Tie_Tolerance: financial performance 5000 bps, Fill_Rate 3000 bps, Service_Level 2000 bps by default, configurable and validated to sum to 10000; Tie_Tolerance 10 hundredths of a point (0.1 points on the 100-point scale); normalization uses Scenario reference values only (Requirement 21.10 to 21.15).
4. Hosting for the public demo: a single VM or PaaS running the Go binary and managed PostgreSQL is assumed.
5. **Resolved as a method (revision 4); budgets remain targets.** 500 ms per flagship replay and 60 seconds per 50-seed paired ensemble are targets until a benchmark measures them early in implementation. Hardware, scenario size and methodology are published in `docs/benchmarks.md`; results are never fabricated and correctness is never traded for a target (Requirement 29.8 to 29.11).
6. Flagship calibration gates (Requirement 36.1 and 36.2): confirm that "at least one stockout without Commands" and "the Rule_Based_Manager beats the no-Command run" are the right sanity gates, and confirm the calibration Seed set (Seeds 1 to 20 assumed).
7. Minute offsets of recurring hourly events within a Tick (demand arrivals, fulfilment batch, labor accrual) are fixed per engine version; the exact offsets are a design decision. The ordering rules that the offsets must respect are fixed in 5.7 to 5.9.
8. Marketing effect: MVP models marketing as raising conversion of exogenous Potential_Arrivals. Confirm, or model added traffic by thinning an upper-bound arrival process (also compatible with common random numbers).
9. **Resolved (revision 4).** Playback offers 1x, 4x and 16x (2, 8 and 32 Ticks per second), manual stepping, and fast-forward to the next Decision_Window or the next Disruption onset; playback never affects State (Requirement 33.1, 33.2, 33.10, 33.16, 33.17).
10. **Resolved (revision 4).** The Demo_Fork_Tick is the first scheduled Decision_Window at or after the first crisis Tick of the calibration run; the demo Commands are a backup-supplier purchase order and, where the calibration run shows a capacity bottleneck, a staffing Command; no financial outcome or AI win is hardcoded. The timing test still waits 2 seconds before each user action (Requirement 34.1, 34.6 to 34.9, 36.10).
11. **Resolved (revision 4).** Temporary workers are paid from the hire effective Tick, including onboarding Ticks, and add no capacity until onboarding completes; the onboarding delay and the temporary wage are Assumptions (Requirements 9.5, 9.8, 9.9, 37.14).
12. **Resolved (revision 4).** OpenRouter is the initial configured provider behind the provider-independent OpenAI-compatible adapter. No model identifier is built in; the operator sets `LLM_MODEL` (Requirement 20.13 to 20.18).

## Glossary

- **Parallel**: The complete application described by this document.
- **Simulation_Engine**: The pure Go package (`internal/sim`) that computes world state from a scenario, a seed and an input log, with no I/O, wall clock, global randomness, goroutines or LLM access.
- **Scenario**: A versioned document describing a business (SKUs, suppliers, workforce, warehouse capacity, opening balances), a horizon in Ticks, a demand profile, a Disruption_Script, Decision_Window cadence, Command_Budget, score weights, Profit_Floor, Profit_Target and Tie_Tolerance.
- **Assumptions**: The adjustable numeric parameters inside a Scenario, each with a documented unit, default, minimum, maximum and rationale note. The Disruption_Script is not an Assumption.
- **Disruption_Script**: The list of Disruptions scheduled by a Scenario, each with kind, onset Tick, duration and magnitude.
- **Scenario_Parser**: The component that decodes a Scenario document into a Scenario value.
- **Scenario_Printer**: The component that encodes a Scenario value into a canonical Scenario document.
- **Scenario_Validator**: The component that checks a Scenario value against schema rules and Assumption bounds.
- **World**: A simulation instance created from one Scenario, one seed and one engine version; a World owns a tree of Branches.
- **Seed**: A 64-bit unsigned integer from which every random stream of a World is derived.
- **Random_Stream**: A named, independent deterministic pseudo-random sequence derived from the Seed and a stream name.
- **Sim_Time**: An integer count of simulated minutes since the Scenario start.
- **Event_Queue**: The Simulation_Engine's priority queue of scheduled events, ordered by Sim_Time, then event priority, then insertion sequence.
- **Tick**: One simulated hour. Tick N (N ≥ 1) covers Sim_Time from (N − 1) × 60 up to but excluding N × 60. Tick 0 denotes the initial State. The last Tick equals the Scenario horizon.
- **Tick_Boundary**: The Sim_Time N × 60 at which the State "at Tick N" is defined: the State after every event with Sim_Time less than N × 60 has been processed.
- **Branch**: A line of history in a World, identified by a UUIDv4, with an optional parent Branch and fork Tick, a Head_Tick, a label and a Manager kind.
- **Root_Branch**: The Branch created with a World; the Root_Branch has no parent.
- **Head_Tick**: The highest Tick for which a Branch's State has been computed.
- **Input**: An immutable record that influences simulation results: a Command or a Disruption, with Branch, effective Tick, sequence number, author kind (Manager_Provenance), payload and metadata (server timestamp, idempotency key, rationale, model name).
- **Input_Log**: The append-only store of Inputs for all Branches.
- **Inputs_File**: A JSON document listing Inputs (effective Tick, kind, payload, Manager_Provenance and optional rationale) together with the Scenario hash, Seed and engine version under which they were recorded.
- **Inputs_File_Parser**: The component that decodes an Inputs_File into a list of Inputs.
- **Inputs_File_Printer**: The component that encodes a list of Inputs into a canonical Inputs_File.
- **Command**: A manager decision drawn from the Command_Set.
- **Command_Set**: The closed set of Command kinds: set_price, place_purchase_order, expedite_purchase_order, schedule_overtime, hire_temp_workers, set_marketing_spend.
- **Command_Validator**: The deterministic component that accepts or rejects a proposed Command against the current Observation, the Command_Set rules and the remaining Command_Budget.
- **Disruption**: An exogenous shock: demand_surge, supplier_outage or workforce_absence, with a start Tick, a duration in Ticks and a magnitude.
- **Disruption_Injector**: The component that records user-injected Disruptions as Inputs.
- **Potential_Arrival**: An exogenous prospective customer visit for a SKU, generated from the demand Random_Stream, which becomes a customer order or is lost according to a deterministic conversion rule.
- **State**: The complete derived simulation state of a Branch at a Tick, sufficient on its own (with the Scenario and the Inputs with later effective Ticks) to continue deterministic execution: clock, inventory with per-SKU Inventory_Values, orders and the order sequence counter, purchase orders and the purchase order identifier counter, workforce including temporary workers in onboarding and scheduled overtime, active Disruptions, current prices and marketing spend, pending Event_Queue entries with the Event_Queue insertion sequence counter, Ledger, and the full internal state of every Random_Stream.
- **State_Hash**: The SHA-256 digest of the canonical encoding of a State.
- **Snapshot**: A stored canonical encoding of a complete State with its Tick and State_Hash, used to accelerate reads and replay; resuming from a Snapshot needs no other derived data.
- **Snapshot_Store**: The persistence adapter that saves and loads Snapshots.
- **Ledger**: The lightweight double-entry journal of a Branch, specific to the simulated business, recorded in integer cents, with exactly these accounts: Cash, Inventory, Opening_Equity, Revenue, Cost_Of_Goods_Sold, Labor_Expense, Freight_Expense, Marketing_Expense, Overhead_Expense and Service_Credit_Expense.
- **Ledger_Entry**: One balanced journal entry with Tick, Sim_Time, description, cause reference and two or more lines.
- **Opening_Unit_Cost**: The per-SKU Assumption, in cents, at which opening inventory is valued.
- **Supplier_Unit_Cost**: The per-supplier, per-SKU Assumption, in cents, paid for each received unit.
- **Inventory_Value**: The per-SKU carrying value of on-hand inventory in cents; the Inventory account balance is the sum of Inventory_Values.
- **Moving_Average_Cost**: The inventory valuation method of the MVP. A receipt adds Supplier_Unit_Cost times quantity to Inventory_Value. Shipping q units from u on-hand units with Inventory_Value V costs V when q equals u, and otherwise V × q / u rounded half to even.
- **End_Of_Horizon_Statement**: The report produced when a Branch reaches the horizon: Cash, Inventory_Value per SKU and in total, Net_Profit, Contribution, Net_Cash_Flow, Backlog, Late_Backlog, cancelled orders and open purchase orders with units and committed cost.
- **Fulfilment_Constraint_Report**: The per-Tick, per-SKU outcome record of open orders and units left unshipped because on-hand inventory was insufficient (stock-bound) and because fulfilment capacity was exhausted (capacity-bound).
- **Tick_Frame**: The read model of one Tick of one Branch used by the World_View: State summary, metrics, Fulfilment_Constraint_Report, active Disruptions, Inputs taking effect at that Tick and State_Hash.
- **Metrics_Calculator**: The pure component in `internal/sim` that computes Net_Revenue, Variable_Cost, Contribution, Net_Profit, Net_Cash_Flow, Service_Level, Fill_Rate, Backlog and the Composite_Score with its components from the Ledger, order records and Scenario.
- **Net_Revenue**: The sum of Revenue account credits from Tick 0 through the stated Tick, in cents. Revenue is recognised at shipment; the MVP models no returns or discounts other than the set price, and cancelled orders were never paid, so no refunds arise (Requirement 37).
- **Variable_Cost**: The sum of Cost_Of_Goods_Sold and Freight_Expense debits from Tick 0 through the stated Tick, in cents.
- **Contribution**: Net_Revenue minus Variable_Cost, in cents.
- **Net_Profit**: Contribution minus Labor_Expense, Marketing_Expense, Overhead_Expense and Service_Credit_Expense from Tick 0 through the stated Tick, in cents. The MVP models no tax, interest or depreciation.
- **Net_Cash_Flow**: The sum of Cash account lines over a stated Tick range, excluding the opening Ledger_Entry, in cents.
- **Orders_Due**: At Tick T, the orders whose promised ship-by Sim_Time is less than or equal to T × 60, whether open, shipped or cancelled.
- **Service_Level**: At Tick T, the number of Orders_Due that shipped at or before their promised ship-by Sim_Time, divided by the number of Orders_Due, in basis points.
- **Fill_Rate**: At Tick T, the units in Orders_Due that shipped by T × 60, divided by the units in Orders_Due, in basis points.
- **Backlog**: At Tick T, the count and the units of orders that are neither shipped nor cancelled. **Late_Backlog** is the subset whose promised ship-by Sim_Time has passed.
- **Composite_Score**: The duel and ensemble score of a Branch at the Comparison_Tick, on a 100-point scale held as an integer number of hundredths of a point from 0 to 10000: RoundHalfEven(Financial_Weight × Financial_Component + Fulfilment_Weight × Fulfilment_Component + On_Time_Weight × On_Time_Component, 10000), where RoundHalfEven(a, b) is the shared half-to-even rounding of a / b. The Composite_Score of a Branch depends only on that Branch's own metrics and the Scenario.
- **Score_Weights**: The Scenario Assumptions Financial_Weight, Fulfilment_Weight and On_Time_Weight, each an integer number of basis points from 0 to 10000, summing to exactly 10000. Defaults: 5000 (50 %), 3000 (30 %) and 2000 (20 %).
- **Profit_Floor**: The Scenario Assumption, in cents, at or below which the Financial_Component is 0.
- **Profit_Target**: The Scenario Assumption, in cents, at or above which the Financial_Component is 10000; Profit_Target is greater than Profit_Floor.
- **Financial_Component**: At the Comparison_Tick, 0 when Net_Profit ≤ Profit_Floor, 10000 when Net_Profit ≥ Profit_Target, and otherwise RoundHalfEven((Net_Profit − Profit_Floor) × 10000, Profit_Target − Profit_Floor), in basis points.
- **Fulfilment_Component**: The Fill_Rate at the Comparison_Tick, in basis points (0 to 10000).
- **On_Time_Component**: The Service_Level at the Comparison_Tick, in basis points (0 to 10000).
- **Tie_Tolerance**: A non-negative integer number of hundredths of a Composite_Score point, defined by the Scenario with a default of 10 (0.1 points); two Composite_Scores whose absolute difference is less than or equal to Tie_Tolerance are a tie.
- **Comparison_Tick**: The single Tick at which two Branches or two ensemble runs are compared.
- **Observation**: The manager-visible projection of a State at a Tick: current inventory, orders, purchase orders, workforce, active Disruptions, Ledger-derived metrics, history up to the Tick and the Assumptions. The Observation excludes the Disruption_Script beyond the current Tick, future random draws and the Seed.
- **Decision_Window**: A pause point at a Tick_Boundary where a manager may submit up to the Command_Budget; Decision_Windows occur at the Scenario cadence, at the onset Tick of every Disruption and at the fork Tick of a child Branch (Requirement 15.7).
- **Command_Budget**: The maximum number of accepted Commands per Decision_Window, defined by the Scenario.
- **Manager**: The actor controlling a Branch: Human, Rule-based AI (Rule_Based_Manager) or LLM AI (LLM_Manager).
- **Rule_Based_Manager**: The deterministic AI policy (`internal/policy/rulebased`) using reorder-point, safety-stock and capacity-gap rules. Its proposals are rule-based and make no optimality claim. UI label: "Rule-based AI".
- **LLM_Manager**: The optional AI policy (`internal/policy/llm`) that requests Command proposals from a configurable external provider through a provider-independent adapter for the OpenAI-compatible chat completions endpoint. OpenRouter is the initial configured provider. UI label: "LLM AI".
- **Manager_Provenance**: The recorded origin of an Input: `human`, `ai_rule_based`, `ai_llm` or `ai_rule_based_fallback` (rule-based proposals used because the LLM_Manager failed), plus the LLM model name for `ai_llm` and the LLM_Fallback_Reason for `ai_rule_based_fallback`. Manager_Provenance is Input metadata and is excluded from the State_Hash.
- **LLM_Configuration**: The server environment variables `LLM_BASE_URL`, `LLM_API_KEY` and `LLM_MODEL`. LLM_Configuration is present only when all three are set and non-empty. Parallel defines no default value for any of the three, including the model identifier.
- **LLM_Call_Budget**: The limits on LLM requests: at most one request per Decision_Window per Branch, and at most `LLM_MAX_CALLS_PER_WORLD` requests per World over its lifetime (an optional server environment variable, default 40, bounds 1 to 200). A Duel's LLM requests count against its World.
- **LLM_Fallback_Reason**: The reason code recorded when the Rule_Based_Manager replaces the LLM_Manager: `timeout`, `http_error`, `invalid_json`, `schema_violation` or `budget_exhausted`.
- **Duel**: A pair of sibling Branches forked from the same parent Branch and Tick, one controlled by a Human Manager and one by an AI Manager, advanced in lockstep.
- **Duel_Service**: The application component that creates, advances and scores Duels.
- **Branch_Service**: The application component that creates Branches, forks them, advances them and reads their States.
- **Branch_Diff_Service**: The application component that compares two Branches of the same World and reports observed differences.
- **First_Divergence_Tick**: The lowest Tick, up to the Comparison_Tick, at which the State_Hashes of two compared Branches differ.
- **Ensemble_Runner**: The Sim_CLI component that runs one Scenario under a list of Seeds for one or two arms and writes an Ensemble_Report.
- **Arm**: One decision source in an ensemble: a Fixed_Replay of an Inputs_File, or an Adaptive_Evaluation of the Rule_Based_Manager.
- **Fixed_Replay**: An Arm that re-applies the Commands and Disruptions of an Inputs_File at their recorded effective Ticks under each Seed, without adapting them.
- **Adaptive_Evaluation**: An Arm in which the Rule_Based_Manager decides at every Decision_Window from the Observation of each Seed's run.
- **Skipped_Command**: A Fixed_Replay Command that failed the Command_Validator under a given Seed and was not applied.
- **Paired_Experiment**: An ensemble with two Arms in which both Arms run with the same Seed for each Seed in the list.
- **Ensemble_Report**: The deterministic JSON and CSV output of the Ensemble_Runner.
- **Disclaimer**: The text "Synthetic simulation output under the stated assumptions. Not a forecast or guarantee of real-world results."
- **Parallel_API**: The Chi HTTP API under `/api/v1`, described by `backend/api/openapi.yaml`.
- **Progress_Stream**: The Server-Sent Events endpoint that publishes tick progress for a Branch or Duel.
- **Sim_CLI**: The headless command-line program `parallel-sim`.
- **Web_App**: The Next.js frontend.
- **World_View**: The PixiJS warehouse diorama in the Web_App.
- **Timeline**: The Web_App scrubber across the Scenario horizon.
- **Branch_Tree**: The React Flow view of a World's Branches.
- **Decision_Panel**: The Web_App panel for building and submitting Commands.
- **Ledger_Panel**: The Web_App panel showing profit and loss and Ledger_Entries.
- **Assumptions_Editor**: The Web_App form for adjusting Assumptions before World creation.
- **Assumptions_Panel**: The Web_App read-only view of a World's effective Assumptions.
- **Diff_Panel**: The Web_App view of a Branch_Diff_Service result.
- **HUD**: The compact top bar of the Web_App showing sim clock, Cash, Net_Profit, Service_Level and Backlog of the displayed Tick.
- **Playback_Controls**: The Web_App controls that pause, play at 1x, 4x or 16x, step the displayed Tick, and fast-forward to the next Decision_Window or the next Disruption onset.
- **Auto_Pause_Mode**: The Web_App playback setting that decides where playback pauses automatically: "every decision window" (default) or "disruption onsets only".
- **Run_To_Tick**: An optional target Tick on an advance request; the Branch advances until the target Tick or the next Disruption onset Decision_Window, whichever comes first.
- **Crisis_Tick**: The lowest Tick of the Flagship_Scenario run with the default Seed and no Commands at which a Disruption is active or the Fulfilment_Constraint_Report lists a stock-bound order.
- **Demo_Fork_Tick**: The lowest scheduled Decision_Window Tick (cadence or Disruption onset) greater than or equal to the Crisis_Tick such that a backup-supplier purchase order placed in that Decision_Window is received before the horizon. The Demo_Fork_Tick is computed from the Flagship_Scenario run with the default Seed and no Commands and, once Requirement 36 is built, published in the Calibration_Report.
- **Demo_Commands**: The Commands submitted in the Demo_Path fork: one place_purchase_order to the backup supplier for the SKU with the earliest stock-bound report after the Demo_Fork_Tick in the no-Command run, and, where that run reports capacity-bound orders after the Demo_Fork_Tick, one hire_temp_workers or schedule_overtime Command. Quantities and the choice of staffing Command are published in the Calibration_Report with their reason.
- **Demo_Path**: The scripted 60-second demonstration: start a Flagship_Scenario World with the default Seed, play past the Demo_Fork_Tick to the next Disruption onset (or to the horizon when no later onset exists), rewind to the Demo_Fork_Tick, fork, submit the Demo_Commands in the fork, play the fork and the parent to the horizon, and show both Branches in the Branch_Tree with the parent comparison in the HUD.
- **Benchmark_Report**: The document `docs/benchmarks.md` that records performance measurements with hardware, operating system, Go version, scenario size, methodology and raw results.
- **Calibration_Report**: The document `docs/calibration.md` that records the Flagship_Scenario's synthetic Assumptions, calibration gates, Seeds, results and every calibration change with its reason.
- **Flagship_Scenario**: The Scenario file `black-friday.json`, "Black Friday Meltdown".
- **Staffing_Advisor**: [Stretch] The optimization component that recommends overtime and temporary workers from an Observation.
- **Read_Only_Link**: [Stretch] A random identifier distinct from the World identifier that grants read-only access to one World.

## Requirements

### Requirement 1: Scenario parsing and validation

**User Story:** As an operations lead, I want to load a business scenario from a file, so that the simulation runs on explicit, inspectable assumptions.

#### Acceptance Criteria

1. WHEN a Scenario document that conforms to the Scenario schema is provided, THE Scenario_Parser SHALL produce a Scenario value.
2. IF a Scenario document contains an unknown field, a missing required field or a value of the wrong type, THEN THE Scenario_Parser SHALL return an error that names the JSON path of the first offending field.
3. WHEN a Scenario value is produced, THE Scenario_Validator SHALL check every Assumption against the documented minimum and maximum of that Assumption.
4. IF an Assumption lies outside the documented bounds, THEN THE Scenario_Validator SHALL return an error that names the Assumption, the provided value and the allowed range.
5. IF a Scenario document declares a schema version other than the versions supported by the Simulation_Engine, THEN THE Scenario_Parser SHALL return an unsupported-version error.
6. THE Scenario_Validator SHALL require every monetary Assumption to be an integer number of cents and every rate Assumption to be an integer number of basis points.
7. THE Scenario_Validator SHALL require every Assumption to carry a unit, a default, a minimum, a maximum and a rationale note.
8. IF the Score_Weights of a Scenario do not sum to exactly 10000 basis points or any Score_Weight lies outside 0 to 10000, THEN THE Scenario_Validator SHALL return an error that names the Score_Weights and their sum.
9. IF the Profit_Target of a Scenario is less than or equal to the Profit_Floor, or the Tie_Tolerance lies outside 0 to 10000, THEN THE Scenario_Validator SHALL return an error that names the offending Assumption and the allowed range.

### Requirement 2: Scenario printing and round trip

**User Story:** As a developer, I want scenarios to serialize canonically, so that stored scenarios, hashes and edited assumptions are reproducible.

#### Acceptance Criteria

1. THE Scenario_Printer SHALL encode a Scenario value into a Scenario document with a fixed field order and no insignificant whitespace variation.
2. FOR ALL valid Scenario values, parsing the output of the Scenario_Printer SHALL produce a Scenario value equal to the original (round-trip property).
3. FOR ALL valid Scenario values, printing, parsing and printing again SHALL produce a byte-identical document to the first print (idempotence property).

### Requirement 3: Flagship scenario content

**User Story:** As a demo presenter, I want a complete Black Friday scenario built on transparent synthetic assumptions, so that every demo shows the same meaningful crisis with the same numbers.

#### Acceptance Criteria

1. THE Flagship_Scenario SHALL define a horizon of 120 Ticks starting Thursday 00:00 simulated time.
2. THE Flagship_Scenario SHALL define 4 SKUs, 2 suppliers (primary and backup) per SKU, 1 warehouse and 1 workforce pool.
3. THE Flagship_Scenario SHALL schedule in its Disruption_Script one demand_surge, one supplier_outage affecting the primary supplier and one workforce_absence, with onset Ticks after Tick 24.
4. THE Flagship_Scenario SHALL pass the Scenario_Validator without errors.
5. THE Flagship_Scenario SHALL define a Decision_Window cadence of 4 Ticks and a Command_Budget of 5 Commands per Decision_Window.
6. THE Flagship_Scenario SHALL label each Assumption's rationale note as either "synthetic, illustrative" or with a cited public source.

Calibration of the Flagship_Scenario (previously criteria 3.7 and 3.8) is specified in Requirement 36. Criterion numbers 3.7 and 3.8 are left unused.

9. THE Flagship_Scenario SHALL define Score_Weights of 5000, 3000 and 2000 basis points for Financial_Weight, Fulfilment_Weight and On_Time_Weight, a Tie_Tolerance of 10 hundredths of a point, and a Profit_Floor and Profit_Target whose values and rationale are published in the Calibration_Report.

### Requirement 4: World creation

**User Story:** As a player, I want to create a world from a scenario and a seed, so that I get a fresh, executable business.

#### Acceptance Criteria

1. WHEN a World creation request with a valid Scenario and a Seed is received, THE Branch_Service SHALL create a World, a Root_Branch and the Tick 0 State, and return the World identifier and Root_Branch identifier.
2. WHEN a World creation request omits the Seed, THE Branch_Service SHALL use the Scenario's default Seed.
3. THE Branch_Service SHALL record the Scenario in canonical form, the Seed and the Simulation_Engine version with each World.
4. WHEN Assumptions overrides are supplied with a World creation request, THE Branch_Service SHALL apply the overrides to the Scenario and validate the result with the Scenario_Validator before creating the World.
5. IF the Scenario or the Assumptions overrides fail validation, THEN THE Branch_Service SHALL reject the request with the validation errors and create no World.

### Requirement 5: Engine time model (hybrid discrete-event)

**User Story:** As a simulation scientist, I want a precisely defined time model, so that event ordering, observation points and decision timing are unambiguous and reproducible.

#### Acceptance Criteria

1. THE Simulation_Engine SHALL represent Sim_Time as an integer count of simulated minutes since the Scenario start.
2. THE Simulation_Engine SHALL apply every State change as an event taken from the Event_Queue.
3. WHEN the Simulation_Engine advances a Branch by one Tick, THE Simulation_Engine SHALL process all events scheduled within that Tick in ascending order of Sim_Time, then event priority, then insertion sequence.
4. THE Simulation_Engine SHALL define the State at Tick N as the State after every event with Sim_Time less than N × 60 has been processed.
5. THE Simulation_Engine SHALL schedule recurring hourly processes (Potential_Arrival generation, fulfilment, labor accrual, marketing and overhead accrual) as events at minute offsets within each Tick that are fixed for a given engine version and documented in the design.
6. THE Simulation_Engine SHALL compute Observations, State_Hashes, Snapshots, metrics and Decision_Windows only at Tick_Boundaries.
7. WHEN a Command has effective Tick N + 1, THE Simulation_Engine SHALL apply the Command at Sim_Time N × 60 before every other event at that Sim_Time, with Commands of equal effective Tick applied in ascending Input sequence order.
8. WHEN several events share one Sim_Time, THE Simulation_Engine SHALL process them in this order: Commands (criterion 7), then Disruption expiries, then Disruption activations (Disruption_Script entries in script order before injected Disruptions in ascending Input sequence order), then every other event in ascending event priority and insertion sequence.
9. WHEN the Simulation_Engine reaches the Tick_Boundary of Tick N, THE Simulation_Engine SHALL, in this order, fix the State at Tick N, compute the State_Hash, metrics, Fulfilment_Constraint_Report and Tick_Frame from that State, store a Snapshot when Tick N is due, derive the Observation from that State, open a Decision_Window when Tick N is a Decision_Window Tick, and process no event at Sim_Time N × 60 or later until any open Decision_Window has ended.

### Requirement 6: Deterministic engine

**User Story:** As a simulation scientist, I want a deterministic engine, so that every result is reproducible and explainable.

#### Acceptance Criteria

1. THE Simulation_Engine SHALL compute State transitions using integer arithmetic only.
2. THE Simulation_Engine SHALL derive each Random_Stream from the Seed and the stream name, independent of every other Random_Stream.
3. FOR ALL Scenarios, Seeds and Input_Logs, computing a Branch twice SHALL produce identical State_Hash values at every Tick (determinism property, INV-1).
4. THE Simulation_Engine SHALL produce the committed golden State_Hash for the recorded Flagship_Scenario demo inputs on both amd64 and arm64.
5. THE Simulation_Engine SHALL compute the State_Hash after every Tick from the canonical encoding of the State defined in Requirement 12.
6. WHEN the Head_Tick of a Branch equals the Scenario horizon, THE Simulation_Engine SHALL reject further advancement with a horizon-reached error.

### Requirement 7: Demand and orders

**User Story:** As an operations lead, I want customer demand to respond to time of day, surges, price and marketing, so that pricing and marketing decisions have real consequences.

#### Acceptance Criteria

1. WHEN a Tick is processed, THE Simulation_Engine SHALL generate Potential_Arrivals per SKU from the Scenario's hourly demand profile and active demand_surge magnitudes, using the demand Random_Stream.
2. THE Simulation_Engine SHALL consume a number of demand Random_Stream draws per Tick that is independent of Manager Commands (common random numbers, INV-12).
3. WHEN a Potential_Arrival is evaluated, THE Simulation_Engine SHALL convert the Potential_Arrival into a customer order using a conversion rate in basis points computed from the current price relative to the reference price, the price elasticity Assumption and the marketing spend effect Assumption.
4. WHEN an order is created, THE Simulation_Engine SHALL record the order's SKU, quantity, unit price, creation Sim_Time and promised ship-by Sim_Time.
5. WHEN an open order reaches the Scenario's cancellation threshold Sim_Time, THE Simulation_Engine SHALL cancel the order at that Sim_Time.
6. FOR ALL Branches and Ticks, the number of orders created SHALL equal the sum of open, shipped and cancelled orders (order conservation property, INV-9).

### Requirement 8: Inventory and suppliers

**User Story:** As an operations lead, I want stock, purchase orders and supplier lead times to be modeled, so that supply decisions and supplier failures affect fulfilment.

#### Acceptance Criteria

1. WHEN a place_purchase_order Command is executed, THE Simulation_Engine SHALL schedule a receipt at the effective Tick plus the supplier's lead time for the ordered SKU and quantity.
2. WHEN a purchase order receipt occurs, THE Simulation_Engine SHALL increase on-hand inventory of the SKU by the received quantity.
3. WHILE a supplier_outage is active for a supplier, THE Simulation_Engine SHALL delay every receipt from that supplier scheduled during the outage until the outage end Tick plus the supplier's lead time.
4. WHEN an expedite_purchase_order Command is executed for an open purchase order, THE Simulation_Engine SHALL reduce that order's remaining lead time by the Scenario's expedite reduction and record the expedite fee.
5. THE Simulation_Engine SHALL assign purchase order identifiers as a per-Branch sequence derived from Input order.
6. FOR ALL Branches and Ticks, opening inventory plus received units minus shipped units minus written-off units SHALL equal on-hand inventory per SKU (inventory conservation property, INV-8).
7. FOR ALL Branches and Ticks, on-hand inventory of every SKU SHALL be greater than or equal to zero (non-negative stock property, INV-8).
8. THE Simulation_Engine SHALL record zero written-off units in the MVP, which models no shrinkage, damage or expiry.

### Requirement 9: Workforce and fulfilment capacity

**User Story:** As an operations lead, I want warehouse labor to limit how many orders ship, so that staffing decisions and absences matter.

#### Acceptance Criteria

1. WHEN a Tick is processed, THE Simulation_Engine SHALL compute fulfilment capacity as on-shift workers multiplied by the units-per-worker-hour Assumption, plus overtime and temporary worker capacity.
2. WHEN a Tick is processed, THE Simulation_Engine SHALL ship open orders in ascending order of promised ship-by Sim_Time, then order creation sequence, until capacity or on-hand inventory for each order's SKU is exhausted.
3. THE Simulation_Engine SHALL ship each order in full in a single shipment.
4. WHILE a workforce_absence is active, THE Simulation_Engine SHALL reduce on-shift workers by the Disruption magnitude in basis points, rounded half to even.
5. WHEN a hire_temp_workers Command is executed, THE Simulation_Engine SHALL add the temporary workers to capacity after the Scenario's onboarding delay in Ticks.
6. WHEN a schedule_overtime Command is executed, THE Simulation_Engine SHALL add overtime hours for the requested Ticks, limited by the Scenario's maximum overtime per worker.
7. WHEN a Tick is processed, THE Simulation_Engine SHALL record a Fulfilment_Constraint_Report listing, per SKU, the open orders and units left unshipped because on-hand inventory was insufficient and, separately, because fulfilment capacity was exhausted.
8. WHILE a temporary worker hired by a hire_temp_workers Command is within the onboarding delay, THE Simulation_Engine SHALL add zero fulfilment capacity for that worker, while the wages of criterion 37.14 still accrue.
9. THE Scenario SHALL define the temporary worker onboarding delay in Ticks and the temporary hourly wage in cents as Assumptions with unit, default, minimum, maximum and rationale note.

### Requirement 10: Financial ledger

**User Story:** As a founder, I want every financial figure calculated by a double-entry ledger, so that I can trust and audit the results.

#### Acceptance Criteria

1. THE Ledger SHALL record every monetary amount as a signed 64-bit integer number of cents.
2. WHEN a World is created, THE Ledger SHALL record one opening Ledger_Entry at Tick 0 that debits Cash by the opening cash Assumption and Inventory by the sum over SKUs of opening units times Opening_Unit_Cost, and credits Opening_Equity by the total.
3. WHEN an order ships, THE Ledger SHALL debit Cash and credit Revenue by the order's unit price times quantity.
4. WHEN an order ships, THE Ledger SHALL debit Cost_Of_Goods_Sold and credit Inventory by the Moving_Average_Cost amount for the shipped quantity defined in Requirement 37.
5. WHEN an order ships, THE Ledger SHALL debit Freight_Expense and credit Cash by the outbound shipping cost Assumption times quantity.
6. WHEN a purchase order is received, THE Ledger SHALL debit Inventory and credit Cash by the Supplier_Unit_Cost of the supplying supplier times received quantity.
7. WHEN an expedite_purchase_order Command is executed, THE Ledger SHALL debit Freight_Expense and credit Cash by the expedite fee.
8. WHEN a Tick is processed, THE Ledger SHALL record labor expense for regular, overtime and temporary hours at the Scenario's wage rates as separate Ledger_Entry lines, following the wage policy in Requirement 37.
9. WHEN a Tick is processed, THE Ledger SHALL record the Tick's marketing spend as Marketing_Expense against Cash.
10. WHEN a Tick is processed, THE Ledger SHALL record the Scenario's hourly overhead as Overhead_Expense against Cash.
11. WHEN an order ships after its promised ship-by Sim_Time, THE Ledger SHALL debit Service_Credit_Expense and credit Cash by the Scenario's service credit per late order.
12. THE Ledger SHALL settle every cash amount in the same Ledger_Entry as the event that causes the amount, so the MVP has no receivables, payables or payment terms.
13. THE Ledger SHALL link every Ledger_Entry to the event, Command or Disruption that caused the Ledger_Entry.
14. FOR ALL Branches and Ticks, total debits SHALL equal total credits (ledger balance property, INV-6).
15. FOR ALL Branches and Ticks, the Cash balance SHALL equal opening cash plus the sum of all Cash lines after the opening Ledger_Entry (cash reconciliation property, INV-7).
16. THE Simulation_Engine SHALL apply a single shared half-to-even rounding function for every integer division that produces cents or basis points.

### Requirement 11: Financial and service metric definitions

**User Story:** As a founder, I want every reported figure to have one precise definition, so that profit, cash and service comparisons mean the same thing everywhere.

#### Acceptance Criteria

1. THE Metrics_Calculator SHALL compute Net_Revenue, Variable_Cost, Contribution and Net_Profit at a Tick exactly as defined in the Glossary, from Ledger account balances through that Tick.
2. THE Metrics_Calculator SHALL compute Net_Cash_Flow for a Tick range as the sum of Cash lines with Tick inside the range, excluding the opening Ledger_Entry.
3. THE Metrics_Calculator SHALL compute Service_Level at a Tick as on-time shipped Orders_Due times 10000 divided by Orders_Due, rounded half to even.
4. WHEN Orders_Due at a Tick is zero, THE Metrics_Calculator SHALL report Service_Level and Fill_Rate as 10000 basis points.
5. THE Metrics_Calculator SHALL compute Fill_Rate at a Tick as shipped units of Orders_Due times 10000 divided by units of Orders_Due, rounded half to even.
6. THE Metrics_Calculator SHALL report Backlog and Late_Backlog at a Tick as both an order count and a unit count.
7. FOR ALL Branches and Ticks, Net_Profit SHALL equal the Revenue balance minus the sum of all expense account balances (profit consistency property).
8. FOR ALL Branches and Ticks T, Net_Profit at T minus Net_Cash_Flow over Ticks 0 to T SHALL equal the Inventory balance at T minus the opening Inventory balance (accrual-to-cash reconciliation property).
9. THE Parallel_API SHALL document in the OpenAPI document the formula and Ledger accounts behind every reported metric (INV-10).

### Requirement 12: Canonical state hash scope

**User Story:** As a judge, I want state hashes that depend only on simulation content, so that equal hashes prove equal simulations and forks compare cleanly.

#### Acceptance Criteria

1. THE Simulation_Engine SHALL encode a State for hashing with a fixed field order and collections sorted by documented keys.
2. THE Simulation_Engine SHALL include in the State_Hash only the State components listed in the Glossary definition of State.
3. THE Simulation_Engine SHALL exclude from the State_Hash World identifiers, Branch identifiers, Branch labels, Manager kinds, Manager_Provenance, wall-clock timestamps, idempotency keys, rationale text, LLM model names and the engine version string.
4. FOR ALL pairs of Branches with the same Scenario, Seed and Input payloads at the same effective Ticks, the State_Hashes SHALL be equal at every Tick regardless of Input metadata and Branch metadata (metadata independence property).
5. FOR ALL States, decoding the canonical encoding SHALL produce a State equal to the original, and re-encoding SHALL produce byte-identical output (round-trip property).

### Requirement 13: Immutable input log

**User Story:** As a player, I want history to be permanent, so that no result can be quietly rewritten.

#### Acceptance Criteria

1. WHEN a Command or Disruption is accepted, THE Input_Log SHALL append an Input with Branch identifier, effective Tick, sequence number, Manager_Provenance, payload and a server timestamp.
2. IF an update or delete of an existing Input is attempted, THEN THE Input_Log SHALL reject the operation.
3. THE Input_Log SHALL assign effective Tick N+1 to every Command submitted at the Decision_Window of Tick N.
4. WHEN a request to append an Input carries an idempotency key already recorded for the same Branch, THE Branch_Service SHALL return the original result and append no new Input (idempotence property).
5. IF an Input's effective Tick is less than or equal to the Branch's Head_Tick, THEN THE Branch_Service SHALL reject the Input with a history-is-immutable error that suggests forking.

### Requirement 14: Replay and rewind

**User Story:** As a player, I want to scrub back to any past hour, so that I can see exactly how the crisis unfolded.

#### Acceptance Criteria

1. WHEN a State is requested for a Tick between 0 and the Head_Tick of a Branch, THE Branch_Service SHALL return the State, Observation, metrics and State_Hash for that Tick.
2. THE Branch_Service SHALL serve a rewind read without changing the Branch's Head_Tick or Input_Log.
3. FOR ALL Branches and Ticks, the State_Hash computed by replaying from Tick 0 SHALL equal the State_Hash computed by resuming from any stored Snapshot (replay equivalence property, INV-2).
4. IF a loaded Snapshot's State_Hash differs from the replayed State_Hash for the same Tick, THEN THE Branch_Service SHALL discard the Snapshot, serve the replayed State and log a snapshot-mismatch warning.
5. IF a State is requested for a Tick greater than the Head_Tick, THEN THE Branch_Service SHALL return a not-yet-simulated error.
6. FOR ALL Branches, Ticks N and Input suffixes with effective Ticks greater than N, continuing from the State decoded from a Snapshot at Tick N SHALL produce the same State_Hash at every Tick from N to the horizon as an uninterrupted run from Tick 0, including identical Event_Queue order, purchase order identifiers and Random_Stream draws (snapshot completeness property).

### Requirement 15: Forking branches

**User Story:** As a player, I want to fork reality at any past hour and make different decisions, so that I can compare alternative futures.

#### Acceptance Criteria

1. WHEN a fork request names a parent Branch and a fork Tick between 0 and the parent's Head_Tick, THE Branch_Service SHALL create a child Branch whose Head_Tick equals the fork Tick.
2. THE Branch_Service SHALL make the child Branch inherit the parent's Inputs with effective Tick less than or equal to the fork Tick, without copying or modifying those Inputs.
3. FOR ALL forks, the child Branch's State_Hash at the fork Tick SHALL equal the parent Branch's State_Hash at the fork Tick (fork fidelity property, INV-5).
4. FOR ALL forks, appending Inputs to and advancing a child Branch SHALL leave every State_Hash of the parent Branch and of every sibling Branch unchanged, and the reverse (fork independence property, INV-4).
5. IF a World already has 32 Branches, THEN THE Branch_Service SHALL reject the fork request with a branch-limit error.
6. IF the fork Tick is greater than the parent's Head_Tick, THEN THE Branch_Service SHALL reject the fork request with a not-yet-simulated error.
7. WHEN a child Branch is created by a fork, THE Branch_Service SHALL open a Decision_Window on the child Branch at the fork Tick.
8. FOR ALL forks at Tick F, Inputs appended to the parent Branch after the fork, and parent advancement after the fork, the child Branch SHALL read from the parent only the Inputs with effective Tick less than or equal to F, and every child State_Hash SHALL be unchanged by the parent's later Inputs and advancement (fork prefix immutability property).

### Requirement 16: Disruption injection

**User Story:** As a player, I want to inject crises such as a supplier failure, so that I can stress-test my business like a chaos experiment.

#### Acceptance Criteria

1. THE Simulation_Engine SHALL apply the Disruption_Script of the Scenario at the scripted onset Ticks in every Branch of the World.
2. WHEN a valid Disruption injection request names a Branch, a kind, a start Tick greater than the Head_Tick, a duration and a magnitude, THE Disruption_Injector SHALL append the Disruption to the Input_Log of that Branch.
3. IF a Disruption injection request has a magnitude or duration outside the Scenario's Disruption bounds, THEN THE Disruption_Injector SHALL reject the request with the allowed range.
4. WHEN a Disruption becomes active, THE Simulation_Engine SHALL create a Decision_Window at the onset Tick.
5. THE Simulation_Engine SHALL draw any random Disruption severity from the disruption Random_Stream, independent of Manager Commands.

### Requirement 17: Manager commands and validation

**User Story:** As a manager, I want a clear set of decisions with enforced rules, so that every decision is legal and has a calculable effect.

#### Acceptance Criteria

1. THE Command_Validator SHALL accept only Command kinds in the Command_Set.
2. WHEN a proposed Command is received, THE Command_Validator SHALL check parameter bounds, available Cash for committed spend, supplier availability as shown in the Observation, the existence of any referenced open purchase order and the remaining Command_Budget.
3. IF a proposed Command fails validation, THEN THE Command_Validator SHALL return a rejection with a machine-readable reason code and a human-readable message, and THE Input_Log SHALL record no Input for that Command.
4. FOR ALL proposed Commands from any Manager kind, THE Branch_Service SHALL route the Command through the Command_Validator before the Command reaches the Input_Log (validation gate property, INV-14).
5. THE Command_Validator SHALL produce identical accept or reject results for identical Observations and proposed Commands.
6. THE Command_Validator SHALL accept or reject a Command without modifying the Command's parameters.
7. WHEN several Commands are submitted in one Decision_Window, THE Command_Validator SHALL validate them one at a time in submission order, each against the Observation at that Tick plus the Cash committed and the Command_Budget consumed by the Commands already accepted in that Decision_Window.

### Requirement 18: Decision windows and manager parity

**User Story:** As an AI-evaluation researcher, I want human and AI managers to face identical constraints, so that a duel result reflects judgment and not unequal rules.

#### Acceptance Criteria

1. THE Simulation_Engine SHALL create Decision_Windows at every Tick that is a multiple of the Scenario cadence and at every Disruption onset Tick.
2. WHILE a Branch is at a Decision_Window, THE Branch_Service SHALL pause advancement of that Branch until the Manager ends the Decision_Window.
3. THE Branch_Service SHALL provide the same Observation fields to Human and AI Managers for the same Branch and Tick.
4. FOR ALL Duels, both Branches SHALL receive Decision_Windows at the same Ticks, the same Command_Budget, the same Command_Set and the same supplier lead-time Assumptions (parity property, INV-11).
5. FOR ALL Duels, both Branches SHALL receive identical Potential_Arrivals with identical random draws and identical Disruptions at every Tick, regardless of Manager Commands (exogenous fairness property, INV-12).
6. THE Observation SHALL exclude Disruption_Script entries with onset after the current Tick, future random draws and the Seed.
7. THE Branch_Service SHALL build the Observation at Tick N only from the State at Tick N, the Scenario Assumptions and the Inputs with effective Tick less than or equal to N, and SHALL list every Observation collection in a documented sort order (SKUs by identifier, orders by promised ship-by Sim_Time then order sequence, purchase orders by identifier, Disruptions by onset Tick then Input sequence).

### Requirement 19: Rule-based AI manager

**User Story:** As a player, I want an AI opponent that works offline, so that the duel runs without an LLM or API key and stays reproducible.

#### Acceptance Criteria

1. WHEN a Decision_Window opens on a Branch controlled by the Rule_Based_Manager, THE Rule_Based_Manager SHALL propose Commands computed from the Observation using reorder-point, safety-stock and capacity-gap rules.
2. THE Rule_Based_Manager SHALL produce identical proposals for identical Observations.
3. THE Rule_Based_Manager SHALL attach a rationale to each proposal that cites the Observation values used.
4. THE Rule_Based_Manager SHALL label each proposal as a rule-based recommendation.
5. THE Rule_Based_Manager SHALL operate with no network access and no LLM_Configuration (INV-13).
6. THE Input_Log SHALL record every accepted Rule_Based_Manager Command with Manager_Provenance `ai_rule_based`.

### Requirement 20: Optional LLM AI manager

**User Story:** As a player, I want an LLM-driven opponent from a provider I configure, so that I can compete against an agent that reasons in language while the rest of Parallel works without any API key.

#### Acceptance Criteria

1. WHERE LLM_Configuration is present, THE LLM_Manager SHALL send the Observation, the Command_Set schema and the remaining Command_Budget through the provider-independent OpenAI-compatible adapter to the chat completions endpoint at `LLM_BASE_URL` with model `LLM_MODEL`, and request proposals in strict JSON.
2. WHERE LLM_Configuration is present, THE Branch_Service SHALL pass every LLM_Manager proposal through the Command_Validator.
3. IF the LLM endpoint returns an error, returns output that is not strict JSON, returns JSON that violates the proposal schema of criterion 15 or exceeds 20 seconds of real time, THEN THE Branch_Service SHALL use the Rule_Based_Manager proposals for that Decision_Window.
4. WHEN the Branch_Service uses Rule_Based_Manager proposals because of an LLM failure, THE Input_Log SHALL record each resulting Command with Manager_Provenance `ai_rule_based_fallback` and the fallback reason.
5. WHERE LLM_Configuration is present, THE Input_Log SHALL record accepted LLM_Manager Commands with Manager_Provenance `ai_llm`, the model name and the rationale text.
6. WHEN a Branch containing LLM_Manager Commands is replayed, THE Simulation_Engine SHALL use the recorded Inputs and make no LLM request.
7. WHERE LLM_Configuration is present, THE LLM_Manager SHALL send at most one request per Decision_Window per Branch.
8. IF LLM_Configuration is absent, THEN THE Parallel_API SHALL report the LLM_Manager as unavailable in the list of AI Manager kinds and SHALL offer the Rule_Based_Manager.
9. IF LLM_Configuration is absent, THEN THE Branch_Service SHALL create, advance, fork, rewind, replay and duel Branches with the Rule_Based_Manager without error (INV-13).
10. THE Parallel_API SHALL read LLM_Configuration only from server environment variables and SHALL include no LLM credential in any response to the Web_App.
11. WHEN an AI Command or rationale is displayed, THE Web_App SHALL label the Command "Rule-based AI", "LLM AI (<model name>)" or "Rule-based AI (LLM fallback: <reason>)" according to its Manager_Provenance.
12. THE Branch_Tree SHALL label each AI-controlled Branch "Rule-based AI" or "LLM AI".
13. THE LLM_Manager SHALL take the model identifier only from `LLM_MODEL`, and Parallel SHALL contain no built-in default model identifier.
14. WHERE LLM_Configuration is present, IF a World has already made `LLM_MAX_CALLS_PER_WORLD` LLM requests when a Decision_Window opens on an LLM_Manager Branch of that World, THEN THE Branch_Service SHALL make no LLM request and SHALL use the Rule_Based_Manager proposals with LLM_Fallback_Reason `budget_exhausted`.
15. WHERE LLM_Configuration is present, THE LLM_Manager SHALL decode each LLM response against a strict proposal schema that rejects unknown fields, Command kinds outside the Command_Set, missing parameters and more proposals than the remaining Command_Budget, before any proposal reaches the Command_Validator.
16. WHERE LLM_Configuration is present, THE Parallel_API SHALL report for each World the number of LLM requests made and the remaining LLM_Call_Budget.
17. THE repository SHALL ship a `.env.example` that documents OpenRouter as the initial provider with `LLM_BASE_URL=https://openrouter.ai/api/v1`, an empty `LLM_API_KEY` placeholder and an empty `LLM_MODEL` placeholder.
18. THE Parallel_API SHALL report for every Decision_Window of an AI-controlled Branch the Manager_Provenance and, for `ai_rule_based_fallback`, the LLM_Fallback_Reason that produced the window's Commands, including Decision_Windows in which the AI Manager submitted no Command.

### Requirement 21: Duel mode and scoring

**User Story:** As a player, I want to compete head-to-head with the AI manager from the same moment, so that I can see who manages the crisis better.

#### Acceptance Criteria

1. WHEN a Duel creation request names a parent Branch, a fork Tick and an AI Manager kind (Rule-based AI or LLM AI), THE Duel_Service SHALL fork two sibling Branches at that Tick, one assigned to the Human Manager and one to the AI Manager.
2. WHEN a Duel is advanced, THE Duel_Service SHALL advance both Branches to the next Decision_Window Tick and pause both Branches there.
3. WHILE a Duel is at a Decision_Window, THE Duel_Service SHALL advance neither Branch until both Managers have ended the Decision_Window.
4. WHEN a Disruption is injected into a Duel, THE Duel_Service SHALL append the same Disruption with the same start Tick to both Branches.
5. THE Duel_Service SHALL compute each Branch's Composite_Score, Financial_Component, Fulfilment_Component and On_Time_Component as defined in the Glossary, using the Scenario's Score_Weights, Profit_Floor and Profit_Target.
6. WHEN a score is reported, THE Duel_Service SHALL report Net_Profit, Fill_Rate, Service_Level, cancelled orders and Backlog order count, and each weighted component, next to the Composite_Score.
7. WHEN both Duel Branches reach the Scenario horizon, THE Duel_Service SHALL declare the Branch with the higher Composite_Score the winner when the absolute score difference exceeds the Tie_Tolerance.
8. WHEN both Duel Branches reach the Scenario horizon and the absolute Composite_Score difference is less than or equal to the Tie_Tolerance, THE Duel_Service SHALL declare a draw.
9. THE Duel_Service SHALL determine every winner and draw only from Composite_Scores that the Metrics_Calculator computes from the two Branches' Ledgers and order records.
10. THE Metrics_Calculator SHALL compute the Composite_Score and its components with integer arithmetic and the shared half-to-even rounding function, without signed 64-bit overflow for all values within Assumption bounds.
11. FOR ALL Branches and Comparison_Ticks, each of Financial_Component, Fulfilment_Component and On_Time_Component SHALL lie between 0 and 10000, and the Composite_Score SHALL lie between 0 and 10000 (score bounds property).
12. FOR ALL Duels, the Composite_Score of each Branch SHALL equal the Composite_Score computed for that Branch alone from its own metrics and the Scenario, so that no Branch's score depends on the other Branch's results (competitor independence property).
13. FOR ALL pairs of metric sets that differ only by a higher Net_Profit, Fill_Rate or Service_Level, the Composite_Score of the higher set SHALL be greater than or equal to the Composite_Score of the lower set (score monotonicity property).
14. WHEN a Duel score is displayed, THE Web_App SHALL show the Composite_Score as points with two decimals (hundredths divided by 100), each weighted component, the Score_Weights and the raw Net_Profit, Fill_Rate, Service_Level, cancelled orders and Backlog of both Branches.
15. WHEN a Duel score is reported, THE Duel_Service SHALL report the Profit_Floor, Profit_Target and Tie_Tolerance used.

### Requirement 22: Comparison parity

**User Story:** As an AI-evaluation researcher, I want every comparison to use one horizon and one rule set, so that no comparison favors one side through timing or configuration.

#### Acceptance Criteria

1. THE Duel_Service SHALL compute both Branches' scores at the same Comparison_Tick.
2. WHILE either Duel Branch has a Head_Tick below the Scenario horizon, THE Duel_Service SHALL label the reported scores as provisional at the Comparison_Tick.
3. IF a comparison request names Branches of different Worlds, THEN THE Branch_Diff_Service SHALL reject the request with an incomparable-branches error.
4. IF the running Simulation_Engine version differs from the engine version recorded with a World, THEN THE Branch_Service SHALL attach an engine-version-mismatch warning to every State, score and diff served for that World.
5. FOR ALL Paired_Experiments, both Arms SHALL use the same Seed, Scenario, horizon, Decision_Window Ticks, Command_Budget and lead-time Assumptions for each run pair (ensemble parity property).
6. THE Ensemble_Runner SHALL record the Scenario hash, the engine version, the horizon and the Comparison_Tick in the header of every Ensemble_Report.
7. IF an Inputs_File records a Scenario hash or engine version that differs from the Scenario or Sim_CLI in use, THEN THE Ensemble_Runner SHALL reject the run with a mismatch error that names the differing field.

### Requirement 23: Branch diff

**User Story:** As a player, I want to see where two realities diverged and which decisions differed, so that I can study the consequences of choices without mistaking correlation for cause.

#### Acceptance Criteria

1. WHEN a diff request names two Branches of the same World, THE Branch_Diff_Service SHALL use as Comparison_Tick the requested Tick, or the lower of the two Head_Ticks when no Tick is requested.
2. IF the requested Comparison_Tick is greater than either Branch's Head_Tick, THEN THE Branch_Diff_Service SHALL reject the request with a not-yet-simulated error.
3. WHEN a diff is computed, THE Branch_Diff_Service SHALL report the First_Divergence_Tick, or report that the State_Hashes are equal at every Tick up to the Comparison_Tick.
4. WHEN a First_Divergence_Tick exists, THE Branch_Diff_Service SHALL report the names of the top-level State components and per-SKU entries whose canonical encodings differ at that Tick.
5. WHEN a First_Divergence_Tick exists, THE Branch_Diff_Service SHALL report, for each Branch, the first outcome-journal event within that Tick that differs between the Branches, with Sim_Time and event kind.
6. WHEN a diff is computed, THE Branch_Diff_Service SHALL report every Input present in one Branch and absent from the other, with Branch, effective Tick, sequence number, kind and payload.
7. WHEN a diff is computed, THE Branch_Diff_Service SHALL report Net_Profit, Service_Level and Backlog of each Branch at the Comparison_Tick, and the difference for each metric.
8. THE Branch_Diff_Service SHALL label every reported difference as an observed difference and include the statement "Observed differences do not establish cause. To attribute an effect to one decision, fork and change only that decision."
9. FOR ALL Branches A, diffing A with A SHALL report no First_Divergence_Tick, no differing Inputs and zero metric differences (identity property).
10. FOR ALL Branch pairs A and B, diffing B with A SHALL report the same First_Divergence_Tick and differing Inputs as diffing A with B, and negated metric differences (symmetry property).
11. FOR ALL Branch pairs whose differing Inputs set is empty, THE Branch_Diff_Service SHALL report no First_Divergence_Tick (input determinism property).
12. FOR ALL Branch pairs with a First_Divergence_Tick, the First_Divergence_Tick SHALL be greater than or equal to the lowest effective Tick among the differing Inputs (no-early-divergence property).
13. WHEN a diff is displayed, THE Diff_Panel SHALL show the First_Divergence_Tick as a link to the Timeline, the differing Inputs with Tick links, the metric differences and the causality statement from criterion 8.

### Requirement 24: Multi-seed ensemble runs

**User Story:** As an operations lead, I want to run a decision plan or policy across many seeds from the command line, so that I can tell skill from luck.

#### Acceptance Criteria

1. WHEN `parallel-sim ensemble` is invoked with a Scenario file, a Seed list and one Arm, THE Ensemble_Runner SHALL simulate the Arm to the horizon under each Seed and write an Ensemble_Report.
2. THE Ensemble_Runner SHALL accept a Seed list given either explicitly or as a base Seed and a count, with 1 to 50 Seeds.
3. IF a Seed list contains more than 50 Seeds or duplicate Seeds, THEN THE Ensemble_Runner SHALL reject the invocation before simulating any run.
4. WHERE an Arm is a Fixed_Replay, THE Ensemble_Runner SHALL apply each Inputs_File Command at the Command's recorded effective Tick under each Seed.
5. WHERE an Arm is a Fixed_Replay, THE Ensemble_Runner SHALL validate each Command with the Command_Validator against the Observation of that Seed's run at the Decision_Window preceding the Command's effective Tick.
6. IF a Fixed_Replay Command fails validation under a Seed, THEN THE Ensemble_Runner SHALL record a Skipped_Command with Seed, effective Tick, Command kind and reason code, and apply no Command in its place.
7. WHERE an Arm is a Fixed_Replay, THE Ensemble_Runner SHALL apply every Command that passes validation with the Command's recorded parameters unchanged.
8. WHERE an Arm is a Fixed_Replay, THE Ensemble_Runner SHALL apply every injected Disruption of the Inputs_File at the Disruption's recorded start Tick under each Seed.
9. WHERE an Arm is an Adaptive_Evaluation, THE Ensemble_Runner SHALL apply the Rule_Based_Manager's validated proposals at every Decision_Window at or after the Arm's start Tick.
10. WHERE an Adaptive_Evaluation names a prefix Inputs_File, THE Ensemble_Runner SHALL apply the prefix Inputs with effective Tick less than or equal to the Arm's start Tick as a Fixed_Replay.
11. IF an Arm names the LLM_Manager, THEN THE Ensemble_Runner SHALL reject the invocation with an unsupported-arm error.
12. THE Ensemble_Report SHALL list, for each run, the Seed, final State_Hash, Net_Profit, Contribution, Net_Cash_Flow, Service_Level, Fill_Rate, Backlog, cancelled orders, Composite_Score and Skipped_Command count.
13. THE Ensemble_Report SHALL list every Skipped_Command of every run.
14. THE Ensemble_Report SHALL list runs in ascending Seed order.
15. FOR ALL Branches exported as an Inputs_File, a Fixed_Replay under the Branch's original Seed SHALL report zero Skipped_Commands and the Branch's final State_Hash (replay round-trip property).
16. FOR ALL identical ensemble invocations, THE Ensemble_Runner SHALL write byte-identical Ensemble_Reports, regardless of the order or concurrency in which runs execute (ensemble determinism property).
17. THE Ensemble_Runner SHALL operate with no database, no network and no LLM configuration.

### Requirement 25: Paired human-versus-AI experiments

**User Story:** As an AI-evaluation researcher, I want to compare a human's plan and an AI policy under the same exogenous conditions across seeds, so that I can report outcome distributions and win rates instead of one lucky result.

#### Acceptance Criteria

1. WHEN `parallel-sim ensemble` is invoked with two Arms, THE Ensemble_Runner SHALL run both Arms under the same Seed for every Seed in the list (common random numbers).
2. THE Ensemble_Report SHALL label each Arm as "fixed plan (non-adaptive)" or "adaptive policy: rule-based", with the Arm's start Tick.
3. WHERE one Arm is a Fixed_Replay of Human Inputs and the other Arm is an Adaptive_Evaluation, THE Ensemble_Report SHALL state that the human Arm replays a recorded plan that cannot adapt to each Seed while the AI Arm decides per Seed.
4. THE Ensemble_Runner SHALL accept a Fixed_Replay of recorded AI Inputs as an Arm, so that a symmetric fixed-plan comparison can be run.
5. THE Ensemble_Report SHALL report, for each Arm and each metric listed in Requirement 24.12, the minimum, 10th percentile, median, mean, 90th percentile and maximum across Seeds.
6. THE Ensemble_Runner SHALL compute percentiles by the nearest-rank method over values sorted ascending, and the mean as the integer sum divided by the Seed count, rounded half to even.
7. THE Ensemble_Report SHALL report, for each Seed, the Composite_Score difference between the two Arms, and the statistics of criterion 5 over those differences.
8. THE Ensemble_Report SHALL report the counts of first-Arm wins, second-Arm wins and ties, where a tie is an absolute Composite_Score difference less than or equal to the Tie_Tolerance.
9. THE Ensemble_Runner SHALL use the Scenario's Tie_Tolerance unless a Tie_Tolerance in hundredths of a point is given on the command line, and SHALL record the Tie_Tolerance used in the Ensemble_Report header.
10. THE Ensemble_Runner SHALL write the Ensemble_Report as canonical JSON and as CSV with a fixed column order.
11. FOR ALL Paired_Experiments, first-Arm wins plus second-Arm wins plus ties SHALL equal the Seed count (count conservation property).
12. FOR ALL Paired_Experiments, swapping the two Arms SHALL swap the win counts, keep the tie count and negate every per-Seed difference (arm symmetry property).
13. FOR ALL Paired_Experiments with two identical Arms, THE Ensemble_Report SHALL report a tie for every Seed and zero differences (identity property).
14. THE Sim_CLI SHALL ship a committed Ensemble_Report for the recorded Flagship_Scenario demo duel over Seeds 1 to 20, and a golden test SHALL reproduce the report byte for byte.

### Requirement 26: API contract

**User Story:** As a frontend developer, I want a documented, versioned API, so that the Web_App and the backend evolve without guessing.

#### Acceptance Criteria

1. THE Parallel_API SHALL publish an OpenAPI 3.1 document describing every endpoint, request schema, response schema and error code under `/api/v1`.
2. THE Parallel_API SHALL expose operations to list Scenarios, list available AI Manager kinds, create a World, read a World with its Branch tree, read a World's effective Assumptions, fork a Branch, advance a Branch (with an optional Run_To_Tick), submit Commands, end a Decision_Window, inject a Disruption, read State and Observation at a Tick, read Tick_Frames for a Tick range, read Ledger_Entries for a Tick range, read the outcome journal for a Tick range, read a Branch's End_Of_Horizon_Statement, diff two Branches, export a Branch's Inputs as an Inputs_File, create a Duel, advance a Duel and read a Duel score.
3. IF a request fails, THEN THE Parallel_API SHALL respond with an `application/problem+json` body containing a type, title, status and detail, without stack traces.
4. IF a request body contains unknown fields, THEN THE Parallel_API SHALL reject the request with status 400.
5. IF a request body exceeds 256 KiB, THEN THE Parallel_API SHALL reject the request with status 413.
6. WHEN an advance request carries an expected Head_Tick that differs from the Branch's current Head_Tick, THE Parallel_API SHALL reject the request with status 409.
7. THE Parallel_API SHALL return money values as integer cents with an ISO 4217 currency code.
8. THE Parallel_API SHALL include the Disclaimer in every response that carries metrics, scores or diffs.

### Requirement 27: Progress stream

**User Story:** As a player, I want the world to update live as it runs, so that the simulation feels playable.

#### Acceptance Criteria

1. WHILE a Branch or Duel is advancing, THE Progress_Stream SHALL publish one event per completed Tick containing the Branch identifier, Tick, State_Hash and metrics.
2. THE Progress_Stream SHALL set each event identifier to the Tick number.
3. WHEN a client reconnects with a Last-Event-ID header, THE Progress_Stream SHALL resume from the Tick after the given identifier.
4. WHEN a Decision_Window opens or the horizon is reached, THE Progress_Stream SHALL publish a decision-window or completed event.
5. IF a client IP already holds 4 open Progress_Stream connections, THEN THE Progress_Stream SHALL reject the new connection with status 429.

### Requirement 28: Persistence and snapshots

**User Story:** As an operator, I want worlds stored durably, so that a demo survives a server restart.

#### Acceptance Criteria

1. THE Snapshot_Store SHALL persist Worlds, Branches, Inputs, Snapshots, State_Hashes per Tick and derived projections in PostgreSQL through parameterized pgx queries.
2. THE Snapshot_Store SHALL apply schema changes through versioned Goose migrations with up and down sections.
3. WHEN a Branch completes a Tick that is a multiple of the Scenario snapshot interval, THE Snapshot_Store SHALL store a Snapshot for that Tick.
4. WHEN the Parallel_API restarts, THE Branch_Service SHALL rebuild any Branch State from the Input_Log and Snapshots with State_Hashes equal to the stored State_Hashes.
5. THE Snapshot_Store SHALL enforce at the database level that rows in the Input_Log table reject UPDATE and DELETE statements.
6. WHEN a Snapshot is stored, THE Snapshot_Store SHALL store the canonical encoding of the complete State defined in the Glossary, including pending Event_Queue entries, the Event_Queue insertion sequence counter, every identifier counter and the full internal state of every Random_Stream.

### Requirement 29: Performance budgets

**User Story:** As a demo presenter, I want scrubbing, replay and experiments to feel instant, so that the demo flows within three minutes.

#### Acceptance Criteria

1. WHEN the Flagship_Scenario is replayed from Tick 0 to Tick 120 by the Sim_CLI on a 2023-or-newer developer laptop, THE Simulation_Engine SHALL complete within 500 milliseconds.
2. WHEN a Paired_Experiment of 50 Seeds on the Flagship_Scenario is run by the Sim_CLI on a 2023-or-newer developer laptop, THE Ensemble_Runner SHALL complete within 60 seconds.
3. WHEN a State read is requested for any computed Tick of a Flagship_Scenario Branch, THE Parallel_API SHALL respond within 300 milliseconds at the 95th percentile on the demo host.
4. WHEN a fork request is received, THE Branch_Service SHALL create the child Branch within 500 milliseconds on the demo host.
5. WHEN a diff request for two Flagship_Scenario Branches at the horizon is received, THE Branch_Diff_Service SHALL respond within 1 second on the demo host.
6. WHEN an advance request runs a Flagship_Scenario Branch from any Tick to the horizon with no open Decision_Window in between, THE Branch_Service SHALL complete the advance within 1 second on the demo host.
7. WHEN Tick_Frames for a range of up to 120 Ticks of a Flagship_Scenario Branch are requested, THE Parallel_API SHALL respond within 1 second on the demo host.
8. THE Benchmark_Report SHALL record, for each benchmark, the hardware model, CPU, memory, operating system, Go version, engine version, Scenario name with SKU count, horizon and event count per run, the Seeds, the number of warm-up and measured iterations, the reported statistics (median and 95th percentile) and the commit identifier measured.
9. THE Benchmark_Report SHALL contain, before any Requirement beyond the vertical slice is started, a measurement of criterion 1 and of 100 sequential flagship replays (2 runs for each of 50 Seeds) labelled as an early proxy for criterion 2, and SHALL contain a measurement of criterion 2 itself before Requirement 36 is started.
10. WHILE a budget in criteria 1 to 7 has no measurement in the Benchmark_Report, THE Benchmark_Report and every document that cites the budget SHALL label the budget "target, not yet measured".
11. IF a measurement misses a budget in criteria 1 to 7, THEN THE Benchmark_Report SHALL record the measured value and the shortfall, and every determinism, invariant and validation check SHALL remain enabled in the measured build.

Criteria 1 to 7 are targets until the Benchmark_Report records a measurement for them; results are never estimated or fabricated.

### Requirement 30: Headless CLI and golden demo

**User Story:** As a judge or developer, I want to reproduce results from the command line, so that I can verify determinism without the UI.

#### Acceptance Criteria

1. WHEN `parallel-sim run` is invoked with a Scenario file, a Seed and an optional Inputs_File, THE Sim_CLI SHALL simulate to the horizon and print the final State_Hash, Net_Profit in cents and Service_Level.
2. WHEN `parallel-sim verify` is invoked with a Scenario file, a Seed, an Inputs_File and an expected State_Hash, THE Sim_CLI SHALL exit with status 0 if the computed final State_Hash matches and status 1 otherwise.
3. IF the Inputs_File given to `parallel-sim verify` records an engine version different from the Sim_CLI engine version, THEN THE Sim_CLI SHALL print an engine-version-mismatch message and exit with status 2.
4. THE Sim_CLI SHALL operate with no database, no network and no LLM configuration.
5. THE Sim_CLI SHALL ship with a recorded Inputs_File for the Flagship_Scenario demo duel and the expected final State_Hash of each duel Branch.
6. THE Inputs_File_Printer SHALL encode a list of Inputs into an Inputs_File with a fixed field order.
7. FOR ALL valid lists of Inputs, parsing the output of the Inputs_File_Printer with the Inputs_File_Parser SHALL produce an equal list (round-trip property).
8. IF an Inputs_File contains an unknown field, a missing required field or a value of the wrong type, THEN THE Inputs_File_Parser SHALL return an error that names the JSON path of the first offending field.

### Requirement 31: Assumptions transparency and disclaimers

**User Story:** As a player, I want to see every assumption behind a result and be told that results are synthetic, so that I never mistake a simulation for a prediction.

#### Acceptance Criteria

1. WHEN a World's effective Assumptions are requested, THE Parallel_API SHALL return every Assumption with value, unit, default, minimum, maximum, rationale note and an overridden flag.
2. THE Web_App SHALL make the Assumptions_Panel reachable in one action from the World_View, the Duel view and the Diff_Panel.
3. THE Assumptions_Editor SHALL hide Disruption_Script entries until the user activates a "Reveal disruption script" control.
4. WHILE a Branch's Head_Tick is below the Scenario horizon, THE Web_App SHALL display only Disruption_Script entries with onset less than or equal to the displayed Tick.
5. THE Web_App SHALL display the Disclaimer on the World_View, the Duel view, the Ledger_Panel and the Diff_Panel.
6. THE Sim_CLI SHALL print the Disclaimer in the output of `parallel-sim run`, and THE Ensemble_Runner SHALL include the Disclaimer and the Seed count in every Ensemble_Report.

### Requirement 32: Web application views

**User Story:** As a player, I want a game-like view of my warehouse, timeline and realities, so that I understand compound effects at a glance and trust what I see.

#### Acceptance Criteria

1. WHEN a Tick is displayed, THE World_View SHALL render per-SKU stock, on-shift workers, the open order queue, inbound purchase orders and active Disruptions from the State returned by the Parallel_API for that Tick.
2. WHILE data for a requested Tick is loading, THE World_View SHALL display the previously loaded Tick dimmed with a syncing label.
3. THE Timeline SHALL display Tick marks for the Scenario horizon, Disruption markers, Decision_Window markers and fork points of the displayed Branch.
4. WHEN the user selects a Tick on the Timeline, THE Web_App SHALL request and display the State for that Tick without changing the Branch's Head_Tick.
5. THE Branch_Tree SHALL display every Branch of the World with label, Manager kind, fork Tick, Head_Tick and a distinct color and line pattern.
6. WHEN the user presses the fork control at a displayed Tick, THE Web_App SHALL send a fork request for the displayed Branch and Tick and display the new Branch in the Branch_Tree.
7. WHILE a Branch is at a Decision_Window, THE Decision_Panel SHALL display the Observation, the remaining Command_Budget and a form per Command kind.
8. WHEN the Command_Validator rejects a Command, THE Decision_Panel SHALL display the rejection message next to the rejected Command.
9. WHEN the user selects a money figure, THE Ledger_Panel SHALL display the Ledger_Entries whose lines sum to that figure.
10. WHILE a Duel is displayed, THE Web_App SHALL show both Branches' World_Views side by side with a shared Timeline and the score components from the Duel_Service.
11. WHEN an AI rationale is displayed, THE Web_App SHALL render the rationale as escaped plain text labelled "AI rationale (not verified)".
12. THE Web_App SHALL display only money and performance figures received from the Parallel_API.
13. THE Assumptions_Editor SHALL display every Assumption with unit, default, minimum, maximum and rationale note, and SHALL submit overrides with the World creation request.
14. WHEN the user selects two Branches in the Branch_Tree and requests a comparison, THE Web_App SHALL request a diff from the Parallel_API and display the result in the Diff_Panel.
15. WHERE the user has enabled reduced motion in the operating system, THE World_View SHALL replace interpolated motion with instant state changes.

### Requirement 33: Playback, stepping and automatic pausing

**User Story:** As a demo presenter, I want to play the 120-hour scenario fast, step through it by hand and have it stop where decisions matter, so that I can tell the story at the audience's pace without changing any result.

#### Acceptance Criteria

1. THE Playback_Controls SHALL offer pause, play at 1x, 4x and 16x, step forward one Tick, step back one Tick, fast-forward to the next Decision_Window and fast-forward to the next Disruption onset.
2. WHILE playback runs at 1x, 4x or 16x, THE Web_App SHALL advance the displayed Tick at 2, 8 or 32 Ticks per second of real time respectively.
3. WHILE playback runs, THE Web_App SHALL display a Tick only after receiving the authoritative Tick_Frame for that Tick from the Parallel_API.
4. WHILE playback runs and the Tick_Frame for the next Tick has not been received, THE Web_App SHALL hold the displayed Tick and show the syncing label.
5. WHILE the Auto_Pause_Mode is "every decision window", WHEN playback reaches a Decision_Window Tick, THE Web_App SHALL pause playback at that Tick and open the Decision_Panel.
6. WHILE the Auto_Pause_Mode is "disruption onsets only", WHEN playback reaches the onset Tick of a Disruption, THE Web_App SHALL pause playback at that Tick and open the Decision_Panel.
7. WHEN an advance request carries a Run_To_Tick, THE Branch_Service SHALL advance the Branch to the Run_To_Tick or to the next Disruption-onset Decision_Window, whichever is earlier, ending every intervening Human Decision_Window with no Commands.
8. WHEN an advance with a Run_To_Tick reaches a Decision_Window of an AI-controlled Branch, THE Branch_Service SHALL apply the AI Manager's validated proposals at that Decision_Window.
9. WHEN a Duel advance request carries a Run_To_Tick, THE Duel_Service SHALL advance both Duel Branches in lockstep under criteria 7 and 8.
10. FOR ALL Branches and Input_Logs, the State_Hash at every Tick SHALL be identical regardless of playback speed, Auto_Pause_Mode, stepping, fast-forward, Run_To_Tick use and the number of Ticks advanced per advance request (presentation independence property).
11. WHEN playback reaches the Head_Tick of a Branch that is below the horizon and not at an open Decision_Window, THE Web_App SHALL request advancement of that Branch.
12. WHEN the user steps back or selects an earlier Tick during playback, THE Web_App SHALL pause playback and display the selected Tick without changing the Branch's Head_Tick.
13. THE Playback_Controls SHALL bind Space to play and pause, Left and Right to step one Tick, Shift+Left and Shift+Right to step one Decision_Window, and Shift+D to fast-forward to the next Disruption onset.
14. WHILE a Duel is displayed, THE Playback_Controls SHALL display the same Tick in both Branches' World_Views.
15. THE Web_App SHALL set the Auto_Pause_Mode to "every decision window" when a World is opened.
16. WHEN the user activates fast-forward to the next Disruption onset, THE Web_App SHALL pause playback and display the lowest computed Disruption onset Tick after the displayed Tick, or, when no such Tick has been computed, send an advance request with Run_To_Tick equal to the horizon and display the Tick at which that advance stops.
17. WHEN the user activates fast-forward to the next Decision_Window, THE Web_App SHALL pause playback and display the lowest computed Decision_Window Tick after the displayed Tick, or, when no such Tick has been computed and the Branch has no open Decision_Window, send an advance request without a Run_To_Tick and display the Tick at which that advance stops.

### Requirement 34: 60-second crisis and fork demonstration

**User Story:** As a demo presenter, I want the flagship crisis and a reality fork to fit in 60 seconds, so that the core idea lands early in a video under three minutes.

#### Acceptance Criteria

1. THE Web_App SHALL support completing the Demo_Path within 60 seconds of real time on the demo host, verified by an automated end-to-end test that waits 2 seconds before each user action.
2. THE Web_App SHALL offer a "Start Black Friday demo" control that creates a Flagship_Scenario World with the default Seed in one action.
3. FOR ALL runs of the Demo_Path with the same Seed, Demo_Fork_Tick and Demo_Commands, the forked Branch SHALL reach the same State_Hash at the horizon (demo reproducibility property).
4. WHEN both Branches of the Demo_Path reach the horizon, THE HUD SHALL show the forked Branch's and the parent Branch's Net_Profit, Service_Level and Backlog with the differences.
5. THE Demo_Path SHALL run with no LLM_Configuration.
6. THE Demo_Path test suite SHALL compute the Crisis_Tick and the Demo_Fork_Tick from the no-Command run with the default Seed, and the Demo_Path SHALL fork at that computed Demo_Fork_Tick.
7. THE Demo_Path SHALL submit the Demo_Commands in the forked Branch, and each Demo_Command SHALL pass the Command_Validator at the Demo_Fork_Tick.
8. THE Demo_Path end-to-end test SHALL assert the demo reproducibility of criterion 3 and that the HUD shows figures received from the Parallel_API, and SHALL assert no specific Net_Profit value, no sign of any metric difference and no Duel winner.
9. IF a change to the Flagship_Scenario or the engine version changes the computed Demo_Fork_Tick or Demo_Commands, THEN THE Calibration_Report SHALL record the previous and new values with the reason, and the demo script in `docs/` SHALL be updated to the new values.

### Requirement 35: Operational visualization

**User Story:** As a player, I want the studio to show where the operation is stuck, what each intervention did and where realities split, so that I can read compound effects without opening tables.

#### Acceptance Criteria

1. WHEN a Tick is displayed, THE World_View SHALL mark as stock-bound every SKU for which the Fulfilment_Constraint_Report lists orders left unshipped because of insufficient inventory, with the unshipped order and unit counts.
2. WHEN a Tick is displayed, THE World_View SHALL mark the pick-and-pack line as capacity-bound when the Fulfilment_Constraint_Report lists orders left unshipped because capacity was exhausted, with the unshipped order and unit counts.
3. WHEN an Input takes effect at the displayed Tick, THE World_View SHALL show an intervention marker at the affected zone with the Command or Disruption kind and the Manager_Provenance label.
4. THE Timeline SHALL display a marker at the effective Tick of every Input of the displayed Branch, distinguished by Manager_Provenance.
5. WHILE a forked Branch is displayed at a Tick that its parent Branch has computed, THE HUD SHALL show the parent Branch's Net_Profit, Service_Level and Backlog at that Tick next to the displayed Branch's values, with the differences.
6. WHEN a Disruption is active at the displayed Tick, THE World_View SHALL highlight the affected zone and show the Disruption kind and the Ticks elapsed since onset.
7. WHEN the Branch_Diff_Service has reported a First_Divergence_Tick between the displayed Branch and its parent, THE Timeline SHALL mark that Tick as the divergence point.
8. THE World_View SHALL provide an accessible text summary of the displayed Tick that lists bottlenecks, active Disruptions and interventions.
9. THE World_View SHALL derive every bottleneck, intervention and divergence marker from Tick_Frames, Inputs or diffs returned by the Parallel_API.

### Requirement 36: Flagship scenario calibration

**User Story:** As a judge, I want the flagship scenario calibrated transparently on synthetic assumptions, so that the crisis is meaningful and no outcome is rigged.

Calibration starts only after the simulation and financial modules (Requirements 5 to 12 and 37) are complete.

#### Acceptance Criteria

1. WHEN the Flagship_Scenario is run to the horizon with the default Seed and no Commands, THE Simulation_Engine SHALL produce at least one Tick at which on-hand inventory of at least one SKU equals zero while that SKU has open orders.
2. WHEN the Flagship_Scenario is run to the horizon with the default Seed, THE Rule_Based_Manager run SHALL achieve a higher Composite_Score than the run with no Commands (sanity check against doing nothing).
3. THE Flagship_Scenario SHALL express every business parameter used by the Simulation_Engine as an Assumption or a Disruption_Script entry visible through the Assumptions_Panel.
4. THE Calibration_Report SHALL list each Flagship_Scenario Assumption with value, unit, bounds and rationale note, the calibration gates of criteria 1 and 2, the Seeds used and the gate results.
5. THE Calibration_Report SHALL record every change to a Flagship_Scenario Assumption made during calibration with the previous value, the new value and the reason.
6. THE Calibration_Report SHALL define calibration gates only on the no-Command run and the Rule_Based_Manager run, with no gate on the relative outcome of a Human Manager and an AI Manager.
7. THE Calibration_Report SHALL report the Composite_Scores of the no-Command run, the Rule_Based_Manager run and the recorded human demo plan over Seeds 1 to 20 as observed results without a target outcome.
8. THE Simulation_Engine SHALL compute every State transition without reading the Manager kind of any Branch or the Manager_Provenance of any Input (manager-blind engine, verified by criterion 12.4).
9. THE Calibration_Report SHALL carry the Disclaimer.
10. THE Calibration_Report SHALL publish the Crisis_Tick, the Demo_Fork_Tick, the Demo_Commands with their parameters, the reason for each choice and the Profit_Floor and Profit_Target with their rationale, and SHALL state that no Demo_Path or Duel outcome is a calibration target.

### Requirement 37: Financial policies and end-of-horizon treatment

**User Story:** As a founder, I want a short, explicit set of accounting policies for the simulated business, so that profit, cash and inventory mean the same thing in every Branch without a general-purpose accounting platform.

#### Acceptance Criteria

1. THE Ledger SHALL use only the accounts listed in the Glossary definition of Ledger, with no user-defined accounts, receivables, payables, accruals, tax, interest or depreciation.
2. THE Ledger SHALL recognise revenue and capture customer payment in the same Ledger_Entry at the Sim_Time an order ships.
3. WHEN an open order is cancelled, THE Simulation_Engine SHALL record the cancellation in the outcome journal with no Ledger_Entry and no refund, because the order was never paid and no revenue was recognised.
4. WHEN a late order ships, THE Ledger SHALL record the service credit of criterion 10.11 as Service_Credit_Expense and leave the order's recognised Revenue unchanged.
5. THE Ledger SHALL value each SKU's on-hand inventory at Moving_Average_Cost through the SKU's Inventory_Value.
6. WHEN a purchase order receipt occurs, THE Ledger SHALL increase the SKU's Inventory_Value by the Supplier_Unit_Cost times the received quantity.
7. WHEN q units of a SKU ship while u units are on hand and q is less than u, THE Ledger SHALL compute Cost_Of_Goods_Sold as Inventory_Value × q / u, rounded half to even with the shared rounding function, and reduce Inventory_Value by that amount.
8. WHEN q units of a SKU ship while exactly q units are on hand, THE Ledger SHALL compute Cost_Of_Goods_Sold as the full remaining Inventory_Value and set Inventory_Value to zero.
9. THE Simulation_Engine SHALL compute Inventory_Value × q / u exactly, without signed 64-bit overflow, for all values within Assumption bounds.
10. FOR ALL Branches and Ticks, the Inventory account balance SHALL equal the sum of per-SKU Inventory_Values (valuation consistency property).
11. FOR ALL Branches, Ticks and SKUs, Inventory_Value SHALL equal zero whenever on-hand units equal zero (no residual value property).
12. WHEN a Tick is processed, THE Ledger SHALL record regular wages as on-shift regular workers times the regular hourly wage Assumption, paid in Cash in that Tick.
13. WHEN a Tick is processed, THE Ledger SHALL record overtime wages as the overtime hours scheduled for that Tick times the overtime hourly wage Assumption, paid in Cash in that Tick.
14. WHEN a Tick is processed, THE Ledger SHALL record temporary wages as contracted temporary workers, including workers still in onboarding, times the temporary hourly wage Assumption, paid in Cash in that Tick.
15. WHEN a Branch reaches the horizon, THE Simulation_Engine SHALL stop processing events at Sim_Time horizon × 60 and simulate no later period.
16. WHEN a Branch reaches the horizon, THE Metrics_Calculator SHALL produce the End_Of_Horizon_Statement for that Branch.
17. THE End_Of_Horizon_Statement SHALL report open orders as Backlog and Late_Backlog, with no revenue, cancellation or service credit recorded for them.
18. THE End_Of_Horizon_Statement SHALL report open purchase orders as commitments with units and committed cost at Supplier_Unit_Cost, with no Ledger_Entry recorded for them.
19. THE End_Of_Horizon_Statement SHALL report remaining inventory at Inventory_Value, with no write-down or liquidation.
20. THE End_Of_Horizon_Statement SHALL report accrued unpaid wages as zero, because wages are paid in the Tick in which they are earned.
21. FOR ALL Branches and Ticks, the Cash balance plus the Inventory balance SHALL equal the Opening_Equity balance plus Net_Profit (balance identity property).

## Priority Polish

Requirement 38 is built only after every MVP acceptance test passes, and before any stretch requirement. The animations may be implemented with CSS transitions, the PixiJS ticker or GSAP.

### Requirement 38: [Polish] Cinematic fork transition and duel result reveal

**User Story:** As a demo presenter, I want polished transitions for forks and duel results, so that the video lands the key moments.

#### Acceptance Criteria

1. WHEN a fork is created, THE Web_App SHALL animate the new Branch splitting from the parent in the Branch_Tree within 600 milliseconds.
2. WHEN a Duel reaches the horizon, THE Web_App SHALL reveal the scoreboard with an animation that completes within 1200 milliseconds.
3. THE Web_App SHALL animate only the position, opacity and scale of fork and scoreboard elements, and SHALL display every number at the final value received from the Parallel_API.
4. WHERE the user has enabled reduced motion in the operating system, THE Web_App SHALL show the fork and the scoreboard in their final state without animation.

## Stretch Goals

The following requirements are out of the MVP and are built only after Requirement 38 is done.

### Requirement 39: [Stretch] Large ensembles and distribution views

**User Story:** As an operations lead, I want larger experiments with visual distributions, so that I can study tail risk.

#### Acceptance Criteria

1. WHERE large ensembles are enabled, THE Ensemble_Runner SHALL accept Seed lists of up to 1000 Seeds.
2. WHERE large ensembles are enabled, THE Web_App SHALL display a histogram per metric and a paired-difference chart from an Ensemble_Report.
3. FOR ALL large ensemble invocations with identical arguments, THE Ensemble_Runner SHALL write byte-identical Ensemble_Reports.

### Requirement 40: [Stretch] Per-tick branch diff visualization

**User Story:** As a player, I want to see differences between two realities over time, so that I can see when gaps opened and closed.

#### Acceptance Criteria

1. WHEN a per-tick diff is requested for two Branches of the same World, THE Branch_Diff_Service SHALL return Net_Profit, Service_Level and Backlog of both Branches for every Tick up to the Comparison_Tick.
2. WHEN a per-tick diff is displayed, THE Diff_Panel SHALL chart the per-Tick differences with Input markers and the causality statement from Requirement 23.8.

### Requirement 41: [Stretch] Optimization-based staffing advisor

**User Story:** As a manager, I want an operations-research optimizer to recommend staffing, so that I can compare my plan against a plan that is optimal for a stated model.

#### Acceptance Criteria

1. WHEN a staffing recommendation is requested at a Decision_Window, THE Staffing_Advisor SHALL compute the overtime and temporary worker plan that minimizes forecast labor cost plus forecast service credits over the next 24 Ticks or until the horizon, whichever is earlier.
2. THE Staffing_Advisor SHALL restrict the plan to the maximum overtime per worker, the onboarding delay, the maximum temporary workers, the remaining Command_Budget and the available Cash shown in the Observation.
3. THE Staffing_Advisor SHALL compute the forecast only from the Observation, using the Scenario's published demand profile and the observed history.
4. FOR ALL pairs of States with identical Observations, THE Staffing_Advisor SHALL return identical recommendations (no-privileged-information property).
5. THE Staffing_Advisor SHALL label each recommendation "optimal for the advisor's forecast model and stated constraints", distinct from the Rule_Based_Manager's "rule-based" label.
6. IF the Staffing_Advisor's search ends before proving optimality, THEN THE Staffing_Advisor SHALL label the recommendation "best found, not proven optimal".
7. THE Staffing_Advisor SHALL return the recommendation as proposed Commands that pass the Command_Validator.
8. FOR ALL Observations, the forecast objective of a recommendation labelled optimal SHALL be less than or equal to the forecast objective of the Rule_Based_Manager's staffing proposals for the same Observation (optimality check property).

### Requirement 42: [Stretch] Additional disruption kinds

**User Story:** As a player, I want more crisis types, so that I can rehearse more failure modes.

#### Acceptance Criteria

1. WHERE the carrier_delay Disruption kind is enabled in the Scenario, THE Simulation_Engine SHALL add the Disruption magnitude in Ticks to the ship time of every order shipped while the Disruption is active.
2. WHERE the payment_outage Disruption kind is enabled in the Scenario, THE Simulation_Engine SHALL reduce order conversion by the Disruption magnitude in basis points while the Disruption is active.

### Requirement 43: [Stretch] Shareable read-only replays

**User Story:** As a player, I want to share a finished duel, so that others can watch and verify it without changing it.

#### Acceptance Criteria

1. WHEN a share request is received for a World, THE Parallel_API SHALL return a Read_Only_Link generated as a random UUIDv4 distinct from the World identifier.
2. WHILE a World is accessed through a Read_Only_Link, THE Parallel_API SHALL reject every mutation request with status 403.
3. THE Parallel_API SHALL enforce the Read_Only_Link check on the server for every operation that appends Inputs, forks or advances Branches, ends a Decision_Window, injects a Disruption, creates or advances a Duel, or creates a share.
4. FOR ALL mutation operations listed in the OpenAPI document, a request authorized by a Read_Only_Link SHALL be rejected with status 403 (read-only enforcement property).
5. WHILE a World is accessed through a Read_Only_Link, THE Parallel_API SHALL omit the World identifier from every response.

## Property-test candidates

Property tests use `pgregory.net/rapid` and are named `TestProp*`.

| Property | Criteria | Invariant |
|---|---|---|
| Scenario round trip and print idempotence | 2.2, 2.3 | |
| Inputs_File round trip | 30.7 | |
| State canonical encoding round trip | 12.5 | |
| Determinism at every Tick | 6.3 | INV-1 |
| Hash metadata independence | 12.4 | INV-15 |
| Replay equivalence from Snapshots | 14.3 | INV-2 |
| Snapshot completeness (Event_Queue, counters, Random_Stream state) | 14.6, 28.6 | INV-2 |
| Fork fidelity | 15.3 | INV-5 |
| Fork independence | 15.4 | INV-4 |
| Fork prefix immutability after parent advances | 15.8 | INV-3, INV-4 |
| Order conservation | 7.6 | INV-9 |
| Inventory conservation and non-negative stock | 8.6, 8.7 | INV-8 |
| Ledger balance | 10.14 | INV-6 |
| Cash reconciliation | 10.15 | INV-7 |
| Profit consistency | 11.7 | INV-10 |
| Accrual-to-cash reconciliation | 11.8 | INV-10 |
| Valuation consistency and no residual value (Moving_Average_Cost) | 37.10, 37.11 | INV-19 |
| Balance identity (Cash + Inventory = Opening_Equity + Net_Profit) | 37.21 | INV-19 |
| Demand draw count independent of Commands | 7.2 | INV-12 |
| Duel parity and exogenous fairness | 18.4, 18.5 | INV-11, INV-12 |
| Validation gate and validator determinism | 17.4, 17.5, 17.6, 17.7 | INV-14 |
| Input idempotency key | 13.4 | INV-3 |
| Rule-based manager determinism | 19.2 | |
| Presentation independence (playback speed, stepping, fast-forward, Run_To_Tick, advance granularity) | 33.10 | INV-18 |
| Demo reproducibility | 34.3 | INV-1 |
| Composite_Score bounds, competitor independence and monotonicity | 21.11 to 21.13 | INV-17 |
| Same-Sim_Time event ordering (Commands, expiries, activations, other events) | 5.7, 5.8 | INV-1 |
| Diff identity, symmetry, input determinism, no early divergence | 23.9 to 23.12 | |
| Fixed replay round trip under original Seed | 24.15 | INV-16 |
| Ensemble determinism under any execution order | 24.16 | INV-16 |
| Ensemble parity | 22.5 | INV-17 |
| Win/tie count conservation, arm symmetry, identity | 25.11 to 25.13 | |
| [Stretch] Advisor no-privileged-information and optimality check | 41.4, 41.8 | |
| [Stretch] Read-only enforcement on every mutation | 43.4 | |

Example and golden tests: Flagship_Scenario calibration gates (36.1, 36.2), manager-blind engine (36.8 via 12.4), Moving_Average_Cost rounding and last-unit examples (37.7, 37.8), End_Of_Horizon_Statement example (37.16 to 37.20), golden State_Hash on amd64 and arm64 (6.4), golden Ensemble_Report (25.14), LLM independence and absent API key (19.5, 20.8, 20.9, 24.17, 30.4, 34.5, INV-13), Manager_Provenance recording including fallback (19.6, 20.4, 20.5), Input_Log UPDATE/DELETE rejection (28.5, INV-3), 60-second Demo_Path end-to-end timing (34.1, Playwright), Score_Weights and Profit bounds validation (1.8, 1.9), worked Composite_Score examples at the clamp edges and a tie at exactly 10 hundredths (21.10, 21.7, 21.8), temporary worker onboarding with wages and zero capacity (9.8, 37.14), LLM call budget exhaustion, schema violation and timeout fallbacks with a stub provider (20.3, 20.14, 20.15), no built-in model identifier (20.13), Demo_Fork_Tick and Demo_Commands derivation with no outcome assertion (34.6 to 34.8), Benchmark_Report completeness review (29.8 to 29.11).
