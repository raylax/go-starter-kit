package identity

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/example/go-starter-kit/internal/apperror"
)

var (
	ErrInvalidSubject = errors.New("subject must contain 1-255 bytes and not be blank")
)

// ValidateSubject 统一主体格式：非空白、有效 UTF-8、无 NUL，最多 255 字节。
func ValidateSubject(subject string) error {
	if strings.TrimSpace(subject) == "" || len(subject) > 255 || !utf8.ValidString(subject) || strings.ContainsRune(subject, '\x00') {
		return ErrInvalidSubject
	}
	return nil
}

// RequireSubject 用于业务服务入口，将无效主体统一映射为未认证错误。
// 只检查主体格式，会话有效性仍由认证层负责，不修改主体原值。
func RequireSubject(subject string) error {
	if ValidateSubject(subject) != nil {
		return apperror.ErrUnauthenticated
	}
	return nil
}
