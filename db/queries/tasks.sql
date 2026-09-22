-- name: CreateTask :one
INSERT INTO tasks (owner_id, title, description, status)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetTask :one
SELECT * FROM tasks WHERE id = $1 AND owner_id = $2;

-- name: ListTasks :many
SELECT * FROM tasks
WHERE owner_id = sqlc.arg(owner_id)
  AND (sqlc.arg(status_filter)::text = '' OR status = sqlc.arg(status_filter)::text)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: UpdateTask :one
UPDATE tasks SET title = $3, description = $4, status = $5, updated_at = now()
WHERE id = $1 AND owner_id = $2
RETURNING *;

-- name: DeleteTask :execrows
DELETE FROM tasks WHERE id = $1 AND owner_id = $2;
