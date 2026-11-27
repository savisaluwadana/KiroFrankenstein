"use client";

import { useCallback, useEffect, useState } from "react";
import {
  getAssumptions,
  type Assumption,
  type AssumptionsResponse,
  type RevealedDisruption,
  type World,
} from "@/lib/api/client";
import { DISCLAIMER_FULL, TERMS, disruptionLabel, formatHours, formatSimTime, productName } from "@/lib/glossary";
import { useUiStore } from "@/stores/ui";
import { displayLabel } from "./labels";
import { cleanRationale } from "@/features/setup/controls";

export interface AssumptionsPanelProps {
  world: World;
  /** The branch whose progress bounds what may be revealed. The root branch when omitted. */
  branchId?: string | undefined;
}

function Row({ a, engineering }: { a: Assumption; engineering: boolean }) {
  return (
    <li className="border-t border-hairline pt-2" data-testid="assumption">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <span className="font-medium">{displayLabel(a.path, engineering)}</span>
        {a.overridden ? (
          <span className="text-xs text-amber" data-testid="assumption-changed">
            Changed from default
          </span>
        ) : null}
      </div>
      {engineering ? <div className="text-xs text-ivory-dim">{a.path}</div> : null}
      <div className="tabular-nums">
        {a.value} {a.unit}
        {a.overridden ? ` (default ${a.default})` : ""} · range {a.min} to {a.max}
      </div>
      <div className="text-xs text-ivory-dim">{cleanRationale(a.rationale)}</div>
    </li>
  );
}

function skuName(target: string | undefined): string {
  return target === undefined ? "" : ` (${productName(target)})`;
}

function Surprise({ d, startClock, engineering }: { d: RevealedDisruption; startClock: string; engineering: boolean }) {
  return (
    <li className="border-t border-hairline pt-2" data-testid="revealed-surprise">
      <span className="font-medium">{disruptionLabel(d.kind)}</span>
      {skuName(d.target)} from {formatSimTime(d.onsetTick, startClock)} for {formatHours(d.durationTicks)}
      {engineering ? <span className="text-xs text-ivory-dim"> · hour {d.onsetTick}</span> : null}
    </li>
  );
}

/** Every assumption of this business with its allowed range and why it has that value. */
export function AssumptionsPanel({ world, branchId }: AssumptionsPanelProps) {
  const engineering = useUiStore((s) => s.engineering);
  const [data, setData] = useState<AssumptionsResponse | null>(null);
  const [revealed, setRevealed] = useState<AssumptionsResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const worldId = world.id;

  useEffect(() => {
    const ctl = new AbortController();
    getAssumptions(worldId, branchId === undefined ? {} : { branchId }, { signal: ctl.signal })
      .then(setData)
      .catch(() => undefined); // the world's own copy stays on screen
    return () => ctl.abort();
  }, [worldId, branchId]);

  const reveal = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      const q = branchId === undefined ? { revealScript: true } : { revealScript: true, branchId };
      setRevealed(await getAssumptions(worldId, q));
    } catch {
      setError("Could not load the surprises. Please try again.");
    } finally {
      setBusy(false);
    }
  }, [worldId, branchId]);

  const list = data?.assumptions ?? world.assumptions;
  const script = revealed?.disruptionScript ?? [];
  return (
    <section aria-label={TERMS.assumptions} className="space-y-3 text-sm text-ivory">
      <h2 className="font-display text-xl">{TERMS.assumptions}</h2>
      <p className="text-xs text-ivory-dim">
        The values this business runs on, with the allowed range and why each has that value. Money is in cents and
        rates are in basis points (hundredths of a percent) unless the unit says otherwise. Changing assumptions starts a
        new business.
      </p>
      <ul className="space-y-2">
        {list.map((a) => (
          <Row key={a.path} a={a} engineering={engineering} />
        ))}
      </ul>

      <div className="space-y-2 border-t border-hairline pt-3">
        <button
          type="button"
          onClick={() => void reveal()}
          disabled={busy}
          className="min-h-11 rounded-[4px] border border-edge px-4 hover:border-amber disabled:opacity-60"
        >
          Show the surprises that have already happened
        </button>
        {error !== null ? (
          <p role="alert" className="text-clay">
            {error}
          </p>
        ) : null}
        {revealed !== null ? (
          <div data-testid="revealed-script" className="space-y-2">
            <p className="text-xs text-ivory-dim">
              {revealed.scriptComplete
                ? "The week is over, so everything that was planned is shown."
                : "Only what has already happened is shown. What comes next stays hidden."}
            </p>
            {script.length === 0 ? (
              <p className="text-ivory-dim">Nothing out of the ordinary has happened yet.</p>
            ) : (
              <ul className="space-y-2">
                {script.map((d) => (
                  <Surprise
                    key={`${d.kind}-${d.onsetTick}-${d.target ?? ""}`}
                    d={d}
                    startClock={world.scenario.startClock}
                    engineering={engineering}
                  />
                ))}
              </ul>
            )}
          </div>
        ) : null}
      </div>
      <p className="text-xs text-ivory-dim">{DISCLAIMER_FULL}</p>
    </section>
  );
}
