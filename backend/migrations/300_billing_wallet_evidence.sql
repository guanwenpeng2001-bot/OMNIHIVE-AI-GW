-- Lock in receipt-to-payer order, matching settlement. The migration runner
-- holds these locks through the opening snapshot and trigger installation.
-- EXCLUSIVE also waits for workers that have only locked a receipt row.
LOCK TABLE usage_settlement_receipts IN EXCLUSIVE MODE;
LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE billing_funding_accounts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id bigint NOT NULL,
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    UNIQUE (user_id, currency),
    UNIQUE (id, currency)
);

-- Each event represents two equal and opposite postings in one currency.
CREATE TABLE billing_wallet_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    funding_account_id bigint NOT NULL,
    currency text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('opening', 'balance_change')),
    amount numeric(28,8) NOT NULL CHECK (amount <> 'NaN'::numeric),
    transaction_id xid8 NOT NULL DEFAULT pg_current_xact_id(),
    posted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (funding_account_id, currency) REFERENCES billing_funding_accounts(id, currency)
);
CREATE INDEX billing_wallet_events_account_idx ON billing_wallet_events(funding_account_id, transaction_id, id);
CREATE UNIQUE INDEX billing_wallet_events_opening_idx
    ON billing_wallet_events(funding_account_id) WHERE kind = 'opening';

INSERT INTO billing_funding_accounts(user_id, currency)
SELECT id, 'USD' FROM users;

INSERT INTO billing_wallet_events(funding_account_id, currency, kind, amount)
SELECT f.id, f.currency, 'opening', u.balance
FROM users u JOIN billing_funding_accounts f ON f.user_id = u.id AND f.currency = 'USD';

CREATE FUNCTION billing_capture_wallet_balance() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    funding_id bigint;
BEGIN
    IF TG_OP = 'INSERT' THEN
        INSERT INTO billing_funding_accounts(user_id, currency)
        VALUES (NEW.id, 'USD') RETURNING id INTO funding_id;
        INSERT INTO billing_wallet_events(funding_account_id, currency, kind, amount)
        VALUES (funding_id, 'USD', 'opening', NEW.balance);
    ELSIF OLD.balance IS DISTINCT FROM NEW.balance THEN
        SELECT id INTO STRICT funding_id FROM billing_funding_accounts
        WHERE user_id = NEW.id AND currency = 'USD';
        INSERT INTO billing_wallet_events(funding_account_id, currency, kind, amount)
        VALUES (funding_id, 'USD', 'balance_change', NEW.balance - OLD.balance);
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER billing_wallet_opened AFTER INSERT ON users
FOR EACH ROW EXECUTE FUNCTION billing_capture_wallet_balance();

CREATE TRIGGER billing_wallet_changed AFTER UPDATE OF balance ON users
FOR EACH ROW WHEN (OLD.balance IS DISTINCT FROM NEW.balance)
EXECUTE FUNCTION billing_capture_wallet_balance();

CREATE FUNCTION billing_reject_history_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'billing history is append-only';
END $$;

CREATE TRIGGER billing_wallet_events_immutable
BEFORE UPDATE OR DELETE OR TRUNCATE ON billing_wallet_events
FOR EACH STATEMENT EXECUTE FUNCTION billing_reject_history_change();

CREATE TRIGGER billing_funding_accounts_immutable
BEFORE UPDATE OR DELETE OR TRUNCATE ON billing_funding_accounts
FOR EACH STATEMENT EXECUTE FUNCTION billing_reject_history_change();

CREATE VIEW billing_wallet_postings AS
SELECT id AS event_id, funding_account_id, currency, kind, 'wallet'::text AS side,
       amount AS signed_amount, posted_at FROM billing_wallet_events
UNION ALL
SELECT id, funding_account_id, currency, kind, 'clearing', -amount, posted_at
FROM billing_wallet_events;

-- Both legacy and new workers already transition this receipt in the financial
-- transaction. Emit one durable notification at that boundary.
CREATE TABLE billing_settlement_outbox (
    receipt_id bigint PRIMARY KEY,
    transaction_id xid8 NOT NULL DEFAULT pg_current_xact_id(),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE FUNCTION billing_emit_settled_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO billing_settlement_outbox(receipt_id) VALUES (NEW.id)
    ON CONFLICT (receipt_id) DO NOTHING;
    RETURN NEW;
END $$;

CREATE TRIGGER billing_settlement_completed AFTER UPDATE OF state ON usage_settlement_receipts
FOR EACH ROW WHEN (OLD.state IS DISTINCT FROM NEW.state AND NEW.state = 'settled')
EXECUTE FUNCTION billing_emit_settled_receipt();
