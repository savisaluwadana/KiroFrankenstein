// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createDemo, forkBranch, advanceBranch, endDuelWindow, getWorld, createDiff, ApiError } from "./client";
import { hasWriteToken, onWriteTokenRejected, WRITE_TOKEN_HEADER } from "./writeToken";

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

type Call = { method: string; url: string; headers: Record<string, string> };

function install(reply: (c: Call) => Response): Call[] {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const c = { method: init?.method ?? "GET", url, headers: { ...(init?.headers as Record<string, string>) } };
      calls.push(c);
      return reply(c);
    }),
  );
  return calls;
}

const demoBody = { world: { id: "w1" }, rootBranch: { id: "b1" }, plan: {}, writeToken: "T".repeat(43) };

beforeEach(() => window.sessionStorage.clear());
afterEach(() => vi.unstubAllGlobals());

describe("write token handling", () => {
  it("stores the token from a create response per id in sessionStorage, never localStorage or a URL", async () => {
    const calls = install(() => json(201, demoBody));
    await createDemo();
    expect(hasWriteToken("w1")).toBe(true);
    expect(hasWriteToken("b1")).toBe(true);
    expect(window.sessionStorage.getItem("parallel.writeToken.w1")).toBe(demoBody.writeToken);
    expect(JSON.stringify({ ...window.localStorage })).not.toContain(demoBody.writeToken);
    expect(calls.every((c) => !c.url.includes(demoBody.writeToken))).toBe(true);
  });

  it("attaches the token to mutations only, and the child of a fork inherits it", async () => {
    const calls = install((c) => {
      if (c.url.endsWith("/demo")) return json(201, demoBody);
      if (c.url.endsWith("/fork")) return json(201, { id: "b2" });
      return json(200, {});
    });
    await createDemo();
    await forkBranch("b1", { atTick: 0, label: "x" });
    await advanceBranch("b2");
    await getWorld("w1");
    await createDiff({ branchA: "b1", branchB: "b2", tick: 0 } as never);
    const by = (m: string, suffix: string) => calls.find((c) => c.method === m && c.url.endsWith(suffix));
    expect(by("POST", "/branches/b1/fork")?.headers[WRITE_TOKEN_HEADER]).toBe(demoBody.writeToken);
    expect(by("POST", "/branches/b2/advance")?.headers[WRITE_TOKEN_HEADER]).toBe(demoBody.writeToken);
    expect(by("GET", "/worlds/w1")?.headers[WRITE_TOKEN_HEADER]).toBeUndefined();
    expect(by("POST", "/diffs")?.headers[WRITE_TOKEN_HEADER]).toBeUndefined(); // a diff is a read
    expect(by("POST", "/demo")?.headers[WRITE_TOKEN_HEADER]).toBeUndefined();
  });

  it("sends no header for a world this tab did not create, and tells listeners when the server refuses", async () => {
    const calls = install(() =>
      json(401, { type: "urn:parallel:problem:write-token-required", title: "Write token required", status: 401 }),
    );
    const rejected = vi.fn();
    const off = onWriteTokenRejected(rejected);
    await expect(endDuelWindow("d-other", { expectedHeadTick: 0 } as never)).rejects.toBeInstanceOf(ApiError);
    off();
    expect(calls[0]?.headers[WRITE_TOKEN_HEADER]).toBeUndefined();
    expect(rejected).toHaveBeenCalledTimes(1);
  });
});
