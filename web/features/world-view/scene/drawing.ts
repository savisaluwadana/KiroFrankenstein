/**
 * Procedural drawing primitives for the isometric warehouse (Pixi Graphics,
 * no external assets). Each function only draws; it never reads application
 * state. Cube helpers take cell dimensions and a pixel height.
 */
import { Container, Graphics, Rectangle, Text, type Renderer, type Texture } from "pixi.js";
import { BOX_COLORS, GLYPH_COUNT, PALETTE, type BoxColors, type Palette } from "./palette";
import { GH, GW, project, TH, TW, ZH } from "./iso";
import { BENCH_D, BENCH_W, BOX_D, BOX_H_PX, BOX_W, CONVEYOR, DOCK_PLATE, RACK_D, RACK_W, ZONES, type CellRect } from "./layout";

type XY = [number, number];

const flat = (pts: XY[]): number[] => pts.flatMap((p) => p);

/** Screen offset (pixels) of a cell-space offset (dx, dy, dz) from an anchor. */
export function off(dx: number, dy: number, dz = 0): XY {
  return [((dx - dy) * TW) / 2, ((dx + dy) * TH) / 2 - dz * ZH];
}

export interface CubeFaces {
  top: number;
  left: number;
  right: number;
  edge?: number;
}

/**
 * Draw an isometric cube whose back (north) corner sits at (ox, oy, oz) cells
 * relative to the Graphics origin. `hPx` is the height in pixels.
 */
export function drawCube(g: Graphics, ox: number, oy: number, oz: number, w: number, d: number, hPx: number, c: CubeFaces): void {
  const [ax, ay] = off(ox, oy, oz);
  const A: XY = [ax, ay];
  const B: XY = [ax + (w * TW) / 2, ay + (w * TH) / 2];
  const C: XY = [ax - (d * TW) / 2, ay + (d * TH) / 2];
  const D: XY = [ax + ((w - d) * TW) / 2, ay + ((w + d) * TH) / 2];
  const up = (p: XY): XY => [p[0], p[1] - hPx];
  const stroke = c.edge === undefined ? undefined : { width: 1, color: c.edge, alpha: 0.9 };
  const face = (pts: XY[], color: number): void => {
    g.poly(flat(pts)).fill(color);
    if (stroke !== undefined) g.poly(flat(pts)).stroke(stroke);
  };
  face([C, D, up(D), up(C)], c.left);
  face([B, D, up(D), up(B)], c.right);
  face([up(A), up(B), up(D), up(C)], c.top);
}

/** A flat diamond on the floor covering a cell rectangle at height gz. */
export function drawDiamond(g: Graphics, gx: number, gy: number, w: number, h: number, gz = 0): void {
  const p = [project(gx, gy, gz), project(gx + w, gy, gz), project(gx + w, gy + h, gz), project(gx, gy + h, gz)];
  g.poly(p.flatMap((q) => [q.x, q.y]));
}

/** Vertical quad on a wall plane. axis "x": plane gy = c from gx a..b; axis "y": plane gx = c from gy a..b. */
export function wallQuad(axis: "x" | "y", c: number, a: number, b: number, z0: number, z1: number): number[] {
  const at = (u: number, z: number) => (axis === "x" ? project(u, c, z) : project(c, u, z));
  const pts = [at(a, z0), at(b, z0), at(b, z1), at(a, z1)];
  return pts.flatMap((q) => [q.x, q.y]);
}

