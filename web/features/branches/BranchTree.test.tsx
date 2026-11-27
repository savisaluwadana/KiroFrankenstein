// @vitest-environment jsdom
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Branch } from "@/lib/api/client";

// React Flow needs layout measurements that jsdom lacks; render each node's label instead.
vi.mock("@xyflow/react", () => ({
  ReactFlow: (p: { nodes: { id: string; data: { label: ReactNode } }[]; onNodeClick?: (e: unknown, n: { id: string }) => void; children?: ReactNode }) => (
    <div>
      {p.nodes.map((n) => (
        <div key={n.id} onClick={() => p.onNodeClick?.({}, n)}>
          {n.data.label}
        </div>
      ))}
    </div>
  ),
  Background: () => null,
  Position: { Left: "left", Right: "right" },
}));
vi.mock("@xyflow/react/dist/style.css", () => ({}));

const diffMock = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api/client", async (orig) => {
  const actual = await orig<typeof import("@/lib/api/client")>();
  return { ...actual, createDiff: diffMock };
});

import { createDiff } from "@/lib/api/client";
import { BranchTree } from "./BranchTree";

function br(id: string, parent: string | null, over: Partial<Branch> = {}): Branch {
  return { id, worldId: "w", parentBranchId: parent, forkTick: 20, headTick: 30, label: id, managerKind: "human", createdAt: `2026-01-01T00:00:0${id.length}Z`, ...over };
}
const root = br("r", null);
const alt = br("alt", "r", { label: "Backup supplier" });
const ai = br("ai", "r", { label: "AI", managerKind: "ai_rule_based" });

afterEach(() => {
  cleanup();
  diffMock.mockReset();
});

describe("BranchTree", () => {
  it("shows plain names and the AI provenance badge", () => {
    render(<BranchTree branches={[root, alt, ai]} activeBranchId="r" onSelect={() => {}} />);
    expect(screen.getByTestId("branch-node-r").textContent).toContain("Original path");
    expect(screen.getByTestId("branch-node-alt").textContent).toContain("Backup supplier (from Thursday 8:00 PM)");
    expect(screen.getByTestId("branch-node-ai").textContent).toContain("Rule-based AI (from Thursday 8:00 PM)");
    expect(screen.getByTestId("branch-node-ai").querySelector('[data-testid="provenance-badge"]')?.textContent).toBe("Rule-based AI");
    expect(screen.getByTestId("branch-node-alt").querySelector('[data-testid="provenance-badge"]')).toBeNull();
  });

  it("offers no selection without a compare handler", () => {
    render(<BranchTree branches={[root, alt]} activeBranchId="r" onSelect={() => {}} />);
    expect(screen.queryByRole("button", { name: "Compare these two" })).toBeNull();
    expect(screen.queryAllByRole("checkbox")).toHaveLength(0);
  });

  it("compares exactly two ticked futures, original path first, and does not change the active branch", async () => {
    const onCompare = vi.fn((a: string, b: string) => {
      void createDiff({ branchA: a, branchB: b });
    });
    const onSelect = vi.fn();
    render(<BranchTree branches={[root, alt, ai]} activeBranchId="r" onSelect={onSelect} onCompare={onCompare} />);
    const button = screen.getByRole("button", { name: "Compare these two" });
    expect((button as HTMLButtonElement).disabled).toBe(true);
    await userEvent.click(screen.getByRole("checkbox", { name: "Select Backup supplier (from Thursday 8:00 PM) to compare" }));
    expect((button as HTMLButtonElement).disabled).toBe(true);
    await userEvent.click(screen.getByRole("checkbox", { name: "Select Original path to compare" }));
    expect((button as HTMLButtonElement).disabled).toBe(false);
    await userEvent.click(button);
    expect(onCompare).toHaveBeenCalledExactlyOnceWith("r", "alt");
    expect(diffMock).toHaveBeenCalledExactlyOnceWith({ branchA: "r", branchB: "alt" });
    expect(onSelect).not.toHaveBeenCalled();
  });
});
