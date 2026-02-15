package router

import (
	"context"
	"fmt"
	"time"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleMedia(c telebot.Context) error {
	if isPrivateChat(c) {
		return nil
	}
	return r.handleGroupMessage(c)
}

func (r *Router) handleAddedToGroup(c telebot.Context) error {
	if !r.gov.Enabled || !r.gov.AntiUseBot {
		return nil
	}
	_ = r.maybeLeaveUnauthorizedGroup(c)
	return nil
}

func (r *Router) handleUserLeftGroup(c telebot.Context) error {
	if !r.gov.Enabled || !r.gov.RevokeOnLeave {
		return nil
	}
	if c == nil || c.Chat() == nil || c.Message() == nil || c.Message().UserLeft == nil {
		return nil
	}
	if c.Chat().Type != telebot.ChatGroup && c.Chat().Type != telebot.ChatSuperGroup {
		return nil
	}
	if len(r.gov.GroupIDs) == 0 || !r.isAuthorizedGroupID(c.Chat().ID) {
		return nil
	}
	if r.revoker == nil {
		return nil
	}

	user := c.Message().UserLeft
	if user == nil || user.ID == 0 {
		return nil
	}

	// 批量发放可能包含大量 TGID，这里不限制“整体任务超时”（按用户要求）。
	// 但对每个 TGID 单独设置超时，避免单点卡死导致整个批次一直阻塞。
	ctx := context.Background()

	account, err := r.reg.Me(ctx, user.ID)
	if err != nil || account == nil || account.EmbyUserID == "" || account.ExpiresAt == nil {
		return nil
	}
	if !r.gov.Strict && !account.ExpiresAt.Before(time.Now()) {
		return nil
	}

	if err := r.revoker.RevokeAccount(ctx, user.ID, "left group"); err != nil {
		logOp(user.ID, "退群回收账号", "结果", "失败", "原因", err)
	} else if r.gov.BanOnLeave {
		_ = c.Bot().Ban(c.Chat(), &telebot.ChatMember{User: user, RestrictedUntil: telebot.Forever()}, true)
	}
	return nil
}

func (r *Router) handleGroupMessage(c telebot.Context) error {
	if !r.gov.Enabled {
		return nil
	}
	if c == nil || c.Chat() == nil || c.Message() == nil {
		return nil
	}
	if c.Chat().Type != telebot.ChatGroup && c.Chat().Type != telebot.ChatSuperGroup {
		return nil
	}

	if r.gov.AntiUseBot {
		if r.maybeLeaveUnauthorizedGroup(c) {
			return nil
		}
	}
	if r.gov.AntiChannel {
		_ = r.maybeBanChannelSender(c)
	}
	return nil
}

func (r *Router) isAuthorizedGroupID(chatID int64) bool {
	for _, gid := range r.gov.GroupIDs {
		if gid == chatID {
			return true
		}
	}
	return false
}

func (r *Router) maybeLeaveUnauthorizedGroup(c telebot.Context) bool {
	if c == nil || c.Chat() == nil || c.Bot() == nil {
		return false
	}
	if c.Chat().Type != telebot.ChatGroup && c.Chat().Type != telebot.ChatSuperGroup {
		return false
	}
	if len(r.gov.GroupIDs) == 0 {
		return false
	}
	if r.isAuthorizedGroupID(c.Chat().ID) {
		return false
	}

	r.leavingMu.Lock()
	if _, ok := r.leavingGroups[c.Chat().ID]; ok {
		r.leavingMu.Unlock()
		return true
	}
	r.leavingGroups[c.Chat().ID] = struct{}{}
	r.leavingMu.Unlock()

	bot := c.Bot()
	chat := c.Chat()

	if r.ownerID != 0 {
		_ = r.trySendToUser(bot, r.ownerID, formatPushedCard(
			"⚠️ 未授权群提示",
			fmt.Sprintf("🧩 群ID：`%d`", chat.ID),
			fmt.Sprintf("🏷 群名称：%s", chat.Title),
			"📌 30 秒后将自动退出该群。",
		))
	}
	_, _ = bot.Send(chat, formatPushedCard(
		"❌ 未授权群组",
		fmt.Sprintf("🧩 群ID：`%d`", chat.ID),
		"📌 本 Bot 将在 30 秒后退出，请联系管理员。",
	), telebot.ModeMarkdown)

	go func() {
		time.Sleep(30 * time.Second)
		if err := bot.Leave(chat); err != nil {
			logOp(c.Sender().ID, "退出未授权群", "群ID", chat.ID, "结果", "失败", "原因", err)
		}
	}()
	return true
}

func (r *Router) maybeBanChannelSender(c telebot.Context) bool {
	if c == nil || c.Chat() == nil || c.Message() == nil || c.Bot() == nil {
		return false
	}
	msg := c.Message()
	if msg.SenderChat == nil {
		return false
	}

	senderID := msg.SenderChat.ID
	if senderID == 0 {
		return false
	}
	for _, id := range r.gov.AntiChannelWhitelistIDs {
		if id == senderID {
			return false
		}
	}

	payload := map[string]any{
		"chat_id":        c.Chat().ID,
		"sender_chat_id": senderID,
	}
	if _, err := c.Bot().Raw("banChatSenderChat", payload); err != nil {
		logOp(c.Sender().ID, "封禁频道发言", "群ID", c.Chat().ID, "sender_chat_id", senderID, "结果", "失败", "原因", err)
		return false
	}
	_ = c.Delete()
	return true
}
