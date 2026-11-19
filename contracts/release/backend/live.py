"""Loopback candidate and deterministic OpenAI fixture. Raw replies stay in memory."""
import collections
import concurrent.futures
import contextlib
import datetime as dt
import hashlib
import hmac
import http.client
import json
import math
import os
from pathlib import Path
import re
import secrets
import ssl
import subprocess as sp
import threading
import time
from uuid import UUID
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlencode

MODEL = "fixture/text"
MODEL_ID = "m_"+hashlib.sha256(json.dumps(["openai",MODEL],separators=(",",":")).encode()).hexdigest()[:32]
HERE = Path(__file__).resolve().parent
WORK = Path(os.environ["BE24_SCRATCH"])
FIXTURE = json.loads((HERE / "journey.json").read_text())
DAY = dt.datetime.now(dt.timezone.utc).replace(hour=0, minute=0, second=0, microsecond=0)
WINDOW = dict(timezone="UTC", currency="USD", **{
    "from": DAY.strftime("%Y-%m-%dT%H:%M:%SZ"),
    "to": (DAY + dt.timedelta(days=1)).strftime("%Y-%m-%dT%H:%M:%SZ")})


def sql(query, db="candidate"):
    return sp.check_output(["psql", "-XqAt", "-v", "ON_ERROR_STOP=1", "-h", "127.0.0.1",
                            "-U", "postgres", "-d", db], input=query, text=True).strip()


def call(method, path, token=None, body=None, headers=None, port=18080, cancel=False, response_headers=None):
    h = {"Content-Type": "application/json", **(headers or {})}
    if token:
        h["Authorization"] = "Bearer " + token
    conn = http.client.HTTPConnection("127.0.0.1", port, timeout=12)
    try:
        conn.request(method, path, json.dumps(body).encode() if body is not None else None, h)
        r = conn.getresponse()
        if response_headers is not None:
            response_headers.update(r.getheaders())
        if cancel:
            # Close after the first upstream event, without buffering the whole stream.
            return r.status, {"opened": r.read(1) == b"d"}
        raw = r.read()
        try:
            obj = json.loads(raw)
        except (ValueError, UnicodeError):
            obj = {"stream_done": raw.count(b"[DONE]") == 1}
        return r.status, obj
    finally:
        conn.close()


class Prerequisite(Exception):
    pass


def need(method, path, expected, *args, **kwargs):
    status, body = call(method, path, *args, **kwargs)
    if status != expected:
        raise Prerequisite(f"{method} {path.split('?')[0]} returned HTTP {status}; expected {expected}")
    return body["data"]


def error_code(body):
    # Keep machine codes, never upstream free text or user input.
    error = body.get("error")
    details = error if isinstance(error, dict) else {}
    for value in (body.get("reason"), details.get("code"), details.get("type"), details.get("status")):
        if isinstance(value, str) and re.fullmatch(r"[a-zA-Z][a-zA-Z0-9_]{0,79}", value):
            return value
    return None


class Upstream(BaseHTTPRequestHandler):
    calls = collections.Counter()
    elapsed = {}
    def log_message(self, *_):
        pass

    def emit(self, obj):
        raw = json.dumps(obj).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        if self.path == "/v1/models":
            self.emit({"object": "list", "data": [{"id": MODEL, "object": "model"}]})
        else:
            self.send_error(404)

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        begin = time.monotonic()
        messages = body.get("messages") or [{}]
        marker = messages[-1].get("content")
        def observe():
            if isinstance(marker, str) and marker.startswith("BE24 load "):
                self.elapsed[marker] = (time.monotonic()-begin)*1000
        self.calls[self.path] += 1
        if self.path == "/v1/responses":
            self.emit({"id":"resp_be24","object":"response","status":"completed","model":MODEL,
                       "output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Fixture pong","annotations":[]}]}],
                       "usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}})
            return
        if self.path != "/v1/chat/completions":
            self.send_error(404)
            return
        chat = dict(id="chatcmpl-be24", model=MODEL, created=1)
        if body.get("stream"):
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.end_headers()
            try:
                for delta in [{"role":"assistant"}, {"content":"Fixture pong"}]:
                    frame = dict(chat, object="chat.completion.chunk", choices=[dict(index=0, delta=delta, finish_reason=None)])
                    if delta.get("role") and isinstance(marker,str) and marker.endswith("cancel"):
                        observe()
                    self.wfile.write(("data: "+json.dumps(frame)+"\n\n").encode())
                    self.wfile.flush()
                    time.sleep(.08)
                if not (isinstance(marker,str) and marker.endswith("cancel")):
                    observe()
                self.wfile.write(b'data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}\n\ndata: [DONE]\n\n')
            except (BrokenPipeError, ConnectionResetError, ssl.SSLError):
                pass
        else:
            observe()
            self.emit(dict(chat, object="chat.completion", choices=[dict(index=0, message=dict(role="assistant",content="Fixture pong"), finish_reason="stop")], usage=dict(prompt_tokens=1,completion_tokens=1,total_tokens=2)))


