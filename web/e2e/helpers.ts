import { expect, type Page, type TestInfo, type Request } from "@playwright/test";
import path from "node:path";

export const API = "http://localhost:18080/api/v1";
export const SHOTS = path.resolve(process.cwd(), "../docs/screenshots");

/* ---------- independent formatters (deliberately not imported from production code) ---------- */

function group(n: number): string {
  return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}
export function money(cents: number, signed = false): string {
  const abs = Math.abs(cents);
  const body = `$${group(Math.floor(abs / 100))}.${String(abs % 100).padStart(2, "0")}`;
  if (cents < 0) return `-${body}`;
  return signed && cents > 0 ? `+${body}` : body;
}
/** bps -> percent with one decimal, half to even on the tenth. */
export function percent(bps: number): string {
  const neg = bps < 0;
  const a = Math.abs(bps);
  let t = Math.floor(a / 10);
  const r = a % 10;
  if (r > 5 || (r === 5 && t % 2 === 1)) t += 1;
  return `${neg && t > 0 ? "-" : ""}${Math.floor(t / 10)}.${t % 10}%`;
}
export function points(bps: number): string {
  const p = percent(Math.abs(bps)).replace("%", "");
  return `${bps > 0 ? "+" : bps < 0 ? "-" : ""}${p} points`;
}
export function orders(n: number): string {
  return `${group(n)} ${n === 1 ? "order" : "orders"}`;
}

/* ---------- monitoring ---------- */

export interface Monitor {
  problems: string[];
  requests: Request[];
}

/** Collects console errors, page errors, failed requests and non-2xx API responses. */
export function monitor(page: Page): Monitor {
  const m: Monitor = { problems: [], requests: [] };
  page.on("console", (msg) => {
    if (msg.type() === "error") m.problems.push(`console.error: ${msg.text()}`);
    // Hydration and React warnings are logged as errors in React 19; GPU driver
    // "stall" notes from headless WebGL are warnings and are not collected.
  });
  page.on("pageerror", (e) => m.problems.push(`pageerror: ${e.message}`));
  page.on("requestfailed", (r) => {
    const err = r.failure()?.errorText ?? "";
    if (err.includes("ERR_ABORTED")) return; // navigation or unmount cancelled a fetch
    m.problems.push(`requestfailed: ${r.method()} ${r.url()} ${err}`);
  });
  page.on("response", (r) => {
    if (r.url().startsWith(API) && r.status() >= 400) m.problems.push(`http ${r.status()}: ${r.request().method()} ${r.url()}`);
  });
  page.on("request", (r) => {
    if (r.url().startsWith(API)) m.requests.push(r);
  });
  return m;
}

export async function shot(page: Page, info: TestInfo, name: string, fullPage = false): Promise<void> {
  await page.screenshot({ path: path.join(SHOTS, `${info.project.name}-${name}.png`), fullPage: fullPage && info.project.name === "desktop" });
}

/* ---------- studio state readers ---------- */

export function branchIdOf(page: Page): string {
  const id = new URL(page.url()).searchParams.get("branch");
  if (id === null) throw new Error(`no branch in ${page.url()}`);
  return id;
}

export function worldIdOf(page: Page): string {
  return new URL(page.url()).pathname.split("/").filter(Boolean).pop() ?? "";
}

const slider = (page: Page) => page.getByRole("slider", { name: "Displayed time" });

export async function selectedTick(page: Page): Promise<number> {
  return Number(await slider(page).getAttribute("aria-valuenow"));
}
export async function headTick(page: Page): Promise<number> {
  return Number(await slider(page).getAttribute("aria-valuemax"));
}
export async function statusText(page: Page): Promise<string> {
  return (await page.getByTestId("playback-status").textContent()) ?? "";
}

/** Opens the right rail (the bottom sheet on a phone) on the "Your move" tab. */
export async function openMoves(page: Page): Promise<void> {
  const show = page.getByRole("button", { name: /Show your move/ });
  if (await show.isVisible()) await show.click();
  const tab = page.getByRole("tab", { name: "Your move" });
  if ((await tab.getAttribute("aria-selected")) !== "true") await tab.click();
}

export async function closeSheetOnPhone(page: Page): Promise<void> {
  const hide = page.getByRole("button", { name: /Hide your move/ });
  if (await hide.isVisible()) await hide.click();
}

