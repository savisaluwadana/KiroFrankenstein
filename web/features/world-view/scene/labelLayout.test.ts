import { describe, expect, it } from "vitest";
import type { TickFrame } from "@/lib/api/client";
import { SCENE_H, SCENE_W } from "./iso";
import { estimateMeasure, intersects, layoutBanners, layoutLabels, type Rect } from "./labelLayout";
import { buildSceneLabels } from "./sceneLabels";
import { buildSceneModel, describeScene } from "./sceneModel";
import { makeFrame, sku } from "./frames.testutil";

const NAMES = ["Headphones", "Speakers", "Chargers", "Watch bands"];
const BOUNDS: Rect = { x: 4, y: 4, w: SCENE_W - 8, h: SCENE_H - 8 };

/** Supplier outage + demand surge + staff absence + four stocked-out products. */
function crisisFrame(over: Partial<TickFrame> = {}): TickFrame {
  const waiting = [1064, 1359, 709, 978];
  return makeFrame({
    tick: 60,
    skus: [sku(0, 0), sku(1, 0), sku(2, 0), sku(3, 0)],
    workersOnShift: 8,
    orderQueueLen: 4110,
    disruptions: [
      { kind: "supplier_outage", target: 2 },
      { kind: "workforce_absence", target: -1 },
      { kind: "demand_surge", target: -1 },
    ] as unknown as TickFrame["disruptions"],
    report: {
      tick: 60,
      capacity: 100,
      capacityUsed: 0,
      shippedOrders: 0,
      skus: waiting.map((n, i) => ({ sku: i, stockBoundOrders: n })) as unknown as TickFrame["report"]["skus"],
    },
    ...over,
  });
}

function run(frame: TickFrame) {
  const m = buildSceneModel(frame, null, NAMES, { regularWorkers: 14 });
  const { banners, labels } = buildSceneLabels(m);
  const bannerRects = layoutBanners(banners, estimateMeasure);
  const result = layoutLabels(labels, estimateMeasure, BOUNDS, bannerRects.map((b) => b.rect));
  return { m, bannerRects, result, labels };
}

describe("layoutLabels", () => {
  it("places the supplier-outage crisis frame with zero pairwise overlaps", () => {
    const { result, bannerRects } = run(crisisFrame());
    const rects = [...bannerRects.map((b) => b.rect), ...result.placed.map((p) => p.rect)];
    for (let i = 0; i < rects.length; i++) {
      for (let j = i + 1; j < rects.length; j++) {
        expect(intersects(rects[i] as Rect, rects[j] as Rect, 0), `${i} vs ${j}`).toBe(false);
      }
    }
    for (const p of result.placed) {
      expect(p.rect.x).toBeGreaterThanOrEqual(BOUNDS.x);
      expect(p.rect.y).toBeGreaterThanOrEqual(BOUNDS.y);
      expect(p.rect.x + p.rect.w).toBeLessThanOrEqual(BOUNDS.x + BOUNDS.w);
      expect(p.rect.y + p.rect.h).toBeLessThanOrEqual(BOUNDS.y + BOUNDS.h);
    }
  });

  it("stacks every disruption as a banner column and never as a floating label", () => {
    const { bannerRects, labels } = run(crisisFrame());
    expect(bannerRects).toHaveLength(3);
    expect(bannerRects.map((b) => b.rect.y)).toEqual([...bannerRects.map((b) => b.rect.y)].sort((a, b) => a - b));
    expect(new Set(bannerRects.map((b) => b.rect.x)).size).toBe(1);
    expect(labels.some((l) => l.variants.some((v) => v.startsWith("Dock closed")))).toBe(false);
  });

  it("merges per-rack sentences into number badges plus one aggregate tag", () => {
    const { labels } = run(crisisFrame());
    const sentences = labels.flatMap((l) => l.variants).filter((v) => v.startsWith("Waiting for inventory"));
    expect(sentences.every((v) => v.includes("4110") || v.includes("orders"))).toBe(true);
    expect(labels.filter((l) => l.id === "waiting-total")).toHaveLength(1);
    expect(labels.filter((l) => l.id.startsWith("waiting-") && l.id !== "waiting-total" && l.id !== "waiting-capacity").map((l) => l.variants[0])).toEqual(["1064", "1359", "709", "978"]);
  });

  it("represents every hidden label in a tooltip or the accessible summary", () => {
    // A tiny canvas forces hiding.
    const { m, labels } = run(crisisFrame());
    const tiny: Rect = { x: 4, y: 4, w: 420, h: 200 };
    const result = layoutLabels(labels, estimateMeasure, tiny, []);
    expect(result.hidden.length).toBeGreaterThan(0);
    const d = describeScene(m);
    const summary = [d.headline, ...d.lines, ...d.disruptions, ...d.interventions].join(" ");
    for (const h of result.hidden) {
      if (h.fact === "") continue;
      const tip = h.hotspotId === undefined ? "" : (m.hotspots.find((x) => x.id === h.hotspotId)?.tooltip ?? "");
      expect(`${tip} ${summary}`, h.id).toContain(h.fact);
    }
  });

  it("is deterministic", () => {
    expect(run(crisisFrame()).result).toEqual(run(crisisFrame()).result);
  });

  it("falls back to compact variants before hiding", () => {
    const r = layoutLabels(
      [{ id: "a", variants: ["a very long label indeed", "short"], tone: "normal", priority: 1, anchor: { x: 30, y: 30 }, fact: "" }],
      estimateMeasure,
      { x: 0, y: 0, w: 70, h: 40 },
    );
    expect(r.placed[0]?.variant).toBe(1);
  });
});
