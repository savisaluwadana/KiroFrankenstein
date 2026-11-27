import { expect, test, type Page } from "@playwright/test";
import {
  API,
  apiMetrics,
  branchIdOf,
  closeSheetOnPhone,
  continueIfOpen,
  headTick,
  monitor,
  money,
  openMoves,
  playOnce,
  setSpeed,
  shot,
  startDemo,
} from "./helpers";

/** (B) Ledger drill-down, the progress stream and its polling fallback, and injected surprises. */

interface SummaryFigure {
  figure: string;
  sumCents: number;
  entryCount: number;
}

async function summary(page: Page, branch: string, tick: number): Promise<SummaryFigure[]> {
  const r = await page.request.get(`${API}/branches/${branch}/ledger/summary?tick=${tick}`);
  expect(r.ok()).toBe(true);
  return ((await r.json()) as { figures: SummaryFigure[] }).figures;
}

/** "+$1,234.50", "-$3.00" or "$0.00" to integer cents. */
function cents(text: string): number {
  const m = /([+-])?\$([\d,]+)\.(\d\d)/.exec(text);
  if (m === null) throw new Error(`no money in "${text}"`);
  const v = Number((m[2] ?? "0").replace(/,/g, "")) * 100 + Number(m[3]);
  return m[1] === "-" ? -v : v;
}

/** Reads every page of the open drill-down and returns the "Adds" contributions and their count. */
async function sumListedEntries(page: Page): Promise<{ sum: number; count: number }> {
  let sum = 0;
  let count = 0;
  for (let guard = 0; guard < 80; guard++) {
    const rows = await page.getByTestId("ledger-entry").allInnerTexts();
    for (const t of rows) {
      const m = /Adds ([+-]?\$[\d,]+\.\d\d)/.exec(t);
      if (m === null) throw new Error(`entry without an Adds line: ${t}`);
      sum += cents(m[1] ?? "");
      count++;
    }
    const later = page.getByRole("button", { name: "Later", exact: true });
    if (await later.isDisabled()) break;
    await later.click();
    await expect(page.getByTestId("ledger-entry").first()).toBeVisible();
  }
  return { sum, count };
}

test("ledger drill-down: the displayed sum equals the API figure and the listed entries add up to it", async ({ page }, info) => {
  test.skip(info.project.name !== "desktop", "desktop only");
  const mon = monitor(page);
  await startDemo(page);
  const branch = branchIdOf(page);
  await openMoves(page);
  await continueIfOpen(page);
  await setSpeed(page, "Fast");
  await playOnce(page);
  expect(await headTick(page)).toBe(4);
  await expect(page.getByTestId("syncing-indicator")).toHaveText("");
  await closeSheetOnPhone(page);

  for (const [figure, testId, label] of [
    ["net_profit", "hud-net-profit", "Profit so far"],
    ["cash", "hud-cash", "Cash"],
  ] as const) {
    const apiFig = (await summary(page, branch, 4)).find((f) => f.figure === figure)!;
    await page.getByTestId(testId).click();
    const panel = page.getByTestId("ledger-panel");
    await expect(panel).toBeVisible();
    await expect(page.getByTestId("ledger-drill").getByRole("heading", { name: label })).toBeVisible();
    const shown = cents((await page.getByTestId("ledger-sum-value").innerText()) ?? "");
    expect(shown, `${label} shown in the panel`).toBe(apiFig.sumCents);
    expect(shown, `${label} equals the header`).toBe(cents(await page.getByTestId(testId).innerText()));
    await expect(page.getByTestId("ledger-count")).toContainText(`${apiFig.entryCount} entries`);
    await expect(page.getByTestId("ledger-disclaimer")).toContainText("Synthetic simulation output");
    if (figure === "net_profit") await shot(page, info, "12-ledger-drilldown", true);
    const listed = await sumListedEntries(page);
    expect(listed.count, "listed entries").toBe(apiFig.entryCount);
    expect(listed.sum, `${label}: sum of the listed 'Adds'`).toBe(apiFig.sumCents);
    await panel.getByRole("button", { name: "Close" }).click();
    await expect(panel).toHaveCount(0);
  }

  // The accounts table shows the API's per-account totals.
  await page.getByTestId("hud-cash").click();
  await page.getByRole("button", { name: "All accounts" }).click();
  const sum = (await (await page.request.get(`${API}/branches/${branch}/ledger/summary?tick=4`)).json()) as {
    accounts: { account: string; balanceCents: number }[];
  };
  const labor = sum.accounts.find((a) => a.account === "Labor_Expense")!;
  await expect(page.getByTestId("pnl-labor")).toHaveText(money(labor.balanceCents));
  expect(mon.problems, mon.problems.join("\n")).toEqual([]);
});

