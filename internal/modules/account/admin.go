package account

import (
	"context"
	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/example/go-starter-kit/internal/pagination"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Service) Users(ctx context.Context, r Request, p pagination.Params) (_ pagination.Result[UserRecord], failureErr error) {
	defer s.recordFailureOnReturn(ctx, r, "user.list", &failureErr)
	if p.Validate() != nil {
		return pagination.Result[UserRecord]{}, ErrInvalid
	}
	if err := s.deps.Authorizer.RequireAdmin(ctx, r.Subject); err != nil {
		return pagination.Result[UserRecord]{}, err
	}
	rows, err := s.queries.ListUsers(ctx, sqlc.ListUsersParams{Limit: p.FetchLimit(), Offset: p.Offset})
	if err != nil {
		return pagination.Result[UserRecord]{}, db.MapError(err, storageErrors)
	}
	return pagination.Build(rows, p, userRecord)
}

func (s *Service) SetStatus(ctx context.Context, r Request, id uuid.UUID, status UserStatus) (_ UserRecord, failureErr error) {
	defer s.recordFailureOnReturn(ctx, r, "user.status_change", &failureErr)
	if !status.Settable() {
		return UserRecord{}, ErrInvalid
	}
	if err := s.deps.Authorizer.RequireAdmin(ctx, r.Subject); err != nil {
		return UserRecord{}, err
	}
	var result UserRecord
	e := db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		u, e := q.GetUser(ctx, id)
		if e != nil {
			return e
		}
		if UserStatus(u.Status) == UserPending {
			return ErrInvalid
		}
		u, e = q.SetUserStatus(ctx, sqlc.SetUserStatusParams{ID: id, Status: string(status)})
		if e != nil {
			return e
		}
		if e = q.RevokeUserSessions(ctx, id); e != nil {
			return e
		}
		metadata := userStatusAuditMetadata{Status: status}
		if e = audit(ctx, q, r, auditEvent{
			Action:       "user.status_change",
			Outcome:      AuditSuccess,
			ResourceType: "user",
			ResourceID:   id.String(),
			ScopeSubject: id.String(),
			Metadata:     metadata,
		}); e != nil {
			return e
		}
		result = userRecord(u)
		return nil
	})
	return result, e
}
func (s *Service) RevokeUserSessions(ctx context.Context, r Request, id uuid.UUID) (failureErr error) {
	defer s.recordFailureOnReturn(ctx, r, "session.revoke_all", &failureErr)
	if err := s.deps.Authorizer.RequireAdmin(ctx, r.Subject); err != nil {
		return err
	}
	return db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		if _, e := q.GetUser(ctx, id); e != nil {
			return e
		}
		if e := s.invalidate(ctx, q, id); e != nil {
			return e
		}
		return audit(ctx, q, r, auditEvent{
			Action:       "session.revoke_all",
			Outcome:      AuditSuccess,
			ResourceType: "user",
			ResourceID:   id.String(),
			ScopeSubject: id.String(),
		})
	})
}
