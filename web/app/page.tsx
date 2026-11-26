"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { createDemo } from "@/lib/api/client";
import { saveDemoPlan } from "@/features/studio/demoPlanStorage";
import { DISCLAIMER_TEXT } from "@/features/world-view/Hud";

export default function HomePage() {
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function start() {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      const demo = await createDemo();
      saveDemoPlan(demo.world.id, demo.plan);
      router.push(`/worlds/${encodeURIComponent(demo.world.id)}?branch=${encodeURIComponent(demo.rootBranch.id)}`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not start the demo.");
      setBusy(false);
    }
  }

  return (
    <main className="mx-auto flex min-h-screen max-w-2xl flex-col justify-center gap-6 bg-[#0B0E14] px-6 py-12 text-[#E6E9EF]">
      <div>
        <h1 className="text-4xl font-semibold tracking-tight">Parallel</h1>
        <p className="mt-2 text-lg text-[#8A93A6]">Reality has no undo button. We built one.</p>
      </div>

      <p className="text-sm leading-relaxed">
        Parallel is a playable simulation of one e-commerce warehouse over a five-day Black Friday peak. You run it hour
        by hour, rewind to any past hour, and fork an alternative reality from there. A deterministic engine computes
        every number from the recorded inputs, so the same inputs always give the same results. No account is needed.
      </p>

      <div>
        <button
          type="button"
          onClick={() => void start()}
          disabled={busy}
          className="rounded-[4px] border border-[#F2A93B] bg-[#F2A93B] px-6 py-3 text-base font-medium text-[#0B0E14] transition-colors duration-150 ease-out hover:bg-[#FFC15E] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#E6E9EF] disabled:opacity-60 motion-reduce:transition-none"
        >
          {busy ? "Starting…" : "Start Black Friday demo"}
        </button>
        {error !== null ? (
          <p role="alert" className="mt-3 text-sm text-[#FF7A7A]">
            {error}
          </p>
        ) : null}
      </div>

      <p className="text-xs text-[#8A93A6]" data-testid="disclaimer">
        {DISCLAIMER_TEXT}
      </p>
    </main>
  );
}
