-- name: AppendAudit :exec
INSERT INTO audit_events(id, action, outcome, actor_type, actor_id, resource_type, resource_id, scope_subject, session_id, request_id, reason_code, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12);
