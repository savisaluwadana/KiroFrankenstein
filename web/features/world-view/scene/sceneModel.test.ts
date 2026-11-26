import { describe, expect, it } from "vitest";
import { buildSceneModel, clockLabel, describeScene, productName } from "./sceneModel";
import { makeFrame, next, sku } from "./frames.testutil";

const NAMES = ["Chargers", "Speakers", "Headphones", "Cables"];

describe("buildSceneModel", () => {
  it("scales boxes at one per 20 units, capped at 54 with an exact remainder", () => {
    const f = makeFrame({ skus: [sku(0, 0), sku(1, 1), sku(2, 312), sku(3, 2000)] });
    const m = buildSceneModel(f, null, NAMES);
    expect(m.racks.map((r) => [r.boxes, r.moreBoxes])).toEqual([
      [0, 0],
      [1, 0],
      [16, 0],
      [54, 100 - 54],
    ]);
    expect(m.racks[2]?.name).toBe("Headphones");
  });

  it("never prints SKU n: missing names become Product n", () => {
    const m = buildSceneModel(makeFrame(), null, undefined);
    expect(m.racks.map((r) => r.name)).toEqual(["Product 1", "Product 2"]);
    expect(productName(["", "B"], 0)).toBe("Product 1");
    const text = JSON.stringify([m.hotspots, describeScene(m)]);
    expect(text).not.toMatch(/SKU/);
  });

  it("counts people, caps the floor at 40 and keeps the exact remainder", () => {
    const m = buildSceneModel(makeFrame({ workersOnShift: 38, tempsOnShift: 6, tempsOnboarding: 12 }), null, NAMES, {
      regularWorkers: 45,
    });
    expect(m.people).toMatchObject({
      onShift: 44,
      drawnRegular: 38,
      drawnTemps: 2,
      hiddenOnFloor: 4,
      onboarding: 12,
      onboardingDrawn: 8,
      absent: 7,
      absentDrawn: 7,
    });
    expect(m.people.perStation.reduce((a, b) => a + b, 0)).toBe(40);
    const noSize = buildSceneModel(makeFrame(), null, NAMES);
    expect(noSize.people.absent).toBeNull();
  });

  it("fills stations in blocks and leaves late stations empty", () => {
    const m = buildSceneModel(makeFrame({ workersOnShift: 5 }), null, NAMES);
    expect(m.people.perStation).toEqual([2, 2, 1, 0]);
  });

  it("caps tickets at 40 with a '+N more' remainder", () => {
    expect(buildSceneModel(makeFrame({ orderQueueLen: 14 }), null, NAMES).queue).toEqual({ length: 14, tickets: 14, more: 0 });
    expect(buildSceneModel(makeFrame({ orderQueueLen: 130 }), null, NAMES).queue).toEqual({ length: 130, tickets: 40, more: 90 });
  });

  it("derives bottlenecks with plain labels and counts", () => {
    const f = makeFrame({
      report: {
        tick: 10,
        capacity: 100,
        capacityUsed: 100,
        shippedOrders: 3,
        skus: [
          { sku: 1, stockBoundOrders: 7, stockBoundUnits: 7, capacityBoundOrders: 2, capacityBoundUnits: 2 },
          { sku: 0, stockBoundOrders: 0, stockBoundUnits: 0, capacityBoundOrders: 4, capacityBoundUnits: 4 },
        ],
      },
    });
    const m = buildSceneModel(f, null, NAMES);
    expect(m.bottlenecks.waitingForInventory).toEqual([{ sku: 1, name: "Speakers", orders: 7 }]);
    expect(m.bottlenecks.waitingForCapacityOrders).toBe(6);
    const lines = describeScene(m).lines.join(" ");
    expect(lines).toContain("Waiting for inventory: Speakers, 7 orders.");
    expect(lines).toContain("Waiting for packing capacity: 6 orders.");
  });

  it("shows the dock closed and a plain-language cause during a supplier outage", () => {
    const f = makeFrame({
      disruptions: [
        { kind: "supplier_outage", origin: "script", inputSeq: 1, target: 1, magnitudeBps: 10000, onsetTick: 8, endTick: 40, ticksSinceOnset: 2 },
      ],
    });
    const m = buildSceneModel(f, null, NAMES);
    expect(m.dock.closed).toBe(true);
    expect(m.disruptions[0]?.text).toBe("The Speakers supplier is down. Deliveries are paused.");
    expect(describeScene(m).disruptions).toEqual(["Supplier outage: The Speakers supplier is down. Deliveries are paused."]);
  });

  it("builds hotspots with the authoritative number in the tooltip", () => {
    const m = buildSceneModel(makeFrame({ skus: [sku(0, 312, 1, 150), sku(1, 50)] }), null, NAMES);
    const rack = m.hotspots.find((h) => h.id === "rack-0");
    expect(rack?.tooltip).toBe("Chargers: 312 in stock 1 delivery on the way (150 units).");
    expect(m.hotspots.map((h) => h.id)).toEqual(["rack-0", "rack-1", "packing", "dock", "orders", "outbound"]);
    for (const h of m.hotspots) {
      expect(h.rect.w).toBeGreaterThan(0);
      expect(h.rect.h).toBeGreaterThan(0);
    }
  });

  it("carries events from the previous frame and is pure", () => {
    const a = makeFrame();
    const b = next(a, { shipped: 12 });
    const m = buildSceneModel(b, a, NAMES);
    expect(m.events.some((e) => e.kind === "shipment")).toBe(true);
    expect(buildSceneModel(b, a, NAMES)).toEqual(m);
    expect(buildSceneModel(b, null, NAMES).events).toEqual([]);
  });

  it("describes the hour in business language", () => {
    expect(clockLabel(19)).toBe("Thursday 7:00 PM");
    expect(clockLabel(24)).toBe("Friday 12:00 AM");
    expect(clockLabel(36)).toBe("Friday 12:00 PM");
    const m = buildSceneModel(makeFrame({ tick: 19, workersOnShift: 20, orderQueueLen: 14 }), null, NAMES);
    const s = describeScene(m);
    expect(s.headline).toBe("Thursday 7:00 PM.");
    expect(s.lines).toContain("20 people on shift.");
    expect(s.lines).toContain("14 orders waiting.");
  });
});
