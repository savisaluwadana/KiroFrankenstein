/**
 * Isometric warehouse scene (PixiJS v8, procedural graphics only).
 *
 * Persistent state (stacks, people, tickets, parked vans, signs) is always set
 * straight from the SceneModel, so it equals the authoritative frame. Transient
 * actors (trucks, flying boxes, departing vans, incoming tickets, onset flash)
 * exist only for events returned by deriveSceneEvents and are skipped under
 * reduced motion, when scrubbing, and at playback speeds too fast to read.
 * Rendering is on demand: the ticker runs only while a tween or ambient cue is
 * active.
 */
import { Container, Graphics, Sprite, type Application, type Ticker } from "pixi.js";
import type { SceneEvent } from "./deriveSceneEvents";
import {
  drawBackground,
  drawBarrier,
  drawBench,
  drawBoard,
  drawLight,
  drawRackBack,
  drawRackFront,
  drawTicket,
  drawTruck,
  drawVan,
  drawWarning,
  makeBoxTexture,
  makeFigureTexture,
  Tag,
  type FigureVariant,
  type SpriteTexture,
} from "./drawing";
import { fitScale, project, SCENE_H, SCENE_W, zIndex, type Pt } from "./iso";
import { BANNER_ICON_W, DEFAULT_FONT, labelScaleFor, layoutBanners, layoutLabels } from "./labelLayout";
import { buildSceneLabels } from "./sceneLabels";
import {
  BENCH_D,
  benchOrigin,
  breakSpot,
  CONVEYOR_GY,
  figureSpot,
  MAX_ABSENT_DRAWN,
  MAX_FIGURES,
  MAX_TICKETS,
  MAX_TRAINING_DRAWN,
  MAX_TRUCKS,
  MAX_VANS,
  rackOrigin,
  RACK_D,
  RACK_SLOTS,
  RACK_W,
  slotOffset,
  STATIONS,
  ticketSlot,
  TRUCK_LANE_GX,
  TRUCK_LEN,
  TRUCK_START_GY,
  TRUCK_W,
  truckBay,
  VAN_D,
  VAN_EXIT_GX,
  VAN_W,
  vanBay,
  ZONES,
  BOX_D,
  BOX_W,
  type ZoneName,
} from "./layout";
import { PALETTE } from "./palette";
import { easeInOut, easeOut, Pool, TweenList } from "./pools";
import type { SceneModel } from "./sceneModel";
import type { ZoneId } from "./worldViewModel";

const ZONE_OF: Record<ZoneId, ZoneName> = {
  inbound_dock: "inbound",
  storage_racks: "storage",
  pick_line: "packing",
  order_queue: "orderLine",
};

/** Base (1x) durations in ms. */
const BASE = { truckIn: 700, unload: 500, truckOut: 600, flight: 900, vanOut: 600, slide: 180, flash: 1200 };
const BASE_TOTAL = 1900;
/** Below this display interval (16x playback and faster) only counts change. */
const MIN_TRAVEL_INTERVAL_MS = 120;

export interface UpdateOptions {
  /** Time between displayed frames. Unknown or large means a full-length animation. */
  tickIntervalMs?: number;
  /** False snaps with no travel (scrubbing, jumping). */
  animate?: boolean;
}

interface RackView {
  back: Graphics;
  front: Graphics;
  boxes: Sprite[];
  style: number;
}

interface Figure {
  sprite: Sprite;
  baseY: number;
  bob: boolean;
}

export class WorldScene {
  private readonly root = new Container();
  private readonly bg = new Container();
  private readonly world = new Container();
  private readonly labels = new Container();
  /** Enlargement of label text so it stays readable when the scene is scaled down. */
  private labelScale = 1;
  private readonly tweens = new TweenList();
  private readonly onTick = (t: Ticker): void => this.tick(t);
  private reduced = false;
  private active = true;
  private clock = 0;
  private model: SceneModel | null = null;
  private destroyed = false;