@contextlib.contextmanager
def deployment(binary):
    data = WORK / "data"
    data.mkdir()
    key, cert = WORK / "tls.key", WORK / "tls.pem"
    sp.run(["openssl","req","-x509","-newkey","rsa:2048","-nodes","-days","1","-subj","/CN=localhost",
            "-addext","subjectAltName=IP:127.0.0.1","-keyout",key,"-out",cert],check=True,stderr=sp.DEVNULL)
    email, password = "release@example.invalid", secrets.token_urlsafe(24)
    env = dict(os.environ, DATABASE_HOST="127.0.0.1", DATABASE_PORT="5432", DATABASE_USER="postgres",
               DATABASE_DBNAME="candidate", DATABASE_PASSWORD=secrets.token_urlsafe(16), DATABASE_SSLMODE="disable",
               REDIS_HOST="127.0.0.1", REDIS_PORT="6379", REDIS_DB="0", ADMIN_EMAIL=email, ADMIN_PASSWORD=password,
               SERVER_HOST="127.0.0.1", SERVER_MODE="release", JWT_SECRET=secrets.token_hex(32),
               OMNIHIVE_DATA_DIR=str(data), CONFIG_FILE=str(data/"config.yaml"), SSL_CERT_FILE=str(cert))
    def launch(stack, **config):
        log = stack.enter_context((WORK / ("server-"+config["SERVER_PORT"]+".log")).open("ab"))
        proc = sp.Popen([binary],cwd=WORK,env=dict(env,**config),stdout=log,stderr=log)
        def stop():
            if proc.poll() is None:
                proc.terminate()
                try:
                    proc.wait(4)
                except sp.TimeoutExpired:
                    proc.kill()
                    proc.wait()
        stack.callback(stop)
        return proc
    def wait(proc, ready):
        for _ in range(300):
            if proc.poll() is not None:
                raise Prerequisite("candidate exited before readiness")
            try:
                if ready():
                    return
            except OSError:
                pass
            time.sleep(.1)
        raise Prerequisite("candidate readiness timeout")
    with contextlib.ExitStack() as boot:
        proc = launch(boot,AUTO_SETUP="true", SERVER_PORT="18080")
        wait(proc,lambda:(data/".installed").is_file() and (data/"config.yaml").is_file())
    with (data/"config.yaml").open("a") as f:
        f.write('\nsecurity:\n  url_allowlist:\n    enabled: true\n    upstream_hosts: ["127.0.0.1"]\n    allow_private_hosts: true\n    allow_insecure_http: true\ntotp:\n  encryption_key: "'+secrets.token_hex(32)+'"\n')
    with contextlib.ExitStack() as stack:
        upstream = ThreadingHTTPServer(("127.0.0.1", 18443), Upstream)
        ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        ctx.load_cert_chain(cert,key)
        upstream.socket = ctx.wrap_socket(upstream.socket,server_side=True)
        threading.Thread(target=upstream.serve_forever,daemon=True).start()
        stack.callback(upstream.server_close)
        stack.callback(upstream.shutdown)
        processes = []
        for port in [18080,18081]:
            proc = launch(stack,SERVER_PORT=str(port))
            wait(proc,lambda:call("GET","/api/v2/me/scopes",port=port)[0]==401)
            processes.append(proc)
        yield email,password,processes,cert,key


