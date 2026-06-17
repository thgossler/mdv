// focuszones implements Tab navigation between the five main UI areas and
// Arrow/Home/End-key navigation within the list areas (document navigator, TOC,
// backlinks) plus Home/End scrolling of the content view.
//
// Tab / Shift+Tab cycle focus between, in order:
//   1. the navigator search field (#nav-filter)
//   2. the navigator document list (#nav-list, items .nav-item)
//   3. the content view (#content-wrap)
//   4. the in-page contents list (#toc-list, items .toc-item)
//   5. the backlinks list (#backlinks-list, items .backlink-item)
//
// The focused area is outlined (see app.css :focus / :focus-within rules). Inside
// the list areas, ArrowUp/ArrowDown move between items, Home/End jump to the
// first/last item, and Enter/Space activate the focused item. In the content view
// (or anywhere that is not a list or an editable field), plain Home/End scroll the
// content to the top/bottom. Areas that are currently hidden are skipped.
//
// All keyboard handling runs in the capture phase on `window` so the events are
// seen before WKWebView's native key handling, which otherwise swallows or
// re-interprets the navigation keys. Plain/Ctrl/Cmd Home/End never reach the
// webview's JS on macOS at all (Wails' native NSWindow keyDown: intercepts them),
// so they are additionally delivered via the `key:home` / `key:end` Wails events
// registered as key bindings in gui/main.go.

import { Events } from "@wailsio/runtime";

export interface FocusZonesRefs {
  navFilter: HTMLInputElement;
  navList: HTMLElement;
  contentWrap: HTMLElement;
  tocList: HTMLElement;
  backlinksList: HTMLElement;
}

type Zone =
  | { kind: "input" | "content"; el: HTMLElement }
  | { kind: "list"; el: HTMLElement; itemSelector: string; activateOnMove: boolean };

