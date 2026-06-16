import type MarkdownIt from "markdown-it";
import type Token from "markdown-it/lib/token.mjs";

// Supported GitHub/Azure DevOps alert types and their display labels.
const ALERTS: Record<string, string> = {
  NOTE: "Note",
  TIP: "Tip",
  IMPORTANT: "Important",
  WARNING: "Warning",
  CAUTION: "Caution",
};

const RE = /^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*/i;

// alertsPlugin converts blockquotes whose first line is `[!TYPE]` into styled
// GitHub-flavoured alert callouts.
export function alertsPlugin(md: MarkdownIt): void {
  md.core.ruler.after("block", "github_alerts", (state) => {
    const tokens = state.tokens;
    for (let i = 0; i < tokens.length; i++) {
      if (tokens[i].type !== "blockquote_open") continue;

      // Find the first inline token inside the blockquote.
      let j = i + 1;
      while (j < tokens.length && tokens[j].type !== "inline") j++;
      if (j >= tokens.length) continue;

      const inline = tokens[j];
      const m = inline.content.match(RE);
      if (!m) continue;

      const type = m[1].toUpperCase();
      const label = ALERTS[type];

      // Strip the marker from the content and its first child token.
      inline.content = inline.content.replace(RE, "");
      if (inline.children && inline.children.length) {
        const first = inline.children[0];
        if (first.type === "text") {
          first.content = first.content.replace(RE, "");
        }
        // Drop a leading softbreak left behind by the removed marker line.
        if (inline.children[0] && inline.children[0].type === "softbreak") {
          inline.children.shift();
        }
      }

      // Tag the blockquote open/close so the renderer emits alert markup.
      const open = tokens[i];
      open.attrSet("class", `markdown-alert markdown-alert-${type.toLowerCase()}`);
      open.meta = { ...(open.meta || {}), alertLabel: label, alertType: type };
      open.type = "alert_open";
      open.tag = "div";

      // Find the matching blockquote_close (respecting nesting).
      let depth = 1;
      for (let k = i + 1; k < tokens.length; k++) {
        if (tokens[k].type === "blockquote_open") depth++;
        else if (tokens[k].type === "blockquote_close") {
          depth--;
          if (depth === 0) {
            tokens[k].type = "alert_close";
            tokens[k].tag = "div";
            break;
          }
        }
      }
    }
    return true;
  });

  const defaultRender =
    md.renderer.rules.blockquote_open ||
    ((tokens, idx, options, _env, self) => self.renderToken(tokens, idx, options));

  md.renderer.rules.alert_open = (tokens, idx, options, env, self) => {
    const tok = tokens[idx] as Token;
    const label = (tok.meta && tok.meta.alertLabel) || "Note";
    const type = ((tok.meta && tok.meta.alertType) || "NOTE").toLowerCase();
    const rendered = self.renderToken(tokens, idx, options);
    const title =
      `<p class="markdown-alert-title">` +
      `<span class="markdown-alert-icon icon-${type}"></span>${label}</p>`;
    return rendered + title;
  };

  md.renderer.rules.alert_close = (tokens, idx, options, _env, self) =>
    self.renderToken(tokens, idx, options);

  // Keep a reference so unused-var linters stay quiet.
  void defaultRender;
}
