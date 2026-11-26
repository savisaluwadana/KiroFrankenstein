// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Branch } from "@/lib/api/client";

const forkMock = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api/client", async (orig) => {
  const actual = await orig<typeof import("@/lib/api/client")>();
  return { ...actual, forkBranch: forkMock };
});

import { ApiError } from "@/lib/api/client";
import { ForkButton } from "./ForkButton";
import { layoutBranches } from "./layout";
import { createStudioStore } from "@/stores/studio";

function br(id: string, parent: string | null, forkTick: number, kind: Branch["managerKind"] = "human"): Branch {
  return {
    id,
    worldId: "w",
    parentBranchId: parent,
    forkTick,
    headTick: forkTick + 10,
    label: id,
    managerKind: kind,
    createdAt: `2026-01-01T00:00:0${forkTick % 10}Z`,
  };
}

afterEach(() => {
  cleanup();
  forkMock.mockReset();
});

describe("layoutBranches", () => {
  const list = [br("c", "a", 30, "ai_rule_based"), br("a", null, 0), br("b", "a", 20), br("d", "b", 40)];

  it("is deterministic regardless of input order", () => {
    expect(layoutBranches([...list].reverse())).toEqual(layoutBranches(list));
  });

  it("emits an edge per child at its fork tick with distinct patterns", () => {
    const l = layoutBranches(list);
    expect(l.edges.map((e) => [e.source, e.target, e.forkTick]).sort()).toEqual(
      [["a", "b", 20], ["a", "c", 30], ["b", "d", 40]].sort(),
    );
    expect(new Set(l.nodes.map((n) => n.style.dash)).size).toBe(4);
  });
});

describe("ForkButton", () => {
  it("sends exactly one fork request for the displayed branch and tick and reports the response", async () => {
    const child = br("child", "a", 5);
    forkMock.mockResolvedValue(child);
    const onForked = vi.fn();
    const store = createStudioStore();
    store.getState().openWorld({ worldId: "w", horizonTicks: 120, branches: [br("a", null, 0)], activeBranchId: "a" });
    render(<ForkButton defaultLabel="Alternative future 1" whenLabel="Thursday, 5:00 AM" branchId="a" tick={5} headTick={10} onForked={(b) => { store.getState().upsertBranch(b); onForked(b); }} />);
    await userEvent.type(screen.getByLabelText(/name/i), "Plan B");
    await userEvent.click(screen.getByRole("button"));
    await waitFor(() => expect(onForked).toHaveBeenCalledWith(child));
    expect(forkMock).toHaveBeenCalledTimes(1);
    expect(forkMock).toHaveBeenCalledWith("a", { atTick: 5, label: "Plan B" });
    expect(store.getState().branches.map((b) => b.id)).toContain("child");
  });

  it("limits the label to 64 characters and disables beyond head", async () => {
    render(<ForkButton defaultLabel="Alternative future 1" whenLabel="Thursday, 5:00 AM" branchId="a" tick={11} headTick={10} onForked={() => undefined} />);
    expect((screen.getByRole("button") as HTMLButtonElement).disabled).toBe(true);
    cleanup();
    render(<ForkButton defaultLabel="Alternative future 1" whenLabel="Thursday, 5:00 AM" branchId="a" tick={1} headTick={10} onForked={() => undefined} />);
    await userEvent.type(screen.getByLabelText(/name/i), "x".repeat(80));
    expect((screen.getByLabelText(/name/i) as HTMLInputElement).value).toHaveLength(64);
  });

  it("shows the ApiError message and re-enables", async () => {
    forkMock.mockRejectedValue(new ApiError({ status: 409, type: "unknown", title: "Conflict", detail: "too many branches" }));
    render(<ForkButton defaultLabel="Alternative future 1" whenLabel="Thursday, 5:00 AM" branchId="a" tick={1} headTick={10} onForked={() => undefined} />);
    await userEvent.click(screen.getByRole("button"));
    expect((await screen.findByRole("alert")).textContent).toBe("Conflict: too many branches");
    expect((screen.getByRole("button") as HTMLButtonElement).disabled).toBe(false);
  });
});
