"use client";

import { TriangleAlert } from "lucide-react";
import type { CrisisAction, CrisisCard } from "./crisis";

export interface CrisisCardsProps {
  cards: readonly CrisisCard[];
  /** True when a decision window is open here, so the card actions work. */
  canAct: boolean;
  onAction: (action: CrisisAction) => void;
}

/** Contextual alerts for the displayed hour. Content comes from `deriveCrisisCards` only. */
export function CrisisCards({ cards, canAct, onAction }: CrisisCardsProps) {
  if (cards.length === 0) {
    return (
      <p className="text-sm text-ivory-dim" data-testid="no-alerts">
        Nothing needs your attention right now.
      </p>
    );
  }
  return (
    <ul className="space-y-3" aria-label="What needs your attention">
      {cards.map((c) => (
        <li key={c.id}>
          <article
            aria-labelledby={`crisis-${c.id}`}
            data-testid="crisis-card"
            className="space-y-2 rounded-[8px] border border-clay bg-surface p-3 text-sm text-ivory"
          >
            <h3 id={`crisis-${c.id}`} className="flex items-start gap-2 font-medium">
              <TriangleAlert aria-hidden size={18} className="mt-0.5 shrink-0 text-clay" />
              {c.title}
            </h3>
            <dl className="space-y-2">
              <div>
                <dt className="text-xs text-ivory-dim">What happened</dt>
                <dd>{c.what}</dd>
              </div>
              <div>
                <dt className="text-xs text-ivory-dim">Why it happened</dt>
                <dd>{c.why}</dd>
              </div>
              {c.affected !== "" ? (
                <div>
                  <dt className="text-xs text-ivory-dim">What is affected</dt>
                  <dd>{c.affected}</dd>
                </div>
              ) : null}
            </dl>
            {c.actions.length > 0 && canAct ? (
              <div className="flex flex-wrap gap-2">
                {c.actions.map((a) => (
                  <button
                    key={a.id}
                    type="button"
                    onClick={() => onAction(a)}
                    className="inline-flex min-h-11 items-center rounded-[4px] border border-amber px-3 text-amber hover:bg-raised"
                  >
                    {a.label}
                  </button>
                ))}
              </div>
            ) : c.actions.length > 0 ? (
              <p className="text-xs text-ivory-dim">You can act at your next decision moment.</p>
            ) : null}
          </article>
        </li>
      ))}
    </ul>
  );
}
