package router

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleWhitelistCmd(c telebot.Context) error {
	if c.Sender() == nil || c.Message() == nil {
		return nil
	}
	if handled, err := r.enforceGroupCommandMisuse(c); handled {
		return err
	}
	if !r.isAdminSender(c) {
		return nil
	}
	if r.regAdmin == nil {
		return nil
	}

	ids, usernameHint := r.extractTargetsFromCommand(c)
	if len(ids) == 0 {
		return c.Send("用法：/Whitelist 123,456 789（也可以在群内回复某人发送 /Whitelist）")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	okCnt := 0
	failCnt := 0
	for i, id := range ids {
		name := ""
		if i == 0 {
			name = usernameHint
		}
		if err := r.regAdmin.SetWhitelist(ctx, id, name, true); err != nil {
			failCnt++
			continue
		}
		okCnt++
	}
	return c.Send(fmt.Sprintf("白名单添加完成：成功 %d，失败 %d。", okCnt, failCnt))
}

func (r *Router) handleUnWhitelistCmd(c telebot.Context) error {
	if c.Sender() == nil || c.Message() == nil {
		return nil
	}
	if handled, err := r.enforceGroupCommandMisuse(c); handled {
		return err
	}
	if !r.isAdminSender(c) {
		return nil
	}
	if r.regAdmin == nil {
		return nil
	}

	ids, usernameHint := r.extractTargetsFromCommand(c)
	if len(ids) == 0 {
		return c.Send("用法：/UnWhitelist 123,456 789（也可以在群内回复某人发送 /UnWhitelist）")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	okCnt := 0
	failCnt := 0
	for i, id := range ids {
		name := ""
		if i == 0 {
			name = usernameHint
		}
		if err := r.regAdmin.SetWhitelist(ctx, id, name, false); err != nil {
			failCnt++
			continue
		}
		okCnt++
	}
	return c.Send(fmt.Sprintf("白名单移除完成：成功 %d，失败 %d。", okCnt, failCnt))
}

func (r *Router) handleLicenseCmd(c telebot.Context) error {
	if c.Sender() == nil || c.Message() == nil {
		return nil
	}
	if handled, err := r.enforceGroupCommandMisuse(c); handled {
		return err
	}
	if !r.isAdminSender(c) {
		return nil
	}
	if r.regAdmin == nil {
		return nil
	}

	ids, _ := r.extractTargetsFromCommand(c)
	if len(ids) == 0 {
		return c.Send("用法：/License 123,456 789（也可以在群内回复某人发送 /License）")
	}

	// 不限制“整体任务总时长”（按用户要求），避免 TGID 数量多时因总超时导致后续批量失败。
	ctx := context.Background()

	okCnt := 0
	failCnt := 0
	var lastExpire time.Time
	sendFail := 0
	var lastSendErr error
	failIDs := make([]int64, 0, 20)
	for _, id := range ids {
		perCtx, perCancel := context.WithTimeout(ctx, 10*time.Second)
		expiresAt, err := r.regAdmin.GrantLicense(perCtx, c.Sender().ID, id)
		perCancel()
		if err != nil {
			failCnt++
			if len(failIDs) < 20 {
				failIDs = append(failIDs, id)
			}
			continue
		}
		lastExpire = expiresAt
		okCnt++

		// 需求：发放资格后自动推送到对应 TGID 的私信。
		// 注意：若用户从未私聊 /start 过，机器人可能无法主动私信（会失败），这属于 Telegram 限制。
		userMsg := formatPushedCard(
			"🎟 注册资格已发放",
			fmt.Sprintf("👤 TGID：`%d`", id),
			fmt.Sprintf("⏳ 有效期至：%s", expiresAt.Local().Format("2006-01-02 15:04:05")),
			"📌 说明：资格自发放起 24 小时有效；注册成功后自动失效。",
			"➡️ 请在有效期内私聊机器人发送 /start 进行注册（无需输入注册码/续费码）。",
		)
		if err := r.trySendToUser(c.Bot(), id, userMsg); err != nil {
			sendFail++
			lastSendErr = err
		}
		// 避免批量推送触发 Telegram Flood 控制（过快会导致部分发送失败）。
		time.Sleep(60 * time.Millisecond)
	}
	if okCnt == 0 {
		return c.Send(fmt.Sprintf("发放注册资格失败：失败 %d。", failCnt))
	}

	// 给管理员回执（命令回复，不属于“推送”，但也统一成卡片样式，便于阅读）。
	lines := []string{
		fmt.Sprintf("✅ 成功：%d", okCnt),
		fmt.Sprintf("❌ 失败：%d", failCnt),
		fmt.Sprintf("⏳ 资格有效期：24 小时（至 %s）", lastExpire.Local().Format("2006-01-02 15:04:05")),
	}
	if sendFail > 0 {
		lines = append(lines, fmt.Sprintf("⚠️ 私信推送失败：%d（可能未私聊 /start 过）", sendFail))
		if lastSendErr != nil {
			lines = append(lines, "最近一次错误："+userFriendlyError(lastSendErr))
		}
	}
	if len(failIDs) > 0 {
		lines = append(lines, fmt.Sprintf("失败 TGID（最多展示 20 个）：%s", joinInt64ForMessage(failIDs)))
	}
	return c.Send(formatPushedCard("✅ 发放注册资格完成", lines...), telebot.ModeMarkdown)
}

func (r *Router) handleRemoveCmd(c telebot.Context) error {
	if c.Sender() == nil || c.Message() == nil {
		return nil
	}
	if handled, err := r.enforceGroupCommandMisuse(c); handled {
		return err
	}
	if !r.isAdminSender(c) && !r.isGroupAdminSender(c) {
		return nil
	}
	if r.revoker == nil {
		return nil
	}

	payload := ""
	if c.Message() != nil {
		payload = strings.TrimSpace(c.Message().Payload)
	}
	idsPart, reason := splitIDsAndReason(payload)

	ids := parseTelegramIDs(idsPart)
	if c.Message().ReplyTo != nil && c.Message().ReplyTo.Sender != nil {
		ids = append([]int64{c.Message().ReplyTo.Sender.ID}, ids...)
	}
	ids = uniqueInt64(ids)
	if len(ids) == 0 {
		return c.Send("用法：/Remove 123,456 789 原因：xxx（也可以在群内回复某人发送 /Remove）")
	}
	// 统一归类为“手动移除”，便于审计/日报统计。
	if strings.TrimSpace(reason) == "" {
		reason = "手动移除"
	} else if !strings.HasPrefix(strings.TrimSpace(reason), "手动移除") {
		reason = "手动移除：" + strings.TrimSpace(reason)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	okCnt := 0
	failCnt := 0
	for _, id := range ids {
		if err := r.revoker.RevokeAccount(ctx, id, reason); err != nil {
			failCnt++
			continue
		}
		okCnt++
	}
	return c.Send(fmt.Sprintf("移除完成：成功 %d，失败 %d。", okCnt, failCnt))
}

func (r *Router) handleCutTreeCmd(c telebot.Context) error {
	if c.Sender() == nil || c.Message() == nil {
		return nil
	}
	if handled, err := r.enforceGroupCommandMisuse(c); handled {
		return err
	}
	if !r.isAdminSender(c) && !r.isGroupAdminSender(c) {
		return nil
	}
	if r.revoker == nil || r.invite == nil {
		return nil
	}

	payload := strings.TrimSpace(c.Message().Payload)
	idsPart, reason := splitIDsAndReason(payload)

	roots := parseTelegramIDs(idsPart)
	if c.Message().ReplyTo != nil && c.Message().ReplyTo.Sender != nil {
		roots = append([]int64{c.Message().ReplyTo.Sender.ID}, roots...)
	}
	roots = uniqueInt64(roots)
	if len(roots) == 0 {
		return c.Send("用法：/CutTree 123,456 789 原因：xxx（也可以在群内回复某人发送 /CutTree）")
	}

	if strings.TrimSpace(reason) == "" {
		reason = "砍树移除"
	} else if !strings.HasPrefix(strings.TrimSpace(reason), "砍树移除") {
		reason = "砍树移除：" + strings.TrimSpace(reason)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	targets := make([]int64, 0, 256)
	for _, root := range roots {
		targets = append(targets, root)
		desc, err := r.invite.Descendants(ctx, root)
		if err != nil {
			return c.Send("砍树失败：" + userFriendlyError(err))
		}
		targets = append(targets, desc...)
	}
	targets = uniqueInt64(targets)

	okCnt := 0
	failCnt := 0
	for _, id := range targets {
		if err := r.revoker.RevokeAccount(ctx, id, reason); err != nil {
			failCnt++
			continue
		}
		okCnt++
	}

	msg := fmt.Sprintf("砍树完成：目标 %d（根 %d），成功 %d，失败 %d。", len(targets), len(roots), okCnt, failCnt)
	if len(targets) > 0 {
		showN := len(targets)
		if showN > 20 {
			showN = 20
		}
		msg += "\n\n目标 TGID（最多展示 20 个）：\n" + joinInt64ForMessage(targets[:showN])
	}
	return c.Send(msg)
}

func (r *Router) handleEliminateCmd(c telebot.Context) error {
	if c.Sender() == nil || c.Message() == nil {
		return nil
	}
	if handled, err := r.enforceGroupCommandMisuse(c); handled {
		return err
	}
	// /Eliminate 属于高危命令：仅管理员可用（不开放给群管理）。
	if !r.isAdminSender(c) {
		return nil
	}
	if r.revoker == nil || r.invite == nil {
		return nil
	}

	payload := strings.TrimSpace(c.Message().Payload)
	idsPart, reason := splitIDsAndReason(payload)

	roots := parseTelegramIDs(idsPart)
	if c.Message().ReplyTo != nil && c.Message().ReplyTo.Sender != nil {
		roots = append([]int64{c.Message().ReplyTo.Sender.ID}, roots...)
	}
	roots = uniqueInt64(roots)
	if len(roots) == 0 {
		return c.Send("用法：/Eliminate 123,456 789 原因：xxx（也可以在群内回复某人发送 /Eliminate）")
	}

	if strings.TrimSpace(reason) == "" {
		reason = "销毁后宫"
	} else if !strings.HasPrefix(strings.TrimSpace(reason), "销毁后宫") {
		reason = "销毁后宫：" + strings.TrimSpace(reason)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// 目标从“根节点本人”开始，仅向下递归（不影响上级）。
	targets := make([]int64, 0, 256)
	for _, root := range roots {
		targets = append(targets, root)
		desc, err := r.invite.Descendants(ctx, root)
		if err != nil {
			logOp(c.Sender().ID, "销毁后宫", "根", root, "结果", "失败", "原因", userFriendlyError(err))
			return c.Send("销毁失败：" + userFriendlyError(err))
		}
		targets = append(targets, desc...)
	}
	targets = uniqueInt64(targets)
	if len(targets) == 0 {
		logOp(c.Sender().ID, "销毁后宫", "根数", len(roots), "结果", "无目标")
		return c.Send(fmt.Sprintf("没有可销毁的后宫：根 %d。", len(roots)))
	}

	okCnt := 0
	failCnt := 0
	for _, id := range targets {
		if err := r.revoker.RevokeAccount(ctx, id, reason); err != nil {
			failCnt++
			continue
		}
		okCnt++
	}

	logOp(c.Sender().ID, "销毁后宫", "根数", len(roots), "目标", len(targets), "成功", okCnt, "失败", failCnt)

	msg := fmt.Sprintf("销毁完成：目标 %d（根 %d），成功 %d，失败 %d。", len(targets), len(roots), okCnt, failCnt)
	if len(targets) > 0 {
		showN := len(targets)
		if showN > 20 {
			showN = 20
		}
		msg += "\n\n目标 TGID（最多展示 20 个）：\n" + joinInt64ForMessage(targets[:showN])
	}
	return c.Send(msg)
}

func (r *Router) handleDisbandCmd(c telebot.Context) error {
	if c.Sender() == nil || c.Message() == nil {
		return nil
	}
	if !isPrivateChat(c) {
		return c.Send("请私聊我使用 /disband。")
	}
	if r.revoker == nil || r.invite == nil {
		return c.Send("功能未初始化，请联系管理员。")
	}

	payload := strings.TrimSpace(c.Message().Payload)
	confirm := strings.Contains(strings.ToLower(payload), "--yes")
	ids := uniqueInt64(parseTelegramIDs(payload))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// /disband <tgid...>：只解除对应邀请关系（不注销账号、不递归）。
	if len(ids) > 0 {
		okCnt := 0
		notFound := 0
		failCnt := 0
		for _, id := range ids {
			it, err := r.invite.RevokeUserInvite(ctx, c.Sender().ID, id)
			if err != nil {
				failCnt++
				continue
			}
			if it == nil {
				notFound++
				continue
			}
			okCnt++
		}
		logOp(c.Sender().ID, "解除后宫关系", "目标", len(ids), "成功", okCnt, "未找到", notFound, "失败", failCnt)
		return c.Send(fmt.Sprintf("已解除关系：成功 %d，未找到 %d，失败 %d。", okCnt, notFound, failCnt))
	}

	// /disband：仅向下解散 1 层（直接后宫），并清理对应关系记录（不递归其后宫）。
	direct, err := r.invite.ListUserInvites(ctx, c.Sender().ID)
	if err != nil {
		return c.Send("查询失败：" + userFriendlyError(err))
	}
	if len(direct) == 0 {
		return c.Send("你还没有后宫。")
	}

	if !confirm {
		lines := []string{
			"⚠️ 解散后宫确认",
			"",
			fmt.Sprintf("将移除你的直接后宫：`%d` 人（仅向下 1 层，不递归其后宫）。", len(direct)),
			"影响范围：不影响你本人。",
			"",
			"该操作会注销目标账号并清理你的“后宫”关系记录。",
			"",
			"如确认，请发送：`/disband --yes`",
		}
		return c.Send(strings.Join(lines, "\n"), telebot.ModeMarkdown)
	}

	okCnt := 0
	failCnt := 0
	targets := make([]int64, 0, len(direct))
	for _, it := range direct {
		if it.InviteeTelegramID == 0 {
			continue
		}
		targets = append(targets, it.InviteeTelegramID)
	}
	targets = uniqueInt64(targets)

	for _, id := range targets {
		if err := r.revoker.RevokeAccount(ctx, id, "解散后宫"); err != nil {
			failCnt++
			continue
		}
		okCnt++
	}

	revokedCnt := 0
	for _, id := range targets {
		if _, err := r.invite.RevokeUserInvite(ctx, c.Sender().ID, id); err == nil {
			revokedCnt++
		}
	}

	logOp(c.Sender().ID, "解散后宫", "直接", len(targets), "注销成功", okCnt, "注销失败", failCnt, "撤回记录", revokedCnt)
	return c.Send(fmt.Sprintf("解散完成：直接后宫 %d，注销成功 %d，注销失败 %d。", len(targets), okCnt, failCnt))
}

func (r *Router) handleHaremCmd(c telebot.Context) error {
	if c.Sender() == nil || c.Message() == nil {
		return nil
	}
	if r.invite == nil {
		return nil
	}

	payload := strings.TrimSpace(c.Message().Payload)
	ids := parseTelegramIDs(payload)
	if c.Message().ReplyTo != nil && c.Message().ReplyTo.Sender != nil {
		ids = append([]int64{c.Message().ReplyTo.Sender.ID}, ids...)
	}
	ids = uniqueInt64(ids)
	if len(ids) == 0 {
		return c.Send("用法：/Harem TGID（也可以回复某人发送 /Harem）\n\n说明：/Harem 是“用户邀请（邀请码）”，不同于 /License（管理员发放 24h 注册资格）。")
	}
	if len(ids) > 1 {
		return c.Send("一次只能邀请 1 个 TGID（也可以回复某人发送 /Harem）。")
	}
	targetID := ids[0]
	if targetID == 0 || targetID == c.Sender().ID {
		return c.Send("目标 TGID 无效。")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := r.invite.PrepareTargetedInviteCode(ctx, c.Sender().ID, targetID)
	if err != nil {
		logOp(c.Sender().ID, "邀请好友", "结果", "失败", "目标", targetID, "原因", userFriendlyError(err))
		_ = r.trySendToUser(c.Bot(), c.Sender().ID, "❌ 邀请失败："+userFriendlyError(err))
		return c.Send("邀请失败：" + userFriendlyError(err))
	}
	if !res.Eligible {
		msg := "🎁 邀请好友\n\n" + strings.TrimSpace(res.Reason)
		if res.NextAllowedAt != nil && !res.NextAllowedAt.IsZero() {
			msg += fmt.Sprintf("\n\n下次可邀请时间：`%s`", res.NextAllowedAt.Local().Format("2006-01-02 15:04:05"))
		}
		logOp(c.Sender().ID, "邀请好友", "结果", "失败", "目标", targetID, "原因", strings.TrimSpace(res.Reason))
		_ = r.trySendToUser(c.Bot(), c.Sender().ID, msg)
		return c.Send(msg, telebot.ModeMarkdown)
	}

	expiresAt := time.Now().Add(r.inviteReservationTTL)

	inviterName := "@" + strings.TrimSpace(c.Sender().Username)
	if strings.TrimSpace(c.Sender().Username) == "" {
		inviterName = fmt.Sprintf("TGID %d", c.Sender().ID)
	}

	inviteeMsg := formatPushedCard(
		"🎁 你获得了邀请资格",
		fmt.Sprintf("来自：%s", inviterName),
		"",
		fmt.Sprintf("我已为你预留注册资格（%d 小时内有效，至 %s）。", int(r.inviteReservationTTL.Hours()), expiresAt.Local().Format("2006-01-02 15:04:05")),
		"请私聊我发送 /start，然后按提示完成注册。",
	)
	sendErr := r.trySendToUser(c.Bot(), targetID, inviteeMsg)

	nextInviteLine := "下次邀请：不限制冷却。"
	if r.inviteCooldownDays > 0 {
		nextInviteLine = fmt.Sprintf("下次邀请：本次邀请成功注册后，需冷却 %d 天。", r.inviteCooldownDays)
	}

	inviterDM := strings.Join([]string{
		"✅ 邀请资格已发放",
		fmt.Sprintf("有效期：至 `%s`（%d 小时内未注册将自动回收并返还给你）", expiresAt.Local().Format("2006-01-02 15:04:05"), int(r.inviteReservationTTL.Hours())),
		nextInviteLine,
	}, "\n")
	if sendErr != nil {
		inviterDM += "\n\n⚠️ 我无法私信通知对方，请让对方主动私聊我发送 /start（无需邀请码）。"
	}
	_ = r.trySendToUser(c.Bot(), c.Sender().ID, inviterDM)

	// 在群里触发时，避免把邀请码发到群里：仅回执结果。
	if !isPrivateChat(c) {
		_ = c.Delete()
		if sendErr != nil {
			logOp(c.Sender().ID, "邀请好友", "结果", "成功", "目标", targetID, "投递", "对方私信失败-已私聊邀请者")
			return c.Send("✅ 邀请资格已发放，但无法私信通知对方；请让对方主动私聊我发送 /start（无需邀请码）。")
		}
		logOp(c.Sender().ID, "邀请好友", "结果", "成功", "目标", targetID, "投递", "已私信对方")
		return c.Send("✅ 邀请资格已发放，我已私信通知对方。")
	}

	// 私聊触发：回执不包含邀请码/链接/TGID等敏感信息。
	lines := []string{
		"✅ 邀请资格已发放。",
		fmt.Sprintf("有效期：至 `%s`（%d 小时内未注册将自动回收并返还给你）", expiresAt.Local().Format("2006-01-02 15:04:05"), int(r.inviteReservationTTL.Hours())),
		nextInviteLine,
	}
	if sendErr != nil {
		lines = append(lines, "", "⚠️ 我无法私信通知对方，请让对方主动私聊我发送 /start（无需邀请码）。")
		logOp(c.Sender().ID, "邀请好友", "结果", "生成成功", "目标", targetID, "投递", "对方私信失败")
	} else {
		logOp(c.Sender().ID, "邀请好友", "结果", "成功", "目标", targetID, "投递", "已私信对方")
	}
	return c.Send(strings.Join(lines, "\n"), telebot.ModeMarkdown)
}

func (r *Router) handleMemberCmd(c telebot.Context) error {
	if c.Sender() == nil || c.Message() == nil {
		return nil
	}
	if handled, err := r.enforceGroupCommandMisuse(c); handled {
		return err
	}
	if !r.isAdminSender(c) {
		return nil
	}
	if r.regAdmin == nil {
		return nil
	}

	payload := strings.TrimSpace(c.Message().Payload)
	if strings.EqualFold(payload, "all") {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		settings, err := r.regAdmin.GetSettings(ctx)
		if err != nil {
			return c.Send("查询失败：" + userFriendlyError(err))
		}
		if len(settings.GroupAdminIDs) == 0 {
			return c.Send("当前没有设置群管理。")
		}
		return c.Send(fmt.Sprintf("群管理 TGID：%s", joinInt64ForMessage(settings.GroupAdminIDs)))
	}

	ids, _ := r.extractTargetsFromCommand(c)
	if len(ids) == 0 {
		return c.Send("用法：/Member 123,456 789\n- 查询全部：/Member All\n- 也可以在群内回复某人发送 /Member")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 读取现有列表并合并
	settings, err := r.regAdmin.GetSettings(ctx)
	if err != nil {
		return c.Send("读取失败：" + userFriendlyError(err))
	}
	merged := append(settings.GroupAdminIDs, ids...)
	if _, err := r.regAdmin.SetGroupAdmins(ctx, c.Sender().ID, merged); err != nil {
		return c.Send("设置失败：" + userFriendlyError(err))
	}
	_ = r.refreshGroupAdmins(ctx)
	return c.Send(fmt.Sprintf("已添加群管理：%s", joinInt64ForMessage(ids)))
}

func (r *Router) handleUnMemberCmd(c telebot.Context) error {
	if c.Sender() == nil || c.Message() == nil {
		return nil
	}
	if handled, err := r.enforceGroupCommandMisuse(c); handled {
		return err
	}
	if !r.isAdminSender(c) {
		return nil
	}
	if r.regAdmin == nil {
		return nil
	}

	ids, _ := r.extractTargetsFromCommand(c)
	if len(ids) == 0 {
		return c.Send("用法：/UnMember 123,456 789（也可以在群内回复某人发送 /UnMember）")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	settings, err := r.regAdmin.GetSettings(ctx)
	if err != nil {
		return c.Send("读取失败：" + userFriendlyError(err))
	}

	toRemove := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		toRemove[id] = struct{}{}
	}
	kept := make([]int64, 0, len(settings.GroupAdminIDs))
	for _, id := range settings.GroupAdminIDs {
		if _, ok := toRemove[id]; ok {
			continue
		}
		kept = append(kept, id)
	}
	if _, err := r.regAdmin.SetGroupAdmins(ctx, c.Sender().ID, kept); err != nil {
		return c.Send("删除失败：" + userFriendlyError(err))
	}
	_ = r.refreshGroupAdmins(ctx)
	return c.Send(fmt.Sprintf("已移除群管理：%s", joinInt64ForMessage(ids)))
}
