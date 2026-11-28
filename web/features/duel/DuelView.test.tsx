// @vitest-environment jsdom
/**
 * Duel view against a fake API. Only fetch is faked; the stores, playback
 * controller and feature components are real. The PixiJS diorama is replaced by
 * a stand-in that exposes the tick it was given (jsdom has no canvas).
 */
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { rememberWriteToken } from "@/lib/api/writeToken";
import type { DuelDecision, DuelScore, TickFrame } from "@/lib/api/client";
import { useStudioStore } from "@/stores/studio";
import { usePlaybackStore } from "@/stores/playback";
import { useUiStore } from "@/stores/ui";
import { DuelView } from "./DuelView";

vi.mock("@/features/world-view/WorldView", () => ({
  WorldView: ({ frame, branchKey }: { frame: TickFrame | null; branchKey: string }) => (
    <div data-testid={`worldview-${branchKey}`} data-tick={frame === null ? "none" : String(frame.tick)} />
  ),
}));

vi.mock("next/link", () => ({
  default: ({ href, children }: { href: string; children: ReactNode }) => <a href={href}>{children}</a>,
}));

const HORIZON = 12;
const HEAD = 4;

function frameAt(tick: number): TickFrame {
  const markers = tick === HEAD ? [{ kind: "decision_window", windowReason: "cadence" }] : [];
  return {
    tick,
    stateHash: `hash-${tick}`,
    skus: [{ sku: 0, onHand: 100 - tick, inboundPOs: 0, inboundUnits: 0 }],
    workersOnShift: 20,
    orderQueueLen: 0,
    markers,
    disruptions: [],
    report: { tick, capacity: 100, capacityUsed: 0, shippedOrders: 0, skus: [] },
  } as unknown as TickFrame;
}

const observation = {
  tick: HEAD,
  cashCents: 1234500,
  skus: [
    {
      sku: 0,
      id: "speaker",
      onHand: 40,
      inventoryValueCents: 0,
      referencePriceCents: 2000,
      priceBounds: { minCents: 1000, maxCents: 4000 },
      priceCents: 2000,
      marketingCents: 0,
      inboundPOs: 0,
      inboundUnits: 0,
      suppliers: [
        { role: "primary", unitCostCents: 1000, leadTimeTicks: 24, available: true, availableFromTick: 0 },
        { role: "backup", unitCostCents: 1250, leadTimeTicks: 8, available: true, availableFromTick: 0 },
      ],
    },
  ],
  openOrders: [],
  purchaseOrders: [],
  workforce: {},
  disruptions: [],
  metrics: { backlogOrders: 3 },
  window: { open: true, commandBudget: 5 },
};

const iso = "2026-01-01T00:00:00Z";
const world = {
  id: "w1",
  name: "Black Friday Meltdown",
  seed: 2026,
  engineVersion: "1",
  scenarioHash: "abc",
  createdAt: iso,
  currency: "USD",
  llmAvailable: false,
  llmCallsMade: 3,
  llmCallsRemaining: 37,
  llmTokensUsed: 1200,
  llmTokensRemaining: 8800,
  scenario: {
    name: "Black Friday Meltdown",
    horizonTicks: HORIZON,
    startClock: "Thu 00:00",
    decisionCadenceTicks: 4,
    skus: [{ index: 0, id: "speaker" }],
  },
  assumptions: [
    { path: "workforce.regularWorkers", value: 20, unit: "workers", default: 20, min: 1, max: 100, rationale: "fixture", overridden: false },
  ],
};

function branch(id: string, label: string, kind: string) {
  return { id, worldId: "w1", parentBranchId: "root", forkTick: 0, headTick: HEAD, label, managerKind: kind, createdAt: iso };
}

function metrics(extra: Record<string, number> = {}) {
  return {
    tick: HEAD,
    cashCents: 1_000_000,
    netProfitCents: 125_050,
    serviceLevelBps: 9125,
    fillRateBps: 9400,
    backlogOrders: 7,
    ...extra,
  };
}

