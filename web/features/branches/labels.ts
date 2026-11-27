/**
 * Plain display names for realities (branches). One helper, used wherever a
 * branch is named. Pure formatting: it never changes what the server holds.
 * A user-supplied name is stripped of control characters and limited to 64
 * characters here; React escapes it when rendered, it is never injected as HTML.
 */
import type { Branch } from "@/lib/api/client";
import { TERMS, formatSimTime } from "@/lib/glossary";

export const MAX_BRANCH_NAME_LENGTH = 64;

export interface BranchLabelContext {
  /** Every branch of the world; used to number alternative futures in creation order. */
  branches: readonly Branch[];
  startClock: string;
  /** Model identifier of the LLM AI, when the server reports one. */
  llmModel?: string | undefined;
}

/** Removes control characters, collapses whitespace and keeps at most 64 characters. */
export function sanitizeBranchName(raw: string): string {
  const cleaned = raw.replace(/[\u0000-\u001f\u007f-\u009f]/g, " ").replace(/\s+/g, " ").trim();
  return Array.from(cleaned).slice(0, MAX_BRANCH_NAME_LENGTH).join("");
}

/** "Thursday 8:00 PM", the fork time without a tick number. */
export function forkTimeText(forkTick: number, startClock: string): string {
  return formatSimTime(forkTick, startClock).replace(", ", " ");
}

/** The kind of manager, in plain words. */
export function managerKindName(kind: Branch["managerKind"], llmModel?: string): string {
  switch (kind) {
    case "human":
      return "You";
    case "ai_rule_based":
      return "Rule-based AI";
    case "ai_llm": {
      const model = llmModel === undefined ? "" : sanitizeBranchName(llmModel);
      return model === "" ? "LLM AI" : `LLM AI (${model})`;
    }
  }
}

/** Badge text for an AI-managed branch (its provenance), or null for a human one. */
export function branchProvenanceBadge(b: Branch, llmModel?: string): string | null {
  return b.managerKind === "human" ? null : managerKindName(b.managerKind, llmModel);
}

/** Position (1-based) of `b` among the human-managed alternatives, in creation order. */
function alternativeNumber(b: Branch, branches: readonly Branch[]): number {
  const alts = branches
    .filter((x) => x.parentBranchId !== null && x.managerKind === "human")
    .sort((x, y) => x.createdAt.localeCompare(y.createdAt) || x.id.localeCompare(y.id));
  const i = alts.findIndex((x) => x.id === b.id);
  return (i < 0 ? alts.length : i) + 1;
}

/**
 * "Original path", "You", "Rule-based AI (from Thursday 8:00 PM)",
 * "LLM AI (model) (from ...)", "Alternative future 2 (from ...)" or the user's
 * own name followed by the fork time.
 */
export function branchDisplayName(b: Branch, ctx: BranchLabelContext): string {
  if (b.parentBranchId === null) return TERMS.originalPath;
  const own = sanitizeBranchName(b.label);
  const from = `from ${forkTimeText(b.forkTick, ctx.startClock)}`;
  if (b.managerKind === "human") {
    if (own === "You") return "You";
    const name = own === "" ? TERMS.alternativeFuture(alternativeNumber(b, ctx.branches)) : own;
    return `${name} (${from})`;
  }
  return `${managerKindName(b.managerKind, ctx.llmModel)} (${from})`;
}
