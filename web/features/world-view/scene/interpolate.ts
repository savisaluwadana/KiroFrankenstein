/**
 * Decorative interpolation between two authoritative Tick_Frames (Requirement
 * 32.15, 35.x; design rule "positions only, never numbers").
 *
 * Everything here maps a progress value in [0, 1] to POSITIONS or to another
 * progress value. There is no function in this module that returns, formats or
 * accepts a quantity from a frame, so a number on screen can never be an
 * intermediate value. The scene draws counts, labels and tooltips straight from
 * the SceneModel of the displayed frame at every step; this module only moves
 * the transient actors (trucks, vans, boxes, tickets) while they travel.
 * Pure: no clock, no random source, no DOM.
 */

export interface Point {
  x: number;
  y: number;
}

/** Below this display interval (16x playback and faster) nothing travels; only counts change. */
export const MIN_TRAVEL_INTERVAL_MS = 120;

export const easeOut = (k: number): number => 1 - Math.pow(1 - k, 3);
export const easeInOut = (k: number): number => (k < 0.5 ? 2 * k * k : 1 - Math.pow(-2 * k + 2, 2) / 2);

export const clamp01 = (k: number): number => (k <= 0 ? 0 : k >= 1 ? 1 : k);

export const lerp = (a: number, b: number, k: number): number => a + (b - a) * k;

export function lerpPoint(a: Point, b: Point, k: number): Point {
  return { x: lerp(a.x, b.x, k), y: lerp(a.y, b.y, k) };
}

export interface TravelGate {
  /** The user's `prefers-reduced-motion: reduce` setting. */
  reducedMotion: boolean;
  /** False for scrubbing and jumping. */
  animate: boolean;
  /** Time between displayed frames in ms. */
  intervalMs: number;
}

/** Whether actors may travel between two frames. Reduced motion, snapping and fast playback all disable it. */
export function travelEnabled(g: TravelGate): boolean {
  return !g.reducedMotion && g.animate && g.intervalMs >= MIN_TRAVEL_INTERVAL_MS;
}

/** Progress of a delayed tween at `elapsedMs`, 0 before the delay and 1 after the duration. */
export function tweenProgress(elapsedMs: number, delayMs: number, durMs: number): number {
  if (elapsedMs < delayMs) return 0;
  return clamp01((elapsedMs - delayMs) / Math.max(1, durMs));
}

/**
 * Position along a polyline at progress `k` (eased), with an optional upward
 * arc in pixels that is zero at every vertex. `k = 0` is the first point and
 * `k = 1` is the last point, so the end of the travel is the resting place the
 * authoritative frame already dictates.
 */
export function pointOnPath(path: readonly Point[], k: number, ease: (k: number) => number = easeInOut, arcPx = 0): Point {
  const first = path[0];
  if (first === undefined) return { x: 0, y: 0 };
  const segs = path.length - 1;
  if (segs < 1) return { ...first };
  const u = ease(clamp01(k)) * segs;
  const i = Math.min(segs - 1, Math.floor(u));
  const a = path[i];
  const b = path[i + 1];
  if (a === undefined || b === undefined) return { ...first };
  const f = u - i;
  const p = lerpPoint(a, b, f);
  return { x: p.x, y: p.y - Math.sin(Math.PI * f) * arcPx };
}
