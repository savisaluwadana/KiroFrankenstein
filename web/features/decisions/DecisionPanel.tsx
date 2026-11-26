"use client";

/**
 * Decision_Panel: shows the Observation of the open Decision_Window and lets
 * the Human manager build and submit Commands. Every number shown is a server
 * value, formatted only. The panel never computes outcomes; validation and
 * effective Ticks come from the API response.
 */
import { useState } from "react";
import {
  ApiError,
  endDecisionWindow,
  submitCommands,
  type Command,
  type Observation,
  type SubmitCommandsResult,
  type ValidationResult,
} from "@/lib/api/client";

export interface DecisionPanelProps {
  observation: Observation;
  /** Tick of the open Decision_Window. */
  windowTick: number;
  branchId: string;
  /** Commands still allowed in this window, from the server. */
  budgetRemaining: number;
  /** Called with the server response after each submit. */
  onSubmitted?: (result: SubmitCommandsResult) => void;
  /** Called after the server confirms the window ended. */
  onWindowEnded?: () => void;
}

/** Formats integer cents as dollars using integer arithmetic only. */
export function formatCents(cents: number): string {
  const neg = cents < 0;
  const abs = Math.abs(cents);
  const whole = Math.trunc(abs / 100).toLocaleString("en-US");
  const frac = String(abs % 100).padStart(2, "0");
  return `${neg ? "-" : ""}$${whole}.${frac}`;
}

/** Parses a dollar string such as "12", "12.5" or "12.50" into integer cents without floating point. */
export function parseDollarsToCents(input: string): number | null {
  const m = /^(\d{1,12})(?:\.(\d{1,2}))?$/.exec(input.trim());
  if (m === null) return null;
  const dollars = m[1] ?? "0";
  const frac = (m[2] ?? "").padEnd(2, "0");
  return Number(dollars) * 100 + Number(frac);
}

interface PendingCommand {
  key: number;
  command: Command;
  rejection?: { code: string; message: string };
}

interface SentCommand {
  key: number;
  command: Command;
  effectiveTick: number;
}

function describe(c: Command, skuNames: Map<number, string>): string {
  const sku = c.sku !== undefined ? (skuNames.get(c.sku) ?? `SKU ${c.sku}`) : "";
  switch (c.kind) {
    case "place_purchase_order":
      return `Order ${c.qty ?? 0} × ${sku} from ${c.role ?? "?"} supplier`;
    case "set_price":
      return `Set price of ${sku} to ${formatCents(c.amountCents ?? 0)}`;
    default:
      return c.kind;
  }
}

const field =
  "rounded-[4px] border border-[#232B3A] bg-[#0B0E14] px-2 py-1 font-mono text-sm text-[#E6E9EF] tabular-nums";
const button =
  "rounded-[4px] border border-[#232B3A] bg-[#121722] px-3 py-1 text-sm text-[#E6E9EF] transition-colors duration-150 ease-out hover:border-[#F2A93B] disabled:opacity-40";

