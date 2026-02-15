package emby

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	accountapp "emby-bot-new/internal/application/account"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client

	// simultaneousStreamLimit 限制单账号同时播放数（0 表示不限制）。
	simultaneousStreamLimit int
}

func New(baseURL, apiKey string, insecureSkipVerify bool, simultaneousStreamLimit int) (*Client, error) {
	if baseURL == "" || apiKey == "" {
		return nil, fmt.Errorf("emby baseURL/apiKey is empty")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid emby base url: %w", err)
	}
	if strings.TrimSpace(u.Scheme) == "" {
		return nil, fmt.Errorf("invalid emby base url: missing scheme")
	}

	transport := &http.Transport{}
	// 仅在 https 场景设置 TLS 参数；对于 http://IP:port，TLS 配置无意义，保持 unset 更清晰/更安全。
	if strings.EqualFold(u.Scheme, "https") {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: insecureSkipVerify, MinVersion: tls.VersionTLS12}
	}

	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		http: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
		simultaneousStreamLimit: maxInt(0, simultaneousStreamLimit),
	}, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type createUserRequest struct {
	Name string `json:"Name"`
}

type createUserResponse struct {
	ID string `json:"Id"`
}

type passwordPolicy struct {
	ID    string `json:"Id"`
	NewPw string `json:"NewPw"`
}

type authenticateByNameRequest struct {
	Username string `json:"Username"`
	Pw       string `json:"Pw"`
}

func (c *Client) CreateUser(ctx context.Context, username, password string) (string, error) {
	raw, err := c.request(ctx, http.MethodPost, "/Users/New", createUserRequest{Name: username})
	if err != nil {
		return "", err
	}
	var resp createUserResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("decode create user response: %w", err)
	}
	if resp.ID == "" {
		return "", fmt.Errorf("create user: empty id")
	}

	_, err = c.request(ctx, http.MethodPost, fmt.Sprintf("/Users/%s/Password", resp.ID), passwordPolicy{ID: resp.ID, NewPw: password})
	if err != nil {
		return "", err
	}

	// 创建账号后立即下发账号策略（播放/下载/字幕等权限 + 同时播放限制）。
	// 如果下发失败，为避免留下“未受控账号”，这里会尝试回滚删除该用户。
	if err := c.applyUserPolicyDefaults(ctx, resp.ID); err != nil {
		_ = c.DeleteUser(ctx, resp.ID)
		return "", err
	}
	return resp.ID, nil
}

func setPolicyBool(policy map[string]any, key string, value bool) {
	if policy == nil || strings.TrimSpace(key) == "" {
		return
	}
	policy[key] = value
}

func setPolicyInt(policy map[string]any, key string, value int) {
	if policy == nil || strings.TrimSpace(key) == "" {
		return
	}
	policy[key] = value
}

func (c *Client) applyUserPolicyDefaults(ctx context.Context, embyUserID string) error {
	if strings.TrimSpace(embyUserID) == "" {
		return fmt.Errorf("emby user id is empty")
	}

	user, err := c.GetUser(ctx, embyUserID)
	if err != nil {
		return err
	}

	policy, _ := user["Policy"].(map[string]any)
	if policy == nil {
		policy = map[string]any{}
	}

	// 账号创建策略（对应 Emby 用户的“策略/权限”开关）。
	//
	// 说明：这里以“尽量明确、默认更安全”为原则；若需要不同策略，建议在此处统一调整。
	//
	// 播放：允许播放，但禁用各类转码/封装（如有需要再在 Emby 后台对特定账号放开）。
	setPolicyBool(policy, "EnableMediaPlayback", true)
	setPolicyBool(policy, "EnableAudioPlaybackTranscoding", false)
	setPolicyBool(policy, "EnableVideoPlaybackTranscoding", false)
	setPolicyBool(policy, "EnablePlaybackRemuxing", false)

	// 下载：允许下载（含需要转码的下载）。
	// 不同 Emby 版本字段可能略有差异，这里按常见字段写入；未知字段通常会被忽略。
	setPolicyBool(policy, "EnableContentDownloading", true)
	setPolicyBool(policy, "EnableContentDownloadingTranscoding", true)
	setPolicyBool(policy, "EnableSyncTranscoding", true)

	// 字幕：允许下载字幕；不允许删除现有字幕/相机上传/媒体转换/分享/社交分享。
	setPolicyBool(policy, "EnableSubtitleDownloading", true)
	setPolicyBool(policy, "EnableSubtitleManagement", false)
	setPolicyBool(policy, "EnablePhotoUpload", false)
	setPolicyBool(policy, "EnableMediaConversion", false)
	setPolicyBool(policy, "EnableSharing", false)
	setPolicyBool(policy, "EnablePublicSharing", false)

	// 账号：允许用户修改头像/密码。
	setPolicyBool(policy, "EnableUserPreferenceAccess", true)
	setPolicyBool(policy, "EnableUserPassword", true)

	// 账号状态：确保未禁用。
	setPolicyBool(policy, "IsDisabled", false)

	// 登录页隐藏（减少登录页账号列表暴露）。
	setPolicyBool(policy, "HideFromLoginScreen", true)
	setPolicyBool(policy, "HideFromLoginScreenForLocalUsers", true)
	setPolicyBool(policy, "HideFromLoginScreenForRemoteUsers", true)
	setPolicyBool(policy, "HideFromLoginScreenForNewDevices", true)

	// 同时播放限制：
	// 1 = 单账号同一时间仅允许 1 路播放（禁止多设备同时播放）。
	if c.simultaneousStreamLimit > 0 {
		setPolicyInt(policy, "SimultaneousStreamLimit", c.simultaneousStreamLimit)
	}
	return c.UpdateUserPolicy(ctx, embyUserID, policy)
}

