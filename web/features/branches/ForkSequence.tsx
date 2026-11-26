"use client";

import { useEffect } from "react";
import { TERMS } from "@/lib/glossary";
import { FORK_SEQUENCE_MS, FORK_STATIC_MS, caption, type ForkSequenceState } from "./forkSequenceMachine";

/**
 * The shared history ribbon splits into two lanes: the original path (ivory,
 * solid) and the alternative (amber, dashed). Only transforms and opacity
 * animate; no number is shown. Click, Skip or Esc ends it.
 */
export function ForkSequence({ state, onDone }: { state: ForkSequenceState; onDone: () => void }) {
  const phase = state.phase;
  useEffect(() => {
    if (phase === "idle") return;
    const t = setTimeout(onDone, phase === "playing" ? FORK_SEQUENCE_MS : FORK_STATIC_MS);
    function onKey(e: KeyboardEvent): void {
      if (e.key === "Escape") onDone();
    }
    document.addEventListener("keydown", onKey);
    return () => {
      clearTimeout(t);
      document.removeEventListener("keydown", onKey);
    };
  }, [phase, onDone]);

  if (state.phase === "idle") return null;
  const anim = state.phase === "playing";
  const cls = (name: string): string | undefined => (anim ? name : undefined);
  const origin = { transformBox: "view-box", transformOrigin: "320px 70px" } as const;

  return (
    <div
      data-testid="fork-sequence"
      data-phase={state.phase}
      onClick={onDone}
      className={`absolute inset-0 z-10 flex cursor-pointer flex-col items-center justify-center gap-3 rounded-[8px] bg-ink/85 p-4 ${anim ? "fork-overlay" : ""}`}
    >
      <svg aria-hidden="true" viewBox="0 0 640 140" className="w-full max-w-xl">
        <g className={cls("fork-trunk")}>
          <path d="M24 70 H320" fill="none" stroke="#F3EDE0" strokeWidth="4" strokeLinecap="round" />
          <text x="24" y="52" fill="#B9B2A3" fontSize="13" fontFamily="sans-serif">
            Shared history
          </text>
        </g>
        <g className={cls("fork-lanes")} style={origin}>
          <path d="M320 70 C380 70 400 38 460 38 H616" fill="none" stroke="#F3EDE0" strokeWidth="4" strokeLinecap="round" vectorEffect="non-scaling-stroke" />
          <path d="M320 70 C380 70 400 102 460 102 H616" fill="none" stroke="#E8A33D" strokeWidth="4" strokeLinecap="round" strokeDasharray="10 6" vectorEffect="non-scaling-stroke" />
        </g>
        <g className={cls("fork-labels")}>
          <text x="616" y="26" textAnchor="end" fill="#F3EDE0" fontSize="14" fontFamily="sans-serif">
            {TERMS.originalPath}
          </text>
          <text x="616" y="128" textAnchor="end" fill="#E8A33D" fontSize="14" fontFamily="sans-serif">
            {TERMS.alternative}
          </text>
        </g>
      </svg>
      <p role="status" className={`text-center font-display text-lg text-ivory ${anim ? "fork-caption" : ""}`}>
        {caption(state.info)}
      </p>
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          onDone();
        }}
        className="min-h-11 rounded-[4px] border border-hairline px-3 text-sm text-ivory-dim hover:text-ivory"
      >
        Skip (Esc)
      </button>
    </div>
  );
}
