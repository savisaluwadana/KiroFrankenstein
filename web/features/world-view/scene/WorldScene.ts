/**
 * Pixi drawing for the warehouse diorama. It consumes a WorldViewModel and
 * draws only what the model holds. Graphics and Text only, no external assets.
 *
 * Decorative motion (worker and conveyor markers easing to their slots, the
 * Disruption edge pulse) is display interpolation between two authoritative
 * ticks. It never encodes a number the model does not hold, and reduced motion
 * turns it off.
 */
import { Container, Graphics, Text, type Application, type Ticker } from "pixi.js";
import type { Palette } from "./palette";
import type { InboundGroup, WorldViewModel, ZoneId } from "./worldViewModel";

export const SCENE_WIDTH = 960;
export const SCENE_HEIGHT = 540;

interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

const ZONES: Record<ZoneId, Rect & { label: string }> = {
  inbound_dock: { x: 20, y: 40, w: 200, h: 250, label: "INBOUND DOCK" },
  storage_racks: { x: 240, y: 40, w: 500, h: 250, label: "STORAGE RACKS" },
  pick_line: { x: 240, y: 310, w: 500, h: 100, label: "PICK / PACK LINE" },
  order_queue: { x: 20, y: 430, w: 920, h: 90, label: "ORDER QUEUE" },
};
const OUTBOUND: Rect = { x: 760, y: 40, w: 180, h: 370 };

const FONT = "IBM Plex Mono, ui-monospace, monospace";
const MAX_WORKER_MARKERS = 24;
const MAX_QUEUE_MARKERS = 40;
const MAX_TRUCKS = 6;

interface Mover {
  g: Graphics;
  tx: number;
  ty: number;
}

function label(text: string, fill: number, size = 11): Text {
  return new Text({ text, style: { fontFamily: FONT, fontSize: size, fill } });
}

export class WorldScene {
  private readonly root = new Container();
  private readonly staticLayer = new Container();
  private readonly workerLayer = new Container();
  private readonly queueLayer = new Container();
  private readonly workers: Mover[] = [];
  private readonly queue: Mover[] = [];
  private pulses: Graphics[] = [];
  private reducedMotion = false;
  private elapsed = 0;
  private readonly onTick = (t: Ticker): void => this.animate(t);

  constructor(
    private readonly app: Application,
    private readonly palette: Palette,
  ) {
    this.root.addChild(this.staticLayer, this.workerLayer, this.queueLayer);
    app.stage.addChild(this.root);
    app.ticker.add(this.onTick);
  }

  /** Fit the logical scene into a pixel box. */
  resize(width: number, height: number): void {
    const s = Math.min(width / SCENE_WIDTH, height / SCENE_HEIGHT);
    this.root.scale.set(s);
    this.root.position.set((width - SCENE_WIDTH * s) / 2, (height - SCENE_HEIGHT * s) / 2);
  }

  setReducedMotion(reduced: boolean): void {
    this.reducedMotion = reduced;
    if (reduced) this.snap();
  }

  update(model: WorldViewModel): void {
    this.drawStatic(model);
    this.syncMovers(this.workers, this.workerLayer, Math.min(model.workers.onShift, MAX_WORKER_MARKERS), (i) => ({
      x: ZONES.pick_line.x + 24 + i * 20,
      y: ZONES.pick_line.y + 60,
    }), this.palette.human);
    this.syncMovers(this.queue, this.queueLayer, Math.min(model.orderQueueLen, MAX_QUEUE_MARKERS), (i) => ({
      x: ZONES.order_queue.x + 20 + i * 20,
      y: ZONES.order_queue.y + 58,
    }), this.palette.text);
    this.root.alpha = model.syncing ? 0.45 : 1;
    if (this.reducedMotion) this.snap();
  }

  destroy(): void {
    this.app.ticker.remove(this.onTick);
    this.root.destroy({ children: true });
  }

  private zoneBox(id: ZoneId, highlighted: boolean): Graphics {
    const z = ZONES[id];
    const g = new Graphics();
    g.roundRect(z.x, z.y, z.w, z.h, 4).fill(this.palette.surface).stroke({ width: 1, color: highlighted ? this.palette.disruption : this.palette.line });
    return g;
  }

