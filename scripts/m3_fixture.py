"""Host-side file barrier for the isolated M3 NATS restart acceptance case."""
import contextlib
from pathlib import Path
import re
import subprocess
import threading
import time

import m0

@contextlib.contextmanager
def broker_restart(project, directory):
    if not re.fullmatch(r"pangolin-m3-[a-z0-9-]+",project or ""):
        raise ValueError("M3 broker restart requires a dedicated pangolin-m3-* project")
    directory=Path(directory).resolve();directory.mkdir(parents=True,exist_ok=True)
    if not directory.is_relative_to((m0.ROOT/"artifacts").resolve()):
        raise ValueError("restart barrier must stay inside workspace artifacts")
    # Verify ownership before accepting a mutation request.
    result=subprocess.run(["docker","compose","-p",project,"-f",str(m0.ROOT/"deploy/compose/m1.yaml"),"ps","-q","nats"],capture_output=True,text=True,check=True)
    identifier=result.stdout.strip()
    if not identifier: raise ValueError("isolated NATS fixture not running")
    inspect=subprocess.run(["docker","inspect","--format",'{{index .Config.Labels "com.docker.compose.project"}}',identifier],capture_output=True,text=True,check=True)
    if inspect.stdout.strip()!=project:raise ValueError("NATS project ownership mismatch")
    stop=threading.Event()
    previous={p.name for p in directory.glob("*.request")}
    def serve():
        seen=set(previous)
        while not stop.wait(.05):
            for request in directory.glob("*.request"):
                if request.name in seen: continue
                if not re.fullmatch(r"[a-zA-Z0-9_-]+\.request",request.name):continue
                if request.read_text().strip()!="RESTART_NATS":continue
                seen.add(request.name)
                result=subprocess.run(["docker","restart",identifier],capture_output=True,text=True,timeout=30)
                # Allow the process to reopen its durable store/listener.
                stop.wait(.5)
                response=request.with_suffix(".done")
                temporary=response.with_suffix(".tmp")
                temporary.write_text("ok" if result.returncode==0 else "failed")
                temporary.replace(response)
    thread=threading.Thread(target=serve,daemon=True);thread.start()
    try: yield directory
    finally:stop.set();thread.join(timeout=35)
