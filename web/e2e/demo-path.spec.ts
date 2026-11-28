import { expect, test, type Page } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import {
  apiPost,
  API,
  apiMetrics,
  branchIdOf,
  closeSheetOnPhone,
  continueIfOpen,
  headTick,
  monitor,
  money,
  openMoves,
  percent,
  playOnce,
  playTo,
  scrubTo,
  selectedTick,
  setSpeed,
  shot,
  statusText,
  worldIdOf,
} from "./helpers";

/**
 * Demo_Path (requirements 34.1 to 34.8, 33.6, 33.7) in a real browser against the real Go API (memory store, no LLM
 * variables). Nothing here asserts a Net_Profit value, the sign of a difference, or a winner: figures shown are compared
 * with the API and the two realities are only required to differ.
 *
 * "Waits 2 s before each user action" (34.1) is modelled by `human()`. It is called before every state-changing click
 * of the timed segment (start, mode, play, rewind, explore, each card's add, lock in, continue, play, switch, compare,
 * what-changed). Filling the fields of one card counts as part of that card's single action.
 */

const HUMAN_MS = 2000;
const TIMED_BUDGET_MS = 60_000;
const ARTIFACTS = path.resolve(process.cwd(), "e2e/.artifacts");

interface Plan {
  forkTick: number;
  crisisTick: number;
  seed: number;
  commands: { kind: string; skuIndex: number; role?: string; qty: number }[];
}
const plan = JSON.parse(fs.readFileSync(path.resolve(process.cwd(), "e2e/fixtures/demo-plan.json"), "utf8")) as Plan;
const order = plan.commands.find((c) => c.kind === "place_purchase_order");
const hire = plan.commands.find((c) => c.kind === "hire_temp_workers");
if (order === undefined || hire === undefined) throw new Error("demo plan fixture lacks the backup order or the hire");

async function human(page: Page): Promise<void> {
  await page.waitForTimeout(HUMAN_MS);
}

const group = (n: number): string => String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
function pts(h: number): string {
  const a = Math.abs(h);
  return `${h < 0 ? "-" : ""}${Math.floor(a / 100)}.${String(a % 100).padStart(2, "0")}`;
}
const signedCount = (n: number): string => (n > 0 ? `+${group(n)}` : group(n));
function bpsPoints(bps: number): string {
  const body = percent(Math.abs(bps)).replace("%", "");
  return `${bps > 0 ? "+" : bps < 0 ? "-" : ""}${body} points`;
}

interface Metrics {
  cashCents: number;
  netProfitCents: number;
  serviceLevelBps: number;
  fillRateBps: number;
  backlogOrders: number;
}
interface Side {
  branchId: string;
  managerLabel: string;
  score: { compositeHundredths: number };
  metrics: Metrics;
  cancelledOrders: number;
}
interface Score {
  comparisonTick: number;
  provisional: boolean;
  outcome: "human_wins" | "ai_wins" | "draw";
  human: Side;
  ai: Side;
}
interface DiffMetric {
  key: string;
  a: number;
  b: number;
  delta: number;
}

async function getJson<T>(page: Page, p: string): Promise<T> {
  const r = await page.request.get(`${API}${p}`);
  expect(r.ok(), `${p} -> ${r.status()}`).toBe(true);
  return (await r.json()) as T;
}
async function postJson<T>(page: Page, p: string, data: unknown): Promise<T> {
  const r = await apiPost(page, p, data);
  const text = await r.text();
  expect(r.ok(), `${p} -> ${r.status()} ${text}`).toBe(true);
  return (text === "" ? {} : JSON.parse(text)) as T;
}
async function idle(page: Page): Promise<void> {
  await expect
    .poll(async () => (await statusText(page)).match(/Playing|working/) === null, { timeout: 60_000, intervals: [100] })
    .toBe(true);
}
function record(name: string, value: unknown): void {
  fs.mkdirSync(ARTIFACTS, { recursive: true });
  const file = path.join(ARTIFACTS, "demo-numbers.json");
  const cur = fs.existsSync(file) ? (JSON.parse(fs.readFileSync(file, "utf8")) as Record<string, unknown>) : {};
  cur[name] = value;
  fs.writeFileSync(file, JSON.stringify(cur, null, 2) + "\n");
}

