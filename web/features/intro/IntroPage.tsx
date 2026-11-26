"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { ArrowRight } from "lucide-react";
import { DISCLAIMER_SHORT } from "@/lib/glossary";
import { DuskIllustration } from "./DuskIllustration";

export function IntroPage() {
  const router = useRouter();

  // Esc skips the intro.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") router.push("/briefing");
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [router]);

  return (
    <main className="relative flex min-h-screen flex-col justify-end overflow-hidden bg-ink text-ivory">
      <DuskIllustration />
      <div aria-hidden className="absolute inset-0 bg-gradient-to-r from-ink/90 via-ink/60 to-transparent sm:via-ink/45 sm:to-transparent" />
      <Link
        href="/briefing"
        className="absolute right-6 top-6 z-10 inline-flex min-h-11 items-center rounded-[4px] bg-ink/80 px-4 text-sm text-ivory hover:text-amber-hot"
      >
        Skip
      </Link>
      <div className="relative z-10 flex max-w-2xl flex-col gap-4 px-6 pb-20 pt-40 sm:px-16">
        <h1 className="intro-title font-display text-6xl font-semibold leading-[1.15] sm:text-7xl">Parallel</h1>
        <p className="intro-title font-display text-2xl text-ivory">Reality has no undo button. We built one.</p>
        <p className="intro-premise max-w-xl text-lg text-ivory">
          Run a warehouse through Black Friday week. When things go wrong, rewind and try a different future.
        </p>
        <div className="intro-cta mt-2">
          <Link
            href="/briefing"
            className="inline-flex min-h-12 items-center gap-2 rounded-[4px] bg-amber px-6 text-base font-medium text-ink transition-colors duration-200 ease-out hover:bg-amber-hot"
          >
            Begin your shift
            <ArrowRight aria-hidden size={18} strokeWidth={1.75} />
          </Link>
        </div>
      </div>
      <p className="relative z-10 bg-ink px-6 py-3 text-xs text-ivory-dim sm:px-16" data-testid="disclaimer">
        {DISCLAIMER_SHORT}
      </p>
    </main>
  );
}
