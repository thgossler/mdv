// search implements in-document find with highlight and next/prev navigation.
// It walks text nodes inside the content element and wraps matches in <mark>.

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
