package logging

import (
	"os"
	"path/filepath"
)

// ResolveLogDir 尽量把日志目录固定在“项目/程序目录”内，避免因工作目录不同导致日志跑到别处。
//
// 优先级：
// 1) 环境变量 LOG_DIR（绝对/相对都可；相对以当前工作目录解析）
// 2) 向上查找包含 .env/go.mod/COMMANDS.md 的目录（从 cwd 与 exeDir 开始）
// 3) 回退到当前工作目录
//
// 返回值为绝对路径。
func ResolveLogDir() string {
	if v := os.Getenv("LOG_DIR"); v != "" {
		if filepath.IsAbs(v) {
			return v
		}
		if wd, err := os.Getwd(); err == nil {
			return filepath.Join(wd, v)
		}
		return filepath.Clean(v)
	}

	candidates := make([]string, 0, 2)
	if wd, err := os.Getwd(); err == nil && wd != "" {
		candidates = append(candidates, wd)
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		candidates = append(candidates, filepath.Dir(exe))
	}

	for _, base := range candidates {
		if root := findProjectRoot(base); root != "" {
			return filepath.Join(root, "log")
		}
	}

	if wd, err := os.Getwd(); err == nil && wd != "" {
		return filepath.Join(wd, "log")
	}
	return "log"
}

func findProjectRoot(start string) string {
	start = filepath.Clean(start)
	if start == "" || start == "." {
		return ""
	}

	// 最多向上 6 层，避免异常路径导致遍历过深。
	dir := start
	for i := 0; i < 6; i++ {
		if exists(filepath.Join(dir, ".env")) || exists(filepath.Join(dir, "go.mod")) || exists(filepath.Join(dir, "COMMANDS.md")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