def provision(email,password):
    t = need("POST","/api/v1/auth/login",200,body=dict(email=email,password=password))
    token, uid = t["access_token"],t["user"]["id"]
    scopes = need("GET","/api/v2/me/scopes",200,token)["items"][0]
    w,p = scopes["workspace_id"],scopes["projects"][0]["id"]
    prefix = f"/api/v2/workspaces/{w}/projects/{p}"
    compliance = need("GET","/api/v1/admin/compliance",200,token)
    if compliance["required"]:
        need("POST","/api/v1/admin/compliance/accept",200,token,dict(phrase=compliance["ack_phrase_en"],language="en"))
    need("POST",f"/api/v1/admin/users/{uid}/balance",200,token,dict(balance=100,operation="set",notes="BE24"))
    need("PUT","/api/v1/admin/settings",200,token,dict(model_plaza_enabled=True,model_plaza_require_auth=False))
    gid = need("POST","/api/v1/admin/groups",200,token,dict(name="BE24",platform="openai",rate_multiplier=1))["id"]
    native = need("POST","/api/v1/keys",200,token,dict(name="BE24 native",group_id=gid))
    need("POST","/api/v1/admin/accounts",200,token,dict(name="BE24",platform="openai",type="apikey",
               credentials=dict(api_key=secrets.token_urlsafe(16),base_url="https://127.0.0.1:18443",api_protocol="chat_completions", model_mapping={MODEL:MODEL}),
               concurrency=4,group_ids=[gid]))
    channel=need("POST","/api/v1/admin/channels",200,token,dict(name="BE24",group_ids=[gid],
        model_pricing=[dict(platform="openai",models=[MODEL],billing_mode="per_request",per_request_price=.00225)]))
    endpoint="/api/v1/admin/channels/"+str(channel["id"])
    for _ in range(50):
        candidates=need("GET",endpoint+"?plaza_group_id="+str(gid),200,token)
        if {"name":MODEL,"platform":"openai"} in candidates.get("candidates",[]):
            break
        time.sleep(.1)
    need("PUT",endpoint+"?plaza_confirmation=true",200,token,dict(group_id=gid,revision=candidates["revision"],models=[dict(platform="openai",name=MODEL)]))
    return token,native,prefix


def journey(token,prefix):
    model_id = MODEL_ID
    key_id, request_id, price, settlement, policy = None,None,None,None,None
    cases=[]
    for step in FIXTURE["steps"]:
        sid=step["id"]
        path=step["path"].replace("{scope}",prefix)
        if sid=="J05":
            path=path.split("?")[0]+"?"+urlencode(WINDOW)
        sentinel="00000000-0000-4000-8000-000000000024"
        path=path.replace("r-fixture",request_id or sentinel)
        raw=json.dumps(step.get("body")).replace("k-fixture",str(key_id or 0)).replace("m-fixture",model_id)
        headers=step.get("headers",{})
        status,reply=call(step["method"],path,token,json.loads(raw),headers)
        data=reply.get("data") or {}
        if sid=="J02":
            key_id=data.get("id")
        if sid=="J03":
            request_id=data.get("request_id")
        if sid=="J04" and request_id:
            rid=str(UUID(request_id))
            price=sql(f"SELECT price_version FROM gateway_executions WHERE request_id='{rid}'") or None
            policy=sql(f"SELECT decision->>'policy_version' FROM gateway_routing_decisions WHERE request_id='{rid}'") or None
        if sid=="J05" and request_id:
            data=dict(data,items=[x for x in data.get("items",[]) if x.get("request_id")==request_id])
            settlement=sql(f"SELECT id FROM usage_settlement_receipts WHERE request_id='{str(UUID(request_id))}' AND api_key_id={int(key_id)}") or None
        identities={"m-fixture":model_id,"p-fixture":prefix.split("/")[-1],"w-fixture":prefix.split("/")[4],
                    "k-fixture":key_id,"r-fixture":request_id,"pr-fixture":price,"st-fixture":settlement,"policy-fixture":policy}
        fields={}
        ok=status==step["status"] and reply.get("code")==0
        for k,want in step["checks"]:
            cur=data
            try:
                for part in k.split("."):
                    cur=cur[int(part)] if isinstance(cur,list) else cur[part]
            except (KeyError,IndexError,TypeError):
                cur=None
            if k=="credential":
                cur="<redacted>" if isinstance(cur,str) and cur.startswith("oh_p_") else None
            if isinstance(want,str):
                want=identities.get(want,want)
                if isinstance(want,str):
                    want=want.replace("{scope}",prefix).replace("r-fixture",request_id or sentinel)
            matches=cur is not None and want is not None and cur==want
            # Only fixture-selected fields, never raw untrusted output or credentials.
            fields[k]={"expected":want,"actual":cur,"match":matches}
            ok &= matches
        if sid=="J02":
            again,replay=call("POST",path,token,json.loads(raw),headers)
            valid=again==201 and key_id is not None and replay.get("data",{}).get("id")==key_id and replay["data"].get("credential","missing") is None
            fields["replay_same_id_null_secret"]={"expected":True,"actual":valid}
            ok &= valid
        case={"id":sid,"expected_status":step["status"],"http":status,"fields":fields,"status":"pass" if ok else "fail"}
        if status >= 400:
            case["error_code"] = error_code(reply)
        if sid == "J03" and key_id:
            case["j02_key_has_group"] = sql(f"SELECT group_id IS NOT NULL FROM api_keys WHERE id={int(key_id)}") == "t"
        if sid in ("J04","J05","J06") and not request_id:
            case.update(status="pending",reason="J03 produced no request ID. Probed route using an unallocated UUID or the current charge window.")
        cases.append(case)
    return cases


