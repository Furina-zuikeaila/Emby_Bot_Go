package router

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/telebot.v3"
)

func (r *Router) sendMainMenu(c telebot.Context, text string) error {
	if c == nil {
		return nil
	}
	// 优先更新主面板锚点消息，避免出现旧菜单/多套面板。
	if c.Sender() != nil {
		if anchor, ok := r.ui.Get(c.Sender().ID); ok {
			if anchor.IsMedia {
				if err := r.editCaptionByMessageID(c, anchor.MessageID, text, r.mainPanelMenu(c)); err == nil {
					return nil
				}
			} else {
				if err := r.editByMessageID(c, anchor.MessageID, text, r.mainPanelMenu(c)); err == nil {
					return nil
				}
			}
		}
	}
	return r.editOrSendText(c, text, r.mainPanelMenu(c))
}

func (r *Router) sendMainMenuMarkdown(c telebot.Context, text string) error {
	if c == nil {
		return nil
	}
	// 同 sendMainMenu：优先更新主面板锚点消息。
	if c.Sender() != nil {
		if anchor, ok := r.ui.Get(c.Sender().ID); ok {
			if anchor.IsMedia {
				if err := r.editCaptionByMessageID(c, anchor.MessageID, text, telebot.ModeMarkdown, r.mainPanelMenu(c)); err == nil {
					return nil
				}
			} else {
				if err := r.editByMessageID(c, anchor.MessageID, text, telebot.ModeMarkdown, r.mainPanelMenu(c)); err == nil {
					return nil
				}
			}
		}
	}
	return r.editOrSendText(c, text, telebot.ModeMarkdown, r.mainPanelMenu(c))
}

func (r *Router) cancelMenu() *telebot.ReplyMarkup {
	menu := &telebot.ReplyMarkup{}
	menu.Inline(menu.Row(menu.Data("取消", CbCancel)))
	return menu
}

func (r *Router) editOrSendText(c telebot.Context, text string, opts ...interface{}) error {
	if c == nil {
		return nil
	}

	// 处理 callback 时，优先把“当前点击的这条消息”作为锚点：
	// 否则会出现“按钮点了没反应”的错觉（实际内容被编辑到一条较早的锚点消息上，用户未滚动到那里）。
	if isPrivateChat(c) && c.Callback() != nil && c.Sender() != nil && c.Message() != nil && r != nil && r.ui != nil {
		if c.Bot() != nil && c.Bot().Me != nil && c.Message().Sender != nil && c.Message().Sender.ID == c.Bot().Me.ID {
			r.ui.Set(c.Sender().ID, c.Message().ID, isMediaMessage(c.Message()))
		}
	}

	// 在私聊场景：优先把所有导航类输出“收敛”到主面板锚点消息，避免出现旧的多套菜单。
	// 典型问题：用户在历史消息上点击“返回/导航”按钮，会把新菜单编辑到旧消息上，从而产生多套面板。
	if isPrivateChat(c) && c.Sender() != nil && c.Message() != nil && r != nil && r.ui != nil {
		if anchor, ok := r.ui.Get(c.Sender().ID); ok && anchor.MessageID > 0 && c.Message().ID != anchor.MessageID {
			if err := r.editByMessageIDAuto(c, anchor.MessageID, text, opts...); err == nil {
				// 清理旧消息（若是机器人消息）
				if c.Bot() != nil && c.Bot().Me != nil && c.Message().Sender != nil && c.Message().Sender.ID == c.Bot().Me.ID {
					_ = c.Delete()
				}
				return nil
			}
		}
	}

	if err := r.editTextOrCaption(c, text, opts...); err != nil {
		if isMessageNotModified(err) {
			return nil
		}
		if err == telebot.ErrBadContext {
			return c.Send(text, opts...)
		}
		sendErr := c.Send(text, opts...)
		if sendErr != nil {
			return sendErr
		}
		if c.Message() != nil && c.Bot() != nil && c.Bot().Me != nil && c.Message().Sender != nil {
			if c.Message().Sender.ID == c.Bot().Me.ID {
				_ = c.Delete()
			}
		}
		return nil
	}
	return nil
}

