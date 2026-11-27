/**
 * Plain-language layer (docs/ux-redesign.md section 4). The single source of
 * manager-facing wording and display formatting. Pure functions, no I/O.
 *
 * Display formatting uses integer arithmetic only (no floating point), with
 * halves rounded to even, matching the backend rounding convention.
 */

export const DISCLAIMER_FULL =
  "Synthetic simulation output under the stated assumptions. Not a forecast or guarantee of real-world results.";
export const DISCLAIMER_SHORT = "Synthetic simulation. Not a forecast.";

/* ---------- integer rounding ---------- */

/** num / den rounded half to even, for den > 0. Works for negative num. */
export function divRoundHalfEven(num: number, den: number): number {
  const q = Math.floor(num / den);
  const r = num - q * den; // 0 <= r < den
  const twice = 2 * r;
  if (twice < den) return q;
  if (twice > den) return q + 1;
  return q % 2 === 0 ? q : q + 1;
}

/* ---------- time ---------- */

const DAY_NAMES = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"];
const DAY_ABBR = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const MINUTES_PER_DAY = 1440;

export interface StartClock {
  /** 0 = Monday ... 6 = Sunday. */
  weekday: number;
  minuteOfDay: number;
}

/** Parses the scenario `startClock` label such as `Thu 00:00`. Falls back to Thursday 00:00. */
export function parseStartClock(label: string): StartClock {
  const m = /^(\w{3})\s+(\d{1,2}):(\d{2})$/.exec(label.trim());
  const weekday = m ? DAY_ABBR.findIndex((d) => d.toLowerCase() === (m[1] ?? "").toLowerCase()) : -1;
  if (!m || weekday < 0) return { weekday: 3, minuteOfDay: 0 };
  const h = Number(m[2]);
  const min = Number(m[3]);
  if (h > 23 || min > 59) return { weekday: 3, minuteOfDay: 0 };
  return { weekday, minuteOfDay: h * 60 + min };
}

export interface ClockParts {
  weekday: number;
  /** Whole days since the scenario start. */
  dayIndex: number;
  hour24: number;
  minute: number;
}

/** Wall-clock parts of the start of tick `tick` (one tick is one simulated hour). */
export function clockParts(tick: number, startClock: string): ClockParts {
  const start = parseStartClock(startClock);
  const total = start.minuteOfDay + tick * 60;
  const dayIndex = Math.floor(total / MINUTES_PER_DAY);
  const minuteOfDay = total - dayIndex * MINUTES_PER_DAY;
  return {
    weekday: (((start.weekday + dayIndex) % 7) + 7) % 7,
    dayIndex,
    hour24: Math.floor(minuteOfDay / 60),
    minute: minuteOfDay % 60,
  };
}

function timeOfDay(hour24: number, minute: number): string {
  const suffix = hour24 < 12 ? "AM" : "PM";
  const h12 = hour24 % 12 === 0 ? 12 : hour24 % 12;
  return `${h12}:${String(minute).padStart(2, "0")} ${suffix}`;
}

/** "Thursday, 7:00 PM" (long) or "Thu 7:00 PM" (short). Tick numbers never appear. */
export function formatSimTime(tick: number, startClock: string, style: "long" | "short" = "long"): string {
  const c = clockParts(tick, startClock);
  const tod = timeOfDay(c.hour24, c.minute);
  return style === "long" ? `${DAY_NAMES[c.weekday]}, ${tod}` : `${DAY_ABBR[c.weekday]} ${tod}`;
}

/** Day name only, for day bands: "Thursday". */
export function formatDayName(tick: number, startClock: string): string {
  return DAY_NAMES[clockParts(tick, startClock).weekday] ?? "";
}

