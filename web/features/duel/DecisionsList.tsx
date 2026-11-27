"use client";

/**
 * "What the AI did and why" and "What you did". The AI's decision for a window
 * is not shown until the human has ended that window (the server withholds it
 * too; this list filters again so a stale response can never reveal it early).
 * Rationale text is untrusted: it is rendered only as escaped plain text under
 * the label "AI rationale (not verified)", never as markdown or HTML.
 */
import type { AcceptedCommand, DuelDecision } from "@/lib/api/client";
import { formatSimTime, rejectionMessage } from "@/lib/glossary";
import { describeMove, fallbackSentence, plainManagerLabel } from "./duelText";

export const RATIONALE_LABEL = "AI rationale (not verified)";

/** Decisions the human may see: none at or after the window that is still open for them. */
export function visibleDecisions(
  decisions: readonly DuelDecision[],
  openWindowTick: number | null,
): DuelDecision[] {
  return decisions.filter(
    (d) => openWindowTick === null || d.windowTick < openWindowTick,
  );
}

export function AiStatus({
  waiting,
  decidedCount,
}: {
  waiting: boolean;
  decidedCount: number;
}) {
  return (
    <p role="status" data-testid="ai-status" className="text-sm text-ivory-dim">
      {waiting
        ? "The AI manager is waiting for you. Its decision stays hidden until you end this move."
        : decidedCount > 0
          ? "The AI manager has decided."
          : "The AI manager has not had a decision to make yet."}
    </p>
  );
}

export function AiDecisions(props: {
  decisions: readonly DuelDecision[];
  startClock: string;
  productNames: readonly string[];
  engineering: boolean;
}) {
  if (props.decisions.length === 0) {
    return (
      <p className="text-xs text-ivory-dim" data-testid="ai-decisions-empty">
        Nothing to show yet. Each decision appears after you end your move at
        the same moment.
      </p>
    );
  }
  return (
    <>
      <p className="text-sm text-ivory-dim" data-testid="ai-decisions-count">
        {props.decisions.length === 1
          ? "1 decision"
          : `${props.decisions.length} decisions`}{" "}
        so far. Scroll the list to see all of them.
      </p>
      <ol
        tabIndex={0}
        aria-label="AI decisions"
        className="max-h-[26rem] space-y-3 overflow-y-auto pr-1"
        data-testid="ai-decisions"
      >
        {props.decisions.map((d) => {
          const rejectedBy = new Map(d.rejected.map((r) => [r.index, r]));
          return (
            <li
              key={`${d.windowTick}-${d.createdAt}`}
              className="space-y-1 rounded-[4px] border border-hairline p-2 text-sm"
              data-testid="ai-decision"
            >
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-medium">
                  {formatSimTime(d.windowTick, props.startClock)}
                </span>
                <span
                  className="rounded-[4px] border border-hairline px-2 text-xs"
                  data-testid="provenance-badge"
                >
                  {plainManagerLabel(d.managerLabel)}
                </span>
              </div>
              {d.fallbackReason !== undefined ? (
                <p
                  className="text-sm text-ivory-dim"
                  data-testid="fallback-reason"
                >
                  {fallbackSentence(d.fallbackReason)}
                </p>
              ) : null}
              {d.proposed.length === 0 ? (
                <p className="text-ivory-dim">Made no moves at this moment.</p>
              ) : (
                <ul className="space-y-1">
                  {d.proposed.map((c, i) => {
                    const rej = rejectedBy.get(i);
                    return rej === undefined ? (
                      <li key={i} data-testid="ai-move-accepted">
                        Did: {describeMove(c, props.productNames)}
                      </li>
                    ) : (
                      <li
                        key={i}
                        className="text-clay"
                        data-testid="ai-move-rejected"
                      >
                        Tried but was not allowed:{" "}
                        {describeMove(c, props.productNames)}.{" "}
                        {rejectionMessage(rej.code)}
                      </li>
                    );
                  })}
                </ul>
              )}
              {d.rationale !== "" ? (
                <div className="space-y-0.5" data-testid="ai-rationale">
                  <p className="text-sm text-ivory-dim">{RATIONALE_LABEL}</p>
                  <p className="whitespace-pre-wrap break-words">
                    {d.rationale}
                  </p>
                </div>
              ) : null}
              {props.engineering ? (
                <pre
                  tabIndex={0}
                  aria-label="Decision record"
                  className="max-h-60 overflow-auto rounded-[4px] border border-hairline bg-ink p-2 font-mono text-xs"
                >
                  {JSON.stringify(d, null, 2)}
                </pre>
              ) : null}
            </li>
          );
        })}
      </ol>
    </>
  );
}

export function HumanMoves(props: {
  moves: readonly AcceptedCommand[];
  startClock: string;
  productNames: readonly string[];
}) {
  if (props.moves.length === 0) {
    return (
      <p className="text-xs text-ivory-dim">You have not made a move yet.</p>
    );
  }
  return (
    <ul className="space-y-1 text-sm" data-testid="human-moves">
      {props.moves.map((m) => (
        <li key={m.seq}>
          {describeMove(m.command, props.productNames)}{" "}
          <span className="text-xs text-ivory-dim">
            (from {formatSimTime(m.effectiveTick, props.startClock)})
          </span>
        </li>
      ))}
    </ul>
  );
}
