package router

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	accountapp "emby-bot-new/internal/application/account"
	adminapp "emby-bot-new/internal/application/admin"
	"emby-bot-new/internal/application/registration"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleAdmin(c telebot.Context) error {
	if !isPrivateChat(c) {
		return r.editOrSendText(c, "请私聊我使用该功能。")
	}
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}
	if r.adm == nil {
		return r.editOrSendText(c, "管理员功能尚未初始化。")
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	r.tryRemoveReplyKeyboard(c)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	registeredStr := "未知"
	whitelistStr := "未知"
	botUsersStr := "未知"
	if s, err := r.adm.Stats(ctx); err == nil {
		registeredStr = fmt.Sprintf("%d 人", s.RegisteredUsers)
		whitelistStr = fmt.Sprintf("%d 人", s.WhitelistUsers)
		botUsersStr = fmt.Sprintf("%d 人", s.BotUsers)
	}

	mode := formatServiceMode(r.getCurrentServiceMode(ctx))

	regStatus := "未知"
	timingStatus := "未知"
	maxUsersStr := "未知"
	if r.regAdmin != nil && r.reg != nil {
		settings, err := r.regAdmin.GetSettings(ctx)
		if err == nil {
			now := time.Now()

			regEnabled := settings.Enabled
			if settings.OpenUntil != nil && now.After(*settings.OpenUntil) {
				regEnabled = false
			}
			if regEnabled {
				regStatus = "✅ 开启"
			} else {
				regStatus = "❌ 关闭"
			}

			timingEnabled := settings.OpenUntil != nil && now.Before(*settings.OpenUntil)
			if timingEnabled {
				timingStatus = fmt.Sprintf("✅ 开启（%d min）", int(time.Until(*settings.OpenUntil).Minutes()))
			} else {
				timingStatus = "❌ 关闭"
			}

			if settings.MaxUsers >= 0 {
				maxUsersStr = fmt.Sprintf("%d 人", settings.MaxUsers)
			} else {
				maxUsersStr = "无限制"
			}
		}
	}

	msg := "⚙️ 管理员控制中心\n"
	msg += "——————————————\n"
	msg += fmt.Sprintf("👋 欢迎回来，%s\n\n", userDisplayName(c.Sender()))
	msg += "📊 系统状态\n"
	msg += fmt.Sprintf("🧭 服务模式：%s\n", mode)
	msg += fmt.Sprintf("📝 注册状态：%s\n", regStatus)
	msg += fmt.Sprintf("⏰ 定时注册：%s\n", timingStatus)
	msg += fmt.Sprintf("🚧 注册限制：%s\n\n", maxUsersStr)
	msg += "👤 用户统计\n"
	msg += fmt.Sprintf("✅ 已注册：%s\n", registeredStr)
	msg += fmt.Sprintf("⭐ 白名单：%s\n", whitelistStr)
	msg += fmt.Sprintf("🤖 Bot用户：%s", botUsersStr)

	return r.editOrSendText(c, msg, r.menus.Admin)
}

func (r *Router) handleAdminHelpCb(c telebot.Context) error {
	_ = c.Respond(&telebot.CallbackResponse{})
	return r.sendAdminHelp(c)
}

func (r *Router) handleAdminUsersCb(c telebot.Context) error {
	_ = c.Respond(&telebot.CallbackResponse{})
	return r.sendAdminUsers(c, c.Data())
}

func (r *Router) handleAdminUsersCmd(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Send("请私聊我使用该功能。")
	}
	if !r.isAdminSender(c) {
		return c.Send("无权限。")
	}
	args := cmdArgs(c)
	offset := 0
	if len(args) >= 1 {
		if v, err := strconv.Atoi(args[0]); err == nil && v >= 0 {
			offset = v
		}
	}
	return r.sendAdminUsers(c, strconv.Itoa(offset))
}

