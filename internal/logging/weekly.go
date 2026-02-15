package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// WeeklyFileWriter 将写入内容追加到“每周一个文件”的日志中。
// 文件名格式：<prefix>_YYYY-Www.log（ISO week）。
type WeeklyFileWriter struct {
	dir    string
	prefix string

	mu      sync.Mutex
	curYear int
	curWeek int
	curFile *os.File
	curPath string
}

func NewWeeklyFileWriter(dir string, prefix string) (*WeeklyFileWriter, error) {
	dir = filepath.Clean(dir)
	if dir == "" || dir == "." {
		return nil, fmt.Errorf("log dir is empty")
	}
	if prefix == "" {
		prefix = "bot"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &WeeklyFileWriter{dir: dir, prefix: prefix}, nil
}

func (w *WeeklyFileWriter) Write(p []byte) (int, error) {
	if w == nil {
		return 0, io.ErrClosedPipe
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	year, week := now.ISOWeek()
	if w.curFile == nil || year != w.curYear || week != w.curWeek {
		_ = w.rotateLocked(year, week)
	}
	if w.curFile == nil {
		return 0, io.ErrClosedPipe
	}
	return w.curFile.Write(p)
}

func (w *WeeklyFileWriter) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.curFile != nil {
		err := w.curFile.Close()
		w.curFile = nil
		w.curPath = ""
		return err
	}
	return nil
}

func (w *WeeklyFileWriter) rotateLocked(year int, week int) error {
	if w.curFile != nil {
		_ = w.curFile.Close()
		w.curFile = nil
		w.curPath = ""
	}
	name := fmt.Sprintf("%s_%04d-W%02d.log", w.prefix, year, week)
	path := filepath.Join(w.dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w.curYear = year
	w.curWeek = week
	w.curFile = f
	w.curPath = path
	return nil
}
