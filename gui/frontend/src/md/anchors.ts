import type MarkdownIt from "markdown-it";
import GithubSlugger from "github-slugger";

// HeadingInfo describes a heading for the table of contents.
export interface HeadingInfo {
  level: number;
  text: string;
  slug: string;
}

// anchorsPlugin assigns GitHub-compatible slugs to every heading and records
// them on env.headings so the TOC can be built. It also injects a hover anchor
// link for deep-linking.
export function anchorsPlugin(md: MarkdownIt): void {
  md.core.ruler.push("mdv_anchors", (state) => {
    const slugger = new GithubSlugger();
    const env = state.env as { headings?: HeadingInfo[] };
    env.headings = [];
    const tokens = state.tokens;

    for (let i = 0; i < tokens.length; i++) {
      if (tokens[i].type !== "heading_open") continue;
      const inline = tokens[i + 1];
      const text = inline && inline.type === "inline" ? collectText(inline) : "";
      const slug = slugger.slug(text);
      tokens[i].attrSet("id", slug);
      tokens[i].attrSet("data-slug", slug);

      const level = Number(tokens[i].tag.slice(1));
      env.headings!.push({ level, text, slug });
    }
    return true;
  });
}

// collectText extracts the plain text content of an inline token.
function collectText(inline: { children?: { type: string; content: string }[] | null }): string {
  if (!inline.children) return "";
  let out = "";
  for (const child of inline.children) {
    if (child.type === "text" || child.type === "code_inline") out += child.content;
    else if (child.type === "softbreak" || child.type === "hardbreak") out += " ";
  }
  return out.trim();
}
