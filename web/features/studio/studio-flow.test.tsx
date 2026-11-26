// @vitest-environment jsdom
/**
 * Studio flow against a fake API: create (open) → play → pause at a window →
 * rewind → fork → submit the demo Command → end window → play to the horizon.
 * Only fetch is faked; the stores, playback controller, page and feature
 * components are real. The diorama (PixiJS) and the React Flow tree are replaced
 * by minimal stand-ins because jsdom has no canvas.
 */
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { Branch, DemoPlan, TickFrame } from "@/lib/api/client";
import { selectActiveBranch, useStudioStore } from "@/stores/studio";
import { usePlaybackStore } from "@/stores/playback";
import { saveDemoPlan } from "./demoPlanStorage";
import { StudioPage } from "./StudioPage";

const shownTicks: (number | null)[] = [];

vi.mock("@/features/world-view/WorldView", () => ({
  WorldView: ({ frame }: { frame: TickFrame | null }) => {
    shownTicks.push(frame === null ? null : frame.tick);
    return <div data-testid="worldview" data-tick={frame === null ? "none" : String(frame.tick)} />;
  },
}));

vi.mock("@/features/branches/BranchTree", () => ({
  BranchTree: ({
    branches,
    onSelect,
  }: {
    branches: Branch[];
    activeBranchId: string | null;
    onSelect: (id: string) => void;
  }) => (
    <ul>
      {branches.map((b) => (
        <li key={b.id}>
          <button type="button" onClick={() => onSelect(b.id)}>
            {`branch ${b.label}`}
          </button>
        </li>
      ))}
    </ul>
  ),
}));

vi.mock("next/link", () => ({
  default: ({ href, children }: { href: string; children: ReactNode }) => <a href={href}>{children}</a>,
}));

const HORIZON = 12;
const WINDOWS = [4, 8];
const WORLD_ID = "w1";

const plan: DemoPlan = {
  scenario: "Black Friday Meltdown",
  seed: 2026,
  crisisTick: 3,
  forkTick: 4,
  commands: [{ kind: "place_purchase_order", skuIndex: 2, skuId: "speaker", role: "backup", qty: 315 }],
  reasons: ["fixture reason"],
};

function frameAt(tick: number): TickFrame {
  const markers = WINDOWS.includes(tick) ? [{ kind: "decision_window", windowReason: "cadence" }] : [];
  return {
    tick,
    stateHash: `hash-${tick}`,
    skus: [{ sku: 2, onHand: 100 - tick }],
    markers,
    disruptions: [],
  } as unknown as TickFrame;
}

function metricsAt(tick: number) {
  return {
    tick,
    currency: "USD",
    disclaimer: "Synthetic fixture disclaimer.",
    metrics: {
      cashCents: 1_000_000 + tick * 100,
      netProfitCents: tick * 10,
      serviceLevelBps: 9000,
      backlogOrders: 1,
      backlogUnits: 2,
    },
  };
}

interface FakeBranch extends Branch {
  windowOpen: boolean;
}

interface Call {
  method: string;
  path: string;
  body: unknown;
}

function toWire(b: FakeBranch): Branch {
  const wire: Partial<FakeBranch> = { ...b };
  delete wire.windowOpen;
  return wire as Branch;
}

