#!/usr/bin/env python3
"""Check the host binding policy of Compose templates and their shared env."""

from pathlib import Path
import re

import yaml


REPO_ROOT = Path(__file__).resolve().parents[2]
for name in ("docker-compose.yml", "docker-compose.local.yml", "docker-compose.dev.yml"):
    path = REPO_ROOT / "deploy" / name
    application = yaml.safe_load(path.read_text(encoding="utf-8"))["services"]["omnihive"]
    assert application["ports"] == [
        "${BIND_HOST:-127.0.0.1}:${SERVER_PORT:-8080}:8080"
    ], f"{name}: application port must default to loopback"
    print(f"PASS {name}: loopback host binding")

standalone_path = REPO_ROOT / "deploy/docker-compose.standalone.yml"
standalone = yaml.safe_load(standalone_path.read_text(encoding="utf-8"))["services"]["omnihive"]
assert standalone["ports"] == [
    "${BIND_HOST:-0.0.0.0}:${SERVER_PORT:-8080}:8080"
], "standalone must remain directly usable"
assert "TLS" in standalone_path.read_text(encoding="utf-8"), (
    "standalone must warn operators to provide TLS"
)
print("PASS docker-compose.standalone.yml: direct binding has a TLS warning")

environment = (REPO_ROOT / "deploy/.env.example").read_text(encoding="utf-8")
bind = re.search(r"^BIND_HOST=(.*)$", environment, re.MULTILINE)
assert bind and bind.group(1) == "127.0.0.1", ".env.example must default to loopback"
print("PASS .env.example: loopback host binding")
