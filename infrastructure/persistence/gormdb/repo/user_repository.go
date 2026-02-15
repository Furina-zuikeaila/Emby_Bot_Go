package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	accountapp "emby-bot-new/internal/application/account"
	"emby-bot-new/internal/application/admin"
	"emby-bot-new/internal/application/registration"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UserRepository struct {
	db *gorm.DB
}

func (r *UserRepository) UpdateEmbyUserID(ctx context.Context, telegramID int64, embyUserID string) error {
	if telegramID == 0 {
		return fmt.Errorf("telegram id is empty")
	}
	embyUserID = strings.TrimSpace(embyUserID)
	if embyUserID == "" {
		return fmt.Errorf("emby user id is empty")
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("db is nil")
	}
	return r.db.WithContext(ctx).
		Model(&EmbyBinding{}).
		Where("telegram_id = ?", telegramID).
		Update("emby_user_id", embyUserID).Error
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// joinedAccountRow 用于把多张表“拼装”为 application 层所需的 registration.Account。
// 注意：这里的字段名用 column tag 绑定 SQL 别名，避免 Scan 失败。
type joinedAccountRow struct {
	TelegramID       int64  `gorm:"column:telegram_id"`
	TelegramUsername string `gorm:"column:telegram_username"`

	EmbyUserID   *string `gorm:"column:emby_user_id"`
	EmbyUsername *string `gorm:"column:emby_username"`

	SecureCodeSalt *string `gorm:"column:secure_code_salt"`
	SecureCodeHash *string `gorm:"column:secure_code_hash"`

	ExpiresAt        *time.Time `gorm:"column:expires_at"`
	PendingDays      *int       `gorm:"column:pending_days"`
	PendingExpiresAt *time.Time `gorm:"column:pending_expires_at"`

	LastPlayedAt *time.Time `gorm:"column:last_played_at"`

	IsWhitelist int `gorm:"column:is_whitelist"`

	// CreatedAt 表示“注册/绑定时间”，用于 admin 列表排序；未绑定时退化为首次对话时间。
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (r *UserRepository) queryAccountByTelegramID(ctx context.Context, telegramID int64) (*joinedAccountRow, error) {
	row, err := r.queryAccountByTelegramIDFrom(ctx, telegramID, "tg_users")
	if err == nil {
		return row, nil
	}
	if errors.Is(err, registration.ErrNotFound) {
		return r.queryAccountByTelegramIDFrom(ctx, telegramID, "tg_visitors")
	}
	return nil, err
}

func (r *UserRepository) queryAccountByTelegramIDFrom(ctx context.Context, telegramID int64, table string) (*joinedAccountRow, error) {
	if telegramID == 0 {
		return nil, registration.ErrNotFound
	}
	if table != "tg_users" && table != "tg_visitors" {
		return nil, fmt.Errorf("invalid base table")
	}

	var row joinedAccountRow
	err := r.db.WithContext(ctx).
		Table(table+" u").
		Select(strings.Join([]string{
			"u.telegram_id AS telegram_id",
			"u.telegram_username AS telegram_username",
			"b.emby_user_id AS emby_user_id",
			"b.emby_username AS emby_username",
			"b.secure_code_salt AS secure_code_salt",
			"b.secure_code_hash AS secure_code_hash",
			"s.expires_at AS expires_at",
			"q.pending_days AS pending_days",
			"q.pending_expires_at AS pending_expires_at",
			"p.last_played_at AS last_played_at",
			"CASE WHEN w.telegram_id IS NULL THEN 0 ELSE 1 END AS is_whitelist",
			"COALESCE(b.created_at, u.created_at) AS created_at",
		}, ", ")).
		Joins("LEFT JOIN emby_bindings b ON b.telegram_id = u.telegram_id").
		Joins("LEFT JOIN user_subscriptions s ON s.telegram_id = u.telegram_id").
		Joins("LEFT JOIN user_qualifications q ON q.telegram_id = u.telegram_id").
		Joins("LEFT JOIN user_playback_states p ON p.telegram_id = u.telegram_id").
		Joins("LEFT JOIN user_whitelists w ON w.telegram_id = u.telegram_id").
		Where("u.telegram_id = ?", telegramID).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, registration.ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

func toDomainAccount(row joinedAccountRow) registration.Account {
	var embyUserID, embyUsername string
	if row.EmbyUserID != nil {
		embyUserID = strings.TrimSpace(*row.EmbyUserID)
	}
	if row.EmbyUsername != nil {
		embyUsername = strings.TrimSpace(*row.EmbyUsername)
	}
	var salt, hash string
	if row.SecureCodeSalt != nil {
		salt = *row.SecureCodeSalt
	}
	if row.SecureCodeHash != nil {
		hash = *row.SecureCodeHash
	}
	pendingDays := 0
	if row.PendingDays != nil {
		pendingDays = *row.PendingDays
	}

	level := "d"
	if embyUserID != "" {
		level = "b"
	}

	return registration.Account{
		TelegramID:       row.TelegramID,
		TelegramUsername: row.TelegramUsername,
		EmbyUserID:       embyUserID,
		EmbyUsername:     embyUsername,
		Level:            level,
		IsWhitelist:      row.IsWhitelist != 0,
		PendingDays:      pendingDays,
		PendingExpiresAt: row.PendingExpiresAt,
		ExpiresAt:        row.ExpiresAt,
		LastPlayedAt:     row.LastPlayedAt,
		SecureCodeSalt:   salt,
		SecureCodeHash:   hash,
		CreatedAt:        row.CreatedAt,
	}
}

func (r *UserRepository) UpsertTelegram(ctx context.Context, telegramID int64, telegramUsername string) error {
	if telegramID == 0 {
		return fmt.Errorf("telegram id is empty")
	}
	telegramUsername = normalizeTelegramUsername(telegramUsername)

	now := time.Now()

	// 已注册用户：只更新 tg_users（不把新用户塞进 tg_users，避免扩大扫描/列表数据）
	if telegramUsername != "" {
		res := r.db.WithContext(ctx).
			Model(&TelegramUser{}).
			Where("telegram_id = ?", telegramID).
			Updates(map[string]any{
				"telegram_username": telegramUsername,
				"updated_at":        now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			_ = r.db.WithContext(ctx).Where("telegram_id = ?", telegramID).Delete(&TelegramVisitor{}).Error
			return nil
		}
	} else {
		var cnt int64
		if err := r.db.WithContext(ctx).
			Model(&TelegramUser{}).
			Where("telegram_id = ?", telegramID).
			Count(&cnt).Error; err != nil {
			return err
		}
		if cnt > 0 {
			_ = r.db.WithContext(ctx).Where("telegram_id = ?", telegramID).Delete(&TelegramVisitor{}).Error
			return nil
		}
	}

	// 未注册/未绑定：落到 tg_visitors
	row := TelegramVisitor{TelegramID: telegramID, TelegramUsername: telegramUsername}
	onConflict := clause.OnConflict{Columns: []clause.Column{{Name: "telegram_id"}}}
	if telegramUsername == "" {
		onConflict.DoUpdates = clause.Assignments(map[string]any{
			"updated_at": now,
		})
	} else {
		onConflict.DoUpdates = clause.Assignments(map[string]any{
			"telegram_username": telegramUsername,
			"updated_at":        now,
		})
	}
	return r.db.WithContext(ctx).Clauses(onConflict).Create(&row).Error
}

func (r *UserRepository) FindByTelegramID(ctx context.Context, telegramID int64) (*registration.Account, error) {
	row, err := r.queryAccountByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	acc := toDomainAccount(*row)
	return &acc, nil
}

func (r *UserRepository) FindByEmbyUsername(ctx context.Context, embyUsername string) (*registration.Account, error) {
	embyUsername = strings.TrimSpace(embyUsername)
	if embyUsername == "" {
		return nil, registration.ErrNotFound
	}
	var bind EmbyBinding
	err := r.db.WithContext(ctx).
		Select("telegram_id").
		Where("emby_username = ?", embyUsername).
		First(&bind).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, registration.ErrNotFound
		}
		return nil, err
	}
	row, err := r.queryAccountByTelegramID(ctx, bind.TelegramID)
	if err != nil {
		return nil, err
	}
	acc := toDomainAccount(*row)
	return &acc, nil
}

func (r *UserRepository) List(ctx context.Context, limit, offset int) ([]registration.Account, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	var rows []joinedAccountRow
	if err := r.db.WithContext(ctx).
		Table("tg_users u").
		Select(strings.Join([]string{
			"u.telegram_id AS telegram_id",
			"u.telegram_username AS telegram_username",
			"b.emby_user_id AS emby_user_id",
			"b.emby_username AS emby_username",
			"b.secure_code_salt AS secure_code_salt",
			"b.secure_code_hash AS secure_code_hash",
			"s.expires_at AS expires_at",
			"q.pending_days AS pending_days",
			"q.pending_expires_at AS pending_expires_at",
			"p.last_played_at AS last_played_at",
			"CASE WHEN w.telegram_id IS NULL THEN 0 ELSE 1 END AS is_whitelist",
			"COALESCE(b.created_at, u.created_at) AS created_at",
		}, ", ")).
		Joins("LEFT JOIN emby_bindings b ON b.telegram_id = u.telegram_id").
		Joins("LEFT JOIN user_subscriptions s ON s.telegram_id = u.telegram_id").
		Joins("LEFT JOIN user_qualifications q ON q.telegram_id = u.telegram_id").
		Joins("LEFT JOIN user_playback_states p ON p.telegram_id = u.telegram_id").
		Joins("LEFT JOIN user_whitelists w ON w.telegram_id = u.telegram_id").
		Order("COALESCE(b.created_at, u.created_at) desc").
		Limit(limit).
		Offset(offset).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]registration.Account, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainAccount(row))
	}
	return out, nil
}

