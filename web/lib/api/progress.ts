/**
 * Progress_Stream consumer. Opens `GET /branches/{id}/progress` as server-sent
 * events and delivers authoritative Tick_Frames in Tick order, each exactly
 * once. The stream never advances the simulation; it only reports frames the
 * server already computed.
 *
 * Resume: ids are Ticks. On an error the stream is closed and reopened with
 * `?fromTick=lastTick+1` (the browser's own `Last-Event-ID` auto-reconnect is
 * not relied on, so behaviour is the same with every EventSource). After
 * `maxRetries` consecutive failures, or when EventSource is unavailable, the
 * consumer falls back to polling `GET /tick-frames` from lastTick+1.
 */
import { apiBaseUrl, getBranch, listTickFrames } from "./client";
import type { TickFrame } from "./client";

export type ProgressStatus = "connecting" | "live" | "polling" | "closed";

export type WindowReason = "cadence" | "disruption_onset" | "fork";

/** The subset of the EventSource interface the consumer uses. */
export interface EventSourceLike {
  addEventListener(type: string, listener: (event: MessageEvent<string>) => void): void;
  close(): void;
  onerror: ((event: Event) => void) | null;
  onopen: ((event: Event) => void) | null;
}

export type EventSourceFactory = (url: string) => EventSourceLike;

export interface PollOptions {
  intervalMs?: number;
  /** Returns the current Head_Tick of the branch. */
  getHeadTick?: (branchId: string) => Promise<number>;
  listFrames?: (branchId: string, range: { from: number; to: number }) => Promise<TickFrame[]>;
}

export interface ProgressOptions {
  /** First Tick wanted. Ticks below it are never delivered. */
  fromTick: number;
  onFrame: (frame: TickFrame) => void;
  /** Called once per Tick that opens a decision window. */
  onWindow?: (tick: number, reasons: WindowReason[]) => void;
  onCompleted?: (tick: number) => void;
  onStatus?: (status: ProgressStatus) => void;
  eventSourceFactory?: EventSourceFactory;
  poll?: PollOptions;
  /** Consecutive stream failures tolerated before polling. Default 3. */
  maxRetries?: number;
  backoffBaseMs?: number;
  backoffCapMs?: number;
}

export interface ProgressHandle {
  close(): void;
  readonly status: ProgressStatus;
  /** Highest Tick delivered so far (fromTick - 1 before any). */
  readonly lastTick: number;
}

/** Largest range the tick-frames endpoint accepts. */
const MAX_POLL_SPAN = 240;

function defaultFactory(): EventSourceFactory | null {
  const ctor = (globalThis as { EventSource?: new (url: string) => EventSourceLike }).EventSource;
  return ctor ? (url) => new ctor(url) : null;
}

function windowReasons(frame: TickFrame): WindowReason[] | null {
  const reasons: WindowReason[] = [];
  let found = false;
  for (const m of frame.markers) {
    if (m.kind !== "decision_window") continue;
    found = true;
    if (m.windowReason) reasons.push(m.windowReason);
  }
  return found ? reasons : null;
}

