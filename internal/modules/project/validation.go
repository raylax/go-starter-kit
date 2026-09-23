package project

import (
	"github.com/example/go-starter-kit/internal/validation"
)

func validate(name, description string) (string, error) {
	name, valid := validation.RequiredText(name, 100)
	if !valid || !validation.TextWithin(description, 2000) {
		return "", ErrInvalid
	}
	return name, nil
}
