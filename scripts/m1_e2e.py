"""M1 real producer/consumer processes, crash/replay and coverage collection."""
import json
import os
from pathlib import Path
import re
import subprocess
import time
import urllib.error
import urllib.request
import uuid

import m0


def http(url, token=None, body=None, headers=None):
    request = urllib.request.Request(url, data=None if body is None else json.dumps(body).encode(), headers={"Content-Type":"application/json", **({"Authorization":"Bearer " + token} if token else {}), **(headers or {})})
    try:
        with urllib.request.urlopen(request, timeout=3) as response:
            return response.status, response.read()
    except urllib.error.HTTPError as error:
        return error.code, error.read()


def run(output, project, packages, execute):
    if not re.fullmatch(r"pangolin-m1[-a-z0-9]*", project):
        raise ValueError("E2E requires an explicit pangolin-m1 namespace")
    suffix = ".exe" if os.name == "nt" else ""
    query=output/("m1-query"+suffix)
    execute(["go","build","-o",str(query),"./examples/bookstore/internal/testkit/cmd/m1-query"])
    def sql(statement):
        return execute([str(query),statement]).strip()
    if sql("SELECT current_database()")!="pangolin_m0":raise ValueError("not the isolated M1 fixture database")
    env = m0.go_environment(m0.ROOT)
    env.update(PANGOLIN_DSN=os.environ["PANGOLIN_M1_APP_DSN"], PANGOLIN_ISSUER=os.environ["PANGOLIN_M1_ISSUER"], PANGOLIN_NATS_URL=os.environ["PANGOLIN_M1_NATS_URL"], PANGOLIN_WORKER_TENANTS="tenant-a,tenant-b")
    api_url = "http://127.0.0.1:" + os.getenv("PANGOLIN_M1_API_PORT", "18181")
    api_cov, worker_cov = output / "api-cov", output / "worker-cov"
    for path in (api_cov, worker_cov):
        path.mkdir(parents=True, exist_ok=True)
    for name,entry in (("bookstore-api","api"),("bookstore-worker","worker")):
        execute(["go", "build", "-cover", "-covermode=atomic", "-coverpkg=" + packages, "-o", str(output / (name + suffix)), "./examples/bookstore/cmd/" + entry])
    execute(["go", "build", "-o", str(output / ("crash-worker" + suffix)), "./examples/bookstore/internal/testkit/cmd/crash-worker"])
    children, logs = [], []
    def start(name, arguments, child_env, label=None):
        log = open(output / ((label or name) + ".log"), "w", encoding="utf-8")
        logs.append(log)
        process = subprocess.Popen([str(output / (name + suffix)), *arguments], cwd=m0.ROOT, env=child_env, stdout=log, stderr=subprocess.STDOUT, creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0)
        children.append(process)
        return process
    def until(predicate, seconds=10):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            try:
                if predicate(): return
            except (OSError, urllib.error.URLError):
                pass
            time.sleep(.05)
        raise ValueError("E2E condition timed out; inspect process logs")
    try:
        crash_env = dict(env, PANGOLIN_DSN=os.environ["PANGOLIN_M1_WORKER_DSN"])
        if sql("SELECT count(*) FROM pangolin.outbox WHERE state='pending'")!="0":
            warmup=start("bookstore-worker",["-run-for","3s"],dict(crash_env,GOCOVERDIR=str(worker_cov)),"worker-warmup")
            if warmup.wait(timeout=8)!=0 or sql("SELECT count(*) FROM pangolin.outbox WHERE state='pending'")!="0":raise ValueError("existing test outbox did not drain")
        api_env = dict(env, GOCOVERDIR=str(api_cov))
        api = start("bookstore-api", ["-listen", api_url.removeprefix("http://"), "-run-for", "18s"], api_env)
        until(lambda: http(api_url + "/readyz")[0] == 204)
        status, raw = http(os.getenv("PANGOLIN_M1_TOKEN_URL",env["PANGOLIN_ISSUER"]+"/token"), body={"subject":"alice", "tenant_id":"tenant-a"})
        if status != 200: raise ValueError("IdP token failed")
        token = json.loads(raw)["access_token"]
        book_id = uuid.uuid4().hex
        status, raw = http(api_url + "/books", token, {"id":book_id, "title":"M1 process crash/replay"})
        if status != 201 or json.loads(raw)["id"] != book_id: raise ValueError("HTTP write did not commit")
        if sql("SELECT count(*) FROM pangolin.books WHERE tenant_id='tenant-a' AND id='" + book_id + "'") != "1": raise ValueError("2xx before commit")
        for headers, expected in [({"X-Tenant-ID":"tenant-b"},403), ({"X-Scope-Kind":"org.organization"},403)]:
            if http(api_url + "/books/" + book_id, token, headers=headers)[0] != expected: raise ValueError("cross-tenant/scope hint accepted")
        if http(api_url + "/books", "forged-token", {"title":"forged"})[0] != 401: raise ValueError("invalid JWT accepted")
        event_id = sql("SELECT event_id FROM pangolin.outbox WHERE convert_from(envelope,'UTF8')::jsonb->'payload'->>'id'='" + book_id + "'")
        crash = start("crash-worker", [], crash_env)
        if crash.wait(timeout=10) != 17: raise ValueError("publish crash fault did not fire")
        if "crash-after-ack" not in (output / "crash-worker.log").read_text(encoding="utf-8"): raise ValueError("missing real publish acknowledgement")
        # A real worker restarts with the same durable consumer, reclaims the
        # expired lease and preserves EventID. JetStream dedup/Inbox are separate.
        worker_env = dict(crash_env, GOCOVERDIR=str(worker_cov))
        worker = start("bookstore-worker", ["-run-for", "7s"], worker_env)
        until(lambda: sql("SELECT count(*) FROM pangolin.book_views WHERE tenant_id='tenant-a' AND id='" + book_id + "'") == "1")
        if worker.wait(timeout=12) != 0: raise ValueError("consumer process failed")
        if sql("SELECT state||':'||attempts FROM pangolin.outbox WHERE event_id='" + event_id + "'") != "sent:2": raise ValueError("published event was not reclaimed with the same ID")
        if sql("SELECT count(*) FROM pangolin.inbox WHERE event_id='" + event_id + "' AND processed") != "1": raise ValueError("inbox duplicate or missing terminal state")
        if api.wait(timeout=22) != 0: raise ValueError("producer process failed")
        for path in (api_cov,worker_cov):
            if not list(path.glob("covmeta.*")) or not list(path.glob("covcounters.*")): raise ValueError("missing E2E coverage instrumentation")
        execute(["go", "tool", "covdata", "textfmt", "-i=" + str(api_cov) + "," + str(worker_cov), "-o=" + str(output / "e2e.out")])
        return {"producer_consumer_processes":True,"signed_jwt":True,"commit_before_2xx":True,"cross_tenant_rejected":True,"crash_after_broker_ack":True,"event_id":event_id,"book_id":book_id,"outbox_attempts":2,"inbox_processed":1}
    finally:
        for process in children:
            if process.poll() is None:
                process.terminate()
                try: process.wait(timeout=5)
                except subprocess.TimeoutExpired: process.kill(); process.wait(timeout=5)
        for log in logs: log.close()
