-- Expand only. After EVERY instance runs the dual-read binary, the operator
-- activates once with SELECT activate_hosted_account_identity(). Do not activate
-- this from startup: older instances do not understand the subtype restrictions.
ALTER TABLE groups ADD COLUMN IF NOT EXISTS account_type varchar(32) NOT NULL DEFAULT '';
ALTER TABLE composite_model_routes ADD COLUMN IF NOT EXISTS account_type varchar(32) NOT NULL DEFAULT '';
ALTER TABLE user_platform_quotas ADD COLUMN IF NOT EXISTS account_type varchar(32) NOT NULL DEFAULT '';

-- Retain legacy Vertex budget keys independently from ordinary Gemini budgets.
ALTER TABLE user_platform_quotas DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;
ALTER TABLE user_platform_quotas ADD CONSTRAINT user_platform_quotas_platform_check
 CHECK (platform IN ('anthropic','openai','gemini','antigravity','grok','kimi','zhipu','deepseek','minimax','opencode_go','cursor','qianwen','commandcode','qianfan','hunyuan','mistral','bedrock','volcengine','vertex'));

CREATE TABLE IF NOT EXISTS hosted_identity_migration_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    activated_at timestamptz,
    rolled_back_at timestamptz
);
INSERT INTO hosted_identity_migration_state(singleton) VALUES (true) ON CONFLICT DO NOTHING;

-- Only fields owned by this migration are saved. Credentials and historical
-- financial records never enter this mapping. New rows are never recorded here.
CREATE TABLE IF NOT EXISTS hosted_identity_migration_mapping (
    entity text NOT NULL,
    entity_id bigint NOT NULL,
    before_config jsonb NOT NULL,
    after_config jsonb NOT NULL,
    PRIMARY KEY(entity, entity_id)
);

CREATE OR REPLACE FUNCTION hosted_identity_canonical_platform(platform text)
RETURNS text LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT CASE platform WHEN 'bedrock' THEN 'anthropic' WHEN 'vertex' THEN 'gemini' ELSE platform END
$$;

CREATE OR REPLACE FUNCTION normalize_hosted_identity_write()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE required_type text;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM hosted_identity_migration_state WHERE activated_at IS NOT NULL AND rolled_back_at IS NULL) THEN
        RETURN NEW;
    END IF;
    IF TG_TABLE_NAME = 'accounts' THEN
        IF NEW.platform IN ('bedrock','vertex') THEN
            required_type := CASE NEW.platform WHEN 'bedrock' THEN 'bedrock' ELSE 'service_account' END;
            IF NEW.type <> required_type THEN
                RAISE EXCEPTION 'hosted platform and account type do not match';
            END IF;
            IF NEW.platform = 'bedrock' THEN
                NEW.extra := COALESCE(NEW.extra,'{}'::jsonb) || '{"hosted_bedrock_runtime":"converse"}'::jsonb;
            END IF;
            NEW.platform := hosted_identity_canonical_platform(NEW.platform);
        END IF;
    ELSIF TG_TABLE_NAME = 'composite_model_routes' THEN
        IF NEW.target_platform IN ('bedrock','vertex') THEN
            required_type := CASE NEW.target_platform WHEN 'bedrock' THEN 'bedrock' ELSE 'service_account' END;
            IF NEW.account_type NOT IN ('',required_type) THEN
                RAISE EXCEPTION 'hosted route and account type do not match';
            END IF;
            NEW.account_type := required_type;
            NEW.target_platform := hosted_identity_canonical_platform(NEW.target_platform);
        END IF;
    ELSIF TG_TABLE_NAME = 'groups' THEN
        IF NEW.platform IN ('bedrock','vertex') THEN
            required_type := CASE NEW.platform WHEN 'bedrock' THEN 'bedrock' ELSE 'service_account' END;
            IF NEW.account_type NOT IN ('',required_type) THEN
                RAISE EXCEPTION 'hosted group and account type do not match';
            END IF;
            NEW.account_type := required_type;
            NEW.platform := hosted_identity_canonical_platform(NEW.platform);
        END IF;
    ELSIF TG_TABLE_NAME = 'user_platform_quotas' THEN
        IF NEW.platform IN ('bedrock','vertex') THEN
            NEW.account_type := CASE NEW.platform WHEN 'bedrock' THEN 'bedrock' ELSE 'service_account' END;
        END IF;
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS hosted_identity_normalize ON accounts;
CREATE TRIGGER hosted_identity_normalize BEFORE INSERT OR UPDATE ON accounts FOR EACH ROW EXECUTE FUNCTION normalize_hosted_identity_write();
DROP TRIGGER IF EXISTS hosted_identity_normalize ON groups;
CREATE TRIGGER hosted_identity_normalize BEFORE INSERT OR UPDATE ON groups FOR EACH ROW EXECUTE FUNCTION normalize_hosted_identity_write();
DROP TRIGGER IF EXISTS hosted_identity_normalize ON composite_model_routes;
CREATE TRIGGER hosted_identity_normalize BEFORE INSERT OR UPDATE ON composite_model_routes FOR EACH ROW EXECUTE FUNCTION normalize_hosted_identity_write();
DROP TRIGGER IF EXISTS hosted_identity_normalize ON user_platform_quotas;
CREATE TRIGGER hosted_identity_normalize BEFORE INSERT OR UPDATE ON user_platform_quotas FOR EACH ROW EXECUTE FUNCTION normalize_hosted_identity_write();

