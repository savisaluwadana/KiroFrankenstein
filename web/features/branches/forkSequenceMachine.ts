/**
 * State machine for the "Explore another future" split sequence. The sequence
 * starts only when the server has returned the new reality, never on the press.
 * Skipping ends it at once. Under reduced motion it is shown as a static
 * caption instead of an animation.
 */
export interface ForkSequenceInfo {
  /** The fork moment as words, for example "Thursday, 4:00 AM". */
  whenLabel: string;
}

export type ForkSequenceState =
  | { phase: "idle" }
  | { phase: "playing" | "static"; info: ForkSequenceInfo };

export type ForkSequenceEvent =
  | { type: "fork_requested" }
  | { type: "server_returned"; info: ForkSequenceInfo; reducedMotion: boolean }
  | { type: "skip" }
  | { type: "finished" };

export const FORK_SEQUENCE_MS = 1500;
/** How long the static caption stays under reduced motion. */
export const FORK_STATIC_MS = 5000;

export const INITIAL_FORK_SEQUENCE: ForkSequenceState = { phase: "idle" };

export function forkSequenceReducer(state: ForkSequenceState, ev: ForkSequenceEvent): ForkSequenceState {
  switch (ev.type) {
    case "fork_requested":
      return state;
    case "server_returned":
      return { phase: ev.reducedMotion ? "static" : "playing", info: ev.info };
    case "skip":
    case "finished":
      return INITIAL_FORK_SEQUENCE;
  }
}

export function caption(info: ForkSequenceInfo): string {
  return `Same history up to ${info.whenLabel}. Different futures from here.`;
}

export function prefersReducedMotion(): boolean {
  return typeof window !== "undefined" && typeof window.matchMedia === "function"
    ? window.matchMedia("(prefers-reduced-motion: reduce)").matches
    : false;
}
