package account

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"emby-bot-new/internal/application/registration"
)

type Options struct {
	RenewCodePrefix string
	PasswordLength  int
}

type service struct {
	repo  UserRepository
	codes InviteCodeRepository
	emby  EmbyClient
	opts  Options
}

var embyStatusRe = regexp.MustCompile(`status=(\d{3})`)

func extractEmbyStatusCode(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	m := embyStatusRe.FindStringSubmatch(err.Error())
	if len(m) != 2 {
		return 0, false
	}
	v, convErr := strconv.Atoi(m[1])
	if convErr != nil || v < 100 {
		return 0, false
	}
	return v, true
}

func isRandomKeyword(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	// 兼容用户输入：random / Random / 随机 / 全角字母等。
	v = strings.ToLower(v)
	v = strings.NewReplacer(
		"ｒ", "r",
		"ａ", "a",
		"ｎ", "n",
		"ｄ", "d",
		"ｏ", "o",
		"ｍ", "m",
	).Replace(v)
	return v == "random" || v == "随机"
}

func NewService(repo UserRepository, codes InviteCodeRepository, emby EmbyClient, opts Options) Service {
	if opts.RenewCodePrefix == "" {
		opts.RenewCodePrefix = registration.DefaultRenewCodePrefix
	}
	if opts.PasswordLength <= 0 {
		opts.PasswordLength = 12
	}
	if opts.PasswordLength < 8 {
		opts.PasswordLength = 8
	}
	return &service{repo: repo, codes: codes, emby: emby, opts: opts}
}

func (s *service) RedeemRenewCode(ctx context.Context, telegramID int64, code string) (*time.Time, int, error) {
	if telegramID == 0 {
		return nil, 0, fmt.Errorf("telegram id is empty")
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, 0, ErrInvalidRenewCode
	}
	if !strings.HasPrefix(strings.ToLower(code), strings.ToLower(s.opts.RenewCodePrefix)) {
		return nil, 0, ErrInvalidRenewCode
	}

	account, err := s.repo.FindByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, 0, err
	}
	if account.EmbyUserID == "" {
		return nil, 0, ErrNotRegistered
	}
	if account.ExpiresAt == nil {
		return nil, 0, ErrUnlimitedAccount
	}

	reserved, err := s.codes.ReserveForUser(ctx, code, telegramID)
	if err != nil {
		return nil, 0, err
	}
	if reserved == nil || reserved.Days <= 0 {
		return nil, 0, ErrInvalidRenewCode
	}

	if err := s.codes.ConfirmUsage(ctx, code, telegramID); err != nil {
		return nil, 0, err
	}

	now := time.Now()
	base := *account.ExpiresAt
	if base.Before(now) {
		base = now
	}
	newExpiresAt := base.AddDate(0, 0, reserved.Days)
	if err := s.repo.UpdateExpiresAt(ctx, telegramID, &newExpiresAt); err != nil {
		return nil, 0, err
	}

	return &newExpiresAt, reserved.Days, nil
}

