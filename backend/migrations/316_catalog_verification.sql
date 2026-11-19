-- Expand only. Existing catalog, access, routing and pricing writers keep ownership.
INSERT INTO identity_actions(action, category) VALUES ('catalog.publish', 'platform')
ON CONFLICT DO NOTHING;

CREATE TABLE catalog_verification_targets (
    id uuid PRIMARY KEY,
    cell_id text NOT NULL CHECK (cell_id ~ '^[a-f0-9]{64}$'),
    owner_id bigint NOT NULL,
    connection_id bigint NOT NULL,
    entry_id text NOT NULL CHECK (entry_id ~ '^m_[a-f0-9]{32}$'),
    provider text NOT NULL CHECK (length(provider) BETWEEN 1 AND 64),
    model text NOT NULL CHECK (length(model) BETWEEN 1 AND 256),
    action text NOT NULL CHECK (length(action) BETWEEN 1 AND 64),
    protocol text NOT NULL CHECK (length(protocol) BETWEEN 1 AND 64),
    account_mode text NOT NULL CHECK (length(account_mode) BETWEEN 1 AND 64),
    adapter_version text NOT NULL CHECK (length(adapter_version) BETWEEN 1 AND 64),
    interval_seconds integer NOT NULL CHECK (interval_seconds = 0 OR interval_seconds BETWEEN 300 AND 604800),
    schedule_version bigint NOT NULL DEFAULT 1 CHECK (schedule_version > 0),
    next_due_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    CHECK ((interval_seconds = 0) = (next_due_at IS NULL)),
    UNIQUE (entry_id, action, protocol, account_mode, adapter_version),
    UNIQUE (cell_id, id)
);
CREATE INDEX catalog_verification_schedule ON catalog_verification_targets(next_due_at, id)
WHERE next_due_at IS NOT NULL;

CREATE TABLE catalog_verification_jobs (
    id uuid PRIMARY KEY,
    target_id uuid NOT NULL REFERENCES catalog_verification_targets(id),
    actor_id bigint NOT NULL,
    state text NOT NULL CHECK (state IN ('queued', 'running', 'succeeded', 'failed', 'unknown')),
    queued_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    started_at timestamptz,
    lease_until timestamptz,
    CHECK ((state = 'running') = (lease_until IS NOT NULL))
);
CREATE UNIQUE INDEX catalog_verification_one_pending ON catalog_verification_jobs(target_id)
WHERE state IN ('queued', 'running');
CREATE INDEX catalog_verification_pending ON catalog_verification_jobs(state, queued_at, id)
WHERE state IN ('queued', 'running');

CREATE TABLE catalog_verification_evidence (
    id uuid PRIMARY KEY,
    target_id uuid NOT NULL REFERENCES catalog_verification_targets(id),
    operation_id uuid UNIQUE REFERENCES catalog_verification_jobs(id),
    origin text NOT NULL CHECK (origin IN ('endpoint_probe', 'connection_verification')),
    state text NOT NULL CHECK (state IN ('success', 'failed', 'unknown')),
    reason text NOT NULL CHECK (reason ~ '^[A-Z0-9_]{1,64}$'),
    sample_count integer NOT NULL CHECK (sample_count BETWEEN 0 AND 1),
    window_start timestamptz NOT NULL,
    observed_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > observed_at),
    fixture_id text CHECK (fixture_id ~ '^[a-z0-9_]{1,64}$'),
    source_revision text NOT NULL,
    source_ref uuid,
    CHECK (observed_at >= window_start),
    CHECK ((state = 'unknown') = (sample_count = 0)),
    CHECK (fixture_id IS NULL OR (origin = 'endpoint_probe' AND state = 'success')),
    CHECK ((source_ref IS NOT NULL) = (origin = 'connection_verification')),
    CHECK ((operation_id IS NOT NULL) = (origin = 'endpoint_probe')),
    UNIQUE (target_id, id),
    UNIQUE (target_id, source_ref)
);
CREATE INDEX catalog_verification_recent_evidence ON catalog_verification_evidence(target_id, observed_at DESC, id);
CREATE INDEX catalog_verification_last_success ON catalog_verification_evidence(target_id, observed_at DESC, id)
WHERE state = 'success';

CREATE TABLE catalog_compatibility_publications (
    id uuid UNIQUE NOT NULL,
    cell_id text NOT NULL,
    target_id uuid NOT NULL,
    version bigint NOT NULL CHECK (version > 0),
    maturity text NOT NULL CHECK (maturity IN ('candidate', 'experimental', 'stable', 'deprecated', 'closed')),
    evidence_id uuid,
    actor_id bigint NOT NULL,
    published_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    deprecation_reason text CHECK (deprecation_reason IN ('provider_retirement', 'adapter_retirement', 'superseded')),
    announced_at timestamptz,
    sunset_at timestamptz,
    rollback_version bigint CHECK (rollback_version >= 0 AND rollback_version < version),
    PRIMARY KEY (cell_id, version),
    FOREIGN KEY (cell_id, target_id) REFERENCES catalog_verification_targets(cell_id, id),
    FOREIGN KEY (target_id, evidence_id) REFERENCES catalog_verification_evidence(target_id, id),
    CHECK ((maturity IN ('deprecated', 'closed')) = (deprecation_reason IS NOT NULL)),
    CHECK ((deprecation_reason IS NULL AND announced_at IS NULL AND sunset_at IS NULL) OR
           (deprecation_reason IS NOT NULL AND announced_at IS NOT NULL AND sunset_at IS NOT NULL AND sunset_at > announced_at)),
    CHECK (maturity <> 'stable' OR evidence_id IS NOT NULL),
    CHECK (maturity <> 'candidate' OR evidence_id IS NULL)
);

CREATE TABLE catalog_verification_commands (
    actor_id bigint NOT NULL,
    kind text NOT NULL CHECK (kind IN ('target', 'verify', 'publish')),
    key text NOT NULL CHECK (length(key) BETWEEN 1 AND 128),
    fingerprint text NOT NULL CHECK (fingerprint ~ '^[a-f0-9]{64}$'),
    result_id uuid NOT NULL,
    PRIMARY KEY (actor_id, kind, key)
);
