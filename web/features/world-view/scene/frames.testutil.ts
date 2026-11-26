import type { TickFrame } from "@/lib/api/client";

export function sku(
  s: number,
  onHand: number,
  inboundPOs = 0,
  inboundUnits = 0,
): TickFrame["skus"][number] {
  return { sku: s, onHand, inboundPOs, inboundUnits };
}

export function makeFrame(over: Partial<TickFrame> = {}): TickFrame {
  return {
    tick: 10,
    stateHash: "ab".repeat(32),
    skus: [sku(0, 200), sku(1, 50)],
    workersOnShift: 8,
    tempsOnShift: 0,
    tempsOnboarding: 0,
    orderQueueLen: 5,
    disruptions: [],
    report: { tick: 10, capacity: 100, capacityUsed: 0, shippedOrders: 0, skus: [] },
    markers: [],
    metrics: {} as TickFrame["metrics"],
    ...over,
  };
}

export function next(prev: TickFrame, over: Partial<TickFrame> & { shipped?: number } = {}): TickFrame {
  const { shipped, ...rest } = over;
  return makeFrame({
    ...prev,
    tick: prev.tick + 1,
    report: { ...prev.report, tick: prev.tick + 1, shippedOrders: shipped ?? 0 },
    ...rest,
  });
}