func (s *service) ResetPassword(ctx context.Context, telegramID int64, secureCode string, newPassword string) (registration.Account, registration.Credentials, error) {
	if telegramID == 0 {
		return registration.Account{}, registration.Credentials{}, fmt.Errorf("telegram id is empty")
	}
	secureCode = strings.TrimSpace(secureCode)
	if secureCode == "" {
		return registration.Account{}, registration.Credentials{}, registration.ErrInvalidInput
	}

	account, err := s.repo.FindByTelegramID(ctx, telegramID)
	if err != nil {
		return registration.Account{}, registration.Credentials{}, err
	}
	if account.EmbyUserID == "" {
		return registration.Account{}, registration.Credentials{}, ErrNotRegistered
	}
	if !registration.VerifySecureCode(account.SecureCodeSalt, account.SecureCodeHash, secureCode) {
		return registration.Account{}, registration.Credentials{}, ErrInvalidSecureCode
	}

	newPassword = strings.TrimSpace(newPassword)
	if newPassword == "" || isRandomKeyword(newPassword) {
		newPassword = registration.GeneratePassword(s.opts.PasswordLength)
	}
	if len(newPassword) < 8 {
		return registration.Account{}, registration.Credentials{}, registration.ErrInvalidInput
	}
	if strings.ContainsAny(newPassword, " \t\r\n") {
		return registration.Account{}, registration.Credentials{}, registration.ErrInvalidInput
	}

	// 自愈：如果 Emby 账号被人工删除，UpdateUserPassword 可能返回 HTTP 404。
	// 这时重新创建同名 Emby 用户（使用新密码），并把新 ID 回写到数据库。
	if err := s.emby.UpdateUserPassword(ctx, account.EmbyUserID, newPassword); err != nil {
		if status, ok := extractEmbyStatusCode(err); ok && status == 404 {
			// 仅允许对已注册用户执行（这里 EmbyUserID != "" 已保证）。
			newID, createErr := s.emby.CreateUser(ctx, strings.TrimSpace(account.EmbyUsername), newPassword)
			if createErr != nil {
				return registration.Account{}, registration.Credentials{}, createErr
			}
			if s.repo != nil {
				if updErr := s.repo.UpdateEmbyUserID(ctx, telegramID, newID); updErr != nil {
					// 尽力回滚：如果 DB 更新失败，尝试删除刚创建的 Emby 用户，避免留下孤儿账号。
					_ = s.emby.DeleteUser(ctx, newID)
					return registration.Account{}, registration.Credentials{}, updErr
				}
			}
			account.EmbyUserID = newID
			return *account, registration.Credentials{Username: account.EmbyUsername, Password: newPassword}, nil
		}
		return registration.Account{}, registration.Credentials{}, err
	}
	return *account, registration.Credentials{Username: account.EmbyUsername, Password: newPassword}, nil
}

func (s *service) DeleteAccount(ctx context.Context, telegramID int64, secureCode string) (registration.Account, error) {
	if telegramID == 0 {
		return registration.Account{}, fmt.Errorf("telegram id is empty")
	}
	secureCode = strings.TrimSpace(secureCode)
	if secureCode == "" {
		return registration.Account{}, registration.ErrInvalidInput
	}

	account, err := s.repo.FindByTelegramID(ctx, telegramID)
	if err != nil {
		return registration.Account{}, err
	}
	if account.EmbyUserID == "" {
		return registration.Account{}, ErrNotRegistered
	}
	if !registration.VerifySecureCode(account.SecureCodeSalt, account.SecureCodeHash, secureCode) {
		return registration.Account{}, ErrInvalidSecureCode
	}

	_ = s.codes.ClearUserReservations(ctx, telegramID)

	deleted, err := s.repo.DeleteByTelegramID(ctx, telegramID)
	if err != nil {
		return registration.Account{}, err
	}
	_ = s.emby.DeleteUser(ctx, deleted.EmbyUserID)
	return *deleted, nil
}

func (s *service) GetActiveSessionsCount(ctx context.Context) (int, error) {
	if s.emby == nil {
		return 0, fmt.Errorf("emby client is nil")
	}
	return s.emby.GetActiveSessionsCount(ctx)
}

func (s *service) ListLibraries(ctx context.Context, telegramID int64) ([]Library, error) {
	account, err := s.repo.FindByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	if account.EmbyUserID == "" {
		return nil, ErrNotRegistered
	}
	if s.emby == nil {
		return nil, fmt.Errorf("emby client is nil")
	}

	libs, err := s.emby.GetLibraries(ctx)
	if err != nil {
		return nil, err
	}
	user, err := s.emby.GetUser(ctx, account.EmbyUserID)
	if err != nil {
		return nil, err
	}

	policy, _ := user["Policy"].(map[string]any)
	enabled := enabledFoldersFromPolicy(libs, policy)

	out := make([]Library, 0, len(libs))
	for _, lib := range libs {
		id := libID(lib)
		name := libName(lib)
		if id == "" || name == "" {
			continue
		}
		out = append(out, Library{
			ID:      id,
			Name:    name,
			Enabled: enabled[id],
		})
	}
	return out, nil
}