function makeFakeApi() {
  const calls: Call[] = [];
  const branches = new Map<string, FakeBranch>();
  const iso = "2026-01-01T00:00:00Z";
  branches.set("r1", {
    id: "r1",
    worldId: WORLD_ID,
    parentBranchId: null,
    forkTick: 0,
    headTick: 0,
    label: "Root",
    managerKind: "human",
    createdAt: iso,
    windowOpen: false,
  } as FakeBranch);

  const world = {
    id: WORLD_ID,
    name: "Black Friday Meltdown",
    seed: 2026,
    engineVersion: "1",
    scenarioHash: "abc",
    createdAt: iso,
    currency: "USD",
    scenario: { name: "Black Friday Meltdown", horizonTicks: HORIZON, startClock: "Thu 00:00", decisionCadenceTicks: 4, commandBudget: 5, skus: [], scoreWeights: {} },
    assumptions: [
      { path: "demand.baseUnits", value: 10, unit: "units/h", default: 10, min: 1, max: 100, rationale: "synthetic", overridden: false },
    ],
  };

  const observation = (b: FakeBranch, tick: number) => ({
    tick,
    cashCents: 1_000_000,
    skus: [0, 2].map((sku) => ({
      sku,
      id: sku === 2 ? "speaker" : "widget",
      onHand: 10,
      inventoryValueCents: 0,
      referencePriceCents: 2000,
      priceCents: 2000,
      marketingCents: 0,
      inboundPOs: 0,
      inboundUnits: 0,
      suppliers: [],
    })),
    openOrders: [],
    purchaseOrders: [],
    workforce: {},
    disruptions: [],
    metrics: { backlogOrders: 1 },
    window: { open: b.windowOpen && tick === b.headTick, reasons: [], cadenceTicks: 4, commandBudget: 5, horizonTicks: HORIZON, ticksRemaining: HORIZON - tick },
  });

  const json = (status: number, body: unknown): Response =>
    ({
      ok: status >= 200 && status < 300,
      status,
      statusText: "",
      json: async () => body,
    }) as unknown as Response;

  async function fetchImpl(input: string | URL | Request, init?: RequestInit): Promise<Response> {
    const url = new URL(String(input));
    const path = url.pathname.replace(/^\/api\/v1/, "");
    const method = init?.method ?? "GET";
    const body: unknown = typeof init?.body === "string" ? JSON.parse(init.body) : undefined;
    calls.push({ method, path, body });
    const q = (k: string): number => Number(url.searchParams.get(k));

    if (method === "GET" && path === `/worlds/${WORLD_ID}`) return json(200, world);
    if (method === "GET" && path === `/worlds/${WORLD_ID}/branches`) {
      return json(200, { branches: [...branches.values()].map(toWire) });
    }
    const m = /^\/branches\/([^/]+)(?:\/(.+))?$/.exec(path);
    const b = m ? branches.get(m[1] ?? "") : undefined;
    if (!m || !b) return json(404, { type: "urn:parallel:problem:not-found", title: "Not found", status: 404 });
    const sub = m[2];

    if (method === "GET" && sub === "tick-frames") {
      const from = q("from");
      const to = Math.min(q("to"), b.headTick);
      const frames: TickFrame[] = [];
      for (let t = from; t <= to; t++) frames.push(frameAt(t));
      return json(200, { branchId: b.id, from, to, frames });
    }
    if (method === "GET" && sub === "state") {
      return json(200, { branchId: b.id, tick: q("tick"), stateHash: "h", observation: observation(b, q("tick")), currency: "USD", disclaimer: "d" });
    }
    if (method === "GET" && sub === "metrics") return json(200, { branchId: b.id, ...metricsAt(q("tick")) });
    if (method === "POST" && sub === "advance") {
      const req = body as { expectedHeadTick: number; runToTick?: number };
      if (req.expectedHeadTick !== b.headTick) {
        return json(409, { type: "urn:parallel:problem:stale-head", title: "Stale head", status: 409 });
      }
      const nextWindow = WINDOWS.find((w) => w > b.headTick);
      const target = nextWindow ?? HORIZON;
      const frames: TickFrame[] = [];
      for (let t = b.headTick + 1; t <= target; t++) frames.push(frameAt(t));
      b.headTick = target;
      b.windowOpen = nextWindow !== undefined;
      return json(200, {
        headTick: target,
        stateHash: `hash-${target}`,
        paused: nextWindow !== undefined ? "decision_window" : "horizon",
        windowReasons: nextWindow !== undefined ? ["cadence"] : [],
        frames,
        currency: "USD",
        disclaimer: "d",
      });
    }
    if (method === "POST" && sub === "fork") {
      const req = body as { atTick: number; label: string };
      const id = `c${branches.size}`;
      const child: FakeBranch = {
        id,
        worldId: WORLD_ID,
        parentBranchId: b.id,
        forkTick: req.atTick,
        headTick: req.atTick,
        label: req.label,
        managerKind: "human",
        createdAt: iso,
        windowOpen: true,
      } as FakeBranch;
      branches.set(id, child);
      return json(201, toWire(child));
    }
    if (method === "POST" && sub === "commands") {
      const req = body as { commands: unknown[] };
      return json(200, {
        accepted: req.commands.map((command, i) => ({ seq: i + 1, effectiveTick: b.headTick + 1, command, provenance: "human" })),
        rejected: [],
        budgetRemaining: 4,
        duplicate: false,
      });
    }
    if (method === "POST" && sub === "decision-window/end") {
      b.windowOpen = false;
      return json(204, undefined);
    }
    return json(404, { type: "urn:parallel:problem:not-found", title: "Not found", status: 404 });
  }

  return { calls, fetchImpl, branches };
}

