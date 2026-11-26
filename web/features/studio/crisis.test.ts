import { describe, expect, it } from "vitest";
import type { Observation, TickFrame } from "@/lib/api/client";
import { deriveCrisisCards } from "./crisis";

const NAMES = ["Headphones", "Chargers", "Speakers", "Watch bands"];
const START = "Thu 00:00";

function frame(over: Partial<TickFrame> = {}): TickFrame {
  return {
    tick: 30,
    stateHash: "h",
    skus: [{ sku: 2, onHand: 0, inboundPOs: 0, inboundUnits: 40 }],
    workersOnShift: 15,
    tempsOnShift: 0,
    tempsOnboarding: 0,
    orderQueueLen: 123,
    disruptions: [],
    report: { tick: 30, capacity: 200, capacityUsed: 200, shippedOrders: 0, skus: [] },
    markers: [],
    metrics: {},
    ...over,
  } as unknown as TickFrame;
}

const sup = (role: "primary" | "backup", available: boolean) => ({
  role,
  unitCostCents: 5700,
  leadTimeTicks: 8,
  available,
  availableFromTick: 40,
});

function obs(over: Partial<Observation> = {}): Observation {
  return {
    tick: 30,
    cashCents: 100,
    skus: [{ sku: 2, id: "speaker", suppliers: [sup("primary", false), sup("backup", true)] }],
    openOrders: [],
    purchaseOrders: [],
    workforce: {},
    disruptions: [{ kind: "supplier_outage", origin: "script", inputSeq: 7, target: 2, supplierRole: "primary", magnitudeBps: 0, onsetTick: 28, endTick: 40 }],
    metrics: {},
    window: { open: true },
    ...over,
  } as unknown as Observation;
}

const outage = { kind: "supplier_outage", origin: "script", inputSeq: 7, target: 2, magnitudeBps: 10000, onsetTick: 28, endTick: 40, ticksSinceOnset: 2 };

describe("deriveCrisisCards", () => {
  it("produces no cards when nothing is wrong", () => {
    expect(deriveCrisisCards({ frame: frame(), observation: null, productNames: NAMES, startClock: START, canAct: true })).toEqual([]);
  });

  it("describes a supplier outage from the frame and observation, with a real backup action", () => {
    const f = frame({
      disruptions: [outage] as TickFrame["disruptions"],
      report: { tick: 30, capacity: 200, capacityUsed: 0, shippedOrders: 0, skus: [{ sku: 2, stockBoundOrders: 123, stockBoundUnits: 200, capacityBoundOrders: 0, capacityBoundUnits: 0 }] },
    });
    const cards = deriveCrisisCards({ frame: f, observation: obs(), productNames: NAMES, startClock: START, canAct: true });
    const c = cards.find((x) => x.id === "disruption-7");
    expect(c?.title).toBe("Normal supplier is down");
    expect(c?.what).toBe("Your normal supplier can't ship Speakers right now.");
    expect(c?.why).toBe("The normal supplier cannot ship until Friday, 4:00 PM.");
    expect(c?.affected).toBe("123 orders are waiting for Speakers.");
    expect(c?.actions).toEqual([{ id: "order-2-backup", label: "Order from the backup supplier", kind: "order", skuIndex: 2, role: "backup" }]);
    const stock = cards.find((x) => x.id === "stock-2");
    expect(stock?.title).toBe("Waiting for inventory: Speakers");
    expect(stock?.affected).toBe("40 units are on the way.");
  });

  it("never offers actions when no decision can be made, and never invents the supplier role", () => {
    const f = frame({ disruptions: [outage] as TickFrame["disruptions"] });
    const [c] = deriveCrisisCards({ frame: f, observation: null, productNames: NAMES, startClock: START, canAct: false });
    expect(c?.title).toBe("A supplier is down");
    expect(c?.actions).toEqual([]);
  });

  it("ignores an observation from another hour", () => {
    const f = frame({ disruptions: [outage] as TickFrame["disruptions"] });
    const [c] = deriveCrisisCards({ frame: f, observation: obs({ tick: 36 }), productNames: NAMES, startClock: START, canAct: true });
    expect(c?.actions).toEqual([]);
  });

  it("does not show a disruption that has not started or has ended at the displayed hour", () => {
    const future = { ...outage, onsetTick: 31 };
    const past = { ...outage, endTick: 30 };
    const cards = deriveCrisisCards({
      frame: frame({ disruptions: [future, past] as TickFrame["disruptions"] }),
      observation: null,
      productNames: NAMES,
      startClock: START,
      canAct: true,
    });
    expect(cards).toEqual([]);
  });

  it("describes absences with the regular worker count and surges with the queue", () => {
    const f = frame({
      workersOnShift: 12,
      disruptions: [
        { kind: "workforce_absence", origin: "script", inputSeq: 1, target: -1, magnitudeBps: 3000, onsetTick: 20, endTick: 50, ticksSinceOnset: 10 },
        { kind: "demand_surge", origin: "script", inputSeq: 2, target: -1, magnitudeBps: 20000, onsetTick: 10, endTick: 50, ticksSinceOnset: 20 },
      ] as TickFrame["disruptions"],
    });
    const cards = deriveCrisisCards({ frame: f, observation: obs({ disruptions: [] }), productNames: NAMES, regularWorkers: 20, startClock: START, canAct: true });
    expect(cards.find((c) => c.id === "disruption-1")?.what).toBe("8 of 20 packers are out.");
    const surge = cards.find((c) => c.id === "disruption-2");
    expect(surge?.title).toBe("Orders are surging across the store");
    expect(surge?.affected).toBe("123 orders are waiting.");
    expect(surge?.actions[0]).toMatchObject({ kind: "price", skuIndex: 2 });
  });

  it("reports packing capacity and late deliveries from the data", () => {
    const f = frame({
      report: { tick: 30, capacity: 180, capacityUsed: 180, shippedOrders: 9, skus: [{ sku: 2, stockBoundOrders: 0, stockBoundUnits: 0, capacityBoundOrders: 14, capacityBoundUnits: 20 }] },
    });
    const o = obs({
      disruptions: [],
      purchaseOrders: [{ id: 3, sku: 2, supplier: "backup", qty: 315, unitCostCents: 5700, dueAt: 25 * 60 + 30, expedited: false }],
    });
    const cards = deriveCrisisCards({ frame: f, observation: o, productNames: NAMES, startClock: START, canAct: true });
    expect(cards.find((c) => c.id === "capacity")?.why).toBe("The team can only pack 180 items an hour.");
    expect(cards.find((c) => c.id === "late-3")?.what).toBe("315 Speakers from the backup supplier were due around Friday, 1:00 AM.");
  });
});
