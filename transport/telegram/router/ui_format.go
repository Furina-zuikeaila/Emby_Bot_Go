package router

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/telebot.v3"
)

// safeInlineCode 用于把用户可控字符串变得“适合放进 Telegram Markdown 行内代码（`...`）”：
// 去掉换行/制表、替换反引号，并限制长度，避免破坏排版或注入额外格式。
func safeInlineCode(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "`", "'")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	if len(s) > 128 {
		s = s[:128]
	}
	return s
}

func formatLastActive(t *time.Time) string {
	if t == nil {
		return "无"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func formatExpiresAt(t *time.Time) string {
	if t == nil {
		return "无限期"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func formatCooldownDuration(d time.Duration) string {
	if d <= 0 {
		return "未启用"
	}
	// 优先展示小时，避免 168h0m0s 这种噪音；并补充天数方便理解。
	if d%time.Hour == 0 {
		h := int(d / time.Hour)
		if h%24 == 0 {
			days := h / 24
			return fmt.Sprintf("%dh（%d 天）", h, days)
		}
		return fmt.Sprintf("%dh", h)
	}
	return d.String()
}

func formatCountdown(d time.Duration) string {
	if d <= 0 {
		return "0"
	}
	// 向上取整到分钟，避免 UI 一直跳秒数。
	d = d.Round(time.Minute)
	if d < time.Minute {
		d = time.Minute
	}
	totalMinutes := int(d.Minutes())
	days := totalMinutes / (60 * 24)
	hours := (totalMinutes % (60 * 24)) / 60
	mins := totalMinutes % 60

	parts := make([]string, 0, 3)
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d 天", days))
	}
	if hours > 0 || days > 0 {
		parts = append(parts, fmt.Sprintf("%d 小时", hours))
	}
	parts = append(parts, fmt.Sprintf("%d 分钟", mins))
	return strings.Join(parts, " ")
}

func startCaption(u *telebot.User) string {
	if u == nil {
		return "✨ 欢迎回来"
	}
	return fmt.Sprintf("✨ 欢迎回来\n\n🍉 你好 %s (ID:%d)\n请选择下面功能 👇", userDisplayName(u), u.ID)
}

func userDisplayName(u *telebot.User) string {
	if u == nil {
		return "朋友"
	}

	name := strings.TrimSpace(u.FirstName)
	last := strings.TrimSpace(u.LastName)
	switch {
	case name != "" && last != "":
		name = name + " | " + last
	case name == "":
		name = last
	}
	if name == "" && strings.TrimSpace(u.Username) != "" {
		name = "@" + strings.TrimSpace(u.Username)
	}
	if name == "" {
		name = "朋友"
	}
	return name
}

func isMediaMessage(m *telebot.Message) bool {
	if m == nil {
		return false
	}
	return m.Photo != nil ||
		m.Video != nil ||
		m.Animation != nil ||
		m.Document != nil ||
		m.Audio != nil ||
		m.Voice != nil ||
		m.VideoNote != nil
}

// formatPushedCard 统一“机器人主动推送”的消息样式（私信/群通知），尽量与注册管理面板风格一致。
func formatPushedCard(title string, lines ...string) string {
	var b strings.Builder
	title = strings.TrimSpace(title)
	if title == "" {
		title = "通知"
	}
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString("——————————————\n")
	for _, line := range lines {
		line = strings.TrimRight(strings.TrimSpace(line), "\n")
		if line == "" {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
