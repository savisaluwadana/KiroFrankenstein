/**
 * Playback controller. Playback is presentation only: it paces the display of
 * authoritative Tick_Frames already received from the API and asks the server
 * to advance a Branch only when display reaches the Head_Tick. Speed, stepping
 * and fast-forward never change what the engine computes (Requirement 33.10).
 */
import { create } from "zustand";
import * as api from "@/lib/api/client";
import { ApiError } from "@/lib/api/client";
import type { AdvanceRequest, AdvanceResult, TickFrame } from "@/lib/api/client";
import { requestFailureMessage } from "@/lib/glossary";
import { openProgress, type ProgressHandle, type ProgressStatus } from "@/lib/api/progress";
import { useStudioStore, type StudioStore } from "./studio";

export type PlaybackSpeed = 1 | 4 | 16;
export type AutoPauseMode = "every_decision_window" | "disruption_onsets_only";
export type PlaybackPauseReason = "user" | "decision_window" | "disruption_onset" | "horizon" | "error";

/** Ticks displayed per second of real time (Requirement 33.2). */
export const PLAYBACK_TICKS_PER_SECOND: Record<PlaybackSpeed, number> = { 1: 2, 4: 8, 16: 32 };

/** Frames requested ahead when the displayed Tick is below the Head_Tick but not loaded. */
const LOOKAHEAD = 64;

export type PlaybackClient = Pick<
  typeof api,
  "advanceBranch" | "listTickFrames" | "getBranch" | "getBranchState" | "getBranchMetrics"
>;

export interface Scheduler {
  setInterval(fn: () => void, ms: number): unknown;
  clearInterval(handle: unknown): void;
}

export const defaultScheduler: Scheduler = {
  setInterval: (fn, ms) => globalThis.setInterval(fn, ms),
  clearInterval: (handle) => globalThis.clearInterval(handle as ReturnType<typeof setInterval>),
};

export interface PlaybackDeps {
  client: PlaybackClient;
  studio: StudioStore;
  scheduler: Scheduler;
  /** Progress_Stream opener; defaults to the real EventSource consumer with polling fallback. */
  progress: typeof openProgress;
}

/** Replaces the server call that advances the active Branch (the Duel view advances both sides through the Duel endpoint). */
export type Advancer = (branchId: string, body: AdvanceRequest) => Promise<AdvanceResult>;

export interface PlaybackState {
  playing: boolean;
  speed: PlaybackSpeed;
  /** Reset to "every_decision_window" by `resetForWorld` when a World is opened (33.15). */
  autoPauseMode: AutoPauseMode;
  /** Why playback last stopped; null while playing. */
  pauseReason: PlaybackPauseReason | null;
  /** True while a load or advance request started by the controller is in flight. */
  busy: boolean;
  /** Progress_Stream connection status; null while not watching. Shown in Engineering Mode only. */
  progressStatus: ProgressStatus | null;

  /**
   * Starts receiving newly computed frames of the active Branch from the
   * Progress_Stream (switching with the active Branch). It never advances the
   * simulation. Returns a function that stops watching.
   */
  watchProgress(): () => void;
  play(speed?: PlaybackSpeed): void;
  pause(): void;
  setSpeed(speed: PlaybackSpeed): void;
  setAutoPauseMode(mode: AutoPauseMode): void;
  /** One playback step; normally driven by the scheduler. Resolves when any request it started finishes. */
  tick(): Promise<void>;
  stepForward(): Promise<void>;
  stepBack(): Promise<void>;
  stepWindowForward(): Promise<void>;
  stepWindowBack(): Promise<void>;
  fastForwardToDecisionWindow(): Promise<void>;
  fastForwardToDisruptionOnset(): Promise<void>;
  /** Pauses and displays `tick` (0..Head_Tick) without changing the Head_Tick. */
  selectTick(tick: number): Promise<void>;
  /** Call when a World is opened: pauses and restores the default Auto_Pause_Mode. */
  resetForWorld(): void;
  /** Sets or clears the advance override. Playback pacing is unchanged. */
  setAdvancer(advancer: Advancer | null): void;
}

function hasMarker(frame: TickFrame | undefined, kind: "decision_window" | "disruption_onset"): boolean {
  return frame?.markers.some((m) => m.kind === kind) ?? false;
}