  private readonly boxTex: SpriteTexture[] = [];
  private readonly figTex: Record<FigureVariant, SpriteTexture>;

  private racks: RackView[] = [];
  private readonly benches: Graphics[] = [];
  private readonly lights: Graphics[] = [];
  private readonly figures: Figure[] = [];
  private readonly training: Sprite[] = [];
  private readonly ghosts: Sprite[] = [];
  private readonly crowd: Sprite[] = [];
  private readonly tickets: Graphics[] = [];
  private readonly parkedVans: Graphics[] = [];
  private readonly leavingVans: Graphics[] = [];
  private readonly trucks: Graphics[] = [];
  private readonly flyers: Pool<Sprite>;
  private readonly zoneFlash = new Map<ZoneName, Graphics>();
  private readonly zoneOutline = new Map<ZoneName, Graphics>();
  private readonly barrier = new Graphics();
  private readonly leaders = new Graphics();
  private readonly measurer: Tag;
  private readonly labelPool: Tag[] = [];
  private readonly bannerTags: Tag[] = [];
  private readonly bannerIcons: Graphics[] = [];
  private lightsCrisis = false;
  private stationLit: boolean[] = [];
  private disruptionZones = new Set<ZoneName>();

  constructor(
    private readonly app: Application,
    private readonly fontFamily: string,
  ) {
    this.root.eventMode = "none";
    this.world.sortableChildren = true;
    this.world.eventMode = "none";
    this.labels.eventMode = "none";
    this.root.addChild(this.bg, this.world, this.labels);
    app.stage.addChild(this.root);

    drawBackground(this.bg);
    this.bg.cacheAsTexture(true);

    const r = app.renderer;
    for (let i = 0; i < 6; i++) this.boxTex.push(makeBoxTexture(r, i));
    this.figTex = {
      regular: makeFigureTexture(r, "regular"),
      temp: makeFigureTexture(r, "temp"),
      ghost: makeFigureTexture(r, "ghost"),
    };

    this.buildFixtures();
    this.measurer = new Tag(fontFamily);
    this.labels.addChild(this.leaders);
    this.flyers = new Pool<Sprite>(24, () => {
      const s = new Sprite(this.boxTex[0]?.texture);
      s.visible = false;
      this.world.addChild(s);
      return s;
    });

    app.ticker.add(this.onTick);
    app.ticker.stop();
    app.ticker.maxFPS = 0;
  }

  /* ---------- public API ---------- */

  resize(width: number, height: number): void {
    const s = fitScale(width, height);
    const k = labelScaleFor(s);
    const relabel = k !== this.labelScale;
    this.labelScale = k;
    this.root.scale.set(s);
    this.root.position.set((width - SCENE_W * s) / 2, (height - SCENE_H * s) / 2);
    if (relabel && this.model !== null) this.applyTags(this.model);
    this.renderNow();
  }

  setReducedMotion(reduced: boolean): void {
    if (this.reduced === reduced) return;
    this.reduced = reduced;
    this.tweens.finish();
    this.hideActors();
    if (this.model !== null) this.applyAmbient(true);
    this.ensureLoop();
    this.renderNow();
  }

  /** Pause all ticker work while the canvas is off screen or the tab is hidden. */
  setActive(active: boolean): void {
    this.active = active;
    this.ensureLoop();
    if (active) this.renderNow();
  }

  update(model: SceneModel, opts: UpdateOptions = {}): void {
    if (this.destroyed) return;
    const prev = this.model;
    this.model = model;
    const T = opts.tickIntervalMs ?? 1000;
    // The same tick re-rendered (for example new interventions) never replays motion.
    const sameTick = prev !== null && prev.tick === model.tick;
    const animate = !sameTick && !this.reduced && opts.animate !== false && !model.syncing && T >= MIN_TRAVEL_INTERVAL_MS && model.events.length > 0;
    if (!animate && !sameTick) {
      this.tweens.finish();
      this.hideActors();
    }
    this.ensureRacks(model);
    this.applyState(model);
    if (animate) this.playEvents(model, prev, Math.min(1, (T * 2) / BASE_TOTAL));
    this.root.alpha = model.syncing ? 0.45 : 1;
    this.applyAmbient(true);
    this.ensureLoop();
    this.renderNow();
  }

