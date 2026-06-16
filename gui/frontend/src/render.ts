import MarkdownIt from "markdown-it";
import footnote from "markdown-it-footnote";
import taskLists from "markdown-it-task-lists";
import deflist from "markdown-it-deflist";
import sub from "markdown-it-sub";
import sup from "markdown-it-sup";
import mark from "markdown-it-mark";
import ins from "markdown-it-ins";
import { full as emoji } from "markdown-it-emoji";
import hljs from "highlight.js";
import DOMPurify from "dompurify";

import { alertsPlugin } from "./md/alerts";
import { wikilinkPlugin } from "./md/wikilink";
import { anchorsPlugin, type HeadingInfo } from "./md/anchors";
import { csvPlugin } from "./md/csv";
import { adoPlugin } from "./md/ado";
import { mathPlugin } from "./md/math";

// RenderResult is the output of rendering a markdown document.
export interface RenderResult {
  html: string;
  headings: HeadingInfo[];
  frontmatter: Record<string, unknown> | null;
}

let md: MarkdownIt | null = null;

function build(): MarkdownIt {
  const instance = new MarkdownIt({
    html: true,
    linkify: true,
    typographer: true,
    breaks: false,
    highlight: highlightCode,
  });

  instance
    .use(footnote)
    .use(taskLists, { enabled: true, label: true })
    .use(deflist)
    .use(sub)
    .use(sup)
    .use(mark)
    .use(ins)
    .use(emoji)
    .use(mathPlugin)
    .use(alertsPlugin)
    .use(wikilinkPlugin)
    .use(csvPlugin)
    .use(adoPlugin)
    .use(anchorsPlugin);

  return instance;
}

// highlightCode renders fenced code with highlight.js, leaving mermaid/csv/tsv
// blocks for their dedicated handlers.
function highlightCode(code: string, lang: string): string {
  const language = (lang || "").toLowerCase();
  if (language === "mermaid" || language === "csv" || language === "tsv") {
    return ""; // handled by custom fence renderers / post-processing
  }
  const inner =
    language && hljs.getLanguage(language)
      ? hljs.highlight(code, { language, ignoreIllegals: true }).value
      : escapeHtml(code);
  const label = language || "text";
  return (
    `<div class="code-block" data-lang="${escapeAttr(label)}">` +
    `<button class="code-copy" title="Copy">Copy</button>` +
    `<pre class="hljs"><code>${inner}</code></pre></div>`
  );
}

// Configure DOMPurify to keep the attributes our features rely on.
function configurePurify(): void {
  DOMPurify.addHook("afterSanitizeAttributes", (node) => {
    // Keep target handling internal; we intercept clicks ourselves.
    if (node.tagName === "A" && node.getAttribute("target")) {
      node.removeAttribute("target");
    }
  });
}
configurePurify();

const ALLOWED_TAGS_EXTRA = [
  "kbd", "sub", "sup", "mark", "ins", "del", "details", "summary",
  "video", "source", "iframe", "input", "abbr", "figure", "figcaption",
];
const ALLOWED_ATTR_EXTRA = [
  "class", "id", "data-slug", "data-lang", "data-wikilink", "data-external",
  "data-ado-toc", "align", "controls", "preload", "frameborder",
  "allowfullscreen", "loading", "type", "checked", "disabled", "colspan", "rowspan",
];

// render parses markdown and returns sanitised HTML plus extracted metadata.
export function render(markdown: string): RenderResult {
  if (!md) md = build();
  const env: { headings?: HeadingInfo[] } = {};
  const rawHtml = md.render(markdown, env);

  const clean = DOMPurify.sanitize(rawHtml, {
    ADD_TAGS: ALLOWED_TAGS_EXTRA,
    ADD_ATTR: ALLOWED_ATTR_EXTRA,
    ALLOW_DATA_ATTR: true,
    ADD_URI_SAFE_ATTR: ["data-wikilink"],
  });

  return {
    html: clean,
    headings: env.headings || [],
    frontmatter: null,
  };
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

function escapeAttr(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/"/g, "&quot;");
}
