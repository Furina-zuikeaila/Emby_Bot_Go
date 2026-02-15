package router

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleRegToggle(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}
	if r.regAdmin == nil {
		return r.editOrSendText(c, "注册管理尚未初始化。")
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	settings, err := r.regAdmin.GetSettings(ctx)
	if err != nil {
		return r.editOrSendText(c, "读取设置失败："+userFriendlyError(err))
	}
	effective := settings.Enabled
	if settings.OpenUntil != nil && time.Now().After(*settings.OpenUntil) {
		effective = false
	}
	_, err = r.regAdmin.SetEnabled(ctx, !effective)
	if err != nil {
		return r.editOrSendText(c, "更新失败："+userFriendlyError(err))
	}
	if !effective {
		r.notifyRegistrationChange(c, "✅ 自由注册已开启")
	} else {
		r.notifyRegistrationChange(c, "❌ 自由注册已关闭")
	}
	return r.sendRegPanel(c)
}

func (r *Router) handleRegSetTiming(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	r.setAdminConvo(c, convoAdminSetTiming, "reg")
	return r.editOrSendText(c, "请输入定时开放分钟数：`[分钟]`（0 取消定时）\n\n点击“取消”返回。", telebot.ModeMarkdown, r.cancelMenu())
}

func (r *Router) handleRegSetMaxUsers(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	r.setAdminConvo(c, convoAdminSetMaxUsers, "reg")
	return r.editOrSendText(c, "请输入注册总人数上限：`[数字]`（0 表示上限为 0）\n\n点击“取消”返回。", telebot.ModeMarkdown, r.cancelMenu())
}

func (r *Router) handleRegSetDefaultDays(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	r.setAdminConvo(c, convoAdminSetDefaultDays, "reg")
	return r.editOrSendText(c, "请输入默认注册天数：`[数字]`（0 表示立即过期）\n\n点击“取消”返回。", telebot.ModeMarkdown, r.cancelMenu())
}

func (r *Router) handleRegCreateCodes(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	r.setAdminConvo(c, convoAdminCreateCodes, "keys")
	return r.editOrSendText(c, "请输入邀请码参数：`[天数] [数量]`（数量 1-100，天数可为 0）\n\n点击“取消”返回。", telebot.ModeMarkdown, r.cancelMenu())
}

func (r *Router) handleRegCreateRenewCodes(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	r.setAdminConvo(c, convoAdminCreateRenewCodes, "keys")
	return r.editOrSendText(c, "请输入续费码参数：`[天数] [数量]`（数量 1-100，天数需>0）\n\n点击“取消”返回。", telebot.ModeMarkdown, r.cancelMenu())
}

func (r *Router) handleRegExport(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}
	if r.regAdmin == nil {
		return r.editOrSendText(c, "注册管理尚未初始化。")
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	botUsername := ""
	if c.Bot() != nil && c.Bot().Me != nil {
		botUsername = c.Bot().Me.Username
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	links, err := r.regAdmin.ExportUnusedLinks(ctx, c.Sender().ID, botUsername)
	if err != nil {
		return r.editOrSendText(c, "导出失败："+userFriendlyError(err))
	}
	if len(links) == 0 {
		return r.editOrSendText(c, "当前没有可导出的未使用邀请码。")
	}

	content := strings.Join(links, "\n")
	tmp, err := os.CreateTemp("", "invite_codes_*.txt")
	if err != nil {
		return r.editOrSendText(c, "导出失败：无法创建临时文件。")
	}
	path := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(path)
	}()
	if _, err := tmp.WriteString(content); err != nil {
		return r.editOrSendText(c, "导出失败：写入临时文件失败。")
	}
	_ = tmp.Close()

	doc := &telebot.Document{File: telebot.FromDisk(path), FileName: "invite_codes.txt"}
	_, err = c.Bot().Send(c.Sender(), doc)
	if err != nil {
		return r.editOrSendText(c, "发送文件失败："+userFriendlyError(err))
	}
	return r.editOrSendText(c, "已发送导出文件到私聊。")
}

func (r *Router) handleRegWipe(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}
	if r.regAdmin == nil {
		return r.editOrSendText(c, "注册管理尚未初始化。")
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	affected, err := r.regAdmin.WipeUnused(ctx, c.Sender().ID)
	if err != nil {
		return r.editOrSendText(c, "销毁失败："+userFriendlyError(err))
	}
	return r.editOrSendText(c, fmt.Sprintf("已销毁 %d 条未使用邀请码。", affected))
}

func (r *Router) handleRegStats(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}
	if r.regAdmin == nil {
		return r.editOrSendText(c, "注册管理尚未初始化。")
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stats, err := r.regAdmin.Stats(ctx)
	if err != nil {
		return r.editOrSendText(c, "统计失败："+userFriendlyError(err))
	}

	msg := "邀请码统计：\n\n"
	msg += fmt.Sprintf("已使用：`%d`\n", stats.UsedCount)
	msg += fmt.Sprintf("未使用：`%d`\n\n", stats.UnusedCount)
	msg += "未使用按天数：\n"
	msg += fmt.Sprintf("- 30 天：`%d`\n", stats.MonthCount)
	msg += fmt.Sprintf("- 90 天：`%d`\n", stats.SeasonCount)
	msg += fmt.Sprintf("- 180 天：`%d`\n", stats.HalfYearCount)
	msg += fmt.Sprintf("- 365 天：`%d`\n", stats.YearCount)
	return r.editOrSendText(c, msg, telebot.ModeMarkdown)
}

func (r *Router) handleRegGrant(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	r.setAdminConvo(c, convoAdminGrantQualification, "keys")
	return r.editOrSendText(c, "请输入发放资格参数：`[tg_id] [天数]`（天数可为 0）\n\n点击“取消”返回。", telebot.ModeMarkdown, r.cancelMenu())
}

// 已迁移至 panel_admin_reg.go
