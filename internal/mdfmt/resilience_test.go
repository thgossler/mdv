package mdfmt

import (
	"strings"
	"testing"
)

// render is a small helper using deterministic, color-free settings so the
// resilience tests assert on stable output. The "notty" style emits no ANSI.
func render(t *testing.T, md string) (string, error) {
	t.Helper()
	return Render(md, 80, "notty", false, nil, "")
}

// TestRenderResilience feeds a range of pathological documents through Render
// and asserts that each one returns without panicking and without an error, so
// a single malformed or unusual construct never aborts the whole render. The
// core guarantee under test: difficult input degrades gracefully instead of
// crashing the process.
func TestRenderResilience(t *testing.T) {
	tests := []struct {
		name string
		in   string
		// want is an optional substring expected in the visible output.
		want string
	}{
		{
			name: "massive table",
			in:   massiveTable(1000),
			want: "Col A",
		},
		{
			name: "deeply nested list",
			in:   deeplyNestedList(250),
			want: "level",
		},
		{
			name: "deeply nested blockquotes",
			in:   strings.Repeat("> ", 250) + "deep quote\n",
			// At this depth glamour's quote prefixes wrap the text, so only
			// assert a single word survives; the key guarantee is no error.
			want: "deep",
		},
		{
			name: "unterminated code fence",
			in:   "```go\nfunc main() {\n\tprintln(\"hi\")\n",
			want: "func main",
		},
		{
			name: "unbalanced emphasis and brackets",
			in:   "This *is **broken _markup [with](unclosed and ` stray backtick\n",
			want: "broken",
		},
		{
			name: "unsupported mermaid diagram is shown as code",
			in:   "Before\n\n```mermaid\ngraph TD; A-->B;\n```\n\nAfter\n",
			want: "After",
		},
		{
			name: "invalid html soup",
			in:   "<div><span><p>unclosed <b>tags <img src= <<<>>> &notanentity;\n",
			want: "unclosed",
		},
		{
			name: "unsupported custom directive",
			in:   "Para\n\n:::weird-extension {opts}\nbody\n:::\n\nMore\n",
			want: "More",
		},
		{
			name: "raw control characters",
			in:   "text with NUL\x00 and bell\x07 and esc\x1b chars\n",
			want: "text with",
		},
		{
			name: "unicode and emoji",
			in:   "# 标题 Ünïcödé 🚀\n\nWide CJK 日本語 and emoji 😀👍 mix.\n",
			want: "Ünïcödé",
		},
		{
			name: "right-to-left text",
			in:   "# مرحبا بالعالم\n\nשלום עולם mixed with English.\n",
			want: "English",
		},
		{
			name: "crlf line endings",
			in:   "# Title\r\n\r\nLine one.\r\nLine two.\r\n",
			want: "Line one",
		},
		{
			name: "leading utf-8 bom",
			in:   "\uFEFF# Heading\n\nbody text\n",
			want: "body text",
		},
		{
			name: "only whitespace",
			in:   "   \n\t\n   \n",
		},
		{
			name: "empty document",
			in:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := render(t, tt.in)
			if err != nil {
				t.Fatalf("Render returned error for resilient input: %v", err)
			}
			if tt.want != "" && !strings.Contains(out, tt.want) {
				t.Errorf("output missing %q; got:\n%s", tt.want, out)
			}
		})
	}
}

// TestRenderResilienceWideWidths exercises edge-case wrap widths that have
// historically tripped up reflow/table code, ensuring none of them panic.
func TestRenderResilienceWidths(t *testing.T) {
	in := massiveTable(50) + "\n" + deeplyNestedList(50)
	for _, w := range []int{-5, 0, 1, 2, 3, 10, 1000} {
		if _, err := Render(in, w, "notty", false, nil, ""); err != nil {
			t.Errorf("width %d: unexpected error: %v", w, err)
		}
	}
}

// massiveTable builds a GitHub-flavoured markdown table with rows data rows.
func massiveTable(rows int) string {
	var b strings.Builder
	b.WriteString("| Col A | Col B | Col C |\n")
	b.WriteString("| --- | --- | --- |\n")
	for i := 0; i < rows; i++ {
		b.WriteString("| a")
		b.WriteString(itoaTest(i))
		b.WriteString(" | b value with some length | c |\n")
	}
	return b.String()
}

// deeplyNestedList builds a list nested depth levels deep.
func deeplyNestedList(depth int) string {
	var b strings.Builder
	for i := 0; i < depth; i++ {
		b.WriteString(strings.Repeat("  ", i))
		b.WriteString("- level ")
		b.WriteString(itoaTest(i))
		b.WriteString("\n")
	}
	return b.String()
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// FuzzRender throws arbitrary bytes at Render to flush out panics in the
// hand-written scanners, regex passes and the underlying glamour/goldmark
// pipeline. The recover guard inside Render must turn any panic into an error,
// so the only acceptable outcomes are (output, nil) or ("", error) — never a
// crash.
func FuzzRender(f *testing.F) {
	seeds := []string{
		"# hello\n",
		"| a | b |\n| - | - |\n| 1 | 2 |\n",
		strings.Repeat("> ", 100) + "x",
		"```\nunterminated",
		"[link](http://example.com) *em* `code`",
		"\uFEFF\x00\x07\x1b[31mraw",
		"مرحبا 🚀 日本語",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		// Must not panic; an error is acceptable for malformed input.
		_, _ = Render(in, 80, "notty", false, nil, "")
	})
}