func (r *UserRepository) ListRegistered(ctx context.Context, limit, offset int) ([]registration.Account, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	var rows []joinedAccountRow
	if err := r.db.WithContext(ctx).
		Table("emby_bindings b").
		Select(strings.Join([]string{
			"u.telegram_id AS telegram_id",
			"u.telegram_username AS telegram_username",
			"b.emby_user_id AS emby_user_id",
			"b.emby_username AS emby_username",
			"b.secure_code_salt AS secure_code_salt",
			"b.secure_code_hash AS secure_code_hash",
			"s.expires_at AS expires_at",
			"q.pending_days AS pending_days",
			"q.pending_expires_at AS pending_expires_at",
			"p.last_played_at AS last_played_at",
			"CASE WHEN w.telegram_id IS NULL THEN 0 ELSE 1 END AS is_whitelist",
			"b.created_at AS created_at",
		}, ", ")).
		Joins("JOIN tg_users u ON u.telegram_id = b.telegram_id").
		Joins("LEFT JOIN user_subscriptions s ON s.telegram_id = u.telegram_id").
		Joins("LEFT JOIN user_qualifications q ON q.telegram_id = u.telegram_id").
		Joins("LEFT JOIN user_playback_states p ON p.telegram_id = u.telegram_id").
		Joins("LEFT JOIN user_whitelists w ON w.telegram_id = u.telegram_id").
		Order("b.created_at desc").
		Limit(limit).
		Offset(offset).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]registration.Account, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainAccount(row))
	}
	return out, nil
}

