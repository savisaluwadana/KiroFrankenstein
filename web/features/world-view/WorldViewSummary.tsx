import type { TickFrame } from "@/lib/api/client";
import { buildWorldViewModel, summarize, type BuildOptions } from "./scene/worldViewModel";

export interface WorldViewSummaryProps extends BuildOptions {
  frame: TickFrame | null;
}

/** Accessible text summary of the displayed Tick. Announced politely on change. */
export function WorldViewSummary({ frame, ...opts }: WorldViewSummaryProps) {
  if (frame === null) {
    return (
      <div aria-live="polite" className="font-mono text-xs text-muted">
        No tick loaded.
      </div>
    );
  }
  const s = summarize(buildWorldViewModel(frame, opts));
  return (
    <div aria-live="polite" className="font-mono text-xs text-text">
      <p>{s.headline}</p>
      <p className="text-muted">Bottlenecks</p>
      {s.bottlenecks.length === 0 ? <p>None.</p> : <ul>{s.bottlenecks.map((l) => <li key={l}>{l}</li>)}</ul>}
      <p className="text-muted">Disruptions</p>
      {s.disruptions.length === 0 ? <p>None in effect.</p> : <ul>{s.disruptions.map((l) => <li key={l}>{l}</li>)}</ul>}
      <p className="text-muted">Interventions at this tick</p>
      {s.interventions.length === 0 ? <p>None.</p> : <ul>{s.interventions.map((l) => <li key={l}>{l}</li>)}</ul>}
    </div>
  );
}
