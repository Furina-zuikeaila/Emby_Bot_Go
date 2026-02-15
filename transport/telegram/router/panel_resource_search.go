//go:build moviepilot
// +build moviepilot

package router

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	resourceapp "emby-bot-new/internal/application/resource"

	"gopkg.in/telebot.v3"
)

type searchQuery struct {
	Raw     string
	Keyword string
	Season  int
	Episode int
}

func (r *Router) handleResourceSearch(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})
	return r.startResourceSearch(c)
}

func (r *Router) handleResourceSearchCmd(c telebot.Context) error {
	if c == nil || c.Sender() == nil || c.Message() == nil {
		return nil
	}
	if !isPrivateChat(c) {
		// 群里尽量少打扰；中间件会自动清理命令消息。
		return c.Send("请私聊我操作")
	}

	q := parseSearchQuery(strings.TrimSpace(c.Message().Payload))
	if q.Keyword == "" {
		return r.startResourceSearch(c)
	}
	r.state.Clear(c.Sender().ID)
	return r.runResourceSearch(c, q)
}

func (r *Router) startResourceSearch(c telebot.Context) error {
	if c == nil || c.Sender() == nil {
		return nil
	}
	if r.res == nil {
		return r.editOrSendText(c, "资源搜索未启用，请联系管理员。", r.userNavMenu())
	}
	r.state.Clear(c.Sender().ID)
	msg := strings.Join([]string{
		"🔎 资源搜索",
		"",
		"请输入要搜索的关键词，例如：`沙丘`、`Dune`。",
		"更精准可带季/集：`剧名 S02E05`、`剧名 第2季 第5集`。",
		"",
		"也可以直接用命令：`/search 关键词`",
		"搜索结果可点序号按钮，将资源自动推送到 MoviePilot 下载。",
		"",
		"点击“取消”返回。",
	}, "\n")
	return r.upsertUserConvoMessage(c, convoResourceSearch, convoSession{}, nil, msg, telebot.ModeMarkdown, r.cancelMenu())
}

func (r *Router) handleResourceSearchInput(c telebot.Context, keyword string) error {
	if c == nil || c.Sender() == nil {
		return nil
	}
	if r.res == nil {
		r.state.Clear(c.Sender().ID)
		return r.editOrSendText(c, "资源搜索未启用，请联系管理员。", r.userNavMenu())
	}

	q := parseSearchQuery(keyword)
	if q.Keyword == "" {
		return r.editOrSendText(c, "请输入关键词。", r.cancelMenu())
	}
	r.state.Clear(c.Sender().ID)
	return r.runResourceSearch(c, q)
}

