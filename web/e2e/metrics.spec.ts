import { expect, test, type Page } from "@playwright/test";
import {
  API, apiMetrics, branchIdOf, headTick, monitor, money, openMoves, orders, percent, playTo, points, scrubTo,
  selectedTick, shot, startDemo, worldIdOf, closeSheetOnPhone,
} from "./helpers";

const group = (n: number): string => String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ",");

async function checkHeader(page: Page, branchId: string, tick: number): Promise<void> {
  const m = await apiMetrics(page, branchId, tick);
  await expect(page.getByTestId("hud-cash"), `cash @${tick}`).toHaveText(money(m.cashCents));
  await expect(page.getByTestId("hud-net-profit"), `profit @${tick}`).toHaveText(money(m.netProfitCents, true));
  await expect(page.getByTestId("hud-service-level"), `on time @${tick}`).toHaveText(percent(m.serviceLevelBps));
  await expect(page.getByTestId("hud-backlog"), `waiting @${tick}`).toHaveText(orders(m.backlogOrders));
}

test("header figures equal the live API at several hours, and comparisons equal the display subtraction", async ({ page }, info) => {
  test.setTimeout(300_000); // healthy runs take about 1.6 min (real-time playback to hour 120)
  const mon = monitor(page);
  await startDemo(page);
  const root = branchIdOf(page);
  await page.waitForTimeout(800);

  // Hour 0, then hours reached by real playback.
  await checkHeader(page, root, 0);
  const checked: number[] = [0];
  for (const target of [20, 48, 60, 120]) {
    await playTo(page, target);
    const head = await headTick(page);
    // The figures shown at the head right after playback pauses.
    await expect(page.getByTestId("syncing-indicator")).toHaveText("");
    await checkHeader(page, root, await selectedTick(page));
    // The exact hour, reached by rewinding with the time strip.
    await closeSheetOnPhone(page);
    await scrubTo(page, target);
    await checkHeader(page, root, target);
    checked.push(target);
    expect(head).toBeGreaterThanOrEqual(target);
  }
  expect(checked).toEqual([0, 20, 48, 60, 120]);
  await shot(page, info, "09-outcome", true);

  // Fork at hour 60, advance the alternative to hour 72, compare.
  await scrubTo(page, 60);
  await openMoves(page);
  await page.locator("#explore-future").click();
  await page.getByTestId("fork-sequence").getByRole("button", { name: /Skip/ }).click();
  // The studio switches to the new reality (and its URL) once its frames are loaded.
  await page.waitForURL((u) => u.searchParams.get("branch") !== root);
  const alt = branchIdOf(page);
  await closeSheetOnPhone(page);
  await playTo(page, 72);
  await checkHeader(page, alt, await selectedTick(page));
  await page.getByRole("button", { name: "Compare futures" }).click();
  await expect(page.getByTestId("comparison-summary")).toBeVisible();

  const raw = await (await page.request.get(`${API}/worlds/${worldIdOf(page)}/branches`)).json();
  const list: { id: string; headTick: number; parentBranchId: string | null }[] = Array.isArray(raw) ? raw : (raw.branches ?? raw.items);
  const rootRow = list.find((b) => b.parentBranchId === null)!;
  const altRow = list.find((b) => b.id === alt)!;
  const common = Math.max(0, Math.min(await selectedTick(page), rootRow.headTick, altRow.headTick));
  const a = await apiMetrics(page, rootRow.id, common); // original path (left column)
  const b = await apiMetrics(page, altRow.id, common);

  const row = (key: string) => page.getByTestId(`diff-${key}`).locator("td");
  await expect(row("cash")).toHaveText([money(a.cashCents), money(b.cashCents), money(b.cashCents - a.cashCents, true)]);
  await expect(row("netProfit")).toHaveText([money(a.netProfitCents), money(b.netProfitCents), money(b.netProfitCents - a.netProfitCents, true)]);
  await expect(row("serviceLevel")).toHaveText([percent(a.serviceLevelBps), percent(b.serviceLevelBps), points(b.serviceLevelBps - a.serviceLevelBps)]);
  const d = b.backlogOrders - a.backlogOrders;
  await expect(row("backlog")).toHaveText([group(a.backlogOrders), group(b.backlogOrders), d > 0 ? `+${group(d)}` : group(d)]);

  // Both panes show the same hour's figures from the API (desktop shows both; phone shows one at a time).
  if (info.project.name === "desktop") {
    const metricsOf = (id: string) => page.getByTestId(`pane-${id}`).getByTestId("pane-metrics").locator("dd");
    // Pane figures are for the selected hour; they match the API when both are simulated that far.
    const sel = await selectedTick(page);
    if (sel === common) {
      await expect(metricsOf(rootRow.id)).toHaveText([money(a.cashCents), money(a.netProfitCents), percent(a.serviceLevelBps), group(a.backlogOrders)]);
      await expect(metricsOf(altRow.id)).toHaveText([money(b.cashCents), money(b.netProfitCents), percent(b.serviceLevelBps), group(b.backlogOrders)]);
    }
  }
  expect(mon.problems, mon.problems.join("\n")).toEqual([]);
});
