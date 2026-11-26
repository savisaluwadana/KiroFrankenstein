"use client";

import { useState } from "react";
import { forkBranch, type Branch } from "@/lib/api/client";
import { TERMS } from "@/lib/glossary";

export const MAX_LABEL_LENGTH = 64;

export interface ForkButtonProps {
  /** The displayed reality. */
  branchId: string;
  /** The displayed hour. */
  tick: number;
  /** The displayed reality's head hour; forking beyond it is not possible. */
  headTick: number;
  /** Name used when the user leaves the name empty, for example "Alternative future 1". */
  defaultLabel: string;
  /** The displayed time as words, for example "Thursday, 8:00 PM". */
  whenLabel: string;
  onForked: (branch: Branch) => void;
}

/**
 * "Explore another future": forks the displayed reality at the displayed hour.
 * One request per press. The signature split animation comes in a later phase.
 */
export function ForkButton({ branchId, tick, headTick, defaultLabel, whenLabel, onForked }: ForkButtonProps) {
  const [label, setLabel] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const beyondHead = tick > headTick;
  const disabled = busy || beyondHead;

  async function press() {
    if (disabled) return;
    setBusy(true);
    setError(null);
    try {
      const trimmed = label.trim().slice(0, MAX_LABEL_LENGTH);
      const branch = await forkBranch(branchId, {
        atTick: tick,
        label: trimmed === "" ? defaultLabel : trimmed,
      });
      setLabel("");
      onForked(branch);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not create the alternative future.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section aria-label={TERMS.exploreFuture} className="space-y-2 rounded-[8px] border border-hairline bg-surface p-3 text-sm text-ivory">
      <h3 className="font-medium">{TERMS.exploreFuture}</h3>
      <p className="text-xs text-ivory-dim">
        Go back to {whenLabel} and try different decisions in a second future. Everything before that hour stays exactly
        as it happened, and the original path is never changed.
      </p>
      <label className="flex flex-col gap-1 text-xs text-ivory-dim">
        Name (optional)
        <input
          className="min-h-11 rounded-[4px] border border-hairline bg-ink px-2 text-sm text-ivory"
          value={label}
          maxLength={MAX_LABEL_LENGTH}
          onChange={(e) => setLabel(e.target.value.slice(0, MAX_LABEL_LENGTH))}
          placeholder={defaultLabel}
        />
      </label>
      <button
        id="explore-future"
        type="button"
        disabled={disabled}
        onClick={() => void press()}
        className="inline-flex min-h-11 items-center rounded-[4px] border border-amber px-3 text-amber disabled:opacity-50"
      >
        {busy ? "Creating…" : TERMS.exploreFuture}
      </button>
      {beyondHead ? <p className="text-xs text-ivory-dim">That hour hasn&apos;t happened yet in this reality.</p> : null}
      {error !== null ? (
        <p role="alert" className="text-clay">
          {error}
        </p>
      ) : null}
    </section>
  );
}
