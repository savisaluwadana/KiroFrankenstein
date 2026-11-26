"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useId, useMemo, useRef, useState } from "react";
import { Minus, Plus } from "lucide-react";
import { ApiError, createWorld, type CreateWorldResponse, type World } from "@/lib/api/client";
import { DISCLAIMER_FULL, formatMoney } from "@/lib/glossary";
import {
  buildControls,
  buildOverrides,
  clamp,
  isValid,
  parseDollarsToCents,
  type ControlGroup,
  type SetupControl,
} from "./controls";

const WORLD_NAME = "Black Friday Meltdown";
const SEED = 2026;
const GROUPS: ControlGroup[] = ["Money", "People", "Suppliers", "Prices"];

function friendlyError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 429) return "Too many requests right now. Please wait a moment and try again.";
    if (e.status === 400) return "Those settings were not accepted. Please adjust them and try again.";
  }
  return "Could not reach the game server. Please try again.";
}

/** Capital first letter and a closing full stop, so the hint reads as sentences. */
function sentence(s: string): string {
  const t = s.trim();
  if (t === "") return t;
  const cap = t.charAt(0).toUpperCase() + t.slice(1);
  return /[.!?]$/.test(cap) ? cap : `${cap}.`;
}

function toText(c: SetupControl, v: number): string {
  return c.kind === "money" ? formatMoney(v).slice(1).replace(/,/g, "") : String(v);
}

function parseText(c: SetupControl, text: string): number | null {
  if (c.kind === "money") return parseDollarsToCents(text);
  return /^\d{1,12}$/.test(text.trim()) ? Number(text.trim()) : null;
}

interface RowProps {
  control: SetupControl;
  value: number;
  onChange: (v: number) => void;
  onValidity: (id: string, ok: boolean) => void;
}

function ControlRow({ control: c, value, onChange, onValidity }: RowProps) {
  const uid = useId();
  // `draft` is what the user is typing; null means show the formatted value.
  const [draft, setDraft] = useState<string | null>(null);
  const isDefault = value === c.default;
  const parsedDraft = draft === null ? null : parseText(c, draft);
  const bad = draft !== null && (parsedDraft === null || !isValid(c, parsedDraft));
  const text = draft !== null && (bad || parsedDraft === value) ? draft : toText(c, value);

  function commit(v: number) {
    setDraft(null);
    onValidity(c.id, true);
    onChange(v);
  }

  function onText(next: string) {
    setDraft(next);
    const parsed = parseText(c, next);
    const ok = parsed !== null && isValid(c, parsed);
    onValidity(c.id, ok);
    if (ok && parsed !== null) onChange(parsed);
  }

  const hint = `Allowed: ${c.format(c.min)} to ${c.format(c.max)}`;
  return (
    <div className="grid gap-2 py-4 sm:grid-cols-[14rem_minmax(0,1fr)_11rem] sm:items-center">
      <label htmlFor={uid} className="font-medium">
        {c.label}
      </label>
      <div className="flex min-w-0 items-center gap-3">
        {c.slider ? (
          <input
            type="range"
            aria-label={`${c.label} slider`}
            min={c.min}
            max={c.max}
            step={c.step}
            value={clamp(c, value)}
            onChange={(e) => commit(Number(e.target.value))}
            className="h-11 min-w-0 flex-1 accent-[#E8A33D]"
          />
        ) : null}
        <button
          type="button"
          aria-label={`Decrease ${c.label}`}
          onClick={() => commit(clamp(c, value - c.step))}
          className="inline-flex size-11 shrink-0 items-center justify-center rounded-[4px] border border-edge hover:border-amber"
        >
          <Minus aria-hidden size={16} />
        </button>
        <input
          id={uid}
          inputMode={c.kind === "money" ? "decimal" : "numeric"}
          value={text}
          onChange={(e) => onText(e.target.value)}
          aria-invalid={bad}
          aria-describedby={`${uid}-hint`}
          className="h-11 w-28 rounded-[4px] border border-edge bg-ink px-3 text-right"
        />
        <button
          type="button"
          aria-label={`Increase ${c.label}`}
          onClick={() => commit(clamp(c, value + c.step))}
          className="inline-flex size-11 shrink-0 items-center justify-center rounded-[4px] border border-edge hover:border-amber"
        >
          <Plus aria-hidden size={16} />
        </button>
      </div>
      <div className="text-sm text-ivory-dim sm:text-right">
        <span>{c.format(value)}</span>
        {isDefault ? <span> (default)</span> : null}
      </div>
      <p id={`${uid}-hint`} className={`text-sm sm:col-span-3 ${bad ? "text-clay" : "text-ivory-dim"}`}>
        {bad ? `Please enter a whole value. ${hint}.` : `${sentence(c.rationale)} ${hint}.`}
      </p>
    </div>
  );
}

export function SetupPage() {
  const router = useRouter();
  const [draft, setDraft] = useState<CreateWorldResponse | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [values, setValues] = useState<Record<string, number>>({});
  const [invalid, setInvalid] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState(false);
  const [startError, setStartError] = useState<string | null>(null);
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
    try {
      const run = changed ? await createWorld({ name: WORLD_NAME, seed: SEED, overrides }) : draft;
      router.push(`/worlds/${encodeURIComponent(run.world.id)}?branch=${encodeURIComponent(run.rootBranch.id)}`);
    } catch (e) {
      setStartError(friendlyError(e));
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

        {draft !== null
          ? GROUPS.map((g) => {
              const rows = controls.filter((c) => c.group === g);
              if (rows.length === 0) return null;
              return (
                <fieldset key={g} className="mt-6 border-t border-hairline pt-4">
                  <legend className="pr-3 text-sm uppercase tracking-wide text-ivory-dim">{g}</legend>
                  {rows.map((c) => (
                    <ControlRow
                      key={`${c.id}-${resetKey}`}
                      control={c}
                      value={valueOf(c)}
                      onChange={(v) => setValues((prev) => ({ ...prev, [c.id]: v }))}
                      onValidity={setValidity}
                    />
                  ))}
                </fieldset>
              );
            })
          : null}

        {draft !== null ? (
          <div className="mt-8 flex flex-wrap items-center justify-between gap-4 border-t border-hairline pt-6">
            <p className="text-sm text-ivory-dim">Changes apply to this run only.</p>
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
