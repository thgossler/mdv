import type { HeadingInfo } from "./md/anchors";

// buildTOC renders the table-of-contents list and wires smooth-scroll plus
// active-heading tracking (scroll spy) against the content scroll container.
export function buildTOC(
  listEl: HTMLElement,
  headings: HeadingInfo[],
  onNavigate: (slug: string) => void
): void {
  listEl.innerHTML = "";
  if (headings.length === 0) {
    listEl.innerHTML = '<div class="toc-empty">No headings</div>';
    return;
  }
  const minLevel = Math.min(...headings.map((h) => h.level));
  for (const h of headings) {
    const a = document.createElement("a");
    a.className = "toc-item";
    a.tabIndex = -1;
    a.textContent = h.text;
    a.href = `#${h.slug}`;
    a.dataset.slug = h.slug;
    a.style.paddingLeft = `${(h.level - minLevel) * 12 + 8}px`;
    a.addEventListener("click", (e) => {
      e.preventDefault();
      onNavigate(h.slug);
    });
    listEl.appendChild(a);
  }
}

// trackActiveHeading highlights the TOC entry nearest the top of the viewport.
export function trackActiveHeading(scroller: HTMLElement, listEl: HTMLElement): () => void {
  const handler = () => {
    const headings = Array.from(
      scroller.querySelectorAll<HTMLElement>("h1[id],h2[id],h3[id],h4[id],h5[id],h6[id]")
    );
    let activeSlug = "";
    const top = scroller.getBoundingClientRect().top + 80;
    for (const h of headings) {
      if (h.getBoundingClientRect().top <= top) activeSlug = h.id;
      else break;
    }
    for (const item of Array.from(listEl.querySelectorAll<HTMLElement>(".toc-item"))) {
      item.classList.toggle("active", item.dataset.slug === activeSlug);
    }
  };
  scroller.addEventListener("scroll", handler, { passive: true });
  handler();
  return () => scroller.removeEventListener("scroll", handler);
}
