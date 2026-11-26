"use client";

/**
 * HUD: sim clock, Cash, Net_Profit, Service_Level and Backlog of the displayed
 * Tick, from the Metrics response. It formats API integers for display only.
 * Parent differences are a display subtraction of two API values for the same
 * Tick and are labelled as such (Requirements 31.5, 35.5).
 */
import type { MetricsResponse } from "@/lib/api/client";
import { selectActiveBranch, useStudioStore } from "@/stores/studio";

export const DISCLAIMER_TEXT =
  "Synthetic simulation output under the stated assumptions. Not a forecast or guarantee of real-world results.";

const DAY_NAMES = ["Thursday", "Friday", "Saturday", "Sunday", "Monday"];

/** Scenario start label: Tick 0 is Thursday 00:00. */
export function formatSimClock(tick: number): string {
  const day = Math.floor(tick / 24);
  const hour = tick % 24;
  const name = DAY_NAMES[day] ?? `Day ${day + 1}`;
  return `${name} ${String(hour).padStart(2, "0")}:00`;
}

type Decimal = `${number}`;

/** Formats integer cents as currency using integer division for the split only. */
export function formatCents(cents: number, currency: string, signed = false): string {
  const negative = cents < 0;
  const abs = Math.abs(cents);
  const whole = Math.trunc(abs / 100);
  const frac = abs % 100;
  const nf = new Intl.NumberFormat("en-US", { style: "currency", currency, minimumFractionDigits: 2, maximumFractionDigits: 2 });
  const body = nf.format(`${whole}.${String(frac).padStart(2, "0")}` as Decimal as unknown as number);
  if (negative) return `-${body}`;
  return signed && abs !== 0 ? `+${body}` : body;
}

/** Formats basis points as a percentage with two decimals. */
export function formatBps(bps: number, signed = false): string {
  const negative = bps < 0;
  const abs = Math.abs(bps);
  const body = `${Math.trunc(abs / 100)}.${String(abs % 100).padStart(2, "0")}%`;
  if (negative) return `-${body}`;
  return signed && abs !== 0 ? `+${body}` : body;
}

function signedInt(n: number): string {
  return n > 0 ? `+${n}` : String(n);
}

export interface HudProps {
  /** Metrics response for the displayed Tick of the displayed Branch. */
  metrics: MetricsResponse | null;
  /** Parent Branch Metrics at the same Tick, when the displayed Branch is a fork. */
  parentMetrics?: MetricsResponse | null;
  /** Label of the parent Branch. */
  parentLabel?: string;
  syncing?: boolean;
  /** Tick used for the clock while Metrics are not loaded. */
  tick?: number;
  /** Makes money figures clickable; opens the ledger lines behind the figure. */
  onMoneyClick?: (figure: "cash" | "net_profit") => void;
}

function Figure(props: { label: string; value: string; onClick?: () => void; testId: string }) {
  const value = (
    <span className="font-mono tabular-nums text-[#E6E9EF]" data-testid={props.testId}>
      {props.value}
    </span>
  );
  return (
    <div className="flex flex-col">
      <span className="text-[10px] uppercase tracking-wide text-[#8A93A6]">{props.label}</span>
      {props.onClick ? (
        <button type="button" onClick={props.onClick} className="text-left underline decoration-dotted underline-offset-2">
          {value}
        </button>
      ) : (
        value
      )}
    </div>
  );
}