  private drawStatic(model: WorldViewModel): void {
    const p = this.palette;
    for (const c of this.staticLayer.removeChildren()) c.destroy({ children: true });
    this.pulses = [];
    const lit = new Set(model.highlightedZones);

    for (const id of Object.keys(ZONES) as ZoneId[]) {
      const z = ZONES[id];
      this.staticLayer.addChild(this.zoneBox(id, lit.has(id)));
      const t = label(z.label, p.muted, 10);
      t.position.set(z.x + 8, z.y + 6);
      this.staticLayer.addChild(t);
    }
    const out = new Graphics();
    out.roundRect(OUTBOUND.x, OUTBOUND.y, OUTBOUND.w, OUTBOUND.h, 4).fill(p.surface).stroke({ width: 1, color: p.line });
    this.staticLayer.addChild(out);
    const outLabel = label("OUTBOUND DOCK", p.muted, 10);
    outLabel.position.set(OUTBOUND.x + 8, OUTBOUND.y + 6);
    this.staticLayer.addChild(outLabel);

    this.drawBins(model);
    this.drawInbound(model);
    this.drawPickLine(model);
    this.drawQueueText(model);
    this.drawDisruptions(model);
    this.drawInterventions(model);

    const clock = label(`TICK ${model.tick}`, p.text, 12);
    clock.position.set(SCENE_WIDTH - 90, 8);
    this.staticLayer.addChild(clock);
    if (model.syncing) {
      const s = label("SYNCING", p.muted, 12);
      s.position.set(SCENE_WIDTH / 2 - 30, 8);
      this.staticLayer.addChild(s);
    }
  }

  private drawBins(model: WorldViewModel): void {
    const p = this.palette;
    const z = ZONES.storage_racks;
    const n = Math.max(1, model.bins.length);
    const slot = Math.min(110, (z.w - 20) / n);
    const binW = Math.max(24, slot - 16);
    const binH = 150;
    model.bins.forEach((b, i) => {
      const x = z.x + 12 + i * slot;
      const y = z.y + 36;
      const box = new Graphics();
      box.rect(x, y, binW, binH).stroke({ width: 1, color: b.stockBoundOrders > 0 ? p.disruption : p.line });
      const fillH = Math.floor((binH * b.fillBps) / 10000);
      if (fillH > 0) box.rect(x + 1, y + binH - fillH, binW - 2, fillH).fill(p.ai);
      this.staticLayer.addChild(box);
      const name = label(`SKU ${b.sku}`, p.text, 11);
      name.position.set(x, y + binH + 6);
      const qty = label(`${b.onHand} u`, p.muted, 11);
      qty.position.set(x, y + binH + 20);
      this.staticLayer.addChild(name, qty);
      if (b.stockBoundOrders > 0) {
        const m = label(`STOCK-BOUND ${b.stockBoundOrders}`, p.disruption, 9);
        m.position.set(x, y - 14);
        this.staticLayer.addChild(m);
      }
    });
    const scale = label(`bin full = ${model.stockScaleMax} u`, p.muted, 9);
    scale.position.set(z.x + z.w - 120, z.y + 6);
    this.staticLayer.addChild(scale);
  }

  private drawInbound(model: WorldViewModel): void {
    const p = this.palette;
    const z = ZONES.inbound_dock;
    let y = z.y + 28;
    const groups: InboundGroup[] = model.inbound;
    let trucks = 0;
    for (const g of groups) {
      const count = Math.min(g.pos, MAX_TRUCKS - trucks);
      for (let i = 0; i < count; i++, trucks++) {
        const t = new Graphics();
        t.rect(z.x + 12 + i * 26, y, 20, 12).fill(p.human);
        t.poly([z.x + 32 + i * 26, y, z.x + 40 + i * 26, y + 6, z.x + 32 + i * 26, y + 12]).fill(p.human);
        this.staticLayer.addChild(t);
      }
      const txt = label(`SKU ${g.sku}: ${g.pos} PO, ${g.units} u`, p.text, 10);
      txt.position.set(z.x + 12, y + 16);
      this.staticLayer.addChild(txt);
      y += 40;
      if (y > z.y + z.h - 30) break;
    }
    if (groups.length === 0) {
      const none = label("no inbound POs", p.muted, 10);
      none.position.set(z.x + 12, y);
      this.staticLayer.addChild(none);
    }
  }

