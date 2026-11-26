"use client";

import type { TickFrame } from "@/lib/api/client";
import type { Intervention } from "@/features/world-view/scene/worldViewModel";
import { WorldView } from "@/features/world-view/WorldView";
import { usePlaybackStore } from "@/stores/playback";

/** One reality's warehouse. The comparison view renders two of these. */
export function StagePane(props: {
  frame: TickFrame | null;
  syncing: boolean;
  branchKey: string;
  productNames: readonly string[];
  regularWorkers: number | undefined;
  interventions: readonly Intervention[];
}) {
  const playing = usePlaybackStore((s) => s.playing);
  const speed = usePlaybackStore((s) => s.speed);
  // Travel animations only while playing at a pace the eye can follow; scrubbing, stepping and Fast snap.
  const animate = playing && speed < 16;
  return (
    <WorldView
      frame={props.frame}
      syncing={props.syncing}
      interventions={props.interventions}
      productNames={props.productNames}
      branchKey={props.branchKey}
      animate={animate}
      {...(props.regularWorkers !== undefined ? { regularWorkers: props.regularWorkers } : {})}
    />
  );
}
