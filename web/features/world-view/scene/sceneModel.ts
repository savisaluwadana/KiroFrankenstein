/**
 * Pure mapping from an authoritative TickFrame (plus the previously displayed
 * frame) to everything the warehouse scene draws and says. No Pixi, no DOM, no
 * randomness. Every number is copied or counted from the frame; caps always
 * come with an exact "+N" remainder.
 */
import type { TickFrame } from "@/lib/api/client";
import { deriveSceneEvents, type SceneEvent } from "./deriveSceneEvents";
import {
  boxesFor,
  MAX_ABSENT_DRAWN,
  MAX_FIGURES,
  MAX_PRODUCTS,
  MAX_TICKETS,
  MAX_TRAINING_DRAWN,
  MAX_VANS,
  ORDERS_PER_VAN,
  rackHotspotRect,
  RACK_SLOTS,
  stationFill,
  zoneHotspotRect,
  ZONES,
  type Hotspot,
} from "./layout";
import {
  buildWorldViewModel,
  DISRUPTION_ZONE,
  type DisruptionKind,
  type Intervention,
  type InterventionMark,
  type WorldViewModel,
  type ZoneId,
} from "./worldViewModel";

export interface SceneOptions {
  /** Regular workers the business employs (assumption `workforce.regularWorkers`). Enables "absent" figures. */
  regularWorkers?: number;
  interventions?: readonly Intervention[];
  syncing?: boolean;
}

export interface RackModel {
  sku: number;
  name: string;
  /** Index into the box colour/glyph tables. */
  styleIndex: number;
  onHand: number;
  boxes: number;
  moreBoxes: number;
  /** Open orders left unshipped because this product was short. */
  waitingForInventory: number;
  inboundPOs: number;
  inboundUnits: number;
}

export interface SceneDisruption {
  kind: DisruptionKind;
  zone: ZoneId;
  title: string;
  /** Plain-language line, for example "Chargers supplier is down". */
  text: string;
  targetSku: number | null;
}

export interface SceneModel {
  tick: number;
  syncing: boolean;
  racks: RackModel[];
  /** Products beyond MAX_PRODUCTS that are not drawn (exact remainder). */
  hiddenProducts: number;
  people: {
    onShift: number;
    regular: number;
    temps: number;
    drawnRegular: number;
    drawnTemps: number;
    hiddenOnFloor: number;
    perStation: number[];
    onboarding: number;
    onboardingDrawn: number;
    /** Regular workers missing from the floor, or null when the business size is unknown. */
    absent: number | null;
    absentDrawn: number;
  };
  queue: { length: number; tickets: number; more: number };
  dock: { openPOs: number; openUnits: number; closed: boolean };
  outbound: { shippedOrders: number; vans: number; hiddenVans: number };
  bottlenecks: {
    waitingForInventory: { sku: number; name: string; orders: number }[];
    waitingForInventoryOrders: number;
    waitingForCapacityOrders: number;
    capacity: number;
    capacityUsed: number;
  };
  disruptions: SceneDisruption[];
  interventions: InterventionMark[];
  events: SceneEvent[];
  hotspots: SceneHotspot[];
}

export interface SceneHotspot extends Hotspot {
  /** Accessible name and tooltip text (plain language, authoritative number included). */
  label: string;
  tooltip: string;
}

export function productName(productNames: readonly string[] | undefined, sku: number): string {
  const n = productNames?.[sku];
  return n !== undefined && n.trim() !== "" ? n : `Product ${sku + 1}`;
}

const DAYS = ["Thursday", "Friday", "Saturday", "Sunday", "Monday", "Tuesday", "Wednesday"];

/** "Thursday 7:00 PM" for an hourly tick, with tick 0 at Thursday 00:00. */
export function clockLabel(tick: number): string {
  const day = DAYS[Math.floor(tick / 24) % 7] ?? "Thursday";
  const h = tick % 24;
  const h12 = h % 12 === 0 ? 12 : h % 12;
  return `${day} ${h12}:00 ${h < 12 ? "AM" : "PM"}`;
}

const plural = (n: number, one: string, many = `${one}s`): string => `${n} ${n === 1 ? one : many}`;

