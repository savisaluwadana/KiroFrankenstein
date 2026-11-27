import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { TickFrame } from "./client";
import { openProgress, type EventSourceLike, type ProgressStatus, type WindowReason } from "./progress";

class FakeES implements EventSourceLike {
  onerror: ((e: Event) => void) | null = null;
  onopen: ((e: Event) => void) | null = null;
  closed = false;
  private listeners = new Map<string, ((e: MessageEvent<string>) => void)[]>();
  constructor(readonly url: string) {}
  addEventListener(type: string, l: (e: MessageEvent<string>) => void): void {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), l]);
  }
  close(): void {
    this.closed = true;
  }
  emit(type: string, data: unknown, id?: number): void {
    const ev = { data: JSON.stringify(data), lastEventId: id === undefined ? "" : String(id) } as MessageEvent<string>;
    for (const l of this.listeners.get(type) ?? []) l(ev);
  }
  fail(): void {
    this.onerror?.(new Event("error"));
  }
}

function frame(tick: number, windowReason?: WindowReason): TickFrame {
  return {
    tick,
    markers: windowReason ? [{ kind: "decision_window", windowReason }] : [],
  } as unknown as TickFrame;
}

let sources: FakeES[];
const factory = (url: string): EventSourceLike => {
  const es = new FakeES(url);
  sources.push(es);
  return es;
};
const last = (): FakeES => sources[sources.length - 1]!;

beforeEach(() => {
  sources = [];
  vi.useFakeTimers();
});
afterEach(() => {
  vi.useRealTimers();
});

describe("openProgress", () => {
  it("opens from fromTick and delivers frames in order, once", () => {
    const got: number[] = [];
    openProgress("b1", { fromTick: 0, onFrame: (f) => got.push(f.tick), eventSourceFactory: factory });
    expect(last().url).toContain("/branches/b1/progress?fromTick=0");
    last().emit("tick", frame(0), 0);
    last().emit("tick", frame(1), 1);
    last().emit("tick", frame(1), 1); // duplicate
    last().emit("tick", frame(2), 2);
    expect(got).toEqual([0, 1, 2]);
  });

  it("reads window info from frame markers once, even when decision_window shares the id", () => {
    const wins: [number, WindowReason[]][] = [];
    openProgress("b1", {
      fromTick: 0,
      onFrame: () => undefined,
      onWindow: (t, r) => wins.push([t, r]),
      eventSourceFactory: factory,
    });
    last().emit("tick", frame(0, "fork"), 0);
    last().emit("decision_window", { tick: 0, reasons: ["fork"] }, 0);
    expect(wins).toEqual([[0, ["fork"]]]);
  });

  it("resumes after an error from lastTick+1 with backoff", () => {
    const got: number[] = [];
    const statuses: ProgressStatus[] = [];
    openProgress("b1", {
      fromTick: 0,
      onFrame: (f) => got.push(f.tick),
      onStatus: (s) => statuses.push(s),
      eventSourceFactory: factory,
      backoffBaseMs: 100,
    });
    last().emit("tick", frame(0), 0);
    last().emit("tick", frame(1), 1);
    const first = last();
    first.fail();
    expect(first.closed).toBe(true);
    expect(sources).toHaveLength(1);
    vi.advanceTimersByTime(100);
    expect(sources).toHaveLength(2);
    expect(last().url).toContain("fromTick=2");
    last().emit("tick", frame(2), 2);
    expect(got).toEqual([0, 1, 2]);
    expect(statuses).toContain("live");
  });

  it("reconnects from last+1 instead of skipping when a tick is missing", () => {
    const got: number[] = [];
    openProgress("b1", { fromTick: 0, onFrame: (f) => got.push(f.tick), eventSourceFactory: factory });
    last().emit("tick", frame(0), 0);
    last().emit("tick", frame(2), 2);
    expect(got).toEqual([0]);
    expect(last().url).toContain("fromTick=1");
  });

  it("falls back to polling after repeated errors without skipping or duplicating a tick", async () => {
    const got: number[] = [];
    const ranges: [number, number][] = [];
    const statuses: ProgressStatus[] = [];
    let head = 3;
    openProgress("b1", {
      fromTick: 0,
      onFrame: (f) => got.push(f.tick),
      onStatus: (s) => statuses.push(s),
      eventSourceFactory: factory,
      maxRetries: 2,
      backoffBaseMs: 10,
      backoffCapMs: 20,
      poll: {
        intervalMs: 100,
        getHeadTick: () => Promise.resolve(head),
        listFrames: (_id, r) => {
          ranges.push([r.from, r.to]);
          const out: TickFrame[] = [];
          for (let t = r.from; t <= r.to; t++) out.push(frame(t));
          return Promise.resolve(out);
        },
      },
    });
    last().emit("tick", frame(0), 0);
    last().emit("tick", frame(1), 1);
    for (let i = 0; i < 3; i++) {
      last().fail();
      await vi.advanceTimersByTimeAsync(20);
    }
    expect(statuses).toContain("polling");
    await vi.advanceTimersByTimeAsync(100);
    expect(ranges).toEqual([[2, 3]]);
    head = 5;
    await vi.advanceTimersByTimeAsync(100);
    expect(ranges).toEqual([
      [2, 3],
      [4, 5],
    ]);
    expect(got).toEqual([0, 1, 2, 3, 4, 5]);
  });

  it("polls when EventSource is unsupported", async () => {
    vi.stubGlobal("EventSource", undefined);
    const got: number[] = [];
    const h = openProgress("b1", {
      fromTick: 4,
      onFrame: (f) => got.push(f.tick),
      poll: {
        intervalMs: 50,
        getHeadTick: () => Promise.resolve(5),
        listFrames: (_id, r) => Promise.resolve([frame(r.from), frame(r.to)]),
      },
    });
    expect(h.status).toBe("polling");
    await vi.advanceTimersByTimeAsync(50);
    expect(got).toEqual([4, 5]);
    h.close();
    vi.unstubAllGlobals();
  });

  it("completed closes the stream and reports the horizon tick", () => {
    const done: number[] = [];
    const h = openProgress("b1", {
      fromTick: 0,
      onFrame: () => undefined,
      onCompleted: (t) => done.push(t),
      eventSourceFactory: factory,
    });
    last().emit("tick", frame(0), 0);
    last().emit("completed", { tick: 0 }, 0);
    expect(done).toEqual([0]);
    expect(last().closed).toBe(true);
    expect(h.status).toBe("closed");
  });

  it("close cleans up source and pending timers", () => {
    const got: number[] = [];
    const h = openProgress("b1", { fromTick: 0, onFrame: (f) => got.push(f.tick), eventSourceFactory: factory });
    last().fail();
    h.close();
    vi.advanceTimersByTime(60_000);
    expect(sources).toHaveLength(1);
    expect(sources[0]!.closed).toBe(true);
    expect(vi.getTimerCount()).toBe(0);
    expect(h.status).toBe("closed");
  });

  it("tracks Last-Event-ID semantics: ids are ticks and the resume asks for the next one", () => {
    const h = openProgress("b1", { fromTick: 11, onFrame: () => undefined, eventSourceFactory: factory, backoffBaseMs: 1 });
    expect(last().url).toContain("fromTick=11");
    last().emit("tick", frame(11), 11);
    expect(h.lastTick).toBe(11);
    last().fail();
    vi.advanceTimersByTime(1);
    expect(last().url).toContain("fromTick=12");
  });
});
