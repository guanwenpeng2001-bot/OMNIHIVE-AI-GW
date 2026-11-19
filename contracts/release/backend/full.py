#!/usr/bin/env python3
import argparse
import collections
import concurrent.futures
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import selectors
import shutil
import subprocess as sp
import time

import live

HERE = Path(__file__).resolve().parent
ROOT = Path(os.environ.get("BE24_PRODUCT_ROOT", HERE.parents[2])).resolve()
BACKEND = ROOT / "backend"
WORK = live.WORK
PREVIOUS = "b0bf5b9e17df1d076f16ca57cfa0709f92a71612"  # last integrated pre-BE-01 schema


def command(*args, cwd=ROOT, **kw):
    return sp.check_output(args,cwd=cwd,text=True,**kw).strip()


def write(path, data):
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2,sort_keys=True)+"\n")


def overlay(name, replacements):
    target=WORK/(name+"-overlay.json")
    write(target,{"Replace":{str(BACKEND/a):str(b) for a,b in replacements.items()}})
    return "-overlay="+str(target)


def go_test(packages, expr, options=(), env=None, matrix=None):
    cmd=["go","test","-p","1","-count=1","-json",*options,"-run",expr,*packages]
    tests={}
    decisions={}
    error_locations=set()
    with sp.Popen(cmd,cwd=BACKEND,env=env,stdout=sp.PIPE,text=True) as proc:
        for line in proc.stdout:
            event=json.loads(line)
            test=event.get("Test")
            if test and event["Action"] in ("pass","fail","skip"):
                tests[test]=event["Action"]
            if test and "Error Trace:" in event.get("Output",""):
                loc=re.search(r"([a-zA-Z0-9_]+_test\.go:\d+)", event["Output"])
                if loc:
                    error_locations.add((test,loc[1]))
            if test and matrix is not None and "BE24_BINDING " in event.get("Output",""):
                decisions[test]=event["Output"].split("BE24_BINDING ")[-1].strip().split("|")
    if matrix is not None:
        for test,parts in sorted(decisions.items()):
            matrix.append([test.split("/",1)[1],*parts,tests.get(test,"pending")])
    counts=dict(collections.Counter(tests.values()))
    roots={k:v for k,v in tests.items() if "/" not in k}
    required=re.fullmatch(r"\^Test(?:\(([A-Za-z0-9|]+)\)|([A-Za-z0-9]+))\$",expr)
    missing=sorted({"Test"+k for k in (required[1] or required[2]).split("|")}-roots.keys()) if required else []
    return dict(status="fail" if proc.returncode or not tests or counts.get("fail") or missing else "pending" if counts.get("skip") else "pass",
                command=" ".join(x for x in cmd if not x.startswith("-overlay=")),counts=counts,
                tests=roots,missing=missing,failed_locations=sorted(error_locations),
                skipped=[t for t,s in tests.items() if s=="skip"],failed=[t for t,s in tests.items() if s=="fail"][:20])


def registry(out):
    path=out/"registry.json"
    move=overlay("registry",{"internal/server/routes/be24_registry_test.go": HERE/"registry_test.go.fixture"})
    result=go_test(["./internal/server/routes"],"^Test(BE24Registry|GatewayActionHandlersCoverEveryImplementedCell)$",
                   [move],env=dict(os.environ,BE24_REGISTRY_REPORT=str(path)))
    if path.exists():
        data=json.loads(path.read_text())
        # One declaration or profile per line keeps individual decisions diffable.
        compact=lambda value:json.dumps(value,ensure_ascii=False,separators=(",",":"))
        with path.open("w") as stream:
            stream.write('{"actions":'+compact(data["actions"]))
            for key in ("decisions","identities"):
                stream.write(',\n"'+key+'":[\n')
                stream.write(",\n".join(compact(row) for row in data[key]))
                stream.write("]")
            stream.write("}\n")
        counts=collections.Counter(data["decisions"][idx]["status"]
                                   for row in data["identities"] for idx in row["cells"])
        result.update(actions=len(data["actions"]),profiles=len(data["identities"]),cells=sum(counts.values()),
                      counts_by_declaration=counts,evidence=path.name)
    else:
        result["status"]="fail"
    return result


