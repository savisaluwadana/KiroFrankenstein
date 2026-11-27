/**
 * Crisis cards: plain-language alerts derived ONLY from authoritative data of
 * the displayed hour (the frame, and the observation when it is for that same
 * hour). Pure functions with fixed templates. Nothing here looks at later
 * hours, so a card can never preview an event that has not happened yet, and
 * "why" lines state only mechanics the data supports.
 */
import type { Observation, TickFrame } from "@/lib/api/client";
import { formatCount, formatSimTime, productName, supplierRoleName } from "@/lib/glossary";

export type SupplierRole = "primary" | "backup";

/** A real button on a card: opens a decision card prefilled. */
export interface CrisisAction {
  id: string;
  label: string;
  kind: "order" | "price" | "overtime" | "hire";
  skuIndex: number;
  role?: SupplierRole;
}

export interface CrisisCard {
  id: string;
  kind: "disruption" | "stock" | "capacity" | "late_delivery";
  title: string;
  /** What happened. */
  what: string;
  /** Why it happened, from known mechanics. */
  why: string;
  /** What is affected; empty when the data has nothing to say. */
  affected: string;
  actions: CrisisAction[];
}

export interface CrisisInput {
  frame: TickFrame;
  /** Only used when `observation.tick === frame.tick`. */
  observation: Observation | null;
  /** Product display names by SKU index. */
  productNames: readonly string[];
  regularWorkers?: number | undefined;
  startClock: string;
  /** True when a decision window is open at the displayed hour, so actions are real. */
  canAct: boolean;
}

function nameOf(names: readonly string[], sku: number): string {
  return names[sku] ?? productName(`product ${sku + 1}`);
}

function orders(n: number): string {
  return `${formatCount(n)} ${n === 1 ? "order" : "orders"}`;
}

/** Cheapest available supplier role for a product, avoiding `avoid`, or null. */
function availableRole(obs: Observation | null, sku: number, avoid?: SupplierRole): SupplierRole | null {
  const s = obs?.skus.find((x) => x.sku === sku);
  if (s === undefined) return null;
  const options = s.suppliers.filter((x) => x.available && x.role !== avoid);
  // Prefer the normal supplier when it is usable.
  const pick = options.find((x) => x.role === "primary") ?? options[0];
  return pick?.role ?? null;
}

function orderAction(names: readonly string[], sku: number, role: SupplierRole): CrisisAction {
  return {
    id: `order-${sku}-${role}`,
    label: role === "backup" ? "Order from the backup supplier" : `Order more ${nameOf(names, sku)}`,
    kind: "order",
    skuIndex: sku,
    role,
  };
}

