package registration

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

const DefaultRenewCodePrefix = "Renew"

func VerifySecureCode(saltHex, hashHex, input string) bool {
	input = strings.TrimSpace(input)
	if input == "" || !secureCodeAllowed(input) {
		return false
	}

	salt, err := hex.DecodeString(strings.TrimSpace(saltHex))
	if err != nil || len(salt) == 0 {
		return false
	}
	expected, err := hex.DecodeString(strings.TrimSpace(hashHex))
	if err != nil || len(expected) == 0 {
		return false
	}

	sum := sha256.Sum256(append(append([]byte{}, salt...), []byte(":"+input)...))
	return subtle.ConstantTimeCompare(sum[:], expected) == 1
}
