// mermaidRunner lazily loads Mermaid and renders any `mermaid` code blocks in
// the content element. Lazy loading keeps cold start fast for documents that do
// not use diagrams.
let mermaidMod: typeof import("mermaid").default | null = null;
let counter = 0;

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
    const source = el.textContent || "";
    const id = `mermaid-${Date.now()}-${counter++}`;
    try {
      const { svg } = await mermaidMod.render(id, source);
      const wrap = document.createElement("div");
      wrap.className = "mermaid-diagram";
      wrap.innerHTML = svg;
      host.replaceWith(wrap);
    } catch (e) {
      const err = document.createElement("div");
      err.className = "mermaid-error";
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
  try {
    const { svg } = await mermaidMod.render(id, source);
    root.innerHTML = `<div class="mermaid-diagram standalone">${svg}</div>`;
  } catch (e) {
    root.innerHTML = `<div class="mermaid-error">Mermaid error: ${
      e instanceof Error ? e.message : String(e)
    }</div>`;
  }
}

function dedupe<T>(arr: T[]): T[] {
  return Array.from(new Set(arr));
}
