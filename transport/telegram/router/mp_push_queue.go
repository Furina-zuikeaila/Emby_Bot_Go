//go:build moviepilot
// +build moviepilot

package router

import (
	"context"
	"errors"
	"sync"
	"time"

	resourceapp "emby-bot-new/internal/application/resource"

	"gopkg.in/telebot.v3"
)

type mpPushQueue struct {
	ch   chan mpPushItem
	once sync.Once

	mu       sync.Mutex
	inFlight bool
	bot      *telebot.Bot
}

type mpPushItem struct {
	ChatID int64
	UserID int64
	Title  string
	Site   string
	Size   string
	Link   string
	Push   func(ctx context.Context, link string) error
}

func newMPPushQueue(buffer int) *mpPushQueue {
	if buffer <= 0 {
		buffer = 50
	}
	return &mpPushQueue{ch: make(chan mpPushItem, buffer)}
}

func (q *mpPushQueue) Start(bot *telebot.Bot) {
	if q == nil {
		return
	}
	q.mu.Lock()
	q.bot = bot
	q.mu.Unlock()
	q.once.Do(func() {
		go q.worker()
	})
}

func (q *mpPushQueue) Enqueue(it mpPushItem) (position int, ok bool) {
	if q == nil {
		return 0, false
	}
	q.mu.Lock()
	inFlight := q.inFlight
	q.mu.Unlock()
	position = len(q.ch)
	if inFlight {
		position++
	}
	position++

	select {
	case q.ch <- it:
		return position, true
	default:
		return 0, false
	}
}

func (q *mpPushQueue) worker() {
	for it := range q.ch {
		q.mu.Lock()
		q.inFlight = true
		bot := q.bot
		q.mu.Unlock()

		if it.Push == nil || it.ChatID == 0 || bot == nil {
			q.mu.Lock()
			q.inFlight = false
			q.mu.Unlock()
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		err := it.Push(ctx, it.Link)
		cancel()

		msg := ""
		if err == nil {
			msg = "✅ 已通知 MoviePilot 开始下载。"
		} else {
			if errors.Is(err, resourceapp.ErrPushDisabled) {
				msg = "（未开启自动推送，请联系管理员开启 `MP_PUSH_ENABLED=true`）"
			} else {
				msg = "⚠️ 推送到 MoviePilot 失败：" + userFriendlyError(err)
			}
		}

		title := it.Title
		if title == "" {
			title = "（无标题）"
		}
		site := safeInlineCode(it.Site)
		size := safeInlineCode(it.Size)
		meta := ""
		if site != "" && size != "" {
			meta = "（" + site + " | " + size + "）"
		} else if site != "" {
			meta = "（" + site + "）"
		} else if size != "" {
			meta = "（" + size + "）"
		}

		_, _ = bot.Send(&telebot.Chat{ID: it.ChatID}, msg+"\n`"+safeInlineCode(title)+"`"+meta, telebot.ModeMarkdown)

		q.mu.Lock()
		q.inFlight = false
		q.mu.Unlock()
	}
}
