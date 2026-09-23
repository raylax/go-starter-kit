// Package validation 提供不依赖业务类型的基础文本校验。
package validation

import (
	"strings"
	"unicode/utf8"
)

// TextWithin 按字符数检查文本，允许空字符串，保留原始空白。
func TextWithin(value string, maxRunes int) bool {
	return utf8.ValidString(value) && maxRunes >= 0 && utf8.RuneCountInString(value) <= maxRunes && !strings.ContainsRune(value, '\x00')
}

// RequiredText 去除首尾空白，再检查非空、长度和 NUL。
func RequiredText(value string, maxRunes int) (string, bool) {
	value = strings.TrimSpace(value)
	return value, value != "" && TextWithin(value, maxRunes)
}