  private drawPickLine(model: WorldViewModel): void {
    const p = this.palette;
    const z = ZONES.pick_line;
    const w = model.workers;
    const t = label(
      `${w.onShift} on shift (${w.regular} regular, ${w.temps} temp), ${w.onboarding} onboarding`,
      p.text,
      10,
    );
    t.position.set(z.x + 8, z.y + 24);
    this.staticLayer.addChild(t);
    const cap = model.bottlenecks;
    const c = label(`capacity ${cap.capacityUsed}/${cap.capacity} u`, p.muted, 10);
    c.position.set(z.x + 8, z.y + 38);
    this.staticLayer.addChild(c);
    if (cap.capacityBoundOrders > 0) {
      const m = label(`CAPACITY-BOUND ${cap.capacityBoundOrders}`, p.disruption, 10);
      m.position.set(z.x + z.w - 150, z.y + 6);
      this.staticLayer.addChild(m);
    }
  }

  private drawQueueText(model: WorldViewModel): void {
    const z = ZONES.order_queue;
    const t = label(`${model.orderQueueLen} open orders`, this.palette.text, 10);
    t.position.set(z.x + z.w - 120, z.y + 6);
    this.staticLayer.addChild(t);
  }

  private drawDisruptions(model: WorldViewModel): void {
    const p = this.palette;
    const rowByZone = new Map<ZoneId, number>();
    for (const d of model.disruptions) {
      const z = ZONES[d.zone];
      const edge = new Graphics();
      edge.roundRect(z.x, z.y, z.w, z.h, 4).stroke({ width: 3, color: p.disruption });
      edge.alpha = 0.5;
      this.pulses.push(edge);
      this.staticLayer.addChild(edge);
      const row = rowByZone.get(d.zone) ?? 0;
      rowByZone.set(d.zone, row + 1);
      const t = label(`${d.label.toUpperCase()}  +${d.ticksSinceOnset} ticks`, p.disruption, 10);
      t.position.set(z.x + 8, z.y + z.h - 18 - row * 14);
      this.staticLayer.addChild(t);
    }
  }

  private drawInterventions(model: WorldViewModel): void {
    const p = this.palette;
    model.interventions.forEach((m, i) => {
      const y = 28 + i * 14;
      const pin = new Graphics();
      pin.circle(SCENE_WIDTH - 300, y + 5, 4).fill(m.provenance.startsWith("Human") ? p.human : p.ai);
      this.staticLayer.addChild(pin);
      const t = label(`${m.provenance}: ${m.text}`, p.text, 10);
      t.position.set(SCENE_WIDTH - 290, y);
      this.staticLayer.addChild(t);
    });
  }

  private syncMovers(
    movers: Mover[],
    layer: Container,
    count: number,
    slot: (i: number) => { x: number; y: number },
    color: number,
  ): void {
    while (movers.length > count) {
      const m = movers.pop();
      m?.g.destroy();
    }
    while (movers.length < count) {
      const i = movers.length;
      const g = new Graphics();
      g.circle(0, 0, 6).fill(color);
      const s = slot(i);
      g.position.set(s.x, s.y);
      layer.addChild(g);
      movers.push({ g, tx: s.x, ty: s.y });
    }
    movers.forEach((m, i) => {
      const s = slot(i);
      m.tx = s.x;
      m.ty = s.y;
    });
  }

  private snap(): void {
    for (const m of [...this.workers, ...this.queue]) m.g.position.set(m.tx, m.ty);
    for (const g of this.pulses) g.alpha = 0.5;
  }

  private animate(t: Ticker): void {
    if (this.reducedMotion) return;
    this.elapsed += t.deltaMS;
    const k = Math.min(1, t.deltaMS / 180);
    for (const m of [...this.workers, ...this.queue]) {
      m.g.x += (m.tx - m.g.x) * k;
      m.g.y += (m.ty - m.g.y) * k;
    }
    const a = 0.5 + 0.3 * Math.sin(this.elapsed / 300);
    for (const g of this.pulses) g.alpha = a;
  }
}
