package account

import (
	"context"

	"github.com/example/go-starter-kit/internal/db/sqlc"
	"github.com/google/uuid"
)

// 显式列出账户用例需要的数据操作，新增生成查询不会自动暴露给业务。
func (q *store) BumpUserVersion(ctx context.Context, id uuid.UUID) error {
	return q.rawQueries.BumpUserVersion(ctx, id)
}

func (q *store) FailFlow(ctx context.Context, id uuid.UUID) error {
	return q.rawQueries.FailFlow(ctx, id)
}

func (q *store) FindFlow(ctx context.Context, tokenHash []byte) (sqlc.AuthFlow, error) {
	return q.rawQueries.FindFlow(ctx, tokenHash)
}

func (q *store) FindProviderAccount(ctx context.Context, arg sqlc.FindProviderAccountParams) (sqlc.Account, error) {
	return q.rawQueries.FindProviderAccount(ctx, arg)
}

func (q *store) FindSession(ctx context.Context, tokenHash []byte) (sqlc.UserSession, error) {
	return q.rawQueries.FindSession(ctx, tokenHash)
}

func (q *store) FindUserByEmail(ctx context.Context, emailNormalized *string) (sqlc.User, error) {
	return q.rawQueries.FindUserByEmail(ctx, emailNormalized)
}

func (q *store) FindVerification(ctx context.Context, tokenHash []byte) (sqlc.AuthVerification, error) {
	return q.rawQueries.FindVerification(ctx, tokenHash)
}

func (q *store) GetAccount(ctx context.Context, arg sqlc.GetAccountParams) (sqlc.Account, error) {
	return q.rawQueries.GetAccount(ctx, arg)
}

func (q *store) GetPasswordAccount(ctx context.Context, userID uuid.UUID) (sqlc.Account, error) {
	return q.rawQueries.GetPasswordAccount(ctx, userID)
}

func (q *store) GetUser(ctx context.Context, id uuid.UUID) (sqlc.User, error) {
	return q.rawQueries.GetUser(ctx, id)
}

func (q *store) GetValidSession(ctx context.Context, arg sqlc.GetValidSessionParams) (sqlc.UserSession, error) {
	return q.rawQueries.GetValidSession(ctx, arg)
}

func (q *store) ListAccounts(ctx context.Context, userID uuid.UUID) ([]sqlc.Account, error) {
	return q.rawQueries.ListAccounts(ctx, userID)
}

func (q *store) ListSessions(ctx context.Context, arg sqlc.ListSessionsParams) ([]sqlc.UserSession, error) {
	return q.rawQueries.ListSessions(ctx, arg)
}

func (q *store) ListUsers(ctx context.Context, arg sqlc.ListUsersParams) ([]sqlc.User, error) {
	return q.rawQueries.ListUsers(ctx, arg)
}

func (q *store) GetFlow(ctx context.Context, id uuid.UUID) (sqlc.AuthFlow, error) {
	return q.rawQueries.GetFlow(ctx, id)
}

func (q *store) LockUser(ctx context.Context, id uuid.UUID) (sqlc.User, error) {
	return q.rawQueries.LockUser(ctx, id)
}

func (q *store) RevokeAccount(ctx context.Context, arg sqlc.RevokeAccountParams) (int64, error) {
	return q.rawQueries.RevokeAccount(ctx, arg)
}

func (q *store) RevokeSession(ctx context.Context, arg sqlc.RevokeSessionParams) (int64, error) {
	return q.rawQueries.RevokeSession(ctx, arg)
}

func (q *store) RevokeUserSessions(ctx context.Context, userID uuid.UUID) error {
	return q.rawQueries.RevokeUserSessions(ctx, userID)
}

func (q *store) TouchAccount(ctx context.Context, arg sqlc.TouchAccountParams) error {
	return q.rawQueries.TouchAccount(ctx, arg)
}

func (q *store) TouchSession(ctx context.Context, arg sqlc.TouchSessionParams) error {
	return q.rawQueries.TouchSession(ctx, arg)
}

func (q *store) GetSessionUser(ctx context.Context, arg sqlc.GetSessionUserParams) (sqlc.User, error) {
	return q.rawQueries.GetSessionUser(ctx, arg)
}
