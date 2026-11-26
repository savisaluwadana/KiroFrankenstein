import type { Branch, ManagerKind } from "@/lib/api/client";

export const NODE_WIDTH = 176;
export const NODE_HEIGHT = 76;
export const COLUMN_GAP = 64;
export const ROW_GAP = 24;

/** Fixed palette of 6 fork hues (ui-design-system). */
export const FORK_PALETTE = ["#B58CFF", "#7BD88F", "#FF8FB1", "#6FA8FF", "#E6D35C", "#FF9F6E"] as const;
const HUMAN_COLOR = "#F2A93B";
const AI_COLOR = "#3BC9DB";
/** Line patterns (SVG dash arrays); color is never the only encoding. */
const PATTERNS = ["", "8 4", "2 4", "10 4 2 4", "4 4", "14 4"] as const;

export interface BranchStyle {
  color: string;
  /** SVG stroke-dasharray; "" means solid. */
  dash: string;
  patternName: string;
}

const PATTERN_NAMES = ["solid", "dashed", "dotted", "dash-dot", "short-dash", "long-dash"] as const;

export function managerLabel(kind: ManagerKind): string {
  switch (kind) {
    case "human":
      return "Human";
    case "ai_rule_based":
      return "Rule-based AI";
    case "ai_llm":
      return "LLM AI";
  }
}

export interface LayoutNode {
  branch: Branch;
  depth: number;
  x: number;
  y: number;
  style: BranchStyle;
}

export interface LayoutEdge {
  id: string;
  source: string;
  target: string;
  forkTick: number;
  style: BranchStyle;
}

export interface BranchLayout {
  nodes: LayoutNode[];
  edges: LayoutEdge[];
}

function compareChildren(a: Branch, b: Branch): number {
  if (a.forkTick !== b.forkTick) return a.forkTick - b.forkTick;
  if (a.createdAt !== b.createdAt) return a.createdAt < b.createdAt ? -1 : 1;
  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
}

/**
 * Deterministic tree layout: depth is the column, leaves take successive rows
 * (depth-first, children ordered by fork Tick then creation time then id), and a
 * parent sits at the mean row of its children. The input order does not matter.
 * Style: Human amber, AI cyan, other managers/forks from the fixed palette; the
 * dash pattern follows the order in the layout, so it is unique per branch up to 6.
 */
export function layoutBranches(branches: readonly Branch[]): BranchLayout {
  const ids = new Set(branches.map((b) => b.id));
  const children = new Map<string, Branch[]>();
  const roots: Branch[] = [];
  for (const b of branches) {
    if (b.parentBranchId !== null && ids.has(b.parentBranchId)) {
      const list = children.get(b.parentBranchId) ?? [];
      list.push(b);
      children.set(b.parentBranchId, list);
    } else {
      roots.push(b);
    }
  }
  roots.sort(compareChildren);
  for (const list of children.values()) list.sort(compareChildren);

  const nodes: LayoutNode[] = [];
  const edges: LayoutEdge[] = [];
  let nextRow = 0;
  let paletteIndex = 0;
  let order = 0;

  const styleFor = (b: Branch): BranchStyle => {
    const i = order++;
    const dashIdx = i % PATTERNS.length;
    let color: string;
    if (b.managerKind === "human" && b.parentBranchId === null) color = HUMAN_COLOR;
    else if (b.managerKind === "ai_rule_based" || b.managerKind === "ai_llm") color = AI_COLOR;
    else if (b.managerKind === "human") color = HUMAN_COLOR;
    else color = FORK_PALETTE[paletteIndex++ % FORK_PALETTE.length] ?? HUMAN_COLOR;
    return { color, dash: PATTERNS[dashIdx] ?? "", patternName: PATTERN_NAMES[dashIdx] ?? "solid" };
  };

  const visit = (b: Branch, depth: number, seen: Set<string>): { row: number; node: LayoutNode } => {
    seen.add(b.id);
    const node: LayoutNode = { branch: b, depth, x: 0, y: 0, style: styleFor(b) };
    nodes.push(node);
    const kids = (children.get(b.id) ?? []).filter((k) => !seen.has(k.id));
    let row: number;
    if (kids.length === 0) {
      row = nextRow++;
    } else {
      const rows: number[] = [];
      for (const k of kids) {
        const r = visit(k, depth + 1, seen);
        rows.push(r.row);
        edges.push({ id: `${b.id}->${k.id}`, source: b.id, target: k.id, forkTick: k.forkTick, style: r.node.style });
      }
      row = ((rows[0] ?? 0) + (rows[rows.length - 1] ?? 0)) / 2;
    }
    node.x = depth * (NODE_WIDTH + COLUMN_GAP);
    node.y = row * (NODE_HEIGHT + ROW_GAP);
    return { row, node };
  };

  const seen = new Set<string>();
  for (const r of roots) visit(r, 0, seen);
  return { nodes, edges };
}
