"use client";

import { useMemo } from "react";
import { Background, Position, ReactFlow, type Edge, type Node } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import type { Branch } from "@/lib/api/client";
import { useStudioStore } from "@/stores/studio";
import { layoutBranches, managerLabel, NODE_HEIGHT, NODE_WIDTH, type LayoutNode } from "./layout";

export interface BranchTreeProps {
  branches: readonly Branch[];
  activeBranchId: string | null;
  onSelect: (branchId: string) => void;
}

function NodeLabel({ n, active }: { n: LayoutNode; active: boolean }) {
  const b = n.branch;
  return (
    <div className="text-left font-sans text-xs leading-tight" data-testid={`branch-node-${b.id}`}>
      <div className="truncate text-sm" style={{ color: n.style.color }}>
        {b.label}
        {active ? " (active)" : ""}
      </div>
      <div className="text-[#8A93A6]">{managerLabel(b.managerKind)}</div>
      <div className="font-mono tabular-nums text-[#E6E9EF]">
        fork tick {b.forkTick} · head tick {b.headTick}
      </div>
      <div className="text-[#8A93A6]">line: {n.style.patternName}</div>
    </div>
  );
}

/** Branch tree: one node per Branch, edges parent → child at the fork Tick. */
export function BranchTree({ branches, activeBranchId, onSelect }: BranchTreeProps) {
  const { nodes, edges } = useMemo(() => {
    const layout = layoutBranches(branches);
    const nodes: Node[] = layout.nodes.map((n) => ({
      id: n.branch.id,
      position: { x: n.x, y: n.y },
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
      draggable: false,
      data: { label: <NodeLabel n={n} active={n.branch.id === activeBranchId} /> },
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
  }, [branches, activeBranchId]);

  return (
    <div className="h-full w-full" aria-label="Branch tree" role="region">
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
  );
}

/** BranchTree bound to the studio store: selecting a node sets the active Branch. */
export function ConnectedBranchTree() {
  const branches = useStudioStore((s) => s.branches);
  const activeBranchId = useStudioStore((s) => s.activeBranchId);
  const setActiveBranch = useStudioStore((s) => s.setActiveBranch);
  return <BranchTree branches={branches} activeBranchId={activeBranchId} onSelect={setActiveBranch} />;
}
