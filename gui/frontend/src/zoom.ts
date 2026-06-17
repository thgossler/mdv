// zoom controls content scaling via a CSS variable on the content element.
// It supports Cmd/Ctrl + wheel and Cmd/Ctrl +/- and reports the level.

let level = 1;
let min = 0.5;
let max = 3;
let step = 0.1;
let onChange: ((pct: number) => void) | null = null;
const target = () => document.getElementById("content");
const boundImages = new WeakSet<HTMLImageElement>();
// Caches each image's intrinsic 100% width so zoom scales from a stable
// baseline instead of the live (already-scaled) rendered width.
const baseWidthCache = new WeakMap<HTMLImageElement, number>();

type ZoomKeyAction = "in" | "out" | "reset";

function isMac(): boolean {
  return /Mac|iPhone|iPad|iPod/i.test(navigator.platform);
}

function shouldZoomWithWheel(e: WheelEvent): boolean {
  // macOS users expect both Cmd+wheel and Ctrl+wheel to zoom; Linux users use
  // Ctrl+wheel. Accept either modifier on macOS, and Ctrl only elsewhere.
  return isMac() ? e.ctrlKey || e.metaKey : e.ctrlKey;
}

function zoomKeyAction(e: KeyboardEvent): ZoomKeyAction | null {
  if (!(e.ctrlKey || e.metaKey)) return null;

  const key = e.key;
  if (key === "+" || key === "=" || key === "Add" || e.code === "NumpadAdd") {
    return "in";
  }
  if (key === "-" || key === "_" || key === "Subtract" || e.code === "NumpadSubtract") {
    return "out";
  }
  if ((e.metaKey || e.ctrlKey) && key === "0") {
    return "reset";
  }
  return null;
}

export function initZoom(opts: { min: number; max: number; step: number; onChange: (pct: number) => void }): void {
  min = opts.min;
  max = opts.max;
  step = opts.step;
  onChange = opts.onChange;

  document.addEventListener(
    "wheel",
    (e) => {
      if (!shouldZoomWithWheel(e)) return;
      e.preventDefault();
      adjust(e.deltaY < 0 ? step : -step);
    },
    { passive: false, capture: true }
  );

  document.addEventListener(
    "keydown",
    (e) => {
      const action = zoomKeyAction(e);
      if (!action) return;
      e.preventDefault();
      e.stopPropagation();
      if (action === "in") adjust(step);
      else if (action === "out") adjust(-step);
      else zoomReset();
    },
    { capture: true }
  );

  // Re-fit images when the content column width changes (window resize, sidebar
  // toggles, etc.). Coalesce bursts of resize events into one rAF callback.
  let resizePending = false;
  const onResize = () => {
    if (resizePending) return;
    resizePending = true;
    requestAnimationFrame(() => {
      resizePending = false;
      syncImages();
    });
  };
  window.addEventListener("resize", onResize, { passive: true });
  const el = target();
  if (el && typeof ResizeObserver !== "undefined") {
    new ResizeObserver(onResize).observe(el);
  }

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
  syncImages();
  if (onChange) onChange(Math.round(level * 100));
}

function syncImages(): void {
  const el = target();
  if (!el) return;

  const computed = getComputedStyle(el);
  const paddingX = parseFloat(computed.paddingLeft) + parseFloat(computed.paddingRight);
  const contentWidth = Math.max(0, el.clientWidth - paddingX);

  if (contentWidth <= 0) return;

  for (const img of Array.from(el.querySelectorAll<HTMLImageElement>("img"))) {
    if (!boundImages.has(img)) {
      boundImages.add(img);
      img.addEventListener(
        "load",
        () => {
          baseWidthCache.delete(img);
          syncImages();
        },
        { passive: true }
      );
    }

    // Height-only sizing (e.g. ADO `=x600`): scale by the author height and let
    // the width follow the natural aspect ratio.
    const attrWidth = parseFloat(img.getAttribute("width") ?? "");
    const attrHeight = parseFloat(img.getAttribute("height") ?? "");
    if (!(Number.isFinite(attrWidth) && attrWidth > 0) && Number.isFinite(attrHeight) && attrHeight > 0) {
      img.style.height = `${Math.round(attrHeight * level)}px`;
      img.style.width = "auto";
      continue;
    }

    const intrinsicW = intrinsicWidth(img, attrWidth);
    if (intrinsicW <= 0) continue;

    // At 100% the image fits within the content column (downscaled only if its
    // intrinsic width is wider). Zoom scales that fitted size linearly, and is
    // allowed to exceed the column when zooming in — pixel images then simply
    // become pixelated rather than being capped at the column width.
    const fit = Math.min(1, contentWidth / intrinsicW);
    const width = intrinsicW * fit * level;
    img.style.width = `${Math.round(width)}px`;
    // Allow the image to exceed the content column when zoomed in; the CSS
    // `max-width: 100%` would otherwise re-cap it at the column width.
    img.style.maxWidth = "none";
    // Preserve an explicit author aspect ratio (e.g. ADO `=800x600`); otherwise
    // let the natural aspect ratio drive the height.
    if (Number.isFinite(attrHeight) && attrHeight > 0) {
      img.style.height = `${Math.round(attrHeight * fit * level)}px`;
    } else {
      img.style.height = "auto";
    }
  }
}

// Resolves a stable intrinsic 100% width for an image. Author-specified widths
// (HTML `width="..."` or ADO `=WxH`) win; otherwise the natural pixel width is
// used, cached so that later zoom steps never compound off the already-scaled
// width.
function intrinsicWidth(img: HTMLImageElement, attrWidth: number): number {
  if (Number.isFinite(attrWidth) && attrWidth > 0) return attrWidth;

  const cached = baseWidthCache.get(img);
  if (cached && cached > 0) return cached;

  if (img.naturalWidth > 0) {
    baseWidthCache.set(img, img.naturalWidth);
    return img.naturalWidth;
  }

  // SVG sources (e.g. shields.io badges) often report naturalWidth === 0.
  // Measure the intrinsic width with any inline width temporarily removed so
  // the reading is not the previously applied (scaled) width.
  const prevWidth = img.style.width;
  img.style.width = "";
  const measured = img.getBoundingClientRect().width;
  img.style.width = prevWidth;
  if (measured > 0) {
    baseWidthCache.set(img, measured);
    return measured;
  }
  return 0;
}

function clamp(v: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, v));
}

export function zoomLevel(): number {
  return level;
}

export function refreshZoom(): void {
  apply();
}
