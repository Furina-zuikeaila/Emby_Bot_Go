package registration

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound          = errors.New("account not found")
	ErrAlreadyRegistered = errors.New("account already registered")

	ErrRegistrationClosed = errors.New("registration closed")
	ErrQuotaFull          = errors.New("registration quota full")
	ErrInvalidInput       = errors.New("invalid input")
	ErrInvalidInviteCode  = errors.New("invalid invite code")
	ErrInviteCodeUsed     = errors.New("invite code already used")
	ErrInviteCodeReserved = errors.New("invite code reserved by someone else")
	ErrEmbyAlreadyBound   = errors.New("emby account already bound")
)

type Account struct {
	TelegramID       int64
	TelegramUsername string

	EmbyUserID   string
	EmbyUsername string

	Level string

	// IsWhitelist 白名单用户（按 TG ID 绑定）。
	// 白名单用户不受：有效期 / 不活跃 的限制，但仍受 Web 检测限制。
	IsWhitelist bool

	PendingDays int
	// PendingExpiresAt “注册资格”的过期时间（例如 /License 发放后 24h）。
	// 为空表示永不过期（用于兼容旧逻辑）。
	PendingExpiresAt *time.Time
	ExpiresAt        *time.Time
	LastPlayedAt     *time.Time

	SecureCodeSalt string
	SecureCodeHash string

	CreatedAt time.Time
}

type Credentials struct {
	Username string
	Password string
}

type UserRepository interface {
	UpsertTelegram(ctx context.Context, telegramID int64, telegramUsername string) error

	FindByTelegramID(ctx context.Context, telegramID int64) (*Account, error)
	FindByEmbyUserID(ctx context.Context, embyUserID string) (*Account, error)

	CountRegistered(ctx context.Context) (int, error)

	SetPendingDays(ctx context.Context, telegramID int64, pendingDays int) error
	// SetPendingExpiresAt 设置“注册资格”的过期时间（例如 /License 发放后 24h）。
	SetPendingExpiresAt(ctx context.Context, telegramID int64, expiresAt *time.Time) error
	// SetWhitelist 设置/取消白名单（按 TG ID 绑定；telegramUsername 用于首次插入时保存）。
	SetWhitelist(ctx context.Context, telegramID int64, telegramUsername string, enabled bool) error
	SetRegistered(ctx context.Context, telegramID int64, telegramUsername string, embyUserID string, embyUsername string, secureSalt string, secureHash string, expiresAt *time.Time) error
	SetBound(ctx context.Context, telegramID int64, telegramUsername string, embyUserID string, embyUsername string, secureSalt string, secureHash string) error
}

type EmbyClient interface {
	CreateUser(ctx context.Context, username, password string) (string, error)
	DeleteUser(ctx context.Context, embyUserID string) error
	UpdateUserPassword(ctx context.Context, embyUserID, newPassword string) error
	AuthenticateByName(ctx context.Context, username, password string) (string, error)
}

type Service interface {
	Gate(ctx context.Context, telegramID int64, telegramUsername string) (Gate, error)
	RedeemInviteCode(ctx context.Context, telegramID int64, telegramUsername string, code string) (int, error)

	// ReservedInviteCode 返回当前用户被预留的“邀请码资格”（若存在）。
	// 典型来源：
	// - /start <邀请码> 兑换后预留；
	// - /Harem <TGID> 定向邀请后预留（无需用户手动拿到邀请码）。
	ReservedInviteCode(ctx context.Context, telegramID int64) (*InviteCode, error)

	Register(ctx context.Context, telegramID int64, telegramUsername string, embyUsername string, secureCode string) (Account, Credentials, error)
	Bind(ctx context.Context, telegramID int64, telegramUsername string, embyUsername string, embyPassword string, secureCode string) (Account, error)

	Me(ctx context.Context, telegramID int64) (*Account, error)
}

type Gate struct {
	Enabled          bool
	MaxUsers         int
	CurrentUsers     int
	Remaining        int
	DefaultDays      int
	OpenUntil        *time.Time
	HasQualification bool
	PendingDays      int
}

