package router

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	accountapp "emby-bot-new/internal/application/account"
	"emby-bot-new/internal/application/registration"

	"gopkg.in/telebot.v3"
)

func buildAdminUserDetailMarkdown(account *registration.Account) string {
	if account == nil {
		return "👤 用户详情\n\n（无）"
	}
	var b strings.Builder
	b.WriteString("👤 用户详情\n")
	b.WriteString("——————————————\n")
	b.WriteString(fmt.Sprintf("🆔 TG ID：`%d`\n", account.TelegramID))
	if strings.TrimSpace(account.TelegramUsername) != "" {
		b.WriteString(fmt.Sprintf("👤 Telegram：`@%s`\n", safeInlineCode(strings.TrimPrefix(strings.TrimSpace(account.TelegramUsername), "@"))))
	} else {
		b.WriteString("👤 Telegram：`无`\n")
	}
	emby := strings.TrimSpace(account.EmbyUsername)
	if emby == "" {
		emby = "未注册"
	}
	b.WriteString(fmt.Sprintf("🎬 Emby：`%s`\n", safeInlineCode(emby)))
	b.WriteString(fmt.Sprintf("⏳ 到期时间：`%s`\n", formatExpiresAt(account.ExpiresAt)))
	b.WriteString(fmt.Sprintf("🕒 上次活跃：`%s`\n", formatLastActive(account.LastPlayedAt)))
	if account.IsWhitelist {
		b.WriteString("⭐ 白名单：`是`\n")
	} else {
		b.WriteString("⭐ 白名单：`否`\n")
	}
	return b.String()
}

func (r *Router) jumpToAdminUserDetail(c telebot.Context, tgID int64, offset int) error {
	if c == nil || c.Sender() == nil {
		return nil
	}
	// 尽量编辑“用户管理中心”的原消息（由 sendAdminUsers 写入 state）。
	messageID := 0
	if sess, ok := r.state.Get(c.Sender().ID); ok {
		if v := strings.TrimSpace(sess.Values["edit_message_id"]); v != "" {
			if id, err := strconv.Atoi(v); err == nil && id > 0 {
				messageID = id
			}
		}
	}
	return r.sendAdminUserFullDetailWithMessageID(c, messageID, tgID, offset)
}

func (r *Router) sendAdminUserFullDetailWithMessageID(c telebot.Context, messageID int, tgID int64, offset int) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}
	if r.adm == nil {
		return r.editOrSendText(c, "管理员功能尚未初始化。", r.menus.Admin)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	account, err := r.adm.GetUser(ctx, tgID)
	if err != nil {
		if errors.Is(err, registration.ErrNotFound) {
			return r.editOrSendText(c, "未找到该用户。", r.menus.Admin)
		}
		return r.editOrSendText(c, "查询失败："+userFriendlyError(err), r.menus.Admin)
	}
	// 自动过滤未注册用户（无 Emby 绑定）；深链仍可被触发，所以这里给出明确提示并返回列表。
	if account == nil || strings.TrimSpace(account.EmbyUserID) == "" {
		if messageID > 0 {
			_ = r.editByMessageIDAuto(c, messageID, "该用户未注册（已自动过滤）。", r.menus.Admin)
		}
		return r.sendAdminUsers(c, strconv.Itoa(offset))
	}

	// 扩展信息：设备/会话、播放记录、审计（删号/违规/警告等）
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
	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(menu.Data("⬅️ 返回用户管理", CbAdminUsers, strconv.Itoa(offset))),
		menu.Row(menu.Data("返回面板", CbAdminPanel)),
	)

	if messageID > 0 {
		if err := r.editByMessageIDAuto(c, messageID, msg, telebot.ModeMarkdown, menu); err == nil {
			return nil
		}
	}
	return r.editOrSendText(c, msg, telebot.ModeMarkdown, menu)
}

