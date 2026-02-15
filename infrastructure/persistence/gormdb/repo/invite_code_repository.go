package repo

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"strings"
	"time"

	"emby-bot-new/internal/application/registration"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type InviteCodeRepository struct {
	db *gorm.DB
}

func NewInviteCodeRepository(db *gorm.DB) *InviteCodeRepository {
	return &InviteCodeRepository{db: db}
}

func (r *InviteCodeRepository) Get(ctx context.Context, code string) (*registration.InviteCode, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, nil
	}
	var row InviteCode
	err := r.db.WithContext(ctx).Where("code = ?", code).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	out := toDomainInviteCode(row)
	return &out, nil
}

func (r *InviteCodeRepository) GetReservedByUser(ctx context.Context, telegramID int64) (*registration.InviteCode, error) {
	if telegramID == 0 {
		return nil, nil
	}
	var row InviteCode
	err := r.db.WithContext(ctx).
		Where("reserved_by_telegram_id = ? AND used_by_telegram_id IS NULL", telegramID).
		Order("reserved_at desc").
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	out := toDomainInviteCode(row)
	return &out, nil
}

func (r *InviteCodeRepository) ReserveForUser(ctx context.Context, code string, telegramID int64) (*registration.InviteCode, error) {
	code = strings.TrimSpace(code)
	if code == "" || telegramID == 0 {
		return nil, registration.ErrInvalidInviteCode
	}

	var out registration.InviteCode
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row InviteCode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code = ?", code).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return registration.ErrInvalidInviteCode
			}
			return err
		}
		if row.UsedByTelegramID != nil {
			return registration.ErrInviteCodeUsed
		}
		if row.ReservedByTelegramID != nil && *row.ReservedByTelegramID != telegramID {
			return registration.ErrInviteCodeReserved
		}

		now := time.Now()
		row.ReservedByTelegramID = &telegramID
		row.ReservedAt = &now
		if err := tx.Save(&row).Error; err != nil {
			return err
		}

		out = toDomainInviteCode(row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *InviteCodeRepository) ConfirmUsage(ctx context.Context, code string, telegramID int64) error {
	code = strings.TrimSpace(code)
	if code == "" || telegramID == 0 {
		return registration.ErrInvalidInviteCode
	}

	now := time.Now()
	res := r.db.WithContext(ctx).
		Model(&InviteCode{}).
		Where("code = ? AND reserved_by_telegram_id = ? AND used_by_telegram_id IS NULL", code, telegramID).
		Updates(map[string]any{
			"used_by_telegram_id": telegramID,
			"used_at":             &now,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return registration.ErrInvalidInviteCode
	}
	return nil
}

func (r *InviteCodeRepository) ListExpiredReservationsByPrefix(ctx context.Context, prefix string, reservedBefore time.Time, limit int) ([]registration.InviteCodeReservation, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}

	type row struct {
		Code                 string    `gorm:"column:code"`
		Days                 int       `gorm:"column:days"`
		CreatorTelegramID    int64     `gorm:"column:creator_telegram_id"`
		ReservedByTelegramID int64     `gorm:"column:reserved_by_telegram_id"`
		ReservedAt           time.Time `gorm:"column:reserved_at"`
	}
	rows := make([]row, 0, 64)
	if err := r.db.WithContext(ctx).
		Model(&InviteCode{}).
		Select("code, days, creator_telegram_id, reserved_by_telegram_id, reserved_at").
		Where("used_by_telegram_id IS NULL AND reserved_by_telegram_id IS NOT NULL AND reserved_at IS NOT NULL AND reserved_at < ? AND code LIKE ?", reservedBefore, prefix+"%").
		Order("reserved_at asc").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]registration.InviteCodeReservation, 0, len(rows))
	for _, v := range rows {
		if strings.TrimSpace(v.Code) == "" || v.CreatorTelegramID == 0 || v.ReservedByTelegramID == 0 || v.ReservedAt.IsZero() {
			continue
		}
		out = append(out, registration.InviteCodeReservation{
			Code:                 strings.TrimSpace(v.Code),
			Days:                 v.Days,
			CreatorTelegramID:    v.CreatorTelegramID,
			ReservedByTelegramID: v.ReservedByTelegramID,
			ReservedAt:           v.ReservedAt,
		})
	}
	return out, nil
}

