import type { components } from "@/lib/api/schema";

type Provenance = components["schemas"]["Provenance"];

/**
 * How an Input marker looks on the time strip. Provenance is encoded three
 * ways so colour is never the only cue: a distinct glyph, a distinct border
 * style and a text label (aria-label, title and the legend).
 */
export interface ProvenanceMarker {
  label: string;
  glyph: string;
  /** Tailwind classes for border and text colour (and border style). */
  classes: string;
}

export const PROVENANCE_MARKER: Record<Provenance, ProvenanceMarker> = {
  human: { label: "Your move", glyph: "●", classes: "border-amber text-amber" },
  ai_rule_based: { label: "Move by Rule-based AI", glyph: "▲", classes: "border-sage text-sage" },
  ai_llm: { label: "Move by LLM AI", glyph: "■", classes: "border-ivory text-ivory" },
  ai_rule_based_fallback: {
    label: "Move by Rule-based AI (LLM fallback)",
    glyph: "△",
    classes: "border-dashed border-sage text-sage",
  },
  scenario: { label: "Scheduled event", glyph: "○", classes: "border-ivory-dim text-ivory-dim" },
  injected: { label: "Surprise you added", glyph: "+", classes: "border-clay text-clay" },
};

/** Order shown in the legend. */
export const LEGEND_PROVENANCES: readonly Provenance[] = ["human", "ai_rule_based", "ai_llm", "ai_rule_based_fallback", "injected"];

const OTHER_MARKERS: readonly { glyph: string; label: string; classes: string }[] = [
  { glyph: "!", label: "Surprise (scheduled event happens)", classes: "border-clay bg-clay text-ink" },
  { glyph: "◇", label: "Decision moment", classes: "border-ivory text-ivory" },
  { glyph: "Y", label: "Futures split here", classes: "border-amber text-amber" },
  { glyph: "≠", label: "The two futures first look different", classes: "border-clay text-clay" },
];

const chip = "inline-flex h-5 min-w-5 items-center justify-center rounded-[4px] border px-px text-[11px] leading-5";

/** Legend for the marker lanes. Shown inside the "?" popover of the playback controls. */
export function MarkerLegend() {
  return (
    <>
      <span className="mt-3 block font-medium">Timeline markers</span>
      <ul className="mt-1 space-y-1" data-testid="marker-legend">
        {LEGEND_PROVENANCES.map((p) => {
          const m = PROVENANCE_MARKER[p];
          return (
            <li key={p} className="flex items-center gap-2" data-legend-provenance={p}>
              <span aria-hidden className={`${chip} bg-ink ${m.classes}`}>
                {m.glyph}
              </span>
              {m.label}
            </li>
          );
        })}
        {OTHER_MARKERS.map((o) => (
          <li key={o.label} className="flex items-center gap-2">
            <span aria-hidden className={`${chip} ${o.classes}`}>
              {o.glyph}
            </span>
            {o.label}
          </li>
        ))}
      </ul>
    </>
  );
}