/** Floor, walls, zone markings, conveyor. Drawn once and cached as one texture. */
export function drawBackground(root: Container, p: Palette = PALETTE): void {
  const g = new Graphics();
  // Slab edge (front-left and front-right thickness).
  const L = project(0, GH);
  const Bm = project(GW, GH);
  const R = project(GW, 0);
  const slab = 14;
  g.poly([L.x, L.y, Bm.x, Bm.y, Bm.x, Bm.y + slab, L.x, L.y + slab]).fill(p.slab);
  g.poly([Bm.x, Bm.y, R.x, R.y, R.x, R.y + slab, Bm.x, Bm.y + slab]).fill(0x17161a);
  // Tiles.
  for (let gx = 0; gx < GW; gx++) {
    for (let gy = 0; gy < GH; gy++) {
      drawDiamond(g, gx, gy, 1, 1);
      g.fill((gx + gy) % 2 === 0 ? p.floorA : p.floorB);
    }
  }
  // Zone tints.
  const tint = (z: CellRect, color: number, alpha: number): void => {
    drawDiamond(g, z.gx, z.gy, z.w, z.h);
    g.fill({ color, alpha });
  };
  tint(ZONES.inbound, p.amber, 0.05);
  tint(ZONES.outbound, p.amber, 0.05);
  tint(ZONES.packing, p.ivory, 0.03);
  tint(ZONES.orderLine, p.ivory, 0.04);
  tint(ZONES.breakArea, p.sage, 0.04);
  // Dock plate and outbound apron, slightly raised.
  const plate = DOCK_PLATE;
  drawDiamond(g, plate.gx, plate.gy, plate.w, plate.h);
  g.fill(p.raised);
  drawDiamond(g, plate.gx, plate.gy, plate.w, plate.h);
  g.stroke({ width: 1, color: p.hairline });
  // Hazard stripes along the dock plate edge and the outbound apron edge.
  hazardStripes(g, 3.3, 2, 3.3, 10, "y", p);
  hazardStripes(g, 16.2, 1, 16.2, 9, "y", p);
  // Zone outlines.
  for (const z of Object.values(ZONES)) {
    drawDiamond(g, z.gx, z.gy, z.w, z.h);
    g.stroke({ width: 1, color: p.hairline, alpha: 0.55 });
  }
  // Conveyor belt.
  const cv = CONVEYOR;
  drawCube(g, cv.gx, cv.gy, 0, cv.w, cv.h, 6, { top: 0x1d1c21, left: p.rackDark, right: p.rack, edge: 0x14131a });
  for (let i = 0; i < 20; i++) {
    const a = project(cv.gx + 0.3 + i * 0.5, cv.gy + 0.04, 0.25);
    const b = project(cv.gx + 0.3 + i * 0.5, cv.gy + cv.h - 0.04, 0.25);
    g.moveTo(a.x, a.y).lineTo(b.x, b.y).stroke({ width: 1, color: p.hairline, alpha: 0.6 });
  }
  // Back walls (cutaway: only the two far walls are drawn).
  const H = 3;
  g.poly(wallQuad("x", 0, 0, GW, 0, H)).fill(p.wall);
  g.poly(wallQuad("y", 0, 0, GH, 0, H)).fill(p.wallDark);
  g.poly(wallQuad("x", 0, 0, GW, 0, 0.35)).fill(p.wallDark);
  g.poly(wallQuad("y", 0, 0, GH, 0, 0.35)).fill(p.shadow);
  g.poly(wallQuad("x", 0, 0, GW, H - 0.12, H)).fill(p.rack);
  g.poly(wallQuad("y", 0, 0, GH, H - 0.12, H)).fill(p.rackDark);
  // Windows with a warm dusk glow.
  for (let i = 0; i < 5; i++) {
    const a = 1.8 + i * 4.2;
    g.poly(wallQuad("x", 0, a, a + 2, 1.3, 2.5)).fill(p.glass);
    g.poly(wallQuad("x", 0, a + 0.08, a + 1.92, 1.38, 2.42)).fill({ color: p.amber, alpha: 0.28 });
  }
  for (let i = 0; i < 3; i++) {
    const a = 2.2 + i * 4;
    g.poly(wallQuad("y", 0, a, a + 2, 1.3, 2.5)).fill(p.glass);
    g.poly(wallQuad("y", 0, a + 0.08, a + 1.92, 1.38, 2.42)).fill({ color: p.amber, alpha: 0.2 });
  }
  root.addChild(g);
}

/** Alternating amber/ink stripes along a floor line. */
function hazardStripes(g: Graphics, ax: number, ay: number, bx: number, by: number, axis: "x" | "y", p: Palette): void {
  const len = axis === "y" ? by - ay : bx - ax;
  const n = Math.floor(len / 0.5);
  for (let i = 0; i < n; i++) {
    const u0 = i * 0.5;
    const u1 = u0 + 0.5;
    if (axis === "y") drawDiamond(g, ax, ay + u0, 0.16, u1 - u0);
    else drawDiamond(g, ax + u0, ay, u1 - u0, 0.16);
    g.fill({ color: i % 2 === 0 ? p.amber : p.ink, alpha: 0.75 });
  }
}

