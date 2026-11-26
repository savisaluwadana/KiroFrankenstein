"use client";

/**
 * The world page: HUD and playback on top, Branch_Tree on the left, diorama in
 * the centre, context panel on the right and Timeline at the bottom
 * (ui-design-system.md). Every figure comes from an API response; this page
 * only wires the feature components to the studio and playback stores.
 */
import Link from "next/link";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  getBranchMetrics,
  getBranchState,
  getWorld,
  listBranches,
  listTickFrames,
  type AcceptedCommand,
  type Branch,
  type DemoPlan,
  type MetricsResponse,
  type Observation,
  type SubmitCommandsResult,
  type World,
} from "@/lib/api/client";
import { BranchTree } from "@/features/branches/BranchTree";
import { ForkButton } from "@/features/branches/ForkButton";
import { DecisionPanel } from "@/features/decisions/DecisionPanel";
import { ConnectedTimeline, type TimelineInput } from "@/features/timeline/Timeline";
import { DISCLAIMER_TEXT, Hud } from "@/features/world-view/Hud";
import { PlaybackControls, usePlaybackKeybindings } from "@/features/world-view/PlaybackControls";
import { maxOnHandSeen, type Intervention } from "@/features/world-view/scene/worldViewModel";
import { WorldView } from "@/features/world-view/WorldView";
import { usePlaybackStore } from "@/stores/playback";
import { selectActiveBranch, selectDisplayedFrame, useStudioStore } from "@/stores/studio";
import { loadDemoPlan } from "./demoPlanStorage";

/** Largest frame range the API serves per request. */
const FRAME_CHUNK = 240;

function messageOf(e: unknown): string {
  return e instanceof Error ? e.message : "Request failed";
}

/** Loads every frame of a Branch from Tick 0 through `head` that is not loaded yet. */
async function ensureFrames(branchId: string, head: number): Promise<void> {
  const have = useStudioStore.getState().framesByBranch[branchId] ?? {};
  let missing = false;
  for (let t = 0; t <= head; t++) {
    if (!have[t]) {
      missing = true;
      break;
    }
  }
  if (!missing) return;
  for (let from = 0; from <= head; from += FRAME_CHUNK) {
    const to = Math.min(head, from + FRAME_CHUNK - 1);
    const res = await listTickFrames(branchId, { from, to });
    useStudioStore.getState().addFrames(branchId, res.frames);
  }
}

function describeCommand(a: AcceptedCommand): string {
  const c = a.command;
  const parts: string[] = [c.kind];
  if (c.sku !== undefined) parts.push(`SKU ${c.sku}`);
  if (c.qty !== undefined) parts.push(`${c.qty} units`);
  if (c.role !== undefined) parts.push(c.role);
  return parts.join(", ");
}

function AssumptionsPanel({ world }: { world: World }) {
  return (
    <section aria-label="Assumptions" className="space-y-3 text-sm text-[#E6E9EF]">
      <h2 className="text-base font-medium">Assumptions</h2>
      <p className="text-xs text-[#8A93A6]">
        Effective values for this world, with the allowed range and the rationale. Money is in cents and rates are in
        basis points unless the unit says otherwise.
      </p>
      <ul className="space-y-2">
        {world.assumptions.map((a) => (
          <li key={a.path} className="border-t border-[#232B3A] pt-2" data-testid="assumption">
            <div className="font-mono text-xs text-[#E6E9EF]">{a.path}</div>
            <div className="font-mono text-xs tabular-nums">
              {a.value} {a.unit}
              {a.overridden ? ` (default ${a.default})` : ""} · range {a.min} to {a.max}
            </div>
            <div className="text-xs text-[#8A93A6]">{a.rationale}</div>
          </li>
        ))}
      </ul>
      <p className="text-xs text-[#8A93A6]">{DISCLAIMER_TEXT}</p>
    </section>
  );
}

function PlanHint({ plan, onGoToFork }: { plan: DemoPlan; onGoToFork: () => void }) {
  return (
    <section
      aria-label="Demo plan hint"
      className="space-y-1 rounded-[4px] border border-[#232B3A] bg-[#0B0E14] p-2 text-xs"
      data-testid="plan-hint"
    >
      <h3 className="font-medium text-[#E6E9EF]">Demo plan hint</h3>
      <p className="text-[#8A93A6]">
        A suggestion computed by the server from the no-command run. It is not a guaranteed good decision.
      </p>
      <p className="font-mono tabular-nums text-[#E6E9EF]">Fork at tick {plan.forkTick}</p>
      <ul className="font-mono tabular-nums text-[#E6E9EF]">
        {plan.commands.map((c, i) => (
          <li key={i}>
            Order {c.qty} units of {c.skuId} from the {c.role} supplier
          </li>
        ))}
      </ul>
      <button
        type="button"
        onClick={onGoToFork}
        className="rounded-[4px] border border-[#232B3A] px-2 py-1 text-[#E6E9EF] hover:border-[#F2A93B] focus-visible:outline focus-visible:outline-2 focus-visible:outline-[#E6E9EF]"
      >
        Show tick {plan.forkTick}
      </button>
      <details>
        <summary className="cursor-pointer text-[#8A93A6]">How the server derived it</summary>
        <ul className="list-disc pl-4 text-[#8A93A6]">
          {plan.reasons.map((r, i) => (
            <li key={i}>{r}</li>
          ))}
        </ul>
      </details>
    </section>
  );
}

