package repo

import "emby-bot-new/internal/infrastructure/persistence/gormdb/models"

// 通过 type alias 让 repo 层保持原有的模型类型名，避免大范围替换。
type (
	TelegramUser         = models.TelegramUser
	TelegramVisitor      = models.TelegramVisitor
	EmbyBinding          = models.EmbyBinding
	UserSubscription     = models.UserSubscription
	UserQualification    = models.UserQualification
	UserWhitelist        = models.UserWhitelist
	UserPlaybackState    = models.UserPlaybackState
	GroupAdmin           = models.GroupAdmin
	AccountLegacy        = models.AccountLegacy
	RegistrationSettings = models.RegistrationSettings
	InviteCode           = models.InviteCode
	AuditEvent           = models.AuditEvent
	CrowdfundReceipt     = models.CrowdfundReceipt
)
