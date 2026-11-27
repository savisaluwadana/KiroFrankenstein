"use client";

/**
 * Side-by-side comparison of two realities at the same displayed hour. The hour
 * and playback are shared (the studio's single time strip drives both panes).
 * Each pane loads its own frames and Metrics through the existing endpoints.
 * A reality that has not been simulated that far shows "not simulated yet" and
 * an explicit "Advance this future" action; nothing advances by itself and a
 * decision moment is never ended here.
 */
import { useEffect, useMemo, useState } from "react";
import { getBranchMetrics, getBranchState, type Branch, type MetricsResponse } from "@/lib/api/client";
import { LaneMark, RealityBadge, laneFor, laneIndexOf, realityName } from "@/features/branches/lanes";
import { RealitySwitcher } from "@/features/branches/RealitySwitcher";
import type { Intervention } from "@/features/world-view/scene/worldViewModel";
import { DISCLAIMER_FULL, METRIC_LABELS, formatMoney, formatCount, formatPercentBps, formatSimTime } from "@/lib/glossary";
import { usePlaybackStore } from "@/stores/playback";
import { useStudioStore } from "@/stores/studio";
import { commonTick, diffMetrics, divergenceTick, hashNote, paneStatus } from "./comparison";
import { StagePane } from "./StagePane";

export const COMPARE_EXPLAINER =
  "A different outcome is not proof of which decision caused it. To test a single decision, explore a future that changes only that decision.";

export interface ComparisonViewProps {
  /** The two realities, in lane order (original path first). */
  left: Branch;
  right: Branch;
  branches: readonly Branch[];
  startClock: string;
  productNames: readonly string[];
  regularWorkers: number | undefined;
  interventionsFor: (branchId: string) => readonly Intervention[];
  engineering: boolean;
  /** Head tick of a branch whose decision moment is open and not ended. */
  waiting: Readonly<Record<string, number>>;
  advancingId: string | null;
  onAdvance: (branchId: string) => void;
  onGoTo: (branchId: string) => void;
  /** Opens "What changed between these two futures" for this pair. */
  onWhatChanged?: () => void;
  /** Model identifier of the LLM AI, for display names. */
  llmModel?: string | undefined;
}

function useIsPhone(): boolean {
  const [phone, setPhone] = useState(false);
  useEffect(() => {
    if (typeof window.matchMedia !== "function") return;
    const mq = window.matchMedia("(max-width: 768px)");
    const update = (): void => setPhone(mq.matches);
    update();
    mq.addEventListener("change", update);
    return () => mq.removeEventListener("change", update);
  }, []);
  return phone;
}

interface Req {
  id: string;
  tick: number;
}

/** Metrics (and, in Engineering Mode, state hashes) by `id@tick`. Fetched only while paused; immutable once fetched. */
function useServerValues(reqs: Req[], enabled: boolean, wantHashes: boolean) {
  const [metrics, setMetrics] = useState<Record<string, MetricsResponse>>({});
  const [hashes, setHashes] = useState<Record<string, string>>({});
  const key = reqs.map((r) => `${r.id}@${r.tick}`).join("|");
  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    for (const r of reqs) {
      const k = `${r.id}@${r.tick}`;
      if (metrics[k] === undefined) {
        getBranchMetrics(r.id, r.tick)
          .then((m) => {
            if (!cancelled) setMetrics((prev) => ({ ...prev, [k]: m }));
          })
          .catch((e: unknown) => {
            if (!cancelled) useStudioStore.getState().setError(e instanceof Error ? e.message : "Request failed");
          });
      }
      if (wantHashes && hashes[k] === undefined) {
        getBranchState(r.id, r.tick)
          .then((st) => {
            if (!cancelled) setHashes((prev) => ({ ...prev, [k]: st.stateHash }));
          })
          .catch(() => undefined);
      }
    }
    return () => {
      cancelled = true;
    };
    // `key` stands for `reqs`; the caches are read, not depended on.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, enabled, wantHashes]);
  return { metrics, hashes };
}

