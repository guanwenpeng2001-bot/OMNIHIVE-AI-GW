-- Owner: key-budget-periods. Rollback floor: a build that understands the
-- period-budget columns and the reservation ledger; never drop recorded usage.
--
-- Period budget is additive to the legacy quota/rate_limit_5h/1d/7d columns:
-- those keep their existing semantics and columns. A closed period is frozen
-- into api_key_budget_periods so late settlement is attributed to the period it
-- belongs to instead of being zeroed, and in-flight requests hold their
-- estimated cost in api_key_budget_reservations so concurrent requests cannot
-- overdraw the budget or the payer balance.
--
-- All statements are idempotent and safe to re-run.

ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS budget_period_mode VARCHAR(16) NOT NULL DEFAULT 'off',
    ADD COLUMN IF NOT EXISTS budget_period_seconds BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS budget_period_anchor VARCHAR(16) NOT NULL DEFAULT 'first_use',
    ADD COLUMN IF NOT EXISTS budget_calendar_unit VARCHAR(16) NOT NULL DEFAULT 'day',
    ADD COLUMN IF NOT EXISTS budget_timezone VARCHAR(64) NOT NULL DEFAULT 'UTC',
    ADD COLUMN IF NOT EXISTS budget_limit NUMERIC(20, 8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS budget_enforced BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS budget_used NUMERIC(20, 8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS budget_reserved NUMERIC(20, 8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS budget_overrun NUMERIC(20, 8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS budget_period_start TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS budget_period_end TIMESTAMPTZ;

COMMENT ON COLUMN api_keys.budget_period_mode IS 'off | duration | calendar';
COMMENT ON COLUMN api_keys.budget_period_anchor IS 'duration mode start alignment: first_use | day';
COMMENT ON COLUMN api_keys.budget_calendar_unit IS 'calendar mode unit: day | week | month';
COMMENT ON COLUMN api_keys.budget_limit IS 'period budget limit in USD (0 = no period budget)';
COMMENT ON COLUMN api_keys.budget_enforced IS 'platform-mandated budget: the owner cannot edit or clear it';
COMMENT ON COLUMN api_keys.budget_used IS 'settled spend of the current period, never exceeds budget_limit';
COMMENT ON COLUMN api_keys.budget_reserved IS 'in-flight holds of the current period';
COMMENT ON COLUMN api_keys.budget_overrun IS 'spend that did not fit the current period limit, recorded instead of dropped';

-- Frozen period ledger. One row per (key, period); a closed period is never
-- rewritten by later periods, and late settlement lands here.
CREATE TABLE IF NOT EXISTS api_key_budget_periods (
    id BIGSERIAL PRIMARY KEY,
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    limit_amount NUMERIC(20, 8) NOT NULL DEFAULT 0,
    used_amount NUMERIC(20, 8) NOT NULL DEFAULT 0,
    overrun_amount NUMERIC(20, 8) NOT NULL DEFAULT 0,
    closed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS api_key_budget_periods_key_start_unique
    ON api_key_budget_periods (api_key_id, period_start);
CREATE INDEX IF NOT EXISTS api_key_budget_periods_key_end
    ON api_key_budget_periods (api_key_id, period_end);

-- Request-level holds. (api_key_id, request_id) is the durable idempotency
-- identity; the same request can never hold twice, and settlement/release of a
-- hold is a state transition, not an additive write.
CREATE TABLE IF NOT EXISTS api_key_budget_reservations (
    id BIGSERIAL PRIMARY KEY,
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL,
    request_id VARCHAR(200) NOT NULL,
    hold_amount NUMERIC(20, 8) NOT NULL DEFAULT 0,
    balance_held BOOLEAN NOT NULL DEFAULT TRUE,
    settled_amount NUMERIC(20, 8) NOT NULL DEFAULT 0,
    uncovered_amount NUMERIC(20, 8) NOT NULL DEFAULT 0,
    status VARCHAR(16) NOT NULL DEFAULT 'reserved',
    period_start TIMESTAMPTZ,
    period_end TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS api_key_budget_reservations_key_request_unique
    ON api_key_budget_reservations (api_key_id, request_id);
CREATE INDEX IF NOT EXISTS api_key_budget_reservations_status_expires
    ON api_key_budget_reservations (status, expires_at);

-- Audited administrative adjustments. The owner cannot zero consumed usage;
-- an administrator records the delta and the reason instead.
CREATE TABLE IF NOT EXISTS api_key_budget_adjustments (
    id BIGSERIAL PRIMARY KEY,
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    actor_user_id BIGINT NOT NULL DEFAULT 0,
    delta_usd NUMERIC(20, 8) NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS api_key_budget_adjustments_key_created
    ON api_key_budget_adjustments (api_key_id, created_at DESC);
