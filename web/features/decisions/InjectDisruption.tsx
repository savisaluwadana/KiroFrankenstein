"use client";

/**
 * "Add a surprise": records one Disruption at a future hour of the displayed
 * reality. Collapsed by default. The server validates every range; its issue
 * text is shown next to the field it names. Nothing here predicts an outcome.
 */
import { useRef, useState } from "react";
import {
  ApiError,
  injectDisruption,
  type DisruptionKind,
  type InjectDisruptionRequest,
  type InjectDisruptionResult,
} from "@/lib/api/client";
import { disruptionChoiceLabel, formatHours, formatSimTime, productName, requestFailureMessage } from "@/lib/glossary";

export interface InjectDisruptionProps {
  branchId: string;
  /** Latest computed hour of the reality. A surprise must start after it. */
  headTick: number;
  horizonTicks: number;
  startClock: string;
  skus: readonly { index: number; id: string }[];
  onInjected?: (result: InjectDisruptionResult) => void;
  /** Replaces the endpoint used to record the surprise (the Duel view injects into both businesses). */
  submitFn?: (body: InjectDisruptionRequest, idempotencyKey: string) => Promise<InjectDisruptionResult>;
  /** Replaces the heading, for example "Add a surprise to both businesses". */
  title?: string;
  /** Called when the server says this world was recorded by another simulator version. */
  onEngineMismatch?: () => void;
}

const KINDS: DisruptionKind[] = ["supplier_outage", "demand_surge", "workforce_absence"];

const field =
  "min-h-11 rounded-[4px] border border-hairline bg-ink px-2 text-sm text-ivory tabular-nums focus-visible:outline-2";
const button =
  "inline-flex min-h-11 items-center justify-center rounded-[4px] border border-hairline bg-surface px-3 text-sm text-ivory transition-colors duration-150 ease-out hover:border-amber disabled:opacity-40 motion-reduce:transition-none";

/** Request field names the server may report, mapped to the form field they belong to. */
type FormField = "startTick" | "durationTicks" | "magnitudeBps" | "target" | "role";

function parseWhole(input: string): number | null {
  return /^\d{1,7}$/.test(input.trim()) ? Number(input.trim()) : null;
}

