/** Fixed-size pool: objects are created once, shown and hidden, never created per tick. */
export class Pool<T> {
  private readonly items: T[] = [];
  private readonly inUse = new Set<T>();
  constructor(
    size: number,
    create: (i: number) => T,
  ) {
    for (let i = 0; i < size; i++) this.items.push(create(i));
  }
  /** Next free item, or null when the pool is exhausted (the caller skips decoration). */
  acquire(): T | null {
    for (const it of this.items) {
      if (!this.inUse.has(it)) {
        this.inUse.add(it);
        return it;
      }
    }
    return null;
  }
  release(it: T): void {
    this.inUse.delete(it);
  }
  all(): readonly T[] {
    return this.items;
  }
  releaseAll(): void {
    this.inUse.clear();
  }
}

export interface Tween {
  /** Milliseconds to wait before the tween starts. */
  delay: number;
  dur: number;
  elapsed: number;
  apply: (k: number) => void;
  onStart?: () => void;
  onEnd?: () => void;
  tag?: unknown;
  started: boolean;
}

export const easeOut = (k: number): number => 1 - Math.pow(1 - k, 3);
export const easeInOut = (k: number): number => (k < 0.5 ? 2 * k * k : 1 - Math.pow(-2 * k + 2, 2) / 2);

/** Advances tweens by dt. Returns true while any remains. */
export class TweenList {
  private list: Tween[] = [];
  get active(): boolean {
    return this.list.length > 0;
  }
  add(t: Omit<Tween, "elapsed" | "started">): void {
    this.list.push({ ...t, elapsed: 0, started: false });
  }
  private start(t: Tween): void {
    if (!t.started) {
      t.started = true;
      t.onStart?.();
    }
  }
  step(dt: number): void {
    const done: Tween[] = [];
    for (const t of this.list) {
      t.elapsed += dt;
      if (t.elapsed < t.delay) continue;
      this.start(t);
      const k = Math.min(1, (t.elapsed - t.delay) / Math.max(1, t.dur));
      t.apply(k);
      if (k >= 1) done.push(t);
    }
    if (done.length > 0) {
      this.list = this.list.filter((t) => !done.includes(t));
      for (const t of done) t.onEnd?.();
    }
  }
  /** Jump every tween (or those with `tag`) to its end state. */
  finish(tag?: unknown): void {
    const hit = this.list.filter((t) => tag === undefined || t.tag === tag);
    this.list = this.list.filter((t) => !hit.includes(t));
    for (const t of hit) {
      this.start(t);
      t.apply(1);
      t.onEnd?.();
    }
  }
}
