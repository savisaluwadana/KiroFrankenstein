// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { WorldViewSummary } from "./WorldViewSummary";
import { buildSceneModel } from "./scene/sceneModel";
import { makeFrame, sku } from "./scene/frames.testutil";

afterEach(cleanup);

describe("WorldViewSummary", () => {
  it("reads in business language with product names", () => {
    const model = buildSceneModel(makeFrame({ tick: 19, skus: [sku(0, 312), sku(1, 50)], orderQueueLen: 14 }), null, ["Chargers", "Speakers"]);
    render(<WorldViewSummary model={model} announce />);
    const el = screen.getByTestId("world-summary");
    expect(el.textContent).toContain("Thursday 7:00 PM.");
    expect(el.textContent).toContain("In stock: Chargers 312, Speakers 50.");
    expect(el.textContent).not.toMatch(/SKU|tick/i);
    expect(el.getAttribute("aria-live")).toBe("polite");
  });

  it("does not announce by default and handles no data", () => {
    const { container } = render(<WorldViewSummary model={null} />);
    expect(container.firstElementChild?.getAttribute("aria-live")).toBe("off");
    expect(container.textContent).toContain("has not loaded");
  });
});
