import { describe, expect, it } from "vitest";
import type { Branch } from "@/lib/api/client";
import { branchDisplayName, branchProvenanceBadge, sanitizeBranchName } from "./labels";

function br(id: string, parent: string | null, over: Partial<Branch> = {}): Branch {
  return {
    id,
    worldId: "w",
    parentBranchId: parent,
    forkTick: 20,
    headTick: 30,
    label: id,
    managerKind: "human",
    createdAt: `2026-01-01T00:00:0${id.length}Z`,
    ...over,
  };
}

const root = br("root", null);
const startClock = "Thu 00:00";

describe("branchDisplayName", () => {
  it("names the root the original path", () => {
    expect(branchDisplayName(root, { branches: [root], startClock })).toBe("Original path");
  });

  it("adds the fork time to a user-named alternative", () => {
    const b = br("b", "root", { label: "Backup supplier" });
    expect(branchDisplayName(b, { branches: [root, b], startClock })).toBe("Backup supplier (from Thursday 8:00 PM)");
  });

  it("numbers an alternative that has no name", () => {
    const a = br("a", "root", { label: "", createdAt: "2026-01-01T00:00:01Z" });
    const b = br("bb", "root", { label: "  ", createdAt: "2026-01-01T00:00:02Z" });
    const ctx = { branches: [root, a, b], startClock };
    expect(branchDisplayName(a, ctx)).toBe("Alternative future 1 (from Thursday 8:00 PM)");
    expect(branchDisplayName(b, ctx)).toBe("Alternative future 2 (from Thursday 8:00 PM)");
  });

  it("keeps You, and names the AI kinds with the fork time", () => {
    const you = br("you", "root", { label: "You" });
    const rb = br("rb", "root", { label: "anything", managerKind: "ai_rule_based" });
    const llm = br("llm", "root", { label: "x", managerKind: "ai_llm" });
    const ctx = { branches: [root, you, rb, llm], startClock, llmModel: "acme-1" };
    expect(branchDisplayName(you, ctx)).toBe("You");
    expect(branchDisplayName(rb, ctx)).toBe("Rule-based AI (from Thursday 8:00 PM)");
    expect(branchDisplayName(llm, ctx)).toBe("LLM AI (acme-1) (from Thursday 8:00 PM)");
    expect(branchDisplayName(llm, { branches: [root, llm], startClock })).toBe("LLM AI (from Thursday 8:00 PM)");
  });

  it("limits user names to 64 characters and strips control characters", () => {
    const long = br("l", "root", { label: "x".repeat(100) });
    expect(sanitizeBranchName("x".repeat(100))).toHaveLength(64);
    expect(branchDisplayName(long, { branches: [root, long], startClock }).startsWith("x".repeat(64) + " (from")).toBe(true);
    expect(sanitizeBranchName("a\u0000b\n\tc")).toBe("a b c");
  });

  it("leaves markup as plain text for the renderer to escape", () => {
    const b = br("m", "root", { label: "<img src=x onerror=alert(1)>" });
    expect(branchDisplayName(b, { branches: [root, b], startClock })).toContain("<img src=x onerror=alert(1)>");
  });
});

describe("branchProvenanceBadge", () => {
  it("shows a badge for AI branches only", () => {
    expect(branchProvenanceBadge(br("h", "root"))).toBeNull();
    expect(branchProvenanceBadge(br("r", "root", { managerKind: "ai_rule_based" }))).toBe("Rule-based AI");
    expect(branchProvenanceBadge(br("l", "root", { managerKind: "ai_llm" }), "acme-1")).toBe("LLM AI (acme-1)");
  });
});
