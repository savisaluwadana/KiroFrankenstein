import { expect, test } from "@playwright/test";

// LOCAL REHEARSAL: cross-origin https web -> https API, CSP, CORS, SSE playback in a real browser.
test("https web talks to the https API without CSP, CORS or mixed-content errors", async ({ page }) => {
  const problems: string[] = [];
  const apiCalls: string[] = [];
  page.on("console", (m) => {
    if (m.type() === "error") problems.push(`console.error: ${m.text()}`);
  });
  page.on("pageerror", (e) => problems.push(`pageerror: ${e.message}`));
  page.on("requestfailed", (r) => {
    if (!(r.failure()?.errorText ?? "").includes("ERR_ABORTED")) problems.push(`requestfailed: ${r.url()} ${r.failure()?.errorText}`);
  });
  page.on("response", (r) => {
    if (r.url().includes("/api/v1/")) {
      apiCalls.push(`${r.status()} ${r.request().method()} ${new URL(r.url()).pathname}`);
      if (r.status() >= 400) problems.push(`http ${r.status()} ${r.url()}`);
    }
  });

  await page.goto("/briefing");
  await page.getByRole("button", { name: "Start Black Friday demo" }).click();
  await page.waitForURL(/\/worlds\//);
  await expect(page.locator("canvas").first()).toBeVisible();
  await expect(page.getByTestId("hud-cash")).toHaveText("$100,000.00");
  expect(await page.evaluate(() => sessionStorage.length), "write token kept in sessionStorage").toBeGreaterThan(0);

  // Continue past the opening decision moment (an advance POST with the write token), then Play (SSE).
  const show = page.getByRole("button", { name: /Show your move/ });
  if (await show.isVisible().catch(() => false)) {
    await show.click();
    await page.getByRole("tab", { name: "Your move" }).click();
  }
  const cont = page.getByRole("button", { name: "Continue", exact: true });
  if (await cont.isVisible().catch(() => false)) await cont.click();
  await page.getByRole("button", { name: /^Play$/ }).click();
  await expect.poll(async () => await page.getByTestId("hud-clock").innerText(), { timeout: 60_000 }).not.toBe("Thursday, 12:00 AM");

  console.log(apiCalls.slice(0, 12).join("\n"));
  expect(problems).toEqual([]);
});