function message(e: unknown): string {
  if (e instanceof ApiError && e.type === "decision-stale") return requestFailureMessage(e.type);
  return e instanceof Error ? e.message : "Request failed";
}

export function createPlaybackStore(deps: Partial<PlaybackDeps> = {}) {
  const client: PlaybackClient = deps.client ?? api;
  const studio: StudioStore = deps.studio ?? useStudioStore;
  const scheduler: Scheduler = deps.scheduler ?? defaultScheduler;
  const progress = deps.progress ?? openProgress;

  let handle: unknown = null;
  let inFlight = false;
  let advancer: Advancer | null = null;

  const store = create<PlaybackState>()((set, get) => {
    function stopTimer(): void {
      if (handle !== null) {
        scheduler.clearInterval(handle);
        handle = null;
      }
    }

    function startTimer(): void {
      stopTimer();
      const ms = 1000 / PLAYBACK_TICKS_PER_SECOND[get().speed];
      handle = scheduler.setInterval(() => {
        void get().tick();
      }, ms);
    }

    function pauseWith(reason: PlaybackPauseReason): void {
      stopTimer();
      set({ playing: false, pauseReason: reason });
    }

    function ctx() {
      const s = studio.getState();
      const branch = s.branches.find((b) => b.id === s.activeBranchId);
      if (!branch) return null;
      return {
        s,
        branchId: branch.id,
        head: branch.headTick,
        horizon: s.horizonTicks,
        selected: s.selectedTick,
        frames: s.framesByBranch[branch.id] ?? {},
        openWindowTick: s.openWindowTick,
      };
    }

    function fail(e: unknown): void {
      if (e instanceof ApiError && e.type === "engine-version-mismatch") studio.getState().setEngineMismatch(true);
      studio.getState().setError(message(e));
      pauseWith("error");
    }

    async function loadFrames(branchId: string, from: number, to: number): Promise<void> {
      const res = await client.listTickFrames(branchId, { from, to });
      if (api.hasEngineMismatch(res.warnings)) studio.getState().setEngineMismatch(true);
      studio.getState().addFrames(branchId, res.frames);
    }

    /** Fetches State and Metrics for a paused Tick; stale responses are dropped. */
    async function loadStateAt(branchId: string, tick: number): Promise<void> {
      try {
        const [state, metrics] = await Promise.all([
          client.getBranchState(branchId, tick),
          client.getBranchMetrics(branchId, tick),
        ]);
        const s = studio.getState();
        if (s.activeBranchId !== branchId || s.selectedTick !== tick) return;
        s.setBranchState(state);
        s.setMetrics(metrics);
      } catch (e) {
        studio.getState().setError(message(e));
      }
    }

    function settle(branchId: string): void {
      if (branchId === "") return;
      void loadStateAt(branchId, studio.getState().selectedTick);
    }

    function display(tick: number): void {
      const s = studio.getState();
      s.setSelectedTick(tick);
      s.setSyncing(false);
    }

    /** Refetches the Branch and any frames missed after a stale-head conflict. */
    async function resync(branchId: string): Promise<void> {
      const branch = await client.getBranch(branchId);
      const s = studio.getState();
      s.upsertBranch(branch);
      const loaded = Object.keys(s.framesByBranch[branchId] ?? {}).map(Number);
      const from = loaded.length === 0 ? 0 : Math.max(...loaded) + 1;
      if (from <= branch.headTick) await loadFrames(branchId, from, branch.headTick);
    }

    /**
     * Sends one advance request at the current Head_Tick. Returns null when the
     * request failed or was superseded; errors other than stale-head pause playback.
     */
    async function advance(branchId: string, runToTick: number | undefined): Promise<AdvanceResult | null> {
      const c = ctx();
      if (!c || c.branchId !== branchId) return null;
      const s = studio.getState();
      s.setSyncing(true);
      const body: AdvanceRequest = { expectedHeadTick: c.head };
      if (runToTick !== undefined) body.runToTick = runToTick;
      try {
        const result = advancer ? await advancer(branchId, body) : await client.advanceBranch(branchId, body);
        const after = studio.getState();
        if (after.activeBranchId !== branchId) return null;
        after.addFrames(branchId, result.frames);
        after.setBranchHead(branchId, result.headTick);
        after.setOpenWindowTick(result.paused === "decision_window" ? result.headTick : null);
        return result;
      } catch (e) {
        if (e instanceof ApiError && e.type === "stale-head") {
          try {
            await resync(branchId);
          } catch (e2) {
            fail(e2);
          }
          return null;
        }
        fail(e);
        return null;
      } finally {
        studio.getState().setSyncing(false);
      }
    }

    /** Displays `tick` after loading its frame if needed. Never sends an advance request. */
    async function showLoaded(branchId: string, tick: number): Promise<boolean> {
      const c = ctx();
      if (!c || c.branchId !== branchId || tick < 0 || tick > c.head) return false;
      if (!c.frames[tick]) {
        studio.getState().setSyncing(true);
        try {
          await loadFrames(branchId, tick, tick);
        } catch (e) {
          studio.getState().setSyncing(false);
          fail(e);
          return false;
        }
      }
      const after = studio.getState();
      if (after.activeBranchId !== branchId || !after.framesByBranch[branchId]?.[tick]) {
        after.setSyncing(false);
        return false;
      }
      display(tick);
      return true;
    }

    /** Runs `fn` as the only in-flight controller request. */
    async function exclusive(fn: () => Promise<void>): Promise<void> {
      if (inFlight) return;
      inFlight = true;
      set({ busy: true });
      try {
        await fn();
      } finally {
        inFlight = false;
        set({ busy: false });
      }
    }

    function lowestLoadedAbove(kind: "decision_window" | "disruption_onset"): number | null {
      const c = ctx();
      if (!c) return null;
      for (let t = c.selected + 1; t <= c.head; t++) {
        if (hasMarker(c.frames[t], kind)) return t;
      }
      return null;
    }

    function highestLoadedBelowWindow(): number | null {
      const c = ctx();
      if (!c) return null;
      for (let t = c.selected - 1; t >= 0; t--) {
        if (hasMarker(c.frames[t], "decision_window")) return t;
      }
      return null;
    }

    return {
      playing: false,
      speed: 1,
      autoPauseMode: "every_decision_window",
      pauseReason: "user",
      busy: false,
      progressStatus: null,

      watchProgress() {
        let handle: ProgressHandle | null = null;
        let watched: string | null = null;
        let stopped = false;

        const close = (): void => {
          handle?.close();
          handle = null;
          watched = null;
        };

        const open = (branchId: string | null): void => {
          if (branchId === watched) return;
          close();
          if (stopped || branchId === null) {
            set({ progressStatus: null });
            return;
          }
          const s = studio.getState();
          const loaded = Object.keys(s.framesByBranch[branchId] ?? {}).map(Number);
          const fromTick = loaded.length === 0 ? 0 : Math.max(...loaded) + 1;
          watched = branchId;
          handle = progress(branchId, {
            fromTick,
            onStatus: (status) => {
              if (watched === branchId) set({ progressStatus: status });
            },
            onFrame: (frame) => {
              if (watched !== branchId) return;
              const st = studio.getState();
              st.addFrames(branchId, [frame]);
              const b = st.branches.find((x) => x.id === branchId);
              if (b && frame.tick > b.headTick) st.setBranchHead(branchId, frame.tick);
            },
          });
        };

        open(studio.getState().activeBranchId);
        const unsubscribe = studio.subscribe((s) => open(s.activeBranchId));
        return () => {
          stopped = true;
          unsubscribe();
          close();
          set({ progressStatus: null });
        };
      },

      play(speed) {
        if (speed !== undefined) set({ speed });
        const c = ctx();
        if (!c) return;
        if (c.selected >= c.horizon) {
          pauseWith("horizon");
          return;
        }
        set({ playing: true, pauseReason: null });
        startTimer();
      },

      pause() {
        pauseWith("user");
        settle(studio.getState().activeBranchId ?? "");
      },

      setSpeed(speed) {
        set({ speed });
        if (get().playing) startTimer();
      },

      setAutoPauseMode(mode) {
        set({ autoPauseMode: mode });
      },

      setAdvancer(next) {
        advancer = next;
      },

      resetForWorld() {
        pauseWith("user");
        set({ autoPauseMode: "every_decision_window", speed: 1 });
      },

      async tick() {
        if (!get().playing || inFlight) return;
        const c = ctx();
        if (!c) {
          pauseWith("error");
          return;
        }
        if (c.selected >= c.horizon) {
          pauseWith("horizon");
          settle(c.branchId);
          return;
        }
        const next = c.selected + 1;
        const mode = get().autoPauseMode;
        if (c.frames[next]) {
          display(next);
          const frame = c.frames[next];
          if (mode === "every_decision_window" && hasMarker(frame, "decision_window")) {
            pauseWith("decision_window");
          } else if (mode === "disruption_onsets_only" && hasMarker(frame, "disruption_onset")) {
            pauseWith("disruption_onset");
          } else if (next >= c.horizon) {
            pauseWith("horizon");
          } else {
            return;
          }
          settle(c.branchId);
          return;
        }
        // The frame for the next Tick is not loaded: hold the displayed Tick and sync (33.4).
        await exclusive(async () => {
          if (next <= c.head) {
            studio.getState().setSyncing(true);
            try {
              await loadFrames(c.branchId, next, Math.min(c.head, next + LOOKAHEAD - 1));
            } catch (e) {
              fail(e);
            } finally {
              studio.getState().setSyncing(false);
            }
            return;
          }
          // Playback reached the Head_Tick (33.11).
          if (mode === "every_decision_window" && c.openWindowTick === c.head) {
            pauseWith("decision_window");
            return;
          }
          // In onsets-only mode, run to the horizon; the server stops at the next onset window (33.7).
          await advance(c.branchId, mode === "disruption_onsets_only" ? c.horizon : undefined);
        });
      },

      async stepForward() {
        pauseWith("user");
        await exclusive(async () => {
          const c = ctx();
          if (!c) return;
          const next = c.selected + 1;
          if (next > c.horizon) return;
          if (!c.frames[next]) {
            if (next <= c.head) {
              if (!(await showLoaded(c.branchId, next))) return;
            } else {
              if (c.openWindowTick === c.head) return;
              const r = await advance(c.branchId, next);
              if (!r) return;
            }
          }
          const after = ctx();
          if (after && after.frames[next]) display(next);
        });
        settle(studio.getState().activeBranchId ?? "");
      },

      async stepBack() {
        pauseWith("user");
        const c = ctx();
        if (!c || c.selected <= 0) return;
        await exclusive(async () => {
          await showLoaded(c.branchId, c.selected - 1);
        });
        settle(c.branchId);
      },

      async stepWindowForward() {
        await get().fastForwardToDecisionWindow();
      },

      async stepWindowBack() {
        pauseWith("user");
        const c = ctx();
        if (!c) return;
        const target = highestLoadedBelowWindow();
        if (target === null) return;
        await exclusive(async () => {
          await showLoaded(c.branchId, target);
        });
        settle(c.branchId);
      },

      async fastForwardToDecisionWindow() {
        pauseWith("user");
        await exclusive(async () => {
          const c = ctx();
          if (!c) return;
          const known = lowestLoadedAbove("decision_window");
          if (known !== null) {
            display(known);
            return;
          }
          if (c.openWindowTick === c.head || c.head >= c.horizon) {
            if (c.head > c.selected) display(c.head);
            return;
          }
          const r = await advance(c.branchId, undefined);
          if (r) await showLoaded(c.branchId, r.headTick);
        });
        settle(studio.getState().activeBranchId ?? "");
      },

      async fastForwardToDisruptionOnset() {
        pauseWith("user");
        await exclusive(async () => {
          const c = ctx();
          if (!c) return;
          const known = lowestLoadedAbove("disruption_onset");
          if (known !== null) {
            display(known);
            return;
          }
          if (c.head >= c.horizon) {
            if (c.head > c.selected) display(c.head);
            return;
          }
          const r = await advance(c.branchId, c.horizon);
          if (r) await showLoaded(c.branchId, r.headTick);
        });
        settle(studio.getState().activeBranchId ?? "");
      },

      async selectTick(tick) {
        pauseWith("user");
        const c = ctx();
        if (!c) return;
        const clamped = Math.max(0, Math.min(tick, c.head));
        await exclusive(async () => {
          await showLoaded(c.branchId, clamped);
        });
        settle(c.branchId);
      },
    };
  });

  return store;
}

export type PlaybackStore = ReturnType<typeof createPlaybackStore>;

export const usePlaybackStore = createPlaybackStore();
