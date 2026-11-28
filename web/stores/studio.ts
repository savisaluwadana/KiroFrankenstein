/**
 * Studio view-state store. Holds which World and Branch are open and the
 * authoritative server responses the views display. It never computes a
 * simulation number: frames, state and metrics are stored exactly as received.
 */
import { create } from "zustand";
import type { Branch, BranchState, MetricsResponse, TickFrame } from "@/lib/api/client";
import { onWriteTokenRejected } from "@/lib/api/writeToken";

export type FramesByTick = Record<number, TickFrame>;

export interface OpenWorldInit {
  worldId: string;
  horizonTicks: number;
  branches: Branch[];
  activeBranchId: string;
  selectedTick?: number;
}

export interface StudioState {
  worldId: string | null;
  horizonTicks: number;
  activeBranchId: string | null;
  branches: Branch[];
  /** Tick_Frames received from the API, by Branch id and then Tick. */
  framesByBranch: Record<string, FramesByTick>;
  /** The Tick being displayed. Always a Tick whose frame is loaded, once frames exist. */
  selectedTick: number;
  /** Latest State response for `selectedTick` of the active Branch, if fetched. */
  branchState: BranchState | null;
  /** Latest Metrics response for `selectedTick` of the active Branch, if fetched. */
  metrics: MetricsResponse | null;
  /** True while data for a requested Tick is loading (show the previous Tick dimmed). */
  syncing: boolean;
  /** Tick at which the active Branch has an open Decision_Window, or null. */
  openWindowTick: number | null;
  error: string | null;
  /** True once any response showed the world was recorded under another engine version. */
  engineMismatch: boolean;
  /** True when this tab has no write token for the world (shared link) or the server refused it. */
  sharedReadOnly: boolean;

  openWorld(init: OpenWorldInit): void;
  setBranches(branches: Branch[]): void;
  upsertBranch(branch: Branch): void;
  setBranchHead(branchId: string, headTick: number): void;
  setActiveBranch(branchId: string): void;
  addFrames(branchId: string, frames: TickFrame[]): void;
  setSelectedTick(tick: number): void;
  setBranchState(state: BranchState | null): void;
  setMetrics(metrics: MetricsResponse | null): void;
  setSyncing(syncing: boolean): void;
  setOpenWindowTick(tick: number | null): void;
  setError(message: string | null): void;
  setEngineMismatch(mismatch: boolean): void;
  setSharedReadOnly(readOnly: boolean): void;
  reset(): void;
}

const INITIAL = {
  worldId: null,
  horizonTicks: 0,
  activeBranchId: null,
  branches: [] as Branch[],
  framesByBranch: {} as Record<string, FramesByTick>,
  selectedTick: 0,
  branchState: null,
  metrics: null,
  syncing: false,
  openWindowTick: null,
  error: null,
  engineMismatch: false,
  sharedReadOnly: false,
};

export function createStudioStore() {
  return create<StudioState>()((set, get) => ({
    ...INITIAL,

    openWorld(init) {
      set({
        ...INITIAL,
        framesByBranch: {},
        worldId: init.worldId,
        horizonTicks: init.horizonTicks,
        branches: init.branches,
        activeBranchId: init.activeBranchId,
        selectedTick: init.selectedTick ?? 0,
      });
    },
    setBranches(branches) {
      set({ branches });
    },
    upsertBranch(branch) {
      const rest = get().branches.filter((b) => b.id !== branch.id);
      set({ branches: [...rest, branch] });
    },
    setBranchHead(branchId, headTick) {
      set({ branches: get().branches.map((b) => (b.id === branchId ? { ...b, headTick } : b)) });
    },
    setActiveBranch(branchId) {
      const branch = get().branches.find((b) => b.id === branchId);
      const selected = branch ? Math.min(get().selectedTick, branch.headTick) : get().selectedTick;
      set({
        activeBranchId: branchId,
        selectedTick: selected,
        branchState: null,
        metrics: null,
        openWindowTick: null,
        syncing: false,
      });
    },
    addFrames(branchId, frames) {
      const current = get().framesByBranch[branchId] ?? {};
      const next: FramesByTick = { ...current };
      for (const f of frames) next[f.tick] = f;
      set({ framesByBranch: { ...get().framesByBranch, [branchId]: next } });
    },
    setSelectedTick(tick) {
      set({ selectedTick: tick });
    },
    setBranchState(branchState) {
      set({ branchState });
    },
    setMetrics(metrics) {
      set({ metrics });
    },
    setSyncing(syncing) {
      set({ syncing });
    },
    setOpenWindowTick(openWindowTick) {
      set({ openWindowTick });
    },
    setError(error) {
      set({ error });
    },
    setEngineMismatch(engineMismatch) {
      set({ engineMismatch });
    },
    setSharedReadOnly(sharedReadOnly) {
      set({ sharedReadOnly });
    },
    reset() {
      set({ ...INITIAL, framesByBranch: {} });
    },
  }));
}

export const useStudioStore = createStudioStore();

// A refused write token (401/403 from any mutation) switches the open world to read-only.
onWriteTokenRejected(() => useStudioStore.getState().setSharedReadOnly(true));

export type StudioStore = ReturnType<typeof createStudioStore>;

/** The active Branch, or undefined. */
export function selectActiveBranch(s: StudioState): Branch | undefined {
  return s.branches.find((b) => b.id === s.activeBranchId);
}

/** The loaded frame of the active Branch at `tick`, or undefined. */
export function selectFrameAt(s: StudioState, tick: number): TickFrame | undefined {
  if (s.activeBranchId === null) return undefined;
  return s.framesByBranch[s.activeBranchId]?.[tick];
}

/** The frame at the selected Tick, or undefined when it is not loaded. */
export function selectDisplayedFrame(s: StudioState): TickFrame | undefined {
  return selectFrameAt(s, s.selectedTick);
}
