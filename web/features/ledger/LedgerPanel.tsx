"use client";

/**
 * "Where this number comes from": profit and loss by account and, for one
 * money figure, the ledger entries that add up to it. Every number is a server
 * value (the account balances, the figure's sum, each entry's contribution);
 * the panel only formats them. Plain mode shows a readable label for each
 * entry's cause; Engineering Mode adds entry numbers, ticks, cause kind and
 * reference, and the raw description.
 *
 * It is a non-modal dialog: focus moves to its heading on open, Escape closes
 * it, and focus returns to whatever opened it.
 */
import { useEffect, useRef, useState } from "react";
import {
  getLedgerSummary,
  listLedgerEntries,
  type Account,
  type LedgerEntry,
  type LedgerResponse,
  type LedgerSummaryResponse,
  type MoneyFigure,
} from "@/lib/api/client";
import { DISCLAIMER_FULL, accountLabel, formatMoney, formatSimTime } from "@/lib/glossary";

const PAGE = 25;
const FETCH = 1000;
const MAX_FETCHES = 20;

export const FIGURE_LABELS: Record<MoneyFigure, string> = {
  cash: "Cash",
  net_profit: "Profit so far",
  net_revenue: "Sales",
  cost_of_goods_sold: "Cost of goods shipped",
  freight: "Shipping",
  labor: "Wages",
  marketing: "Marketing",
  overhead: "Overhead",
  service_credits: "Credits for late orders",
};

/** The figure each account drills into. Stock and starting funds have none. */
const ACCOUNT_FIGURE: Partial<Record<Account, MoneyFigure>> = {
  Cash: "cash",
  Revenue: "net_revenue",
  Cost_Of_Goods_Sold: "cost_of_goods_sold",
  Freight_Expense: "freight",
  Labor_Expense: "labor",
  Marketing_Expense: "marketing",
  Overhead_Expense: "overhead",
  Service_Credit_Expense: "service_credits",
};

const PNL_ACCOUNTS: Account[] = [
  "Revenue",
  "Cost_Of_Goods_Sold",
  "Freight_Expense",
  "Labor_Expense",
  "Marketing_Expense",
  "Overhead_Expense",
  "Service_Credit_Expense",
];

/** Readable cause: "Order shipment · order 12", "Wages · Friday, 6:00 AM". */
export function causeLabel(kind: string, ref: number, startClock: string): string {
  switch (kind) {
    case "opening":
      return "Opening balances";
    case "shipment":
      return `Order shipment · order ${ref}`;
    case "receipt":
      return `Supplier delivery · purchase order ${ref}`;
    case "freight":
      return `Supplier freight · purchase order ${ref}`;
    case "labor":
      return `Wages · ${formatSimTime(ref, startClock)}`;
    case "marketing":
      return `Marketing · ${formatSimTime(ref, startClock)}`;
    case "overhead":
      return `Overhead · ${formatSimTime(ref, startClock)}`;
    case "service_credit":
      return `Credit for a late order · order ${ref}`;
    default:
      return kind.replace(/_/g, " ");
  }
}

function signed(cents: number): string {
  return formatMoney(cents, { signed: true });
}

export interface LedgerPanelProps {
  branchId: string;
  /** Last hour included (the displayed tick). */
  tick: number;
  /** Figure to open straight into; the summary shows when absent. */
  figure?: MoneyFigure;
  title?: string;
  startClock: string;
  engineering?: boolean;
  onClose?: () => void;
}

interface Summary {
  key: string;
  data: LedgerSummaryResponse | null;
  error: string | null;
}

interface Drill {
  key: string;
  entries: LedgerEntry[];
  total: number;
  sumCents: number;
  error: string | null;
}

