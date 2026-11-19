DO $migration$
BEGIN
 IF current_setting('omnihive.restore_journal', true) IS DISTINCT FROM 'on' THEN
   RETURN;
 END IF;
 CREATE SCHEMA IF NOT EXISTS restore_journal;
 CREATE TABLE IF NOT EXISTS restore_journal.targets (
   name text PRIMARY KEY CHECK (name IN ('isolated','production')),
   database_identity text NOT NULL UNIQUE
 );
 CREATE TABLE IF NOT EXISTS restore_journal.backups (
   id text PRIMARY KEY, record jsonb NOT NULL, review_required boolean NOT NULL, removed_at timestamptz
 );
 CREATE TABLE IF NOT EXISTS restore_journal.previews (
   id text PRIMARY KEY, workspace_id bigint NOT NULL, actor_id bigint NOT NULL, request_key text NOT NULL,
   fingerprint text NOT NULL, backup_id text NOT NULL REFERENCES restore_journal.backups(id),
   record jsonb NOT NULL, target text NOT NULL REFERENCES restore_journal.targets(name), digest text NOT NULL, counts jsonb NOT NULL,
   expires_at timestamptz NOT NULL DEFAULT now()+interval '30 minutes',
   UNIQUE(workspace_id,actor_id,request_key)
 );
 CREATE TABLE IF NOT EXISTS restore_journal.operations (
   id text PRIMARY KEY, workspace_id bigint NOT NULL, actor_id bigint NOT NULL, request_key text NOT NULL,
   session_key text NOT NULL, preview_id text NOT NULL UNIQUE REFERENCES restore_journal.previews(id),
   rehearsal_id text REFERENCES restore_journal.operations(id),
   target text NOT NULL REFERENCES restore_journal.targets(name),
   generation bigint NOT NULL DEFAULT 1 CHECK (generation>0), owner text, lease_until timestamptz,
   state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','running','succeeded','failed','unknown')),
   stage text NOT NULL DEFAULT 'queued',
   snapshot jsonb, report jsonb, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
   UNIQUE(workspace_id,actor_id,request_key),
   CHECK (state <> 'succeeded' OR (stage='verified' AND snapshot IS NOT NULL AND report IS NOT NULL)),
   CHECK ((target='production')=(rehearsal_id IS NOT NULL))
 );
 CREATE UNIQUE INDEX IF NOT EXISTS restore_target_active ON restore_journal.operations(target)
   WHERE state IN ('queued','running','unknown');
 CREATE TABLE IF NOT EXISTS restore_journal.legacy_requests (
   id text PRIMARY KEY, actor_id bigint NOT NULL, workspace_id bigint NOT NULL, request_key text NOT NULL,
   backup_id text NOT NULL REFERENCES restore_journal.backups(id),
   state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','submitted','failed')),
   session_key text NOT NULL, production_id text REFERENCES restore_journal.operations(id), reason text,
   created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
   authorized_until timestamptz NOT NULL DEFAULT clock_timestamp()+interval '15 minutes',
   next_check timestamptz NOT NULL DEFAULT clock_timestamp(),
   UNIQUE(workspace_id,actor_id,request_key)
 );
 CREATE UNIQUE INDEX IF NOT EXISTS legacy_request_pending ON restore_journal.legacy_requests(state) WHERE state='pending';
 CREATE TABLE IF NOT EXISTS restore_journal.events (
   sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, operation_id text NOT NULL REFERENCES restore_journal.operations(id),
   generation bigint NOT NULL, actor_id bigint NOT NULL, state text NOT NULL, stage text NOT NULL,
   observed_at timestamptz NOT NULL DEFAULT clock_timestamp()
 );
 CREATE OR REPLACE FUNCTION restore_journal.record_event() RETURNS trigger LANGUAGE plpgsql AS $event$
 BEGIN
   INSERT INTO restore_journal.events(operation_id,generation,actor_id,state,stage)
     VALUES (NEW.id,NEW.generation,NEW.actor_id,NEW.state,NEW.stage);
   RETURN NEW;
 END $event$;
 DROP TRIGGER IF EXISTS restore_event ON restore_journal.operations;
 CREATE TRIGGER restore_event AFTER INSERT OR UPDATE OF generation,state,stage ON restore_journal.operations
   FOR EACH ROW EXECUTE FUNCTION restore_journal.record_event();
END $migration$;
