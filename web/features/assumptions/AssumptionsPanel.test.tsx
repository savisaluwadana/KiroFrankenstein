// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { World } from "@/lib/api/client";
import { AssumptionsPanel } from "./AssumptionsPanel";
import { assumptionLabel, displayLabel } from "./labels";

const world = {
  id: "11111111-1111-4111-8111-111111111111",
  scenario: { startClock: "Thu 00:00", horizonTicks: 120, skus: [] },
  assumptions: [],
} as unknown as World;

const base = {
  worldId: world.id,
  branchId: "b1",
  headTick: 54,
  horizonTicks: 120,
  assumptions: [
    {
      path: "workforce.regularWorkers",
      value: 12,
      unit: "people",
      default: 20,
      min: 1,
      max: 200,
      rationale: "synthetic, illustrative: crew size",
      overridden: true,
    },
    {
      path: "mystery.path",
      value: 3,
      unit: "u",
      default: 3,
      min: 0,
      max: 9,
      rationale: "why",
      overridden: false,
    },
  ],
  scriptRevealed: false,
  scriptComplete: false,
  disclaimer: "x",
};

describe("AssumptionsPanel", () => {
  const urls: string[] = [];
  beforeEach(() => {
    urls.length = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        urls.push(url);
        if (url.includes("revealScript=true")) {
          return new Response(
            JSON.stringify({
              ...base,
              scriptRevealed: true,
              disruptionScript: [{ kind: "demand_surge", onsetTick: 48, durationTicks: 24, magnitudeBps: 25000 }],
            }),
            { status: 200 },
          );
        }
        return new Response(JSON.stringify(base), { status: 200 });
      }),
    );
  });
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("renders server values, range, rationale and the changed flag, with friendly labels", async () => {
    render(<AssumptionsPanel world={world} branchId="b1" />);
    const rows = await screen.findAllByTestId("assumption");
    await waitFor(() => expect(screen.getAllByTestId("assumption").length).toBe(2));
    const first = within(screen.getAllByTestId("assumption")[0] as HTMLElement);
    expect(rows.length).toBeGreaterThan(0);
    expect(first.getByText("Packing staff")).toBeTruthy();
    expect(first.getByText(/12 people \(default 20\) · range 1 to 200/)).toBeTruthy();
    expect(first.getByText("crew size")).toBeTruthy();
    expect(first.getByTestId("assumption-changed").textContent).toBe("Changed from default");
    // No glossary entry: the raw path is for Engineering Mode only.
    const second = within(screen.getAllByTestId("assumption")[1] as HTMLElement);
    expect(second.queryByText("mystery.path")).toBeNull();
    expect(second.getByText("Other setting")).toBeTruthy();
    expect(screen.getByText(/Changing assumptions starts a new business/)).toBeTruthy();
  });

  it("hides the script by default and shows only what the reveal endpoint returns", async () => {
    render(<AssumptionsPanel world={world} branchId="b1" />);
    await screen.findAllByTestId("assumption");
    expect(screen.queryByTestId("revealed-script")).toBeNull();
    expect(urls.every((u) => !u.includes("revealScript"))).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Show the surprises that have already happened" }));
    const box = await screen.findByTestId("revealed-script");
    expect(urls.some((u) => u.includes("revealScript=true") && u.includes("branchId=b1"))).toBe(true);
    const items = within(box).getAllByTestId("revealed-surprise");
    expect(items).toHaveLength(1);
    expect(items[0]?.textContent).toMatch(/Orders surging/);
    expect(box.textContent).toMatch(/What comes next stays hidden/);
  });

  it("labels fall back to the path only in Engineering Mode", () => {
    expect(assumptionLabel("openingCashCents")).toBe("Starting cash");
    expect(displayLabel("nope.x", false)).toBe("Other setting");
    expect(displayLabel("nope.x", true)).toBe("nope.x");
  });
});
