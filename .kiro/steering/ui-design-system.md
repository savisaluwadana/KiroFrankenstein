---
inclusion: fileMatch
fileMatchPattern: "web/**"
---

# UI design system: "Night Shift Operations"

Parallel should feel like a tabletop strategy game crossed with a control room at 2 a.m. on Black Friday: a living diorama of the warehouse in the center, a timeline you can scrub like a video editor, and reality branches you can see split apart. It must not look like a generic AI analytics dashboard.

## Layout

- **Center stage:** PixiJS warehouse diorama (2D, slight top-down/isometric). Zones: inbound dock, storage racks, pick/pack line, outbound dock, order queue.
- **Bottom:** timeline scrubber across the full scenario horizon with tick marks per hour, day bands, disruption markers, decision-window markers and fork points.
- **Left rail:** branch tree (React Flow), each branch a "reality thread" with its color.
- **Right rail:** context panel: decision window and command builder, or the ledger drill-down, or assumptions.
- **Top HUD:** sim clock (day and hour), cash, net profit, service level, backlog. Compact, monospaced, never a grid of KPI cards.
- **Duel mode:** two dioramas side by side with a shared timeline, a central scoreboard spine.

## Visual language

- Background ink `#0B0E14`, surface `#121722`, line `#232B3A`, text `#E6E9EF`, muted text `#8A93A6`.
- Reality colors: Human branch amber `#F2A93B`, AI branch cyan `#3BC9DB`, other forks drawn from a fixed palette of 6 distinguishable hues. Disruption signal red `#FF5C5C`. Money gain `#7BD88F`, loss `#FF7A7A`.
- Typography: IBM Plex Sans for UI, IBM Plex Mono with tabular numerals for every number, sim clock and hash.
- Shapes: 4 px radius, 1 px lines, no drop shadows except the active branch glow in the diorama.
- Iconography: simple line icons (Lucide). No sparkles, robot heads or emoji.

## Authoritative-state rules (principle 10)

- Every quantity encoded visually (box stacks per SKU, worker count on the floor, order queue length, trucks at the dock, HUD figures) comes from the server state of the displayed tick.
- Motion between two ticks is interpolation for display only. Moving workers and conveyor items are decorative and never encode a number that differs from the HUD.
- While data for a tick is loading, show the previous tick dimmed with a "syncing" label; never show estimated numbers.
- Every money figure is clickable and opens the ledger lines that sum to it.
- AI rationale text is labelled "AI rationale (not verified)". Its effects are shown from calculated state only.
- AI kinds are always named: "Rule-based AI" and "LLM AI (<model>)"; a fallback window shows "Rule-based AI (LLM fallback: <reason>)". Never a generic "AI" label alone.
- The diorama shows operational state changes: stock-bound SKUs and a capacity-bound pick line (from the engine's fulfilment constraint report), intervention markers where a command takes effect, disruption highlights on the affected zone, and for a forked branch the parent's profit, service level and backlog at the same tick in the HUD.
- Every results view (world view, duel, ledger, branch diff) shows the disclaimer: "Synthetic simulation output under the stated assumptions. Not a forecast or guarantee of real-world results." Assumptions are one click away from each of these views.
- Branch diff wording says "observed differences", never "caused by", and carries the note that attributing an effect needs a fork that changes one decision.
- The disruption script stays hidden by default; reveal it only through an explicit control or after the horizon.

## Motion

- Durations 120 to 240 ms, ease-out. Tick playback controls: pause, 1x, 4x, 16x (2, 8, 32 ticks per second), step one tick, step one decision window. Playback waits for authoritative tick frames and never changes results. Auto-pause defaults to every decision window; "disruption onsets only" is available for fast demos.
- Signature moments (priority polish after the MVP): the fork split in the branch tree (up to 600 ms) and the duel scoreboard reveal (up to 1200 ms). They animate position, opacity and scale only; numbers appear at their final authoritative values, never as count-ups.
- Disruption onset: a red edge pulse on the affected zone plus a timeline marker; no full-screen effects.
- Respect `prefers-reduced-motion`: disable interpolation, pulses and signature animations, keep instant state changes.

## Anti-patterns (reject in review)

- Purple-to-blue gradients, glassmorphism cards, glowing chat bubbles.
- A dashboard home made of KPI cards with sparklines.
- Chat-first interaction with the AI. The AI is a competitor that submits commands, not a chatbot.
- Default shadcn styling left unthemed.
- Numbers rendered without units or in proportional fonts.

## Accessibility

- WCAG 2.2 AA contrast for text. Color is never the only encoding: branches also carry a label and a line pattern.
- Keyboard: Space play/pause, Left/Right step one tick, Shift+Left/Right step one decision window, F fork at current tick.
- The diorama has an accessible text summary of the displayed tick.
