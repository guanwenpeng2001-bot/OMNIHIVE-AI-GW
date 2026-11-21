ALTER TABLE gateway_execution_attempts
    ADD COLUMN conversion_plan JSONB,
    ADD CONSTRAINT gateway_attempt_conversion_plan_shape CHECK (
        conversion_plan IS NULL OR (
            jsonb_typeof(conversion_plan) = 'object'
            AND conversion_plan ?& ARRAY['mode', 'source_protocol', 'target_protocol', 'diagnostics']
            AND jsonb_typeof(conversion_plan->'mode') = 'string'
            AND conversion_plan->>'mode' IN ('native', 'bridge', 'rejected')
            AND jsonb_typeof(conversion_plan->'source_protocol') = 'string'
            AND jsonb_typeof(conversion_plan->'target_protocol') = 'string'
            AND jsonb_typeof(conversion_plan->'diagnostics') = 'array'
        )
    );
