package mailoutbox

import (
	"fmt"
	"log/slog"

	"github.com/example/go-starter-kit/internal/db/sqlc"
)

type Service struct {
	queries *sqlc.Queries
	send    SendFunc
	logger  *slog.Logger
}

func NewService(database sqlc.DBTX, send SendFunc, logger *slog.Logger) (*Service, error) {
	if database == nil || send == nil || logger == nil {
		return nil, fmt.Errorf("邮件队列依赖不完整")
	}
	return &Service{sqlc.New(database), send, logger}, nil
}
