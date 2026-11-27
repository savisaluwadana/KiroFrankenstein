
# Parallel studio: UX and visual redesign (Phase A design)

Status: design only. Nothing in `web/` or `backend/` changed in Phase A.
Scope: the flagship "Black Friday Meltdown" experience. No stretch features.
Hard constraints carried over: no backend, `openapi.yaml`, financial or test-semantics changes in this redesign; the UI never computes simulation results; every number on screen is a server value or a labelled display subtraction/estimate of server values; the hidden disruption script is never revealed to a manager.

## 1. Review of the current studio

### 1.1 Problems (what makes it feel like a developer console)

1. **Entry.** `app/page.tsx` is one paragraph of prose plus "Start Black Friday demo". No story, no briefing, no way to change the business. Words like "deterministic engine" and "fork" appear in the first sentence.
2. **Header and HUD.** `Hud.tsx` shows `Sim clock  Thursday 19:00 · tick 19`, uppercase 10 px labels ("Service level", "Backlog 12 orders / 40 units") and a parent-comparison block labelled "difference vs parent (display)". A tick counter is meaningless to a business owner.
3. **Three-column tool layout** (`StudioPage.tsx`: 18rem branch rail, canvas, 24rem context panel). It reads as an IDE: a React Flow graph in a small rail, a "Fork label" text box, a button "Fork at tick 20".
4. **Decision panel** (`DecisionPanel.tsx`): "Command budget left", a per-SKU table, a form per command with SKU dropdowns, role selects ("primary"/"backup") and a raw quantity field. Disruptions print as `supplier_outage (primary) on speaker, ticks 19 to 31`. Rejections show server messages such as `sku 2 ... role 1`.
5. **Raw identifiers everywhere.** `describeCommand` produces `place_purchase_order, SKU 2, 315 units, backup`. The summary says "Stock-bound: SKU 2". Assumptions list dotted paths with cents and basis points.
6. **Information leak.** `PlanHint` prints the plan's `reasons`, which state the crisis tick, the stock-bound unit count at a future tick and the expected demand. For a manager that is the hidden script by another route. The decision panel also prints a disruption's `endTick`.
7. **Scene.** `WorldScene.ts` draws labelled rectangles (INBOUND DOCK, STORAGE RACKS) with circles for workers and queue items on a fixed 960x540 logical canvas, a per-SKU bar fill, and no trucks, boxes or movement. It is correct but not legible as a warehouse.
8. **Session-only interventions.** Accepted commands live in component state (`accepted`), so the timeline and scene lose intervention markers after reload, and a forked child never shows the commands it inherited. The footer admits this ("inputs shown are those submitted in this session").
9. **Missing pieces the spec already asks for.** No ledger drill-down UI (`features/ledger` is empty and `Hud.onMoneyClick` is never wired), no comparison view, no assumptions editing, no onboarding, no mobile layout.
10. **Playback vocabulary.** "1x/4x/16x", "step one tick", "fast-forward to next disruption", Shift+D. Correct, but the labels are engine terms.
11. **Palette.** Night Shift Operations (blue-black, cyan AI, red/green signals) is a dashboard look with a thin hierarchy; every panel has the same 1 px border and weight.
12. **Disclaimer repetition.** The same sentence prints in the HUD, under the canvas and in assumptions. It should appear once per view, quietly.

What is worth keeping: the stores (`studio.ts`, `playback.ts`), `client.ts`, frame loading (`ensureFrames`), the pure `worldViewModel.ts` pattern, keyboard bindings, the Timeline logic, the existing tests. The redesign re-skins and re-composes; it does not touch these contracts.

### 1.2 What the backend exposes (truthfully visualizable)

All paths under `/api/v1`. Sources: `backend/api/openapi.yaml`, `backend/scenarios/black-friday.json`, `web/e2e/fixtures/demo-plan.json`.

| Endpoint | Data | Use |
|---|---|---|
| `POST /demo` | World, root branch, `DemoPlan` (`crisisTick`, `forkTick`, `commands[{skuId, role, qty}]`, `reasons`) | Demo path; suggestion card |
| `POST /worlds` | `name`, `seed`, optional `scenario`, **`overrides`** (dotted assumption path to integer, bounds enforced) | Business configuration before creation |
| `GET /worlds/{id}` | `scenario` summary (name, `horizonTicks` 120, `startClock` "Thu 00:00", `decisionCadenceTicks` 4, `commandBudget` 5, `skus[{index,id,referencePriceCents,openingUnits,promiseTicks}]`), `assumptions[{path,value,unit,default,min,max,rationale,overridden}]`. No disruption script. | Product names, sliders, bounds |
| `GET /branches/{id}/tick-frames?from&to` (max 240) | `TickFrame`: per SKU `onHand`, `inboundPOs`, `inboundUnits`; `workersOnShift`, `tempsOnShift`, `tempsOnboarding`; `orderQueueLen`; `disruptions[{kind, origin, target(SKU or -1), magnitudeBps, onsetTick, endTick, ticksSinceOnset}]`; `report{capacity, capacityUsed, shippedOrders, skus[{stockBoundOrders/Units, capacityBoundOrders/Units}]}`; `markers[decision_window\|disruption_onset]`; full `metrics`; `stateHash` | The scene, HUD, timeline, crisis cards |
| `GET /branches/{id}/state?tick` | `Observation` (any tick, a read): `cashCents`; per SKU `id`, `onHand`, `priceCents`, `referencePriceCents`, `marketingCents`, `inboundPOs/Units`, `suppliers[{role, unitCostCents, leadTimeTicks, available, availableFromTick}]`; `openOrders[{id, sku, qty, promisedBy}]`; `purchaseOrders[{id, sku, supplier, qty, unitCostCents, dueAt, expedited}]`; `workforce{regular, overtimeHours, workersOnShift, tempsOnShift, tempsOnboarding}`; `disruptions[...supplierRole]`; `window{open, reasons, cadenceTicks, commandBudget, horizonTicks, ticksRemaining}` | Decision cards, quotes, paused-state detail |
| `GET /branches/{id}/metrics?tick` | `Metrics` (all integers): revenue, COGS, freight, variable cost, contribution, labor, marketing, overhead, service credits, net profit, net cash flow, cash, inventory value, `ordersDue`, `ordersDueOnTime`, `unitsDue`, `unitsDueShipped`, `serviceLevelBps`, `fillRateBps`, `backlogOrders/Units`, `lateBacklogOrders/Units`, `reconciliation` | HUD, comparison |
| `GET /branches/{id}/ledger?from&to&account&limit&offset` | Balanced entries: `tick`, `simMinute`, `description`, `cause{kind: opening/shipment/receipt/freight/labor/marketing/overhead/service_credit, ref}`, `lines[{account, debit, credit}]` | "Where does this number come from", Engineering Mode |
| `POST /branches/{id}/fork` | Child branch at `atTick`, window open there | Explore another future |
| `POST /branches/{id}/advance`, `/commands`, `/decision-window/end` | Playback, decisions | Existing |

**Important implementation facts.**
- Only two commands are implemented by the validator: `set_price` and `place_purchase_order`. `expedite_purchase_order`, `schedule_overtime`, `hire_temp_workers` and `set_marketing_spend` return `not_implemented`. The redesign therefore offers no staffing, overtime, marketing or expedite action (omitted, not disabled). The demo plan already contains only a purchase order.
- There is no duel, diff, share or ensemble endpoint, and no endpoint that lists inputs. The rule-based AI manager is a stub package. Human-vs-AI therefore has no UI (section 3.9).
- Per-order and per-shipment records exist only in the ledger (`cause.kind = shipment`, `ref` = order id, `simMinute`) and in `Observation.openOrders`. Ledger shipment entries carry money lines only (no SKU, no units). Receipts carry the purchase-order id only.

### 1.3 Animation data sources: what is derivable and what is not

Rule: an animation exists only if a specific authoritative field or the delta between two consecutive authoritative frames (t-1, t) of the same branch drives it. Playback speed never changes what is derived.

| Visual event | Authoritative source | Exact? |
|---|---|---|
| Stock level of a product | `skus[s].onHand` at t | Exact |
| Orders shipped this hour | `report.shippedOrders` (count) and `report.capacityUsed` (units) at t | Exact |
| Van departures | `report.shippedOrders` at t: vans = ceil(shippedOrders / ORDERS_PER_VAN) with `ORDERS_PER_VAN = 10`, capped (a van is a display bundle of orders; its label states the exact orders it carries) | Exact count, display grouping |
| Per-product boxes leaving a rack | onHand(t-1) - onHand(t) for product s, only when product s has no inbound change at t (`inboundPOs` and `inboundUnits` equal at t-1 and t) | Exact in that case |
| Delivery trucks | `inboundPOs[s]` decreases between t-1 and t | Lower bound: a receipt in the same hour as a new purchase order is not detectable from frames (placements take effect at tick 1 mod 4 and a 24-hour lead lands on the same boundary, so this happens). Not animated when ambiguous; the dock counter still updates |
| Boxes moving dock to rack | Same condition as a truck | Same |
| Orders waiting | `orderQueueLen` at t | Exact |
| New orders arriving | `max(0, orderQueueLen(t) - orderQueueLen(t-1) + shippedOrders(t))`. Cancellations are not exposed, so this is a lower bound and is described as "at least" | Lower bound |
| Waiting for inventory / for packing capacity | `report.skus[].stockBoundOrders`, `capacityBoundOrders`, `capacity`, `capacityUsed` | Exact |
| Workers on the floor | `workersOnShift`, `tempsOnShift`, `tempsOnboarding`; absent regulars = assumption `workforce.regularWorkers` - `workersOnShift` (assumption is constant: no staffing command is implemented) | Exact |
| Disruption highlight and cause text | `disruptions[]` at t (kind, target SKU, `ticksSinceOnset`); supplier role and `availableFromTick` from `Observation` | Exact |
| Intervention marker (order placed) | `Observation.purchaseOrders` new id between two observations, or an accepted command in this session | See R3 |

