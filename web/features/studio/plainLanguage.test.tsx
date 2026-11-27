// @vitest-environment jsdom
/**
 * Normal mode speaks business language. Crisis cards and the decision panel
 * never show engine words (docs/ux-redesign.md section 4).
 */
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import type { Observation, TickFrame } from "@/lib/api/client";
import { useUiStore } from "@/stores/ui";
import { DecisionPanel } from "@/features/decisions/DecisionPanel";
import { CrisisCards } from "./CrisisCards";
import { deriveCrisisCards } from "./crisis";

const BANNED = /\bSKUs?\b|\bticks?\b|stock-bound|capacity-bound|\bhash(es)?\b|\bPOs?\b|\bengine\b|provenance|disruption|\bnull\b|undefined|\bNaN\b|\bseq\b|\binput log\b|\bsnapshot\b/i;

const NAMES = ["Headphones", "Chargers", "Speakers", "Watch bands"];
const sup = (role: "primary" | "backup", available: boolean) => ({ role, unitCostCents: 5700, leadTimeTicks: 8, available, availableFromTick: 40 });

const observation = {
  tick: 30,
  cashCents: 100000,
  skus: [
    {
      sku: 2,
      id: "speaker",
      onHand: 0,
      inventoryValueCents: 0,
      referencePriceCents: 2000,
      priceBounds: { minCents: 1000, maxCents: 4000 },
      priceCents: 2000,
      marketingCents: 0,
      inboundPOs: 1,
      inboundUnits: 40,
      suppliers: [sup("primary", false), sup("backup", true)],
    },
  ],
  openOrders: [],
  purchaseOrders: [{ id: "po1", sku: 2, qty: 50, supplier: "primary", dueAt: 60 }],
  workforce: {},
  disruptions: [{ kind: "supplier_outage", origin: "script", inputSeq: 7, target: 2, supplierRole: "primary", magnitudeBps: 0, onsetTick: 28, endTick: 40 }],
  metrics: { backlogOrders: 123 },
  window: { open: true, commandBudget: 5 },
} as unknown as Observation;

const frame = {
  tick: 30,
  stateHash: "h",
  skus: [{ sku: 2, onHand: 0, inboundPOs: 1, inboundUnits: 40 }],
  workersOnShift: 15,
  tempsOnShift: 0,
  tempsOnboarding: 0,
  orderQueueLen: 123,
  disruptions: [
    { kind: "supplier_outage", origin: "script", inputSeq: 7, target: 2, magnitudeBps: 10000, onsetTick: 28, endTick: 40, ticksSinceOnset: 2 },
    { kind: "demand_surge", origin: "script", inputSeq: 8, target: 2, magnitudeBps: 20000, onsetTick: 20, endTick: 50, ticksSinceOnset: 10 },
    { kind: "workforce_absence", origin: "script", inputSeq: 9, target: -1, magnitudeBps: 3000, onsetTick: 25, endTick: 50, ticksSinceOnset: 5 },
  ],
  report: { tick: 30, capacity: 200, capacityUsed: 200, shippedOrders: 0, skus: [{ sku: 2, stockBoundOrders: 80, stockBoundUnits: 90, capacityBoundOrders: 30, capacityBoundUnits: 40 }] },
  markers: [],
  metrics: {},
} as unknown as TickFrame;

beforeEach(() => useUiStore.setState({ engineering: false }));
afterEach(cleanup);

describe("plain language in normal mode", () => {
  it("crisis cards of every kind avoid engine words", () => {
    const cards = deriveCrisisCards({ frame, observation, productNames: NAMES, regularWorkers: 20, startClock: "Thu 00:00", canAct: true });
    expect(cards.map((c) => c.kind).sort()).toEqual(["capacity", "disruption", "disruption", "disruption", "late_delivery", "stock"]);
    const { container } = render(<CrisisCards cards={cards} canAct onAction={() => {}} />);
    const text = container.textContent ?? "";
    expect(text.length).toBeGreaterThan(200);
    expect(text).not.toMatch(BANNED);
  });

  it("the decision panel avoids engine words", () => {
    const { container } = render(
      <DecisionPanel observation={observation} windowTick={30} branchId="b1" budgetRemaining={5} startClock="Thu 00:00" />,
    );
    const text = container.textContent ?? "";
    expect(text.length).toBeGreaterThan(100);
    expect(text).not.toMatch(BANNED);
  });
});
