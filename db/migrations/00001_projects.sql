-- +goose Up
CREATE TABLE projects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id TEXT NOT NULL CHECK (length(owner_id) BETWEEN 1 AND 255),
    name TEXT NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT projects_owner_name_key UNIQUE (owner_id, name)
);

CREATE INDEX projects_owner_created_idx ON projects (owner_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE projects;