export function buildSceneModel(
  frame: TickFrame,
  prevFrame: TickFrame | null | undefined,
  productNames?: readonly string[],
  opts: SceneOptions = {},
): SceneModel {
  const viewOpts: Parameters<typeof buildWorldViewModel>[1] = {};
  if (opts.syncing !== undefined) viewOpts.syncing = opts.syncing;
  if (opts.interventions !== undefined) viewOpts.interventions = opts.interventions;
  const view: WorldViewModel = buildWorldViewModel(frame, viewOpts);

  const sorted = [...frame.skus].sort((a, b) => a.sku - b.sku);
  const waitingBySku = new Map(frame.report.skus.map((c) => [c.sku, c.stockBoundOrders]));
  const racks: RackModel[] = sorted.slice(0, MAX_PRODUCTS).map((s) => ({
    sku: s.sku,
    name: productName(productNames, s.sku),
    styleIndex: s.sku,
    onHand: s.onHand,
    ...boxesFor(s.onHand),
    waitingForInventory: waitingBySku.get(s.sku) ?? 0,
    inboundPOs: s.inboundPOs,
    inboundUnits: s.inboundUnits,
  }));

  const regular = Math.max(0, frame.workersOnShift);
  const temps = Math.max(0, frame.tempsOnShift);
  const drawnRegular = Math.min(regular, MAX_FIGURES);
  const drawnTemps = Math.min(temps, MAX_FIGURES - drawnRegular);
  const absent = opts.regularWorkers === undefined ? null : Math.max(0, opts.regularWorkers - regular);

  const disruptions: SceneDisruption[] = frame.disruptions.map((d) => {
    const target = d.target >= 0 ? productName(productNames, d.target) : null;
    const base = {
      kind: d.kind,
      zone: DISRUPTION_ZONE[d.kind],
      targetSku: d.target >= 0 ? d.target : null,
    } as const;
    switch (d.kind) {
      case "supplier_outage":
        return { ...base, title: "Supplier outage", text: target === null ? "A supplier is down. Deliveries are paused." : `The ${target} supplier is down. Deliveries are paused.` };
      case "demand_surge":
        return { ...base, title: "Demand surge", text: target === null ? "Orders are arriving faster than usual." : `Orders for ${target} are arriving faster than usual.` };
      case "workforce_absence":
        return { ...base, title: "Staff shortage", text: "Some of the regular team is absent." };
    }
  });

  const waitingForInventory = [...frame.report.skus]
    .filter((c) => c.stockBoundOrders > 0)
    .map((c) => ({ sku: c.sku, name: productName(productNames, c.sku), orders: c.stockBoundOrders }));
  const vansTotal = Math.ceil(frame.report.shippedOrders / ORDERS_PER_VAN);

  const model: SceneModel = {
    tick: frame.tick,
    syncing: opts.syncing ?? false,
    racks,
    hiddenProducts: Math.max(0, sorted.length - MAX_PRODUCTS),
    people: {
      onShift: regular + temps,
      regular,
      temps,
      drawnRegular,
      drawnTemps,
      hiddenOnFloor: regular + temps - drawnRegular - drawnTemps,
      perStation: stationFill(drawnRegular + drawnTemps),
      onboarding: frame.tempsOnboarding,
      onboardingDrawn: Math.min(frame.tempsOnboarding, MAX_TRAINING_DRAWN),
      absent,
      absentDrawn: Math.min(absent ?? 0, MAX_ABSENT_DRAWN),
    },
    queue: {
      length: frame.orderQueueLen,
      tickets: Math.min(MAX_TICKETS, frame.orderQueueLen),
      more: Math.max(0, frame.orderQueueLen - MAX_TICKETS),
    },
    dock: {
      openPOs: sorted.reduce((n, s) => n + s.inboundPOs, 0),
      openUnits: sorted.reduce((n, s) => n + s.inboundUnits, 0),
      closed: frame.disruptions.some((d) => d.kind === "supplier_outage"),
    },
    outbound: {
      shippedOrders: frame.report.shippedOrders,
      vans: Math.min(MAX_VANS, vansTotal),
      hiddenVans: Math.max(0, vansTotal - MAX_VANS),
    },
    bottlenecks: {
      waitingForInventory,
      waitingForInventoryOrders: waitingForInventory.reduce((n, s) => n + s.orders, 0),
      waitingForCapacityOrders: view.bottlenecks.capacityBoundOrders,
      capacity: frame.report.capacity,
      capacityUsed: frame.report.capacityUsed,
    },
    disruptions,
    interventions: view.interventions,
    events: deriveSceneEvents(prevFrame, frame),
    hotspots: [],
  };
  model.hotspots = buildHotspots(model);
  return model;
}

