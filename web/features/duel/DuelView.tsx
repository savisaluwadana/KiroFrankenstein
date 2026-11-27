"use client";

/**
 * Duel view: your business and the AI manager's business side by side at ONE
 * displayed hour, with one shared time strip and playback. It reuses the studio
 * pieces (StagePane, Timeline, PlaybackControls, DecisionPanel, InjectDisruption,
 * AssumptionsPanel). Every number comes from the API. Playback is presentation
 * only: it paces authoritative frames and asks the Duel endpoint to advance both
 * sides together. Moves go to the Duel endpoints, never the Branch endpoints.
 */
import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";
import {
  advanceDuel,
  endDuelWindow,
  getBranchMetrics,
  getBranchState,
  getDuel,
  getDuelDecisions,
  getDuelScore,
  getWorld,
  hasEngineMismatch,
  injectDuelDisruption,
  listTickFrames,
  submitDuelCommands,
  type AcceptedCommand,
  type AdvanceResult,
  type Duel,
  type DuelDecision,
  type DuelScore,
  type DuelStatus,
  type MetricsResponse,
  type Observation,
  type World,
} from "@/lib/api/client";
import { LaneMark, laneFor } from "@/features/branches/lanes";
import { RealitySwitcher } from "@/features/branches/RealitySwitcher";
import { DecisionPanel } from "@/features/decisions/DecisionPanel";
import { InjectDisruption } from "@/features/decisions/InjectDisruption";
import { EngineeringToggle } from "@/features/engineering/EngineeringToggle";
import { AssumptionsPanel } from "@/features/studio/AssumptionsPanel";
import { EngineVersionBanner } from "@/features/studio/EngineVersionBanner";
import { StagePane } from "@/features/studio/StagePane";
import { ConnectedTimeline, type TimelineInput } from "@/features/timeline/Timeline";
import { PlaybackControls, usePlaybackKeybindings } from "@/features/world-view/PlaybackControls";
import type { Intervention } from "@/features/world-view/scene/worldViewModel";
import {
  DISCLAIMER_FULL,
  METRIC_LABELS,
  TERMS,
  formatCount,
  formatMoney,
  formatPercentBps,
  formatSimTime,
  productName,
} from "@/lib/glossary";
import { usePlaybackStore } from "@/stores/playback";
import { selectActiveBranch, useStudioStore } from "@/stores/studio";
import { useUiStore } from "@/stores/ui";
import { AiDecisions, AiStatus, HumanMoves, visibleDecisions } from "./DecisionsList";
import { describeMove, plainManagerLabel } from "./duelText";
import { Scoreboard } from "./Scoreboard";

/** Largest frame range the API serves per request. */
const FRAME_CHUNK = 240;

type RailTab = "move" | "happened" | "model";

function messageOf(e: unknown): string {
  return e instanceof Error ? e.message : "Request failed";
}

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

function useIsPhone(): boolean {
  const [phone, setPhone] = useState(false);
  useEffect(() => {
    if (typeof window.matchMedia !== "function") return;
    const mq = window.matchMedia("(max-width: 768px)");
    const update = (): void => setPhone(mq.matches);
    update();
    mq.addEventListener("change", update);
    return () => mq.removeEventListener("change", update);
  }, []);
  return phone;
}

interface PaneData {
  metrics: Record<string, MetricsResponse>;
  hashes: Record<string, string>;
}

/** Metrics (and, in Engineering Mode, state hashes) of both businesses at the displayed hour, fetched only while paused. */
function usePaneData(ids: readonly string[], tick: number, enabled: boolean, wantHashes: boolean): PaneData {
  const [data, setData] = useState<PaneData>({ metrics: {}, hashes: {} });
  const key = ids.join("|");
  useEffect(() => {
    if (!enabled || key === "") return;
    let cancelled = false;
    for (const id of key.split("|")) {
      const k = `${id}@${tick}`;
      getBranchMetrics(id, tick)
        .then((m) => {
          if (!cancelled) setData((d) => ({ ...d, metrics: { ...d.metrics, [k]: m } }));
        })
        .catch(() => undefined);
      if (wantHashes) {
        getBranchState(id, tick)
          .then((st) => {
            if (!cancelled) setData((d) => ({ ...d, hashes: { ...d.hashes, [k]: st.stateHash } }));
          })
          .catch(() => undefined);
      }
    }
    return () => {
      cancelled = true;
    };
  }, [key, tick, enabled, wantHashes]);
  return data;
}

