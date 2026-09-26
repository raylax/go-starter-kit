package validation

import (
	"strings"
	"unicode/utf8"
)

const MaxIdentifierBytes = 255

// Identifier 校验不透明标识，不裁剪空白、不改变大小写，也不要求 UUID 格式。
func Identifier(value string) bool {
	return len(value) <= MaxIdentifierBytes && strings.TrimSpace(value) != "" && utf8.ValidString(value) && !strings.ContainsRune(value, '\x00')
}
