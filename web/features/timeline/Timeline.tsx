"use client";

/**
 * Time strip across the scenario horizon. Every marker comes from a Tick_Frame,
 * an Input or a Branch returned by the API. Decision-moment and surprise
 * markers are drawn only up to `revealedUpTo`, the furthest hour this reality
 * has already shown, so the strip never previews what has not happened yet.
 * Selecting an hour calls `onSelectTick` (wired to `playback.selectTick`),
 * which reads State and never advances the Branch.
 */
import { useRef, useState, type KeyboardEvent, type MouseEvent } from "react";
import type { Branch, TickFrame } from "@/lib/api/client";
import type { components } from "@/lib/api/schema";
import { disruptionLabel, formatDayName, formatSimTime } from "@/lib/glossary";
import { selectActiveBranch, useStudioStore } from "@/stores/studio";
import { usePlaybackStore } from "@/stores/playback";
import { PROVENANCE_MARKER } from "./markerStyle";

export type InputProvenance = components["schemas"]["Provenance"];

export interface FrameMarkerAt {
  tick: number;
  kind: "decision_window" | "disruption_onset";
  disruptionKind?: string | undefined;
  windowReason?: string | undefined;
}

export interface TimelineInput {
  effectiveTick: number;
  provenance: InputProvenance;
  /** Command or Disruption kind, for the label. */
  kind: string;
}

export interface ForkPoint {
  tick: number;
  label: string;
}

export interface TimelineProps {
  horizonTicks: number;
  headTick: number;
  selectedTick: number;
  /** Markers at ticks above this are not drawn. */
  revealedUpTo: number;
  startClock: string;
  frameMarkers: FrameMarkerAt[];
  inputs?: TimelineInput[];
  forkPoints?: ForkPoint[];
  /** Hours up to and including this tick are history shared with another reality; drawn as a band. */
  sharedUpTo?: number;
  /** First hour at which the displayed comparison's futures differ (from the diff endpoint); drawn as a labelled marker. */
  divergenceTick?: number;
  /** Engineering Mode adds the tick number to the readout. */
  engineering?: boolean;
  syncing?: boolean;
  onSelectTick: (tick: number) => void;
}

const HOURS_PER_DAY = 24;

/** Flattens loaded Tick_Frames into marker records, sorted by Tick. */
export function markersFromFrames(frames: Record<number, TickFrame> | undefined): FrameMarkerAt[] {
  if (!frames) return [];
  const out: FrameMarkerAt[] = [];
  for (const frame of Object.values(frames)) {
    for (const m of frame.markers) {
      out.push({ tick: frame.tick, kind: m.kind, disruptionKind: m.disruptionKind, windowReason: m.windowReason });
    }
  }
  return out.sort((a, b) => a.tick - b.tick);
}

/**
 * Furthest hour ever displayed for `branchId` in this session. Only grows, so
 * scrubbing backwards does not hide what was already seen.
 */
export function useHighWater(branchId: string | null, selectedTick: number): number {
  const [marks, setMarks] = useState<Record<string, number>>({});
  if (branchId === null) return selectedTick;
  const seen = marks[branchId] ?? -1;
  if (selectedTick > seen) setMarks({ ...marks, [branchId]: selectedTick });
  return Math.max(seen, selectedTick);
}

function pct(tick: number, horizon: number): string {
  if (horizon <= 0) return "0%";
  const clamped = Math.max(0, Math.min(tick, horizon));
  return `${(clamped * 100) / horizon}%`;
}

