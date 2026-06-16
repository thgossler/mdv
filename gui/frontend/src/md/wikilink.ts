import type MarkdownIt from "markdown-it";

// wikilinkPlugin parses `[[target]]`, `[[target|alias]]` and `[[target#heading]]`
// into anchor tokens. Actual resolution (and broken-link styling) happens later
// in the renderer where the Go bridge classifies the destination, so here we
// only emit a link with a data attribute carrying the raw target.
export function wikilinkPlugin(md: MarkdownIt): void {
  md.inline.ruler.before("link", "wikilink", (state, silent) => {
    const start = state.pos;
    const src = state.src;
    if (src.charCodeAt(start) !== 0x5b /* [ */ || src.charCodeAt(start + 1) !== 0x5b) {
      return false;
    }
    const end = src.indexOf("]]", start + 2);
    if (end < 0) return false;

    const inner = src.slice(start + 2, end).trim();
    if (!inner) return false;

    if (!silent) {
      let target = inner;
      let alias = "";
      const pipe = inner.indexOf("|");
      if (pipe >= 0) {
        target = inner.slice(0, pipe).trim();
        alias = inner.slice(pipe + 1).trim();
      }
      const label = alias || target;

      const open = state.push("link_open", "a", 1);
      open.attrSet("href", "");
      open.attrSet("data-wikilink", target);
      open.attrSet("class", "wikilink");

      const text = state.push("text", "", 0);
      text.content = label;

      state.push("link_close", "a", -1);
    }

    state.pos = end + 2;
    return true;
  });
}
