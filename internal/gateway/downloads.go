package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

type PublicDownloadBinary struct {
	Platform  string `json:"platform"`
	Label     string `json:"label"`
	FileName  string `json:"file_name"`
	URL       string `json:"url"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
}

type PublicDownloadsResponse struct {
	Source          string                 `json:"source"`
	Available       bool                   `json:"available"`
	Repo            string                 `json:"repo,omitempty"`
	Tag             string                 `json:"tag,omitempty"`
	ReleaseURL      string                 `json:"release_url,omitempty"`
	ReleaseNotesURL string                 `json:"release_notes_url,omitempty"`
	ChecksumsURL    string                 `json:"checksums_url,omitempty"`
	Downloads       []PublicDownloadBinary `json:"downloads"`
	Message         string                 `json:"message,omitempty"`
	GeneratedAt     string                 `json:"generated_at"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type githubReleasePayload struct {
	TagName string               `json:"tag_name"`
	HTMLURL string               `json:"html_url"`
	Assets  []githubReleaseAsset `json:"assets"`
}

type GitHubReleaseDownloadsProvider struct {
	repo     string
	tag      string
	token    string
	cacheTTL time.Duration
	client   *http.Client
	apiBase  string
	now      func() time.Time

	mu       sync.Mutex
	cachedAt time.Time
	cached   PublicDownloadsResponse
}

func NewGitHubReleaseDownloadsProvider(cfg Config) *GitHubReleaseDownloadsProvider {
	return &GitHubReleaseDownloadsProvider{
		repo:     strings.TrimSpace(cfg.GitHubReleaseRepo),
		tag:      strings.TrimSpace(cfg.GitHubReleaseTag),
		token:    strings.TrimSpace(cfg.GitHubToken),
		cacheTTL: cfg.PublicDownloadCacheTTL,
		client: &http.Client{
			Timeout: 8 * time.Second,
		},
		apiBase: "https://api.github.com",
		now:     func() time.Time { return time.Now().UTC() },
	}
}

func (p *GitHubReleaseDownloadsProvider) Resolve(ctx context.Context) PublicDownloadsResponse {
	now := p.now().UTC()

	p.mu.Lock()
	if !p.cachedAt.IsZero() && now.Sub(p.cachedAt) < p.cacheTTL {
		cached := p.cached
		p.mu.Unlock()
		return cached
	}
	p.mu.Unlock()

	resolved := p.resolveUncached(ctx)
	resolved.GeneratedAt = now.Format(time.RFC3339)

	p.mu.Lock()
	p.cached = resolved
	p.cachedAt = now
	p.mu.Unlock()
	return resolved
}

func (p *GitHubReleaseDownloadsProvider) resolveUncached(ctx context.Context) PublicDownloadsResponse {
	repo := strings.TrimSpace(p.repo)
	if repo == "" {
		return unavailableDownloadsResponse("", "download artifacts are not configured yet")
	}
	if strings.Count(repo, "/") != 1 {
		return unavailableDownloadsResponse(repo, "invalid PROXER_GITHUB_RELEASE_REPO format, expected owner/repo")
	}

	endpointPath := fmt.Sprintf("/repos/%s/releases/latest", repo)
	if tag := strings.TrimSpace(p.tag); tag != "" {
		endpointPath = fmt.Sprintf("/repos/%s/releases/tags/%s", repo, url.PathEscape(tag))
	}
	requestURL := strings.TrimRight(p.apiBase, "/") + endpointPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return unavailableDownloadsResponse(repo, fmt.Sprintf("build GitHub release request: %v", err))
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "proxer-gateway")
	if token := strings.TrimSpace(p.token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return unavailableDownloadsResponse(repo, fmt.Sprintf("fetch release from GitHub: %v", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return unavailableDownloadsResponse(repo, fmt.Sprintf("GitHub release lookup failed: %s", message))
	}

	var release githubReleasePayload
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return unavailableDownloadsResponse(repo, fmt.Sprintf("decode GitHub release payload: %v", err))
	}

	downloads, checksumsURL, releaseNotesURL := mapGitHubReleaseAssets(release.Assets)
	if len(downloads) == 0 {
		return PublicDownloadsResponse{
			Source:          "github_releases",
			Available:       false,
			Repo:            repo,
			Tag:             strings.TrimSpace(release.TagName),
			ReleaseURL:      strings.TrimSpace(release.HTMLURL),
			ChecksumsURL:    checksumsURL,
			ReleaseNotesURL: releaseNotesURL,
			Downloads:       []PublicDownloadBinary{},
			Message:         "no desktop binaries found in the configured release",
		}
	}

	return PublicDownloadsResponse{
		Source:          "github_releases",
		Available:       true,
		Repo:            repo,
		Tag:             strings.TrimSpace(release.TagName),
		ReleaseURL:      strings.TrimSpace(release.HTMLURL),
		ReleaseNotesURL: releaseNotesURL,
		ChecksumsURL:    checksumsURL,
		Downloads:       downloads,
	}
}

func unavailableDownloadsResponse(repo, message string) PublicDownloadsResponse {
	return PublicDownloadsResponse{
		Source:    "github_releases",
		Available: false,
		Repo:      strings.TrimSpace(repo),
		Downloads: []PublicDownloadBinary{},
		Message:   strings.TrimSpace(message),
	}
}

func mapGitHubReleaseAssets(assets []githubReleaseAsset) ([]PublicDownloadBinary, string, string) {
	byPlatform := map[string]PublicDownloadBinary{}
	checksumsURL := ""
	releaseNotesURL := ""
	for _, asset := range assets {
		name := strings.TrimSpace(asset.Name)
		assetURL := strings.TrimSpace(asset.BrowserDownloadURL)
		if name == "" || assetURL == "" {
			continue
		}

		lower := strings.ToLower(name)
		if checksumsURL == "" && strings.Contains(lower, "checksum") && strings.HasSuffix(lower, ".txt") {
			checksumsURL = assetURL
			continue
		}
		if releaseNotesURL == "" && strings.Contains(lower, "release-notes") && strings.HasSuffix(lower, ".md") {
			releaseNotesURL = assetURL
			continue
		}

		platform, label := classifyDesktopAsset(lower)
		if platform == "" {
			continue
		}
		if _, exists := byPlatform[platform]; exists {
			continue
		}
		byPlatform[platform] = PublicDownloadBinary{
			Platform:  platform,
			Label:     label,
			FileName:  name,
			URL:       assetURL,
			SizeBytes: asset.Size,
		}
	}

	order := []string{"macos", "linux", "windows"}
	downloads := make([]PublicDownloadBinary, 0, len(byPlatform))
	for _, platform := range order {
		if binary, ok := byPlatform[platform]; ok {
			downloads = append(downloads, binary)
			delete(byPlatform, platform)
		}
	}
	remaining := make([]string, 0, len(byPlatform))
	for platform := range byPlatform {
		remaining = append(remaining, platform)
	}
	sort.Strings(remaining)
	for _, platform := range remaining {
		downloads = append(downloads, byPlatform[platform])
	}
	return downloads, checksumsURL, releaseNotesURL
}

func classifyDesktopAsset(lowerName string) (platform, label string) {
	if strings.HasSuffix(lowerName, ".appimage") {
		return "linux", "Linux AppImage"
	}
	if strings.HasSuffix(lowerName, ".msi") {
		return "windows", "Windows MSI"
	}
	if strings.HasSuffix(lowerName, ".zip") &&
		(strings.Contains(lowerName, "mac") || strings.Contains(lowerName, "darwin") || strings.Contains(lowerName, "osx")) {
		return "macos", "macOS App Bundle"
	}
	return "", ""
}
