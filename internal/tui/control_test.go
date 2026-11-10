package tui

import (
	"encoding/json"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/thgossler/mdv/internal/core"
)

func TestControlViewportAndStaleRevision(t *testing.T) {
	m := Model{ready: true, currentPath: "/doc.md", rawMarkdown: "first\nsecond\nthird", apiRevision: "revision", apiLines: []int{0, 3, 6}, viewport: viewport.New(20, 2)}
	m.viewport.SetContent("0\n1\n2\n3\n4\n5\n6\n7\n8\n9")
	_, err, _ := m.handleControl("viewport.set", json.RawMessage(`{"line":2,"document":"/doc.md","revision":"revision"}`))
	if err != nil || m.viewport.YOffset != 3 {
		t.Fatalf("line scroll: offset=%d error=%v", m.viewport.YOffset, err)
	}
	_, err, _ = m.handleControl("viewport.set", json.RawMessage(`{"percentage":100}`))
	if err != nil || m.viewport.YOffset != 8 {
		t.Fatalf("percentage scroll: offset=%d error=%v", m.viewport.YOffset, err)
	}
	_, err, _ = m.handleControl("viewport.set", json.RawMessage(`{"line":1,"revision":"old"}`))
	if err == nil || err.Code != -32002 || m.viewport.YOffset != 8 {
		t.Fatalf("stale request: offset=%d error=%v", m.viewport.YOffset, err)
	}
}

func TestControlQuit(t *testing.T) {
	model := Model{ready: true}
	result, rpcErr, cmd := model.handleControl("instance.quit", json.RawMessage(`{}`))
	if rpcErr != nil || cmd != nil || result.(map[string]bool)["quitting"] != true {
		t.Fatalf("unexpected quit response: result=%v error=%v command=%v", result, rpcErr, cmd)
	}
	capabilities, rpcErr, _ := model.handleControl("ui.capabilities", nil)
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	methods := capabilities.(map[string]any)["methods"].([]string)
	for _, method := range methods {
		if method == "instance.quit" {
			return
		}
	}
	t.Fatal("quit missing from capabilities")
}

func TestSourceRowsFrontmatter(t *testing.T) {
	rows := sourceRows("---\ntitle: Later\n---\n# First\n\nLater text\n", "First\n\nLater text\n")
	if rows[1] != 0 || rows[3] != 0 || rows[5] != 2 {
		t.Fatalf("unexpected mapping: %v", rows)
	}
}

func TestControlAnchorSetextAndDuplicates(t *testing.T) {
	model := Model{cfg: core.DefaultSettings(), width: 80, height: 12, resolvedStyle: "notty", rawMarkdown: "Title\n=====\n\nText\n\n# Title\n\nMore\n", viewport: viewport.New(78, 3)}
	model.rerender()
	if !model.controlAnchor("title") {
		t.Fatal("Setext anchor not found")
	}
	first := model.viewport.YOffset
	if !model.controlAnchor("title-1") {
		t.Fatal("duplicate anchor not found")
	}
	if model.viewport.YOffset <= first {
		t.Fatalf("duplicate did not move: first=%d second=%d", first, model.viewport.YOffset)
	}
}
