-- Operator-owned evidence. A deployment starts with no enabled media endpoints.
CREATE TABLE task_media_verifications (
 id uuid PRIMARY KEY, workspace_id bigint NOT NULL, project_id bigint NOT NULL,
 group_id bigint NOT NULL REFERENCES groups(id), account_id bigint NOT NULL REFERENCES accounts(id),
 model text NOT NULL, action text NOT NULL, adapter text NOT NULL,
 capability_evidence text NOT NULL CHECK (btrim(capability_evidence) <> ''),
 action_evidence text NOT NULL CHECK (btrim(action_evidence) <> ''),
 funding_evidence text NOT NULL CHECK (btrim(funding_evidence) <> ''),
 cancel_evidence text CHECK (btrim(cancel_evidence) <> ''),
 price_version text NOT NULL CHECK (btrim(price_version) <> ''),
 billing_unit text NOT NULL CHECK (billing_unit IN ('character','second')),
 usd_rate numeric(38,18) NOT NULL CHECK (usd_rate >= 0), per_units bigint NOT NULL CHECK (per_units > 0),
 verified_at timestamptz NOT NULL, expires_at timestamptz NOT NULL CHECK (expires_at>verified_at), revoked_at timestamptz,
 FOREIGN KEY (workspace_id,project_id) REFERENCES projects(workspace_id,id)
);
-- Keep audit evidence immutable. Revocation is one-way; publish a new row to reverify.
CREATE FUNCTION task_media_protect_verification() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL OR
    (to_jsonb(NEW)-'revoked_at') IS DISTINCT FROM (to_jsonb(OLD)-'revoked_at') THEN
    RAISE EXCEPTION 'publish a new media verification';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER task_media_verification_revision BEFORE UPDATE OR DELETE ON task_media_verifications
FOR EACH ROW EXECUTE FUNCTION task_media_protect_verification();
CREATE INDEX task_media_verifications_lookup ON task_media_verifications(workspace_id,project_id,group_id,model,account_id) WHERE revoked_at IS NULL;

CREATE FUNCTION task_media_revoke_account_approvals() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='accounts' THEN
  UPDATE task_media_verifications SET revoked_at=clock_timestamp() WHERE account_id=NEW.id AND revoked_at IS NULL;
 ELSE
  UPDATE task_media_verifications SET revoked_at=clock_timestamp() WHERE account_id=coalesce(NEW.account_id,OLD.account_id) AND revoked_at IS NULL;
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER task_media_account_changed AFTER UPDATE OF credentials,workspace_id,platform,type,proxy_id,proxy_group_id,status,deleted_at
 ON accounts FOR EACH ROW WHEN (
 (OLD.credentials,OLD.workspace_id,OLD.platform,OLD.type,OLD.proxy_id,OLD.proxy_group_id,OLD.status,OLD.deleted_at) IS DISTINCT FROM
 (NEW.credentials,NEW.workspace_id,NEW.platform,NEW.type,NEW.proxy_id,NEW.proxy_group_id,NEW.status,NEW.deleted_at))
 EXECUTE FUNCTION task_media_revoke_account_approvals();
CREATE TRIGGER task_media_credential_changed AFTER INSERT OR UPDATE OR DELETE ON credential_secrets
 FOR EACH ROW EXECUTE FUNCTION task_media_revoke_account_approvals();

CREATE TABLE task_media_attributes (
 task_id text PRIMARY KEY REFERENCES image_tasks(id), kind text NOT NULL CHECK(kind IN ('audio','video')),
 verification_id uuid REFERENCES task_media_verifications(id), account_id bigint NOT NULL REFERENCES accounts(id),
 format text CHECK(format IN ('wav','mp4')), duration_seconds integer CHECK(duration_seconds>0),
 size text, result_duration_ms bigint CHECK(result_duration_ms>0 AND result_duration_ms<=120000), billing_unit text NOT NULL CHECK(billing_unit IN ('second','character')),
 price_version text, provider_job_id text UNIQUE
);