def matrix(out):
    # Reuse the registry-driven matrix. A temporary Go overlay adds observation only.
    relative="internal/server/routes/gateway_protocol_matrix_test.go"
    source=(BACKEND/relative).read_text()
    needle='require.Equal(test, cell.Status == domain.ActionImplemented, service.ResolveAccountModelBinding(ctx, account, "public-alias", options).Supported'
    if source.count(needle)!=2:
        raise RuntimeError("candidate binding matrix changed; update observation point")
    instrumented=WORK/"matrix_test.go"
    observe='test.Logf("BE24_BINDING %s|%s|%s|%t", cell.Status, cell.Adapter, wireEndpoint, service.ResolveAccountModelBinding(ctx, account, "public-alias", options).Supported)'
    instrumented.write_text(source.replace(needle, observe+"\n\t\t\t\t\t"+needle))
    rows=[]
    result=go_test(["./internal/server/routes"],"^TestGateway(PlatformProtocolMatrix|UnsupportedProtocolContracts)$",
                   [overlay("matrix",{relative:instrumented})],matrix=rows)
    target=out/"matrix.json"
    with target.open("w") as f:
        f.write('{"columns":["platform/account[/mode/protocol]/action","declared","adapter","wire_endpoint","binding","test"],\n"rows":[\n')
        f.write(",\n".join(json.dumps(row, separators=(",",":")) for row in rows))
        f.write("]}\n")
    result.update(rows=len(rows),decisions=dict(collections.Counter(r[1] for r in rows)),
                  evidence="matrix.json",model="fixture-model",layer="candidate registry, account/model binding, and unsupported registered routes")
    if not rows:
        result["status"]="fail"
    return result


def finance():
    helper=WORK/"receipt"
    remap=overlay("receipt",{"api/v2/compose/main.go":HERE/"receipt.go.fixture"})
    command("go","build",remap,"-o",helper,"./api/v2/compose",cwd=BACKEND)
    rows=[]
    for mode in ("before-persist","after-persist","before-commit","after-commit"):
        seed=command(helper,"seed")
        with sp.Popen([helper,mode],stdin=sp.PIPE,stdout=sp.PIPE,text=True) as child:
            try:
                child.stdin.write(seed+"\n")
                child.stdin.close()
                with selectors.DefaultSelector() as selector:
                    selector.register(child.stdout,selectors.EVENT_READ)
                    if not selector.select(30) or child.stdout.readline().strip()!="AT_BOUNDARY":
                        raise RuntimeError("receipt phase barrier was not reached")
            finally:
                child.kill()
                child.wait()
        def inspect(action):
            return json.loads(command(helper,action,input=seed))
        crashed=inspect("read")
        if mode!="before-persist":
            # Duplicate recovery deliveries use only the original persisted fingerprint.
            with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
                list(pool.map(inspect,["replay","replay"]))
        recovered=inspect("read")
        missing=mode=="before-persist"
        want={"receipt":"unconfirmed" if missing else "settled","charge":None if missing else {"currency":"USD","amount":"0.00225000"},
              "balance":"100.00000000" if missing else "99.99775000","outbox":0 if missing else 1,
              "movements":0 if missing else 1,"version":"price-1"}
        if missing:
            want["review_reason"]="missing_receipt"
        prior={"before-persist":"unconfirmed","after-persist":"pending","before-commit":"pending","after-commit":"settled"}[mode]
        ok=child.returncode==-9 and all(recovered.get(k)==v for k,v in want.items()) and crashed["receipt"]==prior
        rows.append(dict(id=mode,signal=-child.returncode,expected_after_crash=prior,after_crash=crashed,
                         expected_after_recovery=want,after_recovery=recovered,status="pass" if ok else "fail"))
    return rows


def financial_tests():
    # These database-backed repository suites use the repository's "unit" build tag.
    return go_test(["./internal/repository"],"^Test(UsageSettlementReceiptPostgres|KeyBudgetReservationPostgres)$",["-tags=unit"])


