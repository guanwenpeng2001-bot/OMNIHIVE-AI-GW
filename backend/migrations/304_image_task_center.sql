-- Original scope and funding remain frozen even when a legacy key changes hands.
CREATE TABLE image_tasks (
 id text PRIMARY KEY, workspace_id bigint NOT NULL, project_id bigint NOT NULL,
 actor_id bigint NOT NULL, owner_id bigint NOT NULL, funding_user_id bigint NOT NULL, api_key_id bigint NOT NULL,
 source text NOT NULL CHECK(source IN ('gateway','batch_image','upstream_job','redis_image')),
 source_id text NOT NULL, parent_id text REFERENCES image_tasks(id), model text NOT NULL,
 request_id uuid UNIQUE, adapter_version text, fingerprint bytea, submission_key bytea,
 state text NOT NULL CHECK(state IN ('queued','running','unknown','failed','succeeded','canceled')),
 acceptance text NOT NULL CHECK(acceptance IN ('not_sent','unknown','accepted','rejected')),
 progress integer CHECK(progress BETWEEN 0 AND 100), cancel_state text NOT NULL DEFAULT 'unsupported',
 version bigint NOT NULL DEFAULT 1, generation bigint NOT NULL DEFAULT 0,
 lease_owner text, lease_until timestamptz, replay_input text, input_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(source,source_id), FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);
CREATE INDEX image_tasks_seek ON image_tasks(workspace_id,project_id,created_at DESC,id DESC);
CREATE INDEX image_tasks_expired_lease ON image_tasks(lease_until) WHERE source='gateway' AND state='running';
CREATE TABLE image_task_commands (
 workspace_id bigint NOT NULL, project_id bigint NOT NULL, actor_id bigint NOT NULL, route text NOT NULL,
 key_hash bytea NOT NULL, fingerprint bytea NOT NULL, task_id text NOT NULL REFERENCES image_tasks(id),
 response jsonb, PRIMARY KEY(workspace_id,project_id,actor_id,route,key_hash)
);
CREATE TABLE image_task_artifacts (
 task_id text NOT NULL REFERENCES image_tasks(id), id text NOT NULL, item_id text NOT NULL,
 locator text NOT NULL, content_type text NOT NULL, size_bytes bigint CHECK(size_bytes>=0),
 expires_at timestamptz NOT NULL, PRIMARY KEY(task_id,id)
);
CREATE TABLE image_artifact_revocations (
 task_id text NOT NULL REFERENCES image_tasks(id), artifact_id text NOT NULL, revoked_at timestamptz NOT NULL DEFAULT now(),
 actor_id bigint NOT NULL, PRIMARY KEY(task_id,artifact_id)
);

-- The caller supplies ids from persisted rows, never guessed project ids.
CREATE FUNCTION image_task_map_legacy(src text, pk bigint) RETURNS void LANGUAGE plpgsql AS $$
DECLARE mapped_kind text; w bigint; p bigint;
BEGIN
 mapped_kind := CASE src WHEN 'batch_image' THEN 'batch_image_jobs' ELSE 'upstream_jobs' END;
 SELECT o.workspace_id,coalesce(r.project_id,k.project_id,kr.project_id,d.id) INTO w,p
 FROM identity_resource_owners o JOIN projects d ON d.workspace_id=o.workspace_id AND d.is_default
 LEFT JOIN project_resources r ON r.kind=o.kind AND r.resource_id=o.id AND r.workspace_id=o.workspace_id
 LEFT JOIN project_resources kr ON kr.kind='api_keys' AND kr.resource_id=o.key_id AND kr.workspace_id=o.workspace_id
 LEFT JOIN project_key_policies k ON k.api_key_id=o.key_id AND k.workspace_id=o.workspace_id
 WHERE o.kind=mapped_kind AND o.id=pk
 AND NOT EXISTS(SELECT 1 FROM identity_scope_quarantine q WHERE q.kind=o.kind AND q.resource_id=o.id)
 AND (r.project_id IS NULL OR k.project_id IS NULL OR r.project_id=k.project_id);
 IF p IS NULL THEN RETURN; END IF;
 IF src='batch_image' THEN
  INSERT INTO image_tasks(id,workspace_id,project_id,actor_id,owner_id,funding_user_id,api_key_id,source,source_id,model,state,acceptance,created_at)
  SELECT 'batch_'||id,w,p,user_id,user_id,user_id,api_key_id,src,batch_id,model,'queued','not_sent',created_at
  FROM batch_image_jobs WHERE id=pk ON CONFLICT DO NOTHING;
 ELSE
  INSERT INTO image_tasks(id,workspace_id,project_id,actor_id,owner_id,funding_user_id,api_key_id,source,source_id,model,state,acceptance,created_at)
  SELECT 'up_'||id,w,p,user_id,user_id,user_id,api_key_id,src,public_request_id,
   coalesce(nullif(billing_snapshot->>'requested_model',''),billing_snapshot->>'model',''),'queued','not_sent',submitted_at
  FROM upstream_jobs WHERE id=pk AND family='image' ON CONFLICT DO NOTHING;
 END IF;
