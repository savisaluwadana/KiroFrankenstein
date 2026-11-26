"use client";

import { useEffect } from "react";
import { Wrench } from "lucide-react";
import { hydrateUiStore, useUiStore } from "@/stores/ui";

/** Header toggle for Engineering Mode. Hydrates the persisted preference on mount. */
export function EngineeringToggle() {
  const engineering = useUiStore((s) => s.engineering);
  const toggle = useUiStore((s) => s.toggleEngineering);
  useEffect(() => {
    hydrateUiStore();
  }, []);
  return (
    <button
      type="button"
      aria-pressed={engineering}
      onClick={toggle}
      title="Engineering Mode shows raw technical detail"
      className={`inline-flex min-h-11 items-center gap-2 rounded-[4px] border px-3 text-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#F3EDE0] ${
        engineering ? "border-[#E8A33D] text-[#E8A33D]" : "border-[#7A7680] text-[#B9B2A3] hover:text-[#F3EDE0]"
      }`}
    >
      <Wrench aria-hidden size={16} strokeWidth={1.75} />
      Engineering Mode
    </button>
  );
}
