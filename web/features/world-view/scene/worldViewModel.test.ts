import { describe, expect, it } from "vitest";
import type { TickFrame } from "@/lib/api/client";
import { buildWorldViewModel, maxOnHandSeen, provenanceLabel } from "./worldViewModel";

function frame(over: Partial<TickFrame> = {}): TickFrame {
  return {
    tick: 30,
    stateHash: "ab".repeat(32),
    skus: [
      { sku: 1, onHand: 50, inboundPOs: 0, inboundUnits: 0 },
      { sku: 0, onHand: 200, inboundPOs: 2, inboundUnits: 300 },
    ],
    workersOnShift: 6,
    tempsOnShift: 2,
    tempsOnboarding: 1,
    orderQueueLen: 17,
    disruptions: [
      {
        kind: "supplier_outage",
        origin: "script",
        inputSeq: 1,
        target: 0,
        magnitudeBps: 10000,
        onsetTick: 28,
        endTick: 60,
        ticksSinceOnset: 2,
      },
    ],
    report: {
      tick: 30,
      capacity: 100,
      capacityUsed: 100,
      shippedOrders: 40,
      skus: [
        { sku: 1, stockBoundOrders: 5, stockBoundUnits: 5, capacityBoundOrders: 3, capacityBoundUnits: 3 },
      ],
    },
    markers: [],
    metrics: {} as TickFrame["metrics"],
    ...over,
  };
}

describe("buildWorldViewModel", () => {
  it("copies figures from the frame and sorts bins by SKU", () => {
    const m = buildWorldViewModel(frame());
    expect(m.tick).toBe(30);
    expect(m.bins.map((b) => b.sku)).toEqual([0, 1]);
    expect(m.workers).toEqual({ regular: 6, temps: 2, onboarding: 1, onShift: 8 });
    expect(m.orderQueueLen).toBe(17);
    expect(m.inbound).toEqual([{ sku: 0, pos: 2, units: 300 }]);
  });

  it("derives bin fill from the display maximum", () => {
    const m = buildWorldViewModel(frame());
    expect(m.stockScaleMax).toBe(200);
    expect(m.bins.map((b) => b.fillBps)).toEqual([10000, 2500]);
    const scaled = buildWorldViewModel(frame(), { stockScaleMax: 400 });
    expect(scaled.bins.map((b) => b.fillBps)).toEqual([5000, 1250]);
  });

  it("never lets a bin overflow when the supplied maximum is too small", () => {
    const m = buildWorldViewModel(frame(), { stockScaleMax: 10 });
    expect(m.stockScaleMax).toBe(200);
    expect(m.bins[0]?.fillBps).toBe(10000);
  });

  it("handles an empty warehouse without dividing by zero", () => {
    const m = buildWorldViewModel(frame({ skus: [{ sku: 0, onHand: 0, inboundPOs: 0, inboundUnits: 0 }] }));
    expect(m.stockScaleMax).toBe(1);
    expect(m.bins[0]?.fillBps).toBe(0);
  });

  it("maps Disruptions to zones with ticks since onset", () => {
    const m = buildWorldViewModel(frame());
    expect(m.disruptions).toHaveLength(1);
    expect(m.disruptions[0]).toMatchObject({ zone: "inbound_dock", ticksSinceOnset: 2, targetSku: 0 });
    expect(m.highlightedZones).toEqual(["inbound_dock"]);
  });

  it("reports stock-bound and capacity-bound bottlenecks with counts", () => {
    const m = buildWorldViewModel(frame());
    expect(m.bottlenecks.stockBound).toEqual([{ sku: 1, orders: 5 }]);
    expect(m.bottlenecks.capacityBoundOrders).toBe(3);
    expect(m.bins[1]?.stockBoundOrders).toBe(5);
  });

  it("shows only interventions effective at the displayed Tick, with provenance", () => {
    const m = buildWorldViewModel(frame(), {
      interventions: [
        { tick: 30, text: "Order 100 of SKU 1", manager: "human" },
        { tick: 31, text: "later", manager: "human" },
        { tick: 30, text: "Add 2 temps", manager: "ai_rule_based" },
      ],
    });
    expect(m.interventions).toEqual([
      { tick: 30, text: "Order 100 of SKU 1", provenance: "Human" },
      { tick: 30, text: "Add 2 temps", provenance: "Rule-based AI" },
    ]);
  });

  it("is a pure function of its inputs", () => {
    expect(buildWorldViewModel(frame())).toEqual(buildWorldViewModel(frame()));
  });

  it("carries the syncing flag", () => {
    expect(buildWorldViewModel(frame(), { syncing: true }).syncing).toBe(true);
  });
});

describe("helpers", () => {
  it("maxOnHandSeen scans every frame", () => {
    expect(maxOnHandSeen([frame(), frame({ skus: [{ sku: 0, onHand: 900, inboundPOs: 0, inboundUnits: 0 }] })])).toBe(900);
    expect(maxOnHandSeen([])).toBe(0);
  });

  it("names AI kinds and fallbacks", () => {
    expect(provenanceLabel({ manager: "ai_llm", model: "m1" })).toBe("LLM AI (m1)");
    expect(provenanceLabel({ manager: "ai_llm", fallbackReason: "timeout" })).toBe(
      "Rule-based AI (LLM fallback: timeout)",
    );
  });
});
