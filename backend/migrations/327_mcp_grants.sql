CREATE TABLE IF NOT EXISTS mcp_clients (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    client_id TEXT NOT NULL,
    name TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, client_id)
);

CREATE TABLE IF NOT EXISTS mcp_grants (
    id UUID PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    client_id TEXT NOT NULL,
    name TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    scope TEXT NOT NULL CHECK (scope IN ('user','admin')),
    permission TEXT NOT NULL CHECK (permission IN ('read','write')),
    status TEXT NOT NULL CHECK (status IN ('active','revoked')),
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    FOREIGN KEY (user_id, client_id) REFERENCES mcp_clients(user_id, client_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_mcp_grants_user ON mcp_grants(user_id, created_at);
