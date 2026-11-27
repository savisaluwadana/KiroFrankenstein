import { expect, test, type Page } from "@playwright/test";
import { API, continueIfOpen, headTick, monitor, money, percent, playOnce, selectedTick, setSpeed, shot } from "./helpers";

/**
 * (C) The full Human versus Rule-based AI duel in a real browser. Every scoreboard number is compared with the live
 * API. The test never asserts who wins.
 */

const group = (n: number): string => String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
/** Hundredths of a point as "12.34" (independent of the production formatter). */
function pts(h: number): string {
  const a = Math.abs(h);
  return `${h < 0 ? "-" : ""}${Math.floor(a / 100)}.${String(a % 100).padStart(2, "0")}`;
}
const signedPts = (h: number): string => (h > 0 ? `+${pts(h)}` : pts(h));
const signedPct = (bps: number): string => (bps > 0 ? `+${percent(bps)}` : percent(bps));
const signedCount = (n: number): string => (n > 0 ? `+${group(n)}` : group(n));

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
  score: { financialBps: number; fulfilmentRateBps: number; onTimeBps: number; compositeHundredths: number };
  weighted: { financialHundredths: number; fulfilmentHundredths: number; onTimeHundredths: number };
  metrics: Metrics;
  cancelledOrders: number;
}
interface Score {
  comparisonTick: number;
  horizonTick: number;
  provisional: boolean;
  outcome: "human_wins" | "ai_wins" | "draw";
  human: Side;
  ai: Side;
  weights: { financialWeightBps: number; fulfilmentWeightBps: number; onTimeWeightBps: number };
}
interface FrameDisruption {
  kind: string;
  onsetTick: number;
  endTick: number;
  target: number;
  magnitudeBps: number;
}

async function getJson<T>(page: Page, path: string): Promise<T> {
  const r = await page.request.get(`${API}${path}`);
  expect(r.ok(), `${path} -> ${r.status()}`).toBe(true);
  return (await r.json()) as T;
}

async function idle(page: Page): Promise<void> {
  await expect
    .poll(async () => ((await page.getByTestId("playback-status").textContent()) ?? "").match(/Playing|working/) === null, { timeout: 60_000, intervals: [100] })
    .toBe(true);
}

