import { describe, expect, it } from "vitest";
import { clamp01, easeInOut, easeOut, lerpPoint, MIN_TRAVEL_INTERVAL_MS, pointOnPath, travelEnabled, tweenProgress, type Point } from "./interpolate";
import { buildSceneModel, describeScene, type SceneModel } from "./sceneModel";
import { buildSceneLabels } from "./sceneLabels";
import { makeFrame, next, sku } from "./frames.testutil";

const NAMES = ["Chargers", "Speakers"];
const STEPS = [0, 0.25, 0.5, 1] as const;

describe("easing and progress", () => {
  it("starts at 0, ends at 1, stays in [0, 1] and is monotonic", () => {
    for (const f of [easeOut, easeInOut]) {
      expect(f(0)).toBe(0);
      expect(f(1)).toBe(1);
      let last = -1;
      for (let i = 0; i <= 200; i++) {
        const v = f(i / 200);
        expect(v).toBeGreaterThanOrEqual(0);
        expect(v).toBeLessThanOrEqual(1);
        expect(v).toBeGreaterThanOrEqual(last - 1e-12);
        last = v;
      }
    }
  });

  it("tweenProgress waits for the delay and clamps at the duration", () => {
    expect(tweenProgress(50, 100, 200)).toBe(0);
    expect(tweenProgress(200, 100, 200)).toBe(0.5);
    expect(tweenProgress(10_000, 100, 200)).toBe(1);
    expect(clamp01(-3)).toBe(0);
    expect(clamp01(7)).toBe(1);
  });
});

describe("pointOnPath", () => {
  const path: Point[] = [
    { x: 0, y: 0 },
    { x: 100, y: 0 },
    { x: 100, y: 100 },
  ];

  it("starts on the first point and ends exactly on the last point", () => {
    expect(pointOnPath(path, 0)).toEqual({ x: 0, y: 0 });
    const end = pointOnPath(path, 1, easeInOut, 12);
    expect(end.x).toBeCloseTo(100, 9);
    expect(end.y).toBeCloseTo(100, 9);
  });

  it("stays inside the path's bounding box when there is no arc, even for out-of-range progress", () => {
    for (let i = -10; i <= 20; i++) {
      const p = pointOnPath(path, i / 10);
      expect(p.x).toBeGreaterThanOrEqual(0);
      expect(p.x).toBeLessThanOrEqual(100);
      expect(p.y).toBeGreaterThanOrEqual(0);
      expect(p.y).toBeLessThanOrEqual(100);
    }
  });

  it("lifts mid-segment by the arc and not at a vertex", () => {
    const flat = pointOnPath(path, 0.25, (k) => k, 0);
    const arched = pointOnPath(path, 0.25, (k) => k, 12);
    expect(arched.y).toBeCloseTo(flat.y - 12, 9);
    const atVertex = pointOnPath(path, 0.5, (k) => k, 12);
    expect(atVertex.y).toBeCloseTo(0, 9);
  });

  it("degenerate paths do not throw", () => {
    expect(pointOnPath([], 0.5)).toEqual({ x: 0, y: 0 });
    expect(pointOnPath([{ x: 3, y: 4 }], 0.5)).toEqual({ x: 3, y: 4 });
    expect(lerpPoint({ x: 0, y: 0 }, { x: 10, y: 20 }, 0.5)).toEqual({ x: 5, y: 10 });
  });
});

describe("travelEnabled", () => {
  const base = { reducedMotion: false, animate: true, intervalMs: 1000 };
  it("allows travel only without reduced motion, with animate and at a readable frame interval", () => {
    expect(travelEnabled(base)).toBe(true);
    expect(travelEnabled({ ...base, reducedMotion: true })).toBe(false);
    expect(travelEnabled({ ...base, animate: false })).toBe(false);
    expect(travelEnabled({ ...base, intervalMs: MIN_TRAVEL_INTERVAL_MS - 1 })).toBe(false);
    expect(travelEnabled({ ...base, intervalMs: MIN_TRAVEL_INTERVAL_MS })).toBe(true);
  });
});

/** Every string the scene would render for a model: labels, tooltips, accessible summary. */
function renderedText(m: SceneModel): string[] {
  const specs = buildSceneLabels(m);
  const d = describeScene(m);
  return [
    ...specs.banners.map((b) => b.text),
    ...specs.labels.flatMap((l) => [...l.variants, l.fact ?? ""]),
    ...m.hotspots.flatMap((h) => [h.label, h.tooltip]),
    d.headline,
    ...d.lines,
    ...d.disruptions,
    ...d.interventions,
  ];
}

function numbersIn(text: string[]): string[] {
  return text.flatMap((t) => t.match(/\d+/g) ?? []);
}

describe("every rendered number equals the displayed Tick_Frame at all interpolation steps", () => {
  const prev = makeFrame({
    tick: 10,
    skus: [sku(0, 230, 2, 400), sku(1, 50)],
    workersOnShift: 9,
    orderQueueLen: 14,
  });
  const shown = next(prev, {
    shipped: 7,
    skus: [sku(0, 190, 1, 150), sku(1, 50)],
    workersOnShift: 8,
    orderQueueLen: 11,
  });
  const model = buildSceneModel(shown, prev, NAMES, { regularWorkers: 10 });

  // The travelling actors, as paths the scene would fly along.
  const actorPath: Point[] = [
    { x: 10, y: 10 },
    { x: 200, y: 60 },
    { x: 320, y: 140 },
  ];

  it("the frame pair really produces travel (the test is not vacuous)", () => {
    expect(model.events.map((e) => e.kind)).toEqual(expect.arrayContaining(["delivery", "shipment", "stock_picked", "orders_arrived"]));
  });

  it("text and numbers are identical at t = 0, 0.25, 0.5 and 1 and match the displayed frame", () => {
    const final = renderedText(model);
    const finalNumbers = numbersIn(final).sort();
    const positions: Point[] = [];
    for (const t of STEPS) {
      // Interpolating moves positions only; the displayed frame (and so the model) is `shown` at every step.
      positions.push(pointOnPath(actorPath, t, easeInOut, 12));
      const atStep = buildSceneModel(shown, prev, NAMES, { regularWorkers: 10 });
      expect(renderedText(atStep)).toEqual(final);
      expect(numbersIn(renderedText(atStep)).sort()).toEqual(finalNumbers);
    }
    // Positions did change across the steps, text did not.
    expect(new Set(positions.map((p) => `${p.x.toFixed(3)},${p.y.toFixed(3)}`)).size).toBe(STEPS.length);

    const nums = new Set(finalNumbers);
    // Values of the displayed frame appear.
    for (const v of [190, 50, 11, 8, 7]) expect(nums.has(String(v))).toBe(true);
    // Values that exist only in the previous frame never appear, and no in-between value does.
    for (const v of [230, 14, 9, 210, 220, 12, 13]) expect(nums.has(String(v))).toBe(false);
  });

  it("the model carries no progress value, so a number cannot depend on t", () => {
    expect(JSON.stringify(model)).not.toMatch(/"(progress|t|k|alpha)"\s*:/);
  });
});