export async function startDemo(page: Page): Promise<void> {
  await page.goto("/briefing");
  await page.getByRole("button", { name: "Start Black Friday demo" }).click();
  await page.waitForURL(/\/worlds\//);
  await expect(page.getByTestId("hud-cash")).toBeVisible();
  await expect(page.locator("canvas").first()).toBeVisible();
}

/** Ends an open decision moment, if one is showing. Returns true when it clicked. */
export async function continueIfOpen(page: Page): Promise<boolean> {
  const btn = page.getByRole("button", { name: "Continue", exact: true });
  const show = page.getByRole("button", { name: /Show your move/ });
  if (!(await btn.isVisible().catch(() => false)) && (await show.isVisible().catch(() => false))) {
    await show.click();
    await page.getByRole("tab", { name: "Your move" }).click();
  }
  if (await btn.isVisible().catch(() => false)) {
    if (await btn.isEnabled()) {
      await btn.click();
      await expect(page.getByRole("button", { name: "Continue", exact: true })).toHaveCount(0);
      return true;
    }
  }
  return false;
}

export async function setSpeed(page: Page, label: "Calm" | "Quick" | "Fast"): Promise<void> {
  await page.getByRole("group", { name: "Speed" }).getByRole("button", { name: label }).click();
}

/** One Play press; resolves when playback has stopped again (a pause reason shows). */
export async function playOnce(page: Page): Promise<void> {
  const before = await headTick(page);
  await page.getByRole("button", { name: /^Play$/ }).click();
  await expect
    .poll(
      async () => {
        const s = await statusText(page);
        return !s.startsWith("Playing") && (await headTick(page)) > before;
      },
      { timeout: 60_000, intervals: [100] },
    )
    .toBe(true);
}

/** Plays (clicking Continue at every decision moment) until the head reaches `target`. */
export async function playTo(page: Page, target: number): Promise<void> {
  for (let guard = 0; guard < 80; guard++) {
    if ((await headTick(page)) >= target) return;
    await continueIfOpen(page);
    if ((await headTick(page)) >= target) return;
    await playOnce(page);
  }
  throw new Error(`could not reach tick ${target}`);
}

/** Clicks the time strip so that `tick` is displayed (a rewind or scrub: a read). */
export async function scrubTo(page: Page, tick: number, horizon = 120): Promise<void> {
  const el = slider(page);
  await el.scrollIntoViewIfNeeded();
  const box = await el.boundingBox();
  if (box === null) throw new Error("no time strip");
  if (tick >= horizon) {
    // The far edge is a sub-pixel target on a phone; the slider's End key reaches it.
    await el.focus();
    await page.keyboard.press("End");
  } else {
    await el.click({ position: { x: (tick / horizon) * box.width + 0.01, y: box.height / 2 } });
  }
  await expect.poll(() => selectedTick(page), { timeout: 15_000 }).toBe(tick);
  await expect(page.getByTestId("hud-cash")).toBeVisible();
  await expect(page.getByTestId("syncing-indicator")).toHaveText("");
}

/** Canvas has painted something other than a single flat colour. */
export async function canvasPainted(page: Page, which = 0): Promise<{ distinct: number; nonBackground: number }> {
  const buf = await page.locator("canvas").nth(which).screenshot();
  return page.evaluate(async (b64) => {
    const bin = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
    const bmp = await createImageBitmap(new Blob([bin], { type: "image/png" }));
    const c = new OffscreenCanvas(bmp.width, bmp.height);
    const ctx = c.getContext("2d");
    if (ctx === null) return { distinct: 0, nonBackground: 0 };
    ctx.drawImage(bmp, 0, 0);
    const d = ctx.getImageData(0, 0, bmp.width, bmp.height).data;
    const seen = new Set<number>();
    const bg = (d[0]! << 16) | (d[1]! << 8) | d[2]!;
    let non = 0;
    for (let i = 0; i < d.length; i += 4 * 7) {
      const v = (d[i]! << 16) | (d[i + 1]! << 8) | d[i + 2]!;
      seen.add(v >> 3);
      if (v !== bg) non++;
    }
    return { distinct: seen.size, nonBackground: non };
  }, buf.toString("base64"));
}

export async function apiMetrics(page: Page, branchId: string, tick: number) {
  const r = await page.request.get(`${API}/branches/${branchId}/metrics?tick=${tick}`);
  expect(r.ok()).toBe(true);
  const j = (await r.json()) as {
    tick: number;
    metrics: { cashCents: number; netProfitCents: number; serviceLevelBps: number; backlogOrders: number };
  };
  expect(j.tick).toBe(tick);
  return j.metrics;
}
