package task

import (
	"context"

	"github.com/google/uuid"

	"github.com/example/go-starter-kit/apperror"
	"github.com/example/go-starter-kit/db"
	"github.com/example/go-starter-kit/db/sqlc"
	"github.com/example/go-starter-kit/pagination"
	"github.com/example/go-starter-kit/validation"
)

var storageErrors = db.ErrorPolicy{Resource: "task", NotFound: ErrNotFound}

type Service struct{ queries *sqlc.Queries }

func NewService(db sqlc.DBTX) *Service { return &Service{queries: sqlc.New(db)} }

func (s *Service) Create(ctx context.Context, owner string, input Details) (Record, error) {
	if owner == "" {
		return Record{}, apperror.ErrUnauthenticated
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
	if owner == "" {
		return Record{}, apperror.ErrUnauthenticated
	}
	row, err := s.queries.GetTask(ctx, sqlc.GetTaskParams{ID: id, OwnerID: owner})
	return fromRow(row), db.MapError(err, storageErrors)
}

func (s *Service) List(ctx context.Context, owner string, status Status, params pagination.Params) (pagination.Result[Record], error) {
	if owner == "" {
		return pagination.Result[Record]{}, apperror.ErrUnauthenticated
	}
	if (status != "" && !status.valid()) || params.Validate() != nil {
		return pagination.Result[Record]{}, ErrInvalid
	}
	rows, err := s.queries.ListTasks(ctx, sqlc.ListTasksParams{OwnerID: owner, StatusFilter: string(status), PageLimit: params.FetchLimit(), PageOffset: params.Offset})
	if err != nil {
		return pagination.Result[Record]{}, db.MapError(err, storageErrors)
	}
	return pagination.Build(rows, params, fromRow)
}

func (s *Service) Update(ctx context.Context, owner string, id uuid.UUID, input Details) (Record, error) {
	if owner == "" {
		return Record{}, apperror.ErrUnauthenticated
	}
	input, err := validate(input)
	if err != nil {
		return Record{}, err
	}
	row, err := s.queries.UpdateTask(ctx, sqlc.UpdateTaskParams{ID: id, OwnerID: owner, Title: input.Title, Description: input.Description, Status: string(input.Status)})
	return fromRow(row), db.MapError(err, storageErrors)
}

func (s *Service) Delete(ctx context.Context, owner string, id uuid.UUID) error {
	if owner == "" {
		return apperror.ErrUnauthenticated
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

func validate(input Details) (Details, error) {
	title, valid := validation.RequiredText(input.Title, 200)
	if !valid || !validation.TextWithin(input.Description, 2000) || !input.Status.valid() {
		return Details{}, ErrInvalid
	}
	input.Title = title
	return input, nil
}

func fromRow(row sqlc.Task) Record {
	return Record{ID: row.ID, Title: row.Title, Description: row.Description, Status: Status(row.Status), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}
