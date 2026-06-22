import type MarkdownIt from "markdown-it";
import type StateInline from "markdown-it/lib/rules_inline/state_inline.mjs";

// imgsizePlugin adds Azure DevOps-style explicit image sizing:
//
//   ![alt](url =800x600)   - explicit width and height
//   ![alt](url =800x)      - explicit width only
//   ![alt](url =x600)      - explicit height only
//
// CommonMark does not allow arbitrary text after the link destination, so the
// default markdown-it image rule rejects `![alt](url =480x)` outright and the
// whole construct is emitted as literal text. We therefore handle the size spec
// during inline parsing: a rule registered before the built-in `image` rule
// recognises the optional ` =WxH` spec, strips it, and emits a normal image
// token carrying width/height attributes. When no size spec is present the rule
// returns false so the built-in image/reference handling runs unchanged.
//
// The width/height are also stored as data-ado-width / data-ado-height so the
// zoom module can scale relative to the author-defined 100% size rather than
// the image's raw pixel dimensions.
//
// Reference: https://learn.microsoft.com/azure/devops/project/wiki/markdown-guidance#image-size
export function imgsizePlugin(md: MarkdownIt): void {
  md.inline.ruler.before("image", "image_size", imageSize);
}

function isSpaceOrNewline(code: number): boolean {
  return code === 0x20 /* space */ || code === 0x09 /* tab */ || code === 0x0a /* \n */;
}

function imageSize(state: StateInline, silent: boolean): boolean {
  const oldPos = state.pos;
  const max = state.posMax;
  const src = state.src;

  if (src.charCodeAt(state.pos) !== 0x21 /* ! */) return false;
  if (src.charCodeAt(state.pos + 1) !== 0x5b /* [ */) return false;

  const labelStart = state.pos + 2;
  const labelEnd = state.md.helpers.parseLinkLabel(state, state.pos + 1, false);

  // No closing ']' -> not a valid image.
  if (labelEnd < 0) return false;

  let pos = labelEnd + 1;
  // Only inline links `(...)` can carry a size spec; reference links cannot.
  if (pos >= max || src.charCodeAt(pos) !== 0x28 /* ( */) return false;

  // Skip the spaces after '('.
  pos++;
  for (; pos < max && isSpaceOrNewline(src.charCodeAt(pos)); pos++);
  if (pos >= max) return false;

  // Parse the link destination.
  let href = "";
  let res = state.md.helpers.parseLinkDestination(src, pos, state.posMax);
  if (res.ok) {
    const normalized = state.md.normalizeLink(res.str);
    if (state.md.validateLink(normalized)) {
      href = normalized;
      pos = res.pos;
    }
  }

  // Skip spaces after the destination.
  const afterHref = pos;
  for (; pos < max && isSpaceOrNewline(src.charCodeAt(pos)); pos++);

  // Parse an optional title.
  let title = "";
  res = state.md.helpers.parseLinkTitle(src, pos, state.posMax);
  if (pos < max && afterHref !== pos && res.ok) {
    title = res.str;
    pos = res.pos;
    for (; pos < max && isSpaceOrNewline(src.charCodeAt(pos)); pos++);
  }

  // Require the size spec `=<width>x<height>`. If it is absent, defer to the
  // built-in image rule so ordinary images are unaffected.
  const m = /^=(\d*)x(\d*)/.exec(src.slice(pos));
  if (!m || (!m[1] && !m[2])) {
    state.pos = oldPos;
    return false;
  }
  pos += m[0].length;

  // Skip trailing spaces, then require the closing ')'.
  for (; pos < max && isSpaceOrNewline(src.charCodeAt(pos)); pos++);
  if (pos >= max || src.charCodeAt(pos) !== 0x29 /* ) */) {
    state.pos = oldPos;
    return false;
  }
  pos++;

  if (!silent) {
    const content = src.slice(labelStart, labelEnd);
    const tokens: ReturnType<typeof state.push>[] = [];
    state.md.inline.parse(content, state.md, state.env, tokens);

    const token = state.push("image", "img", 0);
    token.children = tokens;
    token.content = content;
    token.attrSet("src", href);
    token.attrSet("alt", "");
    if (title) token.attrSet("title", title);

    const w = m[1]; // empty string when only height is given
    const h = m[2]; // empty string when only width is given
    if (w) {
      token.attrSet("width", w);
      // data- attribute consumed by zoom.ts as the 100% baseline width.
      token.attrSet("data-ado-width", w);
    }
    if (h) {
      token.attrSet("height", h);
      token.attrSet("data-ado-height", h);
    }
  }

  state.pos = pos;
  state.posMax = max;
  return true;
}
