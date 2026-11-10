package tui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thgossler/mdv/internal/control"
	"github.com/thgossler/mdv/internal/core"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type controlResult struct {
	value any
	err   *control.Error
}

type controlMessage struct {
	ctx    context.Context
	method string
	params json.RawMessage
	reply  chan controlResult
}

func tuiHandler(program *tea.Program) control.Handler {
	return func(ctx context.Context, method string, params json.RawMessage) (any, *control.Error) {
		reply := make(chan controlResult, 1)
		go program.Send(controlMessage{ctx: ctx, method: method, params: params, reply: reply})
		select {
		case result := <-reply:
			return result.value, result.err
		case <-ctx.Done():
			return nil, &control.Error{Code: -32000, Message: "UI unavailable"}
		}
	}
}

func (m *Model) handleControl(method string, raw json.RawMessage) (any, *control.Error, tea.Cmd) {
	var params struct {
		Document   string          `json:"document"`
		Revision   string          `json:"revision"`
		Line       *int            `json:"line"`
		Percentage *float64        `json:"percentage"`
		Anchor     string          `json:"anchor"`
		Name       string          `json:"name"`
		Value      json.RawMessage `json:"value"`
		Path       string          `json:"path"`
	}
	if len(raw) > 0 && json.Unmarshal(raw, &params) != nil {
		return nil, &control.Error{Code: -32602, Message: "invalid parameters"}, nil
	}
	revision := m.apiRevision
	document := m.currentPath
	if m.stdin {
		document = "stdin"
	}
	if (params.Document != "" && params.Document != document) || (params.Revision != "" && params.Revision != revision) {
		return nil, &control.Error{Code: -32002, Message: "stale document or revision"}, nil
	}
	state := func() map[string]any {
		document := m.currentPath
		if m.stdin {
			document = "stdin"
		}
		return map[string]any{"mode": "tui", "document": document, "revision": m.apiRevision, "viewport": map[string]any{"percentage": m.viewport.ScrollPercent() * 100}, "sidebar": m.showList, "watch": m.watchEnabled, "extended": m.extendedSyntax, "remoteImages": m.cfg.ImagesRemote, "labels": m.labelMode}
	}
	switch method {
	case "instance.quit":
		return map[string]bool{"quitting": true}, nil, nil
	case "instance.info", "ui.get":
		return state(), nil, nil
	case "ui.capabilities":
		return map[string]any{"methods": []string{"instance.info", "instance.quit", "ui.capabilities", "ui.get", "viewport.set", "document.gotoAnchor", "document.reload", "document.open", "ui.set", "ui.execute"}, "settings": []string{"sidebar", "watch", "extended", "remoteImages", "labels"}, "actions": []string{"back", "forward", "reload"}}, nil, nil
	case "viewport.set":
		if !m.ready || (params.Line == nil) == (params.Percentage == nil) {
			return nil, &control.Error{Code: -32602, Message: "provide one line or percentage when ready"}, nil
		}
		if params.Line != nil {
			if *params.Line < 1 || *params.Line > len(m.apiLines) {
				return nil, &control.Error{Code: -32602, Message: "line out of range"}, nil
			}
			m.viewport.SetYOffset(m.apiLines[*params.Line-1])
		} else {
			if *params.Percentage < 0 || *params.Percentage > 100 {
				return nil, &control.Error{Code: -32602, Message: "percentage out of range"}, nil
			}
			maxOffset := max(0, m.viewport.TotalLineCount()-m.viewport.Height)
			m.viewport.SetYOffset(int(math.Round(float64(maxOffset) * *params.Percentage / 100)))
		}
		return state(), nil, nil
	case "document.gotoAnchor":
		if !m.controlAnchor(strings.TrimPrefix(params.Anchor, "#")) {
			return nil, &control.Error{Code: -32004, Message: "anchor not found"}, nil
		}
		return state(), nil, nil
	case "document.reload":
		if m.currentPath == "" {
			return nil, &control.Error{Code: -32003, Message: "no file to reload"}, nil
		}
		cmd := m.reloadCurrent("Reloaded")
		return state(), nil, cmd
	case "document.open":
		if params.Path == "" {
			return nil, &control.Error{Code: -32602, Message: "path required"}, nil
		}
		path, err := filepath.Abs(params.Path)
		if err != nil {
			return nil, &control.Error{Code: -32602, Message: "invalid path"}, nil
		}
		if _, err := core.ReadMarkdownFile(path); err != nil {
			return nil, &control.Error{Code: -32003, Message: "cannot open document"}, nil
		}
		m.stdin = false
		cmd := m.openPath(path, true)
		return state(), nil, cmd
	case "ui.set":
		if params.Name == "labels" {
			var value string
			if json.Unmarshal(params.Value, &value) != nil || (value != "title" && value != "filename") {
				return nil, &control.Error{Code: -32602, Message: "invalid labels"}, nil
			}
			if m.labelMode != value {
				m.toggleLabelMode()
			}
			return state(), nil, nil
		}
		var value bool
		if string(params.Value) != "true" && string(params.Value) != "false" {
			return nil, &control.Error{Code: -32602, Message: "value must be boolean"}, nil
		}
		_ = json.Unmarshal(params.Value, &value)
		switch params.Name {
		case "sidebar":
			if m.showList != value {
				m.toggleNav()
			}
			if m.showList != value {
				return nil, &control.Error{Code: -32005, Message: "sidebar unavailable"}, nil
			}
		case "watch":
			if m.watchEnabled != value {
				cmd := m.toggleWatch()
				if m.watchEnabled != value {
					return nil, &control.Error{Code: -32005, Message: "watch unavailable"}, cmd
				}
				return state(), nil, cmd
			}
		case "extended":
			if m.extendedSyntax != value {
				m.toggleExtendedSyntax()
			}
		case "remoteImages":
			if m.cfg.ImagesRemote != value {
				_, cmd := m.toggleRemoteImages()
				return state(), nil, cmd
			}
		default:
			return nil, &control.Error{Code: -32005, Message: "unsupported setting"}, nil
		}
		return state(), nil, nil
	case "ui.execute":
		switch params.Name {
		case "back":
			cmd := m.goBack()
			return state(), nil, cmd
		case "forward":
			cmd := m.goForward()
			return state(), nil, cmd
		case "reload":
			cmd := m.reloadCurrent("Reloaded")
			return state(), nil, cmd
		default:
			return nil, &control.Error{Code: -32005, Message: "unsupported action"}, nil
		}
	default:
		return nil, &control.Error{Code: -32601, Message: "method not found"}, nil
	}
}

