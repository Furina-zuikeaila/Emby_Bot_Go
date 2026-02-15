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

func (r *Router) handleAdminCommunityModePanel(c telebot.Context) error {
	if !isPrivateChat(c) {
		return r.editOrSendText(c, "请私聊我使用该功能。")
	}
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}
	if r.regAdmin == nil {
		return r.editOrSendText(c, "社区模式尚未初始化。")
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	r.tryRemoveReplyKeyboard(c)
	return r.sendCommunityModePanelWithMessageID(c, 0)
}

func (r *Router) sendCommunityModePanelWithMessageID(c telebot.Context, messageID int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	settings, _ := r.regAdmin.GetSettings(ctx)
	cur := normalizeServiceMode(r.getCurrentServiceMode(ctx))
	curLabel := formatServiceMode(cur)

	inactiveRaw := strings.TrimSpace(settings.InactiveDuration)
	if inactiveRaw == "" {
		inactiveRaw = strings.TrimSpace(os.Getenv("COMM_INACTIVE_DURATION"))
		if inactiveRaw == "" {
			inactiveRaw = "720h"
		}
	}

	msg := "🌐 社区模式\n"
	msg += "——————————————\n"
	msg += "\n"
	msg += fmt.Sprintf("🔧 当前模式：%s\n", curLabel)
	msg += fmt.Sprintf("⏱️ 不活跃时长：%s\n", inactiveRaw)
	msg += "——————————————\n"
	msg += "📌 切换模式会自动调整开关：\n"
	msg += "- 公益：✅ 不活跃检测，❌ 到期检测\n"
	msg += "- 公费：✅ 不活跃检测，✅ 到期检测\n"
	msg += "- 私服：✅ 不活跃检测，✅ 到期检测\n"
	msg += "——————————————\n"
	msg += "请选择要切换的模式："

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(menu.Data("私服", CbReqCommunityMode, "private"), menu.Data("公费", CbReqCommunityMode, "public")),
		menu.Row(menu.Data("公益", CbReqCommunityMode, "charity"), menu.Data("设置不活跃时长", CbAdminSetInactiveDuration)),
		menu.Row(menu.Data("返回面板", CbAdminPanel)),
	)
	if messageID > 0 {
		if err := r.editByMessageIDAuto(c, messageID, msg, telebot.ModeMarkdown, menu); err == nil {
			return nil
		}
	}
	return r.editOrSendText(c, msg, telebot.ModeMarkdown, menu)
}

func (r *Router) handleReqCommunityMode(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		_ = c.Respond(&telebot.CallbackResponse{Text: "无权限", ShowAlert: false})
		return nil
	}
	if r.regAdmin == nil {
		_ = c.Respond(&telebot.CallbackResponse{Text: "未初始化", ShowAlert: false})
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	mode := normalizeServiceMode(c.Data())
	label := formatServiceMode(mode)

	msg := "⚠️ 二次确认\n\n"
	msg += fmt.Sprintf("你将切换社区模式为：%s\n\n", label)
	msg += "切换后将自动调整检测任务默认开关：\n"
	msg += "- 公益：✅ 不活跃检测，❌ 到期检测\n"
	msg += "- 公费：✅ 不活跃检测，✅ 到期检测\n"
	msg += "- 私服：✅ 不活跃检测，✅ 到期检测\n\n"
	msg += "确认继续？"

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(menu.Data("确认切换", CbSetCommunityMode, mode), menu.Data("取消", CbAdminCommunityMode)),
	)
	return r.editOrSendText(c, msg, telebot.ModeMarkdown, menu)
}

func (r *Router) handleSetCommunityMode(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		_ = c.Respond(&telebot.CallbackResponse{Text: "无权限", ShowAlert: false})
		return nil
	}
	if r.regAdmin == nil {
		_ = c.Respond(&telebot.CallbackResponse{Text: "未初始化", ShowAlert: false})
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	mode := normalizeServiceMode(c.Data())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// 记录切换前模式，用于执行清理策略
	prevMode := normalizeServiceMode(r.getCurrentServiceMode(ctx))

	if _, err := r.regAdmin.SetServiceMode(ctx, mode); err != nil {
		return r.editOrSendText(c, "切换失败："+userFriendlyError(err), r.menus.Admin)
	}

	// 按社区模式自动调整“到期检测 / 不活跃检测”定时任务开关：
	// - 公益：开启不活跃检测，关闭到期检测
	// - 公费：两者都开启
	// - 私服：两者都开启
	r.applyModeScheduleDefaults(mode)

	// 按模式切换执行清理策略：
	// 1) 公益 -> 公费：仅清理“已过期且不活跃”的用户
	// 2) 公益/公费 -> 私服：直接清理“已过期”的用户（不看活跃）
	_ = r.cleanupOnModeSwitch(ctx, prevMode, mode)
	return r.sendCommunityModePanelWithMessageID(c, 0)
}

func (r *Router) applyModeScheduleDefaults(mode string) {
	sched, ok := r.revoker.(detectionScheduler)
	if !ok {
		return
	}
	mode = normalizeServiceMode(mode)
	switch mode {
	case "charity":
		_ = sched.SetInactiveScheduleEnabled(true)
		_ = sched.SetExpiredScheduleEnabled(false)
	case "public":
		_ = sched.SetInactiveScheduleEnabled(true)
		_ = sched.SetExpiredScheduleEnabled(true)
	case "private":
		_ = sched.SetInactiveScheduleEnabled(true)
		_ = sched.SetExpiredScheduleEnabled(true)
	}
}

