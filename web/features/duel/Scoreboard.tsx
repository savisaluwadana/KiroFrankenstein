"use client";

/**
 * The duel scoreboard. Every figure is an API value from GET /duels/{id}/score,
 * formatted only. The only arithmetic is the display subtraction in "Observed
 * differences". Wording is neutral: the outcome describes this one simulated
 * week under the stated assumptions, never which side should win.
 */
import type { DuelBranchScore, DuelScore } from "@/lib/api/client";
import { DISCLAIMER_FULL, METRIC_LABELS, formatCount, formatMoney, formatPercentBps, formatSimTime } from "@/lib/glossary";
import { formatPoints, formatPointsShort, formatWeight, plainManagerLabel } from "./duelText";

export const PROVISIONAL_TEXT = "Provisional — the week isn't over";

export function outcomeText(score: Pick<DuelScore, "provisional" | "outcome">): { headline: string; note: string } {
  if (score.provisional) {
    const standing =
      score.outcome === "human_wins" ? "You are ahead" : score.outcome === "ai_wins" ? "The AI is ahead" : "Level so far";
    return {
      headline: PROVISIONAL_TEXT,
      note: `${standing} at this hour. Scores can still change until the week ends.`,
    };
  }
  const headline = score.outcome === "human_wins" ? "You won" : score.outcome === "ai_wins" ? "AI won" : "Draw";
  return {
    headline,
    note: "This describes one simulated week under the stated assumptions. It does not show which approach is better in general.",
  };
}

function signedPoints(h: number): string {
  return h > 0 ? `+${formatPoints(h)}` : formatPoints(h);
}

function signedCount(n: number): string {
  return n > 0 ? `+${formatCount(n)}` : formatCount(n);
}

function signedPercent(bps: number): string {
  return bps > 0 ? `+${formatPercentBps(bps)}` : formatPercentBps(bps);
}

export interface ScoreboardProps {
  score: DuelScore;
  startClock: string;
  onOpenAssumptions: () => void;
}