func (r *Router) handleAdminUserCmd(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Send("请私聊我使用该功能。")
	}
	if !r.isAdminSender(c) {
		return c.Send("无权限。")
	}

	args := cmdArgs(c)
	if len(args) < 1 {
		return c.Send("用法：/user <tg_id>")
	}
	tgID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || tgID <= 0 {
		return c.Send("tg_id 格式错误。")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	account, err := r.adm.GetUser(ctx, tgID)
	if err != nil {
		if errors.Is(err, registration.ErrNotFound) {
			return c.Send("未找到该用户。")
		}
		return c.Send("查询失败：" + userFriendlyError(err))
	}

	// 自动过滤未注册用户
	if account == nil || strings.TrimSpace(account.EmbyUserID) == "" {
		return c.Send("该用户未注册。")
	}

	// 命令查询：直接返回“全部信息”（设备/会话/IP、播放、审计等）。
	var sessions []accountapp.Session
	var history []accountapp.HistoryItem
	if r.acct != nil {
		if s, err := r.acct.ListSessions(ctx, tgID); err == nil {
			sessions = s
		}
		if h, err := r.acct.PlaybackHistory(ctx, tgID, 30, 5); err == nil {
			history = h
		}
	}
	events := make([]registration.AuditEvent, 0)
	if repo, ok := r.adm.(interface {
		ListAuditEventsByTelegramID(ctx context.Context, telegramID int64, limit int) ([]registration.AuditEvent, error)
	}); ok {
		if ev, err := repo.ListAuditEventsByTelegramID(ctx, tgID, 10); err == nil {
			events = ev
		}
	}

	msg := buildAdminUserFullDetailMarkdown(account, sessions, history, events)
	return c.Send(msg, telebot.ModeMarkdown)
}