export function deriveCrisisCards(input: CrisisInput): CrisisCard[] {
  const { frame, productNames: names, startClock } = input;
  const obs = input.observation !== null && input.observation.tick === frame.tick ? input.observation : null;
  const at = (tick: number): string => formatSimTime(tick, startClock);
  const stockBound = new Map(frame.report.skus.map((s) => [s.sku, s]));
  const cards: CrisisCard[] = [];

  // Disruptions that have started by the displayed hour and are still running.
  for (const d of frame.disruptions) {
    if (d.onsetTick > frame.tick || frame.tick >= d.endTick) continue;
    const obsD = obs?.disruptions.find((o) => o.inputSeq === d.inputSeq);
    const product = d.target >= 0 ? nameOf(names, d.target) : null;
    if (d.kind === "supplier_outage") {
      const role = obsD?.supplierRole;
      const who = role !== undefined ? supplierRoleName(role).toLowerCase() : "supplier";
      const waiting = d.target >= 0 ? (stockBound.get(d.target)?.stockBoundOrders ?? 0) : 0;
      const actions: CrisisAction[] = [];
      if (input.canAct && obs !== null) {
        const targets = d.target >= 0 ? [d.target] : obs.skus.map((s) => s.sku);
        for (const sku of targets) {
          const alt = availableRole(obs, sku, role);
          if (alt !== null) actions.push(orderAction(names, sku, alt));
        }
      }
      cards.push({
        id: `disruption-${d.inputSeq}`,
        kind: "disruption",
        title: role !== undefined ? `${supplierRoleName(role)} is down` : "A supplier is down",
        what: `Your ${who} can't ship${product !== null ? ` ${product}` : ""} right now.`,
        why: `The ${who} cannot ship until ${at(d.endTick)}.`,
        affected:
          waiting > 0
            ? `${orders(waiting)} ${waiting === 1 ? "is" : "are"} waiting for ${product ?? "stock"}.`
            : "Deliveries from this supplier are paused.",
        actions: actions.slice(0, 2),
      });
    } else if (d.kind === "demand_surge") {
      const actions: CrisisAction[] = [];
      if (input.canAct && obs !== null) {
        const sku = d.target >= 0 ? d.target : (obs.skus[0]?.sku ?? -1);
        if (sku >= 0) {
          actions.push({ id: `price-${sku}`, label: `Change ${nameOf(names, sku)} price`, kind: "price", skuIndex: sku });
        }
      }
      cards.push({
        id: `disruption-${d.inputSeq}`,
        kind: "disruption",
        title: `Orders are surging${product !== null ? ` for ${product}` : " across the store"}`,
        what: "Far more customers are buying than usual.",
        why: "Demand jumped well above the normal level.",
        affected: `${orders(frame.orderQueueLen)} ${frame.orderQueueLen === 1 ? "is" : "are"} waiting.`,
        actions,
      });
    } else {
      const regular = input.regularWorkers;
      const absent = regular !== undefined ? Math.max(0, regular - frame.workersOnShift) : null;
      cards.push({
        id: `disruption-${d.inputSeq}`,
        kind: "disruption",
        title: "Some of your team can't make it",
        what:
          absent !== null && absent > 0 && regular !== undefined
            ? `${formatCount(absent)} of ${formatCount(regular)} packers are out.`
            : "Some of your packers are out.",
        why: "Team members are absent, so there are fewer people packing orders.",
        affected: `The team can pack ${formatCount(frame.report.capacity)} items an hour.`,
        actions: [],
      });
    }
  }

  // Orders that cannot ship because stock ran short.
  for (const s of frame.report.skus) {
    if (s.stockBoundOrders <= 0) continue;
    const product = nameOf(names, s.sku);
    const inbound = frame.skus.find((x) => x.sku === s.sku)?.inboundUnits ?? 0;
    const role = input.canAct ? availableRole(obs, s.sku) : null;
    cards.push({
      id: `stock-${s.sku}`,
      kind: "stock",
      title: `Waiting for inventory: ${product}`,
      what: `${orders(s.stockBoundOrders)} can't ship because there isn't enough ${product} in stock.`,
      why: "Stock ran short of what customers ordered.",
      affected: inbound > 0 ? `${formatCount(inbound)} units are on the way.` : "Nothing is on the way yet.",
      actions: role !== null ? [orderAction(names, s.sku, role)] : [],
    });
  }

  // Orders that are ready but the team cannot pack them all.
  const capacityBound = frame.report.skus.reduce((n, s) => n + s.capacityBoundOrders, 0);
  if (capacityBound > 0) {
    cards.push({
      id: "capacity",
      kind: "capacity",
      title: "Waiting for packing capacity",
      what: `${orders(capacityBound)} ${capacityBound === 1 ? "is" : "are"} ready but the team can't pack them all this hour.`,
      why: `The team can only pack ${formatCount(frame.report.capacity)} items an hour.`,
      affected: "",
      actions: [
        { id: "hire-temps", label: "Hire temporary staff", kind: "hire", skuIndex: 0 },
        { id: "schedule-overtime", label: "Schedule overtime", kind: "overtime", skuIndex: 0 },
      ],
    });
  }

  // Deliveries past their due time (needs the observation for the same hour).
  if (obs !== null) {
    for (const po of obs.purchaseOrders) {
      if (po.dueAt >= frame.tick * 60) continue;
      const product = nameOf(names, po.sku);
      cards.push({
        id: `late-${po.id}`,
        kind: "late_delivery",
        title: "A delivery is late",
        what: `${formatCount(po.qty)} ${product} from the ${supplierRoleName(po.supplier).toLowerCase()} were due around ${at(Math.floor(po.dueAt / 60))}.`,
        why: "They haven't arrived yet.",
        affected: "",
        actions: [],
      });
    }
  }

  return cards;
}
