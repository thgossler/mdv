import type MarkdownIt from "markdown-it";

// sourceLinesPlugin annotates block-level elements with a `data-source-line`
// attribute carrying the 1-based line number of the element in the raw markdown
// source. Content search uses these markers to jump to the document row of a
// match after the markdown has been rendered to HTML (the rendered DOM has no
// inherent line information otherwise).
export function sourceLinesPlugin(md: MarkdownIt): void {
  md.core.ruler.push("mdv_source_lines", (state) => {
    for (const token of state.tokens) {
      // Only opening tags (nesting 1) and self-contained block tokens
      // (nesting 0, e.g. fences, hr) carry a usable source map and render to an
      // element we can attach the attribute to.
      if (token.nesting < 0) continue;
      if (!token.map) continue;
      if (token.hidden) continue;
      // token.map is [startLine, endLine) with 0-based line numbers.
      token.attrSet("data-source-line", String(token.map[0] + 1));
    }
    return true;
  });
}
