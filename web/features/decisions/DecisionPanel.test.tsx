// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Assumption, Observation } from "@/lib/api/client";
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
      priceBounds: { minCents: 1000, maxCents: 4000 },
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
      priceBounds: { minCents: 500, maxCents: 2000 },
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

  describe("staffing, rush and marketing cards", () => {
    const asm = (path: string, value: number): Assumption => ({
      path,
      value,
      unit: "",
      default: value,
      min: 0,
      max: value * 10,
      rationale: "",
      overridden: false,
    });
    const assumptions = [
      asm("costs.expediteFeeCents", 5000),
      asm("costs.expediteReductionTicks", 4),
      asm("workforce.maxOvertimePerWorker", 4),
      asm("workforce.regularWorkers", 20),
      asm("wages.overtimePerHourCents", 3000),
      asm("workforce.maxTempWorkers", 30),
      asm("wages.tempPerHourCents", 2000),
      asm("workforce.tempOnboardingTicks", 6),
      asm("demand.maxMarketingSpendCentsPerHour", 50000),
    ];
    const rich = {
      ...observation,
      purchaseOrders: [
        { id: 7, sku: 0, supplier: "backup", qty: 200, unitCostCents: 1250, dueAt: 33 * 60, expedited: false },
      ],
      workforce: { regular: 20, overtimeHours: 0, workersOnShift: 20, tempsOnShift: 4, tempsOnboarding: 6 },
    } as unknown as Observation;

    function renderRich(extra: { prefill?: Prefill | null } = {}) {
      return render(
        <DecisionPanel
          observation={rich}
          windowTick={24}
          branchId="b1"
          budgetRemaining={5}
          startClock="Thu 00:00"
          assumptions={assumptions}
          {...extra}
        />,
      );
    }
    const cardOf = (title: string): HTMLElement =>
      screen.getByRole("heading", { name: title }).closest("section") as HTMLElement;
    const accept = (command: unknown) =>
      json(200, { accepted: [{ seq: 1, effectiveTick: 25, command, provenance: "human" }], rejected: [], budgetRemaining: 4, duplicate: false });
    const reject = (code: string, message = "server says no") =>
      json(200, { accepted: [], rejected: [{ index: 0, command: {}, ok: false, code, message }], budgetRemaining: 5, duplicate: false });
    const lastBody = () => JSON.parse(String(fetchMock.mock.calls.at(-1)?.[1]?.body)) as { commands: unknown[] };

    async function addAndLock(user: ReturnType<typeof userEvent.setup>, card: HTMLElement) {
      await user.click(within(card).getByRole("button", { name: "Add to my moves" }));
      await user.click(screen.getByRole("button", { name: "Lock in decisions" }));
    }

    it("rush a delivery: shows the fee and the sooner arrival, sends poRef", async () => {
      fetchMock.mockResolvedValue(accept({ kind: "expedite_purchase_order", poRef: 7 }));
      const user = userEvent.setup();
      renderRich();
      const c = cardOf("Rush a delivery");
      expect(within(c).getByTestId("rush-fee").textContent).toBe("$50.00");
      expect(within(c).getByText("Estimated cost")).toBeTruthy();
      expect(within(c).getByText("about 4 hours sooner")).toBeTruthy();
      await addAndLock(user, c);
      expect((await screen.findByTestId("accepted-command")).textContent).toContain("Rushed the delivery of 200 Speakers");
      expect(lastBody().commands).toEqual([{ kind: "expedite_purchase_order", poRef: 7 }]);
    });

    it("overtime: stepper bounded by the maximum, estimate, sends qty, 0 stops", async () => {
      fetchMock.mockResolvedValue(accept({ kind: "schedule_overtime", qty: 2 }));
      const user = userEvent.setup();
      renderRich();
      const c = cardOf("Schedule overtime");
      const plus = within(c).getByRole("button", { name: "Increase overtime hours per worker" });
      await user.click(plus);
      await user.click(plus);
      await user.click(plus);
      await user.click(plus);
      await user.click(plus);
      expect((within(c).getByLabelText("Overtime hours per worker") as HTMLInputElement).value).toBe("4");
      await user.click(within(c).getByRole("button", { name: "Decrease overtime hours per worker" }));
      await user.click(within(c).getByRole("button", { name: "Decrease overtime hours per worker" }));
      // 20 workers x 2 hours x $30.00
      expect(within(c).getByTestId("overtime-estimate").textContent).toBe("$1,200.00");
      expect(c.textContent).toContain("until you change it");
      await addAndLock(user, c);
      await screen.findByTestId("accepted-command");
      expect(lastBody().commands).toEqual([{ kind: "schedule_overtime", qty: 2 }]);
    });

    it("overtime of 0 hours is a stop", async () => {
      fetchMock.mockResolvedValue(accept({ kind: "schedule_overtime", qty: 0 }));
      const user = userEvent.setup();
      renderRich();
      const c = cardOf("Schedule overtime");
      await user.clear(within(c).getByLabelText("Overtime hours per worker"));
      await user.type(within(c).getByLabelText("Overtime hours per worker"), "0");
      await user.click(within(c).getByRole("button", { name: "Add to my moves" }));
      expect(screen.getByTestId("pending-command").textContent).toContain("Stop overtime");
      await user.click(screen.getByRole("button", { name: "Lock in decisions" }));
      await screen.findByTestId("accepted-command");
      expect(lastBody().commands).toEqual([{ kind: "schedule_overtime", qty: 0 }]);
    });

    it("hire temporary staff: headroom hint, training note, estimate, sends qty", async () => {
      fetchMock.mockResolvedValue(accept({ kind: "hire_temp_workers", qty: 3 }));
      const user = userEvent.setup();
      renderRich();
      const c = cardOf("Hire temporary staff");
      expect(c.textContent).toContain("up to 20 more");
      expect(c.textContent).toContain("only start packing after 6 hours of training");
      const plus = within(c).getByRole("button", { name: "Increase temporary workers" });
      await user.click(plus);
      await user.click(plus);
      expect(within(c).getByTestId("hire-estimate").textContent).toBe("$60.00");
      await addAndLock(user, c);
      await screen.findByTestId("accepted-command");
      expect(lastBody().commands).toEqual([{ kind: "hire_temp_workers", qty: 3 }]);
    });

    it("hiring beyond the headroom is stopped as a hint and not sent", async () => {
      const user = userEvent.setup();
      renderRich();
      const c = cardOf("Hire temporary staff");
      await user.clear(within(c).getByLabelText("Temporary workers"));
      await user.type(within(c).getByLabelText("Temporary workers"), "25");
      await user.click(within(c).getByRole("button", { name: "Add to my moves" }));
      expect((await screen.findAllByRole("alert"))[0]?.textContent).toBe(
        "That would be more temporary workers than the business allows.",
      );
      expect(screen.queryByTestId("pending-command")).toBeNull();
    });

    it("marketing push: dollars parsed without floats, sends amountCents; over the limit is a hint", async () => {
      fetchMock.mockResolvedValue(accept({ kind: "set_marketing_spend", sku: 1, amountCents: 2550 }));
      const user = userEvent.setup();
      renderRich();
      const c = cardOf("Marketing push");
      await user.selectOptions(within(c).getByLabelText("Product to promote"), "1");
      await user.type(within(c).getByLabelText("Spend per hour (USD)"), "600");
      await user.click(within(c).getByRole("button", { name: "Add to my moves" }));
      expect((await screen.findAllByRole("alert"))[0]?.textContent).toBe(
        "That is more marketing spend per hour than the business allows.",
      );
      await user.clear(within(c).getByLabelText("Spend per hour (USD)"));
      await user.type(within(c).getByLabelText("Spend per hour (USD)"), "25.5");
      expect(within(c).getByTestId("marketing-estimate").textContent).toBe("$25.50");
      await addAndLock(user, c);
      await screen.findByTestId("accepted-command");
      expect(lastBody().commands).toEqual([{ kind: "set_marketing_spend", sku: 1, amountCents: 2550 }]);
    });

    it.each([
      ["Rush a delivery", "unknown_purchase_order", "That delivery isn't on order any more."],
      ["Rush a delivery", "already_expedited", "That delivery has already been rushed."],
      ["Schedule overtime", "overtime_exceeds_limit", "That is more overtime per worker than the business allows."],
      ["Hire temporary staff", "too_many_temp_workers", "That would be more temporary workers than the business allows."],
      ["Marketing push", "marketing_exceeds_limit", "That is more marketing spend per hour than the business allows."],
    ])("%s: %s is shown next to its card", async (title, code, copy) => {
      fetchMock.mockResolvedValue(reject(code));
      const user = userEvent.setup();
      renderRich();
      const c = cardOf(title);
      if (title === "Marketing push") await user.type(within(c).getByLabelText("Spend per hour (USD)"), "10");
      await addAndLock(user, c);
      const note = await within(c).findByTestId("card-rejection");
      expect(note.textContent).toBe(copy);
      cleanup();
    });

    it("shows the raw server code and message in Engineering Mode", async () => {
      useUiStore.setState({ engineering: true });
      fetchMock.mockResolvedValue(reject("overtime_exceeds_limit", "max is 4"));
      const user = userEvent.setup();
      renderRich();
      const c = cardOf("Schedule overtime");
      await addAndLock(user, c);
      expect((await within(c).findByTestId("card-rejection")).textContent).toContain("overtime_exceeds_limit: max is 4");
    });

    it("focuses the staffing card from a crisis action", () => {
      const { rerender } = renderRich();
      rerender(
        <DecisionPanel
          observation={rich}
          windowTick={24}
          branchId="b1"
          budgetRemaining={5}
          startClock="Thu 00:00"
          assumptions={assumptions}
          prefill={{ nonce: 1, kind: "hire", skuIndex: 0 }}
        />,
      );
      expect(document.activeElement).toBe(cardOf("Hire temporary staff"));
    });
  });
});
