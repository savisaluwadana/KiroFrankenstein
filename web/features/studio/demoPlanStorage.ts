import type { DemoPlan } from "@/lib/api/client";

const key = (worldId: string): string => `parallel.demoPlan.${worldId}`;

/** Keeps the server-computed demo plan for the tab session so the world page can show hints. */
export function saveDemoPlan(worldId: string, plan: DemoPlan): void {
  try {
    window.sessionStorage.setItem(key(worldId), JSON.stringify(plan));
  } catch {
    // Storage may be unavailable; the plan hint is optional.
  }
}

function isPlan(v: unknown): v is DemoPlan {
  if (typeof v !== "object" || v === null) return false;
  const p = v as Partial<DemoPlan>;
  return typeof p.forkTick === "number" && Array.isArray(p.commands) && Array.isArray(p.reasons);
}

export function loadDemoPlan(worldId: string): DemoPlan | null {
  try {
    const raw = window.sessionStorage.getItem(key(worldId));
    if (raw === null) return null;
    const parsed: unknown = JSON.parse(raw);
    return isPlan(parsed) ? parsed : null;
  } catch {
    return null;
  }
}
