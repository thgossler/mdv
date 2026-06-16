// zoom controls content scaling via a CSS variable on the content element.
// It supports Cmd/Ctrl + wheel and Cmd/Ctrl +/- and reports the level.

let level = 1;
let min = 0.5;
let max = 3;
let step = 0.1;
let onChange: ((pct: number) => void) | null = null;
const target = () => document.getElementById("content");

export function initZoom(opts: { min: number; max: number; step: number; onChange: (pct: number) => void }): void {
  min = opts.min;
  max = opts.max;
  step = opts.step;
  onChange = opts.onChange;

  window.addEventListener(
    "wheel",
    (e) => {
      if (!(e.ctrlKey || e.metaKey)) return;
      e.preventDefault();
      adjust(e.deltaY < 0 ? step : -step);
    },
    { passive: false }
  );

  apply();
}

export function zoomIn(): void {
  adjust(step);
}
export function zoomOut(): void {
  adjust(-step);
}
export function zoomReset(): void {
  level = 1;
  apply();
}

function adjust(delta: number): void {
  level = clamp(Math.round((level + delta) * 100) / 100, min, max);
  apply();
}

function apply(): void {
  const el = target();
  if (el) el.style.setProperty("--zoom", String(level));
  if (onChange) onChange(Math.round(level * 100));
}

function clamp(v: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, v));
}

export function zoomLevel(): number {
  return level;
}
