package project

import (
	"github.com/example/go-starter-kit/internal/apperror"
	"github.com/example/go-starter-kit/internal/db"
)

var storageErrors = db.ErrorPolicy{Resource: "project", NotFound: ErrNotFound, UniqueConstraints: map[string]*apperror.Error{"projects_owner_name_key": ErrConflict}}