  destroy(): void {
    if (this.destroyed) return;
    this.destroyed = true;
    this.app.ticker.remove(this.onTick);
    this.app.ticker.stop();
    this.tweens.finish();
    this.root.destroy({ children: true });
    for (const t of this.boxTex) t.texture.destroy(true);
    for (const t of Object.values(this.figTex)) t.texture.destroy(true);
  }

  /* ---------- construction ---------- */

  private sprite(tex: SpriteTexture): Sprite {
    const s = new Sprite(tex.texture);
    s.pivot.set(tex.pivotX, tex.pivotY);
    s.visible = false;
    s.eventMode = "none";
    this.world.addChild(s);
    return s;
  }

  private buildFixtures(): void {
    // Benches and lights.
    for (let k = 0; k < STATIONS; k++) {
      const o = benchOrigin(k);
      const b = new Graphics();
      drawBench(b);
      const p = project(o.gx, o.gy);
      b.position.set(p.x, p.y);
      b.zIndex = zIndex(o.gx + 2, o.gy + BENCH_D);
      this.world.addChild(b);
      this.benches.push(b);
      const l = new Graphics();
      drawLight(l, PALETTE.edge);
      const lp = project(o.gx + 1.75, o.gy + 0.35, 1.35);
      l.position.set(lp.x, lp.y);
      l.zIndex = zIndex(o.gx + 2, o.gy + BENCH_D) + 1;
      this.world.addChild(l);
      this.lights.push(l);
    }
    // Order board and tickets.
    const board = new Graphics();
    drawBoard(board);
    const bp = project(16.6, 9.7);
    board.position.set(bp.x, bp.y);
    board.zIndex = zIndex(22, 9.7);
    this.world.addChild(board);
    for (let i = 0; i < MAX_TICKETS; i++) {
      const t = new Graphics();
      drawTicket(t);
      const s = ticketSlot(i);
      const p = project(s.gx, 9.68, s.gz);
      t.position.set(p.x, p.y);
      t.zIndex = zIndex(22, 9.7) + 1 + i * 0.001;
      t.visible = false;
      this.world.addChild(t);
      this.tickets.push(t);
    }
    // Figures.
    for (let i = 0; i < MAX_FIGURES; i++) {
      this.figures.push({ sprite: this.sprite(this.figTex.regular), baseY: 0, bob: false });
    }
    for (let i = 0; i < MAX_TRAINING_DRAWN; i++) {
      const s = this.sprite(this.figTex.temp);
      s.alpha = 0.55;
      this.training.push(s);
    }
    for (let i = 0; i < MAX_ABSENT_DRAWN; i++) this.ghosts.push(this.sprite(this.figTex.ghost));
    for (let i = 0; i < 8; i++) {
      const s = this.sprite(this.figTex.regular);
      s.alpha = 0.7;
      s.scale.set(0.85);
      this.crowd.push(s);
    }
    // Vans and trucks.
    for (let i = 0; i < MAX_VANS; i++) {
      for (const list of [this.parkedVans, this.leavingVans]) {
        const v = new Graphics();
        drawVan(v, VAN_W, VAN_D);
        v.visible = false;
        this.world.addChild(v);
        list.push(v);
      }
    }
    for (let i = 0; i < MAX_TRUCKS; i++) {
      const t = new Graphics();
      drawTruck(t, TRUCK_LEN, TRUCK_W);
      t.visible = false;
      this.world.addChild(t);
      this.trucks.push(t);
    }
    // Dock barrier.
    drawBarrier(this.barrier);
    const bar = project(TRUCK_LANE_GX - 0.2, 11.4);
    this.barrier.position.set(bar.x, bar.y);
    this.barrier.zIndex = zIndex(TRUCK_LANE_GX + 1.8, 11.5);
    this.barrier.visible = false;
    this.world.addChild(this.barrier);
    // Zone outlines (disruption) and onset flashes.
    for (const name of Object.keys(ZONES) as ZoneName[]) {
      const z = ZONES[name];
      const pts = [project(z.gx, z.gy), project(z.gx + z.w, z.gy), project(z.gx + z.w, z.gy + z.h), project(z.gx, z.gy + z.h)].flatMap((q) => [q.x, q.y]);
      const o = new Graphics();
      o.poly(pts).stroke({ width: 3, color: PALETTE.clay });
      o.visible = false;
      this.root.addChildAt(o, 1);
      this.zoneOutline.set(name, o);
      const f = new Graphics();
      f.poly(pts).fill({ color: PALETTE.clay, alpha: 0.35 });
      f.alpha = 0;
      this.root.addChildAt(f, 1);
      this.zoneFlash.set(name, f);
    }
  }

