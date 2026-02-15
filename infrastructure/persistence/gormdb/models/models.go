package models

import "time"

// TelegramUser 机器人用户（只要跟机器人说过话，就会存在这条记录）。
type TelegramUser struct {
	TelegramID       int64  `gorm:"column:telegram_id;primaryKey;autoIncrement:false"`
	TelegramUsername string `gorm:"column:telegram_username;not null;default:'';size:64"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (TelegramUser) TableName() string { return "tg_users" }

// TelegramVisitor 访客用户（与机器人对话过，但未注册/未绑定时落在此表）。
// 注册/绑定成功后会迁移到 tg_users，并从 tg_visitors 删除。
type TelegramVisitor struct {
	TelegramID       int64  `gorm:"column:telegram_id;primaryKey;autoIncrement:false"`
	TelegramUsername string `gorm:"column:telegram_username;not null;default:'';size:64"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (TelegramVisitor) TableName() string { return "tg_visitors" }

// EmbyBinding TG 用户与 Emby 账号的绑定关系（注册/绑定成功后才会产生）。
type EmbyBinding struct {
	TelegramID int64 `gorm:"column:telegram_id;primaryKey;autoIncrement:false"`

	EmbyUserID   string `gorm:"column:emby_user_id;uniqueIndex;not null;size:64"`
	EmbyUsername string `gorm:"column:emby_username;uniqueIndex;not null;size:255"`

	SecureCodeSalt string `gorm:"column:secure_code_salt;not null;default:'';size:64"`
	SecureCodeHash string `gorm:"column:secure_code_hash;not null;default:'';size:64"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (EmbyBinding) TableName() string { return "emby_bindings" }

// UserSubscription 用户有效期（为空表示无限期；是否允许无限期由上层业务决定）。
type UserSubscription struct {
	TelegramID int64      `gorm:"column:telegram_id;primaryKey;autoIncrement:false"`
	ExpiresAt  *time.Time `gorm:"column:expires_at;index"`

	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (UserSubscription) TableName() string { return "user_subscriptions" }

// UserQualification 注册资格/待注册信息（邀请码天数、/License 24h 资格等）。
type UserQualification struct {
	TelegramID int64 `gorm:"column:telegram_id;primaryKey;autoIncrement:false"`

	PendingDays      int        `gorm:"column:pending_days;not null;default:0"`
	PendingExpiresAt *time.Time `gorm:"column:pending_expires_at;index"`

	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (UserQualification) TableName() string { return "user_qualifications" }

// UserWhitelist 白名单（按 TGID 绑定，不跟随 EmbyUserID 变化）。
type UserWhitelist struct {
	TelegramID int64     `gorm:"column:telegram_id;primaryKey;autoIncrement:false"`
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (UserWhitelist) TableName() string { return "user_whitelists" }

// UserPlaybackState 观影/活跃状态（用于不活跃检测）。
type UserPlaybackState struct {
	TelegramID   int64      `gorm:"column:telegram_id;primaryKey;autoIncrement:false"`
	LastPlayedAt *time.Time `gorm:"column:last_played_at;index"`

	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (UserPlaybackState) TableName() string { return "user_playback_states" }

// GroupAdmin 群管理（仅允许使用 /Remove）。
type GroupAdmin struct {
	TelegramID int64     `gorm:"column:telegram_id;primaryKey;autoIncrement:false"`
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (GroupAdmin) TableName() string { return "group_admins" }

type AccountLegacy struct {
	TelegramID       int64  `gorm:"column:telegram_id;primaryKey;autoIncrement:false"`
	TelegramUsername string `gorm:"column:telegram_username;not null;default:'';size:64"`

	EmbyUserID   *string `gorm:"column:emby_user_id;uniqueIndex;size:64"`
	EmbyUsername *string `gorm:"column:emby_username;uniqueIndex;size:255"`

	Level string `gorm:"column:level;not null;default:'d';size:1"`

	IsWhitelist bool `gorm:"column:is_whitelist;not null;default:false"`

	PendingDays      int        `gorm:"column:pending_days;not null;default:0"`
	PendingExpiresAt *time.Time `gorm:"column:pending_expires_at"`
	ExpiresAt        *time.Time `gorm:"column:expires_at"`
	LastPlayedAt     *time.Time `gorm:"column:last_played_at"`

	SecureCodeSalt string `gorm:"column:secure_code_salt;not null;default:'';size:64"`
	SecureCodeHash string `gorm:"column:secure_code_hash;not null;default:'';size:64"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (AccountLegacy) TableName() string { return "accounts" }

type RegistrationSettings struct {
	ID uint `gorm:"primaryKey"`

	Enabled          bool       `gorm:"column:enabled;not null;default:false"`
	MaxUsers         int        `gorm:"column:max_users;not null;default:0"`
	DefaultDays      int        `gorm:"column:default_days;not null;default:0"`
	OpenUntil        *time.Time `gorm:"column:open_until"`
	ServiceMode      string     `gorm:"column:service_mode;not null;default:'';size:32"`
	InactiveDuration string     `gorm:"column:inactive_duration;not null;default:'';size:64"`
	// GroupAdminIDs 群管理 TGID 列表（CSV 字符串，支持逗号分隔）。
	// 群管理仅允许使用 /Remove 指令（并支持批量/原因语法）。
	GroupAdminIDs string `gorm:"column:group_admin_ids;not null;default:'';size:2048"`

	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (RegistrationSettings) TableName() string { return "registration_settings" }

type InviteCode struct {
	Code string `gorm:"column:code;primaryKey;size:64"`
	Days int    `gorm:"column:days;not null;default:0"`

	CreatorTelegramID int64 `gorm:"column:creator_telegram_id;not null;index"`

	UsedByTelegramID *int64     `gorm:"column:used_by_telegram_id;index"`
	UsedAt           *time.Time `gorm:"column:used_at"`

	ReservedByTelegramID *int64     `gorm:"column:reserved_by_telegram_id;index"`
	ReservedAt           *time.Time `gorm:"column:reserved_at"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (InviteCode) TableName() string { return "invite_codes" }

// AuditEvent 审计事件（违规/警告/删号等），用于定时推送与日报统计。
type AuditEvent struct {
	ID uint `gorm:"primaryKey"`

	Category string `gorm:"column:category;not null;default:'';size:32;index"`
	Action   string `gorm:"column:action;not null;default:'';size:16;index"`

	TelegramID   int64  `gorm:"column:telegram_id;not null;index"`
	EmbyUsername string `gorm:"column:emby_username;not null;default:'';size:255"`

	Reason string `gorm:"column:reason;not null;default:'';size:1024"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime;index"`
}

func (AuditEvent) TableName() string { return "audit_events" }

// CrowdfundReceipt 记录“众筹支持”兑换结果，用于交易哈希去重与审计。
//
// 设计要点：
// - tx_hash 唯一：同一笔交易只能兑换一次，防止重复领取
// - status：用于区分“处理中/已发放”等状态（便于排查）
type CrowdfundReceipt struct {
	TxHash string `gorm:"column:tx_hash;primaryKey;size:64"`

	TelegramID int64 `gorm:"column:telegram_id;not null;index"`

	ToAddress       string `gorm:"column:to_address;not null;default:'';size:64"`
	ContractAddress string `gorm:"column:contract_address;not null;default:'';size:64"`
	TokenSymbol     string `gorm:"column:token_symbol;not null;default:'';size:16"`

	// AmountQuant 为最小单位整数（字符串形式，避免精度问题与数据库 bigint 限制差异）。
	AmountQuant string `gorm:"column:amount_quant;not null;default:'';size:64"`

	InviteCode string `gorm:"column:invite_code;not null;default:'';size:64"`
	Status     string `gorm:"column:status;not null;default:'';size:16;index"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime;index"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (CrowdfundReceipt) TableName() string { return "crowdfund_receipts" }
