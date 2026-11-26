import { describe, expect, it } from "vitest";
import { fitScale, footprintBounds, project, SCENE_H, SCENE_W, TH, TW, unproject, zIndex, ZH } from "./iso";
import { benchOrigin, boxesFor, figureSpot, rackOrigin, slotOffset, stationFill, ZONES } from "./layout";

describe("iso", () => {
  it("projects 2:1 and inverts", () => {
    const a = project(3, 2);
    const b = project(4, 2);
    expect(b.x - a.x).toBe(TW / 2);
    expect(b.y - a.y).toBe(TH / 2);
    expect(project(3, 2, 1).y).toBe(a.y - ZH);
    const u = unproject(project(5.5, 7.25, 1).x, project(5.5, 7.25, 1).y, 1);
    expect(u.gx).toBeCloseTo(5.5);
    expect(u.gy).toBeCloseTo(7.25);
  });

  it("orders nearer objects later", () => {
    expect(zIndex(5, 5)).toBeGreaterThan(zIndex(4, 5));
    expect(zIndex(5, 5, 1)).toBeGreaterThan(zIndex(5, 5));
  });

  it("keeps the floor inside the scene", () => {
    for (const [gx, gy] of [[0, 0], [22, 0], [0, 14], [22, 14]] as const) {
      const p = project(gx, gy);
      expect(p.x).toBeGreaterThanOrEqual(0);
      expect(p.x).toBeLessThanOrEqual(SCENE_W);
      expect(p.y).toBeLessThanOrEqual(SCENE_H);
    }
    expect(project(0, 0, 3).y).toBeGreaterThan(0);
    expect(fitScale(SCENE_W * 2, SCENE_H * 3)).toBe(2);
  });

  it("bounds a footprint", () => {
    const r = footprintBounds(1, 1, 2, 2, 1);
    expect(r.w).toBe(4 * (TW / 2));
    expect(r.h).toBe(4 * (TH / 2) + ZH);
  });
});

describe("layout", () => {
  it("fills slots bottom-up, back-to-front, and never exceeds 54", () => {
    expect(slotOffset(0)).toMatchObject({ level: 0, dy: 0.1 });
    expect(slotOffset(9).dy).toBeGreaterThan(slotOffset(0).dy);
    expect(slotOffset(18).level).toBe(1);
    expect(slotOffset(53).level).toBe(2);
    expect(boxesFor(1081)).toEqual({ boxes: 54, moreBoxes: 1 });
    expect(boxesFor(1080)).toEqual({ boxes: 54, moreBoxes: 0 });
    expect(boxesFor(-4)).toEqual({ boxes: 0, moreBoxes: 0 });
  });

  it("places racks and stations inside their zones", () => {
    for (let n = 1; n <= 6; n++) {
      for (let k = 0; k < n; k++) {
        const o = rackOrigin(k, n);
        expect(o.gx + 3.8).toBeLessThanOrEqual(ZONES.storage.gx + ZONES.storage.w);
        expect(o.gy + 1.2).toBeLessThanOrEqual(ZONES.storage.gy + ZONES.storage.h);
      }
    }
    for (let k = 0; k < 4; k++) {
      expect(benchOrigin(k).gx + 2).toBeLessThanOrEqual(ZONES.packing.gx + ZONES.packing.w);
      for (let j = 0; j < 10; j++) expect(figureSpot(k, j).gy).toBeLessThan(ZONES.packing.gy + ZONES.packing.h);
    }
    expect(stationFill(40)).toEqual([10, 10, 10, 10]);
    expect(stationFill(0)).toEqual([0, 0, 0, 0]);
  });
});