END $$;
CREATE FUNCTION image_task_capture_source() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM image_task_map_legacy(TG_ARGV[0], NEW.id); RETURN NULL; END $$;
CREATE TRIGGER image_batch_insert AFTER INSERT ON batch_image_jobs FOR EACH ROW EXECUTE FUNCTION image_task_capture_source('batch_image');
CREATE TRIGGER image_upstream_insert AFTER INSERT ON upstream_jobs FOR EACH ROW WHEN(NEW.family='image') EXECUTE FUNCTION image_task_capture_source('upstream_job');
SELECT image_task_map_legacy('batch_image',id) FROM batch_image_jobs;
SELECT image_task_map_legacy('upstream_job',id) FROM upstream_jobs WHERE family='image';

-- During overlap old workers remain the sole writers for their jobs. Read
-- their evidence live including expiration, partial outcomes and deletion.
CREATE VIEW image_task_history AS
SELECT t.*,coalesce(parent.id,t.parent_id) AS parent_task,
 coalesce(t.request_id,e.request_id) AS execution,
 CASE WHEN b.status='cancelled' OR (b.status='output_deleted' AND b.cancelled_count>0 AND b.cancelled_count=b.item_count) OR lower(u.resource_state->>'provider_status') IN ('canceled','cancelled') THEN 'canceled'
 WHEN b.status='uploading' OR (u.status IN ('submitted','expired') AND u.upstream_task_id='') THEN 'unknown'
 WHEN b.status IN ('completed','output_deleted') THEN CASE WHEN b.success_count>0 THEN 'succeeded' WHEN b.fail_count>0 THEN 'failed' ELSE 'unknown' END
 WHEN b.status='created' THEN 'queued' WHEN b.status='failed' THEN 'failed' WHEN b.status IS NOT NULL THEN 'running'
 WHEN u.status='submitted' THEN 'running' WHEN u.status='expired' THEN 'unknown' ELSE coalesce(u.status,t.state) END AS current_state,
 CASE WHEN b.success_count>0 AND b.fail_count+b.cancelled_count>0 THEN 'partially_succeeded'
 ELSE coalesce(b.status,u.status,t.state) END AS stage,
 CASE WHEN nullif(b.provider_job_name,'') IS NOT NULL OR nullif(u.upstream_task_id,'') IS NOT NULL THEN 'accepted'
 WHEN b.status='uploading' OR (u.id IS NOT NULL AND u.upstream_task_id='') THEN 'unknown' ELSE t.acceptance END AS provider_state,
 CASE WHEN b.status='cancelled' OR (b.status='output_deleted' AND b.cancelled_count>0 AND b.cancelled_count=b.item_count) OR lower(u.resource_state->>'provider_status') IN ('canceled','cancelled') THEN 'confirmed'
 WHEN b.status IN ('created','submitted','running') THEN CASE WHEN t.cancel_state='requested' THEN 'requested' ELSE 'available' END
 ELSE t.cancel_state END AS cancellation,
 coalesce(b.version+1,u.version,t.version)::bigint AS current_version,
 coalesce(b.updated_at,u.finished_at,u.last_polled_at,t.updated_at,t.created_at) AS changed_at,
 b.estimated_cost::numeric(28,8)::text AS estimate,b.actual_cost::numeric(28,8)::text AS batch_charge,
 coalesce(b.currency,'USD') AS currency, b.settled_at, b.item_count,b.success_count,b.fail_count,b.cancelled_count
