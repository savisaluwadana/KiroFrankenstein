"use client";

import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { Info } from "lucide-react";

export interface InfoPopoverProps {
  /** What the popover explains; used for the accessible button name. */
  label: string;
  children: ReactNode;
  /** Icon button content override (for example "?" for shortcuts). */
  trigger?: ReactNode;
  align?: "start" | "end";
}

/**
 * A small non-modal popover. Opens on click, Enter or Space, closes on Escape,
 * outside click or a second press. Focus stays on the button.
 */
export function InfoPopover({ label, children, trigger, align = "start" }: InfoPopoverProps) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const wrapRef = useRef<HTMLSpanElement | null>(null);
  const btnRef = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    if (!open) return;
    function onKey(e: KeyboardEvent): void {
      if (e.key === "Escape") {
        setOpen(false);
        btnRef.current?.focus();
      }
    }
    function onPointer(e: PointerEvent): void {
      if (wrapRef.current !== null && e.target instanceof Node && !wrapRef.current.contains(e.target)) setOpen(false);
    }
    document.addEventListener("keydown", onKey);
    document.addEventListener("pointerdown", onPointer);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("pointerdown", onPointer);
    };
  }, [open]);

  return (
    <span ref={wrapRef} className="relative inline-flex">
      <button
        ref={btnRef}
        type="button"
        aria-label={`About ${label}`}
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={() => setOpen((o) => !o)}
        className="relative inline-flex size-5 items-center justify-center rounded-full text-ivory-dim before:absolute before:-inset-3 before:content-[''] hover:text-ivory"
      >
        {trigger ?? <Info aria-hidden size={14} strokeWidth={1.75} />}
      </button>
      {open ? (
        <span
          id={id}
          role="note"
          className={`absolute top-full z-30 mt-2 w-64 rounded-[8px] border border-hairline bg-raised p-3 text-left text-sm font-normal normal-case text-ivory ${
            align === "end" ? "right-0" : "left-0"
          }`}
        >
          {children}
        </span>
      ) : null}
    </span>
  );
}
