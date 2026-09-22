package project

import (
	"time"

	"github.com/google/uuid"

	"github.com/example/go-starter-kit/apperror"
)

var (
	ErrNotFound = apperror.New(apperror.NotFound, "project not found")
	ErrConflict = apperror.New(apperror.Conflict, "a project with this name already exists")
	ErrInvalid  = apperror.New(apperror.Invalid, "invalid project input")
)

type Record struct {
	ID          uuid.UUID
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
