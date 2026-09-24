package account

import (
	"context"
	"errors"

	"github.com/example/go-starter-kit/internal/apperror"
	"github.com/example/go-starter-kit/internal/authorization"
	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AdminChecker 只读取授权数据，独立于 Service，避免授权器和账户服务循环依赖。
type AdminChecker struct{ queries *store }

var _ authorization.Authorizer = (*AdminChecker)(nil)

func NewAdminChecker(database sqlc.DBTX) *AdminChecker {
	return &AdminChecker{queries: newStore(database)}
}

// RequireAdmin 在同一查询快照内检查有效会话与用户角色，不获取显式行锁。
func (c *AdminChecker) RequireAdmin(ctx context.Context, subject authorization.Subject) error {
	uid, err := uuid.Parse(subject.UserID)
	if err != nil {
		return apperror.ErrUnauthenticated
	}
	sid, err := uuid.Parse(subject.SessionID)
	if err != nil {
		return apperror.ErrUnauthenticated
	}
	user, err := c.queries.GetSessionUser(ctx, sqlc.GetSessionUserParams{UserID: uid, SessionID: sid})
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.ErrUnauthenticated
	}
	if err != nil {
		return db.MapError(err, storageErrors)
	}
	if UserRole(user.Role) != RoleAdmin {
		return ErrForbidden
	}
	return nil
}
