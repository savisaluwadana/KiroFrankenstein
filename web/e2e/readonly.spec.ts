import { expect, test, type BrowserContextOptions } from "@playwright/test";
import { API, WRITE_TOKEN_HEADER, branchIdOf, headTick, monitor, startDemo, worldIdOf } from "./helpers";

/**
 * A world opened by id in a fresh browser context (no sessionStorage, so no write token) is a shared,
 * read-only copy: the banner shows, no control that changes the world is offered, reads work, and the API
 * refuses every write with 401. The creator's own tab keeps working.
 */
test("a shared link opens read-only, and the API refuses writes without the token", async ({ page, browser }, info) => {
  const mon = monitor(page);
  await startDemo(page);
  const world = worldIdOf(page);
  const branch = branchIdOf(page);
  const shared = `/worlds/${world}?branch=${branch}`;

  // The creator's tab can change the world.
  await expect(page.getByRole("button", { name: /^Play$/ })).toBeVisible();
  await expect(page.getByTestId("shared-read-only-banner")).toHaveCount(0);

  const ctx = await browser.newContext({ ...(info.project.use as BrowserContextOptions) });
  try {
    const guest = await ctx.newPage();
    await guest.goto(shared);
    await expect(guest.getByTestId("hud-cash")).toBeVisible();
    await expect(guest.getByTestId("shared-read-only-banner")).toContainText(
      "You are viewing a shared, read-only copy. Start your own business to make changes.",
    );
    await expect(guest.getByRole("button", { name: "Start your own" })).toBeVisible();
    // Controls are omitted, not disabled.
    for (const name of [/^Play$/, /Next moment/, /Explore another future/, /Add a surprise/, /Lock in decisions/]) {
      await expect(guest.getByRole("button", { name })).toHaveCount(0);
    }
    expect(await headTick(guest)).toBe(await headTick(page));

    // Reads need only the id.
    expect((await ctx.request.get(`${API}/branches/${branch}/state?tick=0`)).status()).toBe(200);
    expect((await ctx.request.get(`${API}/worlds/${world}`)).status()).toBe(200);

    // Writes need the token: none gives 401, a wrong one 403, and nothing changes.
    const before = await headTick(page);
    const none = await ctx.request.post(`${API}/branches/${branch}/advance`, { data: { runToTick: 4 } });
    expect(none.status()).toBe(401);
    expect(((await none.json()) as { type: string }).type).toBe("urn:parallel:problem:write-token-required");
    const wrong = await ctx.request.post(`${API}/branches/${branch}/advance`, {
      data: { runToTick: 4 },
      headers: { [WRITE_TOKEN_HEADER]: "x".repeat(43) },
    });
    expect(wrong.status()).toBe(403);
    expect(((await wrong.json()) as { type: string }).type).toBe("urn:parallel:problem:write-token-invalid");
    expect(((await (await ctx.request.get(`${API}/branches/${branch}`)).json()) as { headTick: number }).headTick).toBe(before);

    // "Start your own" leads to the briefing.
    await guest.getByRole("button", { name: "Start your own" }).click();
    await guest.waitForURL(/\/briefing/);
  } finally {
    await ctx.close();
  }
  // Only the two refused writes above are expected failures.
  expect(mon.problems.filter((p) => !p.startsWith("http 40"))).toEqual([]);
});