/** Fills the two plan cards in the open window and locks the moves in. */
async function enterPlan(page: Page, paced: boolean): Promise<void> {
  const orderCard = page.locator('section[aria-labelledby="order-card-title"]');
  const hireCard = page.locator('section[aria-labelledby="hire-card-title"]');
  if (paced) await human(page);
  await orderCard.getByRole("combobox", { name: "Product", exact: true }).selectOption({ value: String(order!.skuIndex) });
  await orderCard.getByRole("combobox", { name: "Supplier" }).selectOption({ value: "backup" });
  await orderCard.getByRole("textbox", { name: "Quantity" }).fill(String(order!.qty));
  await orderCard.getByRole("button", { name: "Add to my moves" }).click();
  if (paced) await human(page);
  await hireCard.getByRole("textbox", { name: "Temporary workers" }).fill(String(hire!.qty));
  await hireCard.getByRole("button", { name: "Add to my moves" }).click();
  await expect(page.getByTestId("pending-command")).toHaveCount(2);
}
async function lockIn(page: Page, paced: boolean): Promise<void> {
  if (paced) await human(page);
  await page.getByRole("button", { name: "Lock in decisions" }).click();
  await expect(page.getByTestId("accepted-command")).toHaveCount(2);
}

/** Runs a world to the horizon through the API only, ending every window with no commands. */
async function apiRunTo(page: Page, branch: string, to: number): Promise<number> {
  for (let g = 0; g < 80; g++) {
    const r = await apiPost(page, `/branches/${branch}/advance`, { runToTick: to });
    expect(r.ok(), await r.text()).toBe(true);
    const head = ((await r.json()) as { headTick: number }).headTick;
    if (head >= to) return head;
    const e = await apiPost(page, `/branches/${branch}/decision-window/end`, { windowTick: head });
    expect(e.ok(), await e.text()).toBe(true);
  }
  throw new Error("api run did not reach target");
}

/* ------------------------------------------------------------------------------------------------ */

