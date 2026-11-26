"use client";

import { useState } from "react";
import { forkBranch, type Branch } from "@/lib/api/client";

export const MAX_LABEL_LENGTH = 64;

export interface ForkButtonProps {
  /** The displayed Branch. */
  branchId: string;
  /** The displayed Tick. */
  tick: number;
  /** The displayed Branch's Head_Tick; forking beyond it is disabled. */
  headTick: number;
  onForked: (branch: Branch) => void;
}

/** Forks the displayed Branch at the displayed Tick; one request per press. */
export function ForkButton({ branchId, tick, headTick, onForked }: ForkButtonProps) {
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
        label: trimmed === "" ? `Fork at tick ${tick}` : trimmed,
      });
      setLabel("");
      onForked(branch);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Fork failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-1 font-sans text-xs">
      <label className="text-[#8A93A6]">
        Fork label
        <input
          className="ml-2 rounded-[4px] border border-[#232B3A] bg-[#121722] px-2 py-1 text-[#E6E9EF]"
          value={label}
          maxLength={MAX_LABEL_LENGTH}
          onChange={(e) => setLabel(e.target.value.slice(0, MAX_LABEL_LENGTH))}
          placeholder={`Fork at tick ${tick}`}
        />
      </label>
      <button
        type="button"
        disabled={disabled}
        onClick={() => void press()}
        className="rounded-[4px] border border-[#F2A93B] px-3 py-1 text-[#F2A93B] disabled:opacity-50"
      >
        {busy ? "Forking…" : `Fork at tick ${tick}`}
      </button>
      {beyondHead ? <span className="text-[#8A93A6]">Tick {tick} is beyond the head tick {headTick}.</span> : null}
      {error !== null ? (
        <span role="alert" className="text-[#FF7A7A]">
          {error}
        </span>
      ) : null}
    </div>
  );
}
