package router

import (
	"strings"

	"gopkg.in/telebot.v3"
)

type accountFlags struct {
	isWhitelist    bool
	hasEmbyAccount bool
}

// getAccountFlags 查询用户的“白名单/是否有 Emby 账号”等状态，用于治理逻辑与 UI 判断。
//
// 注意：
// - 这是“最佳努力”查询：失败时返回全 false，调用方应按更保守的策略处理。
// - 统一使用超时，避免在群消息高并发时拖垮 handler。
func (r *Router) getAccountFlags(sender *telebot.User) accountFlags {
	if r == nil || r.reg == nil || sender == nil || sender.ID == 0 {
		return accountFlags{}
	}

	ctx, cancel := bgCtxWithTimeout(timeout10s)
	defer cancel()

	account, err := r.reg.Me(ctx, sender.ID)
	if err != nil || account == nil {
		return accountFlags{}
	}

	return accountFlags{
		isWhitelist:    account.IsWhitelist,
		hasEmbyAccount: strings.TrimSpace(account.EmbyUserID) != "",
	}
}

func (r *Router) isRegisteredUser(sender *telebot.User) bool {
	return r.getAccountFlags(sender).hasEmbyAccount
}
