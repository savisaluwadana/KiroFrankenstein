// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Hud } from "./Hud";
import { metricExplanation } from "@/lib/glossary";
import type { Metrics, MetricsResponse } from "@/lib/api/client";

afterEach(cleanup);

function resp(tick: number, m: Partial<Metrics>): MetricsResponse {
  return {
    branchId: "b",
    tick,
    stateHash: "h",
    currency: "USD",
    disclaimer: "Synthetic simulation output under the stated assumptions. Not a forecast or guarantee of real-world results.",
    metrics: {
      cashCents: 1234567, netProfitCents: 50000, serviceLevelBps: 9250, backlogOrders: 4, backlogUnits: 9, lateBacklogOrders: 1,
      ...m,
    } as Metrics,
  };
}

describe("Hud", () => {
  it("shows plain-language figures formatted from API integers", () => {
    render(<Hud metrics={resp(30, {})} startClock="Thu 00:00" />);
    expect(screen.getByTestId("hud-clock").textContent).toBe("Friday, 6:00 AM");
    expect(screen.getByTestId("hud-cash").textContent).toBe("$12,345.67");
    expect(screen.getByTestId("hud-net-profit").textContent).toBe("+$500.00");
    expect(screen.getByTestId("hud-service-level").textContent).toBe("92.5%");
    expect(screen.getByTestId("hud-backlog").textContent).toBe("4 orders");
    for (const label of ["Cash", "Profit so far", "Orders delivered on time", "Orders waiting"]) {
      expect(screen.getByText(label)).toBeTruthy();
    }
    expect(document.body.textContent).not.toMatch(/tick|backlog|service level|net profit/i);
  });

  it("shows the tick number only in Engineering Mode", () => {
    const { rerender } = render(<Hud metrics={resp(30, {})} startClock="Thu 00:00" />);
    expect(screen.getByTestId("hud-clock").textContent).not.toMatch(/tick/);
    rerender(<Hud metrics={resp(30, {})} startClock="Thu 00:00" engineering />);
    expect(screen.getByTestId("hud-clock").textContent).toContain("tick 30");
  });

  it("shows the clock before metrics load", () => {
    render(<Hud metrics={null} tick={0} startClock="Thu 00:00" />);
    expect(screen.getByTestId("hud-clock").textContent).toBe("Thursday, 12:00 AM");
  });

  it("each figure has a keyboard-accessible explanation that is not a modal", async () => {
    const user = userEvent.setup();
    render(<Hud metrics={resp(30, {})} startClock="Thu 00:00" />);
    const info = screen.getByRole("button", { name: "About Profit so far" });
    expect(info.getAttribute("aria-expanded")).toBe("false");
    info.focus();
    await user.keyboard("{Enter}");
    expect(info.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByRole("note").textContent).toBe(metricExplanation("netProfit"));
    expect(screen.queryByRole("dialog")).toBeNull();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("note")).toBeNull();
    expect(document.activeElement).toBe(info);
  });

  it("explains orders waiting with the late count from the server", async () => {
    render(<Hud metrics={resp(30, { lateBacklogOrders: 3 })} startClock="Thu 00:00" />);
    await userEvent.setup().click(screen.getByRole("button", { name: "About Orders waiting" }));
    expect(screen.getByRole("note").textContent).toContain("3 of them are already late");
  });

  it("shows the other reality's values and labelled display differences for the same tick", () => {
    render(
      <Hud
        metrics={resp(30, { netProfitCents: 50000, serviceLevelBps: 9250, backlogOrders: 4 })}
        parentMetrics={resp(30, { netProfitCents: 80050, serviceLevelBps: 9000, backlogOrders: 6 })}
        parentLabel="Original path"
        startClock="Thu 00:00"
      />,
    );
    expect(screen.getByTestId("parent-net-profit").textContent).toContain("+$800.50");
    expect(screen.getByTestId("hud-diff").textContent).toContain("shown for comparison");
    expect(screen.getByTestId("diff-net-profit").textContent).toContain("-$300.50");
    expect(screen.getByTestId("diff-service-level").textContent).toContain("+2.5 points");
    expect(screen.getByTestId("diff-backlog").textContent).toContain("-2");
  });

  it("omits the comparison when the other reality's Metrics are for another tick", () => {
    render(<Hud metrics={resp(30, {})} parentMetrics={resp(29, {})} startClock="Thu 00:00" />);
    expect(screen.queryByTestId("hud-parent")).toBeNull();
  });

  it("makes money figures buttons when a handler is given", async () => {
    const onClick = vi.fn();
    render(<Hud metrics={resp(1, {})} onMoneyClick={onClick} startClock="Thu 00:00" />);
    await userEvent.setup().click(screen.getByTestId("hud-net-profit"));
    expect(onClick).toHaveBeenCalledWith("net_profit");
    await userEvent.setup().click(screen.getByTestId("hud-cash"));
    expect(onClick).toHaveBeenCalledWith("cash");
  });

  it("handles negative profit with integer formatting", () => {
    render(<Hud metrics={resp(1, { netProfitCents: -5 })} startClock="Thu 00:00" />);
    expect(screen.getByTestId("hud-net-profit").textContent).toBe("-$0.05");
  });
});
