package telegram

import (
	accountapp "emby-bot-new/internal/application/account"
	adminapp "emby-bot-new/internal/application/admin"
	"emby-bot-new/internal/application/registration"
	"emby-bot-new/internal/transport/telegram/router"
)

// Options 透传 router.Options，保持外部调用简洁。
type Options = router.Options

// Router 透传 router.Router。
type Router = router.Router

// NewRouter 构建 Telegram 路由（交付层入口）。
func NewRouter(reg registration.Service, regAdmin registration.AdminService, adminSvc adminapp.Service, accountSvc accountapp.Service, opts Options) *Router {
	return router.NewRouter(reg, regAdmin, adminSvc, accountSvc, opts)
}
