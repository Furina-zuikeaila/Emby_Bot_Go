package tron

import (
	"errors"
	"math/big"
	"strings"
)

var ErrInvalidDecimalAmount = errors.New("invalid decimal amount")

// ParseDecimalToQuant 将十进制金额解析为“最小单位整数”（quant），不使用 float，避免精度问题。
//
// 例：decimals=6
// - "1" => 1000000
// - "1.23" => 1230000
func ParseDecimalToQuant(amount string, decimals int) (*big.Int, error) {
	if decimals < 0 {
		return nil, ErrInvalidDecimalAmount
	}
	s := strings.TrimSpace(amount)
	if s == "" {
		return nil, ErrInvalidDecimalAmount
	}
	if strings.HasPrefix(s, "+") {
		s = strings.TrimPrefix(s, "+")
	}
	if strings.HasPrefix(s, "-") {
		return nil, ErrInvalidDecimalAmount
	}
	parts := strings.Split(s, ".")
	if len(parts) > 2 {
		return nil, ErrInvalidDecimalAmount
	}
	intPart := strings.TrimSpace(parts[0])
	if intPart == "" {
		intPart = "0"
	}
	for _, ch := range intPart {
		if ch < '0' || ch > '9' {
			return nil, ErrInvalidDecimalAmount
		}
	}
	fracPart := ""
	if len(parts) == 2 {
		fracPart = strings.TrimSpace(parts[1])
		for _, ch := range fracPart {
			if ch < '0' || ch > '9' {
				return nil, ErrInvalidDecimalAmount
			}
		}
		if len(fracPart) > decimals {
			// 允许尾部多余小数但必须都是 0（例如 1.2300000 when decimals=6）
			extra := fracPart[decimals:]
			for _, ch := range extra {
				if ch != '0' {
					return nil, ErrInvalidDecimalAmount
				}
			}
			fracPart = fracPart[:decimals]
		}
	}
	for len(fracPart) < decimals {
		fracPart += "0"
	}

	combined := strings.TrimLeft(intPart, "0")
	if combined == "" {
		combined = "0"
	}
	if decimals > 0 {
		combined = combined + fracPart
	} else if fracPart != "" {
		// decimals=0 但用户传了小数，且小数部分不全为 0，则非法（前面已处理截断）
	}

	quant, ok := new(big.Int).SetString(combined, 10)
	if !ok {
		return nil, ErrInvalidDecimalAmount
	}
	return quant, nil
}
