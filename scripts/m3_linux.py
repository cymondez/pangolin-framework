"""Run the fixed Linux/amd64 M3 gate and its host-owned restart fixture."""
import argparse
import json
import os
import re
import subprocess
import m0
import m1
import m3_fixture

def main(milestone=3):
    p=argparse.ArgumentParser(description=__doc__);p.add_argument("--project",required=True);args=p.parse_args()
    project=args.project
    if milestone not in (3,4) or not re.fullmatch(r"pangolin-m"+str(milestone)+r"-[a-z0-9-]+",project):p.error("dedicated milestone project required")
    image=m0.load(m0.ROOT/"deploy/toolchain.json")["go_image"]
    network=project+"_default";cache=project+"-linux-cache"
    # Named cache belongs to this test project and is removed by its cleanup.
    purpose="dev.pangolin.purpose=m"+str(milestone)+"-test-runner"
    subprocess.run(["docker","volume","create","--label","dev.pangolin.owner=pangolin-framework","--label",purpose,cache],check=True)
    with m3_fixture.broker_restart(project,m0.ROOT/"artifacts"/("m"+str(milestone)+"-linux-broker-barrier"),milestone=milestone):
        environment={
            "PANGOLIN_EXPECTED_SOURCE":m1.source_hash(),"PANGOLIN_M3_PROJECT":project,
            "PANGOLIN_MILESTONE":str(milestone),
            "PANGOLIN_M1_ADMIN_DSN":"postgres://pangolin_bootstrap:local-bootstrap-only@postgres:5432/pangolin_m0?sslmode=disable",
            "PANGOLIN_M1_APP_DSN":"postgres://pangolin_app:local-app-only@postgres:5432/pangolin_m0?sslmode=disable",
            "PANGOLIN_M1_WORKER_DSN":"postgres://pangolin_worker:local-worker-only@postgres:5432/pangolin_m0?sslmode=disable",
            "PANGOLIN_M1_NATS_URL":"nats://nats:4222","PANGOLIN_M1_ISSUER":os.getenv("PANGOLIN_M1_ISSUER","http://127.0.0.1:18180"),
            "PANGOLIN_M1_TOKEN_URL":"http://test-idp:8080/token","PANGOLIN_JWKS_URL":"http://test-idp:8080/jwks.json",
            "PANGOLIN_M3_BROKER_RESTART_DIR":"/source/artifacts/m"+str(milestone)+"-linux-broker-barrier",
            "CGO_ENABLED":"1","GOTOOLCHAIN":"local",
        }
        command=["docker","run","--rm","--name",project+"-linux","--cpus","2","--memory","2g","--network",network,
                 "--label","dev.pangolin.owner=pangolin-framework","--label",purpose,
                 "-v",str(m0.ROOT)+":/source","-v",str(m0.ROOT/".cache/go-mod")+":/gomod", "-v",cache+":/cache"]
        for key,value in environment.items():command.extend(["-e",key+"="+value])
        command.extend([image,"python3","/source/scripts/m3_linux_entry.py"])
        return subprocess.call(command)
if __name__=="__main__":raise SystemExit(main())