function PaneMetrics({ m }: { m: MetricsResponse | undefined }) {
  if (m === undefined) return <p className="text-xs text-ivory-dim">Loading figures…</p>;
  const x = m.metrics;
  const item = (label: string, value: string) => (
    <div className="flex flex-col">
      <dt className="text-xs text-ivory-dim">{label}</dt>
      <dd className="font-semibold tabular-nums">{value}</dd>
    </div>
  );
  return (
    <dl className="grid grid-cols-2 gap-2 text-sm" data-testid="pane-metrics">
      {item(METRIC_LABELS.cash, formatMoney(x.cashCents))}
      {item(METRIC_LABELS.netProfit, formatMoney(x.netProfitCents))}
      {item(METRIC_LABELS.serviceLevel, formatPercentBps(x.serviceLevelBps))}
      {item(METRIC_LABELS.backlog, formatCount(x.backlogOrders))}
    </dl>
  );
}

export function ComparisonView(props: ComparisonViewProps) {
  const { left, right, branches, startClock } = props;
  const selectedTick = useStudioStore((s) => s.selectedTick);
  const syncing = useStudioStore((s) => s.syncing);
  const framesByBranch = useStudioStore((s) => s.framesByBranch);
  const playing = usePlaybackStore((s) => s.playing);
  const phone = useIsPhone();
  const [phoneId, setPhoneId] = useState(left.id);
  const shownPhoneId = phoneId === left.id || phoneId === right.id ? phoneId : left.id;

  const labelCtx = useMemo(() => ({ branches, startClock, llmModel: props.llmModel }), [branches, startClock, props.llmModel]);
  const nameOf = (b: Branch): string => realityName(b, labelCtx);
  const sides = [left, right] as const;
  const common = commonTick(selectedTick, left.headTick, right.headTick);
  const divergence = divergenceTick(left, right, branches);

  const reqs = useMemo(() => {
    const out: Req[] = [];
    for (const b of sides) {
      if (paneStatus(selectedTick, b.headTick) === "ready") out.push({ id: b.id, tick: selectedTick });
      if (common !== selectedTick) out.push({ id: b.id, tick: common });
    }
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [left.id, right.id, left.headTick, right.headTick, selectedTick, common]);
  const { metrics, hashes } = useServerValues(reqs, !playing, props.engineering);

  const mAt = (b: Branch, tick: number): MetricsResponse | undefined => metrics[`${b.id}@${tick}`];
  const summaryA = mAt(left, common);
  const summaryB = mAt(right, common);
  const nameL = nameOf(left);
  const nameR = nameOf(right);
  const behind = common < selectedTick ? (left.headTick < right.headTick ? left : right) : null;

  const pane = (b: Branch) => {
    const lane = laneFor(laneIndexOf(branches, b.id));
    const ready = paneStatus(selectedTick, b.headTick) === "ready";
    const waitTick = props.waiting[b.id];
    const isWaiting = waitTick !== undefined && waitTick === b.headTick;
    return (
      <section
        key={b.id}
        aria-label={`Future: ${nameOf(b)}`}
        data-testid={`pane-${b.id}`}
        className="flex min-w-0 flex-col gap-2 rounded-[8px] border border-hairline bg-surface p-2"
      >
        <h3 className="flex items-center gap-2 text-sm font-medium">
          <LaneMark lane={lane} />
          {nameOf(b)}
        </h3>
        {ready ? (
          <StagePane
            frame={framesByBranch[b.id]?.[selectedTick] ?? null}
            syncing={syncing}
            branchKey={b.id}
            productNames={props.productNames}
            regularWorkers={props.regularWorkers}
            interventions={props.interventionsFor(b.id)}
          />
        ) : (
          <div
            data-testid={`not-simulated-${b.id}`}
            className="flex min-h-40 flex-col items-start justify-center gap-2 rounded-[4px] border border-dashed border-hairline p-3 text-sm"
          >
            <p>
              {nameOf(b)} has not been simulated up to {formatSimTime(selectedTick, startClock)} yet.
            </p>
            {isWaiting ? null : (
              <button
                type="button"
                disabled={props.advancingId !== null}
                onClick={() => props.onAdvance(b.id)}
                className="min-h-11 rounded-[4px] border border-amber px-3 text-amber disabled:opacity-50"
              >
                {props.advancingId === b.id ? "Advancing…" : "Advance this future"}
              </button>
            )}
          </div>
        )}
        {isWaiting ? (
          <div role="status" data-testid={`waiting-${b.id}`} className="flex flex-wrap items-center gap-2 text-sm text-amber">
            <span>Your move is waiting in {nameOf(b)}.</span>
            <button
              type="button"
              onClick={() => props.onGoTo(b.id)}
              className="min-h-11 rounded-[4px] border border-amber px-3"
            >
              Go to {nameOf(b)}
            </button>
          </div>
        ) : null}
        {ready ? <PaneMetrics m={mAt(b, selectedTick)} /> : null}
      </section>
    );
  };

  const hashA = hashes[`${left.id}@${common}`];
  const hashB = hashes[`${right.id}@${common}`];

  return (
    <div className="flex flex-col gap-3" data-testid="comparison-view">
      <div className="flex flex-wrap items-center gap-2">
        <div className="max-[768px]:hidden">
          <RealityBadge name={`${nameL} next to ${nameR}`} lane={laneFor(laneIndexOf(branches, left.id))} />
        </div>
        {phone ? (
          <RealitySwitcher
            label="Which future to show"
            activeId={shownPhoneId}
            stickyOnPhone
            onSelect={setPhoneId}
            items={sides.map((b) => ({ id: b.id, label: nameOf(b), laneIndex: laneIndexOf(branches, b.id) }))}
          />
        ) : null}
      </div>
      <div className={`grid gap-3 ${phone ? "grid-cols-1" : "grid-cols-2"}`} data-testid="comparison-panes">
        {phone ? pane(shownPhoneId === right.id ? right : left) : sides.map(pane)}
      </div>

      <section
        aria-label="Observed differences"
        data-testid="comparison-summary"
        className="space-y-2 rounded-[8px] border border-hairline bg-raised p-3 text-sm"
      >
        <h3 className="font-display text-base">Observed differences at {formatSimTime(common, startClock)}</h3>
        {behind !== null ? (
          <p className="text-xs text-ivory-dim" data-testid="common-tick-note">
            Compared at {formatSimTime(common, startClock)}, the latest hour both futures have reached.{" "}
            {nameOf(behind)} has not been simulated further yet.
          </p>
        ) : null}
        {summaryA !== undefined && summaryB !== undefined ? (
          <table className="w-full text-left tabular-nums">
            <thead>
              <tr className="text-xs text-ivory-dim">
                <th scope="col" className="py-1 pr-2 font-normal">
                  &nbsp;
                </th>
                <th scope="col" className="py-1 pr-2 font-normal">{nameL}</th>
                <th scope="col" className="py-1 pr-2 font-normal">{nameR}</th>
                <th scope="col" className="py-1 font-normal">
                  {nameR} vs {nameL}
                </th>
              </tr>
            </thead>
            <tbody>
              {diffMetrics(summaryA.metrics, summaryB.metrics).map((r) => (
                <tr key={r.key} data-testid={`diff-${r.key}`} className="border-t border-hairline">
                  <th scope="row" className="py-1 pr-2 font-normal">{r.label}</th>
                  <td className="py-1 pr-2">{r.a}</td>
                  <td className="py-1 pr-2">{r.b}</td>
                  <td className="py-1 font-semibold">{r.diff}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <p className="text-ivory-dim">{playing ? "Figures update when you pause." : "Loading figures…"}</p>
        )}
        {props.onWhatChanged !== undefined ? (
          <button
            type="button"
            data-testid="what-changed"
            onClick={props.onWhatChanged}
            className="min-h-11 rounded-[4px] border border-amber px-3 text-amber"
          >
            What changed?
          </button>
        ) : null}
        <p className="text-xs text-ivory-dim">{COMPARE_EXPLAINER}</p>
        <p className="text-xs text-ivory-dim" data-testid="compare-disclaimer">
          {DISCLAIMER_FULL}
        </p>
        {props.engineering ? (
          <div className="space-y-1 rounded-[4px] border border-hairline bg-ink p-2 font-mono text-xs" data-testid="compare-hashes">
            <p>
              State hash at tick {common}: {nameL} {hashA ?? "…"}
            </p>
            <p>
              State hash at tick {common}: {nameR} {hashB ?? "…"}
            </p>
            {hashA !== undefined && hashB !== undefined ? (
              <p data-testid="compare-hash-note">{hashNote(common, divergence, hashA, hashB)}</p>
            ) : null}
          </div>
        ) : null}
      </section>
    </div>
  );
}