/** Barrier of hazard stripes across a lane (dock closed). Origin at the lane's back-left cell. */
export function drawBarrier(g: Graphics, p: Palette = PALETTE): void {
  g.clear();
  const n = 6;
  for (let i = 0; i < n; i++) {
    drawCube(g, i * 0.3, 0, 0.9, 0.3, 0.12, 8, { top: i % 2 === 0 ? p.amber : p.ink, left: i % 2 === 0 ? p.amber : p.ink, right: i % 2 === 0 ? 0xb67b22 : 0x0e0d10 });
  }
  drawCube(g, 0, 0, 0, 0.1, 0.12, 22, { top: p.edge, left: p.edge, right: p.hairline });
  drawCube(g, 1.7, 0, 0, 0.1, 0.12, 22, { top: p.edge, left: p.edge, right: p.hairline });
}

/** Rack frame, split so boxes can sit between the back and front parts. Origin at the rack's back-left corner. */
export function drawRackBack(g: Graphics, p: Palette = PALETTE): void {
  g.clear();
  const levels = 3;
  for (let l = 0; l < levels; l++) {
    // Shelf plank.
    drawCube(g, 0, 0, l * 1.0, RACK_W, RACK_D, 3, { top: p.rack, left: p.rackDark, right: p.rackDark, edge: 0x2c2a33 });
  }
  // Back poles.
  for (let i = 0; i <= 3; i++) {
    drawCube(g, (i * (RACK_W - 0.1)) / 3, 0, 0, 0.1, 0.1, 3.1 * ZH, { top: p.edge, left: p.rack, right: p.rackDark });
  }
}
export function drawRackFront(g: Graphics, p: Palette = PALETTE): void {
  g.clear();
  for (let i = 0; i <= 3; i++) {
    drawCube(g, (i * (RACK_W - 0.1)) / 3, RACK_D - 0.1, 0, 0.1, 0.1, 3.1 * ZH, { top: p.edge, left: p.rack, right: p.rackDark });
  }
  // Top rail.
  drawCube(g, 0, RACK_D - 0.08, 3.1, RACK_W, 0.08, 4, { top: p.edge, left: p.rack, right: p.rackDark });
}

/** Packing bench with a scale and a station light (light drawn separately). Origin at back-left corner. */
export function drawBench(g: Graphics, p: Palette = PALETTE): void {
  g.clear();
  drawCube(g, 0, 0, 0, BENCH_W, BENCH_D, 24, { top: 0x6b6676, left: p.rackDark, right: p.rack, edge: 0x2c2a33 });
  // Scale.
  drawCube(g, 0.4, 0.25, 1, 0.5, 0.4, 5, { top: p.ivoryDim, left: p.edge, right: p.hairline });
  drawCube(g, 1.2, 0.3, 1, 0.35, 0.3, 4, { top: p.ivory, left: p.ivoryDim, right: p.edge });
}

export function drawLight(g: Graphics, color: number): void {
  g.clear();
  g.circle(0, 0, 4).fill(color);
  g.circle(0, 0, 7).stroke({ width: 1, color, alpha: 0.5 });
}

export function drawBoard(g: Graphics, p: Palette = PALETTE): void {
  g.clear();
  g.poly(wallQuad("x", 0, 0, 5.2, 0, 2.9)).fill(p.raised).stroke({ width: 1, color: p.hairline });
  g.poly(wallQuad("x", 0, 0, 5.2, 2.7, 2.9)).fill(p.rack);
}

/** A ticket: skewed to the board plane. Origin at its bottom-left. */
export function drawTicket(g: Graphics, p: Palette = PALETTE): void {
  g.clear();
  const w = 0.38;
  const hz = 0.52;
  const pts = [project(0, 0, 0), project(w, 0, 0), project(w, 0, hz), project(0, 0, hz)];
  const o = project(0, 0, 0);
  g.poly(pts.flatMap((q) => [q.x - o.x, q.y - o.y])).fill(p.ivory).stroke({ width: 1, color: p.ivoryDim });
  const l1 = [project(0.06, 0, hz * 0.7), project(w - 0.06, 0, hz * 0.7)];
  g.moveTo(l1[0]!.x - o.x, l1[0]!.y - o.y).lineTo(l1[1]!.x - o.x, l1[1]!.y - o.y).stroke({ width: 1, color: p.edge });
  const l2 = [project(0.06, 0, hz * 0.45), project(w - 0.14, 0, hz * 0.45)];
  g.moveTo(l2[0]!.x - o.x, l2[0]!.y - o.y).lineTo(l2[1]!.x - o.x, l2[1]!.y - o.y).stroke({ width: 1, color: p.edge });
}