test("progress stream: the browser opens /progress and receives consecutive frames", async ({ page }, info) => {
  test.skip(info.project.name !== "desktop", "desktop only");
  await page.addInitScript(() => {
    const w = window as unknown as { __sse: { urls: string[]; ticks: number[]; opened: number; errors: number } };
    w.__sse = { urls: [], ticks: [], opened: 0, errors: 0 };
    const Native = window.EventSource;
    class Spy extends Native {
      constructor(url: string | URL, init?: EventSourceInit) {
        super(url, init);
        w.__sse.urls.push(String(url));
        this.addEventListener("open", () => (w.__sse.opened += 1));
        this.addEventListener("error", () => (w.__sse.errors += 1));
        this.addEventListener("tick", (e) => w.__sse.ticks.push((JSON.parse((e as MessageEvent<string>).data) as { tick: number }).tick));
      }
    }
    window.EventSource = Spy as unknown as typeof EventSource;
  });
  const progressReq = page.waitForRequest((r) => /\/branches\/[^/]+\/progress/.test(r.url()));
  await startDemo(page);
  const req = await progressReq;
  expect(req.url()).toContain(`${API}/branches/${branchIdOf(page)}/progress`);
  expect(req.resourceType()).toBe("eventsource");

  await page.getByRole("button", { name: "Engineering Mode" }).click();
  await expect(page.getByTestId("eng-progress-status")).toHaveText("Live");
  await openMoves(page);
  await continueIfOpen(page);
  await setSpeed(page, "Fast");
  await playOnce(page);
  await closeSheetOnPhone(page);
  await continueIfOpen(page);
  await playOnce(page);
  const head = await headTick(page);
  expect(head).toBe(8);

  await expect.poll(() => page.evaluate(() => (window as unknown as { __sse: { ticks: number[] } }).__sse.ticks.length), { timeout: 15_000 }).toBeGreaterThanOrEqual(8);
  const sse = await page.evaluate(() => (window as unknown as { __sse: { urls: string[]; ticks: number[]; opened: number; errors: number } }).__sse);
  expect(sse.opened).toBeGreaterThanOrEqual(1);
  expect(sse.errors).toBe(0);
  const ticks = sse.ticks;
  expect(ticks[0]).toBe(0); // the stream starts at fromTick=0
  for (let i = 1; i < ticks.length; i++) expect(ticks[i], `tick after ${ticks[i - 1]}`).toBe((ticks[i - 1] ?? 0) + 1);
  expect(ticks[ticks.length - 1]).toBeGreaterThanOrEqual(8);
});

test("blocking the progress stream falls back to polling and the display still advances without skipped hours", async ({ page }, info) => {
  test.skip(info.project.name !== "desktop", "desktop only");
  const mon = monitor(page);
  let aborted = 0;
  await page.route(/\/branches\/[^/]+\/progress/, (route) => {
    aborted++;
    return route.abort();
  });
  await startDemo(page);
  const branch = branchIdOf(page);
  await page.getByRole("button", { name: "Engineering Mode" }).click();
  // After the stream's retries are used up the consumer polls instead (about 3.5 s of backoff).
  await expect(page.getByTestId("eng-progress-status")).toHaveText("Polling", { timeout: 20_000 });
  expect(aborted).toBeGreaterThanOrEqual(3);

  const polls: string[] = [];
  page.on("request", (r) => {
    if (r.url().startsWith(`${API}/branches/${branch}/tick-frames`)) polls.push(r.url());
  });
  await openMoves(page);
  await continueIfOpen(page);
  await setSpeed(page, "Fast");
  await playOnce(page);
  await closeSheetOnPhone(page);
  await continueIfOpen(page);
  await playOnce(page);
  const head = await headTick(page);
  expect(head).toBe(8);

  await expect.poll(() => polls.length, { timeout: 10_000 }).toBeGreaterThan(0); // the fallback is really polling /tick-frames

  // Every hour 0..head can be looked at (none was skipped), and the figures match the live API.
  const m = await apiMetrics(page, branch, head);
  await expect(page.getByTestId("hud-cash")).toHaveText(money(m.cashCents));
  const slider = page.getByRole("slider", { name: "Displayed time" });
  await slider.focus();
  for (let t = head - 1; t >= 0; t--) {
    await page.keyboard.press("ArrowLeft");
    await expect(slider).toHaveAttribute("aria-valuenow", String(t));
    await expect(page.getByTestId("syncing-indicator")).toHaveText("");
  }
  await expect(page.getByTestId("eng-progress-status")).toHaveText("Polling");
  expect(mon.problems.filter((p) => !/progress|Failed to load resource/.test(p)), mon.problems.join("\n")).toEqual([]);
});

