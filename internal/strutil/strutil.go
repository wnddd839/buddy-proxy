package strutil

import (
	"cmp"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// First 返回第一个非空（trim 后）字符串，基于 cmp.Or（Go 1.22+）。
func First(values ...string) string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || value == "<nil>" {
			continue
		}
		cleaned = append(cleaned, value)
	}
	if len(cleaned) == 0 {
		return ""
	}
	return cmp.Or(cleaned...)
}

// Compact 去除首尾空白。
func Compact(value string) string {
	return strings.TrimSpace(value)
}

// Truncate 返回 trim 后最多 n 字节的子串。
func Truncate(value string, n int) string {
	value = strings.TrimSpace(value)
	if n < 0 {
		n = 0
	}
	if len(value) <= n {
		return value
	}
	return value[:n]
}

// RandomHex 生成 n 字节随机数的十六进制串（2n 个字符）。
func RandomHex(n int) string {
	if n < 1 {
		n = 1
	}
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// MaskSecret 遮蔽密钥中间段，两侧各保留 visible 个字符。
func MaskSecret(value string, visible int) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}
	if visible < 1 {
		visible = 1
	}
	if len(text) <= visible*2 {
		if visible > len(text) {
			visible = len(text)
		}
		return text[:visible] + "..."
	}
	return text[:visible] + "..." + text[len(text)-visible:]
}

// PositiveInt 从 JSON 常见数值类型取出正整数；非正数、小数、无法解析时返回 0。
func PositiveInt(value any) int {
	n, ok := asInt(value)
	if !ok || n <= 0 {
		return 0
	}
	return n
}

// FirstPositiveInt 返回第一个正整数。
func FirstPositiveInt(values ...any) int {
	for _, value := range values {
		if n := PositiveInt(value); n > 0 {
			return n
		}
	}
	return 0
}

func asInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int8:
		return int(v), true
	case int16:
		return int(v), true
	case int32:
		return int(v), true
	case int64:
		if v > int64(math.MaxInt) || v < int64(math.MinInt) {
			return 0, false
		}
		return int(v), true
	case uint:
		if v > uint(math.MaxInt) {
			return 0, false
		}
		return int(v), true
	case uint32:
		if uint64(v) > uint64(math.MaxInt) {
			return 0, false
		}
		return int(v), true
	case uint64:
		if v > uint64(math.MaxInt) {
			return 0, false
		}
		return int(v), true
	case float64:
		if v > float64(math.MaxInt) || v < float64(math.MinInt) {
			return 0, false
		}
		n := int(v)
		if float64(n) != v {
			return 0, false
		}
		return n, true
	case json.Number:
		i, err := v.Int64()
		if err != nil || i > int64(math.MaxInt) || i < int64(math.MinInt) {
			return 0, false
		}
		return int(i), true
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}
