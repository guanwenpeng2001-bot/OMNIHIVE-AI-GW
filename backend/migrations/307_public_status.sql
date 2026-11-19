CREATE TABLE public_status_incidents (
    id uuid PRIMARY KEY,
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 160),
    severity text NOT NULL CHECK (severity IN ('degraded', 'outage', 'maintenance')),
    component_ids text[] NOT NULL CHECK (cardinality(component_ids) BETWEEN 1 AND 6 AND
        component_ids <@ ARRAY['inference', 'openai_compatible', 'anthropic_compatible', 'gemini_compatible', 'console_auth', 'async_workers']),
    state text NOT NULL CHECK (state IN ('investigating', 'identified', 'monitoring', 'resolved')),
    started_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    resolved_at timestamptz,
    version bigint NOT NULL CHECK (version >= 1),
    CHECK (updated_at >= started_at),
    CHECK ((state = 'resolved') = (resolved_at IS NOT NULL))
);

CREATE INDEX public_status_incidents_timeline ON public_status_incidents (started_at DESC, id DESC);
CREATE INDEX public_status_incidents_active ON public_status_incidents (started_at DESC, id DESC) WHERE resolved_at IS NULL;
CREATE INDEX public_status_incidents_publication ON public_status_incidents (updated_at DESC);

CREATE TABLE public_status_incident_updates (
    incident_id uuid NOT NULL REFERENCES public_status_incidents (id),
    version bigint NOT NULL CHECK (version >= 1),
    published_at timestamptz NOT NULL,
    state text NOT NULL CHECK (state IN ('investigating', 'identified', 'monitoring', 'resolved')),
    message text NOT NULL CHECK (char_length(message) BETWEEN 1 AND 2000),
    PRIMARY KEY (incident_id, version),
    UNIQUE (incident_id, published_at)
);

-- Store publication command identity separately from the anonymous projection.
CREATE TABLE status_incident_commands (
    actor_id bigint NOT NULL,
    route text NOT NULL,
    key_hash bytea NOT NULL CHECK (octet_length(key_hash) = 32),
    fingerprint bytea NOT NULL CHECK (octet_length(fingerprint) = 32),
    incident_id uuid NOT NULL,
    version bigint NOT NULL,
    PRIMARY KEY (actor_id, route, key_hash),
    FOREIGN KEY (incident_id, version) REFERENCES public_status_incident_updates (incident_id, version)
);

CREATE TABLE public_status_buckets (
    grain text NOT NULL CHECK (grain IN ('minute', 'hour', 'day')),
    bucket_start timestamptz NOT NULL,
    component_id text NOT NULL CHECK (component_id IN ('inference', 'openai_compatible', 'anthropic_compatible', 'gemini_compatible')),
    succeeded bigint NOT NULL CHECK (succeeded >= 0),
    gateway_errors bigint NOT NULL CHECK (gateway_errors >= 0),
    upstream_errors bigint NOT NULL CHECK (upstream_errors >= 0),
    upstream_rate_limits bigint NOT NULL CHECK (upstream_rate_limits >= 0),
    stream_interruptions bigint NOT NULL CHECK (stream_interruptions >= 0),
    unknown_outcomes bigint NOT NULL CHECK (unknown_outcomes >= 0),
    admission_denials bigint NOT NULL CHECK (admission_denials >= 0),
    client_cancellations bigint NOT NULL CHECK (client_cancellations >= 0),
    attempts bigint NOT NULL CHECK (attempts >= 0),
    first_byte_timeouts bigint NOT NULL CHECK (first_byte_timeouts >= 0),
    stream_samples bigint NOT NULL CHECK (stream_samples >= 0),
    stream_succeeded bigint NOT NULL CHECK (stream_succeeded BETWEEN 0 AND stream_samples),
    ttft_bins bigint[] NOT NULL CHECK (cardinality(ttft_bins) = 12 AND 0 <= ALL (ttft_bins)),
    last_observed timestamptz,
    last_success timestamptz,
    PRIMARY KEY (grain, bucket_start, component_id)
);

CREATE TABLE public_status_checkpoint (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    observed_through timestamptz NOT NULL,
    covered_from timestamptz NOT NULL CHECK (covered_from <= observed_through),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- Retain the last success when a closed day leaves the bounded history.
CREATE TABLE public_status_last_success (
    component_id text PRIMARY KEY CHECK (component_id IN ('inference', 'openai_compatible', 'anthropic_compatible', 'gemini_compatible')),
    last_success timestamptz NOT NULL
);

-- The public dimensions are fixed protocol families. Never group by a raw
-- endpoint, a tenant-supplied model name, or a provider / connection ID.
CREATE FUNCTION status_gateway_component(endpoint text) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT AS $$
    SELECT CASE
        WHEN endpoint IN ('/v1/responses', '/v1/responses/compact', '/responses', '/responses/compact',
                          '/backend-api/codex/responses', '/backend-api/codex/responses/compact',
                          '/v1/chat/completions', '/chat/completions', '/v1/embeddings', '/embeddings')
            THEN 'openai_compatible'
        WHEN endpoint = '/v1/messages' THEN 'anthropic_compatible'
        WHEN endpoint = '/v1beta/models'
          OR endpoint ~ '^/v1beta/models/[^/]+:(streamGenerateContent|generateContent|batchEmbedContents|embedContent)$'
            THEN 'gemini_compatible'
    END
$$;