func (r *UserRepository) DeleteByTelegramID(ctx context.Context, telegramID int64) (*registration.Account, error) {
	acc, err := r.FindByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, err
	}

	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("telegram_id = ?", telegramID).Delete(&EmbyBinding{}).Error; err != nil {
			return err
		}
		if err := tx.Where("telegram_id = ?", telegramID).Delete(&UserSubscription{}).Error; err != nil {
			return err
		}
		if err := tx.Where("telegram_id = ?", telegramID).Delete(&UserQualification{}).Error; err != nil {
			return err
		}
		if err := tx.Where("telegram_id = ?", telegramID).Delete(&UserWhitelist{}).Error; err != nil {
			return err
		}
		if err := tx.Where("telegram_id = ?", telegramID).Delete(&UserPlaybackState{}).Error; err != nil {
			return err
		}
		userRes := tx.Where("telegram_id = ?", telegramID).Delete(&TelegramUser{})
		if userRes.Error != nil {
			return userRes.Error
		}
		visitorRes := tx.Where("telegram_id = ?", telegramID).Delete(&TelegramVisitor{})
		if visitorRes.Error != nil {
			return visitorRes.Error
		}
		if userRes.RowsAffected == 0 && visitorRes.RowsAffected == 0 {
			return registration.ErrNotFound
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return acc, nil
}

