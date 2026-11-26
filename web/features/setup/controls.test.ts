import { describe, expect, it } from "vitest";
import type { Assumption } from "@/lib/api/client";
import { buildControls, buildOverrides, cleanRationale, clamp, isValid, parseDollarsToCents } from "./controls";

function a(path: string, value: number, min: number, max: number, rationale = "synthetic, illustrative: x"): Assumption {
  return { path, value, unit: "u", default: value, min, max, rationale, overridden: false };
}

const skus = [{ id: "headphones" }, { id: "speaker" }] as { id: string }[];
const assumptions: Assumption[] = [
  a("openingCashCents", 10000000, 0, 1000000000),
  a("costs.outboundShipCentsPerUnit", 400, 0, 5000),
  a("workforce.regularWorkers", 20, 1, 200),
  a("workforce.unitsPerWorkerHour", 8, 1, 60),
  a("suppliers.headphones.primary.leadTimeTicks", 24, 1, 240),
  a("suppliers.speaker.primary.leadTimeTicks", 24, 2, 100),
  a("suppliers.headphones.backup.leadTimeTicks", 8, 1, 240),
  a("suppliers.speaker.backup.leadTimeTicks", 8, 1, 240),
  a("skus.headphones.referencePriceCents", 5999, 100, 100000),
  // speaker price missing: its control is omitted
];

describe("buildControls", () => {
  const controls = buildControls(assumptions, { skus: skus as never });
  const byId = (id: string) => controls.find((c) => c.id === id);

  it("reads bounds from the API and omits controls with missing assumptions", () => {
    expect(byId("cash")?.max).toBe(1000000000);
    expect(byId("price.headphones")?.label).toBe("Headphones price");
    expect(byId("price.speaker")).toBeUndefined();
  });
  it("uses the intersection of bounds for grouped paths", () => {
    const c = byId("primaryLead");
    expect(c?.min).toBe(2);
    expect(c?.max).toBe(100);
    expect(c?.paths).toHaveLength(2);
  });
  it("formats with plain units", () => {
    expect(byId("staff")?.format(20)).toBe("20 people");
    expect(byId("speed")?.format(8)).toBe("8 items per person per hour");
    expect(byId("backupLead")?.format(8)).toBe("8 hours");
    expect(byId("cash")?.format(10000000)).toBe("$100,000");
  });
  it("blocks values outside bounds", () => {
    const c = byId("staff")!;
    expect(isValid(c, 0)).toBe(false);
    expect(isValid(c, 201)).toBe(false);
    expect(isValid(c, 1.5)).toBe(false);
    expect(isValid(c, 30)).toBe(true);
    expect(clamp(c, 999)).toBe(200);
  });
  it("builds overrides only for changed values, writing every grouped path", () => {
    expect(buildOverrides(controls, {})).toEqual({});
    expect(buildOverrides(controls, { staff: 20, backupLead: 6 })).toEqual({
      "suppliers.headphones.backup.leadTimeTicks": 6,
      "suppliers.speaker.backup.leadTimeTicks": 6,
    });
  });
});

describe("helpers", () => {
  it("parses dollars to cents with integer math", () => {
    expect(parseDollarsToCents("59.99")).toBe(5999);
    expect(parseDollarsToCents("$1,200.5")).toBe(120050);
    expect(parseDollarsToCents("60")).toBe(6000);
    expect(parseDollarsToCents("abc")).toBeNull();
    expect(parseDollarsToCents("1.234")).toBeNull();
    expect(parseDollarsToCents("-5")).toBeNull();
  });
  it("cleans rationale prefixes", () => {
    expect(cleanRationale("synthetic, illustrative; uncalibrated: $100,000 of cash")).toBe("$100,000 of cash");
  });
});
