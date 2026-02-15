package account

import (
	"context"
	"errors"
	"time"

	"emby-bot-new/internal/application/registration"
)

var (
	ErrNotRegistered     = errors.New("not registered")
	ErrInvalidSecureCode = errors.New("invalid secure code")
	ErrUnlimitedAccount  = errors.New("unlimited account")
	ErrInvalidRenewCode  = errors.New("invalid renew code")
)

type UserRepository interface {
	FindByTelegramID(ctx context.Context, telegramID int64) (*registration.Account, error)
	UpdateExpiresAt(ctx context.Context, telegramID int64, expiresAt *time.Time) error
	DeleteByTelegramID(ctx context.Context, telegramID int64) (*registration.Account, error)

	// UpdateEmbyUserID 更新已注册用户在库中的 EmbyUserID 绑定。
	// 用于自愈：当 Emby 账号被人工删除后重新创建，需要把新 ID 写回数据库。
	UpdateEmbyUserID(ctx context.Context, telegramID int64, embyUserID string) error
}

type InviteCodeRepository interface {
	ReserveForUser(ctx context.Context, code string, telegramID int64) (*registration.InviteCode, error)
	ConfirmUsage(ctx context.Context, code string, telegramID int64) error
	ClearUserReservations(ctx context.Context, telegramID int64) error
}

type EmbyClient interface {
	// CreateUser 用于自愈场景：当 Emby 账号被人工删除后，需要重新创建同名账号。
	// 需要创建新 Emby 用户并返回新的 EmbyUserID。
	CreateUser(ctx context.Context, username, password string) (string, error)
	UpdateUserPassword(ctx context.Context, embyUserID, newPassword string) error
	DeleteUser(ctx context.Context, embyUserID string) error

	GetUser(ctx context.Context, embyUserID string) (map[string]any, error)
	UpdateUserPolicy(ctx context.Context, embyUserID string, policy map[string]any) error
	GetLibraries(ctx context.Context) ([]map[string]any, error)

	GetSessions(ctx context.Context) ([]map[string]any, error)
	GetActiveSessionsCount(ctx context.Context) (int, error)

	GetPlaybackHistory(ctx context.Context, userID string, limit int) ([]map[string]any, error)
	// GetActivityLogEntries 获取 Emby “活动日志”条目（管理员接口）。
	// 用于更准确地展示“活动状况/播放记录”（例如“已开始播放/已停止播放”）。
	GetActivityLogEntries(ctx context.Context, startIndex, limit int, minDate *time.Time) ([]ActivityLogEntry, error)
}

type ActivityLogEntry struct {
	ID            int64
	Name          string
	Overview      string
	ShortOverview string
	Type          string
	ItemID        string
	Date          *time.Time
	UserID        string
	Severity      string
}

type Library struct {
	ID      string
	Name    string
	Enabled bool
}

type Session struct {
	DeviceName     string
	Client         string
	RemoteEndPoint string
}

type HistoryItem struct {
	Name         string
	Type         string
	SeriesName   string
	LastPlayedAt *time.Time
}

type Service interface {
	RedeemRenewCode(ctx context.Context, telegramID int64, code string) (*time.Time, int, error)
	ResetPassword(ctx context.Context, telegramID int64, secureCode string, newPassword string) (registration.Account, registration.Credentials, error)
	DeleteAccount(ctx context.Context, telegramID int64, secureCode string) (registration.Account, error)

	GetActiveSessionsCount(ctx context.Context) (int, error)
	ListLibraries(ctx context.Context, telegramID int64) ([]Library, error)
	ToggleLibrary(ctx context.Context, telegramID int64, libraryID string) ([]Library, error)

	ListSessions(ctx context.Context, telegramID int64) ([]Session, error)
	PlaybackHistory(ctx context.Context, telegramID int64, days int, limit int) ([]HistoryItem, error)
}
