import { expect, test, type Page } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import { API, branchIdOf, closeSheetOnPhone, continueIfOpen, headTick, monitor, money, openMoves, percent, playTo, setSpeed, shot, startDemo, worldIdOf } from "./helpers";

/**
 * (M6) "What changed?" in a real browser against the real backend. The demo plan from
 * fixtures/demo-plan.json is entered through the cards in a forked future; both futures
 * are advanced; every number and statement in the panel is compared with POST /diffs and
 * the branch export. The test never asserts which future is better.
 */

interface Plan {
  forkTick: number;
  commands: { kind: string; skuIndex: number; role?: string; qty: number }[];
}
const plan = JSON.parse(fs.readFileSync(path.resolve(process.cwd(), "e2e/fixtures/demo-plan.json"), "utf8")) as Plan;
const order = plan.commands.find((c) => c.kind === "place_purchase_order");
const hire = plan.commands.find((c) => c.kind === "hire_temp_workers");
if (order === undefined || hire === undefined) throw new Error("demo plan fixture lacks the backup order or the hire");

interface Metric {
  key: string;
  a: number;
  b: number;
  delta: number;
}
interface DiffInputApi {
  side: "A" | "B";
  seq: number;
  effectiveTick: number;
  branchId: string;
  command?: { kind: string; sku: number; role: number; qty: number };
}
interface DiffApi {
  branchA: string;
  branchB: string;
  comparisonTick: number;
  divergence: { tick: number; stateHashA: string; stateHashB: string; components: { name: string }[] } | null;
  differingInputs: DiffInputApi[];
  metrics: Metric[];
  statement: string;
}
interface ExportApi {
  inputs: { effectiveTick: number; seq: number; kind: string; command?: { kind: string; sku: number; role: number; qty: number } }[];
}

const group = (n: number): string => String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
const DAYS = ["Thursday", "Friday", "Saturday", "Sunday", "Monday"];
/** Independent of production code: Thursday 12:00 AM plus `tick` hours. */
function clock(tick: number): string {
  const day = DAYS[Math.floor(tick / 24)] ?? "Later";
  const h = tick % 24;
  return `${day}, ${h % 12 === 0 ? 12 : h % 12}:00 ${h < 12 ? "AM" : "PM"}`;
}
const signed = (n: number): string => (n > 0 ? `+${group(n)}` : group(n));
function bpsPoints(bps: number): string {
  const body = percent(Math.abs(bps)).replace("%", "");
  return `${bps > 0 ? "+" : bps < 0 ? "-" : ""}${body} points`;
}
const KIND: Record<string, "money" | "bps" | "count"> = {
  net_profit: "money",
  cash: "money",
  service_level: "bps",
  fill_rate: "bps",
  backlog_orders: "count",
  backlog_units: "count",
  late_backlog_orders: "count",
  cancelled_orders: "count",
};

async function idleStatus(page: Page): Promise<void> {
  await expect
    .poll(async () => ((await page.getByTestId("playback-status").textContent()) ?? "").match(/Playing|working/) === null, { timeout: 60_000, intervals: [100] })
    .toBe(true);
}

