/**
 * Design-system tokens for Pixi drawing. Values are read from the CSS custom
 * properties in app/globals.css; the fallbacks mirror those tokens.
 */
export interface Palette {
  ink: number;
  surface: number;
  line: number;
  text: number;
  muted: number;
  human: number;
  ai: number;
  disruption: number;
  gain: number;
  loss: number;
}

const FALLBACK: Record<keyof Palette, string> = {
  ink: "#0b0e14",
  surface: "#121722",
  line: "#232b3a",
  text: "#e6e9ef",
  muted: "#8a93a6",
  human: "#f2a93b",
  ai: "#3bc9db",
  disruption: "#ff5c5c",
  gain: "#7bd88f",
  loss: "#ff7a7a",
};

function hex(v: string): number {
  const n = Number.parseInt(v.trim().replace("#", ""), 16);
  return Number.isNaN(n) ? 0 : n;
}

export function readPalette(el: Element): Palette {
  const cs = getComputedStyle(el);
  const out = {} as Palette;
  for (const key of Object.keys(FALLBACK) as (keyof Palette)[]) {
    const v = cs.getPropertyValue(`--${key}`);
    out[key] = hex(v !== "" ? v : FALLBACK[key]);
  }
  return out;
}
