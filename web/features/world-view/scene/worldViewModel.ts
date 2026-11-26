/**
 * Pure mapping from an authoritative TickFrame to the scene model the World_View
 * draws. No Pixi, no DOM, no randomness. Every figure and marker in the model is
 * copied or counted from the frame (or from the explicit `interventions` input).
 */
import type { ManagerKind, TickFrame } from "@/lib/api/client";

export type DisruptionFrame = TickFrame["disruptions"][number];
export type DisruptionKind = DisruptionFrame["kind"];

/** Zone of the diorama a Disruption highlights. */
export type ZoneId = "inbound_dock" | "storage_racks" | "pick_line" | "order_queue";

/** An operational change where a Command took effect, supplied by the page from the input log. */
export interface Intervention {
  /** Tick at which the command takes effect. */
  tick: number;
  /** Short description, for example "Order 400 units of SKU 2". */
  text: string;
  manager: ManagerKind;
  /** Model name for `ai_llm`. */
  model?: string;
  /** Present when an LLM window fell back to the rule-based policy. */
  fallbackReason?: string;
}

export interface SkuBin {
  sku: number;
  onHand: number;
  /** onHand relative to the display maximum, in basis points, clamped to 0..10000. */
  fillBps: number;
  /** Open orders left unshipped because stock was short. */
  stockBoundOrders: number;
}

export interface InboundGroup {
  sku: number;
  /** Open purchase orders for the SKU (one truck/arrow each, drawing capped). */
  pos: number;
  units: number;
}

export interface DisruptionMark {
  kind: DisruptionKind;
  label: string;
  zone: ZoneId;
  /** SKU index, or null when the Disruption has no SKU target. */
  targetSku: number | null;
  ticksSinceOnset: number;
  onsetTick: number;
  endTick: number;
  origin: DisruptionFrame["origin"];
}

export interface InterventionMark {
  tick: number;
  text: string;
  /** Provenance label, for example "Human" or "Rule-based AI". */
  provenance: string;
}

export interface WorldViewModel {
  tick: number;
  syncing: boolean;
  /** Display maximum used for every bin fill. */
  stockScaleMax: number;
  bins: SkuBin[];
  workers: { regular: number; temps: number; onboarding: number; onShift: number };
  orderQueueLen: number;
  inbound: InboundGroup[];
  disruptions: DisruptionMark[];
  bottlenecks: {
    stockBound: { sku: number; orders: number }[];
    stockBoundOrders: number;
    capacityBoundOrders: number;
    capacity: number;
    capacityUsed: number;
  };
  interventions: InterventionMark[];
  /** Zones with an active Disruption, ascending by zone name. */
  highlightedZones: ZoneId[];
}

export const DISRUPTION_ZONE: Record<DisruptionKind, ZoneId> = {
  demand_surge: "order_queue",
  supplier_outage: "inbound_dock",
  workforce_absence: "pick_line",
};

export const DISRUPTION_LABEL: Record<DisruptionKind, string> = {
  demand_surge: "Demand surge",
  supplier_outage: "Supplier outage",
  workforce_absence: "Workforce absence",
};

export function provenanceLabel(i: Pick<Intervention, "manager" | "model" | "fallbackReason">): string {
  switch (i.manager) {
    case "human":
      return "Human";
    case "ai_rule_based":
      return "Rule-based AI";
    case "ai_llm":
      if (i.fallbackReason !== undefined && i.fallbackReason !== "") {
        return `Rule-based AI (LLM fallback: ${i.fallbackReason})`;
      }
      return i.model !== undefined && i.model !== "" ? `LLM AI (${i.model})` : "LLM AI";
  }
}

/**
 * Display maximum for stock bins. A bin is full when its SKU holds the largest
 * on-hand quantity seen in the frames the caller passes (for example the opening
 * Tick through the displayed Tick). With no data it is 1, so every bin is empty.
 */
export function maxOnHandSeen(frames: readonly TickFrame[]): number {
  let max = 0;
  for (const f of frames) for (const s of f.skus) if (s.onHand > max) max = s.onHand;
  return max;
}

export interface BuildOptions {
  /** Display maximum for bin fill. Defaults to the largest on-hand in this frame. */
  stockScaleMax?: number;
  interventions?: readonly Intervention[];
  syncing?: boolean;
}

export function buildWorldViewModel(frame: TickFrame, opts: BuildOptions = {}): WorldViewModel {
  const frameMax = maxOnHandSeen([frame]);
  const requested = opts.stockScaleMax ?? frameMax;
  // Never scale below what this frame holds, so a bin cannot overflow.
  const stockScaleMax = Math.max(1, requested, frameMax);

  const constraintBySku = new Map(frame.report.skus.map((c) => [c.sku, c]));

  const bins: SkuBin[] = [...frame.skus]
    .sort((a, b) => a.sku - b.sku)
    .map((s) => ({
      sku: s.sku,
      onHand: s.onHand,
      fillBps: Math.min(10000, Math.max(0, Math.floor((s.onHand * 10000) / stockScaleMax))),
      stockBoundOrders: constraintBySku.get(s.sku)?.stockBoundOrders ?? 0,
    }));

  const inbound: InboundGroup[] = [...frame.skus]
    .sort((a, b) => a.sku - b.sku)
    .filter((s) => s.inboundPOs > 0)
    .map((s) => ({ sku: s.sku, pos: s.inboundPOs, units: s.inboundUnits }));

  const disruptions: DisruptionMark[] = frame.disruptions.map((d) => ({
    kind: d.kind,
    label: DISRUPTION_LABEL[d.kind],
    zone: DISRUPTION_ZONE[d.kind],
    targetSku: d.target >= 0 ? d.target : null,
    ticksSinceOnset: d.ticksSinceOnset,
    onsetTick: d.onsetTick,
    endTick: d.endTick,
    origin: d.origin,
  }));

  const stockBound = frame.report.skus
    .filter((c) => c.stockBoundOrders > 0)
    .map((c) => ({ sku: c.sku, orders: c.stockBoundOrders }));
  let capacityBoundOrders = 0;
  for (const c of frame.report.skus) capacityBoundOrders += c.capacityBoundOrders;

  const interventions: InterventionMark[] = (opts.interventions ?? [])
    .filter((i) => i.tick === frame.tick)
    .map((i) => ({ tick: i.tick, text: i.text, provenance: provenanceLabel(i) }));

  const zones = new Set<ZoneId>(disruptions.map((d) => d.zone));

  return {
    tick: frame.tick,
    syncing: opts.syncing ?? false,
    stockScaleMax,
    bins,
    workers: {
      regular: frame.workersOnShift,
      temps: frame.tempsOnShift,
      onboarding: frame.tempsOnboarding,
      onShift: frame.workersOnShift + frame.tempsOnShift,
    },
    orderQueueLen: frame.orderQueueLen,
    inbound,
    disruptions,
    bottlenecks: {
      stockBound,
      stockBoundOrders: stockBound.reduce((n, s) => n + s.orders, 0),
      capacityBoundOrders,
      capacity: frame.report.capacity,
      capacityUsed: frame.report.capacityUsed,
    },
    interventions,
    highlightedZones: [...zones].sort(),
  };
}
