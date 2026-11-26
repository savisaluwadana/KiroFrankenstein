"use client";

/**
 * Timeline: scrubber across the Scenario horizon. Every marker comes from a
 * Tick_Frame, an Input or a Branch returned by the API. Selecting a Tick calls
 * `onSelectTick` (wired to `playback.selectTick`), which reads State and never
 * advances the Branch (Requirements 32.3, 32.4, 35.4).
 */
import { useRef, type KeyboardEvent, type MouseEvent } from "react";
import type { Branch, TickFrame } from "@/lib/api/client";
import type { components } from "@/lib/api/schema";
import { selectActiveBranch, useStudioStore } from "@/stores/studio";
import { usePlaybackStore } from "@/stores/playback";

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
  frameMarkers: FrameMarkerAt[];
  inputs?: TimelineInput[];
  forkPoints?: ForkPoint[];
  /** First_Divergence_Tick reported by the Branch_Diff_Service, if any (35.7). */
  divergenceTick?: number | null;
  syncing?: boolean;
  onSelectTick: (tick: number) => void;
}

const HOURS_PER_DAY = 24;
const DAY_NAMES = ["Thursday", "Friday", "Saturday", "Sunday", "Monday"];

const PROVENANCE: Record<InputProvenance, { glyph: string; label: string; color: string }> = {
  human: { glyph: "H", label: "Human", color: "#F2A93B" },
  ai_rule_based: { glyph: "R", label: "Rule-based AI", color: "#3BC9DB" },
  ai_llm: { glyph: "L", label: "LLM AI", color: "#3BC9DB" },
  ai_rule_based_fallback: { glyph: "F", label: "Rule-based AI (LLM fallback)", color: "#3BC9DB" },
  scenario: { glyph: "S", label: "Scenario", color: "#8A93A6" },
  injected: { glyph: "I", label: "Injected", color: "#FF5C5C" },
};

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

function pct(tick: number, horizon: number): string {
  if (horizon <= 0) return "0%";
  const clamped = Math.max(0, Math.min(tick, horizon));
  return `${(clamped * 100) / horizon}%`;
}

function dayLabel(tick: number): string {
  const day = Math.floor(tick / HOURS_PER_DAY);
  return DAY_NAMES[day] ?? `Day ${day + 1}`;
}

