-- Preserve the existing durable invalidation outbox after 271 withdraws plaintext.
-- Digests already are cache identifiers: never hash them a second time. Include
-- revoked credentials because their pre-revocation snapshots can still be cached.
CREATE OR REPLACE FUNCTION enqueue_api_key_auth_cache_invalidation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.status IS NOT DISTINCT FROM NEW.status
       AND OLD.deleted_at IS NOT DISTINCT FROM NEW.deleted_at
       AND OLD.user_id IS NOT DISTINCT FROM NEW.user_id
       AND OLD.group_id IS NOT DISTINCT FROM NEW.group_id
       AND OLD.workspace_id IS NOT DISTINCT FROM NEW.workspace_id
       AND OLD.ip_whitelist IS NOT DISTINCT FROM NEW.ip_whitelist
       AND OLD.ip_blacklist IS NOT DISTINCT FROM NEW.ip_blacklist
       AND OLD.expires_at IS NOT DISTINCT FROM NEW.expires_at THEN
        RETURN NEW;
    END IF;
    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT credential_digest FROM api_key_credentials WHERE api_key_id = OLD.id;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_user_auth_cache_invalidation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.status IS NOT DISTINCT FROM NEW.status
       AND OLD.role IS NOT DISTINCT FROM NEW.role
       AND OLD.deleted_at IS NOT DISTINCT FROM NEW.deleted_at THEN
        RETURN NEW;
    END IF;
    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT credential.credential_digest
    FROM api_key_credentials credential JOIN api_keys api_key ON api_key.id = credential.api_key_id
    WHERE api_key.user_id = OLD.id AND api_key.deleted_at IS NULL;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;

-- Keep the full security/pricing projection covered by migration 193.
CREATE OR REPLACE FUNCTION enqueue_group_auth_cache_invalidation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.status IS NOT DISTINCT FROM NEW.status
       AND OLD.is_exclusive IS NOT DISTINCT FROM NEW.is_exclusive
       AND OLD.allow_image_generation IS NOT DISTINCT FROM NEW.allow_image_generation
       AND OLD.platform IS NOT DISTINCT FROM NEW.platform
       AND OLD.subscription_type IS NOT DISTINCT FROM NEW.subscription_type
       AND OLD.rate_multiplier IS NOT DISTINCT FROM NEW.rate_multiplier
       AND OLD.peak_rate_enabled IS NOT DISTINCT FROM NEW.peak_rate_enabled
       AND OLD.peak_start IS NOT DISTINCT FROM NEW.peak_start
       AND OLD.peak_end IS NOT DISTINCT FROM NEW.peak_end
       AND OLD.peak_rate_multiplier IS NOT DISTINCT FROM NEW.peak_rate_multiplier
       AND OLD.profit_control_enabled IS NOT DISTINCT FROM NEW.profit_control_enabled
       AND OLD.profit_min_margin IS NOT DISTINCT FROM NEW.profit_min_margin
       AND OLD.profit_safety_buffer IS NOT DISTINCT FROM NEW.profit_safety_buffer
       AND OLD.deleted_at IS NOT DISTINCT FROM NEW.deleted_at THEN
        RETURN NEW;
    END IF;
    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT credential.credential_digest
    FROM api_key_credentials credential JOIN api_keys api_key ON api_key.id = credential.api_key_id
    WHERE api_key.group_id = OLD.id AND api_key.deleted_at IS NULL;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_allowed_group_auth_cache_invalidation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    target_user_id BIGINT;
    target_group_id BIGINT;
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.user_id IS NOT DISTINCT FROM NEW.user_id
       AND OLD.group_id IS NOT DISTINCT FROM NEW.group_id THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' OR TG_OP = 'DELETE' THEN
        INSERT INTO auth_cache_invalidation_outbox (cache_key)
        SELECT credential.credential_digest
        FROM api_key_credentials credential JOIN api_keys api_key ON api_key.id = credential.api_key_id
        JOIN groups bound_group ON bound_group.id = api_key.group_id
        WHERE api_key.user_id = OLD.user_id AND api_key.group_id = OLD.group_id
          AND api_key.deleted_at IS NULL AND bound_group.is_exclusive;
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    target_user_id := NEW.user_id;
    target_group_id := NEW.group_id;
    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT credential.credential_digest
    FROM api_key_credentials credential JOIN api_keys api_key ON api_key.id = credential.api_key_id
    JOIN groups bound_group ON bound_group.id = api_key.group_id
    WHERE api_key.user_id = target_user_id AND api_key.group_id = target_group_id
      AND api_key.deleted_at IS NULL AND bound_group.is_exclusive;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_credential_auth_cache_invalidation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.credential_digest IS NOT DISTINCT FROM NEW.credential_digest
       AND OLD.api_key_id IS NOT DISTINCT FROM NEW.api_key_id
       AND OLD.workspace_id IS NOT DISTINCT FROM NEW.workspace_id
       AND OLD.status IS NOT DISTINCT FROM NEW.status
       AND OLD.valid_until IS NOT DISTINCT FROM NEW.valid_until
       AND OLD.revoked_at IS NOT DISTINCT FROM NEW.revoked_at THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' OR TG_OP = 'DELETE' THEN
        INSERT INTO auth_cache_invalidation_outbox (cache_key) VALUES (OLD.credential_digest);
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    IF TG_OP = 'INSERT' OR NEW.credential_digest IS DISTINCT FROM OLD.credential_digest THEN
        INSERT INTO auth_cache_invalidation_outbox (cache_key) VALUES (NEW.credential_digest);
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_api_key_credentials_auth_cache_invalidation ON api_key_credentials;
CREATE TRIGGER trg_api_key_credentials_auth_cache_invalidation
AFTER INSERT OR UPDATE OR DELETE ON api_key_credentials
FOR EACH ROW EXECUTE FUNCTION enqueue_credential_auth_cache_invalidation();
