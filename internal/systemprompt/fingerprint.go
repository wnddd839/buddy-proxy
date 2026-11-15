package systemprompt

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Fingerprint 返回 system 内容的 SHA-256 前 8 位小写 hex。
// 空或纯空白返回 ""，用于区分「客户端没带 system」。
func Fingerprint(system string) string {
	if strings.TrimSpace(system) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(system))
	return hex.EncodeToString(sum[:4])
}
