package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// UpdateInfo describes the result of a version check.
type UpdateInfo struct {
	Available       bool   `json:"-"`
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	DownloadURL     string `json:"downloadUrl"`
	ReleaseURL      string `json:"releaseUrl"`
	CheckedAt       int64  `json:"checkedAt"`       // unix seconds of the last successful check
	LastAttemptedAt int64  `json:"lastAttemptedAt"` // unix seconds of the last automatic attempt
}

// updateCacheFile is where the last check result is cached to respect the
// GitHub API rate limit.
func updateCacheFile() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "update-check.json"), nil
}

// CheckForUpdate queries the GitHub Releases API for the latest release of the
// configured repo and compares it to the current version. Results are cached
// for cfg.UpdateCheckHours, including failed attempts. A network failure is not
// fatal to the application: it returns the last cached result and a non-nil
// error for callers that want to distinguish the failed refresh.
func CheckForUpdate(ctx context.Context, cfg Defaults) (UpdateInfo, error) {
	info := UpdateInfo{Current: Version}

	if !cfg.CheckForUpdates || cfg.UpdateRepo == "" {
		return info, nil
	}

	// Serve from cache when fresh.
	if cached, ok := readUpdateCache(cfg); ok {
		return cached, nil
	}

	latest, dl, rel, err := fetchLatestRelease(ctx, cfg.UpdateRepo)
	if err != nil {
		info = CachedUpdate(cfg)
		info.LastAttemptedAt = time.Now().Unix()
		writeUpdateCache(info)
		return info, err
	}

	info.Latest = latest
	info.DownloadURL = dl
	info.ReleaseURL = rel
	info.CheckedAt = time.Now().Unix()
	info.LastAttemptedAt = info.CheckedAt
	info.Available = VersionLess(Version, latest)

	writeUpdateCache(info)
	return info, nil
}

// CheckForUpdateNow performs an immediate version check, bypassing both the
// CheckForUpdates gate and the on-disk cache. It is used by the explicit
// `mdv update` command, where a fresh result is always wanted.
func CheckForUpdateNow(ctx context.Context, cfg Defaults) (UpdateInfo, error) {
	info := UpdateInfo{Current: Version}

	if cfg.UpdateRepo == "" {
		return info, fmt.Errorf("no update repository is configured")
	}

	latest, dl, rel, err := fetchLatestRelease(ctx, cfg.UpdateRepo)
	if err != nil {
		return info, err
	}

	info.Latest = latest
	info.DownloadURL = dl
	info.ReleaseURL = rel
	info.CheckedAt = time.Now().Unix()
	info.LastAttemptedAt = info.CheckedAt
	info.Available = VersionLess(Version, latest)

	writeUpdateCache(info)
	return info, nil
}

// CachedUpdate returns the last successful result without performing any I/O
// beyond reading the local cache. It is safe to call during startup.
func CachedUpdate(cfg Defaults) UpdateInfo {
	if !cfg.CheckForUpdates || cfg.UpdateRepo == "" {
		return UpdateInfo{Current: Version}
	}
	info, ok := readUpdateCacheFile()
	if !ok {
		return UpdateInfo{Current: Version}
	}
	return info
}

func readUpdateCache(cfg Defaults) (UpdateInfo, bool) {
	info, ok := readUpdateCacheFile()
	if !ok {
		return UpdateInfo{}, false
	}
	attemptedAt := info.LastAttemptedAt
	if attemptedAt == 0 {
		attemptedAt = info.CheckedAt
	}
	age := time.Since(time.Unix(attemptedAt, 0))
	if age > time.Duration(cfg.UpdateCheckHours)*time.Hour {
		return UpdateInfo{}, false
	}
	return info, true
}

