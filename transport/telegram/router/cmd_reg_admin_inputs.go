package router

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleAdminSetTimingInput(c telebot.Context, text string) error {
	sess, _ := r.state.Get(c.Sender().ID)
	if !r.isAdminSender(c) {
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, "无权限。")
	}
	minutes, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || minutes < 0 {
		return r.editWithSessionMessage(c, sess, "请输入非负整数分钟，例如：`60`（0 取消定时）。", telebot.ModeMarkdown, r.cancelMenu())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := r.regAdmin.SetTimingMinutes(ctx, minutes); err != nil {
		return r.editWithSessionMessage(c, sess, "更新失败："+userFriendlyError(err))
	}
	r.state.Clear(c.Sender().ID)
	_ = c.Delete()
	if minutes > 0 {
		r.notifyRegistrationChange(c, fmt.Sprintf("⏰ 定时注册已开启（%d min）", minutes))
	} else {
		r.notifyRegistrationChange(c, "⏰ 定时注册已关闭")
	}
	return r.returnAdminPanel(c, sess)
}

func (r *Router) handleAdminSetMaxUsersInput(c telebot.Context, text string) error {
	sess, _ := r.state.Get(c.Sender().ID)
	if !r.isAdminSender(c) {
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, "无权限。")
	}
	maxUsers, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || maxUsers < 0 {
		return r.editWithSessionMessage(c, sess, "请输入非负整数，例如：`10`（0 表示上限为 0）。", telebot.ModeMarkdown, r.cancelMenu())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := r.regAdmin.SetMaxUsers(ctx, maxUsers); err != nil {
		return r.editWithSessionMessage(c, sess, "更新失败："+userFriendlyError(err))
	}
	r.state.Clear(c.Sender().ID)
	_ = c.Delete()
	return r.returnAdminPanel(c, sess)
}

func (r *Router) handleAdminSetDefaultDaysInput(c telebot.Context, text string) error {
	sess, _ := r.state.Get(c.Sender().ID)
	if !r.isAdminSender(c) {
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, "无权限。")
	}
	days, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || days < 0 {
		return r.editWithSessionMessage(c, sess, "请输入非负整数，例如：`30`（0 表示立即过期）。", telebot.ModeMarkdown, r.cancelMenu())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := r.regAdmin.SetDefaultDays(ctx, days); err != nil {
		return r.editWithSessionMessage(c, sess, "更新失败："+userFriendlyError(err))
	}
	r.state.Clear(c.Sender().ID)
	_ = c.Delete()
	return r.returnAdminPanel(c, sess)
}

func (r *Router) handleAdminCreateCodesInput(c telebot.Context, isRenew bool, text string) error {
	sess, _ := r.state.Get(c.Sender().ID)
	if !r.isAdminSender(c) {
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, "无权限。")
	}
	parts := strings.Fields(text)
	if len(parts) != 2 {
		return r.editWithSessionMessage(c, sess, "格式错误，请按 `[天数] [数量]` 输入，例如：`30 5`。", telebot.ModeMarkdown, r.cancelMenu())
	}
	days, err1 := strconv.Atoi(parts[0])
	count, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || days < 0 || count <= 0 || count > 100 {
		return r.editWithSessionMessage(c, sess, "参数错误：天数需>=0，数量 1-100。", telebot.ModeMarkdown, r.cancelMenu())
	}
	if isRenew && days <= 0 {
		return r.editWithSessionMessage(c, sess, "续费码天数需>0。", telebot.ModeMarkdown, r.cancelMenu())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	codes, err := r.regAdmin.CreateCodes(ctx, c.Sender().ID, days, count, isRenew)
	if err != nil {
		return r.editWithSessionMessage(c, sess, "创建失败："+userFriendlyError(err))
	}
	r.state.Clear(c.Sender().ID)

	title := "邀请码"
	if isRenew {
		title = "续费码"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("已生成 %s（%d 天）%d 个：\n\n", title, days, len(codes)))
	for _, code := range codes {
		b.WriteString(fmt.Sprintf("`%s`\n", code))
	}
	content := b.String()
	_ = c.Delete()
	if len(content) <= 3500 {
		_ = c.Send(content, telebot.ModeMarkdown)
	} else {
		tmp, err := os.CreateTemp("", "codes_*.txt")
		if err == nil {
			path := tmp.Name()
			_, _ = tmp.WriteString(strings.ReplaceAll(content, "`", ""))
			_ = tmp.Close()
			defer func() { _ = os.Remove(path) }()
			doc := &telebot.Document{File: telebot.FromDisk(path), FileName: "codes.txt"}
			_, _ = c.Bot().Send(c.Sender(), doc)
		}
	}

	return r.returnAdminPanel(c, sess)
}

func (r *Router) handleAdminGrantQualificationInput(c telebot.Context, text string) error {
	sess, _ := r.state.Get(c.Sender().ID)
	if !r.isAdminSender(c) {
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, "无权限。")
	}
	parts := strings.Fields(text)
	if len(parts) != 2 {
		return r.editWithSessionMessage(c, sess, "格式错误，请按 `[tg_id] [天数]` 输入，例如：`123456 30`。", telebot.ModeMarkdown, r.cancelMenu())
	}
	targetID, err1 := strconv.ParseInt(parts[0], 10, 64)
	days, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || targetID <= 0 || days < 0 {
		return r.editWithSessionMessage(c, sess, "参数错误：tg_id 需为正整数，天数需>=0。", telebot.ModeMarkdown, r.cancelMenu())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	code, err := r.regAdmin.GrantQualification(ctx, c.Sender().ID, targetID, days)
	if err != nil {
		return r.editWithSessionMessage(c, sess, "发放失败："+userFriendlyError(err))
	}
	r.state.Clear(c.Sender().ID)

	botUsername := ""
	if c.Bot() != nil && c.Bot().Me != nil {
		botUsername = c.Bot().Me.Username
	}
	link := ""
	if botUsername != "" {
		link = fmt.Sprintf("https://t.me/%s?start=%s", botUsername, code)
	}

	userMsg := fmt.Sprintf("你获得了注册资格：`%d` 天。\n\n请点击链接开始注册：\n%s\n\n或在私聊发送：`/start %s`", days, link, code)
	userMsg = formatPushedCard(
		"🎟 注册资格已发放",
		fmt.Sprintf("👤 TGID：`%d`", targetID),
		fmt.Sprintf("📅 可注册天数：`%d` 天", days),
		"📌 说明：请尽快完成注册；注册成功后资格会自动失效。",
		"➡️ 开始注册：",
		link,
		fmt.Sprintf("或在私聊发送：`/start %s`", code),
	)
	sendErr := r.trySendToUser(c.Bot(), targetID, userMsg)

	adminMsg := formatPushedCard(
		"✅ 发放资格完成",
		fmt.Sprintf("👤 TGID：`%d`", targetID),
		fmt.Sprintf("📅 天数：`%d`", days),
		fmt.Sprintf("🔑 邀请码：`%s`", code),
	)
	if sendErr != nil {
		adminMsg += "\n\n⚠️ 私信推送失败（可能未私聊 /start 过）：\n" + userFriendlyError(sendErr)
	}
	_ = c.Delete()
	_ = c.Send(adminMsg, telebot.ModeMarkdown)
	return r.returnAdminPanel(c, sess)
}
