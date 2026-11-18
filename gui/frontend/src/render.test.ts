import { describe, it, expect, vi, afterEach } from "vitest";
import DOMPurify from "dompurify";
import { extractFrontmatter, sourceLineMapper } from "./frontmatter";
import { render } from "./render";

// These tests pin down the resilience contract of the GUI render pipeline: a
// single malformed or unusual construct must never blank the whole document.

describe("render resilience", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders a normal document", () => {
    const res = render("# Title\n\nHello **world**.\n");
    expect(res.html).toContain("Title");
    expect(res.html).toContain("<strong>world</strong>");
    expect(res.headings.length).toBeGreaterThan(0);
  });

  it("preserves source ranges on paragraphs and fenced code", () => {
    const result = render("First\nsecond\n\n```go\nline one\nline two\n```\n");
    const root = document.createElement("div");
    root.innerHTML = result.html;
    expect(root.querySelector("p")?.getAttribute("data-source-line")).toBe("1");
    expect(root.querySelector("p")?.getAttribute("data-source-end-line")).toBe("2");
    expect(root.querySelector('[data-source-line="4"]')?.getAttribute("data-source-end-line")).toBe("7");
  });

  it("parses YAML frontmatter", () => {
    expect(extractFrontmatter("---\ntitle: Example\ntags: [one, two]\n---\n\n# Body\n")).toEqual({
      data: { title: "Example", tags: ["one", "two"] },
      body: "# Body\n",
    });
  });

  it("maps heading lines past frontmatter and preserved comments", () => {
    const markdown = "<!-- marker -->\n---\ntitle: example\n---\n\n# Heading\n";
    const body = extractFrontmatter(markdown).body;
    const heading = render(body).headings[0];
    expect(sourceLineMapper(markdown, body)(heading.line)).toBe(6);
  });

  it("renders alerts, video links, and external links", () => {
    const result = render("> [!NOTE]\n> Important\n\n:::video https://example.com/demo\n:::\n\n[External](https://example.com)");
    expect(result.html).toContain("markdown-alert-note");
    expect(result.html).toContain("icon-note");
    expect(result.html).toContain("video-embed");
    expect(result.html).toContain("external-link");
  });

  it("does not throw on invalid / unsupported syntax", () => {
    const ugly =
      "This *is **broken _markup [with](unclosed and ` stray backtick\n\n" +
      "<div><span><p>unclosed <img src= <<<>>> &notanentity;\n\n" +
      ":::weird-extension {opts}\nbody\n:::\n\n```mermaid\ngraph TD; A--B\n```\nAfter\n";
    expect(() => render(ugly)).not.toThrow();
    const res = render(ugly);
    expect(res.html).toContain("After");
  });

  it("preserves raw Mermaid source through HTML escaping", () => {
    const source = 'flowchart LR\n A["<a href=\"https://example.com/?q=&quot;x&quot;&amp;y=1\">Link</a>"]\n';
    const result = render(`\`\`\`mermaid\n${source}\`\`\`\n`);
    const root = document.createElement("div");
    root.innerHTML = result.html;
    const code = root.querySelector<HTMLElement>("code.language-mermaid");
    expect(code?.dataset.mermaidSource).toBeDefined();
    expect(decodeURIComponent(code?.dataset.mermaidSource || "")).toBe(source);
  });

  it("renders a massive table without throwing", () => {
    let md = "| A | B | C |\n| - | - | - |\n";
    for (let i = 0; i < 3000; i++) md += `| ${i} | val | x |\n`;
    expect(() => render(md)).not.toThrow();
    expect(render(md).html).toContain("<table");
  });

  it("renders a deeply nested list without throwing", () => {
    let md = "";
    for (let i = 0; i < 400; i++) md += "  ".repeat(i) + `- level ${i}\n`;
    expect(() => render(md)).not.toThrow();
    expect(render(md).html).toContain("level 0");
  });

  it("strips a leading UTF-8 BOM", () => {
    const res = render("\uFEFF# Heading\n");
    expect(res.html).not.toContain("\uFEFF");
    expect(res.html).toContain("Heading");
  });

  it("falls back to a readable error panel when rendering throws", () => {
    vi.spyOn(DOMPurify, "sanitize").mockImplementation(() => {
      throw new Error("sanitize boom");
    });
    const res = render("# hello\n\nimportant body text\n");
    expect(res.html).toContain("could not be fully rendered");
    // The raw source is preserved so no content is lost.
    expect(res.html).toContain("important body text");
    expect(res.headings).toEqual([]);
  });
});
