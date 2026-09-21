-- Keep this schema and the same-version recoverer while external tasks exist.
-- Reverting code does not cancel a provider task or reverse a billing receipt.
ALTER TABLE upstream_jobs ADD COLUMN IF NOT EXISTS workspace_id BIGINT REFERENCES workspaces(id);
ALTER TABLE upstream_jobs ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 1;
UPDATE upstream_jobs job SET workspace_id=owner.workspace_id
FROM api_keys owner WHERE job.api_key_id=owner.id AND job.user_id=owner.user_id AND job.workspace_id IS NULL;
-- Unresolvable historical owners remain NULL: never guess their workspace.
CREATE INDEX IF NOT EXISTS upstream_jobs_owner_workspace ON upstream_jobs(workspace_id,api_key_id,public_request_id);
CREATE OR REPLACE FUNCTION bind_upstream_job_owner() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE owner_workspace BIGINT;
BEGIN
 IF TG_OP='UPDATE' THEN
  IF NEW.workspace_id IS DISTINCT FROM OLD.workspace_id OR NEW.api_key_id<>OLD.api_key_id
     OR NEW.user_id<>OLD.user_id OR NEW.account_id<>OLD.account_id THEN
   RAISE EXCEPTION 'job ownership and upstream binding are immutable' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 SELECT workspace_id INTO owner_workspace FROM api_keys WHERE id=NEW.api_key_id AND user_id=NEW.user_id;
 IF owner_workspace IS NULL OR (NEW.workspace_id IS NOT NULL AND NEW.workspace_id<>owner_workspace) THEN
  RAISE EXCEPTION 'job owner not found' USING ERRCODE='23514';
 END IF;
 NEW.workspace_id := owner_workspace;
 RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS upstream_job_owner_binding ON upstream_jobs;
CREATE TRIGGER upstream_job_owner_binding BEFORE INSERT OR UPDATE ON upstream_jobs
FOR EACH ROW EXECUTE FUNCTION bind_upstream_job_owner();
