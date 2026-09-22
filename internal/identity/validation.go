package identity

import (
	"errors"
	"strings"
)

var (
	ErrInvalidDevelopmentToken = errors.New("token must be at least 16 characters without whitespace")
	ErrInvalidSubject          = errors.New("subject must contain 1-255 bytes and not be blank")
)

// ValidateSubject 使用字节长度，与已有 JWT 和开发认证的规则一致。
func ValidateSubject(subject string) error {
	if strings.TrimSpace(subject) == "" || len(subject) > 255 {
		return ErrInvalidSubject
	}
	return nil
}

func ValidateDevelopment(token, subject string) error {
	if len(token) < 16 || strings.ContainsAny(token, " \t\r\n") {
		return ErrInvalidDevelopmentToken
	}
	return ValidateSubject(subject)
}
