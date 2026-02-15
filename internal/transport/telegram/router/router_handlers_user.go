package router

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	accountapp "emby-bot-new/internal/application/account"
	"emby-bot-new/internal/application/registration"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleStart(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Send("请私聊我使用该功能。")
	}
	if c.Sender() == nil || c.Message() == nil {
		return nil
	}

	payload := strings.TrimSpace(c.Message().Payload)
	if payload != "" {
		// 管理员“点击消息里的 TGID 链接”跳转：使用 /start 的 payload 做深链跳转，
		// 并尽量编辑“用户管理中心”原消息，避免发一堆新消息。
		// payload 格式：au_<tgid>_<offset>
		if strings.HasPrefix(strings.ToLower(payload), "au_") {
			if !r.isAdminSender(c) {
				_ = c.Delete()
				return r.sendMainMenu(c, "无权限。")
			}
			tgID, offset, ok := parseAdminUserDeepLink(payload)
			if ok {
				_ = c.Delete()
				return r.jumpToAdminUserDetail(c, tgID, offset)
			}
		}

		if strings.EqualFold(payload, "register") {
			return r.startRegister(c)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if strings.HasPrefix(strings.ToLower(payload), strings.ToLower(registration.DefaultRenewCodePrefix)) {
			if r.acct == nil {
				return r.sendMainMenu(c, "续费功能未初始化。")
			}
			newExpiresAt, days, err := r.acct.RedeemRenewCode(ctx, c.Sender().ID, payload)
			if err != nil {
				switch {
				case errors.Is(err, accountapp.ErrNotRegistered):
					return r.sendMainMenu(c, "你还没有注册，无法使用续费码。")
				case errors.Is(err, accountapp.ErrUnlimitedAccount):
					return r.sendMainMenu(c, "你的账号为无限期，无需续费。")
				case errors.Is(err, accountapp.ErrInvalidRenewCode):
					return r.sendMainMenu(c, "续费码无效。")
				case errors.Is(err, registration.ErrInviteCodeUsed):
					return r.sendMainMenu(c, "续费码已被使用。")
				case errors.Is(err, registration.ErrInviteCodeReserved):
					return r.sendMainMenu(c, "续费码已被其他人锁定，请稍后再试。")
				default:
					return r.sendMainMenu(c, "续费失败："+userFriendlyError(err))
				}
			}
			return r.sendMainMenuMarkdown(c, fmt.Sprintf("续费成功：+%d 天\n新到期时间：`%s`", days, newExpiresAt.Format("2006-01-02 15:04:05")))
		}

		if existing, err := r.reg.Me(ctx, c.Sender().ID); err == nil && existing != nil && existing.EmbyUserID != "" {
			return r.sendMainMenu(c, "你已注册，无需邀请码。")
		}

		days, err := r.reg.RedeemInviteCode(ctx, c.Sender().ID, c.Sender().Username, payload)
		if err != nil {
			_ = r.sendMainMenu(c, "邀请码兑换失败："+userFriendlyError(err))
		} else {
			_ = r.sendMainMenu(c, fmt.Sprintf("邀请码兑换成功，获得资格：%d 天。", days))
		}
		return r.startRegister(c)
	}

	return r.sendStartPage(c)
}

func parseAdminUserDeepLink(payload string) (tgID int64, offset int, ok bool) {
	p := strings.TrimSpace(payload)
	p = strings.TrimPrefix(strings.ToLower(p), "au_")
	parts := strings.Split(p, "_")
	if len(parts) < 1 {
		return 0, 0, false
	}
	id, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || id <= 0 {
		return 0, 0, false
	}
	off := 0
	if len(parts) >= 2 {
		if v, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil && v >= 0 {
			off = v
		}
	}
	return id, off, true
}

