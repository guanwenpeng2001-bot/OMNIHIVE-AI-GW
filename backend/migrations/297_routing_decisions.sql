CREATE TABLE IF NOT EXISTS gateway_routing_decisions (
    request_id UUID NOT NULL REFERENCES gateway_executions(request_id) ON DELETE CASCADE,
    snapshot_id UUID NOT NULL,
    decision JSONB NOT NULL CHECK (
        jsonb_typeof(decision) = 'object'
        AND decision->>'mode' IS NOT DISTINCT FROM 'execution'
        AND decision->>'snapshot_id' IS NOT DISTINCT FROM snapshot_id::text
        AND octet_length(decision::text) <= 131072
    ),
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (request_id, snapshot_id)
);