type Settings struct {
	Enabled     bool
	MaxUsers    int
	DefaultDays int
	OpenUntil   *time.Time
	// ServiceMode 服务模式/社区模式（用于展示/策略开关）。
	// 建议取值：private / public / charity（也允许为空，表示未设置）。
	ServiceMode string
	// InactiveDuration 不活跃时长（用于公益->公费清理策略）。
	// 使用 Go 的 time.ParseDuration 格式，例如：720h、168h、30m。
	InactiveDuration string
	// GroupAdminIDs 群管理 TGID 列表（仅允许使用 /Remove 指令）。
	GroupAdminIDs []int64
	UpdatedAt     time.Time
}

type SettingsRepository interface {
	Get(ctx context.Context) (Settings, error)
	Save(ctx context.Context, settings Settings) error
}

type InviteCode struct {
	Code string
	Days int

	CreatorTelegramID    int64
	UsedByTelegramID     *int64
	UsedAt               *time.Time
	ReservedByTelegramID *int64
	ReservedAt           *time.Time
}

// InviteCodeReservation 用于表示一个“已预留但尚未使用”的邀请码。
// 主要用于定时回收（例如定向邀请 12 小时未注册则回收并返还给邀请者）。
type InviteCodeReservation struct {
	Code string
	Days int

	CreatorTelegramID    int64
	ReservedByTelegramID int64
	ReservedAt           time.Time
}

// AuditEvent 用于记录“违规/警告/删号”等审计事件，便于定时汇总推送与日报统计。
// 注意：这是业务层的通用事件结构，落库由基础设施层实现。
type AuditEvent struct {
	ID uint

	// Category 事件类别：web / device / expired / inactive / manual 等。
	Category string
	// Action 行为：warn / revoke 等。
	Action string

	TelegramID   int64
	EmbyUsername string

	// Reason 原因/备注（用于展示与统计归类）。
	Reason string

	CreatedAt time.Time
}

type CodeStats struct {
	UsedCount   int64
	UnusedCount int64

	MonthCount    int64
	SeasonCount   int64
	HalfYearCount int64
	YearCount     int64
}

type InviteCodeRepository interface {
	Get(ctx context.Context, code string) (*InviteCode, error)
	GetReservedByUser(ctx context.Context, telegramID int64) (*InviteCode, error)
	ReserveForUser(ctx context.Context, code string, telegramID int64) (*InviteCode, error)
	ConfirmUsage(ctx context.Context, code string, telegramID int64) error
	ClearUserReservations(ctx context.Context, telegramID int64) error

	CreateBatch(ctx context.Context, creatorTelegramID int64, days int, count int, prefix string) ([]string, error)
	ListUnusedByCreator(ctx context.Context, creatorTelegramID int64) ([]string, error)
	DeleteAllUnusedByCreator(ctx context.Context, creatorTelegramID int64) (int64, error)
	Stats(ctx context.Context) (CodeStats, error)
}

type AdminService interface {
	GetSettings(ctx context.Context) (Settings, error)
	SetEnabled(ctx context.Context, enabled bool) (Settings, error)
	SetTimingMinutes(ctx context.Context, minutes int) (Settings, error)
	SetMaxUsers(ctx context.Context, maxUsers int) (Settings, error)
	SetDefaultDays(ctx context.Context, days int) (Settings, error)
	SetServiceMode(ctx context.Context, mode string) (Settings, error)
	SetInactiveDuration(ctx context.Context, duration string) (Settings, error)

	CreateCodes(ctx context.Context, creatorTelegramID int64, days int, count int, isRenew bool) ([]string, error)
	Stats(ctx context.Context) (CodeStats, error)
	ExportUnusedLinks(ctx context.Context, creatorTelegramID int64, botUsername string) ([]string, error)
	WipeUnused(ctx context.Context, creatorTelegramID int64) (int64, error)
	GrantQualification(ctx context.Context, adminTelegramID int64, targetTelegramID int64, days int) (string, error)

	// 白名单（按 TG ID 绑定；telegramUsername 用于首次插入时保存）。
	SetWhitelist(ctx context.Context, targetTelegramID int64, telegramUsername string, enabled bool) error
	// License 发放注册资格：自发放起 24h 内有效；注册成功后自动失效。
	GrantLicense(ctx context.Context, adminTelegramID int64, targetTelegramID int64) (expiresAt time.Time, err error)

	// 群管理（仅允许使用 /Remove）。
	// SetGroupAdmins 覆盖写入群管理列表。
	SetGroupAdmins(ctx context.Context, adminTelegramID int64, ids []int64) (Settings, error)
}