func (r *InviteCodeRepository) ClearReservationIfStillUnused(ctx context.Context, code string, reservedByTelegramID int64) (bool, error) {
	code = strings.TrimSpace(code)
	if r == nil || r.db == nil || code == "" || reservedByTelegramID == 0 {
		return false, nil
	}
	res := r.db.WithContext(ctx).
		Model(&InviteCode{}).
		Where("code = ? AND used_by_telegram_id IS NULL AND reserved_by_telegram_id = ?", code, reservedByTelegramID).
		Updates(map[string]any{
			"reserved_by_telegram_id": nil,
			"reserved_at":             nil,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *InviteCodeRepository) ClearUserReservations(ctx context.Context, telegramID int64) error {
	if telegramID == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Model(&InviteCode{}).
		Where("reserved_by_telegram_id = ? AND used_by_telegram_id IS NULL", telegramID).
		Updates(map[string]any{
			"reserved_by_telegram_id": nil,
			"reserved_at":             nil,
		}).Error
}

func (r *InviteCodeRepository) CreateBatch(ctx context.Context, creatorTelegramID int64, days int, count int, prefix string) ([]string, error) {
	if creatorTelegramID == 0 || count <= 0 || count > 100 || days < 0 {
		return nil, registration.ErrInvalidInput
	}

	out := make([]string, 0, count)
	for len(out) < count {
		code := strings.TrimSpace(prefix + randomCode(16))
		row := InviteCode{
			Code:              code,
			Days:              days,
			CreatorTelegramID: creatorTelegramID,
		}
		if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
			if isDuplicateKeyError(err) {
				continue
			}
			return nil, err
		}
		out = append(out, code)
	}
	return out, nil
}

func (r *InviteCodeRepository) ListUnusedByCreator(ctx context.Context, creatorTelegramID int64) ([]string, error) {
	if creatorTelegramID == 0 {
		return nil, nil
	}
	var rows []InviteCode
	if err := r.db.WithContext(ctx).
		Select("code").
		Where("creator_telegram_id = ? AND used_by_telegram_id IS NULL", creatorTelegramID).
		Order("created_at desc").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Code)
	}
	return out, nil
}

func (r *InviteCodeRepository) DeleteAllUnusedByCreator(ctx context.Context, creatorTelegramID int64) (int64, error) {
	if creatorTelegramID == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).
		Where("creator_telegram_id = ? AND used_by_telegram_id IS NULL", creatorTelegramID).
		Delete(&InviteCode{})
	return res.RowsAffected, res.Error
}

func (r *InviteCodeRepository) Stats(ctx context.Context) (registration.CodeStats, error) {
	var usedCount int64
	if err := r.db.WithContext(ctx).
		Model(&InviteCode{}).
		Where("used_by_telegram_id IS NOT NULL").
		Count(&usedCount).Error; err != nil {
		return registration.CodeStats{}, err
	}

	var unusedCount int64
	if err := r.db.WithContext(ctx).
		Model(&InviteCode{}).
		Where("used_by_telegram_id IS NULL").
		Count(&unusedCount).Error; err != nil {
		return registration.CodeStats{}, err
	}

	monthCount, err := countUnusedDays(ctx, r.db, 30)
	if err != nil {
		return registration.CodeStats{}, err
	}
	seasonCount, err := countUnusedDays(ctx, r.db, 90)
	if err != nil {
		return registration.CodeStats{}, err
	}
	halfYearCount, err := countUnusedDays(ctx, r.db, 180)
	if err != nil {
		return registration.CodeStats{}, err
	}
	yearCount, err := countUnusedDays(ctx, r.db, 365)
	if err != nil {
		return registration.CodeStats{}, err
	}

	return registration.CodeStats{
		UsedCount:     usedCount,
		UnusedCount:   unusedCount,
		MonthCount:    monthCount,
		SeasonCount:   seasonCount,
		HalfYearCount: halfYearCount,
		YearCount:     yearCount,
	}, nil
}

func countUnusedDays(ctx context.Context, db *gorm.DB, days int) (int64, error) {
	var cnt int64
	if err := db.WithContext(ctx).
		Model(&InviteCode{}).
		Where("used_by_telegram_id IS NULL AND days = ?", days).
		Count(&cnt).Error; err != nil {
		return 0, err
	}
	return cnt, nil
}

func toDomainInviteCode(row InviteCode) registration.InviteCode {
	return registration.InviteCode{
		Code:                 row.Code,
		Days:                 row.Days,
		CreatorTelegramID:    row.CreatorTelegramID,
		UsedByTelegramID:     row.UsedByTelegramID,
		UsedAt:               row.UsedAt,
		ReservedByTelegramID: row.ReservedByTelegramID,
		ReservedAt:           row.ReservedAt,
	}
}

func randomCode(length int) string {
	if length <= 0 {
		length = 16
	}
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	max := big.NewInt(int64(len(charset)))
	out := make([]byte, length)
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			out[i] = charset[i%len(charset)]
			continue
		}
		out[i] = charset[n.Int64()]
	}
	return string(out)
}

func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	// 这里不依赖具体的 MySQL driver 错误类型，避免引入额外耦合（通过错误文本做最佳努力判断）。
	msg := err.Error()
	return strings.Contains(msg, "Duplicate entry") || strings.Contains(msg, "duplicate key")
}

var _ registration.InviteCodeRepository = (*InviteCodeRepository)(nil)
