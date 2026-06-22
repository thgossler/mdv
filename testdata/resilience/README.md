# Rendering resilience fixtures

Sample documents for manually verifying that mdv renders difficult content
without crashing, hanging, or blanking the rest of the document. Open each with
the GUI, the TUI, and piped console output:

```sh
mdv testdata/resilience/<file>.md          # GUI
mdv --tui testdata/resilience/<file>.md     # TUI
mdv testdata/resilience/<file>.md | cat     # console
```

| Fixture | What to check |
| --- | --- |
| `malformed-diagram.md` | Broken Mermaid shows an error placeholder (GUI); the valid diagram and all surrounding text still render. |
| `invalid-and-unsupported.md` | Unbalanced markup, unsafe HTML, unknown directives and an unterminated fence all degrade gracefully; the final "Still here" paragraph renders; `<script>` is stripped. |
| `deeply-nested-lists.md` | Deep list/blockquote nesting does not hang; the trailing paragraph renders. |
| `massive-table.md` | A 5000-row table renders (or degrades) without hanging; the trailing "After" paragraph renders. |
| `unicode-rtl.md` | RTL (Arabic/Hebrew), CJK, emoji and combining characters render; RTL paragraphs flow right-to-left in the GUI. |
| `crlf-bom.md` | UTF-8 BOM + CRLF: no stray BOM glyph, no visible carriage returns. |
| `utf16.md` | UTF-16 (with BOM) is transcoded to UTF-8 and renders normally. |

The automated counterparts live in `internal/mdfmt/resilience_test.go`,
`internal/console/resilience_test.go`, `internal/core/encoding_test.go`, and
`gui/frontend/src/render.test.ts` / `mermaidRunner.test.ts`.
