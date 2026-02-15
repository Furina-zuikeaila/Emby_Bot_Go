package router

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"emby-bot-new/internal/logging"
)

// logOp 输出一行结构化操作日志（中文），便于审计与排查。
// 注意：不要传入敏感值（密码、邀请码、URL 等）。
func logOp(telegramID int64, action string, fields ...any) {
	action = strings.TrimSpace(action)
	if action == "" {
		action = "未知操作"
	}

	var b strings.Builder
	b.WriteString("【操作】")
	b.WriteString(fmt.Sprintf("TGID=%d", telegramID))
	b.WriteString(" ")
	b.WriteString("动作=")
	b.WriteString(action)

	if len(fields) > 0 {
		// 可变参数按“键/值”成对解释：k1, v1, k2, v2...
		b.WriteString(" ")
		b.WriteString("详情=")
		for i := 0; i < len(fields); i += 2 {
			if i > 0 {
				b.WriteString(" ")
			}
			k := fmt.Sprint(fields[i])
			v := any(nil)
			if i+1 < len(fields) {
				v = fields[i+1]
			}
			b.WriteString(strings.TrimSpace(k))
			b.WriteString("=")
			b.WriteString(sanitizeLogValue(v))
		}
	}

	log.Print(b.String())
	appendWeeklyOpLog(b.String())
}

func sanitizeLogValue(v any) string {
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" {
		return "-"
	}
	// 避免多行/日志注入；同时限制长度
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

var weeklyOpLogMu sync.Mutex

func appendWeeklyOpLog(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	// 项目目录/log：尽量固定路径，避免因工作目录变化导致日志跑到别处。
	dir := logging.ResolveLogDir()

	now := time.Now()
	year, week := now.ISOWeek()
	name := fmt.Sprintf("oplog_%04d-W%02d.log", year, week)
	path := filepath.Join(dir, name)

	weeklyOpLogMu.Lock()
	defer weeklyOpLogMu.Unlock()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	// 文件内自带时间戳，避免依赖标准日志前缀格式。
	_, _ = f.WriteString(now.Format("2006-01-02 15:04:05") + " " + line + "\n")
}
