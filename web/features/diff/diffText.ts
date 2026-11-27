/**
 * Plain-language wording for the diff panel. Formatting only: every number and
 * every fact comes from the server's DiffResult. Nothing here states a cause;
 * the only subtraction is the display of B minus A that the server also reports
 * as `delta`.
 */
import type { DiffComponent, DiffInput, DiffMetric } from "@/lib/api/client";
import { describeMove } from "@/features/duel/duelText";
import {
  METRIC_LABELS,
  disruptionLabel,
  formatCount,
  formatHours,
  formatMoney,
  formatPercentBps,
  formatSimTime,
  metricExplanation,
  type MetricKey,
} from "@/lib/glossary";

/** State components that are engine bookkeeping, not business facts. Shown only in Engineering Mode. */
const INTERNAL_COMPONENTS = new Set(["clock", "order_seq", "po_seq", "streams", "pending_events", "event_seq"]);

export function isInternalComponent(name: string): boolean {
  return INTERNAL_COMPONENTS.has(name);
}

function productList(skus: readonly number[], names: readonly string[]): string {
  const list = skus.map((i) => names[i] ?? "a product");
  return list.length === 0 ? "" : list.join(", ");
}

/** "Stock of Chargers", "Orders shipped", "Cash", "Staffing", "Prices". */
export function componentLabel(c: DiffComponent, names: readonly string[]): string {
  const products = productList(c.skus, names);
  switch (c.name) {
    case "inventory":
      return products === "" ? "Stock" : `Stock of ${products}`;
    case "orders":
      return "Orders shipped";
    case "ledger":
      return "Cash";
    case "workforce":
      return "Staffing";
    case "prices":
      return products === "" ? "Prices" : `Prices of ${products}`;
    case "purchase_orders":
      return products === "" ? "Deliveries on order" : `Deliveries on order for ${products}`;
    case "disruptions":
      return "Surprises";
    default:
      return "Internal bookkeeping";
  }
}

/** Plain description of one input present on one side only. */
export function describeDiffInput(i: DiffInput, names: readonly string[], startClock: string): string {
  const when = formatSimTime(i.effectiveTick, startClock);
  let what: string;
  if (i.command !== undefined) {
    what = describeMove(i.command, names);
  } else if (i.disruption !== undefined) {
    const d = i.disruption;
    what = `${disruptionLabel(d.kind)} for ${formatHours(d.durationTicks)}`;
    if (d.target >= 0 && names[d.target] !== undefined) what += ` (${names[d.target]})`;
  } else {
    what = "A decision";
  }
  return `${what} — takes effect ${when}`;
}

/** "Input #3, effective hour 21". */
export function inputReference(i: DiffInput): string {
  return `Input #${i.seq}, effective hour ${i.effectiveTick}`;
}

export type MetricGroup = "financial" | "service" | "backlog";

export interface MetricRowText {
  key: string;
  group: MetricGroup;
  label: string;
  a: string;
  b: string;
  /** B minus A, signed, as the server's delta. */
  delta: string;
  explanation: string | null;
}

interface MetricMeta {
  group: MetricGroup;
  label: string;
  unit: "money" | "bps" | "count";
  glossary?: MetricKey;
}

const METRIC_META: Record<string, MetricMeta> = {
  net_profit: { group: "financial", label: METRIC_LABELS.netProfit, unit: "money", glossary: "netProfit" },
  cash: { group: "financial", label: METRIC_LABELS.cash, unit: "money", glossary: "cash" },
  service_level: { group: "service", label: METRIC_LABELS.serviceLevel, unit: "bps", glossary: "serviceLevel" },
  fill_rate: { group: "service", label: METRIC_LABELS.fillRate, unit: "bps", glossary: "fillRate" },
  backlog_orders: { group: "backlog", label: METRIC_LABELS.backlog, unit: "count", glossary: "backlog" },
  backlog_units: { group: "backlog", label: "Items waiting", unit: "count" },
  late_backlog_orders: { group: "backlog", label: "Late orders waiting", unit: "count" },
  cancelled_orders: { group: "backlog", label: "Orders cancelled", unit: "count" },
};

export const GROUP_TITLES: Record<MetricGroup, string> = {
  financial: "Financial",
  service: "Service level",
  backlog: "Backlog",
};

export function signedCount(n: number): string {
  return n > 0 ? `+${formatCount(n)}` : formatCount(n);
}

function signedPoints(bps: number): string {
  const body = formatPercentBps(Math.abs(bps)).replace("%", "");
  return `${bps > 0 ? "+" : bps < 0 ? "-" : ""}${body} points`;
}

/** Rows for the metrics the server reported. Unknown keys are skipped. */
export function metricRows(metrics: readonly DiffMetric[]): MetricRowText[] {
  const out: MetricRowText[] = [];
  for (const m of metrics) {
    const meta = METRIC_META[m.key];
    if (meta === undefined) continue;
    const fmt = (n: number): string =>
      meta.unit === "money" ? formatMoney(n) : meta.unit === "bps" ? formatPercentBps(n) : formatCount(n);
    const delta =
      meta.unit === "money" ? formatMoney(m.delta, { signed: true }) : meta.unit === "bps" ? signedPoints(m.delta) : signedCount(m.delta);
    out.push({
      key: m.key,
      group: meta.group,
      label: meta.label,
      a: fmt(m.a),
      b: fmt(m.b),
      delta,
      explanation: meta.glossary === undefined ? null : metricExplanation(meta.glossary),
    });
  }
  return out;
}
