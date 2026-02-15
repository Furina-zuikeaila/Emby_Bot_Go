package repo

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"emby-bot-new/internal/application/registration"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const registrationSettingsID uint = 1

type SettingsRepository struct {
	db *gorm.DB
}

func NewSettingsRepository(db *gorm.DB) *SettingsRepository {
	return &SettingsRepository{db: db}
}

func (r *SettingsRepository) Get(ctx context.Context) (registration.Settings, error) {
	var row RegistrationSettings
	err := r.db.WithContext(ctx).First(&row, registrationSettingsID).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return registration.Settings{}, err
		}

		now := time.Now()
		row = RegistrationSettings{
			ID:               registrationSettingsID,
			Enabled:          false,
			MaxUsers:         0,
			DefaultDays:      0,
			OpenUntil:        nil,
			ServiceMode:      "",
			InactiveDuration: "",
			UpdatedAt:        now,
		}
		if err := r.db.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&row).Error; err != nil {
			return registration.Settings{}, fmt.Errorf("init registration settings: %w", err)
		}
		if err := r.db.WithContext(ctx).First(&row, registrationSettingsID).Error; err != nil {
			return registration.Settings{}, err
		}
	}

	// 群管理列表：使用专用表 group_admins；如果为空且旧字段有值，则做一次性迁移写入。
	var admins []GroupAdmin
	if err := r.db.WithContext(ctx).
		Order("telegram_id asc").
		Find(&admins).Error; err != nil {
		return registration.Settings{}, err
	}
	groupIDs := make([]int64, 0, len(admins))
	for _, a := range admins {
		if a.TelegramID > 0 {
			groupIDs = append(groupIDs, a.TelegramID)
		}
	}
	if len(groupIDs) == 0 {
		legacy := parseInt64CSV(row.GroupAdminIDs)
		if len(legacy) != 0 {
			_ = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				for _, id := range legacy {
					if id <= 0 {
						continue
					}
					if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&GroupAdmin{TelegramID: id}).Error; err != nil {
						return err
					}
				}
				return nil
			})
			groupIDs = legacy
		}
	}

	return registration.Settings{
		Enabled:          row.Enabled,
		MaxUsers:         row.MaxUsers,
		DefaultDays:      row.DefaultDays,
		OpenUntil:        row.OpenUntil,
		ServiceMode:      strings.TrimSpace(row.ServiceMode),
		InactiveDuration: strings.TrimSpace(row.InactiveDuration),
		GroupAdminIDs:    groupIDs,
		UpdatedAt:        row.UpdatedAt,
	}, nil
}

func (r *SettingsRepository) Save(ctx context.Context, settings registration.Settings) error {
	now := time.Now()
	row := RegistrationSettings{
		ID:               registrationSettingsID,
		Enabled:          settings.Enabled,
		MaxUsers:         settings.MaxUsers,
		DefaultDays:      settings.DefaultDays,
		OpenUntil:        settings.OpenUntil,
		ServiceMode:      strings.TrimSpace(settings.ServiceMode),
		InactiveDuration: strings.TrimSpace(settings.InactiveDuration),
		// 旧字段保留但不再写入（使用 group_admins 表）。
		GroupAdminIDs: "",
		UpdatedAt:     now,
	}

	// 去重/过滤
	ids := make([]int64, 0, len(settings.GroupAdminIDs))
	seen := make(map[int64]struct{}, len(settings.GroupAdminIDs))
	for _, id := range settings.GroupAdminIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"enabled",
				"max_users",
				"default_days",
				"open_until",
				"service_mode",
				"inactive_duration",
				"group_admin_ids",
				"updated_at",
			}),
		}).Create(&row).Error; err != nil {
			return err
		}

		// 覆盖写入群管理列表
		if err := tx.Where("1 = 1").Delete(&GroupAdmin{}).Error; err != nil {
			return err
		}
		for _, id := range ids {
			if err := tx.Create(&GroupAdmin{TelegramID: id}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

var _ registration.SettingsRepository = (*SettingsRepository)(nil)

func parseInt64CSV(raw string) []int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		if unicode.IsSpace(r) {
			return true
		}
		switch r {
		case ',', '，', ';', '；', '、', '。':
			return true
		default:
			return false
		}
	})
	out := make([]int64, 0, len(parts))
	seen := make(map[int64]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func joinInt64CSV(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	out := make([]string, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, strconv.FormatInt(id, 10))
	}
	return strings.Join(out, ",")
}
