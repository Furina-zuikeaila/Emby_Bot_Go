package router

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	adminapp "emby-bot-new/internal/application/admin"
	"emby-bot-new/internal/application/registration"

	"gopkg.in/telebot.v3"
)

const userInviteRevokeWindow = 7 * 24 * time.Hour

func (r *Router) handleMyHarem(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	if r.invite == nil {
		return r.editOrSendText(c, "邀请功能未初始化，请联系管理员。", r.userNavMenu())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	invites, err := r.invite.ListUserInvites(ctx, c.Sender().ID)
	if err != nil {
		return r.editOrSendText(c, "查询失败："+userFriendlyError(err), r.userNavMenu())
	}

	logOp(c.Sender().ID, "查看后宫", "数量", len(invites))

	if len(invites) == 0 {
		msg := "👑 我的后宫\n\n你还没有成功邀请过任何人。"
		return r.editOrSendText(c, msg, telebot.ModeMarkdown, r.userNavMenu())
	}

	now := time.Now()
	total := len(invites)
	maxShow := 30
	if total < maxShow {
		maxShow = total
	}
	invites = invites[:maxShow]

	var b strings.Builder
	b.WriteString("👑 我的后宫\n")
	b.WriteString("——————————————\n\n")
	b.WriteString(fmt.Sprintf("撤回规则：对方兑换后的 `%d` 天内可撤回；撤回后对方账号会被注销。\n\n", int(userInviteRevokeWindow.Hours()/24)))

	menu := &telebot.ReplyMarkup{}
	rows := make([]telebot.Row, 0, 4)

	maxIDLen := 0
	for _, it := range invites {
		if it.InviteeTelegramID == 0 {
			continue
		}
		if n := len(fmt.Sprintf("%d", it.InviteeTelegramID)); n > maxIDLen {
			maxIDLen = n
		}
	}

	lineNo := 0
	for _, it := range invites {
		lineNo++
		deadline := it.UsedAt.Add(userInviteRevokeWindow)
		status := "已过期"
		if !it.UsedAt.IsZero() && now.Before(deadline) {
			status = "可撤回"
		}

		tg := fmt.Sprintf("%d", it.InviteeTelegramID)
		paddedID := tg
		if maxIDLen > 0 {
			paddedID = fmt.Sprintf("%-*s", maxIDLen, tg)
		}
		usedAt := it.UsedAt.Local().Format("2006-01-02 15:04:05")
		b.WriteString(fmt.Sprintf("%d、ID：`%s` 丨兑换：`%s` 丨%s\n", lineNo, paddedID, usedAt, status))
	}

	if maxShow < total {
		b.WriteString("\n（仅展示前 30 条）\n")
	}
	b.WriteString("\n解散后宫：发送 `/disband` 查看说明。\n")

	rows = append(rows,
		menu.Row(menu.Data("🧹 撤回邀请", CbHaremRevokeIn)),
		menu.Row(
			menu.Data("⬅️ 用户功能", CbUserPanel),
			menu.Data("🏠 主菜单", CbBackMain),
		),
	)
	menu.Inline(rows...)

	return r.editOrSendText(c, b.String(), telebot.ModeMarkdown, menu)
}

func (r *Router) handleHaremRevokeInputStart(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	if r.invite == nil {
		return r.editOrSendText(c, "邀请功能未初始化，请联系管理员。", r.userNavMenu())
	}

	r.state.Set(c.Sender().ID, convoHaremRevokeInput, nil)
	msg := "🧹 撤回邀请\n\n请输入要撤回的 TGID（纯数字）。\n\n提示：仅支持撤回你自己邀请成功过的用户，且需要在 7 天内。"
	return r.editOrSendText(c, msg, r.userNavMenu())
}

func (r *Router) handleHaremRevokeTargetInput(c telebot.Context, text string) error {
	if !isPrivateChat(c) {
		return nil
	}
	if c == nil || c.Sender() == nil {
		return nil
	}
	targets := uniqueInt64(parseTelegramIDs(text))
	if len(targets) != 1 {
		return r.editOrSendText(c, "请输入 1 个 TGID（纯数字）。", r.userNavMenu())
	}
	targetID := targets[0]
	if targetID <= 0 {
		return r.editOrSendText(c, "目标 TGID 无效。", r.userNavMenu())
	}

	r.state.Clear(c.Sender().ID)
	return r.renderHaremRevokePreview(c, targetID)
}

func (r *Router) renderHaremRevokePreview(c telebot.Context, targetID int64) error {
	if !isPrivateChat(c) {
		return nil
	}
	if c == nil || c.Sender() == nil {
		return nil
	}
	if r.invite == nil {
		return r.editOrSendText(c, "邀请功能未初始化，请联系管理员。", r.userNavMenu())
	}
	if r.adm == nil {
		return r.editOrSendText(c, "注销功能未初始化，请联系管理员。", r.userNavMenu())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	it, err := r.invite.GetUserInvite(ctx, c.Sender().ID, targetID)
	if err != nil {
		return r.editOrSendText(c, "查询失败："+userFriendlyError(err), r.userNavMenu())
	}
	if it == nil || it.InviteeTelegramID == 0 {
		return r.editOrSendText(c, "未找到该邀请记录。", r.userNavMenu())
	}

	deadline := it.UsedAt.Add(userInviteRevokeWindow)
	if it.UsedAt.IsZero() || time.Now().After(deadline) {
		return r.editOrSendText(c, "已超过可撤回时间（7 天），无法撤回。", r.userNavMenu())
	}

	msg := strings.Join([]string{
		"⚠️ 撤回邀请确认",
		"",
		fmt.Sprintf("目标 TGID：`%d`", it.InviteeTelegramID),
		fmt.Sprintf("兑换时间：`%s`", it.UsedAt.Local().Format("2006-01-02 15:04:05")),
		fmt.Sprintf("可撤回截止：`%s`", deadline.Local().Format("2006-01-02 15:04:05")),
		"",
		"确认撤回后：",
		"- 会撤回该邀请关系（邀请码使用记录会被清空）；",
		"- 会注销对方账号（删除 DB 记录并尝试删除 Emby 用户）。",
	}, "\n")

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(menu.Data("✅ 确认撤回并注销", CbHaremConfirm, strconv.FormatInt(targetID, 10))),
		menu.Row(
			menu.Data("⬅️ 返回后宫", CbMyHarem),
			menu.Data("🏠 主菜单", CbBackMain),
		),
	)
	return r.editOrSendText(c, msg, telebot.ModeMarkdown, menu)
}

func (r *Router) handleHaremRevokePreview(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	targetID, err := strconv.ParseInt(strings.TrimSpace(c.Data()), 10, 64)
	if err != nil || targetID <= 0 {
		return nil
	}
	return r.renderHaremRevokePreview(c, targetID)
}

func (r *Router) handleHaremRevokeConfirm(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	targetID, err := strconv.ParseInt(strings.TrimSpace(c.Data()), 10, 64)
	if err != nil || targetID <= 0 {
		return nil
	}

	if r.invite == nil {
		return r.editOrSendText(c, "邀请功能未初始化，请联系管理员。", r.userNavMenu())
	}
	if r.adm == nil {
		return r.editOrSendText(c, "注销功能未初始化，请联系管理员。", r.userNavMenu())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	it, err := r.invite.GetUserInvite(ctx, c.Sender().ID, targetID)
	if err != nil {
		return r.editOrSendText(c, "查询失败："+userFriendlyError(err), r.userNavMenu())
	}
	if it == nil || it.InviteeTelegramID == 0 {
		return r.editOrSendText(c, "未找到该邀请记录。", r.userNavMenu())
	}

	deadline := it.UsedAt.Add(userInviteRevokeWindow)
	if it.UsedAt.IsZero() || time.Now().After(deadline) {
		return r.editOrSendText(c, "已超过可撤回时间（7 天），无法撤回。", r.userNavMenu())
	}

	// 先注销账号（若已不存在则视为成功），再撤回邀请记录。
	var deletedEmby string
	if acc, delErr := r.adm.DeleteUser(ctx, targetID); delErr != nil {
		if !errors.Is(delErr, adminapp.ErrUserNotRegistered) && !errors.Is(delErr, registration.ErrNotFound) {
			logOp(c.Sender().ID, "撤回邀请", "目标", targetID, "结果", "注销失败", "错误", userFriendlyError(delErr))
			return r.editOrSendText(c, "注销失败："+userFriendlyError(delErr), r.userNavMenu())
		}
	} else {
		deletedEmby = strings.TrimSpace(acc.EmbyUsername)
	}

	revoked, revokeErr := r.invite.RevokeUserInvite(ctx, c.Sender().ID, targetID)
	if revokeErr != nil {
		logOp(c.Sender().ID, "撤回邀请", "目标", targetID, "结果", "撤回记录失败", "错误", userFriendlyError(revokeErr))
		return r.editOrSendText(c, "撤回失败："+userFriendlyError(revokeErr), r.userNavMenu())
	}
	if revoked == nil {
		logOp(c.Sender().ID, "撤回邀请", "目标", targetID, "结果", "记录不存在")
		return r.editOrSendText(c, "未找到该邀请记录。", r.userNavMenu())
	}

	// 最佳努力通知对方（对方未私聊 /start 过可能会失败）。
	if c.Bot() != nil {
		notify := "⚠️ 你的邀请已被撤回，账号已被注销。"
		_ = r.trySendToUser(c.Bot(), targetID, notify)
	}

	logOp(c.Sender().ID, "撤回邀请", "目标", targetID, "结果", "成功", "兑换时间", revoked.UsedAt.Format(time.RFC3339), "EmbyUser", deletedEmby)
	return r.handleMyHarem(c)
}
