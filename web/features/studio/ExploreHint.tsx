"use client";

import { X } from "lucide-react";
import { useState } from "react";
import { TERMS } from "@/lib/glossary";

export const EXPLORE_HINT_KEY = "parallel.hint.explore.dismissed";
export const EXPLORE_HINT_TEXT = `Want to try a different decision? ${TERMS.exploreFuture} — rewind to any moment and branch`;

function wasDismissed(): boolean {
  try {
    return typeof window !== "undefined" && window.localStorage.getItem(EXPLORE_HINT_KEY) === "1";
  } catch {
    return false;
  }
}

/** Dismissal shared by every placement of the tip, remembered in localStorage. */
export function useExploreHintDismissed(): [boolean, () => void] {
  const [dismissed, setDismissed] = useState<boolean>(wasDismissed);
  const dismiss = (): void => {
    try {
      window.localStorage.setItem(EXPLORE_HINT_KEY, "1");
    } catch {
      // Storage unavailable: the tip is dismissed for this visit only.
    }
    setDismissed(true);
  };
  return [dismissed, dismiss];
}

/**
 * One line shown while a decision moment is open. Copy and placement only: the
 * button does what the F key already does.
 */
export function ExploreHint({ show, onShowMe, onDismiss, className = "" }: { show: boolean; onShowMe: () => void; onDismiss: () => void; className?: string }) {
  if (!show) return null;
  return (
    <div
      role="note"
      data-testid="explore-hint"
      className={`flex flex-wrap items-center gap-x-3 gap-y-1 rounded-[4px] border border-hairline bg-surface px-3 py-1 text-sm text-ivory ${className}`}
    >
      <span className="min-w-0 flex-1">{EXPLORE_HINT_TEXT}</span>
      <button type="button" onClick={onShowMe} className="min-h-11 rounded-[4px] border border-amber px-3 text-amber">
        Show me
      </button>
      <button type="button" onClick={onDismiss} aria-label="Dismiss this tip" className="inline-flex min-h-11 min-w-11 items-center justify-center text-ivory-dim hover:text-ivory">
        <X aria-hidden size={16} />
      </button>
    </div>
  );
}