def access(token, native, prefix):
    rows = []
    headers = {"Idempotency-Key": "be24-export"}
    export = need("POST",prefix+"/billing/exports",202,token,dict(WINDOW,format="csv"),headers)
    download = export["result"]["download_url"]
    other = need("POST",prefix.rsplit("/",1)[0],201,token,{"name":"BE24 second project"},{"Idempotency-Key":"be24-project"})
    other_project = prefix.rsplit("/",1)[0]+"/"+str(other["id"])
    w = int(sql("INSERT INTO workspaces(name) VALUES ('BE24 foreign') RETURNING id"))
    p = int(sql(f"SELECT id FROM projects WHERE workspace_id={w} AND is_default"))
    foreign = f"/api/v2/workspaces/{w}/projects/{p}"
    other_email, other_pwd = "second@example.invalid",secrets.token_urlsafe(24)
    second = need("POST","/api/v1/admin/users",200,token,dict(email=other_email,password=other_pwd,role="user"))
    sql(f"INSERT INTO workspace_members(workspace_id,user_id,role) VALUES({w},{int(second['id'])},'admin')")
    second_token = need("POST","/api/v1/auth/login",200,body=dict(email=other_email,password=other_pwd))["access_token"]

    def observe(name, code, want):
        rows.append(dict(id=name,http=code,expected=want,status="pass" if code in want else "fail"))
    for name,path,who,want in [
        ("owner_download",download,token,[200]),("member_own_workspace",foreign+"/access",second_token,[200]),
        ("other_project_download",download.replace(prefix,other_project),token,[404]),
        ("other_workspace_download",download,second_token,[404]),
        ("other_workspace_read",prefix+"/access",second_token,[404]),
        ("owner_key_read","/api/v1/keys/"+str(native["api_key"]["id"]),token,[200]),
        ("other_user_key_read","/api/v1/keys/"+str(native["api_key"]["id"]),second_token,[404])]:
        observe(name,call("GET",path,who)[0],want)
    revoke = need("POST","/api/v1/keys",200,token,dict(name="Revoke",group_id=native["api_key"]["group_id"]))
    ports=[18080,18081]
    for port in ports:
        observe(f"warm_{port}",call("GET","/v1/models",revoke["secret"],port=port)[0],[200])
    deleted = call("DELETE","/api/v1/keys/"+str(revoke["api_key"]["id"]),token)[0]
    observe("revoke",deleted,[200,204])
    start=time.monotonic()
    observed={}
    for _ in range(600):
        for port in ports:
            if port in observed:
                continue
            status=call("GET","/v1/models",revoke["secret"],port=port)[0]
            if status in (401,403):
                observed[port]=(status,round((time.monotonic()-start)*1000,2))
        if len(observed)==len(ports) or time.monotonic()-start>=60:
            break
        time.sleep(.1)
    for port in ports:
        status,ms=observed.get(port,(0,None))
        # A second read catches stale reuse immediately following the first denial.
        repeat=call("GET","/v1/models",revoke["secret"],port=port)[0]
        rows.append(dict(id=f"revoked_{port}",http=status,repeat=repeat,elapsed_ms=ms,
                         expected="both denied within 60000 ms",status="pass" if status and repeat in (401,403) and ms<=60000 else "fail"))
    return rows


