/**
 * Grid layout and display constants of the warehouse scene (docs/ux-redesign.md
 * section 6.2 and 6.3). Pure; shared by the scene model, hotspots and drawing.
 */
import { footprintBounds, type PxRect } from "./iso";

export interface CellRect {
  gx: number;
  gy: number;
  w: number;
  h: number;
}

export const ZONES = {
  inbound: { gx: 0, gy: 2, w: 5, h: 8 },
  storage: { gx: 6, gy: 1, w: 10, h: 8 },
  packing: { gx: 6, gy: 9, w: 10, h: 5 },
  orderLine: { gx: 16, gy: 9, w: 6, h: 5 },
  outbound: { gx: 16, gy: 1, w: 6, h: 8 },
  breakArea: { gx: 0, gy: 10, w: 5, h: 4 },
} as const satisfies Record<string, CellRect>;
export type ZoneName = keyof typeof ZONES;

/** One displayed box stands for this many units (the floor sign prints exact units). */
export const UNITS_PER_BOX = 20;
export const RACK_SLOTS = 54;
export const MAX_PRODUCTS = 6;
export const STATIONS = 4;
export const MAX_FIGURES = 40;
export const MAX_TICKETS = 40;
export const MAX_NEW_TICKETS = 8;
export const MAX_VANS = 6;
export const MAX_TRUCKS = 3;
export const ORDERS_PER_VAN = 10;
export const MAX_TRAINING_DRAWN = 8;
export const MAX_ABSENT_DRAWN = 8;
export const MAX_MOVING_BOXES = 6;

export const RACK_W = 3.8;
export const RACK_D = 1.2;
export const BOX_W = 0.36;
export const BOX_D = 0.42;
export const BOX_H_PX = 20;

/** Back-left corner of the rack for product k of n. */
export function rackOrigin(k: number, n: number): { gx: number; gy: number } {
  const rows = Math.max(1, Math.ceil(n / 2));
  const spacing = 7 / rows;
  return { gx: 6.6 + (k % 2) * 4.8, gy: 1.3 + Math.floor(k / 2) * spacing };
}

/** Slot i (0 = bottom, back, left) relative to the rack origin. Fill is bottom-up, back-to-front. */
export function slotOffset(i: number): { dx: number; dy: number; level: number } {
  const level = Math.floor(i / 18);
  const j = i % 18;
  const col = j % 9;
  const row = Math.floor(j / 9);
  return { dx: 0.1 + col * 0.4, dy: 0.1 + row * 0.5, level };
}

/** Boxes drawn for an on-hand quantity: ceil(onHand / 20), at most 54. */
export function boxesFor(onHand: number): { boxes: number; moreBoxes: number } {
  const total = onHand <= 0 ? 0 : Math.ceil(onHand / UNITS_PER_BOX);
  return { boxes: Math.min(RACK_SLOTS, total), moreBoxes: Math.max(0, total - RACK_SLOTS) };
}

export const BENCH_W = 2.0;
export const BENCH_D = 0.9;
export function benchOrigin(k: number): { gx: number; gy: number } {
  return { gx: 6.6 + k * 2.4, gy: 10.0 };
}

/** Standing spot j (0..9) of a figure at station k. */
export function figureSpot(k: number, j: number): { gx: number; gy: number } {
  const b = benchOrigin(k);
  return { gx: b.gx + 0.2 + (j % 5) * 0.4, gy: 11.3 + Math.floor(j / 5) * 0.7 };
}

/** Station occupancy: blocks of ceil(n / STATIONS) filled in order. */
export function stationFill(n: number): number[] {
  const block = n <= 0 ? 0 : Math.ceil(n / STATIONS);
  return Array.from({ length: STATIONS }, (_, k) => Math.min(block, Math.max(0, n - k * block)));
}

/** Break area spot m (0..7): training figures and absent ghosts share a 4 x 2 grid each. */
export function breakSpot(row: 0 | 1, m: number): { gx: number; gy: number } {
  return { gx: 0.6 + (m % 4) * 1.0, gy: 10.9 + row * 1.5 + Math.floor(m / 4) * 0.6 };
}

/** Truck bay k (0..2): top-left cell of the parked truck on the inbound lane. */
export const TRUCK_LANE_GX = 2.2;
export const TRUCK_W = 1.3;
export const TRUCK_LEN = 2.6;
export function truckBay(k: number): { gx: number; gy: number } {
  return { gx: TRUCK_LANE_GX, gy: 2.0 + k * 2.9 };
}
export const TRUCK_START_GY = 16;
export const DOCK_PLATE: CellRect = { gx: 3.5, gy: 2, w: 2.0, h: 8 };

export const VAN_W = 1.7;
export const VAN_D = 0.95;
export function vanBay(k: number): { gx: number; gy: number } {
  return { gx: 17.2, gy: 1.4 + k * 1.2 };
}
export const VAN_EXIT_GX = 26;

export const CONVEYOR_GY = 9.15;
export const CONVEYOR: CellRect = { gx: 6, gy: CONVEYOR_GY, w: 10.4, h: 0.5 };

export const ORDER_RAIL_GY = 9.7;
export const ORDER_RAIL_GX = 16.6;
export const TICKET_COLS = 10;
export function ticketSlot(i: number): { gx: number; gz: number } {
  return { gx: ORDER_RAIL_GX + 0.3 + (i % TICKET_COLS) * 0.46, gz: 0.4 + Math.floor(i / TICKET_COLS) * 0.62 };
}

export interface Hotspot {
  id: string;
  kind: "rack" | "packing" | "dock" | "orders" | "outbound" | "break";
  /** Scene-pixel rectangle (SCENE_W x SCENE_H space). */
  rect: PxRect;
}

export function rackHotspotRect(k: number, n: number): PxRect {
  const o = rackOrigin(k, n);
  return footprintBounds(o.gx - 0.1, o.gy - 0.1, RACK_W + 0.2, RACK_D + 1.0, 3.2);
}
export function zoneHotspotRect(c: CellRect, height: number): PxRect {
  return footprintBounds(c.gx, c.gy, c.w, c.h, height);
}