export function InjectDisruption(props: InjectDisruptionProps) {
  const { branchId, headTick, horizonTicks, startClock, skus } = props;
  const [open, setOpen] = useState(false);
  const [kind, setKind] = useState<DisruptionKind>("supplier_outage");
  const [start, setStart] = useState(String(headTick + 1));
  const [duration, setDuration] = useState("12");
  const [percent, setPercent] = useState("50");
  const [target, setTarget] = useState("-1");
  const [role, setRole] = useState<"primary" | "backup">("primary");
  const [busy, setBusy] = useState(false);
  const [issues, setIssues] = useState<Partial<Record<FormField, string>>>({});
  const [failure, setFailure] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const keyRef = useRef<string | null>(null);

  const canAddAny = headTick < horizonTicks;
  const futureHours: number[] = [];
  for (let t = headTick + 1; t <= horizonTicks; t++) futureHours.push(t);
  const startValue = futureHours.includes(Number(start)) ? start : String(headTick + 1);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (busy) return;
    const durationTicks = parseWhole(duration);
    const pct = parseWhole(percent);
    const bad: Partial<Record<FormField, string>> = {};
    if (durationTicks === null) bad.durationTicks = "Enter a whole number of hours.";
    if (kind !== "supplier_outage" && pct === null) bad.magnitudeBps = "Enter a whole percentage.";
    if (Object.keys(bad).length > 0 || durationTicks === null) {
      setIssues(bad);
      return;
    }
    const body: InjectDisruptionRequest = {
      kind,
      startTick: Number(startValue),
      durationTicks,
      target: kind === "workforce_absence" ? -1 : Number(target),
      expectedHeadTick: headTick,
    };
    if (kind === "supplier_outage") body.role = role;
    else body.magnitudeBps = (pct ?? 0) * 100;

    keyRef.current ??= crypto.randomUUID();
    setBusy(true);
    setIssues({});
    setFailure(null);
    setDone(null);
    try {
      const result = props.submitFn
        ? await props.submitFn(body, keyRef.current)
        : await injectDisruption(branchId, body, keyRef.current);
      keyRef.current = null;
      setDone(
        `Added: ${disruptionChoiceLabel(kind).toLowerCase()} from ${formatSimTime(result.effectiveTick, startClock)} for ${formatHours(result.durationTicks)}.`,
      );
      props.onInjected?.(result);
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.type === "engine-version-mismatch") props.onEngineMismatch?.();
        const next: Partial<Record<FormField, string>> = {};
        const general: string[] = [];
        for (const i of err.issues) {
          const f = i.field as FormField;
          if (f === "startTick" || f === "durationTicks" || f === "magnitudeBps" || f === "target" || f === "role") {
            next[f] = i.message;
          } else general.push(i.message);
        }
        setIssues(next);
        if (Object.keys(next).length === 0) {
          setFailure(general.length > 0 ? general.join(" ") : (err.detail ?? requestFailureMessage(err.type)));
        } else if (general.length > 0) setFailure(general.join(" "));
      } else {
        setFailure("Something went wrong. Please try again.");
      }
    } finally {
      setBusy(false);
    }
  }

  const err = (f: FormField) =>
    issues[f] !== undefined ? (
      <span role="alert" data-testid={`issue-${f}`} className="text-xs text-clay">
        {issues[f]}
      </span>
    ) : null;

  return (
    <section aria-label="Add a surprise" className="space-y-2 text-sm text-ivory">
      <button
        type="button"
        aria-expanded={open}
        aria-controls="inject-disruption-form"
        onClick={() => setOpen((o) => !o)}
        className={`${button.replace("justify-center", "")} w-full justify-between gap-2`}
      >
        {props.title ?? "Add a surprise"}
        <span aria-hidden>{open ? "−" : "+"}</span>
      </button>
      {open ? (
        <form id="inject-disruption-form" onSubmit={(e) => void submit(e)} className="space-y-3 rounded-[8px] border border-hairline bg-surface p-3">
          <p className="text-xs text-ivory-dim">
            Try the future with something unexpected. It happens at the hour you choose, after the latest hour
            computed so far.
          </p>
          {!canAddAny ? (
            <p className="text-xs text-ivory-dim">This reality has reached its last hour, so nothing more can be added.</p>
          ) : (
            <>
              <label className="flex flex-col gap-1">
                What happens
                <select className={field} value={kind} onChange={(e) => setKind(e.target.value as DisruptionKind)}>
                  {KINDS.map((k) => (
                    <option key={k} value={k}>
                      {disruptionChoiceLabel(k)}
                    </option>
                  ))}
                </select>
              </label>
              <label className="flex flex-col gap-1">
                Starts at
                <select className={field} value={startValue} onChange={(e) => setStart(e.target.value)}>
                  {futureHours.map((t) => (
                    <option key={t} value={t}>
                      {formatSimTime(t, startClock)}
                    </option>
                  ))}
                </select>
                {err("startTick")}
              </label>
              <label className="flex flex-col gap-1">
                Lasts (hours)
                <input className={`${field} w-28`} inputMode="numeric" value={duration} onChange={(e) => setDuration(e.target.value)} />
                {err("durationTicks")}
              </label>
              {kind === "supplier_outage" ? (
                <label className="flex flex-col gap-1">
                  Which supplier
                  <select className={field} value={role} onChange={(e) => setRole(e.target.value === "backup" ? "backup" : "primary")}>
                    <option value="primary">Main supplier</option>
                    <option value="backup">Backup supplier</option>
                  </select>
                  {err("role")}
                </label>
              ) : null}
              {kind !== "workforce_absence" ? (
                <label className="flex flex-col gap-1">
                  {kind === "supplier_outage" ? "Product affected" : "Product"}
                  <select className={field} value={target} onChange={(e) => setTarget(e.target.value)}>
                    <option value="-1">All products</option>
                    {skus.map((s) => (
                      <option key={s.index} value={s.index}>
                        {productName(s.id)}
                      </option>
                    ))}
                  </select>
                  {err("target")}
                </label>
              ) : null}
              {kind !== "supplier_outage" ? (
                <label className="flex flex-col gap-1">
                  {kind === "demand_surge" ? "Extra orders (%)" : "Team out (%)"}
                  <input className={`${field} w-28`} inputMode="numeric" value={percent} onChange={(e) => setPercent(e.target.value)} />
                  {err("magnitudeBps")}
                </label>
              ) : null}
              {failure !== null ? (
                <p role="alert" className="text-xs text-clay">
                  {failure}
                </p>
              ) : null}
              <button type="submit" disabled={busy} className={`${button} border-amber text-amber`}>
                {busy ? "Adding…" : "Add this surprise"}
              </button>
            </>
          )}
          {done !== null ? (
            <p role="status" data-testid="inject-confirmation" className="text-xs text-sage">
              {done}
            </p>
          ) : null}
        </form>
      ) : null}
    </section>
  );
}
