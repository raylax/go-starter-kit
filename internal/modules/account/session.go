package account

import (
	"context"
	"errors"
	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/example/go-starter-kit/internal/pagination"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

func (s *Service) AuthenticateSession(ctx context.Context, token string) (string, string, error) {
	hash, e := identity.TokenDigest(token, SessionPrefix)
	if e != nil {
		return "", "", identity.ErrUnauthorized
	}
	session, e := s.queries.FindSession(ctx, hash)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", "", identity.ErrUnauthorized
	}
	if e != nil {
		return "", "", e
	}
	renewalInterval := min(time.Minute, s.options.IdleTTL/2)
	if time.Since(session.LastSeenAt) >= renewalInterval {
		e = s.queries.TouchSession(ctx, sqlc.TouchSessionParams{ID: session.ID, UserID: session.UserID, IdleSeconds: int64(s.options.IdleTTL.Seconds()), RenewalMilliseconds: renewalInterval.Milliseconds()})
		if e != nil {
			return "", "", e
		}
	}
	return session.UserID.String(), session.ID.String(), nil
}
func (s *Service) newSession(ctx context.Context, q *store, r Request, u sqlc.User, a sqlc.Account, method AuthMethod) (SessionCredentials, error) {
	if UserStatus(u.Status) != UserActive || a.UserID != u.ID || a.RevokedAt != nil {
		return SessionCredentials{}, ErrCredentials
	}
	token, hash := identity.NewToken(SessionPrefix)
	row, e := q.CreateSession(ctx, sqlc.CreateSessionParams{UserID: u.ID, TokenHash: hash, AuthMethod: string(method), AuthSourceID: a.ID, AuthVersion: u.AuthVersion, IdleSeconds: int64(s.options.IdleTTL.Seconds()), MaxSeconds: int64(s.options.MaxTTL.Seconds())})
	if e != nil {
		return SessionCredentials{}, e
	}
	if e = q.TouchAccount(ctx, sqlc.TouchAccountParams{ID: a.ID, UserID: u.ID}); e != nil {
		return SessionCredentials{}, e
	}
	r.UserID = u.ID.String()
	r.SessionID = row.ID.String()
	metadata := loginAuditMetadata{Method: method}
	if e = audit(ctx, q, r, "auth.login", AuditSuccess, "session", row.ID.String(), u.ID.String(), "", metadata); e != nil {
		return SessionCredentials{}, e
	}
	return SessionCredentials{Token: token, ID: row.ID, IdleExpiresAt: row.IdleExpiresAt, AbsoluteExpiresAt: row.AbsoluteExpiresAt}, nil
}
func (s *Service) Login(ctx context.Context, r Request, email, password string) (_ SessionCredentials, failureErr error) {
	defer s.recordFailureOnReturn(ctx, r, "auth.login", &failureErr)
	email, e := normalizeEmail(email)
	if e != nil {
		return SessionCredentials{}, ErrCredentials
	}
	if e = s.entryLimit(ctx, r, "login", email); e != nil {
		return SessionCredentials{}, e
	}
	u, ue := s.queries.FindUserByEmail(ctx, ptr(email))
	if ue != nil && !errors.Is(ue, pgx.ErrNoRows) {
		return SessionCredentials{}, db.MapError(ue, storageErrors)
	}
	a, ae := s.queries.GetPasswordAccount(ctx, u.ID)
	if ae != nil && !errors.Is(ae, pgx.ErrNoRows) {
		return SessionCredentials{}, db.MapError(ae, storageErrors)
	}
	valid, upgrade, e := s.deps.Passwords.Verify(ctx, text(a.PasswordHash), password)
	if e != nil {
		return SessionCredentials{}, e
	}
	if ue != nil || ae != nil || !valid || UserStatus(u.Status) != UserActive || u.EmailVerifiedAt == nil {
		return SessionCredentials{}, ErrCredentials
	}
	var newHash string
	if upgrade {
		newHash, e = s.deps.Passwords.Hash(ctx, password)
		if e != nil {
			return SessionCredentials{}, e
		}
	}
	var result SessionCredentials
	e = db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		current, e := q.LockUser(ctx, u.ID)
		if e != nil {
			return e
		}
		fresh, e := q.GetAccount(ctx, sqlc.GetAccountParams{ID: a.ID, UserID: u.ID})
		if e != nil {
			return credentialLookupError(e)
		}
		if current.AuthVersion != u.AuthVersion || fresh.Version != a.Version || UserStatus(current.Status) != UserActive {
			return ErrCredentials
		}
		if newHash != "" {
			if e = q.UpgradePasswordHash(ctx, sqlc.UpgradePasswordHashParams{ID: a.ID, UserID: u.ID, PasswordHash: &newHash, PasswordHash_2: a.PasswordHash}); e != nil {
				return e
			}
		}
		result, e = s.newSession(ctx, q, r, current, fresh, MethodPassword)
		return e
	})
	return result, e
}
func sessionRecord(s sqlc.UserSession) SessionRecord {
	return SessionRecord{ID: s.ID, Method: AuthMethod(s.AuthMethod), AuthenticatedAt: s.AuthenticatedAt, CreatedAt: s.CreatedAt, LastSeenAt: s.LastSeenAt, IdleExpiresAt: s.IdleExpiresAt, AbsoluteExpiresAt: s.AbsoluteExpiresAt}
}
func (s *Service) Sessions(ctx context.Context, r Request, p pagination.Params) (pagination.Result[SessionRecord], error) {
	if p.Validate() != nil {
		return pagination.Result[SessionRecord]{}, ErrInvalid
	}
	u, _, err := s.readSelf(ctx, s.queries, r)
	if err != nil {
		return pagination.Result[SessionRecord]{}, db.MapError(err, storageErrors)
	}
	rows, err := s.queries.ListSessions(ctx, sqlc.ListSessionsParams{UserID: u.ID, Limit: p.FetchLimit(), Offset: p.Offset})
	if err != nil {
		return pagination.Result[SessionRecord]{}, db.MapError(err, storageErrors)
	}
	return pagination.Build(rows, p, sessionRecord)
}
func (s *Service) RevokeSession(ctx context.Context, r Request, id uuid.UUID, all bool) error {
	return db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		u, _, e := s.lockSelf(ctx, q, r)
		if e != nil {
			return e
		}
		if all {
			e = s.invalidate(ctx, q, u.ID)
		} else {
			var count int64
			count, e = q.RevokeSession(ctx, sqlc.RevokeSessionParams{ID: id, UserID: u.ID})
			if e == nil && count == 0 {
				return ErrNotFound
			}
		}
		if e != nil {
			return e
		}
		kind, target := "session", id.String()
		if all {
			kind, target = "user", u.ID.String()
		}
		return audit(ctx, q, r, "session.revoke", AuditSuccess, kind, target, u.ID.String(), "", nil)
	})
}
