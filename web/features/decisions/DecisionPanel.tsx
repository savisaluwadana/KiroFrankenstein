"use client";

/**
 * "Your move": guided decision cards for the open decision window. Only the
 * moves the server can validate are offered: order more of a product, and
 * change a product's price. Every number shown is a server value, formatted
 * only; the one multiplication (quantity x unit cost) is labelled "Estimated
 * cost". The panel never computes outcomes. Validation, effective hours and
 * rejection codes come from the API response.
 */
import { useEffect, useRef, useState } from "react";
import {
  ApiError,
  endDecisionWindow,
  submitCommands,
  type Command,
  type Observation,
  type SubmitCommandsResult,
} from "@/lib/api/client";
import {
  TERMS,
  formatCount,
  formatHours,
  formatMoney,
  formatSimTime,
  priceRangeCents,
  productName,
  rejectionMessage,
  requestFailureMessage,
  supplierRoleName,
} from "@/lib/glossary";
import { useUiStore } from "@/stores/ui";
import { InfoPopover } from "@/features/studio/InfoPopover";

type Role = "primary" | "backup";

/** Asks the panel to open a card with these values (from a crisis card action). */
export interface Prefill {
  /** Changes on every request so the same request can be repeated. */
  nonce: number;
  kind: "order" | "price";
  skuIndex: number;
  role?: Role;
}

/** A server-computed suggestion (demo path). Never carries reasons. */
export interface Suggestion {
  skuIndex: number;
  role: Role;
  qty: number;
}

export interface DecisionPanelProps {
  observation: Observation;
  /** Tick of the open decision window. */
  windowTick: number;
  branchId: string;
  /** Moves still allowed in this window, from the server. */
  budgetRemaining: number;
  /** Scenario start label, to show effective times as clock times. */
  startClock: string;
  suggestion?: Suggestion | null;
  prefill?: Prefill | null;
  /** Called with the server response after each submit. */
  onSubmitted?: (result: SubmitCommandsResult) => void;
  /** Called after the server confirms the window ended. */
  onWindowEnded?: () => void;
}

/** Parses a dollar string such as "12", "12.5" or "12.50" into integer cents without floating point. */
export function parseDollarsToCents(input: string): number | null {
  const m = /^(\d{1,12})(?:\.(\d{1,2}))?$/.exec(input.trim());
  if (m === null) return null;
  const dollars = m[1] ?? "0";
  const frac = (m[2] ?? "").padEnd(2, "0");
  return Number(dollars) * 100 + Number(frac);
}

interface Pending {
  key: number;
  command: Command;
  text: string;
  /** "arriving in about N hours" for orders. */
  arrival?: string;
  rejection?: { code: string; message: string };
}

interface Sent {
  key: number;
  text: string;
  arrival?: string;
  effectiveTick: number;
}

const DEFAULT_QTY = 100;
const QTY_STEP = 10;
const MAX_QTY = 9_999_999;

const field =
  "min-h-11 rounded-[4px] border border-hairline bg-ink px-2 text-sm text-ivory tabular-nums focus-visible:outline-2";
const button =
  "inline-flex min-h-11 items-center justify-center rounded-[4px] border border-hairline bg-surface px-3 text-sm text-ivory transition-colors duration-150 ease-out hover:border-amber disabled:opacity-40 motion-reduce:transition-none";
const primary = `${button} border-amber text-amber`;
const card = "space-y-3 rounded-[8px] border border-hairline bg-surface p-3";

function supplierWord(role: Role): string {
  return supplierRoleName(role).toLowerCase();
}