test("a surprise added from the studio shows on the time strip, survives a reload and happens when its hour arrives", async ({ page }, info) => {
  test.skip(info.project.name !== "desktop", "desktop only");
  const mon = monitor(page);
  await startDemo(page);
  const branch = branchIdOf(page);
  await openMoves(page);

  await page.getByRole("button", { name: "Add a surprise" }).click();
  const form = page.locator("#inject-disruption-form");
  await form.getByRole("combobox", { name: "What happens" }).selectOption({ label: await optionLabel(form, "What happens", /surge/i) });
  await form.getByRole("combobox", { name: "Starts at" }).selectOption({ value: "14" });
  await form.getByRole("textbox", { name: "Lasts (hours)" }).fill("6");
  await form.getByRole("combobox", { name: "Product" }).selectOption({ value: "3" }); // Watch bands
  await form.getByRole("textbox", { name: "Extra orders (%)" }).fill("100");
  await shot(page, info, "13-inject-surprise", true);
  await form.getByRole("button", { name: "Add this surprise" }).click();
  await expect(page.getByTestId("inject-confirmation")).toContainText(/Added: .* from Thursday, 2:00 PM for 6 hours\./);

  const marker = () => page.getByRole("button", { name: "Surprise you added, takes effect at Thursday, 2:00 PM" });
  await expect(marker()).toBeVisible();

  const exp = (await (await page.request.get(`${API}/branches/${branch}/export`)).json()) as {
    inputs: { kind: string; effectiveTick: number; provenance: string; disruption?: { kind: string; target: number; durationTicks: number; magnitudeBps: number } }[];
  };
  const inj = exp.inputs.filter((i) => i.kind === "disruption" && i.provenance === "injected");
  expect(inj).toHaveLength(1);
  expect(inj[0]).toMatchObject({ effectiveTick: 14, disruption: { kind: "demand_surge", target: 3, magnitudeBps: 10000 } });

  await page.reload();
  await expect(page.getByTestId("hud-cash")).toBeVisible();
  await expect(marker()).toBeVisible();

  // Nothing about it is announced before its hour.
  const card = page.getByTestId("crisis-card").filter({ hasText: /Orders are surging for Watch bands/ });
  await openMoves(page);
  await continueIfOpen(page);
  await setSpeed(page, "Fast");
  await playOnce(page);
  await closeSheetOnPhone(page);
  expect(await headTick(page)).toBeLessThan(14);
  await expect(card).toHaveCount(0);

  for (let guard = 0; guard < 8 && (await headTick(page)) < 14; guard++) {
    await continueIfOpen(page);
    await closeSheetOnPhone(page);
    await page.getByRole("button", { name: "Skip to next surprise" }).click();
    await expect.poll(async () => (await page.getByTestId("playback-status").textContent()) ?? "", { timeout: 30_000 }).not.toContain("working");
  }
  await continueIfOpen(page);
  expect(await headTick(page)).toBeGreaterThanOrEqual(14);
  await page.getByRole("slider", { name: "Displayed time" }).focus();
  while (Number(await page.getByRole("slider", { name: "Displayed time" }).getAttribute("aria-valuenow")) > 14) await page.keyboard.press("ArrowLeft");
  await expect(page.getByTestId("hud-clock")).toHaveText("Thursday, 2:00 PM");
  await expect(card).toBeVisible();
  expect(mon.problems, mon.problems.join("\n")).toEqual([]);
});

async function optionLabel(scope: ReturnType<Page["locator"]>, name: string, re: RegExp): Promise<string> {
  const texts = await scope.getByRole("combobox", { name }).locator("option").allInnerTexts();
  const hit = texts.find((t) => re.test(t));
  if (hit === undefined) throw new Error(`no option matching ${re} in ${texts.join(" | ")}`);
  return hit;
}