var _ registration.UserRepository = (*UserRepository)(nil)
var _ admin.UserRepository = (*UserRepository)(nil)
var _ accountapp.UserRepository = (*UserRepository)(nil)

func (r *UserRepository) Create(ctx context.Context, account registration.Account) error {
	if account.TelegramID == 0 {
		return fmt.Errorf("telegram id is empty")
	}
	now := time.Now()
	telegramUsername := normalizeTelegramUsername(account.TelegramUsername)

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1) tg_users
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "telegram_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"telegram_username": telegramUsername,
				"updated_at":        now,
			}),
		}).Create(&TelegramUser{
			TelegramID:       account.TelegramID,
			TelegramUsername: telegramUsername,
		}).Error; err != nil {
			return err
		}

		// 2) 白名单
		if account.IsWhitelist {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&UserWhitelist{
				TelegramID: account.TelegramID,
			}).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Where("telegram_id = ?", account.TelegramID).Delete(&UserWhitelist{}).Error; err != nil {
				return err
			}
		}

		// 3) 资格/待注册信息
		if account.PendingDays != 0 || account.PendingExpiresAt != nil {
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "telegram_id"}},
				DoUpdates: clause.Assignments(map[string]any{
					"pending_days":       account.PendingDays,
					"pending_expires_at": account.PendingExpiresAt,
					"updated_at":         now,
				}),
			}).Create(&UserQualification{
				TelegramID:       account.TelegramID,
				PendingDays:      account.PendingDays,
				PendingExpiresAt: account.PendingExpiresAt,
			}).Error; err != nil {
				return err
			}
		}

		// 4) 绑定信息（存在 Emby 信息才写入）
		if strings.TrimSpace(account.EmbyUserID) != "" && strings.TrimSpace(account.EmbyUsername) != "" {
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "telegram_id"}},
				DoUpdates: clause.Assignments(map[string]any{
					"emby_user_id":     strings.TrimSpace(account.EmbyUserID),
					"emby_username":    strings.TrimSpace(account.EmbyUsername),
					"secure_code_salt": account.SecureCodeSalt,
					"secure_code_hash": account.SecureCodeHash,
					"created_at":       now,
					"updated_at":       now,
				}),
			}).Create(&EmbyBinding{
				TelegramID:     account.TelegramID,
				EmbyUserID:     strings.TrimSpace(account.EmbyUserID),
				EmbyUsername:   strings.TrimSpace(account.EmbyUsername),
				SecureCodeSalt: account.SecureCodeSalt,
				SecureCodeHash: account.SecureCodeHash,
			}).Error; err != nil {
				return err
			}

			// 有效期（注册用户才需要）
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "telegram_id"}},
				DoUpdates: clause.Assignments(map[string]any{
					"expires_at": account.ExpiresAt,
					"updated_at": now,
				}),
			}).Create(&UserSubscription{
				TelegramID: account.TelegramID,
				ExpiresAt:  account.ExpiresAt,
			}).Error; err != nil {
				return err
			}
		}

		// 5) 观影状态（按需写入）
		if account.LastPlayedAt != nil {
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "telegram_id"}},
				DoUpdates: clause.Assignments(map[string]any{
					"last_played_at": account.LastPlayedAt,
					"updated_at":     now,
				}),
			}).Create(&UserPlaybackState{
				TelegramID:   account.TelegramID,
				LastPlayedAt: account.LastPlayedAt,
			}).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

