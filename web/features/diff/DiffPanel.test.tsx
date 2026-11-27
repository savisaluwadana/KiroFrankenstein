// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Branch, DiffResult } from "@/lib/api/client";

const diffMock = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api/client", async (orig) => {
  const actual = await orig<typeof import("@/lib/api/client")>();
  return { ...actual, createDiff: diffMock };
});

import { ApiError } from "@/lib/api/client";
import { DiffPanel } from "./DiffPanel";

const STATEMENT =
  "Observed differences do not establish cause. To attribute an effect to one decision, fork and change only that decision.";
const DISCLAIMER = "Synthetic simulation output under the stated assumptions. Not a forecast or guarantee of real-world results.";
const HASH_A = "a".repeat(64);
const HASH_B = "b".repeat(64);

function br(id: string, parent: string | null, over: Partial<Branch> = {}): Branch {
  return { id, worldId: "w", parentBranchId: parent, forkTick: 20, headTick: 40, label: id, managerKind: "human", createdAt: "2026-01-01T00:00:01Z", ...over };
}
const A = br("11111111-1111-4111-8111-111111111111", null);
const B = br("22222222-2222-4222-8222-222222222222", A.id, { label: "Backup supplier" });
const names = ["Headphones", "Chargers", "Speakers"];

function result(over: Partial<DiffResult> = {}): DiffResult {
  return {
    branchA: A.id,
    branchB: B.id,
    worldId: "w",
    comparisonTick: 40,
    divergence: {
      tick: 21,
      stateHashA: HASH_A,
      stateHashB: HASH_B,
      components: [
        { name: "inventory", skus: [1] },
        { name: "orders", skus: [] },
        { name: "ledger", skus: [] },
        { name: "workforce", skus: [] },
        { name: "prices", skus: [] },
        { name: "streams", skus: [] },
      ],
    },
    differingInputs: [
      {
        side: "B",
        branchId: B.id,
        seq: 3,
        effectiveTick: 21,
        kind: "command",
        command: { kind: "place_purchase_order", sku: 2, role: "backup", qty: 315 },
        provenance: "human",
      },
    ],
    metrics: [
      { key: "net_profit", a: 100000, b: 250000, delta: 150000 },
      { key: "cash", a: 500000, b: 450000, delta: -50000 },
      { key: "service_level", a: 9000, b: 9650, delta: 650 },
      { key: "fill_rate", a: 9100, b: 9400, delta: 300 },
      { key: "backlog_orders", a: 40, b: 10, delta: -30 },
      { key: "backlog_units", a: 80, b: 20, delta: -60 },
      { key: "late_backlog_orders", a: 5, b: 1, delta: -4 },
      { key: "cancelled_orders", a: 2, b: 0, delta: -2 },
    ],
    skuStock: [
      { sku: 0, a: 10, b: 10, delta: 0, valueCentsA: 100, valueCentsB: 100, valueCentsDelta: 0 },
      { sku: 2, a: 120, b: 435, delta: 315, valueCentsA: 1000, valueCentsB: 4000, valueCentsDelta: 3000 },
    ],
    statement: STATEMENT,
    disclaimer: DISCLAIMER,
    currency: "USD",
    ...over,
  };
}

function panel(over: { engineering?: boolean; onSelectTick?: (t: number) => void; onResult?: (r: DiffResult | null) => void } = {}) {
  return render(
    <DiffPanel
      branchA={A}
      branchB={B}
      branches={[A, B]}
      startClock="Thu 00:00"
      productNames={names}
      engineering={over.engineering ?? false}
      onSelectTick={over.onSelectTick ?? (() => {})}
      {...(over.onResult !== undefined ? { onResult: over.onResult } : {})}
    />,
  );
}

afterEach(() => {
  cleanup();
  diffMock.mockReset();
});

