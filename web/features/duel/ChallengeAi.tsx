"use client";

/**
 * "Challenge the AI manager": creates a duel on the flagship scenario with the
 * demo seed and opens it. The Rule-based AI is always offered. The LLM AI is
 * offered only when GET /managers says it is available; otherwise it is left
 * out and a one-line note explains why.
 */
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { createDuel, listManagers, type ManagerInfo } from "@/lib/api/client";
import { useUiStore } from "@/stores/ui";

/** Seed of the recorded demo. */
export const DEMO_SEED = 2026;
export const LLM_NOT_CONFIGURED = "LLM manager not configured on this server";

export function ChallengeAi() {
  const router = useRouter();
  const engineering = useUiStore((s) => s.engineering);
  const [managers, setManagers] = useState<ManagerInfo[] | null>(null);
  const [aiKind, setAiKind] = useState<"ai_rule_based" | "ai_llm">("ai_rule_based");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    listManagers()
      .then((r) => {
        if (!cancelled) setManagers(r.managers);
      })
      .catch(() => {
        if (!cancelled) setManagers([]);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const llm = managers?.find((m) => m.kind === "ai_llm");
  const llmAvailable = llm?.available === true;
  const chosen = llmAvailable ? aiKind : "ai_rule_based";

  async function start() {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      const res = await createDuel({ seed: DEMO_SEED, aiKind: chosen });
      router.push(`/duels/${encodeURIComponent(res.duel.id)}`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not start the duel. Please try again.");
      setBusy(false);
    }
  }

  return (
    <section aria-label="Challenge the AI manager" className="mt-8 space-y-3 border-t border-hairline pt-6">
      <h2 className="font-display text-xl">Or play against the AI manager</h2>
      <p className="text-sm text-ivory-dim">
        You and an AI manager run the same business through the same week, with the same customers and the same surprises.
        Compare how each of you does.
      </p>
      {llmAvailable ? (
        <fieldset className="flex flex-wrap gap-4 text-sm">
          <legend className="sr-only">Which AI manager</legend>
          <label className="flex min-h-11 items-center gap-2">
            <input type="radio" name="ai-kind" checked={chosen === "ai_rule_based"} onChange={() => setAiKind("ai_rule_based")} />
            Rule-based AI
          </label>
          <label className="flex min-h-11 items-center gap-2">
            <input type="radio" name="ai-kind" checked={chosen === "ai_llm"} onChange={() => setAiKind("ai_llm")} />
            {`LLM AI (${llm?.model ?? "model"})`}
          </label>
        </fieldset>
      ) : (
        <p className="text-sm text-ivory-dim" title={managers !== null ? LLM_NOT_CONFIGURED : undefined}>
          You will play against the Rule-based AI.
          {engineering && managers !== null ? <span data-testid="llm-note"> {LLM_NOT_CONFIGURED}.</span> : null}
        </p>
      )}
      <button
        type="button"
        onClick={() => void start()}
        disabled={busy}
        className="inline-flex min-h-12 items-center rounded-[4px] border border-edge px-6 text-base hover:border-amber hover:text-amber-hot disabled:opacity-60"
      >
        {busy ? "Starting…" : "Challenge the AI manager"}
      </button>
      {error !== null ? (
        <p role="alert" className="text-clay">
          {error}
        </p>
      ) : null}
    </section>
  );
}
