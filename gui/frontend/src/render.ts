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
import { imgsizePlugin } from "./md/imgsize";
import { sourceLinesPlugin } from "./md/sourcelines";

// RenderResult is the output of rendering a markdown document.
export interface RenderResult {
  html: string;
  headings: HeadingInfo[];
  frontmatter: Record<string, unknown> | null;
}

// Instances are cached per "extended" mode so toggling the extended-syntax
// option at runtime is cheap (no re-parsing of the plugin chain on every
// render).
const instances = new Map<boolean, MarkdownIt>();

function getInstance(extended: boolean): MarkdownIt {
  let instance = instances.get(extended);
  if (!instance) {
    instance = build(extended);
    instances.set(extended, instance);
  }
  return instance;
}

// build assembles a markdown-it instance. The "safe" extensions are always on;
// the "extended" character-stealing inline extensions (math, subscript,
// superscript, highlight, inserted) are only enabled when `extended` is true,
// because they can silently transform ordinary prose (e.g. "$5 to $10").
function build(extended: boolean): MarkdownIt {
  const instance = new MarkdownIt({
    html: true,
    linkify: true,
    typographer: true,
    breaks: false,
    highlight: highlightCode,
  });

  // Always-on extensions: distinctive delimiters, no false positives on plain
  // CommonMark/GitHub/GitLab prose.
  instance
    .use(footnote)
    .use(taskLists, { enabled: true, label: true })
    .use(deflist)
    .use(emoji)
    .use(alertsPlugin)
    .use(wikilinkPlugin)
    .use(csvPlugin)
    .use(adoPlugin)
    .use(imgsizePlugin);

  // Opt-in "extended" inline syntax.
  if (extended) {
    instance
      .use(sub)
      .use(sup)
      .use(mark)
      .use(ins)
      .use(mathPlugin);
  }

  // Infrastructure plugins (heading slugs/TOC + source-line mapping) must run
  // last and are always present.
  instance.use(anchorsPlugin).use(externalLinkPlugin).use(sourceLinesPlugin);

  return instance;
}

// externalLinkPlugin tags links that point at an external destination (an
// absolute http(s)/mailto/protocol-relative URL) with a marker class so the
// stylesheet can render a small "external" icon, making it obvious at a glance
// which links leave the local workspace.
function externalLinkPlugin(md: MarkdownIt): void {
  const defaultRender =
    md.renderer.rules.link_open ||
    ((tokens, idx, options, _env, self) => self.renderToken(tokens, idx, options));
  md.renderer.rules.link_open = (tokens, idx, options, env, self) => {
    const token = tokens[idx];
    const href = token.attrGet("href") || "";
    if (isExternalHref(href)) {
      const existing = token.attrGet("class");
      token.attrSet("class", existing ? `${existing} external-link` : "external-link");
    }
    return defaultRender(tokens, idx, options, env, self);
  };
}

// isExternalHref reports whether a link target leaves the local workspace: an
// absolute URL with a scheme (http:, https:, mailto:, ftp:, …) or a
// protocol-relative URL (//host/…). Relative paths and in-document fragments
// (#anchor) are local.
function isExternalHref(href: string): boolean {
  return /^[a-z][a-z0-9+.-]*:/i.test(href) || href.startsWith("//");
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
  "video", "source", "input", "abbr", "figure", "figcaption",
];
const ALLOWED_ATTR_EXTRA = [
  "class", "id", "data-slug", "data-lang", "data-wikilink", "data-external",
  "data-ado-toc", "data-source-line", "align", "controls", "preload",
  "allowfullscreen", "loading", "type", "checked", "disabled", "colspan", "rowspan",
];
// Tags that must never survive sanitisation, even if some plugin or future
// allow-list change would otherwise admit them. <iframe>/<object>/<embed> can
// load and execute arbitrary remote content; <script>/<style> can run code or
// exfiltrate via CSS; <form>/<base> can redirect actions or rewrite relative
// URLs. This is the core "no JavaScript / no unsafe HTML" guarantee for
// documents that may come from an untrusted source.
const FORBID_TAGS = ["script", "style", "iframe", "object", "embed", "base", "form"];
// srcdoc would let an allowed element smuggle an inline document; on* handlers
// are stripped by DOMPurify already but listed here as belt-and-braces.
const FORBID_ATTR = ["srcdoc"];

// render parses markdown and returns sanitised HTML plus extracted metadata.
// When `extended` is true the opt-in inline extensions (math, sub/sup, mark,
// ins) are enabled.
export function render(markdown: string, extended = false): RenderResult {
  const md = getInstance(extended);
  const env: { headings?: HeadingInfo[] } = {};
  const rawHtml = md.render(markdown, env);

  const clean = DOMPurify.sanitize(rawHtml, {
    ADD_TAGS: ALLOWED_TAGS_EXTRA,
    ADD_ATTR: ALLOWED_ATTR_EXTRA,
    FORBID_TAGS,
    FORBID_ATTR,
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