func (r *Router) editByMessageID(c telebot.Context, messageID int, text string, opts ...interface{}) error {
	if c == nil || c.Bot() == nil || c.Chat() == nil || messageID <= 0 {
		return telebot.ErrBadContext
	}
	msg := &telebot.Message{ID: messageID, Chat: c.Chat()}
	_, err := c.Bot().Edit(msg, text, opts...)
	return err
}

// editByMessageIDAuto 尝试按 message_id 修改“文本消息”，失败后再尝试修改“媒体消息的 caption”。
// 管理员面板可能挂在 /start 的图文消息下方（Photo+Caption）。在输入流程完成后如果通过 message_id 回到原面板，
// 仅调用 Edit（文本）会导致媒体消息无法更新，从而出现“导航漂移/按钮异常（发出新消息或回退失败）”。
func (r *Router) editByMessageIDAuto(c telebot.Context, messageID int, text string, opts ...interface{}) error {
	if messageID <= 0 {
		return telebot.ErrBadContext
	}
	if err := r.editByMessageID(c, messageID, text, opts...); err == nil {
		return nil
	}
	return r.editCaptionByMessageID(c, messageID, text, opts...)
}

func (r *Router) editCaptionByMessageID(c telebot.Context, messageID int, caption string, opts ...interface{}) error {
	if c == nil || c.Bot() == nil || c.Chat() == nil || messageID <= 0 {
		return telebot.ErrBadContext
	}
	msg := &telebot.Message{ID: messageID, Chat: c.Chat()}
	_, err := c.Bot().EditCaption(msg, caption, opts...)
	return err
}

func (r *Router) editWithSessionMessage(c telebot.Context, sess convoSession, text string, opts ...interface{}) error {
	messageID := 0
	if v := strings.TrimSpace(sess.Values["edit_message_id"]); v != "" {
		if id, err := strconv.Atoi(v); err == nil && id > 0 {
			messageID = id
		}
	}
	editIsMedia := false
	if v := strings.TrimSpace(sess.Values["edit_is_media"]); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			editIsMedia = b
		}
	}
	if messageID > 0 {
		var err error
		if editIsMedia {
			err = r.editCaptionByMessageID(c, messageID, text, opts...)
		} else {
			err = r.editByMessageID(c, messageID, text, opts...)
		}
		if err == nil {
			return nil
		}
	}
	return r.editOrSendText(c, text, opts...)
}

func (r *Router) setAdminConvo(c telebot.Context, state convoState, panel string) {
	if c == nil || c.Sender() == nil {
		return
	}
	values := map[string]string{"panel": panel}
	if c.Message() != nil {
		values["edit_message_id"] = strconv.Itoa(c.Message().ID)
		values["edit_is_media"] = strconv.FormatBool(isMediaMessage(c.Message()))
	}
	r.state.Set(c.Sender().ID, state, values)
}

// setUserConvo 用于记录用户会话并尽量保留需要编辑的消息 ID。
func (r *Router) setUserConvo(c telebot.Context, state convoState, sess convoSession, values map[string]string) {
	if c == nil || c.Sender() == nil {
		return
	}
	if values == nil {
		values = map[string]string{}
	}
	if v := strings.TrimSpace(sess.Values["edit_message_id"]); v != "" {
		values["edit_message_id"] = v
		values["edit_is_media"] = strings.TrimSpace(sess.Values["edit_is_media"])
	} else if c.Message() != nil {
		values["edit_message_id"] = strconv.Itoa(c.Message().ID)
		values["edit_is_media"] = strconv.FormatBool(isMediaMessage(c.Message()))
	}
	r.state.Set(c.Sender().ID, state, values)
}