test("steps 1-3, custom assumptions: a configured business starts and the overrides are recorded", async ({ page }, info) => {
  test.skip(info.project.name !== "desktop", "desktop only");
  const mon = monitor(page);

  // 1. Create a business: landing -> briefing.
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Parallel" })).toBeVisible();
  await human(page);
  await page.getByRole("link", { name: /Begin your shift/ }).click();
  await expect(page.getByRole("heading", { name: "Black Friday Meltdown" })).toBeVisible();

  // 2. Adjust assumptions on /setup through the friendly controls.
  await human(page);
  await page.getByRole("link", { name: "Customize the business" }).click();
  await expect(page.getByRole("heading", { name: "Set up your business" })).toBeVisible();
  const cash = page.getByRole("textbox", { name: "Starting cash" });
  const staff = page.getByRole("textbox", { name: "Packing staff" });
  await expect(cash).toHaveValue("100000.00");
  const defaultStaff = Number(await staff.inputValue());
  expect(defaultStaff).toBeGreaterThan(0);
  await cash.fill("120000");
  await staff.fill(String(defaultStaff + 2));
  const start = page.getByRole("button", { name: "Start with these settings" });
  await expect(start).toBeEnabled();
  await shot(page, info, "demo-path-02-setup-custom", true);

  // 3. Start. The world exists, the HUD shows the chosen cash, and the API records both overrides.
  await human(page);
  await start.click();
  await page.waitForURL(/\/worlds\//);
  await expect(page.getByTestId("hud-cash")).toHaveText("$120,000.00");
  await expect(page.locator("canvas").first()).toBeVisible();
  const a = await getJson<{ assumptions: { path: string; value: number; overridden: boolean; default: number }[] }>(page, `/worlds/${worldIdOf(page)}/assumptions`);
  const cashA = a.assumptions.find((x) => x.path === "openingCashCents")!;
  const staffA = a.assumptions.find((x) => x.path === "workforce.regularWorkers")!;
  expect(cashA).toMatchObject({ value: 12_000_000, overridden: true });
  expect(staffA).toMatchObject({ value: defaultStaff + 2, overridden: true });
  expect(staffA.default).toBe(defaultStaff);
  expect(a.assumptions.filter((x) => x.overridden).map((x) => x.path).sort()).toEqual(["openingCashCents", "workforce.regularWorkers"]);
  record("customPath", { openingCashCents: cashA.value, regularWorkers: staffA.value, defaultRegularWorkers: staffA.default });
  expect(mon.problems, mon.problems.join("\n")).toEqual([]);
});

test("steps 3-9, default path: crisis, rewind, fork, compare, duel, scoreboard and ledger", async ({ page }, info) => {
  test.skip(info.project.name !== "desktop", "desktop only");
  test.setTimeout(420_000);
  const mon = monitor(page);
  const numbers: Record<string, unknown> = { seed: plan.seed, fixtureCrisisTick: plan.crisisTick, fixtureForkTick: plan.forkTick };

  // 1 (landing -> briefing) is covered above; this path starts from the briefing.
  await page.goto("/");
  await shot(page, info, "demo-path-01-landing");
  await page.getByRole("link", { name: /Begin your shift/ }).click();
  await expect(page.getByRole("heading", { name: "Black Friday Meltdown" })).toBeVisible();

  /* ================= timed segment: steps 3 to 7 ================= */
  const t0 = Date.now();

  // 3. Start the Black Friday scenario with the default seed in one action.
  await human(page);
  await page.getByRole("button", { name: "Start Black Friday demo" }).click();
  await page.waitForURL(/\/worlds\//);
  await expect(page.getByTestId("hud-cash")).toHaveText("$100,000.00");
  await expect(page.locator("canvas").first()).toBeVisible();
  const world = worldIdOf(page);
  const rootId = branchIdOf(page);
  await shot(page, info, "demo-path-03-start");

  // The first decision moment is open; "disruption onsets only" and Fast keep the crisis demo short (33.6, 33.7).
  await continueIfOpen(page);
  await closeSheetOnPhone(page);
  await human(page);
  await page.getByLabel("Pause automatically at").selectOption("disruption_onsets_only");
  await setSpeed(page, "Fast");

  // 4. Experience an operational crisis: play until the first surprise pauses playback.
  await human(page);
  await page.getByRole("button", { name: /^Play$/ }).click();
  await expect.poll(async () => (await statusText(page)).startsWith("Playing"), { timeout: 5000 }).toBe(true).catch(() => undefined);
  await expect(page.getByTestId("playback-status")).toContainText(/something just happened|surprise|Paused/i, { timeout: 30_000 });
  await idle(page);
  const crisisHead = await headTick(page);
  expect(crisisHead, "onsets-only play stops after the demo crisis hour began").toBeGreaterThan(plan.crisisTick - 1);
  await continueIfOpen(page).catch(() => false);
  await expect.poll(() => page.getByTestId("crisis-card").count(), { timeout: 10_000 }).toBeGreaterThan(0);
  const crisisTitles = await page.getByTestId("crisis-card").locator("h3").allInnerTexts();
  numbers["crisisCardTitles"] = crisisTitles;
  numbers["firstSurprisePauseTick"] = crisisHead;
  numbers["firstSurprisePauseClock"] = (await page.getByTestId("hud-clock").textContent()) ?? "";
  // The right rail keeps its scroll offset from the previous screen; show its first alert from the top.
  await page.getByTestId("crisis-card").first().scrollIntoViewIfNeeded();
  await page.getByTestId("crisis-card").first().evaluate((el) => {
    let p: HTMLElement | null = el.parentElement;
    while (p !== null) {
      if (p.scrollHeight > p.clientHeight + 2 && getComputedStyle(p).overflowY !== "visible") p.scrollTop = 0;
      p = p.parentElement;
    }
  });
  await shot(page, info, "demo-path-04-crisis");
  const crisisBeforeRewind = await apiMetrics(page, rootId, crisisHead);
  numbers["rootAtCrisisPause"] = crisisBeforeRewind;

  // 6. Rewind to the demo fork hour (a read: no mutating request) and explore another future there.
  await human(page);
  const reqsBefore = mon.requests.length;
  await scrubTo(page, plan.forkTick);
  expect(mon.requests.slice(reqsBefore).filter((r) => r.method() !== "GET"), "rewind sends no POST").toHaveLength(0);
  await human(page);
  await openMoves(page);
  await page.locator("#explore-future").click();
  const seq = page.getByTestId("fork-sequence");
  await expect(seq).toBeVisible();
  await expect(seq).toContainText("Same history up to");
  await shot(page, info, "demo-path-06-fork");
  await seq.getByRole("button", { name: /Skip/ }).click();
  await expect(seq).toHaveCount(0);
  await page.waitForURL((u) => u.searchParams.get("branch") !== rootId);
  const childId = branchIdOf(page);
  expect(await headTick(page)).toBe(plan.forkTick);

  // 5. The management decision (fixture values): backup order and temporary workers, from the cards.
  await openMoves(page);
  await enterPlan(page, true);
  await shot(page, info, "demo-path-05-decision");
  await lockIn(page, true);
  await expect(page.getByTestId("accepted-command").first()).toContainText(/Takes effect at/);
  await human(page);
  await continueIfOpen(page);
  await closeSheetOnPhone(page);

  // Play the new reality until the next surprise pause.
  await human(page);
  await playOnce(page);
  const childHead = await headTick(page);
  await continueIfOpen(page).catch(() => false);
  await closeSheetOnPhone(page);

  // 7. Compare the two realities side by side and ask what changed.
  await human(page);
  await page.getByRole("button", { name: "Compare futures" }).click();
  const cmp = page.getByTestId("comparison-view");
  await expect(cmp).toBeVisible();
  const common = Math.min(await selectedTick(page), childHead, crisisHead);
  const mRoot = await apiMetrics(page, rootId, common);
  const mChild = await apiMetrics(page, childId, common);
  await expect(page.getByTestId("diff-cash").locator("td").nth(2)).toHaveText(money(mChild.cashCents - mRoot.cashCents, true));
  await human(page);
  const diffReq = page.waitForRequest((r) => r.method() === "POST" && r.url() === `${API}/diffs`);
  await page.getByTestId("what-changed").click();
  const sent = JSON.parse((await diffReq).postData() ?? "{}") as { branchA: string; branchB: string };
  expect(new Set([sent.branchA, sent.branchB])).toEqual(new Set([rootId, childId]));
  const panel = page.getByTestId("diff-panel");
  await expect(panel).toBeVisible();
  await expect(panel.getByTestId("diff-statement")).toBeVisible();
  const timedMs = Date.now() - t0;
  /* ================= end of timed segment ================= */
  numbers["timedSegmentMs"] = timedMs;
  expect(timedMs, `crisis -> fork -> compare segment took ${timedMs} ms`).toBeLessThan(TIMED_BUDGET_MS);

  // Panel figures equal the API values for the compared hour.
  const diff = await postJson<{ comparisonTick: number; metrics: DiffMetric[]; divergence: { tick: number } | null; differingInputs: unknown[] }>(page, "/diffs", {
    branchA: sent.branchA,
    branchB: sent.branchB,
  });
  expect(diff.differingInputs).toHaveLength(2);
  expect(diff.divergence).not.toBeNull();
  expect(diff.divergence!.tick).toBeGreaterThanOrEqual(plan.forkTick + 1);
  const kinds: Record<string, "money" | "bps" | "count"> = { net_profit: "money", service_level: "bps", backlog_orders: "count" };
  for (const m of diff.metrics) {
    const kind = kinds[m.key];
    if (kind === undefined) continue;
    const cells = panel.getByTestId(`diff-metric-${m.key}`).locator("td");
    const fmt = (n: number): string => (kind === "money" ? money(n) : kind === "bps" ? percent(n) : group(n));
    await expect(cells.nth(0)).toHaveText(fmt(m.a));
    await expect(cells.nth(1)).toHaveText(fmt(m.b));
    await expect(cells.nth(2)).toHaveText(kind === "money" ? money(m.delta, true) : kind === "bps" ? bpsPoints(m.delta) : signedCount(m.delta));
  }
  numbers["comparisonTickMid"] = diff.comparisonTick;
  numbers["comparisonMid"] = Object.fromEntries(diff.metrics.filter((m) => kinds[m.key] !== undefined).map((m) => [m.key, { original: m.a, forked: m.b }]));
  numbers["firstDivergenceTick"] = diff.divergence!.tick;
  await panel.scrollIntoViewIfNeeded();
  await page.waitForTimeout(600);
  await shot(page, info, "demo-path-07-compare", true);
  expect(await panel.innerText()).not.toMatch(/because|caused by|winner/i);

  /* ---- both realities to the horizon (outside the timed segment) ---- */
  await closeSheetOnPhone(page);
  await page.getByRole("button", { name: "Back to one view" }).click();
  await playTo(page, 120);
  const switcher = page.getByRole("group", { name: "Which future you are looking at" });
  await switcher.getByRole("button", { name: /Original path/ }).click();
  await page.waitForURL((u) => u.searchParams.get("branch") === rootId);
  await expect(page.getByTestId("syncing-indicator")).toHaveText("");
  await playTo(page, 120);

  // At the horizon the two realities differ in net profit and in service level (34.4); no direction is asserted.
  const hDiff = await postJson<{ comparisonTick: number; metrics: DiffMetric[] }>(page, "/diffs", { branchA: rootId, branchB: childId });
  expect(hDiff.comparisonTick).toBe(120);
  const byKey = new Map(hDiff.metrics.map((m) => [m.key, m]));
  expect(byKey.get("net_profit")!.a, "net profit differs between realities").not.toBe(byKey.get("net_profit")!.b);
  expect(byKey.get("service_level")!.a, "service level differs between realities").not.toBe(byKey.get("service_level")!.b);
  numbers["horizonMetrics"] = Object.fromEntries(hDiff.metrics.filter((m) => kinds[m.key] !== undefined).map((m) => [m.key, { original: m.a, forked: m.b }]));

  // The forked branch's HUD shows the parent's figures and the differences, all from the API (34.4).
  await page.goto(`/worlds/${world}?branch=${childId}`);
  await expect(page.getByTestId("hud-cash")).toBeVisible();
  await scrubTo(page, 120);
  const mc = await apiMetrics(page, childId, 120);
  const mr = await apiMetrics(page, rootId, 120);
  await expect(page.getByTestId("hud-net-profit")).toContainText(money(Math.abs(mc.netProfitCents)));
  await expect(page.getByTestId("parent-net-profit")).toContainText(money(mr.netProfitCents));
  await expect(page.getByTestId("parent-service-level")).toContainText(percent(mr.serviceLevelBps));
  await expect(page.getByTestId("parent-backlog")).toContainText(group(mr.backlogOrders));
  await expect(page.getByTestId("diff-net-profit")).toContainText(money(mc.netProfitCents - mr.netProfitCents, true));
  await expect(page.getByTestId("diff-service-level")).toContainText(bpsPoints(mc.serviceLevelBps - mr.serviceLevelBps));
  await expect(page.getByTestId("diff-backlog")).toContainText(signedCount(mc.backlogOrders - mr.backlogOrders));

  // Reproducibility (34.3): an independent API run with the same seed, fork hour and commands gives the same horizon hash.
  const shownHash = (await getJson<{ stateHash: string }>(page, `/branches/${childId}/state?tick=120`)).stateHash;
  const second = await postJson<{ world: { id: string }; rootBranch: { id: string } }>(page, "/worlds", { name: "demo repro", seed: plan.seed });
  await apiRunTo(page, second.rootBranch.id, plan.forkTick);
  const fk = await postJson<{ id: string }>(page, `/branches/${second.rootBranch.id}/fork`, { atTick: plan.forkTick, label: "repro" });
  const cmds = plan.commands.map((c) =>
    c.kind === "place_purchase_order" ? { kind: c.kind, sku: c.skuIndex, role: "backup", qty: c.qty } : { kind: c.kind, qty: c.qty },
  );
  const sub = await postJson<{ rejected?: unknown[] }>(page, `/branches/${fk.id}/commands`, { windowTick: plan.forkTick, commands: cmds });
  expect(sub.rejected ?? []).toHaveLength(0);
  await apiRunTo(page, fk.id, 120);
  const secondHash = (await getJson<{ stateHash: string }>(page, `/branches/${fk.id}/state?tick=120`)).stateHash;
  expect(secondHash).toBe(shownHash);
  numbers["forkedHorizonHash"] = shownHash;
  numbers["forkedHorizonHashSecondRun"] = secondHash;
  numbers["rootHorizonHash"] = (await getJson<{ stateHash: string }>(page, `/branches/${rootId}/state?tick=120`)).stateHash;

  /* ================= 8. challenge the AI manager ================= */
  await page.goto("/briefing");
  await expect(page.getByText("You will play against the Rule-based AI.")).toBeVisible();
  await expect(page.getByRole("radio", { name: /LLM AI/ })).toHaveCount(0);
  await human(page);
  await page.getByRole("button", { name: "Challenge the AI manager" }).click();
  await page.waitForURL(/\/duels\/[0-9a-f-]{36}/);
  const duelId = new URL(page.url()).pathname.split("/").pop() ?? "";
  await setSpeed(page, "Fast");
  // Reach the demo fork hour by ending each window with no moves (the AI plays its own).
  for (let g = 0; g < 40 && (await headTick(page)) < plan.forkTick; g++) {
    if (await continueIfOpen(page)) await idle(page);
    else await playOnce(page);
  }
  expect(await headTick(page)).toBe(plan.forkTick);
  await openMoves(page);
  await enterPlan(page, false);
  await lockIn(page, false);
  await expect(page.getByTestId("accepted-command").first()).toContainText(/Takes effect at Friday, 5:00 PM/);
  await page.getByRole("tab", { name: "What happened" }).click();
  await expect(page.getByTestId("human-moves")).toContainText(/Ordered 339 .* from the backup supplier/);
  const decisions = await getJson<{ decisions: { provenance: string; windowTick: number }[] }>(page, `/duels/${duelId}/decisions`);
  expect(decisions.decisions.length).toBeGreaterThan(0);
  expect(new Set(decisions.decisions.map((d) => d.provenance))).toEqual(new Set(["ai_rule_based"]));
  await expect(page.getByTestId("ai-decision").first().getByTestId("provenance-badge")).toHaveText("Rule-based AI");
  await expect(page.getByTestId("ai-decision").first().getByTestId("ai-rationale")).toContainText("AI rationale (not verified)");
  // Midway the scoreboard says it is provisional (API and text agree).
  await page.getByRole("tab", { name: "Your move" }).click();
  await continueIfOpen(page);
  await idle(page);
  const mid = await getJson<Score>(page, `/duels/${duelId}/score`);
  expect(mid.provisional).toBe(true);
  await expect(page.getByTestId("scoreboard")).toContainText("Provisional");
  await shot(page, info, "demo-path-08-duel", true);

  for (let g = 0; g < 90 && (await headTick(page)) < 120; g++) {
    if (await continueIfOpen(page)) {
      await idle(page);
      continue;
    }
    await playOnce(page);
  }
  expect(await headTick(page)).toBe(120);
  await idle(page);
  await expect(page.getByTestId("syncing-indicator")).toHaveText("");

  /* ================= 9. final results ================= */
  const score = await getJson<Score>(page, `/duels/${duelId}/score`);
  expect(score.provisional).toBe(false);
  expect(score.comparisonTick).toBe(120);
  await expect(page.getByTestId("scoreboard")).not.toContainText("Provisional");
  await expect(page.getByTestId("outcome")).toHaveText(score.outcome === "human_wins" ? "You won" : score.outcome === "ai_wins" ? "AI won" : "Draw");
  await expect(page.getByTestId("score-human")).toHaveText(pts(score.human.score.compositeHundredths));
  await expect(page.getByTestId("score-ai")).toHaveText(pts(score.ai.score.compositeHundredths));
  const h = score.human.metrics;
  const ai = score.ai.metrics;
  await expect(page.getByTestId("raw-profit").locator("td")).toHaveText([money(h.netProfitCents), money(ai.netProfitCents), money(h.netProfitCents - ai.netProfitCents, true)]);
  await expect(page.getByTestId("raw-onTime").locator("td").nth(0)).toHaveText(percent(h.serviceLevelBps));
  await expect(page.getByTestId("raw-onTime").locator("td").nth(1)).toHaveText(percent(ai.serviceLevelBps));
  await expect(page.getByTestId("raw-waiting").locator("td").nth(0)).toHaveText(group(h.backlogOrders));
  await expect(page.getByTestId("raw-waiting").locator("td").nth(1)).toHaveText(group(ai.backlogOrders));
  await expect(page.getByTestId("duel-disclaimer")).toContainText("Synthetic simulation output under the stated assumptions.");
  await expect(page.getByTestId("llm-usage").or(page.getByTestId("scoreboard"))).toBeVisible();
  await page.getByTestId("scoreboard").scrollIntoViewIfNeeded();
  await shot(page, info, "demo-path-09-results", true);
  numbers["duel"] = {
    forkTickHumanPlan: plan.forkTick,
    comparisonTick: score.comparisonTick,
    outcome: score.outcome,
    humanCompositeHundredths: score.human.score.compositeHundredths,
    aiCompositeHundredths: score.ai.score.compositeHundredths,
    human: score.human.metrics,
    ai: score.ai.metrics,
    humanCancelled: score.human.cancelledOrders,
    aiCancelled: score.ai.cancelledOrders,
  };

  // Ledger drill-down from "Profit so far" on the human business of the duel world.
  const duel = await getJson<{ duel: { worldId: string } }>(page, `/duels/${duelId}`);
  await page.goto(`/worlds/${duel.duel.worldId}?branch=${score.human.branchId}`);
  await expect(page.getByTestId("hud-cash")).toBeVisible();
  await scrubTo(page, 120);
  const fig = ((await getJson<{ figures: { figure: string; sumCents: number; entryCount: number }[] }>(page, `/branches/${score.human.branchId}/ledger/summary?tick=120`)).figures).find((f) => f.figure === "net_profit")!;
  await page.getByTestId("hud-net-profit").click();
  await expect(page.getByTestId("ledger-drill").getByRole("heading", { name: "Profit so far" })).toBeVisible();
  const shown = (await page.getByTestId("ledger-sum-value").innerText()).replace(/[+,$]/g, "");
  const cents = Math.round(Number(shown) * 100);
  expect(cents, "ledger drill-down sum equals GET /ledger/summary").toBe(fig.sumCents);
  expect(cents).toBe(h.netProfitCents);
  await expect(page.getByTestId("ledger-count")).toContainText(`${fig.entryCount} entries`);
  await shot(page, info, "demo-path-09b-ledger", true);
  const exp = await getJson<{ inputs: { effectiveTick: number; kind: string; command?: { kind: string; sku: number; role: number; qty: number } }[] }>(page, `/branches/${score.human.branchId}/export`);
  numbers["humanInputs"] = exp.inputs.filter((i) => i.command !== undefined).map((i) => ({ effectiveTick: i.effectiveTick, ...i.command }));
  numbers["ledgerNetProfitCents"] = fig.sumCents;
  numbers["ledgerEntryCount"] = fig.entryCount;

  record("defaultPath", numbers);
  expect(mon.problems, mon.problems.join("\n")).toEqual([]);
});

/* ---------- phone screenshots for steps 1, 3, 5 and 9 (state set up through the API for speed) ---------- */

test("phone screenshots for steps 1, 3, 5 and 9", async ({ page }, info) => {
  test.skip(info.project.name !== "mobile", "mobile only");
  test.setTimeout(240_000);
  const mon = monitor(page);
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Parallel" })).toBeVisible();
  await page.waitForTimeout(2500);
  await shot(page, info, "demo-path-01-landing");

  await page.goto("/briefing");
  await page.getByRole("button", { name: "Start Black Friday demo" }).click();
  await page.waitForURL(/\/worlds\//);
  await expect(page.getByTestId("hud-cash")).toHaveText("$100,000.00");
  await page.waitForTimeout(1200);
  await shot(page, info, "demo-path-03-start");

  // Step 5: the decision cards at the demo fork hour.
  const world = worldIdOf(page);
  const root = branchIdOf(page);
  await apiRunTo(page, root, plan.forkTick);
  await page.goto(`/worlds/${world}?branch=${root}`);
  await expect(page.getByTestId("hud-cash")).toBeVisible();
  await openMoves(page);
  await expect(page.getByRole("heading", { name: "Your move", level: 2 })).toBeVisible();
  await enterPlan(page, false);
  await shot(page, info, "demo-path-05-decision");

  // Step 9: a finished duel's scoreboard.
  const created = await postJson<{ duel: { id: string } }>(page, "/duels", { forkTick: 0 });
  const id = created.duel.id;
  for (let g = 0; g < 90; g++) {
    const st = (await getJson<{ status: { humanHeadTick: number; finished: boolean; windowOpen: boolean } }>(page, `/duels/${id}`)).status;
    if (st.finished) break;
    if (st.windowOpen) await postJson(page, `/duels/${id}/decision-window/end`, { windowTick: st.humanHeadTick });
    await postJson(page, `/duels/${id}/advance`, { runToTick: 120 });
  }
  await page.goto(`/duels/${id}`);
  await expect(page.getByTestId("scoreboard")).toBeVisible();
  await expect(page.getByTestId("scoreboard")).not.toContainText("Provisional");
  await page.getByTestId("scoreboard").scrollIntoViewIfNeeded();
  await shot(page, info, "demo-path-09-results");
  expect(mon.problems.filter((p) => !/http 409/.test(p)), mon.problems.join("\n")).toEqual([]);
});
