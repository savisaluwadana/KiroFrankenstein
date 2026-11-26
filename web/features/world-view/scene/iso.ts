/** Pure 2:1 isometric projection helpers (docs/ux-redesign.md section 6.1). */
export const TW = 64;
export const TH = 32;
/** Pixels per unit of height. */
export const ZH = 24;
export const GW = 22;
export const GH = 14;
export const SCENE_W = 1472;
export const SCENE_H = 720;
/** Screen x of cell (0,0). Chosen so the floor diamond is centred in SCENE_W. */
export const ORIGIN_X = (GH * TW) / 2 + (SCENE_W - ((GW + GH) * TW) / 2) / 2;
export const ORIGIN_Y = 120;

export interface Pt {
  x: number;
  y: number;
}

export function project(gx: number, gy: number, gz = 0): Pt {
  return {
    x: ORIGIN_X + ((gx - gy) * TW) / 2,
    y: ORIGIN_Y + ((gx + gy) * TH) / 2 - gz * ZH,
  };
}

/** Inverse of `project` for a given height. */
export function unproject(x: number, y: number, gz = 0): { gx: number; gy: number } {
  const a = ((x - ORIGIN_X) * 2) / TW; // gx - gy
  const b = ((y - ORIGIN_Y + gz * ZH) * 2) / TH; // gx + gy
  return { gx: (a + b) / 2, gy: (b - a) / 2 };
}

/** Painter's order: larger is drawn later (in front). */
export function zIndex(gx: number, gy: number, gz = 0): number {
  return (gx + gy) * 100 + gz;
}

/** Uniform scale that fits the logical scene into a pixel box. */
export function fitScale(width: number, height: number): number {
  return Math.min(width / SCENE_W, height / SCENE_H);
}

export interface PxRect {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** Screen-space bounding box of a cell footprint extruded to `height` units. */
export function footprintBounds(gx: number, gy: number, w: number, d: number, height: number): PxRect {
  const pts = [
    project(gx, gy, height),
    project(gx + w, gy, height),
    project(gx, gy + d, 0),
    project(gx + w, gy + d, 0),
    project(gx + w, gy, 0),
    project(gx, gy + d, height),
  ];
  const xs = pts.map((p) => p.x);
  const ys = pts.map((p) => p.y);
  const x = Math.min(...xs);
  const y = Math.min(...ys);
  return { x, y, w: Math.max(...xs) - x, h: Math.max(...ys) - y };
}
