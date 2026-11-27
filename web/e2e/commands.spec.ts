import { expect, test, type Locator, type Page } from "@playwright/test";
import {
  API,
  branchIdOf,
  closeSheetOnPhone,
  continueIfOpen,
  headTick,
  monitor,
  openMoves,
  playOnce,
  setSpeed,
  shot,
  startDemo,
  worldIdOf,
} from "./helpers";

/**
 * (A) Every management command through its contextual card, in a real browser against the real
 * backend. Afterwards the recorded inputs and the ledger are read from the live API and compared.
 */

interface ExportedInput {
  effectiveTick: number;
  seq: number;
  kind: string;
  command: { kind: string; sku: number; role: number; qty: number; amountCents: number; poRef: number };
}
interface LedgerEntry {
  tick: number;
  cause: { kind: string; ref: number };
  lines: { account: string; debitCents: number; creditCents: number }[];
}

const card = (page: Page, titleId: string): Locator => page.locator(`section[aria-labelledby="${titleId}"]`);

async function get<T>(page: Page, path: string): Promise<T> {
  const r = await page.request.get(`${API}${path}`);
  expect(r.ok(), `${path} -> ${r.status()}`).toBe(true);
  return (await r.json()) as T;
}

/** Adds one move with the card's own button, locks it in, and returns once the accepted line shows. */
async function addAndLock(page: Page, section: Locator, expectedAccepted: number, effect: RegExp): Promise<void> {
  await section.getByRole("button", { name: "Add to my moves" }).click();
  await expect(page.getByTestId("pending-command")).toHaveCount(1);
  await page.getByRole("button", { name: "Lock in decisions" }).click();
  await expect(page.getByTestId("accepted-command")).toHaveCount(expectedAccepted);
  await expect(page.getByTestId("accepted-command").last()).toContainText(effect);
  await expect(page.getByTestId("pending-command")).toHaveCount(0);
}

