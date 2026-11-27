/**
 * Which labels the scene wants to show for a SceneModel, with anchors,
 * priorities and compact variants. Pure. Every number comes from the model.
 *
 * Merged meanings: the per-product "waiting for inventory" sentences become a
 * numeric badge on each rack plus one aggregate bottleneck tag; the "dock
 * closed" tag is the supplier-outage banner, so it is not drawn twice.
 * Disruptions are never floating labels: they are banners (see layoutBanners).
 */
import { project } from "./iso";
import { rackOrigin, RACK_D, RACK_W } from "./layout";
import type { BannerSpec, LabelSpec } from "./labelLayout";
import type { SceneModel } from "./sceneModel";

export const PRIORITY = { crisis: 100, moves: 90, bottleneck: 80, badge: 70, stock: 50, status: 30, ambient: 10 } as const;

const plural = (n: number, one: string, many = `${one}s`): string => `${n} ${n === 1 ? one : many}`;

export interface SceneLabelSpecs {
  banners: BannerSpec[];
  labels: LabelSpec[];
}

export function buildSceneLabels(m: SceneModel): SceneLabelSpecs {
  const labels: LabelSpec[] = [];
  const banners: BannerSpec[] = m.disruptions.map((d, i) => ({ id: `banner-${i}`, text: `${d.title}: ${d.text}` }));
  const surge = m.disruptions.some((d) => d.kind === "demand_surge");

  m.racks.forEach((r, k) => {
    const o = rackOrigin(k, m.racks.length);
    const hotspotId = `rack-${r.sku}`;
    labels.push({
      id: `stock-${r.sku}`,
      variants: [`${r.name}  ${r.onHand} in stock`, `${r.name} ${r.onHand}`, `${r.onHand}`],
      tone: "normal",
      priority: PRIORITY.stock,
      anchor: project(o.gx + RACK_W / 2, o.gy + RACK_D + 0.55, 0.1),
      hotspotId,
      fact: `${r.onHand} in stock`,
    });
    if (r.waitingForInventory > 0) {
      labels.push({
        id: `waiting-${r.sku}`,
        variants: [`${r.waitingForInventory}`],
        tone: "crisis",
        priority: PRIORITY.badge,
        anchor: project(o.gx + RACK_W / 2, o.gy, 3.5),
        fontSize: 11,
        hotspotId,
        fact: `Waiting for inventory: ${plural(r.waitingForInventory, "order")}`,
      });
    }
    if (r.moreBoxes > 0) {
      labels.push({
        id: `more-${r.sku}`,
        variants: [`+${r.moreBoxes} more boxes`, `+${r.moreBoxes}`],
        tone: "muted",
        priority: PRIORITY.ambient,
        anchor: project(o.gx + RACK_W, o.gy + 0.2, 3.1),
        fontSize: 11,
        hotspotId,
        fact: `${r.moreBoxes} more`,
      });
    }
  });

  const b = m.bottlenecks;
  if (b.waitingForInventory.length > 0) {
    const n = b.waitingForInventoryOrders;
    labels.push({
      id: "waiting-total",
      variants: [`Waiting for inventory: ${plural(n, "order")}`, `Waiting for inventory: ${n}`],
      tone: "crisis",
      priority: PRIORITY.bottleneck,
      anchor: project(11.4, 0.6, 4.6),
      fact: "Waiting for inventory",
    });
  }
  if (b.waitingForCapacityOrders > 0) {
    labels.push({
      id: "waiting-capacity",
      variants: [`Waiting for packing capacity: ${b.waitingForCapacityOrders}`, `Packing full: ${b.waitingForCapacityOrders}`],
      tone: "crisis",
      priority: PRIORITY.bottleneck,
      anchor: project(11, 10, 2.4),
      hotspotId: "packing",
      fact: `Waiting for packing capacity: ${plural(b.waitingForCapacityOrders, "order")}`,
    });
  }

  if (!m.dock.closed) {
    labels.push({
      id: "dock",
      variants: m.dock.openPOs > 0 ? [`Deliveries on the way: ${m.dock.openPOs}`, `${m.dock.openPOs} on the way`] : ["No deliveries on order"],
      tone: m.dock.openPOs > 0 ? "normal" : "muted",
      priority: PRIORITY.status,
      anchor: project(4.6, 1.6, 2.4),
      hotspotId: "dock",
      fact: m.dock.openPOs > 0 ? `${plural(m.dock.openPOs, "delivery", "deliveries")} on the way` : "no deliveries on order",
    });
  }

  labels.push({
    id: "orders",
    variants: [`Orders waiting: ${m.queue.length}`, `${m.queue.length} waiting`],
    tone: surge ? "crisis" : "normal",
    priority: surge ? PRIORITY.crisis : PRIORITY.status,
    anchor: project(19.2, 9.7, 3.3),
    hotspotId: "orders",
    fact: `Orders waiting: ${m.queue.length}`,
  });
  if (m.queue.more > 0) {
    labels.push({
      id: "orders-more",
      variants: [`+${m.queue.more} more orders`, `+${m.queue.more}`],
      tone: "muted",
      priority: PRIORITY.ambient,
      anchor: project(21.4, 9.7, 3.0),
      hotspotId: "orders",
      fact: `${m.queue.more} more`,
    });
  }

  labels.push({
    id: "outbound",
    variants:
      m.outbound.shippedOrders > 0
        ? [`Shipped this hour: ${m.outbound.shippedOrders} orders`, `Shipped: ${m.outbound.shippedOrders}`]
        : ["Nothing shipped this hour"],
    tone: "muted",
    priority: PRIORITY.ambient + 5,
    anchor: project(19.5, 1, 1.4),
    hotspotId: "outbound",
    fact: m.outbound.shippedOrders > 0 ? `${plural(m.outbound.shippedOrders, "order")} shipped` : "nothing shipped this hour",
  });
  if (m.outbound.hiddenVans > 0) {
    labels.push({
      id: "vans-more",
      variants: [`+${m.outbound.hiddenVans} more vans`, `+${m.outbound.hiddenVans}`],
      tone: "muted",
      priority: PRIORITY.ambient,
      anchor: project(20.5, 8.6, 1.4),
      hotspotId: "outbound",
      fact: plural(m.outbound.vans + m.outbound.hiddenVans, "van"),
    });
  }

  if (m.people.onboarding > 0) {
    labels.push({
      id: "training",
      variants: [`In training: ${m.people.onboarding}`, `${m.people.onboarding} training`],
      tone: "muted",
      priority: PRIORITY.status,
      anchor: project(2.2, 10.45, 1.6),
      hotspotId: "break",
      fact: `${m.people.onboarding} temporary`,
    });
  }
  const absent = m.people.absent ?? 0;
  if (absent > 0) {
    labels.push({
      id: "absent",
      variants: [`Absent: ${absent}`],
      tone: "crisis",
      priority: PRIORITY.crisis,
      anchor: project(2.2, 11.95, 1.6),
      hotspotId: "break",
      fact: `${absent} regular`,
    });
  }
  if (m.people.hiddenOnFloor > 0) {
    labels.push({
      id: "people-more",
      variants: [`+${m.people.hiddenOnFloor} more people`, `+${m.people.hiddenOnFloor}`],
      tone: "muted",
      priority: PRIORITY.ambient,
      anchor: project(15, 12.4, 1.2),
      hotspotId: "packing",
      fact: plural(m.people.onShift, "person", "people"),
    });
  }

  if (m.syncing) {
    const sp = project(11, 14, 0);
    labels.push({ id: "syncing", variants: ["Catching up…"], tone: "muted", priority: PRIORITY.crisis, anchor: { x: sp.x, y: sp.y + 28 }, fact: "" });
  }
  if (m.interventions.length > 0) {
    const full = `Your move takes effect: ${m.interventions.map((i) => i.text).join("; ")}`.slice(0, 90);
    labels.push({
      id: "moves",
      variants: [full, `${plural(m.interventions.length, "move")} take effect`],
      tone: "good",
      priority: PRIORITY.moves,
      anchor: project(11, 0.2, 3.8),
      fact: m.interventions[0]?.text ?? "",
    });
  }
  return { banners, labels };
}
