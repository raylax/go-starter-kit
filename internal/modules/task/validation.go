package task

import (
	"github.com/example/go-starter-kit/internal/validation"
)

func validate(input Details) (Details, error) {
	title, valid := validation.RequiredText(input.Title, 200)
	if !valid || !validation.TextWithin(input.Description, 2000) || !input.Status.Valid() {
		return Details{}, ErrInvalid
	}
	input.Title = title
	return input, nil
}
