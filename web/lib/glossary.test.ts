import { describe, expect, it } from "vitest";
import {
  divRoundHalfEven,
  formatHorizon,
  formatHorizonEnd,
  formatHours,
  formatMoney,
  formatPercentBps,
  formatSimTime,
  metricExplanation,
  parseStartClock,
  productName,
  productNameByIndex,
} from "./glossary";

const START = "Thu 00:00";

describe("formatSimTime", () => {
  it.each([
    [0, "Thursday, 12:00 AM"],
    [12, "Thursday, 12:00 PM"],
    [19, "Thursday, 7:00 PM"],
    [24, "Friday, 12:00 AM"],
    [119, "Monday, 11:00 PM"],
    [120, "Tuesday, 12:00 AM"],
  ])("tick %i", (tick, label) => {
    expect(formatSimTime(tick, START)).toBe(label);
  });
  it("short style", () => {
    expect(formatSimTime(19, START, "short")).toBe("Thu 7:00 PM");
  });
  it("respects a non-midnight start and weekday wrap", () => {
    expect(formatSimTime(0, "Sun 18:30")).toBe("Sunday, 6:30 PM");
    expect(formatSimTime(6, "Sun 18:30")).toBe("Monday, 12:30 AM");
  });
  it("falls back on a malformed start label", () => {
    expect(parseStartClock("nonsense")).toEqual({ weekday: 3, minuteOfDay: 0 });
  });
  it("horizon labels", () => {
    expect(formatHorizon(120)).toBe("Five days");
    expect(formatHorizon(24)).toBe("One day");
    expect(formatHorizonEnd(120, START)).toBe("Monday, 11:00 PM");
    expect(formatHours(1)).toBe("1 hour");
    expect(formatHours(8)).toBe("8 hours");
  });
});

describe("formatMoney", () => {
  it.each([
    [0, "$0.00"],
    [5, "$0.05"],
    [5999, "$59.99"],
    [10000000, "$100,000.00"],
    [-12345, "-$123.45"],
    [123456789, "$1,234,567.89"],
  ])("%i cents", (c, s) => {
    expect(formatMoney(c)).toBe(s);
  });
  it("signed and whole", () => {
    expect(formatMoney(124000, { signed: true })).toBe("+$1,240.00");
    expect(formatMoney(0, { signed: true })).toBe("$0.00");
    expect(formatMoney(150, { precision: "whole" })).toBe("$2");
    expect(formatMoney(50, { precision: "whole" })).toBe("$0");
    expect(formatMoney(250, { precision: "whole" })).toBe("$2");
    expect(formatMoney(-49, { precision: "whole" })).toBe("$0");
    expect(formatMoney(-150, { precision: "whole" })).toBe("-$2");
  });
});

describe("formatPercentBps", () => {
  it.each([
    [10000, "100.0%"],
    [9650, "96.5%"],
    [0, "0.0%"],
    [5, "0.0%"], // 0.5 tenth rounds to even (0)
    [15, "0.2%"], // 1.5 tenths rounds to even (2)
    [25, "0.2%"], // 2.5 tenths rounds to even (2)
    [9999, "100.0%"],
    [1234, "12.3%"],
    [-250, "-2.5%"],
  ])("%i bps", (bps, s) => {
    expect(formatPercentBps(bps)).toBe(s);
  });
});

describe("divRoundHalfEven", () => {
  it("rounds halves to even, including negatives", () => {
    expect(divRoundHalfEven(5, 2)).toBe(2);
    expect(divRoundHalfEven(7, 2)).toBe(4);
    expect(divRoundHalfEven(-5, 2)).toBe(-2);
    expect(divRoundHalfEven(-7, 2)).toBe(-4);
    expect(divRoundHalfEven(4, 3)).toBe(1);
  });
});

describe("products and copy", () => {
  it("maps ids, never SKU n", () => {
    expect(productName("watchband")).toBe("Watch bands");
    expect(productName("phone_case")).toBe("Phone Case");
    expect(productNameByIndex([{ id: "headphones" }, { id: "speaker" }], 1)).toBe("Speakers");
    expect(productNameByIndex([], 3)).toBe("Product");
  });
  it("metric explanations", () => {
    expect(metricExplanation("backlog", { lateBacklogOrders: 3 })).toContain("3 of them are already late");
    expect(metricExplanation("backlog", { lateBacklogOrders: 1 })).toContain("1 of them is already late");
    expect(metricExplanation("fillRate")).toContain("items");
  });
});

describe("rejection copy", () => {
  it("maps every server reject code to a plain sentence", async () => {
    const { rejectionMessage, requestFailureMessage, priceRangeCents, accountLabel } = await import("./glossary");
    for (const code of [
      "budget_exhausted",
      "insufficient_cash",
      "supplier_unavailable",
      "price_out_of_bounds",
      "invalid_quantity",
      "unknown_sku",
      "unknown_supplier",
    ]) {
      const text = rejectionMessage(code);
      expect(text).not.toContain("_");
      expect(text).not.toBe(rejectionMessage("nonsense"));
    }
    expect(rejectionMessage("insufficient_cash")).toBe("Not enough cash for this order.");
    expect(rejectionMessage(undefined)).toBe("That move wasn't accepted.");
    expect(requestFailureMessage("stale-head")).toMatch(/situation changed/);
    expect(requestFailureMessage("decision-stale")).toMatch(/discarded\. Press Continue to try again\./);
    expect(priceRangeCents(5999)).toEqual({ min: 3000, max: 11998 });
    expect(priceRangeCents(5999, { minCents: 4800, maxCents: 7200 })).toEqual({ min: 4800, max: 7200 });
    expect(rejectionMessage("marketing_exceeds_limit")).toContain("marketing");
    expect(accountLabel("Cost_Of_Goods_Sold")).toBe("Cost of goods shipped");
  });
});
