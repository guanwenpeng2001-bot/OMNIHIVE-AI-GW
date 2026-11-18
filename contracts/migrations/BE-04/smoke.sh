#!/usr/bin/env bash
set -euo pipefail

: "${SMOKE_BASE_URL:=http://127.0.0.1:18091}"
: "${SMOKE_ADMIN_EMAIL:?Set the isolated server admin email}"
: "${SMOKE_ADMIN_PASSWORD:?Set the isolated server admin password}"
: "${SMOKE_TLS_CERT:?Set the loopback fixture certificate trusted by the server}"
: "${SMOKE_TLS_KEY:?Set the matching loopback fixture private key}"
export SMOKE_BASE_URL SMOKE_ADMIN_EMAIL SMOKE_ADMIN_PASSWORD SMOKE_TLS_CERT SMOKE_TLS_KEY

exec python3 - <<'PY'
import json
import os
import ssl
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.error import HTTPError
from urllib.parse import urlsplit
from urllib.request import Request, urlopen
from uuid import uuid4

base = os.environ["SMOKE_BASE_URL"].rstrip("/")
target = urlsplit(base)
if target.scheme != "http" or target.hostname not in ("127.0.0.1", "localhost") or not target.port or target.path or target.query or target.fragment or target.username:
    raise SystemExit("Use the isolated loopback server with an explicit port.")
secrets = [uuid4().hex, uuid4().hex]
model = "connection-smoke"

class Upstream(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/v1/models":
            self.send_error(404)
            return
        if self.headers.get("Authorization") not in ["Bearer " + key for key in secrets]:
            self.send_error(401)
            return
        data = json.dumps({"data": [{"id": model}]}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)
    def log_message(self, *_):
        pass

upstream = ThreadingHTTPServer(("127.0.0.1", 0), Upstream)
tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
tls.load_cert_chain(os.environ["SMOKE_TLS_CERT"], os.environ["SMOKE_TLS_KEY"])
upstream.socket = tls.wrap_socket(upstream.socket, server_side=True)
threading.Thread(target=upstream.serve_forever, daemon=True).start()
endpoint = f"https://127.0.0.1:{upstream.server_port}"
session = None

def call(method, path, status, payload=None, version=None, key=None):
    headers = {"Content-Type": "application/json", "Idempotency-Key": key or uuid4().hex}
    if session:
        headers["Authorization"] = "Bearer " + session
    if version is not None:
        headers["If-Match"] = f'"{version}"'
    req = Request(base + path, data=json.dumps(payload).encode() if payload is not None else None, headers=headers, method=method)
    try:
        response = urlopen(req, timeout=35)
    except HTTPError as exc:
        response = exc
    with response:
        raw = response.read()
        data = json.loads(raw)
        if response.status != status:
            raise SystemExit(f"{method} {path}: {response.status}, {data.get('reason')}")
        if path.startswith("/api/v2/"):
            assert response.headers["Cache-Control"] == "private, no-store" and response.headers["X-Request-ID"]
            assert all(k.encode() not in raw for k in secrets)
    print(f"{method} {path} -> {status}")
    return data.get("data")

call("GET", "/api/v2/workspaces/1/provider-schemas", 401)
session = call("POST", "/api/v1/auth/login", 200, {"email": os.environ["SMOKE_ADMIN_EMAIL"], "password": os.environ["SMOKE_ADMIN_PASSWORD"]})["access_token"]
w = "/api/v2/workspaces/" + call("GET", "/api/v2/me/scopes", 200)["items"][0]["workspace_id"]
assert call("GET", w + "/provider-schemas?provider=openai", 200)["items"][0]["provider"] == "openai"
p = call("POST", w + "/pools", 201, {"name": "smoke", "max_concurrency": 2})["id"]
spec = {"provider": "openai", "schema_version": "1", "auth_type": "api_key", "name": "smoke", "endpoint": endpoint}
receipt = uuid4().hex
c = call("POST", w + "/connections", 201, spec, key=receipt)
assert call("POST", w + "/connections", 201, spec, key=receipt)["id"] == c["id"]
path = w + "/connections/" + c["id"]
cred = call("PUT", path + "/credential", 200, {"operation":"set","type":"api_key","secret":secrets[0]}, c["version"])
v = cred["version"]
op = call("POST", path + "/verify", 202, {"credential_version":cred["credential"]["version"],"checks":["network","authorization","discover_models"]}, v)
verified = call("GET", op["status_url"], 200)
assert verified["state"] == "succeeded" and verified["result"]["models"] == [model]
preview = call("POST", path + "/preview", 200, {"pool_ids":[p],"model_ids":[model],"limits":{"concurrency":2}}, v)
assert not preview["blocked"]
activated = call("POST", path + "/activate", 200, {"preview_id":preview["preview_id"],"verification_id":verified["result"]["verification_id"],"expected_credential_version":"1"}, v)
assert activated["state"] == "active"
op = call("POST", path + "/rotations", 202, {"expected_credential_version":"1","replacement":{"type":"api_key","secret":secrets[1]}}, activated["version"])
assert call("GET", op["status_url"], 200)["result"]["phase"] == "revoked"
c = call("GET", path, 200)
assert c["credential"]["version"] == "2"
assert call("PUT", path + "/credential", 200, {"operation":"clear"}, c["version"])["credential"]["state"] == "revoked"
assert call("GET", path + "/events", 200)["items"]
upstream.shutdown()
PY