export function LedgerPanel({
  branchId,
  tick,
  figure,
  title = "Where this number comes from",
  startClock,
  engineering,
  onClose,
}: LedgerPanelProps) {
  const [pick, setPick] = useState<{ forProp: MoneyFigure | undefined; value: MoneyFigure | null } | null>(null);
  const selected: MoneyFigure | null = pick !== null && pick.forProp === figure ? pick.value : (figure ?? null);
  const [page, setPage] = useState({ forKey: "", n: 0 });
  const [summary, setSummary] = useState<Summary | null>(null);
  const [drill, setDrill] = useState<Drill | null>(null);
  const headingRef = useRef<HTMLHeadingElement>(null);

  const summaryKey = `${branchId}:${tick}`;
  const drillKey = `${branchId}:${tick}:${selected ?? ""}`;
  const offset = page.forKey === drillKey ? page.n : 0;

  // Focus the heading on open; give focus back to the opener on close.
  useEffect(() => {
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    headingRef.current?.focus();
    return () => {
      if (opener !== null && opener.isConnected) opener.focus();
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    getLedgerSummary(branchId, tick)
      .then((data) => {
        if (!cancelled) setSummary({ key: summaryKey, data, error: null });
      })
      .catch((e: unknown) => {
        if (!cancelled) setSummary({ key: summaryKey, data: null, error: e instanceof Error ? e.message : "Could not load." });
      });
    return () => {
      cancelled = true;
    };
  }, [branchId, tick, summaryKey]);

  useEffect(() => {
    if (selected === null) return;
    let cancelled = false;
    const run = async (): Promise<void> => {
      const entries: LedgerEntry[] = [];
      let last: LedgerResponse | null = null;
      for (let i = 0; i < MAX_FETCHES; i++) {
        last = await listLedgerEntries(branchId, { from: 0, to: tick, figure: selected, limit: FETCH, offset: entries.length });
        entries.push(...last.entries);
        if (last.entries.length === 0 || entries.length >= last.total) break;
      }
      if (cancelled || last === null) return;
      setDrill({ key: drillKey, entries, total: last.total, sumCents: last.sumCents ?? 0, error: null });
    };
    run().catch((e: unknown) => {
      if (!cancelled) {
        setDrill({ key: drillKey, entries: [], total: 0, sumCents: 0, error: e instanceof Error ? e.message : "Could not load." });
      }
    });
    return () => {
      cancelled = true;
    };
  }, [branchId, tick, selected, drillKey]);

  const sum = summary !== null && summary.key === summaryKey ? summary : null;
  const dr = drill !== null && drill.key === drillKey ? drill : null;

  const open = (f: MoneyFigure | null): void => setPick({ forProp: figure, value: f });

  const accounts = new Map((sum?.data?.accounts ?? []).map((a) => [a.account, a]));
  const figures = new Map((sum?.data?.figures ?? []).map((f) => [f.figure, f]));

  // Running contribution is a cumulative display of the server's per-entry values.
  const running: number[] = [];
  if (dr !== null) {
    let acc = 0;
    for (const e of dr.entries) {
      acc += e.contributionCents ?? 0;
      running.push(acc);
    }
  }
  const shown = dr === null ? [] : dr.entries.slice(offset, offset + PAGE);

  return (
    <section
      role="dialog"
      aria-modal="false"
      aria-labelledby="ledger-title"
      className="space-y-3 text-sm text-ivory"
      data-testid="ledger-panel"
      onKeyDown={(e) => {
        if (e.key === "Escape" && onClose) {
          e.stopPropagation();
          onClose();
        }
      }}
    >
      <header className="flex items-start justify-between gap-2">
        <h2 id="ledger-title" ref={headingRef} tabIndex={-1} className="font-display text-xl outline-none">
          {title}
        </h2>
        {onClose ? (
          <button
            type="button"
            onClick={onClose}
            className="min-h-11 rounded-[4px] border border-hairline px-3 hover:border-amber"
          >
            Close
          </button>
        ) : null}
      </header>
      <p className="text-xs text-ivory-dim">
        Every figure is the sum of these records, from the start through {formatSimTime(tick, startClock)}.
      </p>

      {selected === null ? (
        <div data-testid="ledger-summary">
          <h3 className="font-display text-base">Profit and loss by account</h3>
          {sum === null ? <p role="status">Loading…</p> : null}
          {sum?.error ? (
            <p role="alert" className="text-clay">
              {sum.error}
            </p>
          ) : null}
          {sum?.data ? (
            <table className="mt-1 w-full text-left tabular-nums">
              <caption className="sr-only">Profit and loss by account</caption>
              <thead className="text-xs text-ivory-dim">
                <tr>
                  <th scope="col" className="py-1 font-normal">Account</th>
                  <th scope="col" className="py-1 text-right font-normal">Amount</th>
                </tr>
              </thead>
              <tbody>
                {PNL_ACCOUNTS.map((acct) => {
                  const a = accounts.get(acct);
                  const f = ACCOUNT_FIGURE[acct];
                  if (a === undefined || f === undefined) return null;
                  return (
                    <tr key={acct} className="border-t border-hairline">
                      <th scope="row" className="py-1 font-normal">
                        <button
                          type="button"
                          onClick={() => open(f)}
                          className="min-h-11 text-left underline decoration-dotted underline-offset-4"
                          title="Where this number comes from"
                        >
                          {accountLabel(acct)}
                        </button>
                        {engineering ? <span className="ml-2 font-mono text-xs text-ivory-dim">{acct}</span> : null}
                      </th>
                      <td className="py-1 text-right" data-testid={`pnl-${f}`}>
                        {formatMoney(a.balanceCents)}
                      </td>
                    </tr>
                  );
                })}
                {figures.get("net_profit") ? (
                  <tr className="border-t border-ivory-dim font-semibold">
                    <th scope="row" className="py-1">
                      <button
                        type="button"
                        onClick={() => open("net_profit")}
                        className="min-h-11 text-left underline decoration-dotted underline-offset-4"
                        title="Where this number comes from"
                      >
                        Profit so far
                      </button>
                    </th>
                    <td className="py-1 text-right" data-testid="pnl-net_profit">
                      {signed(figures.get("net_profit")?.sumCents ?? 0)}
                    </td>
                  </tr>
                ) : null}
                {(["Cash", "Inventory"] as Account[]).map((acct) => {
                  const a = accounts.get(acct);
                  if (a === undefined) return null;
                  const f = ACCOUNT_FIGURE[acct];
                  return (
                    <tr key={acct} className="border-t border-hairline">
                      <th scope="row" className="py-1 font-normal">
                        {f !== undefined ? (
                          <button
                            type="button"
                            onClick={() => open(f)}
                            className="min-h-11 text-left underline decoration-dotted underline-offset-4"
                            title="Where this number comes from"
                          >
                            {accountLabel(acct)}
                          </button>
                        ) : (
                          accountLabel(acct)
                        )}
                      </th>
                      <td className="py-1 text-right" data-testid={`balance-${acct}`}>
                        {formatMoney(a.balanceCents)}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          ) : null}
        </div>
      ) : (
        <div data-testid="ledger-drill">
          <div className="flex items-center justify-between gap-2">
            <h3 className="font-display text-base">{FIGURE_LABELS[selected]}</h3>
            <button
              type="button"
              onClick={() => open(null)}
              className="min-h-11 rounded-[4px] border border-hairline px-3 hover:border-amber"
            >
              All accounts
            </button>
          </div>
          {dr === null ? <p role="status">Loading…</p> : null}
          {dr?.error ? (
            <p role="alert" className="text-clay">
              {dr.error}
            </p>
          ) : null}
          {dr !== null && dr.error === null ? (
            <>
              <p className="tabular-nums" data-testid="ledger-sum">
                <span className="text-lg font-semibold" data-testid="ledger-sum-value">
                  {selected === "net_profit" ? signed(dr.sumCents) : formatMoney(dr.sumCents)}
                </span>{" "}
                <span className="text-xs text-ivory-dim" data-testid="ledger-count">
                  {dr.total} {dr.total === 1 ? "entry adds" : "entries add"} up to this
                </span>
              </p>
              <ul className="space-y-2">
                {shown.map((e, i) => (
                  <li key={e.index} className="border-t border-hairline pt-2" data-testid="ledger-entry">
                    <div>
                      {causeLabel(e.cause.kind, e.cause.ref, startClock)}{" "}
                      <span className="text-xs text-ivory-dim">{formatSimTime(e.tick, startClock)}</span>
                    </div>
                    {engineering ? (
                      <div className="font-mono text-xs text-ivory-dim">
                        entry {e.index} · tick {e.tick} · {e.cause.kind} {e.cause.ref} · {e.description}
                      </div>
                    ) : null}
                    <ul className="text-xs tabular-nums text-ivory-dim">
                      {e.lines.map((l, j) => (
                        <li key={j}>
                          {accountLabel(l.account)}:{" "}
                          {l.debitCents > 0 ? `+${formatMoney(l.debitCents)}` : `-${formatMoney(l.creditCents)}`}
                        </li>
                      ))}
                    </ul>
                    <div className="text-xs tabular-nums">
                      Adds {signed(e.contributionCents ?? 0)} · running {signed(running[offset + i] ?? 0)}
                    </div>
                  </li>
                ))}
              </ul>
              {dr.total === 0 ? <p className="text-ivory-dim">No records yet.</p> : null}
              <div className="flex items-center justify-between text-xs text-ivory-dim">
                <button
                  type="button"
                  disabled={offset === 0}
                  onClick={() => setPage({ forKey: drillKey, n: Math.max(0, offset - PAGE) })}
                  className="min-h-11 rounded-[4px] border border-hairline px-3 disabled:opacity-40"
                >
                  Earlier
                </button>
                <span className="tabular-nums">
                  {dr.entries.length === 0 ? 0 : offset + 1} to {Math.min(offset + PAGE, dr.entries.length)} of {dr.total}
                </span>
                <button
                  type="button"
                  disabled={offset + PAGE >= dr.entries.length}
                  onClick={() => setPage({ forKey: drillKey, n: offset + PAGE })}
                  className="min-h-11 rounded-[4px] border border-hairline px-3 disabled:opacity-40"
                >
                  Later
                </button>
              </div>
            </>
          ) : null}
        </div>
      )}
      <p className="text-xs text-ivory-dim" data-testid="ledger-disclaimer">
        {DISCLAIMER_FULL}
      </p>
    </section>
  );
}
