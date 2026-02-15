package repo

import (
	"context"
	"strings"
	"time"

	"emby-bot-new/internal/application/invite"

	"gorm.io/gorm"
)

type InviteGraphRepository struct {
	db *gorm.DB
}

func NewInviteGraphRepository(db *gorm.DB) *InviteGraphRepository {
	return &InviteGraphRepository{db: db}
}

func (r *InviteGraphRepository) LatestUsedAtByCreatorPrefix(ctx context.Context, creatorTelegramID int64, prefix string) (*time.Time, error) {
	if r == nil || r.db == nil || creatorTelegramID == 0 {
		return nil, nil
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, nil
	}

	type row struct {
		UsedAt *time.Time `gorm:"column:used_at"`
	}
	var got row
	err := r.db.WithContext(ctx).
		Model(&InviteCode{}).
		Select("used_at").
		Where("creator_telegram_id = ? AND used_by_telegram_id IS NOT NULL AND used_at IS NOT NULL AND code LIKE ?", creatorTelegramID, prefix+"%").
		Order("used_at desc").
		Limit(1).
		Find(&got).Error
	if err != nil {
		return nil, err
	}
	if got.UsedAt == nil || got.UsedAt.IsZero() {
		return nil, nil
	}
	return got.UsedAt, nil
}

func (r *InviteGraphRepository) LatestUnusedCodeByCreatorPrefix(ctx context.Context, creatorTelegramID int64, prefix string) (string, error) {
	if r == nil || r.db == nil || creatorTelegramID == 0 {
		return "", nil
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "", nil
	}

	type row struct {
		Code string `gorm:"column:code"`
	}
	var got row
	err := r.db.WithContext(ctx).
		Model(&InviteCode{}).
		Select("code").
		Where("creator_telegram_id = ? AND used_by_telegram_id IS NULL AND reserved_by_telegram_id IS NULL AND code LIKE ?", creatorTelegramID, prefix+"%").
		Order("created_at desc").
		Limit(1).
		Find(&got).Error
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(got.Code), nil
}

func (r *InviteGraphRepository) ListEdgesByCreatorsPrefix(ctx context.Context, creatorTelegramIDs []int64, prefix string) ([]invite.Edge, error) {
	if r == nil || r.db == nil || len(creatorTelegramIDs) == 0 {
		return nil, nil
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, nil
	}

	type row struct {
		CreatorTelegramID int64 `gorm:"column:creator_telegram_id"`
		UsedByTelegramID  int64 `gorm:"column:used_by_telegram_id"`
	}
	rows := make([]row, 0, 64)
	if err := r.db.WithContext(ctx).
		Model(&InviteCode{}).
		Select("creator_telegram_id, used_by_telegram_id").
		Where("creator_telegram_id IN ? AND used_by_telegram_id IS NOT NULL AND code LIKE ?", creatorTelegramIDs, prefix+"%").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]invite.Edge, 0, len(rows))
	for _, v := range rows {
		if v.CreatorTelegramID == 0 || v.UsedByTelegramID == 0 {
			continue
		}
		out = append(out, invite.Edge{InviterTelegramID: v.CreatorTelegramID, InviteeTelegramID: v.UsedByTelegramID})
	}
	return out, nil
}