  private ensureRacks(model: SceneModel): void {
    const n = model.racks.length;
    if (this.racks.length === n) return;
    for (const r of this.racks) {
      r.back.destroy();
      r.front.destroy();
      for (const b of r.boxes) b.destroy();
    }
    this.racks = [];
    for (let k = 0; k < n; k++) {
      const o = rackOrigin(k, n);
      const p = project(o.gx, o.gy);
      const back = new Graphics();
      drawRackBack(back);
      back.position.set(p.x, p.y);
      back.zIndex = zIndex(o.gx, o.gy) - 50;
      const front = new Graphics();
      drawRackFront(front);
      front.position.set(p.x, p.y);
      front.zIndex = zIndex(o.gx + RACK_W, o.gy + RACK_D) + 500;
      this.world.addChild(back, front);
      const boxes: Sprite[] = [];
      for (let i = 0; i < RACK_SLOTS; i++) {
        const s = new Sprite(this.boxTex[0]?.texture);
        const so = slotOffset(i);
        const bp = project(o.gx + so.dx, o.gy + so.dy, so.level * 1 + 0.12);
        s.position.set(bp.x, bp.y);
        s.zIndex = zIndex(o.gx + so.dx + BOX_W, o.gy + so.dy + BOX_D, so.level);
        s.visible = false;
        s.eventMode = "none";
        this.world.addChild(s);
        boxes.push(s);
      }
      this.racks.push({ back, front, boxes, style: -1 });
    }
  }

  /* ---------- persistent state from the model ---------- */

