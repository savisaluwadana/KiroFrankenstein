"use client";

import { useRef, type KeyboardEvent } from "react";
import { LaneMark, laneFor } from "./lanes";

export interface RealityItem {
  id: string;
  label: string;
  /** Position in the ordered list of realities; picks the lane colour and pattern. */
  laneIndex: number;
}

/**
 * Segmented switch between realities. Each segment carries its lane colour and
 * line pattern next to the name. Left/Right (and Home/End) move focus between
 * segments; Enter or Space selects.
 */
export function RealitySwitcher(props: {
  items: readonly RealityItem[];
  activeId: string;
  onSelect: (id: string) => void;
  label: string;
  stickyOnPhone?: boolean;
}) {
  const refs = useRef<(HTMLButtonElement | null)[]>([]);

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>): void {
    const n = props.items.length;
    const i = refs.current.findIndex((el) => el === document.activeElement);
    if (i < 0) return;
    let to: number | null = null;
    if (e.key === "ArrowRight" || e.key === "ArrowDown") to = (i + 1) % n;
    else if (e.key === "ArrowLeft" || e.key === "ArrowUp") to = (i - 1 + n) % n;
    else if (e.key === "Home") to = 0;
    else if (e.key === "End") to = n - 1;
    if (to === null) return;
    e.preventDefault();
    e.stopPropagation();
    refs.current[to]?.focus();
  }

  return (
    <div
      role="group"
      aria-label={props.label}
      onKeyDown={onKeyDown}
      className={`flex flex-wrap gap-1 ${props.stickyOnPhone ? "max-[768px]:sticky max-[768px]:top-0 max-[768px]:z-10 max-[768px]:bg-ink max-[768px]:py-1" : ""}`}
    >
      {props.items.map((it, i) => {
        const active = it.id === props.activeId;
        return (
          <button
            key={it.id}
            ref={(el) => {
              refs.current[i] = el;
            }}
            type="button"
            aria-pressed={active}
            onClick={() => props.onSelect(it.id)}
            className={`inline-flex min-h-11 items-center gap-2 rounded-[4px] border px-3 text-sm ${
              active ? "border-amber text-amber" : "border-hairline text-ivory-dim hover:text-ivory"
            }`}
          >
            <LaneMark lane={laneFor(it.laneIndex)} />
            {it.label}
          </button>
        );
      })}
    </div>
  );
}
