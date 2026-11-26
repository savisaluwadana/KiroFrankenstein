"use client";

/**
 * Playback controls. Playback is presentation only: these buttons pace the
 * display of authoritative frames through the playback store and never change
 * what the engine computes. Key bindings are installed once by the page through
 * `usePlaybackKeybindings`.
 */
import { useEffect } from "react";
import { FastForward, Pause, Play, StepBack, StepForward, TriangleAlert } from "lucide-react";
import { InfoPopover } from "@/features/studio/InfoPopover";
import { usePlaybackStore, type AutoPauseMode, type PlaybackPauseReason, type PlaybackSpeed } from "@/stores/playback";

/** Plain speed names. Pace is unchanged: 2, 8 and 32 hours per second. */
export const SPEEDS: readonly { speed: PlaybackSpeed; label: string; hint: string }[] = [
  { speed: 1, label: "Calm", hint: "about 2 hours per second" },
  { speed: 4, label: "Quick", hint: "about 8 hours per second" },
  { speed: 16, label: "Fast", hint: "about 32 hours per second" },
];

export const KEY_HINTS: readonly { keys: string; action: string }[] = [
  { keys: "Space", action: "Play or pause" },
  { keys: "← →", action: "Previous or next hour" },
  { keys: "Shift + ← →", action: "Previous or next decision moment" },
  { keys: "Shift + D", action: "Skip to the next surprise" },
  { keys: "F", action: "Jump to Explore another future" },
];

const PAUSE_TEXT: Record<PlaybackPauseReason, string> = {
  user: "Paused",
  decision_window: "Paused for your move",
  disruption_onset: "Paused: something just happened",
  horizon: "Reached the end of the week",
  error: "Paused after a problem",
};

const btn =
  "inline-flex min-h-11 min-w-11 items-center justify-center gap-2 rounded-[4px] border border-hairline bg-surface px-3 text-sm text-ivory transition-colors duration-150 ease-out hover:border-amber disabled:opacity-40 motion-reduce:transition-none";

function isEditable(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || target.isContentEditable;
}

function isActivatable(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  return tag === "BUTTON" || tag === "A" || tag === "SUMMARY";
}

/** Installs Space, Left/Right, Shift+Left/Right and Shift+D at document level. Ignored while typing. */
export function usePlaybackKeybindings(): void {
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent): void {
      if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey) return;
      if (isEditable(e.target)) return;
      const pb = usePlaybackStore.getState();
      switch (e.key) {
        case " ":
          // A focused button keeps its own Space activation.
          if (isActivatable(e.target)) return;
          e.preventDefault();
          if (pb.playing) pb.pause();
          else pb.play();
          return;
        case "ArrowLeft":
          e.preventDefault();
          void (e.shiftKey ? pb.stepWindowBack() : pb.stepBack());
          return;
        case "ArrowRight":
          e.preventDefault();
          void (e.shiftKey ? pb.stepWindowForward() : pb.stepForward());
          return;
        case "D":
        case "d":
          if (!e.shiftKey) return;
          e.preventDefault();
          void pb.fastForwardToDisruptionOnset();
          return;
        default:
          return;
      }
    }
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);
}

export function PlaybackControls() {
  const playing = usePlaybackStore((s) => s.playing);
  const speed = usePlaybackStore((s) => s.speed);
  const mode = usePlaybackStore((s) => s.autoPauseMode);
  const reason = usePlaybackStore((s) => s.pauseReason);
  const busy = usePlaybackStore((s) => s.busy);
  const pb = usePlaybackStore.getState;

  return (
    <section aria-label="Playback controls" className="flex flex-wrap items-center gap-2">
      <button
        type="button"
        className={`${btn} border-amber text-amber`}
        aria-pressed={playing}
        onClick={() => (playing ? pb().pause() : pb().play())}
      >
        {playing ? <Pause aria-hidden size={18} /> : <Play aria-hidden size={18} />}
        {playing ? "Pause" : "Play"}
      </button>

      <div role="group" aria-label="Speed" className="flex gap-1">
        {SPEEDS.map((s) => (
          <button
            key={s.speed}
            type="button"
            className={`${btn} ${speed === s.speed ? "border-amber text-amber" : ""}`}
            aria-pressed={speed === s.speed}
            title={s.hint}
            onClick={() => pb().setSpeed(s.speed)}
          >
            {s.label}
          </button>
        ))}
      </div>

      <div role="group" aria-label="Move through time" className="flex flex-wrap gap-1">
        <button type="button" className={btn} onClick={() => void pb().stepBack()}>
          <StepBack aria-hidden size={18} />
          Previous moment
        </button>
        <button type="button" className={btn} onClick={() => void pb().stepForward()}>
          <StepForward aria-hidden size={18} />
          Next moment
        </button>
        <button type="button" className={btn} onClick={() => void pb().fastForwardToDecisionWindow()}>
          <FastForward aria-hidden size={18} />
          Skip to next move
        </button>
        <button type="button" className={btn} onClick={() => void pb().fastForwardToDisruptionOnset()}>
          <TriangleAlert aria-hidden size={18} />
          Skip to next surprise
        </button>
      </div>

      <label className="flex items-center gap-2 text-xs text-ivory-dim">
        Pause automatically at
        <select
          className="min-h-11 rounded-[4px] border border-hairline bg-ink px-2 text-sm text-ivory"
          value={mode}
          onChange={(e) => {
            const next: AutoPauseMode =
              e.target.value === "disruption_onsets_only" ? "disruption_onsets_only" : "every_decision_window";
            pb().setAutoPauseMode(next);
          }}
        >
          <option value="every_decision_window">every decision moment</option>
          <option value="disruption_onsets_only">surprises only</option>
        </select>
      </label>

      <span role="status" className="text-sm text-ivory-dim" data-testid="playback-status">
        {playing ? `Playing (${SPEEDS.find((s) => s.speed === speed)?.label ?? ""})` : reason !== null ? PAUSE_TEXT[reason] : "Paused"}
        {busy ? " · working" : ""}
      </span>

      <InfoPopover label="keyboard shortcuts" align="end" trigger={<span aria-hidden>?</span>}>
        <span className="block font-medium">Keyboard shortcuts</span>
        <ul className="mt-1 space-y-1" data-testid="key-hints">
          {KEY_HINTS.map((h) => (
            <li key={h.keys}>
              <kbd className="rounded-[4px] border border-hairline px-1 text-xs">{h.keys}</kbd> {h.action}
            </li>
          ))}
        </ul>
      </InfoPopover>
    </section>
  );
}
