// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Hud, formatBps, formatCents, formatSimClock } from "./Hud";
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
      cashCents: 1234567, netProfitCents: 50000, serviceLevelBps: 9250, backlogOrders: 4, backlogUnits: 9,
      ...m,
    } as Metrics,
  };
}

describe("Hud", () => {
  it("formats the clock, money, percentage and backlog from API integers", () => {
    render(<Hud metrics={resp(30, {})} />);
    expect(screen.getByTestId("hud-clock").textContent).toContain("Friday 06:00");
    expect(screen.getByTestId("hud-cash").textContent).toBe("$12,345.67");
    expect(screen.getByTestId("hud-net-profit").textContent).toBe("$500.00");
    expect(screen.getByTestId("hud-service-level").textContent).toBe("92.50%");
    expect(screen.getByTestId("hud-backlog").textContent).toBe("4 orders / 9 units");
    expect(screen.getByTestId("hud-disclaimer").textContent).toMatch(/^Synthetic simulation output/);
  });

  it("shows the disclaimer even before metrics load", () => {
    render(<Hud metrics={null} tick={0} />);
    expect(screen.getByTestId("hud-clock").textContent).toContain("Thursday 00:00");
    expect(screen.getByTestId("hud-disclaimer").textContent).toMatch(/Not a forecast/);
  });

  it("shows parent values and labelled display differences for the same tick", () => {
    render(
      <Hud
        metrics={resp(30, { netProfitCents: 50000, serviceLevelBps: 9250, backlogOrders: 4, backlogUnits: 9 })}
        parentMetrics={resp(30, { netProfitCents: 80050, serviceLevelBps: 9000, backlogOrders: 6, backlogUnits: 15 })}
        parentLabel="Main"
      />,
    );
    expect(screen.getByText("difference vs parent (display)")).toBeTruthy();
    expect(screen.getByTestId("parent-net-profit").textContent).toContain("$800.50");
    expect(screen.getByTestId("diff-net-profit").textContent).toContain("-$300.50");
    expect(screen.getByTestId("diff-service-level").textContent).toContain("+2.50%");
    expect(screen.getByTestId("diff-backlog").textContent).toContain("-2 orders / -6 units");
  });

  it("omits the parent comparison when the parent Metrics are for another tick", () => {
    render(<Hud metrics={resp(30, {})} parentMetrics={resp(29, {})} />);
    expect(screen.queryByTestId("hud-parent")).toBeNull();
  });

  it("makes money figures clickable when a handler is given", async () => {
    const onClick = vi.fn();
    render(<Hud metrics={resp(1, {})} onMoneyClick={onClick} />);
    await userEvent.setup().click(screen.getByTestId("hud-net-profit"));
    expect(onClick).toHaveBeenCalledWith("net_profit");
  });

  it("formatters handle negatives and small values with integer math", () => {
    expect(formatCents(-5, "USD")).toBe("-$0.05");
    expect(formatCents(0, "USD", true)).toBe("$0.00");
    expect(formatBps(-1, true)).toBe("-0.01%");
    expect(formatSimClock(119)).toBe("Monday 23:00");
  });
});