**Not exposed, therefore never shown:** the disruption script and any future onset or end (except a supplier's `availableFromTick`, which the Observation gives managers), the seed, random draws, individual customers, per-order product mix of the queue (only SKUs that appear in the constraint report), cancellations, worker identities, trucks that are in transit before arrival, any forecast.

**Never animated without data:** deliveries, vans, customers, queue growth, staffing changes, repairs, supplier "recovery". Idle ambience (a worker's walking cycle, a slow cloud shadow) is allowed only if it is not read as an event: it never moves objects between zones and never changes a count.

### 1.4 Candidate additive read-only fields (none added in Phase A)

| ID | Addition | Why | Verdict |
|---|---|---|---|
| R1 | `TickFrame.flow`: `receivedPOs[{id, sku, qty}]`, `shippedBySku[{sku, orders, units}]`, `ordersCreated`, `ordersCancelled` (all per tick, from engine events already used to post ledger entries) | Exact trucks, boxes and "new orders" in all cases | Recommended, **not necessary**: the design degrades to the table above without it. Needed only if the ambiguous-hour trucks must be shown |
| R2 | `GET /worlds/default/assumptions` (or `scenario` preview with bounds, no world created) | The configuration screen needs min/max before a world exists. Fallback below creates one throwaway draft world | Recommended, **not necessary** |
| R3 | `GET /branches/{id}/inputs` (accepted commands with `effectiveTick`, `provenance`, including those inherited from the parent) | Intervention markers and "what you decided" survive reload and forks | Recommended, **not necessary** for the demo path; needed for a faithful history after reload |
| R4 | Validate-only mode on `POST /commands` (dry run) | Exact affordability and rejection preview | Optional; replaced by display estimate (3.6) |

No candidate is required to ship Phase B. If R1 and R3 are rejected the scene shows the fallback behaviour stated in 1.3 and the history panel shows only this session's decisions, labelled as such.

### 1.5 Parent and child side by side: supported

Yes, with existing endpoints. `tick-frames` and `metrics` work for any branch at any tick up to its head. A child forked at tick N has the parent's history to N, so both branches have frames for ticks 0..N identical, and each diverges afterwards. For a shared tick T the UI needs `head(parent) >= T` and `head(child) >= T`. Advancing both branches to the same `runToTick` is two `advance` calls; exogenous arrivals and disruptions are identical by engine invariants, so onset windows coincide. The comparison is a display subtraction of two `Metrics` for the same tick (the existing HUD already does this). The parent has no new commands unless the user played it, so "Original path" without decisions is what the engine produces when its windows end empty. No new endpoint is needed.

---

## 2. Visual identity: "Dusk shift"

Restrained charcoal, ivory and amber. One accent. Editorial feel (a printed operations brief at dusk), not a dashboard.

### 2.1 Colour tokens (contrast on WCAG 2.2, measured)

| Token | Hex | Role | Contrast vs ink / surface / raised |
|---|---|---|---|
| `--ink` | `#17161A` | page background | n/a |
| `--surface` | `#201F24` | panels | n/a |
| `--raised` | `#2A292F` | cards, popovers | n/a |
| `--hairline` | `#4A4750` | decorative dividers only | 1.98 / 1.80 (not used for control edges) |
| `--edge` | `#7A7680` | input and button borders | 4.06 / 3.69 (meets 3:1 non-text) |
| `--ivory` | `#F3EDE0` | primary text, paper cards | 15.44 / 14.03 / 12.36 |
| `--ivory-dim` | `#B9B2A3` | secondary text | 8.54 / 7.76 / 6.84 |
| `--amber` | `#E8A33D` | accent, primary action, "your" reality | 8.35 / 7.59 / 6.68 |
| `--amber-hot` | `#F5B954` | hover | n/a; ink on it 10.25 |
| `--clay` | `#EE7B63` | crisis and loss (always paired with an icon and words) | 6.56 / 5.96 |
| `--sage` | `#9CC48A` | gain, healthy | 9.16 / 8.32 |

- Ink text on amber buttons: 8.35. Ivory on the amber-tinted crisis card background `#3A2C14`: 11.61. All text pairs above are at least 4.5 (AA); large display text exceeds 7 (AAA).
- Reality identity: **Original path** = ivory (solid line), **Alternative future** = amber (dashed line when drawn on the shared timeline). Colour is never the only cue: every reality has a text label and a line pattern.
- Scene colours are drawn from the same family: floor `#2B2A30` / `#323138` tiles, walls `#3B3A42`, rack frames `#5A5662`, boxes cardboard `#C9A26B` (shade `#A98550`, top `#DDBA85`), employees' tops ivory (regular) and amber (temps), dusk sky gradient `#2A2438` to `#6B4A3A` to `#E8A33D` at the horizon (intro only). No purple-to-blue gradient in UI chrome; the sky gradient exists only in the intro illustration.

### 2.2 Typography

- Display: **Fraunces** (variable serif, SIL OFL), headings, numerals in the briefing and outcome card.
- Body/UI: **Inter** (variable sans, SIL OFL).
- Mono: **IBM Plex Mono**, loaded and used only inside Engineering Mode (and not in the first-paint bundle).
- Loading: `next/font/local` with the three `.woff2` files committed under `web/app/fonts/` (download once in Phase B; licences in `docs/third-party-fonts.md`). Reason: `next/font/google` fetches fonts at build time and fails offline; `next/font/local` is self-hosted by Next 16.4 (confirmed in `node_modules/next/dist/docs/01-app/01-getting-started/13-fonts.md`) and works in CI and on a plane. Fallback stacks: display `Georgia, "Times New Roman", serif`; body `ui-sans-serif, system-ui, sans-serif`; mono `ui-monospace, Menlo, monospace`. Use `display: "swap"` and `adjustFontFallback` so a missing file does not shift layout. Pixi `Text` uses the body family via a `FontFace` already loaded by `next/font` (read the CSS variable's computed family).
- Scale (rem): 0.8125 caption, 0.9375 body, 1.125 lead, 1.5 h3, 2 h2, 3.25 h1 (intro title 4.5 on desktop). Line height 1.5 body, 1.15 display. Numbers use `font-variant-numeric: tabular-nums` in Inter (no mono outside Engineering Mode).

### 2.3 Spacing, shape, surfaces

- 4 px base grid; steps 4, 8, 12, 16, 24, 32, 48, 72. Radii 8 px for cards, 999 px for pills, 4 px for inputs. 1 px hairline dividers; no drop shadows except a soft amber glow (`0 0 0 1px amber, 0 0 24px amber/18%`) on the active reality's scene frame.
- Cards are paper-like: `--raised` fill with ivory text, generous padding (20 to 24 px). Dense tables and grids of KPI cards are not used. The HUD is one calm line.

### 2.4 Motion principles

- Purposeful: motion explains a change of state (a truck arrives, a path splits). No decorative loops except a 12 s slow ambience (lamp flicker is not used).
- Durations 160 to 240 ms ease-out for UI; scene transitions follow the tick pace; the fork ceremony may take up to 1100 ms (section 3.7).
- Numbers never count up. They change to their authoritative value at once.
- `prefers-reduced-motion: reduce`: no interpolation, no ceremony animation (state changes are instant with a fade-free swap), no ambient movement, intro is a still frame. Also an in-app "Reduce motion" toggle in the settings menu that overrides to reduced.
- Focus: 2 px ivory outline with 2 px ink offset on every interactive element. Targets at least 44 px on touch.

### 2.5 Iconography

`lucide-react` (already installed, 1.81.0): `Warehouse`, `Truck`, `Package`, `Users`, `Clock`, `AlertTriangle`, `GitBranch` (alternative future), `Rewind`/`Play`/`Pause`/`StepForward`, `Info`, `Wrench` (Engineering Mode), `Settings2`. Stroke 1.75. The warehouse scene itself is procedural Pixi graphics (no image assets).

---

## 3. Journey and screens

Routes (Next App Router, all client components, no new backend calls beyond 1.2):

| Route | Screen |
|---|---|
| `/` | 1 Cinematic intro |
| `/briefing` | 2 Your business |
| `/setup` | 3 Configure your business (optional step) |
| `/worlds/[worldId]?branch=` | 4 Studio (with 5, 6, 7 inside) and 8 comparison (`&compare=<childBranchId>`) |

Flow: `/` -> `/briefing` -> (optional `/setup`) -> studio. "Quick start" on the briefing creates the demo world with defaults (`POST /demo`, keeps `demoPlanStorage`). "Customize" goes to `/setup`, which creates a world with `POST /worlds {name, seed: 2026, overrides}` and no plan.

### 3.1 Cinematic intro (`/`)

Full-bleed procedural isometric warehouse at dusk (the same Pixi scene code in "intro mode": static building, lit windows, a truck idling, trucks and vans moving along fixed decorative paths; this is an illustration, labelled by context, not simulation state, and carries no numbers). Skippable: "Skip" top right (and Esc) goes straight to `/briefing`; it is the only secondary control. Auto-advance never occurs.

```
+------------------------------------------------------------------+
|                                                          [ Skip ]|
|            ~ dusk sky: charcoal to amber at the horizon ~        |
|                                                                  |
|                 /\  isometric warehouse, windows lit             |
|                /  \___  trucks at the dock, a van leaving        |
|                                                                  |
|   PARALLEL                                                       |
|   Reality has no undo button.                                    |
|   Run a warehouse through Black Friday week. When things go      |
|   wrong, rewind and try a different future.                      |
|                                                                  |
|   [ Begin your shift  -> ]                                       |
+------------------------------------------------------------------+
```

Sequence (reduced motion: single still frame, no sequence): 0 to 600 ms sky fades in; 600 to 1400 ms title (Fraunces) rises 12 px and fades; premise at 1400 ms; CTA at 2000 ms. Primary CTA "Begin your shift" is the only amber element. Footer: small ivory-dim "Synthetic simulation. Not a forecast." (the single disclaimer for this view).

### 3.2 Your business (`/briefing`)

A two-column paper card on charcoal. The text states facts from `GET /worlds` scenario summary (loaded for the default scenario by the draft or demo world; static copy where the API has no data, never a number that the API could contradict).

```
+------------------------------------------------------------------+
| Your business                                                    |
|                                                                  |
|  You run a small online electronics store and its one warehouse. |
|  You sell headphones, chargers, speakers and watch bands.        |
|  Customers expect orders to ship within a day or two.            |
|                                                                  |
|  YOUR GOAL                       WHAT GOOD LOOKS LIKE            |
|  Get through five days of        . Finish the week with more     |
|  Black Friday sales with the     .   profit than you started     |
|  best profit you can, without    . Most orders delivered on time |
|  letting customers down.         . Few orders left waiting       |
|                                                                  |
|  Black Friday week brings surprises. You will not know ahead     |
|  of time what they are.                                          |
|                                                                  |
|  YOU CAN                                                         |
|  . Watch the warehouse work, hour by hour                        |
|  . Place orders with suppliers and change prices                 |
|  . Rewind and explore another future from any hour              |
|                                                                  |
|  [ Start now ]        [ Customize the business ]                 |
+------------------------------------------------------------------+
```

Rules: no crisis is named, timed or counted. The only allowed teaser is the quoted sentence. "Five days" and "Thursday to Monday" come from `horizonTicks` 120 and `startClock`. "You can" lists only implemented actions.

### 3.3 Configure your business (`/setup`)

Seven friendly controls, grouped, each with plain label, unit, default marker, bounds and a one-line rationale (server `rationale`, trimmed of "synthetic, illustrative:" and shown behind an "Info" popover so the screen stays calm). Sliders with a stepper for fine control; the value is also a number input for accessibility. Values map to `overrides`.

| Control (label) | Assumption path | Display |
|---|---|---|
| Starting cash | `openingCashCents` | dollars; slider/stepper steps $1,000 (cents / 100) |
| Packing staff | `workforce.regularWorkers` | "20 people" |
| Packing speed | `workforce.unitsPerWorkerHour` | "8 items per person per hour" |
| Normal supplier delivery time | `suppliers.<sku>.primary.leadTimeTicks`, applied to all four products as one control | "24 hours" (writes four overrides) |
| Backup supplier delivery time | `suppliers.<sku>.backup.leadTimeTicks` (four overrides) | "8 hours" |
| Selling prices | `skus.<sku>.referencePriceCents`, one stepper per product (no UI-side percentage maths) | dollars |
| Shipping cost per item | `costs.outboundShipCentsPerUnit` | dollars |

Everything else stays default and is reachable through "Engineering Mode > Assumptions".

```
+--------------------------------------------------------------+
| Set up your business                         [Reset to default]|
| MONEY                                                        |
|  Starting cash        ----o---------   $100,000   (default)  |
| PEOPLE                                                       |
|  Packing staff        -------o-----   20 people   [-][+]     |
|  Packing speed        ------o------   8 items / person / hr  |
| SUPPLIERS                                                    |
|  Normal delivery      -----o-------   24 hours               |
|  Backup delivery      -o-----------   8 hours                |
| PRICES   Headphones $59.99  Charger $19.99  Speaker $89.99 ..|
| Changes apply to this run only.                              |
|                                  [ Start with these settings ]|
+--------------------------------------------------------------+
```

Data and truth: bounds come from `world.assumptions` min/max. They do not exist before a world exists (R2). Phase B approach with no API change: on entering `/setup`, call `POST /worlds` once with default overrides to obtain a draft world and read assumptions (bounds, defaults, rationale), then on "Start" call `POST /worlds` again with `overrides` containing only changed values. If nothing changed, reuse the draft world as the run. The draft is one extra world per visit (rate limit 429 shown as a friendly retry). The demo path never visits this screen and keeps `POST /demo` defaults. A control that violates a bound is impossible by construction (slider min/max); a server 400 is shown inline in plain words.

### 3.4 Studio layout (living warehouse)

The warehouse is the page. Chrome is minimal, one calm line on top, one rail of cards on the right (bottom sheet on mobile), a time strip at the bottom.

```
+------------------------------------------------------------------------+
| Black Friday Meltdown   Thursday 7:00 PM        [Original path]  [Wrench]|
| Cash $98,420   Profit so far +$1,240   On time 96%   Orders waiting 14 |
+--------------------------------------------------------+---------------+
|                                                        |  YOUR MOVE    |
|          isometric warehouse (PixiJS)                  |  (cards: 5,6) |
|   dock/trucks      racks per product     packing       |               |
|        ... boxes, people, vans, order tickets ...      |  Crisis card  |
|                                                        |  Decision cards|
|  [!] Waiting for inventory: Speakers (123 orders)      |  Explore       |
|                                                        |  another future|
+--------------------------------------------------------+---------------+
| |<  <  [ Play ] speed: Calm / Quick / Fast  >  >|   Thu Fri Sat Sun Mon |
| time strip: day bands, "your moves" pins, fork pins, hours ........|
+------------------------------------------------------------------------+
```

- HUD (one line, Inter, tabular numerals): clock "Thursday 7:00 PM", Cash, Profit so far, Orders delivered on time, Orders waiting. Each money figure is a button opening the "Where does this number come from?" drawer (ledger descriptions in plain language, with Engineering Mode adding account lines and refs). A forked reality shows a quiet second line "Original path: +$X / Y% / N" using the same-tick parent metrics, with the display difference.
- Playback labels: "Calm / Quick / Fast" (2, 8, 32 hours per second, unchanged speeds) and "Skip to next move" (next decision window). Fast-forward to the next disruption onset (Shift+D) moves to Engineering Mode: for a manager it would be a spoiler control. Space, arrows and F bindings stay; F = "Explore another future".
- Time strip: the existing Timeline logic with friendly day bands. Markers appear only for ticks at or before the **high-water mark** (the furthest tick ever displayed on this branch in this session, or the branch head when a branch is loaded already played). Reason: the API returns frames up to the head, which can be ahead of the displayed tick, and onset markers there would preview the future. Fork pins and "your move" pins use the existing inputs source (R3 if added).
- Auto-pause default stays "every decision window" (Requirement 33.15). The "disruption onsets only" mode moves to the settings menu.
- Disclaimer: once, in the bottom-left of the strip, plus the assumptions link ("How this is modelled").

### 3.5 Contextual crisis cards

A card appears in the right rail (and as a pill over the scene) when the displayed frame shows any of: an active disruption, stock-bound orders, capacity-bound orders, a supplier unavailable in the Observation, or an inbound purchase order past its due time. Content is assembled from authoritative data only, with fixed templates. Layout:

```
+--------------------------------------+
| (!) Speakers are running out         |   <- what happened
| Your usual supplier can't deliver    |   <- why (supplier_outage + supplier role)
| right now. 123 orders are waiting    |   <- what is affected (report.skus[].stockBoundOrders)
| for speakers.                        |
| Backup supplier: 8 hours, $57.00 ea. |   <- Observation.suppliers[].leadTimeTicks, unitCostCents
| [ See options ]                      |   <- valid actions (links to cards)
+--------------------------------------+
```

Templates (keys to data):

| Condition | Title | Body |
|---|---|---|
| `disruptions[].kind = demand_surge`, target SKU or -1 | "Orders are surging" (SKU name or "across the store") | "Far more customers are buying than usual. {orderQueueLen} orders are waiting." |
| `supplier_outage` (+ `supplierRole`, `availableFromTick` from Observation) | "{Supplier role name} supplier is down" | "Deliveries from your {normal/backup} supplier for {product} are paused until {clock(availableFromTick)}." |
| `workforce_absence` | "Some of your team can't make it" | "{absent} of {regularWorkers} packers are out. Packing is slower." (absent = assumption regularWorkers - `workersOnShift`) |
| stock-bound > 0 | "Waiting for inventory: {product}" | "{n} orders can't ship because {product} is out of stock. {inboundUnits} on the way." |
| capacity-bound > 0 | "Waiting for packing capacity" | "{n} orders are ready but the team can only pack {capacity} items an hour." |

The card never states a disruption's end tick or magnitude, and it never previews. A disruption not yet in effect at the displayed tick has no card. The existing "crisis tick" in the plan is not shown to the manager. Product names come from `scenario.skus[].id` capitalised (headphones -> "Headphones", watchband -> "Watch bands"; display-name map in `lib/glossary.ts` keyed by id with a title-case fallback).

### 3.6 Guided decisions ("Your move")

Shown when the window is open at the displayed head. Replaces the form panel. Card types (only implemented commands):

1. **Order more** (per product with supplier data): "Order {qty} {product} from the {normal | backup} supplier". qty stepper (default blank means the card shows a suggested quantity only when the plan provides one; otherwise default 100), supplier toggle.
2. **Change a price**: stepper bounded 50% to 200% of `referencePriceCents` (bounds stated in the openapi command description and validator: `PriceBounds`). Bounds are shown as "between $29.99 and $119.98" using `referencePriceCents` x 0.5 and x 2 with integer arithmetic (display, labelled "allowed range").

Quote line on an order card (server numbers only):
- Unit price: `suppliers[role].unitCostCents`.
- Delivery time: `suppliers[role].leadTimeTicks` as "about N hours after your move" (no clock arithmetic: avoids guessing the effective tick; after submit the real due time is shown from `Observation.purchaseOrders[].dueAt` converted to a clock label).
- Total: `qty x unitCostCents`, labelled **"Estimated cost"** with the formula in an Info popover. The validator computes exactly `qty * unitCostCents` for cash commitment, but outbound shipping is paid at sale, so "estimate" is accurate. Display multiplication of two server integers is acceptable (decision) because it is arithmetic on displayed inputs, not a business result; the server remains authority and any rejection (`insufficient_cash`, `supplier_unavailable`, ...) is shown in plain language beside the card.
- "Cash after: $X" is not shown (it would be a derived result). The card shows "You have $Y" from the Observation next to the estimate.
- If `suppliers[role].available` is false, that supplier toggle is replaced by the text "Unavailable until {clock}" (the option is omitted rather than disabled; the other supplier remains).

Suggestion card (demo path only): when `plan !== null` and displayed tick >= `plan.forkTick`: "Suggested: order {qty} {Product} from the backup supplier" with a one-tap "Add to my moves". `plan.reasons` are never shown outside Engineering Mode. Copy states "A suggestion, not a guarantee."

"Decisions remaining: n of 5" (from `window.commandBudget` and `budgetRemaining`) with a sentence "Each move you submit takes effect an hour from now". Buttons: **Confirm moves** (submit) and **Skip this move** (end the window). After confirmation each accepted move is listed as a sentence ("Ordered 315 speakers from the backup supplier, arriving in about 8 hours") with a pin on the time strip. Rejections: plain sentence from a code-to-copy map (`insufficient_cash` "Not enough cash for this order", `supplier_unavailable`, `price_out_of_bounds`, `invalid_quantity`, `budget_exhausted` "No decisions remaining this window"); the raw message is available in Engineering Mode.

### 3.7 Signature interaction: Explore another future (fork)

Entry points: the card at the bottom of the rail "Rewind to now and explore another future" (enabled at any displayed tick at or before the head), key `F`, and a button on the time strip scrubber. Before commit, an explainer sheet (one screen, dismissible forever via "Don't show again" in localStorage) shows:

```
+--------------------------------------------------+
| Explore another future                           |
| Everything up to Thursday 8:00 PM stays exactly  |
| as it happened. From that hour on, you can try   |
| different decisions in a second reality. The     |
| original path is never changed.                  |
|        ====== shared history ======\______ original|
|                                      \_____ yours |
|  [ Create the alternative ]   [ Not now ]        |
+--------------------------------------------------+
```

Commit calls `forkBranch(branchId, {atTick: displayedTick, label})` with label "Alternative future {n}" (auto-numbered from the loaded branch list; no text field). The ceremony starts only when the server returns the child (no optimistic timeline):

| t (ms) | Motion |
|---|---|
| 0 | Scene dims 15% and the time strip's past segment lights as a single ivory "trunk" line labelled "Shared history" |
| 0 to 300 | Request in flight shows "Creating the alternative…" (no animation of results) |
| 300 to 800 | After the response: the trunk line splits at the fork hour into two lanes: upper ivory solid "Original path", lower amber dashed "Your alternative". Opacity and translateY only |
| 800 to 1100 | The scene frame glows amber (active reality), the "Your move" rail opens on the alternative, caption: "Same hour, different future" |
| Reduced motion | Instant swap, no glow; the lanes appear with a static label |

Afterwards the time strip permanently shows both lanes from the fork hour; the shared trunk is drawn once. The reality switcher is a two-segment control in the header ("Original path" / "Your alternative"), keyboard accessible, and replaces the React Flow rail. React Flow is retained only inside Engineering Mode for trees with more than two realities (or removed in a later phase; not Phase B scope). With more than two branches (a second fork) the switcher becomes a menu of "Alternative future 1..n", still with labels and patterns. Branch cap 32 is surfaced as "You've reached the limit of futures for this run."

### 3.8 Side-by-side comparison (`&compare=<childId>`)

Available when the active branch has a parent (or vice versa) and both heads reach the compared tick. Two panes, one shared time strip, one playback. Both panes always display the same tick (Requirement 33.14); playback advances both branches (advance each to the same `runToTick`; windows end empty on the branch the user is not deciding on).

```
+------------------------------------------------------------------+
|  Thursday 8:00 PM + 14 h = Friday 10:00 AM          [ Play ] ... |
| +------------- Original path --+--+--- Your alternative ---------+|
| |  scene (compact)             |  |  scene (compact)             ||
| |                              |  |                              ||
| +------------------------------+  +------------------------------+|
| Profit so far     +$1,240   |  +$3,980   | $2,740 more           |
|   What this means: sales minus every cost so far.                 |
| Orders on time        88%   |     94%    | 6 points higher       |
| Orders waiting         41   |     12     | 29 fewer              |
| Cash              $98,420   | $91,130    | $7,290 less           |
| ---------------------------------------------------------------- |
| Observed differences. To know what caused them, change one decision|
| at a time in a new alternative.                                   |
| Synthetic simulation output under the stated assumptions. ...     |
+------------------------------------------------------------------+
```

- Differences are display subtraction of the two `MetricsResponse` values at the same tick; the sign decides the words ("more"/"less"/"higher"/"fewer"/"the same"). Wording is "observed differences", never "caused by" (steering rule), and carries the note that attributing an effect needs a fork that changes one decision.
- **Outcome summary card** at the horizon (tick 120, both heads at 120): Fraunces headline built from the server numbers, e.g. "Your alternative finished $X ahead on profit, with Y% of orders on time against Z%." No winner, no score, no "AI". If equal, "Both paths finished level." The card lists three rows (profit, on time, orders waiting) with each side's value and a "How is this calculated?" link opening the explanation copy (section 4.2).
- Divergence: the shared trunk ends at `branch.forkTick`; no `firstDivergenceTick` API exists, so the UI marks the fork hour, not the first observable difference.
- Panes render compact scenes (section 6.5) and each has the same click-through to the ledger drawer.

### 3.9 Human vs AI

Omitted. No duel endpoint exists and the rule-based manager is a stub, so any control would be non-functional. Nothing is rendered, not even a disabled button or "coming soon" tile. When the backend gains duels, Phase C adds a third screen.

---

## 4. Plain-language glossary

### 4.1 Term mapping (single source: `web/lib/glossary.ts`, unit-tested)

| Engine term | Manager-facing text | Notes |
|---|---|---|
| Tick `t` | `Thursday 7:00 PM` from `startClock` "Thu 00:00": day = floor(t/24) (Thursday..Monday), hour = t mod 24 in 12-hour form | Same hour is "Thu 7:00 PM" in compact spaces. Tick numbers only in Engineering Mode |
| Horizon | "Five days" / "Ends Monday 11:00 PM" (tick 119 is the last hour; the engine horizon is 120 ticks) | |
| SKU / `sku` index | Product name from `scenario.skus[].id`: Headphones, Chargers, Speakers, Watch bands | Never "SKU 0" |
| Fork | "Explore another future" (verb), "Alternative future" (noun) | |
| Branch (root) | "Original path" | |
| Branch (child) | "Your alternative" / "Alternative future n" | |
| Command | "Move" / "Decision" | |
| Command budget | "Decisions remaining: n of N" | |
| Decision window | "Your move" | |
| Place purchase order | "Order more {product}" | |
| Supplier role primary/backup | "Normal supplier" / "Backup supplier" | |
| Lead time | "Delivery time (hours)" | |
| Stock-bound | "Waiting for inventory" | |
| Capacity-bound | "Waiting for packing capacity" | |
| Backlog | "Orders waiting" (units: "items") | |
| Late backlog | "Orders already late" | |
| Service level | "Orders delivered on time" | |
| Fill rate | "Items shipped (of those due)" | Unit-based per backend formula, so the label says items, not orders |
| Net profit | "Profit so far" | |
| Contribution | "Profit before wages and overhead" | |
| Cash | "Cash" | |
| Inventory value | "Stock value" | |
| Disruption kinds | demand_surge "Orders surging", supplier_outage "Supplier down", workforce_absence "Team members out" | |
| State hash | (hidden) | Engineering Mode |
| Observation | (hidden) | |
| Ledger | "Where this number comes from" | Entries by `description` |
| Disclaimer | "Synthetic simulation. Not a forecast." (short) plus the full sentence behind "How this is modelled" | The full required sentence stays available |

### 4.2 Metric explanation copy (matches `Metrics` descriptions in openapi)

- **Cash**: "Money in the bank right now. Orders and wages move it as they happen."
- **Profit so far**: "Sales minus the cost of the goods you shipped, shipping, wages, overhead, marketing and credits for late orders. Sales count when an order ships."
- **Orders delivered on time**: "Of the orders whose ship-by time has passed, the share that shipped in time. It shows 100% until some orders come due."
- **Items shipped (of those due)**: "Of the items customers were due to receive by now, the share that has shipped."
- **Orders waiting**: "Orders that haven't shipped yet. {late} of them are already late." (late = `lateBacklogOrders`)
- **Stock value**: "What the products in the warehouse are worth at your cost."
- **Profit before wages and overhead**: "Sales minus the cost of goods shipped and shipping."

---

## 5. Engineering Mode

- A toggle in the settings menu (wrench icon in the header, `aria-pressed`) stored in `localStorage["parallel.engineering"]`; default off; Zustand store `stores/ui.ts` (`engineering`, `reduceMotion`, `explainerDismissed`, hydrated on mount to avoid SSR mismatch; no flash because the studio is client-rendered).
- When on: the HUD gains the tick number and clock in `tick 19`, state hash (full, copy button), world and branch ids, engine version; the right rail gets tabs: **Decisions** (existing `DecisionPanel`), **Ledger** (new drawer with account lines and cause refs from `listLedgerEntries`), **Events** (frame markers, constraint report as raw rows, disruptions with `onsetTick`, `endTick`, `origin`), **Raw metrics** (every `Metrics` field), **Assumptions** (existing `AssumptionsPanel`), **Plan** (existing `PlanHint` including reasons: the only place the plan's derivation shows). Mono font loads only on first enable. React Flow tree and the old timeline tick labels are available here.
- The old components are kept and re-homed rather than deleted, so their tests stay valid. Friendly components are new files beside them (`features/studio/*`, `features/briefing/*`).
- Engineering Mode does not alter any request or number.

---

## 6. Warehouse scene (PixiJS 8)

### 6.1 Projection

2:1 isometric, grid of `GW x GH = 22 x 14` cells, tile `TW = 64`, `TH = 32`.

```
screen.x = originX + (gx - gy) * TW / 2
screen.y = originY + (gx + gy) * TH / 2 - gz * ZH      (ZH = 24 px per unit height)
zIndex   = (gx + gy) * 100 + gz                         (painter's order)
```

`originX = sceneWidth / 2`, `originY = 80`. The scene is a `Container` scaled to fit (`scale = min(w / 1472, h / 700)`, DPR capped at 1.5), centred. All drawing is procedural `Graphics` (no external assets) and drawn once into cached `Container`s with `cacheAsTexture` for static parts (floor, walls, rack frames), then reused. Hit testing is off (`eventMode = "none"`) except labels needed for focus.

### 6.2 Layout (grid cells)

| Zone | gx..gy cells | Contents |
|---|---|---|
| Inbound dock (left) | 0..4 x 2..9 | 2 dock bays, dock plate, delivery truck lane, "Deliveries" sign with open-purchase-order count |
| Storage (centre) | 6..15 x 1..8 | 4 rack blocks, one per product, in `scenario.skus` order, each with a floor sign "Headphones 700" |
| Packing (centre-low) | 6..15 x 9..13 | 4 packing stations (each a bench with a scale and a station light) and a conveyor to the outbound bay |
| Order line (right-low) | 16..21 x 9..13 | "Orders waiting" wall rail with tickets |
| Outbound dock (right) | 16..21 x 1..8 | 2 bays, delivery van lane |
| Break/training area | 0..4 x 10..13 | where absent and onboarding people appear |

The building shell is a low-poly box: two back walls (`#3B3A42`), side walls with windows, flat roof removed for the cutaway. Floor tiles alternate `#2B2A30` / `#323138`, painted as a single cached texture. Zone floor stripes (amber hazard lines at the docks) are static decoration.

### 6.3 Objects, drawing spec and driving data

| Object | Drawing | Driven by |
|---|---|---|
| Rack block (per product) | 3 bays x 3 levels frame (poles `#5A5662`, 2 px, shelves) | Static; position by SKU index |
| Boxes on racks | Isometric cubes 24 x 20 x 20 px (top `#DDBA85`, left `#C9A26B`, right `#A98550`, 1 px darker edge). `boxes = min(RACK_SLOTS, ceil(onHand / UNITS_PER_BOX))`, `UNITS_PER_BOX = 20`, `RACK_SLOTS = 54` per product (3 bays x 3 levels x 6 depth). Slots fill bottom-up, back-to-front, so depletion visibly empties from the front | `skus[s].onHand` at t. If `ceil(onHand/20) > 54`, show a "+N more boxes" chip (`N = ceil(onHand/20) - 54`). The floor sign always prints exact units, so the box count is a rounded picture of an exact number |
| Stock-bound marker | Empty rack with a clay "out of stock" tag, and a speech-bubble ticket count | `report.skus[].stockBoundOrders` |
| People | 18 x 34 px figure: head circle, body capsule, shadow ellipse. Regular: ivory top. Temp past onboarding: amber vest. Onboarding temp: dimmed with a "training" badge in the break area. Absent regular: not drawn, an empty chair at the break area | `workersOnShift`, `tempsOnShift`, `tempsOnboarding`; absent = `workforce.regularWorkers` - `workersOnShift` (from `world.assumptions`) |
| People cap | Max 40 figures on the floor (40 regular + temps), then a "+N" chip | Count capped for performance, number in the chip exact |
| Packing stations | 4 benches. Figures assigned in blocks of `ceil(onShift / 4)`. Station light: sage when `capacityBoundOrders = 0`, clay pulsing 1 Hz when > 0 | `report.skus[].capacityBoundOrders` |
| Order tickets (queue) | Small paper rectangles (14 x 18, ivory, a folded corner) clipped onto the "Orders waiting" rail. `tickets = min(40, orderQueueLen)` placed in rows of 10; `+N more` chip when `orderQueueLen > 40`; the rail sign prints the exact count | `orderQueueLen` |
| Incoming tickets | New ticket slides from the right edge to the rail (180 ms) | `max(0, orderQueueLen(t) - orderQueueLen(t-1) + shippedOrders(t))`, at most 8 per hour drawn. Labelled "at least" in the accessible summary |
| Delivery truck | Box truck 3 x 1.4 cells: body ivory, cab amber, wheels. Drives in from the left edge along the inbound lane (700 ms at 1x, scaled down with speed, minimum 120 ms), docks, waits while boxes unload, drives out | A decrease of `inboundPOs[s]` between t-1 and t (1 truck per decreased PO, max 3 per hour; "+N" label). Fallback and R1 as in 1.3 |
| Inbound boxes | 3 to 6 boxes move truck to dock to the product's rack (carried by a figure or on a trolley, 500 ms) | Same event; the number of moving boxes is decorative, the rack's final stack is the authoritative `onHand` after the move |
| Outbound van | White van with an amber stripe. Departs the outbound bay and exits right (600 ms) | `vans = min(6, ceil(shippedOrders / 10))` from `report.shippedOrders`; the pop-up label says "{shippedOrders} orders shipped" (exact) |
| Boxes on shipment | Boxes leave the rack, ride the conveyor to the outbound bay, load into a van. Per-product only when the exact condition in 1.3 holds; otherwise boxes are drawn from the product with the largest on-hand decrease | `onHand(t-1) - onHand(t)` |
| Disruption cue | Affected zone gets a clay edge pulse (2 px stroke, 1.2 s), and a small icon (`Truck` slashed, `Users`, `Zap`-like surge arrow drawn as a path) with a caption | `disruptions[]` at t; supplier outage lights the dock sign "Normal supplier paused"; no cue before onset in the displayed frame |
| Intervention marker | A pin at the affected zone for the hour a move took effect (order placed: dock; price change: the product's rack sign) | Accepted command `effectiveTick` equals t (session now, R3 later) |

Idle life: figures at stations show a 2-frame work cycle (1 Hz). This has no data meaning and stops under reduced motion; no figure moves between zones except for the two data-driven carry events above.

### 6.4 Statement of fidelity

- Every count the viewer can read (stack height, tickets, figures, vans, trucks) is either exact or capped with an exact "+N" chip or exact sign text.
- Motion happens only for the events in the table above, between two authoritative frames. If a frame is loading (syncing), the previous frame is shown dimmed with a "Catching up…" label; no new motion starts.
- No event is replayed when scrubbing backwards; scrubbing, stepping and jumping snap the scene to the frame instantly with no travel animation (animation plays only on forward playback of consecutive ticks).

### 6.5 Performance and rendering

- Target 60 fps at 1440x900 on an M-series laptop, and 30 fps minimum on a mid mobile device (check in Phase B with `performance.now` frame-time sampling, recorded in `docs/benchmarks.md`; do not claim numbers beforehand).
- Static layers cached as textures. Dynamic sprites pooled: `BoxPool` (max 54 x 4 + 24 moving), `FigurePool` (max 40), `TicketPool` (40 + 8), `TruckPool` (3), `VanPool` (6). Objects are shown/hidden, never created or destroyed per tick. Total display objects below 700.
- Render on demand: ticker runs only while a tween exists; otherwise the stage renders once per state change. Pause rendering when the tab is hidden or the canvas is off screen.
- One `Application` per visible pane, DPR cap 1.5, `antialias: false`, `powerPreference: "low-power"`. Compare mode uses two apps at half the pixel budget each (compact scene: boxes quantized to 3 per level, labels simplified).
- Tween clock is the Pixi ticker; tween duration = `min(base, tickInterval * 0.8)` so 16x playback (32 ticks/s) shows only trimmed motion and never lags behind data; at 16x only counts change, no travel animation.

### 6.6 Scene code structure (Phase B)

```
features/world-view/scene/
  iso.ts               # project(gx,gy,gz), zIndex, tile math (pure, unit tested)
  sceneModel.ts        # buildSceneModel(frame, prevFrame, assumptions) -> counts + events (pure, tested)
  drawStatic.ts        # building, floor, racks, stations
  pools.ts             # object pools
  WorldSceneV2.ts      # orchestrates, tweens, reduced motion
  worldViewModel.ts    # kept; extended, not replaced
```

`sceneModel.ts` is the only place that interprets deltas and is unit tested with synthetic frame pairs (truck only when `inboundPOs` decreases, vans from `shippedOrders`, boxes cap, absent workers).

### 6.7 Accessibility

`WorldViewSummary` keeps the text summary and is rewritten in the glossary vocabulary: "Thursday 7:00 PM. 20 packers on shift. 14 orders waiting. 2 deliveries on the way (315 speakers). Waiting for inventory: Speakers, 123 orders." The canvas is `role="img"` with `aria-labelledby` pointing at the summary, which updates with `aria-live="polite"` only at pauses and decision windows (not at 32 ticks/s). Reduced motion removes all tweens. Keyboard: Space play/pause, arrows step, Shift+arrows jump a move, F explore another future, `?` opens a shortcuts list.

---

## 7. Responsive behaviour

Breakpoints: `>= 1100 px` desktop, `700 to 1099` tablet (same as desktop, rail 320 px), `< 700` mobile.

- **Desktop (>= 1100)**: layout in 3.4. Rail width 360 px, collapsible. Scene fills the remaining area; time strip fixed at bottom (88 px).
- **Mobile (390x844)**: stacked.

```
+--------------------------+
| Thu 7:00 PM    [Original]|
| Cash  Profit  On time  14|   one scrollable row of 4 figures
+--------------------------+
|                          |
|   scene (compact),       |   16:11 area, pinch-less; tap an object
|   4:5 crop, rack labels  |   for its exact number (tooltip)
|                          |
+--------------------------+
| [|<] [ Play ] [Quick] [>|]|   playback bar, 48 px targets
| time strip (scrubbable)  |
+--------------------------+
| ^ Your move  (bottom sheet, 3 detents: 72 px peek, 50%, 90%) |
|   tabs: Moves | Alerts | Compare                             |
+--------------------------+
```

The bottom sheet opens to 50% automatically at a decision window and for a new crisis card (pill with a count badge at peek). Compare on mobile stacks the two scenes (each 16:9) with a sticky metric table. Setup sliders use full-width rows with 44 px thumbs. The intro uses a cropped scene with the same sequence.

---

## 8. Testing plan

### 8.1 Unit and component (Vitest, existing runner)

`glossary.test.ts` (clock labels for ticks 0, 19, 119, product names, term mapping), `sceneModel.test.ts` (delta rules), `iso.test.ts`, decision-card quote tests (qty x unit cost, supplier unavailable omitted), crisis-card template tests per condition, high-water-mark marker filtering test (no future onset marker), engineering-mode persistence test. Existing tests must keep passing.

### 8.2 Playwright (real browser)

- Add `@playwright/test` exact-pinned in `web/package.json` devDependencies in Phase B (`npm i -D -E @playwright/test`), chromium only (`npx playwright install chromium`); `playwright.config.ts` with `webServer` starting `next dev -p 3100` and an externally started backend (`docker compose up -d db`; `go run ./cmd/parallel-api`, or the in-memory repo if the API supports it; document in README). Script `npm run e2e`. The 2-second pause between actions of Requirement 34.1 is kept for the demo-path test only.
- Viewports: desktop 1440x900 and mobile 390x844 (`isMobile`, `hasTouch`).
- Screenshots written to `docs/screenshots/` with stable names: `01-intro-{desktop,mobile}.png`, `02-briefing-*`, `03-setup-*`, `04-studio-start-*`, `05-crisis-card-*`, `06-decision-cards-*`, `07-fork-ceremony-*`, `08-comparison-*`, `09-outcome-*`, `10-engineering-mode-*`. Screenshots are artifacts for review, not visual-regression baselines (no pixel assertions).
- Flow tests: (1) skip intro and quick-start; (2) customize and start, asserting `POST /worlds` carried the overrides and that the studio's assumptions match the response; (3) play to the first move, assert the window opens and no disruption text appears before the displayed frame contains one; (4) the demo path: fork at the plan tick, add the suggested order, assert accepted and a time-strip pin; (5) play both realities to the horizon and open the outcome card; (6) engineering toggle persists across reload; (7) `prefers-reduced-motion` run asserts no tweens (checks `data-motion="reduced"`).
- Hidden-script test: with a fresh world, intercept every response and assert that no on-screen text contains a future onset (`disruptions` entries whose `onsetTick > displayed tick`) before it is displayed, and that the briefing contains no crisis keyword (surge, outage, shortage, absence, supplier failure).

### 8.3 Metric verification test

For desktop and mobile: for a set of ticks (0, a mid-run window, the horizon), after the UI shows a tick, the test calls the live backend directly (`GET /branches/{id}/metrics?tick=`, `/state?tick=`, `/tick-frames`) and asserts each displayed figure equals the formatted API value: cash, profit so far, on time %, orders waiting, late waiting, stock value, per-product on-hand on the floor signs, orders-waiting rail sign, workers on shift, deliveries on the way. In comparison mode both panes and every displayed difference are recomputed from the two API responses (subtraction in the test). It also asserts that the number of rendered boxes equals `min(54, ceil(onHand / 20))` using a debug accessor `window.__parallelScene?.counts()` that exists only when `NEXT_PUBLIC_E2E=1`. Formatting helpers are the production ones; the expected strings come from an independent formatter in the test to catch bugs in the shared one.

### 8.4 Gates

`npm run typecheck`, `npm run lint`, `npm test`, `npm run build`, `npm run e2e` pass; backend untouched, `go test ./...` still passes (no backend change).

---

## 9. Phase B work breakdown (ordered, each independently verifiable)

| # | Work | Verify |
|---|---|---|
| B0 | Tokens and fonts: new CSS variables in `globals.css`, `next/font/local` fonts + fallbacks, `lib/glossary.ts` (+ tests), `stores/ui.ts` (engineering, reduced motion), restyle shared primitives. Update `.kiro/steering/ui-design-system.md` | Unit tests, `npm run build` offline, contrast table re-run |
| B1 | Intro, briefing and setup routes; `createWorld` overrides flow with draft world; quick start via `/demo` | Component tests; Playwright flows 1 and 2 |
| B2 | Studio shell: header HUD in plain language, reality switcher, time strip restyle, playback labels, high-water-mark marker filter, ledger "where does this come from" drawer, Engineering Mode with relocated panels | Component tests; metric-verification subset (HUD) |
| B3 | Crisis cards and guided decision cards (order, price, suggestion, quotes, plain rejections, accepted-move list) | Template and quote tests; Playwright flow 3 and 4 |
| B4 | Scene v2: `iso.ts`, `sceneModel.ts`, static art, pools, boxes, people, tickets, stations, disruption cues, summary text; then trucks, vans and carry events | `sceneModel` tests; metric verification of counts; frame-time sample into `docs/benchmarks.md` |
| B5 | Fork ceremony, explainer sheet, auto-labelled alternatives, comparison view and outcome card, mobile bottom sheet and stacked compare | Playwright flows 5 to 7, both viewports; screenshots under `docs/screenshots/` |
| B6 | Polish and docs: reduced-motion pass, a11y pass (keyboard, focus, aria-live), README and `docs/kiro-build-log.md` entry, remove dead components | Full gate in 8.4 |

Deferred (not in Phase B): R1 to R4 backend fields, human-vs-AI screen, React Flow replacement, share links, sound.

## 10. Decisions summary

- Palette charcoal `#17161A`, ivory `#F3EDE0`, amber `#E8A33D`; Fraunces + Inter, Plex Mono in Engineering Mode only; local fonts via `next/font/local`.
- Zero backend changes needed. Optional additive fields R1 to R4 are listed in 1.4 and none is required.
- Only purchase orders and price changes are offered as decisions, because the validator rejects every other command kind.
- Configuration uses `createWorld.overrides` before creation, with a throwaway draft world to learn the bounds.
- Truck arrivals are shown only when frames prove them; otherwise the dock counter updates without animation.
- No human-vs-AI UI, no disabled controls.

---

## 11. Phase B-studio: delivered and known issues

Delivered in `web/features/studio`, `decisions`, `timeline`, `world-view/{Hud,PlaybackControls}`, `engineering`:

- Plain-language header (time, Cash, Profit so far, Orders delivered on time, Orders waiting), each with a keyboard-accessible info popover from `glossary.metricExplanation`. Cash and Profit open "Where this number comes from" (paged ledger).
- Crisis cards (`studio/crisis.ts`, pure, unit-tested) from the displayed frame and, only when it is for the same hour, the observation. Disruption cards show only disruptions that have started by the displayed hour. Supplier-outage "why" uses the disruption end hour from the frame, as requested for this phase; section 3.5 said never to state an end tick, so revisit if that is judged a spoiler.
- Guided decisions: order more and change price only (staffing, overtime, marketing and expedite are not implemented server-side, so they are omitted). Rejection codes map to plain sentences (`glossary.rejectionMessage`); the server message appears only in Engineering Mode. The price range hint is 50% to 200% of the reference price computed on the client for display; the server validator stays the authority because bounds are not exposed in the API.
- Time strip: day names, markers only up to the furthest hour this reality has already shown in this session (`useHighWater`). A reality opened at its head shows its full played history.
- Engineering Mode drawer holds tick, state hash, ids, raw metrics and observation, ledger with cause references, event log, the plan hint with reasons (never in normal mode) and the React Flow branch tree.
- Mobile (up to 768 px): canvas on top, playback and time strip, a bottom sheet with Your move / Alerts / How this is modelled tabs that opens by itself for a new decision moment or alert. Controls are at least 44 px.
- Fork is restyled as "Explore another future" (optional name).

Known issues:

- Move markers on the time strip and scene are session-tracked. The API has no inputs endpoint, so a reload loses them (the ledger and metrics still reflect the moves).
- "Skip to next surprise" advances the reality to the next disruption onset and stops there. It is a user-chosen skip, not a preview, but section 3.4 argued for hiding it; kept because Phase B-studio asked for it.
- The "How this is modelled" panel still lists raw assumption paths and units.

## 11b. Phase B-fork+compare: delivered and known limitations

Delivered (`web/features/branches`, `web/features/studio`, `Timeline`):

- **Fork sequence.** `forkSequenceMachine.ts` (pure reducer) starts the sequence only on `server_returned`, never when the button is pressed. `ForkSequence.tsx` draws an SVG ribbon: the shared-history trunk splits into "Original path" (ivory, solid) and "Your alternative" (amber, dashed) with the caption "Same history up to <time>. Different futures from here." About 1.5 s, CSS keyframes in `globals.css` (opacity and transform only, no numbers, no GSAP). Click, the Skip button or Esc ends it. Under `prefers-reduced-motion` the phase is `static`: the caption is shown without animation classes and stays for 5 s or until dismissed. The canvas already continues on the new reality while the overlay plays; a persistent "Showing: <reality>" badge with the lane's line sample stays on the canvas.
- **Reality switcher.** `RealitySwitcher` is a segmented group of `aria-pressed` buttons, each with a lane colour and line pattern (`lanes.tsx`: solid ivory, dashed amber, dotted sage, dash-dot clay). Left/Right/Home/End move focus. Names are "Original path", "Alternative future N" or the user's name.
- **Compare futures.** Available when there is more than one reality. `ComparisonView` renders two `StagePane`s (one `WorldView` each) at the one displayed hour; the existing time strip and playback drive both. The strip shades the shared history up to the divergence moment (`sharedUpTo`, also shown for any alternative). A reality that has not reached the shown hour is drawn as "not simulated yet" with "Advance this future" (`advanceBranch` with `expectedHeadTick` and `runToTick`; `stale-head` refetches). If a decision moment is open at that reality's head, it shows "Your move is waiting in <reality>" and "Go to <reality>"; windows are never ended or advanced past from the comparison. "Observed differences at <time>" subtracts two server values at the same hour (Cash, Profit so far, Orders delivered on time, Orders waiting), with the explainer and the synthetic disclaimer. When the realities have different heads, it compares at the lower head and says so. Figures load while paused (not on every playback tick) and are cached per reality and hour.
- **Phones (up to 768 px).** One pane at a time under a sticky segmented switch (the hidden pane is not rendered, so only one canvas exists), plus the summary card.
- **Engineering Mode.** The summary adds both state hashes at the compared hour (`getBranchState`) and a note: identical is expected up to the divergence tick, different afterwards.
- Tests: `comparison.test.ts` (subtraction, common tick, divergence, advance request), `forkSequence.test.tsx` (state machine, skip, reduced motion), `studio-flow.test.tsx` (fork held until the server answers, compare, shared stepping and rewinding with no extra advance, waiting move, explicit advance request body, hashes).

Known limitations:

- Moves already made in a reality are session-tracked; the API has no inputs endpoint, so a forked child's inherited moves and any moves after a reload are unknown (unchanged).
- Switching the active reality inside a comparison lowers the shared hour if the new active reality has a lower head (the studio store clamps it).
- The sequence overlays the stage while the new reality loads behind it; a slow `ensureFrames` shows the usual "Updating…" state under the overlay.
- Divergence for unrelated or unloaded branches is unknown and hides the shared-history band and hash expectation.
- The `fork-sequence` animation was verified in tests (state machine, classes, timers), not by eye in a browser.


---

## 11. Verification results (Phase B-verify, real browser)

Run: Playwright 1.65.1 (exact-pinned), headless Chromium 159, macOS arm64. `npm run e2e` builds the web app with `NEXT_PUBLIC_API_BASE_URL=http://localhost:18080/api/v1`, starts the real Go API (in-memory store, port 18080, rate limit raised for the suite) and `next start` on port 3199, and stops both afterwards. No mocks. Projects: desktop 1440x900 and mobile 390x844 (Pixel 7 emulation, touch).

**Results (final run):** `npm run e2e` 8 of 8 passed (4 tests x 2 projects, 3.9 min). `npx tsc --noEmit`, `npx eslint .` clean; `vitest --run` 170 of 170 passed (21 files); `npm run build` passes; `cd backend && go test ./... -count=1` passes (all packages `ok`). The journey test also passed once against `next dev` (React StrictMode double mount, hydration checks) with zero console errors.

**What the tests cover**

- `e2e/journey.spec.ts`: landing shows title, premise and CTA; briefing contains no crisis words; setup renders; demo starts; canvas has more than 12 distinct colours and over 500 non-background pixels; clock reads "Thursday, 12:00 AM" and no "Tick" appears; decision card places and locks a move ("Takes effect at Thursday, 1:00 AM"); Play pauses "for your move"; skip to the supplier outage until a "... is down" card shows; plan reasons and the plan hint are absent in normal mode; rewinding on the time strip sends no non-GET request and no `/advance`; "Explore another future" plays the fork sequence with the right caption; the switcher lists both futures; "Compare futures" shows two painted canvases (desktop) and the compared cash difference equals the API subtraction; Engineering Mode shows tick number, a 64-hex state hash and the ledger, and turns off again. Throughout, console errors, page errors, failed requests and non-2xx API responses must be empty.
- `e2e/metrics.spec.ts`: header Cash, Profit so far, Orders delivered on time and Orders waiting equal `GET /branches/{id}/metrics?tick=` at hours 0, 20, 48, 60 and 120 (reached by real playback, then re-read by rewinding to the exact hour), and for the alternative reality at its head. After forking at hour 60 and playing the alternative to 72, every cell of the compared-differences table (both columns and the difference column for cash, profit, on time and waiting) equals the independently formatted API values and the subtraction of the two API values; on desktop the two pane figures also equal the API values. Expected strings come from a separate formatter in the test, not from `lib/glossary.ts`.
- `e2e/setup-and-perf.spec.ts`: a starting cash of 150000 in `/setup` sends a `POST /worlds` with overrides containing 15000000, the studio shows $150,000.00, and `GET /worlds/{id}` returns `openingCashCents` = 15000000. A second test records frame times (see `docs/benchmarks.md`: about 60 frames per second as measured by `requestAnimationFrame` in headless Chromium, 301 frames in 5 s on desktop, 302 on emulated mobile, in the final run; earlier runs had occasional single frames of 33 to 50 ms).

**Screenshots** (`docs/screenshots/`, `desktop-*` and `mobile-*`): `01-intro`, `02-briefing`, `03-setup`, `04-studio-start`, `05-studio-crisis` (stock-bound orders), `05b-studio-supplier-outage` (supplier outage, staff shortage and demand surge cues on the canvas), `06-decision-card`, `07-fork-sequence` (mid-animation, 500 ms in), `08-comparison`, `09-outcome` (rewound to the end of the week), `10-engineering-mode`. They are review artifacts, not pixel baselines.

**Defects found and fixed**

1. Phone: the bottom sheet opened at 60 % of the screen and hid the warehouse and the playback controls; the page had no room under it. Now the sheet content is capped at 30 vh, the toggle and tabs share one row, and the page gets bottom padding while the sheet is open.
2. Phone: the scene was drawn at about 0.26 scale (about 190 px tall) with unreadable labels. The scene is now at least 780 px wide inside a horizontally scrollable wrapper, centred on the racks (pan with a finger); the hotspots scale with it.
3. Phone: header figures were clipped in a sideways-scrolling row and took a third of the screen. They are now a compact two-column grid; the second reality switcher and the "Showing ..." badge are hidden while comparing on a phone; the decision-moment markers (a smear of overlapping circles on a 340 px strip) are hidden on a phone, where "Skip to next move" covers them.
4. Desktop: the plain-language scene summary was a visible bullet list under the canvas (a debug-console look that pushed the controls below the fold, and duplicated in both compare panes). It is now visually hidden but still read by assistive technology. The rail height is capped so the stage is not followed by a tall empty area.
5. Desktop: in comparison mode the header repeated the "Original path at this hour" and "Difference" lines already shown in the Observed differences table. They are hidden while comparing.
6. Intro: the title and tagline sat on top of the skyline. A left-to-right dark scrim now sits behind the text.
7. Setup: slider widths changed from row to row, and hints ran together ("... of opening cash Allowed: ..."). The control grid has fixed columns and hints are now capitalised sentences ending in a full stop.
8. Test-found: none of the above produced console errors; no hydration warnings, StrictMode double-mount or Pixi destroy errors were found (production build and `next dev`).

**Remaining defects and limits**

- Comparison panes on desktop draw each scene at about a third of its size, so the labels are too small to read; the racks, workers and docks are recognisable but the text is not. A larger compare layout (stacked panes, or a zoom) is not done.
- The phone scene labels are readable only after the sideways pan; the left and right docks are partly off screen until panned. There is no on-screen hint that the scene pans.
- The supplier-outage, staff and demand cues overlap each other at the busiest moment (several red tags on the canvas at once).
- The compared futures in the screenshots are identical because the alternative was advanced without a different decision; that is honest but makes the comparison screenshot look empty. The tests compare real values either way.
- At the horizon the clock reads "Tuesday, 12:00 AM" (tick 120, the state after the last simulated hour), while the week is described as ending on Monday.
- The time strip's right edge is a sub-pixel target on a phone; the test uses the End key for it.
- Frame times were measured in headless Chromium on a desktop-class Mac with no CPU throttling, so they say nothing about real phones. WebGL "GPU stall" notices from the headless driver appear as warnings and are not treated as failures.
- Not tested: Firefox and Safari, real touch devices, screen readers, `prefers-reduced-motion` in the browser, the Docker/PostgreSQL store.

## 11c. Scene label collisions: fixed, and what remains

Fixed (`web/features/world-view/scene/labelLayout.ts`, `sceneLabels.ts`, tests in `labelLayout.test.ts`): about nine red tags used to overlap during a supplier outage. Now:

- Disruptions are a stacked banner column along the top-left edge of the canvas, each with a warning icon. They are no longer floating at zone anchors, and "Dock closed" is not drawn twice (the dock hotspot tooltip still says it).
- The four "Waiting for inventory: N" sentences became a small numeric badge on each rack plus one aggregate tag ("Waiting for inventory: N orders"). Per-product sentences stay in the rack tooltips and the accessible summary.
- All other labels go through a deterministic greedy layout by priority (crisis, bottleneck, rack badges, stock, ambient). Each label tries a fixed order of offsets (with a thin leader line when moved) and shorter text variants, and is hidden only if nothing fits. The unit test builds the outage + surge + absence + four-stockout frame and asserts zero pairwise overlaps, and that every hidden label's facts appear in a hotspot tooltip or the accessible summary.
- No new animation; numbers are unchanged frame values.

Remaining:

- On a phone the scene is wider than the screen and starts scrolled to the centre, so the top-left banner column and "Absent: N" are off-screen until the user pans. The Alerts tab carries the same information.
- Labels are scene-pixel text, so they are small at desktop panel width (about 0.7x). Larger text would need more hiding.
- Layout avoids label-on-label overlap only; a label can still sit over rack art.

## 12. M4/M5 verification results (real browser, 2026-11-27)

Headless Chromium, desktop 1440x900 and phone 390x844, against the real Go API (in-memory store, port 18080) and a production build of the web app (port 3199). Full `npm run e2e`: 16 passed, 6 skipped (desktop-only specs on the phone project), 0 failed, 4.7 min.

| Spec | Desktop | Phone | Checks |
|---|---|---|---|
| `commands.spec.ts` | 1/1 | skipped | Order (backup), price, rush (PO placed at hour 0, rushed at hour 4), overtime, temp hire, marketing through their cards; each shows "Takes effect at …"; export inputs carry the kinds and fields; Labor_Expense lines for regular, overtime and temp wages, Marketing_Expense per hour and the $50 expedite Freight_Expense match the assumptions; plain-language rejection next to the price and staffing cards, and the marketing limit message before sending |
| `ledger-sse.spec.ts` | 4/4 | skipped | Profit so far and Cash drill-downs equal `ledger/summary` and the sum of every listed "Adds" line (all pages); the browser opens `/progress` as an EventSource and receives ticks 0..8 in order; with `/progress` aborted the client switches to "Polling", polls `/tick-frames` and every hour 0..8 stays viewable; an injected surprise shows its time-strip marker, survives a reload (via export) and its crisis card appears at its hour, not before |
| `duel.spec.ts` | 1/1 | 1/1 | Challenge from the briefing (Rule-based AI), both panes at one hour, AI decision withheld (UI and API) until Continue, then shown with "Rule-based AI" and "AI rationale (not verified)"; run to hour 120; every scoreboard number (composite, components, raw metrics, differences, outcome) equals `GET /duels/{id}/score`, which equals per-branch metrics; identical disruptions in both branches' frames and in both panes' descriptions; equal state hashes at the fork hour in Engineering Mode. No winner is asserted |
| `duel-llm-stub.spec.ts` | 1/1 | skipped | **Stub provider. Live LLM verification is pending (no runtime API key configured).** "LLM AI (stub-model)" offered and shown; windows 0, 4, 16 use the stub's proposal, window 8 (malformed reply) shows "Rule-based AI (LLM fallback: invalid reply)", window 12 (HTTP 500) "… service error"; the `<script>` and markdown-link rationale renders as literal text (no anchor, no alert); usage counters equal GET world and the stub's own request count; `stub-key-not-secret` appears in no API response, DOM, storage, console line or server log; reloading and re-reading decisions, export, state and frames make zero further stub requests |
| `journey`, `metrics`, `setup-and-perf` | 5/5 | 5/5 | Unchanged checks |

Approach for the stub spec: the spec builds the API to `/tmp/parallel-api-llm-e2e`, starts it on a free port with the stub's `LLM_*` variables, and rewrites the web build's API origin (`localhost:18080`) to that instance with `page.route`. No second web server is built. Both the stub and the second API are stopped in `afterAll`; after the run nothing listened on 3199 or 18080.

Defects found and fixed (minimal changes, no redesign):

- Duel Engineering Mode LLM counters went stale: the world was read once on open, so "LLM calls made" stayed at 1 while the API said 6. `DuelView` now re-reads the world when the head or the waiting state changes.
- Phone scoreboard: the "Observed differences" table overflowed the card (the profit difference was cut at the right edge). Both scoreboard tables now sit in a horizontal-scroll wrapper and use smaller text below 769 px.
- Duel rail stretched to the full page height with a large empty area under short tabs. The rail now aligns to its content and stays in view (sticky) on desktop.
- "Add a surprise" toggle: the label and the +/− sign were centred together ("Add a surprise−"). They now sit at opposite ends.
- Test race (not a product defect): `journey` and `metrics` read the branch id from the URL right after skipping the fork sequence, before the studio switches reality. They now wait for the URL to change. This was failing `metrics` reliably.

Observed and not changed:

- The duel panes are small at 1440 px (two scenes side by side); scene labels are legible only as shapes. Hover/tap tooltips and the text summary carry the figures.
- The timeline lane of decision-moment markers is dense across a full week; markers touch but do not overlap text.
- On the phone, the stage and the rail are long; the scoreboard is reached by scrolling past the controls. No overlap or clipping was seen after the fix.


## 13. M4 closure (task 5.7)

What exists at the M4 checkpoint:

- **Interpolation:** `web/features/world-view/scene/interpolate.ts` holds the easing, `pointOnPath`, `tweenProgress` and the `travelEnabled` gate (off under reduced motion, when `animate` is false, or when frames arrive less than 120 ms apart). `WorldScene` uses it for trucks, vans, boxes and tickets. It moves positions only. `interpolate.test.ts` builds the scene model, labels, tooltips and summary text at t = 0, 0.25, 0.5 and 1 and asserts every number equals the displayed Tick_Frame value (and that no in-between or previous-frame value appears).
- **Timeline input markers by provenance:** `features/timeline/markerStyle.tsx`. Each provenance has its own glyph, border style and label: Your move ●, Rule-based AI ▲, LLM AI ■, LLM fallback △ (dashed), Surprise you added +. The "?" popover next to the playback controls carries the legend. Tests in `Timeline.test.tsx` and `PlaybackControls.test.tsx`.
- **Scoreboard on phones:** difference and value cells no longer wrap (`whitespace-nowrap` inside the existing horizontal-scroll wrapper).
- **Discoverability:** at an open decision moment a dismissible one-line tip ("Want to try a different decision? Explore another future — rewind to any moment and branch") with a "Show me" button that does what the F key does. It sits above the playback strip on desktop and above the warehouse on phones. Dismissal is remembered in localStorage. The briefing shows Start Black Friday demo (primary), Challenge the AI manager (secondary) and Customize the business (tertiary link).
- **Disclaimers:** `features/disclaimers.test.tsx` covers intro, briefing, setup and the diff panel (also while loading or failed). Studio, duel, ledger and assumptions are asserted in their own tests. The diff panel now shows the disclaimer in every state.
- **Plain language:** `features/studio/plainLanguage.test.tsx` and a duel test scan crisis cards, the decision panel and the duel page for engine words in normal mode. No hits.
- **Controls:** `studio-flow.test.tsx` ("every playback control does something") clicks play/pause, the three paces, the auto-pause choice, previous/next moment, skip to next move, skip to next surprise, and the keyboard shortcuts, and asserts the requests and displayed hours.
- **CSP and PixiJS:** the production CSP forbids `unsafe-eval`, which stopped PixiJS from creating its canvas. `WorldView.tsx` now imports `pixi.js/unsafe-eval`.

Not done or still open: the comparison view has no dedicated disclaimer test (it renders the disclaimer, and the studio footer is always present). At 1440x900 the briefing's "Challenge the AI manager" button sits just below the fold; "Start Black Friday demo" is visible.