function glyph(g: Graphics, i: number, cx: number, cy: number, color: number): void {
  switch (i % GLYPH_COUNT) {
    case 0:
      g.circle(cx, cy, 2.4).fill(color);
      break;
    case 1:
      g.rect(cx - 2.2, cy - 2.2, 4.4, 4.4).fill(color);
      break;
    case 2:
      g.poly([cx, cy - 2.8, cx + 2.8, cy + 2.2, cx - 2.8, cy + 2.2]).fill(color);
      break;
    case 3:
      g.poly([cx, cy - 3, cx + 3, cy, cx, cy + 3, cx - 3, cy]).fill(color);
      break;
    case 4:
      g.rect(cx - 3, cy - 0.9, 6, 1.8).fill(color);
      g.rect(cx - 0.9, cy - 3, 1.8, 6).fill(color);
      break;
    default:
      g.circle(cx, cy, 2.6).stroke({ width: 1.2, color });
  }
}

export function glyphName(i: number): string {
  return ["circle", "square", "triangle", "diamond", "cross", "ring"][i % GLYPH_COUNT] ?? "circle";
}

export interface SpriteTexture {
  texture: Texture;
  /** Pivot in texture pixels at the anchor point (the ground corner). */
  pivotX: number;
  pivotY: number;
}

/** Product box texture with a distinct colour and glyph. Anchor: the cube's back-bottom corner. */
export function makeBoxTexture(renderer: Renderer, style: number): SpriteTexture {
  const colors: BoxColors = BOX_COLORS[style % BOX_COLORS.length] ?? BOX_COLORS[0]!;
  const g = new Graphics();
  drawCube(g, 0, 0, 0, BOX_W, BOX_D, BOX_H_PX, colors);
  // Glyph on the left face.
  const mid = off(BOX_W / 2 - BOX_D / 2, BOX_W / 2 + BOX_D / 2, 0);
  glyph(g, style, mid[0] - 0.5 + 0, mid[1] - BOX_H_PX / 2, 0xf3ede0);
  const minX = (-BOX_D * TW) / 2 - 1;
  const minY = -BOX_H_PX - 1;
  const frame = new Rectangle(minX, minY, ((BOX_W + BOX_D) * TW) / 2 + 2, ((BOX_W + BOX_D) * TH) / 2 + BOX_H_PX + 2);
  const texture = renderer.generateTexture({ target: g, frame, resolution: 2 });
  g.destroy();
  return { texture, pivotX: -minX, pivotY: -minY };
}

export type FigureVariant = "regular" | "temp" | "ghost";

/** 18 x 36 figure: shadow, body capsule, head. Anchor at the feet (9, 33). */
export function makeFigureTexture(renderer: Renderer, variant: FigureVariant, p: Palette = PALETTE): SpriteTexture {
  const g = new Graphics();
  if (variant === "ghost") {
    g.ellipse(9, 33, 7, 2.5).stroke({ width: 1, color: p.edge, alpha: 0.7 });
    g.roundRect(4, 12, 10, 18, 5).stroke({ width: 1.2, color: p.ivoryDim, alpha: 0.8 });
    g.circle(9, 7, 5).stroke({ width: 1.2, color: p.ivoryDim, alpha: 0.8 });
  } else {
    g.ellipse(9, 33, 7, 2.5).fill({ color: p.shadow, alpha: 0.55 });
    g.roundRect(4, 12, 10, 19, 5).fill(variant === "temp" ? p.amber : p.ivory);
    if (variant === "temp") g.rect(8, 12, 2, 19).fill(p.ivoryDim);
    g.circle(9, 7, 5).fill(0xe3c9a8);
    g.arc(9, 7, 5, Math.PI, 0).fill(p.ink);
  }
  const texture = renderer.generateTexture({ target: g, frame: new Rectangle(0, 0, 18, 36), resolution: 2 });
  g.destroy();
  return { texture, pivotX: 9, pivotY: 33 };
}