interface HeadObservation {
  branchId: string;
  tick: number;
  observation: Observation;
}

export interface StudioPageProps {
  worldId: string;
  /** Branch to open; the root Branch when absent or unknown. */
  branchId?: string | null;
}

export function StudioPage({ worldId, branchId }: StudioPageProps) {
  const [world, setWorld] = useState<World | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [tab, setTab] = useState<"context" | "assumptions">("context");
  const [headObs, setHeadObs] = useState<HeadObservation | null>(null);
  const [parentMetrics, setParentMetrics] = useState<MetricsResponse | null>(null);
  const [accepted, setAccepted] = useState<Record<string, AcceptedCommand[]>>({});
  const [plan] = useState<DemoPlan | null>(() => (typeof window === "undefined" ? null : loadDemoPlan(worldId)));
  const endedWindows = useRef<Set<string>>(new Set());
  const initialBranchId = useRef(branchId ?? null);

  usePlaybackKeybindings();

  const active = useStudioStore(selectActiveBranch);
  const branches = useStudioStore((s) => s.branches);
  const activeBranchId = useStudioStore((s) => s.activeBranchId);
  const selectedTick = useStudioStore((s) => s.selectedTick);
  const horizonTicks = useStudioStore((s) => s.horizonTicks);
  const syncing = useStudioStore((s) => s.syncing);
  const metrics = useStudioStore((s) => s.metrics);
  const openWindowTick = useStudioStore((s) => s.openWindowTick);
  const error = useStudioStore((s) => s.error);
  const frame = useStudioStore(selectDisplayedFrame);
  const frames = useStudioStore((s) => (s.activeBranchId === null ? undefined : s.framesByBranch[s.activeBranchId]));

  // Open the world: load it, open the studio store, reset playback, load the displayed frames.
  useEffect(() => {
    let cancelled = false;
    const studio = useStudioStore.getState();
    const playback = usePlaybackStore.getState();
    void (async () => {
      try {
        const [w, list] = await Promise.all([getWorld(worldId), listBranches(worldId)]);
        if (cancelled) return;
        const root = list.branches.find((b) => b.parentBranchId === null) ?? list.branches[0];
        const chosen = list.branches.find((b) => b.id === initialBranchId.current) ?? root;
        if (!chosen) throw new Error("This world has no branches.");
        studio.openWorld({
          worldId,
          horizonTicks: w.scenario.horizonTicks,
          branches: list.branches,
          activeBranchId: chosen.id,
          selectedTick: chosen.headTick,
        });
        playback.resetForWorld();
        await ensureFrames(chosen.id, chosen.headTick);
        if (cancelled) return;
        setWorld(w);
        setLoading(false);
        await usePlaybackStore.getState().selectTick(chosen.headTick);
      } catch (e) {
        if (cancelled) return;
        setLoadError(messageOf(e));
        setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
      usePlaybackStore.getState().pause();
      useStudioStore.getState().reset();
    };
  }, [worldId, attempt]);

  // Observation at the head of the active Branch: needed by the Decision_Panel and to detect an open window.
  const headTick = active?.headTick;
  useEffect(() => {
    if (activeBranchId === null || headTick === undefined || world === null) return;
    let cancelled = false;
    getBranchState(activeBranchId, headTick)
      .then((st) => {
        if (cancelled) return;
        setHeadObs({ branchId: activeBranchId, tick: headTick, observation: st.observation });
        const s = useStudioStore.getState();
        if (st.observation.window.open && !endedWindows.current.has(`${activeBranchId}:${headTick}`)) {
          if (s.activeBranchId === activeBranchId) s.setOpenWindowTick(headTick);
        }
      })
      .catch((e: unknown) => {
        if (!cancelled) useStudioStore.getState().setError(messageOf(e));
      });
    return () => {
      cancelled = true;
    };
  }, [activeBranchId, headTick, world]);

  // Parent Metrics at the displayed Tick, for forked Branches.
  const parentId = active?.parentBranchId ?? null;
  const metricsTick = metrics?.tick;
  useEffect(() => {
    if (parentId === null || metricsTick === undefined) return;
    let cancelled = false;
    getBranchMetrics(parentId, metricsTick)
      .then((m) => {
        if (!cancelled) setParentMetrics(m);
      })
      .catch(() => {
        if (!cancelled) setParentMetrics(null);
      });
    return () => {
      cancelled = true;
    };
  }, [parentId, metricsTick]);

  const activate = useCallback(async (id: string) => {
    const s = useStudioStore.getState();
    const target = s.branches.find((b) => b.id === id);
    if (!target) return;
    try {
      await ensureFrames(id, target.headTick);
    } catch (e) {
      useStudioStore.getState().setError(messageOf(e));
      return;
    }
    const st = useStudioStore.getState();
    st.setActiveBranch(id);
    window.history.replaceState(null, "", `?branch=${encodeURIComponent(id)}`);
    await usePlaybackStore.getState().selectTick(Math.min(st.selectedTick, target.headTick));
  }, []);

  const onForked = useCallback(async (child: Branch) => {
    const s = useStudioStore.getState();
    s.upsertBranch(child);
    try {
      await ensureFrames(child.id, child.headTick);
    } catch (e) {
      useStudioStore.getState().setError(messageOf(e));
      return;
    }
    const st = useStudioStore.getState();
    st.setActiveBranch(child.id);
    st.setSelectedTick(child.headTick);
    st.setOpenWindowTick(child.headTick);
    window.history.replaceState(null, "", `?branch=${encodeURIComponent(child.id)}`);
    setTab("context");
    await usePlaybackStore.getState().selectTick(child.headTick);
  }, []);

  const onSubmitted = useCallback(
    (result: SubmitCommandsResult) => {
      if (activeBranchId === null) return;
      setAccepted((prev) => ({ ...prev, [activeBranchId]: [...(prev[activeBranchId] ?? []), ...result.accepted] }));
    },
    [activeBranchId],
  );

  const onWindowEnded = useCallback(() => {
    const s = useStudioStore.getState();
    if (s.activeBranchId !== null && s.openWindowTick !== null) {
      endedWindows.current.add(`${s.activeBranchId}:${s.openWindowTick}`);
    }
    s.setOpenWindowTick(null);
  }, []);

  const acceptedHere = activeBranchId !== null ? (accepted[activeBranchId] ?? []) : [];
  const inputs: TimelineInput[] = acceptedHere.map((a) => ({
    effectiveTick: a.effectiveTick,
    provenance: a.provenance,
    kind: a.command.kind,
  }));
  const interventions: Intervention[] = acceptedHere.map((a) => ({
    tick: a.effectiveTick,
    text: describeCommand(a),
    manager: "human",
  }));

  const stockScaleMax = useMemo(() => {
    if (frames === undefined) return 0;
    return maxOnHandSeen(Object.values(frames).filter((f) => f.tick <= selectedTick));
  }, [frames, selectedTick]);

  if (loading) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-[#0B0E14] text-[#8A93A6]">
        <p role="status" className="font-mono text-sm">
          Loading world…
        </p>
      </main>
    );
  }
  if (loadError !== null || world === null || active === undefined) {
    return (
      <main className="flex min-h-screen flex-col items-center justify-center gap-3 bg-[#0B0E14] text-[#E6E9EF]">
        <p role="alert" className="max-w-xl text-sm text-[#FF7A7A]">
          {loadError ?? "The world could not be opened."}
        </p>
        <div className="flex gap-2 text-sm">
          <button
            type="button"
            className="rounded-[4px] border border-[#232B3A] px-3 py-1 hover:border-[#F2A93B] focus-visible:outline focus-visible:outline-2 focus-visible:outline-[#E6E9EF]"
            onClick={() => {
              setLoadError(null);
              setLoading(true);
              setAttempt((n) => n + 1);
            }}
          >
            Retry
          </button>
          <Link
            href="/"
            className="rounded-[4px] border border-[#232B3A] px-3 py-1 hover:border-[#F2A93B] focus-visible:outline focus-visible:outline-2 focus-visible:outline-[#E6E9EF]"
          >
            Back to start
          </Link>
        </div>
      </main>
    );
  }

  const windowOpenHere = openWindowTick !== null && openWindowTick === active.headTick;
  const observation = headObs !== null && headObs.branchId === active.id && headObs.tick === active.headTick ? headObs.observation : null;
  const atHead = selectedTick === active.headTick;
  const tabBtn = (id: "context" | "assumptions", label: string) => (
    <button
      type="button"
      role="tab"
      aria-selected={tab === id}
      onClick={() => setTab(id)}
      className={`rounded-[4px] border px-2 py-1 text-xs focus-visible:outline focus-visible:outline-2 focus-visible:outline-[#E6E9EF] ${
        tab === id ? "border-[#F2A93B] text-[#F2A93B]" : "border-[#232B3A] text-[#8A93A6]"
      }`}
    >
      {label}
    </button>
  );

  return (
    <main className="flex min-h-screen flex-col gap-2 bg-[#0B0E14] p-2 font-sans text-[#E6E9EF]">
      <header className="flex flex-col gap-2">
        <div className="flex items-baseline justify-between gap-4">
          <h1 className="text-sm">
            <Link href="/" className="text-[#8A93A6] hover:text-[#E6E9EF]">
              Parallel
            </Link>{" "}
            <span className="text-[#E6E9EF]">{world.name}</span>{" "}
            <span className="text-[#8A93A6]">· {active.label}</span>
          </h1>
          <span className="font-mono text-xs text-[#8A93A6]" aria-live="polite" data-testid="syncing-indicator">
            {syncing ? "syncing" : ""}
          </span>
        </div>
        <Hud metrics={metrics} parentMetrics={parentId !== null ? parentMetrics : null} {...(parentId !== null ? { parentLabel: branches.find((b) => b.id === parentId)?.label ?? "parent" } : {})} syncing={syncing} tick={selectedTick} />
        <PlaybackControls />
        {error !== null ? (
          <div role="alert" className="flex items-center justify-between rounded-[4px] border border-[#FF5C5C] px-3 py-1 text-xs text-[#FF7A7A]">
            <span>{error}</span>
            <button
              type="button"
              className="underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-[#E6E9EF]"
              onClick={() => useStudioStore.getState().setError(null)}
            >
              Dismiss
            </button>
          </div>
        ) : null}
      </header>

      <div className="grid flex-1 grid-cols-1 gap-2 lg:grid-cols-[18rem_minmax(0,1fr)_24rem]">
        <aside aria-label="Branches" className="flex flex-col gap-2">
          <div className="h-72 rounded-[4px] border border-[#232B3A] bg-[#121722]">
            <BranchTree branches={branches} activeBranchId={activeBranchId} onSelect={(id) => void activate(id)} />
          </div>
          <ForkButton
            branchId={active.id}
            tick={selectedTick}
            headTick={active.headTick}
            onForked={(b) => void onForked(b)}
          />
        </aside>

        <section aria-label="World view" className="flex flex-col gap-2">
          <WorldView
            frame={frame ?? null}
            syncing={syncing}
            interventions={interventions}
            {...(stockScaleMax > 0 ? { stockScaleMax } : {})}
          />
          <p className="text-xs text-[#8A93A6]" data-testid="world-disclaimer">
            {DISCLAIMER_TEXT}
          </p>
        </section>

        <aside aria-label="Context panel" className="flex flex-col gap-2 rounded-[4px] border border-[#232B3A] bg-[#121722] p-3">
          <div role="tablist" aria-label="Context panel" className="flex gap-1">
            {tabBtn("context", "Decisions")}
            {tabBtn("assumptions", "Assumptions")}
          </div>
          {tab === "assumptions" ? (
            <AssumptionsPanel world={world} />
          ) : (
            <>
              {plan !== null ? (
                <PlanHint
                  plan={plan}
                  onGoToFork={() => void usePlaybackStore.getState().selectTick(plan.forkTick)}
                />
              ) : null}
              {windowOpenHere && observation !== null && atHead ? (
                <DecisionPanel
                  key={`${active.id}:${active.headTick}`}
                  observation={observation}
                  windowTick={active.headTick}
                  branchId={active.id}
                  budgetRemaining={observation.window.commandBudget}
                  onSubmitted={onSubmitted}
                  onWindowEnded={onWindowEnded}
                />
              ) : windowOpenHere ? (
                <p className="text-sm text-[#8A93A6]" data-testid="window-elsewhere">
                  A decision window is open at tick {active.headTick}. Show that tick to act.
                </p>
              ) : (
                <p className="text-sm text-[#8A93A6]" data-testid="no-window">
                  No decision window is open on this branch. Play to the next window, or fork to decide differently.
                </p>
              )}
            </>
          )}
        </aside>
      </div>

      <ConnectedTimeline inputs={inputs} />
      <p className="text-center font-mono text-[10px] text-[#8A93A6]">
        Horizon {horizonTicks} ticks · inputs shown are those submitted in this session
      </p>
    </main>
  );
}
