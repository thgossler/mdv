import type MarkdownIt from "markdown-it";

// csvPlugin renders ```csv and ```tsv fenced blocks as styled HTML tables.
// The first row is treated as a header. Quoted fields (CSV) are supported.
export function csvPlugin(md: MarkdownIt): void {
  const defaultFence =
    md.renderer.rules.fence ||
    ((tokens, idx, options, _env, self) => self.renderToken(tokens, idx, options));

  md.renderer.rules.fence = (tokens, idx, options, env, self) => {
    const token = tokens[idx];
    const info = (token.info || "").trim().toLowerCase();
    if (info === "csv" || info === "tsv") {
      const delim = info === "tsv" ? "\t" : ",";
      return renderTable(token.content, delim);
    }
    return defaultFence(tokens, idx, options, env, self);
  };
}

function renderTable(content: string, delim: string): string {
  const rows = content.replace(/\r\n?/g, "\n").replace(/\n+$/, "").split("\n");
  if (rows.length === 0) return "";

  const parse = delim === "," ? parseCsvRow : (r: string) => r.split("\t");

  let html = '<div class="csv-table-wrap"><table class="csv-table">';
  rows.forEach((row, i) => {
    const cells = parse(row);
    html += "<tr>";
    for (const cell of cells) {
      const tag = i === 0 ? "th" : "td";
      html += `<${tag}>${escapeHtml(cell)}</${tag}>`;
    }
    html += "</tr>";
    if (i === 0) html += "";
  });
  html += "</table></div>";
  return html;
}

// parseCsvRow handles quoted fields with embedded commas and escaped quotes.
function parseCsvRow(row: string): string[] {
  const out: string[] = [];
  let cur = "";
  let inQuotes = false;
  for (let i = 0; i < row.length; i++) {
    const c = row[i];
    if (inQuotes) {
      if (c === '"') {
        if (row[i + 1] === '"') {
          cur += '"';
          i++;
        } else {
          inQuotes = false;
        }
      } else {
        cur += c;
      }
    } else if (c === '"') {
      inQuotes = true;
    } else if (c === ",") {
      out.push(cur);
      cur = "";
    } else {
      cur += c;
    }
  }
  out.push(cur);
  return out.map((s) => s.trim());
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}
