package logger

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/utils"
)

// NewZhGormLogger 返回一个“尽量中文化”的 GORM logger：
// - 忽略 record not found（通常是业务分支，不应刷屏）
// - 统一中文前缀与字段名（耗时/行数/位置）
func NewZhGormLogger(level logger.LogLevel) logger.Interface {
	l := log.New(os.Stdout, "", log.LstdFlags)
	return &zhGormLogger{
		level:                level,
		slowThreshold:        200 * time.Millisecond,
		ignoreRecordNotFound: true,
		out:                  l,
	}
}

type zhGormLogger struct {
	level                logger.LogLevel
	slowThreshold        time.Duration
	ignoreRecordNotFound bool
	out                  *log.Logger
}

func (l *zhGormLogger) LogMode(level logger.LogLevel) logger.Interface {
	cp := *l
	cp.level = level
	return &cp
}

func (l *zhGormLogger) Info(_ context.Context, msg string, data ...interface{}) {
	if l.level < logger.Info {
		return
	}
	l.out.Printf("【DB】信息："+msg, data...)
}

func (l *zhGormLogger) Warn(_ context.Context, msg string, data ...interface{}) {
	if l.level < logger.Warn {
		return
	}
	l.out.Printf("【DB】警告："+msg, data...)
}

func (l *zhGormLogger) Error(_ context.Context, msg string, data ...interface{}) {
	if l.level < logger.Error {
		return
	}
	l.out.Printf("【DB】错误："+msg, data...)
}

func (l *zhGormLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level == logger.Silent {
		return
	}
	if err != nil && l.ignoreRecordNotFound && errors.Is(err, gorm.ErrRecordNotFound) {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()
	location := utils.FileWithLineNum()

	switch {
	case err != nil && l.level >= logger.Error:
		l.out.Printf("【DB】执行失败 耗时=%s 行数=%d 位置=%s 错误=%s SQL=%s", fmtDuration(elapsed), rows, location, zhErr(err), sql)
	case l.slowThreshold > 0 && elapsed > l.slowThreshold && l.level >= logger.Warn:
		l.out.Printf("【DB】慢查询 耗时=%s 行数=%d 位置=%s SQL=%s", fmtDuration(elapsed), rows, location, sql)
	case l.level >= logger.Info:
		l.out.Printf("【DB】SQL 耗时=%s 行数=%d 位置=%s SQL=%s", fmtDuration(elapsed), rows, location, sql)
	}
}

func fmtDuration(d time.Duration) string {
	// 参考 GORM 默认输出，保留到毫秒级别即可。
	if d < time.Second {
		return fmt.Sprintf("%.3fms", float64(d.Microseconds())/1000.0)
	}
	return d.Truncate(time.Millisecond).String()
}

func zhErr(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "未找到记录"
	}
	return err.Error()
}
