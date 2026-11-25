package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httpclient"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type githubReleaseClient struct {
	httpClient         *http.Client
	downloadHTTPClient *http.Client
	updateGitHubToken  string
	cacheMu            sync.Mutex
	cache              map[string]githubReleaseCacheEntry
}

type githubReleaseCacheEntry struct {
	etag string
	body []byte
}

type githubReleaseClientError struct {
	err error
}

// NewGitHubReleaseClient 创建 GitHub Release 客户端
// proxyURL 为空时直连 GitHub，支持 http/https/socks5/socks5h 协议
// 代理配置失败时行为由 allowDirectOnProxyError 控制：
//   - false（默认）：返回错误占位客户端，禁止回退到直连
//   - true：回退到直连（仅限管理员显式开启）
func NewGitHubReleaseClient(proxyURL string, allowDirectOnProxyError bool) service.GitHubReleaseClient {
	// 安全说明：httpclient.GetClient 的错误链（url.Parse / proxyutil）不含明文代理凭据，
	// 但仍通过 slog 仅在服务端日志记录，不会暴露给 HTTP 响应。
	sharedClient, err := httpclient.GetClient(httpclient.Options{
		Timeout:  30 * time.Second,
		ProxyURL: proxyURL,
	})
	if err != nil {
		if strings.TrimSpace(proxyURL) != "" && !allowDirectOnProxyError {
			slog.Warn("proxy client init failed, all requests will fail", "service", "github_release", "error", err)
			return &githubReleaseClientError{err: fmt.Errorf("proxy client init failed and direct fallback is disabled; set security.proxy_fallback.allow_direct_on_error=true to allow fallback: %w", err)}
		}
		sharedClient = &http.Client{Timeout: 30 * time.Second}
	}
	apiClient := cloneHTTPClient(sharedClient)
	apiClient.CheckRedirect = githubAPICheckRedirect(apiClient.CheckRedirect)

	// 下载客户端需要更长的超时时间
	downloadClient, err := httpclient.GetClient(httpclient.Options{
		Timeout:  10 * time.Minute,
		ProxyURL: proxyURL,
	})
	if err != nil {
		if strings.TrimSpace(proxyURL) != "" && !allowDirectOnProxyError {
			slog.Warn("proxy download client init failed, all requests will fail", "service", "github_release", "error", err)
			return &githubReleaseClientError{err: fmt.Errorf("proxy client init failed and direct fallback is disabled; set security.proxy_fallback.allow_direct_on_error=true to allow fallback: %w", err)}
		}
		downloadClient = &http.Client{Timeout: 10 * time.Minute}
	}
	downloadClient = cloneHTTPClient(downloadClient)
	downloadClient.CheckRedirect = githubAPICheckRedirect(downloadClient.CheckRedirect)

	return &githubReleaseClient{
		httpClient:         apiClient,
		downloadHTTPClient: downloadClient,
		updateGitHubToken:  os.Getenv("UPDATE_GITHUB_TOKEN"),
		cache:              make(map[string]githubReleaseCacheEntry),
	}
}

func cloneHTTPClient(client *http.Client) *http.Client {
	cloned := *client
	return &cloned
}

func isGitHubAPIURL(url *url.URL) bool {
	return url != nil && strings.EqualFold(url.Scheme, "https") && url.User == nil &&
		strings.EqualFold(url.Host, "api.github.com")
}

func githubAPICheckRedirect(previous func(*http.Request, []*http.Request) error) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if !isGitHubAPIURL(req.URL) {
			req.Header.Del("Authorization")
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
}

func (c *githubReleaseClient) newAPIRequest(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "OmniHive-Updater")
	if c.updateGitHubToken != "" && isGitHubAPIURL(req.URL) {
		req.Header.Set("Authorization", "Bearer "+c.updateGitHubToken)
	}
	return req, nil
}

func (c *githubReleaseClientError) FetchLatestRelease(ctx context.Context, repo string) (*service.GitHubRelease, error) {
	return nil, c.err
}

func (c *githubReleaseClientError) FetchRecentReleases(ctx context.Context, repo string, perPage int) ([]*service.GitHubRelease, error) {
	return nil, c.err
}

func (c *githubReleaseClientError) DownloadFile(ctx context.Context, url, dest string, maxSize int64) error {
	return c.err
}

func (c *githubReleaseClientError) FetchChecksumFile(ctx context.Context, url string) ([]byte, error) {
	return nil, c.err
}

func (c *githubReleaseClient) FetchLatestRelease(ctx context.Context, repo string) (*service.GitHubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	var release service.GitHubRelease
	if err := c.fetchAPIJSON(ctx, url, &release); err != nil {
		if c.updateGitHubToken == "" && (strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "429")) {
			return c.fetchLatestReleaseRedirect(ctx, repo)
		}
		return nil, err
	}
	return &release, nil
}

