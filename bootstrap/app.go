package bootstrap

import (
	"fmt"
	"time"

	"emby-bot-new/internal/application/account"
	"emby-bot-new/internal/application/admin"
	"emby-bot-new/internal/application/invite"
	"emby-bot-new/internal/application/registration"
	"emby-bot-new/internal/config"
	"emby-bot-new/internal/infrastructure/emby"
	"emby-bot-new/internal/infrastructure/persistence/gormdb"

	"gopkg.in/telebot.v3"
	"gorm.io/gorm"
)

type App struct {
	// Config 是启动时加载并校验过的配置快照。
	// 注意：配置来自环境变量；运行时一般不变（动态开关类配置在 DB 中维护）。
	Config config.Config
	// DB 是 GORM 的数据库连接句柄（底层为 *sql.DB 池）。
	DB *gorm.DB
	// Bot 是 Telegram bot 客户端（telebot.v3）。
	Bot *telebot.Bot
	// UserRepo / SettingsRepo 为持久化层仓储实现。
	// 这里暴露出来，便于 cmd 层把它们注入 scheduler 等组件。
	UserRepo       *gormdb.UserRepository
	SettingsRepo   *gormdb.SettingsRepository
	InviteCodeRepo *gormdb.InviteCodeRepository
	// EmbyClient 封装 Emby HTTP API。
	EmbyClient *emby.Client
	// application 层服务：由 repository + 外部 client 组合而成。
	Registration registration.Service
	RegAdmin     registration.AdminService
	Admin        admin.Service
	Account      account.Service
	Invite       invite.Service
}

// New 构建一个可运行的 App。
//
// 约定：
// - 只做“初始化与组装”，不启动 goroutine（定时任务/轮询在 cmd 层启动）。
// - 任何初始化失败都应返回 error，让上层决定如何记录与退出。
func New() (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	db, err := gormdb.OpenMySQL(cfg.DB.MySQLDSN)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	embyClient, err := emby.New(cfg.Emby.BaseURL, cfg.Emby.APIKey, cfg.Emby.InsecureSkipVerify, cfg.Emby.SimultaneousStreamLimit)
	if err != nil {
		return nil, fmt.Errorf("init emby client: %w", err)
	}

	userRepo := gormdb.NewUserRepository(db)
	settingsRepo := gormdb.NewSettingsRepository(db)
	codeRepo := gormdb.NewInviteCodeRepository(db)
	inviteGraphRepo := gormdb.NewInviteGraphRepository(db)

	regSvc := registration.NewService(userRepo, settingsRepo, codeRepo, embyClient, registration.Options{
		UsernamePrefix: "tg",
		PasswordLength: 12,
	})
	regAdminSvc := registration.NewAdminService(userRepo, settingsRepo, codeRepo, registration.AdminOptions{
		RenewCodePrefix: "Renew",
	})
	adminSvc := admin.NewService(userRepo, embyClient, admin.Options{
		UsernamePrefix: "tg",
		PasswordLength: 12,
	})
	accountSvc := account.NewService(userRepo, codeRepo, embyClient, account.Options{
		RenewCodePrefix: registration.DefaultRenewCodePrefix,
		PasswordLength:  12,
	})
	inviteSvc := invite.NewService(userRepo, codeRepo, inviteGraphRepo, invite.Options{
		MinAccountAgeDays: cfg.Invite.MinAccountAgeDays,
		CooldownDays:      cfg.Invite.CooldownDays,
	})

	bot, err := telebot.NewBot(telebot.Settings{
		Token:  cfg.Telegram.BotToken,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		return nil, fmt.Errorf("create bot: %w", err)
	}

	// 只保留 /start 的原因：
	// - Telegram 客户端会展示“命令菜单”，但其内容会被 BotFather/平台缓存；
	// - 历史版本若注册过 /ping /sched 等调试命令，用户侧会长期残留；
	// - 运行时覆写为最小集合，可以降低误触与困惑，并让 UI 入口统一走面板按钮。
	setMinimalBotCommands(bot)

	return &App{
		Config:         cfg,
		DB:             db,
		Bot:            bot,
		UserRepo:       userRepo,
		SettingsRepo:   settingsRepo,
		InviteCodeRepo: codeRepo,
		EmbyClient:     embyClient,
		Registration:   regSvc,
		RegAdmin:       regAdminSvc,
		Admin:          adminSvc,
		Account:        accountSvc,
		Invite:         inviteSvc,
	}, nil
}

// setMinimalBotCommands 在 Telegram 侧覆盖 bot 的可见命令列表。
//
// 注意：
// - 这是“最佳努力”的调用：失败不应阻塞启动（用户仍可通过按钮/文本触发功能）。
// - scopes 需要覆盖多个范围，否则某些会话（私聊/群聊）会残留旧命令。
func setMinimalBotCommands(bot *telebot.Bot) {
	if bot == nil {
		return
	}

	commands := []map[string]string{
		{"command": "start", "description": "开始 / 主菜单"},
	}

	// 同时覆盖 default / all_private_chats / all_group_chats，避免不同会话范围残留旧命令。
	scopes := []map[string]string{
		{"type": "default"},
		{"type": "all_private_chats"},
		{"type": "all_group_chats"},
	}

	for _, scope := range scopes {
		_, _ = bot.Raw("setMyCommands", map[string]any{
			"commands": commands,
			"scope":    scope,
		})
	}
}
