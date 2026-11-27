"use client";

/**
 * The studio. The warehouse is the page: a compact header of plain-language
 * figures on top, the isometric warehouse in the centre, a rail of crisis and
 * decision cards on the right (a bottom sheet on phones), and a time strip
 * with playback controls below. Every figure comes from an API response; this
 * page only wires the feature components to the studio and playback stores.
 *
 * `StagePane` renders one reality's warehouse; the comparison view renders two
 * of them side by side in its place, keeping the header, rail and one shared
 * time strip and playback.
 */
import Link from "next/link";
import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from "react";
import { ChevronDown, ChevronUp } from "lucide-react";
import {
  ApiError,
  advanceBranch,
  getBranch,
  getBranchMetrics,
  getBranchState,
  getWorld,
  hasEngineMismatch,
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
import { ForkButton } from "@/features/branches/ForkButton";
import { ForkSequence } from "@/features/branches/ForkSequence";
import {
  INITIAL_FORK_SEQUENCE,
  forkSequenceReducer,
  prefersReducedMotion,
} from "@/features/branches/forkSequenceMachine";
import { RealityBadge, laneFor, laneIndexOf, orderBranches, realityName } from "@/features/branches/lanes";
import { RealitySwitcher } from "@/features/branches/RealitySwitcher";
import { InjectDisruption } from "@/features/decisions/InjectDisruption";
import { DecisionPanel, type Prefill, type Suggestion } from "@/features/decisions/DecisionPanel";
import { EngineeringDrawer } from "@/features/engineering/EngineeringDrawer";
import { EngineeringToggle } from "@/features/engineering/EngineeringToggle";
import { EngineVersionBanner } from "./EngineVersionBanner";
import { useInjectedInputs } from "./injectedInputs";
import { ConnectedTimeline, type TimelineInput } from "@/features/timeline/Timeline";
import { Hud, type MoneyFigure } from "@/features/world-view/Hud";
import { PlaybackControls, usePlaybackKeybindings } from "@/features/world-view/PlaybackControls";
import type { Intervention } from "@/features/world-view/scene/worldViewModel";
import {
  DISCLAIMER_FULL,
  TERMS,
  formatCount,
  formatMoney,
  formatSimTime,
  productName,
  supplierRoleName,
} from "@/lib/glossary";
import { usePlaybackStore } from "@/stores/playback";
import { selectActiveBranch, selectDisplayedFrame, useStudioStore } from "@/stores/studio";
import { useUiStore } from "@/stores/ui";
import { AssumptionsPanel } from "./AssumptionsPanel";
import { CrisisCards } from "./CrisisCards";
import { deriveCrisisCards, type CrisisAction } from "./crisis";
import { LedgerPanel } from "./LedgerPanel";
import { loadDemoPlan } from "./demoPlanStorage";
import { advanceRequestFor, defaultPartnerId, divergenceTick } from "./comparison";
import { ComparisonView } from "./ComparisonView";
import { StagePane } from "./StagePane";

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

/** One accepted move as a sentence. Never shows product indexes. */
function describeCommand(a: AcceptedCommand, names: readonly string[]): string {
  const c = a.command;
  const product = c.sku !== undefined ? (names[c.sku] ?? "a product") : "";
  if (c.kind === "place_purchase_order") {
    return `Ordered ${formatCount(c.qty ?? 0)} ${product} from the ${supplierRoleName(c.role ?? "primary").toLowerCase()}`;
  }
  if (c.kind === "set_price") return `Changed ${product} price to ${formatMoney(c.amountCents ?? 0)}`;
  return "A decision";
}

interface HeadObservation {
  branchId: string;
  tick: number;
  observation: Observation;
}

type RailTab = "moves" | "alerts" | "model";

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
  const [tab, setTab] = useState<RailTab>("moves");
  const [sheetOpen, setSheetOpen] = useState(false);
  const [ledger, setLedger] = useState<MoneyFigure | null>(null);
  const [prefill, setPrefill] = useState<Prefill | null>(null);
  const [headObs, setHeadObs] = useState<HeadObservation | null>(null);
  const [parentMetrics, setParentMetrics] = useState<MetricsResponse | null>(null);
  const [accepted, setAccepted] = useState<Record<string, AcceptedCommand[]>>({});
  const [plan] = useState<DemoPlan | null>(() => (typeof window === "undefined" ? null : loadDemoPlan(worldId)));
  const [prevAlert, setPrevAlert] = useState({ window: false, cards: 0 });
  const [compareWith, setCompareWith] = useState<string | null>(null);
  const [waiting, setWaiting] = useState<Record<string, number>>({});
  const [advancingId, setAdvancingId] = useState<string | null>(null);
  const [forkSeq, dispatchFork] = useReducer(forkSequenceReducer, INITIAL_FORK_SEQUENCE);
  const endedWindows = useRef<Set<string>>(new Set());
  const initialBranchId = useRef(branchId ?? null);
  const startClockRef = useRef("Thu 00:00");

  const mismatch = useStudioStore((s) => s.engineMismatch);
  usePlaybackKeybindings(mismatch);
  const engineering = useUiStore((s) => s.engineering);
  const markMismatch = useCallback(() => useStudioStore.getState().setEngineMismatch(true), []);

  const active = useStudioStore(selectActiveBranch);
  const branches = useStudioStore((s) => s.branches);
  const activeBranchId = useStudioStore((s) => s.activeBranchId);
  const selectedTick = useStudioStore((s) => s.selectedTick);
  const syncing = useStudioStore((s) => s.syncing);
  const metrics = useStudioStore((s) => s.metrics);
  const openWindowTick = useStudioStore((s) => s.openWindowTick);
  const error = useStudioStore((s) => s.error);
  const frame = useStudioStore(selectDisplayedFrame);
  const frames = useStudioStore((s) => (s.activeBranchId === null ? undefined : s.framesByBranch[s.activeBranchId]));

  // Open the world: load it, open the studio store, reset playback, load the displayed frames.
  useEffect(() => {
    let cancelled = false;
    let stopProgress: (() => void) | null = null;
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
        studio.setEngineMismatch(hasEngineMismatch(w.warnings) || hasEngineMismatch(list.warnings));
        playback.resetForWorld();
        stopProgress = playback.watchProgress();
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
      stopProgress?.();
      usePlaybackStore.getState().pause();
      useStudioStore.getState().reset();
    };
  }, [worldId, attempt]);

  // Observation at the head of the active Branch: needed by the decision cards and to detect an open window.
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

  // The other reality's Metrics at the displayed hour, for forked Branches.
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
    const previous = st.activeBranchId;
    st.setActiveBranch(id);
    // Switching to the reality being compared swaps the pair instead of comparing it with itself.
    setCompareWith((cur) => (cur === id ? previous : cur));
    window.history.replaceState(null, "", `?branch=${encodeURIComponent(id)}`);
    await usePlaybackStore.getState().selectTick(Math.min(st.selectedTick, target.headTick));
  }, []);

  const onForked = useCallback(async (child: Branch) => {
    const s = useStudioStore.getState();
    // The server has returned the new reality: only now does the split sequence start.
    dispatchFork({
      type: "server_returned",
      info: { whenLabel: formatSimTime(child.forkTick, startClockRef.current) },
      reducedMotion: prefersReducedMotion(),
    });
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
    setTab("moves");
    setSheetOpen(true);
    await usePlaybackStore.getState().selectTick(child.headTick);
  }, []);


  // Comparison: the other reality shown next to the active one.
  const partner = compareWith === null ? undefined : branches.find((b) => b.id === compareWith && b.id !== activeBranchId);
  const partnerId = partner?.id;
  const partnerHead = partner?.headTick;
  useEffect(() => {
    if (partnerId === undefined || partnerHead === undefined) return;
    let cancelled = false;
    void ensureFrames(partnerId, partnerHead).catch((e: unknown) => {
      if (!cancelled) useStudioStore.getState().setError(messageOf(e));
    });
    // Is a decision moment open at that reality's head? It is shown, never ended from here.
    getBranchState(partnerId, partnerHead)
      .then((st) => {
        if (cancelled) return;
        const open = st.observation.window.open && !endedWindows.current.has(`${partnerId}:${partnerHead}`);
        setWaiting((w) =>
          open ? { ...w, [partnerId]: partnerHead } : Object.fromEntries(Object.entries(w).filter(([k]) => k !== partnerId)),
        );
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [partnerId, partnerHead]);

  const startCompare = useCallback(() => {
    const s = useStudioStore.getState();
    const cur = s.branches.find((b) => b.id === s.activeBranchId);
    if (cur) setCompareWith(defaultPartnerId(cur, s.branches));
  }, []);

  const advancePartner = useCallback(async (id: string) => {
    const s = useStudioStore.getState();
    const b = s.branches.find((x) => x.id === id);
    if (!b) return;
    const req = advanceRequestFor(b.headTick, s.selectedTick);
    if (req === null) return;
    setAdvancingId(id);
    try {
      const r = await advanceBranch(id, req);
      const st = useStudioStore.getState();
      st.addFrames(id, r.frames);
      st.setBranchHead(id, r.headTick);
      if (r.paused === "decision_window") setWaiting((w) => ({ ...w, [id]: r.headTick }));
    } catch (e) {
      if (e instanceof ApiError && e.type === "stale-head") {
        try {
          const fresh = await getBranch(id);
          useStudioStore.getState().upsertBranch(fresh);
          await ensureFrames(id, fresh.headTick);
        } catch (e2) {
          useStudioStore.getState().setError(messageOf(e2));
        }
      } else {
        useStudioStore.getState().setError(messageOf(e));
      }
    } finally {
      setAdvancingId(null);
    }
  }, []);

  const goToPartnerReality = useCallback((id: string) => void activate(id), [activate]);
  const onForkDone = useCallback(() => dispatchFork({ type: "skip" }), []);

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
    if (s.activeBranchId !== null) {
      const id = s.activeBranchId;
      setWaiting((w) => {
        return Object.fromEntries(Object.entries(w).filter(([k]) => k !== id));
      });
    }
    s.setOpenWindowTick(null);
  }, []);

  const openModelTab = useCallback(() => {
    setTab("model");
    setSheetOpen(true);
  }, []);

  // F jumps to "Explore another future" (it never forks by itself).
  useEffect(() => {
    function onKey(e: KeyboardEvent): void {
      if (e.key !== "f" && e.key !== "F") return;
      if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey || e.shiftKey) return;
      if (useStudioStore.getState().engineMismatch) return;
      const t = e.target;
      if (t instanceof HTMLElement && (["INPUT", "TEXTAREA", "SELECT", "BUTTON", "A"].includes(t.tagName) || t.isContentEditable)) return;
      e.preventDefault();
      setTab("moves");
      setSheetOpen(true);
      requestAnimationFrame(() => document.getElementById("explore-future")?.focus());
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  const productNames = useMemo(() => {
    const names: string[] = [];
    for (const s of world?.scenario.skus ?? []) names[s.index] = productName(s.id);
    return names;
  }, [world]);
  const regularWorkers = world?.assumptions.find((a) => a.path === "workforce.regularWorkers")?.value;
  const startClock = world?.scenario.startClock ?? "Thu 00:00";
  useEffect(() => {
    startClockRef.current = startClock;
  }, [startClock]);

  const injected = useInjectedInputs(activeBranchId, markMismatch);
  const acceptedHere = activeBranchId !== null ? (accepted[activeBranchId] ?? []) : [];
  const inputs: TimelineInput[] = [
    ...acceptedHere.map((a) => ({
      effectiveTick: a.effectiveTick,
      provenance: a.provenance,
      kind: a.command.kind,
    })),
    ...injected.inputs,
  ];
  const interventions: Intervention[] = acceptedHere.map((a) => ({
    tick: a.effectiveTick,
    text: describeCommand(a, productNames),
    manager: "human",
  }));

  const interventionsFor = (id: string): Intervention[] =>
    (accepted[id] ?? []).map((a) => ({ tick: a.effectiveTick, text: describeCommand(a, productNames), manager: "human" }));

  const windowOpenHere = active !== undefined && openWindowTick !== null && openWindowTick === active.headTick;
  const observation =
    active !== undefined && headObs !== null && headObs.branchId === active.id && headObs.tick === active.headTick
      ? headObs.observation
      : null;
  const atHead = active !== undefined && selectedTick === active.headTick;
  const canAct = !mismatch && windowOpenHere && atHead && observation !== null;

  const cards = useMemo(
    () =>
      frame === undefined
        ? []
        : deriveCrisisCards({
            frame,
            observation,
            productNames,
            regularWorkers,
            startClock,
            canAct,
          }),
    [frame, observation, productNames, regularWorkers, startClock, canAct],
  );

  // The sheet opens by itself for a new decision moment or a new alert.
  if (prevAlert.window !== windowOpenHere || prevAlert.cards !== cards.length) {
    setPrevAlert({ window: windowOpenHere, cards: cards.length });
    if ((windowOpenHere && !prevAlert.window) || cards.length > prevAlert.cards) setSheetOpen(true);
  }

  const onCrisisAction = useCallback((a: CrisisAction) => {
    setPrefill((p) => ({
      nonce: (p?.nonce ?? 0) + 1,
      kind: a.kind,
      skuIndex: a.skuIndex,
      ...(a.role !== undefined ? { role: a.role } : {}),
    }));
    setTab("moves");
    setSheetOpen(true);
  }, []);

  if (loading) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-ink text-ivory-dim">
        <p role="status" className="text-sm">
          Opening your business…
        </p>
      </main>
    );
  }
  if (loadError !== null || world === null || active === undefined) {
    return (
      <main className="flex min-h-screen flex-col items-center justify-center gap-3 bg-ink text-ivory">
        <p role="alert" className="max-w-xl text-sm text-clay">
          {loadError ?? "The world could not be opened."}
        </p>
        <div className="flex gap-2 text-sm">
          <button
            type="button"
            className="min-h-11 rounded-[4px] border border-hairline px-3 hover:border-amber"
            onClick={() => {
              setLoadError(null);
              setLoading(true);
              setAttempt((n) => n + 1);
            }}
          >
            Retry
          </button>
          <Link href="/" className="inline-flex min-h-11 items-center rounded-[4px] border border-hairline px-3 hover:border-amber">
            Back to start
          </Link>
        </div>
      </main>
    );
  }

  const parent = parentId !== null ? branches.find((b) => b.id === parentId) : undefined;
  const parentLabel = parent === undefined ? undefined : parent.parentBranchId === null ? TERMS.originalPath : parent.label;
  const childCount = branches.filter((b) => b.parentBranchId !== null).length;
  const suggestionCommand = plan !== null && selectedTick >= plan.forkTick ? plan.commands[0] : undefined;
  const suggestion: Suggestion | null =
    suggestionCommand !== undefined
      ? { skuIndex: suggestionCommand.skuIndex, role: suggestionCommand.role, qty: suggestionCommand.qty }
      : null;
  const sharedUpTo =
    partner !== undefined ? (divergenceTick(active, partner, branches) ?? undefined) : active.parentBranchId !== null ? active.forkTick : undefined;

  const tabBtn = (id: RailTab, label: string, hideOnDesktop = false) => (
    <button
      type="button"
      role="tab"
      aria-selected={tab === id}
      onClick={() => {
        setTab(id);
        setSheetOpen(true);
      }}
      className={`min-h-11 rounded-[4px] border px-3 text-sm whitespace-nowrap max-[768px]:px-2 max-[768px]:text-xs ${hideOnDesktop ? "min-[769px]:hidden" : ""} ${
        tab === id ? "border-amber text-amber" : "border-hairline text-ivory-dim"
      }`}
    >
      {label}
    </button>
  );

  const panelClass = (id: RailTab): string => {
    if (id === "alerts") return tab === "alerts" ? "" : tab === "moves" ? "max-[768px]:hidden" : "hidden";
    return tab === id ? "" : "hidden";
  };

  return (
    <main className={`flex min-h-screen flex-col gap-3 bg-ink p-3 font-sans text-ivory min-[769px]:pb-3 ${sheetOpen ? "pb-[40vh]" : "pb-20"}`}>
      <header className="flex flex-col gap-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h1 className="flex items-baseline gap-3">
            <Link href="/" className="text-sm text-ivory-dim hover:text-ivory">
              Parallel
            </Link>
            <span className="font-display text-xl">{world.name}</span>
          </h1>
          <div className="flex flex-wrap items-center gap-2">
            {branches.length > 1 ? (
              <div className={partner !== undefined ? "max-[768px]:hidden" : ""}>
              <RealitySwitcher
                label="Which future you are looking at"
                activeId={active.id}
                onSelect={(id) => void activate(id)}
                items={orderBranches(branches).map((b, i) => ({ id: b.id, label: realityName(b), laneIndex: i }))}
              />
              </div>
            ) : null}
            {branches.length > 1 ? (
              <button
                type="button"
                aria-pressed={partner !== undefined}
                onClick={() => (partner !== undefined ? setCompareWith(null) : startCompare())}
                className={`min-h-11 rounded-[4px] border px-3 text-sm ${
                  partner !== undefined ? "border-amber text-amber" : "border-hairline text-ivory-dim hover:text-ivory"
                }`}
              >
                {partner !== undefined ? "Back to one view" : "Compare futures"}
              </button>
            ) : null}
            {partner !== undefined && branches.length > 2 ? (
              <label className="flex items-center gap-1 text-xs text-ivory-dim">
                Compare with
                <select
                  value={partner.id}
                  onChange={(e) => setCompareWith(e.target.value)}
                  className="min-h-11 rounded-[4px] border border-hairline bg-ink px-2 text-sm text-ivory"
                >
                  {orderBranches(branches)
                    .filter((b) => b.id !== active.id)
                    .map((b) => (
                      <option key={b.id} value={b.id}>
                        {realityName(b)}
                      </option>
                    ))}
                </select>
              </label>
            ) : null}
            <span className="text-xs text-ivory-dim" aria-live="polite" data-testid="syncing-indicator">
              {syncing ? "Updating…" : ""}
            </span>
            <EngineeringToggle />
          </div>
        </div>
        <Hud
          metrics={metrics}
          parentMetrics={parentId !== null && partner === undefined ? parentMetrics : null}
          {...(parentLabel !== undefined ? { parentLabel } : {})}
          syncing={syncing}
          tick={selectedTick}
          startClock={startClock}
          engineering={engineering}
          onMoneyClick={(f) => {
            setLedger(f);
            setTab("moves");
            setSheetOpen(true);
          }}
        />
        {error !== null ? (
          <div role="alert" className="flex items-center justify-between rounded-[4px] border border-clay px-3 py-1 text-sm text-clay">
            <span>{error}</span>
            <button
              type="button"
              className="min-h-11 underline"
              onClick={() => useStudioStore.getState().setError(null)}
            >
              Dismiss
            </button>
          </div>
        ) : null}
        {mismatch ? <EngineVersionBanner storedVersion={world.engineVersion} engineering={engineering} /> : null}
      </header>

      <div className="grid flex-1 grid-cols-1 gap-3 min-[769px]:grid-cols-[minmax(0,1fr)_22.5rem]">
        <section aria-label="Your warehouse" className="relative flex min-w-0 flex-col gap-2">
          {partner !== undefined ? (
            (() => {
              const [left, right] =
                laneIndexOf(branches, active.id) <= laneIndexOf(branches, partner.id) ? [active, partner] : [partner, active];
              return (
                <ComparisonView
                  left={left}
                  right={right}
                  branches={branches}
                  startClock={startClock}
                  productNames={productNames}
                  regularWorkers={regularWorkers}
                  interventionsFor={interventionsFor}
                  engineering={engineering}
                  waiting={waiting}
                  advancingId={advancingId}
                  onAdvance={(id) => void advancePartner(id)}
                  onGoTo={goToPartnerReality}
                />
              );
            })()
          ) : (
            <>
              <StagePane
                frame={frame ?? null}
                syncing={syncing}
                branchKey={active.id}
                productNames={productNames}
                regularWorkers={regularWorkers}
                interventions={interventions}
              />
              {branches.length > 1 ? (
                <div className="pointer-events-none absolute left-2 top-2 z-[5]">
                  <RealityBadge name={realityName(active)} lane={laneFor(laneIndexOf(branches, active.id))} />
                </div>
              ) : null}
            </>
          )}
          <ForkSequence state={forkSeq} onDone={onForkDone} />
        </section>

        <aside
          aria-label="Your move and alerts"
          className="rounded-[8px] border border-hairline bg-raised max-[768px]:fixed max-[768px]:inset-x-0 max-[768px]:bottom-0 max-[768px]:z-20 max-[768px]:rounded-b-none"
        >
          <div className="flex items-center gap-1 p-2 min-[769px]:flex-wrap">
            <button
              type="button"
              aria-expanded={sheetOpen}
              aria-controls="rail-content"
              onClick={() => setSheetOpen((o) => !o)}
              className="inline-flex min-h-11 items-center gap-2 rounded-[4px] border border-hairline px-3 text-sm min-[769px]:hidden"
            >
              {sheetOpen ? <ChevronDown aria-hidden size={18} /> : <ChevronUp aria-hidden size={18} />}
              <span className="max-[768px]:sr-only">{sheetOpen ? "Hide" : "Show"} your move</span>
              {cards.length > 0 ? (
                <span className="rounded-full bg-clay px-2 text-xs font-semibold text-ink" data-testid="alert-badge">
                  {cards.length}
                </span>
              ) : null}
            </button>
            <div role="tablist" aria-label="Rail" className="flex gap-1 min-[769px]:flex-wrap">
              {tabBtn("moves", TERMS.yourMove)}
              {tabBtn("alerts", cards.length > 0 ? `Alerts (${cards.length})` : "Alerts", true)}
              {tabBtn("model", TERMS.assumptions)}
            </div>
          </div>
          <div
            id="rail-content"
            className={`space-y-4 overflow-y-auto p-3 pt-1 max-[768px]:max-h-[30vh] min-[769px]:max-h-[36rem] ${
              sheetOpen ? "" : "max-[768px]:hidden"
            }`}
          >
            {ledger !== null ? (
              <LedgerPanel
                branchId={active.id}
                tick={selectedTick}
                title="Where this number comes from"
                startClock={startClock}
                engineering={engineering}
                figure={ledger}
                onClose={() => setLedger(null)}
              />
            ) : (
              <>
                <div className={panelClass("alerts")} role="tabpanel" aria-label="Alerts">
                  <CrisisCards cards={cards} canAct={canAct} onAction={onCrisisAction} />
                </div>
                <div className={`space-y-4 ${panelClass("moves")}`} role="tabpanel" aria-label={TERMS.yourMove}>
                  {canAct && observation !== null ? (
                    <DecisionPanel
                      key={`${active.id}:${active.headTick}`}
                      observation={observation}
                      windowTick={active.headTick}
                      branchId={active.id}
                      budgetRemaining={observation.window.commandBudget}
                      startClock={startClock}
                      suggestion={suggestion}
                      prefill={prefill}
                      assumptions={world.assumptions}
                      onSubmitted={onSubmitted}
                      onWindowEnded={onWindowEnded}
                    />
                  ) : mismatch ? (
                    <p className="text-sm text-ivory-dim" data-testid="read-only">
                      Read only. You can look back at this world, but not make moves in it.
                    </p>
                  ) : windowOpenHere ? (
                    <div className="space-y-2 text-sm" data-testid="window-elsewhere">
                      <p className="text-ivory-dim">
                        A decision moment is open at {formatSimTime(active.headTick, startClock)}. Show that hour to
                        make your move.
                      </p>
                      <button
                        type="button"
                        className="min-h-11 rounded-[4px] border border-amber px-3 text-amber"
                        onClick={() => void usePlaybackStore.getState().selectTick(active.headTick)}
                      >
                        Go to that hour
                      </button>
                    </div>
                  ) : (
                    <p className="text-sm text-ivory-dim" data-testid="no-window">
                      No decision is needed right now. Press Play, or skip to your next move.
                    </p>
                  )}
                  {mismatch ? null : (
                    <>
                      <ForkButton
                        key={active.id}
                        branchId={active.id}
                        tick={selectedTick}
                        headTick={active.headTick}
                        defaultLabel={TERMS.alternativeFuture(childCount + 1)}
                        whenLabel={formatSimTime(selectedTick, startClock)}
                        onForked={(b) => void onForked(b)}
                      />
                      <InjectDisruption
                        key={`inject-${active.id}`}
                        branchId={active.id}
                        headTick={active.headTick}
                        horizonTicks={world.scenario.horizonTicks}
                        startClock={startClock}
                        skus={world.scenario.skus}
                        onInjected={injected.refresh}
                        onEngineMismatch={markMismatch}
                      />
                    </>
                  )}
                </div>
                <div className={panelClass("model")} role="tabpanel" aria-label={TERMS.assumptions}>
                  <AssumptionsPanel world={world} />
                </div>
              </>
            )}
          </div>
        </aside>
      </div>

      <div className="space-y-2">
        <PlaybackControls readOnly={mismatch} />
        <ConnectedTimeline
          inputs={inputs}
          startClock={startClock}
          engineering={engineering}
          {...(sharedUpTo !== undefined ? { sharedUpTo } : {})}
        />
      </div>

      {engineering ? (
        <EngineeringDrawer
          world={world}
          branch={active}
          branches={branches}
          tick={selectedTick}
          frame={frame ?? null}
          frames={frames}
          metrics={metrics}
          observation={observation}
          plan={plan}
          accepted={acceptedHere}
          onSelectBranch={(id) => void activate(id)}
          onGoToTick={(t) => void usePlaybackStore.getState().selectTick(t)}
        />
      ) : null}

      <footer className="flex flex-wrap items-center justify-between gap-2 text-xs text-ivory-dim">
        <p data-testid="world-disclaimer">{DISCLAIMER_FULL}</p>
        <button type="button" onClick={openModelTab} className="min-h-11 underline">
          {TERMS.assumptions}
        </button>
      </footer>
    </main>
  );
}
