"use client";

import { Suspense } from "react";
import { useParams, useSearchParams } from "next/navigation";
import { StudioPage } from "@/features/studio/StudioPage";

function WorldRoute() {
  const params = useParams<{ worldId: string }>();
  const search = useSearchParams();
  const worldId = params.worldId;
  // Re-key by world so every world opens with fresh view state.
  return <StudioPage key={worldId} worldId={worldId} branchId={search.get("branch")} />;
}

export default function WorldPage() {
  return (
    <Suspense
      fallback={
        <main className="flex min-h-screen items-center justify-center bg-[#0B0E14] text-[#8A93A6]">
          <p role="status" className="font-mono text-sm">
            Loading world…
          </p>
        </main>
      }
    >
      <WorldRoute />
    </Suspense>
  );
}