function scoreFixture(over: Partial<DuelScore> = {}): DuelScore {
  return {
    duelId: "d1",
    comparisonTick: HEAD,
    horizonTick: HORIZON,
    provisional: true,
    outcome: "human_wins",
    human: {
      branchId: "bh",
      managerKind: "human",
      managerLabel: "You",
      score: { financialBps: 6000, fulfilmentRateBps: 9400, onTimeBps: 9125, compositeHundredths: 5234 },
      weighted: { financialHundredths: 3000, fulfilmentHundredths: 2820, onTimeHundredths: 1825 },
      metrics: metrics() as never,
      cancelledOrders: 2,
    },
    ai: {
      branchId: "ba",
      managerKind: "ai_rule_based",
      managerLabel: "Rule-based AI",
      score: { financialBps: 5000, fulfilmentRateBps: 9000, onTimeBps: 8000, compositeHundredths: 4876 },
      weighted: { financialHundredths: 2500, fulfilmentHundredths: 2700, onTimeHundredths: 1600 },
      metrics: metrics({ cashCents: 900_000, netProfitCents: 80_000, serviceLevelBps: 8000, fillRateBps: 9000, backlogOrders: 11 }) as never,
      cancelledOrders: 5,
    },
    weights: { financialWeightBps: 5000, fulfilmentWeightBps: 3000, onTimeWeightBps: 2000 },
    profitFloorCents: -100_000,
    profitTargetCents: 500_000,
    tieToleranceHundredths: 10,
    currency: "USD",
    disclaimer: "Synthetic simulation output under the stated assumptions. Not a forecast or guarantee of real-world results.",
    ...over,
  } as DuelScore;
}

function decision(over: Partial<DuelDecision> = {}): DuelDecision {
  return {
    managerLabel: "Rule-based AI",
    windowTick: 0,
    managerKind: "ai_rule_based",
    provenance: "ai_rule_based",
    rationale: "Stock is low.",
    proposed: [{ kind: "place_purchase_order", sku: 0, role: "backup", qty: 200 }],
    acceptedCount: 1,
    rejected: [],
    createdAt: iso,
    ...over,
  } as DuelDecision;
}

interface Opts {
  waitingOn?: "human" | "none";
  score?: DuelScore;
  decisions?: DuelDecision[];
  aiLabel?: string;
}

