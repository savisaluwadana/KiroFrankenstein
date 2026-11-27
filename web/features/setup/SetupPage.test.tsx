// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";

const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push }) }));

import { SetupPage } from "./SetupPage";

function asm(path: string, value: number, min: number, max: number) {
  return { path, value, unit: "u", default: value, min, max, rationale: "synthetic, illustrative: test", overridden: false };
}

const response = (id: string) => ({
  world: {
    id,
    name: "Black Friday Meltdown",
    scenario: { name: "x", skus: [{ index: 0, id: "headphones" }] },
    assumptions: [
      asm("workforce.regularWorkers", 20, 1, 200),
      asm("workforce.unitsPerWorkerHour", 8, 1, 60),
      asm("skus.headphones.referencePriceCents", 5999, 100, 100000),
    ],
  },
  rootBranch: { id: `${id}-root` },
});

describe("SetupPage", () => {
  const bodies: unknown[] = [];
  beforeEach(() => {
    bodies.length = 0;
    push.mockReset();
    let n = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init: RequestInit) => {
        bodies.push(JSON.parse(String(init.body)));
        n += 1;
        return new Response(JSON.stringify(response(`w${n}`)), { status: 201 });
      }),
    );
  });
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("starts with the draft world when nothing changed", async () => {
    render(<SetupPage />);
    const start = await screen.findByRole("button", { name: "Start" });
    await waitFor(() => expect((start as HTMLButtonElement).disabled).toBe(false));
    const draftCalls = bodies.length;
    fireEvent.click(start);
    await waitFor(() => expect(push).toHaveBeenCalled());
    expect(bodies.length).toBe(draftCalls);
    expect(String(push.mock.calls[0]?.[0])).toMatch(/^\/worlds\/w\d\?branch=w\d-root$/);
  });

  it("sends only changed overrides and blocks out-of-bounds values", async () => {
    render(<SetupPage />);
    const staff = await screen.findByLabelText("Packing staff");
    fireEvent.change(staff, { target: { value: "500" } });
    const start = screen.getByRole("button", { name: "Start" }) as HTMLButtonElement;
    expect(start.disabled).toBe(true);
    fireEvent.change(staff, { target: { value: "25" } });
    const go = await screen.findByRole("button", { name: "Start with these settings" });
    fireEvent.click(go);
    await waitFor(() => expect(push).toHaveBeenCalled());
    const last = bodies[bodies.length - 1] as { overrides: Record<string, number> };
    expect(last.overrides).toEqual({ "workforce.regularWorkers": 25 });
  });

  it("shows the validator's message next to the rejected control", async () => {
    render(<SetupPage />);
    const staff = await screen.findByLabelText("Packing staff");
    fireEvent.change(staff, { target: { value: "25" } });
    const go = await screen.findByRole("button", { name: "Start with these settings" });
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            type: "urn:parallel:problem:invalid-scenario",
            title: "Invalid scenario",
            status: 400,
            issues: [{ field: "workforce.regularWorkers", message: "must be at most 24" }],
          }),
          { status: 400, headers: { "Content-Type": "application/problem+json" } },
        ),
      ),
    );
    fireEvent.click(go);
    const msg = await screen.findByTestId("server-issue");
    expect(msg.textContent).toBe("must be at most 24");
    expect(push).not.toHaveBeenCalled();
  });
});