def restore():
    relative="internal/modules/ops/restore/workflow_integration_test.go"
    source=(BACKEND/relative).read_text()
    start='p := preview("preview-a", Isolated)'
    result='require.EqualValues(t, 2, current.Generation)'
    if source.count(start)!=1 or source.count(result)!=1:
        raise RuntimeError("candidate BE-09 rehearsal observation points changed")
    target=WORK/"restore-verified.json"
    instrumented=WORK/"restore_test.go"
    observed=r'''
    var matched int
    for _, table := range tables {
        var count int
        require.NoError(t, isolated.QueryRow("SELECT count(*) FROM "+table+" WHERE id=1 AND value='original'").Scan(&count))
        matched += count
    }
    require.Equal(t, len(tables), matched)
    metrics, err := json.Marshal(map[string]any{"rto_seconds":time.Since(be24Begin).Seconds(),
        "recovered_source_rows":matched,"source_rows":len(tables),"source_row_loss":len(tables)-matched,
        "rpo_seconds_quiescent_snapshot":0,"post_snapshot_business_writes":0})
    require.NoError(t,err)
    require.NoError(t,os.WriteFile(os.Getenv("BE24_RESTORE_METRICS"),metrics,0600))
'''
    instrumented.write_text(source.replace(start,"be24Begin := time.Now()\n"+start).replace(result,result+observed))
    move=overlay("restore",{relative:instrumented})
    report=go_test(["./internal/modules/ops/restore","./internal/control/restore"],"^Test(FencedRestore|RestoreHTTPAndLegacy)$",
                   ["-tags=integration",move],env=dict(os.environ,BE24_RESTORE_METRICS=str(target)))
    if target.exists():
        report.update(json.loads(target.read_text()),rto_window="first isolated preview to successful fenced recovery and recovered-row checks")
    else:
        report["status"]="fail"
    return report


def migration():
    import tarfile
    # Read historical SQL bytes from a pinned commit, independently of the candidate checkout.
    historical=WORK/"historical"
    historical.mkdir()
    with sp.Popen(["git","archive",PREVIOUS,"backend/migrations"],cwd=ROOT,stdout=sp.PIPE) as proc:
        with tarfile.open(fileobj=proc.stdout,mode="r|") as archive:
            for entry in archive:
                if entry.isfile() and entry.name.endswith(".sql"):
                    (historical/Path(entry.name).name).write_bytes(archive.extractfile(entry).read())
    if proc.returncode:
        raise RuntimeError("historical revision missing from clone")
    move=overlay("migration",{"internal/repository/be24_snapshot_test.go":HERE/"migrate_test.go.fixture"})
    env=dict(os.environ,BE24_MIGRATION_DB="previous",BE24_PREVIOUS_SQL=str(historical))
    def apply():
        result=go_test(["./internal/repository"],"^TestBE24Snapshot$",[move],env)
        if result["status"]!="pass":
            raise RuntimeError("migration runner failed")
    apply()
    prior=live.sql("SELECT count(*) FROM schema_migrations","previous")
    live.sql("INSERT INTO users(email,password_hash,balance) VALUES('upgrade@example.invalid','fixture',9.12345678)","previous")
    snapshot=WORK/"previous.dump"
    sp.run(["pg_dump","-Fc","-h","127.0.0.1","-U","postgres","-f",snapshot,"previous"],check=True)
    start=time.monotonic()
    sp.run(["pg_restore","-e","-h","127.0.0.1","-U","postgres","-d","upgraded",snapshot],check=True)
    env["BE24_MIGRATION_DB"]="upgraded"
    apply()
    rto=round(time.monotonic()-start,3)
    apply()
    def inventory(db):
        return live.sql("SELECT filename||':'||checksum FROM schema_migrations ORDER BY filename",db).splitlines()
    old,latest,fresh=inventory("previous"),inventory("upgraded"),inventory("candidate")
    balance=live.sql("SELECT balance::text FROM users WHERE email='upgrade@example.invalid'","upgraded")
    ok=set(old)<=set(latest) and latest==fresh and balance=="9.12345678"
    return dict(status="pass" if ok else "fail",previous_revision=PREVIOUS,previous_files=int(prior),latest_files=len(latest),
                source_checksums_retained=set(old)<=set(latest),same_as_empty_db=latest==fresh,source_balance="9.12345678",
                restored_balance=balance,restore_and_first_upgrade_s=rto,snapshot_row_loss=int(not bool(balance)),
                latest_inventory_sha256=hashlib.sha256("\n".join(latest).encode()).hexdigest())


