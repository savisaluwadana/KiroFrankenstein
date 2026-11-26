"use client";

import { useEffect, useMemo, useRef } from "react";
import { Application } from "pixi.js";
import type { TickFrame } from "@/lib/api/client";
import { readPalette } from "./scene/palette";
import { WorldScene } from "./scene/WorldScene";
import { buildWorldViewModel, type Intervention } from "./scene/worldViewModel";
import { WorldViewSummary } from "./WorldViewSummary";

export interface WorldViewProps {
  /** Authoritative frame of the displayed Tick, or null before any frame loads. */
  frame: TickFrame | null;
  /** True while data for a requested Tick is loading; the previous frame is shown dimmed. */
  syncing?: boolean;
  /** Commands that take effect at a Tick, with provenance (slice: human). */
  interventions?: readonly Intervention[];
  /** Bin display maximum; use maxOnHandSeen(frames). Defaults to the frame's own maximum. */
  stockScaleMax?: number;
  className?: string;
}

export function WorldView({ frame, syncing = false, interventions, stockScaleMax, className }: WorldViewProps) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const sceneRef = useRef<WorldScene | null>(null);

  const model = useMemo(() => {
    if (frame === null) return null;
    const opts: Parameters<typeof buildWorldViewModel>[1] = { syncing };
    if (interventions !== undefined) opts.interventions = interventions;
    if (stockScaleMax !== undefined) opts.stockScaleMax = stockScaleMax;
    return buildWorldViewModel(frame, opts);
  }, [frame, syncing, interventions, stockScaleMax]);
  const modelRef = useRef(model);
  useEffect(() => {
    modelRef.current = model;
  }, [model]);

  useEffect(() => {
    const host = hostRef.current;
    if (host === null) return;
    let cancelled = false;
    const app = new Application();
    let ready = false;
    let observer: ResizeObserver | null = null;
    let media: MediaQueryList | null = null;
    let onMedia: (() => void) | null = null;

    void app
      .init({
        width: Math.max(1, host.clientWidth),
        height: Math.max(1, host.clientHeight),
        background: readPalette(host).ink,
        antialias: true,
        resolution: window.devicePixelRatio || 1,
        autoDensity: true,
      })
      .then(() => {
        ready = true;
        if (cancelled) {
          app.destroy(true, { children: true });
          return;
        }
        host.appendChild(app.canvas);
        const scene = new WorldScene(app, readPalette(host));
        sceneRef.current = scene;
        const fit = (): void => {
          const w = Math.max(1, host.clientWidth);
          const h = Math.max(1, host.clientHeight);
          app.renderer.resize(w, h);
          scene.resize(w, h);
        };
        fit();
        observer = new ResizeObserver(fit);
        observer.observe(host);
        media = window.matchMedia("(prefers-reduced-motion: reduce)");
        onMedia = (): void => scene.setReducedMotion(media?.matches ?? false);
        onMedia();
        media.addEventListener("change", onMedia);
        if (modelRef.current !== null) scene.update(modelRef.current);
      });

    return () => {
      cancelled = true;
      observer?.disconnect();
      if (media !== null && onMedia !== null) media.removeEventListener("change", onMedia);
      if (ready) {
        sceneRef.current?.destroy();
        sceneRef.current = null;
        app.destroy(true, { children: true });
      }
    };
  }, []);

  useEffect(() => {
    if (model !== null) sceneRef.current?.update(model);
  }, [model]);

  return (
    <div className={className}>
      <div
        ref={hostRef}
        role="img"
        aria-label="Warehouse diorama of the displayed tick. A text summary follows."
        className="aspect-video w-full overflow-hidden rounded-sm border border-line bg-ink"
        data-syncing={syncing ? "true" : "false"}
      />
      <WorldViewSummary
        frame={frame}
        syncing={syncing}
        {...(interventions !== undefined ? { interventions } : {})}
        {...(stockScaleMax !== undefined ? { stockScaleMax } : {})}
      />
    </div>
  );
}