// upsertUserConvoMessage 会“尽量修改原消息”，如果无法修改（比如用户发来的 /start 消息），则发送新消息并记录其 message_id，
// 后续步骤（输入、成功、失败）都将编辑这条消息，避免不断刷屏。
func (r *Router) upsertUserConvoMessage(c telebot.Context, state convoState, sess convoSession, values map[string]string, text string, opts ...interface{}) error {
	if c == nil || c.Sender() == nil || c.Bot() == nil || c.Chat() == nil {
		return nil
	}
	if values == nil {
		values = map[string]string{}
	}

	// 只有“机器人自己发的消息”才能被编辑；用户发送的消息（/start、输入内容）不能被机器人编辑。
	canEditCurrent := false
	if c.Message() != nil && c.Message().Sender != nil && c.Bot().Me != nil {
		canEditCurrent = c.Message().Sender.ID == c.Bot().Me.ID
	}

	if canEditCurrent {
		r.setUserConvo(c, state, sess, values)
		if err := r.editTextOrCaption(c, text, opts...); err == nil {
			return nil
		}
	}

	sent, err := c.Bot().Send(c.Chat(), text, opts...)
	if err != nil {
		return err
	}
	if c.Message() != nil && c.Bot().Me != nil && c.Message().Sender != nil {
		if c.Message().Sender.ID == c.Bot().Me.ID {
			_ = c.Delete()
		}
	}
	values["edit_message_id"] = strconv.Itoa(sent.ID)
	values["edit_is_media"] = strconv.FormatBool(isMediaMessage(sent))
	r.state.Set(c.Sender().ID, state, values)
	return nil
}

func (r *Router) returnAdminPanel(c telebot.Context, sess convoSession) error {
	panel := strings.TrimSpace(sess.Values["panel"])
	messageID := 0
	if v := strings.TrimSpace(sess.Values["edit_message_id"]); v != "" {
		if id, err := strconv.Atoi(v); err == nil && id > 0 {
			messageID = id
		}
	}
	switch panel {
	case "community":
		return r.sendCommunityModePanelWithMessageID(c, messageID)
	case "whitelist":
		return r.sendWhitelistPanelWithMessageID(c, messageID)
	case "keys":
		return r.sendKeyPanelWithMessageID(c, messageID)
	default:
		return r.sendRegPanelWithMessageID(c, messageID)
	}
}

func (r *Router) notifyRegistrationChange(c telebot.Context, msg string) {
	if msg == "" || c == nil || c.Bot() == nil {
		return
	}
	// 群内通知使用“注册管理中心”样式，并包含：注册上限、剩余名额、（若定时注册则包含剩余时间）。
	now := time.Now()
	broadcast := ""
	if r.regAdmin != nil && r.adm != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		settings, _ := r.regAdmin.GetSettings(ctx)
		stats, _ := r.adm.Stats(ctx)

		enabled := settings.Enabled
		timingEnabled := false
		timeLeft := ""
		if settings.OpenUntil != nil && now.Before(*settings.OpenUntil) {
			timingEnabled = true
			d := time.Until(*settings.OpenUntil)
			if d < 0 {
				d = 0
			}
			// 统一按分钟展示，避免过长。
			mins := int(d.Minutes())
			timeLeft = fmt.Sprintf("%d min", mins)
		}

		maxStr := "未知"
		remainingStr := "未知"
		if settings.MaxUsers < 0 {
			maxStr = "无限制"
			remainingStr = "∞"
		} else {
			maxStr = fmt.Sprintf("%d 人", settings.MaxUsers)
			remaining := settings.MaxUsers - stats.RegisteredUsers
			if remaining < 0 {
				remaining = 0
			}
			remainingStr = fmt.Sprintf("%d 人", remaining)
		}

		regIcon := "❌ 关闭"
		if enabled {
			regIcon = "✅ 开放"
		}
		timingIcon := "❌ 关闭"
		if timingEnabled {
			timingIcon = "✅ 开启"
		}

		var b strings.Builder
		b.WriteString("🎫 注册管理中心\n")
		b.WriteString("——————————————\n")
		b.WriteString("📣 通知：" + strings.TrimSpace(msg) + "\n")
		b.WriteString("——————————————\n")
		b.WriteString("📌 系统状态\n")
		b.WriteString("🎟 注册状态：" + regIcon + "\n")
		b.WriteString("⏰ 定时注册：" + timingIcon + "\n")
		b.WriteString("——————————————\n")
		b.WriteString("📊 名额信息\n")
		b.WriteString("👥 注册上限：" + maxStr + "\n")
		b.WriteString("🧾 剩余名额：" + remainingStr + "\n")
		if timingEnabled && timeLeft != "" {
			b.WriteString("⏳ 剩余时间：" + timeLeft + "\n")
		}
		broadcast = b.String()
	} else {
		broadcast = msg
	}

	bot := c.Bot()
	for _, gid := range r.gov.GroupIDs {
		if gid == 0 {
			continue
		}
		_, _ = bot.Send(&telebot.Chat{ID: gid}, broadcast)
	}
	if len(r.gov.GroupIDs) > 0 {
		return
	}
	u := strings.TrimSpace(r.gov.MainGroupUsername)
	u = strings.TrimPrefix(u, "@")
	if u == "" {
		return
	}
	_, _ = bot.Send(&telebot.Chat{Username: "@" + u}, broadcast)
}

