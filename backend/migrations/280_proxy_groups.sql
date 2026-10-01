-- Logical IP groups are additive: existing proxy bindings are never rewritten.
CREATE TABLE IF NOT EXISTS proxy_groups (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL DEFAULT 1,
    name VARCHAR(100) NOT NULL,
    description VARCHAR NOT NULL DEFAULT '',
    region VARCHAR(100) NOT NULL DEFAULT '',
    target_services JSONB NOT NULL DEFAULT '[]'::jsonb,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    fallback_mode VARCHAR(20) NOT NULL DEFAULT 'none',
    fallback_proxy_id BIGINT REFERENCES proxies(id) ON DELETE SET NULL,
    binding_ttl_seconds INT NOT NULL DEFAULT 86400 CHECK (binding_ttl_seconds > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS proxygroup_workspace_id_status ON proxy_groups(workspace_id, status);
CREATE INDEX IF NOT EXISTS proxygroup_deleted_at ON proxy_groups(deleted_at);
CREATE INDEX IF NOT EXISTS proxygroup_fallback_proxy_id ON proxy_groups(fallback_proxy_id);

CREATE TABLE IF NOT EXISTS proxy_group_members (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL DEFAULT 1,
    group_id BIGINT NOT NULL REFERENCES proxy_groups(id) ON DELETE NO ACTION,
    proxy_id BIGINT NOT NULL REFERENCES proxies(id) ON DELETE NO ACTION,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS proxygroupmember_group_id_proxy_id ON proxy_group_members(group_id, proxy_id);
CREATE INDEX IF NOT EXISTS proxygroupmember_workspace_id ON proxy_group_members(workspace_id);
CREATE INDEX IF NOT EXISTS proxygroupmember_proxy_id ON proxy_group_members(proxy_id);

ALTER TABLE accounts ADD COLUMN IF NOT EXISTS proxy_group_id BIGINT REFERENCES proxy_groups(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS account_proxy_group_id ON accounts(proxy_group_id);