export function Scoreboard({ score, startClock, onOpenAssumptions }: ScoreboardProps) {
  const h: DuelBranchScore = score.human;
  const a: DuelBranchScore = score.ai;
  const aiName = plainManagerLabel(a.managerLabel);
  const outcome = outcomeText(score);
  const w = score.weights;

  const components = [
    { key: "financial", label: "Financial", weight: w.financialWeightBps, hv: h.weighted.financialHundredths, av: a.weighted.financialHundredths, hp: h.score.financialBps, ap: a.score.financialBps },
    { key: "fulfilment", label: "Orders fully shipped", weight: w.fulfilmentWeightBps, hv: h.weighted.fulfilmentHundredths, av: a.weighted.fulfilmentHundredths, hp: h.score.fulfilmentRateBps, ap: a.score.fulfilmentRateBps },
    { key: "onTime", label: "On time", weight: w.onTimeWeightBps, hv: h.weighted.onTimeHundredths, av: a.weighted.onTimeHundredths, hp: h.score.onTimeBps, ap: a.score.onTimeBps },
  ];

  const raw = [
    { key: "cash", label: METRIC_LABELS.cash, hv: formatMoney(h.metrics.cashCents), av: formatMoney(a.metrics.cashCents), diff: formatMoney(h.metrics.cashCents - a.metrics.cashCents, { signed: true }) },
    { key: "profit", label: METRIC_LABELS.netProfit, hv: formatMoney(h.metrics.netProfitCents), av: formatMoney(a.metrics.netProfitCents), diff: formatMoney(h.metrics.netProfitCents - a.metrics.netProfitCents, { signed: true }) },
    { key: "onTime", label: METRIC_LABELS.serviceLevel, hv: formatPercentBps(h.metrics.serviceLevelBps), av: formatPercentBps(a.metrics.serviceLevelBps), diff: signedPercent(h.metrics.serviceLevelBps - a.metrics.serviceLevelBps) },
    { key: "shipped", label: METRIC_LABELS.fillRate, hv: formatPercentBps(h.metrics.fillRateBps), av: formatPercentBps(a.metrics.fillRateBps), diff: signedPercent(h.metrics.fillRateBps - a.metrics.fillRateBps) },
    { key: "waiting", label: METRIC_LABELS.backlog, hv: formatCount(h.metrics.backlogOrders), av: formatCount(a.metrics.backlogOrders), diff: signedCount(h.metrics.backlogOrders - a.metrics.backlogOrders) },
    { key: "cancelled", label: "Orders cancelled", hv: formatCount(h.cancelledOrders), av: formatCount(a.cancelledOrders), diff: signedCount(h.cancelledOrders - a.cancelledOrders) },
  ];

  const th = "py-1 pr-2 text-left text-xs font-normal text-ivory-dim";

  return (
    <section aria-label="Scoreboard" data-testid="scoreboard" className="space-y-4 rounded-[8px] border border-hairline bg-raised p-3 text-sm">
      <header className="space-y-1">
        <h2 className="font-display text-xl">Scoreboard</h2>
        <p data-testid="outcome" className="font-display text-lg">
          {outcome.headline}
        </p>
        <p className="text-xs text-ivory-dim">{outcome.note}</p>
        <p className="text-xs text-ivory-dim" data-testid="score-time">
          Scored at {formatSimTime(score.comparisonTick, startClock)}, the latest hour both businesses have reached.
        </p>
      </header>

      <div className="grid grid-cols-2 gap-3 text-center" data-testid="composite">
        <div className="rounded-[4px] border border-hairline p-2">
          <p className="text-xs text-ivory-dim">You</p>
          <p className="font-display text-3xl tabular-nums" data-testid="score-human">
            {formatPoints(h.score.compositeHundredths)}
          </p>
          <p className="text-xs text-ivory-dim">points of 100</p>
        </div>
        <div className="rounded-[4px] border border-hairline p-2">
          <p className="text-xs text-ivory-dim">{aiName}</p>
          <p className="font-display text-3xl tabular-nums" data-testid="score-ai">
            {formatPoints(a.score.compositeHundredths)}
          </p>
          <p className="text-xs text-ivory-dim">points of 100</p>
        </div>
      </div>

      <div>
        <h3 className="font-medium">How the points are made</h3>
        <div className="overflow-x-auto"><table className="w-full tabular-nums max-[768px]:text-xs" data-testid="components">
          <thead>
            <tr>
              <th scope="col" className={th}>Part</th>
              <th scope="col" className={th}>You</th>
              <th scope="col" className={th}>{aiName}</th>
            </tr>
          </thead>
          <tbody>
            {components.map((c) => (
              <tr key={c.key} className="border-t border-hairline" data-testid={`component-${c.key}`}>
                <th scope="row" className="py-1 pr-2 text-left font-normal">
                  {c.label} ({formatWeight(c.weight)})
                </th>
                <td className="py-1 pr-2">
                  {formatPoints(c.hv)} <span className="text-xs text-ivory-dim">({formatPercentBps(c.hp)})</span>
                </td>
                <td className="py-1 pr-2">
                  {formatPoints(c.av)} <span className="text-xs text-ivory-dim">({formatPercentBps(c.ap)})</span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        </div>
        <p className="mt-1 text-xs text-ivory-dim">
          Each part is scored from 0 to 100, then multiplied by its weight. The profit part is measured against the
          business&apos;s own range, from {formatMoney(score.profitFloorCents)} (no points) to{" "}
          {formatMoney(score.profitTargetCents)} (full points), never against the other side. Scores within{" "}
          {formatPointsShort(score.tieToleranceHundredths)} points count as a draw.
        </p>
      </div>

      <div>
        <h3 className="font-medium">Observed differences (You minus {aiName})</h3>
        <div className="overflow-x-auto"><table className="w-full tabular-nums max-[768px]:text-xs" data-testid="raw-metrics">
          <thead>
            <tr>
              <th scope="col" className={th}>Figure</th>
              <th scope="col" className={th}>You</th>
              <th scope="col" className={th}>{aiName}</th>
              <th scope="col" className={th}>Difference</th>
            </tr>
          </thead>
          <tbody>
            <tr className="border-t border-hairline" data-testid="raw-composite">
              <th scope="row" className="py-1 pr-2 text-left font-normal">Points</th>
              <td className="py-1 pr-2">{formatPoints(h.score.compositeHundredths)}</td>
              <td className="py-1 pr-2">{formatPoints(a.score.compositeHundredths)}</td>
              <td className="py-1 font-semibold">{signedPoints(h.score.compositeHundredths - a.score.compositeHundredths)}</td>
            </tr>
            {raw.map((r) => (
              <tr key={r.key} className="border-t border-hairline" data-testid={`raw-${r.key}`}>
                <th scope="row" className="py-1 pr-2 text-left font-normal">{r.label}</th>
                <td className="py-1 pr-2">{r.hv}</td>
                <td className="py-1 pr-2">{r.av}</td>
                <td className="py-1 font-semibold">{r.diff}</td>
              </tr>
            ))}
          </tbody>
        </table>
        </div>
        <p className="mt-1 text-xs text-ivory-dim">
          A difference shows what was observed, not what caused it. To test one decision, explore a future that changes
          only that decision.
        </p>
      </div>

      <footer className="flex flex-wrap items-center justify-between gap-2 text-xs text-ivory-dim">
        <p data-testid="duel-disclaimer">{score.disclaimer ?? DISCLAIMER_FULL}</p>
        <button type="button" onClick={onOpenAssumptions} className="min-h-11 underline">
          How this is modelled
        </button>
      </footer>
    </section>
  );
}
