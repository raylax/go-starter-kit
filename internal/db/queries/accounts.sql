-- name: GetPasswordAccount :one
SELECT * FROM accounts WHERE user_id = $1 AND provider_id = 'credential' AND revoked_at IS NULL;

-- name: FindProviderAccount :one
SELECT * FROM accounts WHERE provider_namespace = $1 AND provider_account_id = $2 AND revoked_at IS NULL;

-- name: GetAccount :one
SELECT * FROM accounts WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL;

-- name: ListAccounts :many
SELECT * FROM accounts WHERE user_id = $1 AND revoked_at IS NULL ORDER BY created_at, id;

-- name: CreateAccount :one
INSERT INTO accounts(id, user_id, provider_id, provider_account_id, provider_namespace, password_hash)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: ChangePassword :execrows
UPDATE accounts SET password_hash = $3, version = version + 1, updated_at = now()
WHERE id = $1 AND user_id = $2 AND provider_id = 'credential' AND revoked_at IS NULL;

-- name: UpgradePasswordHash :exec
UPDATE accounts SET password_hash = sqlc.narg(new_password_hash), updated_at = now()
WHERE id = $1 AND user_id = $2 AND password_hash = sqlc.narg(old_password_hash) AND revoked_at IS NULL;

-- name: RevokeAccount :execrows
UPDATE accounts SET revoked_at = now(), version = version + 1, updated_at = now()
WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL;

-- name: TouchAccount :exec
UPDATE accounts SET last_used_at = now() WHERE id = $1 AND user_id = $2;
