import { describe, expect, it } from "vitest";
import { deriveSceneEvents } from "./deriveSceneEvents";
import { makeFrame, next, sku } from "./frames.testutil";

const dis = (over: Record<string, unknown> = {}) => ({
  kind: "supplier_outage" as const,
  origin: "script" as const,
  inputSeq: 1,
  target: 0,
  magnitudeBps: 10000,
  onsetTick: 11,
  endTick: 40,
  ticksSinceOnset: 0,
  ...over,
});

describe("deriveSceneEvents", () => {
  it("returns nothing without a previous frame or for non-consecutive ticks", () => {
    const a = makeFrame();
    expect(deriveSceneEvents(null, a)).toEqual([]);
    expect(deriveSceneEvents(undefined, a)).toEqual([]);
    expect(deriveSceneEvents(a, a)).toEqual([]);
    expect(deriveSceneEvents(next(next(a)), a)).toEqual([]);
    expect(deriveSceneEvents(a, { ...next(next(a)) })).toEqual([]);
  });

  it("emits nothing when the frames are the same hour in every respect", () => {
    const a = makeFrame();
    expect(deriveSceneEvents(a, next(a))).toEqual([]);
  });

  it("sends a truck only when open purchase orders decrease", () => {
    const a = makeFrame({ skus: [sku(0, 100, 2, 300), sku(1, 50)] });
    const received = next(a, { skus: [sku(0, 250, 1, 150), sku(1, 50)] });
    expect(deriveSceneEvents(a, received)).toEqual([{ kind: "delivery", sku: 0, trucks: 1, hiddenTrucks: 0, boxes: 6 }]);
    // PO count unchanged (a new order may have replaced the delivered one): not animated.
    const ambiguous = next(a, { skus: [sku(0, 250, 2, 300), sku(1, 50)] });
    expect(deriveSceneEvents(a, ambiguous)).toEqual([]);
    // PO count increases (a new order was placed): no truck.
    const placed = next(a, { skus: [sku(0, 100, 3, 450), sku(1, 50)] });
    expect(deriveSceneEvents(a, placed)).toEqual([]);
  });

  it("caps trucks per hour and reports the exact remainder", () => {
    const a = makeFrame({ skus: [sku(0, 0, 4, 400), sku(1, 0, 2, 100)] });
    const b = next(a, { skus: [sku(0, 400, 0, 0), sku(1, 100, 1, 50)] });
    const ev = deriveSceneEvents(a, b).filter((e) => e.kind === "delivery");
    expect(ev).toEqual([
      { kind: "delivery", sku: 0, trucks: 3, hiddenTrucks: 1, boxes: 6 },
      { kind: "delivery", sku: 1, trucks: 0, hiddenTrucks: 1, boxes: 5 },
    ]);
  });

  it("makes vans from shipped orders, ten orders per van, capped at six", () => {
    const a = makeFrame();
    const one = deriveSceneEvents(a, next(a, { shipped: 1 })).find((e) => e.kind === "shipment");
    expect(one).toEqual({ kind: "shipment", orders: 1, vans: 1, hiddenVans: 0 });
    const eleven = deriveSceneEvents(a, next(a, { shipped: 11 })).find((e) => e.kind === "shipment");
    expect(eleven).toMatchObject({ vans: 2 });
    const many = deriveSceneEvents(a, next(a, { shipped: 95 })).find((e) => e.kind === "shipment");
    expect(many).toEqual({ kind: "shipment", orders: 95, vans: 6, hiddenVans: 4 });
    expect(deriveSceneEvents(a, next(a, { shipped: 0 })).some((e) => e.kind === "shipment")).toBe(false);
  });

  it("moves boxes from the rack whose stock fell, exact when inbound is unchanged", () => {
    const a = makeFrame({ skus: [sku(0, 200), sku(1, 50)] });
    const b = next(a, { shipped: 3, skus: [sku(0, 140), sku(1, 50)], orderQueueLen: 5 });
    expect(deriveSceneEvents(a, b).filter((e) => e.kind === "stock_picked")).toEqual([
      { kind: "stock_picked", sku: 0, units: 60, boxes: 3, exact: true },
    ]);
  });

  it("falls back to the largest decrease and flags it when inbound also changed", () => {
    const a = makeFrame({ skus: [sku(0, 200, 1, 100), sku(1, 50)] });
    const b = next(a, { shipped: 3, skus: [sku(0, 150, 0, 0), sku(1, 50)] });
    expect(deriveSceneEvents(a, b).filter((e) => e.kind === "stock_picked")).toEqual([
      { kind: "stock_picked", sku: 0, units: 50, boxes: 3, exact: false },
    ]);
  });

  it("draws no pick when nothing shipped", () => {
    const a = makeFrame({ skus: [sku(0, 200), sku(1, 50)] });
    const b = next(a, { shipped: 0, skus: [sku(0, 190), sku(1, 50)] });
    expect(deriveSceneEvents(a, b).some((e) => e.kind === "stock_picked")).toBe(false);
  });

  it("reports new orders as a lower bound, capped at eight tickets", () => {
    const a = makeFrame({ orderQueueLen: 5 });
    // queue 5 -> 9 with 3 shipped: at least 7 arrived.
    expect(deriveSceneEvents(a, next(a, { orderQueueLen: 9, shipped: 3 })).find((e) => e.kind === "orders_arrived")).toEqual({
      kind: "orders_arrived",
      atLeast: 7,
      drawn: 7,
    });
    expect(deriveSceneEvents(a, next(a, { orderQueueLen: 60, shipped: 0 })).find((e) => e.kind === "orders_arrived")).toEqual({
      kind: "orders_arrived",
      atLeast: 55,
      drawn: 8,
    });
    // Queue shrank by more than shipped is impossible to explain: no arrival.
    expect(deriveSceneEvents(a, next(a, { orderQueueLen: 1, shipped: 0 })).some((e) => e.kind === "orders_arrived")).toBe(false);
  });

  it("marks a disruption onset only in the hour it starts", () => {
    const a = makeFrame();
    const onset = next(a, { disruptions: [dis()] });
    expect(deriveSceneEvents(a, onset)).toEqual([{ kind: "disruption_onset", disruption: "supplier_outage", targetSku: 0 }]);
    const later = next(onset, { disruptions: [dis({ ticksSinceOnset: 1 })] });
    expect(deriveSceneEvents(onset, later)).toEqual([]);
    // Jumping into a frame with ticksSinceOnset 0 that the previous frame already had: no replay.
    const same = next(onset, { disruptions: [dis({ ticksSinceOnset: 0 })] });
    expect(deriveSceneEvents(onset, same)).toEqual([]);
  });

  it("is a pure function", () => {
    const a = makeFrame({ skus: [sku(0, 100, 2, 300), sku(1, 50)] });
    const b = next(a, { shipped: 12, skus: [sku(0, 250, 1, 150), sku(1, 30)], orderQueueLen: 9 });
    expect(deriveSceneEvents(a, b)).toEqual(deriveSceneEvents(a, b));
  });
});
