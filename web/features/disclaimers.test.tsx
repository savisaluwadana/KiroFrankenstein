// @vitest-environment jsdom
/**
 * Every major results or entry view carries the "synthetic" disclaimer
 * (DISCLAIMER_FULL, or DISCLAIMER_SHORT on the intro). Studio, duel, ledger and
 * assumptions are asserted in their own tests; this covers the rest, including
 * the diff panel while it is still loading or has failed.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import type { Branch } from "@/lib/api/client";
import { DISCLAIMER_FULL, DISCLAIMER_SHORT } from "@/lib/glossary";

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));
vi.mock("next/link", () => ({
  default: ({ href, children }: { href: string; children: ReactNode }) => <a href={href}>{children}</a>,
}));

import { BriefingPage } from "./briefing/BriefingPage";
import { DiffPanel } from "./diff/DiffPanel";
import { IntroPage } from "./intro/IntroPage";
import { SetupPage } from "./setup/SetupPage";

const branch = (id: string): Branch => ({
  id,
  worldId: "w",
  parentBranchId: null,
  forkTick: 0,
  headTick: 10,
  label: id,
  managerKind: "human",
  createdAt: "2026-01-01T00:00:00Z",
});

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify({ title: "nope", status: 500 }), { status: 500, headers: { "Content-Type": "application/problem+json" } })),
  );
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("synthetic-results disclaimer", () => {
  it("is on the intro (short form)", () => {
    render(<IntroPage />);
    expect(document.body.textContent).toContain(DISCLAIMER_SHORT);
  });

  it("is on the briefing", () => {
    render(<BriefingPage />);
    expect(screen.getByTestId("disclaimer").textContent).toBe(DISCLAIMER_FULL);
  });

  it("is on the business setup page", () => {
    render(<SetupPage />);
    expect(document.body.textContent).toContain(DISCLAIMER_FULL);
  });

  it("is on the diff panel while loading and after a failure", async () => {
    render(<DiffPanel branchA={branch("a")} branchB={branch("b")} branches={[branch("a"), branch("b")]} startClock="Thu 00:00" productNames={[]} engineering={false} onSelectTick={() => {}} />);
    expect(screen.getByTestId("diff-disclaimer").textContent).toBe(DISCLAIMER_FULL);
    await waitFor(() => expect(screen.getByRole("alert")).toBeTruthy());
    expect(screen.getByTestId("diff-disclaimer").textContent).toBe(DISCLAIMER_FULL);
  });
});