func (r *InviteGraphRepository) ListDirectInvitesByCreatorPrefix(ctx context.Context, creatorTelegramID int64, prefix string) ([]invite.DirectInvite, error) {
	if r == nil || r.db == nil || creatorTelegramID == 0 {
		return nil, nil
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, nil
	}

	type row struct {
		Code              string    `gorm:"column:code"`
		CreatorTelegramID int64     `gorm:"column:creator_telegram_id"`
		UsedByTelegramID  int64     `gorm:"column:used_by_telegram_id"`
		UsedAt            time.Time `gorm:"column:used_at"`
	}
	rows := make([]row, 0, 64)
	if err := r.db.WithContext(ctx).
		Model(&InviteCode{}).
		Select("code, creator_telegram_id, used_by_telegram_id, used_at").
		Where("creator_telegram_id = ? AND used_by_telegram_id IS NOT NULL AND used_at IS NOT NULL AND code LIKE ?", creatorTelegramID, prefix+"%").
		Order("used_at desc").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]invite.DirectInvite, 0, len(rows))
	for _, v := range rows {
		if v.CreatorTelegramID == 0 || v.UsedByTelegramID == 0 || v.UsedAt.IsZero() {
			continue
		}
		out = append(out, invite.DirectInvite{
			Code:              strings.TrimSpace(v.Code),
			InviterTelegramID: v.CreatorTelegramID,
			InviteeTelegramID: v.UsedByTelegramID,
			UsedAt:            v.UsedAt,
		})
	}
	return out, nil
}

func (r *InviteGraphRepository) GetDirectInviteByCreatorInviteePrefix(ctx context.Context, creatorTelegramID int64, inviteeTelegramID int64, prefix string) (*invite.DirectInvite, error) {
	if r == nil || r.db == nil || creatorTelegramID == 0 || inviteeTelegramID == 0 {
		return nil, nil
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, nil
	}

	type row struct {
		Code              string    `gorm:"column:code"`
		UsedAt            time.Time `gorm:"column:used_at"`
		CreatorTelegramID int64     `gorm:"column:creator_telegram_id"`
		UsedByTelegramID  int64     `gorm:"column:used_by_telegram_id"`
	}
	var got row
	err := r.db.WithContext(ctx).
		Model(&InviteCode{}).
		Select("code, creator_telegram_id, used_by_telegram_id, used_at").
		Where("creator_telegram_id = ? AND used_by_telegram_id = ? AND used_at IS NOT NULL AND code LIKE ?", creatorTelegramID, inviteeTelegramID, prefix+"%").
		Order("used_at desc").
		Limit(1).
		Find(&got).Error
	if err != nil {
		return nil, err
	}
	if got.CreatorTelegramID == 0 || got.UsedByTelegramID == 0 || got.UsedAt.IsZero() || strings.TrimSpace(got.Code) == "" {
		return nil, nil
	}
	return &invite.DirectInvite{
		Code:              strings.TrimSpace(got.Code),
		InviterTelegramID: got.CreatorTelegramID,
		InviteeTelegramID: got.UsedByTelegramID,
		UsedAt:            got.UsedAt,
	}, nil
}

func (r *InviteGraphRepository) RevokeDirectInviteByCreatorInviteePrefix(ctx context.Context, creatorTelegramID int64, inviteeTelegramID int64, prefix string) (*invite.DirectInvite, error) {
	if r == nil || r.db == nil || creatorTelegramID == 0 || inviteeTelegramID == 0 {
		return nil, nil
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, nil
	}

	// 先读取一份快照用于返回/审计，再做更新（避免把 used_by/used_at 清空后丢信息）。
	got, err := r.GetDirectInviteByCreatorInviteePrefix(ctx, creatorTelegramID, inviteeTelegramID, prefix)
	if err != nil {
		return nil, err
	}
	if got == nil || strings.TrimSpace(got.Code) == "" {
		return nil, nil
	}

	updates := map[string]any{
		"used_by_telegram_id":     nil,
		"used_at":                 nil,
		"reserved_by_telegram_id": nil,
		"reserved_at":             nil,
	}
	res := r.db.WithContext(ctx).
		Model(&InviteCode{}).
		Where("code = ? AND creator_telegram_id = ? AND used_by_telegram_id = ? AND code LIKE ?", got.Code, creatorTelegramID, inviteeTelegramID, prefix+"%").
		Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		// 可能被并发撤回/变更；按“未找到”处理。
		return nil, nil
	}
	return got, nil
}

var _ invite.GraphRepository = (*InviteGraphRepository)(nil)
