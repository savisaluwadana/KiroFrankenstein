import { expect, test, type Page } from "@playwright/test";
import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import http from "node:http";
import net from "node:net";
import path from "node:path";
import { continueIfOpen, headTick, monitor, playOnce, setSpeed, shot } from "./helpers";

/**
 * (D) stub provider: live LLM verification pending (no runtime API key configured).
 *
 * The LLM path is exercised against a tiny local OpenAI-compatible stub (node http server on 127.0.0.1, dynamic
 * port) and a SECOND Go API instance started with LLM_BASE_URL pointing at it. The already running production web
 * build (baked to the first API on :18080) is pointed at the second API with a page.route rewrite of the API origin.
 * No real provider, key or model is involved: the key `stub-key-not-secret` and model `stub-model` are fake.
 */

const KEY = "stub-key-not-secret";
const MODEL = "stub-model";
const WEB_PORT = 3199;
const FIRST_API = "http://localhost:18080";
const RATIONALE = "Stock is low <script>alert(1)</script> see [click here](https://evil.example/x) now";

interface StubCall {
  n: number;
  auth: string | undefined;
  body: string;
  answer: "valid" | "malformed" | "http500";
}

const calls: StubCall[] = [];
let stub: http.Server;
let stubPort = 0;
let api: ChildProcess | null = null;
let apiPort = 0;
let apiLog = "";

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const s = net.createServer();
    s.once("error", reject);
    s.listen(0, "127.0.0.1", () => {
      const p = (s.address() as net.AddressInfo).port;
      s.close(() => resolve(p));
    });
  });
}

function startStub(): Promise<void> {
  stub = http.createServer((req, res) => {
    let body = "";
    req.on("data", (c: Buffer) => (body += c.toString()));
    req.on("end", () => {
      const n = calls.length + 1;
      const answer: StubCall["answer"] = n === 3 ? "malformed" : n === 4 ? "http500" : "valid";
      calls.push({ n, auth: req.headers.authorization, body, answer });
      if (answer === "http500") {
        res.writeHead(500, { "content-type": "application/json" });
        res.end('{"error":"stub failure"}');
        return;
      }
      const content =
        answer === "malformed"
          ? "Sure! You should probably buy more stock, hope that helps."
          : JSON.stringify({ commands: [{ kind: "place_purchase_order", sku: 1, role: "primary", qty: 40 }], rationale: RATIONALE });
      res.writeHead(200, { "content-type": "application/json" });
      res.end(JSON.stringify({ choices: [{ message: { role: "assistant", content } }], usage: { prompt_tokens: 100, completion_tokens: 50, total_tokens: 150 } }));
    });
  });
  return new Promise((resolve) => stub.listen(0, "127.0.0.1", () => {
    stubPort = (stub.address() as net.AddressInfo).port;
    resolve();
  }));
}

