package console

import (
	"strings"
	"testing"
)

// TestRenderResilientInput feeds a range of difficult documents through the
// console renderer and asserts each one renders without error and still
// surfaces its surrounding content. This exercises the resilience contract end
// to end: one malformed or unusual construct must not abort the whole render.
func TestRenderResilientInput(t *testing.T) {
	t.Setenv("NO_COLOR", "1") // deterministic, ANSI-free output

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "malformed table next to prose",
			in:   "Intro.\n\n| a | b |\n| --- |\n| 1 | 2 | 3 |\n\nOutro.\n",
			want: "Outro",
		},
		{
			name: "unterminated code fence",
			in:   "Before\n\n```go\nfunc main() {\n",
			want: "Before",
		},
		{
			name: "unsupported mermaid then text",
			in:   "```mermaid\ngraph TD; A-->B\n```\n\nStill here.\n",
			want: "Still here",
		},
		{
			name: "rtl and unicode",
			in:   "# مرحبا\n\nשלום and English 🚀 日本語.\n",
			want: "English",
		},
		{
			name: "crlf and bom",
			in:   "\uFEFF# Title\r\n\r\nParagraph.\r\n",
			want: "Paragraph",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sb strings.Builder
			if err := Render(&sb, tt.in, "doc.md", Options{Width: 80}); err != nil {
				t.Fatalf("Render returned error for resilient input: %v", err)
			}
			if !strings.Contains(sb.String(), tt.want) {
				t.Errorf("output missing %q; got:\n%s", tt.want, sb.String())
			}
		})
	}
}