  private applyState(m: SceneModel): void {
    // Racks and boxes.
    m.racks.forEach((r, k) => {
      const v = this.racks[k];
      if (v === undefined) return;
      const style = r.styleIndex % this.boxTex.length;
      const tex = this.boxTex[style];
      if (tex === undefined) return;
      if (v.style !== style) {
        for (const s of v.boxes) {
          s.texture = tex.texture;
          s.pivot.set(tex.pivotX, tex.pivotY);
        }
        v.style = style;
      }
      v.boxes.forEach((s, i) => (s.visible = i < r.boxes));
    });

    // People at stations.
    let f = 0;
    const p = m.people;
    p.perStation.forEach((count, k) => {
      for (let j = 0; j < count; j++, f++) {
        const fig = this.figures[f];
        if (fig === undefined) return;
        const isTemp = f >= p.drawnRegular;
        const tex = isTemp ? this.figTex.temp : this.figTex.regular;
        fig.sprite.texture = tex.texture;
        const spot = figureSpot(k, j);
        const pt = project(spot.gx, spot.gy);
        fig.sprite.position.set(pt.x, pt.y);
        fig.baseY = pt.y;
        fig.bob = true;
        fig.sprite.zIndex = zIndex(spot.gx, spot.gy) + 0.5;
        fig.sprite.visible = true;
      }
    });
    for (; f < this.figures.length; f++) {
      const fig = this.figures[f];
      if (fig !== undefined) fig.sprite.visible = false;
    }
    this.stationLit = p.perStation.map((c) => c > 0);
    this.lightsCrisis = m.bottlenecks.waitingForCapacityOrders > 0;
    this.placeRow(this.training, p.onboardingDrawn, 0);
    this.placeRow(this.ghosts, p.absentDrawn, 1);

    // Crowd at the order line during a demand surge.
    const surge = m.disruptions.some((d) => d.kind === "demand_surge");
    this.crowd.forEach((s, i) => {
      s.visible = surge;
      if (!surge) return;
      const gx = 16.9 + i * 0.55;
      const gy = 13.2 + (i % 2) * 0.5;
      const pt = project(gx, gy);
      s.position.set(pt.x, pt.y);
      s.zIndex = zIndex(gx, gy);
    });

    // Tickets.
    this.tickets.forEach((t, i) => {
      t.visible = i < m.queue.tickets;
      const s = ticketSlot(i);
      const pt = project(s.gx, 9.68, s.gz);
      t.position.set(pt.x, pt.y);
      t.alpha = 1;
    });

    // Parked vans (this hour's shipments).
    this.parkedVans.forEach((v, k) => {
      v.visible = k < m.outbound.vans;
      const b = vanBay(k);
      const pt = project(b.gx, b.gy);
      v.position.set(pt.x, pt.y);
      v.alpha = 1;
      v.zIndex = zIndex(b.gx + VAN_W, b.gy + VAN_D);
    });

    // Dock closure.
    this.barrier.visible = m.dock.closed;

    // Zone highlight set.
    this.disruptionZones = new Set(m.disruptions.map((d) => ZONE_OF[d.zone]));

    this.applyTags(m);
  }

  private placeRow(list: Sprite[], count: number, row: 0 | 1): void {
    list.forEach((s, i) => {
      s.visible = i < count;
      if (i >= count) return;
      const spot = breakSpot(row, i);
      const pt = project(spot.gx, spot.gy);
      s.position.set(pt.x, pt.y);
      s.zIndex = zIndex(spot.gx, spot.gy);
    });
  }

  private measure = (text: string, size: number): { w: number; h: number } => {
    this.measurer.set(text, "normal", size);
    return { w: this.measurer.boxW, h: this.measurer.boxH };
  };

  private poolTag(list: Tag[], i: number): Tag {
    let t = list[i];
    if (t === undefined) {
      t = new Tag(this.fontFamily);
      this.labels.addChild(t);
      list.push(t);
    }
    return t;
  }

  /** Banners in a column along the top edge, then every other label through the collision-free layout. */
  private applyTags(m: SceneModel): void {
    const specs = buildSceneLabels(m);
    const k = this.labelScale;
    const bannerFont = Math.round(DEFAULT_FONT * k);
    const banners = layoutBanners(specs.banners, this.measure, bannerFont, Math.round(8 * k), Math.round(BANNER_ICON_W * k));
    this.bannerTags.forEach((t) => (t.visible = false));
    this.bannerIcons.forEach((g) => (g.visible = false));
    banners.forEach((b, i) => {
      const tag = this.poolTag(this.bannerTags, i);
      tag.set(b.text, "crisis", bannerFont);
      tag.position.set(b.textX + tag.boxW / 2, b.rect.y + tag.boxH);
      tag.visible = true;
      let icon = this.bannerIcons[i];
      if (icon === undefined) {
        icon = new Graphics();
        drawWarning(icon, PALETTE.clay);
        this.labels.addChild(icon);
        this.bannerIcons.push(icon);
      }
      icon.scale.set(k);
      icon.position.set(b.iconCenter.x, b.iconCenter.y);
      icon.visible = true;
    });

    const bounds = { x: 4, y: 4, w: SCENE_W - 8, h: SCENE_H - 8 };
    const result = layoutLabels(
      specs.labels,
      this.measure,
      bounds,
      banners.map((b) => b.rect),
      k,
    );
    this.labelPool.forEach((t) => (t.visible = false));
    this.leaders.clear();
    result.placed.forEach((p, i) => {
      const tag = this.poolTag(this.labelPool, i);
      tag.set(p.text, p.tone, p.fontSize);
      tag.position.set(p.rect.x + p.rect.w / 2, p.rect.y + p.rect.h);
      tag.visible = true;
      if (p.leaderTo !== null) {
        const cx = Math.min(Math.max(p.leaderTo.x, p.rect.x), p.rect.x + p.rect.w);
        const cy = Math.min(Math.max(p.leaderTo.y, p.rect.y), p.rect.y + p.rect.h);
        this.leaders.moveTo(cx, cy).lineTo(p.leaderTo.x, p.leaderTo.y).stroke({ width: 1, color: PALETTE.ivoryDim, alpha: 0.8 });
        this.leaders.circle(p.leaderTo.x, p.leaderTo.y, 1.8).fill({ color: PALETTE.ivoryDim, alpha: 0.9 });
      }
    });
  }

