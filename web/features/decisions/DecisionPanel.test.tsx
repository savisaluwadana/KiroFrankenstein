// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Observation } from "@/lib/api/client";
import { useUiStore } from "@/stores/ui";
import { DecisionPanel, parseDollarsToCents, type Prefill } from "./DecisionPanel";

const supplier = (role: "primary" | "backup", unitCostCents: number, leadTimeTicks: number, available = true) => ({
  role,
  unitCostCents,
  leadTimeTicks,
  available,
  availableFromTick: available ? 0 : 30,
});

const observation = {
  tick: 24,
  cashCents: 1234500,
  skus: [
    {
      sku: 0,
      id: "speaker",
      onHand: 40,
      inventoryValueCents: 0,
      referencePriceCents: 2000,
      priceCents: 2000,
      marketingCents: 0,
      inboundPOs: 1,
      inboundUnits: 10,
      suppliers: [supplier("primary", 1000, 24), supplier("backup", 1250, 8)],
    },
    {
      sku: 1,
      id: "charger",
      onHand: 10,
      inventoryValueCents: 0,
      referencePriceCents: 1000,
      priceCents: 1000,
      marketingCents: 0,
      inboundPOs: 0,
      inboundUnits: 0,
      suppliers: [supplier("primary", 300, 24, false), supplier("backup", 400, 8)],
    },
  ],
  openOrders: [],
  purchaseOrders: [],
  workforce: {},
  disruptions: [],
  metrics: { backlogOrders: 3 },
  window: { open: true, commandBudget: 5 },
} as unknown as Observation;

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

