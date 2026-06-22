import { describe, it, expect, vi, afterEach } from "vitest";
import DOMPurify from "dompurify";
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

  it("does not throw on invalid / unsupported syntax", () => {
    const ugly =
      "This *is **broken _markup [with](unclosed and ` stray backtick\n\n" +
      "<div><span><p>unclosed <img src= <<<>>> &notanentity;\n\n" +
      ":::weird-extension {opts}\nbody\n:::\n\n```mermaid\ngraph TD; A--B\n```\nAfter\n";
    expect(() => render(ugly)).not.toThrow();
    const res = render(ugly);
    expect(res.html).toContain("After");
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
