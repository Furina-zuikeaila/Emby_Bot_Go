package registration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Options struct {
	UsernamePrefix  string
	PasswordLength  int
	SecureSaltBytes int
}

type service struct {
	repo     UserRepository
	settings SettingsRepository
	codes    InviteCodeRepository
	emby     EmbyClient
	opts     Options

	registerMu sync.Mutex
}

func NewService(repo UserRepository, settings SettingsRepository, codes InviteCodeRepository, emby EmbyClient, opts Options) Service {
	if opts.UsernamePrefix == "" {
		opts.UsernamePrefix = "tg"
	}
	if opts.PasswordLength <= 0 {
		opts.PasswordLength = 12
	}
	if opts.PasswordLength < 8 {
		opts.PasswordLength = 8
	}
	if opts.SecureSaltBytes <= 0 {
		opts.SecureSaltBytes = 16
	}
	return &service{repo: repo, settings: settings, codes: codes, emby: emby, opts: opts}
}

func (s *service) Gate(ctx context.Context, telegramID int64, telegramUsername string) (Gate, error) {
	if telegramID == 0 {
		return Gate{}, fmt.Errorf("telegram id is empty")
	}
	if err := s.repo.UpsertTelegram(ctx, telegramID, normalizeTelegramUsername(telegramUsername)); err != nil {
		return Gate{}, err
	}

	account, err := s.repo.FindByTelegramID(ctx, telegramID)
	if err != nil {
		return Gate{}, err
	}
	settings, err := s.settings.Get(ctx)
	if err != nil {
		return Gate{}, err
	}
	now := time.Now()
	currentUsers, err := s.repo.CountRegistered(ctx)
	if err != nil {
		return Gate{}, err
	}

	enabled := settings.Enabled
	if settings.OpenUntil != nil {
		if now.After(*settings.OpenUntil) {
			enabled = false
			settings.Enabled = false
			settings.OpenUntil = nil
			settings.UpdatedAt = now
			_ = s.settings.Save(ctx, settings)
		} else {
			enabled = true
		}
	}

	remaining := 0
	if settings.OpenUntil != nil && now.Before(*settings.OpenUntil) && settings.MaxUsers >= 0 && currentUsers >= settings.MaxUsers {
		enabled = false
		settings.Enabled = false
		settings.OpenUntil = nil
		settings.UpdatedAt = now
		_ = s.settings.Save(ctx, settings)
	}

	if settings.MaxUsers >= 0 {
		remaining = settings.MaxUsers - currentUsers
		if remaining < 0 {
			remaining = 0
		}
	}

	// License/资格过期自动失效（例如 /License 发放后 24h）。
	if account.PendingExpiresAt != nil && now.After(*account.PendingExpiresAt) {
		_ = s.codes.ClearUserReservations(ctx, telegramID)
		_ = s.repo.SetPendingDays(ctx, telegramID, 0)
		_ = s.repo.SetPendingExpiresAt(ctx, telegramID, nil)
		account.PendingDays = 0
		account.PendingExpiresAt = nil
	}

	hasQualification := account.PendingDays > 0 || account.PendingExpiresAt != nil
	if !hasQualification {
		if reserved, err := s.codes.GetReservedByUser(ctx, telegramID); err == nil && reserved != nil {
			hasQualification = true
		}
	}

	return Gate{
		Enabled:          enabled,
		MaxUsers:         settings.MaxUsers,
		CurrentUsers:     currentUsers,
		Remaining:        remaining,
		DefaultDays:      settings.DefaultDays,
		OpenUntil:        settings.OpenUntil,
		HasQualification: hasQualification,
		PendingDays:      account.PendingDays,
	}, nil
}

func (s *service) RedeemInviteCode(ctx context.Context, telegramID int64, telegramUsername string, code string) (int, error) {
	if telegramID == 0 {
		return 0, fmt.Errorf("telegram id is empty")
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return 0, ErrInvalidInviteCode
	}
	if strings.HasPrefix(strings.ToLower(code), strings.ToLower(DefaultRenewCodePrefix)) {
		return 0, ErrInvalidInviteCode
	}

	if err := s.repo.UpsertTelegram(ctx, telegramID, normalizeTelegramUsername(telegramUsername)); err != nil {
		return 0, err
	}

	if err := s.codes.ClearUserReservations(ctx, telegramID); err != nil {
		return 0, err
	}
	if err := s.repo.SetPendingDays(ctx, telegramID, 0); err != nil {
		return 0, err
	}

	reserved, err := s.codes.ReserveForUser(ctx, code, telegramID)
	if err != nil {
		return 0, err
	}
	if reserved == nil {
		return 0, ErrInvalidInviteCode
	}

	if err := s.repo.SetPendingDays(ctx, telegramID, reserved.Days); err != nil {
		return 0, err
	}
	return reserved.Days, nil
}

