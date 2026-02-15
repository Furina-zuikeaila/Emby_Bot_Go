package registration

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

var usernameAllowed = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

func GenerateEmbyUsername(prefix string, telegramID int64, telegramUsername string) string {
	telegramUsername = strings.TrimSpace(strings.TrimPrefix(telegramUsername, "@"))
	base := usernameAllowed.ReplaceAllString(telegramUsername, "_")
	base = strings.Trim(base, "_")
	base = strings.ToLower(base)

	if base == "" {
		return fmt.Sprintf("%s%d", prefix, telegramID)
	}

	const maxLen = 32
	suffix := fmt.Sprintf("_%d", telegramID)
	remain := maxLen - len(suffix)
	if remain <= 0 {
		return fmt.Sprintf("%s%d", prefix, telegramID)
	}
	if len(base) > remain {
		base = base[:remain]
		base = strings.Trim(base, "_")
		if base == "" {
			return fmt.Sprintf("%s%d", prefix, telegramID)
		}
	}
	return base + suffix
}

func GeneratePassword(length int) string {
	if length < 8 {
		length = 8
	}
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	out := make([]byte, length)
	max := big.NewInt(int64(len(charset)))
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			out[i] = charset[i%len(charset)]
			continue
		}
		out[i] = charset[n.Int64()]
	}
	return string(out)
}
