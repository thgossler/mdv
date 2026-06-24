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
// Caches each image's intrinsic height/width ratio so the height can be set
// explicitly. WebKit's `height:auto` computation is unreliable for SVG sources
// that report `naturalWidth === 0` (e.g. shields.io badges), which otherwise
// renders rows of equal-height badges at slightly different heights.
const aspectCache = new WeakMap<HTMLImageElement, number>();

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
          aspectCache.delete(img);
          syncImages();
        },
        { passive: true }
      );
    }

    // Remote-blocked placeholders are sized by CSS (the dashed box). Their box
    // dimensions are not the real image's, so measuring or caching them would
    // leak a wrong size once the image is unblocked. Leave them untouched.
    if (img.classList.contains("remote-blocked")) {
      clearImageSizing(img);
      baseWidthCache.delete(img);
      aspectCache.delete(img);
      continue;
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
    // Without an author width or a reliable natural pixel width (SVG badges
    // report naturalWidth === 0 in WebKit), leave the image at its natural CSS
    // size. The `max-width: 100%` rule still fits it to the column. Forcing a
    // measured width here produced inconsistent badge heights and broke
    // re-rendering when remote images were unblocked.
    if (intrinsicW <= 0) {
      clearImageSizing(img);
      continue;
    }

    // The content column itself scales with zoom (its max-width is
    // `--content-width * --zoom`), so contentWidth already includes the zoom
    // factor. Fit each image against the *unzoomed* column width, then re-apply
    // the zoom once, so images track the column instead of growing twice as
    // fast. When the window is narrower than the zoomed column, contentWidth is
    // clamped to the window, which keeps images within view.
    const baseContentWidth = level > 0 ? contentWidth / level : contentWidth;
    const fit = Math.min(1, baseContentWidth / intrinsicW);
    const width = intrinsicW * fit * level;
    img.style.width = `${Math.round(width)}px`;
    // Allow the image to exceed the content column when zoomed in; the CSS
    // `max-width: 100%` would otherwise re-cap it at the column width.
    img.style.maxWidth = "none";
    // Preserve an explicit author aspect ratio (e.g. ADO `=800x600`); otherwise
    // derive the height from the cached intrinsic aspect ratio. Setting the
    // height explicitly (rather than `auto`) keeps SVG badge rows uniform,
    // because WebKit's `height:auto` is unreliable for sources that report
    // `naturalWidth === 0`.
    if (Number.isFinite(attrHeight) && attrHeight > 0) {
      img.style.height = `${Math.round(attrHeight * fit * level)}px`;
    } else {
      const ratio = aspectCache.get(img);
      if (ratio && ratio > 0) {
        img.style.height = `${Math.round(width * ratio)}px`;
      } else {
        img.style.height = "auto";
      }
    }

    // Keep an explicitly centered image (inside an `align="center"` block)
    // visually centered even when zoomed wider than the column. WebKit anchors
    // an overflowing inline image to the left edge, so re-center it
    // deterministically once it exceeds the content width.
    centerOverflowingImage(img, width > contentWidth);
  }
}

// centerOverflowingImage centers an image that has grown wider than its content
// column. It is applied only to images within an `align="center"` container so
// left-aligned and inline images (e.g. badge rows) are left untouched. When the
// image fits again, the inline overrides are cleared so normal `text-align`
// centering resumes.
function centerOverflowingImage(img: HTMLImageElement, overflowing: boolean): void {
  if (overflowing && img.closest('[align="center"]')) {
    // display:block gives a deterministic left-anchored normal position; the
    // left/transform pair then shifts the image to the column's center.
    img.style.display = "block";
    img.style.position = "relative";
    img.style.left = "50%";
    img.style.transform = "translateX(-50%)";
  } else {
    img.style.display = "";
    img.style.position = "";
    img.style.left = "";
    img.style.transform = "";
  }
}

// Resolves a stable intrinsic 100% width for an image. Author-specified widths
// (HTML `width="..."` or ADO `=WxH`) win; otherwise the natural pixel width is
// used, cached so that later zoom steps never compound off the already-scaled
// width. Returns 0 when no reliable intrinsic width is available (e.g. SVG
// sources that report naturalWidth === 0), so the caller can leave the image at
// its natural CSS size instead of forcing an unreliable measured size.
function intrinsicWidth(img: HTMLImageElement, attrWidth: number): number {
  if (Number.isFinite(attrWidth) && attrWidth > 0) return attrWidth;

  const cached = baseWidthCache.get(img);
  if (cached && cached > 0) return cached;

  if (img.naturalWidth > 0) {
    baseWidthCache.set(img, img.naturalWidth);
    if (img.naturalHeight > 0) {
      aspectCache.set(img, img.naturalHeight / img.naturalWidth);
    }
    return img.naturalWidth;
  }

  return 0;
}

// clearImageSizing removes any inline sizing this module applied, restoring the
// element to its natural, CSS-driven size.
function clearImageSizing(img: HTMLImageElement): void {
  img.style.width = "";
  img.style.height = "";
  img.style.maxWidth = "";
  centerOverflowingImage(img, false);
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
