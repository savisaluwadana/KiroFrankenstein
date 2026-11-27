// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { LedgerEntry, LedgerResponse, LedgerSummaryResponse, MoneyFigure } from "@/lib/api/client";
import { DISCLAIMER_FULL } from "@/lib/glossary";

const api = vi.hoisted(() => ({ getLedgerSummary: vi.fn(), listLedgerEntries: vi.fn() }));
vi.mock("@/lib/api/client", () => api);

import { LedgerPanel, causeLabel } from "./LedgerPanel";

afterEach(cleanup);

const base = { currency: "USD", disclaimer: DISCLAIMER_FULL } as const;

const summary: LedgerSummaryResponse = {
  ...base,
  branchId: "b",
  tick: 30,
  accounts: [
    { account: "Revenue", normalSide: "credit", debitCents: 0, creditCents: 90000, balanceCents: 90000, entryCount: 3 },
    { account: "Cost_Of_Goods_Sold", normalSide: "debit", debitCents: 40000, creditCents: 0, balanceCents: 40000, entryCount: 3 },
    { account: "Labor_Expense", normalSide: "debit", debitCents: 5000, creditCents: 0, balanceCents: 5000, entryCount: 2 },
    { account: "Cash", normalSide: "debit", debitCents: 100000, creditCents: 5000, balanceCents: 95000, entryCount: 4 },
    { account: "Inventory", normalSide: "debit", debitCents: 10000, creditCents: 0, balanceCents: 10000, entryCount: 1 },
  ],
  figures: [
    { figure: "net_profit", sumCents: 45000, entryCount: 5, components: [{ account: "Revenue", sign: 1 }] },
    { figure: "cash", sumCents: 95000, entryCount: 4, components: [{ account: "Cash", sign: 1 }] },
  ],
};

function entry(index: number, kind: LedgerEntry["cause"]["kind"], ref: number, tick: number, contribution: number): LedgerEntry {
  return {
    index,
    tick,
    simMinute: tick * 60,
    description: `raw description ${index}`,
    cause: { kind, ref },
    contributionCents: contribution,
    lines: [
      { account: "Cash", debitCents: contribution > 0 ? contribution : 0, creditCents: contribution < 0 ? -contribution : 0 },
      { account: "Revenue", debitCents: contribution < 0 ? -contribution : 0, creditCents: contribution > 0 ? contribution : 0 },
    ],
  };
}

function ledger(entries: LedgerEntry[], total: number, sum: number, figure: MoneyFigure): LedgerResponse {
  return { ...base, branchId: "b", from: 0, to: 30, total, offset: 0, entries, figure, sumCents: sum };
}

beforeEach(() => {
  api.getLedgerSummary.mockReset().mockResolvedValue(summary);
  api.listLedgerEntries.mockReset().mockImplementation(async (_b: string, q: { figure: MoneyFigure }) =>
    ledger([entry(0, "labor", 29, 29, 20000), entry(5, "shipment", 12, 24, 25000)], 2, 45000, q.figure),
  );
});

describe("LedgerPanel", () => {
  it("shows server P&L numbers and opens a drill-down from a row", async () => {
    const user = userEvent.setup();
    render(<LedgerPanel branchId="b" tick={30} startClock="Thu 00:00" />);
    expect((await screen.findByTestId("pnl-net_revenue")).textContent).toBe("$900.00");
    expect(screen.getByTestId("pnl-net_profit").textContent).toBe("+$450.00");
    expect(screen.getByTestId("ledger-disclaimer").textContent).toBe(DISCLAIMER_FULL);

    await user.click(screen.getByRole("button", { name: "Wages" }));
    expect((await screen.findByTestId("ledger-sum-value")).textContent).toBe("$450.00");
    expect(screen.getByTestId("ledger-count").textContent).toBe("2 entries add up to this");
    expect(api.listLedgerEntries).toHaveBeenCalledWith("b", expect.objectContaining({ from: 0, to: 30, figure: "labor" }));
    const rows = screen.getAllByTestId("ledger-entry");
    expect(rows[0]?.textContent).toContain("Wages · Friday, 5:00 AM");
    expect(rows[1]?.textContent).toContain("Order shipment · order 12");
    expect(rows[1]?.textContent).toContain("running +$450.00");
    expect(document.body.textContent).not.toMatch(/raw description|entry 0|tick \d/);
  });

  it("opens straight into a figure, shows raw ids in Engineering Mode and closes on Escape", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    render(<LedgerPanel branchId="b" tick={30} figure="net_profit" engineering startClock="Thu 00:00" onClose={onClose} />);
    expect((await screen.findByTestId("ledger-sum-value")).textContent).toBe("+$450.00");
    expect(screen.getAllByTestId("ledger-entry")[0]?.textContent).toContain("entry 0 · tick 29 · labor 29 · raw description 0");
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("heading", { level: 2 })));
    await user.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("shows an error when the summary fails", async () => {
    api.getLedgerSummary.mockRejectedValueOnce(new Error("boom"));
    render(<LedgerPanel branchId="b" tick={30} startClock="Thu 00:00" />);
    expect((await screen.findByRole("alert")).textContent).toBe("boom");
  });

  it("labels causes without tick numbers", () => {
    expect(causeLabel("receipt", 3, "Thu 00:00")).toBe("Supplier delivery · purchase order 3");
    expect(causeLabel("opening", 0, "Thu 00:00")).toBe("Opening balances");
  });
});