def grouped_playground(token, native, prefix):
    steps={step["id"]:step for step in FIXTURE["steps"]}
    key_body=json.loads(json.dumps(steps["J02"]["body"]).replace("m-fixture",MODEL_ID))
    created=need("POST",prefix+"/keys",201,token,key_body,{"Idempotency-Key":"be24-grouped-key"})
    kid=str(created["id"])
    status,reply=call("PUT","/api/v1/keys/"+kid,token,dict(group_id=native["api_key"]["group_id"]))
    rows=[dict(id="eligible_group_assignment",http=status,error_code=error_code(reply),status="pass" if status==200 else "fail")]
    if status!=200:
        return rows
    body=json.loads(json.dumps(steps["J03"]["body"]).replace("k-fixture",kid).replace("m-fixture",MODEL_ID))
    begin=sum(Upstream.calls.values())
    status,reply=call("POST",prefix+"/playground/runs",token,body,{"Idempotency-Key":"be24-grouped-run","Accept":"application/json"})
    data=reply.get("data") or {}
    rows.append(dict(id="grouped_project_key_run",http=status,error_code=error_code(reply),
                     upstream_posts=sum(Upstream.calls.values())-begin,request_id=data.get("request_id"),
                     status="pass" if status==200 and data.get("outcome")=="succeeded" and data.get("request_id") else "fail"))
    return rows


def surfaces(token, prefix, policy_seed):
    rows = []
    compare = dict(model_ids=[MODEL_ID], scenario=dict(input_tokens="1", requests="1"))
    public = "/api/v2/public"
    def probe(label, method, path, who, body, fields, check):
        status, reply = call(method, path, who, body)
        data = reply.get("data") or {}
        valid = status == 200 and reply.get("code") == 0 and all(k in data for k in fields) and check(data)
        rows.append(dict(id=label, http=status, expected_fields=fields, observed_fields=sorted(data),
                         error_code=error_code(reply), status="pass" if valid else "fail"))
    for label, root, who in [("public", public, None), ("project", prefix, token)]:
        def matches_scope(data):
            models = [m for m in data["items"] if m.get("id")==MODEL_ID]
            state = ("requires_scope","requires_scope") if who is None else ("allowed","eligible")
            return any((m.get("access_state"),m.get("route_state"))==state for m in models)
        probe(label+"_models", "GET", root+"/models", who, None, ["items"], matches_scope)
        probe(label+"_compare", "POST", root+"/models/compare", who, compare, ["catalog_version","rows"],
              lambda d:any(r.get("model_id")==MODEL_ID and "quote" in r for r in d["rows"]))
    probe("public_status", "GET", public+"/status?window=1h", None, None, ["overall","components","incidents"],
          lambda d:isinstance(d["components"],list) and bool(d["components"]))
    probe("project_quota", "GET",prefix+"/billing/quotas",token,None,["rules","blocked","blocking_rule_ids"],
          lambda d:isinstance(d["blocked"],bool))
    probe("project_tasks", "GET", prefix+"/tasks", token, None, ["items"], lambda d:isinstance(d["items"],list))
    project = prefix.split("/")[-1]
    policies = prefix.split("/projects/")[0]+"/routing/policies"
    draft = dict(name="BE24 region",scope=dict(type="project",id=project),
                 constraints=dict(regions=dict(mode="allow_list",values=["EU"])),fallback=dict(endpoint=True))
    status, reply = call("POST",policies,token,draft,{"Idempotency-Key":"be24-policy"})
    head = reply.get("data") or {}
    created = status==201 and head.get("id") and head.get("draft_version")
    rows.append(dict(id="policy_draft",http=status,error_code=error_code(reply),status="pass" if created else "fail"))
    if created:
        sim = dict(version=head["draft_version"],project_id=project,model_id=MODEL_ID,protocol="openai_chat",
                   action="text.chat",input_token_bound="1",output_token_bound="1",seed="24")
        path=policies+"/"+head["id"]+"/simulate"
        status,reply=call("POST",path,token,sim)
        rows.append(dict(id="policy_no_graph",http=status,error_code=error_code(reply),
                         expected="snapshot required",status="pass" if status==410 and error_code(reply)=="SNAPSHOT_EXPIRED" else "fail"))
        sp.run([policy_seed,prefix.split("/")[4],project,MODEL_ID],check=True)
        def counts():
            return sql("""SELECT (SELECT count(*) FROM gateway_executions),
                (SELECT count(*) FROM api_key_budget_reservations),(SELECT count(*) FROM usage_settlement_receipts),
                (SELECT count(*) FROM billing_wallet_events)""")
        before=counts()
        probe("policy_simulate","POST",path,token,sim,["simulation_id","trace"],
              lambda d:bool(d["simulation_id"]) and bool(d["trace"].get("selected")))
        rows[-1].update(graph_source="BE-10 SnapshotWriter / synthetic eligible EU endpoint",effects_unchanged=before==counts())
        if not rows[-1]["effects_unchanged"]:
            rows[-1]["status"]="fail"
    else:
        rows.append(dict(id="policy_simulate",status="pending",reason="draft creation failed"))
    return rows


