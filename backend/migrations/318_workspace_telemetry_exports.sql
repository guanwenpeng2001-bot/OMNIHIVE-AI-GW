-- BE-29 is the first consumer of this action. Workspace administrators can
-- approve export for their own workspace using the existing identity authority.
INSERT INTO identity_actions(action, category) VALUES ('ops.export.configure', 'workspace')
ON CONFLICT (action) DO UPDATE SET category = EXCLUDED.category
WHERE identity_actions.category = 'platform';

CREATE TABLE telemetry_export_destinations (
 workspace_id bigint NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 id uuid NOT NULL,
 endpoint text CHECK (octet_length(endpoint) BETWEEN 1 AND 2048),
 version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
 revoked_at timestamptz,
 PRIMARY KEY (workspace_id, id),
 CHECK ((endpoint IS NULL) = (revoked_at IS NOT NULL))
);

CREATE TABLE telemetry_exports (
 workspace_id bigint NOT NULL,
 id uuid NOT NULL,
 destination_id uuid NOT NULL,
 version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
 state text NOT NULL CHECK (state IN ('validating', 'active', 'failed', 'disabled')),
 active_slot smallint CHECK (active_slot BETWEEN 1 AND 8),
 approved_by bigint NOT NULL CHECK (approved_by > 0),
 approved_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 data_classes text[] NOT NULL CHECK (data_classes = ARRAY['timings']),
 sample_ppm integer NOT NULL CHECK (sample_ppm BETWEEN 0 AND 1000000),
 retention_days integer NOT NULL CHECK (retention_days BETWEEN 1 AND 30),
 last_success_at timestamptz,
 last_error_reason text CHECK (last_error_reason IN
  ('DESTINATION_UNAVAILABLE', 'DESTINATION_REJECTED', 'RETRY_EXHAUSTED', 'DESTINATION_REVOKED', 'APPROVAL_REVOKED')),
 PRIMARY KEY (workspace_id, id),
 UNIQUE (workspace_id, id, version),
 UNIQUE (workspace_id, active_slot),
 FOREIGN KEY (workspace_id, destination_id) REFERENCES telemetry_export_destinations ON DELETE CASCADE,
 CHECK ((active_slot IS NOT NULL) = (state IN ('validating', 'active')))
);

CREATE INDEX telemetry_exports_destination ON telemetry_exports(workspace_id, destination_id)
 WHERE active_slot IS NOT NULL;

-- The key receipt and its configuration or destination commit together.
CREATE TABLE telemetry_export_create_keys (
 workspace_id bigint NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 actor_id bigint NOT NULL CHECK (actor_id > 0),
 resource text NOT NULL CHECK (resource IN ('destination', 'export')),
 key_hash bytea NOT NULL CHECK (octet_length(key_hash) = 32),
 fingerprint bytea NOT NULL CHECK (octet_length(fingerprint) = 32),
 result_id uuid NOT NULL,
 result_destination_id uuid,
 PRIMARY KEY (workspace_id, actor_id, resource, key_hash),
 CHECK ((result_destination_id IS NOT NULL) = (resource = 'export'))
);

-- Slot zero is the data-free destination validation. Positive slots enforce
-- the pending bound. A completed sample retains its delivery key and TTL only.
CREATE TABLE telemetry_export_jobs (
 id uuid PRIMARY KEY,
 workspace_id bigint NOT NULL,
 export_id uuid NOT NULL,
 export_version bigint NOT NULL,
 slot smallint CHECK (slot BETWEEN 0 AND 256),
 operation text CHECK (operation IN ('scope.read', 'scope.write')),
 success boolean,
 latency_ms integer CHECK (latency_ms BETWEEN 0 AND 3600000),
 expires_at timestamptz,
 completed_at timestamptz,
 attempts smallint NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
 ready_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 lease_id uuid,
 FOREIGN KEY (workspace_id, export_id, export_version) REFERENCES telemetry_exports(workspace_id, id, version) ON DELETE CASCADE,
 UNIQUE (workspace_id, export_id, slot),
 CHECK (
  (slot IS NOT NULL AND slot > 0 AND operation IS NOT NULL AND success IS NOT NULL AND latency_ms IS NOT NULL AND expires_at IS NOT NULL AND completed_at IS NULL)
  OR (slot IS NOT NULL AND slot = 0 AND operation IS NULL AND success IS NULL AND latency_ms IS NULL AND expires_at IS NULL AND completed_at IS NULL)
  OR (slot IS NULL AND operation IS NULL AND success IS NULL AND latency_ms IS NULL AND expires_at IS NOT NULL AND completed_at IS NOT NULL)),
 CHECK ((attempts > 0) = (lease_id IS NOT NULL))
);

CREATE INDEX telemetry_export_jobs_ready ON telemetry_export_jobs(ready_at, id) WHERE slot IS NOT NULL;
CREATE INDEX telemetry_export_jobs_expiry ON telemetry_export_jobs(expires_at, id) WHERE expires_at IS NOT NULL;
CREATE INDEX telemetry_export_jobs_completed ON telemetry_export_jobs(workspace_id, export_id, completed_at DESC, id DESC)
 WHERE slot IS NULL;
