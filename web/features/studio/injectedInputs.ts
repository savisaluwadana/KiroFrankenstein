import { useCallback, useEffect, useState } from "react";
import { exportBranchInputs, type InputsFileInput } from "@/lib/api/client";
import type { TimelineInput } from "@/features/timeline/Timeline";

/** Surprises the user added, as Timeline markers. Scripted disruptions are never listed (hidden until they happen). */
export function injectedTimelineInputs(inputs: readonly InputsFileInput[]): TimelineInput[] {
  return inputs
    .filter((i) => i.kind === "disruption" && i.provenance === "injected")
    .map((i) => ({
      effectiveTick: i.effectiveTick,
      provenance: i.provenance,
      kind: i.disruption?.kind ?? "disruption",
    }));
}

/**
 * Reads the active reality's Inputs_File (own and inherited inputs) on open and
 * whenever `refresh` is called, so markers survive a reload.
 */
export function useInjectedInputs(
  branchId: string | null,
  onEngineMismatch?: () => void,
): { inputs: TimelineInput[]; refresh: () => void } {
  const [state, setState] = useState<{ branchId: string; inputs: TimelineInput[] } | null>(null);
  const [nonce, setNonce] = useState(0);
  useEffect(() => {
    if (branchId === null) return;
    let cancelled = false;
    exportBranchInputs(branchId)
      .then((r) => {
        if (cancelled) return;
        if (r.engineMismatch) onEngineMismatch?.();
        setState({ branchId, inputs: injectedTimelineInputs(r.file.inputs) });
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [branchId, nonce, onEngineMismatch]);
  const refresh = useCallback(() => setNonce((n) => n + 1), []);
  return { inputs: state !== null && state.branchId === branchId ? state.inputs : [], refresh };
}
