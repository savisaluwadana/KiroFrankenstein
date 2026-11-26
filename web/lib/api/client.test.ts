import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  advanceBranch,
  endDecisionWindow,
  getHealth,
  listLedgerEntries,
  listTickFrames,
  submitCommands,
} from "./client";

const BASE = "http://localhost:8080/api/v1";

function jsonResponse(body: unknown, status = 200, contentType = "application/json"): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": contentType } });
}

let fetchMock: ReturnType<typeof vi.fn<typeof fetch>>;

beforeEach(() => {
  delete process.env.NEXT_PUBLIC_API_BASE_URL;
  fetchMock = vi.fn<typeof fetch>();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function lastCall(): { url: string; init: RequestInit } {
  const call = fetchMock.mock.calls.at(-1);
  if (!call) throw new Error("fetch not called");
  return { url: String(call[0]), init: call[1] ?? {} };
}

describe("api client", () => {
  it("returns the server value on success", async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ status: "ok", engineVersion: "1" }));
    await expect(getHealth()).resolves.toEqual({ status: "ok", engineVersion: "1" });
    expect(lastCall().url).toBe(`${BASE}/health`);
    expect(lastCall().init.method).toBe("GET");
  });

  it("parses problem+json into ApiError", async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(
        {
          type: "urn:parallel:problem:stale-head",
          title: "Stale head",
          status: 409,
          detail: "head moved",
          issues: [{ field: "expectedHeadTick", message: "mismatch" }],
          expectedHeadTick: 4,
          actualHeadTick: 8,
        },
        409,
        "application/problem+json",
      ),
    );
    const err = await advanceBranch("b1", { expectedHeadTick: 4 }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    const e = err as ApiError;
    expect(e.status).toBe(409);
    expect(e.type).toBe("stale-head");
    expect(e.title).toBe("Stale head");
    expect(e.detail).toBe("head moved");
    expect(e.issues).toEqual([{ field: "expectedHeadTick", message: "mismatch" }]);
    expect(e.actualHeadTick).toBe(8);
  });

  it("falls back to an unknown ApiError for non-problem bodies", async () => {
    fetchMock.mockResolvedValueOnce(new Response("bad gateway", { status: 502, statusText: "Bad Gateway" }));
    const e = (await getHealth().catch((x: unknown) => x)) as ApiError;
    expect(e).toBeInstanceOf(ApiError);
    expect(e.status).toBe(502);
    expect(e.type).toBe("unknown");
  });

  it("sets the Idempotency-Key header and JSON body", async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ accepted: [], rejected: [], budgetRemaining: 5, duplicate: false }),
    );
    await submitCommands("b1", { windowTick: 4, commands: [{ kind: "set_price", sku: 0, amountCents: 100 }] }, "key-1");
    const { url, init } = lastCall();
    expect(url).toBe(`${BASE}/branches/b1/commands`);
    expect(init.method).toBe("POST");
    const headers = init.headers as Record<string, string>;
    expect(headers["Idempotency-Key"]).toBe("key-1");
    expect(headers["Content-Type"]).toBe("application/json");
    expect(JSON.parse(String(init.body))).toMatchObject({ windowTick: 4 });
  });

  it("omits Idempotency-Key when none is given", async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ accepted: [], rejected: [], budgetRemaining: 5, duplicate: false }),
    );
    await submitCommands("b1", { windowTick: 4, commands: [{ kind: "set_price" }] });
    expect((lastCall().init.headers as Record<string, string>)["Idempotency-Key"]).toBeUndefined();
  });

  it("builds URLs with query parameters and skips undefined ones", async () => {
    fetchMock.mockImplementation(() => Promise.resolve(jsonResponse({})));
    await listTickFrames("b/1", { from: 2, to: 5 });
    expect(lastCall().url).toBe(`${BASE}/branches/b%2F1/tick-frames?from=2&to=5`);
    await listLedgerEntries("b1", { from: 0, to: 3, account: "Cash", limit: 50 });
    expect(lastCall().url).toBe(`${BASE}/branches/b1/ledger?from=0&to=3&account=Cash&limit=50`);
  });

  it("uses NEXT_PUBLIC_API_BASE_URL when set and trims trailing slashes", async () => {
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://example.test/api/v1/";
    fetchMock.mockResolvedValueOnce(jsonResponse({ status: "ok", engineVersion: "1" }));
    await getHealth();
    expect(lastCall().url).toBe("https://example.test/api/v1/health");
  });

  it("resolves void for 204 responses", async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));
    await expect(endDecisionWindow("b1", { windowTick: 4 })).resolves.toBeUndefined();
    expect(lastCall().url).toBe(`${BASE}/branches/b1/decision-window/end`);
  });
});