func buildAdminUserFullDetailMarkdown(account *registration.Account, sessions []accountapp.Session, history []accountapp.HistoryItem, events []registration.AuditEvent) string {
	var b strings.Builder
	b.WriteString(buildAdminUserDetailMarkdown(account))

	b.WriteString("——————————————\n")
	b.WriteString("🌐 IP 记录（在线会话，最多 8 条）\n")
	if len(sessions) == 0 {
		b.WriteString("无\n")
	} else {
		for i, s := range sessions {
			if i >= 8 {
				b.WriteString("...\n")
				break
			}
			name := strings.TrimSpace(s.DeviceName)
			if name == "" {
				name = "设备"
			}
			client := strings.TrimSpace(s.Client)
			ip := strings.TrimSpace(s.RemoteEndPoint)
			line := "- " + name
			if client != "" {
				line += " (" + client + ")"
			}
			if ip != "" {
				line += fmt.Sprintf("  IP：`%s`", ip)
			}
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("——————————————\n")
	b.WriteString("🎬 最近播放（30 天内，最多 5 条）\n")
	if len(history) == 0 {
		b.WriteString("无\n")
	} else {
		for i, it := range history {
			if i >= 5 {
				break
			}
			name := strings.TrimSpace(it.Name)
			if name == "" {
				name = "未知"
			}
			at := "未知"
			if it.LastPlayedAt != nil {
				at = it.LastPlayedAt.Local().Format("2006-01-02 15:04:05")
			}
			b.WriteString(fmt.Sprintf("- %s  `(%s)`\n", name, at))
		}
	}

	b.WriteString("——————————————\n")
	b.WriteString("🧾 违规 / 删号 / 警告（最近 10 条）\n")
	if len(events) == 0 {
		b.WriteString("无\n")
	} else {
		for i, e := range events {
			if i >= 10 {
				break
			}
			at := e.CreatedAt.Local().Format("2006-01-02 15:04:05")
			cat := strings.TrimSpace(e.Category)
			act := strings.TrimSpace(e.Action)
			reason := strings.TrimSpace(e.Reason)
			if reason == "" {
				reason = "-"
			}
			b.WriteString(fmt.Sprintf("- `%s` `%s/%s` %s\n", at, cat, act, reason))
		}
	}
	return b.String()
}

func (r *Router) sendAdminUsers(c telebot.Context, offsetStr string) error {
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}

	offset := 0
	if strings.TrimSpace(offsetStr) != "" {
		if v, err := strconv.Atoi(strings.TrimSpace(offsetStr)); err == nil && v >= 0 {
			offset = v
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const limit = 10
	users, err := r.adm.ListUsers(ctx, limit, offset)
	if err != nil {
		return r.editOrSendText(c, "查询失败："+userFriendlyError(err), r.menus.Admin)
	}
	if len(users) == 0 {
		return r.editOrSendText(c, "暂无用户。", r.menus.Admin)
	}

	var b strings.Builder
	b.WriteString("👥 用户管理中心\n")
	b.WriteString("——————————————\n\n")

	maxIDLen := 0
	for _, u := range users {
		if u.TelegramID == 0 {
			continue
		}
		if n := len(fmt.Sprintf("%d", u.TelegramID)); n > maxIDLen {
			maxIDLen = n
		}
	}

	for i, u := range users {
		tg := fmt.Sprintf("%d", u.TelegramID)
		emby := strings.TrimSpace(u.EmbyUsername)
		if emby == "" {
			emby = "未注册"
		}
		paddedID := tg
		if maxIDLen > 0 {
			paddedID = fmt.Sprintf("%-*s", maxIDLen, tg)
		}
		b.WriteString(fmt.Sprintf("%d、ID：`%s` 丨🎬 Emby：`%s`\n", offset+i+1, paddedID, safeInlineCode(emby)))
	}

	menu := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	prevOffset := offset - limit
	if prevOffset < 0 {
		prevOffset = 0
	}
	nextOffset := offset + limit
	rows = append(rows, menu.Row(
		menu.Data("上一页", CbAdminUsers, strconv.Itoa(prevOffset)),
		menu.Data("下一页", CbAdminUsers, strconv.Itoa(nextOffset)),
	))
	rows = append(rows, menu.Row(menu.Data("返回面板", CbAdminPanel)))
	menu.Inline(rows...)

	// 记录“用户管理中心”面板的 message_id，供 /start 深链跳转后回写编辑（避免发送新消息）。
	// 注意：仅在回调编辑场景最可靠；若通过 /users 命令触发，则 message_id 可能来自用户消息，无法编辑。
	r.setAdminConvo(c, convoNone, "admin_users")

	return r.editOrSendText(c, b.String(), telebot.ModeMarkdown, menu)
}

func (r *Router) sendAdminHelp(c telebot.Context) error {
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}
	msg := "管理员命令：\n\n"
	msg += "- /admin：打开管理员面板\n"
	msg += "- /users [offset]：用户管理（默认 0）\n"
	msg += "- /user <tg_id>：查询用户\n"
	msg += "- /ucr <tg_id> [emby_username]：创建用户\n"
	msg += "- /resetpass <tg_id>：重置密码\n"
	msg += "- /renew <tg_id|emby_username> <+/-天数>：续期/调整到期\n"
	msg += "- /renewall <+/-天数>：批量续期（level=b）\n"
	msg += "- /urm <tg_id> --yes：删除用户\n"
	return r.editOrSendText(c, msg, r.menus.Admin)
}
