-- name: CreateSession :one
INSERT INTO user_sessions(id, user_id, token_hash, auth_method, auth_source_id, auth_version, idle_expires_at, absolute_expires_at)
VALUES ($1, $2, $3, $4, $5, $6, now() + sqlc.arg(idle_seconds)::bigint * interval '1 second', now() + sqlc.arg(max_seconds)::bigint * interval '1 second') RETURNING *;

-- name: FindSession :one
SELECT s.* FROM user_sessions s JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1 AND s.revoked_at IS NULL AND s.idle_expires_at > now()
 AND s.absolute_expires_at > now() AND u.status = 'active' AND u.auth_version = s.auth_version;

-- name: GetValidSession :one
SELECT s.* FROM user_sessions s JOIN users u ON u.id = s.user_id
WHERE s.id = $1 AND s.user_id = $2 AND s.revoked_at IS NULL AND s.idle_expires_at > now()
 AND s.absolute_expires_at > now() AND u.status = 'active' AND u.auth_version = s.auth_version;

-- name: TouchSession :exec
UPDATE user_sessions s SET last_seen_at = now(), idle_expires_at = LEAST(s.absolute_expires_at, now() + sqlc.arg(idle_seconds)::bigint * interval '1 second')
FROM users u WHERE s.id = sqlc.arg(id) AND s.user_id = sqlc.arg(user_id) AND u.id = s.user_id
 AND s.last_seen_at <= now() - sqlc.arg(renewal_milliseconds)::bigint * interval '1 millisecond' AND s.revoked_at IS NULL AND s.idle_expires_at > now()
 AND s.absolute_expires_at > now() AND u.status = 'active' AND u.auth_version = s.auth_version;

-- name: ListSessions :many
SELECT s.* FROM user_sessions s JOIN users u ON u.id = s.user_id WHERE s.user_id = $1 AND s.revoked_at IS NULL
 AND s.idle_expires_at > now() AND s.absolute_expires_at > now() AND s.auth_version = u.auth_version
ORDER BY s.created_at DESC, s.id DESC LIMIT $2 OFFSET $3;

-- name: RevokeSession :execrows
UPDATE user_sessions SET revoked_at = COALESCE(revoked_at, now()) WHERE id = $1 AND user_id = $2;

-- name: RevokeUserSessions :exec
UPDATE user_sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL;