async function startApi(): Promise<void> {
  const bin = "/tmp/parallel-api-llm-e2e";
  execFileSync("go", ["build", "-o", bin, "./cmd/parallel-api"], { cwd: path.resolve(process.cwd(), "../backend"), stdio: "inherit" });
  apiPort = await freePort();
  api = spawn(bin, [], {
    env: {
      ...process.env,
      PORT: String(apiPort),
      WEB_ORIGIN: `http://localhost:${WEB_PORT}`,
      RATE_LIMIT_RPS: "1000",
      RATE_LIMIT_BURST: "2000",
      LLM_BASE_URL: `http://127.0.0.1:${stubPort}/v1`,
      LLM_API_KEY: KEY,
      LLM_MODEL: MODEL,
    },
  });
  api.stdout?.on("data", (d: Buffer) => (apiLog += d.toString()));
  api.stderr?.on("data", (d: Buffer) => (apiLog += d.toString()));
  for (let i = 0; i < 100; i++) {
    try {
      if ((await fetch(`http://localhost:${apiPort}/api/v1/health`)).ok) return;
    } catch {
      /* not up yet */
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`second API did not start: ${apiLog}`);
}

test.beforeAll(async ({ browserName: _b }, info) => {
  void _b;
  if (info.project.name !== "desktop") return;
  await startStub();
  await startApi();
});

test.afterAll(async () => {
  api?.kill("SIGTERM");
  if (stub?.listening) await new Promise<void>((r) => stub.close(() => r()));
});

async function idle(page: Page): Promise<void> {
  await expect
    .poll(async () => ((await page.getByTestId("playback-status").textContent()) ?? "").match(/Playing|working/) === null, { timeout: 60_000, intervals: [100] })
    .toBe(true);
}

test("stub provider (live LLM verification pending, no runtime API key configured): LLM AI duel, fallbacks, safe rationale, no key leaks, no replay calls", async ({ page }, info) => {
  test.skip(info.project.name !== "desktop", "desktop only");
  test.setTimeout(240_000);
  const SECOND = `http://localhost:${apiPort}`;
  const mon = monitor(page);

  // Point the already built web app (baked to the first API) at the second API.
  await page.route(`${FIRST_API}/api/v1/**`, (route) => route.continue({ url: route.request().url().replace(FIRST_API, SECOND) }));

  const dialogs: string[] = [];
  page.on("dialog", (d) => {
    dialogs.push(d.message());
    void d.dismiss();
  });
  const consoleText: string[] = [];
  page.on("console", (m) => consoleText.push(m.text()));
  const bodies: string[] = [];
  page.on("response", async (r) => {
    if (!/\/api\/v1\//.test(r.url())) return;
    if ((r.headers()["content-type"] ?? "").includes("event-stream")) return;
    try {
      bodies.push(`${r.url()} ${JSON.stringify(r.headers())} ${await r.text()}`);
    } catch {
      /* body not available (redirect or aborted) */
    }
  });

  /* ---------- the LLM AI is offered because /managers says so ---------- */
  const managers = (await (await fetch(`${SECOND}/api/v1/managers`)).json()) as { managers: { kind: string; available: boolean; model?: string }[] };
  expect(managers.managers.find((m) => m.kind === "ai_llm")).toMatchObject({ available: true, model: MODEL });
  await page.goto("/briefing");
  const llmRadio = page.getByRole("radio", { name: `LLM AI (${MODEL})` });
  await expect(llmRadio).toBeVisible();
  await expect(page.getByRole("radio", { name: "Rule-based AI" })).toBeVisible();
  await llmRadio.check();
  await page.getByRole("button", { name: "Challenge the AI manager" }).click();
  await page.waitForURL(/\/duels\/[0-9a-f-]{36}/);
  const duelId = new URL(page.url()).pathname.split("/").pop() ?? "";
  await expect(page.getByTestId("pane-title-ai")).toHaveText(`LLM AI (${MODEL})`);

  /* ---------- play five windows: valid, valid, malformed, HTTP 500, valid ---------- */
  await setSpeed(page, "Fast");
  for (let guard = 0; guard < 30 && (await headTick(page)) < 20; guard++) {
    if (await continueIfOpen(page)) await idle(page);
    else await playOnce(page);
  }
  expect(await headTick(page)).toBe(20);

  await page.getByRole("tab", { name: "What happened" }).click();
  const decisions = (await (await fetch(`${SECOND}/api/v1/duels/${duelId}/decisions`)).json()) as {
    decisions: { windowTick: number; managerLabel: string; provenance: string; fallbackReason?: string; model?: string; rationale: string }[];
  };
  const byTick = new Map(decisions.decisions.map((d) => [d.windowTick, d]));
  expect(byTick.get(0)).toMatchObject({ provenance: "ai_llm", managerLabel: `LLM AI (${MODEL})`, model: MODEL });
  expect(byTick.get(4)).toMatchObject({ provenance: "ai_llm" });
  expect(byTick.get(8)).toMatchObject({ provenance: "ai_rule_based_fallback", fallbackReason: "invalid_json" });
  expect(byTick.get(12)).toMatchObject({ provenance: "ai_rule_based_fallback", fallbackReason: "http_error" });
  expect(byTick.get(16)).toMatchObject({ provenance: "ai_llm" });
  expect(byTick.get(20), "the open window's decision is withheld").toBeUndefined();

  const rows = page.getByTestId("ai-decision");
  await expect(rows).toHaveCount(5);
  await expect(rows.nth(0).getByTestId("provenance-badge")).toHaveText(`LLM AI (${MODEL})`);
  await expect(rows.nth(1).getByTestId("provenance-badge")).toHaveText(`LLM AI (${MODEL})`);
  await expect(rows.nth(2).getByTestId("provenance-badge")).toHaveText("Rule-based AI (LLM fallback: invalid reply)");
  await expect(rows.nth(3).getByTestId("provenance-badge")).toHaveText("Rule-based AI (LLM fallback: service error)");
  await expect(rows.nth(4).getByTestId("provenance-badge")).toHaveText(`LLM AI (${MODEL})`);
  await expect(rows.nth(2).getByTestId("fallback-reason")).toHaveText("The AI's answer wasn't in a usable format, so the rule-based manager decided instead.");
  await expect(rows.nth(3).getByTestId("fallback-reason")).toHaveText("The AI service had a problem and could not answer, so the rule-based manager decided instead.");
  await expect(page.locator("main")).not.toContainText(/invalid_json|http_error|ai_rule_based_fallback/);
  await shot(page, info, "17-duel-llm-fallback", true);

  /* ---------- the stub's rationale is shown as literal, labelled, untrusted text ---------- */
  const rationale = rows.nth(0).getByTestId("ai-rationale");
  await expect(rationale).toContainText("AI rationale (not verified)");
  await expect(rationale.locator("p").nth(1)).toHaveText(RATIONALE.replace(/\s+/g, " "));
  expect(await rationale.locator("p").nth(1).innerHTML()).toContain("&lt;script&gt;alert(1)&lt;/script&gt;");
  await expect(page.locator('a[href*="evil.example"]')).toHaveCount(0);
  await expect(page.locator("main script")).toHaveCount(0);
  expect(dialogs, "no alert() fired").toEqual([]);

  /* ---------- usage and budget counters equal the API world, and the stub's own count ---------- */
  await page.getByRole("button", { name: "Engineering Mode" }).click();
  const worldRaw = (await (await fetch(`${SECOND}/api/v1/duels/${duelId}`)).json()) as { duel: { worldId: string } };
  const w0 = (await (await fetch(`${SECOND}/api/v1/worlds/${worldRaw.duel.worldId}`)).json()) as Record<string, unknown>;
  const w = ((w0.world as Record<string, unknown> | undefined) ?? w0) as { llmCallsMade: number; llmCallsRemaining: number; llmTokensUsed: number; llmTokensRemaining: number };
  const g = (n: number): string => String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  await expect(page.getByTestId("llm-usage")).toHaveText(
    `LLM calls made ${g(w.llmCallsMade)}, remaining ${g(w.llmCallsRemaining)}; tokens used ${g(w.llmTokensUsed)}, remaining ${g(w.llmTokensRemaining)}`,
  );
  expect(w.llmCallsMade, "every window handed to the LLM made one call").toBe(calls.length);
  expect(calls.length).toBeGreaterThanOrEqual(5);
  expect(w.llmCallsMade + w.llmCallsRemaining).toBe(40);
  expect(w.llmTokensUsed).toBeGreaterThan(0);

  /* ---------- the key appears nowhere ---------- */
  for (const c of calls) {
    expect(c.auth, "the stub receives the key as a bearer token").toBe(`Bearer ${KEY}`);
    expect(c.body, "the key is never in the prompt").not.toContain(KEY);
  }
  expect(bodies.length).toBeGreaterThan(10);
  expect(bodies.filter((b) => b.includes(KEY))).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.outerHTML)).not.toContain(KEY);
  expect(await page.evaluate(() => JSON.stringify([localStorage, sessionStorage, document.cookie]))).not.toContain(KEY);
  expect(consoleText.filter((t) => t.includes(KEY))).toEqual([]);
  expect(apiLog, "server log").not.toContain(KEY);

  /* ---------- replay and reloading never call the provider again ---------- */
  const before = calls.length;
  await page.reload();
  await expect(page.getByTestId("shared-tick")).toBeVisible();
  await page.getByRole("tab", { name: "What happened" }).click();
  await expect(page.getByTestId("ai-decision")).toHaveCount(5);
  for (let i = 0; i < 3; i++) await fetch(`${SECOND}/api/v1/duels/${duelId}/decisions`);
  const branchIds = (await (await fetch(`${SECOND}/api/v1/duels/${duelId}`)).json()) as { duel: { human: { branch: { id: string } }; ai: { branch: { id: string } } } };
  const aiId = branchIds.duel.ai.branch.id;
  expect((await fetch(`${SECOND}/api/v1/branches/${aiId}/export`)).ok).toBe(true);
  expect((await fetch(`${SECOND}/api/v1/branches/${aiId}/state?tick=20`)).ok).toBe(true);
  expect((await fetch(`${SECOND}/api/v1/branches/${aiId}/tick-frames?from=0&to=20`)).ok).toBe(true);
  await new Promise((r) => setTimeout(r, 500));
  expect(calls.length, "stub requests after reload and repeated reads").toBe(before);

  expect(mon.problems.filter((p) => !/duels\/[^/]+\/decisions/.test(p)), mon.problems.join("\n")).toEqual([]);
});