func (c *githubReleaseClient) FetchRecentReleases(ctx context.Context, repo string, perPage int) ([]*service.GitHubRelease, error) {
	if perPage <= 0 {
		perPage = 10
	}
	if perPage > 100 {
		perPage = 100 // GitHub API hard limit
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=%d", repo, perPage)

	var releases []*service.GitHubRelease
	if err := c.fetchAPIJSON(ctx, url, &releases); err != nil {
		return nil, err
	}
	return releases, nil
}

func (c *githubReleaseClient) fetchAPIJSON(ctx context.Context, target string, destination any) error {
	req, err := c.newAPIRequest(ctx, target)
	if err != nil {
		return err
	}
	c.cacheMu.Lock()
	cached, found := c.cache[target]
	c.cacheMu.Unlock()
	if found && cached.etag != "" {
		req.Header.Set("If-None-Match", cached.etag)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotModified && found {
		return json.Unmarshal(cached.body, destination)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, destination); err != nil {
		return err
	}
	c.cacheMu.Lock()
	if c.cache == nil {
		c.cache = make(map[string]githubReleaseCacheEntry)
	}
	c.cache[target] = githubReleaseCacheEntry{etag: resp.Header.Get("ETag"), body: body}
	c.cacheMu.Unlock()
	return nil
}

func (c *githubReleaseClient) fetchLatestReleaseRedirect(ctx context.Context, repo string) (*service.GitHubRelease, error) {
	latestURL := fmt.Sprintf("https://github.com/%s/releases/latest", repo)
	client := cloneHTTPClient(c.httpClient)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "OmniHive-Updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return nil, fmt.Errorf("GitHub latest release redirect returned %d after API failure", resp.StatusCode)
	}
	location, err := resp.Location()
	if err != nil {
		return nil, err
	}
	marker := "/" + repo + "/releases/tag/"
	index := strings.Index(location.Path, marker)
	if location.Scheme != "https" || location.Host != "github.com" || index < 0 {
		return nil, fmt.Errorf("unexpected GitHub latest release redirect")
	}
	tag, err := url.PathUnescape(strings.TrimPrefix(location.Path[index:], marker))
	if err != nil || tag == "" || strings.Contains(tag, "/") {
		return nil, fmt.Errorf("invalid GitHub latest release tag")
	}
	pageURL := "https://github.com" + location.Path[index:]
	pageReq, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	pageReq.Header.Set("User-Agent", "OmniHive-Updater")
	pageResp, err := c.httpClient.Do(pageReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = pageResp.Body.Close() }()
	if pageResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub release page returned %d", pageResp.StatusCode)
	}
	document, err := html.Parse(io.LimitReader(pageResp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	prefix := "/" + repo + "/releases/download/" + location.Path[index+len(marker):] + "/"
	seen := make(map[string]bool)
	assets := []*service.GitHubAsset{}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" {
			for _, attribute := range node.Attr {
				if attribute.Key != "href" || !strings.HasPrefix(attribute.Val, prefix) {
					continue
				}
				name := path.Base(attribute.Val)
				if name == "." || name == "/" || name == "" || seen[name] {
					continue
				}
				seen[name] = true
				assets = append(assets, &service.GitHubAsset{Name: name, BrowserDownloadURL: "https://github.com" + attribute.Val})
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	return &service.GitHubRelease{
		TagName: tag,
		Name:    tag,
		HTMLURL: pageURL,
		Assets:  fallbackReleaseAssets(repo, tag, assets),
	}, nil
}

func fallbackReleaseAssets(repo, tag string, discovered []*service.GitHubAsset) []service.GitHubAsset {
	assets := make([]service.GitHubAsset, 0, len(discovered)+3)
	seen := make(map[string]bool)
	for _, asset := range discovered {
		if asset != nil && asset.Name != "" {
			seen[asset.Name] = true
			assets = append(assets, *asset)
		}
	}
	for _, name := range []string{
		"checksums.txt",
		"omnihive-release-manifest.json",
		fmt.Sprintf("omnihive_%s_%s_%s.tar.gz", strings.TrimPrefix(tag, "v"), runtime.GOOS, runtime.GOARCH),
	} {
		if !seen[name] {
			assets = append(assets, service.GitHubAsset{Name: name, BrowserDownloadURL: fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, tag, name)})
		}
	}
	return assets
}

func (c *githubReleaseClient) DownloadFile(ctx context.Context, url, dest string, maxSize int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	// 使用预配置的下载客户端（已包含代理配置）
	if c.updateGitHubToken != "" && isGitHubAPIURL(req.URL) {
		req.Header.Set("Authorization", "Bearer "+c.updateGitHubToken)
		req.Header.Set("Accept", "application/octet-stream")
		req.Header.Set("User-Agent", "OmniHive-Updater")
	}

	resp, err := c.downloadHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	// SECURITY: Check Content-Length if available
	if resp.ContentLength > maxSize {
		return fmt.Errorf("file too large: %d bytes (max %d)", resp.ContentLength, maxSize)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}

	// SECURITY: Use LimitReader to enforce max download size even if Content-Length is missing/wrong
	limited := io.LimitReader(resp.Body, maxSize+1)
	written, err := io.Copy(out, limited)

	// Close file before attempting to remove (required on Windows)
	_ = out.Close()

	if err != nil {
		_ = os.Remove(dest) // Clean up partial file (best-effort)
		return err
	}

	// Check if we hit the limit (downloaded more than maxSize)
	if written > maxSize {
		_ = os.Remove(dest) // Clean up partial file (best-effort)
		return fmt.Errorf("download exceeded maximum size of %d bytes", maxSize)
	}

	return nil
}

func (c *githubReleaseClient) FetchChecksumFile(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	if c.updateGitHubToken != "" && isGitHubAPIURL(req.URL) {
		req.Header.Set("Authorization", "Bearer "+c.updateGitHubToken)
		req.Header.Set("Accept", "application/octet-stream")
		req.Header.Set("User-Agent", "OmniHive-Updater")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
