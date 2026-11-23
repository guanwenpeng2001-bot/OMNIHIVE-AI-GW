CREATE FUNCTION resolve_default_workspace_id() RETURNS bigint
LANGUAGE plpgsql STABLE SECURITY INVOKER AS $$
DECLARE result bigint;
BEGIN
    SELECT id INTO STRICT result FROM workspaces WHERE is_default;
    RETURN result;
END;
$$;

DO $$
DECLARE
    resource_table text;
    historical_grants bigint;
BEGIN
    PERFORM resolve_default_workspace_id();
    SELECT count(*) INTO historical_grants
    FROM project_grants g JOIN projects p ON p.id=g.project_id AND p.workspace_id=g.workspace_id
    JOIN workspaces w ON w.id=p.workspace_id WHERE w.is_default AND p.is_default;
    IF historical_grants > 0 THEN
        RAISE WARNING 'Shared Default has % historical grants; review ownership before rollout', historical_grants;
    END IF;
    FOREACH resource_table IN ARRAY ARRAY['accounts','groups','channels','proxies','channel_monitors',
        'channel_monitor_request_templates','api_keys','credential_secrets','api_key_credentials',
        'proxy_groups','proxy_group_members'] LOOP
        EXECUTE format('ALTER TABLE %I ALTER COLUMN workspace_id SET DEFAULT resolve_default_workspace_id()',resource_table);
    END LOOP;
END;
$$;
