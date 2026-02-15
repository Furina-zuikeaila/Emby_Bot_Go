package router

import (
	"context"
	"time"
)

// 常用超时集中定义，避免在业务代码里散落大量 magic number。
//
// 约定：
// - Telegram handler 侧的 timeout 以“尽快响应用户”为主；
// - 对外部依赖（DB/Emby/Telegram API）调用统一通过 context 超时保护，避免 handler 堵死。
const (
	timeout5s  = 5 * time.Second
	timeout10s = 10 * time.Second
	timeout30s = 30 * time.Second
)

// bgCtxWithTimeout 创建一个带超时的后台 context。
//
// Telegram 的 telebot.Context 并不直接提供可传递的 request context；
// 因此这里统一用 Background 并显式设置超时，保证所有外部调用都有上限。
func bgCtxWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
