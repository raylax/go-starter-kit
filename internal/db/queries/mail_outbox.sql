-- name: EnqueueMail :exec
INSERT INTO mail_outbox(id, kind, recipient, subject, body, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ClaimMail :one
WITH candidate AS (
    SELECT id FROM mail_outbox
    WHERE ((status = 'pending' AND available_at <= now()) OR (status = 'processing' AND leased_until <= now()))
      AND expires_at > now() AND attempts < sqlc.arg(max_attempts)::integer
    ORDER BY available_at, created_at
    FOR UPDATE SKIP LOCKED LIMIT 1
)
UPDATE mail_outbox SET status = 'processing', attempts = attempts + 1,
    lease_id = sqlc.arg(lease_id), leased_until = now() + sqlc.arg(lease_seconds)::bigint * interval '1 second'
WHERE id = (SELECT id FROM candidate)
RETURNING *;

-- name: CompleteMail :execrows
UPDATE mail_outbox SET status = 'sent', recipient = '', subject = '', body = '',
    lease_id = NULL, leased_until = NULL, last_error = NULL, completed_at = now()
WHERE id = $1 AND lease_id = $2 AND status = 'processing' AND leased_until > now();

-- name: RetryMail :execrows
UPDATE mail_outbox SET status = CASE WHEN attempts >= sqlc.arg(max_attempts)::integer OR expires_at <= now() THEN 'failed' ELSE 'pending' END,
    recipient = CASE WHEN attempts >= sqlc.arg(max_attempts)::integer OR expires_at <= now() THEN '' ELSE recipient END,
    subject = CASE WHEN attempts >= sqlc.arg(max_attempts)::integer OR expires_at <= now() THEN '' ELSE subject END,
    body = CASE WHEN attempts >= sqlc.arg(max_attempts)::integer OR expires_at <= now() THEN '' ELSE body END,
    completed_at = CASE WHEN attempts >= sqlc.arg(max_attempts)::integer OR expires_at <= now() THEN now() ELSE NULL END,
    available_at = now() + sqlc.arg(delay_seconds)::bigint * interval '1 second',
    lease_id = NULL, leased_until = NULL, last_error = 'sender_unavailable'
WHERE id = $1 AND lease_id = $2 AND status = 'processing' AND leased_until > now();

-- name: ExpireMail :exec
WITH expired AS (
    SELECT id FROM mail_outbox
    WHERE (status = 'pending' OR (status = 'processing' AND leased_until <= now()))
      AND (expires_at <= now() OR attempts >= sqlc.arg(max_attempts)::integer)
    ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT sqlc.arg(batch_size)::integer
)
UPDATE mail_outbox SET status = 'failed', recipient = '', subject = '', body = '',
    lease_id = NULL, leased_until = NULL, completed_at = now(), last_error = 'expired_or_exhausted'
WHERE id IN (SELECT id FROM expired);
