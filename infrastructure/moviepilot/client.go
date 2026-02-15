package moviepilot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidBaseURL = errors.New("invalid moviepilot base url")
)

type Options struct {
	BaseURL     string
	APIToken    string
	HTTPTimeout time.Duration
	// MaxResults 仅用于客户端侧 limit 参数（同时也给上层做默认兜底）。
	MaxResults int
	// PushPath 为推送下载的接口路径。
	PushPath string
}

type Client struct {
	baseURL    string
	apiToken   string
	httpClient *http.Client
	maxResults int
	pushPath  string
}

type Result struct {
	Title    string
	Site     string
	Size     string
	Seeders  int
	Leechers int
	Link     string
	Raw      map[string]any
}

func New(opts Options) (*Client, error) {
	base := strings.TrimSpace(opts.BaseURL)
	if base == "" {
		return nil, ErrInvalidBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || u == nil {
		return nil, ErrInvalidBaseURL
	}
	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	if scheme != "http" && scheme != "https" {
		return nil, ErrInvalidBaseURL
	}

	timeout := opts.HTTPTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = 8
	}
	pushPath := strings.TrimSpace(opts.PushPath)
	if pushPath == "" {
		pushPath = "/api/v1/download"
	}

	return &Client{
		baseURL:  strings.TrimRight(base, "/"),
		apiToken: strings.TrimSpace(opts.APIToken),
		httpClient: &http.Client{
			Timeout: timeout,
		},
		maxResults: maxResults,
		pushPath:   pushPath,
	}, nil
}

func (c *Client) Check(ctx context.Context) (int, error) {
	if c == nil {
		return 0, fmt.Errorf("nil moviepilot client")
	}
	u, err := url.Parse(c.baseURL)
	if err != nil || u == nil {
		return 0, ErrInvalidBaseURL
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/v1/search/title"
	q := u.Query()
	q.Set("keyword", "ping")
	q.Set("limit", "1")
	if c.apiToken != "" {
		q.Set("token", c.apiToken)
		q.Set("api_token", c.apiToken)
		q.Set("apiToken", c.apiToken)
		q.Set("api_key", c.apiToken)
		q.Set("apikey", c.apiToken)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; emby-bot-new/1.0; +https://example.invalid)")
	// 注意：MoviePilot v2 的鉴权字段/优先级可能因版本与部署而异。
	// 实测部分部署在携带 Authorization/X-API-* 等 header 时会直接 403，
	// 因此这里默认仅使用 query 参数传递 token（与其 UI 提供的“带 token URL”一致）。

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return resp.StatusCode, fmt.Errorf("moviepilot auth failed http=%d", resp.StatusCode)
		case http.StatusNotFound:
			return resp.StatusCode, fmt.Errorf("moviepilot api not found http=%d", resp.StatusCode)
		default:
			return resp.StatusCode, fmt.Errorf("moviepilot http=%d", resp.StatusCode)
		}
	}
	return resp.StatusCode, nil
}

func (c *Client) SearchTitle(ctx context.Context, keyword string, limit int) ([]Result, error) {
	if c == nil {
		return nil, fmt.Errorf("nil moviepilot client")
	}
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("empty keyword")
	}
	if limit <= 0 || limit > c.maxResults {
		limit = c.maxResults
	}

	u, err := url.Parse(c.baseURL)
	if err != nil || u == nil {
		return nil, ErrInvalidBaseURL
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/v1/search/title"
	q := u.Query()
	if c.apiToken != "" {
		q.Set("token", c.apiToken)
		q.Set("api_token", c.apiToken)
		q.Set("apiToken", c.apiToken)
		q.Set("api_key", c.apiToken)
		q.Set("apikey", c.apiToken)
	}

	// MoviePilot 的参数名可能随版本变化；这里做“多键兼容”，服务端会忽略未知参数。
	q.Set("keyword", keyword)
	q.Set("title", keyword)
	q.Set("name", keyword)
	q.Set("q", keyword)
	q.Set("search", keyword)
	q.Set("search_word", keyword)

	limitStr := strconv.Itoa(limit)
	q.Set("limit", limitStr)
	q.Set("size", limitStr)
	q.Set("count", limitStr)
	q.Set("per_page", limitStr)
	q.Set("page", "1")

	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	// 部分反代/WAF 会对“非浏览器 UA/缺少 Accept”的请求更严格，导致 403。
	// 这里显式带上常见头，降低被误拦概率。
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; emby-bot-new/1.0; +https://example.invalid)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, fmt.Errorf("moviepilot auth failed http=%d", resp.StatusCode)
		case http.StatusNotFound:
			return nil, fmt.Errorf("moviepilot api not found http=%d", resp.StatusCode)
		default:
			return nil, fmt.Errorf("moviepilot http=%d", resp.StatusCode)
		}
	}

	var raw any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	items := extractItems(raw)
	out := make([]Result, 0, min(len(items), limit))
	for _, it := range items {
		if len(out) >= limit {
			break
		}
		m, ok := it.(map[string]any)
		if !ok || m == nil {
			continue
		}
		out = append(out, parseResult(m))
	}
	return out, nil
}