CREATE OR REPLACE FUNCTION activate_hosted_account_identity()
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    PERFORM 1 FROM hosted_identity_migration_state WHERE singleton FOR UPDATE;
    IF EXISTS (SELECT 1 FROM hosted_identity_migration_state WHERE activated_at IS NOT NULL) THEN RETURN; END IF;
    -- Prevent old writes from landing between the captured mapping and backfill.
    LOCK TABLE accounts, groups, composite_model_routes, user_platform_quotas IN SHARE ROW EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM accounts WHERE (platform='bedrock' AND type<>'bedrock') OR (platform='vertex' AND type<>'service_account')) THEN
        RAISE EXCEPTION 'repair invalid hosted account types before activation';
    END IF;
    INSERT INTO hosted_identity_migration_mapping
    SELECT 'accounts', id,
        jsonb_build_object('platform',platform,'type',type,'runtime',extra->'hosted_bedrock_runtime'),
        jsonb_build_object('platform',hosted_identity_canonical_platform(platform),'type',type,'runtime',CASE WHEN platform='bedrock' THEN '"converse"'::jsonb ELSE extra->'hosted_bedrock_runtime' END)
    FROM accounts WHERE platform IN ('bedrock','vertex') ON CONFLICT DO NOTHING;
    INSERT INTO hosted_identity_migration_mapping
    SELECT 'groups', id, jsonb_build_object('platform',platform,'account_type',account_type),
        jsonb_build_object('platform',hosted_identity_canonical_platform(platform),'account_type',CASE platform WHEN 'bedrock' THEN 'bedrock' ELSE 'service_account' END)
    FROM groups WHERE platform IN ('bedrock','vertex') ON CONFLICT DO NOTHING;
    INSERT INTO hosted_identity_migration_mapping
    SELECT 'composite_model_routes', id, jsonb_build_object('platform',target_platform,'account_type',account_type),
        jsonb_build_object('platform',hosted_identity_canonical_platform(target_platform),'account_type',CASE target_platform WHEN 'bedrock' THEN 'bedrock' ELSE 'service_account' END)
    FROM composite_model_routes WHERE target_platform IN ('bedrock','vertex') ON CONFLICT DO NOTHING;
    INSERT INTO hosted_identity_migration_mapping
    SELECT 'user_platform_quotas', id, jsonb_build_object('platform',platform,'account_type',account_type),
        jsonb_build_object('platform',platform,'account_type',CASE platform WHEN 'bedrock' THEN 'bedrock' ELSE 'service_account' END)
    FROM user_platform_quotas WHERE platform IN ('bedrock','vertex') ON CONFLICT DO NOTHING;
    UPDATE hosted_identity_migration_state SET activated_at=clock_timestamp() WHERE singleton;
    UPDATE accounts SET platform=platform WHERE platform IN ('bedrock','vertex');
    UPDATE groups SET platform=platform WHERE platform IN ('bedrock','vertex');
    UPDATE composite_model_routes SET target_platform=target_platform WHERE target_platform IN ('bedrock','vertex');
    UPDATE user_platform_quotas SET platform=platform WHERE platform IN ('bedrock','vertex');
    -- Quota storage keys and all windows/usage stay unchanged. The read adapter
    -- exposes their product identity and hosted subtype without merging budgets.
END
$$;

CREATE OR REPLACE FUNCTION rollback_hosted_account_identity()
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    PERFORM 1 FROM hosted_identity_migration_state WHERE singleton FOR UPDATE;
    IF NOT EXISTS (SELECT 1 FROM hosted_identity_migration_state WHERE activated_at IS NOT NULL AND rolled_back_at IS NULL) THEN RETURN; END IF;
    LOCK TABLE accounts, groups, composite_model_routes, user_platform_quotas IN SHARE ROW EXCLUSIVE MODE;
    UPDATE hosted_identity_migration_state SET rolled_back_at=clock_timestamp() WHERE singleton;
    UPDATE accounts current SET platform=record.before_config->>'platform',
        extra=CASE WHEN record.before_config->'runtime'='null'::jsonb THEN current.extra-'hosted_bedrock_runtime'
              ELSE jsonb_set(COALESCE(current.extra,'{}'::jsonb),'{hosted_bedrock_runtime}',record.before_config->'runtime') END
    FROM hosted_identity_migration_mapping record
    WHERE record.entity='accounts' AND current.id=record.entity_id
      AND jsonb_build_object('platform',current.platform,'type',current.type,'runtime',current.extra->'hosted_bedrock_runtime')=record.after_config;
    UPDATE groups current SET platform=record.before_config->>'platform', account_type=record.before_config->>'account_type'
    FROM hosted_identity_migration_mapping record
    WHERE record.entity='groups' AND current.id=record.entity_id
      AND jsonb_build_object('platform',current.platform,'account_type',current.account_type)=record.after_config;
    UPDATE composite_model_routes current SET target_platform=record.before_config->>'platform', account_type=record.before_config->>'account_type'
    FROM hosted_identity_migration_mapping record
    WHERE record.entity='composite_model_routes' AND current.id=record.entity_id
      AND jsonb_build_object('platform',current.target_platform,'account_type',current.account_type)=record.after_config;
    UPDATE user_platform_quotas current SET account_type=record.before_config->>'account_type'
    FROM hosted_identity_migration_mapping record
    WHERE record.entity='user_platform_quotas' AND current.id=record.entity_id
      AND jsonb_build_object('platform',current.platform,'account_type',current.account_type)=record.after_config;
END
$$;
