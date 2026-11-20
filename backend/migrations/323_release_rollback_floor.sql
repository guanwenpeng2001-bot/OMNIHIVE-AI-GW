-- Raise this entry in the same migration as an incompatible schema change.
CREATE TABLE release_schema_compatibility (
    singleton boolean PRIMARY KEY CHECK (singleton),
    minimum_rollback_version text NOT NULL
);
INSERT INTO release_schema_compatibility VALUES (true, 'v0.2.23-omnihive.2');
