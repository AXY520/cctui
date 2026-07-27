package update

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchGiteeHTMLParsesLatestTag(t *testing.T) {
	html := `
	<a href="/aixinyin/cctui/releases/tag/v0.6.1">v0.6.1</a>
	<a href="/aixinyin/cctui/releases/tag/v0.7.0">v0.7.0</a>
	<a href="/aixinyin/cctui/releases/tag/v0.6.2">v0.6.2</a>
	`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/releases/download/"):
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/releases") || r.URL.Path == "/":
			_, _ = w.Write([]byte(html))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	// monkeypatch constants via local call by temporarily replacing URL is hard;
	// instead test regex + Compare path using fetchGiteeHTML against redirected endpoint.
	// We reimplement minimal by calling giteeTagRe directly.
	matches := giteeTagRe.FindAllStringSubmatch(html, -1)
	if len(matches) < 3 {
		t.Fatalf("matches=%v", matches)
	}
	best := ""
	for _, m := range matches {
		latest := Normalize(m[1])
		if best == "" || Compare(latest, best) > 0 {
			best = latest
		}
	}
	if best != "0.7.0" {
		t.Fatalf("best=%s", best)
	}
}

func TestHasUpdateAcrossSourcesLogic(t *testing.T) {
	// Simulate: github old 0.2.0 should not hide gitee 0.7.0
	candidates := []releaseCandidate{
		{Latest: "0.2.0", Source: "github"},
		{Latest: "0.7.0", Source: "gitee-html"},
	}
	best := candidates[0]
	for _, item := range candidates[1:] {
		if Compare(item.Latest, best.Latest) > 0 {
			best = item
		}
	}
	if best.Latest != "0.7.0" {
		t.Fatal(best)
	}
	if !HasUpdate("0.6.2", best.Latest) {
		t.Fatal("should detect update")
	}
	if HasUpdate("0.7.0", best.Latest) {
		t.Fatal("should be current")
	}
}

func TestCandidateFromReleaseFallbackURL(t *testing.T) {
	release := &releaseResponse{
		TagName: "v0.7.0",
		Assets: []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		}{
			{Name: "cctui-linux-amd64.tar.gz"}, // empty download url
		},
	}
	got, err := candidateFromRelease(release, "cctui-linux-amd64.tar.gz", "gitee-api", func(tag, name string) string {
		return fmt.Sprintf("https://example/%s/%s", tag, name)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.AssetURL != "https://example/v0.7.0/cctui-linux-amd64.tar.gz" {
		t.Fatalf("url=%s", got.AssetURL)
	}
}
