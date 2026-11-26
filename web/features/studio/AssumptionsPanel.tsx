import type { World } from "@/lib/api/client";
import { DISCLAIMER_FULL, TERMS } from "@/lib/glossary";

/** Every assumption of this business with its allowed range and why it has that value. */
export function AssumptionsPanel({ world }: { world: World }) {
  return (
    <section aria-label={TERMS.assumptions} className="space-y-3 text-sm text-ivory">
      <h2 className="font-display text-xl">{TERMS.assumptions}</h2>
      <p className="text-xs text-ivory-dim">
        The values this business runs on, with the allowed range and why each has that value. Money is in cents and
        rates are in basis points (hundredths of a percent) unless the unit says otherwise.
      </p>
      <ul className="space-y-2">
        {world.assumptions.map((a) => (
          <li key={a.path} className="border-t border-hairline pt-2" data-testid="assumption">
            <div className="text-xs text-ivory-dim">{a.path}</div>
            <div className="tabular-nums">
              {a.value} {a.unit}
              {a.overridden ? ` (default ${a.default})` : ""} · range {a.min} to {a.max}
            </div>
            <div className="text-xs text-ivory-dim">{a.rationale}</div>
          </li>
        ))}
      </ul>
      <p className="text-xs text-ivory-dim">{DISCLAIMER_FULL}</p>
    </section>
  );
}
