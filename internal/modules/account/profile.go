package account

import (
	"context"
	"errors"
	"github.com/example/go-starter-kit/internal/apperror"
	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/example/go-starter-kit/internal/validation"
	"github.com/jackc/pgx/v5"
)

func (s *Service) Me(ctx context.Context, r Request) (Profile, error) {
	u, session, err := s.readSelf(ctx, s.queries, r)
	if err != nil {
		return Profile{}, db.MapError(err, storageErrors)
	}
	accounts, err := s.queries.ListAccounts(ctx, u.ID)
	if err != nil {
		return Profile{}, db.MapError(err, storageErrors)
	}
	return Profile{User: userRecord(u), Accounts: s.accountRecords(accounts), SessionID: session.ID}, nil
}

func (s *Service) UpdateProfile(ctx context.Context, r Request, name string) (UserRecord, error) {
	name, valid := validation.RequiredText(name, maxDisplayNameRunes)
	if !valid {
		return UserRecord{}, ErrInvalid
	}
	var result UserRecord
	e := db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		u, session, e := s.readSelf(ctx, q, r)
		if e != nil {
			return e
		}
		u, e = q.UpdateUserProfile(ctx, sqlc.UpdateUserProfileParams{ID: u.ID, DisplayName: name, SessionID: session.ID})
		if errors.Is(e, pgx.ErrNoRows) {
			return apperror.ErrUnauthenticated
		}
		if e != nil {
			return e
		}
		result = userRecord(u)
		return audit(ctx, q, r, "user.update", AuditSuccess, "user", u.ID.String(), u.ID.String(), "", nil)
	})
	return result, e
}
