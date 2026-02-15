package db

import (
	"fmt"
	"log"
	"time"

	zlogger "emby-bot-new/internal/infrastructure/persistence/gormdb/logger"
	"emby-bot-new/internal/infrastructure/persistence/gormdb/models"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// OpenMySQL 打开 MySQL 连接并完成启动期的必要工作：
// - 建立 GORM 连接并设置连接池参数
// - 自动迁移数据表（AutoMigrate）
// - 执行“兼容旧数据”的整理迁移（失败不阻塞启动）
//
// 注意：
// - 该函数会对数据库做写操作（迁移/整理），适用于自建部署与快速迭代场景。
// - 若你希望严格控制 schema 变更，可在此处替换为显式迁移机制。
func OpenMySQL(dsn string) (*gorm.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("mysql dsn is empty")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: zlogger.NewZhGormLogger(gormlogger.Warn),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(50)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// 数据表按职责拆分：tg_users / emby_bindings / user_subscriptions / user_qualifications /
	// user_whitelists / user_playback_states / group_admins / registration_settings / invite_codes / audit_events
	if err := db.AutoMigrate(
		&models.TelegramUser{},
		&models.TelegramVisitor{},
		&models.EmbyBinding{},
		&models.UserSubscription{},
		&models.UserQualification{},
		&models.UserWhitelist{},
		&models.UserPlaybackState{},
		&models.GroupAdmin{},
		&models.RegistrationSettings{},
		&models.InviteCode{},
		&models.AuditEvent{},
		&models.CrowdfundReceipt{},
	); err != nil {
		return nil, err
	}

	// 兼容旧数据：把“未注册/未绑定”的 tg_users 迁移到 tg_visitors，避免 tg_users 越积越大。
	// 迁移失败不应阻塞启动（仅影响数据整理）。
	if err := migrateTelegramUsersToVisitors(db); err != nil {
		log.Printf("【启动】迁移访客表失败：结果=失败 原因=%v", err)
	}

	return db, nil
}

// migrateTelegramUsersToVisitors 把“未注册/未绑定”的历史 tg_users 数据迁移到 tg_visitors。
//
// 背景：
// - 旧版本可能把所有对话过的用户都写入 tg_users；
// - 新版本按职责拆分为 tg_users（已注册/已绑定）与 tg_visitors（仅对话未注册）；
// - 为避免 tg_users 无界增长，需要一次性迁移历史数据。
//
// 约束：
// - 迁移仅影响“没有 emby_bindings 的用户”，即“等价于未注册/未绑定”的记录。
// - 失败不应阻塞启动（仅影响数据整理与后台统计）。
func migrateTelegramUsersToVisitors(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	// 仅迁移“没有 emby_bindings”的用户：这些用户等价于“已对话但未注册/未绑定”。
	// created_at/updated_at 直接沿用 tg_users 的时间。
	insertSQL := `
INSERT INTO tg_visitors (telegram_id, telegram_username, created_at, updated_at)
SELECT u.telegram_id, u.telegram_username, u.created_at, u.updated_at
FROM tg_users u
LEFT JOIN emby_bindings b ON b.telegram_id = u.telegram_id
WHERE b.telegram_id IS NULL
ON DUPLICATE KEY UPDATE
  telegram_username = VALUES(telegram_username),
  updated_at = VALUES(updated_at)
`
	deleteSQL := `
DELETE u FROM tg_users u
LEFT JOIN emby_bindings b ON b.telegram_id = u.telegram_id
WHERE b.telegram_id IS NULL
`

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(insertSQL).Error; err != nil {
			return err
		}
		return tx.Exec(deleteSQL).Error
	})
}
