package router

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	accountapp "emby-bot-new/internal/application/account"
	adminapp "emby-bot-new/internal/application/admin"
	"emby-bot-new/internal/application/registration"
)

type classifiedError struct {
	Code    string
	Summary string
	Range   string
}

func (e classifiedError) String() string {
	code := strings.TrimSpace(e.Code)
	if code == "" {
		code = "E0"
	}
	summary := strings.TrimSpace(e.Summary)
	if summary == "" {
		summary = "操作失败，请稍后再试"
	}
	rng := strings.TrimSpace(e.Range)
	if rng == "" {
		return fmt.Sprintf("[%s] %s", code, summary)
	}
	return fmt.Sprintf("[%s] %s（%s）", code, summary, rng)
}

// userFriendlyError 把内部错误转换为“用户可见”的错误信息。
//
// 目标：
// - 统一对外展示格式：`[EXX] 一句话说明（范围）`
// - 让用户/管理员能快速定位大类问题（输入、注册、网络、数据库、Emby、Telegram…）
//
// 安全约束（非常重要）：
// - 绝不回显原始 error 字符串，避免泄露敏感信息：
//   - Emby URL / 具体接口路径 / 请求参数 / 响应 body
//   - 数据库 DSN / host / 账户信息
//   - Telegram 平台返回的详细错误（可能包含 chat id 等）
//
// 做法：
// - 先识别已知业务错误（registration/account/admin 自定义错误）。
// - 再做“尽力而为”的平台/基础设施分类（通过错误文本特征识别 Emby/Telegram/DB 等）。
//
// 约定：
// - 未归类错误统一返回 E0（未知）。
func userFriendlyError(err error) string {
	return classifyError(err).String()
}

// embyStatusRe 用于从内部错误信息中抽取 Emby HTTP status（例如 "status=404"）。
// 之所以不直接把原错误透传给用户，是为了避免泄露 endpoint/body 等敏感细节。
var embyStatusRe = regexp.MustCompile(`status=(\d{3})`)

// extractEmbyStatusCode 从错误文本中尽力解析 Emby 返回的 HTTP 状态码。
// 返回 (code, ok)。当 ok=false 时表示无法识别或不可信。
func extractEmbyStatusCode(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	m := embyStatusRe.FindStringSubmatch(err.Error())
	if len(m) != 2 {
		return 0, false
	}
	v, convErr := strconv.Atoi(m[1])
	if convErr != nil || v < 100 {
		return 0, false
	}
	return v, true
}

