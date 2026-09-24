package project

import (
	"context"

	"github.com/example/go-starter-kit/internal/apperror"
	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/example/go-starter-kit/internal/pagination"
	"github.com/example/go-starter-kit/internal/validation"
	"github.com/google/uuid"
)

var storageErrors = db.ErrorPolicy{Resource: "project", NotFound: ErrNotFound, UniqueConstraints: map[string]*apperror.Error{"projects_owner_name_key": ErrConflict}}

type Service struct{ queries *sqlc.Queries }

func NewService(db sqlc.DBTX) *Service { return &Service{queries: sqlc.New(db)} }

func (s *Service) Create(ctx context.Context, owner, name, description string) (Record, error) {
	if err := identity.RequireSubject(owner); err != nil {
		return Record{}, err
	}
	name, err := validate(name, description)
	if err != nil {
		return Record{}, err
	}
	row, err := s.queries.CreateProject(ctx, sqlc.CreateProjectParams{ID: uuid.New(), OwnerID: owner, Name: name, Description: description})
	return fromRow(row), db.MapError(err, storageErrors)
}

func (s *Service) Get(ctx context.Context, owner string, id uuid.UUID) (Record, error) {
	if err := identity.RequireSubject(owner); err != nil {
		return Record{}, err
	}
	row, err := s.queries.GetProject(ctx, sqlc.GetProjectParams{ID: id, OwnerID: owner})
	return fromRow(row), db.MapError(err, storageErrors)
}

// List 多取一条记录判断 has_more，避免额外执行 COUNT(*)。
func (s *Service) List(ctx context.Context, owner string, params pagination.Params) (pagination.Result[Record], error) {
	if err := identity.RequireSubject(owner); err != nil {
		return pagination.Result[Record]{}, err
	}
	if params.Validate() != nil {
		return pagination.Result[Record]{}, ErrInvalid
	}
	rows, err := s.queries.ListProjects(ctx, sqlc.ListProjectsParams{OwnerID: owner, Limit: params.FetchLimit(), Offset: params.Offset})
	if err != nil {
		return pagination.Result[Record]{}, db.MapError(err, storageErrors)
	}
	return pagination.Build(rows, params, fromRow)
}

func (s *Service) Update(ctx context.Context, owner string, id uuid.UUID, name, description string) (Record, error) {
	if err := identity.RequireSubject(owner); err != nil {
		return Record{}, err
	}
	name, err := validate(name, description)
	if err != nil {
		return Record{}, err
	}
	row, err := s.queries.UpdateProject(ctx, sqlc.UpdateProjectParams{ID: id, OwnerID: owner, Name: name, Description: description})
	return fromRow(row), db.MapError(err, storageErrors)
}

func (s *Service) Delete(ctx context.Context, owner string, id uuid.UUID) error {
	if err := identity.RequireSubject(owner); err != nil {
		return err
	}
	count, err := s.queries.DeleteProject(ctx, sqlc.DeleteProjectParams{ID: id, OwnerID: owner})
	if err != nil {
		return db.MapError(err, storageErrors)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func validate(name, description string) (string, error) {
	name, valid := validation.RequiredText(name, 100)
	if !valid || !validation.TextWithin(description, 2000) {
		return "", ErrInvalid
	}
	return name, nil
}

func fromRow(row sqlc.Project) Record {
	return Record{ID: row.ID, Name: row.Name, Description: row.Description, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}
