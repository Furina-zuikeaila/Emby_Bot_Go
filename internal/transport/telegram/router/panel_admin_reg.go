package router

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleAdminRegPanel(c telebot.Context) error {
	if !isPrivateChat(c) {
		return r.editOrSendText(c, "请私聊我使用该功能。")
	}
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}
	if r.regAdmin == nil {
		return r.editOrSendText(c, "注册管理尚未初始化。")
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	r.tryRemoveReplyKeyboard(c)
	return r.sendRegPanel(c)
}

func (r *Router) sendRegPanel(c telebot.Context) error {
	return r.sendRegPanelWithMessageID(c, 0)
}

func (r *Router) sendRegPanelWithMessageID(c telebot.Context, messageID int) error {
	if c.Sender() == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	settings, err := r.regAdmin.GetSettings(ctx)
	if err != nil {
		return r.editOrSendText(c, "读取设置失败："+userFriendlyError(err))
	}

	mode := formatServiceMode(r.getCurrentServiceMode(ctx))

	effectiveEnabled := settings.Enabled
	if settings.OpenUntil != nil && time.Now().After(*settings.OpenUntil) {
		effectiveEnabled = false
	}

	gate, err := r.reg.Gate(ctx, c.Sender().ID, c.Sender().Username)
	if err != nil {
		return r.editOrSendText(c, "读取注册状态失败："+userFriendlyError(err))
	}

	openUntilStr := "无"
	if settings.OpenUntil != nil {
		openUntilStr = settings.OpenUntil.Format("2006-01-02 15:04:05")
		if time.Now().Before(*settings.OpenUntil) {
			openUntilStr += fmt.Sprintf("（剩余 %d min）", int(time.Until(*settings.OpenUntil).Minutes()))
		} else {
			openUntilStr += "（已过期）"
		}
	}

	maxUsersStr := "无限制"
	remainingStr := "∞"
	if settings.MaxUsers >= 0 {
		maxUsersStr = fmt.Sprintf("%d", settings.MaxUsers)
		remaining := settings.MaxUsers - gate.CurrentUsers
		if remaining < 0 {
			remaining = 0
		}
		remainingStr = fmt.Sprintf("%d", remaining)
	}

	defaultDaysStr := fmt.Sprintf("%d", settings.DefaultDays)
	if settings.DefaultDays <= 0 {
		defaultDaysStr = "0"
	}

	freeRegFlag := "❌"
	if effectiveEnabled {
		freeRegFlag = "✅"
	}
	timingFlag := "❌"
	if settings.OpenUntil != nil && time.Now().Before(*settings.OpenUntil) {
		timingFlag = "✅"
	}

	quotaLabel := "无限制"
	if settings.MaxUsers >= 0 {
		quotaLabel = fmt.Sprintf("%s 人", maxUsersStr)
	}

	var b strings.Builder
	b.WriteString("🛂 注册管理中心\n")
	b.WriteString("――――――――――――――――\n\n")
	b.WriteString("📊 系统状态\n")
	b.WriteString(fmt.Sprintf("🌐 服务模式：%s\n", mode))
	b.WriteString(fmt.Sprintf(" 🆓 自由注册：%s\n", freeRegFlag))
	b.WriteString(fmt.Sprintf("⏰ 定时注册：%s\n", timingFlag))
	if settings.OpenUntil != nil {
		b.WriteString(fmt.Sprintf("🕒 定时截止：%s\n", openUntilStr))
	}
	b.WriteString(fmt.Sprintf("🚧 注册限制：%s\n\n", quotaLabel))

	b.WriteString(fmt.Sprintf("✅ 注册人数：%d\n", gate.CurrentUsers))
	if settings.MaxUsers >= 0 {
		b.WriteString(fmt.Sprintf("🟡 剩余名额：%s\n", remainingStr))
	} else {
		b.WriteString("🟡 剩余名额：∞\n")
	}
	b.WriteString(fmt.Sprintf("📅 默认天数：%s\n", defaultDaysStr))

	msg := b.String()

	if messageID > 0 {
		if err := r.editByMessageIDAuto(c, messageID, msg, telebot.ModeMarkdown, r.buildRegMenu(effectiveEnabled)); err == nil {
			return nil
		}
	}
	return r.editOrSendText(c, msg, telebot.ModeMarkdown, r.buildRegMenu(effectiveEnabled))
}

func (r *Router) buildRegMenu(enabled bool) *telebot.ReplyMarkup {
	menu := &telebot.ReplyMarkup{}

	toggle := "❌ 自由注册"
	if enabled {
		toggle = "✅ 自由注册"
	}

	menu.Inline(
		menu.Row(menu.Data(toggle, CbRegToggle), menu.Data("⏰ 定时注册", CbRegSetTiming)),
		menu.Row(menu.Data("设置名额", CbRegSetMaxUsers), menu.Data("默认天数", CbRegSetDefaultDays)),
		menu.Row(menu.Data("返回面板", CbAdminPanel)),
	)
	return menu
}
