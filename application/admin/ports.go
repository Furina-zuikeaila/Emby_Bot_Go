package admin

import (
	"context"
	"time"

	"emby-bot-new/internal/application/registration"
)

type UserRepository interface {
	FindByTelegramID(ctx context.Context, telegramID int64) (*registration.Account, error)
	FindByEmbyUsername(ctx context.Context, embyUsername string) (*registration.Account, error)
	List(ctx context.Context, limit, offset int) ([]registration.Account, error)
	ListRegistered(ctx context.Context, limit, offset int) ([]registration.Account, error)
	CountAll(ctx context.Context) (int, error)
	CountRegistered(ctx context.Context) (int, error)
	CountWhitelist(ctx context.Context) (int, error)
	Create(ctx context.Context, account registration.Account) error
	DeleteByTelegramID(ctx context.Context, telegramID int64) (*registration.Account, error)
	UpdateExpiresAt(ctx context.Context, telegramID int64, expiresAt *time.Time) error
}

type EmbyClient interface {
	CreateUser(ctx context.Context, username, password string) (string, error)
	DeleteUser(ctx context.Context, embyUserID string) error
	UpdateUserPassword(ctx context.Context, embyUserID, newPassword string) error
}

type Service interface {
	Stats(ctx context.Context) (Stats, error)

	ListUsers(ctx context.Context, limit, offset int) ([]registration.Account, error)
	GetUser(ctx context.Context, telegramID int64) (*registration.Account, error)
	CreateUser(ctx context.Context, telegramID int64, telegramUsername string, embyUsername string) (registration.Account, registration.Credentials, error)
	ResetPassword(ctx context.Context, telegramID int64) (registration.Account, registration.Credentials, error)
	DeleteUser(ctx context.Context, telegramID int64) (registration.Account, error)

	RenewByTelegramID(ctx context.Context, telegramID int64, deltaDays float64) (registration.Account, *time.Time, error)
	RenewByEmbyUsername(ctx context.Context, embyUsername string, deltaDays float64) (registration.Account, *time.Time, error)
	RenewAll(ctx context.Context, deltaDays float64) (updated int, skippedUnlimited int, err error)
}

type Stats struct {
	BotUsers        int
	RegisteredUsers int
	WhitelistUsers  int
}
