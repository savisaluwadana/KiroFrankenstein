// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { PlaybackControls } from "./PlaybackControls";
import { usePlaybackStore } from "@/stores/playback";

afterEach(() => {
  cleanup();
  usePlaybackStore.setState({ speed: 1, playing: false, pauseReason: "user" });
});

describe("PlaybackControls", () => {
  it("labels every control in plain words", () => {
    render(<PlaybackControls />);
    for (const name of ["Play", "Calm", "Quick", "Fast", "Previous moment", "Next moment", "Skip to next move", "Skip to next surprise"]) {
      expect(screen.getByRole("button", { name })).toBeTruthy();
    }
    expect(document.body.textContent).not.toMatch(/tick|\dx\b|disruption|window/i);
  });

  it("changes the pace through the store and marks the choice", async () => {
    render(<PlaybackControls />);
    await userEvent.setup().click(screen.getByRole("button", { name: "Fast" }));
    expect(usePlaybackStore.getState().speed).toBe(16);
    expect(screen.getByRole("button", { name: "Fast" }).getAttribute("aria-pressed")).toBe("true");
  });

  it("lists the keyboard shortcuts in a keyboard-accessible popover", async () => {
    const user = userEvent.setup();
    render(<PlaybackControls />);
    const help = screen.getByRole("button", { name: "About keyboard shortcuts" });
    help.focus();
    await user.keyboard("{Enter}");
    const list = screen.getByTestId("key-hints");
    expect(list.textContent).toContain("Play or pause");
    expect(list.textContent).toContain("Skip to the next surprise");
    await user.keyboard("{Escape}");
    expect(screen.queryByTestId("key-hints")).toBeNull();
  });

  it("explains every Input marker by provenance in the same popover", async () => {
    const user = userEvent.setup();
    render(<PlaybackControls />);
    await user.click(screen.getByRole("button", { name: "About keyboard shortcuts" }));
    const legend = screen.getByTestId("marker-legend");
    for (const text of ["Your move", "Move by Rule-based AI", "Move by LLM AI", "Move by Rule-based AI (LLM fallback)", "Surprise you added"]) {
      expect(legend.textContent).toContain(text);
    }
    expect(legend.querySelectorAll("[data-legend-provenance]")).toHaveLength(5);
  });
});