// classifyError 将内部错误归类为稳定的错误码 + 摘要 + 范围。
//
// 约定：
// - Code：稳定且短（E01/E20/E32…），便于文档化与客服排查。
// - Summary：给用户看的“下一步”指引，避免术语与实现细节。
// - Range：给管理员/排查用的粗粒度范围（例如 Emby/HTTP 403、数据库、Telegram…）。
func classifyError(err error) classifiedError {
	if err == nil {
		return classifiedError{Code: "E0", Summary: "操作失败，请稍后再试", Range: "未知"}
	}

	low := strings.ToLower(err.Error())

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return classifiedError{Code: "E20", Summary: "请求超时，请稍后再试", Range: "网络/超时"}
	case errors.Is(err, context.Canceled):
		return classifiedError{Code: "E20", Summary: "请求已取消，请稍后再试", Range: "网络/取消"}

	case errors.Is(err, registration.ErrInvalidInput):
		return classifiedError{Code: "E01", Summary: "输入格式不正确", Range: "输入"}
	case errors.Is(err, registration.ErrAlreadyRegistered):
		return classifiedError{Code: "E02", Summary: "已注册，无需重复操作", Range: "注册"}
	case errors.Is(err, registration.ErrRegistrationClosed):
		return classifiedError{Code: "E03", Summary: "注册已关闭", Range: "注册"}
	case errors.Is(err, registration.ErrQuotaFull):
		return classifiedError{Code: "E04", Summary: "名额已满", Range: "注册"}
	case errors.Is(err, registration.ErrInvalidInviteCode):
		return classifiedError{Code: "E05", Summary: "邀请码无效", Range: "邀请码"}
	case errors.Is(err, registration.ErrInviteCodeUsed):
		return classifiedError{Code: "E05", Summary: "邀请码已被使用", Range: "邀请码"}
	case errors.Is(err, registration.ErrInviteCodeReserved):
		return classifiedError{Code: "E05", Summary: "邀请码已被占用，请稍后再试", Range: "邀请码"}
	case errors.Is(err, registration.ErrEmbyAlreadyBound):
		return classifiedError{Code: "E02", Summary: "该 Emby 账号已被绑定", Range: "绑定"}
	case errors.Is(err, registration.ErrNotFound):
		return classifiedError{Code: "E30", Summary: "数据不存在", Range: "数据库"}

	case errors.Is(err, accountapp.ErrNotRegistered):
		return classifiedError{Code: "E10", Summary: "你还没有注册", Range: "账号"}
	case errors.Is(err, accountapp.ErrInvalidSecureCode):
		return classifiedError{Code: "E06", Summary: "安全码错误", Range: "安全码"}
	case errors.Is(err, accountapp.ErrUnlimitedAccount):
		return classifiedError{Code: "E11", Summary: "账号为无限期", Range: "账号"}
	case errors.Is(err, accountapp.ErrInvalidRenewCode):
		return classifiedError{Code: "E05", Summary: "续费码无效", Range: "续费码"}

	case errors.Is(err, adminapp.ErrUserNotRegistered):
		return classifiedError{Code: "E10", Summary: "该用户未注册", Range: "账号"}
	}

	// Emby API 错误：只对外暴露 HTTP 状态码，绝不回显 endpoint/body。
	if status, ok := extractEmbyStatusCode(err); ok {
		switch {
		case status == 401 || status == 403:
			return classifiedError{Code: "E21", Summary: "Emby 鉴权失败", Range: fmt.Sprintf("Emby/HTTP %d", status)}
		case status == 404 || status == 405:
			return classifiedError{Code: "E22", Summary: "Emby 接口不可用", Range: fmt.Sprintf("Emby/HTTP %d", status)}
		case status == 429:
			return classifiedError{Code: "E23", Summary: "请求过于频繁，请稍后再试", Range: "Emby/HTTP 429"}
		case status >= 500 && status <= 599:
			return classifiedError{Code: "E24", Summary: "Emby 服务异常，请稍后再试", Range: fmt.Sprintf("Emby/HTTP %d", status)}
		default:
			if status >= 400 && status <= 499 {
				return classifiedError{Code: "E22", Summary: "Emby 请求被拒绝", Range: fmt.Sprintf("Emby/HTTP %d", status)}
			}
			return classifiedError{Code: "E25", Summary: "Emby 返回异常", Range: fmt.Sprintf("Emby/HTTP %d", status)}
		}
	}

	// Telegram 平台限制：通过匹配常见错误文本做“尽力而为”的分类。
	// 注意：绝不把原始 error 文本返回给用户，避免泄露细节（例如 chat id 等）。
	switch {
	case strings.Contains(low, "bot can't initiate conversation") ||
		(strings.Contains(low, "forbidden") && strings.Contains(low, "bot") && strings.Contains(low, "user")):
		return classifiedError{Code: "E40", Summary: "无法给用户发起私信（对方未先私聊 /start）", Range: "Telegram"}
	case strings.Contains(low, "parse entities") || strings.Contains(low, "end of the entity"):
		return classifiedError{Code: "E42", Summary: "消息格式解析失败，请稍后再试", Range: "Telegram/Markdown"}
	case strings.Contains(low, "too many requests") || strings.Contains(low, "flood"):
		return classifiedError{Code: "E41", Summary: "触发频率限制，请稍后再试", Range: "Telegram"}
	}

	// 数据库错误：通过错误文本做“尽力而为”的分类。
	switch {
	case strings.Contains(low, "record not found"):
		return classifiedError{Code: "E30", Summary: "数据不存在", Range: "数据库"}
	case strings.Contains(low, "duplicate entry") || strings.Contains(low, "duplicate key"):
		return classifiedError{Code: "E31", Summary: "数据冲突，请稍后重试", Range: "数据库"}
	case strings.Contains(low, "connection refused") ||
		(strings.Contains(low, "timeout") && strings.Contains(low, "sql")) ||
		(strings.Contains(low, "driver") && strings.Contains(low, "bad connection")):
		return classifiedError{Code: "E32", Summary: "数据库不可用，请稍后再试", Range: "数据库"}
	}

	// 兜底：未知错误。
	return classifiedError{Code: "E0", Summary: "操作失败，请稍后再试", Range: "未知"}
}
