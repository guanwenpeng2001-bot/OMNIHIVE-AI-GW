CREATE TABLE IF NOT EXISTS catalog_capability_evidence (
    source text NOT NULL CHECK(source IN ('approved_registry','legacy_discovery','connection_verification','legacy_visibility')),
    subject text NOT NULL,
    version text NOT NULL,
    workspace_id bigint,
    maturity text NOT NULL CHECK(maturity IN ('unknown','stable','experimental','deprecated')),
    imported_at timestamptz NOT NULL DEFAULT now(),
    payload jsonb NOT NULL,
    PRIMARY KEY(source,subject,version)
);

CREATE TABLE IF NOT EXISTS catalog_registry_versions (
    version text PRIMARY KEY,
    imported_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS catalog_publications (
    version text PRIMARY KEY,
    published_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK(expires_at > published_at),
    revocation_epoch bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    registry_version text NOT NULL,
    payload jsonb NOT NULL,
    FOREIGN KEY(registry_version) REFERENCES catalog_registry_versions(version)
);
CREATE TABLE IF NOT EXISTS catalog_publication_outbox (
    version text PRIMARY KEY REFERENCES catalog_publications(version),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION catalog_record_discovery() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE sample jsonb;
BEGIN
    IF jsonb_typeof(NEW.extra->'upstream_model_catalog')='object'
       AND (TG_OP='INSERT' OR
            (NEW.extra->'upstream_model_catalog',NEW.extra->'upstream_model_metadata')
            IS DISTINCT FROM (OLD.extra->'upstream_model_catalog',OLD.extra->'upstream_model_metadata')) THEN
        sample := jsonb_build_object('catalog',NEW.extra->'upstream_model_catalog',
                                     'metadata',NEW.extra->'upstream_model_metadata');
        INSERT INTO catalog_capability_evidence(source,subject,version,workspace_id,maturity,payload)
        VALUES ('legacy_discovery',NEW.id::text,encode(sha256(convert_to(sample::text,'UTF8')),'hex'),NEW.workspace_id,'unknown',sample)
        ON CONFLICT DO NOTHING;
    END IF;
    RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS catalog_discovery_versions ON accounts;
CREATE TRIGGER catalog_discovery_versions AFTER INSERT OR UPDATE OF extra ON accounts
FOR EACH ROW EXECUTE FUNCTION catalog_record_discovery();

INSERT INTO catalog_capability_evidence(source,subject,version,workspace_id,maturity,payload)
SELECT 'legacy_discovery',a.id::text,encode(sha256(convert_to(sample.payload::text,'UTF8')),'hex'),a.workspace_id,'unknown',sample.payload
FROM accounts a
CROSS JOIN LATERAL (
    SELECT jsonb_build_object('catalog',a.extra->'upstream_model_catalog','metadata',a.extra->'upstream_model_metadata') AS payload
) sample
WHERE jsonb_typeof(a.extra->'upstream_model_catalog')='object'
ON CONFLICT DO NOTHING;

CREATE INDEX IF NOT EXISTS catalog_connection_verification_lookup
    ON connection_verifications(workspace_id,account_id,expires_at DESC);

CREATE OR REPLACE FUNCTION catalog_record_connection_verification() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.eligible THEN
        INSERT INTO catalog_capability_evidence(source,subject,version,workspace_id,maturity,payload)
        VALUES ('connection_verification',NEW.account_id::text,NEW.id::text,NEW.workspace_id,'unknown',NEW.snapshot)
        ON CONFLICT DO NOTHING;
    END IF;
    RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS catalog_connection_verification_versions ON connection_verifications;
CREATE TRIGGER catalog_connection_verification_versions AFTER INSERT ON connection_verifications
FOR EACH ROW EXECUTE FUNCTION catalog_record_connection_verification();

INSERT INTO catalog_capability_evidence(source,subject,version,workspace_id,maturity,payload)
SELECT 'connection_verification',account_id::text,id::text,workspace_id,'unknown',snapshot
FROM connection_verifications WHERE eligible
ON CONFLICT DO NOTHING;
