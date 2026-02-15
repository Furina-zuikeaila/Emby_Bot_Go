package registration

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type AdminOptions struct {
	CodePrefix      string
	RenewCodePrefix string
}

type adminService struct {
	repo     UserRepository
	settings SettingsRepository
	codes    InviteCodeRepository

	opts AdminOptions
}

func NewAdminService(repo UserRepository, settings SettingsRepository, codes InviteCodeRepository, opts AdminOptions) AdminService {
	if opts.CodePrefix == "" {
		opts.CodePrefix = ""
	}
	if opts.RenewCodePrefix == "" {
		opts.RenewCodePrefix = DefaultRenewCodePrefix
	}
	return &adminService{repo: repo, settings: settings, codes: codes, opts: opts}
}

func (s *adminService) GetSettings(ctx context.Context) (Settings, error) {
	return s.settings.Get(ctx)
}

func (s *adminService) SetEnabled(ctx context.Context, enabled bool) (Settings, error) {
	settings, err := s.settings.Get(ctx)
	if err != nil {
		return Settings{}, err
	}
	settings.Enabled = enabled
	settings.OpenUntil = nil
	settings.UpdatedAt = time.Now()
	if err := s.settings.Save(ctx, settings); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func (s *adminService) SetTimingMinutes(ctx context.Context, minutes int) (Settings, error) {
	settings, err := s.settings.Get(ctx)
	if err != nil {
		return Settings{}, err
	}
	if minutes <= 0 {
		settings.OpenUntil = nil
		settings.UpdatedAt = time.Now()
		if err := s.settings.Save(ctx, settings); err != nil {
			return Settings{}, err
		}
		return settings, nil
	}
	t := time.Now().Add(time.Duration(minutes) * time.Minute)
	settings.Enabled = true
	settings.OpenUntil = &t
	settings.UpdatedAt = time.Now()
	if err := s.settings.Save(ctx, settings); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func (s *adminService) SetMaxUsers(ctx context.Context, maxUsers int) (Settings, error) {
	if maxUsers < 0 {
		return Settings{}, ErrInvalidInput
	}
	settings, err := s.settings.Get(ctx)
	if err != nil {
		return Settings{}, err
	}
	settings.MaxUsers = maxUsers
	settings.UpdatedAt = time.Now()
	if err := s.settings.Save(ctx, settings); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func (s *adminService) SetDefaultDays(ctx context.Context, days int) (Settings, error) {
	if days < 0 {
		return Settings{}, ErrInvalidInput
	}
	settings, err := s.settings.Get(ctx)
	if err != nil {
		return Settings{}, err
	}
	settings.DefaultDays = days
	settings.UpdatedAt = time.Now()
	if err := s.settings.Save(ctx, settings); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func (s *adminService) SetServiceMode(ctx context.Context, mode string) (Settings, error) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	switch mode {
	case "", "private", "public", "charity":
	default:
		return Settings{}, ErrInvalidInput
	}

	settings, err := s.settings.Get(ctx)
	if err != nil {
		return Settings{}, err
	}
	settings.ServiceMode = mode
	settings.UpdatedAt = time.Now()
	if err := s.settings.Save(ctx, settings); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func (s *adminService) SetInactiveDuration(ctx context.Context, duration string) (Settings, error) {
	duration = strings.TrimSpace(duration)
	// 允许空值（表示回退到环境变量默认值）
	if duration != "" {
		if _, err := parseFlexibleDuration(duration); err != nil {
			return Settings{}, ErrInvalidInput
		}
	}

	settings, err := s.settings.Get(ctx)
	if err != nil {
		return Settings{}, err
	}
	settings.InactiveDuration = duration
	settings.UpdatedAt = time.Now()
	if err := s.settings.Save(ctx, settings); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func parseFlexibleDuration(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	// 支持 30d 这种“天”单位（Go 原生不支持 d）
	if strings.HasSuffix(strings.ToLower(raw), "d") {
		v := strings.TrimSpace(strings.TrimSuffix(strings.ToLower(raw), "d"))
		days, err := strconv.Atoi(v)
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(raw)
}

func (s *adminService) CreateCodes(ctx context.Context, creatorTelegramID int64, days int, count int, isRenew bool) ([]string, error) {
	if creatorTelegramID == 0 || days < 0 || count <= 0 {
		return nil, ErrInvalidInput
	}
	if count > 100 {
		return nil, ErrInvalidInput
	}
	prefix := s.opts.CodePrefix
	if isRenew {
		prefix = s.opts.RenewCodePrefix
	}
	return s.codes.CreateBatch(ctx, creatorTelegramID, days, count, prefix)
}

func (s *adminService) Stats(ctx context.Context) (CodeStats, error) {
	return s.codes.Stats(ctx)
}

func (s *adminService) ExportUnusedLinks(ctx context.Context, creatorTelegramID int64, botUsername string) ([]string, error) {
	if creatorTelegramID == 0 {
		return nil, ErrInvalidInput
	}
	botUsername = strings.TrimSpace(strings.TrimPrefix(botUsername, "@"))
	if botUsername == "" {
		return nil, ErrInvalidInput
	}

	codes, err := s.codes.ListUnusedByCreator(ctx, creatorTelegramID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(codes))
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		out = append(out, fmt.Sprintf("https://t.me/%s?start=%s", botUsername, code))
	}
	return out, nil
}

func (s *adminService) WipeUnused(ctx context.Context, creatorTelegramID int64) (int64, error) {
	if creatorTelegramID == 0 {
		return 0, ErrInvalidInput
	}
	return s.codes.DeleteAllUnusedByCreator(ctx, creatorTelegramID)
}

func (s *adminService) GrantQualification(ctx context.Context, adminTelegramID int64, targetTelegramID int64, days int) (string, error) {
	if adminTelegramID == 0 || targetTelegramID == 0 || targetTelegramID == adminTelegramID {
		return "", ErrInvalidInput
	}
	if days < 0 {
		return "", ErrInvalidInput
	}

	if err := s.codes.ClearUserReservations(ctx, targetTelegramID); err != nil {
		return "", err
	}
	if err := s.repo.SetPendingDays(ctx, targetTelegramID, 0); err != nil {
		return "", err
	}

	created, err := s.codes.CreateBatch(ctx, adminTelegramID, days, 1, s.opts.CodePrefix)
	if err != nil {
		return "", err
	}
	if len(created) == 0 || strings.TrimSpace(created[0]) == "" {
		return "", fmt.Errorf("failed to create code")
	}
	code := strings.TrimSpace(created[0])

	reserved, err := s.codes.ReserveForUser(ctx, code, targetTelegramID)
	if err != nil {
		return "", err
	}
	if reserved == nil {
		return "", fmt.Errorf("failed to reserve code")
	}
	if err := s.repo.SetPendingDays(ctx, targetTelegramID, reserved.Days); err != nil {
		return "", err
	}
	return code, nil
}

func (s *adminService) SetWhitelist(ctx context.Context, targetTelegramID int64, telegramUsername string, enabled bool) error {
	if targetTelegramID == 0 {
		return ErrInvalidInput
	}
	return s.repo.SetWhitelist(ctx, targetTelegramID, telegramUsername, enabled)
}

func (s *adminService) GrantLicense(ctx context.Context, adminTelegramID int64, targetTelegramID int64) (time.Time, error) {
	if adminTelegramID == 0 || targetTelegramID == 0 || targetTelegramID == adminTelegramID {
		return time.Time{}, ErrInvalidInput
	}

	// 清理任何旧的邀请码锁定/资格天数，避免叠加。
	_ = s.codes.ClearUserReservations(ctx, targetTelegramID)
	_ = s.repo.SetPendingDays(ctx, targetTelegramID, 0)

	expiresAt := time.Now().Add(24 * time.Hour)
	if err := s.repo.SetPendingExpiresAt(ctx, targetTelegramID, &expiresAt); err != nil {
		return time.Time{}, err
	}
	return expiresAt, nil
}

func (s *adminService) SetGroupAdmins(ctx context.Context, adminTelegramID int64, ids []int64) (Settings, error) {
	if adminTelegramID == 0 {
		return Settings{}, ErrInvalidInput
	}
	// 过滤/去重
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if id == adminTelegramID {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}

	settings, err := s.settings.Get(ctx)
	if err != nil {
		return Settings{}, err
	}
	settings.GroupAdminIDs = out
	settings.UpdatedAt = time.Now()
	if err := s.settings.Save(ctx, settings); err != nil {
		return Settings{}, err
	}
	return settings, nil
}
