"use client";

/**
 * "Where this number comes from": the ledger entries behind a money figure,
 * paged from the API. Plain mode shows descriptions, times and amounts;
 * Engineering Mode adds entry numbers, ticks and cause references.
 */
import { useEffect, useState } from "react";
import { listLedgerEntries, type Account, type LedgerResponse } from "@/lib/api/client";
import { accountLabel, formatMoney, formatSimTime } from "@/lib/glossary";

const PAGE = 25;

export interface LedgerPanelProps {
  branchId: string;
  /** Last hour included (the displayed tick). */
  tick: number;
  /** Restrict to one account (Cash for the cash figure). */
  account?: Account;
  title: string;
  startClock: string;
  engineering?: boolean;
  onClose?: () => void;
}

interface Loaded {
  key: string;
  data: LedgerResponse | null;
  error: string | null;
}

export function LedgerPanel({ branchId, tick, account, title, startClock, engineering, onClose }: LedgerPanelProps) {
  const [offset, setOffset] = useState(0);
  const [loaded, setLoaded] = useState<Loaded | null>(null);
  const key = `${branchId}:${tick}:${account ?? ""}:${offset}`;

  useEffect(() => {
    let cancelled = false;
    listLedgerEntries(branchId, { from: 0, to: tick, ...(account !== undefined ? { account } : {}), limit: PAGE, offset })
      .then((data) => {
        if (!cancelled) setLoaded({ key, data, error: null });
      })
      .catch((e: unknown) => {
        if (!cancelled) setLoaded({ key, data: null, error: e instanceof Error ? e.message : "Could not load." });
      });
    return () => {
      cancelled = true;
    };
  }, [branchId, tick, account, offset, key]);

  const ready = loaded !== null && loaded.key === key ? loaded : null;
  const data = ready?.data ?? null;

  return (
    <section aria-label={title} className="space-y-3 text-sm text-ivory" data-testid="ledger-panel">
      <header className="flex items-start justify-between gap-2">
        <h2 className="font-display text-xl">{title}</h2>
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
      {ready === null ? <p role="status">Loading…</p> : null}
      {ready?.error ? (
        <p role="alert" className="text-clay">
          {ready.error}
        </p>
      ) : null}
      {data !== null ? (
        <>
          <ul className="space-y-2">
            {data.entries.map((e) => (
              <li key={e.index} className="border-t border-hairline pt-2" data-testid="ledger-entry">
                <div>
                  {e.description}{" "}
                  <span className="text-xs text-ivory-dim">
                    {formatSimTime(e.tick, startClock)}
                    {engineering ? ` · entry ${e.index} · tick ${e.tick} · ${e.cause.kind} ${e.cause.ref}` : ""}
                  </span>
                </div>
                <ul className="text-xs tabular-nums text-ivory-dim">
                  {e.lines
                    .filter((l) => account === undefined || l.account === account)
                    .map((l, i) => (
                      <li key={i}>
                        {accountLabel(l.account)}:{" "}
                        {l.debitCents > 0 ? `+${formatMoney(l.debitCents)}` : `-${formatMoney(l.creditCents)}`}
                      </li>
                    ))}
                </ul>
              </li>
            ))}
          </ul>
          {data.total === 0 ? <p className="text-ivory-dim">No records yet.</p> : null}
          <div className="flex items-center justify-between text-xs text-ivory-dim">
            <button
              type="button"
              disabled={offset === 0}
              onClick={() => setOffset(Math.max(0, offset - PAGE))}
              className="min-h-11 rounded-[4px] border border-hairline px-3 disabled:opacity-40"
            >
              Earlier
            </button>
            <span className="tabular-nums">
              {data.total === 0 ? 0 : offset + 1} to {Math.min(offset + PAGE, data.total)} of {data.total}
            </span>
            <button
              type="button"
              disabled={offset + PAGE >= data.total}
              onClick={() => setOffset(offset + PAGE)}
              className="min-h-11 rounded-[4px] border border-hairline px-3 disabled:opacity-40"
            >
              Later
            </button>
          </div>
        </>
      ) : null}
    </section>
  );
}