describe("DiffPanel", () => {
  it("asks the server for the pair and renders only the server's values", async () => {
    diffMock.mockResolvedValue(result());
    panel();
    await screen.findByTestId("diff-divergence");
    expect(diffMock).toHaveBeenCalledTimes(1);
    expect(diffMock.mock.calls[0]?.[0]).toEqual({ branchA: A.id, branchB: B.id });

    const text = document.body.textContent ?? "";
    expect(text).toContain("What changed between these two futures");
    expect(text).toContain("Stock of Chargers");
    for (const t of ["Orders shipped", "Cash", "Staffing", "Prices"]) expect(text).toContain(t);
    expect(text).not.toContain("Internal bookkeeping");
    expect(screen.getByTestId("diff-input").textContent).toContain("Ordered 315 Speakers from the backup supplier — takes effect Thursday, 9:00 PM");
    expect(screen.getByTestId("diff-input-ref").textContent).toBe("Input #3, effective hour 21");

    const profit = screen.getByTestId("diff-metric-net_profit").textContent ?? "";
    expect(profit).toContain("$1,000.00");
    expect(profit).toContain("$2,500.00");
    expect(profit).toContain("+$1,500.00");
    expect(screen.getByTestId("diff-metric-service_level").textContent).toContain("+6.5 points");
    expect(screen.getByTestId("diff-metric-backlog_orders").textContent).toContain("-30");
    expect(screen.getByTestId("diff-metric-service_level").textContent).toContain("Of the orders whose ship-by time has passed");
    expect(screen.getByTestId("diff-stock-2").textContent).toContain("+315 units");
    expect(screen.queryByTestId("diff-stock-0")).toBeNull();
  });

  it("links the first difference to the shared time without advancing", async () => {
    diffMock.mockResolvedValue(result());
    const onSelectTick = vi.fn();
    panel({ onSelectTick });
    const link = await screen.findByTestId("diff-divergence-link");
    expect(link.textContent).toBe("Thursday, 9:00 PM");
    await userEvent.click(link);
    expect(onSelectTick).toHaveBeenCalledExactlyOnceWith(21);
  });

  it("reports the result so the time strip can mark the divergence", async () => {
    const r = result();
    diffMock.mockResolvedValue(r);
    const onResult = vi.fn();
    panel({ onResult });
    await waitFor(() => expect(onResult).toHaveBeenLastCalledWith(r));
  });

  it("shows the statement and disclaimer verbatim and never states a cause", async () => {
    diffMock.mockResolvedValue(result());
    panel({ engineering: true });
    await screen.findByTestId("diff-statement");
    expect(screen.getByTestId("diff-statement").textContent).toBe(STATEMENT);
    expect(screen.getByTestId("diff-disclaimer").textContent).toBe(DISCLAIMER);
    for (const fixture of [result(), result({ divergence: null, differingInputs: [] })]) {
      cleanup();
      diffMock.mockResolvedValue(fixture);
      panel({ engineering: true });
      await screen.findByTestId("diff-statement");
      const text = (document.body.textContent ?? "").toLowerCase();
      expect(text).not.toMatch(/because|caused by|due to/);
    }
  });

  it("says so when the two futures are identical", async () => {
    diffMock.mockResolvedValue(result({ divergence: null, differingInputs: [], skuStock: [] }));
    panel();
    const sec = await screen.findByTestId("diff-divergence");
    expect(sec.textContent).toContain("look the same up to Friday, 4:00 PM");
    expect(screen.queryByTestId("diff-divergence-link")).toBeNull();
  });

  it("shows technical references only in Engineering Mode, behind a button", async () => {
    diffMock.mockResolvedValue(result());
    panel();
    await screen.findByTestId("diff-divergence");
    expect(screen.queryByRole("button", { name: "Show technical references" })).toBeNull();
    expect(document.body.textContent).not.toContain(HASH_A);
    expect(document.body.textContent).not.toContain(B.id);
    cleanup();

    diffMock.mockResolvedValue(result());
    panel({ engineering: true });
    await screen.findByTestId("diff-divergence");
    expect(document.body.textContent).not.toContain(HASH_A);
    await userEvent.click(screen.getByRole("button", { name: "Show technical references" }));
    expect(screen.getByTestId("diff-tech").textContent).toContain(HASH_A);
    expect(screen.getByTestId("diff-tech").textContent).toContain(HASH_B);
    expect(screen.getByTestId("diff-input-tech").textContent).toContain(B.id);
  });

  it("explains a refusal in plain words", async () => {
    diffMock.mockRejectedValue(new ApiError({ status: 422, type: "incomparable-branches", title: "Branches cannot be compared" }));
    panel();
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("can't be compared");
    expect(alert.textContent?.toLowerCase()).not.toMatch(/because|caused by|due to/);
  });
});