-- Capture original ownership and any unambiguous project mapping. Re-running this
-- function after resolving a quarantine preserves the first accepted owner.
CREATE FUNCTION task_media_import(pk bigint) RETURNS void LANGUAGE plpgsql AS $$
DECLARE j upstream_jobs; w bigint; p bigint; task text; k text;
BEGIN
 SELECT * INTO j FROM upstream_jobs WHERE id=pk AND platform='qianwen';
 IF j.family='video' AND j.billing_snapshot->>'model' IN ('wan2.6-t2v','wan2.6-i2v') THEN k:='video';
 ELSIF j.family='realtime_turn' AND j.billing_snapshot->>'model' IN ('qwen3-tts-flash-realtime','qwen3-tts-flash-realtime-2025-09-18','qwen3-tts-flash-realtime-2025-11-27') THEN k:='audio';
 ELSE RETURN; END IF;
 IF EXISTS(SELECT 1 FROM task_media_attributes WHERE provider_job_id=j.public_request_id) THEN RETURN; END IF;
 SELECT o.workspace_id,coalesce(r.project_id,policy.project_id,kr.project_id,d.id) INTO w,p
 FROM identity_resource_owners o JOIN projects d ON d.workspace_id=o.workspace_id AND d.is_default
 LEFT JOIN project_resources r ON r.kind=o.kind AND r.resource_id=o.id AND r.workspace_id=o.workspace_id
 LEFT JOIN project_resources kr ON kr.kind='api_keys' AND kr.resource_id=o.key_id AND kr.workspace_id=o.workspace_id
 LEFT JOIN project_key_policies policy ON policy.api_key_id=o.key_id AND policy.workspace_id=o.workspace_id
 WHERE o.kind='upstream_jobs' AND o.id=pk
 AND NOT EXISTS(SELECT 1 FROM identity_scope_quarantine q WHERE (q.kind=o.kind AND q.resource_id=pk) OR (q.kind='api_keys' AND q.resource_id=o.key_id))
 AND (r.project_id IS NULL OR policy.project_id IS NULL OR r.project_id=policy.project_id)
 AND (r.project_id IS NULL OR kr.project_id IS NULL OR r.project_id=kr.project_id)
 AND (policy.project_id IS NULL OR kr.project_id IS NULL OR policy.project_id=kr.project_id);
 IF p IS NULL THEN RETURN; END IF;
 task := 'media_'||pk;
 INSERT INTO image_tasks(id,workspace_id,project_id,actor_id,owner_id,funding_user_id,api_key_id,source,source_id,model,state,acceptance,created_at)
 VALUES(task,w,p,j.user_id,j.user_id,j.user_id,j.api_key_id,'upstream_job',j.public_request_id,
 coalesce(nullif(j.billing_snapshot->>'requested_model',''),j.billing_snapshot->>'model'),'queued','not_sent',j.submitted_at)
 ON CONFLICT DO NOTHING;
 IF NOT EXISTS(SELECT 1 FROM image_tasks WHERE id=task AND workspace_id=w AND project_id=p AND source_id=j.public_request_id) THEN RETURN; END IF;
 INSERT INTO task_media_attributes(task_id,kind,account_id,format,billing_unit,price_version,provider_job_id)
 VALUES(task,k,j.account_id,CASE WHEN k='video' THEN 'mp4' END,CASE WHEN k='video' THEN 'second' ELSE 'character' END,
 j.billing_snapshot#>>'{realtime,price_version}',j.public_request_id) ON CONFLICT DO NOTHING;
END $$;
CREATE FUNCTION task_media_capture_job() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM task_media_import(NEW.id); RETURN NULL; END $$;
CREATE TRIGGER task_media_job_insert AFTER INSERT ON upstream_jobs FOR EACH ROW
 WHEN (NEW.platform='qianwen' AND NEW.family IN ('video','realtime_turn')) EXECUTE FUNCTION task_media_capture_job();
SELECT task_media_import(id) FROM upstream_jobs WHERE platform='qianwen' AND family IN ('video','realtime_turn');

-- A native send permit transfers a v2 video job to the existing durable
-- poller. A claim made before a lost generation cannot publish a result.
CREATE VIEW task_media_owned_jobs AS
SELECT m.task_id,u.* FROM task_media_attributes m JOIN image_tasks t ON t.id=m.task_id
JOIN upstream_jobs u ON u.public_request_id=m.provider_job_id AND u.workspace_id=t.workspace_id
 AND u.platform='qianwen' AND u.user_id=t.owner_id AND u.api_key_id=t.api_key_id AND u.account_id=m.account_id
WHERE t.source='upstream_job' OR (t.source='gateway' AND t.request_id::text=u.public_request_id
 AND EXISTS (SELECT 1 FROM gateway_execution_attempts a WHERE a.request_id=t.request_id AND a.account_id=m.account_id AND a.dispatched_at IS NOT NULL));

CREATE VIEW task_center_history AS SELECT h.*,coalesce(m.kind,'image') AS kind,m.provider_job_id,
 CASE WHEN u.id IS NULL THEN CASE WHEN h.source IN ('gateway','redis_image') AND h.state='running' AND h.lease_until<=clock_timestamp() THEN 'unknown' ELSE h.current_state END
 WHEN lower(u.resource_state->>'provider_status') IN ('canceled','cancelled') OR (u.family='realtime_turn' AND u.result_ref IN ('cancelled','canceled')) THEN 'canceled'
 WHEN u.error_code IN ('submission_unknown','invalid_upstream_response') OR u.status='expired' OR (u.status='submitted' AND u.upstream_task_id='') THEN 'unknown'
 WHEN u.status='submitted' THEN 'running' ELSE u.status END AS display_state,
 CASE WHEN u.id IS NULL THEN h.provider_state WHEN u.upstream_task_id<>'' THEN 'accepted'
 WHEN u.error_code='upstream_rejected' THEN 'rejected' ELSE 'unknown' END AS display_acceptance,
 CASE WHEN u.id IS NULL THEN h.stage ELSE coalesce(nullif(u.resource_state->>'provider_status',''),u.status) END AS display_stage,
 CASE WHEN m.kind IS NULL THEN h.cancellation
 WHEN lower(u.resource_state->>'provider_status') IN ('canceled','cancelled') OR (u.family='realtime_turn' AND u.result_ref IN ('cancelled','canceled')) THEN 'confirmed'
 WHEN h.cancel_state='requested' THEN 'requested'
 WHEN u.status='submitted' AND u.upstream_task_id<>'' AND v.cancel_evidence IS NOT NULL AND v.revoked_at IS NULL AND v.verified_at<=now() AND v.expires_at>now() THEN 'available'
 ELSE 'unsupported' END AS display_cancel,
 greatest(h.changed_at,u.finished_at,u.last_polled_at) AS display_updated,
 h.current_version+CASE WHEN h.source='gateway' THEN coalesce(u.version,0) ELSE 0 END AS display_version,
 u.account_id AS media_account,u.charged_usage_log_id AS media_usage
FROM image_task_history h LEFT JOIN task_media_attributes m ON m.task_id=h.id
LEFT JOIN task_media_owned_jobs u ON u.task_id=h.id
LEFT JOIN task_media_verifications v ON v.id=m.verification_id;

CREATE VIEW task_center_artifacts AS SELECT a.* FROM image_artifacts a
WHERE NOT EXISTS (SELECT 1 FROM task_media_attributes m WHERE m.task_id=a.task_id AND m.provider_job_id IS NOT NULL)
UNION ALL
SELECT m.task_id,'video_1','1',u.public_request_id,'video/mp4',NULL::bigint,u.expires_at,false,
 CASE WHEN r.task_id IS NOT NULL THEN 'revoked' WHEN u.expires_at<=clock_timestamp() THEN 'expired' ELSE 'available' END
FROM task_media_attributes m JOIN task_media_owned_jobs u ON u.task_id=m.task_id AND u.family='video'
LEFT JOIN image_artifact_revocations r ON r.task_id=m.task_id AND r.artifact_id='video_1'
WHERE m.kind='video' AND u.status='succeeded' AND u.result_ref LIKE 'https://%';