func controlRevision(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

func (m *Model) controlAnchor(anchor string) bool {
	rows := strings.Split(stripANSI(m.renderedRaw()), "\n")
	slugger := core.NewSlugger()
	_, body := core.ExtractFrontmatter(m.rawMarkdown)
	source := []byte(body)
	doc := goldmark.New().Parser().Parse(text.NewReader(source))
	row := 0
	found := false
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || node.Kind() != ast.KindHeading {
			return ast.WalkContinue, nil
		}
		var title strings.Builder
		_ = ast.Walk(node, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
			if entering {
				switch value := child.(type) {
				case *ast.Text:
					title.Write(value.Value(source))
					if value.SoftLineBreak() || value.HardLineBreak() {
						title.WriteByte(' ')
					}
				case *ast.String:
					title.Write(value.Value)
				}
			}
			return ast.WalkContinue, nil
		})
		heading := strings.TrimSpace(title.String())
		slug := slugger.Slug(heading)
		for row < len(rows) && !strings.Contains(normalize(rows[row]), normalize(heading)) {
			row++
		}
		if slug == anchor && row < len(rows) {
			m.viewport.SetYOffset(row)
			found = true
			return ast.WalkStop, nil
		}
		if row < len(rows) {
			row++
		}
		return ast.WalkSkipChildren, nil
	})
	return found
}

func sourceRows(source, rendered string) []int {
	sourceLines := strings.Split(source, "\n")
	rows := strings.Split(rendered, "\n")
	mapping := make([]int, len(sourceLines))
	frontmatter, body := core.ExtractFrontmatter(source)
	removedStart, removedLines := -1, 0
	if frontmatter.Has {
		prefix := 0
		for prefix < len(body) && prefix < len(source) && body[prefix] == source[prefix] {
			prefix++
		}
		removed := len(source) - len(body)
		removedStart = strings.Count(source[:prefix], "\n")
		removedLines = strings.Count(source[prefix:prefix+removed], "\n")
	}
	index := make(map[string][]int)
	for row, text := range rows {
		seen := make(map[string]bool)
		for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
			if len(word) >= 3 && !seen[word] {
				index[word] = append(index[word], row)
				seen[word] = true
			}
		}
	}
	row := 0
	for lineIndex, line := range sourceLines {
		if removedStart >= 0 && lineIndex >= removedStart && lineIndex < removedStart+removedLines {
			mapping[lineIndex] = 0
			continue
		}
		words := strings.FieldsFunc(strings.ToLower(line), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
		for _, word := range words {
			candidates := index[word]
			position := sort.SearchInts(candidates, row)
			if position < len(candidates) {
				row = candidates[position]
				break
			}
		}
		mapping[lineIndex] = row
	}
	return mapping
}
