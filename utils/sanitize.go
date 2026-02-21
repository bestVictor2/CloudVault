package utils

import "strings"

// SanitizeHeaderFilename removes characters that can break headers.
func SanitizeHeaderFilename(name string) string {
	clean := strings.TrimSpace(name)
	if clean == "" {
		return "download"
	}
	clean = strings.ReplaceAll(clean, "\r", "")
	clean = strings.ReplaceAll(clean, "\n", "")
	clean = strings.ReplaceAll(clean, "\"", "")
	return clean
}

// filename := `a.txt"\r\nX-Admin: true`
// 如果不进行数据清洗 会产生 http 攻击 比如上面的 filename 会变成两行
