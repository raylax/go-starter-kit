package account

import (
	"github.com/example/go-starter-kit/internal/validation"
	"strings"
)

// normalizeEmail 定义账户邮箱的长度和大小写规则。
func normalizeEmail(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) > 254 || !validation.Email(value) {
		return "", ErrInvalid
	}
	return strings.ToLower(value), nil
}
