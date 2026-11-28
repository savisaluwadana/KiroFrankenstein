import { expect, test } from "@playwright/test";
import {
  API,
  apiMetrics,
  branchIdOf,
  canvasPainted,
  closeSheetOnPhone,
  continueIfOpen,
  headTick,
  monitor,
  money,
  openMoves,
  playOnce,
  playTo,
  scrubTo,
  selectedTick,
  setSpeed,
  shot,
  statusText,
  worldIdOf,
} from "./helpers";

const CRISIS_WORDS = /surge|outage|shortage|absence|supplier failure/i;

interface BranchRow {
  id: string;
  headTick: number;
  parentBranchId: string | null;
}

test("first-time journey on the real backend", async ({ page }, info) => {
  const mon = monitor(page);

  // 1. Landing: title, premise and call to action are visible without reading docs.
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Parallel" })).toBeVisible();
  await expect(page.getByText("Reality has no undo button. We built one.")).toBeVisible();
  await expect(page.getByText(/Run a warehouse through Black Friday week/)).toBeVisible();
  const cta = page.getByRole("link", { name: /Begin your shift/ });
  await expect(cta).toBeVisible();
  await page.waitForFunction(() => { const c = document.querySelector(".intro-cta"); return !!c && parseFloat(getComputedStyle(c).opacity) >= 0.99; }, undefined, { timeout: 15_000 }); // intro fade-in finished
  await shot(page, info, "01-intro");

  // 2. Briefing: no crisis is spoiled.
  await cta.click();
  await expect(page).toHaveURL(/\/briefing/);
  await expect(page.getByRole("heading", { name: "Black Friday Meltdown" })).toBeVisible();
  expect(await page.locator("main").innerText()).not.toMatch(CRISIS_WORDS);
  await shot(page, info, "02-briefing", true);

  // 3. Setup page screenshot (the customise flow itself is tested separately).
  await page.getByRole("link", { name: "Customize the business" }).click();
  await expect(page.getByRole("heading", { name: "Set up your business" })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Starting cash" })).toBeVisible();
  await shot(page, info, "03-setup", true);
  await page.goBack();

  // 4. Start the flagship demo. The warehouse canvas has real pixels.
  await page.getByRole("button", { name: "Start Black Friday demo" }).click();
  await page.waitForURL(/\/worlds\//);
  await expect(page.locator("canvas").first()).toBeVisible();
  await expect(page.getByTestId("hud-cash")).toHaveText("$100,000.00");
  await page.waitForTimeout(1200);
  const painted = await canvasPainted(page);
  expect(painted.distinct, "canvas has many distinct colours").toBeGreaterThan(12);
  expect(painted.nonBackground, "canvas has non-background pixels").toBeGreaterThan(500);
  await expect(page.getByTestId("hud-clock")).toHaveText("Thursday, 12:00 AM");
  await expect(page.locator("body")).not.toContainText(/\bTick\b/);
  expect(await page.locator("main").innerText(), "no hidden-schedule words at hour 0").not.toMatch(CRISIS_WORDS);
  await shot(page, info, "04-studio-start", true);

  // 5. The first decision moment is open; place a decision with the card, then continue.
  await openMoves(page);
  await expect(page.getByRole("heading", { name: "Your move", level: 2 })).toBeVisible();
  await shot(page, info, "06-decision-card", true);
  await page.getByRole("button", { name: "Add to my moves" }).first().click();
  await expect(page.getByTestId("pending-command")).toHaveCount(1);
  await page.getByRole("button", { name: "Lock in decisions" }).click();
  await expect(page.getByTestId("accepted-command")).toHaveCount(1);
  await expect(page.getByTestId("accepted-command")).toContainText(/Takes effect at Thursday, 1:00 AM/);
  await continueIfOpen(page);
  await closeSheetOnPhone(page);

  // 6. Play: it pauses by itself for the next move, and time reads in words.
  await setSpeed(page, "Fast");
  await playOnce(page);
  await expect(page.getByTestId("playback-status")).toContainText(/Paused for your move|something just happened/);
  const clock = (await page.getByTestId("hud-clock").textContent()) ?? "";
  expect(clock).toMatch(/^(Thursday|Friday|Saturday|Sunday|Monday), \d{1,2}:00 (AM|PM)$/);

  // 7. Advance to the crisis, pausing at every move.
  for (let guard = 0; guard < 12; guard++) {
    if ((await page.getByTestId("crisis-card").count()) > 0 || (await headTick(page)) >= 30) break;
    await continueIfOpen(page);
    await closeSheetOnPhone(page);
    await playOnce(page);
  }
  expect(await headTick(page)).toBeGreaterThan(8);
  await page.waitForTimeout(1500);
  await shot(page, info, "05-studio-crisis", true);
  // Hidden schedule: nothing about the plan or its reasons in normal mode.
  expect(await page.locator("main").innerText()).not.toMatch(
    /first scheduled decision window|crisis tick|Demo plan hint|stock-bound/i,
  );
  await expect(page.getByTestId("plan-hint")).toHaveCount(0);

  // 7b. Keep going to the supplier outage and capture it (skip to the next surprise, as a player would).
  const outage = page.getByTestId("crisis-card").filter({ hasText: /is down/ });
  for (let guard = 0; guard < 8 && (await outage.count()) === 0; guard++) {
    await continueIfOpen(page);
    await closeSheetOnPhone(page);
    const before = await headTick(page);
    await page.getByRole("button", { name: "Skip to next surprise" }).click();
    await expect.poll(async () => (await headTick(page)) > before || /Reached/.test(await statusText(page)), { timeout: 30_000 }).toBe(true);
    await expect(page.getByTestId("playback-status")).not.toContainText("working");
  }
  await expect.poll(() => outage.count(), { timeout: 10_000 }).toBeGreaterThan(0);
  await page.waitForTimeout(1500);
  if (info.project.name === "mobile") {
    await page.getByRole("tab", { name: /Alerts/ }).click();
    await page.locator("canvas").first().scrollIntoViewIfNeeded();
  }
  await shot(page, info, "05b-studio-supplier-outage", true);

  // 8. Rewind through the time strip: it is a read, no mutating request goes out.
  await closeSheetOnPhone(page);
  const apiBefore = mon.requests.length;
  await scrubTo(page, 8);
  await expect(page.getByTestId("timeline-readout")).toContainText("Showing Thursday, 8:00 AM");
  const rewindReqs = mon.requests.slice(apiBefore);
  expect(rewindReqs.filter((r) => r.method() !== "GET"), "rewind sends no POST").toHaveLength(0);
  expect(rewindReqs.filter((r) => /\/advance/.test(r.url()))).toHaveLength(0);
  await scrubTo(page, 4);

  // 9. Explore another future from the rewound hour: the split sequence plays.
  const rootBeforeFork = branchIdOf(page);
  await openMoves(page);
  await page.locator("#explore-future").click();
  const seq = page.getByTestId("fork-sequence");
  await expect(seq).toBeVisible();
  await expect(seq).toContainText("Same history up to Thursday, 4:00 AM");
  await page.waitForTimeout(500);
  await shot(page, info, "07-fork-sequence");
  await seq.getByRole("button", { name: /Skip/ }).click();
  await expect(seq).toHaveCount(0);

  // 10. Reality switcher lists both futures.
  const switcher = page.getByRole("group", { name: "Which future you are looking at" });
  await expect(switcher.getByRole("button")).toHaveCount(2);
  await expect(switcher.getByRole("button", { name: /Original path/ })).toBeVisible();
  await expect(switcher.getByRole("button", { name: /Original path/ })).toHaveAttribute("aria-pressed", "false");
  await page.waitForURL((u) => u.searchParams.get("branch") !== rootBeforeFork);
  const altBranch = branchIdOf(page);

  // 11. Advance the alternative a little, then compare side by side.
  await closeSheetOnPhone(page);
  await playTo(page, 12);
  await page.getByRole("button", { name: "Compare futures" }).click();
  const cmp = page.getByTestId("comparison-view");
  await expect(cmp).toBeVisible();
  const raw = await (await page.request.get(`${API}/worlds/${worldIdOf(page)}/branches`)).json();
  const list: BranchRow[] = Array.isArray(raw) ? raw : (raw.branches ?? raw.items ?? []);
  expect(list).toHaveLength(2);
  const root = list.find((b) => b.parentBranchId === null)!;
  const alt = list.find((b) => b.id === altBranch)!;
  const sel = await selectedTick(page);
  const common = Math.max(0, Math.min(sel, root.headTick, alt.headTick));
  await expect(cmp.getByRole("heading", { level: 3, name: /Observed differences at/ })).toBeVisible();
  await expect(page.getByTestId("diff-netProfit")).toBeVisible();
  const a = await apiMetrics(page, root.id, common);
  const b = await apiMetrics(page, alt.id, common);
  await expect(page.getByTestId("diff-cash").locator("td").nth(2)).toHaveText(money(b.cashCents - a.cashCents, true));
  if (info.project.name === "desktop") {
    await expect(page.getByTestId("comparison-panes").locator("canvas")).toHaveCount(2);
    for (const i of [0, 1]) expect((await canvasPainted(page, i)).distinct).toBeGreaterThan(12);
  }
  await closeSheetOnPhone(page);
  await page.waitForTimeout(800);
  await shot(page, info, "08-comparison", true);

  // 12. Engineering Mode: tick number, state hash and ledger show up.
  await page.getByRole("button", { name: "Back to one view" }).click();
  await page.getByRole("button", { name: "Engineering Mode" }).click();
  await expect(page.getByTestId("eng-tick")).toHaveText(String(await selectedTick(page)));
  await expect(page.getByTestId("eng-hash")).toHaveText(/^[0-9a-f]{64}$/);
  await expect(page.getByTestId("hud-clock")).toContainText(/tick \d+/);
  await page.waitForTimeout(500);
  await shot(page, info, "10-engineering-mode", true);
  await page.getByRole("tab", { name: "Ledger" }).click();
  await expect(page.getByText(/Every figure is the sum of these records/)).toBeVisible();
  await expect(page.getByRole("tab", { name: "Plan" })).toBeVisible();
  await page.getByRole("button", { name: "Engineering Mode" }).click();
  await expect(page.getByTestId("eng-hash")).toHaveCount(0);

  expect(mon.problems, mon.problems.join("\n")).toEqual([]);
});