func (r *UserRepository) FindByEmbyUserID(ctx context.Context, embyUserID string) (*registration.Account, error) {
	embyUserID = strings.TrimSpace(embyUserID)
	if embyUserID == "" {
		return nil, registration.ErrNotFound
	}

	var bind EmbyBinding
	err := r.db.WithContext(ctx).
		Select("telegram_id").
		Where("emby_user_id = ?", embyUserID).
		First(&bind).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, registration.ErrNotFound
		}
		return nil, err
	}

	row, err := r.queryAccountByTelegramID(ctx, bind.TelegramID)
	if err != nil {
		return nil, err
	}
	acc := toDomainAccount(*row)
	return &acc, nil
}

func (r *UserRepository) CountRegistered(ctx context.Context) (int, error) {
	var cnt int64
	if err := r.db.WithContext(ctx).
		Model(&EmbyBinding{}).
		Count(&cnt).Error; err != nil {
		return 0, err
	}
	return int(cnt), nil
}

func (r *UserRepository) CountAll(ctx context.Context) (int, error) {
	var cnt int64
	if err := r.db.WithContext(ctx).
		Model(&TelegramUser{}).
		Count(&cnt).Error; err != nil {
		return 0, err
	}
	return int(cnt), nil
}

func (r *UserRepository) CountWhitelist(ctx context.Context) (int, error) {
	var cnt int64
	if err := r.db.WithContext(ctx).
		Model(&UserWhitelist{}).
		Count(&cnt).Error; err != nil {
		return 0, err
	}
	return int(cnt), nil
}

func (r *UserRepository) SetPendingDays(ctx context.Context, telegramID int64, pendingDays int) error {
	if telegramID == 0 {
		return fmt.Errorf("telegram id is empty")
	}
	if pendingDays < 0 {
		pendingDays = 0
	}

	now := time.Now()
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "telegram_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"pending_days": pendingDays,
			"updated_at":   now,
		}),
	}).Create(&UserQualification{
		TelegramID:  telegramID,
		PendingDays: pendingDays,
	}).Error
}

func (r *UserRepository) SetPendingExpiresAt(ctx context.Context, telegramID int64, expiresAt *time.Time) error {
	if telegramID == 0 {
		return fmt.Errorf("telegram id is empty")
	}

	now := time.Now()
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "telegram_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"pending_expires_at": expiresAt,
			"updated_at":         now,
		}),
	}).Create(&UserQualification{
		TelegramID:       telegramID,
		PendingExpiresAt: expiresAt,
	}).Error
}

func (r *UserRepository) SetWhitelist(ctx context.Context, telegramID int64, telegramUsername string, enabled bool) error {
	if telegramID == 0 {
		return fmt.Errorf("telegram id is empty")
	}

	if enabled {
		return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&UserWhitelist{
			TelegramID: telegramID,
		}).Error
	}

	return r.db.WithContext(ctx).Where("telegram_id = ?", telegramID).Delete(&UserWhitelist{}).Error
}

func (r *UserRepository) UpdateExpiresAt(ctx context.Context, telegramID int64, expiresAt *time.Time) error {
	if telegramID == 0 {
		return fmt.Errorf("telegram id is empty")
	}

	// 续期/改期必须是已存在用户
	var user TelegramUser
	if err := r.db.WithContext(ctx).
		Select("telegram_id").
		Where("telegram_id = ?", telegramID).
		First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return registration.ErrNotFound
		}
		return err
	}

	now := time.Now()
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "telegram_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"expires_at": expiresAt,
			"updated_at": now,
		}),
	}).Create(&UserSubscription{
		TelegramID: telegramID,
		ExpiresAt:  expiresAt,
	}).Error
}

