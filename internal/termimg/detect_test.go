package termimg

import (
	"os"
	"testing"
)

func TestInsideMultiplexer(t *testing.T) {
	t.Run("TMUX set", func(t *testing.T) {
		t.Setenv("TMUX", "/tmp/tmux-1000/default,1234,0")
		t.Setenv("TERM", "xterm-256color")
		if !insideMultiplexer() {
			t.Error("insideMultiplexer() = false with TMUX set, want true")
		}
	})
	t.Run("TERM screen prefix", func(t *testing.T) {
		t.Setenv("TMUX", "")
		t.Setenv("TERM", "screen-256color")
		if !insideMultiplexer() {
			t.Error("insideMultiplexer() = false with TERM=screen, want true")
		}
	})
	t.Run("TERM tmux prefix", func(t *testing.T) {
		t.Setenv("TMUX", "")
		t.Setenv("TERM", "tmux-256color")
		if !insideMultiplexer() {
			t.Error("insideMultiplexer() = false with TERM=tmux, want true")
		}
	})
	t.Run("plain terminal", func(t *testing.T) {
		t.Setenv("TMUX", "")
		t.Setenv("TERM", "xterm-256color")
		if insideMultiplexer() {
			t.Error("insideMultiplexer() = true on plain terminal, want false")
		}
	})
}

func TestDetectGraphicsNilAndNonTTY(t *testing.T) {
	if got := DetectGraphics(nil); got != ProtocolNone {
		t.Errorf("DetectGraphics(nil) = %v, want none", got)
	}
	// os.Stdout under `go test` is not a terminal -> none regardless of env.
	if got := DetectGraphics(os.Stdout); got != ProtocolNone {
		t.Errorf("DetectGraphics(non-tty) = %v, want none", got)
	}
}

func TestSupportsColorNilAndNonTTY(t *testing.T) {
	if SupportsColor(nil) {
		t.Error("SupportsColor(nil) = true, want false")
	}
	if SupportsColor(os.Stdout) {
		t.Error("SupportsColor(non-tty) = true, want false")
	}
}

func TestResolveModes(t *testing.T) {
	// ModeOff always yields none, even on a real terminal.
	if got := Resolve(ModeOff, os.Stdout); got != ProtocolNone {
		t.Errorf("Resolve(ModeOff) = %v, want none", got)
	}
	// With a non-terminal output, every other mode degrades to none.
	for _, m := range []Mode{ModeAuto, ModeBlocks, ModeGraphics} {
		if got := Resolve(m, os.Stdout); got != ProtocolNone {
			t.Errorf("Resolve(%v, non-tty) = %v, want none", m, got)
		}
	}
	// A nil file is treated as non-terminal too.
	if got := Resolve(ModeAuto, nil); got != ProtocolNone {
		t.Errorf("Resolve(ModeAuto, nil) = %v, want none", got)
	}
}

func TestReadDataURI(t *testing.T) {
	t.Run("plain text url-encoded", func(t *testing.T) {
		raw, isSVG, err := readDataURI("data:text/plain,hello%20world")
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if string(raw) != "hello world" || isSVG {
			t.Errorf("got (%q, svg=%v), want (\"hello world\", false)", raw, isSVG)
		}
	})
	t.Run("base64", func(t *testing.T) {
		raw, isSVG, err := readDataURI("data:application/octet-stream;base64,aGVsbG8=")
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if string(raw) != "hello" || isSVG {
			t.Errorf("got (%q, svg=%v), want (\"hello\", false)", raw, isSVG)
		}
	})
	t.Run("svg media type", func(t *testing.T) {
		_, isSVG, err := readDataURI("data:image/svg+xml,<svg></svg>")
		if err != nil || !isSVG {
			t.Errorf("svg data URI: isSVG=%v err=%v, want isSVG=true nil", isSVG, err)
		}
	})
	t.Run("malformed no comma", func(t *testing.T) {
		if _, _, err := readDataURI("data:image/png;base64"); err == nil {
			t.Error("malformed data URI returned nil error, want error")
		}
	})
	t.Run("invalid base64", func(t *testing.T) {
		if _, _, err := readDataURI("data:image/png;base64,!!!not-base64!!!"); err == nil {
			t.Error("invalid base64 returned nil error, want error")
		}
	})
}

func TestLooksLikeSVG(t *testing.T) {
	cases := []struct {
		name string
		data string
		want bool
	}{
		{"svg root", `<svg xmlns="http://www.w3.org/2000/svg"></svg>`, true},
		{"xml then svg", "<?xml version=\"1.0\"?>\n<svg></svg>", true},
		{"xml no svg", "<?xml version=\"1.0\"?>\n<html></html>", false},
		{"png magic", "\x89PNG\r\n\x1a\n", false},
		{"empty", "", false},
		{"uppercase", "<SVG></SVG>", true},
	}
	for _, c := range cases {
		if got := looksLikeSVG([]byte(c.data)); got != c.want {
			t.Errorf("looksLikeSVG(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPathOf(t *testing.T) {
	cases := map[string]string{
		"https://example.com/a/b.png":     "/a/b.png",
		"https://example.com/img.svg?x=1": "/img.svg",
		"http://host/path/file.jpeg":      "/path/file.jpeg",
		"relative/path.png":               "relative/path.png",
	}
	for in, want := range cases {
		if got := pathOf(in); got != want {
			t.Errorf("pathOf(%q) = %q, want %q", in, got, want)
		}
	}
}
