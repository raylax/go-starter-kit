package account

import (
	"context"
	"errors"
	"time"

	"github.com/example/go-starter-kit/internal/db"
	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/example/go-starter-kit/internal/identity"
	"github.com/example/go-starter-kit/internal/pagination"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Service) AuthenticateSession(ctx context.Context, token string) (string, string, error) {
	hash, err := identity.TokenDigest(token, SessionPrefix)
	if err != nil {
		return "", "", identity.ErrUnauthorized
	}
	session, err := s.queries.FindSession(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", identity.ErrUnauthorized
	}
	if err != nil {
		return "", "", err
	}
	renewalInterval := min(time.Minute, s.options.IdleTTL/2)
	if time.Since(session.LastSeenAt) >= renewalInterval {
		err = s.queries.TouchSession(ctx, sqlc.TouchSessionParams{
			ID:                  session.ID,
			UserID:              session.UserID,
			IdleSeconds:         int64(s.options.IdleTTL.Seconds()),
			RenewalMilliseconds: renewalInterval.Milliseconds(),
		})
		if err != nil {
			return "", "", err
		}
	}
	return session.UserID.String(), session.ID.String(), nil
}

func (s *Service) newSession(ctx context.Context, q *store, r Request, user sqlc.User, account sqlc.Account, method AuthMethod) (SessionCredentials, error) {
	if UserStatus(user.Status) != UserActive || account.UserID != user.ID || account.RevokedAt != nil {
		return SessionCredentials{}, ErrCredentials
	}
	token, hash := identity.NewToken(SessionPrefix)
	row, err := q.CreateSession(ctx, sqlc.CreateSessionParams{
		UserID:       user.ID,
		TokenHash:    hash,
		AuthMethod:   string(method),
		AuthSourceID: account.ID,
		AuthVersion:  user.AuthVersion,
		IdleSeconds:  int64(s.options.IdleTTL.Seconds()),
		MaxSeconds:   int64(s.options.MaxTTL.Seconds()),
	})
	if err != nil {
		return SessionCredentials{}, err
	}
	if err = q.TouchAccount(ctx, sqlc.TouchAccountParams{ID: account.ID, UserID: user.ID}); err != nil {
		return SessionCredentials{}, err
	}
	r.UserID = user.ID.String()
	r.SessionID = row.ID.String()
	metadata := loginAuditMetadata{Method: method}
	if err = audit(ctx, q, r, auditEvent{
		Action:       "auth.login",
		Outcome:      AuditSuccess,
		ResourceType: "session",
		ResourceID:   row.ID.String(),
		ScopeSubject: user.ID.String(),
		Metadata:     metadata,
	}); err != nil {
		return SessionCredentials{}, err
	}
	return SessionCredentials{
		Token:             token,
		ID:                row.ID,
		IdleExpiresAt:     row.IdleExpiresAt,
		AbsoluteExpiresAt: row.AbsoluteExpiresAt,
	}, nil
}

func (s *Service) Login(ctx context.Context, r Request, email, password string) (_ SessionCredentials, failureErr error) {
	defer s.recordFailureOnReturn(ctx, r, "auth.login", &failureErr)
	email, err := normalizeEmail(email)
	if err != nil {
		return SessionCredentials{}, ErrCredentials
	}
	if err = s.entryLimit(ctx, r, "login", email); err != nil {
		return SessionCredentials{}, err
	}
	user, userErr := s.queries.FindUserByEmail(ctx, ptr(email))
	if userErr != nil && !errors.Is(userErr, pgx.ErrNoRows) {
		return SessionCredentials{}, db.MapError(userErr, storageErrors)
	}
	account, accountErr := s.queries.GetPasswordAccount(ctx, user.ID)
	if accountErr != nil && !errors.Is(accountErr, pgx.ErrNoRows) {
		return SessionCredentials{}, db.MapError(accountErr, storageErrors)
	}
	valid, upgrade, err := s.deps.Passwords.Verify(ctx, text(account.PasswordHash), password)
	if err != nil {
		return SessionCredentials{}, err
	}
	if userErr != nil || accountErr != nil || !valid || UserStatus(user.Status) != UserActive || user.EmailVerifiedAt == nil {
		return SessionCredentials{}, ErrCredentials
	}
	var newPasswordHash string
	if upgrade {
		newPasswordHash, err = s.deps.Passwords.Hash(ctx, password)
		if err != nil {
			return SessionCredentials{}, err
		}
	}
	var result SessionCredentials
	err = db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		currentUser, err := q.LockUser(ctx, user.ID)
		if err != nil {
			return err
		}
		currentAccount, err := q.GetAccount(ctx, sqlc.GetAccountParams{ID: account.ID, UserID: user.ID})
		if err != nil {
			return credentialLookupError(err)
		}
		if currentUser.AuthVersion != user.AuthVersion || currentAccount.Version != account.Version || UserStatus(currentUser.Status) != UserActive {
			return ErrCredentials
		}
		if newPasswordHash != "" {
			if err = q.UpgradePasswordHash(ctx, sqlc.UpgradePasswordHashParams{
				ID:              account.ID,
				UserID:          user.ID,
				NewPasswordHash: &newPasswordHash,
				OldPasswordHash: account.PasswordHash,
			}); err != nil {
				return err
			}
		}
		result, err = s.newSession(ctx, q, r, currentUser, currentAccount, MethodPassword)
		return err
	})
	return result, err
}

func sessionRecord(session sqlc.UserSession) SessionRecord {
	return SessionRecord{
		ID:                session.ID,
		Method:            AuthMethod(session.AuthMethod),
		AuthenticatedAt:   session.AuthenticatedAt,
		CreatedAt:         session.CreatedAt,
		LastSeenAt:        session.LastSeenAt,
		IdleExpiresAt:     session.IdleExpiresAt,
		AbsoluteExpiresAt: session.AbsoluteExpiresAt,
	}
}

func (s *Service) Sessions(ctx context.Context, r Request, p pagination.Params) (pagination.Result[SessionRecord], error) {
	if p.Validate() != nil {
		return pagination.Result[SessionRecord]{}, ErrInvalid
	}
	user, _, err := s.readSelf(ctx, s.queries, r)
	if err != nil {
		return pagination.Result[SessionRecord]{}, db.MapError(err, storageErrors)
	}
	rows, err := s.queries.ListSessions(ctx, sqlc.ListSessionsParams{UserID: user.ID, Limit: p.FetchLimit(), Offset: p.Offset})
	if err != nil {
		return pagination.Result[SessionRecord]{}, db.MapError(err, storageErrors)
	}
	return pagination.Build(rows, p, sessionRecord)
}

func (s *Service) RevokeSession(ctx context.Context, r Request, id uuid.UUID, all bool) error {
	return db.WithTransaction(ctx, s.database, storageErrors, func(tx pgx.Tx) error {
		q := newStore(tx)
		user, _, err := s.lockSelf(ctx, q, r)
		if err != nil {
			return err
		}
		if all {
			err = s.invalidate(ctx, q, user.ID)
		} else {
			var count int64
			count, err = q.RevokeSession(ctx, sqlc.RevokeSessionParams{ID: id, UserID: user.ID})
			if err == nil && count == 0 {
				return ErrNotFound
			}
		}
		if err != nil {
			return err
		}
		kind, target := "session", id.String()
		if all {
			kind, target = "user", user.ID.String()
		}
		return audit(ctx, q, r, auditEvent{
			Action:       "session.revoke",
			Outcome:      AuditSuccess,
			ResourceType: kind,
			ResourceID:   target,
			ScopeSubject: user.ID.String(),
		})
	})
}
