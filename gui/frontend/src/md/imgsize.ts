import type MarkdownIt from "markdown-it";

// imgsizePlugin adds Azure DevOps-style explicit image sizing:
//
//   ![alt](url =800x600)   — explicit width and height
//   ![alt](url =800x)      — explicit width only
//   ![alt](url =x600)      — explicit height only
//
// The spec is stripped from the src and converted into HTML width/height
// attributes so the browser renders the image at the intended size at 100% zoom.
// The values are also stored as data-ado-width / data-ado-height so the zoom
// module can scale relative to the author-defined 100% size rather than the
// image's raw pixel dimensions.
//
// Reference: https://learn.microsoft.com/azure/devops/project/wiki/markdown-guidance#image-size
export function imgsizePlugin(md: MarkdownIt): void {
  // Pattern: optional space, then =<width>x<height> at end of src.
  // Both width and height are optional digits; at least one must be present.
  const reSizeSpec = / =(\d*)x(\d*)$/;

  // Capture whatever renderer was registered before us so we can delegate to it.
  const defaultRender =
    md.renderer.rules.image ||
    ((tokens, idx, options, _env, self) => self.renderToken(tokens, idx, options));

  md.renderer.rules.image = (tokens, idx, options, env, self) => {
    const token = tokens[idx];
    let src = token.attrGet("src") ?? "";

    const m = reSizeSpec.exec(src);
    if (m) {
      // Strip the size spec so the URL stays clean.
      token.attrSet("src", src.slice(0, m.index));

      const w = m[1]; // may be empty string when only height is given
      const h = m[2]; // may be empty string when only width is given

      if (w) {
        token.attrSet("width", w);
        // data- attribute consumed by zoom.ts to use as the 100% baseline width.
        token.attrSet("data-ado-width", w);
      }
      if (h) {
        token.attrSet("height", h);
        token.attrSet("data-ado-height", h);
      }
    }

    return defaultRender(tokens, idx, options, env, self);
  };
}
