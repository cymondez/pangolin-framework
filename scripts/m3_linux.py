"""Run the fixed Linux/amd64 M3 gate and its host-owned restart fixture."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import m0
import m1
import m3_fixture

def main(milestone=3):
    p=argparse.ArgumentParser(description=__doc__);p.add_argument("--project",required=True);args=p.parse_args()
    project=args.project
    if milestone not in (3,4,5,6,7) or not re.fullmatch(r"pangolin-m"+str(milestone)+r"-[a-z0-9-]+",project):p.error("dedicated milestone project required")
    image=m0.load(m0.ROOT/"deploy/toolchain.json")["go_image"]
    runner_id=None
    if milestone==6:
        # Both inputs are immutable image digests. Native PostgreSQL clients
        # allow the complete integration/race suites to perform real restores
        # without granting the runner a Docker socket or adopting host results.
        image=project+"-runner"
        subprocess.run(["docker","build","-f",str(m0.ROOT/"deploy/docker/Dockerfile.m6-runner"),
                        "--label","dev.pangolin.source-commit="+m0.revision(),"-t",image,str(m0.ROOT)],check=True)
        runner_id=subprocess.check_output(["docker","image","inspect",image,"--format","{{.Id}}"],text=True).strip()
    elif milestone==7:
        # The M7 runner adds pinned Node.js to the exact M6 toolchain set so
        # the official @microsoft/signalr client harness runs inside the fixed
        # OCI environment; Node/npm versions are recorded by the M7 gate.
        image=project+"-runner"
        subprocess.run(["docker","build","-f",str(m0.ROOT/"deploy/docker/Dockerfile.m7-runner"),
                        "--label","dev.pangolin.source-commit="+m0.revision(),"--label","dev.pangolin.purpose=m7-test-runner",
                        "-t",image,str(m0.ROOT)],check=True)
        runner_id=subprocess.check_output(["docker","image","inspect",image,"--format","{{.Id}}"],text=True).strip()
    network=project+"_default";cache=project+"-linux-cache"
    # Named cache belongs to this test project and is removed by its cleanup.
    purpose="dev.pangolin.purpose=m"+str(milestone)+"-test-runner"
    subprocess.run(["docker","volume","create","--label","dev.pangolin.owner=pangolin-framework","--label",purpose,cache],check=True)
    # The fixed OCI runner has no Docker socket, so M6 drills must have run on
    # this host against the same source bytes; only provenance is checked in.
    host_drills=os.getenv("PANGOLIN_M6_DRILLS_DIR")
    if milestone==6 and host_drills:
        drills=Path(host_drills).resolve()
        if not drills.is_relative_to(m0.ROOT) or not (drills/"release-drills.json").is_file():
            p.error("host M6 drills require a release-drills.json inside this repository tree")
        host_drills="/source/"+drills.relative_to(m0.ROOT).as_posix()
    if milestone==7:
        # The pinned OCI runner has no Docker socket or host privileges, and a
        # same-source release drill can never be re-executed inside it; the
        # host must run the five drills against the same source bytes first.
        if not host_drills:
            p.error("M7 Linux gate requires host-run drills (PANGOLIN_M6_DRILLS_DIR) executed on this host")
        if not os.getenv("PANGOLIN_M7_DEMO_KEY"):
            p.error("M7 Linux gate requires PANGOLIN_M7_DEMO_KEY for the official-client harness")
        drills=Path(host_drills).resolve()
        if not drills.is_relative_to(m0.ROOT) or not (drills/"release-drills.json").is_file():
            p.error("host M7 drills require a release-drills.json inside this repository tree")
        host_drills="/source/"+drills.relative_to(m0.ROOT).as_posix()
    with m3_fixture.broker_restart(project,m0.ROOT/"artifacts"/("m"+str(milestone)+"-linux-broker-barrier"),milestone=milestone):
        environment={
            "PANGOLIN_EXPECTED_SOURCE":m1.source_hash(),"PANGOLIN_M3_PROJECT":project,
            "PANGOLIN_MILESTONE":str(milestone),
            "PANGOLIN_M1_ADMIN_DSN":"postgres://pangolin_bootstrap:local-bootstrap-only@postgres:5432/pangolin_m0?sslmode=disable",
            "PANGOLIN_M1_APP_DSN":"postgres://pangolin_app:local-app-only@postgres:5432/pangolin_m0?sslmode=disable",
            "PANGOLIN_M1_WORKER_DSN":"postgres://pangolin_worker:local-worker-only@postgres:5432/pangolin_m0?sslmode=disable",
            "PANGOLIN_M1_NATS_URL":"nats://nats:4222",
            # The IdP container signs tokens with its configured issuer URL
            # verbatim (compose default http://127.0.0.1:<idp port>), while
            # the runner fetches JWKS and fixture tokens over the compose
            # service name; authn compares the signed `iss` string with the
            # validator's issuer, so both must carry the same value here.
            "PANGOLIN_M1_ISSUER":os.getenv("PANGOLIN_M1_ISSUER","http://127.0.0.1:18180"),
            "PANGOLIN_M1_TOKEN_URL":"http://test-idp:8080/token","PANGOLIN_JWKS_URL":"http://test-idp:8080/jwks.json",
            "PANGOLIN_M3_BROKER_RESTART_DIR":"/source/artifacts/m"+str(milestone)+"-linux-broker-barrier",
            "CGO_ENABLED":"1","GOTOOLCHAIN":"local",
        }
        if milestone==6:
            environment["PANGOLIN_M6_RUNNER_IMAGE_ID"]=runner_id
            environment["PANGOLIN_M6_RUNNER_IMAGE"]=image
            if host_drills:environment["PANGOLIN_M6_DRILLS_DIR"]=host_drills
        if milestone==7:
            environment["PANGOLIN_M7_RUNNER_IMAGE_ID"]=runner_id
            environment["PANGOLIN_M7_RUNNER_IMAGE"]=image
            environment["PANGOLIN_M6_DRILLS_DIR"]=host_drills
            environment["PANGOLIN_M7_DEMO_KEY"]=os.environ["PANGOLIN_M7_DEMO_KEY"]
            # The official-client harness installs its committed package-lock
            # dependencies inside the runner; keep the npm cache on the volume.
            environment["npm_config_cache"]="/cache/npm-cache"
        if milestone>=5:
            environment["PANGOLIN_DATABASE_POSTGRES_DSN"]=environment["PANGOLIN_M1_APP_DSN"]
            environment["PANGOLIN_DATABASE_MYSQL_DSN"]="pangolin_database:local-database-only@tcp(mysql:3306)/pangolin_db_contracts"
        command=["docker","run","--rm","--name",project+"-linux","--cpus","2","--memory","2g","--network",network,
                 "--label","dev.pangolin.owner=pangolin-framework","--label",purpose,
                 "-v",str(m0.ROOT)+":/source","-v",str(m0.ROOT/".cache/go-mod")+":/gomod", "-v",cache+":/cache"]
        for key,value in environment.items():command.extend(["-e",key+"="+value])
        command.extend([image,"python3","/source/scripts/m3_linux_entry.py"])
        return subprocess.call(command)
if __name__=="__main__":raise SystemExit(main())