export function DecisionPanel(props: DecisionPanelProps) {
  const { observation, windowTick, branchId, budgetRemaining, onSubmitted, onWindowEnded } = props;
  const skus = observation.skus;
  const skuNames = new Map(skus.map((s) => [s.sku, s.id]));

  const [seq, setSeq] = useState(0);
  const [pending, setPending] = useState<PendingCommand[]>([]);
  const [sent, setSent] = useState<SentCommand[]>([]);
  const [formError, setFormError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [ended, setEnded] = useState(false);

  const [poSku, setPoSku] = useState<number>(skus[0]?.sku ?? 0);
  const [poRole, setPoRole] = useState<"primary" | "backup">("primary");
  const [poQty, setPoQty] = useState("");
  const [prSku, setPrSku] = useState<number>(skus[0]?.sku ?? 0);
  const [prPrice, setPrPrice] = useState("");

  const [remaining, setRemaining] = useState(budgetRemaining);
  const [prevBudget, setPrevBudget] = useState(budgetRemaining);
  if (prevBudget !== budgetRemaining) {
    setPrevBudget(budgetRemaining);
    setRemaining(budgetRemaining);
  }

  function addPending(command: Command) {
    setPending((p) => [...p, { key: seq, command }]);
    setSeq((n) => n + 1);
    setFormError(null);
  }

  function addPurchaseOrder() {
    if (!/^\d{1,7}$/.test(poQty) || Number(poQty) < 1) {
      setFormError("Quantity must be a whole number of at least 1.");
      return;
    }
    addPending({ kind: "place_purchase_order", sku: poSku, role: poRole, qty: Number(poQty) });
    setPoQty("");
  }

  function addSetPrice() {
    const cents = parseDollarsToCents(prPrice);
    if (cents === null) {
      setFormError("Price must be dollars with up to two decimals, for example 19.99.");
      return;
    }
    addPending({ kind: "set_price", sku: prSku, amountCents: cents });
    setPrPrice("");
  }

  async function submit() {
    if (pending.length === 0 || busy) return;
    setBusy(true);
    setFormError(null);
    const batch = pending;
    try {
      const result = await submitCommands(
        branchId,
        { windowTick, commands: batch.map((p) => p.command), expectedHeadTick: observation.tick },
        crypto.randomUUID(),
      );
      const rejectedBy = new Map<number, ValidationResult>(result.rejected.map((r) => [r.index, r]));
      const acceptedQueue = [...result.accepted];
      const stillPending: PendingCommand[] = [];
      const newlySent: SentCommand[] = [];
      batch.forEach((p, i) => {
        const rej = rejectedBy.get(i);
        if (rej !== undefined) {
          stillPending.push({
            ...p,
            rejection: { code: rej.code ?? "rejected", message: rej.message ?? "Command rejected." },
          });
          return;
        }
        const acc = acceptedQueue.shift();
        if (acc !== undefined) newlySent.push({ key: p.key, command: p.command, effectiveTick: acc.effectiveTick });
      });
      setPending(stillPending);
      setSent((s) => [...s, ...newlySent]);
      setRemaining(result.budgetRemaining);
      onSubmitted?.(result);
    } catch (e) {
      setFormError(e instanceof ApiError ? e.message : "Submit failed.");
    } finally {
      setBusy(false);
    }
  }

  async function endWindow() {
    if (busy) return;
    setBusy(true);
    setFormError(null);
    try {
      await endDecisionWindow(branchId, { windowTick, expectedHeadTick: observation.tick });
      setEnded(true);
      onWindowEnded?.();
    } catch (e) {
      setFormError(e instanceof ApiError ? e.message : "Could not end the decision window.");
    } finally {
      setBusy(false);
    }
  }

  const metrics = observation.metrics;

  return (
    <section aria-label="Decision panel" className="space-y-4 text-sm text-[#E6E9EF]">
      <header className="flex items-baseline justify-between">
        <h2 className="text-base font-medium">Decision window</h2>
        <span className="font-mono tabular-nums text-[#8A93A6]">Tick {windowTick}</span>
      </header>

      <dl className="grid grid-cols-2 gap-x-3 gap-y-1 font-mono tabular-nums">
        <dt className="text-[#8A93A6]">Cash</dt>
        <dd data-testid="obs-cash">{formatCents(observation.cashCents)}</dd>
        <dt className="text-[#8A93A6]">Backlog</dt>
        <dd data-testid="obs-backlog">{metrics.backlogOrders} orders</dd>
        <dt className="text-[#8A93A6]">Command budget left</dt>
        <dd data-testid="obs-budget">{remaining} commands</dd>
      </dl>

      <table className="w-full font-mono tabular-nums">
        <caption className="sr-only">Stock, price and inbound per SKU</caption>
        <thead className="text-left text-[#8A93A6]">
          <tr>
            <th scope="col">SKU</th>
            <th scope="col">On hand</th>
            <th scope="col">Price</th>
            <th scope="col">Inbound</th>
          </tr>
        </thead>
        <tbody>
          {skus.map((s) => (
            <tr key={s.sku}>
              <th scope="row" className="text-left font-normal">
                {s.id}
              </th>
              <td>{s.onHand} units</td>
              <td>{formatCents(s.priceCents)}</td>
              <td>{s.inboundUnits} units</td>
            </tr>
          ))}
        </tbody>
      </table>

      <div>
        <h3 className="text-[#8A93A6]">Active disruptions</h3>
        {observation.disruptions.length === 0 ? (
          <p className="font-mono">None</p>
        ) : (
          <ul className="font-mono tabular-nums text-[#FF5C5C]">
            {observation.disruptions.map((d) => (
              <li key={d.inputSeq}>
                {d.kind}
                {d.supplierRole !== undefined ? ` (${d.supplierRole})` : ""}
                {d.target >= 0 ? ` on ${skuNames.get(d.target) ?? `SKU ${d.target}`}` : ""}, ticks {d.onsetTick} to{" "}
                {d.endTick}
              </li>
            ))}
          </ul>
        )}
      </div>

      <form
        aria-label="Place purchase order"
        className="space-y-2"
        onSubmit={(e) => {
          e.preventDefault();
          addPurchaseOrder();
        }}
      >
        <h3 className="text-[#8A93A6]">Place purchase order</h3>
        <div className="flex flex-wrap gap-2">
          <label className="flex flex-col gap-1">
            PO SKU
            <select className={field} value={poSku} onChange={(e) => setPoSku(Number(e.target.value))}>
              {skus.map((s) => (
                <option key={s.sku} value={s.sku}>
                  {s.id}
                </option>
              ))}
            </select>
          </label>
          <label className="flex flex-col gap-1">
            Supplier
            <select
              className={field}
              value={poRole}
              onChange={(e) => setPoRole(e.target.value === "backup" ? "backup" : "primary")}
            >
              <option value="primary">primary</option>
              <option value="backup">backup</option>
            </select>
          </label>
          <label className="flex flex-col gap-1">
            Quantity
            <input
              className={`${field} w-24`}
              inputMode="numeric"
              value={poQty}
              onChange={(e) => setPoQty(e.target.value)}
            />
          </label>
        </div>
        <button type="submit" className={button}>
          Add order
        </button>
      </form>

      <form
        aria-label="Set price"
        className="space-y-2"
        onSubmit={(e) => {
          e.preventDefault();
          addSetPrice();
        }}
      >
        <h3 className="text-[#8A93A6]">Set price</h3>
        <div className="flex flex-wrap gap-2">
          <label className="flex flex-col gap-1">
            Price SKU
            <select className={field} value={prSku} onChange={(e) => setPrSku(Number(e.target.value))}>
              {skus.map((s) => (
                <option key={s.sku} value={s.sku}>
                  {s.id}
                </option>
              ))}
            </select>
          </label>
          <label className="flex flex-col gap-1">
            Price (USD)
            <input
              className={`${field} w-28`}
              inputMode="decimal"
              value={prPrice}
              onChange={(e) => setPrPrice(e.target.value)}
            />
          </label>
        </div>
        <button type="submit" className={button}>
          Add price
        </button>
      </form>

      {formError !== null && (
        <p role="alert" className="text-[#FF7A7A]">
          {formError}
        </p>
      )}

      <div>
        <h3 className="text-[#8A93A6]">Pending commands</h3>
        {pending.length === 0 ? (
          <p className="text-[#8A93A6]">No pending commands.</p>
        ) : (
          <ul className="space-y-1">
            {pending.map((p) => (
              <li key={p.key} data-testid="pending-command">
                <span className="font-mono tabular-nums">{describe(p.command, skuNames)}</span>
                {p.rejection !== undefined && (
                  <span role="alert" className="ml-2 text-[#FF7A7A]">
                    Rejected ({p.rejection.code}): {p.rejection.message}
                  </span>
                )}
                <button
                  type="button"
                  className="ml-2 text-[#8A93A6] underline"
                  onClick={() => setPending((list) => list.filter((x) => x.key !== p.key))}
                >
                  Remove
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

      {sent.length > 0 && (
        <div>
          <h3 className="text-[#8A93A6]">Accepted commands</h3>
          <ul className="space-y-1">
            {sent.map((s) => (
              <li key={s.key} data-testid="accepted-command" className="font-mono tabular-nums">
                {describe(s.command, skuNames)}, effective Tick {s.effectiveTick}
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="flex gap-2">
        <button
          type="button"
          className={button}
          disabled={busy || ended || pending.length === 0}
          onClick={() => void submit()}
        >
          Submit
        </button>
        <button type="button" className={button} disabled={busy || ended} onClick={() => void endWindow()}>
          End decision window
        </button>
      </div>
    </section>
  );
}
