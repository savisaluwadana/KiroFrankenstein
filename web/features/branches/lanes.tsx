/**
 * Reality lanes: each reality has a name, a colour and a line pattern, so the
 * colour is never the only cue. The first lane is the original path (ivory,
 * solid); alternatives follow in a fixed order (amber dashed first).
 */
import type { Branch } from "@/lib/api/client";
import { TERMS } from "@/lib/glossary";

export interface Lane {
  color: string;
  /** SVG stroke-dasharray; empty for a solid line. */
  dash: string;
  patternName: "solid" | "dashed" | "dotted" | "dash-dot";
}

const LANES: readonly Lane[] = [
  { color: "#F3EDE0", dash: "", patternName: "solid" },
  { color: "#E8A33D", dash: "8 5", patternName: "dashed" },
  { color: "#9CC48A", dash: "2 5", patternName: "dotted" },
  { color: "#EE7B63", dash: "10 4 2 4", patternName: "dash-dot" },
];

/** Lane for the reality at `index` in `orderBranches` order; cycles after four. */
export function laneFor(index: number): Lane {
  return LANES[Math.max(0, index) % LANES.length] as Lane;
}

/** Original path first, then alternatives in creation order. */
export function orderBranches(branches: readonly Branch[]): Branch[] {
  return [...branches].sort((a, b) =>
    a.parentBranchId === null ? -1 : b.parentBranchId === null ? 1 : a.createdAt.localeCompare(b.createdAt),
  );
}

export function realityName(b: Branch): string {
  return b.parentBranchId === null ? TERMS.originalPath : b.label;
}

export function laneIndexOf(branches: readonly Branch[], id: string): number {
  return Math.max(
    0,
    orderBranches(branches).findIndex((b) => b.id === id),
  );
}

/** A short line sample in the lane's colour and pattern. */
export function LaneMark({ lane, className }: { lane: Lane; className?: string }) {
  return (
    <svg aria-hidden="true" width="28" height="8" viewBox="0 0 28 8" className={className} data-pattern={lane.patternName}>
      <line x1="1" y1="4" x2="27" y2="4" stroke={lane.color} strokeWidth="3" strokeDasharray={lane.dash === "" ? undefined : lane.dash} />
    </svg>
  );
}

/** Small persistent label saying which reality the canvas shows. */
export function RealityBadge({ name, lane }: { name: string; lane: Lane }) {
  return (
    <span
      data-testid="reality-badge"
      className="pointer-events-none inline-flex items-center gap-2 rounded-[4px] border border-hairline bg-ink/75 px-2 py-1 text-xs text-ivory"
    >
      <LaneMark lane={lane} />
      <span>Showing: {name}</span>
    </span>
  );
}
