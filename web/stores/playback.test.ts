import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/lib/api/client";
import type { AdvanceRequest, AdvanceResult, Branch, TickFrame } from "@/lib/api/client";
import { createPlaybackStore, defaultScheduler, type PlaybackClient, type Scheduler } from "./playback";
import { createStudioStore } from "./studio";

const BRANCH = "b1";

type MarkerKind = "decision_window" | "disruption_onset";

function frame(tick: number, kinds: MarkerKind[] = []): TickFrame {
  return {
    tick,
    stateHash: `h${tick}`,
    markers: kinds.map((kind) => ({ kind })),
  } as unknown as TickFrame;
}

function branch(headTick: number): Branch {
  return {
    id: BRANCH,
    worldId: "w1",
    parentBranchId: null,
    forkTick: 0,
    headTick,
    label: "root",
    managerKind: "human",
    createdAt: "2026-01-01T00:00:00Z",
  } as Branch;
}

function advanceResult(headTick: number, frames: TickFrame[], paused: AdvanceResult["paused"]): AdvanceResult {
  return { headTick, stateHash: `h${headTick}`, paused, windowReasons: [], frames } as unknown as AdvanceResult;
}

const HORIZON = 12;
// Windows at 4, 8, 12; disruption onset at 6 (which is also a window).
const MARKERS: Record<number, MarkerKind[]> = {
  4: ["decision_window"],
  6: ["decision_window", "disruption_onset"],
  8: ["decision_window"],
  12: ["decision_window"],
};

function frames(from: number, to: number): TickFrame[] {
  const out: TickFrame[] = [];
  for (let t = from; t <= to; t++) out.push(frame(t, MARKERS[t] ?? []));
  return out;
}

function makeClient(overrides: Partial<PlaybackClient> = {}) {
  const advanceBranch = vi.fn<PlaybackClient["advanceBranch"]>(async (_id, body) => {
    const target = body?.runToTick ?? 4;
    return advanceResult(target, frames(1, target), "run_to_tick");
  });
  const client: PlaybackClient = {
    advanceBranch,
    listTickFrames: vi.fn(async (_id, range) => ({ branchId: BRANCH, frames: frames(range.from, range.to) })) as never,
    getBranch: vi.fn(async () => branch(HORIZON)) as never,
    getBranchState: vi.fn(async () => ({ tick: 0 })) as never,
    getBranchMetrics: vi.fn(async () => ({ tick: 0 })) as never,
    ...overrides,
  };
  return { client, advanceBranch };
}

function setup(opts: { head: number; loaded: [number, number]; client?: Partial<PlaybackClient>; selected?: number }) {
  const studio = createStudioStore();
  studio.getState().openWorld({
    worldId: "w1",
    horizonTicks: HORIZON,
    branches: [branch(opts.head)],
    activeBranchId: BRANCH,
    selectedTick: opts.selected ?? 0,
  });
  studio.getState().addFrames(BRANCH, frames(opts.loaded[0], opts.loaded[1]));
  const intervals: number[] = [];
  const scheduler: Scheduler = {
    setInterval: (fn, ms) => {
      intervals.push(ms);
      return defaultScheduler.setInterval(fn, ms);
    },
    clearInterval: (h) => defaultScheduler.clearInterval(h),
  };
  const made = makeClient(opts.client);
  const playback = createPlaybackStore({ client: made.client, studio, scheduler });
  return { studio, playback, intervals, ...made };
}

beforeEach(() => {
  vi.useFakeTimers();
});
afterEach(() => {
  vi.useRealTimers();
});

