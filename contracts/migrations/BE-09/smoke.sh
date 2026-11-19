#!/usr/bin/env bash
set -euo pipefail

: "${SMOKE_BASE_URL:?Set the disposable control server loopback URL}"
: "${SMOKE_ADMIN_EMAIL:?Set the disposable admin email}"
: "${SMOKE_ADMIN_PASSWORD:?Set the disposable admin password}"
: "${SMOKE_BACKUP_ID:?Set an existing completed fixture backup ID}"
export SMOKE_BASE_URL SMOKE_ADMIN_EMAIL SMOKE_ADMIN_PASSWORD SMOKE_BACKUP_ID
exec python3 - <<'PY'
import json, os, time, uuid
from urllib.error import HTTPError
from urllib.parse import urlsplit
from urllib.request import Request, urlopen

base = os.environ["SMOKE_BASE_URL"].rstrip("/")
url = urlsplit(base)
if url.scheme != "http" or url.hostname not in ("localhost", "127.0.0.1") or not url.port or url.path or url.username or url.query or url.fragment:
    raise SystemExit("Use the isolated smoke server's explicit loopback HTTP address and port.")
session = None
def request(method,path,status,body=None,key=None):
    headers = {"Accept":"application/json"}
    if session: headers["Authorization"] = "Bearer " + session
    if key: headers["Idempotency-Key"] = key
    if body is not None: headers["Content-Type"] = "application/json"
    req = Request(base+path,method=method,headers=headers,data=json.dumps(body).encode() if body is not None else None)
    try: result=urlopen(req, timeout=180)
    except HTTPError as err: result=err
    with result:
        data=json.load(result)
        if result.status!=status: raise SystemExit(f"{method} {path} returned {result.status} {data.get('reason','')}")
        if path.startswith("/api/v2/"):
            assert result.headers["Cache-Control"]=="private, no-store" and result.headers["X-Request-ID"]
    return data.get("data")
root="/api/v2/admin/restores"
request("GET",root+"/rs_missing",401)
session=request("POST","/api/v1/auth/login",200,{"email":os.environ["SMOKE_ADMIN_EMAIL"],"password":os.environ["SMOKE_ADMIN_PASSWORD"]})["access_token"]
key=uuid.uuid4().hex
body={"backup_id":os.environ["SMOKE_BACKUP_ID"],"target":"isolated"}
p=request("POST",root+"/previews",201,body,key)
assert p["verified"] and p["id"]==request("POST",root+"/previews",201,body,key)["id"]
request("POST",root+"/previews",409,{**body,"target":"production"},key)
cmd={"preview_id":p["id"],"target":"isolated","rehearsal_id":None}
op=request("POST",root,202,cmd,key)
assert op["id"]==request("POST",root,202,cmd,key)["id"]
deadline=time.monotonic()+600
while op["state"] in ("running","queued") and time.monotonic()<deadline:
    time.sleep(1)
    op=request("GET",root+"/"+op["id"],200)
assert op["state"]=="succeeded" and op["stage"]=="verified", op
assert op["report"]["source_digest"]==p["impact"]["source_digest"]
for table,count in op["report"]["counts"].items(): assert count==p["impact"][table]
lookup=request("GET","/api/v2/workspaces/1/operations/"+op["id"],200)
assert lookup["state"]=="succeeded" and lookup["result"]==op
print("Verified isolated restore",op["id"],"generation",op["generation"])
PY
