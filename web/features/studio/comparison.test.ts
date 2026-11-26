import { describe, expect, it } from "vitest";
import type { Branch, Metrics } from "@/lib/api/client";
import { advanceRequestFor, commonTick, defaultPartnerId, diffMetrics, divergenceTick, hashNote, paneStatus } from "./comparison";

const br = (id: string, parent: string | null, forkTick: number, createdAt = "2026-01-01T00:00:00Z"): Branch =>
  ({ id, worldId: "w", parentBranchId: parent, forkTick, headTick: forkTick, label: id, managerKind: "human", createdAt }) as Branch;

const m = (over: Partial<Metrics>): Metrics =>
  ({ cashCents: 0, netProfitCents: 0, serviceLevelBps: 10000, backlogOrders: 0, ...over }) as Metrics;

describe("commonTick", () => {
  it("is the shown hour when both realities have reached it", () => {
    expect(commonTick(10, 20, 30)).toBe(10);
  });
  it("is the lower head when one reality is behind", () => {
    expect(commonTick(25, 40, 12)).toBe(12);
    expect(commonTick(25, 8, 40)).toBe(8);
  });
});

describe("diffMetrics", () => {
  it("subtracts the two server values (b minus a) without any other computation", () => {
    const rows = diffMetrics(
      m({ cashCents: 1_000_000, netProfitCents: 50_000, serviceLevelBps: 9000, backlogOrders: 10 }),
      m({ cashCents: 1_012_550, netProfitCents: 20_000, serviceLevelBps: 9250, backlogOrders: 4 }),
    );
    const by = Object.fromEntries(rows.map((r) => [r.key, r]));
    expect(by.cash?.delta).toBe(12_550);
    expect(by.cash?.diff).toBe("+$125.50");
    expect(by.netProfit?.diff).toBe("-$300.00");
    expect(by.serviceLevel?.delta).toBe(250);
    expect(by.serviceLevel?.diff).toBe("+2.5 points");
    expect(by.backlog?.diff).toBe("-6");
    expect(by.cash?.a).toBe("$10,000.00");
  });
  it("shows no sign for no difference", () => {
    const rows = diffMetrics(m({}), m({}));
    expect(rows.map((r) => r.delta)).toEqual([0, 0, 0, 0]);
    expect(rows[0]?.diff).toBe("$0.00");
  });
});

describe("pane status and advance request", () => {
  it("marks hours beyond the head as not simulated", () => {
    expect(paneStatus(8, 8)).toBe("ready");
    expect(paneStatus(9, 8)).toBe("not_simulated");
  });
  it("asks for an advance only beyond the head, never when rewinding", () => {
    expect(advanceRequestFor(8, 9)).toEqual({ expectedHeadTick: 8, runToTick: 9 });
    expect(advanceRequestFor(8, 8)).toBeNull();
    expect(advanceRequestFor(8, 3)).toBeNull();
  });
});

describe("divergenceTick", () => {
  const root = br("r", null, 0);
  const a = br("a", "r", 10, "2026-01-01T00:00:01Z");
  const b = br("b", "r", 20, "2026-01-01T00:00:02Z");
  const aa = br("aa", "a", 30, "2026-01-01T00:00:03Z");
  const all = [root, a, b, aa];
  it("is the fork tick for parent and child", () => {
    expect(divergenceTick(root, a, all)).toBe(10);
    expect(divergenceTick(a, root, all)).toBe(10);
  });
  it("is the earlier fork tick for siblings", () => {
    expect(divergenceTick(a, b, all)).toBe(10);
  });
  it("walks through grandchildren", () => {
    expect(divergenceTick(root, aa, all)).toBe(10);
    expect(divergenceTick(b, aa, all)).toBe(10);
    expect(divergenceTick(a, aa, all)).toBe(30);
  });
  it("is null for unrelated branches", () => {
    expect(divergenceTick(root, br("x", null, 0), all)).toBeNull();
  });
});

describe("defaultPartnerId", () => {
  it("prefers the parent, then the oldest child", () => {
    const root = br("r", null, 0);
    const a = br("a", "r", 10, "2026-01-01T00:00:01Z");
    const b = br("b", "r", 12, "2026-01-01T00:00:02Z");
    expect(defaultPartnerId(a, [root, a, b])).toBe("r");
    expect(defaultPartnerId(root, [root, b, a])).toBe("a");
    expect(defaultPartnerId(root, [root])).toBeNull();
  });
});

describe("hashNote", () => {
  it("expects identical hashes up to the divergence", () => {
    expect(hashNote(4, 4, "x", "x")).toContain("Identical, as expected");
    expect(hashNote(3, 4, "x", "y")).toContain("not expected");
  });
  it("expects different hashes afterwards", () => {
    expect(hashNote(9, 4, "x", "y")).toContain("Different, as expected");
  });
});
