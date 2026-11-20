#!/usr/bin/env python3
"""Real QA smoke for the Omnihive refactor deployment.

This script is intentionally dependency-free (Python 3 standard library only).
It drives the live QA instance through the developer/admin journeys and writes
redacted JSON/text evidence. It never prints or persists credentials.

Environment variables:
  QA_BASE             Base URL, e.g. http://localhost:18090
  QA_ADMIN_EMAIL      Admin email (fallback: ADMIN_EMAIL in QA_ENV_FILE)
  QA_ADMIN_PASSWORD   Admin password (fallback: ADMIN_PASSWORD in QA_ENV_FILE)
  QA_MAGPIE_KEY       Optional Magpie gateway key. When present the connection
                      create/credential path is exercised; otherwise an existing
                      connection is reused and "keep" covers the credential step.
  QA_ENV_FILE         Deploy .env to source admin credentials from (no key output).
  QA_WORKSPACE_ID     Optional pin for workspace (auto-detected from /me/scopes).
  QA_GROUP_ID         Optional pin for the API-key group (auto-detected).
  QA_CONNECTION_ID    Optional pin for the connection (auto-detected).

Exit status:
  0  all executed checks passed (skips are allowed and recorded)
  1  one or more checks failed
  2  configuration/runtime error before checks could run
"""

from __future__ import annotations

import json
import os
import re
import sys
import time
import uuid
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


DEFAULT_BASE = "http://localhost:18090"
DEFAULT_ENV_FILE = "/home/administrator/omnihive-refactor-qa-deploy/.env"
DEFAULT_MAGPIE_BASE = "https://host.docker.internal:3443"
DEFAULT_MODELS = {
    "deepseek": "deepseek/deepseek-v4-pro",
    "gpt": "codex/gpt-6.2-astra",
    "kimi": "auto/kimi-k3",
    "gemini": "auto/gemini-3.8-flash",
}
PROMPT = "OMNIHIVE_QA_OK"
MAX_TOKENS = 32


def log(msg: str) -> None:
    print(f"[qa-smoke] {msg}", flush=True)


