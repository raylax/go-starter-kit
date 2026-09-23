package validation

import (
	"net/mail"
	"strings"
	"unicode/utf8"
)

// Email 校验不带展示名称的单一邮箱地址，长度上限及大小写策略由业务决定。
// 不修改输入；调用方按业务规则处理首尾空白。
func Email(value string) bool {
	if value == "" || !utf8.ValidString(value) || strings.ContainsAny(value, "\r\n\x00") {
		return false
	}
	parsed, err := mail.ParseAddress(value)
	return err == nil && parsed.Address == value && strings.Contains(value, "@")
}