  /* ---------- events ---------- */

  private playEvents(m: SceneModel, prev: SceneModel | null, f: number): void {
    const d = (ms: number): number => Math.max(60, ms * f);
    let truckSlot = 0;
    const shipment = m.events.find((e): e is Extract<SceneEvent, { kind: "shipment" }> => e.kind === "shipment");

    // Vans from the previous hour leave; this hour's vans pull in.
    const leaving = prev?.outbound.vans ?? 0;
    for (let k = 0; k < leaving; k++) this.vanDeparts(k, d(BASE.vanOut), 0);
    if (shipment !== undefined) {
      for (let k = 0; k < shipment.vans; k++) {
        const van = this.parkedVans[k];
        const b = vanBay(k);
        if (van === undefined) continue;
        van.alpha = 0;
        this.tweens.add({
          delay: d(BASE.vanOut) * 0.6,
          dur: d(BASE.slide) * 2,
          tag: van,
          apply: (t) => {
            const e = easeOut(t);
            const pt = project(b.gx - (1 - e) * 2.5, b.gy);
            van.position.set(pt.x, pt.y);
            van.alpha = e;
          },
          onStart: () => {
            van.visible = true;
            van.alpha = 0;
          },
        });
      }
    }

    let pick = 0;
    for (const e of m.events) {
      switch (e.kind) {
        case "delivery": {
          const rackIdx = m.racks.findIndex((r) => r.sku === e.sku);
          for (let i = 0; i < e.trucks && truckSlot < MAX_TRUCKS; i++, truckSlot++) {
            this.truckVisit(truckSlot, f, i === 0 ? { rackIdx, rackCount: m.racks.length, boxes: e.boxes, style: m.racks[rackIdx]?.styleIndex ?? 0 } : null);
          }
          break;
        }
        case "stock_picked": {
          const rackIdx = m.racks.findIndex((r) => r.sku === e.sku);
          if (rackIdx < 0) break;
          const vanCount = Math.max(1, m.outbound.vans);
          for (let i = 0; i < e.boxes; i++, pick++) {
            const bay = vanBay(pick % vanCount);
            const ro = rackOrigin(rackIdx, m.racks.length);
            const path: Pt[] = [
              project(ro.gx + 1.9, ro.gy + RACK_D + 0.1, 1.4),
              project(ro.gx + 1.9, CONVEYOR_GY + 0.25, 0.9),
              project(16.2, CONVEYOR_GY + 0.25, 0.9),
              project(bay.gx + 0.8, bay.gy + 0.5, 1.2),
            ];
            this.flyBox(m.racks[rackIdx]?.styleIndex ?? 0, path, i * d(120), d(BASE.flight));
          }
          break;
        }
        case "orders_arrived": {
          const n = Math.min(e.drawn, m.queue.tickets);
          for (let i = 0; i < n; i++) this.ticketSlides(m.queue.tickets - 1 - i, i, d(BASE.slide));
          break;
        }
        case "disruption_onset": {
          const kind = m.disruptions.find((x) => x.kind === e.disruption);
          const flash = kind === undefined ? undefined : this.zoneFlash.get(ZONE_OF[kind.zone]);
          if (flash !== undefined) {
            this.tweens.add({ delay: 0, dur: d(BASE.flash), tag: flash, apply: (t) => (flash.alpha = 1 - t), onStart: () => (flash.alpha = 1), onEnd: () => (flash.alpha = 0) });
          }
          break;
        }
        case "shipment":
          break;
      }
    }
  }