test("every management command works through its card and is recorded and booked by the backend", async ({ page }, info) => {
  test.skip(info.project.name !== "desktop", "desktop only; the phone layout is covered by the journey spec");
  const mon = monitor(page);
  await startDemo(page);
  const branch = branchIdOf(page);
  const world = worldIdOf(page);
  await openMoves(page);
  await expect(page.getByRole("heading", { name: "Your move", level: 2 })).toBeVisible();

  const w = await get<{ world?: { assumptions: { path: string; value: number }[] }; assumptions?: { path: string; value: number }[] }>(page, `/worlds/${world}`);
  const assumptions = w.world?.assumptions ?? w.assumptions ?? [];
  const av = (p: string): number => {
    const a = assumptions.find((x) => x.path === p);
    if (a === undefined) throw new Error(`no assumption ${p}`);
    return a.value;
  };

  /* ---------- window at hour 0: order, price, overtime, hire, marketing ---------- */
  const effect1 = /Takes effect at Thursday, 1:00 AM/;

  const order = card(page, "order-card-title");
  await order.getByRole("combobox", { name: "Product", exact: true }).selectOption({ value: "1" }); // charger
  await order.getByRole("combobox", { name: "Supplier" }).selectOption({ value: "backup" });
  await order.getByRole("textbox", { name: "Quantity" }).fill("50");
  await expect(page.getByTestId("estimated-cost")).toHaveText("$525.00"); // 50 x $10.50, the server's backup unit cost
  await addAndLock(page, order, 1, effect1);
  await expect(page.getByTestId("accepted-command").first()).toContainText(/arriving in about 8 hours/);

  const price = card(page, "price-card-title");
  await price.getByRole("combobox", { name: "Product to reprice" }).selectOption({ value: "0" });
  await price.getByRole("textbox", { name: "New price (USD)" }).fill("55.00");
  await addAndLock(page, price, 2, effect1);

  const overtime = card(page, "overtime-card-title");
  await overtime.getByRole("textbox", { name: "Overtime hours per worker" }).fill("2");
  const otExpected = av("workforce.regularWorkers") * 2 * av("wages.overtimePerHourCents");
  await expect(page.getByTestId("overtime-estimate")).toHaveText(`$${(otExpected / 100).toLocaleString("en-US", { minimumFractionDigits: 2 })}`);
  await addAndLock(page, overtime, 3, effect1);

  const hire = card(page, "hire-card-title");
  await hire.getByRole("textbox", { name: "Temporary workers" }).fill("2");
  await addAndLock(page, hire, 4, effect1);

  const marketing = card(page, "marketing-card-title");
  await marketing.getByRole("combobox", { name: "Product to promote" }).selectOption({ value: "1" });
  await marketing.getByRole("textbox", { name: "Spend per hour (USD)" }).fill("25");
  await expect(page.getByTestId("marketing-estimate")).toHaveText("$25.00");
  await hire.scrollIntoViewIfNeeded();
  await shot(page, info, "11-commands", true);
  await addAndLock(page, marketing, 5, effect1);
  await expect(page.getByTestId("obs-budget")).toContainText(/0/);

  /* ---------- reach the next decision window (hour 4) ---------- */
  await closeSheetOnPhone(page);
  await continueIfOpen(page);
  await setSpeed(page, "Fast");
  await playOnce(page);
  expect(await headTick(page)).toBe(4);
  await openMoves(page);

  /* ---------- window at hour 4: rush the open order, then rejections ---------- */
  const rush = card(page, "rush-card-title");
  await expect(rush.getByRole("combobox", { name: "Delivery to rush" })).toContainText(/50 Chargers from the backup supplier, due/);
  await expect(page.getByTestId("rush-fee")).toHaveText("$50.00");
  await addAndLock(page, rush, 1, /Takes effect at Thursday, 5:00 AM/);
  await expect(page.getByTestId("accepted-command").last()).toContainText(/Rushed the delivery of 50/);

  // Server-side rejection: a price outside the range passes the form and is refused by the validator.
  const price2 = card(page, "price-card-title");
  await price2.getByRole("combobox", { name: "Product to reprice" }).selectOption({ value: "1" });
  await price2.getByRole("textbox", { name: "New price (USD)" }).fill("999.00");
  await price2.getByRole("button", { name: "Add to my moves" }).click();
  await page.getByRole("button", { name: "Lock in decisions" }).click();
  const priceReject = price2.getByTestId("card-rejection");
  await expect(priceReject).toHaveText("That price is outside the allowed range.");
  await expect(priceReject).not.toContainText(/price_out_of_bounds|cents/);

  // Two hires that each fit alone but not together: the second is refused next to the staffing card.
  const hire2 = card(page, "hire-card-title");
  const stepper = hire2.getByRole("textbox", { name: "Temporary workers" });
  await stepper.fill("20");
  await hire2.getByRole("button", { name: "Add to my moves" }).click();
  await hire2.getByRole("button", { name: "Add to my moves" }).click();
  await page.getByRole("button", { name: "Lock in decisions" }).click();
  await expect(hire2.getByTestId("card-rejection")).toHaveText("That would be more temporary workers than the business allows.");
  await expect(page.getByTestId("accepted-command").filter({ hasText: /Hired 20 temporary workers/ })).toHaveCount(1);

  // Client-side limit: marketing above the business limit is explained before anything is sent.
  const marketing2 = card(page, "marketing-card-title");
  await marketing2.getByRole("textbox", { name: "Spend per hour (USD)" }).fill("9999");
  await marketing2.getByRole("button", { name: "Add to my moves" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "That is more marketing spend per hour than the business allows." })).toBeVisible();

  /* ---------- advance to hour 8 and compare against the live API ---------- */
  await closeSheetOnPhone(page);
  await continueIfOpen(page);
  await playOnce(page);
  expect(await headTick(page)).toBe(8);

  const exp = await get<{ inputs: ExportedInput[] }>(page, `/branches/${branch}/export`);
  const cmds = exp.inputs.filter((i) => i.kind === "command");
  const byKind = (k: string) => cmds.filter((c) => c.command.kind === k);
  expect(byKind("place_purchase_order")).toHaveLength(1);
  expect(byKind("place_purchase_order")[0]).toMatchObject({ effectiveTick: 1, command: { sku: 1, role: 1, qty: 50 } });
  expect(byKind("set_price")).toHaveLength(1); // the 999.00 attempt is not recorded
  expect(byKind("set_price")[0]).toMatchObject({ effectiveTick: 1, command: { sku: 0, amountCents: 5500 } });
  expect(byKind("schedule_overtime")[0]).toMatchObject({ effectiveTick: 1, command: { qty: 2 } });
  expect(byKind("hire_temp_workers").map((c) => c.command.qty)).toEqual([2, 20]); // the second 20 was refused
  expect(byKind("set_marketing_spend")).toHaveLength(1);
  expect(byKind("set_marketing_spend")[0]).toMatchObject({ effectiveTick: 1, command: { sku: 1, amountCents: 2500 } });
  expect(byKind("expedite_purchase_order")[0]).toMatchObject({ effectiveTick: 5, command: { poRef: 1 } });
  expect(cmds.every((c) => (c as unknown as { provenance: string }).provenance === "human")).toBe(true);

  const ledger = async (figure: string, from: number, to: number) =>
    get<{ entries: LedgerEntry[]; sumCents: number; total: number }>(page, `/branches/${branch}/ledger?figure=${figure}&from=${from}&to=${to}&limit=500`);

  // Wages: regular + overtime + temporary lines in one Labor_Expense entry per hour.
  const labor = await ledger("labor", 0, 8);
  const tick2 = labor.entries.find((e) => e.tick === 2);
  expect(tick2, "wage entry at hour 2").toBeDefined();
  const laborLines = tick2!.lines.filter((l) => l.account === "Labor_Expense").map((l) => l.debitCents);
  expect(laborLines).toContain(av("workforce.regularWorkers") * av("wages.regularPerHourCents"));
  expect(laborLines).toContain(av("workforce.regularWorkers") * 2 * av("wages.overtimePerHourCents"));
  expect(laborLines).toContain(2 * av("wages.tempPerHourCents"));
  // After the second hire (effective hour 5) the temporary line grows by the 20 hired.
  const tick6 = labor.entries.find((e) => e.tick === 6)!;
  expect(tick6.lines.filter((l) => l.account === "Labor_Expense").map((l) => l.debitCents)).toContain(22 * av("wages.tempPerHourCents"));

  // Marketing: one Marketing_Expense entry per hour from hour 1.
  const mk = await ledger("marketing", 0, 8);
  expect(mk.total).toBe(8);
  expect(mk.sumCents).toBe(8 * 2500);
  expect(mk.entries.every((e) => e.lines.some((l) => l.account === "Marketing_Expense" && l.debitCents === 2500))).toBe(true);

  // Rush fee: one freight entry caused by the purchase order, at the assumption's fee.
  const fr = await ledger("freight", 0, 8);
  const fee = fr.entries.filter((e) => e.cause.kind === "freight");
  expect(fee).toHaveLength(1);
  expect(fee[0]).toMatchObject({ tick: 5, cause: { ref: 1 } });
  expect(fee[0]!.lines.find((l) => l.account === "Freight_Expense")?.debitCents).toBe(av("costs.expediteFeeCents"));

  // Metrics agree with the ledger figures, and the HUD shows the API's cash.
  const m = await get<{ metrics: { cashCents: number; marketingExpenseCents: number; laborExpenseCents: number } }>(page, `/branches/${branch}/metrics?tick=8`);
  expect(m.metrics.marketingExpenseCents).toBe(mk.sumCents);
  expect(m.metrics.laborExpenseCents).toBe((await ledger("labor", 0, 8)).sumCents);
  const cash = m.metrics.cashCents;
  await expect(page.getByTestId("hud-cash")).toHaveText(`$${(cash / 100).toLocaleString("en-US", { minimumFractionDigits: 2 })}`);
  expect(mon.problems.filter((p) => !/http 4(09|22)/.test(p)), mon.problems.join("\n")).toEqual([]);
});
