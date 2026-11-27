"use client";

import { useId, useState } from "react";
import { Minus, Plus } from "lucide-react";
import type { ProblemIssue } from "@/lib/api/client";
import { formatMoney } from "@/lib/glossary";
import {
  clamp,
  isValid,
  parseDollarsToCents,
  type ControlGroup,
  type SetupControl,
} from "@/features/setup/controls";

const GROUPS: ControlGroup[] = ["Money", "People", "Suppliers", "Prices"];

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
  /** The validator's message for this control, from the server's last answer. */
  serverMessage?: string | undefined;
}

function ControlRow({ control: c, value, onChange, onValidity, serverMessage }: RowProps) {
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
      {serverMessage !== undefined ? (
        <p role="alert" className="text-sm text-clay sm:col-span-3" data-testid="server-issue">
          {serverMessage}
        </p>
      ) : null}
    </div>
  );
}


export interface AssumptionsEditorProps {
  controls: SetupControl[];
  valueOf: (c: SetupControl) => number;
  onChange: (id: string, v: number) => void;
  onValidity: (id: string, ok: boolean) => void;
  /** Bumped to discard typed-but-uncommitted text (Reset to default). */
  resetKey: number;
  /** Issues from the validator's last rejection; each shows next to the control that writes its path. */
  issues?: ProblemIssue[];
}

/**
 * The friendly assumption controls. Values are only collected here: they are
 * sent as overrides when a new World is created, because a World's
 * assumptions never change. Changing assumptions starts a new business.
 */
export function AssumptionsEditor({ controls, valueOf, onChange, onValidity, resetKey, issues = [] }: AssumptionsEditorProps) {
  const messageFor = (c: SetupControl): string | undefined => issues.find((i) => c.paths.includes(i.field))?.message;
  return (
    <>
      {GROUPS.map((g) => {
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
                onChange={(v) => onChange(c.id, v)}
                onValidity={onValidity}
                serverMessage={messageFor(c)}
              />
            ))}
          </fieldset>
        );
      })}
    </>
  );
}
