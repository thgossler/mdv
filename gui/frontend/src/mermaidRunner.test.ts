import { describe, it, expect, vi, beforeEach } from "vitest";

// Mock Mermaid so the test is deterministic and offline: render() throws for a
// diagram whose source contains "BAD" and otherwise returns a stub SVG.
vi.mock("mermaid", () => {
  return {
    default: {
      initialize: vi.fn(),
      render: vi.fn(async (id: string, source: string) => {
        if (source.includes("BAD")) {
          throw new Error("Parse error in diagram");
        }
        const links = Array.from(
          source.matchAll(/https:\/\/mdv\.invalid\/__mdv_mermaid_[^\s"']+/g),
          (match) => `<a href="${match[0]}" target="_blank"><text>Link</text></a>`
        );
        return { svg: `<svg data-id="${id}">${links.join("") || "ok"}</svg>` };
      }),
    },
  };
});

import {
  getLinkDestination,
  prepareMermaidSvg,
  protectMermaidLinks,
  renderMermaid,
} from "./mermaidRunner";

function mermaidBlock(source: string): string {
  return `<pre><code class="language-mermaid">${source}</code></pre>`;
}

describe("mermaid link protection", () => {
  it("protects any number of label and click links without changing surrounding source", () => {
    const source =
      'flowchart LR\n' +
      '  A["<a href=\'https://example.com/?a=1&b=2\'>Web</a>"]\n' +
      '  B["<a href=\"docs/a & b.md\">Local</a>"]\n' +
      '  click A href "file:///Users/me/guide.md#setup" _blank\n' +
      '  click B href "../other.md"';
    const protectedSource = protectMermaidLinks(source);

    expect(protectedSource.source).not.toContain("https://example.com/");
    expect(protectedSource.source).not.toContain("docs/a & b.md");
    expect(protectedSource.links.size).toBe(4);
    expect([...protectedSource.links.values()]).toEqual([
      "https://example.com/?a=1&b=2",
      "docs/a & b.md",
      "file:///Users/me/guide.md#setup",
      "../other.md",
    ]);
    expect(protectedSource.source).toContain('A["<a href=\'');
    expect(protectedSource.source).toContain('click B href "');
  });

  it("leaves no-link source unchanged and makes unsafe destinations inert", () => {
    const plain = "graph TD; A-->B";
    expect(protectMermaidLinks(plain)).toMatchObject({ source: plain, links: new Map() });

    const protectedSource = protectMermaidLinks(
      'flowchart LR\n A["<a href=\" javascript:alert(1) \">Unsafe</a>"]'
    );
    expect(protectedSource.source).not.toContain("javascript:");
    expect([...protectedSource.links.values()]).toEqual([null]);
  });

  it("decodes HTML character references once in restored destinations", () => {
    const protectedSource = protectMermaidLinks(
      'flowchart LR\n A["<a href=\"https://example.com/?q=&quot;x&quot;&amp;y=1\">Link</a>"]'
    );
    expect([...protectedSource.links.values()]).toEqual([
      'https://example.com/?q="x"&y=1',
    ]);
  });

  it("restores protected links only as inert data attributes", () => {
    const protectedSource = protectMermaidLinks('flowchart LR\n click A href "docs/a.md"');
    const [placeholder] = protectedSource.links.keys();
    const svg = `<svg><a href="${placeholder}" target="_blank"><text>A</text></a><use href="#shape" /></svg>`;

    const root = document.createElement("div");
    root.innerHTML = prepareMermaidSvg(svg, protectedSource.links);
    const anchor = root.querySelector("a");
    expect(anchor?.getAttribute("data-mdv-href")).toBe("docs/a.md");
    expect(anchor?.hasAttribute("href")).toBe(false);
    expect(anchor?.hasAttribute("xlink:href")).toBe(false);
    expect(anchor?.hasAttribute("target")).toBe(false);
    expect(anchor && getLinkDestination(anchor)).toBe("docs/a.md");
    expect(root.querySelector("use")?.getAttribute("href")).toBe("#shape");
  });
});

describe("mermaid resilience", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
  });

  it("isolates a failing diagram so siblings still render", async () => {
    const root = document.createElement("div");
    root.innerHTML =
      mermaidBlock("graph TD; A-->B") +
      mermaidBlock("BAD diagram source") +
      mermaidBlock("graph LR; C-->D");
    document.body.appendChild(root);

    await renderMermaid(root, false);

    // Two good diagrams rendered, one error placeholder — the bad one does not
    // prevent the others from rendering.
    expect(root.querySelectorAll(".mermaid-diagram").length).toBe(2);
    const errors = root.querySelectorAll(".mermaid-error");
    expect(errors.length).toBe(1);
    expect(errors[0].textContent).toContain("Mermaid error");
    // No raw mermaid code blocks remain.
    expect(root.querySelectorAll("code.language-mermaid").length).toBe(0);
  });

  it("routes every rendered diagram link through inert mdv data", async () => {
    const root = document.createElement("div");
    root.innerHTML = mermaidBlock(
      'flowchart LR\n A[Web]\n click A href "https://example.com" _blank\n click A href "docs/guide.md"'
    );

    await renderMermaid(root, false);

    const links = Array.from(root.querySelectorAll("a"));
    expect(links.map(getLinkDestination)).toEqual(["https://example.com", "docs/guide.md"]);
    expect(links.every((link) => !link.hasAttribute("href"))).toBe(true);
    expect(links.every((link) => !link.hasAttribute("target"))).toBe(true);
  });

  it("does nothing when there are no diagrams", async () => {
    const root = document.createElement("div");
    root.innerHTML = "<p>no diagrams here</p>";
    await expect(renderMermaid(root, false)).resolves.toBeUndefined();
    expect(root.querySelector(".mermaid-error")).toBeNull();
  });

  it("preserves source ranges on rendered diagrams and errors", async () => {
    const root = document.createElement("div");
    root.innerHTML = '<pre data-source-line="4" data-source-end-line="7"><code class="language-mermaid">graph TD; A--&gt;B</code></pre>' +
      '<pre data-source-line="9" data-source-end-line="11"><code class="language-mermaid">BAD</code></pre>';
    await renderMermaid(root, false);
    expect(root.querySelector(".mermaid-diagram")?.getAttribute("data-source-line")).toBe("4");
    expect(root.querySelector(".mermaid-error")?.getAttribute("data-source-end-line")).toBe("11");
  });
});