func (c *Client) DeleteUser(ctx context.Context, embyUserID string) error {
	if embyUserID == "" {
		return nil
	}
	_, err := c.request(ctx, http.MethodDelete, fmt.Sprintf("/Users/%s", embyUserID), nil)
	return err
}

func (c *Client) UpdateUserPassword(ctx context.Context, embyUserID, newPassword string) error {
	if embyUserID == "" || newPassword == "" {
		return fmt.Errorf("emby user id or new password is empty")
	}
	_, err := c.request(ctx, http.MethodPost, fmt.Sprintf("/Users/%s/Password", embyUserID), passwordPolicy{
		ID:    embyUserID,
		NewPw: newPassword,
	})
	return err
}

func (c *Client) AuthenticateByName(ctx context.Context, username, password string) (string, error) {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" || password == "" {
		return "", fmt.Errorf("username or password is empty")
	}

	raw, err := c.request(ctx, http.MethodPost, "/Users/AuthenticateByName", authenticateByNameRequest{
		Username: username,
		Pw:       password,
	})
	if err != nil {
		return "", err
	}

	var resp struct {
		User struct {
			ID string `json:"Id"`
		} `json:"User"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("decode authenticate response: %w", err)
	}
	if resp.User.ID == "" {
		return "", fmt.Errorf("authenticate: empty user id")
	}
	return resp.User.ID, nil
}

func (c *Client) GetUser(ctx context.Context, embyUserID string) (map[string]any, error) {
	embyUserID = strings.TrimSpace(embyUserID)
	if embyUserID == "" {
		return nil, fmt.Errorf("emby user id is empty")
	}

	raw, err := c.request(ctx, http.MethodGet, fmt.Sprintf("/Users/%s", embyUserID), nil)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode user: %w", err)
	}
	return out, nil
}

func (c *Client) UpdateUserPolicy(ctx context.Context, embyUserID string, policy map[string]any) error {
	embyUserID = strings.TrimSpace(embyUserID)
	if embyUserID == "" {
		return fmt.Errorf("emby user id is empty")
	}
	if policy == nil {
		return fmt.Errorf("policy is nil")
	}
	_, err := c.request(ctx, http.MethodPost, fmt.Sprintf("/Users/%s/Policy", embyUserID), policy)
	return err
}

func (c *Client) GetLibraries(ctx context.Context) ([]map[string]any, error) {
	raw, err := c.request(ctx, http.MethodGet, "/Library/VirtualFolders", nil)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode libraries: %w", err)
	}
	return out, nil
}

func (c *Client) GetSessions(ctx context.Context) ([]map[string]any, error) {
	raw, err := c.request(ctx, http.MethodGet, "/Sessions", nil)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode sessions: %w", err)
	}
	return out, nil
}

func (c *Client) GetActiveSessionsCount(ctx context.Context) (int, error) {
	sessions, err := c.GetSessions(ctx)
	if err != nil {
		return 0, err
	}

	unique := make(map[string]struct{})
	for _, s := range sessions {
		if s == nil {
			continue
		}
		if _, ok := s["NowPlayingItem"]; !ok {
			continue
		}
		userID, _ := s["UserId"].(string)
		userID = strings.TrimSpace(userID)
		if userID == "" {
			continue
		}
		unique[userID] = struct{}{}
	}
	return len(unique), nil
}

func (c *Client) GetPlaybackHistory(ctx context.Context, userID string, limit int) ([]map[string]any, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("user id is empty")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// Emby 不同版本/插件下观影记录 API 可能不同：
	// - 推荐：/Users/{id}/Items + Filters=IsPlayed + SortBy=DatePlayed（更稳定，且可显式排序）
	// - 兼容：/Users/{id}/PlayedItems（部分版本不存在，会 404）
	//
	// 为保证“最近一次观看”准确：
	// - 强制 Fields=UserData（用于读取 LastPlayedDate）
	// - 强制 SortBy=DatePlayed&SortOrder=Descending
	itemsEndpoint := fmt.Sprintf(
		"/Users/%s/Items?Recursive=true&IncludeItemTypes=Movie,Series,Episode&SortBy=DatePlayed&SortOrder=Descending&Filters=IsPlayed&Fields=UserData&Limit=%d",
		url.PathEscape(userID),
		limit,
	)
	playedEndpoint := fmt.Sprintf(
		"/Users/%s/PlayedItems?IncludeItemTypes=Movie,Series,Episode&SortBy=DatePlayed&SortOrder=Descending&Fields=UserData&Limit=%d",
		url.PathEscape(userID),
		limit,
	)

	raw, err := c.request(ctx, http.MethodGet, itemsEndpoint, nil)
	if err != nil {
		// 回退：PlayedItems（若仍失败则返回原始错误）
		if raw2, err2 := c.request(ctx, http.MethodGet, playedEndpoint, nil); err2 == nil {
			raw = raw2
		} else {
			return nil, err
		}
	}

	var resp struct {
		Items []map[string]any `json:"Items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode playback history: %w", err)
	}
	return resp.Items, nil
}

