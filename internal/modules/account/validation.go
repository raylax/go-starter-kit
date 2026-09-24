package account

import (
	"github.com/example/go-starter-kit/internal/validation"
	"strings"
)

// 字段上限与 DTO 的静态校验标签保持一致；长度单位由名称明确区分。
const (
	maxEmailBytes             = 254
	maxDisplayNameRunes       = 100
	maxProviderIDRunes        = 64
	maxProviderSubjectBytes   = 1024
	maxProviderNamespaceBytes = 2048
)

// normalizeEmail 定义账户邮箱的长度和大小写规则。
func normalizeEmail(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) > maxEmailBytes || !validation.Email(value) {
		return "", ErrInvalid
	}
	return strings.ToLower(value), nil
}
