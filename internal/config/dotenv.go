package config

import (
	"bufio"
	"os"
	"strings"
)

// LoadDotenv 从一个 “.env 风格” 文件加载环境变量到当前进程。
//
// 行为约定（刻意保持简单、可预期）：
// - 不覆盖进程中已经存在的环境变量（进程 env 永远优先于文件）。
// - 支持空行与注释行（以 # 开头）。
// - 支持 `export KEY=VAL` 形式（会去掉 export 前缀）。
// - 支持最外层的单/双引号包裹（例如 KEY="value" 或 KEY='value'）。
//
// 非目标（不会实现的特性）：
// - 不做变量展开：例如不会把 `FOO=$BAR` 解析成 BAR 的值。
// - 不解析转义序列：例如不会把 `\n` 变成换行。
// - 不支持多行值。
//
// 说明：
// - 缺少 .env 文件不是错误（方便容器/生产环境只用真实 env）。
func LoadDotenv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		// 缺少文件时直接忽略（便于容器/生产环境只用真实 env）。
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key == "" {
			continue
		}
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, val)
	}
	return scanner.Err()
}
