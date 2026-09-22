-- +goose Up
-- 仅调整新记录的默认 ID，保留已有 ID 和引用；依赖 PostgreSQL 18。
ALTER TABLE projects ALTER COLUMN id SET DEFAULT uuidv7();
ALTER TABLE tasks ALTER COLUMN id SET DEFAULT uuidv7();

-- +goose Down
-- 回滚生成规则，不改写已有 UUID。
ALTER TABLE projects ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE tasks ALTER COLUMN id SET DEFAULT gen_random_uuid();
