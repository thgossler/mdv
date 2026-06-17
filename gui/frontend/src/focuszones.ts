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
// re-interprets the navigation keys (the cause of Home/End behaving erratically).

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
    { kind: "list", el: refs.navList, itemSelector: ".nav-item", activateOnMove: true },
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

  // Navigate inside a list zone. Moving the selection optionally activates the
  // focused item (clicks it) so the content view follows the highlighted
  // document/section, while keyboard focus stays in the list.
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
    else if (isHomeKey(e)) target = items[0];
    else if (isEndKey(e)) target = items[items.length - 1];
    else return false;

    target.focus();
    target.scrollIntoView({ block: "nearest" });
    lastItem.set(z.el, target);
    if (z.activateOnMove && target !== active) target.click();
    return true;
  };

  // Single capture-phase keydown handler for everything: Tab cycling, list
  // navigation, and content Home/End scrolling. Capture runs before WKWebView's
  // native handling, so the navigation keys behave consistently.
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

      // List navigation when focus is inside one of the list zones.
      const listZone = listZoneFor(active);
      if (listZone) {
        if (handleList(listZone, e)) {
          e.preventDefault();
          e.stopImmediatePropagation();
        }
        return;
      }

      // Plain Home/End scroll the content view, unless an editable field is
      // focused (those keep their caret behaviour).
      if ((isHomeKey(e) || isEndKey(e)) && !hasModifier(e) && !isTypingTarget(active)) {
        e.preventDefault();
        e.stopImmediatePropagation();
        refs.contentWrap.scrollTo({
          top: isHomeKey(e) ? 0 : refs.contentWrap.scrollHeight,
          behavior: "smooth",
        });
      }
    },
    true
  );

  // Clicking an item (mouse) records it as the remembered selection.
  for (const z of listZones) {
    z.el.addEventListener("focusin", (e) => {
      const t = e.target as HTMLElement;
      if (t && t.matches(z.itemSelector)) lastItem.set(z.el, t);
    });
  }
}
