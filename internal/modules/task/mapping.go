package task

import (
	"github.com/example/go-starter-kit/internal/db/sqlc"
)

func fromRow(row sqlc.Task) Record {
	return Record{ID: row.ID, Title: row.Title, Description: row.Description, Status: Status(row.Status), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}
