package invite

import (
	"context"
	"time"
)

const (
	// UserInviteCodePrefix 用于区分“用户邀请”生成的邀请码（避免把管理员批量码/发电码算作邀请关系）。
	UserInviteCodePrefix = "UI"
)

const (
	DefaultMinAccountAgeDays = 30
	DefaultCooldownDays      = 90
)

type Edge struct {
	InviterTelegramID int64
	InviteeTelegramID int64
}

// DirectInvite 表示一次“用户邀请（UI 前缀邀请码）”被成功兑换后的关系记录。
// UsedAt 以邀请码 used_at 为准（即被兑换的时间点）。
type DirectInvite struct {
	Code string

	InviterTelegramID int64
	InviteeTelegramID int64

	UsedAt time.Time
}

type GraphRepository interface {
	LatestUsedAtByCreatorPrefix(ctx context.Context, creatorTelegramID int64, prefix string) (*time.Time, error)
	LatestUnusedCodeByCreatorPrefix(ctx context.Context, creatorTelegramID int64, prefix string) (string, error)
	ListEdgesByCreatorsPrefix(ctx context.Context, creatorTelegramIDs []int64, prefix string) ([]Edge, error)

	// ListDirectInvitesByCreatorPrefix 列出某个创建者在指定 prefix 下已被使用的邀请码（用于“我的后宫”）。
	ListDirectInvitesByCreatorPrefix(ctx context.Context, creatorTelegramID int64, prefix string) ([]DirectInvite, error)
	// GetDirectInviteByCreatorInviteePrefix 查询指定邀请者->被邀请者的邀请关系（若存在）。
	GetDirectInviteByCreatorInviteePrefix(ctx context.Context, creatorTelegramID int64, inviteeTelegramID int64, prefix string) (*DirectInvite, error)
	// RevokeDirectInviteByCreatorInviteePrefix 撤回一条邀请关系（清空 used_by/used_at），并返回被撤回的记录快照。
	RevokeDirectInviteByCreatorInviteePrefix(ctx context.Context, creatorTelegramID int64, inviteeTelegramID int64, prefix string) (*DirectInvite, error)
}

type InviteCodeResult struct {
	Eligible bool
	Code     string
	IsNew    bool

	// NextAllowedAt 在不满足条件时用于提示下一次允许时间：
	// - 不满 1 个月：为满足 1 个月门槛的时间；
	// - 冷却中：为冷却结束时间。
	NextAllowedAt *time.Time
	Reason        string
}
