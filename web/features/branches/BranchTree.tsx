"use client";

import { useMemo, useState } from "react";
import { Background, Position, ReactFlow, type Edge, type Node } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import type { Branch } from "@/lib/api/client";
import { useStudioStore } from "@/stores/studio";
import { layoutBranches, managerLabel, NODE_HEIGHT, NODE_WIDTH, type LayoutNode } from "./layout";
import { branchDisplayName, branchProvenanceBadge, type BranchLabelContext } from "./labels";

export interface BranchTreeProps {
  branches: readonly Branch[];
  activeBranchId: string | null;
  onSelect: (branchId: string) => void;
  /** When given, two futures can be ticked and compared. Receives the ids in lane order. */
  onCompare?: (branchA: string, branchB: string) => void;
  startClock?: string;
  /** Model identifier of the LLM AI, when the server reports one. */
  llmModel?: string | undefined;
}

function NodeLabel(props: {
  n: LayoutNode;
  active: boolean;
  name: string;
  badge: string | null;
  selectable: boolean;
  checked: boolean;
  onToggle: () => void;
}) {
  const { n, active } = props;
  const b = n.branch;
  return (
    <div className="text-left font-sans text-xs leading-tight" data-testid={`branch-node-${b.id}`}>
      <div className="flex items-center gap-1">
        {props.selectable ? (
          <input
            type="checkbox"
            className="nodrag nopan h-4 w-4 shrink-0"
            aria-label={`Select ${props.name} to compare`}
            checked={props.checked}
            onChange={props.onToggle}
            onClick={(e) => e.stopPropagation()}
          />
        ) : null}
        <div className="min-w-0 flex-1 truncate text-sm" style={{ color: n.style.color }} title={props.name}>
          {props.name}
          {active ? " (active)" : ""}
        </div>
      </div>
      <div className="text-[#8A93A6]" data-testid={`branch-kind-${b.id}`}>
        {props.badge !== null ? <span data-testid="provenance-badge">{props.badge}</span> : managerLabel(b.managerKind)}
      </div>
      <div className="font-mono tabular-nums text-[#E6E9EF]">
        fork tick {b.forkTick} · head tick {b.headTick}
      </div>
      <div className="text-[#8A93A6]">line: {n.style.patternName}</div>
    </div>
  );
}

/** Creation order of a branch (original path first), used to put the two compared futures in lane order. */
function laneRank(branches: readonly Branch[], id: string): number {
  const b = branches.find((x) => x.id === id);
  if (b === undefined) return 0;
  return b.parentBranchId === null ? -1 : Date.parse(b.createdAt) || 0;
}

/** Branch tree: one node per Branch, edges parent → child at the fork Tick. */
export function BranchTree({ branches, activeBranchId, onSelect, onCompare, startClock = "Thu 00:00", llmModel }: BranchTreeProps) {
  const [picked, setPicked] = useState<string[]>([]);
  const selectable = onCompare !== undefined;
  const ctx: BranchLabelContext = useMemo(() => ({ branches, startClock, llmModel }), [branches, startClock, llmModel]);
  const toggle = (id: string): void =>
    setPicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : p.length >= 2 ? [p[1] as string, id] : [...p, id]));
  const knownKey = picked.filter((id) => branches.some((b) => b.id === id)).join("|");
  const known = knownKey === "" ? [] : knownKey.split("|");
  const { nodes, edges } = useMemo(() => {
    const layout = layoutBranches(branches);
    const nodes: Node[] = layout.nodes.map((n) => ({
      id: n.branch.id,
      position: { x: n.x, y: n.y },
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
      draggable: false,
      data: {
        label: (
          <NodeLabel
            n={n}
            active={n.branch.id === activeBranchId}
            name={branchDisplayName(n.branch, ctx)}
            badge={branchProvenanceBadge(n.branch, llmModel)}
            selectable={selectable}
            checked={known.includes(n.branch.id)}
            onToggle={() => toggle(n.branch.id)}
          />
        ),
      },
      style: {
        width: NODE_WIDTH,
        height: NODE_HEIGHT,
        background: "#121722",
        color: "#E6E9EF",
        borderRadius: 4,
        border: `${n.branch.id === activeBranchId ? 2 : 1}px ${n.style.dash === "" ? "solid" : "dashed"} ${n.style.color}`,
        padding: 6,
      },
    }));
    const edges: Edge[] = layout.edges.map((e) => ({
      id: e.id,
      source: e.source,
      target: e.target,
      label: `tick ${e.forkTick}`,
      labelStyle: { fill: "#8A93A6", fontFamily: "IBM Plex Mono, monospace", fontSize: 10 },
      style: { stroke: e.style.color, strokeWidth: 2, strokeDasharray: e.style.dash },
    }));
    return { nodes, edges };
    // `toggle` only calls the state setter.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [branches, activeBranchId, ctx, knownKey, selectable, llmModel]);

  const compare = (): void => {
    const [x, y] = known;
    if (onCompare === undefined || x === undefined || y === undefined) return;
    const ordered = [x, y].sort((p, q) => laneRank(branches, p) - laneRank(branches, q));
    onCompare(ordered[0] as string, ordered[1] as string);
  };
  const nameOf = (id: string): string => {
    const b = branches.find((x) => x.id === id);
    return b === undefined ? "" : branchDisplayName(b, ctx);
  };

  return (
    <div className="flex h-full w-full flex-col" aria-label="Branch tree" role="region">
      {selectable ? (
        <div className="flex flex-wrap items-center gap-2 border-b border-[#232B3A] p-1 text-xs text-[#8A93A6]" data-testid="compare-picker">
          <span>
            Compare any two futures:{" "}
            {known.length === 0 ? "tick two below." : known.map(nameOf).join(" and ")}
          </span>
          <button
            type="button"
            disabled={known.length !== 2}
            onClick={compare}
            className="min-h-11 rounded-[4px] border border-[#E8A33D] px-3 text-[#E8A33D] disabled:opacity-50"
          >
            Compare these two
          </button>
        </div>
      ) : null}
      <div className="min-h-0 flex-1">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodesDraggable={false}
        nodesConnectable={false}
        fitView
        proOptions={{ hideAttribution: true }}
        onNodeClick={(_, node) => onSelect(node.id)}
      >
        <Background color="#232B3A" />
      </ReactFlow>
      </div>
    </div>
  );
}

/** BranchTree bound to the studio store: selecting a node sets the active Branch. */
export function ConnectedBranchTree() {
  const branches = useStudioStore((s) => s.branches);
  const activeBranchId = useStudioStore((s) => s.activeBranchId);
  const setActiveBranch = useStudioStore((s) => s.setActiveBranch);
  return <BranchTree branches={branches} activeBranchId={activeBranchId} onSelect={setActiveBranch} />;
}