func buildUserSummaryMarkdown(account *registration.Account, serviceMode string) string {
	if account == nil {
		return "👤 用户信息\n\n（无）"
	}
	var b strings.Builder
	b.WriteString("👤 用户信息\n")
	b.WriteString("——————————————\n")
	b.WriteString(fmt.Sprintf("🆔 TG ID：`%d`\n", account.TelegramID))
	if strings.TrimSpace(account.TelegramUsername) != "" {
		b.WriteString(fmt.Sprintf("👤 Telegram：`@%s`\n", safeInlineCode(strings.TrimPrefix(strings.TrimSpace(account.TelegramUsername), "@"))))
	}
	if strings.TrimSpace(account.EmbyUsername) != "" {
		b.WriteString(fmt.Sprintf("🎬 Emby：`%s`\n", safeInlineCode(account.EmbyUsername)))
	}
	if normalizeServiceMode(serviceMode) == "charity" {
		b.WriteString("⏳ 到期时间：当前为公益模式，不受到有效期限制\n")
	} else {
		b.WriteString(fmt.Sprintf("⏳ 到期时间：`%s`\n", formatExpiresAt(account.ExpiresAt)))
	}
	b.WriteString(fmt.Sprintf("🕒 上次活跃：`%s`\n", formatLastActive(account.LastPlayedAt)))
	return b.String()
}

// 管理员用户面板相关的辅助函数已迁移至 panel_admin_users.go

func formatServiceMode(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "未设置"
	}
	switch strings.ToLower(raw) {
	case "private", "p", "私服":
		return "私服"
	case "public", "pub", "公费":
		return "公费"
	case "charity", "c", "公益":
		return "公益"
	default:
		return raw
	}
}

func normalizeServiceMode(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	switch raw {
	case "private", "p", "私服":
		return "private"
	case "public", "pub", "公费":
		return "public"
	case "charity", "c", "公益":
		return "charity"
	default:
		return raw
	}
}

// getCurrentServiceMode 优先从数据库读取（可动态切换），否则回退到环境变量 SERVICE_MODE。
func (r *Router) getCurrentServiceMode(ctx context.Context) string {
	if r == nil || r.regAdmin == nil {
		return strings.TrimSpace(os.Getenv("SERVICE_MODE"))
	}
	settings, err := r.regAdmin.GetSettings(ctx)
	if err == nil {
		if v := strings.TrimSpace(settings.ServiceMode); v != "" {
			return v
		}
	}
	return strings.TrimSpace(os.Getenv("SERVICE_MODE"))
}

func (r *Router) handleRegister(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	return r.startRegister(c)
}

func (r *Router) handleMe(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	account, err := r.reg.Me(ctx, c.Sender().ID)
	if err != nil {
		if errors.Is(err, registration.ErrNotFound) {
			return r.editOrSendText(c, "你还没有注册，先点“注册 Emby”。", r.userNavMenu())
		}
		return r.editOrSendText(c, "查询失败："+userFriendlyError(err), r.userNavMenu())
	}
	if account.EmbyUserID == "" {
		return r.editOrSendText(c, "你还没有注册，先点“注册 Emby”。", r.userNavMenu())
	}

	// 账号信息页不展示 EmbyUserID，并补充“上次活跃时间（最后一次播放时间）”。
	msg := "📄 账号信息\n\n"
	msg += fmt.Sprintf("TelegramID：`%d`\n", account.TelegramID)
	if strings.TrimSpace(account.TelegramUsername) != "" {
		msg += fmt.Sprintf("Telegram：`@%s`\n", safeInlineCode(strings.TrimPrefix(strings.TrimSpace(account.TelegramUsername), "@")))
	}
	msg += fmt.Sprintf("Emby 用户名：`%s`\n", safeInlineCode(account.EmbyUsername))
	if normalizeServiceMode(r.getCurrentServiceMode(ctx)) == "charity" {
		msg += "到期时间：当前为公益模式，不受到有效期限制\n"
	} else {
		msg += fmt.Sprintf("到期时间：`%s`\n", formatExpiresAt(account.ExpiresAt))
	}
	msg += fmt.Sprintf("上次活跃：`%s`\n", formatLastActive(account.LastPlayedAt))
	return r.editOrSendText(c, msg, telebot.ModeMarkdown, r.userNavMenu())
}

