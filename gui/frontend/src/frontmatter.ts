import yaml from "js-yaml";

// Frontmatter holds parsed YAML metadata and the markdown body without it.
export interface Frontmatter {
  data: Record<string, unknown> | null;
  body: string;
}

const FM_RE = /^\uFEFF?---\r?\n([\s\S]*?)\r?\n---\s*(?:\r?\n|$)/;

// extractFrontmatter splits a leading YAML `--- ... ---` block from the body.
export function extractFrontmatter(markdown: string): Frontmatter {
  const m = markdown.match(FM_RE);
  if (!m) return { data: null, body: markdown };
  try {
    const data = yaml.load(m[1]) as Record<string, unknown>;
    return { data: data && typeof data === "object" ? data : null, body: markdown.slice(m[0].length) };
  } catch {
    return { data: null, body: markdown };
  }
}

// A stable palette so each tag keeps a consistent colour within a session.
const TAG_COLORS = [
  "#3b82f6", "#10b981", "#f59e0b", "#ef4444", "#8b5cf6",
  "#ec4899", "#14b8a6", "#f97316", "#6366f1", "#84cc16",
];

function tagColor(tag: string): string {
  let h = 0;
  for (let i = 0; i < tag.length; i++) h = (h * 31 + tag.charCodeAt(i)) >>> 0;
  return TAG_COLORS[h % TAG_COLORS.length];
}

// renderFrontmatter builds an HTML metadata block from parsed frontmatter.
export function renderFrontmatter(data: Record<string, unknown> | null): string {
  if (!data) return "";
  const title = pick(data, "title");
  const author = pick(data, "author", "authors");
  const date = pick(data, "date", "updated");
  const tags = normalizeTags(data["tags"] ?? data["keywords"]);
  const extra = extraFields(data);

  if (!title && !author && !date && tags.length === 0 && extra.length === 0) return "";

  let html = '<div class="frontmatter">';
  if (title) html += `<div class="fm-title">${escapeHtml(title)}</div>`;
  const meta: string[] = [];
  if (author) meta.push(`<span class="fm-author">${escapeHtml(author)}</span>`);
  if (date) meta.push(`<span class="fm-date">${escapeHtml(date)}</span>`);
  if (meta.length) html += `<div class="fm-meta">${meta.join('<span class="fm-sep">·</span>')}</div>`;
  if (tags.length) {
    html += '<div class="fm-tags">';
    for (const t of tags) {
      const c = tagColor(t);
      html += `<span class="fm-tag" style="--tag-color:${c}">${escapeHtml(t)}</span>`;
    }
    html += "</div>";
  }
  if (extra.length) {
    const count = extra.length;
    html += '<details class="fm-details">';
    html += `<summary class="fm-summary">Metadata <span class="fm-count">${count}</span></summary>`;
    html += '<dl class="fm-kv">';
    for (const [key, value] of extra) {
      html += `<dt class="fm-key">${escapeHtml(key)}</dt>`;
      html += `<dd class="fm-value">${escapeHtml(value)}</dd>`;
    }
    html += "</dl></details>";
  }
  html += "</div>";
  return html;
}

// RECOGNIZED keys are surfaced as the prominent headline (title/author/date/
// tags) and so are omitted from the collapsible field list to avoid repetition.
const RECOGNIZED = new Set([
  "title", "author", "authors", "date", "updated", "tags", "keywords",
]);

// extraFields returns the remaining top-level entries in document order, each
// formatted as a [key, displayValue] pair. Object key insertion order is
// preserved by JavaScript for string keys, matching the source YAML order.
function extraFields(data: Record<string, unknown>): [string, string][] {
  const out: [string, string][] = [];
  for (const [key, value] of Object.entries(data)) {
    if (RECOGNIZED.has(key.toLowerCase().trim())) continue;
    const formatted = formatValue(value);
    if (formatted === "") continue;
    out.push([key, formatted]);
  }
  return out;
}

// formatValue renders an arbitrary YAML value to a compact single-line string:
// scalars verbatim, arrays as "a, b, c", and objects as "k: v, k2: v2".
function formatValue(v: unknown): string {
  if (v == null) return "";
  if (v instanceof Date) return v.toISOString().slice(0, 10);
  if (Array.isArray(v)) return v.map(formatValue).filter((s) => s !== "").join(", ");
  if (typeof v === "object") {
    return Object.entries(v as Record<string, unknown>)
      .map(([k, val]) => `${k}: ${formatValue(val)}`)
      .join(", ");
  }
  return String(v).trim();
}

function pick(data: Record<string, unknown>, ...keys: string[]): string {
  for (const k of keys) {
    const v = data[k];
    if (typeof v === "string" && v.trim()) return v.trim();
    if (typeof v === "number") return String(v);
    if (v instanceof Date) return v.toISOString().slice(0, 10);
  }
  return "";
}

function normalizeTags(v: unknown): string[] {
  if (Array.isArray(v)) return v.map((x) => String(x)).filter(Boolean);
  if (typeof v === "string") return v.split(/[,\s]+/).filter(Boolean);
  return [];
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}
