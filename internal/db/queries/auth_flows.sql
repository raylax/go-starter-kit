-- name: CreateFlow :one
INSERT INTO auth_flows(id, purpose, token_hash, provider_id, config_version, user_id, session_id, auth_version, account_id, account_version,
 operation, target, reauthentication_id, state_hash, protocol_state, status, authenticated_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, now() + interval '5 minutes') RETURNING *;

-- name: FindFlow :one
SELECT * FROM auth_flows WHERE token_hash = $1 AND expires_at > now();

-- name: GetFlow :one
SELECT * FROM auth_flows WHERE id = $1 AND expires_at > now();

-- name: ClaimFlow :execrows
UPDATE auth_flows SET status = 'processing' WHERE id = $1 AND status = 'pending' AND expires_at > now();

-- name: VerifyFlow :execrows
UPDATE auth_flows SET status = $2, verified_namespace = $3, verified_subject = $4, verified_name = $5,
 authenticated_at = $6, protocol_state = NULL
WHERE id = $1 AND status = 'processing' AND expires_at > now();

-- name: FinishFlow :execrows
UPDATE auth_flows SET status = 'consumed', protocol_state = NULL
WHERE id = $1 AND status = $2 AND expires_at > now();

-- name: FailFlow :exec
UPDATE auth_flows SET status = 'failed', protocol_state = NULL WHERE id = $1 AND status IN ('pending', 'processing');

-- name: ClaimAuthorization :execrows
UPDATE auth_flows SET status = 'claimed', claimed_by = $2
WHERE id = $1 AND status = 'authorized' AND expires_at > now();
