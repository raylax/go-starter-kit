-- +goose Up
-- 当前完整基线，用于空数据库初始化；UUID 由应用代码生成。
CREATE TABLE projects (
    id UUID PRIMARY KEY,
    owner_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT projects_owner_name_key UNIQUE (owner_id, name)
);

CREATE INDEX projects_owner_created_idx ON projects (owner_id, created_at DESC, id DESC);

CREATE TABLE tasks (
    id UUID PRIMARY KEY,
    owner_id TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'todo',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX tasks_owner_created_idx ON tasks (owner_id, created_at DESC, id DESC);

CREATE TABLE users (
    id UUID PRIMARY KEY,
    display_name TEXT NOT NULL DEFAULT '',
    email TEXT,
    email_normalized TEXT,
    email_verified_at TIMESTAMPTZ,
    recovery_enabled BOOLEAN NOT NULL DEFAULT false,
    status TEXT NOT NULL DEFAULT 'pending',
    role TEXT NOT NULL DEFAULT 'user',
    auth_version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT users_email_normalized_key UNIQUE (email_normalized)
);

CREATE TABLE accounts (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider_id TEXT NOT NULL,
    provider_account_id TEXT NOT NULL,
    provider_namespace TEXT NOT NULL,
    password_hash TEXT,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX accounts_provider_identity_key ON accounts(provider_id, provider_account_id) WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX accounts_namespace_identity_key ON accounts(provider_namespace, provider_account_id) WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX accounts_user_credential_key ON accounts(user_id) WHERE provider_id = 'credential' AND revoked_at IS NULL;
CREATE INDEX accounts_user_id_idx ON accounts(user_id);

CREATE TABLE user_sessions (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE,
    auth_method TEXT NOT NULL,
    auth_source_id UUID NOT NULL,
    authenticated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    auth_version BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    idle_expires_at TIMESTAMPTZ NOT NULL,
    absolute_expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ
);
CREATE INDEX user_sessions_user_id_created_at_idx ON user_sessions(user_id, created_at DESC, id);

CREATE TABLE auth_flows (
    id UUID PRIMARY KEY,
    purpose TEXT NOT NULL,
    token_hash BYTEA UNIQUE,
    provider_id TEXT NOT NULL DEFAULT '',
    config_version TEXT NOT NULL DEFAULT '',
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    session_id UUID REFERENCES user_sessions(id) ON DELETE CASCADE,
    auth_version BIGINT NOT NULL DEFAULT 0,
    account_id UUID,
    account_version BIGINT NOT NULL DEFAULT 0,
    operation TEXT NOT NULL DEFAULT '',
    target TEXT NOT NULL DEFAULT '',
    reauthentication_id UUID,
    claimed_by UUID,
    state_hash BYTEA,
    protocol_state BYTEA,
    verified_namespace TEXT NOT NULL DEFAULT '',
    verified_subject TEXT NOT NULL DEFAULT '',
    verified_name TEXT NOT NULL DEFAULT '',
    authenticated_at TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX auth_flows_expires_at_idx ON auth_flows(expires_at);
CREATE INDEX auth_flows_user_id_idx ON auth_flows(user_id);

CREATE TABLE auth_verifications (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose TEXT NOT NULL,
    token_hash BYTEA NOT NULL UNIQUE,
    email TEXT NOT NULL,
    auth_version BIGINT NOT NULL,
    session_id UUID,
    reauthentication_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ
);
CREATE INDEX auth_verifications_expires_at_idx ON auth_verifications(expires_at);

CREATE TABLE audit_events (
    id UUID PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    action TEXT NOT NULL,
    outcome TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id TEXT,
    resource_type TEXT NOT NULL,
    resource_id TEXT,
    scope_subject TEXT,
    session_id UUID,
    request_id TEXT,
    reason_code TEXT,
    schema_version INTEGER NOT NULL DEFAULT 1,
    metadata JSONB NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_events_occurred_at_id_idx ON audit_events(occurred_at, id);
CREATE INDEX audit_events_scope_subject_occurred_at_id_idx ON audit_events(scope_subject, occurred_at, id);
CREATE INDEX audit_events_resource_occurred_at_id_idx ON audit_events(resource_type, resource_id, occurred_at, id);
REVOKE UPDATE, DELETE, TRUNCATE ON audit_events FROM PUBLIC;

-- 限速键保存摘要，不保存原始邮箱或 IP。
CREATE TABLE auth_rate_limits (
    bucket_key BYTEA PRIMARY KEY,
    count INTEGER NOT NULL DEFAULT 1,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX auth_rate_limits_expires_at_idx ON auth_rate_limits(expires_at);

-- 事务内入队的邮件正文为 HTML，投递终态清除敏感载荷。
CREATE TABLE mail_outbox (
    id UUID PRIMARY KEY,
    kind TEXT NOT NULL,
    recipient TEXT NOT NULL,
    subject TEXT NOT NULL,
    body TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    lease_id UUID,
    leased_until TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX mail_outbox_ready_idx ON mail_outbox (available_at, created_at) WHERE status IN ('pending', 'processing');

-- +goose Down
DROP TABLE mail_outbox;
DROP TABLE auth_rate_limits;
DROP TABLE audit_events;
DROP TABLE auth_verifications;
DROP TABLE auth_flows;
DROP TABLE user_sessions;
DROP TABLE accounts;
DROP TABLE users;
DROP TABLE tasks;
DROP TABLE projects;