export function initFocusZones(refs: FocusZonesRefs): void {
  const zones: Zone[] = [
    { kind: "input", el: refs.navFilter },
    { kind: "list", el: refs.navList, itemSelector: ".nav-item, .nav-match", activateOnMove: true },
    { kind: "content", el: refs.contentWrap },
    { kind: "list", el: refs.tocList, itemSelector: ".toc-item", activateOnMove: true },
    // Backlinks navigate to other documents (which rebuilds this very list), so
    // moving the selection must not auto-activate — only Enter/Space opens one.
    { kind: "list", el: refs.backlinksList, itemSelector: ".backlink-item", activateOnMove: false },
  ];

  const listZones = zones.filter(
    (z): z is Extract<Zone, { kind: "list" }> => z.kind === "list"
  );

  // Remembers the last focused item per list so returning to a list restores the
  // previous selection instead of jumping back to the top.
  const lastItem = new WeakMap<HTMLElement, HTMLElement>();

  const isVisible = (el: HTMLElement): boolean => {
    const r = el.getBoundingClientRect();
    return r.width > 0 && r.height > 0;
  };

  const zoneVisible = (z: Zone): boolean => isVisible(z.el);

  const itemsOf = (z: Extract<Zone, { kind: "list" }>): HTMLElement[] =>
    Array.from(z.el.querySelectorAll<HTMLElement>(z.itemSelector));

  const focusZone = (z: Zone): void => {
    if (z.kind === "list") {
      const items = itemsOf(z);
      if (items.length === 0) {
        z.el.focus({ preventScroll: true });
        return;
      }
      const remembered = lastItem.get(z.el);
      const active = items.find((i) => i.classList.contains("active"));
      const target = (remembered && items.includes(remembered) ? remembered : active) ?? items[0];
      target.focus();
      target.scrollIntoView({ block: "nearest" });
    } else {
      z.el.focus({ preventScroll: true });
    }
  };

  const currentZoneIndex = (): number => {
    const active = document.activeElement;
    if (!active) return -1;
    return zones.findIndex((z) => z.el === active || z.el.contains(active));
  };

  const listZoneFor = (el: HTMLElement | null): Extract<Zone, { kind: "list" }> | null => {
    if (!el) return null;
    return listZones.find((z) => z.el.contains(el)) ?? null;
  };

  const isTypingTarget = (el: HTMLElement | null): boolean => {
    if (!el) return false;
    const tag = el.tagName;
    return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || el.isContentEditable;
  };

  const hasModifier = (e: KeyboardEvent): boolean =>
    e.ctrlKey || e.metaKey || e.altKey || e.shiftKey;

  // Match both `key` and `code` because WKWebView is inconsistent about which it
  // reports for the navigation keys depending on keyboard layout.
  const isHomeKey = (e: KeyboardEvent): boolean => e.key === "Home" || e.code === "Home";
  const isEndKey = (e: KeyboardEvent): boolean => e.key === "End" || e.code === "End";
  const isDownKey = (e: KeyboardEvent): boolean => e.key === "ArrowDown" || e.code === "ArrowDown";
  const isUpKey = (e: KeyboardEvent): boolean => e.key === "ArrowUp" || e.code === "ArrowUp";

  // Jump to the first/last item of the focused list, or scroll the content view
  // to the top/bottom when focus is not in a list. Shared by the keyboard
  // handler and the native `key:home` / `key:end` events.
  const goToEdge = (toStart: boolean): void => {
    const active = document.activeElement as HTMLElement | null;
    const listZone = listZoneFor(active);
    if (listZone) {
      const items = itemsOf(listZone);
      if (items.length === 0) return;
      const target = toStart ? items[0] : items[items.length - 1];
      target.focus();
      target.scrollIntoView({ block: "nearest" });
      lastItem.set(listZone.el, target);
      if (listZone.activateOnMove && target !== active) target.click();
      return;
    }
    if (isTypingTarget(active)) return; // keep caret behaviour in editable fields
    refs.contentWrap.scrollTo({
      top: toStart ? 0 : refs.contentWrap.scrollHeight,
      behavior: "smooth",
    });
  };

  // Navigate inside a list zone with Arrow keys / Enter. Moving the selection
  // optionally activates the focused item (clicks it) so the content view follows
  // the highlighted document/section, while keyboard focus stays in the list.
  const handleList = (z: Extract<Zone, { kind: "list" }>, e: KeyboardEvent): boolean => {
    const items = itemsOf(z);
    if (items.length === 0) return false;
    const active = document.activeElement as HTMLElement | null;
    const idx = active ? items.indexOf(active) : -1;

    if (e.key === "Enter" || e.key === " " || e.code === "Space") {
      if (idx >= 0) {
        items[idx].click();
        return true;
      }
      return false;
    }

    if (hasModifier(e)) return false;

    let target: HTMLElement | null = null;
    if (isDownKey(e)) target = items[Math.min(items.length - 1, idx + 1)] ?? items[0];
    else if (isUpKey(e)) target = items[Math.max(0, idx - 1)] ?? items[items.length - 1];
    else return false;

    target.focus();
    target.scrollIntoView({ block: "nearest" });
    lastItem.set(z.el, target);
    if (z.activateOnMove && target !== active) target.click();
    return true;
  };

  // Single capture-phase keydown handler: Tab cycling, list Arrow/Enter
  // navigation, and Home/End edge jumps. Capture runs before WKWebView's native
  // handling. Home/End are also delivered as Wails events (see below) because on
  // macOS they never reach JS keydown at all.
  window.addEventListener(
    "keydown",
    (e) => {
      const active = document.activeElement as HTMLElement | null;

      // Tab / Shift+Tab cycle between the visible zones.
      if (e.key === "Tab") {
        const visible = zones.filter(zoneVisible);
        if (visible.length === 0) return;
        e.preventDefault();
        const current = currentZoneIndex();
        const currentZone = current >= 0 ? zones[current] : null;
        let pos = currentZone ? visible.indexOf(currentZone) : -1;
        if (pos === -1) pos = e.shiftKey ? 0 : visible.length - 1;
        const next = visible[(pos + (e.shiftKey ? -1 : 1) + visible.length) % visible.length];
        focusZone(next);
        return;
      }

      // Home/End (alone or with Ctrl/Cmd) jump to the edge of the focused area.
      // Shift/Alt are excluded so text selection / native combos are untouched.
      if ((isHomeKey(e) || isEndKey(e)) && !e.shiftKey && !e.altKey) {
        if (isTypingTarget(active)) return;
        e.preventDefault();
        e.stopImmediatePropagation();
        goToEdge(isHomeKey(e));
        return;
      }

      // Arrow / Enter navigation inside one of the list zones.
      const listZone = listZoneFor(active);
      if (listZone && handleList(listZone, e)) {
        e.preventDefault();
        e.stopImmediatePropagation();
      }
    },
    true
  );

  // Native delivery of Home/End: on macOS these are intercepted by Wails'
  // NSWindow keyDown: handler and forwarded as key bindings (gui/main.go), so the
  // capture-phase listener above never sees them. The events drive the same jump.
  Events.On("key:home", () => goToEdge(true));
  Events.On("key:end", () => goToEdge(false));

  // Clicking an item (mouse) records it as the remembered selection.
  for (const z of listZones) {
    z.el.addEventListener("focusin", (e) => {
      const t = e.target as HTMLElement;
      if (t && t.matches(z.itemSelector)) lastItem.set(z.el, t);
    });
  }
}
