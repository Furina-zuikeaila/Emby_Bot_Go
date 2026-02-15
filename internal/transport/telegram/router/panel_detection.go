package router

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleAdminDetectionTasksPanel(c telebot.Context) error {
	if !isPrivateChat(c) {
		return r.editOrSendText(c, "请私聊我使用该功能。")
	}
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	r.tryRemoveReplyKeyboard(c)
	return r.sendDetectionTasksPanel(c)
}

func (r *Router) sendDetectionTasksPanel(c telebot.Context) error {
	var b strings.Builder
	b.WriteString("🧪 检测任务中心\n")
	b.WriteString("——————————————\n")
	b.WriteString("⚙️ 任务间隔时间\n")
	if sched, ok := r.revoker.(detectionScheduler); ok {
		b.WriteString(fmt.Sprintf("`🌐 Web   检测： %s`\n", formatIntervalMinutes(sched.WebClientInterval())))
		b.WriteString(fmt.Sprintf("`⌛️ 到期   检测： %s`\n", formatIntervalMinutes(sched.ExpiredInterval())))
		b.WriteString(fmt.Sprintf("`🕒 不活跃检测： %s`\n", formatIntervalMinutes(sched.InactiveInterval())))
	} else {
		b.WriteString("当前未启用检测任务调度器。\n")
	}
	return r.editOrSendText(c, b.String(), r.buildDetectionTasksMenu())
}

func formatIntervalMinutes(d time.Duration) string {
	if d <= 0 {
		return "0分钟"
	}
	mins := int(d.Round(time.Minute).Minutes())
	if mins <= 0 {
		mins = 1
	}
	return fmt.Sprintf("%d分钟", mins)
}

func (r *Router) buildDetectionTasksMenu() *telebot.ReplyMarkup {
	menu := &telebot.ReplyMarkup{}
	guard := "❌ 群内命令检测"
	if r.groupCommandGuardEnabled() {
		guard = "✅ 群内命令检测"
	}
	if sched, ok := r.revoker.(detectionScheduler); ok {
		exp := "❌ 到期检测"
		if sched.ExpiredScheduleEnabled() {
			exp = "✅ 到期检测"
		}
		inactive := "❌ 不活跃检测"
		if sched.InactiveScheduleEnabled() {
			inactive = "✅ 不活跃检测"
		}
		web := "❌ Web 检测"
		if sched.WebClientScheduleEnabled() {
			web = "✅ Web 检测"
		}
		menu.Inline(
			menu.Row(menu.Data(guard, CbToggleGroupCommandGuard), menu.Data(web, CbToggleWebSchedule)),
			menu.Row(menu.Data(exp, CbToggleExpiredSchedule), menu.Data(inactive, CbToggleInactiveSchedule)),
			menu.Row(menu.Data("返回管理面板", CbAdminPanel)),
		)
		return menu
	}

	// 没有检测任务调度器时，仅提供治理开关与返回入口。
	menu.Inline(
		menu.Row(menu.Data(guard, CbToggleGroupCommandGuard)),
		menu.Row(menu.Data("返回管理面板", CbAdminPanel)),
	)
	return menu
}

func (r *Router) handleToggleGroupCommandGuard(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		_ = c.Respond(&telebot.CallbackResponse{Text: "无权限", ShowAlert: false})
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	_ = r.toggleGroupCommandGuardEnabled()
	return r.sendDetectionTasksPanel(c)
}

func (r *Router) handleToggleWebSchedule(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		_ = c.Respond(&telebot.CallbackResponse{Text: "无权限", ShowAlert: false})
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	sched, ok := r.revoker.(detectionScheduler)
	if !ok {
		return r.sendDetectionTasksPanel(c)
	}
	_ = sched.SetWebClientScheduleEnabled(!sched.WebClientScheduleEnabled())
	return r.sendDetectionTasksPanel(c)
}

func (r *Router) handleToggleExpiredSchedule(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		_ = c.Respond(&telebot.CallbackResponse{Text: "无权限", ShowAlert: false})
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	sched, ok := r.revoker.(detectionScheduler)
	if !ok {
		return r.sendDetectionTasksPanel(c)
	}
	_ = sched.SetExpiredScheduleEnabled(!sched.ExpiredScheduleEnabled())
	return r.sendDetectionTasksPanel(c)
}

func (r *Router) handleToggleInactiveSchedule(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		_ = c.Respond(&telebot.CallbackResponse{Text: "无权限", ShowAlert: false})
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	sched, ok := r.revoker.(detectionScheduler)
	if !ok {
		return r.sendDetectionTasksPanel(c)
	}
	_ = sched.SetInactiveScheduleEnabled(!sched.InactiveScheduleEnabled())
	return r.sendDetectionTasksPanel(c)
}

func (r *Router) handleRunWebCheck(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		_ = c.Respond(&telebot.CallbackResponse{Text: "无权限", ShowAlert: false})
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	runner, ok := r.revoker.(detectionRunner)
	if !ok {
		return r.editOrSendText(c, "当前未启用检测任务执行器。", r.buildDetectionTasksMenu())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stats := runner.RunWebCheck(ctx)
	msg := fmt.Sprintf("Web 检测完成：\n\n扫描：%d\n封号：%d", stats.ScannedUsers, stats.RevokedUsers)
	return r.editOrSendText(c, msg, r.buildDetectionTasksMenu())
}

func (r *Router) handleRunAllChecks(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		_ = c.Respond(&telebot.CallbackResponse{Text: "无权限", ShowAlert: false})
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	runner, ok := r.revoker.(detectionRunner)
	if !ok {
		return r.editOrSendText(c, "当前未启用检测任务执行器。", r.buildDetectionTasksMenu())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	web := runner.RunWebCheck(ctx)
	msg := "检测任务完成：\n\n"
	msg += fmt.Sprintf("Web：扫描 %d，封号 %d\n", web.ScannedUsers, web.RevokedUsers)
	return r.editOrSendText(c, msg, r.buildDetectionTasksMenu())
}
