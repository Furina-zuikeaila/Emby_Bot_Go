//go:build moviepilot
// +build moviepilot

package router

import (
	"context"
	"fmt"
	"time"

	"gopkg.in/telebot.v3"
)

func (r *Router) handleMPCheckCmd(c telebot.Context) error {
	if c == nil || c.Sender() == nil {
		return nil
	}
	if !isPrivateChat(c) {
		return c.Send("请私聊我操作")
	}
	if !r.isAdminSender(c) {
		return nil
	}
	if r.res == nil {
		return c.Send("MoviePilot 未启用（MP_ENABLED=false 或未初始化）。")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	code, err := r.res.Check(ctx)
	if err != nil {
		return c.Send(fmt.Sprintf("MoviePilot 检查失败：%s\nHTTP：%d", userFriendlyError(err), code))
	}
	return c.Send(fmt.Sprintf("MoviePilot 检查通过。\nHTTP：%d", code))
}
