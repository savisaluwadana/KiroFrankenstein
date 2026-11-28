"use client";

/**
 * Notice for a world opened without its write token (a shared link, or a token
 * the server refused). The studio hides every control that would change the
 * world, as it does for a world from an older simulator version.
 */
import { Eye } from "lucide-react";
import { useRouter } from "next/navigation";

export const SHARED_READ_ONLY_TEXT = "You are viewing a shared, read-only copy. Start your own business to make changes.";

export function SharedReadOnlyBanner() {
  const router = useRouter();
  return (
    <div
      role="status"
      data-testid="shared-read-only-banner"
      className="flex flex-wrap items-center gap-3 rounded-[4px] border border-amber bg-raised px-3 py-2 text-sm text-ivory"
    >
      <Eye aria-hidden size={20} className="shrink-0 text-amber" />
      <p className="flex-1">{SHARED_READ_ONLY_TEXT}</p>
      <button
        type="button"
        className="min-h-11 rounded-[4px] border border-amber px-3 text-amber hover:text-amber-hot"
        onClick={() => router.push("/briefing")}
      >
        Start your own
      </button>
    </div>
  );
}