def resources(procs):
    samples=[]
    for proc in procs:
        base=Path(f"/proc/{proc.pid}")
        stat=(base/"stat").read_text().split()
        rss=next(int(x.split()[1]) for x in (base/"status").read_text().splitlines() if x.startswith("VmRSS:"))
        samples.append(dict(rss_kib=rss,threads=int(stat[19]),cpu_seconds=(int(stat[13])+int(stat[14]))/os.sysconf("SC_CLK_TCK"),
                            fds=len(list((base/"fd").iterdir()))))
    redis=sp.check_output(["redis-cli","--raw","info","memory"],text=True)
    used=next(int(x.split(":")[1]) for x in redis.splitlines() if x.startswith("used_memory:"))
    reserved,pending=sql("""SELECT (SELECT count(*) FROM api_key_budget_reservations WHERE status='reserved'),
                                (SELECT count(*) FROM usage_settlement_receipts WHERE state='pending')""").split("|")
    return dict(instances=samples,redis_bytes=used,pg_connections=int(sql("SELECT count(*) FROM pg_stat_activity WHERE datname='candidate'")),
                pending_receipts=int(pending),reserved_budgets=int(reserved))


def load(token,native,prefix,procs,seconds):
    kinds=["access","catalog","chat","stream","cancel","chat"]
    start=time.monotonic()
    before=resources(procs)
    def lane(lane_id):
        results=[]
        i=lane_id
        while True:
            due=start+i
            if i>=seconds:
                break
            time.sleep(max(0,due-time.monotonic()))
            kind=kinds[i%len(kinds)]
            marker=f"BE24 load {i} {kind}"
            begin=time.monotonic()
            try:
                if kind=="access":
                    status,body=call("GET",prefix+"/access",token)
                    valid=body.get("code")==0
                elif kind=="catalog":
                    status,body=call("GET","/api/v2/public/models?limit=1")
                    valid=body.get("code")==0
                else:
                    status,body=call("POST","/v1/chat/completions",native["secret"],
                                    dict(model=MODEL,messages=[dict(role="user",content=marker)],stream=kind!="chat"),
                                    cancel=kind=="cancel")
                    valid=(body.get("choices",[{}])[0].get("message",{}).get("content")=="Fixture pong" if kind=="chat"
                           else body.get("stream_done") if kind=="stream" else body.get("opened"))
                ok=status==200 and bool(valid)
            except (OSError,http.client.HTTPException):
                status,ok=0,False
            elapsed=(time.monotonic()-begin)*1000
            upstream_ms=Upstream.elapsed.pop(marker, None)
            overhead=max(0,elapsed-upstream_ms) if upstream_ms is not None else None
            results.append((kind,status,ok,elapsed,overhead))
            i+=2
        return results
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        parts=list(pool.map(lane,[0,1]))
    time.sleep(max(0, start+seconds-time.monotonic()))
    elapsed=time.monotonic()-start
    results=[row for part in parts for row in part]
    def stats(rows):
        timing=sorted(row[3] for row in rows)
        def percentile(p):
            return round(timing[max(0,math.ceil(len(rows)*p)-1)],2)
        failed=sum(not row[2] for row in rows)
        return dict(n=len(rows),errors=failed,error_rate=round(failed/len(rows),6),p50_ms=percentile(.5),p95_ms=percentile(.95),
                    http=dict(collections.Counter(str(row[1]) for row in rows)))
    end=resources(procs)
    cooldown=60 if seconds==7200 else 2
    time.sleep(cooldown)
    overhead=sorted(r[4] for r in results if r[4] is not None)
    return dict(duration_s=seconds,elapsed_s=round(elapsed,3),schedule="BE24-v2: two staggered lanes, 1 request/s; six-request cycle",
                all=stats(results),actions={k:stats([r for r in results if r[0]==k]) for k in set(kinds) if any(r[0]==k for r in results)},
                overhead_samples=len(overhead),expected_overhead_samples=sum(r[0] not in ("access","catalog") for r in results),
                gateway_overhead_p95_ms=round(overhead[math.ceil(len(overhead)*.95)-1],2) if overhead else None,
                resources_before=before,resources_end=end,resources_after_cooldown=resources(procs),cooldown_s=cooldown,
                upstream_posts=dict(Upstream.calls))


