import { expect, test } from "@playwright/test";
import { API, branchIdOf, closeSheetOnPhone, continueIfOpen, monitor, setSpeed, worldIdOf } from "./helpers";
import fs from "node:fs";
import path from "node:path";

test("setup: a changed starting cash reaches the backend assumption and the studio", async ({ page }) => {
  const mon = monitor(page);
  await page.goto("/setup");
  const cash = page.getByRole("textbox", { name: "Starting cash" });
  await expect(cash).toHaveValue("100000.00");
  await cash.fill("150000");
  await expect(page.getByRole("button", { name: "Start with these settings" })).toBeEnabled();
  const post = page.waitForRequest((r) => r.method() === "POST" && r.url() === `${API}/worlds` && (r.postData() ?? "").includes("overrides"));
  await page.getByRole("button", { name: "Start with these settings" }).click();
  const body = (await post).postData() ?? "";
  expect(body).toContain("15000000");
  await page.waitForURL(/\/worlds\//);
  await expect(page.getByTestId("hud-cash")).toHaveText("$150,000.00");
  const w = (await (await page.request.get(`${API}/worlds/${worldIdOf(page)}`)).json()) as {
    world?: { assumptions: { path: string; value: number }[] };
    assumptions?: { path: string; value: number }[];
  };
  const assumptions = w.world?.assumptions ?? w.assumptions ?? [];
  expect(assumptions.find((a) => a.path === "openingCashCents")?.value).toBe(15_000_000);
  expect(branchIdOf(page)).toBeTruthy();
  expect(mon.problems, mon.problems.join("\n")).toEqual([]);
});

test("frame times of the warehouse canvas during 5 s of Quick (4x) playback", async ({ page }, info) => {
  await page.goto("/briefing");
  await page.getByRole("button", { name: "Start Black Friday demo" }).click();
  await page.waitForURL(/\/worlds\//);
  await expect(page.locator("canvas").first()).toBeVisible();
  await page.waitForTimeout(1000);
  await closeSheetOnPhone(page);
  await continueIfOpen(page);
  // Pause only for surprises so playback keeps running for the whole sample.
  await page.getByLabel("Pause automatically at").selectOption("disruption_onsets_only");
  await setSpeed(page, "Quick");
  await page.evaluate(() => {
    const w = window as unknown as { __dts: number[]; __stop: boolean };
    w.__dts = [];
    w.__stop = false;
    let last = performance.now();
    const loop = (t: number): void => {
      w.__dts.push(t - last);
      last = t;
      if (!w.__stop) requestAnimationFrame(loop);
    };
    requestAnimationFrame(loop);
  });
  const t0 = Date.now();
  await page.getByRole("button", { name: /^Play$/ }).click();
  await page.waitForTimeout(5000);
  const playing = (await page.getByTestId("playback-status").textContent()) ?? "";
  const stats = await page.evaluate(() => {
    const w = window as unknown as { __dts: number[]; __stop: boolean };
    w.__stop = true;
    const d = [...w.__dts.slice(1)].sort((a, b) => a - b);
    const q = (p: number): number => d[Math.min(d.length - 1, Math.floor(p * d.length))]!;
    const mean = d.reduce((a, b) => a + b, 0) / d.length;
    return { frames: d.length, meanMs: mean, p50: q(0.5), p95: q(0.95), p99: q(0.99), maxMs: d[d.length - 1]!, over33: d.filter((x) => x > 33.4).length, over50: d.filter((x) => x > 50).length };
  });
  const out = { project: info.project.name, windowMs: Date.now() - t0, statusAtEnd: playing, ...stats, fps: 1000 / stats.meanMs };
  console.log("FRAMESTATS " + JSON.stringify(out));
  fs.writeFileSync(path.resolve(process.cwd(), `test-results/frame-stats-${info.project.name}.json`), JSON.stringify(out, null, 2));
  expect(stats.frames).toBeGreaterThan(20);
});