/** Delivery truck driving along +/- gy. Origin: back-left corner of the footprint. Cab at low gy. */
export function drawTruck(g: Graphics, len: number, w: number, p: Palette = PALETTE): void {
  g.clear();
  const cabD = 0.85;
  const cargo = { top: p.ivory, left: 0xd9d2c3, right: p.ivoryDim, edge: 0x6a655c };
  const cab = { top: p.amberHot, left: p.amber, right: 0xb67b22, edge: 0x7b5314 };
  drawCube(g, 0.05, 0, 0, w - 0.1, cabD, 30, cab);
  drawCube(g, 0, cabD, 0, w, len - cabD, 44, cargo);
  for (const [dx, dy] of [
    [0.1, 0.2],
    [w - 0.1, 0.2],
    [0.1, len - 0.5],
    [w - 0.1, len - 0.5],
  ] as XY[]) {
    const [x, y] = off(dx, dy, 0);
    g.ellipse(x, y, 5, 3).fill(0x0e0d10);
  }
}

/** Outbound van driving along +gx. Origin: back-left corner of the footprint. Nose at high gx. */
export function drawVan(g: Graphics, len: number, d: number, p: Palette = PALETTE): void {
  g.clear();
  const body = { top: p.ivory, left: 0xe6e0d2, right: p.ivoryDim, edge: 0x6a655c };
  drawCube(g, 0, 0, 0, len - 0.5, d, 30, body);
  drawCube(g, len - 0.5, 0.05, 0, 0.5, d - 0.1, 20, { top: p.amberHot, left: p.amber, right: 0xb67b22, edge: 0x7b5314 });
  // Amber stripe on the long (left-facing) side.
  const a = off(0.05, d, 0.35);
  const b = off(len - 0.55, d, 0.35);
  const h = 6;
  g.poly([a[0], a[1], b[0], b[1], b[0], b[1] - h, a[0], a[1] - h]).fill(p.amber);
  for (const dx of [0.35, len - 0.4]) {
    const [x, y] = off(dx, d - 0.05, 0);
    g.ellipse(x, y, 4.5, 2.6).fill(0x0e0d10);
  }
}

/** Little warning triangle used by disruption tags. */
export function drawWarning(g: Graphics, color: number): void {
  g.clear();
  g.poly([0, -7, 7, 6, -7, 6]).fill(color);
  g.rect(-0.9, -2.5, 1.8, 5).fill(0x17161a);
  g.circle(0, 4, 0.9).fill(0x17161a);
}

/** Pill tag with text. Redraws only when the text changes. */
export class Tag extends Container {
  private readonly bg = new Graphics();
  private readonly text: Text;
  private current = "";
  private tone = "";
  private fontSize = 0;
  /** Size of the pill in scene pixels after the last set(). */
  boxW = 0;
  boxH = 0;
  constructor(
    private readonly fontFamily: string,
    private readonly size = 12,
  ) {
    super();
    this.eventMode = "none";
    this.text = new Text({ text: "", style: { fontFamily, fontSize: size, fill: PALETTE.ivory, fontWeight: "500" } });
    this.text.position.set(8, 4);
    this.addChild(this.bg, this.text);
  }
  set(text: string, tone: "normal" | "crisis" | "good" | "muted" = "normal", size = this.size): void {
    if (text === this.current && tone === this.tone && size === this.fontSize) return;
    this.current = text;
    this.tone = tone;
    this.fontSize = size;
    const fill = tone === "crisis" ? PALETTE.ink : tone === "muted" ? PALETTE.ivoryDim : tone === "good" ? PALETTE.ink : PALETTE.ivory;
    const back = tone === "crisis" ? PALETTE.clay : tone === "good" ? PALETTE.sage : PALETTE.surface;
    this.text.text = text;
    this.text.style.fontSize = size;
    this.text.style.fill = fill;
    const w = Math.ceil(this.text.width) + 16;
    const h = Math.ceil(this.text.height) + 8;
    this.boxW = w;
    this.boxH = h;
    this.bg.clear();
    this.bg.roundRect(0, 0, w, h, 8).fill({ color: back, alpha: 0.94 }).stroke({ width: 1, color: tone === "crisis" ? PALETTE.clay : PALETTE.hairline });
    this.pivot.set(w / 2, h);
  }
}
