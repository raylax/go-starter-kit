package account

import (
	"context"
	"errors"
	"github.com/example/go-starter-kit/internal/apperror"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Service) lockSelf(ctx context.Context, q *store, r Request) (sqlc.User, sqlc.UserSession, error) {
	uid, e := uuid.Parse(r.UserID)
	if e != nil {
		return sqlc.User{}, sqlc.UserSession{}, apperror.ErrUnauthenticated
	}
	sid, e := uuid.Parse(r.SessionID)
	if e != nil {
		return sqlc.User{}, sqlc.UserSession{}, apperror.ErrUnauthenticated
	}
	user, e := q.LockUser(ctx, uid)
	if errors.Is(e, pgx.ErrNoRows) {
		return user, sqlc.UserSession{}, apperror.ErrUnauthenticated
	}
	if e != nil {
		return user, sqlc.UserSession{}, e
	}
	session, e := q.GetValidSession(ctx, sqlc.GetValidSessionParams{ID: sid, UserID: uid})
	if errors.Is(e, pgx.ErrNoRows) {
		return user, session, apperror.ErrUnauthenticated
	}
	return user, session, e
}

// readSelf 校验当前用户和原会话，用于不要求串行化的资料读取。
func (s *Service) readSelf(ctx context.Context, q *store, r Request) (sqlc.User, sqlc.UserSession, error) {
	uid, err := uuid.Parse(r.UserID)
	if err != nil {
		return sqlc.User{}, sqlc.UserSession{}, apperror.ErrUnauthenticated
	}
	sid, err := uuid.Parse(r.SessionID)
	if err != nil {
		return sqlc.User{}, sqlc.UserSession{}, apperror.ErrUnauthenticated
	}
	user, err := q.GetUser(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return user, sqlc.UserSession{}, apperror.ErrUnauthenticated
	}
	if err != nil {
		return user, sqlc.UserSession{}, err
	}
	session, err := q.GetValidSession(ctx, sqlc.GetValidSessionParams{ID: sid, UserID: uid})
	if errors.Is(err, pgx.ErrNoRows) {
		return user, session, apperror.ErrUnauthenticated
	}
	return user, session, err
}

func (s *Service) invalidate(ctx context.Context, q *store, userID uuid.UUID) error {
	if e := q.BumpUserVersion(ctx, userID); e != nil {
		return e
	}
	return q.RevokeUserSessions(ctx, userID)
}