func (s *service) ReservedInviteCode(ctx context.Context, telegramID int64) (*InviteCode, error) {
	if telegramID == 0 {
		return nil, fmt.Errorf("telegram id is empty")
	}
	if s.codes == nil {
		return nil, fmt.Errorf("invite code repository not initialized")
	}
	return s.codes.GetReservedByUser(ctx, telegramID)
}

func (s *service) Register(ctx context.Context, telegramID int64, telegramUsername string, embyUsername string, secureCode string) (Account, Credentials, error) {
	if telegramID == 0 {
		return Account{}, Credentials{}, fmt.Errorf("telegram id is empty")
	}

	telegramUsername = normalizeTelegramUsername(telegramUsername)
	embyUsername = strings.TrimSpace(embyUsername)
	secureCode = strings.TrimSpace(secureCode)
	if embyUsername == "" {
		embyUsername = GenerateEmbyUsername(s.opts.UsernamePrefix, telegramID, telegramUsername)
	}
	if secureCode == "" || !secureCodeAllowed(secureCode) {
		return Account{}, Credentials{}, ErrInvalidInput
	}

	if err := s.repo.UpsertTelegram(ctx, telegramID, telegramUsername); err != nil {
		return Account{}, Credentials{}, err
	}

	account, err := s.repo.FindByTelegramID(ctx, telegramID)
	if err != nil {
		return Account{}, Credentials{}, err
	}
	if account.EmbyUserID != "" {
		return Account{}, Credentials{}, ErrAlreadyRegistered
	}

	settings, err := s.settings.Get(ctx)
	if err != nil {
		return Account{}, Credentials{}, err
	}
	now := time.Now()
	enabled := settings.Enabled
	if settings.OpenUntil != nil {
		if now.After(*settings.OpenUntil) {
			enabled = false
			settings.Enabled = false
			settings.OpenUntil = nil
			settings.UpdatedAt = now
			_ = s.settings.Save(ctx, settings)
		} else {
			enabled = true
		}
	}

	// License/资格过期自动失效（例如 /License 发放后 24h）。
	if account.PendingExpiresAt != nil && now.After(*account.PendingExpiresAt) {
		_ = s.codes.ClearUserReservations(ctx, telegramID)
		_ = s.repo.SetPendingDays(ctx, telegramID, 0)
		_ = s.repo.SetPendingExpiresAt(ctx, telegramID, nil)
		account.PendingDays = 0
		account.PendingExpiresAt = nil
	}

	hasQualification := account.PendingDays > 0 || account.PendingExpiresAt != nil
	if !hasQualification {
		if reserved, err := s.codes.GetReservedByUser(ctx, telegramID); err == nil && reserved != nil {
			hasQualification = true
		}
	}
	if !hasQualification {
		if !enabled {
			return Account{}, Credentials{}, ErrRegistrationClosed
		}
		if settings.MaxUsers >= 0 {
			currentUsers, err := s.repo.CountRegistered(ctx)
			if err != nil {
				return Account{}, Credentials{}, err
			}
			if currentUsers >= settings.MaxUsers {
				return Account{}, Credentials{}, ErrQuotaFull
			}
		}
	}

	// 关键修复：在创建 Emby 用户前先加锁并二次校验（注册开关/配额），避免“注册失败但已在 Emby 创建账号”。
	var (
		embyUserID string
		password   string
	)

	s.registerMu.Lock()

	// 二次校验（仅对“无资格”走公共注册通道的用户生效）。
	if !hasQualification {
		settingsNow, err := s.settings.Get(ctx)
		if err != nil {
			s.registerMu.Unlock()
			return Account{}, Credentials{}, err
		}
		enabledNow := settingsNow.Enabled
		now2 := time.Now()
		if settingsNow.OpenUntil != nil {
			if now2.After(*settingsNow.OpenUntil) {
				enabledNow = false
				settingsNow.Enabled = false
				settingsNow.OpenUntil = nil
				settingsNow.UpdatedAt = now2
				_ = s.settings.Save(ctx, settingsNow)
			} else {
				enabledNow = true
			}
		}
		if !enabledNow {
			s.registerMu.Unlock()
			return Account{}, Credentials{}, ErrRegistrationClosed
		}
		if settingsNow.MaxUsers >= 0 {
			currentUsers, err := s.repo.CountRegistered(ctx)
			if err != nil {
				s.registerMu.Unlock()
				return Account{}, Credentials{}, err
			}
			if currentUsers >= settingsNow.MaxUsers {
				s.registerMu.Unlock()
				return Account{}, Credentials{}, ErrQuotaFull
			}
		}
		// 使用最新 settings（避免后续 DefaultDays 使用旧值）。
		settings = settingsNow
	}

	password = GeneratePassword(s.opts.PasswordLength)
	var errCreate error
	embyUserID, errCreate = s.emby.CreateUser(ctx, embyUsername, password)
	if errCreate != nil {
		s.registerMu.Unlock()
		return Account{}, Credentials{}, errCreate
	}

	salt, hash, err := hashSecureCode(secureCode, s.opts.SecureSaltBytes)
	if err != nil {
		s.registerMu.Unlock()
		_ = s.emby.DeleteUser(ctx, embyUserID)
		return Account{}, Credentials{}, err
	}

	expiresAt := calculateExpiry(settings.DefaultDays)
	if hasQualification && account.PendingDays > 0 {
		expiresAt = calculateExpiry(account.PendingDays)
	}

	commitErr := s.repo.SetRegistered(ctx, telegramID, telegramUsername, embyUserID, embyUsername, salt, hash, expiresAt)
	if commitErr == nil {
		// 触发定时开关 + 配额自动关闭。
		if settings.OpenUntil != nil && time.Now().Before(*settings.OpenUntil) && settings.MaxUsers >= 0 {
			currentUsers, err := s.repo.CountRegistered(ctx)
			if err == nil && currentUsers >= settings.MaxUsers {
				settings.Enabled = false
				settings.OpenUntil = nil
				settings.UpdatedAt = time.Now()
				_ = s.settings.Save(ctx, settings)
			}
		}
		// 成功后再确认邀请码使用（失败不确认，避免资格被吞）。
		if reserved, err := s.codes.GetReservedByUser(ctx, telegramID); err == nil && reserved != nil {
			_ = s.codes.ConfirmUsage(ctx, reserved.Code, telegramID)
		}
	}

	s.registerMu.Unlock()

	if commitErr != nil {
		_ = s.emby.DeleteUser(ctx, embyUserID)
		return Account{}, Credentials{}, commitErr
	}

	return Account{
		TelegramID:       telegramID,
		TelegramUsername: telegramUsername,
		EmbyUserID:       embyUserID,
		EmbyUsername:     embyUsername,
		Level:            "b",
		PendingDays:      0,
		ExpiresAt:        expiresAt,
		SecureCodeSalt:   salt,
		SecureCodeHash:   hash,
		CreatedAt:        time.Now(),
	}, Credentials{Username: embyUsername, Password: password}, nil
}

