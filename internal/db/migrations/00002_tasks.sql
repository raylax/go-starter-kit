-- +goose Up
CREATE TABLE tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id TEXT NOT NULL CHECK (length(owner_id) BETWEEN 1 AND 255),
    title TEXT NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
    status TEXT NOT NULL DEFAULT 'todo' CHECK (status IN ('todo', 'in_progress', 'done')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX tasks_owner_created_idx ON tasks (owner_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE tasks;
