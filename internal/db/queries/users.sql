-- name: FindUserByEmail :one
SELECT * FROM users WHERE email_normalized = $1;

-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: LockUser :one
SELECT * FROM users WHERE id = $1 FOR UPDATE;

-- name: CreatePendingUser :one
INSERT INTO users(email, email_normalized) VALUES ($1, $2)
ON CONFLICT (email_normalized) DO UPDATE SET email_normalized = users.email_normalized
RETURNING *;

-- name: CreateFederatedUser :one
INSERT INTO users(display_name, status) VALUES ($1, 'active') RETURNING *;

-- name: ActivateUser :one
UPDATE users SET status = 'active', email_verified_at = now(), recovery_enabled = true, updated_at = now()
WHERE id = $1 AND status = 'pending' RETURNING *;

-- name: UpdateUserProfile :one
UPDATE users u SET display_name = sqlc.arg(display_name), updated_at = now()
WHERE u.id = sqlc.arg(id) AND u.status = 'active'
 AND EXISTS (
  SELECT 1 FROM user_sessions s
  WHERE s.id = sqlc.arg(session_id) AND s.user_id = u.id
   AND s.revoked_at IS NULL AND s.idle_expires_at > now()
   AND s.absolute_expires_at > now() AND s.auth_version = u.auth_version
 )
RETURNING u.*;

-- name: UpdateUserEmail :one
UPDATE users SET email = $2, email_normalized = $3, email_verified_at = now(), recovery_enabled = true,
 auth_version = auth_version + 1, updated_at = now() WHERE id = $1 AND status = 'active' RETURNING *;

-- name: BumpUserVersion :exec
UPDATE users SET auth_version = auth_version + 1, updated_at = now() WHERE id = $1;

-- name: SetUserStatus :one
UPDATE users SET status = $2, auth_version = auth_version + 1, updated_at = now()
WHERE id = $1 RETURNING *;

-- name: ListUsers :many
SELECT * FROM users ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2;

-- name: GetSessionUser :one
SELECT u.* FROM users u JOIN user_sessions s ON s.user_id = u.id
WHERE u.id = sqlc.arg(user_id) AND s.id = sqlc.arg(session_id)
 AND u.status = 'active' AND s.auth_version = u.auth_version
 AND s.revoked_at IS NULL AND s.idle_expires_at > now() AND s.absolute_expires_at > now();
