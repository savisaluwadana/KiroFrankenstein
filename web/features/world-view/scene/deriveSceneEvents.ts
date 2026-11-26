/**
 * Pure derivation of scene events from two authoritative frames of the same
 * branch (docs/ux-redesign.md section 1.3). An event exists only when a specific
 * field or delta justifies it. Nothing here reads a clock, a random source or
 * the DOM. Final counts on screen never depend on these events; they decorate
 * the transition between two states the frames already hold.
 */
import type { TickFrame } from "@/lib/api/client";
import { MAX_MOVING_BOXES, MAX_NEW_TICKETS, MAX_TRUCKS, MAX_VANS, ORDERS_PER_VAN, UNITS_PER_BOX } from "./layout";
import type { DisruptionKind } from "./worldViewModel";

export type SceneEvent =
  /** `trucks` purchase orders for the product were received (lower bound, see 1.3). */
  | { kind: "delivery"; sku: number; trucks: number; hiddenTrucks: number; boxes: number }
  /** Orders shipped this hour: exact order count, vans are display bundles of ORDERS_PER_VAN. */
  | { kind: "shipment"; orders: number; vans: number; hiddenVans: number }
  /** Boxes leave a rack. `exact` is false when the product is a fallback pick (see 1.3). */
  | { kind: "stock_picked"; sku: number; units: number; boxes: number; exact: boolean }
  /** At least `atLeast` new orders arrived (cancellations are not exposed); `drawn` tickets slide in. */
  | { kind: "orders_arrived"; atLeast: number; drawn: number }
  | { kind: "disruption_onset"; disruption: DisruptionKind; targetSku: number | null };

const CAP_BOXES = (units: number): number => Math.min(MAX_MOVING_BOXES, Math.max(1, Math.ceil(units / UNITS_PER_BOX)));

function disruptionKey(d: TickFrame["disruptions"][number]): string {
  return `${d.kind}|${d.inputSeq}|${d.onsetTick}|${d.target}`;
}

/**
 * Events between the previously displayed frame and the next one. Returns an
 * empty list unless `next` is the tick directly after `prev` (scrubbing,
 * jumping and backwards moves snap without travel).
 */
export function deriveSceneEvents(prev: TickFrame | null | undefined, next: TickFrame): SceneEvent[] {
  if (prev === null || prev === undefined || next.tick !== prev.tick + 1) return [];
  const events: SceneEvent[] = [];
  const prevBySku = new Map(prev.skus.map((s) => [s.sku, s]));
  const skus = [...next.skus].sort((a, b) => a.sku - b.sku);

  // Deliveries: a purchase order disappeared from the open list.
  let trucksLeft = MAX_TRUCKS;
  for (const s of skus) {
    const p = prevBySku.get(s.sku);
    if (p === undefined || s.inboundPOs >= p.inboundPOs) continue;
    const received = p.inboundPOs - s.inboundPOs;
    const shown = Math.min(received, trucksLeft);
    trucksLeft -= shown;
    const unitsIn = s.onHand > p.onHand ? s.onHand - p.onHand : Math.max(0, p.inboundUnits - s.inboundUnits);
    events.push({
      kind: "delivery",
      sku: s.sku,
      trucks: shown,
      hiddenTrucks: received - shown,
      boxes: Math.min(MAX_MOVING_BOXES, Math.max(3, Math.ceil(unitsIn / UNITS_PER_BOX))),
    });
  }

  // Shipments: vans from the report, boxes from on-hand decreases.
  const shipped = next.report.shippedOrders;
  if (shipped > 0) {
    const vans = Math.ceil(shipped / ORDERS_PER_VAN);
    events.push({ kind: "shipment", orders: shipped, vans: Math.min(MAX_VANS, vans), hiddenVans: Math.max(0, vans - MAX_VANS) });

    const decreases = skus
      .map((s) => ({ s, p: prevBySku.get(s.sku) }))
      .filter((x) => x.p !== undefined && x.s.onHand < x.p.onHand)
      .map((x) => ({
        sku: x.s.sku,
        units: (x.p?.onHand ?? 0) - x.s.onHand,
        inboundChanged: x.p !== undefined && (x.p.inboundPOs !== x.s.inboundPOs || x.p.inboundUnits !== x.s.inboundUnits),
      }));
    const exact = decreases.filter((d) => !d.inboundChanged);
    for (const d of exact) events.push({ kind: "stock_picked", sku: d.sku, units: d.units, boxes: CAP_BOXES(d.units), exact: true });
    if (exact.length === 0 && decreases.length > 0) {
      // Ambiguous hour: take the largest decrease (lowest SKU index on ties).
      let best = decreases[0];
      for (const d of decreases) if (best !== undefined && d.units > best.units) best = d;
      if (best !== undefined) {
        events.push({ kind: "stock_picked", sku: best.sku, units: best.units, boxes: CAP_BOXES(best.units), exact: false });
      }
    }
  }

  // New orders: queue growth plus what shipped. Lower bound only.
  const atLeast = Math.max(0, next.orderQueueLen - prev.orderQueueLen + shipped);
  if (atLeast > 0) events.push({ kind: "orders_arrived", atLeast, drawn: Math.min(MAX_NEW_TICKETS, atLeast) });

  // Disruption onsets visible in this frame and absent from the previous one.
  const before = new Set(prev.disruptions.map(disruptionKey));
  for (const d of next.disruptions) {
    if (d.ticksSinceOnset === 0 && !before.has(disruptionKey(d))) {
      events.push({ kind: "disruption_onset", disruption: d.kind, targetSku: d.target >= 0 ? d.target : null });
    }
  }
  return events;
}
