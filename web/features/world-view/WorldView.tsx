"use client";

/**
 * Isometric warehouse view. It draws the authoritative TickFrame it is given.
 *
 * Previous frame: derived internally. WorldView remembers the frame it showed
 * before the current one (reset when `branchKey` changes). Travel animations
 * play only when the new frame is the tick directly after the remembered one
 * (see deriveSceneEvents); anything else snaps.
 */
import { useEffect, useId, useMemo, useRef, useState } from "react";
// CSP forbids unsafe-eval in production: this replaces Pixi's runtime shader code generation.
import "pixi.js/unsafe-eval";
import { Application } from "pixi.js";
import type { TickFrame } from "@/lib/api/client";
import { SCENE_H, SCENE_W } from "./scene/iso";
import { buildSceneModel, type SceneModel } from "./scene/sceneModel";
import type { Intervention } from "./scene/worldViewModel";
import { PALETTE } from "./scene/palette";
import { WorldScene } from "./scene/WorldScene";
import { WorldViewSummary } from "./WorldViewSummary";

export interface WorldViewProps {
  /** Authoritative frame of the displayed Tick, or null before any frame loads. */
  frame: TickFrame | null;
  /** True while data for a requested Tick is loading; the previous frame is shown dimmed. */
  syncing?: boolean;
  /** Commands that take effect at a Tick, with provenance. */
  interventions?: readonly Intervention[];
  /** @deprecated Ignored. Stacks use the documented scale of one box per 20 units. */
  stockScaleMax?: number;
  /** Product display names indexed by SKU index (scenario.skus[i].id or a nicer name). Missing names read "Product n". */
  productNames?: readonly string[];
  /** Regular workers the business employs (assumption `workforce.regularWorkers`); enables the "absent" figures. */
  regularWorkers?: number;
  /** Identifies the branch; changing it forgets the previous frame so nothing animates across branches. */
  branchKey?: string;
  /** Time between displayed frames in ms. Measured from frame arrivals when omitted. */
  tickIntervalMs?: number;
  /** False snaps with no travel animation (for example while scrubbing). Default true. */
  animate?: boolean;
  /** Announce the summary to assistive technology (pauses and decision windows only). Default false. */
  announce?: boolean;
  className?: string;
}

interface FramePair {
  key: string | undefined;
  cur: TickFrame | null;
  prev: TickFrame | null;
}

