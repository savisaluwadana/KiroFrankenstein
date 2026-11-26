"use client";

/**
 * Header figures for the displayed hour: the time, Cash, Profit so far, Orders
 * delivered on time and Orders waiting, from the Metrics response. It formats
 * API integers for display only. The difference from the original path is a
 * display subtraction of two API values for the same tick, labelled as such.
 * Tick numbers appear only in Engineering Mode.
 */
import type { MetricsResponse } from "@/lib/api/client";
import {
  METRIC_LABELS,
  formatCount,
  formatMoney,
  formatPercentBps,
  formatSimTime,
  metricExplanation,
  type MetricKey,
} from "@/lib/glossary";
import { InfoPopover } from "@/features/studio/InfoPopover";

export type MoneyFigure = "cash" | "net_profit";

export interface HudProps {
  /** Metrics response for the displayed hour of the displayed reality. */
  metrics: MetricsResponse | null;
  /** The other reality's Metrics at the same tick, when the displayed one is an alternative. */
  parentMetrics?: MetricsResponse | null;
  /** Name of that other reality, for example "Original path". */
  parentLabel?: string;
  syncing?: boolean;
  /** Tick used for the clock while Metrics are not loaded. */
  tick?: number;
  /** Scenario start label such as "Thu 00:00". */
  startClock?: string;
  /** Adds the tick number next to the time. */
  engineering?: boolean;
  /** Makes money figures buttons that open "Where this number comes from". */
  onMoneyClick?: (figure: MoneyFigure) => void;
}

function signedMoney(cents: number): string {
  return formatMoney(cents, { signed: true });
}

/** "+2.5 points" / "-1.0 points" / "0.0 points" from a basis point difference. */
function pointsDiff(bps: number): string {
  const body = formatPercentBps(Math.abs(bps)).replace("%", "");
  return `${bps > 0 ? "+" : bps < 0 ? "-" : ""}${body} points`;
}

function signedCount(n: number): string {
  return n > 0 ? `+${formatCount(n)}` : formatCount(n);
}

function Figure(props: {
  metric: MetricKey;
  value: string;
  testId: string;
  explanation: string;
  onClick?: () => void;
}) {
  const label = METRIC_LABELS[props.metric];
  const value = (
    <span className="text-lg font-semibold tabular-nums text-ivory max-[768px]:text-base" data-testid={props.testId}>
      {props.value}
    </span>
  );
  return (
    <div className="flex min-w-[8.5rem] flex-col max-[768px]:min-w-0">
      <span className="flex items-center gap-1 text-xs text-ivory-dim">
        {label}
        <InfoPopover label={label}>{props.explanation}</InfoPopover>
      </span>
      {props.onClick ? (
        <button
          type="button"
          onClick={props.onClick}
          title="Where this number comes from"
          className="text-left underline decoration-dotted underline-offset-4 max-[768px]:min-h-11"
        >
          {value}
        </button>
      ) : (
        value
      )}
    </div>
  );
}

export function Hud({ metrics, parentMetrics, parentLabel, syncing, tick, startClock, engineering, onMoneyClick }: HudProps) {
  const m = metrics?.metrics;
  const displayTick = metrics?.tick ?? tick ?? 0;
  const p = parentMetrics && metrics && parentMetrics.tick === metrics.tick ? parentMetrics.metrics : null;
  const other = parentLabel ?? "Original path";
  const clock = formatSimTime(displayTick, startClock ?? "Thu 00:00");

  return (
    <section
      aria-label="Business figures"
      data-syncing={syncing ? "true" : "false"}
      className="text-sm text-ivory"
    >
      <div className={`flex items-end gap-x-6 gap-y-2 max-[768px]:grid max-[768px]:grid-cols-2 max-[768px]:gap-y-1.5 min-[769px]:flex-wrap ${syncing ? "opacity-60" : ""}`}>
        <div className="flex min-w-[10rem] flex-col max-[768px]:col-span-2">
          <span className="text-xs text-ivory-dim">Time</span>
          <span className="text-lg font-semibold max-[768px]:text-base" data-testid="hud-clock">
            {clock}
            {engineering ? <span className="ml-2 font-mono text-xs text-ivory-dim">tick {displayTick}</span> : null}
          </span>
        </div>
        {m ? (
          <>
            <Figure
              metric="cash"
              testId="hud-cash"
              value={formatMoney(m.cashCents)}
              explanation={metricExplanation("cash")}
              {...(onMoneyClick ? { onClick: () => onMoneyClick("cash") } : {})}
            />
            <Figure
              metric="netProfit"
              testId="hud-net-profit"
              value={formatMoney(m.netProfitCents, { signed: true })}
              explanation={metricExplanation("netProfit")}
              {...(onMoneyClick ? { onClick: () => onMoneyClick("net_profit") } : {})}
            />
            <Figure
              metric="serviceLevel"
              testId="hud-service-level"
              value={formatPercentBps(m.serviceLevelBps)}
              explanation={metricExplanation("serviceLevel")}
            />
            <Figure
              metric="backlog"
              testId="hud-backlog"
              value={`${formatCount(m.backlogOrders)} ${m.backlogOrders === 1 ? "order" : "orders"}`}
              explanation={metricExplanation("backlog", { lateBacklogOrders: m.lateBacklogOrders })}
            />
          </>
        ) : (
          <span className="text-ivory-dim">Loading figures…</span>
        )}
        {syncing ? <span className="text-xs text-ivory-dim">Updating…</span> : null}
      </div>

      {m && p ? (
        <div className={`mt-2 border-t border-hairline pt-2 text-xs ${syncing ? "opacity-60" : ""}`} data-testid="hud-parent">
          <p className="tabular-nums text-ivory-dim">
            {other} at this hour:{" "}
            <span data-testid="parent-cash">Cash {formatMoney(p.cashCents)}</span> ·{" "}
            <span data-testid="parent-net-profit">Profit so far {signedMoney(p.netProfitCents)}</span> ·{" "}
            <span data-testid="parent-service-level">On time {formatPercentBps(p.serviceLevelBps)}</span> ·{" "}
            <span data-testid="parent-backlog">
              Orders waiting {formatCount(p.backlogOrders)}
            </span>
          </p>
          <p className="tabular-nums text-ivory-dim" data-testid="hud-diff">
            Difference from {other} (shown for comparison):{" "}
            <span data-testid="diff-net-profit">Profit so far {signedMoney(m.netProfitCents - p.netProfitCents)}</span> ·{" "}
            <span data-testid="diff-service-level">On time {pointsDiff(m.serviceLevelBps - p.serviceLevelBps)}</span> ·{" "}
            <span data-testid="diff-backlog">Orders waiting {signedCount(m.backlogOrders - p.backlogOrders)}</span>
          </p>
        </div>
      ) : null}
    </section>
  );
}