export function Hud({ metrics, parentMetrics, parentLabel, syncing, tick, onMoneyClick }: HudProps) {
  const m = metrics?.metrics;
  const currency = metrics?.currency ?? "USD";
  const displayTick = metrics?.tick ?? tick ?? 0;
  const p = parentMetrics && metrics && parentMetrics.tick === metrics.tick ? parentMetrics.metrics : null;
  const disclaimer = metrics?.disclaimer ?? DISCLAIMER_TEXT;

  return (
    <section
      aria-label="Heads-up display"
      data-syncing={syncing ? "true" : "false"}
      className="rounded-[4px] border border-[#232B3A] bg-[#121722] px-3 py-2 text-sm text-[#E6E9EF]"
    >
      <div className={`flex flex-wrap items-end gap-x-6 gap-y-1 ${syncing ? "opacity-60" : ""}`}>
        <div className="flex flex-col">
          <span className="text-[10px] uppercase tracking-wide text-[#8A93A6]">Sim clock</span>
          <span className="font-mono tabular-nums" data-testid="hud-clock">
            {formatSimClock(displayTick)} · tick {displayTick}
          </span>
        </div>
        {m ? (
          <>
            <Figure
              label="Cash"
              testId="hud-cash"
              value={formatCents(m.cashCents, currency)}
              {...(onMoneyClick ? { onClick: () => onMoneyClick("cash") } : {})}
            />
            <Figure
              label="Net profit"
              testId="hud-net-profit"
              value={formatCents(m.netProfitCents, currency)}
              {...(onMoneyClick ? { onClick: () => onMoneyClick("net_profit") } : {})}
            />
            <Figure label="Service level" testId="hud-service-level" value={formatBps(m.serviceLevelBps)} />
            <Figure
              label="Backlog"
              testId="hud-backlog"
              value={`${m.backlogOrders} orders / ${m.backlogUnits} units`}
            />
          </>
        ) : (
          <span className="font-mono text-[#8A93A6]">No metrics loaded</span>
        )}
        {syncing ? <span className="font-mono text-xs text-[#8A93A6]">syncing</span> : null}
      </div>

      {m && p ? (
        <div className={`mt-2 border-t border-[#232B3A] pt-2 ${syncing ? "opacity-60" : ""}`} data-testid="hud-parent">
          <div className="text-[10px] uppercase tracking-wide text-[#8A93A6]">
            Parent{parentLabel ? ` (${parentLabel})` : ""} at tick {displayTick}
          </div>
          <div className="flex flex-wrap gap-x-6 font-mono tabular-nums">
            <span data-testid="parent-net-profit">Net profit {formatCents(p.netProfitCents, currency)}</span>
            <span data-testid="parent-service-level">Service level {formatBps(p.serviceLevelBps)}</span>
            <span data-testid="parent-backlog">
              Backlog {p.backlogOrders} orders / {p.backlogUnits} units
            </span>
          </div>
          <div className="text-[10px] text-[#8A93A6]">difference vs parent (display)</div>
          <div className="flex flex-wrap gap-x-6 font-mono tabular-nums" data-testid="hud-diff">
            <span data-testid="diff-net-profit">
              Net profit {formatCents(m.netProfitCents - p.netProfitCents, currency, true)}
            </span>
            <span data-testid="diff-service-level">
              Service level {formatBps(m.serviceLevelBps - p.serviceLevelBps, true)}
            </span>
            <span data-testid="diff-backlog">
              Backlog {signedInt(m.backlogOrders - p.backlogOrders)} orders /{" "}
              {signedInt(m.backlogUnits - p.backlogUnits)} units
            </span>
          </div>
        </div>
      ) : null}

      <p className="mt-2 text-xs text-[#8A93A6]" data-testid="hud-disclaimer">
        {disclaimer}
      </p>
    </section>
  );
}

/**
 * HUD wired to the studio store. `parentMetrics` is the parent Branch's Metrics
 * response at the displayed Tick, fetched by the caller (it is shown only when
 * its Tick equals the displayed Tick).
 */
export function ConnectedHud(props: {
  parentMetrics?: MetricsResponse | null;
  onMoneyClick?: HudProps["onMoneyClick"];
}) {
  const metrics = useStudioStore((s) => s.metrics);
  const syncing = useStudioStore((s) => s.syncing);
  const tick = useStudioStore((s) => s.selectedTick);
  const branches = useStudioStore((s) => s.branches);
  const active = useStudioStore(selectActiveBranch);
  const parent = active?.parentBranchId ? branches.find((b) => b.id === active.parentBranchId) : undefined;
  return (
    <Hud
      metrics={metrics}
      parentMetrics={props.parentMetrics ?? null}
      {...(parent ? { parentLabel: parent.label } : {})}
      syncing={syncing}
      tick={tick}
      {...(props.onMoneyClick ? { onMoneyClick: props.onMoneyClick } : {})}
    />
  );
}
