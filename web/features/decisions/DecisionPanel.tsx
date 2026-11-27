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
  type Assumption,
  type Command,
  type EndWindowRequest,
  type Observation,
  type SubmitCommandsRequest,
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
  kind: "order" | "price" | "overtime" | "hire";
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
  /** The world's effective assumptions: source of fees, wages and limits shown as hints. The server validates. */
  assumptions?: readonly Assumption[];
  prefill?: Prefill | null;
  /** Called with the server response after each submit. */
  onSubmitted?: (result: SubmitCommandsResult) => void;
  /** Called after the server confirms the window ended. */
  onWindowEnded?: () => void;
  /**
   * Replaces the endpoints used to submit Commands and end the window. The Duel view points them at the
   * Duel endpoints; the Branch endpoints answer 409 for a Branch that belongs to a Duel.
   */
  submitCommandsFn?: (body: SubmitCommandsRequest, idempotencyKey: string) => Promise<SubmitCommandsResult>;
  endWindowFn?: (body: EndWindowRequest) => Promise<void>;
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
  /** Past-tense text for the "Locked in" list; defaults to the text with "Order" turned into "Ordered". */
  done?: string;
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

/** Parses a small whole number ("0", "12"); null for anything else. */
function parseSmallInt(input: string): number | null {
  return /^\d{1,3}$/.test(input.trim()) ? Number(input.trim()) : null;
}

