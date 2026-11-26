import { describeScene, type SceneModel } from "./scene/sceneModel";

export interface WorldViewSummaryProps {
  model: SceneModel | null;
  /** Announce changes to assistive technology. Leave off during playback; turn on at pauses and decision windows. */
  announce?: boolean;
  id?: string;
}

/** Accessible text summary of the displayed hour, in business language. */
export function WorldViewSummary({ model, announce = false, id }: WorldViewSummaryProps) {
  const live = announce ? "polite" : "off";
  if (model === null) {
    return (
      <div id={id} aria-live={live} className="text-sm text-[#B9B2A3]">
        The warehouse has not loaded yet.
      </div>
    );
  }
  const s = describeScene(model);
  return (
    <div id={id} aria-live={live} className="text-sm text-[#F3EDE0]" data-testid="world-summary">
      <p className="font-medium">{s.headline}</p>
      <ul className="mt-1 list-disc pl-5">
        {s.lines.map((l) => (
          <li key={l}>{l}</li>
        ))}
      </ul>
      {s.disruptions.length > 0 ? (
        <>
          <p className="mt-2 text-[#B9B2A3]">What is going wrong</p>
          <ul className="list-disc pl-5">
            {s.disruptions.map((l) => (
              <li key={l}>{l}</li>
            ))}
          </ul>
        </>
      ) : null}
      {s.interventions.length > 0 ? (
        <>
          <p className="mt-2 text-[#B9B2A3]">Moves taking effect this hour</p>
          <ul className="list-disc pl-5">
            {s.interventions.map((l) => (
              <li key={l}>{l}</li>
            ))}
          </ul>
        </>
      ) : null}
    </div>
  );
}
