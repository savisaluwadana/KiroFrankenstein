/**
 * Plain-language text for the Duel view. Formatting only: every number is an
 * API value, never computed here (the one exception is the display subtraction
 * of two server values, labelled "Observed differences").
 */
import type { Command } from "@/lib/api/client";
import { formatCount, formatMoney, supplierRoleName } from "@/lib/glossary";

/** Hundredths of a point to "xx.xx". Integer math only. */
export function formatPoints(hundredths: number): string {
  const negative = hundredths < 0;
  const abs = Math.abs(hundredths);
  const whole = Math.floor(abs / 100);
  return `${negative ? "-" : ""}${whole}.${String(abs - whole * 100).padStart(2, "0")}`;
}

/** "0.10" -> "0.1" for prose. */
export function formatPointsShort(hundredths: number): string {
  return formatPoints(hundredths).replace(/(\.\d)0$/, "$1");
}

/** Weight in basis points as a percent: 5000 -> "50%". */
export function formatWeight(bps: number): string {
  if (bps % 100 === 0) return `${bps / 100}%`;
  const tenths = Math.floor(bps / 10);
  return `${Math.floor(tenths / 10)}.${tenths % 10}%`;
}

const FALLBACK_PLAIN: Record<string, string> = {
  timeout: "timed out",
  http_error: "service error",
  invalid_json: "invalid reply",
  schema_violation: "reply broke the rules",
  budget_exhausted: "call limit reached",
  llm_unavailable: "service unavailable",
};

const FALLBACK_SENTENCE: Record<string, string> = {
  timeout: "The AI service took too long, so the rule-based manager decided instead.",
  http_error: "The AI service had a problem and could not answer, so the rule-based manager decided instead.",
  invalid_json: "The AI's answer wasn't in a usable format, so the rule-based manager decided instead.",
  schema_violation: "The AI's answer broke the rules of the game, so the rule-based manager decided instead.",
  budget_exhausted: "The AI call limit for this game was reached, so the rule-based manager decided instead.",
  llm_unavailable: "The AI service isn't available right now, so the rule-based manager decided instead.",
};

export function fallbackReasonPlain(code: string): string {
  return FALLBACK_PLAIN[code] ?? "unavailable";
}

export function fallbackSentence(code: string): string {
  return FALLBACK_SENTENCE[code] ?? "The AI service isn't available right now, so the rule-based manager decided instead.";
}

/** Replaces a raw fallback reason code in a server label with plain words. */
export function plainManagerLabel(label: string): string {
  return label.replace(/LLM fallback: ([a-z_]+)/, (_m, code: string) => `LLM fallback: ${fallbackReasonPlain(code)}`);
}

/** One move as a sentence. Never shows product indexes. */
export function describeMove(c: Command, productNames: readonly string[]): string {
  const product = c.sku !== undefined ? (productNames[c.sku] ?? "a product") : "a product";
  switch (c.kind) {
    case "place_purchase_order":
      return `Ordered ${formatCount(c.qty ?? 0)} ${product} from the ${supplierRoleName(c.role ?? "primary").toLowerCase()}`;
    case "set_price":
      return `Set the ${product} price to ${formatMoney(c.amountCents ?? 0)}`;
    case "expedite_purchase_order":
      return "Rushed an order that was on its way";
    case "schedule_overtime":
      return (c.qty ?? 0) === 0
        ? "Cancelled overtime"
        : `Scheduled ${formatCount(c.qty ?? 0)} overtime ${(c.qty ?? 0) === 1 ? "hour" : "hours"} per worker each hour`;
    case "hire_temp_workers":
      return `Brought in ${formatCount(c.qty ?? 0)} temporary ${(c.qty ?? 0) === 1 ? "worker" : "workers"}`;
    case "set_marketing_spend":
      return (c.amountCents ?? 0) === 0
        ? `Stopped marketing ${product}`
        : `Spent ${formatMoney(c.amountCents ?? 0)} an hour marketing ${product}`;
    default:
      return "Made a decision";
  }
}