const NUMBER_WORDS = ["Zero", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine", "Ten"];

/** "Five days" for 120 ticks. Partial days round up. */
export function formatHorizon(horizonTicks: number): string {
  const days = Math.max(1, Math.ceil(horizonTicks / 24));
  const word = NUMBER_WORDS[days] ?? String(days);
  return `${word} ${days === 1 ? "day" : "days"}`;
}

/** Label of the final simulated hour: "Monday, 11:00 PM" for 120 ticks from Thu 00:00. */
export function formatHorizonEnd(horizonTicks: number, startClock: string): string {
  return formatSimTime(horizonTicks - 1, startClock);
}

/** "8 hours", "1 hour". */
export function formatHours(n: number): string {
  return `${n} ${n === 1 ? "hour" : "hours"}`;
}

/* ---------- money and rates ---------- */

export interface MoneyOptions {
  /** "cents" (default) always shows two decimals; "whole" rounds to whole dollars. */
  precision?: "cents" | "whole";
  /** Prefix "+" on positive values (for differences and profit). */
  signed?: boolean;
}

function groupThousands(n: number): string {
  return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

/** Integer cents to "$1,234.50" / "-$12.00". Integer math only. */
export function formatMoney(cents: number, opts: MoneyOptions = {}): string {
  const whole = opts.precision === "whole";
  const negative = cents < 0;
  const abs = Math.abs(cents);
  let body: string;
  if (whole) {
    body = `$${groupThousands(divRoundHalfEven(abs, 100))}`;
  } else {
    const dollars = Math.floor(abs / 100);
    body = `$${groupThousands(dollars)}.${String(abs - dollars * 100).padStart(2, "0")}`;
  }
  const isZero = whole ? divRoundHalfEven(abs, 100) === 0 : abs === 0;
  if (negative && !isZero) return `-${body}`;
  if (opts.signed && !isZero) return `+${body}`;
  return body;
}

/** Basis points to a percent with one decimal: 9650 -> "96.5%". Halves round to even. */
export function formatPercentBps(bps: number): string {
  const tenths = divRoundHalfEven(bps, 10);
  const negative = tenths < 0;
  const abs = Math.abs(tenths);
  const whole = Math.floor(abs / 10);
  return `${negative ? "-" : ""}${whole}.${abs - whole * 10}%`;
}

/** 1234 -> "1,234". */
export function formatCount(n: number): string {
  return n < 0 ? `-${groupThousands(-n)}` : groupThousands(n);
}

/* ---------- products ---------- */

const PRODUCT_NAMES: Record<string, string> = {
  headphones: "Headphones",
  charger: "Chargers",
  speaker: "Speakers",
  watchband: "Watch bands",
};

/** Display name for a scenario product id. Unknown ids are title-cased. Never "SKU n". */
export function productName(id: string): string {
  const known = PRODUCT_NAMES[id];
  if (known !== undefined) return known;
  const words = id.replace(/[_-]+/g, " ").trim().split(/\s+/).filter((w) => w !== "");
  if (words.length === 0) return "Product";
  return words.map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join(" ");
}

/** Product name from a SKU index, using the scenario's ordered product list. */
export function productNameByIndex(skus: ReadonlyArray<{ id: string }>, index: number): string {
  const sku = skus[index];
  return sku === undefined ? "Product" : productName(sku.id);
}

/* ---------- terms ---------- */

export const TERMS = {
  originalPath: "Original path",
  alternative: "Your alternative",
  exploreFuture: "Explore another future",
  alternativeFuture: (n: number): string => `Alternative future ${n}`,
  yourMove: "Your move",
  decisionsRemaining: (left: number, total: number): string => `Decisions remaining: ${left} of ${total}`,
  waitingForInventory: "Waiting for inventory",
  waitingForPacking: "Waiting for packing capacity",
  primarySupplier: "Normal supplier",
  backupSupplier: "Backup supplier",
  deliveryTime: "Delivery time (hours)",
  ledger: "Where this number comes from",
  assumptions: "How this is modelled",
} as const;

export function supplierRoleName(role: "primary" | "backup"): string {
  return role === "primary" ? TERMS.primarySupplier : TERMS.backupSupplier;
}

const DISRUPTION_LABELS: Record<string, string> = {
  demand_surge: "Orders surging",
  supplier_outage: "Supplier down",
  workforce_absence: "Team members out",
};

/** Names of the surprises a user can add, as shown on the "Add a surprise" control. */
const DISRUPTION_CHOICE_LABELS: Record<string, string> = {
  supplier_outage: "Supplier outage",
  demand_surge: "Demand surge",
  workforce_absence: "Staff absence",
};

export function disruptionChoiceLabel(kind: string): string {
  return DISRUPTION_CHOICE_LABELS[kind] ?? "Something unexpected";
}

export function disruptionLabel(kind: string): string {
  return DISRUPTION_LABELS[kind] ?? "Something unexpected";
}

/* ---------- metrics ---------- */

export type MetricKey = "cash" | "netProfit" | "serviceLevel" | "fillRate" | "backlog" | "stockValue" | "contribution";

export const METRIC_LABELS: Record<MetricKey, string> = {
  cash: "Cash",
  netProfit: "Profit so far",
  serviceLevel: "Orders delivered on time",
  fillRate: "Items shipped (of those due)",
  backlog: "Orders waiting",
  stockValue: "Stock value",
  contribution: "Profit before wages and overhead",
};

/** Explanations match the formulas in backend/api/openapi.yaml (Metrics). */
export function metricExplanation(key: MetricKey, ctx: { lateBacklogOrders?: number } = {}): string {
  switch (key) {
    case "cash":
      return "Money in the bank right now. Orders and wages move it as they happen.";
    case "netProfit":
      return "Sales minus the cost of the goods you shipped, shipping, wages, overhead, marketing and credits for late orders. Sales count when an order ships.";
    case "serviceLevel":
      return "Of the orders whose ship-by time has passed, the share that shipped in time. It shows 100% until some orders come due.";
    case "fillRate":
      return "Of the items customers were expecting by now, the share that has shipped. It shows 100% until some items come due.";
    case "backlog": {
      const late = ctx.lateBacklogOrders;
      return late === undefined
        ? "Orders that haven't shipped yet."
        : `Orders that haven't shipped yet. ${formatCount(late)} of them ${late === 1 ? "is" : "are"} already late.`;
    }
    case "stockValue":
      return "What the products in the warehouse are worth at your cost.";
    case "contribution":
      return "Sales minus the cost of goods shipped and shipping.";
  }
}

/* ---------- rejections ---------- */

const REJECTION_COPY: Record<string, string> = {
  unknown_command: "That kind of move isn't available.",
  not_implemented: "That kind of move isn't available yet.",
  budget_exhausted: "No decisions remaining this window.",
  unknown_sku: "That product isn't part of this business.",
  price_out_of_bounds: "That price is outside the allowed range.",
  invalid_quantity: "That quantity isn't allowed. Use a whole number of at least 1.",
  unknown_supplier: "That supplier isn't available.",
  supplier_unavailable: "That supplier can't deliver right now.",
  insufficient_cash: "Not enough cash for this order.",
  unknown_purchase_order: "That delivery isn't on order any more.",
  already_expedited: "That delivery has already been rushed.",
  overtime_exceeds_limit: "That is more overtime per worker than the business allows.",
  too_many_temp_workers: "That would be more temporary workers than the business allows.",
  marketing_exceeds_limit: "That is more marketing spend per hour than the business allows.",
};

/** Plain-language sentence for a server rejection code. Unknown codes get a generic sentence. */
export function rejectionMessage(code: string | undefined): string {
  if (code !== undefined) {
    const known = REJECTION_COPY[code];
    if (known !== undefined) return known;
  }
  return "That move wasn't accepted.";
}

/** Sentence for a failed request, keyed by the problem slug of the response. */
export function requestFailureMessage(type: string): string {
  switch (type) {
    case "stale-head":
      return "The situation changed while you were deciding. Look at the latest hour and try again.";
    case "decision-stale":
      return "The AI's answer arrived after the situation changed, so it was discarded. Press Continue to try again.";
    case "validation":
      return "That move wasn't accepted.";
    default:
      return "Something went wrong. Please try again.";
  }
}

/** Display labels for ledger accounts. */
const ACCOUNT_LABELS: Record<string, string> = {
  Cash: "Cash",
  Inventory: "Stock",
  Opening_Equity: "Starting funds",
  Revenue: "Sales",
  Cost_Of_Goods_Sold: "Cost of goods shipped",
  Labor_Expense: "Wages",
  Freight_Expense: "Shipping",
  Marketing_Expense: "Marketing",
  Overhead_Expense: "Overhead",
  Service_Credit_Expense: "Credits for late orders",
};

export function accountLabel(account: string): string {
  return ACCOUNT_LABELS[account] ?? account.replace(/_/g, " ");
}

/**
 * Allowed price range hint. Prefers the server's bounds from the observation; without them it falls back
 * to the default 50% to 200% of the reference price (integer math; the server decides).
 */
export function priceRangeCents(
  referencePriceCents: number,
  serverBounds?: { minCents: number; maxCents: number },
): { min: number; max: number } {
  if (serverBounds !== undefined) return { min: serverBounds.minCents, max: serverBounds.maxCents };
  return { min: divRoundHalfEven(referencePriceCents, 2), max: referencePriceCents * 2 };
}