export function Timeline(props: TimelineProps) {
  const { horizonTicks, headTick, selectedTick, revealedUpTo, startClock, onSelectTick } = props;
  const frameMarkers = props.frameMarkers.filter((m) => m.tick <= revealedUpTo);
  const inputs = props.inputs ?? [];
  const forkPoints = props.forkPoints ?? [];
  const trackRef = useRef<HTMLDivElement>(null);
  const days = Math.max(1, Math.ceil(horizonTicks / HOURS_PER_DAY));
  const at = (tick: number): string => formatSimTime(tick, startClock);

  function select(tick: number): void {
    onSelectTick(Math.max(0, Math.min(tick, headTick)));
  }

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>): void {
    let target: number | null = null;
    switch (e.key) {
      case "ArrowLeft":
      case "ArrowDown":
        if (!e.shiftKey) target = selectedTick - 1;
        break;
      case "ArrowRight":
      case "ArrowUp":
        if (!e.shiftKey) target = selectedTick + 1;
        break;
      case "PageDown":
        target = selectedTick - HOURS_PER_DAY;
        break;
      case "PageUp":
        target = selectedTick + HOURS_PER_DAY;
        break;
      case "Home":
        target = 0;
        break;
      case "End":
        target = headTick;
        break;
      default:
        break;
    }
    if (target === null) return;
    e.preventDefault();
    e.stopPropagation();
    select(target);
  }

  function onTrackClick(e: MouseEvent<HTMLDivElement>): void {
    const rect = trackRef.current?.getBoundingClientRect();
    if (!rect || rect.width <= 0 || horizonTicks <= 0) return;
    const ratio = (e.clientX - rect.left) / rect.width;
    select(Math.round(ratio * horizonTicks));
  }

  const lane = "relative h-5";
  const markerBase =
    "absolute top-0 -translate-x-1/2 h-5 min-w-4 px-px text-[11px] leading-5 text-center rounded-[4px] border";

  return (
    <section
      aria-label="Time strip"
      data-syncing={props.syncing ? "true" : "false"}
      className={`select-none rounded-[8px] border border-hairline bg-surface p-3 text-ivory ${props.syncing ? "opacity-70" : ""}`}
    >
      <div className="mb-1 flex justify-between text-xs tabular-nums text-ivory-dim">
        <span data-testid="timeline-readout">
          Showing {at(selectedTick)}
          {props.engineering ? ` · tick ${selectedTick}` : ""}
          {props.sharedUpTo !== undefined ? ` · Same history until ${at(props.sharedUpTo)}` : ""}
          {props.divergenceTick !== undefined ? ` · The two futures first look different at ${at(props.divergenceTick)}` : ""}
        </span>
        {props.syncing ? <span>Updating…</span> : null}
      </div>

      <div ref={trackRef} className="relative">
        <div className={lane} data-lane="disruptions">
          {frameMarkers
            .filter((m) => m.kind === "disruption_onset")
            .map((m, i) => (
              <button
                key={`d${m.tick}-${i}`}
                type="button"
                tabIndex={-1}
                data-marker="disruption"
                aria-label={`Surprise at ${at(m.tick)}: ${disruptionLabel(m.disruptionKind ?? "")}`}
                onClick={() => select(m.tick)}
                className={`${markerBase} border-clay bg-clay font-semibold text-ink`}
                style={{ left: pct(m.tick, horizonTicks) }}
              >
                !
              </button>
            ))}
        </div>
        <div className={`${lane} max-[768px]:hidden`} data-lane="decision-windows">
          {frameMarkers
            .filter((m) => m.kind === "decision_window")
            .map((m, i) => (
              <button
                key={`w${m.tick}-${i}`}
                type="button"
                tabIndex={-1}
                data-marker="decision_window"
                aria-label={`Decision moment at ${at(m.tick)}`}
                onClick={() => select(m.tick)}
                className={`${markerBase} border-ivory bg-transparent text-ivory max-[768px]:w-2 max-[768px]:min-w-2 max-[768px]:px-0 max-[768px]:text-[0px]`}
                style={{ left: pct(m.tick, horizonTicks) }}
              >
                ◇
              </button>
            ))}
        </div>
        <div className={lane} data-lane="forks">
          {forkPoints.map((f, i) => (
            <button
              key={`f${f.tick}-${i}`}
              type="button"
              tabIndex={-1}
              data-marker="fork"
              aria-label={`Futures split at ${at(f.tick)}: ${f.label}`}
              onClick={() => select(f.tick)}
              className={`${markerBase} border-amber bg-raised text-amber`}
              style={{ left: pct(f.tick, horizonTicks) }}
            >
              Y
            </button>
          ))}
        </div>
        {props.divergenceTick !== undefined ? (
          <div className={lane} data-lane="divergence">
            <button
              type="button"
              tabIndex={-1}
              data-marker="divergence"
              aria-label={`The two futures first look different at ${at(props.divergenceTick)}`}
              onClick={() => select(props.divergenceTick as number)}
              className={`${markerBase} border-clay bg-raised px-1 text-clay`}
              style={{ left: pct(props.divergenceTick, horizonTicks) }}
            >
              ≠
            </button>
          </div>
        ) : null}
        <div className={lane} data-lane="inputs">
          {inputs.map((inp, i) => {
            const m = PROVENANCE_MARKER[inp.provenance];
            const text = `${m.label}, takes effect at ${at(inp.effectiveTick)}`;
            return (
              <button
                key={`i${inp.effectiveTick}-${i}`}
                type="button"
                tabIndex={-1}
                data-marker="input"
                data-provenance={inp.provenance}
                aria-label={text}
                title={text}
                onClick={() => select(inp.effectiveTick)}
                className={`${markerBase} bg-ink ${m.classes}`}
                style={{ left: pct(inp.effectiveTick, horizonTicks) }}
              >
                {m.glyph}
              </button>
            );
          })}
        </div>

        {/* The focusable scrub bar: day bands, computed region and cursor. */}
        <div
          role="slider"
          tabIndex={0}
          aria-label="Displayed time"
          aria-orientation="horizontal"
          aria-valuemin={0}
          aria-valuemax={headTick}
          aria-valuenow={selectedTick}
          aria-valuetext={at(selectedTick)}
          onKeyDown={onKeyDown}
          onClick={onTrackClick}
          className="relative mt-1 h-11 cursor-pointer overflow-hidden rounded-[4px] border border-hairline bg-ink"
        >
          <div className="absolute inset-0 flex" aria-hidden="true">
            {Array.from({ length: days }, (_, d) => (
              <div
                key={d}
                data-day-band={d}
                className="h-full flex-1 border-r border-hairline px-1 pt-1 text-xs text-ivory-dim last:border-r-0"
              >
                {formatDayName(d * HOURS_PER_DAY, startClock)}
              </div>
            ))}
          </div>
          {props.sharedUpTo !== undefined ? (
            <div
              aria-hidden="true"
              data-testid="shared-history"
              className="absolute inset-y-0 left-0 border-r-2 border-dashed border-amber bg-ivory/10"
              style={{ width: pct(props.sharedUpTo, horizonTicks) }}
            />
          ) : null}
          <div
            aria-hidden="true"
            data-testid="uncomputed"
            className="absolute inset-y-0 right-0 bg-ink/70"
            style={{ left: pct(headTick, horizonTicks) }}
          />
          <div
            aria-hidden="true"
            data-testid="cursor"
            className="absolute inset-y-0 w-0.5 -translate-x-1/2 bg-amber transition-[left] duration-150 ease-out motion-reduce:transition-none"
            style={{ left: pct(selectedTick, horizonTicks) }}
          />
        </div>
      </div>
    </section>
  );
}