func (r *Router) editTextOrCaption(c telebot.Context, text string, opts ...interface{}) error {
	if c == nil {
		return telebot.ErrBadContext
	}
	if isMediaMessage(c.Message()) {
		return c.EditCaption(text, opts...)
	}
	return c.Edit(text, opts...)
}

func messageTextOrCaption(m *telebot.Message) string {
	if m == nil {
		return ""
	}
	if m.Caption != "" {
		return m.Caption
	}
	return m.Text
}

func isMessageNotModified(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "message is not modified")
}

// tryRemoveReplyKeyboard 用于隐藏“底部的自定义键盘”（ReplyKeyboard）。
// 我们的面板按钮都使用 InlineKeyboard（嵌入在消息下方），但如果用户之前触发过 ReplyKeyboard，
// Telegram 会把它一直显示在输入框上方，需要通过 RemoveKeyboard 主动关闭。
//
// 注意：remove_keyboard 和 inline_keyboard 不能放在同一个 reply_markup 里，所以这里先做一次“无感知编辑”
// 来关闭底部键盘，然后再由后续的 editOrSendText 渲染真正的 InlineKeyboard。
func (r *Router) tryRemoveReplyKeyboard(c telebot.Context) {
	if c == nil || c.Bot() == nil || c.Chat() == nil || c.Message() == nil || c.Bot().Me == nil {
		return
	}
	// 只有机器人自己发的消息才能编辑；用户发的消息无法编辑。
	if c.Message().Sender == nil || c.Message().Sender.ID != c.Bot().Me.ID {
		return
	}
	cur := messageTextOrCaption(c.Message())
	if cur == "" {
		return
	}
	_ = r.editTextOrCaption(c, cur, &telebot.ReplyMarkup{RemoveKeyboard: true})
}

func isAdminConvo(state convoState) bool {
	return state == convoAdminSetTiming ||
		state == convoAdminSetMaxUsers ||
		state == convoAdminSetDefaultDays ||
		state == convoAdminCreateCodes ||
		state == convoAdminCreateRenewCodes ||
		state == convoAdminGrantQualification ||
		state == convoAdminSetInactiveDuration ||
		state == convoAdminWhitelistAdd ||
		state == convoAdminWhitelistRemove
}

// 管理面板相关的 UI 已拆分到独立文件（panel_admin_reg.go、panel_admin_keys.go、panel_community_mode.go）。

// 已迁移至 panel_community_mode.go

// 已迁移至 panel_community_mode.go

// 已迁移至 panel_community_mode.go

// 已迁移至 panel_community_mode.go

// 已迁移至 panel_community_mode.go

// 已迁移至 panel_community_mode.go

// 已迁移至 panel_community_mode.go