func (r *UserRepository) SetRegistered(
	ctx context.Context,
	telegramID int64,
	telegramUsername string,
	embyUserID string,
	embyUsername string,
	secureSalt string,
	secureHash string,
	expiresAt *time.Time,
) error {
	if telegramID == 0 {
		return fmt.Errorf("telegram id is empty")
	}
	embyUserID = strings.TrimSpace(embyUserID)
	embyUsername = strings.TrimSpace(embyUsername)
	if embyUserID == "" || embyUsername == "" {
		return fmt.Errorf("emby id/username is empty")
	}
	telegramUsername = normalizeTelegramUsername(telegramUsername)

	now := time.Now()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// tg_users（确保存在，并更新用户名）
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "telegram_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"telegram_username": telegramUsername,
				"updated_at":        now,
			}),
		}).Create(&TelegramUser{
			TelegramID:       telegramID,
			TelegramUsername: telegramUsername,
		}).Error; err != nil {
			return err
		}

		// 注册后不再保留访客记录（减少非注册用户对列表/扫描的干扰）
		if err := tx.Where("telegram_id = ?", telegramID).Delete(&TelegramVisitor{}).Error; err != nil {
			return err
		}

		// emby_bindings（注册时间以 created_at 记录；重复注册会刷新 created_at）
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "telegram_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"emby_user_id":     embyUserID,
				"emby_username":    embyUsername,
				"secure_code_salt": secureSalt,
				"secure_code_hash": secureHash,
				"created_at":       now,
				"updated_at":       now,
			}),
		}).Create(&EmbyBinding{
			TelegramID:     telegramID,
			EmbyUserID:     embyUserID,
			EmbyUsername:   embyUsername,
			SecureCodeSalt: secureSalt,
			SecureCodeHash: secureHash,
		}).Error; err != nil {
			return err
		}

		// user_subscriptions
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "telegram_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"expires_at": expiresAt,
				"updated_at": now,
			}),
		}).Create(&UserSubscription{
			TelegramID: telegramID,
			ExpiresAt:  expiresAt,
		}).Error; err != nil {
			return err
		}

		// 清理资格（邀请码/License 注册后自动失效）
		if err := tx.Where("telegram_id = ?", telegramID).Delete(&UserQualification{}).Error; err != nil {
			return err
		}

		return nil
	})
}

func (r *UserRepository) SetBound(
	ctx context.Context,
	telegramID int64,
	telegramUsername string,
	embyUserID string,
	embyUsername string,
	secureSalt string,
	secureHash string,
) error {
	if telegramID == 0 {
		return fmt.Errorf("telegram id is empty")
	}
	embyUserID = strings.TrimSpace(embyUserID)
	embyUsername = strings.TrimSpace(embyUsername)
	if embyUserID == "" || embyUsername == "" {
		return fmt.Errorf("emby id/username is empty")
	}
	telegramUsername = normalizeTelegramUsername(telegramUsername)

	now := time.Now()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// tg_users
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "telegram_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"telegram_username": telegramUsername,
				"updated_at":        now,
			}),
		}).Create(&TelegramUser{
			TelegramID:       telegramID,
			TelegramUsername: telegramUsername,
		}).Error; err != nil {
			return err
		}

		// 绑定后同样清理访客记录
		if err := tx.Where("telegram_id = ?", telegramID).Delete(&TelegramVisitor{}).Error; err != nil {
			return err
		}

		// emby_bindings（绑定也算注册：刷新 created_at 便于列表排序）
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "telegram_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"emby_user_id":     embyUserID,
				"emby_username":    embyUsername,
				"secure_code_salt": secureSalt,
				"secure_code_hash": secureHash,
				"created_at":       now,
				"updated_at":       now,
			}),
		}).Create(&EmbyBinding{
			TelegramID:     telegramID,
			EmbyUserID:     embyUserID,
			EmbyUsername:   embyUsername,
			SecureCodeSalt: secureSalt,
			SecureCodeHash: secureHash,
		}).Error; err != nil {
			return err
		}

		// 清理资格（绑定后也不应保留“待注册/License”）
		if err := tx.Where("telegram_id = ?", telegramID).Delete(&UserQualification{}).Error; err != nil {
			return err
		}

		return nil
	})
}

func normalizeTelegramUsername(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "@")
	return v
}