func (r *Router) handleRenewCode(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	return r.startRenewCode(c)
}

func (r *Router) handleResetPassword(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	return r.startResetPassword(c)
}

func (r *Router) handleDeleteAccount(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	return r.startDeleteAccount(c)
}

func (r *Router) handleUserPanel(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	// 进入用户面板视为“重新开始导航”，清理可能残留的输入会话，避免出现“点了按钮但输入被当成其它流程”的情况。
	r.state.Clear(c.Sender().ID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	account, err := r.reg.Me(ctx, c.Sender().ID)
	if err != nil || account == nil || account.EmbyUserID == "" {
		return r.sendMainMenu(c, "你还没有注册，先点“注册 Emby”。")
	}

	// 用户功能页直接展示用户信息，不再需要再点“账户信息”按钮。
	// 若数据库尚未同步到 last_played_at，则在展示“用户信息”时按需拉取一次最近观影记录作为“上次活跃”。
	if account.LastPlayedAt == nil && r.acct != nil {
		if items, err := r.acct.PlaybackHistory(ctx, c.Sender().ID, 0, 1); err == nil && len(items) > 0 {
			account.LastPlayedAt = items[0].LastPlayedAt
		}
	}

	msg := buildUserSummaryMarkdown(account, r.getCurrentServiceMode(ctx))
	return r.editOrSendText(c, msg, telebot.ModeMarkdown, r.menus.UserPanel)
}

func (r *Router) handleUserInvite(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	if r.invite == nil || r.reg == nil {
		return r.editOrSendText(c, "邀请功能未初始化，请联系管理员。", r.userNavMenu())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	account, err := r.reg.Me(ctx, c.Sender().ID)
	if err != nil || account == nil || strings.TrimSpace(account.EmbyUserID) == "" {
		return r.editOrSendText(c, "你还没有注册/绑定账号，暂时无法邀请。", r.userNavMenu())
	}

	// 这里不再展示邀请码/链接，避免泄露；改为引导用户输入目标 TGID，走“定向预留资格（/Harem）”流程。
	r.state.Set(c.Sender().ID, convoUserInviteTarget, nil)
	msg := strings.Join([]string{
		"🎁 邀请好友",
		"",
		"请发送要邀请的好友 TGID（纯数字）。",
		"",
		fmt.Sprintf("对方无需拿到邀请码，只要在 %d 小时内私聊我发送 /start，即可直接开始注册。", int(r.inviteReservationTTL.Hours())),
	}, "\n")
	return r.editOrSendText(c, msg, r.userNavMenu())
}

func (r *Router) userNavMenu() *telebot.ReplyMarkup {
	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			menu.Data("⬅️ 用户功能", CbUserPanel),
			menu.Data("🏠 主菜单", CbBackMain),
		),
	)
	return menu
}