func (s *service) Bind(ctx context.Context, telegramID int64, telegramUsername string, embyUsername string, embyPassword string, secureCode string) (Account, error) {
	if telegramID == 0 {
		return Account{}, fmt.Errorf("telegram id is empty")
	}
	telegramUsername = normalizeTelegramUsername(telegramUsername)
	embyUsername = strings.TrimSpace(embyUsername)
	embyPassword = strings.TrimSpace(embyPassword)
	secureCode = strings.TrimSpace(secureCode)

	if embyUsername == "" || embyPassword == "" || secureCode == "" || !secureCodeAllowed(secureCode) {
		return Account{}, ErrInvalidInput
	}

	if err := s.repo.UpsertTelegram(ctx, telegramID, telegramUsername); err != nil {
		return Account{}, err
	}

	account, err := s.repo.FindByTelegramID(ctx, telegramID)
	if err != nil {
		return Account{}, err
	}
	if account.EmbyUserID != "" {
		return Account{}, ErrAlreadyRegistered
	}

	embyUserID, err := s.emby.AuthenticateByName(ctx, embyUsername, embyPassword)
	if err != nil {
		return Account{}, err
	}
	if embyUserID == "" {
		return Account{}, fmt.Errorf("empty emby user id")
	}

	existing, err := s.repo.FindByEmbyUserID(ctx, embyUserID)
	if err == nil && existing != nil && existing.TelegramID != telegramID {
		return Account{}, ErrEmbyAlreadyBound
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Account{}, err
	}

	salt, hash, err := hashSecureCode(secureCode, s.opts.SecureSaltBytes)
	if err != nil {
		return Account{}, err
	}

	if err := s.repo.SetBound(ctx, telegramID, telegramUsername, embyUserID, embyUsername, salt, hash); err != nil {
		return Account{}, err
	}

	return Account{
		TelegramID:       telegramID,
		TelegramUsername: telegramUsername,
		EmbyUserID:       embyUserID,
		EmbyUsername:     embyUsername,
		Level:            "b",
		PendingDays:      0,
		SecureCodeSalt:   salt,
		SecureCodeHash:   hash,
		CreatedAt:        time.Now(),
	}, nil
}

func (s *service) Me(ctx context.Context, telegramID int64) (*Account, error) {
	return s.repo.FindByTelegramID(ctx, telegramID)
}

func normalizeTelegramUsername(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "@")
	return v
}

var secureCodeRE = regexp.MustCompile(`^[A-Za-z0-9]+$`)

func secureCodeAllowed(code string) bool {
	return secureCodeRE.MatchString(code)
}

func hashSecureCode(code string, saltBytes int) (saltHex string, hashHex string, err error) {
	if saltBytes <= 0 {
		saltBytes = 16
	}
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(append(append([]byte{}, salt...), []byte(":"+code)...))
	return hex.EncodeToString(salt), hex.EncodeToString(sum[:]), nil
}

func calculateExpiry(days int) *time.Time {
	if days <= 0 {
		t := time.Now().Add(-1 * time.Second)
		return &t
	}
	t := time.Now().AddDate(0, 0, days)
	return &t
}
