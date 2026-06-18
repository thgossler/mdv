package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseSemverInvalid(t *testing.T) {
	bad := []string{"", "x", "1.x.0", "1.2.three", "v..", "abc.def.ghi"}
	for _, s := range bad {
		if _, _, ok := parseSemver(s); ok {
			t.Errorf("parseSemver(%q) ok = true, want false", s)
		}
	}

	good := []struct {
		in   string
		core [3]int
		pre  string
	}{
		{"v1.2.3", [3]int{1, 2, 3}, ""},
		{"1.2.3-rc.1", [3]int{1, 2, 3}, "rc.1"},
		{"v2.0.0+build5", [3]int{2, 0, 0}, ""},
		{"1", [3]int{1, 0, 0}, ""}, // lenient: missing minor/patch default to 0
		{"1.5", [3]int{1, 5, 0}, ""},
	}
	for _, c := range good {
		gotCore, gotPre, ok := parseSemver(c.in)
		if !ok || gotCore != c.core || gotPre != c.pre {
			t.Errorf("parseSemver(%q) = (%v,%q,%v), want (%v,%q,true)", c.in, gotCore, gotPre, ok, c.core, c.pre)
		}
	}
}

func TestPickAssetForHost(t *testing.T) {
	goos, goarch := hostOSArch()

	matching := ghRelease{Assets: []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	}{
		{Name: "mdv-" + goos + "-" + goarch + ".tar.gz", BrowserDownloadURL: "https://dl/exact"},
		{Name: "mdv-other-os-arch.tar.gz", BrowserDownloadURL: "https://dl/other"},
	}}
	if got := pickAssetForHost(matching); got != "https://dl/exact" {
		t.Errorf("exact match = %q, want https://dl/exact", got)
	}

	universal := ghRelease{Assets: []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	}{
		{Name: "mdv-" + goos + "-universal.zip", BrowserDownloadURL: "https://dl/universal"},
	}}
	if got := pickAssetForHost(universal); got != "https://dl/universal" {
		t.Errorf("universal match = %q, want https://dl/universal", got)
	}

	none := ghRelease{Assets: []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	}{
		{Name: "mdv-some-unrelated-thing.txt", BrowserDownloadURL: "https://dl/no"},
	}}
	if got := pickAssetForHost(none); got != "" {
		t.Errorf("no match = %q, want empty", got)
	}
}

func TestUpdateCacheRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := DefaultSettings()
	cfg.UpdateCheckHours = 24

	// No cache file yet -> miss.
	if _, ok := readUpdateCache(cfg); ok {
		t.Fatal("readUpdateCache on empty config dir returned ok=true")
	}

	// Write a fresh cache and read it back.
	fresh := UpdateInfo{
		Latest:     "v9.9.9",
		ReleaseURL: "https://example.com/release",
		CheckedAt:  time.Now().Unix(),
	}
	writeUpdateCache(fresh)

	got, ok := readUpdateCache(cfg)
	if !ok {
		t.Fatal("readUpdateCache after write returned ok=false")
	}
	if got.Latest != "v9.9.9" {
		t.Errorf("cached Latest = %q, want v9.9.9", got.Latest)
	}
	// Current and Available are recomputed against the running Version.
	if got.Current != Version {
		t.Errorf("cached Current = %q, want %q", got.Current, Version)
	}
	if got.Available != VersionLess(Version, "v9.9.9") {
		t.Errorf("cached Available = %v, want %v", got.Available, VersionLess(Version, "v9.9.9"))
	}
}

func TestReadUpdateCacheExpired(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := DefaultSettings()
	cfg.UpdateCheckHours = 1

	stale := UpdateInfo{
		Latest:    "v9.9.9",
		CheckedAt: time.Now().Add(-2 * time.Hour).Unix(),
	}
	writeUpdateCache(stale)

	if _, ok := readUpdateCache(cfg); ok {
		t.Error("expired cache returned ok=true, want false")
	}
}

func TestReadUpdateCacheMalformed(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := DefaultSettings()
	cfg.UpdateCheckHours = 24

	path, err := updateCacheFile()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := readUpdateCache(cfg); ok {
		t.Error("malformed cache returned ok=true, want false")
	}
}
