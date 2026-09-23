package task

import (
	"context"

	"github.com/google/uuid"

	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/example/go-starter-kit/internal/pagination"
)

type Service struct{ queries *sqlc.Queries }

func NewService(db sqlc.DBTX) *Service { return &Service{queries: sqlc.New(db)} }

func (s *Service) Create(ctx context.Context, owner string, input Details) (Record, error) {
	if err := identity.RequireSubject(owner); err != nil {
		return Record{}, err
	}
	if input.Status == "" {
		input.Status = Todo
	}
	input, err := validate(input)
	if err != nil {
		return Record{}, err
	}
	row, err := s.queries.CreateTask(ctx, sqlc.CreateTaskParams{OwnerID: owner, Title: input.Title, Description: input.Description, Status: string(input.Status)})
	return fromRow(row), db.MapError(err, storageErrors)
}

func (s *Service) Get(ctx context.Context, owner string, id uuid.UUID) (Record, error) {
	if err := identity.RequireSubject(owner); err != nil {
		return Record{}, err
	}
	row, err := s.queries.GetTask(ctx, sqlc.GetTaskParams{ID: id, OwnerID: owner})
	return fromRow(row), db.MapError(err, storageErrors)
}

func (s *Service) List(ctx context.Context, owner string, status Status, params pagination.Params) (pagination.Result[Record], error) {
	if err := identity.RequireSubject(owner); err != nil {
		return pagination.Result[Record]{}, err
	}
	if (status != "" && !status.Valid()) || params.Validate() != nil {
		return pagination.Result[Record]{}, ErrInvalid
	}
	rows, err := s.queries.ListTasks(ctx, sqlc.ListTasksParams{OwnerID: owner, StatusFilter: string(status), PageLimit: params.FetchLimit(), PageOffset: params.Offset})
	if err != nil {
		return pagination.Result[Record]{}, db.MapError(err, storageErrors)
	}
	return pagination.Build(rows, params, fromRow)
}

func (s *Service) Update(ctx context.Context, owner string, id uuid.UUID, input Details) (Record, error) {
	if err := identity.RequireSubject(owner); err != nil {
		return Record{}, err
	}
	input, err := validate(input)
	if err != nil {
		return Record{}, err
	}
	row, err := s.queries.UpdateTask(ctx, sqlc.UpdateTaskParams{ID: id, OwnerID: owner, Title: input.Title, Description: input.Description, Status: string(input.Status)})
	return fromRow(row), db.MapError(err, storageErrors)
}

func (s *Service) Delete(ctx context.Context, owner string, id uuid.UUID) error {
	if err := identity.RequireSubject(owner); err != nil {
		return err
	}
	count, err := s.queries.DeleteTask(ctx, sqlc.DeleteTaskParams{ID: id, OwnerID: owner})
	if err != nil {
		return db.MapError(err, storageErrors)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}
