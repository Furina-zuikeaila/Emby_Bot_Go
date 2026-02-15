package router

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	accountapp "emby-bot-new/internal/application/account"
	adminapp "emby-bot-new/internal/application/admin"
	inviteapp "emby-bot-new/internal/application/invite"
	"emby-bot-new/internal/application/registration"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleInviteCode(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	return r.startInviteCode(c)
}

func (r *Router) handleBind(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	return r.startBind(c)
}

func (r *Router) handleCancel(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	if sess, ok := r.state.Get(c.Sender().ID); ok && isAdminConvo(sess.State) && r.isAdminSender(c) {
		r.state.Clear(c.Sender().ID)
		return r.returnAdminPanel(c, sess)
	}
	r.state.Clear(c.Sender().ID)
	return r.sendMainMenu(c, "已取消。")
}

func (r *Router) handleText(c telebot.Context) error {
	if !isPrivateChat(c) {
		return r.handleGroupMessage(c)
	}
	if c.Sender() == nil || c.Message() == nil {
		return nil
	}

	text := strings.TrimSpace(c.Text())
	if text == "" {
		return nil
	}

	sess, ok := r.state.Get(c.Sender().ID)
	if strings.HasPrefix(text, "/") {
		return nil
	}

	if handled, err := r.routeStartButtons(c, text); handled {
		r.state.Clear(c.Sender().ID)
		return err
	}
	if !ok {
		return nil
	}

	switch sess.State {
	case convoRegisterInput:
		return r.handleRegisterInput(c, text)
	case convoInviteCode:
		return r.handleInviteCodeInput(c, text)
	case convoBindUsername:
		return r.handleBindUsernameInput(c, text)
	case convoBindPassword:
		return r.handleBindPasswordInput(c, sess, text)
	case convoBindSecureCode:
		return r.handleBindSecureCodeInput(c, sess, text)
	case convoRenewCode:
		return r.handleRenewCodeInput(c, text)
	case convoResetPassword:
		return r.handleResetPasswordInput(c, text)
	case convoDeleteAccount:
		return r.handleDeleteAccountInput(c, text)
	case convoUserInviteTarget:
		return r.handleUserInviteTargetInput(c, text)
	case convoHaremRevokeInput:
		return r.handleHaremRevokeTargetInput(c, text)

	case convoAdminSetTiming:
		return r.handleAdminSetTimingInput(c, text)
	case convoAdminSetMaxUsers:
		return r.handleAdminSetMaxUsersInput(c, text)
	case convoAdminSetDefaultDays:
		return r.handleAdminSetDefaultDaysInput(c, text)
	case convoAdminCreateCodes:
		return r.handleAdminCreateCodesInput(c, false, text)
	case convoAdminCreateRenewCodes:
		return r.handleAdminCreateCodesInput(c, true, text)
	case convoAdminGrantQualification:
		return r.handleAdminGrantQualificationInput(c, text)
	case convoAdminSetInactiveDuration:
		return r.handleAdminSetInactiveDurationInput(c, text)
	case convoAdminWhitelistAdd:
		return r.handleAdminWhitelistAddInput(c, text)
	case convoAdminWhitelistRemove:
		return r.handleAdminWhitelistRemoveInput(c, text)
	case convoCrowdfundTxHash:
		return r.handleCrowdfundTxHashInput(c, text)
	default:
		return nil
	}
}

func (r *Router) routeStartButtons(c telebot.Context, text string) (bool, error) {
	switch text {
	case StartBtnUserPanel, "用户功能":
		return true, r.handleUserPanel(c)
	case StartBtnServer, "服务器":
		return true, r.handleServerInfo(c)
	case StartBtnCrowdfund, "发电", "发电支持", "众筹支持":
		return true, r.handleCrowdfund(c)
	case StartBtnAdminPanel, "管理面板", "管理员面板":
		return true, r.handleAdmin(c)
	default:
		return false, nil
	}
}

