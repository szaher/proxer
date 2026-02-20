package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestMapGitHubReleaseAssets(t *testing.T) {
	assets := []githubReleaseAsset{
		{Name: "proxer-agent-macos.app.zip", BrowserDownloadURL: "https://example.com/mac.zip", Size: 123},
		{Name: "proxer-agent-v1.0.0-x86_64.AppImage", BrowserDownloadURL: "https://example.com/linux.appimage", Size: 456},
		{Name: "proxer-agent-v1.0.0-x64.msi", BrowserDownloadURL: "https://example.com/windows.msi", Size: 789},
		{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums.txt"},
		{Name: "release-notes.md", BrowserDownloadURL: "https://example.com/release-notes.md"},
	}

	downloads, checksumsURL, releaseNotesURL := mapGitHubReleaseAssets(assets)
	if len(downloads) != 3 {
		t.Fatalf("expected 3 platform downloads, got %d", len(downloads))
	}
	if downloads[0].Platform != "macos" || downloads[1].Platform != "linux" || downloads[2].Platform != "windows" {
		t.Fatalf("unexpected platform order: %+v", downloads)
	}
	if checksumsURL == "" {
		t.Fatalf("expected checksums URL")
	}
	if releaseNotesURL == "" {
		t.Fatalf("expected release notes URL")
	}
}

func TestGitHubReleaseDownloadsProviderResolveUnavailableWhenRepoMissing(t *testing.T) {
	provider := &GitHubReleaseDownloadsProvider{
		repo:     "",
		cacheTTL: time.Minute,
		now:      func() time.Time { return time.Now().UTC() },
	}
	payload := provider.Resolve(context.Background())
	if payload.Available {
		t.Fatalf("expected unavailable payload when repo is missing")
	}
	if payload.Message == "" {
		t.Fatalf("expected explanatory message for unavailable payload")
	}
}

func TestGitHubReleaseDownloadsProviderResolveFromGitHubAPI(t *testing.T) {
	releasePayload, err := json.Marshal(githubReleasePayload{
		TagName: "desktop-agent-v1.2.3",
		HTMLURL: "https://github.com/acme/proxer/releases/tag/desktop-agent-v1.2.3",
		Assets: []githubReleaseAsset{
			{Name: "proxer-agent-macos.app.zip", BrowserDownloadURL: "https://example.com/mac.zip", Size: 11},
			{Name: "proxer-agent-v1.2.3-x86_64.AppImage", BrowserDownloadURL: "https://example.com/linux.appimage", Size: 22},
			{Name: "proxer-agent-v1.2.3-x64.msi", BrowserDownloadURL: "https://example.com/windows.msi", Size: 33},
		},
	})
	if err != nil {
		t.Fatalf("marshal release payload: %v", err)
	}

	provider := &GitHubReleaseDownloadsProvider{
		repo:     "acme/proxer",
		cacheTTL: time.Minute,
		client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet {
					t.Fatalf("expected GET request, got %s", req.Method)
				}
				if req.URL.Path != "/repos/acme/proxer/releases/latest" {
					t.Fatalf("unexpected request path %q", req.URL.Path)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(string(releasePayload))),
					Header:     make(http.Header),
				}, nil
			}),
		},
		apiBase: "https://api.github.test",
		now:     func() time.Time { return time.Now().UTC() },
	}

	payload := provider.Resolve(context.Background())
	if !payload.Available {
		t.Fatalf("expected available downloads payload, got unavailable: %s", payload.Message)
	}
	if payload.Tag != "desktop-agent-v1.2.3" {
		t.Fatalf("unexpected tag %q", payload.Tag)
	}
	if len(payload.Downloads) != 3 {
		t.Fatalf("expected 3 platform downloads, got %d", len(payload.Downloads))
	}
}
