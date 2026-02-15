package router

// 本文件集中定义 Telegram 回调（callback）与按钮文案常量。
//
// 约定：
// - Cb*：inline keyboard 的 callback data（telebot.Btn.Unique）。
// - StartBtn*：/start 面板上常见入口的“可识别文本”，用于兼容用户直接发送按钮文案的情况。
//
// 为什么要集中管理：
// - callback data 一旦分散在各处，修改时容易漏改；
// - 统一常量便于 router.go 做路由注册与回归测试；
// - 对外 UI 文案调整更可控。
const (
	CbRegister      = "register"
	CbInviteCode    = "invite_code"
	CbBind          = "bind"
	CbMe            = "me"
	CbUserPanel     = "user_panel"
	CbServerInfo    = "server_info"
	CbEmbyLibs      = "emby_libs"
	CbToggleLib     = "toggle_lib"
	CbMyHistory     = "my_history"
	CbMyHarem       = "my_harem"
	CbHaremRevokeIn = "harem_revoke_in"
	CbHaremRevoke   = "harem_revoke"
	CbHaremConfirm  = "harem_confirm"
	CbBackMain      = "back_main"
	CbRenew         = "renew"
	CbResetPassword = "reset_password"
	CbDeleteAccount = "delete_account"
	CbCrowdfund     = "crowdfund"
	CbUserInvite    = "user_invite"
	CbCancel        = "cancel"
	CbRecheckJoin   = "recheck_join"

	CbAdminPanel               = "admin_panel"
	CbAdminUsers               = "admin_users"
	CbAdminHelp                = "admin_help"
	CbAdminReg                 = "admin_reg"
	CbAdminKeys                = "admin_keys"
	CbAdminCommunityMode       = "admin_comm_mode"
	CbAdminSetInactiveDuration = "admin_set_inactive"
	CbAdminDetectionTasks      = "admin_detect_tasks"
	CbAdminWhitelist           = "admin_whitelist"
	CbWhitelistAdd             = "whitelist_add"
	CbWhitelistRemove          = "whitelist_remove"
	CbToggleWebSchedule        = "toggle_web_schedule"
	CbToggleExpiredSchedule    = "toggle_expired_schedule"
	CbToggleInactiveSchedule   = "toggle_inactive_schedule"
	CbToggleGroupCommandGuard  = "toggle_group_command_guard"
	CbRunWebCheck              = "run_web_check"
	CbRunAllChecks             = "run_all_checks"

	CbRegToggle           = "reg_toggle"
	CbRegSetTiming        = "reg_set_timing"
	CbRegSetMaxUsers      = "reg_set_max_users"
	CbRegSetDefaultDays   = "reg_set_default_days"
	CbRegCreateCodes      = "reg_create_codes"
	CbRegCreateRenewCodes = "reg_create_renew_codes"
	CbRegExport           = "reg_export"
	CbRegWipe             = "reg_wipe"
	CbRegStats            = "reg_stats"
	CbRegGrant            = "reg_grant"
	// CbReqCommunityMode 用于“切换社区模式”的二次确认入口。
	CbReqCommunityMode = "req_comm_mode"
	// CbSetCommunityMode 作为“确认切换”后执行的回调（兼容旧逻辑）。
	CbSetCommunityMode = "set_comm_mode"
)

const (
	StartBtnUserPanel  = "👥 用户功能"
	StartBtnServer     = "🌐 服务器"
	StartBtnRegister   = "📝 注册 Emby"
	StartBtnCrowdfund  = "⚡ 发电"
	StartBtnAdminPanel = "⚙️ 管理面板"
)
