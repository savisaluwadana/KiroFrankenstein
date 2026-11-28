import { expect, test, type Page } from "@playwright/test";
import { API, apiPost, branchIdOf, closeSheetOnPhone, continueIfOpen, headTick, monitor, openMoves, playTo, setSpeed, shot, startDemo, worldIdOf } from "./helpers";

/**
 * (M6) "How this is modelled" in a real browser against the real backend: values, ranges and rationale
 * equal GET /worlds/{id}/assumptions, the disruption script is hidden by default, and the explicit reveal
 * never shows a surprise that has not happened yet (also for duel worlds).
 */

interface AssumptionApi {
  path: string;
  value: number;
  unit: string;
  min: number;
  max: number;
  default: number;
  overridden: boolean;
  rationale: string;
}
interface Reveal {
  headTick: number;
  horizonTicks: number;
  assumptions: AssumptionApi[];
  scriptRevealed: boolean;
  scriptComplete: boolean;
  disruptionScript?: { kind: string; onsetTick: number; durationTicks: number }[];
}

async function getAssumptions(page: Page, world: string, query: string): Promise<Reveal> {
  const r = await page.request.get(`${API}/worlds/${world}/assumptions${query}`);
  expect(r.ok(), `${query} -> ${r.status()}`).toBe(true);
  return (await r.json()) as Reveal;
}

const cleanRationale = (r: string): string => r.replace(/^synthetic, illustrative(; uncalibrated)?:\s*/i, "");

async function openModel(page: Page): Promise<void> {
  await openMoves(page);
  await page.getByRole("tab", { name: "How this is modelled" }).click();
  await expect(page.getByRole("heading", { name: "How this is modelled", level: 2 })).toBeVisible();
}

