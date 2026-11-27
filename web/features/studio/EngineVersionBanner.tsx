"use client";

/**
 * Persistent notice for a world recorded under another simulator version. The
 * world can be looked at but not continued, so the studio hides every control
 * that would compute new hours or record a decision.
 */
import { useEffect, useState } from "react";
import { TriangleAlert } from "lucide-react";
import { getHealth } from "@/lib/api/client";

export interface EngineVersionBannerProps {
  /** Version the world was recorded with. */
  storedVersion: string;
  engineering: boolean;
}

export function EngineVersionBanner({ storedVersion, engineering }: EngineVersionBannerProps) {
  const [running, setRunning] = useState<string | null>(null);
  useEffect(() => {
    if (!engineering) return;
    let cancelled = false;
    getHealth()
      .then((h) => {
        if (!cancelled) setRunning(h.engineVersion);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [engineering]);

  return (
    <div
      role="alert"
      data-testid="engine-version-banner"
      className="flex items-start gap-3 rounded-[4px] border border-clay bg-raised px-3 py-2 text-sm text-ivory"
    >
      <TriangleAlert aria-hidden size={20} className="mt-0.5 shrink-0 text-clay" />
      <div className="space-y-1">
        <p>
          <span className="font-medium text-clay">Read only. </span>
          This world was created by an older version of the simulator, so you can look back at it but not continue
          it. Results may not match a fresh run.
        </p>
        {engineering ? (
          <p className="font-mono text-xs text-ivory-dim" data-testid="engine-versions">
            Stored engine version: {storedVersion} · Running engine version: {running ?? "unknown"}
          </p>
        ) : null}
      </div>
    </div>
  );
}