FROM image_tasks t
LEFT JOIN batch_image_jobs b ON t.source='batch_image' AND b.batch_id=t.source_id AND b.user_id=t.owner_id AND b.api_key_id=t.api_key_id
LEFT JOIN upstream_jobs u ON t.source='upstream_job' AND u.public_request_id=t.source_id AND u.user_id=t.owner_id AND u.api_key_id=t.api_key_id
LEFT JOIN image_tasks parent ON parent.source='batch_image' AND parent.source_id=b.parent_batch_id AND parent.workspace_id=t.workspace_id AND parent.project_id=t.project_id AND parent.owner_id=t.owner_id
LEFT JOIN LATERAL (SELECT min(g.request_id::text)::uuid AS request_id FROM gateway_executions g WHERE g.workspace_id=t.workspace_id AND g.project_id=t.project_id
 AND g.owner_id=t.owner_id AND g.funding_user_id=t.funding_user_id AND g.api_key_id=t.api_key_id AND g.kind='job' AND g.legacy_id=t.source_id
 HAVING count(*)=1) e ON true
WHERE (t.source<>'batch_image' OR (b.id IS NOT NULL AND b.user_deleted_at IS NULL)) AND (t.source<>'upstream_job' OR u.id IS NOT NULL);

-- Malformed historical provider responses have no usable artifact.
CREATE FUNCTION image_task_result_array(value text) RETURNS jsonb LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE parsed jsonb;
BEGIN parsed:=value::jsonb; IF jsonb_typeof(parsed)='array' THEN RETURN parsed; END IF; RETURN '[]';
EXCEPTION WHEN invalid_text_representation THEN RETURN '[]'; END $$;

CREATE VIEW image_artifact_sources AS
SELECT task_id,id,item_id,locator,content_type,size_bytes,expires_at,false AS removed FROM image_task_artifacts
UNION ALL
SELECT t.id,'bi_'||i.id||'_'||n,i.custom_id,n::text,coalesce(nullif(i.mime_type,''),'application/octet-stream'),NULL::bigint,
 b.output_expires_at,b.output_deleted_at IS NOT NULL OR b.status='output_deleted'
FROM image_tasks t JOIN batch_image_jobs b ON t.source='batch_image' AND b.batch_id=t.source_id
 JOIN batch_image_items i ON i.job_id=b.batch_id CROSS JOIN LATERAL generate_series(0,i.image_count-1) n
WHERE i.status='success' AND b.settled_at IS NOT NULL
UNION ALL
SELECT t.id,'up_'||part.n,'1',part.val->>'url','application/octet-stream',NULL::bigint,u.expires_at,false
FROM image_tasks t JOIN upstream_jobs u ON t.source='upstream_job' AND u.public_request_id=t.source_id
CROSS JOIN LATERAL jsonb_array_elements(image_task_result_array(u.result_ref)) WITH ORDINALITY part(val,n)
WHERE u.status='succeeded' AND part.val->>'url' LIKE 'https://%';

CREATE VIEW image_artifacts AS SELECT a.*,
 CASE WHEN r.task_id IS NOT NULL OR a.removed THEN 'revoked'
 WHEN a.expires_at IS NULL OR a.expires_at<=clock_timestamp() THEN 'expired' ELSE 'available' END AS state
FROM image_artifact_sources a LEFT JOIN image_artifact_revocations r ON r.task_id=a.task_id AND r.artifact_id=a.id;
INSERT INTO identity_actions(action,category) VALUES('tasks.artifacts.revoke','project') ON CONFLICT DO NOTHING;