func (r *Router) handleUserInviteTargetInput(c telebot.Context, text string) error {
	if c == nil || c.Sender() == nil {
		return nil
	}
	if r.invite == nil || r.reg == nil {
		r.state.Clear(c.Sender().ID)
		return r.editOrSendText(c, "邀请功能未初始化，请联系管理员。", r.userNavMenu())
	}

	ids := uniqueInt64(parseTelegramIDs(text))
	if len(ids) != 1 {
		return r.editOrSendText(c, "请输入 1 个 TGID（纯数字）。", r.userNavMenu())
	}
	targetID := ids[0]
	if targetID == 0 || targetID == c.Sender().ID {
		return r.editOrSendText(c, "目标 TGID 无效。", r.userNavMenu())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := r.invite.PrepareTargetedInviteCode(ctx, c.Sender().ID, targetID)
	if err != nil {
		r.state.Clear(c.Sender().ID)
		logOp(c.Sender().ID, "邀请好友", "结果", "失败", "目标", targetID, "原因", userFriendlyError(err))
		return r.editOrSendText(c, "邀请失败："+userFriendlyError(err), r.userNavMenu())
	}
	if !res.Eligible {
		r.state.Clear(c.Sender().ID)
		msg := "🎁 邀请好友\n\n" + strings.TrimSpace(res.Reason)
		if res.NextAllowedAt != nil && !res.NextAllowedAt.IsZero() {
			msg += fmt.Sprintf("\n\n下次可邀请时间：`%s`", res.NextAllowedAt.Local().Format("2006-01-02 15:04:05"))
		}
		logOp(c.Sender().ID, "邀请好友", "结果", "失败", "目标", targetID, "原因", strings.TrimSpace(res.Reason))
		return r.editOrSendText(c, msg, telebot.ModeMarkdown, r.userNavMenu())
	}

	// 成功：为目标预留资格，并尝试私信通知（不包含邀请码/链接/TGID等敏感信息）。
	r.state.Clear(c.Sender().ID)

	expiresAt := time.Now().Add(r.inviteReservationTTL)

	inviterName := "朋友"
	if strings.TrimSpace(c.Sender().Username) != "" {
		inviterName = "@" + strings.TrimSpace(c.Sender().Username)
	}

	inviteeMsg := formatPushedCard(
		"🎁 你获得了注册资格",
		fmt.Sprintf("来自：%s", inviterName),
		"",
		fmt.Sprintf("我已为你预留注册资格（%d 小时内有效，至 %s）。", int(r.inviteReservationTTL.Hours()), expiresAt.Local().Format("2006-01-02 15:04:05")),
		"请私聊我发送 /start，然后按提示完成注册。",
	)
	sendErr := r.trySendToUser(c.Bot(), targetID, inviteeMsg)

	nextInviteLine := "下次邀请：不限制冷却。"
	if r.inviteCooldownDays > 0 {
		nextInviteLine = fmt.Sprintf("下次邀请：本次邀请成功注册后，需冷却 %d 天。", r.inviteCooldownDays)
	}

	lines := []string{
		"✅ 邀请资格已发放",
		fmt.Sprintf("有效期：至 `%s`（%d 小时内未注册将自动回收并返还给你）", expiresAt.Local().Format("2006-01-02 15:04:05"), int(r.inviteReservationTTL.Hours())),
		nextInviteLine,
	}
	if sendErr != nil {
		lines = append(lines, "", "⚠️ 我无法私信通知对方。请让对方主动私聊我发送 /start（无需邀请码）。")
		logOp(c.Sender().ID, "邀请好友", "结果", "成功", "目标", targetID, "投递", "对方私信失败")
	} else {
		logOp(c.Sender().ID, "邀请好友", "结果", "成功", "目标", targetID, "投递", "已私信对方")
	}
	return r.editOrSendText(c, strings.Join(lines, "\n"), telebot.ModeMarkdown, r.userNavMenu())
}

func (r *Router) handleBackMain(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	// 如果当前消息不是“主面板锚点消息”，尽量删除，避免聊天里残留多套面板。
	if c.Bot() != nil && c.Bot().Me != nil && c.Message() != nil && c.Message().Sender != nil && c.Message().Sender.ID == c.Bot().Me.ID {
		if anchor, ok := r.ui.Get(c.Sender().ID); ok && c.Message().ID != anchor.MessageID {
			_ = c.Delete()
		}
	}
	return r.updateStartPage(c)
}

func (r *Router) handleServerInfo(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	// 服务器信息仅对“已注册/已绑定”用户开放：
	// - 未注册用户不展示入口，也不允许通过历史消息/回调绕过。
	// - 删号后用户会回到“未注册”状态，因此同样不能继续查看。
	if !r.isRegisteredUser(c.Sender()) {
		return r.sendMainMenu(c, "你还没有注册，无法查看服务器信息。")
	}

	line := strings.TrimSpace(r.embyPublicURL)
	online := 0
	if r.acct != nil {
		ctx, cancel := bgCtxWithTimeout(timeout10s)
		defer cancel()
		if v, err := r.acct.GetActiveSessionsCount(ctx); err == nil {
			online = v
		}
	}

	msg := "服务器信息\n\n"
	if line != "" {
		msg += fmt.Sprintf("线路地址：\n`%s`\n\n", line)
	}
	msg += fmt.Sprintf("在线用户：`%d`\n", online)
	msg += fmt.Sprintf("更新时间：`%s`\n", time.Now().Format("2006-01-02 15:04:05"))

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(menu.Data("刷新", CbServerInfo)),
		menu.Row(
			menu.Data("⬅️ 用户功能", CbUserPanel),
			menu.Data("🏠 主菜单", CbBackMain),
		),
	)
	return r.editOrSendText(c, msg, telebot.ModeMarkdown, menu)
}

