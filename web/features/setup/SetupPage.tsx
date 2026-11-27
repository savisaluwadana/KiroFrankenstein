"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { ApiError, createWorld, type CreateWorldResponse, type ProblemIssue, type World } from "@/lib/api/client";
import { DISCLAIMER_FULL } from "@/lib/glossary";
import { AssumptionsEditor } from "@/features/assumptions/AssumptionsEditor";
import { buildControls, buildOverrides, type SetupControl } from "./controls";

const WORLD_NAME = "Black Friday Meltdown";
const SEED = 2026;

function friendlyError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 429) return "Too many requests right now. Please wait a moment and try again.";
    if (e.status === 400) {
      const first = e.issues[0]?.message;
      return first !== undefined && first !== ""
        ? `Those settings were not accepted: ${first}`
        : "Those settings were not accepted. Please adjust them and try again.";
    }
  }
  return "Could not reach the game server. Please try again.";
}

export function SetupPage() {
  const router = useRouter();
  const [draft, setDraft] = useState<CreateWorldResponse | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [values, setValues] = useState<Record<string, number>>({});
  const [invalid, setInvalid] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState(false);
  const [startError, setStartError] = useState<string | null>(null);
  const [issues, setIssues] = useState<ProblemIssue[]>([]);
  const [attempt, setAttempt] = useState(0);
  const [resetKey, setResetKey] = useState(0);
  const requested = useRef<number | null>(null);

  // One throwaway draft world per visit: its assumptions give the bounds and defaults.
  useEffect(() => {
    if (requested.current === attempt) return;
    requested.current = attempt;
    let live = true;
    createWorld({ name: WORLD_NAME, seed: SEED })
      .then((r) => {
        if (live) setDraft(r);
      })
      .catch((e: unknown) => {
        if (live) setLoadError(friendlyError(e));
      });
    return () => {
      live = false;
      // Allow the strict-mode remount to request again.
      requested.current = null;
    };
  }, [attempt]);

  const world: World | null = draft?.world ?? null;
  const controls = useMemo(
    () => (world ? buildControls(world.assumptions, world.scenario) : []),
    [world],
  );
  const valueOf = (c: SetupControl): number => values[c.id] ?? c.value;
  const overrides = buildOverrides(
    controls,
    Object.fromEntries(controls.map((c) => [c.id, valueOf(c)])),
  );
  const changed = Object.keys(overrides).length > 0;
  const anyInvalid = Object.values(invalid).some(Boolean);

  const setValidity = useMemo(
    () => (id: string, ok: boolean) =>
      setInvalid((prev) => (prev[id] === !ok ? prev : { ...prev, [id]: !ok })),
    [],
  );

  async function start() {
    if (!draft || busy || anyInvalid) return;
    setBusy(true);
    setStartError(null);
    setIssues([]);
    try {
      const run = changed ? await createWorld({ name: WORLD_NAME, seed: SEED, overrides }) : draft;
      router.push(`/worlds/${encodeURIComponent(run.world.id)}?branch=${encodeURIComponent(run.rootBranch.id)}`);
    } catch (e) {
      setStartError(friendlyError(e));
      if (e instanceof ApiError) setIssues(e.issues);
      setBusy(false);
    }
  }

  return (
    <main className="mx-auto flex min-h-screen max-w-4xl flex-col justify-center px-6 py-10">
      <section className="rounded-lg bg-raised p-6 sm:p-10">
        <div className="flex flex-wrap items-baseline justify-between gap-4">
          <h1 className="font-display text-3xl font-semibold leading-[1.15]">Set up your business</h1>
          <button
            type="button"
            onClick={() => {
              setValues({});
              setInvalid({});
              setResetKey((n) => n + 1);
            }}
            disabled={!changed && !anyInvalid}
            className="min-h-11 rounded-[4px] border border-edge px-4 text-sm hover:border-amber disabled:opacity-50"
          >
            Reset to default
          </button>
        </div>

        {loadError !== null ? (
          <div className="mt-6">
            <p role="alert" className="text-clay">
              {loadError}
            </p>
            <button
              type="button"
              onClick={() => {
                setLoadError(null);
                setAttempt((n) => n + 1);
              }}
              className="mt-3 min-h-11 rounded-[4px] border border-edge px-4 hover:border-amber"
            >
              Try again
            </button>
          </div>
        ) : null}
        {loadError === null && draft === null ? <p className="mt-6 text-ivory-dim">Loading your business…</p> : null}

        {draft !== null ? (
          <AssumptionsEditor
            controls={controls}
            valueOf={valueOf}
            onChange={(id, v) => setValues((prev) => ({ ...prev, [id]: v }))}
            onValidity={setValidity}
            resetKey={resetKey}
            issues={issues}
          />
        ) : null}

        {draft !== null ? (
          <div className="mt-8 flex flex-wrap items-center justify-between gap-4 border-t border-hairline pt-6">
            <p className="text-sm text-ivory-dim">Changing assumptions starts a new business.</p>
            <div className="flex items-center gap-4">
              <Link href="/briefing" className="inline-flex min-h-12 items-center px-2 text-ivory-dim hover:text-ivory">
                Back
              </Link>
              <button
                type="button"
                onClick={() => void start()}
                disabled={busy || anyInvalid}
                className="min-h-12 rounded-[4px] bg-amber px-6 text-base font-medium text-ink transition-colors duration-200 ease-out hover:bg-amber-hot disabled:opacity-60"
              >
                {busy ? "Starting…" : changed ? "Start with these settings" : "Start"}
              </button>
            </div>
          </div>
        ) : null}
        {startError !== null ? (
          <p role="alert" className="mt-4 text-clay">
            {startError}
          </p>
        ) : null}

        <p className="mt-6 text-sm text-ivory-dim" data-testid="disclaimer">
          {DISCLAIMER_FULL}
        </p>
      </section>
    </main>
  );
}
