import type MarkdownIt from "markdown-it";

// adoPlugin adds a pragmatic subset of Azure DevOps wiki markdown:
//   - [[_TOC_]]               → an inline table-of-contents placeholder
//   - :::video <url> :::      → an embedded player for YouTube/direct video
//   - #123                    → a work-item reference (rendered as a chip)
// These are best-effort and documented as such; they cover the common cases.
export function adoPlugin(md: MarkdownIt): void {
  // [[_TOC_]] on its own line → placeholder the frontend fills after render.
  md.core.ruler.before("inline", "ado_toc", (state) => {
    for (const tok of state.tokens) {
      if (tok.type !== "inline") continue;
      if (/^\s*\[\[_TOC_\]\]\s*$/i.test(tok.content)) {
        tok.content = "";
        if (tok.children) tok.children = [];
        const parent = findParent(state.tokens, tok);
        if (parent) parent.attrSet("data-ado-toc", "1");
      }
    }
    return true;
  });

  // ::: video blocks.
  md.block.ruler.before("fence", "ado_video", (state, startLine, endLine, silent) => {
    const start = state.bMarks[startLine] + state.tShift[startLine];
    const max = state.eMarks[startLine];
    const line = state.src.slice(start, max).trim();
    const m = line.match(/^:::\s*video\s*(\S+)?\s*$/i);
    if (!m) return false;
    if (silent) return true;

    let url = m[1] || "";
    let nextLine = startLine + 1;
    // Allow the URL on the following line, terminated by `:::`.
    while (nextLine < endLine) {
      const ls = state.bMarks[nextLine] + state.tShift[nextLine];
      const le = state.eMarks[nextLine];
      const content = state.src.slice(ls, le).trim();
      if (content === ":::") break;
      if (content && !url) url = content;
      nextLine++;
    }

    const token = state.push("ado_video", "", 0);
    token.meta = { url };
    token.map = [startLine, nextLine + 1];
    state.line = nextLine + 1;
    return true;
  });

  md.renderer.rules.ado_video = (tokens, idx) => {
    const url = (tokens[idx].meta && tokens[idx].meta.url) || "";
    return renderVideo(url);
  };

  // #123 work-item references (avoid matching inside words / code handled by MD).
  md.core.ruler.push("ado_workitem", (state) => {
    for (const tok of state.tokens) {
      if (tok.type !== "inline" || !tok.children) continue;
      for (let i = 0; i < tok.children.length; i++) {
        const child = tok.children[i];
        if (child.type !== "text") continue;
        if (!/(^|\s)#\d+\b/.test(child.content)) continue;
        // Wrap into HTML inline tokens.
        const html = child.content.replace(
          /(^|\s)#(\d+)\b/g,
          (_all, pre, num) => `${pre}<span class="ado-workitem">#${num}</span>`
        );
        const repl = new state.Token("html_inline", "", 0);
        repl.content = html;
        tok.children[i] = repl;
      }
    }
    return true;
  });
}

function renderVideo(url: string): string {
  const yt = url.match(/(?:youtube\.com\/watch\?v=|youtu\.be\/|youtube\.com\/embed\/)([\w-]{6,})/);
  if (yt) {
    const id = yt[1];
    return `<div class="video-embed"><iframe src="https://www.youtube.com/embed/${id}" frameborder="0" allowfullscreen loading="lazy"></iframe></div>`;
  }
  if (/\.(mp4|webm|ogg)(\?.*)?$/i.test(url)) {
    return `<div class="video-embed"><video controls preload="metadata" src="${escapeAttr(url)}"></video></div>`;
  }
  return `<div class="video-embed"><a href="${escapeAttr(url)}" data-external="1">▶ ${escapeAttr(url)}</a></div>`;
}

function escapeAttr(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");
}

function findParent(tokens: { children?: unknown }[], inline: unknown) {
  // The paragraph_open immediately preceding the inline token.
  const idx = tokens.indexOf(inline as never);
  for (let i = idx - 1; i >= 0; i--) {
    const t = tokens[i] as { type?: string };
    if (t.type === "paragraph_open") return tokens[i] as { attrSet: (k: string, v: string) => void };
  }
  return null;
}
