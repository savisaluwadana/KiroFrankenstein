/**
 * Pure model of the setup screen: friendly controls mapped onto assumption
 * paths. Bounds, defaults and rationale come from the API's assumption
 * metadata (World.assumptions); nothing numeric is hardcoded here except UI
 * step sizes.
 */
import type { Assumption, ScenarioSummary } from "@/lib/api/client";
import { formatCount, formatHours, formatMoney, productName } from "@/lib/glossary";

export type ControlGroup = "Money" | "People" | "Suppliers" | "Prices";

export interface SetupControl {
  id: string;
  group: ControlGroup;
  label: string;
  /** Assumption paths this control writes (all get the same value). */
  paths: string[];
  /** Server value, min, max and default (integers in the assumption's own unit). */
  value: number;
  min: number;
  max: number;
  default: number;
  step: number;
  /** "money" values are cents; "int" values are plain integers. */
  kind: "money" | "int";
  /** Show a slider next to the number field. */
  slider: boolean;
  format: (v: number) => string;
  rationale: string;
}

export function cleanRationale(r: string): string {
  return r.replace(/^synthetic, illustrative(; uncalibrated)?:\s*/i, "");
}

function pick(assumptions: Assumption[], path: string): Assumption | undefined {
  return assumptions.find((a) => a.path === path);
}

interface Spec {
  id: string;
  group: ControlGroup;
  label: string;
  paths: string[];
  step: number;
  kind: "money" | "int";
  slider: boolean;
  format: (v: number) => string;
}

/** Builds the control list. A control whose assumption is missing is omitted. */
export function buildControls(assumptions: Assumption[], scenario: Pick<ScenarioSummary, "skus">): SetupControl[] {
  const specs: Spec[] = [
    {
      id: "cash",
      group: "Money",
      label: "Starting cash",
      paths: ["openingCashCents"],
      step: 100000,
      kind: "money",
      slider: true,
      format: (v) => formatMoney(v, { precision: "whole" }),
    },
    {
      id: "shipping",
      group: "Money",
      label: "Shipping cost per item",
      paths: ["costs.outboundShipCentsPerUnit"],
      step: 25,
      kind: "money",
      slider: true,
      format: (v) => formatMoney(v),
    },
    {
      id: "staff",
      group: "People",
      label: "Packing staff",
      paths: ["workforce.regularWorkers"],
      step: 1,
      kind: "int",
      slider: true,
      format: (v) => `${formatCount(v)} ${v === 1 ? "person" : "people"}`,
    },
    {
      id: "speed",
      group: "People",
      label: "Packing speed",
      paths: ["workforce.unitsPerWorkerHour"],
      step: 1,
      kind: "int",
      slider: true,
      format: (v) => `${formatCount(v)} ${v === 1 ? "item" : "items"} per person per hour`,
    },
    {
      id: "primaryLead",
      group: "Suppliers",
      label: "Normal supplier delivery time",
      paths: scenario.skus.map((s) => `suppliers.${s.id}.primary.leadTimeTicks`),
      step: 1,
      kind: "int",
      slider: true,
      format: formatHours,
    },
    {
      id: "backupLead",
      group: "Suppliers",
      label: "Backup supplier delivery time",
      paths: scenario.skus.map((s) => `suppliers.${s.id}.backup.leadTimeTicks`),
      step: 1,
      kind: "int",
      slider: true,
      format: formatHours,
    },
    ...scenario.skus.map(
      (s): Spec => ({
        id: `price.${s.id}`,
        group: "Prices",
        label: `${productName(s.id)} price`,
        paths: [`skus.${s.id}.referencePriceCents`],
        step: 100,
        kind: "money",
        slider: false,
        format: (v) => formatMoney(v),
      }),
    ),
  ];

  const out: SetupControl[] = [];
  for (const spec of specs) {
    const found = spec.paths.map((p) => pick(assumptions, p));
    const first = found[0];
    if (first === undefined || found.some((a) => a === undefined)) continue;
    const list = found as Assumption[];
    out.push({
      id: spec.id,
      group: spec.group,
      label: spec.label,
      paths: spec.paths,
      value: first.value,
      // Intersection of bounds so one value is valid for every path it writes.
      min: Math.max(...list.map((a) => a.min)),
      max: Math.min(...list.map((a) => a.max)),
      default: first.default,
      step: spec.step,
      kind: spec.kind,
      slider: spec.slider,
      format: spec.format,
      rationale: cleanRationale(first.rationale),
    });
  }
  return out;
}

export function isValid(c: SetupControl, v: number): boolean {
  return Number.isInteger(v) && v >= c.min && v <= c.max;
}

export function clamp(c: SetupControl, v: number): number {
  return Math.min(c.max, Math.max(c.min, v));
}

/** Overrides for the values that differ from the server's current value. Empty when nothing changed. */
export function buildOverrides(controls: SetupControl[], values: Record<string, number>): Record<string, number> {
  const overrides: Record<string, number> = {};
  for (const c of controls) {
    const v = values[c.id];
    if (v === undefined || v === c.value) continue;
    for (const p of c.paths) overrides[p] = v;
  }
  return overrides;
}

/** Parses "59.99", "60", "$1,200.5" to integer cents with string math. Returns null when invalid. */
export function parseDollarsToCents(text: string): number | null {
  const m = /^\$?\s*(\d{1,12}(?:,\d{3})*|\d+)(?:\.(\d{1,2}))?$/.exec(text.trim());
  if (!m) return null;
  const dollars = Number((m[1] ?? "").replace(/,/g, ""));
  const frac = (m[2] ?? "").padEnd(2, "0");
  return dollars * 100 + Number(frac);
}
