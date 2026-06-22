package tui

import (
	"testing"

	"github.com/thgossler/mdv/internal/core"
)

func TestEntryPointPath(t *testing.T) {
	doc := func(rel string) core.DocFile {
		name := rel
		if i := lastSlash(rel); i >= 0 {
			name = rel[i+1:]
		}
		return core.DocFile{Path: "/ws/" + rel, Name: name, Rel: rel}
	}

	tests := []struct {
		name  string
		files []core.DocFile
		want  string
	}{
		{
			name:  "root README wins over docs README",
			files: []core.DocFile{doc("docs/README.md"), doc("README.md")},
			want:  "/ws/README.md",
		},
		{
			name:  "root index when no README",
			files: []core.DocFile{doc("index.md"), doc("docs/README.md")},
			want:  "/ws/index.md",
		},
		{
			name:  "README preferred over index at root",
			files: []core.DocFile{doc("index.md"), doc("README.md")},
			want:  "/ws/README.md",
		},
		{
			name:  "docs README when no root entry",
			files: []core.DocFile{doc("guide.md"), doc("docs/README.md")},
			want:  "/ws/docs/README.md",
		},
		{
			name:  "docs preferred over wiki",
			files: []core.DocFile{doc("wiki/README.md"), doc("docs/README.md")},
			want:  "/ws/docs/README.md",
		},
		{
			name:  "non-doc subfolder README does not qualify",
			files: []core.DocFile{doc("src/README.md")},
			want:  "",
		},
		{
			name:  "depth 2 never qualifies",
			files: []core.DocFile{doc("docs/guide/README.md")},
			want:  "",
		},
		{
			name:  "case insensitive",
			files: []core.DocFile{doc("ReadMe.md")},
			want:  "/ws/ReadMe.md",
		},
		{
			name:  "no candidates",
			files: []core.DocFile{doc("a.md"), doc("b.md")},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := entryPointPath(tt.files); got != tt.want {
				t.Errorf("entryPointPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
