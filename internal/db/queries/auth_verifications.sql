-- name: CreateVerification :one
INSERT INTO auth_verifications(id, user_id, purpose, token_hash, email, auth_version, session_id, reauthentication_id, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now() + sqlc.arg(ttl_seconds)::bigint * interval '1 second') RETURNING *;

-- name: FindVerification :one
SELECT * FROM auth_verifications WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > now();

-- name: ConsumeVerification :execrows
UPDATE auth_verifications SET consumed_at = now()
WHERE id = $1 AND user_id = $2 AND consumed_at IS NULL AND expires_at > now();