def native_projections(token,native,prefix):
    headers={}
    status,body=call("POST","/v1/responses",native["secret"],
                     dict(model=MODEL,input="Fixture ping",stream=False),
                     {"Idempotency-Key":"be24-native-correlated"},response_headers=headers)
    rid=next((v for k,v in headers.items() if k.lower()=="x-execution-id"),None)
    ok=status==200 and rid and body.get("object")=="response"
    out=[dict(id="native_responses",http=status,request_id=rid,expected="HTTP 200 + native Responses object + execution ID",status="pass" if ok else "fail")]
    if not rid:
        return out
    for label,path in [("native_detail",prefix+"/requests/"+rid),("native_charges",prefix+"/billing/charges?"+urlencode(WINDOW)),
                       ("native_explain",prefix+"/requests/"+rid+"/route-explain")]:
        status,body=call("GET",path,token)
        data=body.get("data") or {}
        if label=="native_charges":
            observed=next((r for r in data.get("items",[]) if r.get("request_id")==rid),{})
        else:
            observed=data
        linked=rid in (observed.get("id"),observed.get("request_id"))
        if label=="native_detail":
            billing=observed.get("billing") or {}
            facts={k:observed.get(k) for k in ("workspace_id","project_id","api_key_id","outcome")}
            facts.update({k:billing.get(k) for k in ("receipt_state","price_version","platform_charge")})
        elif label=="native_charges":
            facts={k:observed.get(k) for k in ("state","settlement_id","price_version","platform_charge")}
        else:
            trace=observed.get("trace") or {}
            facts=dict(mode=trace.get("mode"),policy_version=trace.get("policy_version"),
                       completeness=(observed.get("data_completeness") or {}).get("state"))
        out.append(dict(id=label,http=status,expected="200 + same execution ID",correlated=linked,
                        projection=facts,status="pass" if status==200 and linked else "fail"))
    return out