const fetchMock = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>();

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
  useUiStore.setState({ engineering: false });
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function renderPanel(extra: { prefill?: Prefill | null } = {}) {
  return render(
    <DecisionPanel
      observation={observation}
      windowTick={24}
      branchId="b1"
      budgetRemaining={5}
      startClock="Thu 00:00"
      {...extra}
    />,
  );
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
  it("shows plain cards with remaining decisions, estimated cost and arrival", async () => {
    const user = userEvent.setup();
    renderPanel();
    expect(screen.getByTestId("obs-cash").textContent).toBe("$12,345.00");
    expect(screen.getByTestId("obs-budget").textContent).toBe("Decisions remaining: 5 of 5");
    expect(screen.getByRole("heading", { name: "Order more Speakers from the normal supplier" })).toBeTruthy();
    // default quantity 100 x $10.00
    expect(screen.getByTestId("estimated-cost").textContent).toBe("$1,000.00");
    expect(screen.getByText("about 24 hours after your move")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Increase quantity" }));
    expect(screen.getByTestId("estimated-cost").textContent).toBe("$1,100.00");
    await user.selectOptions(screen.getByLabelText("Supplier"), "backup");
    expect(screen.getByRole("heading", { name: "Order more Speakers from the backup supplier" })).toBeTruthy();
    expect(screen.getByTestId("estimated-cost").textContent).toBe("$1,375.00");
    expect(screen.getByText("about 8 hours after your move")).toBeTruthy();
    expect(document.body.textContent).not.toMatch(/\btick\b|\bSKU\b|primary|command/i);
  });

  it("omits an unavailable supplier and says when it returns", async () => {
    const user = userEvent.setup();
    renderPanel();
    await user.selectOptions(screen.getByLabelText("Product"), "1");
    expect(screen.getByText("Normal supplier is unavailable until Friday, 6:00 AM.")).toBeTruthy();
    expect(screen.queryByLabelText("Supplier")).toBeNull();
    expect(screen.getByRole("heading", { name: "Order more Chargers from the backup supplier" })).toBeTruthy();
  });

  it("submits with an idempotency key and expected head, and shows the effective time as a clock time", async () => {
    fetchMock.mockResolvedValue(
      json(200, {
        accepted: [
          { seq: 1, effectiveTick: 25, command: { kind: "place_purchase_order", sku: 0, role: "backup", qty: 110 }, provenance: "human" },
        ],
        rejected: [],
        budgetRemaining: 4,
        duplicate: false,
      }),
    );
    const user = userEvent.setup();
    renderPanel();
    await user.selectOptions(screen.getByLabelText("Supplier"), "backup");
    await user.click(screen.getByRole("button", { name: "Increase quantity" }));
    await user.click(screen.getAllByRole("button", { name: "Add to my moves" })[0] as HTMLElement);
    expect(screen.getByTestId("pending-command").textContent).toContain("Order 110 Speakers from the backup supplier, arriving in about 8 hours");
    await user.click(screen.getByRole("button", { name: "Lock in decisions" }));

    const item = await screen.findByTestId("accepted-command");
    expect(item.textContent).toContain("Ordered 110 Speakers from the backup supplier");
    expect(item.textContent).toContain("Takes effect at Friday, 1:00 AM");
    expect(screen.getByTestId("obs-budget").textContent).toBe("Decisions remaining: 4 of 5");

    const init = fetchMock.mock.calls[0]?.[1];
    const headers = init?.headers as Record<string, string>;
    expect(headers["Idempotency-Key"]).toMatch(/^[0-9a-f-]{36}$/);
    expect(JSON.parse(String(init?.body))).toMatchObject({
      windowTick: 24,
      expectedHeadTick: 24,
      commands: [{ kind: "place_purchase_order", sku: 0, role: "backup", qty: 110 }],
    });
  });

  it("maps a rejection code to plain language and keeps the server message in Engineering Mode", async () => {
    fetchMock.mockImplementation(async () =>
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
    expect(screen.getByText(/Allowed range: between \$10\.00 and \$40\.00/)).toBeTruthy();
    await user.type(screen.getByLabelText("New price (USD)"), "999");
    await user.click(screen.getAllByRole("button", { name: "Add to my moves" })[1] as HTMLElement);
    await user.click(screen.getByRole("button", { name: "Lock in decisions" }));
    const row = await screen.findByTestId("pending-command");
    expect(within(row).getByRole("alert").textContent).toBe("That price is outside the allowed range.");

    cleanup();
    useUiStore.setState({ engineering: true });
    renderPanel();
    await user.type(screen.getByLabelText("New price (USD)"), "999");
    await user.click(screen.getAllByRole("button", { name: "Add to my moves" })[1] as HTMLElement);
    await user.click(screen.getByRole("button", { name: "Lock in decisions" }));
    const row2 = await screen.findByTestId("pending-command");
    expect(within(row2).getByRole("alert").textContent).toContain("price_out_of_bounds");
    expect(within(row2).getByRole("alert").textContent).toContain("within 50% to 200%");
  });

  it("shows a plain sentence for a failed request and the server text in Engineering Mode", async () => {
    fetchMock.mockImplementation(async () =>
      json(409, { type: "urn:parallel:problem:stale-head", title: "Stale head", status: 409 }),
    );
    const user = userEvent.setup();
    renderPanel();
    await user.click(screen.getAllByRole("button", { name: "Add to my moves" })[0] as HTMLElement);
    await user.click(screen.getByRole("button", { name: "Lock in decisions" }));
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toMatch(/situation changed/);
    expect(alert.textContent).not.toMatch(/Stale head/);
  });

  it("applies a prefill from a crisis card", () => {
    const { rerender } = renderPanel();
    rerender(
      <DecisionPanel
        observation={observation}
        windowTick={24}
        branchId="b1"
        budgetRemaining={5}
        startClock="Thu 00:00"
        prefill={{ nonce: 1, kind: "order", skuIndex: 1, role: "backup" }}
      />,
    );
    expect(screen.getByRole("heading", { name: "Order more Chargers from the backup supplier" })).toBeTruthy();
  });

  it("offers a suggestion without reasons and adds it to the moves", async () => {
    const user = userEvent.setup();
    render(
      <DecisionPanel
        observation={observation}
        windowTick={24}
        branchId="b1"
        budgetRemaining={5}
        startClock="Thu 00:00"
        suggestion={{ skuIndex: 0, role: "backup", qty: 315 }}
      />,
    );
    expect(screen.getByTestId("suggestion").textContent).toContain("A suggestion, not a guarantee.");
    await user.click(screen.getByRole("button", { name: "Add suggestion to my moves" }));
    expect(screen.getByTestId("pending-command").textContent).toContain("Order 315 Speakers from the backup supplier");
  });
});
