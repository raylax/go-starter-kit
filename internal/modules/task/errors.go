package task

import (
	"github.com/example/go-starter-kit/internal/db"
)

var storageErrors = db.ErrorPolicy{Resource: "task", NotFound: ErrNotFound}
