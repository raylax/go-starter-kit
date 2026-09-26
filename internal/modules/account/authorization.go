package account

import (
	"context"
	"errors"

	"github.com/example/go-starter-kit/internal/apperror"
	"github.com/example/go-starter-kit/internal/authorization"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/jackc/pgx/v5"
)

// lockSelf 仅用于敏感写入，保护账号数量、凭据变更和会话撤销。
func (s *Service) lockSelf(ctx context.Context, q *store, r Request) (sqlc.User, sqlc.UserSession, error) {
	if err := requireSubject(r.Subject); err != nil {
		return sqlc.User{}, sqlc.UserSession{}, err
	}
	user, e := q.queries.LockUser(ctx, r.UserID)
	if errors.Is(e, pgx.ErrNoRows) {
		return user, sqlc.UserSession{}, apperror.ErrUnauthenticated
	}
	if e != nil {
		return user, sqlc.UserSession{}, e
	}
	session, e := q.queries.GetValidSession(ctx, sqlc.GetValidSessionParams{ID: r.SessionID, UserID: r.UserID})
	if errors.Is(e, pgx.ErrNoRows) {
		return user, session, apperror.ErrUnauthenticated
	}
	return user, session, e
}

// readSelf 校验当前用户和原会话，不保证与后续操作串行；证明消费时必须复核版本。
func (s *Service) readSelf(ctx context.Context, q *store, r Request) (sqlc.User, sqlc.UserSession, error) {
	if err := requireSubject(r.Subject); err != nil {
		return sqlc.User{}, sqlc.UserSession{}, err
	}
	user, err := q.queries.GetUser(ctx, r.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return user, sqlc.UserSession{}, apperror.ErrUnauthenticated
	}
	if err != nil {
		return user, sqlc.UserSession{}, err
	}
	session, err := q.queries.GetValidSession(ctx, sqlc.GetValidSessionParams{ID: r.SessionID, UserID: r.UserID})
	if errors.Is(err, pgx.ErrNoRows) {
		return user, session, apperror.ErrUnauthenticated
	}
	return user, session, err
}

func (s *Service) invalidate(ctx context.Context, q *store, userID string) error {
	if e := q.BumpUserVersion(ctx, userID); e != nil {
		return e
	}
	return q.RevokeUserSessions(ctx, userID)
}

// requireSubject 在账户入口校验用户与原会话的标识，数据库查询仍负责有效性与归属。
func requireSubject(subject authorization.Subject) error {
	if err := identity.RequireSubject(subject.UserID); err != nil {
		return err
	}
	return identity.RequireSubject(subject.SessionID)
}