func (s *service) ToggleLibrary(ctx context.Context, telegramID int64, libraryID string) ([]Library, error) {
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return nil, registration.ErrInvalidInput
	}

	account, err := s.repo.FindByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	if account.EmbyUserID == "" {
		return nil, ErrNotRegistered
	}
	if s.emby == nil {
		return nil, fmt.Errorf("emby client is nil")
	}

	libs, err := s.emby.GetLibraries(ctx)
	if err != nil {
		return nil, err
	}
	user, err := s.emby.GetUser(ctx, account.EmbyUserID)
	if err != nil {
		return nil, err
	}

	policy, _ := user["Policy"].(map[string]any)
	if policy == nil {
		policy = map[string]any{}
	}

	enabledSet := enabledFoldersFromPolicy(libs, policy)
	if enabledSet[libraryID] {
		delete(enabledSet, libraryID)
	} else {
		enabledSet[libraryID] = true
	}

	enabledFolders := make([]string, 0, len(enabledSet))
	for id := range enabledSet {
		if strings.TrimSpace(id) == "" {
			continue
		}
		enabledFolders = append(enabledFolders, id)
	}

	policy["EnableAllFolders"] = false
	policy["EnabledFolders"] = enabledFolders

	if err := s.emby.UpdateUserPolicy(ctx, account.EmbyUserID, policy); err != nil {
		return nil, err
	}

	return s.ListLibraries(ctx, telegramID)
}

func (s *service) ListSessions(ctx context.Context, telegramID int64) ([]Session, error) {
	account, err := s.repo.FindByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	if account.EmbyUserID == "" {
		return nil, ErrNotRegistered
	}
	if s.emby == nil {
		return nil, fmt.Errorf("emby client is nil")
	}

	sessions, err := s.emby.GetSessions(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]Session, 0, len(sessions))
	for _, sess := range sessions {
		userID, _ := sess["UserId"].(string)
		if strings.TrimSpace(userID) != account.EmbyUserID {
			continue
		}

		deviceName, _ := sess["DeviceName"].(string)
		clientName, _ := sess["Client"].(string)
		remote, _ := sess["RemoteEndPoint"].(string)

		out = append(out, Session{
			DeviceName:     strings.TrimSpace(deviceName),
			Client:         strings.TrimSpace(clientName),
			RemoteEndPoint: strings.TrimSpace(remote),
		})
	}
	return out, nil
}