function PaneFigures({ m }: { m: MetricsResponse | undefined }) {
  if (m === undefined) return <p className="text-xs text-ivory-dim">Loading figures…</p>;
  const x = m.metrics;
  const item = (label: string, value: string) => (
    <div className="flex flex-col">
      <dt className="text-xs text-ivory-dim">{label}</dt>
      <dd className="font-semibold tabular-nums">{value}</dd>
    </div>
  );
  return (
    <dl className="grid grid-cols-2 gap-2 text-sm" data-testid="pane-metrics">
      {item(METRIC_LABELS.cash, formatMoney(x.cashCents))}
      {item(METRIC_LABELS.netProfit, formatMoney(x.netProfitCents))}
      {item(METRIC_LABELS.serviceLevel, formatPercentBps(x.serviceLevelBps))}
      {item(METRIC_LABELS.backlog, formatCount(x.backlogOrders))}
    </dl>
  );
}

export interface DuelViewProps {
  duelId: string;
}

export function DuelView({ duelId }: DuelViewProps) {
  const [duel, setDuel] = useState<Duel | null>(null);
  const [status, setStatus] = useState<DuelStatus | null>(null);
  const [world, setWorld] = useState<World | null>(null);
  const [score, setScore] = useState<DuelScore | null>(null);
  const [decisions, setDecisions] = useState<DuelDecision[]>([]);
  const [humanMoves, setHumanMoves] = useState<AcceptedCommand[]>([]);
  const [observation, setObservation] = useState<{ tick: number; value: Observation } | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [tab, setTab] = useState<RailTab>("move");
  const [phoneSide, setPhoneSide] = useState<"human" | "ai">("human");
  const phone = useIsPhone();

  const engineering = useUiStore((s) => s.engineering);
  const mismatch = useStudioStore((s) => s.engineMismatch);
  usePlaybackKeybindings(mismatch);
  const markMismatch = useCallback(() => useStudioStore.getState().setEngineMismatch(true), []);

  const selectedTick = useStudioStore((s) => s.selectedTick);
  const syncing = useStudioStore((s) => s.syncing);
  const openWindowTick = useStudioStore((s) => s.openWindowTick);
  const error = useStudioStore((s) => s.error);
  const framesByBranch = useStudioStore((s) => s.framesByBranch);
  const playing = usePlaybackStore((s) => s.playing);
  const humanBranch = useStudioStore(selectActiveBranch);

  const humanId = duel?.human.branch.id ?? null;
  const aiId = duel?.ai.branch.id ?? null;

  // Open the duel: load it and its world, open the studio store on the human side, load both sides' frames.
  useEffect(() => {
    let cancelled = false;
    const studio = useStudioStore.getState();
    const playback = usePlaybackStore.getState();
    void (async () => {
      try {
        const res = await getDuel(duelId);
        const w = await getWorld(res.duel.worldId);
        if (cancelled) return;
        const st = res.status;
        const hb = { ...res.duel.human.branch, headTick: st.humanHeadTick };
        const ab = { ...res.duel.ai.branch, headTick: st.aiHeadTick };
        const shared = Math.min(st.humanHeadTick, st.aiHeadTick);
        studio.openWorld({
          worldId: res.duel.worldId,
          horizonTicks: st.horizonTick,
          branches: [hb, ab],
          activeBranchId: hb.id,
          selectedTick: shared,
        });
        studio.setEngineMismatch(hasEngineMismatch(st.warnings) || hasEngineMismatch(w.warnings));
        playback.resetForWorld();
        // Both sides advance together through the Duel endpoint; the Branch endpoint answers 409 for a duel Branch.
        playback.setAdvancer(async (_branchId, body): Promise<AdvanceResult> => {
          const r = await advanceDuel(duelId, body);
          const s = useStudioStore.getState();
          if (r.ai !== undefined) {
            s.addFrames(ab.id, r.ai.frames);
            s.setBranchHead(ab.id, r.ai.headTick);
          }
          setStatus(r.status);
          return (
            r.human ?? {
              headTick: r.headTick,
              stateHash: "",
              paused: r.paused,
              windowReasons: r.windowReasons,
              frames: [],
              currency: r.currency,
              disclaimer: r.disclaimer,
            }
          );
        });
        await Promise.all([ensureFrames(hb.id, shared), ensureFrames(ab.id, shared)]);
        if (cancelled) return;
        if (st.waitingOn === "human") useStudioStore.getState().setOpenWindowTick(st.humanHeadTick);
        setDuel(res.duel);
        setStatus(st);
        setWorld(w);
        setLoading(false);
        await usePlaybackStore.getState().selectTick(shared);
      } catch (e) {
        if (cancelled) return;
        setLoadError(messageOf(e));
        setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
      usePlaybackStore.getState().pause();
      usePlaybackStore.getState().setAdvancer(null);
      useStudioStore.getState().reset();
    };
  }, [duelId, attempt]);

  const refreshStatus = useCallback(async () => {
    try {
      const res = await getDuel(duelId);
      setStatus(res.status);
    } catch (e) {
      useStudioStore.getState().setError(messageOf(e));
    }
  }, [duelId]);

  // The score at the comparison hour. It changes only when the two sides reach new hours.
  const comparisonTick = status?.comparisonTick;
  const finished = status?.finished;
  const ready = !loading && duel !== null;
  useEffect(() => {
    if (!ready) return;
    let cancelled = false;
    getDuelScore(duelId)
      .then((s) => {
        if (!cancelled) setScore(s);
      })
      .catch((e: unknown) => {
        if (!cancelled) useStudioStore.getState().setError(messageOf(e));
      });
    return () => {
      cancelled = true;
    };
  }, [duelId, ready, comparisonTick, finished]);

  // The AI's decisions. The server withholds the open window's decision until the human ends it.
  const waitingOn = status?.waitingOn;
  const humanHead = status?.humanHeadTick;
  useEffect(() => {
    if (!ready) return;
    let cancelled = false;
    getDuelDecisions(duelId)
      .then((r) => {
        if (!cancelled) setDecisions(r.decisions);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [duelId, ready, waitingOn, humanHead]);

  // The world's LLM usage and budget counters change as the AI decides; refresh them with each new head.
  const worldId = duel?.worldId;
  useEffect(() => {
    if (!ready || worldId === undefined || humanHead === undefined) return;
    let cancelled = false;
    getWorld(worldId)
      .then((w) => {
        if (!cancelled) setWorld(w);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [ready, worldId, humanHead, waitingOn]);

  // The human's Observation at their head, for the decision cards.
  useEffect(() => {
    if (!ready || humanId === null || humanHead === undefined) return;
    let cancelled = false;
    getBranchState(humanId, humanHead)
      .then((st) => {
        if (!cancelled) setObservation({ tick: humanHead, value: st.observation });
      })
      .catch((e: unknown) => {
        if (!cancelled) useStudioStore.getState().setError(messageOf(e));
      });
    return () => {
      cancelled = true;
    };
  }, [ready, humanId, humanHead]);

  const productNames = useMemo(() => {
    const names: string[] = [];
    for (const s of world?.scenario.skus ?? []) names[s.index] = productName(s.id);
    return names;
  }, [world]);
  const startClock = world?.scenario.startClock ?? "Thu 00:00";

  const shownDecisions = useMemo(() => visibleDecisions(decisions, openWindowTick), [decisions, openWindowTick]);

  const paneIds = useMemo(() => (humanId !== null && aiId !== null ? [humanId, aiId] : []), [humanId, aiId]);
  const pane = usePaneData(paneIds, selectedTick, !playing && !loading, engineering);

  const onSubmitted = useCallback((result: { accepted: AcceptedCommand[] }) => {
    setHumanMoves((prev) => [...prev, ...result.accepted]);
  }, []);

  const onWindowEnded = useCallback(() => {
    useStudioStore.getState().setOpenWindowTick(null);
    void refreshStatus();
    // Continue: the window is ended, so now advance both businesses to the next moment.
    usePlaybackStore.getState().play();
  }, [refreshStatus]);

  if (loading) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-ink text-ivory-dim">
        <p role="status" className="text-sm">
          Opening the duel…
        </p>
      </main>
    );
  }
  if (loadError !== null || duel === null || status === null || world === null || humanBranch === undefined) {
    return (
      <main className="flex min-h-screen flex-col items-center justify-center gap-3 bg-ink text-ivory">
        <p role="alert" className="max-w-xl text-sm text-clay">
          {loadError ?? "The duel could not be opened."}
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

  const humanLabel = "You";
  const aiLabel = plainManagerLabel(duel.ai.managerLabel);
  const windowOpenHere = openWindowTick !== null && openWindowTick === humanBranch.headTick;
  const atHead = selectedTick === humanBranch.headTick;
  const canAct =
    !mismatch &&
    !status.finished &&
    windowOpenHere &&
    atHead &&
    observation !== null &&
    observation.tick === humanBranch.headTick;
  const waiting = status.waitingOn === "human" || windowOpenHere;

  const timelineInputs: TimelineInput[] = humanMoves.map((a) => ({
    effectiveTick: a.effectiveTick,
    provenance: a.provenance,
    kind: a.command.kind,
  }));
  const humanInterventions: Intervention[] = humanMoves.map((a) => ({
    tick: a.effectiveTick,
    text: describeMove(a.command, productNames),
    manager: "human",
  }));
  const aiInterventions: Intervention[] = shownDecisions.flatMap((d) => {
    const rejected = new Set(d.rejected.map((r) => r.index));
    return d.proposed.flatMap((c, i) =>
      rejected.has(i)
        ? []
        : [
            {
              tick: d.windowTick + 1,
              text: describeMove(c, productNames),
              manager: d.managerKind,
              ...(d.model !== undefined ? { model: d.model } : {}),
              ...(d.fallbackReason !== undefined ? { fallbackReason: d.fallbackReason } : {}),
            } satisfies Intervention,
          ],
    );
  });

  const sides = [
    { key: "human" as const, id: duel.human.branch.id, label: humanLabel, lane: 0, interventions: humanInterventions },
    { key: "ai" as const, id: duel.ai.branch.id, label: aiLabel, lane: 1, interventions: aiInterventions },
  ];

  const paneFor = (s: (typeof sides)[number]) => (
    <section
      key={s.key}
      aria-label={`Business: ${s.label}`}
      data-testid={`pane-${s.key}`}
      className="flex min-w-0 flex-col gap-2 rounded-[8px] border border-hairline bg-surface p-2"
    >
      <h3 className="flex items-center gap-2 text-sm font-medium" data-testid={`pane-title-${s.key}`}>
        <LaneMark lane={laneFor(s.lane)} />
        {s.label}
      </h3>
      <StagePane
        frame={framesByBranch[s.id]?.[selectedTick] ?? null}
        syncing={syncing}
        branchKey={s.id}
        productNames={productNames}
        regularWorkers={world.assumptions.find((a) => a.path === "workforce.regularWorkers")?.value}
        interventions={s.interventions}
      />
      <PaneFigures m={pane.metrics[`${s.id}@${selectedTick}`]} />
    </section>
  );

  const tabBtn = (id: RailTab, label: string) => (
    <button
      type="button"
      role="tab"
      aria-selected={tab === id}
      onClick={() => setTab(id)}
      className={`min-h-11 rounded-[4px] border px-3 text-sm whitespace-nowrap ${tab === id ? "border-amber text-amber" : "border-hairline text-ivory-dim"}`}
    >
      {label}
    </button>
  );

  const hashHuman = pane.hashes[`${duel.human.branch.id}@${selectedTick}`];
  const hashAi = pane.hashes[`${duel.ai.branch.id}@${selectedTick}`];

  return (
    <main className="flex min-h-screen flex-col gap-3 bg-ink p-3 font-sans text-ivory">
      <header className="flex flex-wrap items-center justify-between gap-2">
        <h1 className="flex items-baseline gap-3">
          <Link href="/" className="text-sm text-ivory-dim hover:text-ivory">
            Parallel
          </Link>
          <span className="font-display text-xl">You versus the AI manager</span>
        </h1>
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs text-ivory-dim" aria-live="polite" data-testid="syncing-indicator">
            {syncing ? "Updating…" : ""}
          </span>
          <EngineeringToggle />
        </div>
      </header>

      <p className="text-sm text-ivory-dim" data-testid="shared-tick">
        Both businesses are shown at {formatSimTime(selectedTick, startClock)}. They face the same customers and the same
        surprises.
      </p>

      {error !== null ? (
        <div role="alert" className="flex items-center justify-between rounded-[4px] border border-clay px-3 py-1 text-sm text-clay">
          <span>{error}</span>
          <button type="button" className="min-h-11 underline" onClick={() => useStudioStore.getState().setError(null)}>
            Dismiss
          </button>
        </div>
      ) : null}
      {mismatch ? <EngineVersionBanner storedVersion={world.engineVersion} engineering={engineering} /> : null}

      <div className="grid flex-1 grid-cols-1 gap-3 min-[769px]:grid-cols-[minmax(0,1fr)_22.5rem]">
        <div className="flex min-w-0 flex-col gap-3">
          {phone ? (
            <RealitySwitcher
              label="Which business to show"
              activeId={phoneSide === "ai" ? duel.ai.branch.id : duel.human.branch.id}
              stickyOnPhone
              onSelect={(id) => setPhoneSide(id === duel.ai.branch.id ? "ai" : "human")}
              items={sides.map((s) => ({ id: s.id, label: s.label, laneIndex: s.lane }))}
            />
          ) : null}
          <div className={`grid gap-3 ${phone ? "grid-cols-1" : "grid-cols-2"}`} data-testid="duel-panes">
            {phone ? paneFor(phoneSide === "ai" ? (sides[1] as (typeof sides)[number]) : (sides[0] as (typeof sides)[number])) : sides.map(paneFor)}
          </div>

          <div className="space-y-2">
            <PlaybackControls readOnly={mismatch} />
            <ConnectedTimeline inputs={timelineInputs} startClock={startClock} engineering={engineering} />
          </div>

          {score !== null ? (
            <Scoreboard score={score} startClock={startClock} onOpenAssumptions={() => setTab("model")} />
          ) : (
            <p className="text-sm text-ivory-dim" role="status">
              Loading the scoreboard…
            </p>
          )}

          {engineering ? (
            <section aria-label="Engineering details" data-testid="duel-engineering" className="space-y-1 rounded-[4px] border border-hairline bg-ink p-2 font-mono text-xs">
              <p>
                State hash at tick {selectedTick}: you {hashHuman ?? "…"}
              </p>
              <p>
                State hash at tick {selectedTick}: AI {hashAi ?? "…"}
              </p>
              <p data-testid="llm-usage">
                LLM calls made {formatCount(world.llmCallsMade)}, remaining {formatCount(world.llmCallsRemaining)}; tokens
                used {formatCount(world.llmTokensUsed)}, remaining {formatCount(world.llmTokensRemaining)}
              </p>
              <p>
                Heads: you {status.humanHeadTick}, AI {status.aiHeadTick}, comparison tick {status.comparisonTick}, horizon{" "}
                {status.horizonTick}
              </p>
            </section>
          ) : null}
        </div>

        <aside aria-label="Your move and what happened" className="self-start rounded-[8px] border border-hairline bg-raised min-[769px]:sticky min-[769px]:top-3">
          <div className="flex flex-wrap items-center gap-1 p-2" role="tablist" aria-label="Rail">
            {tabBtn("move", TERMS.yourMove)}
            {tabBtn("happened", "What happened")}
            {tabBtn("model", TERMS.assumptions)}
          </div>
          <div className="space-y-4 overflow-y-auto p-3 pt-1 min-[769px]:max-h-[40rem]">
            {tab === "move" ? (
              <div className="space-y-4" role="tabpanel" aria-label={TERMS.yourMove}>
                <AiStatus waiting={waiting} decidedCount={shownDecisions.length} />
                {canAct && observation !== null ? (
                  <DecisionPanel
                    key={`${humanBranch.id}:${humanBranch.headTick}`}
                    observation={observation.value}
                    windowTick={humanBranch.headTick}
                    branchId={humanBranch.id}
                    budgetRemaining={observation.value.window.commandBudget}
                    startClock={startClock}
                    assumptions={world.assumptions}
                    submitCommandsFn={(body, key) => submitDuelCommands(duelId, body, key)}
                    endWindowFn={(body) => endDuelWindow(duelId, body)}
                    onSubmitted={onSubmitted}
                    onWindowEnded={onWindowEnded}
                  />
                ) : mismatch ? (
                  <p className="text-sm text-ivory-dim" data-testid="read-only">
                    Read only. You can look back at this duel, but not make moves in it.
                  </p>
                ) : status.finished ? (
                  <p className="text-sm text-ivory-dim" data-testid="duel-finished">
                    The week is over. Look back at any hour with the time strip.
                  </p>
                ) : windowOpenHere ? (
                  <div className="space-y-2 text-sm" data-testid="window-elsewhere">
                    <p className="text-ivory-dim">
                      A decision moment is open at {formatSimTime(humanBranch.headTick, startClock)}. Show that hour to make
                      your move.
                    </p>
                    <button
                      type="button"
                      className="min-h-11 rounded-[4px] border border-amber px-3 text-amber"
                      onClick={() => void usePlaybackStore.getState().selectTick(humanBranch.headTick)}
                    >
                      Go to that hour
                    </button>
                  </div>
                ) : (
                  <p className="text-sm text-ivory-dim" data-testid="no-window">
                    No decision is needed right now. Press Play, or skip to your next move.
                  </p>
                )}
                {mismatch || status.finished ? null : (
                  <InjectDisruption
                    key={`inject-${humanBranch.id}`}
                    branchId={humanBranch.id}
                    headTick={humanBranch.headTick}
                    horizonTicks={status.horizonTick}
                    startClock={startClock}
                    skus={world.scenario.skus}
                    title="Add a surprise to both businesses"
                    submitFn={async (body, key) => (await injectDuelDisruption(duelId, body, key)).human}
                    onEngineMismatch={markMismatch}
                  />
                )}
              </div>
            ) : null}
            {tab === "happened" ? (
              <div className="space-y-4" role="tabpanel" aria-label="What happened">
                <section aria-label="What you did" className="space-y-1">
                  <h3 className="font-display text-lg">What you did</h3>
                  <HumanMoves moves={humanMoves} startClock={startClock} productNames={productNames} />
                </section>
                <section aria-label="What the AI did and why" className="space-y-2">
                  <h3 className="font-display text-lg">What the AI did and why</h3>
                  <AiStatus waiting={waiting} decidedCount={shownDecisions.length} />
                  <AiDecisions
                    decisions={shownDecisions}
                    startClock={startClock}
                    productNames={productNames}
                    engineering={engineering}
                  />
                </section>
              </div>
            ) : null}
            {tab === "model" ? (
              <div role="tabpanel" aria-label={TERMS.assumptions}>
                <AssumptionsPanel world={world} />
              </div>
            ) : null}
          </div>
        </aside>
      </div>

      <footer className="text-xs text-ivory-dim">
        <p data-testid="world-disclaimer">{DISCLAIMER_FULL}</p>
      </footer>
    </main>
  );
}
