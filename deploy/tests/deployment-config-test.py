#!/usr/bin/env python3
"""Render all Compose variants with isolated fixtures; never start services.

Requires Docker Compose 2.24+. Set TMPDIR to keep fixtures outside the checkout.
"""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULTS = {
    "SERVER_HOST": "0.0.0.0",
    "SERVER_PORT": "8080",
    "SERVER_H2C_ENABLED": "false",
    "SERVER_MAX_REQUEST_BODY_SIZE": "268435456",
    "GATEWAY_MAX_BODY_SIZE": "268435456",
    "GATEWAY_UPSTREAM_RESPONSE_READ_MAX_BYTES": "134217728",
    "DATABASE_MAX_OPEN_CONNS": "32",
    "DATABASE_MAX_IDLE_CONNS": "8",
    "REDIS_POOL_SIZE": "1024",
    "REDIS_MIN_IDLE_CONNS": "128",
    "SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL": "true",
    "SECURITY_URL_ALLOWLIST_ENABLED": "false",
    "SECURITY_URL_ALLOWLIST_ALLOW_PRIVATE_HOSTS": "true",
    "SECURITY_URL_ALLOWLIST_ALLOW_INSECURE_HTTP": "true",
}
CONNECTIONS = {
    "POSTGRES_PASSWORD": "deployment-fixture-password",
    "DATABASE_HOST": "external-postgres.invalid",
    "DATABASE_PASSWORD": "deployment-fixture-password",
    "REDIS_HOST": "external-redis.invalid",
}
OVERRIDES = {
    "SERVER_PORT": "18081",
    "SERVER_H2C_ENABLED": "true",
    "SERVER_H2C_MAX_CONCURRENT_STREAMS": "17",
    "GATEWAY_UPSTREAM_RESPONSE_READ_MAX_BYTES": "150994944",
    "DATABASE_MAX_OPEN_CONNS": "80",
    "DATABASE_MAX_IDLE_CONNS": "20",
    "REDIS_POOL_SIZE": "64",
    "REDIS_MIN_IDLE_CONNS": "8",
    "REDIS_MAXCLIENTS": "12345",
    "POSTGRES_MAX_CONNECTIONS": "300",
    "TOTP_ENCRYPTION_KEY": "ab" * 32,
    "JWT_SECRET": "cd" * 32,
    "LOG_LEVEL": "warn",
    "LINUXDO_CONNECT_USE_PKCE": "true",
    "BATCH_IMAGE_VERTEX_MANAGED_GCS_PREFIX": "batch-image/{env}/{batch_id}",
    "SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL": "false",
    "SECURITY_URL_ALLOWLIST_ENABLED": "true",
    "SECURITY_URL_ALLOWLIST_ALLOW_PRIVATE_HOSTS": "false",
    "SECURITY_URL_ALLOWLIST_ALLOW_INSECURE_HTTP": "false",
}


def render(compose_path, fixture_dir, overrides=None):
    # Prevent developer shell values from changing the fixture's interpolation.
    prefixes = ("COMPOSE_", "SERVER_", "DATABASE_", "POSTGRES_", "REDIS_",
                "GATEWAY_", "SECURITY_", "BATCH_IMAGE_", "JWT_", "TOTP_",
                "LOG_", "LINUXDO_")
    environment = {key: value for key, value in os.environ.items()
                   if not key.startswith(prefixes)}
    environment.update(CONNECTIONS)
    environment.update(overrides or {})
    result = subprocess.run(
        ["docker", "compose", "--project-directory", str(fixture_dir),
         "-p", "deployment-config-test", "-f", str(compose_path),
         "config", "--format", "json"],
        env=environment, check=True, capture_output=True, text=True,
    )
    return json.loads(result.stdout)["services"]


def verify_variant(source_path, fixture_dir):
    compose_path = fixture_dir / source_path.name
    shutil.copyfile(source_path, compose_path)
    env_path = fixture_dir / ".env"
    for profile in ("omitted", "example", "override"):
        if profile == "example":
            shutil.copyfile(REPO_ROOT / "deploy/.env.example", env_path)
        elif profile == "override":
            env_path.write_text("\n".join(f"{key}={value}" for key, value in
                                          OVERRIDES.items()), encoding="utf-8")
        services = render(compose_path, fixture_dir)
        application = services["omnihive"]
        actual = application["environment"]
        expected = DEFAULTS | (OVERRIDES if profile == "override" else {})
        expected["SERVER_PORT"] = "8080"
        for key, value in expected.items():
            assert actual.get(key) == value, (source_path.name, profile, key,
                                              actual.get(key), value)
        published = "18081" if profile == "override" else "8080"
        assert application["ports"][0]["published"] == published
        assert application["ports"][0]["target"] == 8080
        assert "/health" in " ".join(application["healthcheck"]["test"])
        assert "project-28424c50" not in str(services)
        if "postgres" in services:
            assert actual["DATABASE_HOST"] == "postgres"
            assert actual["DATABASE_PORT"] == "5432"
            assert actual["REDIS_HOST"] == "redis"
            assert actual["REDIS_PORT"] == "6379"
            maxclients = "12345" if profile == "override" else "10000"
            assert f"--maxclients {maxclients}" in " ".join(services["redis"]["command"])
            command = services["redis"]["command"]
            assert command[:3] == ["redis-server", "--maxclients", maxclients]
            assert command[3:-1] == ["--save", "60", "1", "--appendonly",
                                     "yes", "--appendfsync", "everysec", "--requirepass"]
            connections = "300" if profile == "override" else "100"
            assert f"max_connections={connections}" in services["postgres"]["command"]
        else:
            assert actual["DATABASE_HOST"] == CONNECTIONS["DATABASE_HOST"]
            assert actual["REDIS_HOST"] == CONNECTIONS["REDIS_HOST"]
        if "dev" in source_path.name:
            for key in ("BATCH_IMAGE_ENABLED", "BATCH_IMAGE_QUEUE_ENABLED",
                        "BATCH_IMAGE_VERTEX_ENABLED"):
                assert actual[key] == "false"
            assert actual["BATCH_IMAGE_VERTEX_PROJECT_ID"] == ""
            assert actual["BATCH_IMAGE_VERTEX_MANAGED_GCS_BUCKET"] == ""
        print(f"PASS {source_path.name}: {profile}")
    if "redis" in services:
        for password in ("", "space 'double\" dollar$ semicolon; backtick` slash\\"):
            rendered = render(compose_path, fixture_dir, {"REDIS_PASSWORD": password})
            command = rendered["redis"]["command"]
            # Compose escapes dollars in rendered config so it can be loaded again.
            assert command[-1] == password.replace("$", "$$")
            assert command[0] == "redis-server" and len(command) == 12
        print(f"PASS {source_path.name}: Redis empty and special-character argv")
    env_path.unlink()


def main():
    with tempfile.TemporaryDirectory(prefix="deployment-config-test-") as directory:
        for compose_path in sorted((REPO_ROOT / "deploy").glob("docker-compose*.yml")):
            verify_variant(compose_path, Path(directory))
    # Source build entrypoints must carry the same runtime, not just the same tools.
    marker = "FROM ${POSTGRES_IMAGE} AS pg-client"
    canonical = (REPO_ROOT / "Dockerfile").read_text(encoding="utf-8").split(marker)[1]
    alternate = (REPO_ROOT / "deploy/Dockerfile").read_text(encoding="utf-8").split(marker)[1]
    assert canonical == alternate, "production runtime sections drifted"
    print("PASS production runtime parity")


if __name__ == "__main__":
    main()