func (c *Client) GetActivityLogEntries(ctx context.Context, startIndex, limit int, minDate *time.Time) ([]accountapp.ActivityLogEntry, error) {
	if startIndex < 0 {
		startIndex = 0
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	params := url.Values{}
	params.Set("StartIndex", strconv.Itoa(startIndex))
	params.Set("Limit", strconv.Itoa(limit))
	if minDate != nil && !minDate.IsZero() {
		// Emby 接受 RFC3339 时间
		params.Set("MinDate", minDate.UTC().Format(time.RFC3339))
	}

	endpoint := "/System/ActivityLog/Entries"
	if q := params.Encode(); q != "" {
		endpoint += "?" + q
	}
	raw, err := c.request(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Items []struct {
			ID            int64  `json:"Id"`
			Name          string `json:"Name"`
			Overview      string `json:"Overview"`
			ShortOverview string `json:"ShortOverview"`
			Type          string `json:"Type"`
			ItemID        string `json:"ItemId"`
			Date          string `json:"Date"`
			UserID        string `json:"UserId"`
			Severity      string `json:"Severity"`
		} `json:"Items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode activity log: %w", err)
	}

	out := make([]accountapp.ActivityLogEntry, 0, len(resp.Items))
	for _, it := range resp.Items {
		var t *time.Time
		if strings.TrimSpace(it.Date) != "" {
			raw := strings.TrimSpace(it.Date)
			if tt, err := time.Parse(time.RFC3339, raw); err == nil {
				t = &tt
			} else if tt, err := time.Parse(time.RFC3339Nano, raw); err == nil {
				t = &tt
			}
		}
		out = append(out, accountapp.ActivityLogEntry{
			ID:            it.ID,
			Name:          strings.TrimSpace(it.Name),
			Overview:      strings.TrimSpace(it.Overview),
			ShortOverview: strings.TrimSpace(it.ShortOverview),
			Type:          strings.TrimSpace(it.Type),
			ItemID:        strings.TrimSpace(it.ItemID),
			Date:          t,
			UserID:        strings.TrimSpace(it.UserID),
			Severity:      strings.TrimSpace(it.Severity),
		})
	}
	return out, nil
}

func (c *Client) GetActivityLogEntriesRaw(ctx context.Context, startIndex, limit int, minDate *time.Time) ([]map[string]any, error) {
	if startIndex < 0 {
		startIndex = 0
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	params := url.Values{}
	params.Set("StartIndex", strconv.Itoa(startIndex))
	params.Set("Limit", strconv.Itoa(limit))
	if minDate != nil && !minDate.IsZero() {
		params.Set("MinDate", minDate.UTC().Format(time.RFC3339))
	}

	endpoint := "/System/ActivityLog/Entries"
	if q := params.Encode(); q != "" {
		endpoint += "?" + q
	}
	raw, err := c.request(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Items []map[string]any `json:"Items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode activity log: %w", err)
	}
	return resp.Items, nil
}

func (c *Client) GetAllDevices(ctx context.Context) ([]map[string]any, error) {
	raw, err := c.request(ctx, http.MethodGet, "/Devices", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Items []map[string]any `json:"Items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode devices: %w", err)
	}
	return resp.Items, nil
}

func (c *Client) request(ctx context.Context, method, endpoint string, body any) ([]byte, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint: %w", err)
	}

	fullURL, err := url.JoinPath(c.baseURL, u.Path)
	if err != nil {
		return nil, fmt.Errorf("join url: %w", err)
	}
	if u.RawQuery != "" {
		fullURL += "?" + u.RawQuery
	}

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode json body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Emby-Token", c.apiKey)
	req.Header.Set("X-Emby-Authorization", fmt.Sprintf(`MediaBrowser Client="EmbyBot", Device="Server", DeviceId="EmbyBot", Version="2.0.0", Token="%s"`, c.apiKey))
	req.Header.Set("User-Agent", "EmbyBot/2.0.0")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 重要：错误信息中不要包含 endpoint/完整 URL/响应 body，避免在日志或用户消息里泄露敏感信息。
		// 仅保留 status=XXX，便于下游做稳定分类（例如 userFriendlyError）。
		return nil, fmt.Errorf("emby api failed: status=%d", resp.StatusCode)
	}
	return raw, nil
}
