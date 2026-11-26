"use client";

/**
 * Playback controls. Playback is presentation only: these buttons pace the
 * display of authoritative frames through the playback store and never change
 * what the engine computes (Requirements 33.2, 33.10). Key bindings are
 * installed once by the page through `usePlaybackKeybindings`.
 */
import { useEffect } from "react";
import { usePlaybackStore, type AutoPauseMode, type PlaybackPauseReason, type PlaybackSpeed } from "@/stores/playback";

const SPEEDS: readonly PlaybackSpeed[] = [1, 4, 16];

export const KEY_HINTS: readonly { keys: string; action: string }[] = [
  { keys: "Space", action: "play / pause" },
  { keys: "← →", action: "step one tick" },
  { keys: "Shift+← →", action: "step one decision window" },
  { keys: "Shift+D", action: "next disruption" },
];

const PAUSE_TEXT: Record<PlaybackPauseReason, string> = {
  user: "Paused",
  decision_window: "Paused at a decision window",
  disruption_onset: "Paused at a disruption onset",
  horizon: "Reached the horizon",
  error: "Paused after an error",
};

const btn =
  "rounded-[4px] border border-[#232B3A] bg-[#121722] px-2 py-1 font-mono text-xs text-[#E6E9EF] transition-colors duration-150 ease-out hover:border-[#F2A93B] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#E6E9EF] disabled:opacity-40 motion-reduce:transition-none";

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
    <section
      aria-label="Playback controls"
      className="flex flex-wrap items-center gap-2 rounded-[4px] border border-[#232B3A] bg-[#121722] px-3 py-2"
    >
      <button
        type="button"
        className={btn}
        aria-pressed={playing}
        onClick={() => (playing ? pb().pause() : pb().play())}
      >
        {playing ? "Pause" : "Play"}
      </button>

      <div role="group" aria-label="Playback speed" className="flex gap-1">
        {SPEEDS.map((s) => (
          <button
            key={s}
            type="button"
            className={`${btn} ${speed === s ? "border-[#F2A93B] text-[#F2A93B]" : ""}`}
            aria-pressed={speed === s}
            onClick={() => pb().setSpeed(s)}
          >
            {s}x
          </button>
        ))}
      </div>

      <div role="group" aria-label="Stepping" className="flex gap-1">
        <button type="button" className={btn} onClick={() => void pb().stepBack()} aria-label="Step back one tick">
          −1 tick
        </button>
        <button type="button" className={btn} onClick={() => void pb().stepForward()} aria-label="Step forward one tick">
          +1 tick
        </button>
        <button
          type="button"
          className={btn}
          onClick={() => void pb().stepWindowBack()}
          aria-label="Step back one decision window"
        >
          ◇ prev
        </button>
        <button
          type="button"
          className={btn}
          onClick={() => void pb().fastForwardToDecisionWindow()}
          aria-label="Fast-forward to the next decision window"
        >
          ◇ next
        </button>
        <button
          type="button"
          className={btn}
          onClick={() => void pb().fastForwardToDisruptionOnset()}
          aria-label="Fast-forward to the next disruption"
        >
          ! next
        </button>
      </div>

      <label className="flex items-center gap-1 text-xs text-[#8A93A6]">
        Auto-pause
        <select
          className="rounded-[4px] border border-[#232B3A] bg-[#0B0E14] px-1 py-1 font-mono text-xs text-[#E6E9EF] focus-visible:outline focus-visible:outline-2 focus-visible:outline-[#E6E9EF]"
          value={mode}
          onChange={(e) => {
            const next: AutoPauseMode =
              e.target.value === "disruption_onsets_only" ? "disruption_onsets_only" : "every_decision_window";
            pb().setAutoPauseMode(next);
          }}
        >
          <option value="every_decision_window">every decision window</option>
          <option value="disruption_onsets_only">disruption onsets only</option>
        </select>
      </label>

      <span role="status" className="font-mono text-xs text-[#8A93A6]" data-testid="playback-status">
        {playing ? `Playing ${speed}x` : reason !== null ? PAUSE_TEXT[reason] : "Paused"}
        {busy ? " · working" : ""}
      </span>

      <p className="w-full font-mono text-[10px] text-[#8A93A6]" data-testid="key-hints">
        {KEY_HINTS.map((h) => `${h.keys}: ${h.action}`).join(" · ")}
      </p>
    </section>
  );
}