func (s *service) PlaybackHistory(ctx context.Context, telegramID int64, days int, limit int) ([]HistoryItem, error) {
	account, err := s.repo.FindByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	if account.EmbyUserID == "" {
		return nil, ErrNotRegistered
	}
	if s.emby == nil {
		return nil, fmt.Errorf("emby client is nil")
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	var cutoff time.Time
	if days > 0 {
		cutoff = time.Now().AddDate(0, 0, -days)
	}

	// 优先使用 Emby “活动日志”（对应后台的“活动状况”），它记录了开始/停止播放，时间更准确。
	// 注意：/System/ActivityLog/Entries 通常需要管理员 API Key。
	out := make([]HistoryItem, 0, limit)
	entries, err := s.emby.GetActivityLogEntries(ctx, 0, 200, func() *time.Time {
		if cutoff.IsZero() {
			return nil
		}
		return &cutoff
	}())
	if err == nil && len(entries) > 0 {
		out = append(out, historyFromActivityLog(entries, account.EmbyUsername, cutoff, limit)...)
	}

	// 回退：若活动日志不可用或没有匹配记录，则使用 PlayedItems/Items（兼容性更好，但精度可能略差）。
	if len(out) == 0 {
		rawItems, err := s.emby.GetPlaybackHistory(ctx, account.EmbyUserID, limit*5)
		if err != nil {
			return nil, err
		}
		for _, it := range rawItems {
			name := strings.TrimSpace(stringFromMap(it, "Name"))
			if name == "" {
				continue
			}
			itemType := strings.TrimSpace(stringFromMap(it, "Type"))
			seriesName := strings.TrimSpace(stringFromMap(it, "SeriesName"))

			var lastPlayedAt *time.Time
			if ud, ok := it["UserData"].(map[string]any); ok {
				if t := parseTimeRFC3339(stringFromMap(ud, "LastPlayedDate")); t != nil {
					lastPlayedAt = t
				}
			}
			// “最近 N 天”查询：如果没有 LastPlayedDate，无法判断是否在 N 天内，直接跳过避免误报。
			if !cutoff.IsZero() {
				if lastPlayedAt == nil || lastPlayedAt.Before(cutoff) {
					continue
				}
			}

			out = append(out, HistoryItem{
				Name:         name,
				Type:         itemType,
				SeriesName:   seriesName,
				LastPlayedAt: lastPlayedAt,
			})
		}
	}

	// 防御性排序：不同 Emby 版本/接口返回顺序可能不稳定，按 LastPlayedDate 倒序展示更符合直觉。
	sort.Slice(out, func(i, j int) bool {
		a := out[i].LastPlayedAt
		b := out[j].LastPlayedAt
		if a == nil && b == nil {
			return out[i].Name < out[j].Name
		}
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		return a.After(*b)
	})
	if len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}

func historyFromActivityLog(entries []ActivityLogEntry, username string, cutoff time.Time, limit int) []HistoryItem {
	username = strings.TrimSpace(username)
	usernameLower := strings.ToLower(username)

	stopItems := make([]HistoryItem, 0, limit)
	startItems := make([]HistoryItem, 0, limit)

	for _, e := range entries {
		if e.Date == nil {
			continue
		}
		if !cutoff.IsZero() && e.Date.Before(cutoff) {
			continue
		}

		// 优先用 Name 匹配用户名（活动日志里有时 UserId 不是 GUID，不可靠）。
		nameLower := strings.ToLower(strings.TrimSpace(e.Name))
		if usernameLower != "" && !strings.Contains(nameLower, usernameLower) {
			continue
		}

		isStop := strings.Contains(nameLower, "已停止播放") || strings.Contains(nameLower, "停止播放") || strings.Contains(nameLower, "stopped playing")
		isStart := strings.Contains(nameLower, "已开始播放") || strings.Contains(nameLower, "开始播放") || strings.Contains(nameLower, "started playing") || strings.Contains(nameLower, "is playing")
		if !isStop && !isStart && !strings.Contains(strings.ToLower(strings.TrimSpace(e.Type)), "playback") {
			continue
		}

		title := extractPlaybackTitle(e.Name)
		if title == "" {
			continue
		}
		item := HistoryItem{
			Name:         title,
			Type:         "",
			SeriesName:   "",
			LastPlayedAt: e.Date,
		}
		if isStop {
			stopItems = append(stopItems, item)
		} else {
			startItems = append(startItems, item)
		}
	}

	// 优先返回“停止播放”记录，避免“开始/停止”重复；若没有则回退到“开始播放”。
	out := stopItems
	if len(out) == 0 {
		out = startItems
	}
	if len(out) == 0 {
		return nil
	}

	sort.Slice(out, func(i, j int) bool {
		a := out[i].LastPlayedAt
		b := out[j].LastPlayedAt
		if a == nil && b == nil {
			return out[i].Name < out[j].Name
		}
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		return a.After(*b)
	})

	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func extractPlaybackTitle(name string) string {
	raw := strings.TrimSpace(name)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	// 中英文关键短语尽量覆盖
	phrases := []string{
		"已停止播放", "停止播放", "已开始播放", "开始播放",
		"stopped playing", "started playing", "is playing",
	}
	for _, p := range phrases {
		idx := strings.Index(lower, strings.ToLower(p))
		if idx < 0 {
			continue
		}
		// 取短语后面的内容作为媒体标题
		after := strings.TrimSpace(raw[idx+len(p):])
		after = strings.TrimLeft(after, " :：-–—")
		return after
	}
	return raw
}

func enabledFoldersFromPolicy(libs []map[string]any, policy map[string]any) map[string]bool {
	enabled := make(map[string]bool)
	if policy == nil {
		return enabled
	}

	if enableAll, ok := policy["EnableAllFolders"].(bool); ok && enableAll {
		for _, lib := range libs {
			id := libID(lib)
			if id != "" {
				enabled[id] = true
			}
		}
		return enabled
	}

	switch v := policy["EnabledFolders"].(type) {
	case []any:
		for _, it := range v {
			id, _ := it.(string)
			id = strings.TrimSpace(id)
			if id != "" {
				enabled[id] = true
			}
		}
	case []string:
		for _, id := range v {
			id = strings.TrimSpace(id)
			if id != "" {
				enabled[id] = true
			}
		}
	}

	return enabled
}

func libID(m map[string]any) string {
	if m == nil {
		return ""
	}
	if v, ok := m["Id"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := m["ItemId"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return ""
}

func libName(m map[string]any) string {
	if m == nil {
		return ""
	}
	if v, ok := m["Name"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func stringFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func parseTimeRFC3339(v string) *time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, v); err == nil {
			return &t
		}
	}
	return nil
}
