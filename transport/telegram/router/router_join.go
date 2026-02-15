package router

import (
	"strings"

	"gopkg.in/telebot.v3"
)

func (r *Router) shouldEnforceJoin() bool {
	if !r.gov.Enabled {
		return false
	}
	return r.gov.RequireGroup || r.gov.RequireChannel
}

func (r *Router) requireJoinMiddleware(next telebot.HandlerFunc) telebot.HandlerFunc {
	return func(c telebot.Context) error {
		if !r.gov.Enabled || (!r.gov.RequireGroup && !r.gov.RequireChannel) {
			return next(c)
		}
		if c == nil || c.Chat() == nil || c.Sender() == nil {
			return next(c)
		}
		if c.Chat().Type != telebot.ChatPrivate {
			return next(c)
		}
		if r.isAdminSender(c) {
			return next(c)
		}

		inGroup, inChannel := r.checkJoin(c.Bot(), c.Sender().ID)
		if inGroup && inChannel {
			return next(c)
		}

		if c.Callback() != nil {
			_ = c.Respond(&telebot.CallbackResponse{Text: "请先加入群组/频道后再使用", ShowAlert: false})
		}
		return r.sendJoinRequired(c, inGroup, inChannel)
	}
}

type chatRecipient string

func (c chatRecipient) Recipient() string { return string(c) }

func (r *Router) checkJoin(bot *telebot.Bot, telegramID int64) (inGroup bool, inChannel bool) {
	inGroup = true
	inChannel = true

	if bot == nil || telegramID == 0 {
		return inGroup, inChannel
	}

	if r.gov.RequireGroup {
		inGroup = r.isUserInAnyGroup(bot, telegramID)
	}
	if r.gov.RequireChannel {
		inChannel = r.isUserInChannel(bot, telegramID)
	}
	return inGroup, inChannel
}

func (r *Router) isUserInAnyGroup(bot *telebot.Bot, telegramID int64) bool {
	if bot == nil || telegramID == 0 {
		// 无法验证时的策略：
		// - 严格模式：拒绝（避免“查不到就放行”带来的绕过）。
		// - 非严格：放行（避免因权限/网络问题导致所有用户无法使用）。
		return !r.gov.Strict
	}
	if len(r.gov.GroupIDs) == 0 {
		// RequireGroup=true 但未配置 GroupIDs 时：
		// - 严格模式：视为未加入（帮助尽早暴露配置错误）。
		// - 非严格：视为通过（保持旧行为的兼容性）。
		return !r.gov.Strict
	}

	anySuccess := false
	for _, gid := range r.gov.GroupIDs {
		member, err := bot.ChatMemberOf(&telebot.Chat{ID: gid}, &telebot.User{ID: telegramID})
		if err != nil {
			continue
		}
		anySuccess = true
		if isAllowedGroupRole(member.Role) {
			return true
		}
	}
	if !anySuccess {
		// 无法查询任何一个群（常见原因：bot 不在群里/没权限/群 ID 配错）。
		// 严格模式下拒绝，防止被动绕过；非严格模式保持可用性。
		return !r.gov.Strict
	}
	return false
}

func (r *Router) isUserInChannel(bot *telebot.Bot, telegramID int64) bool {
	if bot == nil || telegramID == 0 {
		return !r.gov.Strict
	}

	chat := r.channelRecipient()
	if chat == nil {
		// RequireChannel=true 但未配置 channel（ID/username）时：
		// - 严格模式：拒绝以提示配置错误。
		// - 非严格：放行保持兼容。
		return !r.gov.Strict
	}

	member, err := bot.ChatMemberOf(chat, &telebot.User{ID: telegramID})
	if err != nil {
		// 常见原因：bot 无权限查看频道成员、频道配置不正确、网络波动等。
		// 严格模式下拒绝，避免“查不到就放行”。
		return !r.gov.Strict
	}
	return isAllowedChannelRole(member.Role)
}

func (r *Router) channelRecipient() telebot.Recipient {
	if r.gov.ChannelID != 0 {
		return &telebot.Chat{ID: r.gov.ChannelID}
	}
	u := strings.TrimSpace(r.gov.ChannelUsername)
	u = strings.TrimPrefix(u, "@")
	if u != "" {
		return chatRecipient("@" + u)
	}
	return nil
}

func isAllowedGroupRole(role telebot.MemberStatus) bool {
	switch role {
	case "creator", "administrator", "member", "restricted":
		return true
	default:
		return false
	}
}

func isAllowedChannelRole(role telebot.MemberStatus) bool {
	switch role {
	case "creator", "administrator", "member":
		return true
	default:
		return false
	}
}

func (r *Router) sendJoinRequired(c telebot.Context, inGroup bool, inChannel bool) error {
	if c == nil {
		return nil
	}

	var need []string
	if r.gov.RequireGroup && !inGroup {
		need = append(need, "群组")
	}
	if r.gov.RequireChannel && !inChannel {
		need = append(need, "频道")
	}
	if len(need) == 0 {
		return nil
	}

	msg := "请先加入 " + strings.Join(need, " + ") + " 后再使用 Bot。\n\n加入后点击：✅ 我已加入，重新验证"

	menu := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	groupLink := strings.TrimSpace(r.gov.MainGroupInviteLink)
	if groupLink == "" {
		u := strings.TrimSpace(r.gov.MainGroupUsername)
		u = strings.TrimPrefix(u, "@")
		if u != "" {
			groupLink = "https://t.me/" + u
		}
	}
	channelLink := strings.TrimSpace(r.gov.ChannelInviteLink)
	if channelLink == "" {
		u := strings.TrimSpace(r.gov.ChannelUsername)
		u = strings.TrimPrefix(u, "@")
		if u != "" {
			channelLink = "https://t.me/" + u
		}
	}

	var links []telebot.Btn
	if channelLink != "" {
		links = append(links, menu.URL("频道", channelLink))
	}
	if groupLink != "" {
		links = append(links, menu.URL("群组", groupLink))
	}
	if len(links) > 0 {
		rows = append(rows, menu.Row(links...))
	}
	rows = append(rows, menu.Row(menu.Data("✅ 我已加入，重新验证", CbRecheckJoin)))
	menu.Inline(rows...)

	return r.editOrSendText(c, msg, menu)
}

func (r *Router) handleRecheckJoin(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	if r.shouldEnforceJoin() {
		inGroup, inChannel := r.checkJoin(c.Bot(), c.Sender().ID)
		if !(inGroup && inChannel) {
			return r.sendJoinRequired(c, inGroup, inChannel)
		}
	}

	return r.updateStartPage(c)
}
