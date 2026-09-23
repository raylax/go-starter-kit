package project

import (
	"github.com/example/go-starter-kit/internal/db/sqlc"
)

func fromRow(row sqlc.Project) Record {
	return Record{ID: row.ID, Name: row.Name, Description: row.Description, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}
