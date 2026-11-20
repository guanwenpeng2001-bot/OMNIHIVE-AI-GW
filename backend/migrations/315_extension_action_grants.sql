-- An approval belongs to one activation. BE-20 replaces binding rows on
-- enable, disable and package replacement, including same-version rollbacks.
CREATE TABLE omnihive_extension_action_grants (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    binding_id bigint NOT NULL REFERENCES sub2api_plugin_bindings(id) ON DELETE CASCADE,
    action text NOT NULL CHECK (action IN ('provider.responses.v1','task.image.generate.v1')),
    package_version text NOT NULL,
    archive_sha256 text NOT NULL CHECK (archive_sha256 ~ '^[a-f0-9]{64}$'),
    workspace_id bigint NOT NULL,
    project_id bigint NOT NULL,
    account_id bigint NOT NULL,
    credential_version bigint NOT NULL,
    model text NOT NULL CHECK (length(trim(model)) BETWEEN 1 AND 256),
    approved_by bigint NOT NULL REFERENCES users(id),
    verification_id uuid NOT NULL,
    verified_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > verified_at),
    FOREIGN KEY (workspace_id,project_id) REFERENCES projects(workspace_id,id) ON DELETE CASCADE,
    FOREIGN KEY (account_id,credential_version) REFERENCES connection_credential_versions(account_id,version) ON DELETE CASCADE,
    UNIQUE (workspace_id,project_id,account_id,model,action)
);
CREATE INDEX omnihive_extension_action_binding ON omnihive_extension_action_grants(binding_id);

CREATE FUNCTION omnihive_verify_action_grant() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM 1 FROM sub2api_plugin_bindings b
    JOIN sub2api_plugin_installations i ON i.id=b.plugin_id
    WHERE b.id=NEW.binding_id AND b.enabled AND b.capability=NEW.action
      AND i.state='enabled' AND i.signature_status='trusted'
      AND i.manifest->>'schema_version'='2'
      AND i.version=NEW.package_version AND encode(sha256(i.artifact_data),'hex')=NEW.archive_sha256
      AND i.manifest->'capabilities' @> jsonb_build_array(jsonb_build_object(
          'id',b.capability,'platform',b.platform,'account_type',b.account_type))
    FOR SHARE OF i,b;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'extension action activation or artifact changed' USING ERRCODE='23514';
    END IF;
    PERFORM 1 FROM project_resources r
    JOIN projects p ON p.id=r.project_id AND p.workspace_id=r.workspace_id AND p.archived_at IS NULL
    JOIN accounts a ON a.id=r.resource_id AND a.workspace_id=r.workspace_id AND a.deleted_at IS NULL
    JOIN sub2api_plugin_bindings b ON b.id=NEW.binding_id AND b.platform=a.platform AND b.account_type=a.type
    JOIN credential_secrets cs ON cs.account_id=a.id AND cs.workspace_id=a.workspace_id AND cs.revoked_at IS NULL
    WHERE r.kind='accounts' AND r.workspace_id=NEW.workspace_id AND r.project_id=NEW.project_id AND r.resource_id=NEW.account_id
      AND cs.version=NEW.credential_version AND cs.reference=a.credentials->>'credential_secret_reference'
      AND NOT EXISTS (SELECT FROM identity_scope_quarantine q WHERE q.kind=r.kind AND q.resource_id=a.id)
    FOR SHARE OF r,p,a,cs;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'extension action project, account or credential changed' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER omnihive_verify_action_grant BEFORE INSERT OR UPDATE ON omnihive_extension_action_grants
    FOR EACH ROW EXECUTE FUNCTION omnihive_verify_action_grant();

CREATE FUNCTION omnihive_expire_package_actions() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM omnihive_extension_action_grants g USING sub2api_plugin_bindings b
      WHERE b.plugin_id=NEW.id AND g.binding_id=b.id;
    RETURN NEW;
END $$;
CREATE TRIGGER omnihive_expire_package_actions AFTER UPDATE OF version,manifest,artifact_data ON sub2api_plugin_installations
    FOR EACH ROW EXECUTE FUNCTION omnihive_expire_package_actions();

CREATE VIEW omnihive_verified_extension_actions AS
SELECT g.*, b.plugin_id, b.rollout_percent, cs.reference AS credential_reference
FROM omnihive_extension_action_grants g
JOIN sub2api_plugin_bindings b ON b.id=g.binding_id AND b.enabled AND b.capability=g.action
JOIN sub2api_plugin_installations i ON i.id=b.plugin_id AND i.state='enabled' AND i.version=g.package_version
JOIN projects p ON p.workspace_id=g.workspace_id AND p.id=g.project_id AND p.archived_at IS NULL
JOIN project_resources owner ON owner.workspace_id=p.workspace_id AND owner.project_id=p.id
    AND owner.kind='accounts' AND owner.resource_id=g.account_id
JOIN accounts a ON a.id=g.account_id AND a.workspace_id=g.workspace_id AND a.deleted_at IS NULL
    AND a.platform=b.platform AND a.type=b.account_type AND a.status='active' AND a.schedulable
JOIN credential_secrets cs ON cs.account_id=a.id AND cs.workspace_id=a.workspace_id
    AND cs.reference=a.credentials->>'credential_secret_reference' AND cs.version=g.credential_version AND cs.revoked_at IS NULL
WHERE b.rollout_percent>0 AND i.signature_status='trusted' AND g.verified_at<=CURRENT_TIMESTAMP AND g.expires_at>CURRENT_TIMESTAMP
    AND NOT EXISTS (SELECT FROM identity_scope_quarantine q WHERE q.kind='accounts' AND q.resource_id=a.id);