func (r *UserRepository) ResetRegistration(ctx context.Context, telegramID int64) error {
	if telegramID == 0 {
		return fmt.Errorf("telegram id is empty")
	}

	now := time.Now()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 用户必须存在
		var user TelegramUser
		if err := tx.Select("telegram_id", "telegram_username").Where("telegram_id = ?", telegramID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return registration.ErrNotFound
			}
			return err
		}

		// 取消注册后：把 tg_users 迁回 tg_visitors（保留用户名/首次对话时间）
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "telegram_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"telegram_username": user.TelegramUsername,
				"updated_at":        now,
			}),
		}).Create(&TelegramVisitor{
			TelegramID:       user.TelegramID,
			TelegramUsername: user.TelegramUsername,
		}).Error; err != nil {
			return err
		}
		if err := tx.Where("telegram_id = ?", telegramID).Delete(&TelegramUser{}).Error; err != nil {
			return err
		}

		// 删除绑定与有效期
		if err := tx.Where("telegram_id = ?", telegramID).Delete(&EmbyBinding{}).Error; err != nil {
			return err
		}
		if err := tx.Where("telegram_id = ?", telegramID).Delete(&UserSubscription{}).Error; err != nil {
			return err
		}

		// 清空资格（pending_days / pending_expires_at）
		if err := tx.Where("telegram_id = ?", telegramID).Delete(&UserQualification{}).Error; err != nil {
			return err
		}

		return nil
	})
}

func (r *UserRepository) UpdateLastPlayedAt(ctx context.Context, telegramID int64, at *time.Time) error {
	if telegramID == 0 {
		return fmt.Errorf("telegram id is empty")
	}

	// 用户必须存在
	var user TelegramUser
	if err := r.db.WithContext(ctx).
		Select("telegram_id").
		Where("telegram_id = ?", telegramID).
		First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return registration.ErrNotFound
		}
		return err
	}

	now := time.Now()
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "telegram_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"last_played_at": at,
			"updated_at":     now,
		}),
	}).Create(&UserPlaybackState{
		TelegramID:   telegramID,
		LastPlayedAt: at,
	}).Error
}

func (r *UserRepository) CreateAuditEvent(ctx context.Context, e registration.AuditEvent) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("db is nil")
	}
	if e.TelegramID == 0 {
		return fmt.Errorf("telegram id is empty")
	}
	row := AuditEvent{
		Category:     strings.TrimSpace(e.Category),
		Action:       strings.TrimSpace(e.Action),
		TelegramID:   e.TelegramID,
		EmbyUsername: strings.TrimSpace(e.EmbyUsername),
		Reason:       strings.TrimSpace(e.Reason),
	}
	return r.db.WithContext(ctx).Create(&row).Error
}

func (r *UserRepository) ListAuditEvents(ctx context.Context, from, to time.Time, categories []string) ([]registration.AuditEvent, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("db is nil")
	}
	if to.IsZero() {
		to = time.Now()
	}
	q := r.db.WithContext(ctx).
		Where("created_at >= ? AND created_at < ?", from, to).
		Order("created_at asc")
	if len(categories) > 0 {
		q = q.Where("category IN ?", categories)
	}
	var rows []AuditEvent
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]registration.AuditEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, registration.AuditEvent{
			ID:           row.ID,
			Category:     row.Category,
			Action:       row.Action,
			TelegramID:   row.TelegramID,
			EmbyUsername: row.EmbyUsername,
			Reason:       row.Reason,
			CreatedAt:    row.CreatedAt,
		})
	}
	return out, nil
}

func (r *UserRepository) ListAuditEventsByTelegramID(ctx context.Context, telegramID int64, limit int) ([]registration.AuditEvent, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("db is nil")
	}
	if telegramID == 0 {
		return nil, fmt.Errorf("telegram id is empty")
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	var rows []AuditEvent
	if err := r.db.WithContext(ctx).
		Where("telegram_id = ?", telegramID).
		Order("created_at desc").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]registration.AuditEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, registration.AuditEvent{
			ID:           row.ID,
			Category:     row.Category,
			Action:       row.Action,
			TelegramID:   row.TelegramID,
			EmbyUsername: row.EmbyUsername,
			Reason:       row.Reason,
			CreatedAt:    row.CreatedAt,
		})
	}
	return out, nil
}