func (c *Client) PushDownload(ctx context.Context, link string) error {
	if c == nil {
		return fmt.Errorf("nil moviepilot client")
	}
	link = strings.TrimSpace(link)
	if link == "" {
		return fmt.Errorf("empty link")
	}

	u, err := url.Parse(c.baseURL)
	if err != nil || u == nil {
		return ErrInvalidBaseURL
	}
	path := strings.TrimSpace(c.pushPath)
	if path == "" {
		path = "/api/v1/download"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	q := u.Query()
	if c.apiToken != "" {
		q.Set("token", c.apiToken)
		q.Set("api_token", c.apiToken)
		q.Set("apiToken", c.apiToken)
		q.Set("api_key", c.apiToken)
		q.Set("apikey", c.apiToken)
	}
	u.RawQuery = q.Encode()

	// 兼容不同版本字段名：服务端通常会忽略未知字段。
	body := map[string]any{
		"url":          link,
		"link":         link,
		"download_url": link,
		"torrent":      link,
		"magnet":       link,
	}
	bs, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(bs))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; emby-bot-new/1.0; +https://example.invalid)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, resp.Body)
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return fmt.Errorf("moviepilot auth failed http=%d", resp.StatusCode)
		case http.StatusNotFound:
			return fmt.Errorf("moviepilot api not found http=%d", resp.StatusCode)
		default:
			return fmt.Errorf("moviepilot http=%d", resp.StatusCode)
		}
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func extractItems(raw any) []any {
	switch v := raw.(type) {
	case []any:
		return v
	case map[string]any:
		// v2 常见结构：{"success":true,"data":{"total":...,"list":[...]}}
		if dv, ok := v["data"]; ok {
			switch data := dv.(type) {
			case []any:
				return data
			case map[string]any:
				for _, key := range []string{"list", "items", "results", "result"} {
					if vv, ok := data[key]; ok {
						if arr, ok := vv.([]any); ok {
							return arr
						}
					}
				}
			}
		}
		for _, key := range []string{"items", "list", "results", "result"} {
			if vv, ok := v[key]; ok {
				if arr, ok := vv.([]any); ok {
					return arr
				}
			}
		}
	}
	return nil
}

func parseResult(m map[string]any) Result {
	getString := func(keys ...string) string {
		for _, k := range keys {
			v, ok := m[k]
			if !ok || v == nil {
				continue
			}
			switch x := v.(type) {
			case string:
				if s := strings.TrimSpace(x); s != "" {
					return s
				}
			}
		}
		return ""
	}
	getStringFrom := func(mm map[string]any, keys ...string) string {
		if mm == nil {
			return ""
		}
		for _, k := range keys {
			v, ok := mm[k]
			if !ok || v == nil {
				continue
			}
			switch x := v.(type) {
			case string:
				if s := strings.TrimSpace(x); s != "" {
					return s
				}
			}
		}
		return ""
	}
	getInt := func(keys ...string) int {
		for _, k := range keys {
			v, ok := m[k]
			if !ok || v == nil {
				continue
			}
			switch x := v.(type) {
			case float64:
				return int(x)
			case int:
				return x
			case int64:
				return int(x)
			case string:
				if n, err := strconv.Atoi(strings.TrimSpace(x)); err == nil {
					return n
				}
			}
		}
		return 0
	}

	pickTitle := func() string {
		keys := []string{
			"title", "name", "resource_title", "resource_name", "media_name",
			"torrent_name", "torrent_title", "torrentName", "torrentTitle",
			"release", "release_name", "display_title",
		}
		if s := getStringFrom(m, keys...); s != "" {
			return s
		}
		// 常见嵌套：torrent_info/resource/info/metadata 等。
		for _, parent := range []string{"torrent_info", "torrentInfo", "resource", "item", "media", "metadata", "info"} {
			if v, ok := m[parent]; ok {
				if mm, ok := v.(map[string]any); ok {
					if s := getStringFrom(mm, keys...); s != "" {
						return s
					}
				}
			}
		}
		// 兜底：扫描所有字段，挑出疑似 title/name 的字符串字段。
		best := ""
		for k, v := range m {
			ks := strings.ToLower(strings.TrimSpace(k))
			if ks == "" {
				continue
			}
			// 避免把 URL/magnet/token/cookie 等误当标题。
			if strings.Contains(ks, "url") || strings.Contains(ks, "link") ||
				strings.Contains(ks, "cookie") || strings.Contains(ks, "passkey") ||
				strings.Contains(ks, "token") || strings.Contains(ks, "apikey") ||
				strings.Contains(ks, "authorization") {
				continue
			}
			if !(strings.Contains(ks, "title") || strings.Contains(ks, "name")) {
				continue
			}
			sv, ok := v.(string)
			if !ok {
				continue
			}
			sv = strings.TrimSpace(sv)
			if sv == "" || len(sv) > 300 {
				continue
			}
			low := strings.ToLower(sv)
			if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "magnet:") || strings.Contains(low, "://") {
				continue
			}
			if len(sv) > len(best) {
				best = sv
			}
		}
		return best
	}

	pickLink := func() string {
		keys := []string{"link", "url", "download_url", "torrent_url", "magnet", "enclosure"}
		if s := getStringFrom(m, keys...); s != "" {
			return s
		}
		for _, parent := range []string{"torrent_info", "torrentInfo", "resource", "item", "info"} {
			if v, ok := m[parent]; ok {
				if mm, ok := v.(map[string]any); ok {
					if s := getStringFrom(mm, keys...); s != "" {
						return s
					}
				}
			}
		}
		return ""
	}

	r := Result{
		Title:    pickTitle(),
		Site:     getString("site", "site_name", "tracker", "source"),
		Size:     getString("size", "size_str", "filesize", "file_size"),
		Seeders:  getInt("seeders", "seeder", "seeds", "seed"),
		Leechers: getInt("leechers", "leecher", "peers", "peer"),
		Link:     pickLink(),
		Raw:      m,
	}
	return r
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
