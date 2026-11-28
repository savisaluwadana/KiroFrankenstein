import { expect, test, type APIRequestContext, type Page } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import {
  branchIdOf,
  closeSheetOnPhone,
  continueIfOpen,
  headTick,
  money,
  openMoves,
  percent,
  playOnce,
  playTo,
  scrubTo,
  setSpeed,
  startDemo,
  worldIdOf,
} from "../e2e/helpers";

/**
 * Production acceptance against a deployed stack. See playwright.prod.config.ts for the environment variables.
 * Default run: zero LLM calls. The LLM section runs only when PROD_LLM_ACCESS_CODE is set.
 * The suite never asserts who wins a duel or any particular profit figure.
 */

const WEB = (process.env.PROD_WEB_URL ?? "").replace(/\/+$/, "");
const API_ORIGIN = (process.env.PROD_API_URL ?? "").replace(/\/+$/, "").replace(/\/api\/v1$/, "");
const API = `${API_ORIGIN}/api/v1`;
const PHASE = process.env.PHASE ?? "all";
const STATE_FILE = process.env.PROD_STATE_FILE ?? path.resolve(process.cwd(), "test-results/prod-phase.json");
const TOKEN_HEADER = "X-Parallel-Write-Token";
const LLM_CODE = process.env.PROD_LLM_ACCESS_CODE ?? "";

/* ---------- recorded state for the persistence check ---------- */

interface Recorded {
  worldId: string;
  branches: { id: string; ticks: number[]; hashes: string[]; headTick: number }[];
  writeToken?: string;
}
interface StateFile {
  apiUrl: string;
  webUrl: string;
  recordedAt: string;
  items: Record<string, Recorded>;
}

function readState(): StateFile {
  try {
    return JSON.parse(fs.readFileSync(STATE_FILE, "utf8")) as StateFile;
  } catch {
    return { apiUrl: API, webUrl: WEB, recordedAt: "", items: {} };
  }
}
function record(key: string, item: Recorded): void {
  const s = readState();
  s.apiUrl = API;
  s.webUrl = WEB;
  s.recordedAt = new Date().toISOString();
  s.items[key] = item;
  fs.mkdirSync(path.dirname(STATE_FILE), { recursive: true });
  fs.writeFileSync(STATE_FILE, JSON.stringify(s, null, 2));
}

async function getJson<T>(request: APIRequestContext, p: string): Promise<T> {
  const r = await request.get(`${API}${p}`);
  expect(r.ok(), `GET ${p} -> ${r.status()}`).toBe(true);
  return (await r.json()) as T;
}

async function hashAt(request: APIRequestContext, branchId: string, tick: number): Promise<string> {
  return (await getJson<{ stateHash: string }>(request, `/branches/${branchId}/state?tick=${tick}`)).stateHash;
}

/** Hashes at tick 0, the middle and the head of a branch, read through the API. */
async function snapshotBranch(request: APIRequestContext, branchId: string): Promise<Recorded["branches"][number]> {
  const head = (await getJson<{ headTick: number }>(request, `/branches/${branchId}`)).headTick;
  const ticks = [...new Set([0, Math.floor(head / 2), head])];
  const hashes: string[] = [];
  for (const t of ticks) hashes.push(await hashAt(request, branchId, t));
  return { id: branchId, ticks, hashes, headTick: head };
}

async function tokenOf(page: Page, id: string): Promise<string | undefined> {
  return (await page.evaluate((k) => window.sessionStorage.getItem(`parallel.writeToken.${k}`), id)) ?? undefined;
}

interface Mon {
  problems: string[];
  nonGetRequests: string[];
}
/** Console errors, page errors, failed requests and non-2xx API responses of this deployment. */
function monitor(page: Page): Mon {
  const m: Mon = { problems: [], nonGetRequests: [] };
  page.on("console", (c) => {
    if (c.type() === "error") m.problems.push(`console.error: ${c.text()}`);
  });
  page.on("pageerror", (e) => m.problems.push(`pageerror: ${e.message}`));
  page.on("requestfailed", (r) => {
    const err = r.failure()?.errorText ?? "";
    if (!err.includes("ERR_ABORTED")) m.problems.push(`requestfailed: ${r.method()} ${r.url()} ${err}`);
  });
  page.on("response", (r) => {
    if (r.url().startsWith(API) && r.status() >= 400) m.problems.push(`http ${r.status()}: ${r.request().method()} ${r.url()}`);
  });
  page.on("request", (r) => {
    if (r.url().startsWith(API) && r.method() !== "GET") m.nonGetRequests.push(`${r.method()} ${r.url()}`);
  });
  return m;
}

