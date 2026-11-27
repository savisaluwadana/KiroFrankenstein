/**
 * Deterministic label layout. Pure: no Pixi, no DOM, no randomness.
 *
 * Labels are placed greedily in priority order. Each label tries its text
 * variants (long to short) at a fixed list of offsets around its anchor and
 * takes the first position that stays inside the bounds and overlaps nothing
 * already placed. A label that fits nowhere is hidden; the caller keeps its
 * facts reachable through hotspot tooltips and the accessible summary.
 */
import type { Pt } from "./iso";

export type LabelTone = "normal" | "crisis" | "good" | "muted";

export interface Size {
  w: number;
  h: number;
}
export interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface LabelSpec {
  id: string;
  /** Text variants, longest first. A shorter variant is the compact chip form. */
  variants: string[];
  tone: LabelTone;
  /** Higher is placed first: crisis 100, bottleneck 80, badges 70, stock 50, ambient 10-30. */
  priority: number;
  /** Where the label's bottom centre sits when placed without an offset. */
  anchor: Pt;
  fontSize?: number;
  /** Hotspot whose tooltip carries the facts of this label, when it is hidden. */
  hotspotId?: string;
  /** Text that must appear in the hotspot tooltip or accessible summary if the label is hidden. */
  fact: string;
}

export interface PlacedLabel {
  id: string;
  text: string;
  variant: number;
  tone: LabelTone;
  fontSize: number;
  rect: Rect;
  /** Anchor to connect with a leader line, or null when the label sits on its anchor. */
  leaderTo: Pt | null;
}

export interface LayoutResult {
  placed: PlacedLabel[];
  hidden: LabelSpec[];
}

export type Measure = (text: string, fontSize: number) => Size;

export const DEFAULT_FONT = 12;
export const LABEL_GAP = 3;
/** Smallest effective label text size on screen, in CSS pixels. */
export const MIN_LABEL_PX = 11;
/** Largest enlargement of scene labels; beyond it the layout would hide most of them. */
export const MAX_LABEL_SCALE = 2.4;

/**
 * Factor applied to label font sizes (and banner chrome) so text stays at least
 * MIN_LABEL_PX on screen when the scene is scaled down to fit a small canvas.
 * `sceneScale` is the scene-to-screen scale. 1 when the scene is large enough.
 */
export function labelScaleFor(sceneScale: number): number {
  if (!(sceneScale > 0)) return 1;
  const k = MIN_LABEL_PX / (DEFAULT_FONT * sceneScale);
  return Math.min(MAX_LABEL_SCALE, Math.max(1, Math.ceil(k * 20) / 20));
}

/** Width and height estimate used when no real text measurement is available (tests). */
export const estimateMeasure: Measure = (text, fontSize) => ({
  w: Math.ceil(text.length * fontSize * 0.58) + 16,
  h: Math.ceil(fontSize * 1.2) + 8,
});

/** Fixed offset order in units of (0.75 w, h + gap). Same input, same output. */
const OFFSETS: readonly (readonly [number, number])[] = [
  [0, 0], [0, -1], [0, 1], [1, 0], [-1, 0], [1, -1], [-1, -1], [1, 1], [-1, 1],
  [0, -2], [0, 2], [2, 0], [-2, 0], [1, -2], [-1, -2], [1, 2], [-1, 2],
  [0, -3], [0, 3], [2, -1], [-2, -1], [2, 1], [-2, 1], [0, -4], [0, 4],
];

export function intersects(a: Rect, b: Rect, gap = LABEL_GAP): boolean {
  return a.x < b.x + b.w + gap && b.x < a.x + a.w + gap && a.y < b.y + b.h + gap && b.y < a.y + a.h + gap;
}

function inside(r: Rect, bounds: Rect): boolean {
  return r.x >= bounds.x && r.y >= bounds.y && r.x + r.w <= bounds.x + bounds.w && r.y + r.h <= bounds.y + bounds.h;
}

export function layoutLabels(specs: readonly LabelSpec[], measure: Measure, bounds: Rect, obstacles: readonly Rect[] = [], labelScale = 1): LayoutResult {
  const order = specs.map((s, i) => ({ s, i })).sort((a, b) => b.s.priority - a.s.priority || a.i - b.i);
  const taken: Rect[] = [...obstacles];
  const placedById = new Map<string, PlacedLabel>();
  const hiddenIds = new Set<string>();

  for (const { s } of order) {
    const size = Math.round((s.fontSize ?? DEFAULT_FONT) * labelScale);
    let done = false;
    for (let v = 0; v < s.variants.length && !done; v++) {
      const text = s.variants[v] ?? "";
      const { w, h } = measure(text, size);
      for (const [kx, ky] of OFFSETS) {
        const rect: Rect = {
          x: Math.round(s.anchor.x - w / 2 + kx * w * 0.75),
          y: Math.round(s.anchor.y - h + ky * (h + LABEL_GAP)),
          w,
          h,
        };
        if (!inside(rect, bounds) || taken.some((t) => intersects(rect, t))) continue;
        taken.push(rect);
        placedById.set(s.id, {
          id: s.id,
          text,
          variant: v,
          tone: s.tone,
          fontSize: size,
          rect,
          leaderTo: kx === 0 && ky === 0 ? null : s.anchor,
        });
        done = true;
        break;
      }
    }
    if (!done) hiddenIds.add(s.id);
  }
  return {
    placed: specs.flatMap((s) => {
      const p = placedById.get(s.id);
      return p === undefined ? [] : [p];
    }),
    hidden: specs.filter((s) => hiddenIds.has(s.id)),
  };
}

export interface BannerSpec {
  id: string;
  text: string;
}
export interface PlacedBanner {
  id: string;
  text: string;
  /** Whole banner row including the icon column. */
  rect: Rect;
  /** Left of the text pill. */
  textX: number;
  iconCenter: Pt;
}

export const BANNER_ICON_W = 22;

/** Stack banners as a column along the top-left edge, top first. */
export function layoutBanners(banners: readonly BannerSpec[], measure: Measure, fontSize = DEFAULT_FONT, margin = 8, iconW = BANNER_ICON_W): PlacedBanner[] {
  let y = margin;
  return banners.map((b) => {
    const { w, h } = measure(b.text, fontSize);
    const placed: PlacedBanner = {
      id: b.id,
      text: b.text,
      rect: { x: margin, y, w: iconW + w, h },
      textX: margin + iconW,
      iconCenter: { x: margin + iconW / 2, y: y + h / 2 },
    };
    y += h + 4;
    return placed;
  });
}
