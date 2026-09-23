package account

import (
	"errors"
	"github.com/example/go-starter-kit/internal/apperror"
	"github.com/example/go-starter-kit/internal/db"
	"github.com/jackc/pgx/v5"
)

var storageErrors = db.ErrorPolicy{Resource: "account", NotFound: ErrNotFound, UniqueConstraints: map[string]*apperror.Error{
	"users_email_normalized_key": ErrConflict, "accounts_provider_identity_key": ErrConflict, "accounts_namespace_identity_key": ErrConflict, "accounts_user_credential_key": ErrConflict,
}}

// proofError 将操作证明失效与当前 HTTP 会话失效区分，保留数据库故障语义。
func proofError(err error, kind *apperror.Error) error {
	if errors.Is(err, ErrCredentials) {
		return apperror.Wrap(kind, err)
	}
	return err
}

// credentialLookupError 只将记录不存在视为凭据无效，保留数据库故障和取消原因。
func credentialLookupError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCredentials
	}
	return err
}