def callback(token):
    secret=secrets.token_urlsafe(32)
    provider=need("POST","/api/v1/admin/payment/providers",201,token,dict(provider_key="stripe",name="BE24 Stripe",
        enabled=True,supported_types=["stripe"],config={"secretKey":secrets.token_urlsafe(32),"publishableKey":secrets.token_urlsafe(32),
        "webhookSecret":secret,"currency":"USD"}))
    user=int(sql("INSERT INTO users(email,password_hash,balance) VALUES('payer@example.invalid','fixture',10) RETURNING id"))
    provider_id=int(provider["id"])
    order=int(sql(f"""INSERT INTO payment_orders(user_id,amount,pay_amount,order_type,payment_type,provider_key,
        provider_instance_id,provider_snapshot,out_trade_no,recharge_code,expires_at)
        VALUES({user},1.25,1.25,'balance','stripe','stripe','{provider_id}','{{"currency":"USD"}}',
        'be24-order','be24-redeem',now()+interval '1 day') RETURNING id"""))
    payload={"id":"evt_fixture_release","object":"event","api_version":"2026-03-25.dahlia","type":"payment_intent.succeeded",
             "data":{"object":{"id":"pi_fixture_release","currency":"usd","amount_received":125,"amount":125,"metadata":{"orderId":"be24-order"}}}}
    ts=str(int(time.time()))
    digest=hmac.new(secret.encode(),(ts+"."+json.dumps(payload)).encode(),"sha256").hexdigest()
    states=[]
    for delivery in range(2):
        status,_=call("POST","/api/v1/payment/webhook/stripe",body=payload,headers={"Stripe-Signature":"t="+ts+",v1="+digest})
        balance=sql(f"SELECT balance::text FROM users WHERE id={user}")
        state=sql(f"SELECT status FROM payment_orders WHERE id={order}")
        events=int(sql(f"SELECT count(*) FROM payment_provider_events WHERE order_id={order}"))
        wallet=sql(f"""SELECT count(*),coalesce(sum(e.amount),0)::text
            FROM billing_wallet_events e JOIN billing_funding_accounts f ON f.id=e.funding_account_id
            WHERE f.user_id={user} AND e.kind='balance_change' AND e.currency='USD'""").split("|")
        states.append(dict(delivery=delivery+1,http=status,balance=balance,order=state,
                           provider_event_rows=events,wallet_postings=int(wallet[0]),net_credit_usd=wallet[1]))
    valid=all(r["http"]==200 and r["order"]=="COMPLETED" and r["balance"]=="11.25000000" and
              r["wallet_postings"]==1 and r["net_credit_usd"]=="1.25000000" for r in states)
    return dict(status="pass" if valid else "fail",expected="two HTTP 200s; one USD 1.25 wallet posting; completed order",deliveries=states)


def sdk(native):
    if not os.environ.get("BE24_SDK_ROOT"):
        return dict(status="pending",reason="install the offline openai@4.104.0 Node fixture and set BE24_SDK_ROOT")
    completed=sp.run(["node",HERE/"sdk.cjs"],input=json.dumps(dict(key=native["secret"],model=MODEL)),
                     text=True,capture_output=True,timeout=45,check=True)
    return json.loads(completed.stdout)


def protocol_http(token,native,prefix):
    limited=need("POST",prefix+"/keys",201,token,dict(name="BE24 read-only",actions=["usage.read"],
                 model_access=dict(mode="inherit",ids=[])),{"Idempotency-Key":"be24-read-only"})
    rows=[]
    shapes=[
        ("chat","/v1/chat/completions",dict(model=MODEL,messages=[dict(role="user",content="Fixture ping")])),
        ("responses","/v1/responses",dict(model=MODEL,input="Fixture ping")),
        ("messages","/v1/messages",dict(model=MODEL,max_tokens=32,messages=[dict(role="user",content="Fixture ping")])),
        ("gemini","/v1beta/models/"+MODEL+":generateContent",dict(contents=[dict(parts=[dict(text="Fixture ping")])])),
        ("embeddings","/v1/embeddings",dict(model=MODEL,input="Fixture ping")),
        ("images","/v1/images/generations",dict(model=MODEL,prompt="fixture",n=1))]
    for action,path,payload in shapes:
        for account,key in [("read-only project key",limited["credential"]),("OpenAI apikey with chat model",native["secret"])]:
            start=sum(Upstream.calls.values())
            kwargs={"headers":{"x-goog-api-key":key}} if action=="gemini" else {"token":key}
            status,body=call("POST",path,body=payload,**kwargs)
            calls=sum(Upstream.calls.values())-start
            if account=="read-only project key":
                want="403; zero upstream requests"
                ok=status==403 and calls==0
            elif action=="messages":
                want="AllowMessagesDispatch is off for this OpenAI group; 403 and zero upstream requests"
                ok=status==403 and calls==0
            elif action in ("embeddings","images"):
                want="text model has no verified media/embedding endpoint; client rejection and zero upstream requests"
                ok=status in (400,403,404,422) and calls==0
            else:
                want="native text envelope; HTTP 200 and one upstream request"
                shape={"chat":"choices","responses":"output","messages":"content","gemini":"candidates"}[action]
                ok=status==200 and shape in body and calls==1
            rows.append(dict(action=action,account=account,expected=want,http=status,upstream_posts=calls,
                             observed="dispatched" if calls else "no send",error_code=error_code(body),status="pass" if ok else "fail"))
    return rows