async function idle(page: Page): Promise<void> {
  await expect
    .poll(async () => ((await page.getByTestId("playback-status").textContent()) ?? "").match(/Playing|working/) === null, { timeout: 90_000, intervals: [150] })
    .toBe(true);
}

test.describe.configure({ mode: "serial" });

test.describe("deployed stack", () => {
  test.skip(PHASE === "after", "PHASE=after only re-reads the worlds recorded by PHASE=before");

  /* 1 + 2 + 3 + 4: demo world, decision, rewind, fork, comparison. Runs on desktop and on the phone viewport. */
  test("main flow: demo world, first decision window, a decision, rewind (no POST), fork, comparison, What changed?", async ({ page, request }, info) => {
    test.setTimeout(420_000);
    const mon = monitor(page);
    await startDemo(page);
    await expect(page.getByTestId("hud-cash")).toHaveText("$100,000.00");
    const world = worldIdOf(page);
    const rootId = branchIdOf(page);

    // The first decision window is open at hour 0: place a decision with the cards and lock it in.
    await openMoves(page);
    await expect(page.getByRole("heading", { name: "Your move", level: 2 })).toBeVisible();
    await page.getByRole("button", { name: "Add to my moves" }).first().click();
    await expect(page.getByTestId("pending-command")).toHaveCount(1);
    await page.getByRole("button", { name: "Lock in decisions" }).click();
    await expect(page.getByTestId("accepted-command")).toHaveCount(1);
    await continueIfOpen(page);
    await closeSheetOnPhone(page);

    // Playback stops by itself at the next decision window (hour 4).
    await setSpeed(page, "Fast");
    await playOnce(page);
    expect(await headTick(page)).toBe(4);
    await expect(page.getByTestId("playback-status")).toContainText(/Paused for your move|something just happened/);
    await playTo(page, 8);

    // Rewind is a read: scrubbing sends no POST.
    await closeSheetOnPhone(page);
    const before = mon.nonGetRequests.length;
    await scrubTo(page, 4);
    await expect(page.getByTestId("timeline-readout")).toContainText("Showing Thursday, 4:00 AM");
    expect(mon.nonGetRequests.slice(before), "rewind sends no POST").toHaveLength(0);

    // Fork from the rewound hour.
    await openMoves(page);
    await page.locator("#explore-future").click();
    const seq = page.getByTestId("fork-sequence");
    await expect(seq).toBeVisible();
    await seq.getByRole("button", { name: /Skip/ }).click();
    await expect(seq).toHaveCount(0);
    await page.waitForURL((u) => u.searchParams.get("branch") !== rootId);
    const altId = branchIdOf(page);
    await closeSheetOnPhone(page);
    await playTo(page, 12);

    // Comparison and "What changed?".
    await page.getByRole("button", { name: "Compare futures" }).click();
    await expect(page.getByTestId("comparison-view")).toBeVisible();
    const diffRes = page.waitForResponse((r) => r.url() === `${API}/diffs` && r.request().method() === "POST");
    await page.getByTestId("what-changed").click();
    expect((await diffRes).status()).toBe(200);
    const panel = page.getByTestId("diff-panel");
    await expect(panel).toBeVisible();
    await expect(panel.getByTestId("diff-statement")).toBeVisible();

    // Authoritative hashes for the persistence check.
    record(`journey-${info.project.name}`, {
      worldId: world,
      branches: [await snapshotBranch(request, rootId), await snapshotBranch(request, altId)],
      writeToken: await tokenOf(page, world),
    });
    expect(mon.problems, mon.problems.join("\n")).toEqual([]);
  });

  /* 1b: custom setup (desktop only) */
  test("custom setup: a changed starting cash reaches the world's assumptions", async ({ page, request }, info) => {
    test.skip(info.project.name !== "desktop", "desktop only");
    const mon = monitor(page);
    await page.goto("/setup");
    const cash = page.getByRole("textbox", { name: "Starting cash" });
    await expect(cash).toHaveValue("100000.00");
    await cash.fill("150000");
    await page.getByRole("button", { name: "Start with these settings" }).click();
    await page.waitForURL(/\/worlds\//);
    await expect(page.getByTestId("hud-cash")).toHaveText("$150,000.00");
    const w = await getJson<{ world?: { assumptions: { path: string; value: number }[] }; assumptions?: { path: string; value: number }[] }>(
      request,
      `/worlds/${worldIdOf(page)}`,
    );
    const assumptions = w.world?.assumptions ?? w.assumptions ?? [];
    expect(assumptions.find((a) => a.path === "openingCashCents")?.value).toBe(15_000_000);
    expect(mon.problems, mon.problems.join("\n")).toEqual([]);
  });

  /* 5: rule-based duel to the horizon; scoreboard equals the API (desktop only) */
  test("duel: Human vs Rule-based AI to the horizon, scoreboard equals the API", async ({ page, request }, info) => {
    test.skip(info.project.name !== "desktop", "desktop only");
    test.setTimeout(540_000);
    const mon = monitor(page);
    await page.goto("/briefing");
    const ruleRadio = page.getByRole("radio", { name: "Rule-based AI" }); // offered only when the server has an LLM configured
    if ((await ruleRadio.count()) > 0) await expect(ruleRadio).toBeChecked();
    await page.getByRole("button", { name: "Challenge the AI manager" }).click();
    await page.waitForURL(/\/duels\/[0-9a-f-]{36}/);
    const duelId = new URL(page.url()).pathname.split("/").pop() ?? "";
    await expect(page.getByTestId("pane-human")).toBeVisible();
    await setSpeed(page, "Fast");
    for (let guard = 0; guard < 100 && (await headTick(page)) < 120; guard++) {
      if (await continueIfOpen(page)) {
        await idle(page);
        continue;
      }
      await playOnce(page);
    }
    expect(await headTick(page)).toBe(120);
    await idle(page);
    await expect(page.getByTestId("syncing-indicator")).toHaveText("");

    interface Side {
      branchId: string;
      score: { compositeHundredths: number };
      metrics: { cashCents: number; netProfitCents: number; serviceLevelBps: number; backlogOrders: number };
    }
    const score = await getJson<{ provisional: boolean; comparisonTick: number; human: Side; ai: Side }>(request, `/duels/${duelId}/score`);
    expect(score.provisional).toBe(false);
    expect(score.comparisonTick).toBe(120);
    const pts = (h: number): string => `${h < 0 ? "-" : ""}${Math.floor(Math.abs(h) / 100)}.${String(Math.abs(h) % 100).padStart(2, "0")}`;
    await expect(page.getByTestId("scoreboard")).toBeVisible();
    await expect(page.getByTestId("score-human")).toHaveText(pts(score.human.score.compositeHundredths));
    await expect(page.getByTestId("score-ai")).toHaveText(pts(score.ai.score.compositeHundredths));
    const h = score.human.metrics;
    const a = score.ai.metrics;
    await expect(page.getByTestId("raw-cash").locator("td")).toHaveText([money(h.cashCents), money(a.cashCents), money(h.cashCents - a.cashCents, true)]);
    await expect(page.getByTestId("raw-profit").locator("td")).toHaveText([money(h.netProfitCents), money(a.netProfitCents), money(h.netProfitCents - a.netProfitCents, true)]);
    await expect(page.getByTestId("raw-onTime").locator("td").nth(0)).toHaveText(percent(h.serviceLevelBps));
    await expect(page.getByTestId("raw-onTime").locator("td").nth(1)).toHaveText(percent(a.serviceLevelBps));
    await expect(page.getByTestId("duel-disclaimer")).toContainText("Synthetic simulation output under the stated assumptions.");

    // The AI branch replays to the same hash: re-read the state at the horizon twice.
    const first = await hashAt(request, score.ai.branchId, 120);
    expect(await hashAt(request, score.ai.branchId, 120)).toBe(first);

    record("duel", {
      worldId: await duelWorld(request, duelId),
      branches: [await snapshotBranch(request, score.human.branchId), await snapshotBranch(request, score.ai.branchId)],
      writeToken: await tokenOf(page, duelId),
    });
    expect(mon.problems, mon.problems.join("\n")).toEqual([]);
  });

  /* 6: authorization and shared link (desktop only) */
  test("authorization: no token 401, wrong token 403, token works, shared link is read-only", async ({ request, browser }, info) => {
    test.skip(info.project.name !== "desktop", "desktop only");
    const created = await request.post(`${API}/demo`);
    expect(created.status()).toBe(201);
    const body = (await created.json()) as { writeToken: string; world: { id: string }; rootBranch: { id: string } };
    const branch = body.rootBranch.id;
    const none = await request.post(`${API}/branches/${branch}/advance`, { data: { runToTick: 4 } });
    expect(none.status()).toBe(401);
    expect(((await none.json()) as { type: string }).type).toBe("urn:parallel:problem:write-token-required");
    const wrong = await request.post(`${API}/branches/${branch}/advance`, { data: { runToTick: 4 }, headers: { [TOKEN_HEADER]: "x".repeat(43) } });
    expect(wrong.status()).toBe(403);
    expect(((await wrong.json()) as { type: string }).type).toBe("urn:parallel:problem:write-token-invalid");
    expect((await getJson<{ headTick: number }>(request, `/branches/${branch}`)).headTick).toBe(0);
    const ok = await request.post(`${API}/branches/${branch}/advance`, { data: { runToTick: 4 }, headers: { [TOKEN_HEADER]: body.writeToken } });
    expect(ok.status()).toBe(200);

    const ctx = await browser.newContext(info.project.use);
    try {
      const guest = await ctx.newPage();
      await guest.goto(`/worlds/${body.world.id}?branch=${branch}`);
      await expect(guest.getByTestId("hud-cash")).toBeVisible();
      await expect(guest.getByTestId("shared-read-only-banner")).toBeVisible();
      await expect(guest.getByRole("button", { name: /^Play$/ })).toHaveCount(0);
    } finally {
      await ctx.close();
    }
  });

  /* 7: CORS and headers (desktop only). No rate-limit probing: see the runbook for the manual check. */
  test("limits and headers: foreign origin denied, web origin allowed, security headers", async ({ request }, info) => {
    test.skip(info.project.name !== "desktop", "desktop only");
    const pre = (origin: string) =>
      request.fetch(`${API}/health`, { method: "OPTIONS", headers: { Origin: origin, "Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "content-type,x-parallel-write-token" } });
    const evil = await pre("https://evil.example");
    expect(evil.headers()["access-control-allow-origin"]).toBeUndefined();
    const good = await pre(WEB);
    expect(good.headers()["access-control-allow-origin"]).toBe(WEB);
    const evilGet = await request.get(`${API}/health`, { headers: { Origin: "https://evil.example" } });
    expect(evilGet.headers()["access-control-allow-origin"]).toBeUndefined();
    const h = (await request.get(`${API}/health`)).headers();
    expect(h["x-content-type-options"]).toBe("nosniff");
    expect(h["content-security-policy"] ?? "").not.toContain("unsafe-eval");
    expect((await request.get(`${API_ORIGIN}/readyz`)).status()).toBe(200);
    const web = await request.get(`${WEB}/`);
    expect(web.status()).toBe(200);
    const csp = web.headers()["content-security-policy"] ?? "";
    expect(csp).not.toContain("unsafe-eval");
    expect(csp).toContain(API_ORIGIN);
    expect(WEB.startsWith("https://") ? web.headers()["strict-transport-security"] : "hsts-not-applicable").toBeTruthy();
  });

  /* 8: persistence. In "all" mode this re-opens the worlds in a fresh context; PHASE=before also leaves the file for PHASE=after. */
  test("persistence: recorded worlds re-open read-only with identical hashes in a fresh context", async ({ browser, request }, info) => {
    test.skip(info.project.name !== "desktop", "desktop only");
    await verifyRecorded(browser, request, false);
  });

  /* 9: LLM section, only with PROD_LLM_ACCESS_CODE (at most 2 paid calls) */
  test("LLM duel (opt-in): access code gate, labels, key never in a response", async ({ page, request }, info) => {
    test.skip(info.project.name !== "desktop", "desktop only");
    test.skip(LLM_CODE === "", "set PROD_LLM_ACCESS_CODE to run the LLM section (it makes up to 2 provider calls)");
    test.setTimeout(240_000);
    const managers = await getJson<{ managers: { kind: string; available: boolean; model?: string; requiresAccessCode?: boolean }[] }>(request, "/managers");
    const llm = managers.managers.find((m) => m.kind === "ai_llm");
    expect(llm?.available, "LLM AI is configured on the server").toBe(true);
    expect(llm?.requiresAccessCode, "LLM_ACCESS_CODE is set on the server").toBe(true);
    const refused = await request.post(`${API}/duels`, { data: { aiKind: "ai_llm" } });
    expect(refused.status()).toBe(403);

    const bodies: string[] = [];
    page.on("response", async (r) => {
      if (!r.url().startsWith(API) || (r.headers()["content-type"] ?? "").includes("event-stream")) return;
      try {
        bodies.push(await r.text());
      } catch {
        /* redirect or aborted */
      }
    });
    await page.goto("/briefing");
    await page.getByRole("radio", { name: `LLM AI (${llm?.model ?? ""})` }).check();
    await page.getByLabel("Access code for the LLM AI").fill(LLM_CODE);
    await page.getByRole("button", { name: "Challenge the AI manager" }).click();
    await page.waitForURL(/\/duels\/[0-9a-f-]{36}/);
    const duelId = new URL(page.url()).pathname.split("/").pop() ?? "";
    await expect(page.getByTestId("pane-title-ai")).toHaveText(`LLM AI (${llm?.model ?? ""})`);
    await setSpeed(page, "Fast");
    for (let guard = 0; guard < 12 && (await headTick(page)) < 8; guard++) {
      if (await continueIfOpen(page)) await idle(page);
      else await playOnce(page);
    }
    const decisions = await getJson<{ decisions: { provenance: string; managerLabel: string; model?: string }[] }>(request, `/duels/${duelId}/decisions`);
    expect(decisions.decisions.length).toBeGreaterThan(0);
    for (const d of decisions.decisions) {
      expect(["ai_llm", "ai_rule_based_fallback"]).toContain(d.provenance);
      expect(d.managerLabel).toMatch(/^(LLM AI \(|Rule-based AI \(LLM fallback)/);
    }
    const all = bodies.join("\n");
    expect(all).not.toMatch(/sk-or-[A-Za-z0-9_-]{8,}|sk-[A-Za-z0-9_-]{24,}/);
    const key = process.env.PROD_LLM_API_KEY ?? "";
    if (key !== "") expect(all).not.toContain(key);
    expect(all).not.toContain(LLM_CODE);
  });
});

/* 8b: PHASE=after, run alone after a restart or redeploy of the API */
test.describe("after restart", () => {
  test.skip(PHASE !== "after", "only for PHASE=after");
  test("recorded worlds survived the restart: same hashes, same heads, token still advances", async ({ browser, request }, info) => {
    test.skip(info.project.name !== "desktop", "desktop only");
    await verifyRecorded(browser, request, true);
  });
});

async function verifyRecorded(browser: import("@playwright/test").Browser, request: APIRequestContext, afterRestart: boolean): Promise<void> {
  const state = readState();
  expect(Object.keys(state.items).length, `no recorded worlds in ${STATE_FILE}; run the main flow (PHASE=before) first`).toBeGreaterThan(0);
  expect(state.apiUrl, "state file belongs to this API").toBe(API);
  for (const [key, item] of Object.entries(state.items)) {
    for (const b of item.branches) {
      expect((await getJson<{ headTick: number }>(request, `/branches/${b.id}`)).headTick, `${key} ${b.id} head`).toBe(b.headTick);
      for (let i = 0; i < b.ticks.length; i++) expect(await hashAt(request, b.id, b.ticks[i]!), `${key} ${b.id} hash at ${b.ticks[i]}`).toBe(b.hashes[i]);
    }
    // A fresh context has no sessionStorage, hence no write token: the world opens as a read-only copy.
    if (item.worldId !== "" && !key.startsWith("duel")) {
      const ctx = await browser.newContext();
      try {
        const p = await ctx.newPage();
        await p.goto(`${WEB}/worlds/${item.worldId}?branch=${item.branches[0]!.id}`);
        await expect(p.getByTestId("hud-cash")).toBeVisible();
        await expect(p.getByTestId("shared-read-only-banner")).toBeVisible();
      } finally {
        await ctx.close();
      }
    }
  }
  if (afterRestart) {
    const j = Object.entries(state.items).find(([k, v]) => k.startsWith("journey") && v.writeToken !== undefined);
    if (j !== undefined) {
      const [, item] = j;
      const b = item.branches[0]!;
      const token = item.writeToken!;
      const none = await request.post(`${API}/branches/${b.id}/advance`, { data: { runToTick: b.headTick } });
      expect(none.status(), "no token still refused after restart").toBe(401);
      const withToken = await request.post(`${API}/branches/${b.id}/advance`, { data: { runToTick: b.headTick }, headers: { [TOKEN_HEADER]: token } });
      expect(withToken.status(), "the stored token is still accepted after restart (hash persisted)").not.toBe(401);
      expect(withToken.status()).not.toBe(403);
    }
  }
}

async function duelWorld(request: APIRequestContext, duelId: string): Promise<string> {
  const d = await getJson<{ worldId?: string; duel?: { worldId?: string } }>(request, `/duels/${duelId}`);
  return d.worldId ?? d.duel?.worldId ?? "";
}