export function openProgress(branchId: string, opts: ProgressOptions): ProgressHandle {
  const maxRetries = opts.maxRetries ?? 3;
  const backoffBase = opts.backoffBaseMs ?? 500;
  const backoffCap = opts.backoffCapMs ?? 8000;
  const pollMs = opts.poll?.intervalMs ?? 1500;
  const getHead = opts.poll?.getHeadTick ?? (async (id: string) => (await getBranch(id)).headTick);
  const listFrames =
    opts.poll?.listFrames ?? (async (id: string, r: { from: number; to: number }) => (await listTickFrames(id, r)).frames);
  const factory = opts.eventSourceFactory ?? defaultFactory();

  let status: ProgressStatus = "connecting";
  let closed = false;
  let last = opts.fromTick - 1;
  let windowTick = -1;
  let failures = 0;
  let source: EventSourceLike | null = null;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let polling = false;

  function setStatus(next: ProgressStatus): void {
    if (status === next) return;
    status = next;
    opts.onStatus?.(next);
  }

  /** Delivers a frame once, in order. Returns false when it would skip a Tick. */
  function deliver(frame: TickFrame): boolean {
    if (frame.tick <= last) return true; // duplicate
    if (frame.tick > last + 1) return false; // gap: caller resyncs from last+1
    last = frame.tick;
    opts.onFrame(frame);
    if (frame.tick > windowTick) {
      const reasons = windowReasons(frame);
      if (reasons) {
        windowTick = frame.tick;
        opts.onWindow?.(frame.tick, reasons);
      }
    }
    return true;
  }

  function dropSource(): void {
    if (!source) return;
    const s = source;
    source = null;
    s.onerror = null;
    s.onopen = null;
    s.close();
  }

  function finish(tick: number | null): void {
    if (closed) return;
    closed = true;
    dropSource();
    if (timer !== null) clearTimeout(timer);
    timer = null;
    setStatus("closed");
    if (tick !== null) opts.onCompleted?.(tick);
  }

  function parse<T>(raw: string): T | null {
    try {
      return JSON.parse(raw) as T;
    } catch {
      return null;
    }
  }

  function fail(): void {
    if (closed) return;
    dropSource();
    failures += 1;
    if (failures > maxRetries) {
      startPolling();
      return;
    }
    const delay = Math.min(backoffCap, backoffBase * 2 ** (failures - 1));
    setStatus("connecting");
    timer = setTimeout(() => {
      timer = null;
      connect();
    }, delay);
  }

  function connect(): void {
    if (closed) return;
    if (!factory) {
      startPolling();
      return;
    }
    const url = `${apiBaseUrl()}/branches/${encodeURIComponent(branchId)}/progress?fromTick=${last + 1}`;
    let es: EventSourceLike;
    try {
      es = factory(url);
    } catch {
      fail();
      return;
    }
    source = es;
    es.onopen = () => {
      if (source === es) setStatus("live");
    };
    es.onerror = () => {
      if (source === es) fail();
    };
    es.addEventListener("tick", (ev) => {
      if (source !== es) return;
      const frame = parse<TickFrame>(ev.data);
      if (!frame) return;
      failures = 0;
      setStatus("live");
      if (!deliver(frame)) {
        // A Tick is missing: reopen from last+1 rather than skip it.
        dropSource();
        connect();
      }
    });
    es.addEventListener("decision_window", (ev) => {
      if (source !== es) return;
      const w = parse<{ tick: number; reasons: WindowReason[] }>(ev.data);
      // Markers on the tick frame normally fired this already (same id).
      if (w && w.tick === last && w.tick > windowTick) {
        windowTick = w.tick;
        opts.onWindow?.(w.tick, w.reasons);
      }
    });
    es.addEventListener("completed", (ev) => {
      if (source !== es) return;
      const c = parse<{ tick: number }>(ev.data);
      finish(c ? c.tick : last);
    });
  }

  async function pollOnce(): Promise<void> {
    const head = await getHead(branchId);
    while (!closed && last < head) {
      const from = last + 1;
      const to = Math.min(head, from + MAX_POLL_SPAN - 1);
      const frames = (await listFrames(branchId, { from, to })).slice().sort((a, b) => a.tick - b.tick);
      if (closed) return;
      let progressed = false;
      for (const f of frames) {
        if (!deliver(f)) break;
        progressed = true;
      }
      if (!progressed) return;
    }
  }

  function schedulePoll(): void {
    if (closed) return;
    timer = setTimeout(() => {
      timer = null;
      if (closed || polling) return;
      polling = true;
      pollOnce()
        .catch(() => undefined) // transient errors: try again next interval
        .finally(() => {
          polling = false;
          schedulePoll();
        });
    }, pollMs);
  }

  function startPolling(): void {
    if (closed) return;
    dropSource();
    setStatus("polling");
    schedulePoll();
  }

  connect();

  return {
    close() {
      if (closed) return;
      closed = true;
      dropSource();
      if (timer !== null) clearTimeout(timer);
      timer = null;
      setStatus("closed");
    },
    get status() {
      return status;
    },
    get lastTick() {
      return last;
    },
  };
}
