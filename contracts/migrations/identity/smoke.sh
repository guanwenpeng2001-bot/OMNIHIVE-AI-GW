#!/usr/bin/env bash
set -euo pipefail

# Run against an isolated server in standard mode with a fresh database, a
# bootstrap admin, no upstream accounts and default ungrouped-key scheduling.
# This provisions a test balance, a routing group and two keys in that database.
: "${SMOKE_BASE_URL:=http://127.0.0.1:18091}"
: "${SMOKE_ADMIN_EMAIL:?Set the isolated server's admin email}"
: "${SMOKE_ADMIN_PASSWORD:?Set the isolated server's admin password}"
export SMOKE_BASE_URL SMOKE_ADMIN_EMAIL SMOKE_ADMIN_PASSWORD

exec python3 - <<'PY'
import json
import os
from urllib.error import HTTPError
from urllib.parse import urlsplit
from urllib.request import Request, urlopen
from uuid import uuid4

base = os.environ["SMOKE_BASE_URL"].rstrip("/")
url = urlsplit(base)
if url.scheme != "http" or url.netloc not in (f"127.0.0.1:{url.port}", f"localhost:{url.port}") or url.path or url.query or url.fragment:
    raise SystemExit("Use the isolated server's explicit loopback HTTP address and port.")


def require(condition, message):
    if not condition:
        raise SystemExit(message)


def request(method, path, expected, *, bearer=None, google_key=None, payload=None, idempotency_key=None, with_status=False):
    headers = {"Accept": "application/json"}
    if bearer:
        headers["Authorization"] = "Bearer " + bearer
    if google_key:
        headers["x-goog-api-key"] = google_key
    if idempotency_key:
        headers["Idempotency-Key"] = idempotency_key
    body = None
    if payload is not None:
        headers["Content-Type"] = "application/json"
        body = json.dumps(payload).encode()
    req = Request(base + path, method=method, headers=headers, data=body)
    try:
        response = urlopen(req, timeout=15)
    except HTTPError as error:
        response = error
    with response:
        content = json.load(response)
        status = response.status
        headers = response.headers
    allowed = expected if isinstance(expected, tuple) else (expected,)
    if status not in allowed:
        code = content.get("reason", content.get("error", {}).get("code"))
        message = content.get("error", {}).get("message", content.get("message"))
        raise SystemExit(f"{method} {path}: expected {expected}, got {status} ({code}, {message})")
    if path.startswith("/api/v2/"):
        require(headers.get("Cache-Control") == "private, no-store", f"{path}: cache policy missing")
        require(headers.get("X-Request-ID"), f"{path}: request ID missing")
    print(f"{method} {path} -> {status}")
    return (status, content) if with_status else content


request("GET", "/api/v2/me/scopes", 401)
login = request("POST", "/api/v1/auth/login", 200, payload={
    "email": os.environ["SMOKE_ADMIN_EMAIL"],
    "password": os.environ["SMOKE_ADMIN_PASSWORD"],
})["data"]
session = login["access_token"]
user_id = login["user"]["id"]

scopes = request("GET", "/api/v2/me/scopes", 200, bearer=session)["data"]["items"]
require(scopes and scopes[0]["projects"], "The bootstrap admin has no visible default project.")
workspace = scopes[0]
project_id = workspace["projects"][0]["id"]
project = f"/api/v2/workspaces/{workspace['workspace_id']}/projects/{project_id}"
access = request("GET", project + "/access", 200, bearer=session)["data"]
require(access["actor_id"] == str(user_id), "Project access resolved to a different actor.")
require(access["project_id"] == project_id, "Project access resolved to a different project.")
require({"keys.create", "text.chat", "usage.read"} <= set(access["actions"]), "Project actions missing.")

run = uuid4().hex
key_input = {
    "name": f"Restricted smoke {run}",
    "actions": ["usage.read", "text.chat"],
    "model_access": {"mode": "inherit", "ids": []},
}
issued = request("POST", project + "/keys", 201, bearer=session, payload=key_input, idempotency_key=run)["data"]
credential = issued["credential"]
require(credential and credential.startswith("oh_p_"), "Project credential missing on first issue.")
replay = request("POST", project + "/keys", 200, bearer=session, payload=key_input, idempotency_key=run)["data"]
require(replay["id"] == issued["id"] and replay["credential"] is None, "Replay returned a new key or repeated the credential.")
print("Project key replay preserved the key ID and returned a null credential.")

compliance = request("GET", "/api/v1/admin/compliance", 200, bearer=session)["data"]
if compliance["required"]:
    request("POST", "/api/v1/admin/compliance/accept", 200, bearer=session,
            payload={"phrase": compliance["ack_phrase_en"], "language": "en"})
request("POST", f"/api/v1/admin/users/{user_id}/balance", 200, bearer=session,
        payload={"balance": 1, "operation": "set", "notes": "Isolated BE-01 smoke"})
# The billing alias is registered before group admission. Whether the restricted
# key already has an eligible group depends on fixture state, so both admitted
# outcomes are valid; an action denial is not.
billing_status, billing = request("GET", "/v1/sub2api/billing", (200, 403), bearer=credential, with_status=True)
if billing_status == 200:
    require(billing.get("object") == "sub2api.key_billing", "Ordinary gateway did not admit the allowed usage.read project action.")
else:
    require(billing["error"]["message"] == "API key is not assigned to a group", "Ordinary gateway did not admit the allowed usage.read project action.")
ordinary = request("POST", "/v1/embeddings", 403, bearer=credential,
                   payload={"model": "text-embedding-3-small", "input": "Smoke"})
require(ordinary["error"]["message"] == "Access denied", "Ordinary gateway failed outside project action enforcement.")
google_payload = {"contents": [{"role": "user", "parts": [{"text": "Smoke"}]}]}
google_model = "/v1beta/models/gemini-2.5-flash:"
google = request("POST", google_model + "countTokens", 403, google_key=credential, payload=google_payload)
require(google["error"]["message"] == "Access denied", "Google gateway failed outside project action enforcement.")
# Admitted text.chat: depending on whether the restricted key already resolved
# an eligible group, the request stops either at group admission (403) or at
# scheduling because this isolated server has no upstream accounts (503).
google_status, google = request("POST", google_model + "generateContent", (403, 503), google_key=credential,
                                payload=google_payload, with_status=True)
if google_status == 403:
    require("not assigned to any group" in google["error"]["message"], "Google gateway did not admit the allowed project action.")
else:
    require("available" in google["error"]["message"].lower(), "Google gateway did not reach scheduling for the allowed project action.")
print("Both gateway adapters recognized the restricted credential and enforced its actions.")

group = request("POST", "/api/v1/admin/groups", 200, bearer=session, payload={
    "name": f"Legacy smoke {run}", "platform": "openai", "rate_multiplier": 1,
})["data"]
legacy = request("POST", "/api/v1/keys", 200, bearer=session, payload={
    "name": f"Legacy smoke {run}", "group_id": group["id"],
})["data"]["secret"]
chat = {"model": "gpt-4o-mini", "messages": [{"role": "user", "content": "Smoke"}]}
request("POST", "/v1/chat/completions", 401, bearer="invalid-be01-smoke-key", payload=chat)
reply = request("POST", "/v1/chat/completions", 503, bearer=legacy, payload=chat)
require("available" in reply["error"]["message"].lower(), "Legacy chat did not reach scheduling.")
print("Legacy chat authenticated and reached scheduling with no upstream accounts.")
PY