/** Time strip wired to the studio and playback stores. */
export function ConnectedTimeline({
  inputs,
  startClock,
  engineering,
  sharedUpTo,
  divergenceTick,
}: {
  sharedUpTo?: number;
  divergenceTick?: number;
  inputs?: TimelineInput[];
  startClock: string;
  engineering?: boolean;
}) {
  const horizonTicks = useStudioStore((s) => s.horizonTicks);
  const selectedTick = useStudioStore((s) => s.selectedTick);
  const syncing = useStudioStore((s) => s.syncing);
  const branches = useStudioStore((s) => s.branches);
  const active: Branch | undefined = useStudioStore(selectActiveBranch);
  const frames = useStudioStore((s) => (s.activeBranchId === null ? undefined : s.framesByBranch[s.activeBranchId]));
  const selectTick = usePlaybackStore((s) => s.selectTick);
  const revealedUpTo = useHighWater(active?.id ?? null, selectedTick);

  const forkPoints: ForkPoint[] = [];
  if (active) {
    if (active.parentBranchId !== null) forkPoints.push({ tick: active.forkTick, label: active.label });
    for (const b of branches) {
      if (b.parentBranchId === active.id) forkPoints.push({ tick: b.forkTick, label: b.label });
    }
  }

  return (
    <Timeline
      horizonTicks={horizonTicks}
      headTick={active?.headTick ?? 0}
      selectedTick={selectedTick}
      revealedUpTo={revealedUpTo}
      startClock={startClock}
      frameMarkers={markersFromFrames(frames)}
      inputs={inputs ?? []}
      forkPoints={forkPoints}
      {...(sharedUpTo !== undefined ? { sharedUpTo } : {})}
      {...(divergenceTick !== undefined ? { divergenceTick } : {})}
      {...(engineering !== undefined ? { engineering } : {})}
      syncing={syncing}
      onSelectTick={(t) => {
        void selectTick(t);
      }}
    />
  );
}
