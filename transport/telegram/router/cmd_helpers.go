package router

import (
	"strconv"
	"strings"

	"gopkg.in/telebot.v3"
)

func joinInt64ForMessage(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, ", ")
}

func (r *Router) extractTargetsFromCommand(c telebot.Context) (ids []int64, usernameHint string) {
	if c == nil || c.Message() == nil {
		return nil, ""
	}
	payload := strings.TrimSpace(c.Message().Payload)
	ids = parseTelegramIDs(payload)

	if c.Message().ReplyTo != nil && c.Message().ReplyTo.Sender != nil {
		ids = append([]int64{c.Message().ReplyTo.Sender.ID}, ids...)
		usernameHint = c.Message().ReplyTo.Sender.Username
	}
	ids = uniqueInt64(ids)
	return ids, usernameHint
}

func splitIDsAndReason(raw string) (idsPart string, reason string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	idx := strings.Index(raw, "原因：")
	if idx < 0 {
		return raw, ""
	}
	return strings.TrimSpace(raw[:idx]), strings.TrimSpace(raw[idx+len("原因："):])
}

func parseTelegramIDs(raw string) []int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', '，', ' ', '\n', '\t', ';', '；':
			return true
		default:
			return false
		}
	})
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if id, err := strconv.ParseInt(p, 10, 64); err == nil && id > 0 {
			out = append(out, id)
		}
	}
	return out
}

func uniqueInt64(ids []int64) []int64 {
	if len(ids) <= 1 {
		return ids
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// 已迁移至 panel_admin_keys.go
