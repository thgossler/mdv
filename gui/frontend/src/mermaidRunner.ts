// mermaidRunner lazily loads Mermaid and renders any `mermaid` code blocks in
// the content element. Lazy loading keeps cold start fast for documents that do
// not use diagrams.
let mermaidMod: typeof import("mermaid").default | null = null;
let counter = 0;
let linkCounter = 0;

export interface ProtectedMermaidSource {
  source: string;
  links: Map<string, string | null>;
}

export function getLinkDestination(anchor: Element): string {
  return (
    anchor.getAttribute("data-mdv-href") ||
    anchor.getAttribute("href") ||
    anchor.getAttribute("xlink:href") ||
    ""
  );
}

export function protectMermaidLinks(source: string): ProtectedMermaidSource {
  const links = new Map<string, string | null>();
  let prefix: string;
  do {
    prefix = `https://mdv.invalid/__mdv_mermaid_${linkCounter++}_${Date.now()}_`;
  } while (source.includes(prefix));

  const protect = (raw: string): string => {
    const placeholder = `${prefix}${links.size}`;
    const href = decodeHtmlAttribute(raw);
    links.set(placeholder, isSupportedMermaidHref(href) ? href : null);
    return placeholder;
  };

  let protectedSource = source.replace(
    /(<a\b[^>]*?\bhref\s*=\s*)(["'])(.*?)\2/gi,
    (_match, before: string, quote: string, href: string) =>
      `${before}${quote}${protect(href)}${quote}`
  );
  protectedSource = protectedSource.replace(
    /(\bclick\s+[^\r\n]+?\s+href\s+)(["'])(.*?)\2/gi,
    (_match, before: string, quote: string, href: string) =>
      `${before}${quote}${protect(href)}${quote}`
  );
  return { source: protectedSource, links };
}

export function prepareMermaidSvg(svg: string, links: Map<string, string | null>): string {
  const container = document.createElement("div");
  container.innerHTML = svg;
  for (const anchor of container.querySelectorAll("a")) {
    const href = anchor.getAttribute("href") || anchor.getAttribute("xlink:href") || "";
    const original = links.get(href);
    if (original) {
      anchor.setAttribute("data-mdv-href", original);
    }
    anchor.removeAttribute("href");
    anchor.removeAttribute("xlink:href");
    anchor.removeAttribute("target");
    anchor.removeAttribute("download");
  }
  return container.innerHTML;
}

function decodeHtmlAttribute(value: string): string {
  if (!value.includes("&")) return value;
  const textarea = document.createElement("textarea");
  textarea.innerHTML = value;
  return textarea.value;
}

function isSupportedMermaidHref(href: string): boolean {
  if (/[\u0000-\u001f\u007f]/.test(href)) return false;
  const trimmed = href.trim();
  if (/^[#\/\.]/.test(trimmed) || /^[A-Za-z]:[\\/]/.test(trimmed)) return true;
  const scheme = trimmed.match(/^([A-Za-z][A-Za-z0-9+.-]*):/);
  if (!scheme) return true;
  return ["http", "https", "mailto", "file"].includes(scheme[1].toLowerCase());
}

function readMermaidSource(element: HTMLElement): string {
  const encoded = element.dataset.mermaidSource;
  if (!encoded) return element.textContent || "";
  try {
    return decodeURIComponent(encoded);
  } catch {
    return element.textContent || "";
  }
}

export async function renderMermaid(root: HTMLElement, dark: boolean): Promise<void> {
  const blocks = Array.from(
    root.querySelectorAll<HTMLElement>('pre code.language-mermaid, code.language-mermaid, .language-mermaid')
  );
  // Also handle fenced blocks emitted as <pre><code> with info mermaid.
  const fences = Array.from(root.querySelectorAll<HTMLElement>("pre > code")).filter((c) =>
    c.className.includes("language-mermaid")
  );
  const targets = dedupe([...blocks, ...fences]);
  if (targets.length === 0) return;

  if (!mermaidMod) {
    mermaidMod = (await import("mermaid")).default;
  }
  mermaidMod.initialize({
    startOnLoad: false,
    theme: dark ? "dark" : "default",
    securityLevel: "strict",
    fontFamily: "inherit",
  });

  for (const el of targets) {
    const host = el.closest("pre") || el;
    const source = readMermaidSource(el);
    const protectedSource = protectMermaidLinks(source);
    const id = `mermaid-${Date.now()}-${counter++}`;
    try {
      const { svg } = await mermaidMod.render(id, protectedSource.source);
      const wrap = document.createElement("div");
      wrap.className = "mermaid-diagram";
      if (host.dataset.sourceLine) wrap.dataset.sourceLine = host.dataset.sourceLine;
      if (host.dataset.sourceEndLine) wrap.dataset.sourceEndLine = host.dataset.sourceEndLine;
      wrap.innerHTML = prepareMermaidSvg(svg, protectedSource.links);
      host.replaceWith(wrap);
    } catch (e) {
      const err = document.createElement("div");
      err.className = "mermaid-error";
      if (host.dataset.sourceLine) err.dataset.sourceLine = host.dataset.sourceLine;
      if (host.dataset.sourceEndLine) err.dataset.sourceEndLine = host.dataset.sourceEndLine;
      err.textContent = "Mermaid error: " + (e instanceof Error ? e.message : String(e));
      host.replaceWith(err);
    }
  }
}

// renderMermaidSource renders a single Mermaid document (for `.mmd` files).
export async function renderMermaidSource(
  root: HTMLElement,
  source: string,
  dark: boolean
): Promise<void> {
  if (!mermaidMod) mermaidMod = (await import("mermaid")).default;
  mermaidMod.initialize({ startOnLoad: false, theme: dark ? "dark" : "default", securityLevel: "strict" });
  const id = `mermaid-${Date.now()}-${counter++}`;
  const protectedSource = protectMermaidLinks(source);
  try {
    const { svg } = await mermaidMod.render(id, protectedSource.source);
    root.innerHTML = `<div class="mermaid-diagram standalone">${prepareMermaidSvg(
      svg,
      protectedSource.links
    )}</div>`;
  } catch (e) {
    root.innerHTML = `<div class="mermaid-error">Mermaid error: ${
      e instanceof Error ? e.message : String(e)
    }</div>`;
  }
}

function dedupe<T>(arr: T[]): T[] {
  return Array.from(new Set(arr));
}
