import type MarkdownIt from "markdown-it";
import type StateInline from "markdown-it/lib/rules_inline/state_inline.mjs";
import type StateBlock from "markdown-it/lib/rules_block/state_block.mjs";
import katex from "katex";

// mathPlugin adds inline `$...$` and block `$$...$$` math, rendered with KaTeX.
// Rendering happens at parse time so the output HTML is self-contained.
export function mathPlugin(md: MarkdownIt): void {
  md.inline.ruler.after("escape", "math_inline", inlineMath);
  md.block.ruler.after("blockquote", "math_block", blockMath, {
    alt: ["paragraph", "reference", "blockquote", "list"],
  });

  md.renderer.rules.math_inline = (tokens, idx) => renderKatex(tokens[idx].content, false);
  md.renderer.rules.math_block = (tokens, idx) => renderKatex(tokens[idx].content, true) + "\n";
}

function renderKatex(tex: string, displayMode: boolean): string {
  try {
    return katex.renderToString(tex, {
      displayMode,
      throwOnError: false,
      output: "htmlAndMathml",
    });
  } catch (e) {
    return `<code class="math-error">${escapeHtml(tex)}</code>`;
  }
}

function inlineMath(state: StateInline, silent: boolean): boolean {
  const start = state.pos;
  if (state.src.charCodeAt(start) !== 0x24 /* $ */) return false;
  // Not an opener if it is escaped or doubled (handled by block rule).
  let pos = start + 1;
  if (state.src.charCodeAt(pos) === 0x24) return false;

  // Find the closing unescaped `$` that is not followed by a digit (price-safe).
  let found = -1;
  while (pos < state.src.length) {
    const code = state.src.charCodeAt(pos);
    if (code === 0x5c /* \ */) {
      pos += 2;
      continue;
    }
    if (code === 0x24) {
      found = pos;
      break;
    }
    pos++;
  }
  if (found < 0) return false;

  const content = state.src.slice(start + 1, found);
  if (content.trim() === "") return false;

  if (!silent) {
    const token = state.push("math_inline", "math", 0);
    token.content = content;
    token.markup = "$";
  }
  state.pos = found + 1;
  return true;
}

function blockMath(state: StateBlock, startLine: number, endLine: number, silent: boolean): boolean {
  const start = state.bMarks[startLine] + state.tShift[startLine];
  const max = state.eMarks[startLine];
  if (start + 2 > max) return false;
  if (state.src.slice(start, start + 2) !== "$$") return false;
  if (silent) return true;

  const firstLine = state.src.slice(start + 2, max);
  let content = "";
  let nextLine = startLine;
  let found = false;

  // Single-line $$ ... $$.
  if (firstLine.trim().endsWith("$$")) {
    content = firstLine.trim().replace(/\$\$$/, "");
    found = true;
  } else {
    if (firstLine.trim()) content += firstLine + "\n";
    nextLine = startLine + 1;
    for (; nextLine < endLine; nextLine++) {
      const ls = state.bMarks[nextLine] + state.tShift[nextLine];
      const le = state.eMarks[nextLine];
      const line = state.src.slice(ls, le);
      if (line.trim().endsWith("$$")) {
        content += line.replace(/\$\$\s*$/, "");
        found = true;
        break;
      }
      content += line + "\n";
    }
  }
  if (!found) return false;

  const token = state.push("math_block", "math", 0);
  token.block = true;
  token.content = content.trim();
  token.markup = "$$";
  token.map = [startLine, nextLine + 1];
  state.line = nextLine + 1;
  return true;
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}