test("assumptions panel equals the API; the surprise script is hidden and never leaks the future", async ({ page, request }, info) => {
  test.skip(info.project.name !== "desktop", "desktop only; the phone layout is covered by the diff spec screenshot");
  test.setTimeout(240_000);
  const mon = monitor(page);
  await startDemo(page);
  const world = worldIdOf(page);
  const branch = branchIdOf(page);
  await openModel(page);

  /* ---- values, ranges and rationale equal the API ---- */
  const api = await getAssumptions(page, world, "");
  expect(api.scriptRevealed).toBe(false);
  expect(api.disruptionScript ?? [], "the script is absent unless asked for").toEqual([]);
  expect(api.assumptions.length).toBeGreaterThan(10);
  const rows = page.getByTestId("assumption");
  await expect(rows).toHaveCount(api.assumptions.length);
  for (let i = 0; i < api.assumptions.length; i++) {
    const a = api.assumptions[i]!;
    const row = rows.nth(i);
    await expect(row).toContainText(`${a.value} ${a.unit}`);
    await expect(row).toContainText(`range ${a.min} to ${a.max}`);
    await expect(row).toContainText(cleanRationale(a.rationale));
    if (a.overridden) await expect(row).toContainText(`default ${a.default}`);
  }
  await expect(page.getByText(/Synthetic simulation output under the stated assumptions/).first()).toBeVisible();

  /* ---- the script is hidden by default; at hour 0 the reveal shows nothing ---- */
  await expect(page.getByTestId("revealed-script")).toHaveCount(0);
  await expect(page.getByTestId("revealed-surprise")).toHaveCount(0);
  const reveal = page.getByRole("button", { name: "Show the surprises that have already happened" });
  await reveal.click();
  await expect(page.getByTestId("revealed-script")).toBeVisible();
  await expect(page.getByTestId("revealed-surprise")).toHaveCount(0);
  await expect(page.getByText("Nothing out of the ordinary has happened yet.")).toBeVisible();
  await expect(page.getByText(/What comes next stays hidden/)).toBeVisible();

  /* ---- direct API: revealScript at head 0 leaks nothing ---- */
  const at0 = await getAssumptions(page, world, "?revealScript=true");
  expect(at0.headTick).toBe(0);
  expect(at0.scriptRevealed).toBe(true);
  expect(at0.scriptComplete).toBe(false);
  expect(at0.disruptionScript ?? []).toEqual([]);

  /* ---- the full script of an identical world, advanced to the horizon through the API only ---- */
  const full = await (async () => {
    const w = (await (await apiPost(page, "/worlds", { name: "reveal oracle", seed: 2026 }, request)).json()) as { rootBranch: { id: string }; world: { id: string } };
    for (let guard = 0; guard < 60; guard++) {
      const adv = await apiPost(page, `/branches/${w.rootBranch.id}/advance`, { runToTick: 120 }, request);
      expect(adv.ok(), await adv.text()).toBe(true);
      const head = ((await adv.json()) as { headTick: number }).headTick;
      if (head >= 120) break;
      const end = await apiPost(page, `/branches/${w.rootBranch.id}/decision-window/end`, { windowTick: head }, request);
      expect(end.ok(), await end.text()).toBe(true);
    }
    return getAssumptions(page, w.world.id, "?revealScript=true");
  })();
  expect(full.scriptComplete).toBe(true);
  const fullScript = full.disruptionScript ?? [];
  expect(fullScript.length).toBeGreaterThan(0);
  const firstOnset = Math.min(...fullScript.map((d) => d.onsetTick));

  /* ---- advance past the first onset: only past surprises, fewer than the whole script ---- */
  await openMoves(page);
  await closeSheetOnPhone(page);
  await setSpeed(page, "Fast");
  await playTo(page, firstOnset + 2);
  const head = await headTick(page);
  await continueIfOpen(page);
  await openModel(page);
  await page.getByRole("button", { name: "Show the surprises that have already happened" }).click();
  const apiNow = await getAssumptions(page, world, `?revealScript=true&branchId=${branch}`);
  expect(apiNow.headTick).toBe(head);
  const shown = apiNow.disruptionScript ?? [];
  expect(shown.length).toBeGreaterThan(0);
  expect(shown.every((d) => d.onsetTick <= head)).toBe(true);
  expect(shown.length).toBe(fullScript.filter((d) => d.onsetTick <= head).length);
  if (fullScript.some((d) => d.onsetTick > head)) expect(shown.length).toBeLessThan(fullScript.length);
  await expect(page.getByTestId("revealed-surprise")).toHaveCount(shown.length);
  const panelText = await page.getByTestId("revealed-script").innerText();
  for (const d of fullScript.filter((x) => x.onsetTick > head)) {
    // a future entry's kind may equal a past entry's kind; count occurrences instead of presence
    expect(shown.filter((s) => s.kind === d.kind && s.onsetTick === d.onsetTick)).toHaveLength(0);
  }
  expect(panelText).toContain("Only what has already happened is shown");
  await page.waitForTimeout(500);
  await shot(page, info, "19-assumptions", true);

  /* ---- duel world: the reveal is bounded by the lower head and never shows the future ---- */
  const created = await apiPost(page, "/duels", { forkTick: 0 }, request);
  expect(created.ok()).toBe(true);
  const duel = (await created.json()) as { world: { id: string }; duel: { id: string; human: { branch: { id: string } }; ai: { branch: { id: string } } } };
  const dWorld = duel.world.id;
  const humanId = duel.duel.human.branch.id;
  const aiId = duel.duel.ai.branch.id;
  const dAt0 = await getAssumptions(page, dWorld, `?revealScript=true&branchId=${humanId}`);
  expect(dAt0.disruptionScript ?? []).toEqual([]);
  // Try to put one side ahead; the API may refuse a single-side advance, which is also safe.
  const ahead = await apiPost(page, `/branches/${humanId}/advance`, { runToTick: 120 }, request);
  const heads = await Promise.all(
    [humanId, aiId].map(async (id) => ((await (await request.get(`${API}/branches/${id}`)).json()) as { branch?: { headTick: number }; headTick?: number })),
  );
  const headOf = (h: { branch?: { headTick: number }; headTick?: number }): number => h.branch?.headTick ?? h.headTick ?? 0;
  const low = Math.min(...heads.map(headOf));
  for (const id of [humanId, aiId]) {
    const r = await getAssumptions(page, dWorld, `?revealScript=true&branchId=${id}`);
    expect(r.headTick).toBe(low);
    expect((r.disruptionScript ?? []).every((d) => d.onsetTick <= low)).toBe(true);
    if (low < 120) expect(r.scriptComplete).toBe(false);
  }
  info.annotations.push({ type: "single-side advance", description: `HTTP ${ahead.status()}; lower head ${low}` });
  // Then advance the duel in lockstep and check again.
  const lock = await apiPost(page, `/duels/${duel.duel.id}/advance`, { runToTick: 60 }, request);
  if (lock.ok()) {
    const r = await getAssumptions(page, dWorld, `?revealScript=true&branchId=${humanId}`);
    expect(r.headTick).toBeLessThanOrEqual(60);
    expect((r.disruptionScript ?? []).every((d) => d.onsetTick <= r.headTick)).toBe(true);
  }

  expect(mon.problems.filter((p) => !/http 40[0-9]/.test(p)), mon.problems.join("\n")).toEqual([]);
});
