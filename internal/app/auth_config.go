package app

import (
	"errors"
	"fmt"

	"github.com/example/go-starter-kit/internal/identity"
)

// validateAuthEnvironment 由配置解析和装配入口共用，防止绕过生产环境限制。
func validateAuthEnvironment(environment, mode string) error {
	if mode == "dev" && environment != "development" && environment != "test" {
		return fmt.Errorf("dev authentication is forbidden outside development/test")
	}
	return nil
}

func validateDevelopmentCredentials(token, subject string) error {
	err := identity.ValidateDevelopment(token, subject)
	switch {
	case errors.Is(err, identity.ErrInvalidDevelopmentToken):
		return fmt.Errorf("DEV_AUTH_TOKEN: %w", err)
	case errors.Is(err, identity.ErrInvalidSubject):
		return fmt.Errorf("DEV_AUTH_SUBJECT: %w", err)
	default:
		return err
	}
}