function buildHotspots(m: SceneModel): SceneHotspot[] {
  const out: SceneHotspot[] = [];
  m.racks.forEach((r, k) => {
    const extra: string[] = [];
    if (r.moreBoxes > 0) extra.push(`The picture shows ${RACK_SLOTS} boxes and ${r.moreBoxes} more.`);
    if (r.waitingForInventory > 0) extra.push(`Waiting for inventory: ${plural(r.waitingForInventory, "order")}.`);
    if (r.inboundPOs > 0) extra.push(`${plural(r.inboundPOs, "delivery", "deliveries")} on the way (${r.inboundUnits} units).`);
    out.push({
      id: `rack-${r.sku}`,
      kind: "rack",
      label: `${r.name} shelves`,
      tooltip: [`${r.name}: ${r.onHand} in stock`, ...extra].join(" "),
      rect: rackHotspotRect(k, m.racks.length),
    });
  });
  const b = m.bottlenecks;
  out.push({
    id: "packing",
    kind: "packing",
    label: "Packing stations",
    tooltip:
      `Packing: ${plural(m.people.onShift, "person", "people")} on shift. ` +
      `${b.capacityUsed} of ${b.capacity} units packed this hour.` +
      (b.waitingForCapacityOrders > 0 ? ` Waiting for packing capacity: ${plural(b.waitingForCapacityOrders, "order")}.` : ""),
    rect: zoneHotspotRect(ZONES.packing, 1.6),
  });
  out.push({
    id: "dock",
    kind: "dock",
    label: "Loading dock",
    tooltip: m.dock.closed
      ? `Loading dock closed: supplier outage. ${plural(m.dock.openPOs, "delivery", "deliveries")} still on order (${m.dock.openUnits} units).`
      : m.dock.openPOs === 0
        ? "Loading dock: no deliveries on order."
        : `Loading dock: ${plural(m.dock.openPOs, "delivery", "deliveries")} on the way (${m.dock.openUnits} units).`,
    rect: zoneHotspotRect(ZONES.inbound, 1.6),
  });
  out.push({
    id: "orders",
    kind: "orders",
    label: "Order line",
    tooltip: `Orders waiting: ${m.queue.length}.` + (m.queue.more > 0 ? ` The board shows ${MAX_TICKETS} tickets and ${m.queue.more} more.` : ""),
    rect: zoneHotspotRect({ gx: 16.4, gy: 9.4, w: 5.4, h: 1 }, 2.6),
  });
  out.push({
    id: "outbound",
    kind: "outbound",
    label: "Delivery vans",
    tooltip:
      m.outbound.shippedOrders === 0
        ? "Outbound dock: nothing shipped this hour."
        : `Outbound dock: ${plural(m.outbound.shippedOrders, "order")} shipped this hour in ${plural(m.outbound.vans + m.outbound.hiddenVans, "van")} (each van stands for up to ${ORDERS_PER_VAN} orders).`,
    rect: zoneHotspotRect(ZONES.outbound, 1.6),
  });
  if (m.people.onboarding > 0 || (m.people.absent ?? 0) > 0) {
    const parts: string[] = [];
    if (m.people.onboarding > 0) parts.push(`${m.people.onboarding} temporary ${m.people.onboarding === 1 ? "worker" : "workers"} in training`);
    if ((m.people.absent ?? 0) > 0) parts.push(`${m.people.absent} regular ${m.people.absent === 1 ? "worker" : "workers"} absent`);
    out.push({
      id: "break",
      kind: "break",
      label: "Break and training area",
      tooltip: `${parts.join("; ")}.`,
      rect: zoneHotspotRect(ZONES.breakArea, 1.2),
    });
  }
  return out;
}

/** Plain-language lines for the accessible summary of the displayed tick. */
export function describeScene(m: SceneModel): { headline: string; lines: string[]; disruptions: string[]; interventions: string[] } {
  const p = m.people;
  const lines: string[] = [];
  lines.push(`${plural(p.onShift, "person", "people")} on shift${p.temps > 0 ? ` (${p.temps} temporary)` : ""}.`);
  if (p.onboarding > 0) lines.push(`${p.onboarding} temporary ${p.onboarding === 1 ? "worker is" : "workers are"} in training.`);
  if (p.absent !== null && p.absent > 0) lines.push(`${p.absent} regular ${p.absent === 1 ? "worker is" : "workers are"} absent.`);
  lines.push(`${plural(m.queue.length, "order")} waiting.`);
  const stock = m.racks.map((r) => `${r.name} ${r.onHand}`).join(", ");
  if (stock !== "") lines.push(`In stock: ${stock}.`);
  if (m.hiddenProducts > 0) lines.push(`${plural(m.hiddenProducts, "more product")} not drawn.`);
  if (m.dock.openPOs > 0) {
    const per = m.racks.filter((r) => r.inboundPOs > 0).map((r) => `${r.name} ${r.inboundUnits} units`);
    lines.push(`${plural(m.dock.openPOs, "delivery", "deliveries")} on the way (${per.join(", ")}).`);
  } else {
    lines.push("No deliveries on order.");
  }
  lines.push(
    m.outbound.shippedOrders > 0
      ? `${plural(m.outbound.shippedOrders, "order")} shipped this hour.`
      : "Nothing shipped this hour.",
  );
  for (const w of m.bottlenecks.waitingForInventory) lines.push(`Waiting for inventory: ${w.name}, ${plural(w.orders, "order")}.`);
  if (m.bottlenecks.waitingForCapacityOrders > 0) {
    lines.push(`Waiting for packing capacity: ${plural(m.bottlenecks.waitingForCapacityOrders, "order")}.`);
  }
  const atLeast = m.events.find((e) => e.kind === "orders_arrived");
  if (atLeast?.kind === "orders_arrived") lines.push(`At least ${plural(atLeast.atLeast, "new order")} came in this hour.`);
  return {
    headline: `${clockLabel(m.tick)}.`,
    lines,
    disruptions: m.disruptions.map((d) => `${d.title}: ${d.text}`),
    interventions: m.interventions.map((i) => `${i.provenance}: ${i.text}`),
  };
}
