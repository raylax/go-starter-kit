package task

import (
	"time"

	"github.com/google/uuid"

	"github.com/example/go-starter-kit/internal/apperror"
)

var (
	ErrNotFound = apperror.New(apperror.NotFound, "task not found")
	ErrInvalid  = apperror.New(apperror.Invalid, "invalid task input")
)

type Status string

const (
	Todo       Status = "todo"
	InProgress Status = "in_progress"
	Done       Status = "done"
)

func (s Status) valid() bool { return s == Todo || s == InProgress || s == Done }

type Record struct {
	ID          uuid.UUID
	Title       string
	Description string
	Status      Status
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Details struct {
	Title       string
	Description string
	Status      Status
}
