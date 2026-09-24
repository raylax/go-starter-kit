-- name: RateLimit :one
INSERT INTO auth_rate_limits(bucket_key, count, expires_at)
VALUES ($1, 1, now() + sqlc.arg(window_seconds)::bigint * interval '1 second')
ON CONFLICT (bucket_key) DO UPDATE SET
 count = CASE WHEN auth_rate_limits.expires_at <= now() THEN 1 ELSE LEAST(auth_rate_limits.count + 1, 1000000) END,
 expires_at = CASE WHEN auth_rate_limits.expires_at <= now() THEN EXCLUDED.expires_at ELSE auth_rate_limits.expires_at END
RETURNING count, GREATEST(1, ceil(extract(epoch FROM (expires_at - now()))))::bigint AS retry_after_seconds;
