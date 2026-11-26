// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Timeline, markersFromFrames, useHighWater, type TimelineProps } from "./Timeline";
import { renderHook } from "@testing-library/react";
import { createPlaybackStore, type PlaybackClient } from "@/stores/playback";
import { createStudioStore } from "@/stores/studio";
import type { Branch, TickFrame } from "@/lib/api/client";

afterEach(cleanup);

function props(over: Partial<TimelineProps> = {}): TimelineProps {
  return {
    horizonTicks: 120,
    headTick: 60,
    selectedTick: 10,
    frameMarkers: [
      { tick: 8, kind: "decision_window", windowReason: "cadence" },
      { tick: 30, kind: "disruption_onset", disruptionKind: "supplier_outage" },
    ],
    inputs: [
      { effectiveTick: 9, provenance: "human", kind: "place_purchase_order" },
      { effectiveTick: 9, provenance: "ai_rule_based_fallback", kind: "set_price" },
    ],
    forkPoints: [{ tick: 8, label: "Alternative future 1" }],
    startClock: "Thu 00:00",
    revealedUpTo: 60,
    onSelectTick: () => {},
    ...over,
  };
}

describe("Timeline", () => {
  it("marks shared history only when asked", () => {
    const { rerender } = render(<Timeline {...props()} />);
    expect(screen.queryByTestId("shared-history")).toBeNull();
    rerender(<Timeline {...props({ sharedUpTo: 8 })} />);
    expect(screen.getByTestId("shared-history")).toBeTruthy();
    expect(screen.getByTestId("timeline-readout").textContent).toContain("Same history until Thursday, 8:00 AM");
  });

  it("renders markers in plain language", () => {
    render(<Timeline {...props()} />);
    expect(screen.getAllByRole("button", { name: "Decision moment at Thursday, 8:00 AM" })).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Surprise at Friday, 6:00 AM: Supplier down" })).toBeTruthy();
    expect(screen.getByRole("button", { name: /Futures split at Thursday, 8:00 AM: Alternative future 1/ })).toBeTruthy();
    expect(screen.getAllByRole("button", { name: "Your move, takes effect at Thursday, 9:00 AM" }).length).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: /Move by Rule-based AI \(LLM fallback\)/ })).toBeTruthy();
    const bands = [...document.querySelectorAll("[data-day-band]")].map((b) => b.textContent);
    expect(bands).toEqual(["Thursday", "Friday", "Saturday", "Sunday", "Monday"]);
    expect(document.body.textContent).not.toMatch(/tick/i);
  });

  it("does not draw markers beyond what the displayed reality has already reached", () => {
    render(<Timeline {...props({ revealedUpTo: 20 })} />);
    expect(screen.getByRole("button", { name: /Decision moment at Thursday, 8:00 AM/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Surprise at/ })).toBeNull();
    expect(document.querySelectorAll('[data-marker="disruption"]')).toHaveLength(0);
  });

  it("shows tick numbers only in Engineering Mode", () => {
    const { rerender } = render(<Timeline {...props()} />);
    expect(screen.getByTestId("timeline-readout").textContent).toBe("Showing Thursday, 10:00 AM");
    rerender(<Timeline {...props({ engineering: true })} />);
    expect(screen.getByTestId("timeline-readout").textContent).toContain("tick 10");
  });

  it("exposes the displayed tick as a keyboard slider and selects with arrow keys", async () => {
    const onSelect = vi.fn();
    render(<Timeline {...props({ onSelectTick: onSelect })} />);
    const slider = screen.getByRole("slider");
    expect(slider.getAttribute("aria-valuenow")).toBe("10");
    expect(slider.getAttribute("aria-valuemax")).toBe("60");
    const user = userEvent.setup();
    slider.focus();
    await user.keyboard("{ArrowRight}{ArrowLeft}{End}{Home}");
    expect(onSelect.mock.calls.map((c) => c[0])).toEqual([11, 9, 60, 0]);
  });

  it("clamps selection to the Head_Tick and 0", async () => {
    const onSelect = vi.fn();
    render(<Timeline {...props({ selectedTick: 60, onSelectTick: onSelect })} />);
    screen.getByRole("slider").focus();
    await userEvent.setup().keyboard("{ArrowRight}");
    expect(onSelect).toHaveBeenCalledWith(60);
  });

  it("disables cursor motion under prefers-reduced-motion via motion-reduce", () => {
    render(<Timeline {...props()} />);
    expect(screen.getByTestId("cursor").className).toContain("motion-reduce:transition-none");
  });

  it("selecting a tick reads state and never advances", async () => {
    const studio = createStudioStore();
    const branch: Branch = {
      id: "b1", worldId: "w", parentBranchId: null, forkTick: 0, headTick: 20,
      label: "Main", managerKind: "human", createdAt: "2026-01-01T00:00:00Z",
    };
    studio.getState().openWorld({ worldId: "w", horizonTicks: 120, branches: [branch], activeBranchId: "b1", selectedTick: 10 });
    const frame = (tick: number) => ({ tick, markers: [] }) as unknown as TickFrame;
    studio.getState().addFrames("b1", [frame(9), frame(10), frame(11)]);
    const client = {
      advanceBranch: vi.fn(),
      listTickFrames: vi.fn(async () => ({ frames: [] })),
      getBranch: vi.fn(),
      getBranchState: vi.fn(async () => ({})),
      getBranchMetrics: vi.fn(async () => ({})),
    };
    const playback = createPlaybackStore({
      studio,
      client: client as unknown as PlaybackClient,
      scheduler: { setInterval: () => 0, clearInterval: () => {} },
    });
    render(<Timeline {...props({ headTick: 20, onSelectTick: (t) => void playback.getState().selectTick(t) })} />);
    screen.getByRole("slider").focus();
    await userEvent.setup().keyboard("{ArrowRight}");
    await waitFor(() => expect(studio.getState().selectedTick).toBe(11));
    expect(client.advanceBranch).not.toHaveBeenCalled();
    expect(client.getBranchState).toHaveBeenCalledWith("b1", 11);
    expect(studio.getState().branches[0]?.headTick).toBe(20);
  });

  it("high-water mark only grows while scrubbing back", () => {
    const { result, rerender } = renderHook(({ t }) => useHighWater("b1", t), { initialProps: { t: 5 } });
    expect(result.current).toBe(5);
    rerender({ t: 12 });
    expect(result.current).toBe(12);
    rerender({ t: 3 });
    expect(result.current).toBe(12);
  });

  it("markersFromFrames flattens frame markers in tick order", () => {
    const frames = {
      5: { tick: 5, markers: [{ kind: "disruption_onset", disruptionKind: "demand_surge" }] },
      2: { tick: 2, markers: [{ kind: "decision_window", windowReason: "fork" }] },
    } as unknown as Record<number, TickFrame>;
    expect(markersFromFrames(frames).map((m) => [m.tick, m.kind])).toEqual([
      [2, "decision_window"],
      [5, "disruption_onset"],
    ]);
  });
});
