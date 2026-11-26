// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ForkSequence } from "./ForkSequence";
import {
  FORK_SEQUENCE_MS,
  INITIAL_FORK_SEQUENCE,
  caption,
  forkSequenceReducer,
  prefersReducedMotion,
  type ForkSequenceState,
} from "./forkSequenceMachine";

const info = { whenLabel: "Thursday, 4:00 AM" };

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("forkSequenceReducer", () => {
  it("does not start when the fork is only requested", () => {
    expect(forkSequenceReducer(INITIAL_FORK_SEQUENCE, { type: "fork_requested" })).toEqual({ phase: "idle" });
  });
  it("starts when the server has returned", () => {
    expect(forkSequenceReducer(INITIAL_FORK_SEQUENCE, { type: "server_returned", info, reducedMotion: false })).toEqual({ phase: "playing", info });
  });
  it("shows a static caption under reduced motion", () => {
    expect(forkSequenceReducer(INITIAL_FORK_SEQUENCE, { type: "server_returned", info, reducedMotion: true }).phase).toBe("static");
  });
  it("ends on skip or finish", () => {
    const playing: ForkSequenceState = { phase: "playing", info };
    expect(forkSequenceReducer(playing, { type: "skip" })).toEqual({ phase: "idle" });
    expect(forkSequenceReducer(playing, { type: "finished" })).toEqual({ phase: "idle" });
  });
  it("reads the reduced-motion preference", () => {
    vi.stubGlobal("matchMedia", () => ({ matches: true }));
    expect(prefersReducedMotion()).toBe(true);
  });
});

describe("ForkSequence", () => {
  it("renders nothing while idle", () => {
    render(<ForkSequence state={INITIAL_FORK_SEQUENCE} onDone={() => undefined} />);
    expect(screen.queryByTestId("fork-sequence")).toBeNull();
  });

  it("plays with the caption, two lanes and no numbers, then finishes by itself", () => {
    vi.useFakeTimers();
    const done = vi.fn();
    render(<ForkSequence state={{ phase: "playing", info }} onDone={done} />);
    expect(screen.getByRole("status").textContent).toBe("Same history up to Thursday, 4:00 AM. Different futures from here.");
    expect(screen.getByTestId("fork-sequence").textContent).toContain("Original path");
    expect(screen.getByTestId("fork-sequence").textContent).toContain("Your alternative");
    expect(document.querySelector(".fork-lanes")).not.toBeNull();
    expect(FORK_SEQUENCE_MS).toBeGreaterThanOrEqual(1200);
    expect(FORK_SEQUENCE_MS).toBeLessThanOrEqual(1600);
    act(() => {
      vi.advanceTimersByTime(FORK_SEQUENCE_MS - 1);
    });
    expect(done).not.toHaveBeenCalled();
    act(() => {
      vi.advanceTimersByTime(2);
    });
    expect(done).toHaveBeenCalledTimes(1);
  });

  it("can be skipped with Escape, a click or the Skip button", () => {
    const done = vi.fn();
    render(<ForkSequence state={{ phase: "playing", info }} onDone={done} />);
    fireEvent.keyDown(document, { key: "Escape" });
    fireEvent.click(screen.getByTestId("fork-sequence"));
    fireEvent.click(screen.getByRole("button", { name: /Skip/ }));
    expect(done).toHaveBeenCalledTimes(3);
  });

  it("is static, without animation classes, under reduced motion", () => {
    render(<ForkSequence state={{ phase: "static", info }} onDone={() => undefined} />);
    expect(screen.getByTestId("fork-sequence").dataset.phase).toBe("static");
    expect(screen.getByRole("status").textContent).toBe(caption(info));
    expect(document.querySelector(".fork-lanes, .fork-trunk, .fork-overlay, .fork-caption")).toBeNull();
  });
});