export function DecisionPanel(props: DecisionPanelProps) {
  const { observation, windowTick, branchId, budgetRemaining, startClock, onSubmitted, onWindowEnded } = props;
  const engineering = useUiStore((s) => s.engineering);
  const skus = observation.skus;
  const nameOfSku = (sku: number): string => {
    const s = skus.find((x) => x.sku === sku);
    return s === undefined ? "this product" : productName(s.id);
  };

  const [seq, setSeq] = useState(0);
  const [pending, setPending] = useState<Pending[]>([]);
  const [sent, setSent] = useState<Sent[]>([]);
  const [formError, setFormError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [ended, setEnded] = useState(false);

  const [poSku, setPoSku] = useState<number>(skus[0]?.sku ?? 0);
  const [poRole, setPoRole] = useState<Role>("primary");
  const [poQty, setPoQty] = useState(String(DEFAULT_QTY));
  const [prSku, setPrSku] = useState<number>(skus[0]?.sku ?? 0);
  const [prPrice, setPrPrice] = useState("");

  const [total] = useState(observation.window.commandBudget);
  const [remaining, setRemaining] = useState(budgetRemaining);
  const [prevBudget, setPrevBudget] = useState(budgetRemaining);
  if (prevBudget !== budgetRemaining) {
    setPrevBudget(budgetRemaining);
    setRemaining(budgetRemaining);
  }

  const [seenNonce, setSeenNonce] = useState(props.prefill?.nonce ?? 0);
  const prefill = props.prefill ?? null;
  if (prefill !== null && prefill.nonce !== seenNonce) {
    setSeenNonce(prefill.nonce);
    if (prefill.kind === "order") {
      setPoSku(prefill.skuIndex);
      if (prefill.role !== undefined) setPoRole(prefill.role);
    } else {
      setPrSku(prefill.skuIndex);
    }
  }
  const orderCardRef = useRef<HTMLElement | null>(null);
  const priceCardRef = useRef<HTMLElement | null>(null);
  const prefillNonce = prefill?.nonce;
  const prefillKind = prefill?.kind;
  useEffect(() => {
    if (prefillNonce === undefined) return;
    (prefillKind === "price" ? priceCardRef : orderCardRef).current?.focus();
  }, [prefillNonce, prefillKind]);

  const poSkuObs = skus.find((s) => s.sku === poSku);
  const usable = (poSkuObs?.suppliers ?? []).filter((s) => s.available);
  const role: Role | null = usable.some((s) => s.role === poRole) ? poRole : (usable[0]?.role ?? null);
  const supplier = poSkuObs?.suppliers.find((s) => s.role === role);
  const qty = /^\d{1,7}$/.test(poQty) ? Number(poQty) : null;
  const estimate = supplier !== undefined && qty !== null ? qty * supplier.unitCostCents : null;
  const prSkuObs = skus.find((s) => s.sku === prSku);

  function addPending(command: Command, text: string, arrival?: string) {
    setPending((p) => [...p, { key: seq, command, text, ...(arrival !== undefined ? { arrival } : {}) }]);
    setSeq((n) => n + 1);
    setFormError(null);
  }

  function orderText(sku: number, r: Role, q: number): string {
    return `Order ${formatCount(q)} ${nameOfSku(sku)} from the ${supplierWord(r)}`;
  }

  function addOrder() {
    if (qty === null || qty < 1 || qty > MAX_QTY) {
      setFormError(rejectionMessage("invalid_quantity"));
      return;
    }
    if (role === null || supplier === undefined) {
      setFormError(rejectionMessage("supplier_unavailable"));
      return;
    }
    addPending(
      { kind: "place_purchase_order", sku: poSku, role, qty },
      orderText(poSku, role, qty),
      `arriving in about ${formatHours(supplier.leadTimeTicks)}`,
    );
  }

  function addPrice() {
    const cents = parseDollarsToCents(prPrice);
    if (cents === null) {
      setFormError("Enter a price in dollars, for example 19.99.");
      return;
    }
    addPending({ kind: "set_price", sku: prSku, amountCents: cents }, `Change ${nameOfSku(prSku)} price to ${formatMoney(cents)}`);
    setPrPrice("");
  }

  function addSuggestion(s: Suggestion) {
    const lead = skus.find((x) => x.sku === s.skuIndex)?.suppliers.find((x) => x.role === s.role)?.leadTimeTicks;
    addPending(
      { kind: "place_purchase_order", sku: s.skuIndex, role: s.role, qty: s.qty },
      orderText(s.skuIndex, s.role, s.qty),
      lead !== undefined ? `arriving in about ${formatHours(lead)}` : undefined,
    );
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
      const rejectedBy = new Map(result.rejected.map((r) => [r.index, r]));
      const acceptedQueue = [...result.accepted];
      const stillPending: Pending[] = [];
      const newlySent: Sent[] = [];
      batch.forEach((p, i) => {
        const rej = rejectedBy.get(i);
        if (rej !== undefined) {
          stillPending.push({
            ...p,
            rejection: { code: rej.code ?? "rejected", message: rej.message ?? "" },
          });
          return;
        }
        const acc = acceptedQueue.shift();
        if (acc !== undefined) {
          newlySent.push({
            key: p.key,
            text: p.text.replace(/^Order /, "Ordered "),
            ...(p.arrival !== undefined ? { arrival: p.arrival } : {}),
            effectiveTick: acc.effectiveTick,
          });
        }
      });
      setPending(stillPending);
      setSent((s) => [...s, ...newlySent]);
      setRemaining(result.budgetRemaining);
      onSubmitted?.(result);
    } catch (e) {
      setFormError(failureText(e, engineering));
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
      setFormError(failureText(e, engineering));
    } finally {
      setBusy(false);
    }
  }

  const sug = props.suggestion ?? null;
  const sugOffered =
    sug !== null && skus.find((s) => s.sku === sug.skuIndex)?.suppliers.some((s) => s.role === sug.role && s.available) === true;

  return (
    <section aria-label={TERMS.yourMove} className="space-y-4 text-sm text-ivory">
      <header className="space-y-1">
        <h2 className="font-display text-xl">{TERMS.yourMove}</h2>
        <p className="text-ivory-dim">
          You have <span className="tabular-nums text-ivory" data-testid="obs-cash">{formatMoney(observation.cashCents)}</span>.
          Each move you lock in takes effect an hour from now.
        </p>
        <p data-testid="obs-budget" className="tabular-nums">
          {TERMS.decisionsRemaining(remaining, total)}
        </p>
      </header>

      {sug !== null && sugOffered ? (
        <div className={card} data-testid="suggestion">
          <p>
            Suggested: order {formatCount(sug.qty)} {nameOfSku(sug.skuIndex)} from the {supplierWord(sug.role)}.
          </p>
          <p className="text-xs text-ivory-dim">A suggestion, not a guarantee.</p>
          <button type="button" className={button} onClick={() => addSuggestion(sug)}>
            Add suggestion to my moves
          </button>
        </div>
      ) : null}

      <section ref={orderCardRef} tabIndex={-1} aria-labelledby="order-card-title" className={card}>
        <h3 id="order-card-title" className="font-medium">
          {poSkuObs !== undefined ? `Order more ${nameOfSku(poSku)}` : "Order more"}
          {role !== null ? ` from the ${supplierWord(role)}` : ""}
        </h3>
        <div className="flex flex-wrap gap-3">
          <label className="flex flex-col gap-1">
            Product
            <select className={field} value={poSku} onChange={(e) => setPoSku(Number(e.target.value))}>
              {skus.map((s) => (
                <option key={s.sku} value={s.sku}>
                  {productName(s.id)}
                </option>
              ))}
            </select>
          </label>
          {usable.length > 1 ? (
            <label className="flex flex-col gap-1">
              Supplier
              <select
                className={field}
                value={role ?? ""}
                onChange={(e) => setPoRole(e.target.value === "backup" ? "backup" : "primary")}
              >
                {usable.map((s) => (
                  <option key={s.role} value={s.role}>
                    {supplierRoleName(s.role)}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
        </div>
        {poSkuObs?.suppliers
          .filter((s) => !s.available)
          .map((s) => (
            <p key={s.role} className="text-ivory-dim">
              {supplierRoleName(s.role)} is unavailable until {formatSimTime(s.availableFromTick, startClock)}.
            </p>
          ))}
        {supplier !== undefined && role !== null ? (
          <>
            <div className="flex items-end gap-2">
              <button
                type="button"
                className={`${button} min-w-11`}
                aria-label="Decrease quantity"
                onClick={() => setPoQty(String(Math.max(1, (qty ?? DEFAULT_QTY) - QTY_STEP)))}
              >
                −
              </button>
              <label className="flex flex-col gap-1">
                Quantity
                <input
                  className={`${field} w-24 text-center`}
                  inputMode="numeric"
                  value={poQty}
                  onChange={(e) => setPoQty(e.target.value)}
                />
              </label>
              <button
                type="button"
                className={`${button} min-w-11`}
                aria-label="Increase quantity"
                onClick={() => setPoQty(String(Math.min(MAX_QTY, (qty ?? 0) + QTY_STEP)))}
              >
                +
              </button>
            </div>
            <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 tabular-nums">
              <dt className="text-ivory-dim">Price each</dt>
              <dd>{formatMoney(supplier.unitCostCents)}</dd>
              <dt className="flex items-center gap-1 text-ivory-dim">
                Estimated cost
                <InfoPopover label="estimated cost">
                  Quantity times the price each. Shipping to customers is paid when an order ships, so this is an
                  estimate of what the order will cost you.
                </InfoPopover>
              </dt>
              <dd data-testid="estimated-cost">{estimate !== null ? formatMoney(estimate) : "Enter a quantity"}</dd>
              <dt className="text-ivory-dim">Arrives</dt>
              <dd>about {formatHours(supplier.leadTimeTicks)} after your move</dd>
            </dl>
            <button type="button" className={button} onClick={addOrder}>
              Add to my moves
            </button>
          </>
        ) : (
          <p className="text-ivory-dim">No supplier can deliver this product right now.</p>
        )}
      </section>

      <section ref={priceCardRef} tabIndex={-1} aria-labelledby="price-card-title" className={card}>
        <h3 id="price-card-title" className="font-medium">
          Change {nameOfSku(prSku)} price
        </h3>
        <div className="flex flex-wrap gap-3">
          <label className="flex flex-col gap-1">
            Product to reprice
            <select
              className={field}
              value={prSku}
              onChange={(e) => setPrSku(Number(e.target.value))}
            >
              {skus.map((s) => (
                <option key={s.sku} value={s.sku}>
                  {productName(s.id)}
                </option>
              ))}
            </select>
          </label>
          <label className="flex flex-col gap-1">
            New price (USD)
            <input
              className={`${field} w-28`}
              inputMode="decimal"
              value={prPrice}
              onChange={(e) => setPrPrice(e.target.value)}
            />
          </label>
        </div>
        {prSkuObs !== undefined ? (
          <p className="text-ivory-dim tabular-nums">
            Now {formatMoney(prSkuObs.priceCents)}. Allowed range: between{" "}
            {formatMoney(priceRangeCents(prSkuObs.referencePriceCents).min)} and{" "}
            {formatMoney(priceRangeCents(prSkuObs.referencePriceCents).max)}.
          </p>
        ) : null}
        <button type="button" className={button} onClick={addPrice}>
          Add to my moves
        </button>
      </section>

      {formError !== null && (
        <p role="alert" className="text-clay">
          {formError}
        </p>
      )}

      <div>
        <h3 className="text-ivory-dim">Your moves so far</h3>
        {pending.length === 0 ? (
          <p className="text-ivory-dim">Nothing added yet.</p>
        ) : (
          <ul className="space-y-2">
            {pending.map((p) => (
              <li key={p.key} data-testid="pending-command">
                <span>{p.text}</span>
                {p.arrival !== undefined ? <span className="text-ivory-dim">, {p.arrival}</span> : null}
                {p.rejection !== undefined && (
                  <span role="alert" className="ml-2 text-clay">
                    {rejectionMessage(p.rejection.code)}
                    {engineering ? ` (server: ${p.rejection.code}: ${p.rejection.message})` : ""}
                  </span>
                )}
                <button
                  type="button"
                  className="ml-2 min-h-11 text-ivory-dim underline"
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
          <h3 className="text-ivory-dim">Locked in</h3>
          <ul className="space-y-1">
            {sent.map((s) => (
              <li key={s.key} data-testid="accepted-command">
                {s.text}
                {s.arrival !== undefined ? `, ${s.arrival}` : ""}. Takes effect at{" "}
                {formatSimTime(s.effectiveTick, startClock)}.
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          className={primary}
          disabled={busy || ended || pending.length === 0}
          onClick={() => void submit()}
        >
          Lock in decisions
        </button>
        <button type="button" className={button} disabled={busy || ended} onClick={() => void endWindow()}>
          Continue
        </button>
      </div>
      {ended ? <p className="text-ivory-dim">Done for now. Press Play to carry on.</p> : null}
    </section>
  );
}

function failureText(e: unknown, engineering: boolean): string {
  if (!(e instanceof ApiError)) return "Something went wrong. Please try again.";
  const plain = requestFailureMessage(e.type);
  return engineering ? `${plain} (server: ${e.message})` : plain;
}
