// search implements in-document find with highlight and next/prev navigation.
// It walks text nodes inside the content element and wraps matches in <mark>.

// maxSearchChars caps the amount of visible text the in-document find will walk.
// Above it, highlighting is skipped to keep the UI responsive: building <mark>
// nodes across a multi-megabyte document would block the main thread. The
// backend already refuses to load documents larger than its own size limit, so
// this only affects unusually large-but-loadable files.
const maxSearchChars = 4_000_000;

let matches: HTMLElement[] = [];
let current = -1;
let countEl: HTMLElement | null = null;

export function initSearch(refs: {
  bar: HTMLElement;
  input: HTMLInputElement;
  count: HTMLElement;
  next: HTMLElement;
  prev: HTMLElement;
  close: HTMLElement;
}): void {
  countEl = refs.count;

  refs.input.addEventListener("input", () => run(refs.input.value));
  refs.input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      e.preventDefault();
      e.shiftKey ? step(-1) : step(1);
    } else if (e.key === "Escape") {
      hide(refs.bar);
    }
  });
  refs.next.addEventListener("click", () => step(1));
  refs.prev.addEventListener("click", () => step(-1));
  refs.close.addEventListener("click", () => hide(refs.bar));
}

export function showSearch(bar: HTMLElement, input: HTMLInputElement): void {
  bar.classList.remove("hidden");
  input.focus();
  input.select();
}

function hide(bar: HTMLElement): void {
  bar.classList.add("hidden");
  clear();
}

export function clearSearch(): void {
  clear();
}

function run(query: string): void {
  clear();
  const content = document.getElementById("content");
  if (!content || !query.trim()) {
    update();
    return;
  }
  if ((content.textContent?.length ?? 0) > maxSearchChars) {
    if (countEl) countEl.textContent = "too large";
    return;
  }
  const q = query.toLowerCase();
  const walker = document.createTreeWalker(content, NodeFilter.SHOW_TEXT, {
    acceptNode: (node) => {
      const p = node.parentElement;
      if (!p) return NodeFilter.FILTER_REJECT;
      if (p.closest("script,style,.code-copy")) return NodeFilter.FILTER_REJECT;
      return node.nodeValue && node.nodeValue.toLowerCase().includes(q)
        ? NodeFilter.FILTER_ACCEPT
        : NodeFilter.FILTER_REJECT;
    },
  });

  const toProcess: Text[] = [];
  let n: Node | null;
  while ((n = walker.nextNode())) toProcess.push(n as Text);

  for (const textNode of toProcess) highlightNode(textNode, q);

  matches = Array.from(content.querySelectorAll<HTMLElement>("mark.search-hit"));
  if (matches.length) step(1, true);
  update();
}

function highlightNode(textNode: Text, q: string): void {
  const text = textNode.nodeValue || "";
  const lower = text.toLowerCase();
  const frag = document.createDocumentFragment();
  let i = 0;
  let idx = lower.indexOf(q, i);
  if (idx < 0) return;
  while (idx >= 0) {
    if (idx > i) frag.appendChild(document.createTextNode(text.slice(i, idx)));
    const mark = document.createElement("mark");
    mark.className = "search-hit";
    mark.textContent = text.slice(idx, idx + q.length);
    frag.appendChild(mark);
    i = idx + q.length;
    idx = lower.indexOf(q, i);
  }
  if (i < text.length) frag.appendChild(document.createTextNode(text.slice(i)));
  textNode.parentNode?.replaceChild(frag, textNode);
}

function step(dir: number, initial = false): void {
  if (matches.length === 0) return;
  if (current >= 0 && matches[current]) matches[current].classList.remove("current");
  current = initial ? 0 : (current + dir + matches.length) % matches.length;
  const el = matches[current];
  el.classList.add("current");
  el.scrollIntoView({ block: "center", behavior: "smooth" });
  update();
}

function update(): void {
  if (!countEl) return;
  countEl.textContent = matches.length ? `${current + 1}/${matches.length}` : "0/0";
}