/** Lets pending promises and effects finish. Real setTimeout is not faked. */
async function settle(): Promise<void> {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

/** Fires the faked playback interval `n` times, letting each tick's requests finish. */
async function pump(n: number, ms = 32): Promise<void> {
  for (let i = 0; i < n; i++) {
    await act(async () => {
      vi.advanceTimersByTime(ms);
    });
    await settle();
  }
}

function press(key: string, init: KeyboardEventInit = {}): void {
  fireEvent.keyDown(document.body, { key, ...init });
}

const clock = (): string => screen.getByTestId("hud-clock").textContent ?? "";
const advanceCalls = (calls: Call[]): Call[] => calls.filter((c) => c.method === "POST" && c.path.endsWith("/advance"));

beforeEach(() => {
  shownTicks.length = 0;
  vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
  window.sessionStorage.clear();
  window.history.replaceState(null, "", "/");
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("studio flow", () => {
  it("runs open → play → pause at window → rewind → fork → command → end window → horizon", async () => {
    const api = makeFakeApi();
    vi.stubGlobal("fetch", vi.fn(api.fetchImpl));
    saveDemoPlan(WORLD_ID, plan);

    // Invariant: whenever frames of the active branch exist, the displayed tick has a frame.
    const violations: string[] = [];
    const unsubscribe = useStudioStore.subscribe((s) => {
      if (s.activeBranchId === null) return;
      const frames = s.framesByBranch[s.activeBranchId];
      if (frames !== undefined && Object.keys(frames).length > 0 && frames[s.selectedTick] === undefined) {
        violations.push(`tick ${s.selectedTick} displayed without a frame on ${s.activeBranchId}`);
      }
    });

    render(<StudioPage worldId={WORLD_ID} branchId="r1" />);
    await settle();

    // Open: world, branches, frames, state and metrics are requested; hints and disclaimer are visible.
    expect(api.calls.slice(0, 2).map((c) => c.path).sort()).toEqual([`/worlds/${WORLD_ID}`, `/worlds/${WORLD_ID}/branches`]);
    expect(screen.getByTestId("plan-hint").textContent).toContain("Fork at tick 4");
    expect(screen.getByTestId("plan-hint").textContent).toContain("computed by the server");
    expect(screen.getByTestId("world-disclaimer").textContent).toContain("Synthetic simulation output");
    expect(clock()).toContain("tick 0");
    expect(screen.getByTestId("no-window")).toBeTruthy();

    // Assumptions are one action away and show bounds and rationale.
    fireEvent.click(screen.getByRole("tab", { name: "Assumptions" }));
    expect(screen.getByTestId("assumption").textContent).toContain("range 1 to 100");
    expect(screen.getByTestId("assumption").textContent).toContain("synthetic");
    fireEvent.click(screen.getByRole("tab", { name: "Decisions" }));

    // Play at 16x: the first advance runs to the window at tick 4 and playback pauses there.
    fireEvent.click(screen.getByRole("button", { name: "16x" }));
    fireEvent.click(screen.getByRole("button", { name: "Play" }));
    await pump(10);
    expect(usePlaybackStore.getState().pauseReason).toBe("decision_window");
    expect(clock()).toContain("tick 4");
    expect(advanceCalls(api.calls).map((c) => c.body)).toEqual([{ expectedHeadTick: 0 }]);
    expect(screen.getByTestId("obs-budget").textContent).toBe("5 commands");

    // End the root window, then play on to the window at tick 8.
    fireEvent.click(screen.getByRole("button", { name: "End decision window" }));
    await settle();
    fireEvent.click(screen.getByRole("button", { name: "Play" }));
    await pump(10);
    expect(clock()).toContain("tick 8");
    expect(usePlaybackStore.getState().pauseReason).toBe("decision_window");
    expect(advanceCalls(api.calls)).toHaveLength(2);

    // Rewind to the previous window with Shift+Left: a read, never an advance.
    press("ArrowLeft", { shiftKey: true });
    await settle();
    expect(clock()).toContain("tick 4");
    expect(advanceCalls(api.calls)).toHaveLength(2);
    expect(useStudioStore.getState().branches.find((b) => b.id === "r1")?.headTick).toBe(8);

    // Fork at the displayed tick; the child opens its fork window.
    fireEvent.click(screen.getByRole("button", { name: "Fork at tick 4" }));
    await settle();
    const forkCall = api.calls.find((c) => c.path === "/branches/r1/fork");
    expect(forkCall?.body).toMatchObject({ atTick: 4 });
    const child = selectActiveBranch(useStudioStore.getState());
    expect(child?.parentBranchId).toBe("r1");
    expect(child?.headTick).toBe(4);
    expect(window.location.search).toContain("branch=c1");
    expect(screen.getByTestId("obs-budget").textContent).toBe("5 commands");
    expect(screen.getByTestId("hud-parent")).toBeTruthy();

    // Submit the demo plan command.
    fireEvent.change(screen.getByLabelText("PO SKU"), { target: { value: "2" } });
    fireEvent.change(screen.getByLabelText("Supplier"), { target: { value: "backup" } });
    fireEvent.change(screen.getByLabelText("Quantity"), { target: { value: "315" } });
    fireEvent.click(screen.getByRole("button", { name: "Add order" }));
    fireEvent.click(screen.getByRole("button", { name: "Submit" }));
    await settle();
    const submit = api.calls.find((c) => c.path === "/branches/c1/commands");
    expect(submit?.body).toMatchObject({
      windowTick: 4,
      commands: [{ kind: "place_purchase_order", sku: 2, role: "backup", qty: 315 }],
    });
    expect(screen.getByTestId("accepted-command").textContent).toContain("effective Tick 5");
    expect(document.querySelector('[data-marker="input"]')).not.toBeNull();

    // End the fork window, then play to the next window and on to the horizon.
    fireEvent.click(screen.getByRole("button", { name: "End decision window" }));
    await settle();
    expect(api.calls.some((c) => c.path === "/branches/c1/decision-window/end")).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Play" }));
    await pump(10);
    expect(clock()).toContain("tick 8");
    fireEvent.click(screen.getByRole("button", { name: "End decision window" }));
    await settle();
    fireEvent.click(screen.getByRole("button", { name: "Play" }));
    await pump(10);
    expect(clock()).toContain("tick 12");
    expect(usePlaybackStore.getState().pauseReason).toBe("horizon");
    expect(screen.getByTestId("playback-status").textContent).toContain("Reached the horizon");

    const childAdvances = advanceCalls(api.calls).filter((c) => c.path === "/branches/c1/advance");
    expect(childAdvances.map((c) => c.body)).toEqual([{ expectedHeadTick: 4 }, { expectedHeadTick: 8 }]);
    expect(api.branches.get("r1")?.headTick).toBe(8);

    // Playback never displayed a tick without a frame.
    expect(violations).toEqual([]);
    expect(shownTicks.every((t) => t !== null)).toBe(true);
    unsubscribe();
  });

  it("shows the API error message when the world cannot be loaded", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: false,
        status: 404,
        statusText: "Not Found",
        json: async () => ({ type: "urn:parallel:problem:not-found", title: "World not found", status: 404, detail: "no such world" }),
      })),
    );
    render(<StudioPage worldId="missing" />);
    await settle();
    expect(screen.getByRole("alert").textContent).toContain("World not found: no such world");
  });
});