func (r *Router) startRegister(c telebot.Context) error {
	if c.Sender() == nil {
		return nil
	}
	r.state.Clear(c.Sender().ID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gate, err := r.reg.Gate(ctx, c.Sender().ID, c.Sender().Username)

	if err != nil {
		return r.sendMainMenu(c, "获取注册状态失败："+userFriendlyError(err))
	}
	account, err := r.reg.Me(ctx, c.Sender().ID)
	if err == nil && account != nil && account.EmbyUserID != "" {
		return r.sendMainMenu(c, "你已经注册过了，可以点“我的信息”查看账号。")
	}

	if !gate.HasQualification {
		if !gate.Enabled || (gate.MaxUsers >= 0 && gate.CurrentUsers >= gate.MaxUsers) {
			reason := "当前注册需要邀请码。"
			if !gate.Enabled {
				reason = "当前注册已关闭，需要邀请码。"
			} else if gate.MaxUsers >= 0 && gate.CurrentUsers >= gate.MaxUsers {
				reason = fmt.Sprintf("名额已满（%d/%d），需要邀请码。", gate.CurrentUsers, gate.MaxUsers)
			}
			r.state.Set(c.Sender().ID, convoInviteCode, nil)
			return r.editOrSendText(c, reason+"\n\n请输入邀请码：\n\n点击“取消”返回。", r.cancelMenu())
		}
	}

	msg := r.buildRegisterPrompt(gate)
	return r.upsertUserConvoMessage(c, convoRegisterInput, convoSession{}, nil, msg, telebot.ModeMarkdown, r.cancelMenu())
}

// buildRegisterPrompt 生成注册输入提示文案。
func (r *Router) buildRegisterPrompt(gate registration.Gate) string {
	msg := "请在 2 分钟内发送：`[Emby用户名] [安全码]`\n例如：`Furina 1234`\n\n- Emby 用户名不能包含空格\n- 安全码仅允许字母/数字\n- 请勿发送任何链接/域名/IP/URL\n- 点击“取消”返回"
	if gate.HasQualification {
		if gate.PendingDays > 0 {
			msg += fmt.Sprintf("\n\n你当前有资格：%d 天。", gate.PendingDays)
		} else {
			msg += "\n\n你当前有邀请码资格。"
		}
	}
	return msg
}

func (r *Router) startInviteCode(c telebot.Context) error {
	if c.Sender() == nil {
		return nil
	}
	return r.upsertUserConvoMessage(c, convoInviteCode, convoSession{}, nil, "请输入邀请码：\n\n点击“取消”返回。", r.cancelMenu())
}

func (r *Router) startRenewCode(c telebot.Context) error {
	if c.Sender() == nil {
		return nil
	}
	if r.acct == nil {
		return r.sendMainMenu(c, "续费功能未初始化。")
	}
	r.state.Clear(c.Sender().ID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	account, err := r.reg.Me(ctx, c.Sender().ID)
	if err != nil || account == nil || account.EmbyUserID == "" {
		return r.sendMainMenu(c, "你还没有注册，无法使用续费码。")
	}
	if account.ExpiresAt == nil {
		return r.sendMainMenu(c, "你的账号为无限期，无需续费。")
	}

	return r.upsertUserConvoMessage(c, convoRenewCode, convoSession{}, nil, "请在 2 分钟内发送续费码，例如：`Renewxxxxxx`。\n\n点击“取消”返回。", telebot.ModeMarkdown, r.cancelMenu())
}

func (r *Router) startResetPassword(c telebot.Context) error {
	if c.Sender() == nil {
		return nil
	}
	if r.acct == nil {
		return r.sendMainMenu(c, "账户管理功能未初始化。")
	}
	r.state.Clear(c.Sender().ID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	account, err := r.reg.Me(ctx, c.Sender().ID)
	if err != nil || account == nil || account.EmbyUserID == "" {
		return r.sendMainMenu(c, "你还没有注册，无法重置密码。")
	}

	msg := "请在 2 分钟内发送：`[安全码] [新密码|random/随机]`\n例如：`1234 random`\n\n- `random/Random/随机` 表示自动生成新密码\n- 新密码长度需 >= 8 且不能包含空格\n- 点击“取消”返回"
	return r.upsertUserConvoMessage(c, convoResetPassword, convoSession{}, nil, msg, telebot.ModeMarkdown, r.cancelMenu())
}

func (r *Router) startDeleteAccount(c telebot.Context) error {
	if c.Sender() == nil {
		return nil
	}
	if r.acct == nil {
		return r.sendMainMenu(c, "账户管理功能未初始化。")
	}
	r.state.Clear(c.Sender().ID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	account, err := r.reg.Me(ctx, c.Sender().ID)
	if err != nil || account == nil || account.EmbyUserID == "" {
		return r.sendMainMenu(c, "你还没有注册，无法删除账号。")
	}

	msg := "危险操作：将永久删除你的 Emby 账号。\n\n"
	if r.invite != nil {
		if lastUsedAt, err := r.invite.LatestUserInviteUsedAt(ctx, c.Sender().ID); err == nil && lastUsedAt != nil && !lastUsedAt.IsZero() {
			if time.Since(*lastUsedAt) <= 30*24*time.Hour {
				msg += "⚠️ 风控提示：你在近 30 天内成功邀请过用户；若此时主动删号，将连带注销你的后宫（后宫及其后宫，类推）。\n\n"
			}
		}
	}
	msg += "请在 2 分钟内发送：`[安全码] DELETE`\n例如：`1234 DELETE`\n\n点击“取消”返回。"
	return r.upsertUserConvoMessage(c, convoDeleteAccount, convoSession{}, nil, msg, telebot.ModeMarkdown, r.cancelMenu())
}

func (r *Router) startBind(c telebot.Context) error {
	if c.Sender() == nil {
		return nil
	}
	r.state.Clear(c.Sender().ID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	account, err := r.reg.Me(ctx, c.Sender().ID)
	if err == nil && account != nil && account.EmbyUserID != "" {
		return r.sendMainMenu(c, "你已经绑定/注册过了，无需重复绑定。")
	}

	return r.upsertUserConvoMessage(c, convoBindUsername, convoSession{}, nil, "请输入你的 Emby 用户名：\n\n点击“取消”返回。", r.cancelMenu())
}

func (r *Router) handleRegisterInput(c telebot.Context, text string) error {
	sess, _ := r.state.Get(c.Sender().ID)
	parts := strings.Fields(text)
	if len(parts) != 2 {
		return r.editWithSessionMessage(c, sess, "格式错误，请按 `[Emby用户名] [安全码]` 发送，例如：`Furina 1234`。", telebot.ModeMarkdown, r.cancelMenu())
	}
	_ = c.Delete()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// 预先读取“邀请码预留”信息：用于注册成功后通知邀请者（若为 /Harem 定向邀请或手动兑换邀请码）。
	var reservedBefore *registration.InviteCode
	if r.reg != nil {
		if v, err := r.reg.ReservedInviteCode(ctx, c.Sender().ID); err == nil && v != nil && strings.TrimSpace(v.Code) != "" {
			reservedBefore = v
		}
	}

	_, cred, err := r.reg.Register(ctx, c.Sender().ID, c.Sender().Username, parts[0], parts[1])
	if err != nil {
		logOp(c.Sender().ID, "注册", "结果", "失败", "原因", userFriendlyError(err))
		if errors.Is(err, registration.ErrAlreadyRegistered) {
			r.state.Clear(c.Sender().ID)
			return r.editWithSessionMessage(c, sess, "你已经注册过了，可以点“我的信息”查看账号。", r.mainPanelMenu(c))
		}
		if errors.Is(err, registration.ErrRegistrationClosed) || errors.Is(err, registration.ErrQuotaFull) {
			r.state.Clear(c.Sender().ID)
			return r.editWithSessionMessage(c, sess, "注册失败："+userFriendlyError(err)+"。如有邀请码，请先点“使用邀请码”。", r.mainPanelMenu(c))
		}
		return r.editWithSessionMessage(c, sess, "注册失败："+userFriendlyError(err), r.cancelMenu())
	}

	r.state.Clear(c.Sender().ID)
	logOp(c.Sender().ID, "注册", "结果", "成功")

	// /Harem 定向邀请：注册成功后通知邀请者“成功 + 下次可邀请时间”。
	if reservedBefore != nil &&
		reservedBefore.CreatorTelegramID != 0 &&
		reservedBefore.CreatorTelegramID != c.Sender().ID &&
		strings.HasPrefix(strings.ToUpper(strings.TrimSpace(reservedBefore.Code)), strings.ToUpper(inviteapp.UserInviteCodePrefix)) &&
		c.Bot() != nil {
		nextLine := "下次邀请：不限制冷却。"
		if r.inviteCooldownDays > 0 {
			// 冷却按“邀请码被使用的时间（used_at）”计算，取一次查询得到更精确的时间点。
			if r.invite != nil {
				if lastUsedAt, e := r.invite.LatestUserInviteUsedAt(ctx, reservedBefore.CreatorTelegramID); e == nil && lastUsedAt != nil && !lastUsedAt.IsZero() {
					next := lastUsedAt.AddDate(0, 0, r.inviteCooldownDays)
					nextLine = fmt.Sprintf("下次可邀请时间：`%s`（冷却 %d 天）", next.Local().Format("2006-01-02 15:04:05"), r.inviteCooldownDays)
				} else {
					next := time.Now().AddDate(0, 0, r.inviteCooldownDays)
					nextLine = fmt.Sprintf("下次可邀请时间：`%s`（冷却 %d 天，时间以实际兑换为准）", next.Local().Format("2006-01-02 15:04:05"), r.inviteCooldownDays)
				}
			} else {
				next := time.Now().AddDate(0, 0, r.inviteCooldownDays)
				nextLine = fmt.Sprintf("下次可邀请时间：`%s`（冷却 %d 天，时间以实际兑换为准）", next.Local().Format("2006-01-02 15:04:05"), r.inviteCooldownDays)
			}
		}

		creatorMsg := strings.Join([]string{
			"✅ 你的邀请已注册成功",
			"",
			nextLine,
			"",
			"你可在“用户功能 -> 我的后宫”查看/撤回（7 天内可撤回，撤回会注销对方账号）。",
		}, "\n")
		_ = r.trySendToUser(c.Bot(), reservedBefore.CreatorTelegramID, creatorMsg)
		logOp(reservedBefore.CreatorTelegramID, "邀请注册成功", "invitee", c.Sender().ID)
	}

	msg := "注册成功。\n\n"
	msg += fmt.Sprintf("用户名：`%s`\n", safeInlineCode(cred.Username))
	msg += fmt.Sprintf("密码：`%s`\n", safeInlineCode(cred.Password))
	msg += "\n请妥善保存密码（系统不会在以后再次显示）。"
	return r.editWithSessionMessage(c, sess, msg, telebot.ModeMarkdown, r.menus.UserOnly)
}

func (r *Router) handleInviteCodeInput(c telebot.Context, code string) error {
	sess, _ := r.state.Get(c.Sender().ID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	days, err := r.reg.RedeemInviteCode(ctx, c.Sender().ID, c.Sender().Username, code)
	if err != nil {
		logOp(c.Sender().ID, "兑换邀请码", "结果", "失败", "原因", userFriendlyError(err))
		return r.editWithSessionMessage(c, sess, "兑换失败："+userFriendlyError(err)+"\n\n请重新输入，或点击“取消”返回。", r.cancelMenu())
	}
	logOp(c.Sender().ID, "兑换邀请码", "结果", "成功", "天数", days)

	_ = c.Delete()
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gate, err := r.reg.Gate(ctx, c.Sender().ID, c.Sender().Username)
	if err != nil {
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, "获取注册状态失败："+userFriendlyError(err), r.mainPanelMenu(c))
	}
	prompt := r.buildRegisterPrompt(gate)
	msg := fmt.Sprintf("邀请码兑换成功，获得资格：%d 天。\n\n%s", days, prompt)
	r.setUserConvo(c, convoRegisterInput, sess, nil)
	return r.editWithSessionMessage(c, sess, msg, telebot.ModeMarkdown, r.cancelMenu())
}

func (r *Router) handleRenewCodeInput(c telebot.Context, code string) error {
	sess, _ := r.state.Get(c.Sender().ID)
	if r.acct == nil {
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, "续费功能未初始化。", r.mainPanelMenu(c))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	newExpiresAt, days, err := r.acct.RedeemRenewCode(ctx, c.Sender().ID, code)
	if err != nil {
		logOp(c.Sender().ID, "使用续费码", "结果", "失败", "原因", userFriendlyError(err))
		switch {
		case errors.Is(err, accountapp.ErrNotRegistered):
			r.state.Clear(c.Sender().ID)
			return r.editWithSessionMessage(c, sess, "你还没有注册，无法使用续费码。", r.mainPanelMenu(c))
		case errors.Is(err, accountapp.ErrUnlimitedAccount):
			r.state.Clear(c.Sender().ID)
			return r.editWithSessionMessage(c, sess, "你的账号为无限期，无需续费。", r.mainPanelMenu(c))
		case errors.Is(err, accountapp.ErrInvalidRenewCode):
			return r.editWithSessionMessage(c, sess, "续费码无效，请重新输入。\n\n点击“取消”返回。", r.cancelMenu())
		case errors.Is(err, registration.ErrInviteCodeUsed):
			return r.editWithSessionMessage(c, sess, "续费码已被使用，请重新输入。\n\n点击“取消”返回。", r.cancelMenu())
		case errors.Is(err, registration.ErrInviteCodeReserved):
			return r.editWithSessionMessage(c, sess, "续费码已被其他人锁定，请稍后再试。\n\n点击“取消”返回。", r.cancelMenu())
		default:
			r.state.Clear(c.Sender().ID)
			return r.editWithSessionMessage(c, sess, "续费失败："+userFriendlyError(err), r.userNavMenu())
		}
	}

	r.state.Clear(c.Sender().ID)
	logOp(c.Sender().ID, "使用续费码", "结果", "成功", "天数", days)
	_ = c.Delete()
	return r.editWithSessionMessage(c, sess, fmt.Sprintf("续费成功：+%d 天\n新到期时间：`%s`", days, newExpiresAt.Format("2006-01-02 15:04:05")), telebot.ModeMarkdown, r.userNavMenu())
}

func (r *Router) handleResetPasswordInput(c telebot.Context, text string) error {
	sess, _ := r.state.Get(c.Sender().ID)
	parts := strings.Fields(text)
	if len(parts) != 2 {
		return r.editWithSessionMessage(c, sess, "格式错误，请发送：`[安全码] [新密码|random/随机]`。", telebot.ModeMarkdown, r.cancelMenu())
	}
	secureCode := parts[0]
	newPassword := parts[1]
	_ = c.Delete()

	if r.acct == nil {
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, "账户管理功能未初始化。", r.mainPanelMenu(c))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, cred, err := r.acct.ResetPassword(ctx, c.Sender().ID, secureCode, newPassword)
	if err != nil {
		logOp(c.Sender().ID, "重置密码", "结果", "失败", "原因", userFriendlyError(err))
		switch {
		case errors.Is(err, accountapp.ErrNotRegistered):
			r.state.Clear(c.Sender().ID)
			return r.editWithSessionMessage(c, sess, "你还没有注册，无法重置密码。", r.mainPanelMenu(c))
		case errors.Is(err, accountapp.ErrInvalidSecureCode):
			return r.editWithSessionMessage(c, sess, "安全码错误，请重新输入。\n\n点击“取消”返回。", r.cancelMenu())
		case errors.Is(err, registration.ErrInvalidInput):
			return r.editWithSessionMessage(c, sess, "参数不合法，请重新输入。\n\n点击“取消”返回。", r.cancelMenu())
		default:
			r.state.Clear(c.Sender().ID)
			return r.editWithSessionMessage(c, sess, "重置密码失败："+userFriendlyError(err), r.userNavMenu())
		}
	}

	r.state.Clear(c.Sender().ID)
	logOp(c.Sender().ID, "重置密码", "结果", "成功")
	msg := "密码重置成功。\n\n"
	msg += fmt.Sprintf("用户名：`%s`\n", safeInlineCode(cred.Username))
	msg += fmt.Sprintf("新密码：`%s`\n", safeInlineCode(cred.Password))
	msg += "\n请妥善保存（系统不会再次显示）。"
	return r.editWithSessionMessage(c, sess, msg, telebot.ModeMarkdown, r.userNavMenu())
}

func (r *Router) handleDeleteAccountInput(c telebot.Context, text string) error {
	sess, _ := r.state.Get(c.Sender().ID)
	parts := strings.Fields(text)
	if len(parts) != 2 || !strings.EqualFold(parts[1], "DELETE") {
		return r.editWithSessionMessage(c, sess, "格式错误，请发送：`[安全码] DELETE`。", telebot.ModeMarkdown, r.cancelMenu())
	}
	secureCode := parts[0]
	_ = c.Delete()

	if r.acct == nil {
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, "账户管理功能未初始化。", r.mainPanelMenu(c))
	}

	// 防止“制造树根”：若用户在成功邀请后的 30 天内主动删号，则连带注销邀请树（后宫及其后宫，类推）。
	cascade := false
	var lastInviteUsedAt *time.Time
	var descendants []int64
	if r.invite != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		last, err := r.invite.LatestUserInviteUsedAt(ctx, c.Sender().ID)
		cancel()
		if err == nil && last != nil && !last.IsZero() {
			lastInviteUsedAt = last
			if time.Since(*lastInviteUsedAt) <= 30*24*time.Hour {
				cascade = true
			}
		}
		if cascade {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			desc, err := r.invite.Descendants(ctx, c.Sender().ID)
			cancel()
			if err == nil {
				descendants = uniqueInt64(desc)
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	account, err := r.acct.DeleteAccount(ctx, c.Sender().ID, secureCode)
	if err != nil {
		logOp(c.Sender().ID, "删除账号", "结果", "失败", "原因", userFriendlyError(err))
		switch {
		case errors.Is(err, accountapp.ErrNotRegistered):
			r.state.Clear(c.Sender().ID)
			return r.editWithSessionMessage(c, sess, "你还没有注册，无法删除账号。", r.mainPanelMenu(c))
		case errors.Is(err, accountapp.ErrInvalidSecureCode):
			return r.editWithSessionMessage(c, sess, "安全码错误，请重新输入。\n\n点击“取消”返回。", r.cancelMenu())
		case errors.Is(err, registration.ErrInvalidInput):
			return r.editWithSessionMessage(c, sess, "参数不合法，请重新输入。\n\n点击“取消”返回。", r.cancelMenu())
		default:
			r.state.Clear(c.Sender().ID)
			return r.editWithSessionMessage(c, sess, "删除账号失败："+userFriendlyError(err), r.userNavMenu())
		}
	}

	r.state.Clear(c.Sender().ID)
	logOp(c.Sender().ID, "删除账号", "结果", "成功")

	// 最佳努力清理“未使用邀请码”，避免删号后遗留可用邀请码形成根节点。
	if r.invite != nil {
		if n, err := r.invite.CleanupUnusedCodes(ctx, c.Sender().ID); err == nil && n > 0 {
			logOp(c.Sender().ID, "清理邀请码", "结果", "成功", "数量", n)
		}
	}

	cascadeOK := 0
	cascadeFail := 0
	if cascade && len(descendants) > 0 {
		reason := fmt.Sprintf("邀请者在 30 天内主动删号，连带清理（根 TGID=%d）", c.Sender().ID)
		cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer ccancel()
		for _, id := range descendants {
			if id == 0 {
				continue
			}
			// 使用“硬删除”语义：删除 DB 记录并尝试删除 Emby 用户。
			if r.adm != nil {
				_, err := r.adm.DeleteUser(cctx, id)
				if err != nil && !errors.Is(err, adminapp.ErrUserNotRegistered) && !errors.Is(err, registration.ErrNotFound) {
					cascadeFail++
					continue
				}
				cascadeOK++
			} else if r.revoker != nil {
				if err := r.revoker.RevokeAccount(cctx, id, reason); err != nil {
					cascadeFail++
					continue
				}
				cascadeOK++
			} else {
				cascadeFail++
				continue
			}

			if r.invite != nil {
				_, _ = r.invite.CleanupUnusedCodes(cctx, id)
			}
		}
		if lastInviteUsedAt != nil && !lastInviteUsedAt.IsZero() {
			logOp(c.Sender().ID, "删号连带清理", "结果", "完成", "根", c.Sender().ID, "近邀时间", lastInviteUsedAt.Format(time.RFC3339), "目标", len(descendants), "成功", cascadeOK, "失败", cascadeFail)
		} else {
			logOp(c.Sender().ID, "删号连带清理", "结果", "完成", "根", c.Sender().ID, "目标", len(descendants), "成功", cascadeOK, "失败", cascadeFail)
		}
	}

	msg := "账号已删除。\n\n"
	msg += fmt.Sprintf("Emby用户：`%s`\n", safeInlineCode(account.EmbyUsername))
	if cascade && len(descendants) > 0 {
		msg += fmt.Sprintf("\n因你在近 30 天内成功邀请过用户，系统已连带注销你的邀请树：目标 `%d`，成功 `%d`，失败 `%d`。\n", len(descendants), cascadeOK, cascadeFail)
	}
	msg += "\n如需重新开通，请重新注册。"
	return r.editWithSessionMessage(c, sess, msg, telebot.ModeMarkdown, r.userNavMenu())
}

func (r *Router) handleBindUsernameInput(c telebot.Context, text string) error {
	sess, _ := r.state.Get(c.Sender().ID)
	embyUsername := strings.TrimSpace(text)
	if embyUsername == "" {
		return r.editWithSessionMessage(c, sess, "请输入有效的 Emby 用户名。\n\n点击“取消”返回。", r.cancelMenu())
	}

	_ = c.Delete()
	r.setUserConvo(c, convoBindPassword, sess, map[string]string{
		"emby_username": embyUsername,
	})
	return r.editWithSessionMessage(c, sess, fmt.Sprintf("请输入用户 `%s` 的密码（我会尝试删除你发送的密码消息）：\n\n点击“取消”返回。", safeInlineCode(embyUsername)), telebot.ModeMarkdown, r.cancelMenu())
}

func (r *Router) handleBindPasswordInput(c telebot.Context, sess convoSession, password string) error {
	embyUsername := strings.TrimSpace(sess.Values["emby_username"])
	if embyUsername == "" {
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, "会话已过期，请重新开始绑定。", r.mainPanelMenu(c))
	}
	_ = c.Delete()

	r.setUserConvo(c, convoBindSecureCode, sess, map[string]string{
		"emby_username": embyUsername,
		"emby_password": password,
	})
	return r.editWithSessionMessage(c, sess, "请输入安全码（仅字母/数字）：\n\n点击“取消”返回。", r.cancelMenu())
}

func (r *Router) handleBindSecureCodeInput(c telebot.Context, sess convoSession, secureCode string) error {
	embyUsername := strings.TrimSpace(sess.Values["emby_username"])
	embyPassword := sess.Values["emby_password"]
	if embyUsername == "" || embyPassword == "" {
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, "会话已过期，请重新开始绑定。", r.mainPanelMenu(c))
	}
	_ = c.Delete()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	account, err := r.reg.Bind(ctx, c.Sender().ID, c.Sender().Username, embyUsername, embyPassword, secureCode)
	if err != nil {
		logOp(c.Sender().ID, "绑定已有账号", "结果", "失败", "原因", userFriendlyError(err))
		r.state.Clear(c.Sender().ID)
		return r.editWithSessionMessage(c, sess, "绑定失败："+userFriendlyError(err), r.userNavMenu())
	}

	r.state.Clear(c.Sender().ID)
	logOp(c.Sender().ID, "绑定已有账号", "结果", "成功")
	msg := "绑定成功。\n\n"
	msg += fmt.Sprintf("Emby用户名：`%s`\n", account.EmbyUsername)
	msg += fmt.Sprintf("EmbyUserID：`%s`\n", account.EmbyUserID)
	return r.editWithSessionMessage(c, sess, msg, telebot.ModeMarkdown, r.userNavMenu())
}
