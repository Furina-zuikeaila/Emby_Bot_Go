package router

import (
	"strings"
	"time"

	"gopkg.in/telebot.v3"
)

const groupCommandEphemeralTTL = 120 * time.Second

// groupCommandEphemeralMiddleware 在群组内对“命令交互”做自动清理：
// 1) 删除用户发出的命令消息；
// 2) 120 秒后自动删除机器人在群里发出的回复消息。
//
// 仅在群组/超级群生效，不影响私聊。
func (r *Router) groupCommandEphemeralMiddleware(next telebot.HandlerFunc) telebot.HandlerFunc {
	return func(c telebot.Context) error {
		if c == nil || c.Chat() == nil || c.Message() == nil {
			return next(c)
		}
		if isPrivateChat(c) {
			return next(c)
		}

		// 只对“命令消息”生效（以 '/' 开头）。
		// 注：未注册的命令通常不会进入任何 handler；这里针对的是“已识别并进入 handler 的命令”。
		text := strings.TrimSpace(c.Text())
		if !strings.HasPrefix(text, "/") {
			return next(c)
		}

		ec := &ephemeralContext{Context: c, ttl: groupCommandEphemeralTTL}
		err := next(ec)

		// 删除用户命令消息（最佳努力）。
		_ = ec.Delete()
		return err
	}
}

type ephemeralContext struct {
	telebot.Context
	ttl time.Duration
}

func (c *ephemeralContext) Send(what interface{}, opts ...interface{}) error {
	// telebot.Context.Send 的签名为 error（不返回 message）。
	// 这里改用 bot.Send 获取 message_id，从而可以定时删除。
	if c == nil || c.Context == nil || c.Bot() == nil || c.Chat() == nil {
		return telebot.ErrBadContext
	}
	msg, err := c.Bot().Send(c.Chat(), what, opts...)
	if err != nil || msg == nil {
		return err
	}
	c.scheduleDelete(msg)
	return nil
}

func (c *ephemeralContext) scheduleDelete(msg *telebot.Message) {
	if c == nil || msg == nil || c.ttl <= 0 {
		return
	}
	if c.Chat() == nil || c.Chat().Type == telebot.ChatPrivate {
		return
	}
	bot := c.Bot()
	if bot == nil {
		return
	}
	time.AfterFunc(c.ttl, func() {
		_ = bot.Delete(msg)
	})
}
