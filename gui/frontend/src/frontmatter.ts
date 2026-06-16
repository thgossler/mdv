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

  if (!title && !author && !date && tags.length === 0) return "";

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
  html += "</div>";
  return html;
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