def now_utc() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def parse_env_file(path: str) -> dict[str, str]:
    out: dict[str, str] = {}
    if not path or not os.path.isfile(path):
        return out
    with open(path, "r", encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            key, _, value = line.partition("=")
            key = key.strip()
            value = value.strip().strip('"').strip("'")
            out[key] = value
    return out


def redact_text(text: str) -> str:
    """Best-effort redaction for any error text we might include."""
    text = re.sub(r"(?i)(bearer\s+)[A-Za-z0-9._\-]+", r"\1***", text)
    text = re.sub(r"(?i)(authorization[\"':= ]+)[A-Za-z0-9._\-]+", r"\1***", text)
    text = re.sub(r"(?i)((?:api[_-]?key|secret|password|token)[\"']?\s*[:=]\s*[\"']?)[^\"'&\s,}]+", r"\1***", text)
    return text


SECRET_KEYS = {
    "access_token",
    "refresh_token",
    "token",
    "secret",
    "api_key",
    "key",
    "credential",
    "password",
    "authorization",
}


def redact(obj: Any) -> Any:
    if isinstance(obj, dict):
        return {k: "***" if k.lower() in SECRET_KEYS else redact(v) for k, v in obj.items()}
    if isinstance(obj, list):
        return [redact(v) for v in obj]
    if isinstance(obj, str):
        return redact_text(obj)
    return obj


class QAError(Exception):
    pass


class Smoke:
    def __init__(self, args: list[str]) -> None:
        self.checks: list[dict[str, Any]] = []
        self.failures = 0
        self.started_at = now_utc()
        self.workspace_id: str | None = None
        self.project_id: str | None = None
        self.group_id: int | None = None
        self.group_name: str | None = None
        self.connection_id: str | None = None
        self.connection_version: str | None = None
        self.connection_credential_version: str | None = None
        self.connection_model_ids: list[str] = []
        self.connection_limits: dict[str, Any] = {}
        self.token: str | None = None
        self.key_id: int | None = None
        self.key_secret: str | None = None

        env_file = os.environ.get("QA_ENV_FILE", DEFAULT_ENV_FILE)
        deploy_env = parse_env_file(env_file)
        self.base = os.environ.get("QA_BASE", DEFAULT_BASE).rstrip("/")
        self.admin_email = os.environ.get("QA_ADMIN_EMAIL") or deploy_env.get("ADMIN_EMAIL", "")
        self.admin_password = os.environ.get("QA_ADMIN_PASSWORD") or deploy_env.get("ADMIN_PASSWORD", "")
        self.magpie_key = os.environ.get("QA_MAGPIE_KEY", "")
        self.magpie_base = os.environ.get("QA_MAGPIE_BASE", DEFAULT_MAGPIE_BASE)
        self.pinned_workspace = os.environ.get("QA_WORKSPACE_ID", "")
        self.pinned_project = os.environ.get("QA_PROJECT_ID", "")
        self.pinned_group = os.environ.get("QA_GROUP_ID", "")
        self.pinned_connection = os.environ.get("QA_CONNECTION_ID", "")
        self.timeout = int(os.environ.get("QA_HTTP_TIMEOUT", "30"))
        self.chat_timeout = int(os.environ.get("QA_CHAT_TIMEOUT", "150"))
        self.evidence_dir = os.environ.get("QA_EVIDENCE_DIR", "/tmp/omnihive-qa-smoke")

        self.expected_models = {
            "deepseek": os.environ.get("QA_DEEPSEEK_MODEL", DEFAULT_MODELS["deepseek"]),
            "gpt": os.environ.get("QA_GPT_MODEL", DEFAULT_MODELS["gpt"]),
            "kimi": os.environ.get("QA_KIMI_MODEL", DEFAULT_MODELS["kimi"]),
            "gemini": os.environ.get("QA_GEMINI_MODEL", DEFAULT_MODELS["gemini"]),
        }

    def add_check(
        self,
        name: str,
        status: str,
        summary: str,
        details: dict[str, Any] | None = None,
        reason: str | None = None,
    ) -> dict[str, Any]:
        entry: dict[str, Any] = {
            "check": name,
            "status": status,
            "summary": summary,
            "at": now_utc(),
        }
        if details:
            entry["details"] = redact(details)
        if reason:
            entry["reason"] = reason
        self.checks.append(entry)
        if status == "FAIL":
            self.failures += 1
        return entry

    def request(
        self,
        method: str,
        path: str,
        token: str | None = None,
        body: Any = None,
        headers: dict[str, str] | None = None,
        timeout: int | None = None,
    ) -> tuple[int, Any]:
        url = self.base + path
        data = None
        hdrs = dict(headers or {})
        if token is not None:
            hdrs.setdefault("Authorization", "Bearer " + token)
        if body is not None:
            data = json.dumps(body).encode("utf-8")
            hdrs.setdefault("Content-Type", "application/json")
        req = Request(url, data=data, headers=hdrs, method=method)
        try:
            with urlopen(req, timeout=(timeout or self.timeout)) as resp:
                raw = resp.read()
                status = resp.status
        except HTTPError as exc:
            raw = exc.read()
            status = exc.code
        except URLError as exc:
            raise QAError(f"cannot reach {url}: {exc}") from exc

        text = raw.decode("utf-8", "replace")
        try:
            parsed = json.loads(text) if text else None
        except json.JSONDecodeError:
            parsed = text
        return status, parsed

    def api(self, method: str, path: str, **kwargs: Any) -> tuple[int, Any]:
        return self.request(method, path, token=self.token, **kwargs)

    def run(self) -> int:
        try:
            self.record_config()
            self.login()
            self.compliance()
            self.scopes()
            self.connection_workflow()
            self.groups_channels_plaza()
            self.v1_key_and_models()
            self.authoritative_quote()
            self.members_and_extensions()
            self.chat_requests()
            self.request_logs()
            self.billing()
            self.tasks_empty()
            self.check_updates()
            self.cleanup_key()
        except QAError as exc:
            log(f"FATAL: {exc}")
            self.add_check("fatal", "FAIL", str(exc), reason=str(exc))
            self.failures += 1
        return self.finish()

    def record_config(self) -> None:
        self.add_check(
            "config",
            "PASS" if self.admin_email and self.admin_password else "FAIL",
            "configuration loaded and redacted",
            {
                "base": self.base,
                "env_file_used": os.environ.get("QA_ENV_FILE", DEFAULT_ENV_FILE),
                "admin_email_loaded": bool(self.admin_email),
                "admin_password_loaded": bool(self.admin_password),
                "magpie_key_loaded": bool(self.magpie_key),
                "workspace_pin": self.pinned_workspace or None,
                "project_pin": self.pinned_project or None,
                "group_pin": self.pinned_group or None,
                "connection_pin": self.pinned_connection or None,
            },
        )

    def login(self) -> None:
        status, payload = self.request(
            "POST",
            "/api/v1/auth/login",
            body={"email": self.admin_email, "password": self.admin_password},
        )
        if status != 200 or not isinstance(payload, dict):
            self.add_check("login", "FAIL", "login failed", {"http_status": status}, reason=str(redact(payload)))
            raise QAError("login failed; cannot continue")
        data = payload.get("data") or {}
        self.token = data.get("access_token")
        if not self.token:
            self.add_check("login", "FAIL", "login response missing access_token")
            raise QAError("login response missing access_token")
        user = data.get("user") or {}
        self.add_check(
            "login",
            "PASS",
            "admin login succeeded",
            {
                "http_status": status,
                "user_id": user.get("id"),
                "role": user.get("role"),
                "expires_in": data.get("expires_in"),
            },
        )

    def compliance(self) -> None:
        status, payload = self.api("GET", "/api/v1/admin/compliance")
        if status != 200 or not isinstance(payload, dict):
            self.add_check("compliance", "FAIL", "compliance status unavailable", {"http_status": status}, reason=str(redact(payload)))
            return
        data = payload.get("data") or {}
        required = bool(data.get("required"))
        acknowledged = bool(data.get("acknowledgement"))
        if not required or acknowledged:
            self.add_check(
                "compliance",
                "SKIP",
                "compliance already confirmed; accept step skipped",
                {"required": required, "acknowledged": acknowledged, "version": data.get("version")},
            )
            return
        self.add_check(
            "compliance",
            "FAIL",
            "compliance is required but not confirmed; refusing to auto-accept",
            {"required": required, "acknowledged": acknowledged},
        )

    def scopes(self) -> None:
        status, payload = self.api("GET", "/api/v2/me/scopes")
        if status != 200 or not isinstance(payload, dict):
            self.add_check("scopes", "FAIL", "scopes unavailable", {"http_status": status}, reason=str(redact(payload)))
            raise QAError("scopes failed")
        data = payload.get("data") or {}
        items = data.get("items") or []
        if not items:
            self.add_check("scopes", "FAIL", "no workspaces returned")
            raise QAError("no workspace/project scope")
        first = items[0]
        projects = first.get("projects") or []
        if self.pinned_workspace:
            self.workspace_id = self.pinned_workspace
        else:
            self.workspace_id = str(first.get("workspace_id"))
        if self.pinned_project:
            self.project_id = self.pinned_project
        elif projects:
            self.project_id = str(projects[0].get("id"))
        else:
            self.project_id = None
        self.add_check(
            "scopes",
            "PASS",
            "workspace/project scope resolved",
            {
                "workspace_count": len(items),
                "workspace_id": self.workspace_id,
                "project_id": self.project_id,
                "projects_in_first_workspace": [p.get("id") for p in projects],
            },
        )

    def connection_workflow(self) -> None:
        if not self.workspace_id:
            self.add_check("connection", "FAIL", "workspace scope missing")
            raise QAError("workspace scope missing")
        prefix = f"/api/v2/workspaces/{self.workspace_id}"
        connection = self.find_connection(prefix)
        if connection is None:
            if not self.magpie_key:
                self.add_check("connection_create", "FAIL", "no reusable connection and QA_MAGPIE_KEY is not set", reason="QA_MAGPIE_KEY is required to create a new connection")
                return
            connection = self.create_connection(prefix)

        self.connection_id = str(connection.get("id"))
        self.connection_version = str(connection.get("version"))
        cred = connection.get("credential") or {}
        self.connection_credential_version = str(cred.get("version"))
        existing_models = list(connection.get("model_ids") or [])
        for m in self.expected_model_values():
            if m not in existing_models:
                existing_models.append(m)
        self.connection_model_ids = existing_models
        limits = connection.get("limits") or {}
        self.connection_limits = {"concurrency": limits.get("concurrency", 4)}

        base = f"{prefix}/connections/{self.connection_id}"
        version = self.connection_version
        cred_version = self.connection_credential_version or "1"

        if self.magpie_key and connection.get("credential", {}).get("state") not in ("verified",):
            self.set_credential(base, version, cred_version)
        else:
            self.keep_credential(base, version)

        verification_id = self.verify_connection(base, version, cred_version)
        preview = self.preview_connection(base, version)
        self.activate_connection(base, version, cred_version, verification_id, preview)

    def find_connection(self, prefix: str) -> dict[str, Any] | None:
        if self.pinned_connection:
            status, payload = self.api("GET", f"{prefix}/connections/{self.pinned_connection}")
            if status == 200 and isinstance(payload, dict):
                data = payload.get("data")
                if isinstance(data, dict):
                    self.add_check("connection_reuse", "PASS", "pinned connection reused", {"id": data.get("id"), "name": data.get("name"), "state": data.get("state")})
                    return data
            self.add_check("connection_reuse", "FAIL", "pinned connection not found", {"http_status": status})
            return None

        status, payload = self.api("GET", f"{prefix}/connections?limit=100")
        if status != 200 or not isinstance(payload, dict):
            self.add_check("connection_reuse", "FAIL", "connection list unavailable", {"http_status": status}, reason=str(redact(payload)))
            raise QAError("connection list failed")
        items = (payload.get("data") or {}).get("items") or []
        candidates = [c for c in items if isinstance(c, dict)]
        preferred = [
            c for c in candidates
            if c.get("provider") == "openai"
            and c.get("auth_type") == "api_key"
            and (c.get("credential") or {}).get("state") in ("verified", "unverified")
            and c.get("state") == "active"
        ]
        chosen = (preferred or candidates)[0] if (preferred or candidates) else None
        if chosen is None:
            return None
        self.add_check(
            "connection_reuse",
            "PASS",
            "existing connection reused",
            {"id": chosen.get("id"), "name": chosen.get("name"), "state": chosen.get("state"), "provider": chosen.get("provider")},
        )
        return chosen

    def create_connection(self, prefix: str) -> dict[str, Any]:
        name = f"qa-smoke-{uuid.uuid4().hex[:8]}"
        status, payload = self.api(
            "POST",
            f"{prefix}/connections",
            body={
                "provider": "openai",
                "schema_version": "1",
                "auth_type": "api_key",
                "name": name,
                "endpoint": self.magpie_base,
            },
            headers={"Idempotency-Key": uuid.uuid4().hex},
        )
        data = payload.get("data") if isinstance(payload, dict) else None
        if status != 201 or not isinstance(data, dict):
            self.add_check("connection_create", "FAIL", "connection create failed", {"http_status": status}, reason=str(redact(payload)))
            raise QAError("connection create failed")
        self.add_check(
            "connection_create",
            "PASS",
            "connection created with unique name",
            {"id": data.get("id"), "name": name, "endpoint": self.magpie_base, "state": data.get("state")},
        )
        return data

    def keep_credential(self, base: str, version: str) -> None:
        status, payload = self.api(
            "PUT",
            f"{base}/credential",
            body={"operation": "keep"},
            headers={"If-Match": f'"{version}"'},
        )
        data = payload.get("data") if isinstance(payload, dict) else None
        if status != 200 or not isinstance(data, dict):
            self.add_check("connection_credential", "FAIL", "credential keep failed", {"http_status": status}, reason=str(redact(payload)))
            return
        cred = data.get("credential") or {}
        self.connection_credential_version = str(cred.get("version"))
        self.add_check(
            "connection_credential",
            "PASS",
            "credential keep succeeded (existing credential reused)",
            {"connection_id": data.get("connection_id"), "credential_state": cred.get("state"), "credential_version": cred.get("version"), "connection_version": data.get("version")},
        )

    def set_credential(self, base: str, version: str, cred_version: str) -> None:
        status, payload = self.api(
            "PUT",
            f"{base}/credential",
            body={"operation": "set", "type": "api_key", "secret": self.magpie_key},
            headers={"If-Match": f'"{version}"'},
        )
        data = payload.get("data") if isinstance(payload, dict) else None
        if status != 200 or not isinstance(data, dict):
            self.add_check("connection_credential", "FAIL", "credential set failed", {"http_status": status}, reason=str(redact(payload)))
            raise QAError("credential set failed")
        cred = data.get("credential") or {}
        self.connection_credential_version = str(cred.get("version"))
        self.add_check(
            "connection_credential",
            "PASS",
            "credential set succeeded",
            {"connection_id": data.get("connection_id"), "credential_state": cred.get("state"), "credential_version": cred.get("version"), "connection_version": data.get("version")},
        )

    def verify_connection(self, base: str, version: str, cred_version: str) -> str:
        status, payload = self.api(
            "POST",
            f"{base}/verify",
            body={"checks": ["network", "authorization", "discover_models"], "credential_version": cred_version},
            headers={"If-Match": f'"{version}"', "Idempotency-Key": uuid.uuid4().hex},
        )
        data = payload.get("data") if isinstance(payload, dict) else None
        if status != 202 or not isinstance(data, dict):
            self.add_check("connection_verify", "FAIL", "verify submission failed", {"http_status": status}, reason=str(redact(payload)))
            raise QAError("connection verify failed")
        op = self.poll_operation(data)
        result = op.get("result") or {}
        verification_id = str(result.get("verification_id") or "")
        checks = result.get("checks") or []
        ok = op.get("state") == "succeeded" and verification_id and all((c or {}).get("state") == "passed" for c in checks)
        if not ok:
            self.add_check("connection_verify", "FAIL", "connection verification failed", {"operation_state": op.get("state"), "checks": checks}, reason=str(redact(result)))
            raise QAError("connection verification failed")
        self.add_check(
            "connection_verify",
            "PASS",
            "connection verified against real upstream",
            {"verification_id": verification_id, "checks": checks, "discovered_models": len(result.get("models") or [])},
        )
        return verification_id

    def poll_operation(self, submit: dict[str, Any]) -> dict[str, Any]:
        status_url = submit.get("status_url")
        if not status_url:
            return submit
        deadline = time.time() + 60
        last = submit
        while time.time() < deadline:
            state = last.get("state")
            if state in ("succeeded", "failed", "unknown"):
                return last
            time.sleep(1)
            status, payload = self.api("GET", status_url)
            last = (payload.get("data") if isinstance(payload, dict) else None) or {}
        return last

    def preview_connection(self, base: str, version: str) -> dict[str, Any]:
        status, payload = self.api(
            "POST",
            f"{base}/preview",
            body={"model_ids": self.connection_model_ids, "pool_ids": [], "limits": self.connection_limits},
            headers={"If-Match": f'"{version}"', "Idempotency-Key": uuid.uuid4().hex},
        )
        data = payload.get("data") if isinstance(payload, dict) else None
        blocked = (data or {}).get("blocked") if isinstance(data, dict) else None
        preview_id = str((data or {}).get("preview_id") or "")
        if status != 200 or not isinstance(data, dict) or not preview_id or blocked:
            self.add_check("connection_preview", "FAIL", "connection preview failed", {"http_status": status, "blocked": blocked}, reason=str(redact(payload)))
            raise QAError("connection preview failed")
        self.add_check(
            "connection_preview",
            "PASS",
            "connection preview applied",
            {"preview_id": preview_id, "adds": data.get("adds"), "removes": data.get("removes"), "blocked": blocked},
        )
        return data

    def activate_connection(self, base: str, version: str, cred_version: str, verification_id: str, preview: dict[str, Any]) -> None:
        preview_id = str(preview.get("preview_id") or "")
        status, payload = self.api(
            "POST",
            f"{base}/activate",
            body={"preview_id": preview_id, "verification_id": verification_id, "expected_credential_version": cred_version},
            headers={"If-Match": f'"{version}"', "Idempotency-Key": uuid.uuid4().hex},
        )
        data = payload.get("data") if isinstance(payload, dict) else None
        if status != 200 or not isinstance(data, dict) or data.get("state") != "active":
            self.add_check("connection_activate", "FAIL", "connection activation failed", {"http_status": status}, reason=str(redact(payload)))
            raise QAError("connection activation failed")
        self.add_check(
            "connection_activate",
            "PASS",
            "connection activated",
            {"id": data.get("id"), "state": data.get("state"), "version": data.get("version"), "model_ids": data.get("model_ids")},
        )

    def groups_channels_plaza(self) -> None:
        group = self.resolve_group()
        self.group_id = int(group.get("id"))
        self.group_name = str(group.get("name"))
        self.add_check(
            "group",
            "PASS",
            "group resolved for v1 key binding",
            {
                "id": self.group_id,
                "name": self.group_name,
                "platform": group.get("platform"),
                "account_count": group.get("account_count"),
                "active_account_count": group.get("active_account_count"),
            },
        )

        channel = self.resolve_channel()
        confirmation = self.channel_confirmation(channel)
        confirmed_models = sorted(confirmation.get("confirmed_models") or [])
        expected = sorted(self.expected_model_values())
        has_confirmation = bool(confirmed_models) and all(m in confirmed_models for m in expected)
        if channel and has_confirmation:
            self.add_check(
                "channel",
                "PASS",
                "channel resolved and model plaza confirmation found",
                {"id": channel.get("id"), "name": channel.get("name"), "status": channel.get("status"), "group_ids": channel.get("group_ids"), "confirmed_models": confirmed_models},
            )
        else:
            self.add_check(
                "channel",
                "FAIL",
                "channel/plaza confirmation incomplete",
                {"channel_id": channel.get("id") if channel else None, "confirmed_models": confirmed_models, "expected_models": expected},
            )

        plaza = self.plaza_models()
        missing = [m for m in expected if m not in plaza]
        if not missing:
            self.add_check("plaza", "PASS", "public model plaza lists all expected models", {"models": expected, "listed_count": len(plaza)})
        else:
            self.add_check("plaza", "FAIL", "public model plaza is missing models", {"missing": missing, "listed": sorted(plaza)})

    def resolve_group(self) -> dict[str, Any]:
        if self.pinned_group:
            status, payload = self.api("GET", "/api/v1/admin/groups")
            items = ((payload.get("data") or {}).get("items") or []) if isinstance(payload, dict) else []
            for item in items:
                if str(item.get("id")) == str(self.pinned_group):
                    return item
            raise QAError(f"pinned group {self.pinned_group} not found")
        status, payload = self.api("GET", "/api/v1/admin/groups")
        if status != 200 or not isinstance(payload, dict):
            self.add_check("group", "FAIL", "group list unavailable", {"http_status": status}, reason=str(redact(payload)))
            raise QAError("group list failed")
        items = (payload.get("data") or {}).get("items") or []
        by_name = [g for g in items if "magpie" in str(g.get("name") or "").lower()]
        non_default = [g for g in items if str(g.get("id")) != "1"]
        openai_groups = [g for g in non_default if g.get("platform") == "openai"]
        chosen = (by_name or openai_groups or non_default or items)[0]
        return chosen

    def resolve_channel(self) -> dict[str, Any] | None:
        status, payload = self.api("GET", "/api/v1/admin/channels")
        if status != 200 or not isinstance(payload, dict):
            return None
        items = (payload.get("data") or {}).get("items") or []
        gid = str(self.group_id)
        for item in items:
            group_ids = [str(g) for g in (item.get("group_ids") or [])]
            if gid in group_ids:
                return item
        return items[0] if items else None

    def channel_confirmation(self, channel: dict[str, Any] | None) -> dict[str, Any]:
        if not channel:
            return {"confirmed_models": []}
        features = channel.get("features_config") or {}
        confirmation = features.get("model_plaza_confirmation") or {}
        groups = confirmation.get("groups") or {}
        entries = groups.get(str(self.group_id)) or []
        models = sorted({str(e.get("name")) for e in entries if isinstance(e, dict) and e.get("name")})
        return {"confirmed_models": models}

    def plaza_models(self) -> set[str]:
        status, payload = self.request("GET", "/api/v2/public/models?limit=100")
        if status != 200 or not isinstance(payload, dict):
            return set()
        items = (payload.get("data") or {}).get("items") or []
        return {str(i.get("model")) for i in items if isinstance(i, dict) and i.get("model")}

    def expected_model_values(self) -> list[str]:
        return [
            self.expected_models["deepseek"],
            self.expected_models["gpt"],
            self.expected_models["kimi"],
            self.expected_models["gemini"],
        ]

    def v1_key_and_models(self) -> None:
        if not self.group_id:
            self.add_check("v1_key_create", "FAIL", "group missing")
            raise QAError("group missing")
        name = f"qa-smoke-key-{uuid.uuid4().hex[:8]}"
        status, payload = self.api("POST", "/api/v1/keys", body={"name": name, "group_id": self.group_id})
        data = payload.get("data") if isinstance(payload, dict) else None
        if status != 200 or not isinstance(data, dict):
            self.add_check("v1_key_create", "FAIL", "v1 key creation failed", {"http_status": status}, reason=str(redact(payload)))
            raise QAError("v1 key creation failed")
        self.key_id = int(data.get("api_key", {}).get("id"))
        self.key_secret = str(data.get("secret") or "")
        if not self.key_id or not self.key_secret:
            self.add_check("v1_key_create", "FAIL", "v1 key response missing id or secret")
            raise QAError("v1 key response missing id/secret")
        self.add_check(
            "v1_key_create",
            "PASS",
            "v1 key created with unique name",
            {"id": self.key_id, "name": name, "group_id": self.group_id, "secret_length": len(self.key_secret), "secret_redacted": True},
        )

        status, payload = self.request("GET", "/v1/models", token=self.key_secret)
        models = payload.get("data") if isinstance(payload, dict) else None
        listed = {str(m.get("id")) for m in models} if isinstance(models, list) else set()
        missing = [m for m in self.expected_model_values() if m not in listed]
        if status == 200 and isinstance(models, list) and not missing:
            self.add_check("v1_models", "PASS", "/v1/models lists expected models", {"listed_count": len(listed), "models": self.expected_model_values()})
        else:
            self.add_check("v1_models", "FAIL", "/v1/models did not list expected models", {"http_status": status, "missing": missing, "listed": sorted(listed)}, reason=str(redact(payload)))

    def authoritative_quote(self) -> None:
        if not self.workspace_id or not self.project_id:
            self.add_check("quote", "FAIL", "workspace/project scope missing")
            return
        status, payload = self.api(
            "GET",
            f"/api/v2/workspaces/{self.workspace_id}/projects/{self.project_id}/models?limit=50",
        )
        items = ((payload.get("data") or {}).get("items") or []) if isinstance(payload, dict) else []
        target = next((m for m in items if isinstance(m, dict) and m.get("model") == self.expected_models["gpt"]), None)
        if status != 200 or not target:
            self.add_check("quote", "FAIL", "scoped catalog target model missing", {"http_status": status})
            return
        q_status, q_payload = self.api(
            "POST",
            f"/api/v2/workspaces/{self.workspace_id}/projects/{self.project_id}/quotes",
            body={
                "model_id": target["id"],
                "api_key_id": str(self.key_id) if self.key_id else None,
                "protocol": "openai_chat" if "openai_chat" in (target.get("protocols") or []) else (target.get("protocols") or ["openai_chat"])[0],
                "action": "text.chat" if "text.chat" in (target.get("actions") or []) else (target.get("actions") or ["text.chat"])[0],
                "scenario": {"input_tokens": "1000", "output_tokens": "500"},
            },
        )
        q_data = q_payload.get("data") if isinstance(q_payload, dict) else None
        if q_status == 200 and isinstance(q_data, dict) and q_data.get("state") in ("priced", "estimated"):
            self.add_check(
                "quote",
                "PASS",
                "authoritative server-side quote succeeded",
                {"model_id": target["id"], "state": q_data.get("state"), "charge": q_data.get("charge")},
            )
        else:
            self.add_check("quote", "FAIL", "authoritative quote failed", {"http_status": q_status}, reason=str(redact(q_payload)))

    def members_and_extensions(self) -> None:
        m_status, m_payload = self.api("GET", f"/api/v2/workspaces/{self.workspace_id}/members?limit=20")
        e_status, e_payload = self.api("GET", "/api/v2/admin/extensions")
        if m_status == 200 and e_status == 200:
            self.add_check(
                "members_extensions",
                "PASS",
                "workspace members and admin extensions returned 200",
                {
                    "members_count": len(((m_payload.get("data") or {}).get("items") or [])) if isinstance(m_payload, dict) else 0,
                    "extensions_status": e_status,
                },
            )
        else:
            self.add_check(
                "members_extensions",
                "FAIL",
                "workspace members or admin extensions failed",
                {"members_status": m_status, "extensions_status": e_status},
            )

    def chat_requests(self) -> None:
        if not self.key_secret:
            self.add_check("chat", "FAIL", "no v1 key available for chat requests")
            raise QAError("no v1 key")
        for provider in ("deepseek", "gpt", "kimi", "gemini"):
            self.chat_one(provider, self.expected_models[provider])

    def chat_one(self, provider: str, model: str) -> None:
        status, payload = self.request(
            "POST",
            "/v1/chat/completions",
            token=self.key_secret,
            body={
                "model": model,
                "messages": [{"role": "user", "content": PROMPT}],
                "stream": False,
                "max_tokens": MAX_TOKENS,
            },
            timeout=self.chat_timeout,
        )
        detail: dict[str, Any] = {"model": model, "http_status": status}
        if status == 200 and isinstance(payload, dict) and payload.get("object") == "chat.completion":
            usage = payload.get("usage") or {}
            choices = payload.get("choices") or [{}]
            first = choices[0] if choices else {}
            detail.update(
                {
                    "response_model": payload.get("model"),
                    "finish_reason": first.get("finish_reason"),
                    "prompt_tokens": usage.get("prompt_tokens"),
                    "completion_tokens": usage.get("completion_tokens"),
                    "has_content": bool((first.get("message") or {}).get("content")),
                }
            )
            self.add_check(f"chat_{provider}", "PASS", f"{provider} non-streaming request succeeded", detail)
            return
        err = payload.get("error") if isinstance(payload, dict) else None
        reason = (err or {}).get("message") or str(redact(payload))
        code = (err or {}).get("code") if isinstance(err, dict) else None
        detail["error_code"] = code
        self.add_check(f"chat_{provider}", "FAIL", f"{provider} non-streaming request failed", detail, reason=reason)

    def request_logs(self) -> None:
        if not self.key_id:
            self.add_check("request_logs", "FAIL", "no key id recorded for log lookup")
            return
        deadline = time.time() + 15
        matched: list[dict[str, Any]] = []
        status = 0
        payload: Any = None
        while time.time() < deadline:
            status, payload = self.api("GET", "/api/v1/usage?page=1&page_size=100")
            items = ((payload.get("data") or {}).get("items") or []) if isinstance(payload, dict) else []
            matched = [i for i in items if isinstance(i, dict) and i.get("api_key_id") == self.key_id]
            if matched:
                break
            time.sleep(1)
        if status != 200:
            self.add_check("request_logs", "FAIL", "request log list unavailable", {"http_status": status}, reason=str(redact(payload)))
            return
        deepseek_hit = [i for i in matched if i.get("model") == self.expected_models["deepseek"]]
        if matched and deepseek_hit:
            self.add_check(
                "request_logs",
                "PASS",
                "request logs include real v1 requests",
                {
                    "matched_count": len(matched),
                    "models": sorted({str(i.get("model")) for i in matched}),
                    "deepseek_logged": bool(deepseek_hit),
                    "settlement_states": sorted({str((i.get("billing_explanation") or {}).get("settlement_state")) for i in matched if isinstance(i.get("billing_explanation"), dict)}),
                },
            )
        else:
            self.add_check(
                "request_logs",
                "FAIL",
                "request logs do not contain the expected DeepSeek request",
                {"http_status": status, "matched_count": len(matched), "models": sorted({str(i.get("model")) for i in matched})},
            )

    def billing(self) -> None:
        if not self.workspace_id or not self.project_id:
            self.add_check("billing", "FAIL", "workspace/project scope missing")
            return
        start = (datetime.now(timezone.utc) - timedelta(hours=2)).strftime("%Y-%m-%dT%H:%M:%SZ")
        end = (datetime.now(timezone.utc) + timedelta(hours=1)).strftime("%Y-%m-%dT%H:%M:%SZ")
        path = (
            f"/api/v2/workspaces/{self.workspace_id}/projects/{self.project_id}/billing/activity"
            f"?from={start}&to={end}&timezone=UTC&currency=USD&limit=10"
        )
        status, payload = self.api("GET", path)
        data = payload.get("data") if isinstance(payload, dict) else None
        if status == 200 and isinstance(data, dict):
            totals = data.get("totals") or {}
            self.add_check(
                "billing",
                "PASS",
                "billing activity returned a valid totals projection",
                {
                    "http_status": status,
                    "logical_requests": totals.get("logical_requests"),
                    "platform_charge": totals.get("platform_charge"),
                    "data_completeness": data.get("data_completeness"),
                    "item_count": len(data.get("items") or []),
                },
            )
        else:
            self.add_check("billing", "FAIL", "billing activity failed", {"http_status": status}, reason=str(redact(payload)))

    def tasks_empty(self) -> None:
        if not self.workspace_id or not self.project_id:
            self.add_check("tasks", "FAIL", "workspace/project scope missing")
            return
        path = f"/api/v2/workspaces/{self.workspace_id}/projects/{self.project_id}/tasks?limit=20"
        status, payload = self.api("GET", path)
        data = payload.get("data") if isinstance(payload, dict) else None
        if status == 200 and isinstance(data, dict):
            items = data.get("items") or []
            self.add_check(
                "tasks_empty",
                "PASS",
                "task center empty state returned",
                {"http_status": status, "item_count": len(items), "is_empty": len(items) == 0},
            )
        else:
            self.add_check("tasks_empty", "FAIL", "task list failed", {"http_status": status}, reason=str(redact(payload)))

    def check_updates(self) -> None:
        status, payload = self.api("GET", "/api/v1/admin/system/check-updates")
        data = payload.get("data") if isinstance(payload, dict) else None
        if status == 200 and isinstance(data, dict):
            self.add_check(
                "check_updates",
                "PASS",
                "version check-updates returned",
                {
                    "current_version": data.get("current_version"),
                    "latest_version": data.get("latest_version"),
                    "has_update": data.get("has_update"),
                    "build_type": data.get("build_type"),
                    "cached": data.get("cached"),
                },
            )
        else:
            self.add_check("check_updates", "FAIL", "check-updates failed", {"http_status": status}, reason=str(redact(payload)))

    def cleanup_key(self) -> None:
        if not self.key_id:
            return
        status, payload = self.api("DELETE", f"/api/v1/keys/{self.key_id}")
        if status == 200:
            self.add_check("cleanup_key", "PASS", "created v1 key cleaned up", {"key_id": self.key_id})
        else:
            self.add_check("cleanup_key", "SKIP", "created key cleanup deferred", {"key_id": self.key_id, "http_status": status}, reason=str(redact(payload)))

    def finish(self) -> int:
        results = {
            "summary": {
                "status": "FAIL" if self.failures else "PASS",
                "executed": len(self.checks),
                "failed": self.failures,
                "skipped": sum(1 for c in self.checks if c.get("status") == "SKIP"),
                "started_at": self.started_at,
                "finished_at": now_utc(),
                "base": self.base,
            },
            "checks": self.checks,
        }
        evidence_dir = Path(self.evidence_dir)
        evidence_dir.mkdir(parents=True, exist_ok=True)
        stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
        json_path = evidence_dir / f"qa_real_smoke-{stamp}.json"
        txt_path = evidence_dir / f"qa_real_smoke-{stamp}.txt"
        json_path.write_text(json.dumps(results, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
        lines = [f"{c['status']:4} {c['check']}: {c['summary']}" for c in self.checks]
        if results["summary"]["status"] == "FAIL":
            lines.extend(["", "FAILED CHECKS:"])
            lines.extend(f"- {c['check']}: {c.get('reason') or c.get('summary')}" for c in self.checks if c["status"] == "FAIL")
        txt_path.write_text("\n".join(lines) + "\n", encoding="utf-8")
        log(f"summary: {results['summary']}")
        log(f"evidence JSON: {json_path}")
        log(f"evidence text: {txt_path}")
        return 1 if self.failures else 0


def main(argv: list[str]) -> int:
    smoke = Smoke(argv)
    return smoke.run()


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
