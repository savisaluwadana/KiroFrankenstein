"use client";

import { useParams } from "next/navigation";
import { DuelView } from "@/features/duel/DuelView";

export default function DuelPage() {
  const params = useParams<{ duelId: string }>();
  // Re-key by duel so every duel opens with fresh view state.
  return <DuelView key={params.duelId} duelId={params.duelId} />;
}
