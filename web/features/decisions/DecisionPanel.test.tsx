// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Observation } from "@/lib/api/client";
import { DecisionPanel, parseDollarsToCents } from "./DecisionPanel";

const observation = {
  tick: 24,
  cashCents: 1234500,
  skus: [
    {
      sku: 0,
      id: "widget",
      onHand: 40,
      inventoryValueCents: 0,
      referencePriceCents: 2000,
      priceCents: 2000,
      marketingCents: 0,
      inboundPOs: 1,
      inboundUnits: 10,
      suppliers: [],
    },
  ],
  openOrders: [],
  purchaseOrders: [],
  workforce: {},
  disruptions: [],
  metrics: { backlogOrders: 3 },
  window: {},
} as unknown as Observation;

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

const fetchMock = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>();

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function renderPanel() {
  render(<DecisionPanel observation={observation} windowTick={24} branchId="b1" budgetRemaining={5} />);
}

describe("parseDollarsToCents", () => {
  it("parses without floating point", () => {
    expect(parseDollarsToCents("19.99")).toBe(1999);
    expect(parseDollarsToCents("5.5")).toBe(550);
    expect(parseDollarsToCents("7")).toBe(700);
    expect(parseDollarsToCents("1.234")).toBeNull();
    expect(parseDollarsToCents("-1")).toBeNull();
  });
});

describe("DecisionPanel", () => {
  it("shows effective Tick from the response for submitted commands", async () => {
    fetchMock.mockResolvedValue(
      json(200, {
        accepted: [
          {
            seq: 1,
            effectiveTick: 25,
            command: { kind: "place_purchase_order", sku: 0, role: "backup", qty: 50 },
            provenance: "human",
          },
        ],
        rejected: [],
        budgetRemaining: 4,
        duplicate: false,
      }),
    );
    const user = userEvent.setup();
    renderPanel();
    expect(screen.getByTestId("obs-cash").textContent).toBe("$12,345.00");
    await user.selectOptions(screen.getByLabelText("Supplier"), "backup");
    await user.type(screen.getByLabelText("Quantity"), "50");
    await user.click(screen.getByRole("button", { name: "Add order" }));
    await user.click(screen.getByRole("button", { name: "Submit" }));

    const item = await screen.findByTestId("accepted-command");
    expect(item.textContent).toContain("effective Tick 25");
    expect(screen.getByTestId("obs-budget").textContent).toBe("4 commands");

    const init = fetchMock.mock.calls[0]?.[1];
    const headers = init?.headers as Record<string, string>;
    expect(headers["Idempotency-Key"]).toMatch(/^[0-9a-f-]{36}$/);
    expect(JSON.parse(String(init?.body))).toMatchObject({ windowTick: 24, expectedHeadTick: 24 });
  });

  it("shows a 422 rejection message next to the form", async () => {
    fetchMock.mockResolvedValue(
      json(422, {
        type: "urn:parallel:problem:validation",
        title: "Validation failed",
        status: 422,
        detail: "price out of bounds",
      }),
    );
    const user = userEvent.setup();
    renderPanel();
    await user.type(screen.getByLabelText("Price (USD)"), "999");
    await user.click(screen.getByRole("button", { name: "Add price" }));
    await user.click(screen.getByRole("button", { name: "Submit" }));
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("price out of bounds");
  });

  it("shows reason code and message beside a rejected command", async () => {
    fetchMock.mockResolvedValue(
      json(200, {
        accepted: [],
        rejected: [
          {
            index: 0,
            command: { kind: "set_price", sku: 0, amountCents: 99900 },
            ok: false,
            code: "price_out_of_bounds",
            message: "price must be within 50% to 200% of reference",
          },
        ],
        budgetRemaining: 5,
        duplicate: false,
      }),
    );
    const user = userEvent.setup();
    renderPanel();
    await user.type(screen.getByLabelText("Price (USD)"), "999");
    await user.click(screen.getByRole("button", { name: "Add price" }));
    await user.click(screen.getByRole("button", { name: "Submit" }));
    const row = await screen.findByTestId("pending-command");
    expect(within(row).getByRole("alert").textContent).toContain("price_out_of_bounds");
    expect(within(row).getByRole("alert").textContent).toContain("within 50% to 200%");
  });
});