function clear(): void {
  const content = document.getElementById("content");
  if (content) {
    for (const m of Array.from(content.querySelectorAll("mark.search-hit"))) {
      const parent = m.parentNode;
      if (!parent) continue;
      parent.replaceChild(document.createTextNode(m.textContent || ""), m);
      parent.normalize();
    }
  }
  matches = [];
  current = -1;
  update();
}

// jumpToContentMatch highlights every occurrence of the given keywords in the
// freshly-rendered document and scrolls to the match nearest to `sourceLine`
// (the 1-based raw-markdown row reported by the content search), reusing the
// same highlight styling as in-document find. It is used when the user clicks a
// content-search result row in the navigator.
export function jumpToContentMatch(keywords: string[], sourceLine: number): void {
  clear();
  const content = document.getElementById("content");
  if (!content) {
    update();
    return;
  }
  const kws = keywords.map((k) => k.toLowerCase().trim()).filter(Boolean);
  if (kws.length === 0) {
    update();
    return;
  }
  if ((content.textContent?.length ?? 0) > maxSearchChars) {
    if (countEl) countEl.textContent = "too large";
    return;
  }
  const walker = document.createTreeWalker(content, NodeFilter.SHOW_TEXT, {
    acceptNode: (node) => {
      const p = node.parentElement;
      if (!p) return NodeFilter.FILTER_REJECT;
      if (p.closest("script,style,.code-copy")) return NodeFilter.FILTER_REJECT;
      const lower = node.nodeValue?.toLowerCase() ?? "";
      return kws.some((k) => lower.includes(k))
        ? NodeFilter.FILTER_ACCEPT
        : NodeFilter.FILTER_REJECT;
    },
  });

  const toProcess: Text[] = [];
  let n: Node | null;
  while ((n = walker.nextNode())) toProcess.push(n as Text);
  for (const textNode of toProcess) highlightNodeMulti(textNode, kws);

  matches = Array.from(content.querySelectorAll<HTMLElement>("mark.search-hit"));
  const target = pickBySourceLine(matches, sourceLine);
  if (target >= 0) {
    current = target;
    matches[current].classList.add("current");
    matches[current].scrollIntoView({ block: "center", behavior: "smooth" });
    update();
  } else if (matches.length) {
    step(1, true);
  } else {
    update();
  }
}

// highlightNodeMulti wraps occurrences of any of the keywords within one text
// node, choosing the earliest match at each position.
function highlightNodeMulti(textNode: Text, kws: string[]): void {
  const text = textNode.nodeValue || "";
  const lower = text.toLowerCase();
  const frag = document.createDocumentFragment();
  let i = 0;
  let any = false;
  while (i < text.length) {
    let bestIdx = -1;
    let bestLen = 0;
    for (const k of kws) {
      const idx = lower.indexOf(k, i);
      if (idx >= 0 && (bestIdx < 0 || idx < bestIdx)) {
        bestIdx = idx;
        bestLen = k.length;
      }
    }
    if (bestIdx < 0) break;
    any = true;
    if (bestIdx > i) frag.appendChild(document.createTextNode(text.slice(i, bestIdx)));
    const mark = document.createElement("mark");
    mark.className = "search-hit";
    mark.textContent = text.slice(bestIdx, bestIdx + bestLen);
    frag.appendChild(mark);
    i = bestIdx + bestLen;
  }
  if (!any) return;
  if (i < text.length) frag.appendChild(document.createTextNode(text.slice(i)));
  textNode.parentNode?.replaceChild(frag, textNode);
}

// pickBySourceLine returns the index of the highlighted match that best
// corresponds to `sourceLine`: the one inside the block whose data-source-line
// is the greatest value not exceeding the target. Returns -1 when no match has
// a usable source line.
function pickBySourceLine(marks: HTMLElement[], sourceLine: number): number {
  let best = -1;
  let bestLine = -1;
  for (let i = 0; i < marks.length; i++) {
    const block = marks[i].closest("[data-source-line]") as HTMLElement | null;
    if (!block) continue;
    const line = parseInt(block.dataset.sourceLine || "", 10);
    if (!Number.isFinite(line)) continue;
    if (line <= sourceLine && line > bestLine) {
      bestLine = line;
      best = i;
    }
  }
  return best;
}