export function WorldView({
  frame,
  syncing = false,
  interventions,
  productNames,
  regularWorkers,
  branchKey,
  tickIntervalMs,
  animate = true,
  announce = false,
  className,
}: WorldViewProps) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const sceneRef = useRef<WorldScene | null>(null);
  const summaryId = useId();
  const panRef = useRef<HTMLDivElement | null>(null);
  // On a phone the scene is wider than the screen; start centred on the racks.
  useEffect(() => {
    const el = panRef.current;
    if (el !== null) el.scrollLeft = (el.scrollWidth - el.clientWidth) / 2;
  }, []);
  const [hover, setHover] = useState<string | null>(null);

  const [pair, setPair] = useState<FramePair>({ key: branchKey, cur: frame, prev: null });
  if (pair.cur !== frame || pair.key !== branchKey) {
    const sameBranch = pair.key === branchKey;
    const advanced = pair.cur !== null && frame !== null && pair.cur.tick !== frame.tick;
    setPair({ key: branchKey, cur: frame, prev: !sameBranch || frame === null ? null : advanced ? pair.cur : pair.prev });
  }

  const model: SceneModel | null = useMemo(() => {
    if (frame === null) return null;
    const opts: Parameters<typeof buildSceneModel>[3] = { syncing };
    if (interventions !== undefined) opts.interventions = interventions;
    if (regularWorkers !== undefined) opts.regularWorkers = regularWorkers;
    return buildSceneModel(frame, pair.prev, productNames, opts);
  }, [frame, pair.prev, productNames, regularWorkers, interventions, syncing]);

  const latest = useRef({ model, tickIntervalMs, animate });
  const lastArrival = useRef<number | null>(null);
  const lastTick = useRef<number | null>(null);

  useEffect(() => {
    latest.current = { model, tickIntervalMs, animate };
  }, [model, tickIntervalMs, animate]);

  useEffect(() => {
    const host = hostRef.current;
    if (host === null) return;
    let cancelled = false;
    let ready = false;
    const app = new Application();
    let resizeObs: ResizeObserver | null = null;
    let visObs: IntersectionObserver | null = null;
    let media: MediaQueryList | null = null;
    let onMedia: (() => void) | null = null;
    let onVisibility: (() => void) | null = null;
    let onScreen = true;

    void app
      .init({
        width: Math.max(1, host.clientWidth),
        height: Math.max(1, host.clientHeight),
        background: PALETTE.ink,
        antialias: true,
        resolution: Math.min(window.devicePixelRatio || 1, 1.5),
        autoDensity: true,
        autoStart: false,
        powerPreference: "low-power",
      })
      .then(() => {
        ready = true;
        if (cancelled) {
          app.destroy(true, { children: true, texture: true });
          return;
        }
        host.appendChild(app.canvas);
        const family = getComputedStyle(host).fontFamily || "Inter, ui-sans-serif, system-ui, sans-serif";
        const scene = new WorldScene(app, family);
        sceneRef.current = scene;
        const fit = (): void => {
          const w = Math.max(1, host.clientWidth);
          const h = Math.max(1, host.clientHeight);
          app.renderer.resize(w, h);
          scene.resize(w, h);
        };
        fit();
        resizeObs = new ResizeObserver(fit);
        resizeObs.observe(host);
        media = window.matchMedia("(prefers-reduced-motion: reduce)");
        onMedia = (): void => scene.setReducedMotion(media?.matches ?? false);
        onMedia();
        media.addEventListener("change", onMedia);
        const refreshActive = (): void => scene.setActive(onScreen && !document.hidden);
        onVisibility = refreshActive;
        document.addEventListener("visibilitychange", refreshActive);
        visObs = new IntersectionObserver((entries) => {
          onScreen = entries.some((e) => e.isIntersecting);
          refreshActive();
        });
        visObs.observe(host);
        const cur = latest.current;
        if (cur.model !== null) scene.update(cur.model, { animate: false });
      });

    return () => {
      cancelled = true;
      resizeObs?.disconnect();
      visObs?.disconnect();
      if (media !== null && onMedia !== null) media.removeEventListener("change", onMedia);
      if (onVisibility !== null) document.removeEventListener("visibilitychange", onVisibility);
      if (ready) {
        sceneRef.current?.destroy();
        sceneRef.current = null;
        app.destroy(true, { children: true, texture: true });
      }
    };
  }, []);

  useEffect(() => {
    const scene = sceneRef.current;
    if (model === null || scene === null) return;
    const now = performance.now();
    const measured = lastArrival.current === null || lastTick.current === model.tick ? undefined : now - lastArrival.current;
    if (lastTick.current !== model.tick) lastArrival.current = now;
    lastTick.current = model.tick;
    const interval = latest.current.tickIntervalMs ?? measured;
    scene.update(model, {
      animate: latest.current.animate,
      ...(interval !== undefined ? { tickIntervalMs: interval } : {}),
    });
  }, [model]);

  const active = model?.hotspots.find((h) => h.id === hover) ?? null;

  return (
    <div className={className}>
      <div ref={panRef} className="max-[768px]:overflow-x-auto" data-testid="scene-pan">
      <div className="relative w-full max-[768px]:min-w-[780px]" style={{ aspectRatio: `${SCENE_W} / ${SCENE_H}` }}>
        <div
          ref={hostRef}
          role="img"
          aria-labelledby={summaryId}
          className="absolute inset-0 overflow-hidden rounded-[8px] border border-[#4A4750] bg-[#17161A]"
          data-syncing={syncing ? "true" : "false"}
        />
        {model?.hotspots.map((h) => (
          <button
            key={h.id}
            type="button"
            aria-label={`${h.label}: ${h.tooltip}`}
            aria-describedby={hover === h.id ? `${summaryId}-tip` : undefined}
            data-testid={`hotspot-${h.id}`}
            className="absolute cursor-help rounded-[6px] bg-transparent outline-offset-2 focus-visible:outline-2 focus-visible:outline-[#F3EDE0]"
            style={{
              left: `${(h.rect.x / SCENE_W) * 100}%`,
              top: `${(h.rect.y / SCENE_H) * 100}%`,
              width: `${(h.rect.w / SCENE_W) * 100}%`,
              height: `${(h.rect.h / SCENE_H) * 100}%`,
            }}
            onPointerEnter={() => setHover(h.id)}
            onPointerLeave={() => setHover((c) => (c === h.id ? null : c))}
            onFocus={() => setHover(h.id)}
            onBlur={() => setHover((c) => (c === h.id ? null : c))}
            onKeyDown={(e) => {
              if (e.key === "Escape") setHover(null);
            }}
          />
        ))}
        {active !== null ? (
          <div
            id={`${summaryId}-tip`}
            role="tooltip"
            data-testid="scene-tooltip"
            className="pointer-events-none absolute z-10 max-w-[260px] -translate-x-1/2 rounded-[8px] border border-[#4A4750] bg-[#2A292F] px-3 py-2 text-sm text-[#F3EDE0]"
            style={{
              left: `${Math.min(88, Math.max(12, ((active.rect.x + active.rect.w / 2) / SCENE_W) * 100))}%`,
              top: `${Math.max(0, (active.rect.y / SCENE_H) * 100 - 2)}%`,
              transform: "translate(-50%, -100%)",
            }}
          >
            {active.tooltip}
          </div>
        ) : null}
      </div>
      </div>
      <div className="sr-only">
        <WorldViewSummary model={model} announce={announce} id={summaryId} />
      </div>
    </div>
  );
}