func (r *Router) handleAdminSetInactiveDuration(c telebot.Context) error {
	if !isPrivateChat(c) {
		return nil
	}
	if !r.isAdminSender(c) {
		return r.editOrSendText(c, "无权限。")
	}
	if r.regAdmin == nil {
		return r.editOrSendText(c, "社区模式尚未初始化。")
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	r.setAdminConvo(c, convoAdminSetInactiveDuration, "community")
	return r.editOrSendText(c, "请输入不活跃时长：`[时长]`\n\n例如：`720h`（30 天）、`168h`（7 天）、`30m`\n\n点击“取消”返回。", telebot.ModeMarkdown, r.cancelMenu())
}

func (r *Router) handleAdminSetInactiveDurationInput(c telebot.Context, text string) error {
	sess, _ := r.state.Get(c.Sender().ID)
	if !r.isAdminSender(c) {
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, "无权限。")
	}
	raw := strings.TrimSpace(text)
	if raw == "" {
		// 允许清空，表示回退到环境变量默认值
		raw = ""
	} else {
		if _, err := time.ParseDuration(raw); err != nil {
			// 兼容 30d
			if !strings.HasSuffix(strings.ToLower(raw), "d") {
				return r.editWithSessionMessage(c, sess, "格式错误，请输入合法时长，例如：`720h`、`168h`、`30m`，或 `30d`。", telebot.ModeMarkdown, r.cancelMenu())
			}
			v := strings.TrimSpace(strings.TrimSuffix(strings.ToLower(raw), "d"))
			if _, err := strconv.Atoi(v); err != nil {
				return r.editWithSessionMessage(c, sess, "格式错误，请输入合法时长，例如：`720h`、`168h`、`30m`，或 `30d`。", telebot.ModeMarkdown, r.cancelMenu())
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := r.regAdmin.SetInactiveDuration(ctx, raw); err != nil {
		return r.editWithSessionMessage(c, sess, "更新失败："+userFriendlyError(err))
	}
	r.state.Clear(c.Sender().ID)
	_ = c.Delete()
	messageID := 0
	if v := strings.TrimSpace(sess.Values["edit_message_id"]); v != "" {
		if id, err := strconv.Atoi(v); err == nil && id > 0 {
			messageID = id
		}
	}
	return r.sendCommunityModePanelWithMessageID(c, messageID)
}

func (r *Router) cleanupOnModeSwitch(ctx context.Context, prevMode, newMode string) error {
	if r == nil || r.revoker == nil || r.adm == nil {
		return nil
	}

	prevMode = normalizeServiceMode(prevMode)
	newMode = normalizeServiceMode(newMode)
	if prevMode == newMode {
		return nil
	}

	now := time.Now()

	inactiveThreshold, _ := r.getInactiveDuration(ctx)

	shouldCleanExpiredOnly := (prevMode == "charity" || prevMode == "public") && newMode == "private"
	shouldCleanExpiredInactive := prevMode == "charity" && newMode == "public"
	if !shouldCleanExpiredOnly && !shouldCleanExpiredInactive {
		return nil
	}

	reason := "社区模式切换"
	if shouldCleanExpiredInactive {
		reason = "公益 -> 公费：清理过期且不活跃用户"
	}
	if shouldCleanExpiredOnly {
		reason = "切换到私服：清理过期用户"
	}

	const limit = 200
	offset := 0
	revoked := 0

	for {
		users, err := r.adm.ListUsers(ctx, limit, offset)
		if err != nil {
			return err
		}
		if len(users) == 0 {
			break
		}

		for _, u := range users {
			if u.TelegramID == 0 || strings.TrimSpace(u.EmbyUserID) == "" {
				continue
			}
			if u.IsWhitelist {
				// 白名单用户不受模式切换清理影响
				continue
			}
			if u.ExpiresAt == nil || u.ExpiresAt.After(now) {
				continue
			}

			if shouldCleanExpiredInactive && inactiveThreshold > 0 {
				inactive := false
				if u.LastPlayedAt == nil {
					inactive = true
				} else if now.Sub(*u.LastPlayedAt) > inactiveThreshold {
					inactive = true
				}
				if !inactive {
					continue
				}
			}

			if err := r.revoker.RevokeAccount(ctx, u.TelegramID, reason); err == nil {
				revoked++
			}
		}

		offset += limit
		if len(users) < limit {
			break
		}
	}

	if revoked > 0 {
		logOp(0, "切换社区模式清理", "旧模式", prevMode, "新模式", newMode, "回收数", revoked)
	}
	return nil
}

func (r *Router) getInactiveDuration(ctx context.Context) (time.Duration, string) {
	raw := ""
	if r != nil && r.regAdmin != nil {
		if settings, err := r.regAdmin.GetSettings(ctx); err == nil {
			raw = strings.TrimSpace(settings.InactiveDuration)
		}
	}
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("COMM_INACTIVE_DURATION"))
	}
	if raw == "" {
		raw = "720h"
	}
	d, err := time.ParseDuration(raw)
	if err == nil {
		return d, raw
	}
	// 兼容 30d
	if strings.HasSuffix(strings.ToLower(raw), "d") {
		v := strings.TrimSpace(strings.TrimSuffix(strings.ToLower(raw), "d"))
		if days, err := strconv.Atoi(v); err == nil {
			return time.Duration(days) * 24 * time.Hour, raw
		}
	}
	return 0, raw
}
