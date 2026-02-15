package router

import (
	"context"
	"strings"
	"time"

	"gopkg.in/telebot.v3"
)

func (r *Router) isGroupAdminSender(c telebot.Context) bool {
	if r == nil || c == nil || c.Sender() == nil {
		return false
	}
	senderID := c.Sender().ID
	if senderID == 0 {
		return false
	}
	// 管理员不需要走群管理列表
	if r.isAdminSender(c) {
		return false
	}

	r.groupAdminMu.Lock()
	cache := r.groupAdminCache
	updated := r.groupAdminUpdated
	r.groupAdminMu.Unlock()

	// 简单 TTL 缓存，避免每次命令都查 DB
	if time.Since(updated) > 30*time.Second {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = r.refreshGroupAdmins(ctx)
		r.groupAdminMu.Lock()
		cache = r.groupAdminCache
		r.groupAdminMu.Unlock()
	}

	_, ok := cache[senderID]
	return ok
}

func (r *Router) refreshGroupAdmins(ctx context.Context) error {
	if r == nil || r.regAdmin == nil {
		return nil
	}
	settings, err := r.regAdmin.GetSettings(ctx)
	if err != nil {
		return err
	}
	m := make(map[int64]struct{}, len(settings.GroupAdminIDs))
	for _, id := range settings.GroupAdminIDs {
		if id > 0 {
			m[id] = struct{}{}
		}
	}
	r.groupAdminMu.Lock()
	r.groupAdminCache = m
	r.groupAdminUpdated = time.Now()
	r.groupAdminMu.Unlock()
	return nil
}

func (r *Router) groupCommandGuardEnabled() bool {
	if r == nil {
		return false
	}
	r.guardMu.Lock()
	v := r.guardGroupCommandsEnabled
	r.guardMu.Unlock()
	return v
}

func (r *Router) toggleGroupCommandGuardEnabled() bool {
	if r == nil {
		return false
	}
	r.guardMu.Lock()
	r.guardGroupCommandsEnabled = !r.guardGroupCommandsEnabled
	v := r.guardGroupCommandsEnabled
	r.guardMu.Unlock()
	return v
}

func (r *Router) enforceGroupCommandMisuse(c telebot.Context) (bool, error) {
	if r == nil || c == nil || c.Sender() == nil || c.Message() == nil {
		return false, nil
	}
	// 只在群聊/超级群中生效（私聊不做治理）。
	if isPrivateChat(c) {
		return false, nil
	}
	if !r.groupCommandGuardEnabled() {
		return false, nil
	}

	// 管理员/群管理免疫。
	if r.isAdminSender(c) || r.isGroupAdminSender(c) {
		return false, nil
	}

	// 判断是否白名单、是否已经拥有 Emby 账号。
	var (
		isWhitelist    bool
		hasEmbyAccount bool
	)
	if r.reg != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		account, err := r.reg.Me(ctx, c.Sender().ID)
		cancel()
		if err == nil && account != nil {
			isWhitelist = account.IsWhitelist
			hasEmbyAccount = strings.TrimSpace(account.EmbyUserID) != ""
		}
	}
	if isWhitelist {
		return false, nil
	}

	// 一律删除触发的命令消息，避免在群内传播“操作入口”。
	_ = c.Delete()

	// 若用户已拥有 Emby 账号：立即删号，并（最佳努力）通过私信告知。
	if hasEmbyAccount && r.revoker != nil {
		reason := "群组内使用命令"
		if txt := strings.TrimSpace(c.Message().Text); strings.HasPrefix(txt, "/") {
			if parts := strings.Fields(txt); len(parts) > 0 && strings.HasPrefix(parts[0], "/") {
				reason = reason + "：" + parts[0]
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = r.revoker.RevokeAccount(ctx, c.Sender().ID, reason)
		logOp(c.Sender().ID, "群内使用命令", "结果", "已删号", "命令", reason)
	} else {
		logOp(c.Sender().ID, "群内使用命令", "结果", "已删消息")
	}
	return true, nil
}