export function Timeline(props: TimelineProps) {
  const { horizonTicks, headTick, selectedTick, frameMarkers, onSelectTick } = props;
  const inputs = props.inputs ?? [];
  const forkPoints = props.forkPoints ?? [];
  const trackRef = useRef<HTMLDivElement>(null);
  const days = Math.max(1, Math.ceil(horizonTicks / HOURS_PER_DAY));

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

  const lane = "relative h-4";
  const markerBase =
    "absolute top-0 -translate-x-1/2 h-4 min-w-3 px-px font-mono text-[10px] leading-4 text-center rounded-[4px] border";

  return (
    <section
      aria-label="Timeline"
      data-syncing={props.syncing ? "true" : "false"}
      className={`select-none rounded-[4px] border border-[#232B3A] bg-[#121722] p-2 text-[#E6E9EF] ${props.syncing ? "opacity-70" : ""}`}
    >
      <div className="mb-1 flex justify-between font-mono text-xs tabular-nums text-[#8A93A6]">
        <span>Tick {selectedTick}</span>
        <span>
          Head {headTick} / Horizon {horizonTicks}
        </span>
        {props.syncing ? <span>syncing</span> : null}
      </div>

      <div ref={trackRef} className="relative">
        {/* Decision windows, disruptions, forks and Inputs sit in separate lanes. */}
        <div className={lane} data-lane="disruptions">
          {frameMarkers
            .filter((m) => m.kind === "disruption_onset")
            .map((m, i) => (
              <button
                key={`d${m.tick}-${i}`}
                type="button"
                tabIndex={-1}
                data-marker="disruption"
                aria-label={`Disruption onset at tick ${m.tick}${m.disruptionKind ? `: ${m.disruptionKind}` : ""}`}
                onClick={() => select(m.tick)}
                className={`${markerBase} border-[#FF5C5C] bg-[#FF5C5C] text-[#0B0E14]`}
                style={{ left: pct(m.tick, horizonTicks) }}
              >
                !
              </button>
            ))}
        </div>
        <div className={lane} data-lane="decision-windows">
          {frameMarkers
            .filter((m) => m.kind === "decision_window")
            .map((m, i) => (
              <button
                key={`w${m.tick}-${i}`}
                type="button"
                tabIndex={-1}
                data-marker="decision_window"
                aria-label={`Decision window at tick ${m.tick}${m.windowReason ? ` (${m.windowReason})` : ""}`}
                onClick={() => select(m.tick)}
                className={`${markerBase} border-[#E6E9EF] bg-transparent text-[#E6E9EF]`}
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
              aria-label={`Fork point at tick ${f.tick}: ${f.label}`}
              onClick={() => select(f.tick)}
              className={`${markerBase} border-[#8A93A6] bg-[#232B3A] text-[#E6E9EF]`}
              style={{ left: pct(f.tick, horizonTicks) }}
            >
              Y
            </button>
          ))}
          {props.divergenceTick !== null && props.divergenceTick !== undefined ? (
            <button
              type="button"
              tabIndex={-1}
              data-marker="divergence"
              aria-label={`First divergence from parent at tick ${props.divergenceTick}`}
              onClick={() => select(props.divergenceTick as number)}
              className={`${markerBase} border-[#E6E9EF] bg-[#E6E9EF] text-[#0B0E14]`}
              style={{ left: pct(props.divergenceTick, horizonTicks) }}
            >
              ≠
            </button>
          ) : null}
        </div>
        <div className={lane} data-lane="inputs">
          {inputs.map((inp, i) => {
            const p = PROVENANCE[inp.provenance];
            return (
              <button
                key={`i${inp.effectiveTick}-${i}`}
                type="button"
                tabIndex={-1}
                data-marker="input"
                data-provenance={inp.provenance}
                aria-label={`Input ${inp.kind} by ${p.label}, effective tick ${inp.effectiveTick}`}
                onClick={() => select(inp.effectiveTick)}
                className={markerBase}
                style={{
                  left: pct(inp.effectiveTick, horizonTicks),
                  borderColor: p.color,
                  color: p.color,
                  background: "#0B0E14",
                }}
              >
                {p.glyph}
              </button>
            );
          })}
        </div>

        {/* The focusable scrub bar: day bands, hour ticks, computed region and cursor. */}
        <div
          role="slider"
          tabIndex={0}
          aria-label="Displayed tick"
          aria-orientation="horizontal"
          aria-valuemin={0}
          aria-valuemax={headTick}
          aria-valuenow={selectedTick}
          aria-valuetext={`Tick ${selectedTick}, ${dayLabel(selectedTick)} ${String(selectedTick % HOURS_PER_DAY).padStart(2, "0")}:00`}
          onKeyDown={onKeyDown}
          onClick={onTrackClick}
          className="relative mt-1 h-8 cursor-pointer overflow-hidden rounded-[4px] border border-[#232B3A] bg-[#0B0E14] focus-visible:outline focus-visible:outline-2 focus-visible:outline-[#E6E9EF]"
        >
          <div className="absolute inset-0 flex" aria-hidden="true">
            {Array.from({ length: days }, (_, d) => (
              <div
                key={d}
                data-day-band={d}
                className="h-full flex-1 border-r border-[#232B3A] px-1 font-mono text-[10px] text-[#8A93A6] last:border-r-0"
                style={{
                  backgroundImage: "repeating-linear-gradient(90deg, #232B3A 0 1px, transparent 1px 4.1667%)",
                  backgroundPosition: "bottom",
                  backgroundSize: "100% 25%",
                  backgroundRepeat: "no-repeat",
                }}
              >
                {DAY_NAMES[d] ?? `Day ${d + 1}`}
              </div>
            ))}
          </div>
          <div
            aria-hidden="true"
            data-testid="uncomputed"
            className="absolute inset-y-0 right-0 bg-[#0B0E14]/70"
            style={{ left: pct(headTick, horizonTicks) }}
          />
          <div
            aria-hidden="true"
            data-testid="cursor"
            className="absolute inset-y-0 w-0.5 -translate-x-1/2 bg-[#F2A93B] transition-[left] duration-150 ease-out motion-reduce:transition-none"
            style={{ left: pct(selectedTick, horizonTicks) }}
          />
        </div>
      </div>
    </section>
  );
}

/** Timeline wired to the studio and playback stores. */
export function ConnectedTimeline({ inputs }: { inputs?: TimelineInput[] }) {
  const horizonTicks = useStudioStore((s) => s.horizonTicks);
  const selectedTick = useStudioStore((s) => s.selectedTick);
  const syncing = useStudioStore((s) => s.syncing);
  const branches = useStudioStore((s) => s.branches);
  const active: Branch | undefined = useStudioStore(selectActiveBranch);
  const frames = useStudioStore((s) => (s.activeBranchId === null ? undefined : s.framesByBranch[s.activeBranchId]));
  const selectTick = usePlaybackStore((s) => s.selectTick);

  const forkPoints: ForkPoint[] = [];
  if (active) {
    if (active.parentBranchId !== null) forkPoints.push({ tick: active.forkTick, label: `forked from parent (${active.label})` });
    for (const b of branches) {
      if (b.parentBranchId === active.id) forkPoints.push({ tick: b.forkTick, label: b.label });
    }
  }

  return (
    <Timeline
      horizonTicks={horizonTicks}
      headTick={active?.headTick ?? 0}
      selectedTick={selectedTick}
      frameMarkers={markersFromFrames(frames)}
      inputs={inputs ?? []}
      forkPoints={forkPoints}
      syncing={syncing}
      onSelectTick={(t) => {
        void selectTick(t);
      }}
    />
  );
}