func readUpdateCacheFile() (UpdateInfo, bool) {
	path, err := updateCacheFile()
	if err != nil {
		return UpdateInfo{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return UpdateInfo{}, false
	}
	var info UpdateInfo
	if json.Unmarshal(raw, &info) != nil {
		return UpdateInfo{}, false
	}
	info.Current = Version
	info.Available = VersionLess(Version, info.Latest)
	return info, true
}

func writeUpdateCache(info UpdateInfo) {
	path, err := updateCacheFile()
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if raw, err := json.MarshalIndent(info, "", "  "); err == nil {
		_ = os.WriteFile(path, raw, 0o644)
	}
}

// ghRelease is the subset of the GitHub release payload we use.
type ghRelease struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func fetchLatestRelease(ctx context.Context, repo string) (tag, downloadURL, releaseURL string, err error) {
	return fetchLatestReleaseWithClient(ctx, repo, &http.Client{Timeout: 8 * time.Second})
}

func fetchLatestReleaseWithClient(ctx context.Context, repo string, client *http.Client) (tag, downloadURL, releaseURL string, err error) {
	api := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", AppName+"/"+Version)

	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			latest, release, fallbackErr := fetchLatestReleaseRedirect(ctx, repo, client)
			if fallbackErr == nil {
				return latest, release, release, nil
			}
			return "", "", "", fmt.Errorf("github releases API returned %s; release redirect failed: %w", resp.Status, fallbackErr)
		}
		return "", "", "", fmt.Errorf("github releases API returned %s", resp.Status)
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", "", err
	}

	dl := pickAssetForHost(rel)
	if dl == "" {
		dl = rel.HTMLURL
	}
	return rel.TagName, dl, rel.HTMLURL, nil
}

func fetchLatestReleaseRedirect(ctx context.Context, repo string, client *http.Client) (string, string, error) {
	latestURL := fmt.Sprintf("https://github.com/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, latestURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", AppName+"/"+Version)
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("github release page returned %s", resp.Status)
	}
	url := resp.Request.URL
	prefix := "/" + repo + "/releases/tag/"
	if url.Scheme != "https" || url.Hostname() != "github.com" || !strings.HasPrefix(url.Path, prefix) {
		return "", "", fmt.Errorf("unexpected github release redirect")
	}
	tag := strings.TrimPrefix(url.Path, prefix)
	if _, _, ok := parseSemver(tag); !ok || strings.Contains(tag, "/") {
		return "", "", fmt.Errorf("invalid github release tag %q", tag)
	}
	return tag, url.String(), nil
}

// pickAssetForHost selects the release asset matching the current OS/arch, or
// returns "" if none is an obvious match.
func pickAssetForHost(rel ghRelease) string {
	goos, goarch := hostOSArch()
	for _, a := range rel.Assets {
		name := strings.ToLower(a.Name)
		if strings.Contains(name, goos) && (strings.Contains(name, goarch) || strings.Contains(name, "universal")) {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

// VersionLess reports whether version a is older than version b using a lenient
// semantic-version comparison. Leading "v" and pre-release/build metadata are
// handled; unparsable versions compare as equal (no false update prompt).
func VersionLess(a, b string) bool {
	if b == "" {
		return false
	}
	am, ap, aok := parseSemver(a)
	bm, bp, bok := parseSemver(b)
	if !aok || !bok {
		return false
	}
	for i := 0; i < 3; i++ {
		if am[i] != bm[i] {
			return am[i] < bm[i]
		}
	}
	// Equal core versions: a release (no pre) outranks a pre-release.
	switch {
	case ap == "" && bp == "":
		return false
	case ap == "" && bp != "":
		return false // a is final, b is pre -> a not less
	case ap != "" && bp == "":
		return true // a is pre, b is final -> a is less
	default:
		return ap < bp
	}
}

// parseSemver parses "vMAJOR.MINOR.PATCH[-pre][+build]" leniently.
func parseSemver(s string) (core [3]int, pre string, ok bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	if s == "" {
		return core, "", false
	}
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		pre = s[i+1:]
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	for i := 0; i < 3 && i < len(parts); i++ {
		n, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil {
			return core, "", false
		}
		core[i] = n
	}
	return core, pre, true
}