func (r *Router) handleEmbyLibs(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	if r.acct == nil {
		return r.editOrSendText(c, "功能未初始化。", r.userNavMenu())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	libs, err := r.acct.ListLibraries(ctx, c.Sender().ID)
	if err != nil {
		if errors.Is(err, accountapp.ErrNotRegistered) {
			return r.sendMainMenu(c, "你还没有注册，无法管理媒体库。")
		}
		return r.editOrSendText(c, "获取媒体库失败："+userFriendlyError(err), r.userNavMenu())
	}
	if len(libs) == 0 {
		return r.editOrSendText(c, "未获取到媒体库列表。", r.userNavMenu())
	}

	menu := &telebot.ReplyMarkup{}
	var rows []telebot.Row
	for _, lib := range libs {
		status := "关"
		if lib.Enabled {
			status = "开"
		}
		rows = append(rows, menu.Row(menu.Data(fmt.Sprintf("%s %s", status, lib.Name), CbToggleLib, lib.ID)))
	}
	rows = append(rows, menu.Row(
		menu.Data("⬅️ 用户功能", CbUserPanel),
		menu.Data("🏠 主菜单", CbBackMain),
	))
	menu.Inline(rows...)

	return r.editOrSendText(c, "媒体库管理：\n\n点击按钮切换开/关。", menu)
}

func (r *Router) handleToggleLib(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	if r.acct == nil {
		return r.editOrSendText(c, "功能未初始化。", r.userNavMenu())
	}
	libID := strings.TrimSpace(c.Data())
	if libID == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err := r.acct.ToggleLibrary(ctx, c.Sender().ID, libID)
	if err != nil {
		if errors.Is(err, accountapp.ErrNotRegistered) {
			return r.sendMainMenu(c, "你还没有注册，无法管理媒体库。")
		}
		return r.editOrSendText(c, "切换失败："+userFriendlyError(err), r.userNavMenu())
	}
	return r.handleEmbyLibs(c)
}

func (r *Router) handleMyHistory(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	if r.acct == nil {
		return r.editOrSendText(c, "功能未初始化。", r.userNavMenu())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	items, err := r.acct.PlaybackHistory(ctx, c.Sender().ID, 7, 10)
	if err != nil {
		if errors.Is(err, accountapp.ErrNotRegistered) {
			return r.sendMainMenu(c, "你还没有注册，无法查看观影记录。")
		}
		return r.editOrSendText(c, "查询失败："+userFriendlyError(err), r.userNavMenu())
	}

	var b strings.Builder
	b.WriteString("观影记录（最近 7 天）：\n\n")
	if len(items) == 0 {
		b.WriteString("暂无记录。\n")
	} else {
		for i, it := range items {
			title := it.Name
			if it.SeriesName != "" {
				title = it.SeriesName + " - " + it.Name
			}
			b.WriteString(fmt.Sprintf("%d) %s", i+1, title))
			if it.Type != "" {
				b.WriteString(fmt.Sprintf(" [%s]", it.Type))
			}
			if it.LastPlayedAt != nil {
				b.WriteString(fmt.Sprintf("\n   %s", it.LastPlayedAt.Local().Format("01-02 15:04")))
			}
			b.WriteString("\n")
		}
	}

	menu := &telebot.ReplyMarkup{}
	menu.Inline(menu.Row(
		menu.Data("⬅️ 用户功能", CbUserPanel),
		menu.Data("🏠 主菜单", CbBackMain),
	))
	return r.editOrSendText(c, b.String(), telebot.ModeMarkdown, menu)
}
