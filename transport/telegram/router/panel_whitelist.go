package router

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"emby-bot-new/internal/application/registration"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleAdminWhitelistPanel(c telebot.Context) error {
	if !isPrivateChat(c) {
		return r.editOrSendText(c, "请私聊我使用该功能。")
	}
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	r.tryRemoveReplyKeyboard(c)
	return r.sendWhitelistPanel(c)
}

func (r *Router) sendWhitelistPanel(c telebot.Context) error {
	return r.sendWhitelistPanelWithMessageID(c, 0)
}

func (r *Router) sendWhitelistPanelWithMessageID(c telebot.Context, messageID int) error {
	return r.sendWhitelistPanelWithMessageIDNotice(c, messageID, "")
}

func (r *Router) sendWhitelistPanelWithMessageIDNotice(c telebot.Context, messageID int, notice string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	accounts, err := r.listWhitelistUsers(ctx, 200, 2000)
	if err != nil {
		return r.editOrSendText(c, "读取失败："+userFriendlyError(err))
	}

	var b strings.Builder
	b.WriteString("👥 白名单管理\n")
	b.WriteString("——————————————\n")
	b.WriteString("❗️ 白名单仍受【Web 检测】限制\n")
	b.WriteString("——————————————\n\n")

	if strings.TrimSpace(notice) != "" {
		b.WriteString(strings.TrimSpace(notice))
		b.WriteString("\n\n")
	}

	if len(accounts) == 0 {
		b.WriteString("暂无白名单用户。\n")
	} else {
		maxIDLen := 0
		for _, a := range accounts {
			if a.TelegramID == 0 {
				continue
			}
			if n := len(fmt.Sprintf("%d", a.TelegramID)); n > maxIDLen {
				maxIDLen = n
			}
		}

		for i, a := range accounts {
			tg := fmt.Sprintf("%d", a.TelegramID)
			paddedID := tg
			if maxIDLen > 0 {
				paddedID = fmt.Sprintf("%-*s", maxIDLen, tg)
			}

			emby := strings.TrimSpace(a.EmbyUsername)
			if emby == "" {
				emby = "未注册"
			}

			b.WriteString(fmt.Sprintf("%d、ID：`%s` 丨🎬 Emby：`%s`\n", i+1, paddedID, safeInlineCode(emby)))
		}
	}

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(menu.Data("➕ 添加用户", CbWhitelistAdd), menu.Data("➖ 删除用户", CbWhitelistRemove)),
		menu.Row(menu.Data("返回管理面板", CbAdminPanel)),
	)
	if messageID > 0 {
		if err := r.editByMessageIDAuto(c, messageID, b.String(), menu); err == nil {
			return nil
		}
	}
	return r.editOrSendText(c, b.String(), menu)
}

func (r *Router) listWhitelistUsers(ctx context.Context, pageSize int, maxTotal int) ([]registration.Account, error) {
	if r == nil || r.adm == nil {
		return nil, nil
	}
	if pageSize <= 0 {
		pageSize = 200
	}
	if maxTotal <= 0 {
		maxTotal = 2000
	}

	out := make([]registration.Account, 0, 32)
	offset := 0
	for offset < maxTotal {
		users, err := r.adm.ListUsers(ctx, pageSize, offset)
		if err != nil {
			return nil, err
		}
		if len(users) == 0 {
			break
		}
		for _, u := range users {
			if u.IsWhitelist {
				out = append(out, u)
			}
		}
		offset += pageSize
		if len(users) < pageSize {
			break
		}
	}
	return out, nil
}

func (r *Router) handleAdminWhitelistAdd(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		_ = c.Respond(&telebot.CallbackResponse{Text: "无权限", ShowAlert: false})
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	r.setAdminConvo(c, convoAdminWhitelistAdd, "whitelist")
	return r.editOrSendText(c, "请输入要加入白名单的 TGID（可多个，支持中文逗号/英文逗号/空格分隔）。\n\n点击“取消”返回上一页。", r.cancelMenu())
}

func (r *Router) handleAdminWhitelistRemove(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		_ = c.Respond(&telebot.CallbackResponse{Text: "无权限", ShowAlert: false})
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	r.setAdminConvo(c, convoAdminWhitelistRemove, "whitelist")
	return r.editOrSendText(c, "请输入要从白名单删除的 TGID（可多个，支持中文逗号/英文逗号/空格分隔）。\n\n点击“取消”返回上一页。", r.cancelMenu())
}

func (r *Router) handleAdminWhitelistAddInput(c telebot.Context, text string) error {
	sess, _ := r.state.Get(c.Sender().ID)
	if !r.isAdminSender(c) {
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, "无权限。")
	}
	if r.regAdmin == nil {
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, "白名单模块未初始化。")
	}

	ids := uniqueInt64(parseTelegramIDs(text))
	if len(ids) == 0 {
		return r.editWithSessionMessage(c, sess, "未识别到 TGID，请重新输入。\n\n示例：`123,456 789`", telebot.ModeMarkdown, r.cancelMenu())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	okCnt := 0
	failCnt := 0
	for _, id := range ids {
		if err := r.regAdmin.SetWhitelist(ctx, id, "", true); err != nil {
			failCnt++
			continue
		}
		okCnt++
	}
	r.state.Clear(c.Sender().ID)
	_ = c.Delete()
	messageID := 0
	if v := strings.TrimSpace(sess.Values["edit_message_id"]); v != "" {
		if id, err := strconv.Atoi(v); err == nil && id > 0 {
			messageID = id
		}
	}
	notice := fmt.Sprintf("✅ 添加完成：成功 %d，失败 %d。", okCnt, failCnt)
	return r.sendWhitelistPanelWithMessageIDNotice(c, messageID, notice)
}

func (r *Router) handleAdminWhitelistRemoveInput(c telebot.Context, text string) error {
	sess, _ := r.state.Get(c.Sender().ID)
	if !r.isAdminSender(c) {
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, "无权限。")
	}
	if r.regAdmin == nil {
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, "白名单模块未初始化。")
	}

	ids := uniqueInt64(parseTelegramIDs(text))
	if len(ids) == 0 {
		return r.editWithSessionMessage(c, sess, "未识别到 TGID，请重新输入。\n\n示例：`123,456 789`", telebot.ModeMarkdown, r.cancelMenu())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	okCnt := 0
	failCnt := 0
	for _, id := range ids {
		if err := r.regAdmin.SetWhitelist(ctx, id, "", false); err != nil {
			failCnt++
			continue
		}
		okCnt++
	}
	r.state.Clear(c.Sender().ID)
	_ = c.Delete()
	messageID := 0
	if v := strings.TrimSpace(sess.Values["edit_message_id"]); v != "" {
		if id, err := strconv.Atoi(v); err == nil && id > 0 {
			messageID = id
		}
	}
	notice := fmt.Sprintf("✅ 删除完成：成功 %d，失败 %d。", okCnt, failCnt)
	return r.sendWhitelistPanelWithMessageIDNotice(c, messageID, notice)
}