test("What changed? matches the diff API, the export and the timeline marker", async ({ page }, info) => {
  test.skip(info.project.name !== "desktop" && info.project.name !== "mobile", "unknown project");
  test.setTimeout(300_000);
  const mon = monitor(page);
  await startDemo(page);
  const world = worldIdOf(page);
  const rootId = branchIdOf(page);
  await setSpeed(page, "Fast");

  /* ---- play the original path to the demo fork window, then explore another future ---- */
  await playTo(page, plan.forkTick);
  expect(await headTick(page)).toBe(plan.forkTick);
  await openMoves(page);
  await page.locator("#explore-future").click();
  const seq = page.getByTestId("fork-sequence");
  await expect(seq).toBeVisible();
  await seq.getByRole("button", { name: /Skip/ }).click();
  await expect(seq).toHaveCount(0);
  await page.waitForURL((u) => u.searchParams.get("branch") !== rootId);
  const childId = branchIdOf(page);

  /* ---- enter the demo plan's two moves with the cards in the fork window ---- */
  await openMoves(page);
  const orderCard = page.locator('section[aria-labelledby="order-card-title"]');
  await orderCard.getByRole("combobox", { name: "Product", exact: true }).selectOption({ value: String(order.skuIndex) });
  await orderCard.getByRole("combobox", { name: "Supplier" }).selectOption({ value: "backup" });
  await orderCard.getByRole("textbox", { name: "Quantity" }).fill(String(order.qty));
  await orderCard.getByRole("button", { name: "Add to my moves" }).click();
  const hireCard = page.locator('section[aria-labelledby="hire-card-title"]');
  await hireCard.getByRole("textbox", { name: "Temporary workers" }).fill(String(hire.qty));
  await hireCard.getByRole("button", { name: "Add to my moves" }).click();
  await expect(page.getByTestId("pending-command")).toHaveCount(2);
  await page.getByRole("button", { name: "Lock in decisions" }).click();
  await expect(page.getByTestId("accepted-command")).toHaveCount(2);
  await continueIfOpen(page);
  await closeSheetOnPhone(page);

  /* ---- advance both futures to a later hour ---- */
  const target = plan.forkTick + 20;
  await playTo(page, target);
  const switcher = page.getByRole("group", { name: "Which future you are looking at" });
  await switcher.getByRole("button", { name: /Original path/ }).click();
  await page.waitForURL((u) => u.searchParams.get("branch") === rootId);
  await expect(page.getByText("Showing: Original path")).toBeVisible();
  await expect.poll(() => headTick(page)).toBe(plan.forkTick);
  await expect(page.getByTestId("syncing-indicator")).toHaveText("");
  await playTo(page, target);

  /* ---- compare, then ask what changed ---- */
  await closeSheetOnPhone(page);
  await page.getByRole("button", { name: "Compare futures" }).click();
  await expect(page.getByTestId("comparison-view")).toBeVisible();
  const diffReq = page.waitForRequest((r) => r.method() === "POST" && r.url() === `${API}/diffs`);
  await page.getByTestId("what-changed").click();
  const sent = JSON.parse((await diffReq).postData() ?? "{}") as { branchA: string; branchB: string };
  expect(new Set([sent.branchA, sent.branchB])).toEqual(new Set([rootId, childId]));
  const panel = page.getByTestId("diff-panel");
  await expect(panel).toBeVisible();
  await expect(panel.getByTestId("diff-statement")).toBeVisible();

  const apiRes = await page.request.post(`${API}/diffs`, { data: { branchA: sent.branchA, branchB: sent.branchB } });
  expect(apiRes.ok()).toBe(true);
  const diff = (await apiRes.json()) as DiffApi;
  expect(diff.divergence, "the two futures diverge").not.toBeNull();
  const div = diff.divergence!;
  expect(div.tick, "no divergence before the first different Input takes effect").toBeGreaterThanOrEqual(plan.forkTick + 1);

  // First divergence: link text equals the API tick, clicking it selects that hour.
  const link = panel.getByTestId("diff-divergence-link");
  await expect(link).toHaveText(clock(div.tick));
  await link.click();
  await expect(page.getByRole("slider", { name: "Displayed time" })).toHaveAttribute("aria-valuenow", String(div.tick));

  // Differing decisions equal the API list, and the API list equals the export difference.
  const exportOf = async (id: string): Promise<ExportApi> => (await (await page.request.get(`${API}/branches/${id}/export`)).json()) as ExportApi;
  const [expRoot, expChild] = [await exportOf(rootId), await exportOf(childId)];
  const sig = (i: ExportApi["inputs"][number]): string => `${i.effectiveTick}|${JSON.stringify(i.command ?? null)}`;
  const rootSigs = new Set(expRoot.inputs.map(sig));
  const onlyChild = expChild.inputs.filter((i) => !rootSigs.has(sig(i)));
  const onlyChildCmds = onlyChild.map((i) => i.command).filter((c): c is NonNullable<typeof c> => c !== undefined);
  expect(onlyChildCmds.map((c) => c.kind).sort()).toEqual(["hire_temp_workers", "place_purchase_order"]);
  expect(onlyChildCmds.find((c) => c.kind === "place_purchase_order")).toMatchObject({ sku: order.skuIndex, role: 1, qty: order.qty });
  expect(onlyChildCmds.find((c) => c.kind === "hire_temp_workers")).toMatchObject({ qty: hire.qty });
  expect(diff.differingInputs).toHaveLength(onlyChild.length);
  expect(diff.differingInputs.map((i) => i.branchId)).toEqual(onlyChild.map(() => childId));
  const items = panel.getByTestId("diff-input");
  await expect(items).toHaveCount(2);
  const text = (await items.allInnerTexts()).join("\n");
  expect(text).toMatch(new RegExp(`Ordered ${group(order.qty)} .* from the backup supplier`));
  expect(text).toMatch(new RegExp(`Brought in ${hire.qty} temporary workers`));
  for (const i of diff.differingInputs) expect(text).toContain(`Input #${i.seq}, effective hour ${i.effectiveTick}`);

  // Figures: A, B and B minus A equal the API values (display subtraction only).
  expect(diff.metrics.length).toBeGreaterThan(3);
  for (const m of diff.metrics) {
    const kind = KIND[m.key];
    if (kind === undefined) continue;
    expect(m.delta, `${m.key}: server delta is b - a`).toBe(m.b - m.a);
    const cells = panel.getByTestId(`diff-metric-${m.key}`).locator("td");
    const fmt = (n: number): string => (kind === "money" ? money(n) : kind === "bps" ? percent(n) : group(n));
    await expect(cells.nth(0)).toHaveText(fmt(m.a));
    await expect(cells.nth(1)).toHaveText(fmt(m.b));
    await expect(cells.nth(2)).toHaveText(kind === "money" ? money(m.delta, true) : kind === "bps" ? bpsPoints(m.delta) : signed(m.delta));
  }
  await expect(panel.getByRole("heading", { name: "Financial" })).toBeVisible();
  await expect(panel.getByRole("heading", { name: "Service level" })).toBeVisible();
  await expect(panel.getByRole("heading", { name: "Backlog" })).toBeVisible();

  // The causality statement is verbatim, and the panel never claims a cause.
  await expect(panel.getByTestId("diff-statement")).toHaveText(diff.statement);
  const panelText = await panel.innerText();
  expect(panelText).not.toMatch(/because|caused by/i);
  expect(panelText).toContain("Compared at " + clock(diff.comparisonTick));

  // Normal mode hides internal references; Engineering Mode offers them.
  expect(panelText).not.toContain(div.stateHashA);
  await page.getByRole("button", { name: "Engineering Mode" }).click();
  await panel.getByRole("button", { name: "Show technical references" }).click();
  const tech = panel.getByTestId("diff-tech");
  await expect(tech).toContainText(`first divergence tick ${div.tick}`);
  await expect(tech).toContainText(div.stateHashA);
  await expect(tech).toContainText(div.stateHashB);
  await expect(tech).toContainText(`comparison tick ${diff.comparisonTick}`);
  await expect(tech).toContainText(diff.branchA);
  await expect(tech).toContainText(diff.branchB);
  await expect(panel.getByTestId("diff-input-tech").first()).toContainText("provenance");
  await page.getByRole("button", { name: "Engineering Mode" }).click();

  // The time strip shows the divergence marker at the same hour.
  const marker = page.locator('[data-marker="divergence"]');
  await expect(marker).toHaveCount(1);
  await expect(marker).toHaveAttribute("aria-label", `The two futures first look different at ${clock(div.tick)}`);
  await expect(page.getByTestId("timeline-readout")).toContainText(`The two futures first look different at ${clock(div.tick)}`);

  await panel.scrollIntoViewIfNeeded();
  await page.waitForTimeout(600);
  await shot(page, info, "18-diff", true);
  expect(world).toMatch(/^[0-9a-f-]{36}$/);
  await idleStatus(page);
  expect(mon.problems, mon.problems.join("\n")).toEqual([]);
});