interface Call {
  method: string;
  path: string;
  body: unknown;
  headers: Record<string, string>;
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function installFetch(opts: Opts = {}) {
  const calls: Call[] = [];
  let ended = false;
  const waitingOn = (): "human" | "none" => (ended ? "none" : (opts.waitingOn ?? "human"));
  const status = () => ({
    duelId: "d1",
    humanHeadTick: HEAD,
    aiHeadTick: HEAD,
    comparisonTick: HEAD,
    horizonTick: HORIZON,
    finished: false,
    windowOpen: waitingOn() === "human",
    windowReasons: ["cadence"],
    waitingOn: waitingOn(),
  });
  const duel = () => ({
    id: "d1",
    worldId: "w1",
    parentBranchId: "root",
    aiKind: "ai_rule_based",
    forkTick: 0,
    comparisonPolicy: "min_head_tick",
    createdAt: iso,
    human: { branch: branch("bh", "You", "human"), managerLabel: "You" },
    ai: { branch: branch("ba", "AI", "ai_rule_based"), managerLabel: opts.aiLabel ?? "Rule-based AI" },
  });

  const fake = vi.fn(async (url: string, init?: RequestInit): Promise<Response> => {
    const u = new URL(url);
    const path = u.pathname.replace(/^\/api\/v1/, "");
    const method = init?.method ?? "GET";
    let body: unknown;
    if (typeof init?.body === "string") body = JSON.parse(init.body);
    calls.push({ method, path, body, headers: (init?.headers ?? {}) as Record<string, string> });

    if (path === "/duels/d1" && method === "GET") return json(200, { duel: duel(), status: status() });
    if (path === "/worlds/w1") return json(200, world);
    if (path === "/duels/d1/score") return json(200, opts.score ?? scoreFixture());
    if (path === "/duels/d1/decisions") return json(200, { duelId: "d1", branchId: "ba", decisions: opts.decisions ?? [] });
    if (path === "/duels/d1/decision-window/end") {
      ended = true;
      return new Response(null, { status: 204 });
    }
    if (path === "/duels/d1/commands") {
      const b = body as { commands: unknown[] };
      return json(200, {
        accepted: b.commands.map((c, i) => ({ seq: i + 1, effectiveTick: HEAD + 1, command: c, provenance: "human" })),
        rejected: [],
        budgetRemaining: 4,
        duplicate: false,
      });
    }
    if (path === "/duels/d1/disruptions") {
      const r = { id: "i1", effectiveTick: HEAD + 1, durationTicks: 12, kind: "supplier_outage" };
      return json(200, { human: r, ai: r });
    }
    if (path === "/duels/d1/advance") return new Response(null, { status: 500 });
    const frames = /^\/branches\/(bh|ba)\/tick-frames$/.exec(path);
    if (frames) {
      const from = Number(u.searchParams.get("from"));
      const to = Number(u.searchParams.get("to"));
      const out: TickFrame[] = [];
      for (let t = from; t <= to; t++) out.push(frameAt(t));
      return json(200, { frames: out });
    }
    const state = /^\/branches\/(bh|ba)\/state$/.exec(path);
    if (state) {
      const tick = Number(u.searchParams.get("tick"));
      return json(200, { tick, stateHash: `state-${state[1]}-${tick}`, observation: { ...observation, tick } });
    }
    if (/^\/branches\/(bh|ba)\/metrics$/.test(path)) {
      return json(200, { tick: Number(u.searchParams.get("tick")), currency: "USD", disclaimer: "x", metrics: metrics() });
    }
    return json(404, { type: "about:blank", title: "not found", status: 404 });
  });
  vi.stubGlobal("fetch", fake);
  return calls;
}

beforeEach(() => {
  window.sessionStorage.clear();
  rememberWriteToken("tok-test", "d1"); // this tab created the duel
  useUiStore.setState({ engineering: false });
});

afterEach(() => {
  cleanup();
  usePlaybackStore.getState().pause();
  usePlaybackStore.getState().setAdvancer(null);
  useStudioStore.getState().reset();
  vi.unstubAllGlobals();
});

async function openDuel(opts: Opts = {}) {
  const calls = installFetch(opts);
  render(<DuelView duelId="d1" />);
  await screen.findByTestId("duel-panes");
  return calls;
}

describe("DuelView", () => {
  it("renders both businesses at the same displayed hour", async () => {
    await openDuel();
    await waitFor(() => {
      expect(screen.getByTestId("worldview-bh").getAttribute("data-tick")).toBe(String(HEAD));
      expect(screen.getByTestId("worldview-ba").getAttribute("data-tick")).toBe(String(HEAD));
    });
    expect(screen.getByTestId("pane-title-human").textContent).toBe("You");
    expect(screen.getByTestId("pane-title-ai").textContent).toBe("Rule-based AI");
    expect(screen.getByTestId("shared-tick").textContent).toContain("Thursday");
    // The one shared time strip drives both panes.
    expect(screen.getAllByRole("region", { name: "Playback controls" })).toHaveLength(1);
  });

  it("shows API scores to two decimals with weights, raw figures and the tie rule", async () => {
    await openDuel();
    await screen.findByTestId("scoreboard");
    expect(screen.getByTestId("score-human").textContent).toBe("52.34");
    expect(screen.getByTestId("score-ai").textContent).toBe("48.76");
    expect(screen.getByTestId("component-financial").textContent).toContain("Financial (50%)");
    expect(screen.getByTestId("component-financial").textContent).toContain("30.00");
    expect(screen.getByTestId("component-fulfilment").textContent).toContain("Orders fully shipped (30%)");
    expect(screen.getByTestId("component-onTime").textContent).toContain("On time (20%)");
    expect(screen.getByTestId("raw-cash").textContent).toContain("$10,000.00");
    expect(screen.getByTestId("raw-profit").textContent).toContain("$1,250.50");
    expect(screen.getByTestId("raw-onTime").textContent).toContain("91.2%");
    expect(screen.getByTestId("raw-waiting").textContent).toContain("7");
    expect(screen.getByTestId("raw-cancelled").textContent).toContain("5");
    // Observed differences are display subtraction of API values.
    expect(screen.getByTestId("raw-composite").textContent).toContain("+3.58");
    expect(screen.getByTestId("raw-profit").textContent).toContain("+$450.50");
    expect(screen.getByText(/Scores within 0\.1 points count as a draw/)).toBeTruthy();
    expect(screen.getByText(/-\$1,000\.00/)).toBeTruthy();
    expect(screen.getByTestId("duel-disclaimer").textContent).toContain("Not a forecast or guarantee");
  });

  it("speaks business language in normal mode (no engine words on the duel page)", async () => {
    await openDuel({ decisions: [decision()] });
    await screen.findByTestId("scoreboard");
    const text = document.body.textContent ?? "";
    expect(text.length).toBeGreaterThan(500);
    expect(text).not.toMatch(/\bSKUs?\b|\bticks?\b|stock-bound|capacity-bound|\bhash(es)?\b|\bPOs?\b|\bengine\b|provenance|disruption|\bnull\b|undefined/i);
    expect(text).not.toMatch(/\bNaN\b/i);
  });

  it("calls the outcome provisional before the horizon and names it only at the end", async () => {
    await openDuel({ score: scoreFixture({ provisional: true, outcome: "ai_wins" }) });
    await screen.findByTestId("scoreboard");
    expect(screen.getByTestId("outcome").textContent).toBe("Provisional — the week isn't over");
    expect(document.body.textContent).not.toContain("AI won");
    cleanup();

    for (const [outcome, text] of [
      ["human_wins", "You won"],
      ["ai_wins", "AI won"],
      ["draw", "Draw"],
    ] as const) {
      await openDuel({ score: scoreFixture({ provisional: false, outcome }) });
      await screen.findByTestId("scoreboard");
      expect(screen.getByTestId("outcome").textContent).toBe(text);
      cleanup();
      useStudioStore.getState().reset();
    }
  });

  it("hides the AI decision until the human ends the window, then explains it", async () => {
    const user = userEvent.setup();
    const calls = await openDuel({ waitingOn: "human", decisions: [decision({ windowTick: HEAD })] });
    expect(await screen.findByText(/The AI manager is waiting for you/)).toBeTruthy();
    await user.click(screen.getByRole("tab", { name: "What happened" }));
    expect(screen.queryByTestId("ai-decision")).toBeNull();
    expect(screen.queryByText("Stock is low.")).toBeNull();

    await user.click(screen.getByRole("tab", { name: "Your move" }));
    await user.click(await screen.findByRole("button", { name: "Continue" }));
    await waitFor(() => expect(calls.some((c) => c.path === "/duels/d1/decision-window/end")).toBe(true));
    usePlaybackStore.getState().pause();

    await user.click(screen.getByRole("tab", { name: "What happened" }));
    const item = await screen.findByTestId("ai-decision");
    expect(within(item).getByText("Stock is low.")).toBeTruthy();
    expect(within(item).getByTestId("provenance-badge").textContent).toBe("Rule-based AI");
    expect(item.textContent).toContain("Did: Ordered 200");
    expect(screen.getByText("The AI manager has decided.")).toBeTruthy();
  });

  it("renders a rationale as literal text labelled not verified", async () => {
    const user = userEvent.setup();
    const rationale = "<script>alert(1)</script> and [x](javascript:alert(1)) and **bold** <img src=x onerror=alert(2)>";
    await openDuel({ waitingOn: "none", decisions: [decision({ rationale })] });
    await user.click(await screen.findByRole("tab", { name: "What happened" }));
    const box = await screen.findByTestId("ai-rationale");
    expect(within(box).getByText("AI rationale (not verified)")).toBeTruthy();
    expect(box.textContent).toContain(rationale);
    expect(box.querySelector("script")).toBeNull();
    expect(box.querySelector("a")).toBeNull();
    expect(box.querySelector("img")).toBeNull();
    expect(box.querySelector("strong")).toBeNull();
  });

  it("explains a fallback in plain words", async () => {
    const user = userEvent.setup();
    await openDuel({
      waitingOn: "none",
      aiLabel: "Rule-based AI (LLM fallback: timeout)",
      decisions: [
        decision({
          managerLabel: "Rule-based AI (LLM fallback: timeout)",
          provenance: "ai_rule_based_fallback",
          fallbackReason: "timeout",
        }),
      ],
    });
    expect(screen.getByTestId("pane-title-ai").textContent).toBe("Rule-based AI (LLM fallback: timed out)");
    await user.click(await screen.findByRole("tab", { name: "What happened" }));
    expect((await screen.findByTestId("provenance-badge")).textContent).toBe("Rule-based AI (LLM fallback: timed out)");
    expect(screen.getByTestId("fallback-reason").textContent).toContain("took too long");
  });

  it("sends moves and the surprise to the duel endpoints with the exact body", async () => {
    const user = userEvent.setup();
    const calls = await openDuel({ waitingOn: "human" });
    await user.click((await screen.findAllByRole("button", { name: "Add to my moves" }))[0] as HTMLElement);
    await user.click(await screen.findByRole("button", { name: "Lock in decisions" }));
    const submit = await waitFor(() => {
      const c = calls.find((x) => x.path === "/duels/d1/commands");
      if (!c) throw new Error("not yet");
      return c;
    });
    expect(submit.method).toBe("POST");
    expect(submit.body).toEqual({
      windowTick: HEAD,
      commands: [{ kind: "place_purchase_order", sku: 0, role: "primary", qty: 100 }],
      expectedHeadTick: HEAD,
    });
    expect(submit.headers["Idempotency-Key"]).toMatch(/[0-9a-f-]{36}/);
    expect(calls.some((c) => /^\/branches\/[^/]+\/commands$/.test(c.path))).toBe(false);

    await user.click(await screen.findByRole("button", { name: "Continue" }));
    const end = await waitFor(() => {
      const c = calls.find((x) => x.path === "/duels/d1/decision-window/end");
      if (!c) throw new Error("not yet");
      return c;
    });
    expect(end.body).toEqual({ windowTick: HEAD, expectedHeadTick: HEAD });
    expect(calls.some((c) => /^\/branches\/[^/]+\/decision-window\/end$/.test(c.path))).toBe(false);
    usePlaybackStore.getState().pause();
  });

  it("adds a surprise to both businesses through the duel endpoint", async () => {
    const user = userEvent.setup();
    const calls = await openDuel({ waitingOn: "none" });
    await user.click(await screen.findByRole("button", { name: /Add a surprise to both businesses/ }));
    await user.click(screen.getByRole("button", { name: "Add this surprise" }));
    const inject = await waitFor(() => {
      const c = calls.find((x) => x.path === "/duels/d1/disruptions");
      if (!c) throw new Error("not yet");
      return c;
    });
    expect(inject.body).toEqual({
      kind: "supplier_outage",
      startTick: HEAD + 1,
      durationTicks: 12,
      target: -1,
      expectedHeadTick: HEAD,
      role: "primary",
    });
    expect(calls.some((c) => /^\/branches\/[^/]+\/disruptions$/.test(c.path))).toBe(false);
  });

  it("shows both hashes and the LLM counters in Engineering Mode only", async () => {
    await openDuel({ waitingOn: "none" });
    expect(screen.queryByTestId("duel-engineering")).toBeNull();
    useUiStore.setState({ engineering: true });
    const panel = await screen.findByTestId("duel-engineering");
    await waitFor(() => {
      expect(panel.textContent).toContain(`state-bh-${HEAD}`);
      expect(panel.textContent).toContain(`state-ba-${HEAD}`);
    });
    expect(screen.getByTestId("llm-usage").textContent).toContain("calls made 3, remaining 37");
  });
});
