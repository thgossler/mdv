// print.ts is the entry point for the standalone PDF "print" page. It renders
// the injected Markdown with the exact same pipeline as the GUI and then signals
// readiness so the headless browser driver can capture the page as a PDF.
//
// The launcher injects two globals before navigation:
//   window.__mdvMarkdown  – the raw Markdown source (required)
//   window.__mdvExtended  – whether extended syntax is enabled (optional)
// and polls window.__mdvPdfReady to know when the document is fully laid out
// (fonts loaded, images decoded, Mermaid diagrams rendered).
//
// Relative images resolve over HTTP against the local print server, so no
// bridge or asset-inlining step is needed here.

import "katex/dist/katex.min.css";
import "./styles/themes.css";
import "./styles/markdown.css";
import "./styles/alerts.css";
import "./styles/code.css";
import "./styles/print.css";

import { render } from "./render";
import { renderMermaid } from "./mermaidRunner";
import { extractFrontmatter, renderFrontmatter } from "./frontmatter";

declare global {
  interface Window {
    __mdvMarkdown?: string;
    __mdvExtended?: boolean;
    __mdvPdfReady?: boolean;
  }
}

async function run(): Promise<void> {
  const content = document.getElementById("content") as HTMLElement;
  const markdown = window.__mdvMarkdown ?? "";
  const extended = window.__mdvExtended === true;

  const fm = extractFrontmatter(markdown);
  const result = render(fm.body, extended);
  content.innerHTML = renderFrontmatter(fm.data) + result.html;

  // Render Mermaid diagrams using the light theme (print is always light).
  try {
    await renderMermaid(content, false);
  } catch {
    // A diagram failure should not block the export.
  }

  // Wait for web fonts and images so the captured page is complete.
  try {
    await (document as Document & { fonts?: FontFaceSet }).fonts?.ready;
  } catch {
    // Older engines without the Font Loading API: ignore.
  }
  await waitForImages(content);

  window.__mdvPdfReady = true;
}

// waitForImages resolves once every <img> has loaded or failed, so the PDF is
// not captured before remote/relative images have been decoded.
function waitForImages(root: HTMLElement): Promise<void> {
  const imgs = Array.from(root.querySelectorAll("img"));
  return Promise.all(
    imgs.map((img) =>
      img.complete
        ? Promise.resolve()
        : new Promise<void>((resolve) => {
            img.addEventListener("load", () => resolve(), { once: true });
            img.addEventListener("error", () => resolve(), { once: true });
          })
    )
  ).then(() => undefined);
}

void run();