  private vanDeparts(k: number, dur: number, delay: number): void {
    const van = this.leavingVans[k];
    if (van === undefined) return;
    const b = vanBay(k);
    this.tweens.finish(van);
    this.tweens.add({
      delay,
      dur,
      tag: van,
      apply: (t) => {
        const gx = b.gx + (VAN_EXIT_GX - b.gx) * easeInOut(t);
        const pt = project(gx, b.gy);
        van.position.set(pt.x, pt.y);
        van.zIndex = zIndex(gx + VAN_W, b.gy + VAN_D);
      },
      onStart: () => (van.visible = true),
      onEnd: () => (van.visible = false),
    });
  }

  private truckVisit(slot: number, f: number, cargo: { rackIdx: number; rackCount: number; boxes: number; style: number } | null): void {
    const truck = this.trucks[slot];
    if (truck === undefined) return;
    const bay = truckBay(slot);
    const tin = Math.max(80, BASE.truckIn * f);
    const unload = Math.max(80, BASE.unload * f);
    const tout = Math.max(80, BASE.truckOut * f);
    this.tweens.finish(truck);
    const setPos = (gy: number): void => {
      const pt = project(bay.gx, gy);
      truck.position.set(pt.x, pt.y);
      truck.zIndex = zIndex(bay.gx + TRUCK_W, gy + TRUCK_LEN);
    };
    this.tweens.add({
      delay: slot * 80,
      dur: tin,
      tag: truck,
      apply: (t) => setPos(TRUCK_START_GY + (bay.gy - TRUCK_START_GY) * easeOut(t)),
      onStart: () => (truck.visible = true),
    });
    this.tweens.add({ delay: slot * 80 + tin + unload, dur: tout, tag: truck, apply: (t) => setPos(bay.gy + (TRUCK_START_GY - bay.gy) * easeInOut(t)), onEnd: () => (truck.visible = false) });
    if (cargo !== null && cargo.rackIdx >= 0) {
      const ro = rackOrigin(cargo.rackIdx, cargo.rackCount);
      for (let i = 0; i < cargo.boxes; i++) {
        const path: Pt[] = [
          project(bay.gx + TRUCK_W - 0.1, bay.gy + TRUCK_LEN - 0.6, 1.0),
          project(5.9, bay.gy + 1.2, 1.4),
          project(ro.gx + 0.4 + (i % 3) * 0.5, ro.gy + RACK_D + 0.2, 1.4),
        ];
        this.flyBox(cargo.style, path, slot * 80 + tin + (i * unload) / Math.max(1, cargo.boxes), unload * 1.4);
      }
    }
  }

  private flyBox(style: number, path: Pt[], delay: number, dur: number): void {
    const s = this.flyers.acquire();
    const tex = this.boxTex[style % this.boxTex.length];
    if (s === null || tex === undefined || path.length < 2) return;
    s.texture = tex.texture;
    s.pivot.set(tex.pivotX, tex.pivotY);
    const segs = path.length - 1;
    this.tweens.add({
      delay,
      dur,
      tag: s,
      apply: (t) => {
        const u = easeInOut(t) * segs;
        const i = Math.min(segs - 1, Math.floor(u));
        const a = path[i];
        const b = path[i + 1];
        if (a === undefined || b === undefined) return;
        const k = u - i;
        s.position.set(a.x + (b.x - a.x) * k, a.y + (b.y - a.y) * k - Math.sin(Math.PI * k) * 12);
        s.zIndex = 99999;
      },
      onStart: () => (s.visible = true),
      onEnd: () => {
        s.visible = false;
        this.flyers.release(s);
      },
    });
  }