func (r *Router) handleAdminCreateCmd(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Send("请私聊我使用该功能。")
	}
	if !r.isAdminSender(c) {
		return c.Send("无权限。")
	}

	targetID, targetUsername, embyUsername, err := parseCreateArgs(c)
	if err != nil {
		return c.Send(userFriendlyError(err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	account, cred, err := r.adm.CreateUser(ctx, targetID, targetUsername, embyUsername)
	if err != nil {
		if errors.Is(err, registration.ErrAlreadyRegistered) {
			return c.Send("该用户已存在绑定记录。")
		}
		return c.Send("创建失败：" + userFriendlyError(err))
	}

	msg := "创建成功：\n\n"
	msg += fmt.Sprintf("TelegramID：`%d`\n", account.TelegramID)
	msg += fmt.Sprintf("用户名：`%s`\n", cred.Username)
	msg += fmt.Sprintf("密码：`%s`\n", cred.Password)
	msg += fmt.Sprintf("EmbyUserID：`%s`\n", account.EmbyUserID)
	msg += "\n已尝试私聊发给用户（若用户未 /start 过可能失败）。"

	userMsg := formatPushedCard(
		"✅ 账号创建成功",
		fmt.Sprintf("👤 TGID：`%d`", account.TelegramID),
		fmt.Sprintf("🎬 Emby 用户名：`%s`", cred.Username),
		fmt.Sprintf("🔑 初始密码：`%s`", cred.Password),
		"📌 请尽快登录并修改密码。",
	)
	if err := r.trySendToUser(c.Bot(), account.TelegramID, userMsg); err != nil {
		msg += "\n发送给用户失败：" + userFriendlyError(err)
	}

	return c.Send(msg, telebot.ModeMarkdown)
}

func (r *Router) handleAdminResetPassCmd(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Send("请私聊我使用该功能。")
	}
	if !r.isAdminSender(c) {
		return c.Send("无权限。")
	}

	targetID, err := parseTargetTGID(c)
	if err != nil {
		return c.Send(userFriendlyError(err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	account, cred, err := r.adm.ResetPassword(ctx, targetID)
	if err != nil {
		if errors.Is(err, registration.ErrNotFound) {
			return c.Send("未找到该用户。")
		}
		return c.Send("重置失败：" + userFriendlyError(err))
	}

	msg := "密码已重置：\n\n"
	msg += fmt.Sprintf("TelegramID：`%d`\n", account.TelegramID)
	msg += fmt.Sprintf("用户名：`%s`\n", cred.Username)
	msg += fmt.Sprintf("新密码：`%s`\n", cred.Password)
	msg += "\n已尝试私聊发给用户（若用户未 /start 过可能失败）。"

	userMsg := formatPushedCard(
		"🔁 密码已重置",
		fmt.Sprintf("👤 TGID：`%d`", account.TelegramID),
		fmt.Sprintf("🎬 Emby 用户名：`%s`", cred.Username),
		fmt.Sprintf("🔑 新密码：`%s`", cred.Password),
		"📌 请尽快登录并修改密码。",
	)
	if err := r.trySendToUser(c.Bot(), account.TelegramID, userMsg); err != nil {
		msg += "\n发送给用户失败：" + userFriendlyError(err)
	}

	return c.Send(msg, telebot.ModeMarkdown)
}

func (r *Router) handleAdminRenewCmd(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Send("请私聊我使用该功能。")
	}
	if !r.isAdminSender(c) {
		return c.Send("无权限。")
	}
	if r.adm == nil {
		return c.Send("管理员功能尚未初始化。")
	}
	if c.Message() == nil {
		return nil
	}

	args := cmdArgs(c)

	var target string
	var daysStr string

	if c.Message().ReplyTo != nil && c.Message().ReplyTo.Sender != nil {
		if len(args) < 1 {
			return c.Send("用法：回复用户 /renew <+/-天数>\n或：/renew <tg_id|emby_username> <+/-天数>")
		}
		target = strconv.FormatInt(c.Message().ReplyTo.Sender.ID, 10)
		daysStr = args[0]
	} else {
		if len(args) < 2 {
			return c.Send("用法：/renew <tg_id|emby_username> <+/-天数>")
		}
		target = args[0]
		daysStr = args[1]
	}

	deltaDays, err := strconv.ParseFloat(daysStr, 64)
	if err != nil {
		return c.Send("天数格式错误。")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var account registration.Account
	var newExpiresAt *time.Time

	tgID, parseErr := strconv.ParseInt(target, 10, 64)
	var renewErr error
	if parseErr == nil && tgID > 0 {
		account, newExpiresAt, renewErr = r.adm.RenewByTelegramID(ctx, tgID, deltaDays)
	} else {
		account, newExpiresAt, renewErr = r.adm.RenewByEmbyUsername(ctx, target, deltaDays)
	}
	if renewErr != nil {
		if errors.Is(renewErr, registration.ErrNotFound) {
			return c.Send("未找到该用户。")
		}
		if errors.Is(renewErr, adminapp.ErrUserNotRegistered) {
			return c.Send("该用户尚未注册/绑定 Emby。")
		}
		return c.Send("续期失败：" + userFriendlyError(renewErr))
	}

	expiresAt := "∞"
	if newExpiresAt != nil {
		expiresAt = newExpiresAt.Format("2006-01-02 15:04:05")
	}

	msg := "续期成功：\n\n"
	msg += fmt.Sprintf("TelegramID：`%d`\n", account.TelegramID)
	msg += fmt.Sprintf("EmbyUser：`%s`\n", account.EmbyUsername)
	msg += fmt.Sprintf("续期：`%.1f` 天\n", deltaDays)
	msg += fmt.Sprintf("新到期：`%s`\n", expiresAt)
	msg += "\n已尝试私聊通知用户（若用户未 /start 过可能失败）。"

	userMsg := formatPushedCard(
		"✅ 续期成功",
		fmt.Sprintf("👤 TGID：`%d`", account.TelegramID),
		fmt.Sprintf("🎬 Emby 用户名：`%s`", account.EmbyUsername),
		fmt.Sprintf("📅 续期：`%.1f` 天", deltaDays),
		fmt.Sprintf("⏳ 新到期：`%s`", expiresAt),
	)
	if err := r.trySendToUser(c.Bot(), account.TelegramID, userMsg); err != nil {
		msg += "\n发送给用户失败：" + userFriendlyError(err)
	}

	return c.Send(msg, telebot.ModeMarkdown)
}

func (r *Router) handleAdminRenewAllCmd(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Send("请私聊我使用该功能。")
	}
	if !r.isAdminSender(c) {
		return c.Send("无权限。")
	}
	if r.adm == nil {
		return c.Send("管理员功能尚未初始化。")
	}

	args := cmdArgs(c)
	if len(args) < 1 {
		return c.Send("用法：/renewall <+/-天数>")
	}
	deltaDays, err := strconv.ParseFloat(args[0], 64)
	if err != nil {
		return c.Send("天数格式错误。")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	updated, skippedUnlimited, err := r.adm.RenewAll(ctx, deltaDays)
	if err != nil {
		return c.Send("批量续期失败：" + userFriendlyError(err))
	}

	msg := "批量续期完成：\n\n"
	msg += fmt.Sprintf("续期：`%.1f` 天\n", deltaDays)
	msg += fmt.Sprintf("成功：`%d`\n", updated)
	if skippedUnlimited > 0 {
		msg += fmt.Sprintf("跳过无限期：`%d`\n", skippedUnlimited)
	}
	msg += "\n注：仅对 level=b 的用户生效。"
	return c.Send(msg, telebot.ModeMarkdown)
}

func (r *Router) handleAdminDeleteCmd(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Send("请私聊我使用该功能。")
	}
	if !r.isAdminSender(c) {
		return c.Send("无权限。")
	}

	args := cmdArgs(c)
	if len(args) < 1 {
		return c.Send("用法：/urm <tg_id> --yes")
	}
	tgID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || tgID <= 0 {
		return c.Send("tg_id 格式错误。")
	}
	if len(args) < 2 || args[1] != "--yes" {
		return c.Send("危险操作：请使用 /urm <tg_id> --yes 以确认删除。")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	account, err := r.adm.DeleteUser(ctx, tgID)
	if err != nil {
		if errors.Is(err, registration.ErrNotFound) {
			return c.Send("未找到该用户。")
		}
		return c.Send("删除失败：" + userFriendlyError(err))
	}

	msg := "已删除：\n\n"
	msg += fmt.Sprintf("TelegramID：`%d`\n", account.TelegramID)
	msg += fmt.Sprintf("EmbyUser：`%s`\n", account.EmbyUsername)
	msg += fmt.Sprintf("EmbyUserID：`%s`\n", account.EmbyUserID)
	return c.Send(msg, telebot.ModeMarkdown)
}

// 管理员用户面板相关的辅助函数已迁移至 panel_admin_users.go

func (r *Router) isAdminSender(c telebot.Context) bool {
	if c == nil || c.Sender() == nil {
		return false
	}
	_, ok := r.admins[c.Sender().ID]
	return ok
}

func (r *Router) trySendToUser(bot *telebot.Bot, telegramID int64, msg string) error {
	if bot == nil || telegramID == 0 {
		return fmt.Errorf("invalid target")
	}
	target := &telebot.User{ID: telegramID}

	// 默认走 Markdown（用于对齐项目内大多数消息模板）。但 Telegram 的 Markdown
	// 对 `_` 等字符很敏感：例如 botUsername/用户名包含下划线时，URL 可能触发
	// "can't parse entities" 导致发送失败。此时降级为纯文本再试一次。
	_, err := bot.Send(target, msg, telebot.ModeMarkdown)
	if err == nil {
		return nil
	}
	if isTelegramMarkdownParseError(err) {
		_, plainErr := bot.Send(target, msg)
		if plainErr == nil {
			return nil
		}
		return plainErr
	}
	return err
}

func isTelegramMarkdownParseError(err error) bool {
	if err == nil {
		return false
	}
	low := strings.ToLower(err.Error())
	return strings.Contains(low, "parse entities") || strings.Contains(low, "end of the entity")
}

func parseTargetTGID(c telebot.Context) (int64, error) {
	args := cmdArgs(c)
	if len(args) < 1 {
		return 0, fmt.Errorf("用法：<命令> <tg_id>")
	}
	tgID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || tgID <= 0 {
		return 0, fmt.Errorf("tg_id 格式错误。")
	}
	return tgID, nil
}

func parseCreateArgs(c telebot.Context) (telegramID int64, telegramUsername string, embyUsername string, err error) {
	args := cmdArgs(c)
	if len(args) < 1 {
		return 0, "", "", fmt.Errorf("用法：/ucr <tg_id> [emby_username]")
	}
	tgID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || tgID <= 0 {
		return 0, "", "", fmt.Errorf("tg_id 格式错误。")
	}
	var e string
	if len(args) >= 2 {
		e = args[1]
	}
	return tgID, "", e, nil
}

func isPrivateChat(c telebot.Context) bool {
	if c == nil || c.Chat() == nil {
		return false
	}
	return c.Chat().Type == telebot.ChatPrivate
}

// userFriendlyError 已迁移至 ui_error.go

func cmdArgs(c telebot.Context) []string {
	if c == nil || c.Message() == nil {
		return nil
	}
	fields := strings.Fields(c.Message().Text)
	if len(fields) <= 1 {
		return nil
	}
	return fields[1:]
}
