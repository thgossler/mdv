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
        return { svg: `<svg data-id="${id}">ok</svg>` };
      }),
    },
  };
});

import { renderMermaid } from "./mermaidRunner";

function mermaidBlock(source: string): string {
  return `<pre><code class="language-mermaid">${source}</code></pre>`;
}

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

  it("does nothing when there are no diagrams", async () => {
    const root = document.createElement("div");
    root.innerHTML = "<p>no diagrams here</p>";
    await expect(renderMermaid(root, false)).resolves.toBeUndefined();
    expect(root.querySelector(".mermaid-error")).toBeNull();
  });
});
