package identity

import (
	"errors"
	"strings"
	"testing"
)

func TestCredentialValidation(t *testing.T) {
	token := strings.Repeat("x", 16)
	for _, tc := range []struct {
		token, subject string
		want           error
	}{
		{token, "alice", nil}, {token, strings.Repeat("中", 85), nil}, {token, strings.Repeat("中", 86), ErrInvalidSubject},
		{token, " \t\n", ErrInvalidSubject}, {token, "", ErrInvalidSubject}, {token[:15], "alice", ErrInvalidDevelopmentToken},
		{token + " ", "alice", ErrInvalidDevelopmentToken}, {token + "\t", "alice", ErrInvalidDevelopmentToken},
	} {
		err := ValidateDevelopment(tc.token, tc.subject)
		_, constructorErr := NewDevelopment(tc.token, tc.subject)
		if !errors.Is(err, tc.want) || !errors.Is(constructorErr, tc.want) {
			t.Fatalf("构造与校验规则不一致：%v / %v", err, constructorErr)
		}
	}
}