func (r *Router) runResourceSearch(c telebot.Context, q searchQuery) error {
	if r.res == nil {
		return r.editOrSendText(c, "资源搜索未启用，请联系管理员。", r.userNavMenu())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	items, err := r.res.Search(ctx, q.Raw)
	if err != nil {
		return r.editOrSendText(c, "搜索失败："+userFriendlyError(err), r.userNavMenu())
	}
	if len(items) == 0 {
		return r.editOrSendText(c, fmt.Sprintf("未找到结果：`%s`", safeInlineCode(q.Keyword)), telebot.ModeMarkdown, r.userNavMenu())
	}

	if q.Season > 0 || q.Episode > 0 {
		filtered := make([]resourceapp.Result, 0, len(items))
		for _, it := range items {
			title := strings.TrimSpace(it.Title)
			if title == "" {
				continue
			}
			if matchSeasonEpisode(title, q.Season, q.Episode) {
				filtered = append(filtered, it)
			}
		}
		if len(filtered) > 0 {
			items = filtered
		}
	}

	// 缓存本次结果，支持“回复序号拿 magnet/link”。
	if c.Sender() != nil && r.rs != nil {
		r.rs.Set(c.Sender().ID, items)
		r.state.Set(c.Sender().ID, convoResourceSearchPick, nil)
	}

	lines := make([]string, 0, 4+len(items)*2)
	lines = append(lines, "🔎 搜索结果")
	lines = append(lines, "——————————————")
	lines = append(lines, fmt.Sprintf("关键词：`%s`", safeInlineCode(q.Keyword)))
	if q.Season > 0 || q.Episode > 0 {
		lines = append(lines, fmt.Sprintf("过滤：%s", formatSeasonEpisode(q.Season, q.Episode)))
	}
	lines = append(lines, "")
	for i, it := range items {
		title := strings.TrimSpace(it.Title)
		if title == "" {
			title = "（无标题）"
		}
		meta := make([]string, 0, 4)
		if s := strings.TrimSpace(it.Site); s != "" {
			meta = append(meta, s)
		}
		if s := strings.TrimSpace(it.Size); s != "" {
			meta = append(meta, s)
		}
		if it.Seeders > 0 {
			meta = append(meta, fmt.Sprintf("S:%d", it.Seeders))
		}
		if it.Leechers > 0 {
			meta = append(meta, fmt.Sprintf("L:%d", it.Leechers))
		}
		line := fmt.Sprintf("%d. `%s`", i+1, safeInlineCode(title))
		if len(meta) > 0 {
			line += "（" + strings.Join(meta, " | ") + "）"
		}
		lines = append(lines, line)
	}
	lines = append(lines, "")
	lines = append(lines, "回复序号（或点下面按钮）将该条推送到 MoviePilot 下载，并发送对应 magnet/下载链接。")

	return r.editOrSendText(c, strings.Join(lines, "\n"), telebot.ModeMarkdown, r.resourcePickMenu(len(items)))
}

func (r *Router) handleResourcePickCb(c telebot.Context) error {
	if !isPrivateChat(c) {
		return c.Respond(&telebot.CallbackResponse{Text: "请私聊我操作", ShowAlert: false})
	}
	if c == nil || c.Sender() == nil {
		return nil
	}
	_ = c.Respond(&telebot.CallbackResponse{})

	if r.rs == nil {
		return r.editOrSendText(c, "搜索结果已过期，请重新搜索。", r.userNavMenu())
	}
	n, err := strconv.Atoi(strings.TrimSpace(c.Data()))
	if err != nil || n <= 0 {
		return nil
	}
	items, ok := r.rs.Get(c.Sender().ID)
	if !ok || len(items) == 0 {
		return r.editOrSendText(c, "搜索结果已过期，请重新搜索。", r.userNavMenu())
	}
	if n > len(items) {
		return r.editOrSendText(c, fmt.Sprintf("序号超出范围（1-%d）。", len(items)), r.resourcePickMenu(len(items)))
	}

	it := items[n-1]
	title := strings.TrimSpace(it.Title)
	if title == "" {
		title = "（无标题）"
	}
	link := strings.TrimSpace(it.Link)
	if link == "" {
		return r.editOrSendText(c, "该条未提供 magnet/下载链接。", r.resourcePickMenu(len(items)))
	}
	return r.enqueueMPDownload(c, items[n-1], n, len(items))
}

func (r *Router) handleResourceSearchPickInput(c telebot.Context, text string) error {
	if c == nil || c.Sender() == nil {
		return nil
	}
	if r.rs == nil {
		r.state.Clear(c.Sender().ID)
		return r.editOrSendText(c, "搜索结果已过期，请重新搜索。", r.userNavMenu())
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if strings.EqualFold(text, "取消") || strings.EqualFold(text, "cancel") {
		r.state.Clear(c.Sender().ID)
		r.rs.Clear(c.Sender().ID)
		return r.sendMainMenu(c, "已取消。")
	}

	// 如果输入是序号，则返回该条的完整链接；否则视为新关键词继续搜索。
	if n, err := strconv.Atoi(text); err == nil && n > 0 {
		items, ok := r.rs.Get(c.Sender().ID)
		if !ok || len(items) == 0 {
			r.state.Clear(c.Sender().ID)
			return r.editOrSendText(c, "搜索结果已过期，请重新搜索。", r.userNavMenu())
		}
		if n > len(items) {
			return r.editOrSendText(c, fmt.Sprintf("序号超出范围（1-%d）。", len(items)), r.userNavMenu())
		}
		it := items[n-1]
		title := strings.TrimSpace(it.Title)
		if title == "" {
			title = "（无标题）"
		}
		link := strings.TrimSpace(it.Link)
		if link == "" {
			return r.editOrSendText(c, "该条未提供 magnet/下载链接。", r.userNavMenu())
		}
		return r.enqueueMPDownload(c, items[n-1], n, len(items))
	}

	q := parseSearchQuery(text)
	if q.Keyword == "" {
		return r.editOrSendText(c, "请输入关键词或序号。", r.userNavMenu())
	}
	// 保持在 pick 状态，便于继续拿链接/继续搜索。
	return r.runResourceSearch(c, q)
}

func (r *Router) enqueueMPDownload(c telebot.Context, it resourceapp.Result, idx int, total int) error {
	if c == nil || c.Sender() == nil {
		return nil
	}
	if r.res == nil || r.mpq == nil {
		return r.editOrSendText(c, "功能未初始化。", r.userNavMenu())
	}
	if !r.res.PushEnabled() {
		return r.editOrSendText(c, "（未开启自动推送，请联系管理员开启 `MP_PUSH_ENABLED=true`）", r.resourcePickMenu(total))
	}

	chatID := int64(0)
	if c.Chat() != nil {
		chatID = c.Chat().ID
	}
	pos, enqOK := r.mpq.Enqueue(mpPushItem{
		ChatID: chatID,
		UserID: c.Sender().ID,
		Title:  strings.TrimSpace(it.Title),
		Site:   strings.TrimSpace(it.Site),
		Size:   strings.TrimSpace(it.Size),
		Link:   strings.TrimSpace(it.Link),
		Push:   r.res.Push,
	})
	if !enqOK {
		return r.editOrSendText(c, "队列已满，请稍后再试。", r.resourcePickMenu(total))
	}

	title := strings.TrimSpace(it.Title)
	if title == "" {
		title = "（无标题）"
	}
	meta := []string{}
	if s := strings.TrimSpace(it.Site); s != "" {
		meta = append(meta, safeInlineCode(s))
	}
	if s := strings.TrimSpace(it.Size); s != "" {
		meta = append(meta, safeInlineCode(s))
	}
	metaStr := ""
	if len(meta) > 0 {
		metaStr = "\n信息：`" + safeInlineCode(strings.Join(meta, " | ")) + "`"
	}
	msg := strings.Join([]string{
		"🧾 已加入下载队列",
		"——————————————",
		fmt.Sprintf("序号：`%d`", idx),
		fmt.Sprintf("队列位置：`%d`", pos),
		fmt.Sprintf("资源：`%s`%s", safeInlineCode(title), metaStr),
		"",
		"（处理完成后会通知你结果）",
	}, "\n")
	return r.editOrSendText(c, msg, telebot.ModeMarkdown, r.resourcePickMenu(total))
}

func (r *Router) resourcePickMenu(count int) *telebot.ReplyMarkup {
	menu := &telebot.ReplyMarkup{}
	if count <= 0 {
		menu.Inline(
			menu.Row(
				menu.Data("⬅️ 用户功能", CbUserPanel),
				menu.Data("🏠 主菜单", CbBackMain),
			),
		)
		return menu
	}
	if count > 12 {
		count = 12
	}
	rows := make([]telebot.Row, 0, 6)
	btns := make([]telebot.Btn, 0, count)
	for i := 1; i <= count; i++ {
		btns = append(btns, menu.Data(strconv.Itoa(i), CbResourcePick, strconv.Itoa(i)))
	}
	for i := 0; i < len(btns); i += 4 {
		end := i + 4
		if end > len(btns) {
			end = len(btns)
		}
		rows = append(rows, menu.Row(btns[i:end]...))
	}
	rows = append(rows, menu.Row(
		menu.Data("⬅️ 用户功能", CbUserPanel),
		menu.Data("🏠 主菜单", CbBackMain),
	))
	menu.Inline(rows...)
	return menu
}

func safeInlineCodeMax(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "`", "'")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	if max > 0 && len(s) > max {
		s = s[:max]
	}
	return s
}

// --- query parsing / filtering ---

var (
	reSxxEyy = regexp.MustCompile(`(?i)\bS(\d{1,2})\s*E(\d{1,2})\b`)
	reSxx    = regexp.MustCompile(`(?i)\bS(\d{1,2})\b`)
	reEyy    = regexp.MustCompile(`(?i)\bE(\d{1,2})\b`)
	reXyy    = regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{1,2})\b`)
	reCnSE   = regexp.MustCompile(`第\s*(\d{1,2})\s*季.*第\s*(\d{1,3})\s*集`)
	reCnS    = regexp.MustCompile(`第\s*(\d{1,2})\s*季`)
	reCnE    = regexp.MustCompile(`第\s*(\d{1,3})\s*集`)
)

func parseSearchQuery(raw string) searchQuery {
	raw = strings.TrimSpace(raw)
	q := searchQuery{Raw: raw, Keyword: raw}
	if raw == "" {
		return q
	}

	season, episode := 0, 0
	switch {
	case reSxxEyy.MatchString(raw):
		m := reSxxEyy.FindStringSubmatch(raw)
		season, _ = strconv.Atoi(m[1])
		episode, _ = strconv.Atoi(m[2])
	case reXyy.MatchString(raw):
		m := reXyy.FindStringSubmatch(raw)
		season, _ = strconv.Atoi(m[1])
		episode, _ = strconv.Atoi(m[2])
	case reCnSE.MatchString(raw):
		m := reCnSE.FindStringSubmatch(raw)
		season, _ = strconv.Atoi(m[1])
		episode, _ = strconv.Atoi(m[2])
	default:
		// 尝试只给了季或集
		if reCnS.MatchString(raw) {
			m := reCnS.FindStringSubmatch(raw)
			season, _ = strconv.Atoi(m[1])
		} else if reSxx.MatchString(raw) {
			m := reSxx.FindStringSubmatch(raw)
			season, _ = strconv.Atoi(m[1])
		}
		if reCnE.MatchString(raw) {
			m := reCnE.FindStringSubmatch(raw)
			episode, _ = strconv.Atoi(m[1])
		} else if reEyy.MatchString(raw) {
			m := reEyy.FindStringSubmatch(raw)
			episode, _ = strconv.Atoi(m[1])
		}
	}

	if season < 0 || season > 99 {
		season = 0
	}
	if episode < 0 || episode > 999 {
		episode = 0
	}
	q.Season = season
	q.Episode = episode

	// Keyword 用于展示；Raw 仍然保留原字符串用于上游搜索（更可能命中站点标题）。
	q.Keyword = strings.TrimSpace(raw)
	return q
}

func formatSeasonEpisode(season, episode int) string {
	if season > 0 && episode > 0 {
		return fmt.Sprintf("S%02dE%02d", season, episode)
	}
	if season > 0 {
		return fmt.Sprintf("S%02d", season)
	}
	if episode > 0 {
		return fmt.Sprintf("E%02d", episode)
	}
	return ""
}

func matchSeasonEpisode(title string, season, episode int) bool {
	title = strings.TrimSpace(title)
	if title == "" {
		return false
	}
	low := strings.ToLower(title)
	// 先用最常见的 SxxEyy 判断
	if season > 0 && episode > 0 {
		p1 := fmt.Sprintf("s%02de%02d", season, episode)
		p2 := fmt.Sprintf("s%de%d", season, episode)
		p3 := fmt.Sprintf("%dx%02d", season, episode)
		p4 := fmt.Sprintf("第%d季", season)
		p5 := fmt.Sprintf("第%d集", episode)
		if strings.Contains(low, p1) || strings.Contains(low, p2) || strings.Contains(low, p3) {
			return true
		}
		// 中文分开出现也算（标题里可能被其它字段隔开）
		return strings.Contains(title, p4) && strings.Contains(title, p5)
	}
	if season > 0 {
		p1 := fmt.Sprintf("s%02d", season)
		p2 := fmt.Sprintf("s%d", season)
		p3 := fmt.Sprintf("第%d季", season)
		return strings.Contains(low, p1) || strings.Contains(low, p2) || strings.Contains(title, p3)
	}
	if episode > 0 {
		p1 := fmt.Sprintf("e%02d", episode)
		p2 := fmt.Sprintf("e%d", episode)
		p3 := fmt.Sprintf("第%d集", episode)
		return strings.Contains(low, p1) || strings.Contains(low, p2) || strings.Contains(title, p3)
	}
	return true
}
