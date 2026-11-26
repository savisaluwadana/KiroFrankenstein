"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { createDemo } from "@/lib/api/client";
import { DISCLAIMER_FULL } from "@/lib/glossary";
import { saveDemoPlan } from "@/features/studio/demoPlanStorage";

export function BriefingPage() {
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function quickStart() {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      const demo = await createDemo();
      saveDemoPlan(demo.world.id, demo.plan);
      router.push(`/worlds/${encodeURIComponent(demo.world.id)}?branch=${encodeURIComponent(demo.rootBranch.id)}`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not start. Please try again.");
      setBusy(false);
    }
  }

  return (
    <main className="mx-auto flex min-h-screen max-w-3xl flex-col justify-center px-6 py-12">
      <article className="rounded-lg bg-raised p-6 sm:p-10">
        <p className="text-sm uppercase tracking-wide text-ivory-dim">Your business</p>
        <h1 className="mt-2 font-display text-4xl font-semibold leading-[1.15]">Black Friday Meltdown</h1>
        <p className="mt-4 text-lg">
          You run a small online electronics store and its one warehouse. You sell headphones, chargers, speakers and
          watch bands. Customers expect their orders to ship within a day or two.
        </p>

        <div className="mt-8 grid gap-8 sm:grid-cols-2">
          <section>
            <h2 className="font-display text-xl">Your goal</h2>
            <p className="mt-2">
              Get through five days of Black Friday sales, Thursday to Monday, with the best profit you can, without
              letting customers down.
            </p>
          </section>
          <section>
            <h2 className="font-display text-xl">What good looks like</h2>
            <ul className="mt-2 list-disc space-y-1 pl-5">
              <li>You finish the week with more profit than you started with.</li>
              <li>Most orders are delivered on time.</li>
              <li>Few orders are left waiting.</li>
            </ul>
          </section>
        </div>

        <p className="mt-8 border-l-2 border-amber pl-4 font-display text-lg">
          Black Friday week brings surprises. You will not know ahead of time what they are.
        </p>

        <section className="mt-8">
          <h2 className="font-display text-xl">You can</h2>
          <ul className="mt-2 list-disc space-y-1 pl-5">
            <li>Watch the warehouse work, hour by hour</li>
            <li>Order more products from your suppliers and change prices</li>
            <li>Rewind and explore another future from any hour</li>
          </ul>
        </section>

        <div className="mt-10 flex flex-wrap items-center gap-4">
          <button
            type="button"
            onClick={() => void quickStart()}
            disabled={busy}
            className="min-h-12 rounded-[4px] bg-amber px-6 text-base font-medium text-ink transition-colors duration-200 ease-out hover:bg-amber-hot disabled:opacity-60"
          >
            {busy ? "Starting…" : "Start Black Friday demo"}
          </button>
          <Link
            href="/setup"
            className="inline-flex min-h-12 items-center rounded-[4px] border border-edge px-6 text-base hover:border-amber hover:text-amber-hot"
          >
            Customize the business
          </Link>
        </div>
        {error !== null ? (
          <p role="alert" className="mt-4 text-clay">
            {error}
          </p>
        ) : null}

        <p className="mt-8 text-sm text-ivory-dim" data-testid="disclaimer">
          {DISCLAIMER_FULL}
        </p>
      </article>
    </main>
  );
}
