/**
 * Pure helpers for the side-by-side comparison. Nothing here computes a
 * simulation result: differences are display subtractions of two server values
 * for the same tick, and every helper only decides what to show or request.
 */
import type { AdvanceRequest, Branch, Metrics } from "@/lib/api/client";
import { METRIC_LABELS, formatCount, formatMoney, formatPercentBps } from "@/lib/glossary";

/** The tick both realities can be compared at: the shown hour, capped by both heads. */
export function commonTick(selected: number, headA: number, headB: number): number {
  return Math.max(0, Math.min(selected, headA, headB));
}

export type PaneStatus = "ready" | "not_simulated";

/** A reality that has not reached the shared hour cannot be drawn there. */
export function paneStatus(tick: number, head: number): PaneStatus {
  return tick <= head ? "ready" : "not_simulated";
}

/**
 * The request "Advance this future" sends, or null when nothing needs
 * computing (the hour is already simulated, for example after rewinding).
 */
export function advanceRequestFor(head: number, tick: number): AdvanceRequest | null {
  return tick > head ? { expectedHeadTick: head, runToTick: tick } : null;
}

/** Parent when there is one, otherwise the oldest child; null for a lone reality. */
export function defaultPartnerId(active: Branch, branches: readonly Branch[]): string | null {
  if (active.parentBranchId !== null && branches.some((b) => b.id === active.parentBranchId)) return active.parentBranchId;
  const child = branches
    .filter((b) => b.parentBranchId === active.id)
    .sort((x, y) => x.createdAt.localeCompare(y.createdAt))[0];
  return child?.id ?? branches.find((b) => b.id !== active.id)?.id ?? null;
}

/**
 * Last tick at which two realities share history (their state hashes are
 * identical up to and including it). Null when the branches are not related.
 */
export function divergenceTick(a: Branch, b: Branch, branches: readonly Branch[]): number | null {
  const byId = new Map(branches.map((x) => [x.id, x]));
  /** Path from the branch up to the root, with the tick each step left its parent. */
  const path = (start: Branch): { id: string; leftAt: number }[] => {
    const out: { id: string; leftAt: number }[] = [];
    let cur: Branch | undefined = start;
    let leftAt = Number.POSITIVE_INFINITY;
    while (cur !== undefined) {
      out.push({ id: cur.id, leftAt });
      leftAt = cur.forkTick;
      cur = cur.parentBranchId === null ? undefined : byId.get(cur.parentBranchId);
    }
    return out;
  };
  const pa = path(a);
  const pb = path(b);
  const bIds = new Map(pb.map((p) => [p.id, p.leftAt]));
  for (const p of pa) {
    const other = bIds.get(p.id);
    if (other !== undefined) return Math.min(p.leftAt, other);
  }
  return null;
}

export type CompareKey = "cash" | "netProfit" | "serviceLevel" | "backlog";

export interface DiffRow {
  key: CompareKey;
  label: string;
  a: string;
  b: string;
  /** b minus a, signed, in the same unit as the values. */
  diff: string;
  /** Raw display subtraction b - a (cents, basis points or orders). */
  delta: number;
}

function points(bps: number): string {
  const body = formatPercentBps(Math.abs(bps)).replace("%", "");
  return `${bps > 0 ? "+" : bps < 0 ? "-" : ""}${body} points`;
}

/** Display subtraction of two server Metrics for the same tick (b minus a). */
export function diffMetrics(a: Metrics, b: Metrics): DiffRow[] {
  const cash = b.cashCents - a.cashCents;
  const profit = b.netProfitCents - a.netProfitCents;
  const service = b.serviceLevelBps - a.serviceLevelBps;
  const backlog = b.backlogOrders - a.backlogOrders;
  return [
    { key: "cash", label: METRIC_LABELS.cash, a: formatMoney(a.cashCents), b: formatMoney(b.cashCents), diff: formatMoney(cash, { signed: true }), delta: cash },
    { key: "netProfit", label: METRIC_LABELS.netProfit, a: formatMoney(a.netProfitCents), b: formatMoney(b.netProfitCents), diff: formatMoney(profit, { signed: true }), delta: profit },
    { key: "serviceLevel", label: METRIC_LABELS.serviceLevel, a: formatPercentBps(a.serviceLevelBps), b: formatPercentBps(b.serviceLevelBps), diff: points(service), delta: service },
    { key: "backlog", label: METRIC_LABELS.backlog, a: formatCount(a.backlogOrders), b: formatCount(b.backlogOrders), diff: backlog > 0 ? `+${formatCount(backlog)}` : formatCount(backlog), delta: backlog },
  ];
}

/** Engineering Mode note comparing two state hashes at one tick. */
export function hashNote(tick: number, divergence: number | null, hashA: string, hashB: string): string {
  const same = hashA === hashB;
  if (divergence !== null && tick <= divergence) {
    return same
      ? `Identical, as expected: the two realities share history up to tick ${divergence}.`
      : `Different, which is not expected: the two realities share history up to tick ${divergence}.`;
  }
  if (divergence === null) return same ? "Identical." : "Different.";
  return same ? "Identical, although the realities diverged after tick " + divergence + "." : `Different, as expected: the realities diverged after tick ${divergence}.`;
}
