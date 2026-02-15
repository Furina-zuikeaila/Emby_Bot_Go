package router

import "gopkg.in/telebot.v3"

// Menus 负责集中构建本项目用到的所有 inline keyboard 菜单。
//
// 设计目的：
// - 把按钮的 Unique（callback data）与按钮布局（Row/Inline）集中管理；
// - router.go 只负责“注册路由与处理逻辑”，不夹杂大量 UI 拼装代码；
// - 方便后续做“按钮精简/文案统一/不同权限展示不同菜单”。
//
// 注意：
// - telebot.ReplyMarkup 内部不是线程安全的，但 Menus 在启动时构建一次后只读使用，属于安全用法。
type Menus struct {
	Main      *telebot.ReplyMarkup
	MainAdmin *telebot.ReplyMarkup
	Admin     *telebot.ReplyMarkup
	UserPanel *telebot.ReplyMarkup
	// UserOnly 只展示“用户功能”一个入口，避免在关键流程成功后刷出一堆按钮。
	UserOnly *telebot.ReplyMarkup

	RegisterBtn      telebot.Btn
	InviteCodeBtn    telebot.Btn
	BindBtn          telebot.Btn
	MeBtn            telebot.Btn
	UserPanelBtn     telebot.Btn
	RenewBtn         telebot.Btn
	ResetPasswordBtn telebot.Btn
	DeleteAccountBtn telebot.Btn
	AdminPanelBtn    telebot.Btn

	AdminRegBtn       telebot.Btn
	AdminUsersBtn     telebot.Btn
	AdminKeysBtn      telebot.Btn
	AdminWhitelistBtn telebot.Btn
	AdminModeBtn      telebot.Btn
	AdminDetectBtn    telebot.Btn
	BackMainBtn       telebot.Btn
}

// NewMenus 构建并返回所有菜单与按钮的集合。
//
// 约定：
// - 普通用户：Main（主菜单）与 UserPanel（用户功能页）。
// - 管理员：MainAdmin（主菜单 + 管理入口）与 Admin（管理员面板）。
// - UserOnly：在关键流程成功后（例如注册成功）只给出“用户功能”入口，避免按钮过多干扰用户理解。
func NewMenus() *Menus {
	menu := &telebot.ReplyMarkup{}
	menuAdmin := &telebot.ReplyMarkup{}
	adminMenu := &telebot.ReplyMarkup{}
	userPanel := &telebot.ReplyMarkup{}
	userOnly := &telebot.ReplyMarkup{}
	m := &Menus{Main: menu, MainAdmin: menuAdmin, Admin: adminMenu, UserPanel: userPanel, UserOnly: userOnly}

	m.RegisterBtn = menu.Data("注册 Emby", CbRegister)
	m.InviteCodeBtn = menu.Data("使用邀请码", CbInviteCode)
	m.BindBtn = menu.Data("绑定已有账号", CbBind)
	m.MeBtn = menu.Data("我的信息", CbMe)
	m.UserPanelBtn = menu.Data("用户功能", CbUserPanel)
	m.RenewBtn = menu.Data("使用续费码", CbRenew)
	m.ResetPasswordBtn = menu.Data("重置密码", CbResetPassword)
	m.DeleteAccountBtn = menu.Data("删除账号", CbDeleteAccount)
	m.AdminPanelBtn = menu.Data("管理员面板", CbAdminPanel)

	menu.Inline(
		menu.Row(m.RegisterBtn, m.InviteCodeBtn),
		menu.Row(m.BindBtn, m.MeBtn),
		menu.Row(m.RenewBtn, m.ResetPasswordBtn),
		menu.Row(m.DeleteAccountBtn),
		menu.Row(m.UserPanelBtn),
	)

	menuAdmin.Inline(
		menuAdmin.Row(m.RegisterBtn, m.InviteCodeBtn),
		menuAdmin.Row(m.BindBtn, m.MeBtn),
		menuAdmin.Row(m.RenewBtn, m.ResetPasswordBtn),
		menuAdmin.Row(m.DeleteAccountBtn),
		menuAdmin.Row(m.UserPanelBtn),
		menuAdmin.Row(m.AdminPanelBtn),
	)

	userPanel.Inline(
		// 用户功能页会直接展示“用户信息”，因此不再提供“账户信息”的入口。
		userPanel.Row(userPanel.Data("🎁 邀请好友", CbUserInvite), userPanel.Data("🎬 观影记录", CbMyHistory)),
		userPanel.Row(userPanel.Data("💳 使用续费码", CbRenew), userPanel.Data("🔐 重置密码", CbResetPassword)),
		userPanel.Row(userPanel.Data("🗑 删除账号", CbDeleteAccount), userPanel.Data("👑 我的后宫", CbMyHarem)),
		userPanel.Row(userPanel.Data("🏠 返回主菜单", CbBackMain)),
	)

	userOnly.Inline(
		userOnly.Row(m.UserPanelBtn),
	)

	m.AdminRegBtn = adminMenu.Data("注册管理", CbAdminReg)
	m.AdminUsersBtn = adminMenu.Data("用户管理", CbAdminUsers, "0")
	m.AdminKeysBtn = adminMenu.Data("密钥管理", CbAdminKeys)
	m.AdminWhitelistBtn = adminMenu.Data("白名单管理", CbAdminWhitelist)
	m.AdminModeBtn = adminMenu.Data("社区模式", CbAdminCommunityMode)
	m.AdminDetectBtn = adminMenu.Data("检测任务", CbAdminDetectionTasks)
	m.BackMainBtn = adminMenu.Data("返回主菜单", CbBackMain)
	adminMenu.Inline(
		adminMenu.Row(m.AdminRegBtn, m.AdminUsersBtn),
		adminMenu.Row(m.AdminKeysBtn, m.AdminWhitelistBtn),
		adminMenu.Row(m.AdminModeBtn, m.AdminDetectBtn),
		adminMenu.Row(m.BackMainBtn),
	)

	return m
}
