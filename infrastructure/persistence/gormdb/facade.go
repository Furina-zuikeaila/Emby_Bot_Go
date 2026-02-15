package gormdb

import (
	"emby-bot-new/internal/infrastructure/persistence/gormdb/db"
	"emby-bot-new/internal/infrastructure/persistence/gormdb/repo"

	"gorm.io/gorm"
)

// OpenMySQL 打开 MySQL 连接并完成启动期的必要工作（迁移/整理等）。
func OpenMySQL(dsn string) (*gorm.DB, error) { return db.OpenMySQL(dsn) }

// 仓储类型别名（保持外部 import 与类型名不变）。
type (
	UserRepository             = repo.UserRepository
	SettingsRepository         = repo.SettingsRepository
	InviteCodeRepository       = repo.InviteCodeRepository
	CrowdfundReceiptRepository = repo.CrowdfundReceiptRepository
	InviteGraphRepository      = repo.InviteGraphRepository
)

// 构造函数透传（保持外部调用不变）。
var (
	NewUserRepository             = repo.NewUserRepository
	NewSettingsRepository         = repo.NewSettingsRepository
	NewInviteCodeRepository       = repo.NewInviteCodeRepository
	NewCrowdfundReceiptRepository = repo.NewCrowdfundReceiptRepository
	NewInviteGraphRepository      = repo.NewInviteGraphRepository
)