def main():
    parser=argparse.ArgumentParser()
    mode=parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--seconds",type=int)
    mode.add_argument("--finance",action="store_true")
    args=parser.parse_args()
    duration=args.seconds
    utc=lambda:dt.datetime.now(dt.timezone.utc)
    started=utc()
    rev=command("git","rev-parse","HEAD")
    if args.finance:
        result=financial_tests()
        print(json.dumps(dict(revision=rev,started=started.isoformat(),finished=utc().isoformat(),
                              postgres=command("pg_ctl","--version"),network=command("ip","-brief","address"),
                              FINANCE_MODULES=result),indent=2))
        return int(result["status"]!="pass")
    out=HERE/"reports"/(started.strftime("%Y%m%dT%H%M%SZ")+"-"+rev[:9])
    out.mkdir(parents=True,exist_ok=False)
    results={}
    env=dict(revision=rev,go=command("go","version"),postgres=command("pg_ctl","--version"),redis=command("redis-server","--version"),
             kernel=platform.release(),machine_hash=hashlib.sha256(Path("/etc/machine-id").read_bytes()).hexdigest()[:16],cpu=platform.machine(),cpu_count=os.cpu_count(),network=command("ip","-brief","address"),
             openapi_sha256=hashlib.sha256((BACKEND/"api/v2/openapi.yaml").read_bytes()).hexdigest(),started=started.isoformat())
    env.update(product_clean=not bool(command("git","status","--porcelain","--","backend","frontend/src/api/generated/v2.d.ts")),
               load_driver_sha256=hashlib.sha256((HERE/"live.py").read_bytes()).hexdigest(),
               node=command("node","--version"),python=platform.python_version())
    def run(name,fn):
        print(name,flush=True)
        try:
            value=fn()
            if isinstance(value,list):
                state="fail" if any(r["status"]=="fail" for r in value) else "pending" if not value or any(r["status"]!="pass" for r in value) else "pass"
                value=dict(status=state,cases=value)
            results[name]=value
        except (RuntimeError,ValueError,KeyError,IndexError,OSError,sp.SubprocessError,live.Prerequisite) as exc:
            # Never persist raw HTTP bodies, secrets, or subprocess argv/env.
            reason=str(exc) if isinstance(exc,(RuntimeError,live.Prerequisite)) else type(exc).__name__
            results[name]={"status":"fail","reason":reason}
            print(f"{name} failed: {reason}",flush=True)
        write(out/"results.json",{"environment":env,"checks":results})
    def openapi():
        command(HERE/"check-openapi.sh")
        command("python3",HERE/"check-journey.py")
        return dict(status="pass",fixture_steps=6,openapi="byte-for-byte")
    run("OPENAPI",openapi)
    def types():
        cli=os.environ.get("OPENAPI_TYPESCRIPT") or shutil.which("openapi-typescript")
        if not cli:
            return dict(status="pending",reason="install openapi-typescript@7.10.1 in the offline tool cache")
        version=command(cli,"--version")
        generated=WORK/"v2.d.ts"
        command(cli,BACKEND/"api/v2/openapi.yaml","-o",generated)
        committed=ROOT/"frontend/src/api/generated/v2.d.ts"
        digest=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
        ok=version.lstrip("v")=="7.10.1" and generated.read_bytes()==committed.read_bytes()
        return dict(status="pass" if ok else "fail",version=version,checked_in_sha256=digest(committed),generated_sha256=digest(generated))
    run("TYPESCRIPT",types)
    def http():
        binary=WORK/"server"
        command("go","build","-o",binary,"./cmd/server",cwd=BACKEND)
        policy_seed=WORK/"policy-seed"
        policy_overlay=overlay("policy",{"api/v2/compose/main.go":HERE/"policy_snapshot.go.fixture"})
        command("go","build",policy_overlay,"-o",policy_seed,"./api/v2/compose",cwd=BACKEND)
        with live.deployment(binary) as (email,pwd,procs,cert,key):
            token,native,prefix=live.provision(email,pwd)
            run("JOURNEY",lambda:live.journey(token,prefix))
            run("GROUPED_PLAYGROUND",lambda:live.grouped_playground(token,native,prefix))
            run("SURFACES_HTTP",lambda:live.surfaces(token,prefix,policy_seed))
            run("PROTOCOL_HTTP",lambda:live.protocol_http(token,native,prefix))
            run("PUBLIC_NATIVE",lambda:live.native_projections(token,native,prefix))
            run("SDK_HTTP",lambda:live.sdk(native))
            run("CALLBACK_HTTP",lambda:live.callback(token))
            run("ACCESS",lambda:live.access(token,native,prefix))
            def load():
                obs=live.load(token,native,prefix,procs,duration)
                complete=obs["all"]["n"]==duration and obs["overhead_samples"]==obs["expected_overhead_samples"]
                return dict(status="pass" if complete and not obs["all"]["errors"] else "fail",**obs)
            run("LOAD",load)
            # Independent HTTP smoke uses its own local TLS upstream and exercises wizard/rotation.
            smoke_env=dict(os.environ,SMOKE_BASE_URL="http://127.0.0.1:18080",SMOKE_ADMIN_EMAIL=email,SMOKE_ADMIN_PASSWORD=pwd,SMOKE_TLS_CERT=str(cert),SMOKE_TLS_KEY=str(key))
            rc=sp.run([ROOT/"contracts/migrations/BE-04/smoke.sh"],env=smoke_env,stdout=sp.DEVNULL).returncode
            return dict(status="pass" if rc==0 else "fail",fixture="BE-04/smoke.sh",exit_code=rc)
    run("LIVE",http)
    run("MATRIX",lambda:matrix(out))
    run("REGISTRY",lambda:registry(out))
    # Status counts every execution in its database, including other test data.
    # Give it an empty job_test on the independent rehearsal cluster.
    status_env=dict(os.environ,UPSTREAM_JOB_TEST_DSN="postgres://postgres@127.0.0.1:5433/job_test?sslmode=disable")
    run("STATUS_MODULE",lambda:go_test(["./internal/control/status"],"^TestStatusPostgres$",["-tags=integration"],status_env))
    run("FINANCE",finance)
    run("FINANCE_MODULES",financial_tests)
    run("SIGNED_CALLBACK",lambda:go_test(["./internal/modules/payments"],"^TestPaymentWritersPostgres$",["-tags=integration"]))
    run("ACCESS_MODULES",lambda:go_test(["./internal/control/requests","./internal/control/billing","./internal/control/audit","./internal/control/identity"],
        "^Test(RequestsPostgres|CostWorkbenchPostgres|ScopedAudit|ProjectIdentity)$",["-tags=integration"]))
    run("CONTROL_MODULES",lambda:go_test(["./internal/control/playground","./internal/control/tasks","./internal/control/policies"],
        "^Test(PaidPlayground|ImageTaskCenterDownloadHTTP|PolicyWorkflow)$",["-tags=integration"]))
    run("MIGRATION",migration)
    run("RESTORE",restore)
    run("CUTOVER_FIXTURE",lambda:go_test(["./cmd/omnihivectl/refactor"],"^Test",["-tags=integration"]))
    for name in ("JOURNEY","GROUPED_PLAYGROUND","SURFACES_HTTP","PROTOCOL_HTTP","PUBLIC_NATIVE","CALLBACK_HTTP","SDK_HTTP","ACCESS","LOAD"):
        results.setdefault(name,dict(status="pending",reason="candidate preparation failed"))
    for name,reason in [
        ("PAIRED_2H","two same-machine 7200-second load reports and post-cooldown resource comparison required"),
        ("CUTOVER","final BE-22/23 integration, mixed-version live writers and independence release sign-off required"),
        ("WIRE_PARITY","WebSocket, compact, media/task provider success and remaining supplier-specific SDK/frame parity await deterministic positive wire fixtures")]:
        results[name]=dict(status="pending",reason=reason)
    env["finished"]=utc().isoformat()
    write(out/"results.json",{"environment":env,"checks":results})
    summary=["# BE-24 candidate verification","",f"Candidate `{rev}`. All services ran on loopback in an isolated network namespace.","",
             "| Check | Status |","| --- | --- |"]
    summary.extend(f"| {key} | {result['status']} |" for key,result in results.items())
    summary.extend(["","Cohort decision: HOLD. Measurement details and field assertions are in [results.json](results.json). Individual candidate binding decisions are in [matrix.json](matrix.json)."])
    (out/"summary.md").write_text("\n".join(summary)+"\n")
    print(out,flush=True)
    return int(any(result["status"]!="pass" for result in results.values()))


if __name__=="__main__":
    raise SystemExit(main())
