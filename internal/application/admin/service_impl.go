package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"emby-bot-new/internal/application/registration"
)

type Options struct {
	UsernamePrefix string
	PasswordLength int
}

var ErrUserNotRegistered = errors.New("user not registered")

type service struct {
	repo UserRepository
	emby EmbyClient
	opts Options
}

func NewService(repo UserRepository, emby EmbyClient, opts Options) Service {
	if opts.UsernamePrefix == "" {
		opts.UsernamePrefix = "tg"
	}
	if opts.PasswordLength <= 0 {
		opts.PasswordLength = 12
	}
	if opts.PasswordLength < 8 {
		opts.PasswordLength = 8
	}
	return &service{repo: repo, emby: emby, opts: opts}
}

func (s *service) Stats(ctx context.Context) (Stats, error) {
	botUsers, err := s.repo.CountAll(ctx)
	if err != nil {
		return Stats{}, err
	}
	registered, err := s.repo.CountRegistered(ctx)
	if err != nil {
		return Stats{}, err
	}
	whitelist, err := s.repo.CountWhitelist(ctx)
	if err != nil {
		return Stats{}, err
	}
	return Stats{
		BotUsers:        botUsers,
		RegisteredUsers: registered,
		WhitelistUsers:  whitelist,
	}, nil
}

func (s *service) ListUsers(ctx context.Context, limit, offset int) ([]registration.Account, error) {
	// 用户管理默认仅展示“已注册用户”（过滤未注册的 tg_users）。
	return s.repo.ListRegistered(ctx, limit, offset)
}

func (s *service) GetUser(ctx context.Context, telegramID int64) (*registration.Account, error) {
	return s.repo.FindByTelegramID(ctx, telegramID)
}

func (s *service) ListAuditEventsByTelegramID(ctx context.Context, telegramID int64, limit int) ([]registration.AuditEvent, error) {
	if s == nil || s.repo == nil {
		return nil, nil
	}
	type auditLister interface {
		ListAuditEventsByTelegramID(ctx context.Context, telegramID int64, limit int) ([]registration.AuditEvent, error)
	}
	if r, ok := s.repo.(auditLister); ok {
		return r.ListAuditEventsByTelegramID(ctx, telegramID, limit)
	}
	return nil, nil
}

func (s *service) CreateUser(ctx context.Context, telegramID int64, telegramUsername string, embyUsername string) (registration.Account, registration.Credentials, error) {
	if telegramID == 0 {
		return registration.Account{}, registration.Credentials{}, fmt.Errorf("telegram id is empty")
	}

	if _, err := s.repo.FindByTelegramID(ctx, telegramID); err == nil {
		return registration.Account{}, registration.Credentials{}, registration.ErrAlreadyRegistered
	} else if err != nil && !errors.Is(err, registration.ErrNotFound) {
		return registration.Account{}, registration.Credentials{}, err
	}

	embyUsername = strings.TrimSpace(embyUsername)
	if embyUsername == "" {
		embyUsername = registration.GenerateEmbyUsername(s.opts.UsernamePrefix, telegramID, telegramUsername)
	}
	password := registration.GeneratePassword(s.opts.PasswordLength)

	embyUserID, err := s.emby.CreateUser(ctx, embyUsername, password)
	if err != nil {
		return registration.Account{}, registration.Credentials{}, err
	}

	account := registration.Account{
		TelegramID:       telegramID,
		TelegramUsername: telegramUsername,
		EmbyUserID:       embyUserID,
		EmbyUsername:     embyUsername,
		CreatedAt:        time.Now(),
	}
	if err := s.repo.Create(ctx, account); err != nil {
		_ = s.emby.DeleteUser(ctx, embyUserID)
		return registration.Account{}, registration.Credentials{}, err
	}

	return account, registration.Credentials{Username: embyUsername, Password: password}, nil
}

func (s *service) ResetPassword(ctx context.Context, telegramID int64) (registration.Account, registration.Credentials, error) {
	account, err := s.repo.FindByTelegramID(ctx, telegramID)
	if err != nil {
		return registration.Account{}, registration.Credentials{}, err
	}

	newPassword := registration.GeneratePassword(s.opts.PasswordLength)
	if err := s.emby.UpdateUserPassword(ctx, account.EmbyUserID, newPassword); err != nil {
		return registration.Account{}, registration.Credentials{}, err
	}

	return *account, registration.Credentials{Username: account.EmbyUsername, Password: newPassword}, nil
}

func (s *service) DeleteUser(ctx context.Context, telegramID int64) (registration.Account, error) {
	account, err := s.repo.DeleteByTelegramID(ctx, telegramID)
	if err != nil {
		return registration.Account{}, err
	}
	_ = s.emby.DeleteUser(ctx, account.EmbyUserID)
	return *account, nil
}

func (s *service) RenewByTelegramID(ctx context.Context, telegramID int64, deltaDays float64) (registration.Account, *time.Time, error) {
	account, err := s.repo.FindByTelegramID(ctx, telegramID)
	if err != nil {
		return registration.Account{}, nil, err
	}
	if account.EmbyUserID == "" {
		return registration.Account{}, nil, ErrUserNotRegistered
	}

	newExpiresAt := calcRenewedExpiresAt(time.Now(), account.ExpiresAt, deltaDays)
	if err := s.repo.UpdateExpiresAt(ctx, telegramID, newExpiresAt); err != nil {
		return registration.Account{}, nil, err
	}
	account.ExpiresAt = newExpiresAt
	return *account, newExpiresAt, nil
}

func (s *service) RenewByEmbyUsername(ctx context.Context, embyUsername string, deltaDays float64) (registration.Account, *time.Time, error) {
	account, err := s.repo.FindByEmbyUsername(ctx, strings.TrimSpace(embyUsername))
	if err != nil {
		return registration.Account{}, nil, err
	}
	if account.EmbyUserID == "" {
		return registration.Account{}, nil, ErrUserNotRegistered
	}

	newExpiresAt := calcRenewedExpiresAt(time.Now(), account.ExpiresAt, deltaDays)
	if err := s.repo.UpdateExpiresAt(ctx, account.TelegramID, newExpiresAt); err != nil {
		return registration.Account{}, nil, err
	}
	account.ExpiresAt = newExpiresAt
	return *account, newExpiresAt, nil
}

func (s *service) RenewAll(ctx context.Context, deltaDays float64) (updated int, skippedUnlimited int, err error) {
	now := time.Now()
	const limit = 200
	offset := 0

	for {
		users, err := s.repo.ListRegistered(ctx, limit, offset)
		if err != nil {
			return 0, 0, err
		}
		if len(users) == 0 {
			break
		}

		for _, u := range users {
			if u.TelegramID == 0 {
				continue
			}
			if u.ExpiresAt == nil {
				skippedUnlimited++
				continue
			}
			newExpiresAt := calcRenewedExpiresAt(now, u.ExpiresAt, deltaDays)
			if err := s.repo.UpdateExpiresAt(ctx, u.TelegramID, newExpiresAt); err != nil {
				return updated, skippedUnlimited, err
			}
			updated++
		}

		offset += limit
	}

	return updated, skippedUnlimited, nil
}

func calcRenewedExpiresAt(now time.Time, current *time.Time, deltaDays float64) *time.Time {
	if deltaDays == 0 {
		return current
	}

	delta := time.Duration(deltaDays * 24 * float64(time.Hour))
	base := now
	if current != nil && current.After(now) {
		base = *current
	}
	t := base.Add(delta)
	return &t
}