test("duel: side by side, hidden AI move, and a scoreboard whose every number matches the API", async ({ page }, info) => {
  test.setTimeout(240_000);
  const phone = info.project.name === "mobile";
  const mon = monitor(page);

  /* ---------- start from the briefing ---------- */
  await page.goto("/briefing");
  await expect(page.getByText("You will play against the Rule-based AI.")).toBeVisible();
  await expect(page.getByRole("radio", { name: /LLM AI/ })).toHaveCount(0); // not configured on this server
  await page.getByRole("button", { name: "Challenge the AI manager" }).click();
  await page.waitForURL(/\/duels\/[0-9a-f-]{36}/);
  const duelId = new URL(page.url()).pathname.split("/").pop() ?? "";
  await expect(page.getByTestId("shared-tick")).toContainText("Both businesses are shown at Thursday, 12:00 AM.");

  // Both businesses at one tick (the phone shows one at a time behind a switcher).
  if (phone) {
    await expect(page.getByTestId("pane-human")).toBeVisible();
    await expect(page.getByRole("group", { name: "Which business to show" })).toBeVisible();
  } else {
    await expect(page.getByTestId("pane-human")).toBeVisible();
    await expect(page.getByTestId("pane-ai")).toBeVisible();
    await expect(page.getByTestId("pane-title-ai")).toHaveText("Rule-based AI");
    await expect(page.getByTestId("duel-panes").locator("canvas")).toHaveCount(2);
  }
  await expect(page.getByTestId("pane-human").getByTestId("pane-metrics")).toBeVisible();

  // The human's turn: the AI is waiting and its decision is hidden, in the UI and in the API.
  await expect(page.getByRole("heading", { name: "Your move", level: 2 })).toBeVisible();
  await expect(page.getByTestId("ai-status").first()).toContainText("waiting for you");
  const hidden = await getJson<{ decisions: unknown[] }>(page, `/duels/${duelId}/decisions`);
  expect(hidden.decisions, "AI decision withheld while the human window is open").toHaveLength(0);
  await shot(page, info, phone ? "14-duel" : "14-duel-start", true);

  /* ---------- the human decides: order backup stock, lock in, continue ---------- */
  const order = page.locator('section[aria-labelledby="order-card-title"]');
  await order.getByRole("combobox", { name: "Product", exact: true }).selectOption({ value: "1" });
  await order.getByRole("combobox", { name: "Supplier" }).selectOption({ value: "backup" });
  await order.getByRole("textbox", { name: "Quantity" }).fill("60");
  await order.getByRole("button", { name: "Add to my moves" }).click();
  await page.getByRole("button", { name: "Lock in decisions" }).click();
  await expect(page.getByTestId("accepted-command")).toContainText(/Takes effect at Thursday, 1:00 AM/);
  await setSpeed(page, "Fast");
  await continueIfOpen(page);
  await idle(page);
  expect(await headTick(page)).toBe(4);

  // After continuing, the AI's first decision is visible with plain provenance and labelled rationale.
  await page.getByRole("tab", { name: "What happened" }).click();
  const first = page.getByTestId("ai-decision").first();
  await expect(first).toBeVisible();
  await expect(first.getByTestId("provenance-badge")).toHaveText("Rule-based AI");
  await expect(first.getByTestId("ai-rationale")).toContainText("AI rationale (not verified)");
  const aiApi = await getJson<{ decisions: { windowTick: number; managerLabel: string; provenance: string; rationale: string }[] }>(page, `/duels/${duelId}/decisions`);
  expect(aiApi.decisions.length).toBeGreaterThan(0);
  expect(aiApi.decisions[0]).toMatchObject({ windowTick: 0, managerLabel: "Rule-based AI", provenance: "ai_rule_based" });
  if (aiApi.decisions[0]!.rationale !== "") await expect(first.getByTestId("ai-rationale")).toContainText(aiApi.decisions[0]!.rationale);
  // Hours 0..head are open windows only for the human at the head: the AI list never includes the open window.
  await expect(page.getByTestId("ai-decision")).toHaveCount(aiApi.decisions.filter((d) => d.windowTick < 4).length);
  await expect(page.getByTestId("human-moves")).toContainText(/Ordered 60 Chargers from the backup supplier/);
  await page.getByRole("tab", { name: "Your move" }).click();
  await shot(page, info, "15-duel-ai-decision", true);
  await page.getByRole("tab", { name: "What happened" }).click();
  await shot(page, info, "15b-duel-what-happened", true);
  await page.getByRole("tab", { name: "Your move" }).click();

  /* ---------- run both businesses to the end of the week, ending windows with no moves ---------- */
  for (let guard = 0; guard < 90 && (await headTick(page)) < 120; guard++) {
    if (await continueIfOpen(page)) {
      await idle(page);
      continue;
    }
    await playOnce(page);
  }
  expect(await headTick(page)).toBe(120);
  await idle(page);
  await expect(page.getByTestId("syncing-indicator")).toHaveText("");

  /* ---------- scoreboard against the API ---------- */
  const score = await getJson<Score>(page, `/duels/${duelId}/score`);
  expect(score.provisional).toBe(false);
  expect(score.comparisonTick).toBe(120);
  const sb = page.getByTestId("scoreboard");
  await expect(sb).toBeVisible();
  await expect(page.getByTestId("outcome")).toHaveText(score.outcome === "human_wins" ? "You won" : score.outcome === "ai_wins" ? "AI won" : "Draw");
  await expect(page.getByTestId("score-human")).toHaveText(pts(score.human.score.compositeHundredths));
  await expect(page.getByTestId("score-ai")).toHaveText(pts(score.ai.score.compositeHundredths));

  const parts = [
    ["financial", "Financial", score.weights.financialWeightBps, "financialHundredths", "financialBps"],
    ["fulfilment", "Orders fully shipped", score.weights.fulfilmentWeightBps, "fulfilmentHundredths", "fulfilmentRateBps"],
    ["onTime", "On time", score.weights.onTimeWeightBps, "onTimeHundredths", "onTimeBps"],
  ] as const;
  for (const [key, label, weight, wKey, bKey] of parts) {
    const row = page.getByTestId(`component-${key}`);
    await expect(row.locator("th")).toHaveText(`${label} (${weight / 100}%)`);
    await expect(row.locator("td").nth(0)).toHaveText(`${pts(score.human.weighted[wKey])} (${percent(score.human.score[bKey])})`);
    await expect(row.locator("td").nth(1)).toHaveText(`${pts(score.ai.weighted[wKey])} (${percent(score.ai.score[bKey])})`);
  }
  // The composite is the sum of its weighted parts (display check of the API's own numbers).
  for (const s of [score.human, score.ai]) {
    expect(s.weighted.financialHundredths + s.weighted.fulfilmentHundredths + s.weighted.onTimeHundredths).toBe(s.score.compositeHundredths);
  }

  const h = score.human.metrics;
  const a = score.ai.metrics;
  const raw: [string, string, string, string][] = [
    ["composite", pts(score.human.score.compositeHundredths), pts(score.ai.score.compositeHundredths), signedPts(score.human.score.compositeHundredths - score.ai.score.compositeHundredths)],
    ["cash", money(h.cashCents), money(a.cashCents), money(h.cashCents - a.cashCents, true)],
    ["profit", money(h.netProfitCents), money(a.netProfitCents), money(h.netProfitCents - a.netProfitCents, true)],
    ["onTime", percent(h.serviceLevelBps), percent(a.serviceLevelBps), signedPct(h.serviceLevelBps - a.serviceLevelBps)],
    ["shipped", percent(h.fillRateBps), percent(a.fillRateBps), signedPct(h.fillRateBps - a.fillRateBps)],
    ["waiting", group(h.backlogOrders), group(a.backlogOrders), signedCount(h.backlogOrders - a.backlogOrders)],
    ["cancelled", group(score.human.cancelledOrders), group(score.ai.cancelledOrders), signedCount(score.human.cancelledOrders - score.ai.cancelledOrders)],
  ];
  for (const [key, hv, av, diff] of raw) {
    await expect(page.getByTestId(`raw-${key}`).locator("td"), `raw ${key}`).toHaveText([hv, av, diff]);
  }

  // The score's raw metrics are the branches' own metrics at the comparison hour.
  for (const [side, id] of [
    [score.human, score.human.branchId],
    [score.ai, score.ai.branchId],
  ] as const) {
    const m = await getJson<{ metrics: Metrics & { cancelledOrders?: number } }>(page, `/branches/${id}/metrics?tick=${score.comparisonTick}`);
    expect(m.metrics).toMatchObject({
      cashCents: side.metrics.cashCents,
      netProfitCents: side.metrics.netProfitCents,
      serviceLevelBps: side.metrics.serviceLevelBps,
      fillRateBps: side.metrics.fillRateBps,
      backlogOrders: side.metrics.backlogOrders,
    });
  }
  await expect(page.getByTestId("score-time")).toContainText("Scored at");
  await expect(page.getByTestId("duel-disclaimer")).toContainText("Synthetic simulation output under the stated assumptions.");

  // The panes show the same API figures for the displayed hour.
  const shown = await selectedTick(page);
  for (const [key, id] of [
    ["human", score.human.branchId],
    ["ai", score.ai.branchId],
  ] as const) {
    if (phone && key === "ai") await page.getByRole("group", { name: "Which business to show" }).getByRole("button").nth(1).click();
    const m = (await getJson<{ metrics: Metrics }>(page, `/branches/${id}/metrics?tick=${shown}`)).metrics;
    await expect(page.getByTestId(`pane-${key}`).getByTestId("pane-metrics").locator("dd")).toHaveText([
      money(m.cashCents),
      money(m.netProfitCents),
      percent(m.serviceLevelBps),
      group(m.backlogOrders),
    ]);
  }
  if (phone) await page.getByRole("group", { name: "Which business to show" }).getByRole("button").nth(0).click();

  await sb.scrollIntoViewIfNeeded();
  await shot(page, info, "16-duel-scoreboard", true);

  /* ---------- fairness: identical surprises, identical history at the fork ---------- */
  type Frames = { frames: { tick: number; disruptions: (FrameDisruption & { inputSeq: number })[] }[] };
  const fh = await getJson<Frames>(page, `/branches/${score.human.branchId}/tick-frames?from=0&to=119`);
  const fa = await getJson<Frames>(page, `/branches/${score.ai.branchId}/tick-frames?from=0&to=119`);
  const norm = (f: Frames) =>
    f.frames.map((x) => ({ tick: x.tick, d: x.disruptions.map(({ kind, onsetTick, endTick, target, magnitudeBps }) => ({ kind, onsetTick, endTick, target, magnitudeBps })) }));
  expect(norm(fh)).toEqual(norm(fa));
  const onsets = new Map<number, string[]>();
  for (const x of norm(fh)) for (const d of x.d) if (d.onsetTick === x.tick) onsets.set(x.tick, [...(onsets.get(x.tick) ?? []), d.kind]);
  expect(onsets.size, "the flagship scenario has surprises").toBeGreaterThan(0);

  // In the browser, look at the first surprise hour: both businesses describe the same thing.
  const firstOnset = [...onsets.keys()].sort((x, y) => x - y)[0]!;
  const slider = page.getByRole("slider", { name: "Displayed time" });
  await slider.focus();
  await page.keyboard.press("Home");
  for (let t = 0; t < firstOnset; t++) await page.keyboard.press("ArrowRight");
  await expect(slider).toHaveAttribute("aria-valuenow", String(firstOnset));
  await expect(page.getByTestId("syncing-indicator")).toHaveText("");
  const label = async (side: "human" | "ai"): Promise<string> => {
    if (phone) await page.getByRole("group", { name: "Which business to show" }).getByRole("button").nth(side === "ai" ? 1 : 0).click();
    const img = page.getByTestId(`pane-${side}`).getByRole("img", { name: /orders waiting/ }).first();
    await expect(img).toHaveAccessibleName(/What is going wrong/);
    return img.evaluate((el) => (document.getElementById(el.getAttribute("aria-labelledby") ?? "")?.textContent ?? "").replace(/\s+/g, " "));
  };
  const going = (s: string): string => (/What is going wrong\s*(.*)$/.exec(s)?.[1] ?? "").trim();
  const lh = going(await label("human"));
  const la = going(await label("ai"));
  expect(lh, "a surprise is described at its onset hour").not.toBe("");
  expect(la).toBe(lh);

  // Engineering Mode: the two states are identical at the fork hour (hour 0).
  if (phone) await page.getByRole("group", { name: "Which business to show" }).getByRole("button").nth(0).click();
  await page.getByRole("button", { name: "Engineering Mode" }).click();
  await slider.focus();
  await page.keyboard.press("Home");
  await expect(slider).toHaveAttribute("aria-valuenow", "0");
  const eng = page.getByTestId("duel-engineering");
  await expect(eng).toContainText("State hash at tick 0: you");
  await expect(eng).not.toContainText("…", { timeout: 10_000 });
  const text = await eng.innerText();
  const hy = /you ([0-9a-f]{64})/.exec(text)?.[1];
  const ha = /AI ([0-9a-f]{64})/.exec(text)?.[1];
  expect(hy).toBeTruthy();
  expect(ha).toBe(hy);
  const sy = await getJson<{ stateHash: string }>(page, `/branches/${score.human.branchId}/state?tick=0`);
  const sa = await getJson<{ stateHash: string }>(page, `/branches/${score.ai.branchId}/state?tick=0`);
  expect(sy.stateHash).toBe(hy);
  expect(sa.stateHash).toBe(hy);
  await expect(page.getByTestId("llm-usage")).toContainText("LLM calls made 0");

  expect(mon.problems, mon.problems.join("\n")).toEqual([]);
});