describe("playback controller", () => {
  it("never displays a tick whose frame is not loaded, and holds while syncing", async () => {
    let release: (r: AdvanceResult) => void = () => undefined;
    const pending = new Promise<AdvanceResult>((res) => {
      release = res;
    });
    const t = setup({
      head: 3,
      loaded: [0, 3],
      client: { advanceBranch: vi.fn(() => pending) as never },
    });
    const seen: number[] = [];
    t.studio.subscribe((s, prev) => {
      if (s.selectedTick !== prev.selectedTick) {
        expect(s.framesByBranch[BRANCH]?.[s.selectedTick]).toBeDefined();
        seen.push(s.selectedTick);
      }
    });
    t.studio.getState().setSelectedTick(3);
    t.playback.getState().play(1);
    await vi.advanceTimersByTimeAsync(500);
    expect(t.studio.getState().selectedTick).toBe(3);
    expect(t.studio.getState().syncing).toBe(true);
    await vi.advanceTimersByTimeAsync(1500);
    expect(t.studio.getState().selectedTick).toBe(3);

    release(advanceResult(4, frames(4, 4), "decision_window"));
    await vi.advanceTimersByTimeAsync(500);
    expect(t.studio.getState().syncing).toBe(false);
    await vi.advanceTimersByTimeAsync(500);
    expect(t.studio.getState().selectedTick).toBe(4);
    expect(seen).toEqual([3, 4]);
  });

  it("pauses at every decision window by default", async () => {
    const t = setup({ head: 12, loaded: [0, 12] });
    expect(t.playback.getState().autoPauseMode).toBe("every_decision_window");
    t.playback.getState().play(16);
    await vi.advanceTimersByTimeAsync(2000);
    expect(t.studio.getState().selectedTick).toBe(4);
    expect(t.playback.getState().playing).toBe(false);
    expect(t.playback.getState().pauseReason).toBe("decision_window");
  });

  it("pauses only at disruption onsets in onsets-only mode, and at the horizon", async () => {
    const t = setup({ head: 12, loaded: [0, 12] });
    t.playback.getState().setAutoPauseMode("disruption_onsets_only");
    t.playback.getState().play(16);
    await vi.advanceTimersByTimeAsync(2000);
    expect(t.studio.getState().selectedTick).toBe(6);
    expect(t.playback.getState().pauseReason).toBe("disruption_onset");
    t.playback.getState().play();
    await vi.advanceTimersByTimeAsync(2000);
    expect(t.studio.getState().selectedTick).toBe(12);
    expect(t.playback.getState().pauseReason).toBe("horizon");
  });

  it("requests advancement at the head with runToTick horizon in onsets-only mode", async () => {
    const t = setup({ head: 3, loaded: [0, 3], selected: 3 });
    t.playback.getState().setAutoPauseMode("disruption_onsets_only");
    t.playback.getState().play(16);
    await vi.advanceTimersByTimeAsync(100);
    expect(t.advanceBranch).toHaveBeenCalledWith(BRANCH, { expectedHeadTick: 3, runToTick: HORIZON });
  });

  it("step back never sends an advance request and pauses", async () => {
    const t = setup({ head: 3, loaded: [0, 3], selected: 3 });
    t.playback.getState().play(4);
    await t.playback.getState().stepBack();
    expect(t.studio.getState().selectedTick).toBe(2);
    expect(t.playback.getState().playing).toBe(false);
    await t.playback.getState().stepBack();
    await t.playback.getState().selectTick(0);
    expect(t.studio.getState().selectedTick).toBe(0);
    expect(t.advanceBranch).not.toHaveBeenCalled();
    expect(t.studio.getState().branches[0]?.headTick).toBe(3);
  });

  it("loads a missing frame below the head when stepping back", async () => {
    const t = setup({ head: 6, loaded: [5, 6], selected: 5 });
    await t.playback.getState().stepBack();
    expect(t.studio.getState().selectedTick).toBe(4);
    expect(t.advanceBranch).not.toHaveBeenCalled();
  });

  it("speed changes only change the interval, not what is requested", async () => {
    const t = setup({ head: 12, loaded: [0, 12] });
    t.playback.getState().setAutoPauseMode("disruption_onsets_only");
    t.playback.getState().play(1);
    await vi.advanceTimersByTimeAsync(1000);
    expect(t.studio.getState().selectedTick).toBe(2);
    t.playback.getState().setSpeed(16);
    await vi.advanceTimersByTimeAsync(125);
    expect(t.studio.getState().selectedTick).toBe(6);
    expect(t.intervals).toEqual([500, 31.25]);
    expect(t.advanceBranch).not.toHaveBeenCalled();
  });

  it("fast-forward to a disruption onset uses runToTick = horizon when none is computed", async () => {
    const t = setup({ head: 3, loaded: [0, 3], selected: 3 });
    await t.playback.getState().fastForwardToDisruptionOnset();
    expect(t.advanceBranch).toHaveBeenCalledTimes(1);
    expect(t.advanceBranch).toHaveBeenCalledWith(BRANCH, { expectedHeadTick: 3, runToTick: HORIZON });
    expect(t.studio.getState().selectedTick).toBe(HORIZON);
    expect(t.playback.getState().playing).toBe(false);
  });

  it("fast-forward uses computed markers without a request, else advances without runToTick", async () => {
    const t = setup({ head: 12, loaded: [0, 12] });
    await t.playback.getState().fastForwardToDecisionWindow();
    expect(t.studio.getState().selectedTick).toBe(4);
    await t.playback.getState().fastForwardToDisruptionOnset();
    expect(t.studio.getState().selectedTick).toBe(6);
    expect(t.advanceBranch).not.toHaveBeenCalled();

    const u = setup({ head: 3, loaded: [0, 3], selected: 3 });
    await u.playback.getState().fastForwardToDecisionWindow();
    const body = u.advanceBranch.mock.calls[0]?.[1] as AdvanceRequest;
    expect(body).toEqual({ expectedHeadTick: 3 });
    expect("runToTick" in body).toBe(false);
    expect(u.studio.getState().selectedTick).toBe(4);
  });

  it("refetches the head after a stale-head conflict", async () => {
    const stale = new ApiError({ status: 409, type: "stale-head", title: "Stale head", expectedHeadTick: 3, actualHeadTick: 5 });
    const getBranch = vi.fn(async () => branch(5));
    const t = setup({
      head: 3,
      loaded: [0, 3],
      selected: 3,
      client: { advanceBranch: vi.fn(async () => Promise.reject(stale)) as never, getBranch: getBranch as never },
    });
    t.playback.getState().play(16);
    await vi.advanceTimersByTimeAsync(100);
    expect(getBranch).toHaveBeenCalledTimes(1);
    expect(t.studio.getState().branches[0]?.headTick).toBe(5);
    expect(t.studio.getState().framesByBranch[BRANCH]?.[5]).toBeDefined();
  });
});

describe("progress stream watching", () => {
  it("adds streamed frames to the studio, resumes after loaded frames and stops on unwatch", () => {
    const { studio } = setup({ head: 4, loaded: [0, 4] });
    const close = vi.fn();
    let opts: Parameters<typeof import("@/lib/api/progress").openProgress>[1] | undefined;
    const progress = vi.fn((_id: string, o: NonNullable<typeof opts>) => {
      opts = o;
      return { close, status: "live" as const, lastTick: 4 };
    });
    const playback = createPlaybackStore({ studio, progress: progress as never });
    const stop = playback.getState().watchProgress();
    expect(progress.mock.calls[0]?.[0]).toBe(BRANCH);
    expect(opts?.fromTick).toBe(5);
    opts?.onStatus?.("polling");
    expect(playback.getState().progressStatus).toBe("polling");
    opts?.onFrame(frame(5));
    expect(studio.getState().framesByBranch[BRANCH]?.[5]?.tick).toBe(5);
    expect(studio.getState().branches[0]?.headTick).toBe(5);
    stop();
    expect(close).toHaveBeenCalled();
    expect(playback.getState().progressStatus).toBeNull();
  });
});