  private ticketSlides(index: number, order: number, dur: number): void {
    const t = this.tickets[index];
    if (t === undefined) return;
    const home = { x: t.x, y: t.y };
    t.alpha = 0;
    this.tweens.add({
      delay: order * dur * 0.4,
      dur: dur * 1.5,
      tag: t,
      apply: (k) => {
        const e = easeOut(k);
        t.position.set(home.x + (1 - e) * 220, home.y - (1 - e) * 110);
        t.alpha = e;
      },
      onStart: () => (t.alpha = 0),
      onEnd: () => {
        t.position.set(home.x, home.y);
        t.alpha = 1;
      },
    });
  }

  private hideActors(): void {
    for (const v of this.leavingVans) v.visible = false;
    for (const t of this.trucks) t.visible = false;
    for (const s of this.flyers.all()) s.visible = false;
    this.flyers.releaseAll();
    for (const f of this.zoneFlash.values()) f.alpha = 0;
    this.tickets.forEach((t) => (t.alpha = 1));
    this.parkedVans.forEach((v) => (v.alpha = 1));
    if (this.model !== null) {
      // Restore resting positions that a cancelled tween may have moved.
      this.applyState(this.model);
    }
  }

  /* ---------- ambient and loop ---------- */

  private ambientNeeded(): boolean {
    if (this.reduced || this.model === null) return false;
    return this.figures.some((f) => f.sprite.visible) || this.disruptionZones.size > 0 || this.lightsCrisis;
  }

  /** Static (or reduced-motion) ambient values; the loop overwrites them while running. */
  private applyAmbient(force = false): void {
    const bobStep = this.reduced ? 0 : Math.floor(this.clock / 500);
    this.figures.forEach((f, i) => {
      if (f.sprite.visible && f.bob) f.sprite.y = f.baseY - (this.reduced ? 0 : (bobStep + i) % 2) * 1.5;
    });
    const wave = this.reduced || !force ? 1 : 0.5 + 0.5 * Math.sin((this.clock / 1000) * Math.PI * 2);
    const alpha = this.reduced ? 0.7 : 0.45 + 0.4 * wave;
    for (const [name, g] of this.zoneOutline) {
      g.visible = this.disruptionZones.has(name);
      g.alpha = alpha;
    }
    this.lights.forEach((l, k) => {
      const lit = this.stationLit[k] === true;
      drawLightIfChanged(l, lit ? (this.lightsCrisis ? PALETTE.clay : PALETTE.sage) : PALETTE.edge);
      l.alpha = lit && this.lightsCrisis ? alpha + 0.15 : lit ? 1 : 0.5;
    });
  }

  private ensureLoop(): void {
    if (this.destroyed) return;
    const need = this.active && (this.tweens.active || this.ambientNeeded());
    this.app.ticker.maxFPS = this.tweens.active ? 0 : 12;
    if (need) {
      if (!this.app.ticker.started) this.app.ticker.start();
    } else if (this.app.ticker.started) {
      this.app.ticker.stop();
    }
  }

  private tick(t: Ticker): void {
    if (this.destroyed) return;
    this.clock += t.deltaMS;
    this.tweens.step(t.deltaMS);
    this.applyAmbient(true);
    if (!this.active || !(this.tweens.active || this.ambientNeeded())) {
      this.app.ticker.stop();
      this.renderNow();
    }
  }

  private renderNow(): void {
    if (this.destroyed || !this.active) return;
    if (!this.app.ticker.started) this.app.render();
  }
}

const lightColors = new WeakMap<Graphics, number>();
function drawLightIfChanged(g: Graphics, color: number): void {
  if (lightColors.get(g) === color) return;
  lightColors.set(g, color);
  drawLight(g, color);
}