function Stepper(props: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  min: number;
  max: number;
}) {
  const n = parseSmallInt(props.value);
  return (
    <div className="flex items-end gap-2">
      <button
        type="button"
        className={`${button} min-w-11`}
        aria-label={`Decrease ${props.label.toLowerCase()}`}
        onClick={() => props.onChange(String(Math.max(props.min, (n ?? props.min) - 1)))}
      >
        −
      </button>
      <label className="flex flex-col gap-1">
        {props.label}
        <input
          className={`${field} w-24 text-center`}
          inputMode="numeric"
          value={props.value}
          onChange={(e) => props.onChange(e.target.value)}
        />
      </label>
      <button
        type="button"
        className={`${button} min-w-11`}
        aria-label={`Increase ${props.label.toLowerCase()}`}
        onClick={() => props.onChange(String(Math.min(props.max, (n ?? props.min) + 1)))}
      >
        +
      </button>
    </div>
  );
}

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
  const [rushPo, setRushPo] = useState("");
  const [otHours, setOtHours] = useState("1");
  const [hireCount, setHireCount] = useState("1");
  const [mkSku, setMkSku] = useState<number>(skus[0]?.sku ?? 0);
  const [mkSpend, setMkSpend] = useState("");

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
    } else if (prefill.kind === "overtime") {
      setOtHours("1");
    } else if (prefill.kind === "hire") {
      setHireCount("1");
    } else {
      setPrSku(prefill.skuIndex);
    }
  }
  const orderCardRef = useRef<HTMLElement | null>(null);
  const priceCardRef = useRef<HTMLElement | null>(null);
  const overtimeCardRef = useRef<HTMLElement | null>(null);
  const hireCardRef = useRef<HTMLElement | null>(null);
  const prefillNonce = prefill?.nonce;
  const prefillKind = prefill?.kind;
  useEffect(() => {
    if (prefillNonce === undefined) return;
    const refs = { price: priceCardRef, overtime: overtimeCardRef, hire: hireCardRef, order: orderCardRef };
    refs[prefillKind ?? "order"].current?.focus();
  }, [prefillNonce, prefillKind]);

  const poSkuObs = skus.find((s) => s.sku === poSku);
  const usable = (poSkuObs?.suppliers ?? []).filter((s) => s.available);
  const role: Role | null = usable.some((s) => s.role === poRole) ? poRole : (usable[0]?.role ?? null);
  const supplier = poSkuObs?.suppliers.find((s) => s.role === role);
  const qty = /^\d{1,7}$/.test(poQty) ? Number(poQty) : null;
  const estimate = supplier !== undefined && qty !== null ? qty * supplier.unitCostCents : null;
  const prSkuObs = skus.find((s) => s.sku === prSku);

  const av = (path: string): number | undefined => props.assumptions?.find((a) => a.path === path)?.value;
  const rejectionFor = (kind: Command["kind"]) => pending.find((p) => p.command.kind === kind && p.rejection !== undefined);

  const expediteFee = av("costs.expediteFeeCents");
  const expediteReduction = av("costs.expediteReductionTicks");
  const purchaseOrders = observation.purchaseOrders ?? [];
  const rushSel = purchaseOrders.find((o) => String(o.id) === rushPo) ?? purchaseOrders[0];

  const maxOvertime = av("workforce.maxOvertimePerWorker");
  const overtimeWage = av("wages.overtimePerHourCents");
  const regularWorkers = observation.workforce?.regular ?? av("workforce.regularWorkers");
  const otN = parseSmallInt(otHours);

  const maxTemps = av("workforce.maxTempWorkers");
  const tempWage = av("wages.tempPerHourCents");
  const onboardingTicks = av("workforce.tempOnboardingTicks");
  const contracted = (observation.workforce?.tempsOnShift ?? 0) + (observation.workforce?.tempsOnboarding ?? 0);
  const headroom = maxTemps === undefined ? undefined : Math.max(0, maxTemps - contracted);
  const hireN = parseSmallInt(hireCount);

  const maxMarketing = av("demand.maxMarketingSpendCentsPerHour");
  const mkSkuObs = skus.find((s) => s.sku === mkSku);
  const mkCents = parseDollarsToCents(mkSpend);

  function addPending(command: Command, text: string, arrival?: string, done?: string) {
    setPending((p) => [
      ...p,
      { key: seq, command, text, ...(arrival !== undefined ? { arrival } : {}), ...(done !== undefined ? { done } : {}) },
    ]);
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

  function addRush() {
    if (rushSel === undefined) {
      setFormError(rejectionMessage("unknown_purchase_order"));
      return;
    }
    const what = `${formatCount(rushSel.qty)} ${nameOfSku(rushSel.sku)} from the ${supplierWord(rushSel.supplier)}`;
    addPending(
      { kind: "expedite_purchase_order", poRef: rushSel.id },
      `Rush the delivery of ${what}`,
      expediteReduction !== undefined ? `arriving about ${formatHours(expediteReduction)} sooner` : undefined,
      `Rushed the delivery of ${what}`,
    );
  }

  function addOvertime() {
    if (otN === null || (maxOvertime !== undefined && otN > maxOvertime)) {
      setFormError(rejectionMessage("overtime_exceeds_limit"));
      return;
    }
    addPending(
      { kind: "schedule_overtime", qty: otN },
      otN === 0 ? "Stop overtime" : `Schedule ${formatHours(otN)} of overtime per worker`,
      undefined,
      otN === 0 ? "Stopped overtime" : `Scheduled ${formatHours(otN)} of overtime per worker`,
    );
  }

  function addHire() {
    if (hireN === null || hireN < 1) {
      setFormError(rejectionMessage("invalid_quantity"));
      return;
    }
    if (headroom !== undefined && hireN > headroom) {
      setFormError(rejectionMessage("too_many_temp_workers"));
      return;
    }
    addPending(
      { kind: "hire_temp_workers", qty: hireN },
      `Hire ${formatCount(hireN)} temporary ${hireN === 1 ? "worker" : "workers"}`,
      undefined,
      `Hired ${formatCount(hireN)} temporary ${hireN === 1 ? "worker" : "workers"}`,
    );
  }

  function addMarketing() {
    if (mkCents === null) {
      setFormError("Enter an amount in dollars per hour, for example 25 (0 stops it).");
      return;
    }
    if (maxMarketing !== undefined && mkCents > maxMarketing) {
      setFormError(rejectionMessage("marketing_exceeds_limit"));
      return;
    }
    const name = nameOfSku(mkSku);
    addPending(
      { kind: "set_marketing_spend", sku: mkSku, amountCents: mkCents },
      mkCents === 0 ? `Stop marketing ${name}` : `Spend ${formatMoney(mkCents)} an hour marketing ${name}`,
      undefined,
      mkCents === 0 ? `Stopped marketing ${name}` : `Now spending ${formatMoney(mkCents)} an hour marketing ${name}`,
    );
    setMkSpend("");
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
      const body: SubmitCommandsRequest = {
        windowTick,
        commands: batch.map((p) => p.command),
        expectedHeadTick: observation.tick,
      };
      const key = crypto.randomUUID();
      const result = props.submitCommandsFn
        ? await props.submitCommandsFn(body, key)
        : await submitCommands(branchId, body, key);
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
            text: p.done ?? p.text.replace(/^Order /, "Ordered "),
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
      const body: EndWindowRequest = { windowTick, expectedHeadTick: observation.tick };
      if (props.endWindowFn) await props.endWindowFn(body);
      else await endDecisionWindow(branchId, body);
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
            <CardRejection p={rejectionFor("place_purchase_order")} engineering={engineering} />
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
            {formatMoney(priceRangeCents(prSkuObs.referencePriceCents, prSkuObs.priceBounds).min)} and{" "}
            {formatMoney(priceRangeCents(prSkuObs.referencePriceCents, prSkuObs.priceBounds).max)}.
          </p>
        ) : null}
        <button type="button" className={button} onClick={addPrice}>
          Add to my moves
        </button>
        <CardRejection p={rejectionFor("set_price")} engineering={engineering} />
      </section>

      {expediteFee !== undefined && expediteReduction !== undefined ? (
        <section aria-labelledby="rush-card-title" className={card}>
          <h3 id="rush-card-title" className="font-medium">
            Rush a delivery
          </h3>
          {purchaseOrders.length === 0 ? (
            <p className="text-ivory-dim">No deliveries are on the way right now.</p>
          ) : (
            <>
              <label className="flex flex-col gap-1">
                Delivery to rush
                <select className={field} value={rushSel?.id ?? ""} onChange={(e) => setRushPo(e.target.value)}>
                  {purchaseOrders.map((o) => (
                    <option key={o.id} value={o.id}>
                      {`${formatCount(o.qty)} ${nameOfSku(o.sku)} from the ${supplierWord(o.supplier)}, due ${formatSimTime(Math.floor(o.dueAt / 60), startClock)}${o.expedited ? " (already rushed)" : ""}`}
                    </option>
                  ))}
                </select>
              </label>
              <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 tabular-nums">
                <dt className="flex items-center gap-1 text-ivory-dim">
                  Estimated cost
                  <InfoPopover label="rush fee">A flat fee for each delivery you rush, charged when the move takes effect.</InfoPopover>
                </dt>
                <dd data-testid="rush-fee">{formatMoney(expediteFee)}</dd>
                <dt className="text-ivory-dim">Arrives</dt>
                <dd>about {formatHours(expediteReduction)} sooner</dd>
              </dl>
              {rushSel?.expedited === true ? (
                <p className="text-ivory-dim">This delivery has already been rushed once.</p>
              ) : null}
              <button type="button" className={button} onClick={addRush}>
                Add to my moves
              </button>
            </>
          )}
          <CardRejection p={rejectionFor("expedite_purchase_order")} engineering={engineering} />
        </section>
      ) : null}

      {maxOvertime !== undefined && overtimeWage !== undefined && regularWorkers !== undefined ? (
        <section ref={overtimeCardRef} tabIndex={-1} aria-labelledby="overtime-card-title" className={card}>
          <h3 id="overtime-card-title" className="font-medium">
            Schedule overtime
          </h3>
          <Stepper label="Overtime hours per worker" value={otHours} onChange={setOtHours} min={0} max={maxOvertime} />
          <p className="text-ivory-dim">
            Each worker can do up to {formatHours(maxOvertime)} of overtime an hour. It applies every hour until you
            change it. Choose 0 hours to stop overtime.
          </p>
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 tabular-nums">
            <dt className="text-ivory-dim">Estimated extra wages per hour</dt>
            <dd data-testid="overtime-estimate">
              {otN !== null && otN <= maxOvertime ? formatMoney(regularWorkers * otN * overtimeWage) : `Choose 0 to ${maxOvertime} hours`}
            </dd>
          </dl>
          <button type="button" className={button} onClick={addOvertime}>
            Add to my moves
          </button>
          <CardRejection p={rejectionFor("schedule_overtime")} engineering={engineering} />
        </section>
      ) : null}

      {headroom !== undefined && tempWage !== undefined && onboardingTicks !== undefined ? (
        <section ref={hireCardRef} tabIndex={-1} aria-labelledby="hire-card-title" className={card}>
          <h3 id="hire-card-title" className="font-medium">
            Hire temporary staff
          </h3>
          {headroom === 0 ? (
            <p className="text-ivory-dim">You already have as many temporary workers as the business allows.</p>
          ) : (
            <>
              <Stepper label="Temporary workers" value={hireCount} onChange={setHireCount} min={1} max={headroom} />
              <p className="text-ivory-dim">
                You can add up to {formatCount(headroom)} more. They get paid from day one but only start packing after{" "}
                {formatHours(onboardingTicks)} of training.
              </p>
              <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 tabular-nums">
                <dt className="text-ivory-dim">Estimated wages per hour</dt>
                <dd data-testid="hire-estimate">
                  {hireN !== null && hireN >= 1 && hireN <= headroom ? formatMoney(hireN * tempWage) : `Choose 1 to ${formatCount(headroom)}`}
                </dd>
              </dl>
              <button type="button" className={button} onClick={addHire}>
                Add to my moves
              </button>
            </>
          )}
          <CardRejection p={rejectionFor("hire_temp_workers")} engineering={engineering} />
        </section>
      ) : null}

      {maxMarketing !== undefined ? (
        <section aria-labelledby="marketing-card-title" className={card}>
          <h3 id="marketing-card-title" className="font-medium">
            Marketing push
          </h3>
          <div className="flex flex-wrap gap-3">
            <label className="flex flex-col gap-1">
              Product to promote
              <select className={field} value={mkSku} onChange={(e) => setMkSku(Number(e.target.value))}>
                {skus.map((s) => (
                  <option key={s.sku} value={s.sku}>
                    {productName(s.id)}
                  </option>
                ))}
              </select>
            </label>
            <label className="flex flex-col gap-1">
              Spend per hour (USD)
              <input
                className={`${field} w-28`}
                inputMode="decimal"
                value={mkSpend}
                onChange={(e) => setMkSpend(e.target.value)}
              />
            </label>
          </div>
          <p className="text-ivory-dim tabular-nums">
            {mkSkuObs !== undefined ? `Now ${formatMoney(mkSkuObs.marketingCents)} an hour. ` : ""}
            You can spend up to {formatMoney(maxMarketing)} an hour. It continues until you change it. Enter 0 to stop.
          </p>
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 tabular-nums">
            <dt className="text-ivory-dim">Estimated cost per hour</dt>
            <dd data-testid="marketing-estimate">
              {mkCents !== null && mkCents <= maxMarketing ? formatMoney(mkCents) : "Enter an amount"}
            </dd>
          </dl>
          <button type="button" className={button} onClick={addMarketing}>
            Add to my moves
          </button>
          <CardRejection p={rejectionFor("set_marketing_spend")} engineering={engineering} />
        </section>
      ) : null}

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

/** The server's rejection of a move, shown next to the card that made it. */
function CardRejection({ p, engineering }: { p: Pending | undefined; engineering: boolean }) {
  if (p?.rejection === undefined) return null;
  return (
    <p role="alert" data-testid="card-rejection" className="text-clay">
      {rejectionMessage(p.rejection.code)}
      {engineering ? ` (server: ${p.rejection.code}: ${p.rejection.message})` : ""}
    </p>
  );
}

function failureText(e: unknown, engineering: boolean): string {
  if (!(e instanceof ApiError)) return "Something went wrong. Please try again.";
  const plain = requestFailureMessage(e.type);
  return engineering ? `${plain} (server: ${e.message})` : plain;
}
