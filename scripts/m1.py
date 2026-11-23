"""M1 cumulative conformance, real-service suites and same-source evidence."""
import argparse
import hashlib
import json
import os
import re
from pathlib import Path
import subprocess
import sys
import time
import traceback

import m0
import m1_e2e

ROOT=m0.ROOT


def source_hash():
    paths=m0.command(["git","-c","core.quotepath=false","ls-files","--cached","--others","--exclude-standard"]).splitlines()
    digest=hashlib.sha256()
    for name in sorted(set(paths)):
        path=ROOT/name
        if path.is_file(): digest.update(name.encode()+b"\0"+path.read_bytes())
    return digest.hexdigest()


def validate_tests(text, required=()):
    events=[json.loads(line) for line in text.splitlines() if line.startswith("{")]
    if not events or any(e.get("Action")=="fail" or e.get("Action")=="skip" and e.get("Test") for e in events):
        raise ValueError("test failure, skip or missing JSON events")
    passed={e.get("Package","")+":"+e.get("Test","") for e in events if e.get("Action")=="pass"}
    missing=set(required)-passed
    if missing: raise ValueError("missing required cases: "+", ".join(sorted(missing)))
    return sorted(passed)


def validate_python_output(text,required=()):
    count=re.search(r"Ran ([0-9]+) tests?",text)
    if not count or int(count[1])==0 or "skipped=" in text or not re.search(r"\nOK\s*$",text):raise ValueError("Python regression failed, skipped or did not run")
    if any(name not in text for name in required):raise ValueError("required Python acceptance case missing")


def validate_mapping(mapping):
    expected={s["id"] for g in m0.load(ROOT/"tests/contract/subtests.json")["groups"] if g["milestone"]=="M1" for s in g["subtests"]}
    go,python=mapping["go_tests"],mapping["python_tests"]
    if set(go)|set(python)!=expected or set(go)&set(python) or not all(go.values()) or not all(python.values()):raise ValueError("M1 acceptance mapping is incomplete or duplicated")


def check_module_docs(mapping):
    for name in mapping["module_docs"]:
        path=ROOT/name
        if not path.is_file() or not path.read_text(encoding="utf-8-sig").strip():raise ValueError("module documentation missing: "+name)
        prose=re.sub(r"```.*?```","",path.read_text(encoding="utf-8-sig"),flags=re.DOTALL)
        for link in re.findall(r"\]\(([^)]+)\)",prose):
            target=link.split("#")[0]
            if target and not target.startswith(("http:","https:","mailto:")) and not (path.parent/target).exists():raise ValueError("broken module documentation link: "+name+" -> "+target)


def merge_profiles(paths, output):
    blocks={}
    for path in paths:
        lines=path.read_text().splitlines()
        if not lines or lines[0]!="mode: atomic": raise ValueError("missing atomic suite profile")
        for line in lines[1:]:
            location,statements,count=line.rsplit(" ",2)
            statements,count=int(statements),int(count)
            if location in blocks and blocks[location][0]!=statements: raise ValueError("coverage block metadata drift")
            previous=blocks.get(location,(statements,0))
            blocks[location]=(statements,max(count,previous[1]))
    output.write_text("mode: atomic\n"+"\n".join(f"{k} {s} {c}" for k,(s,c) in sorted(blocks.items()))+"\n")


def validate_commands(commands, race):
    required={"inventory","build","vet","go-module-verify","unit","m0-regression","m1-regression","integration","e2e","coverage","generation"}
    if race: required.add("race")
    found={c["name"] for c in commands if c["exit_code"]==0}
    if not required<=found or any(c["exit_code"]!=0 for c in commands): raise ValueError("required engineering gate missing or failed")


def validate_report(report,commit,directory):
    if report["commit"]!=commit or report["working_tree_dirty"] or report["baseline"]!="v1.3" or report["status"]!="passed":raise ValueError("M1 requires passed clean same-commit evidence")
    linux=report["platform"]["GOOS"]=="linux"
    validate_commands(report["commands"],linux)
    if linux and report["platform"]["CGO_ENABLED"]!="1":raise ValueError("race evidence requires CGO")
    if report["e2e"].get("outbox_attempts")!=2 or report["e2e"].get("inbox_processed")!=1 or not all(report["e2e"].get(k) for k in ("producer_consumer_processes","signed_jwt","commit_before_2xx","cross_tenant_rejected","crash_after_broker_ack")):raise ValueError("missing M1 thin-slice evidence")
    if not {"unit.out","integration.out","e2e.out","merged.out","coverage.json","coverage.html","go-list.json","exclusions.json","dependencies.json"}<=report["artifacts"].keys():raise ValueError("required raw evidence missing")
    for name,digest in report["artifacts"].items():
        path=(directory/name).resolve()
        if not path.is_relative_to(directory.resolve()) or not path.is_file() or m0.digest(path)!=digest:raise ValueError("M1 evidence hash mismatch: "+name)
    for command in report["commands"]:
        if command["name"]=="e2e":continue
        if command.get("log") not in report["artifacts"] or command.get("sha256")!=report["artifacts"][command["log"]]:raise ValueError("command evidence missing")
    required=m0.load(ROOT/"tests/contract/m1.json")["go_tests"].values()
    if not set(required)<=set(report["tests"]):raise ValueError("required M1 case evidence missing")


def gate(commit):
    root=ROOT/"artifacts"/commit/"m1"
    reports={}
    for label in ("windows-amd64","linux-amd64"):
        directory=root/label;report=m0.load(directory/"report.json");validate_report(report,commit,directory)
        if report["platform"]["GOOS"]+"-"+report["platform"]["GOARCH"]!=label:raise ValueError("wrong evidence platform")
        reports[label]=report
    if reports["windows-amd64"]["source_sha256"]!=reports["linux-amd64"]["source_sha256"]:raise ValueError("different source trees")
    result={"milestone":"M1","status":"verified","source_commit":commit,"source_sha256":reports["windows-amd64"]["source_sha256"],"baseline":"v1.3","reports":{label:m0.digest(root/label/"report.json") for label in reports}}
    (root/"gate.json").write_text(json.dumps(result,indent=2)+"\n");print(json.dumps(result,indent=2))


def verify(args, increment=None):
    milestone=increment["milestone"] if increment else "M1"
    increments=([m0.load(ROOT/("tests/contract/m"+str(n)+".json")) for n in range(2,int(milestone[1:]))]+[increment]) if increment else []
    commit=m0.revision();source=source_hash();dirty=not m0.clean()
    platform=json.loads(m0.command(["go","env","-json","GOOS","GOARCH","GOVERSION","CGO_ENABLED"]))
    if platform["GOVERSION"]!="go"+m0.load(ROOT/"deploy/toolchain.json")["go"]:raise ValueError("unexpected toolchain")
    label=platform["GOOS"]+"-"+platform["GOARCH"]
    # Dirty development runs are inspectable but cannot become Verified evidence.
    output=ROOT/"artifacts"/(milestone.lower()+"-development-"+source[:12]+"/"+label if dirty else commit+"/"+milestone.lower()+"/"+label)
    output.mkdir(parents=True,exist_ok=True)
    if int(milestone[1:])>=4:os.environ["PANGOLIN_M4_METRICS_DIR"]=str(output)
    report={"milestone":milestone,"commit":commit,"source_sha256":source,"working_tree_dirty":dirty,"baseline":"v1.3","platform":platform,"seed":20261121,"project":args.project,"status":"failed","commands":[]}
    mapping=m0.load(ROOT/"tests/contract/m1.json");validate_mapping(mapping)
    check_module_docs(mapping)
    for extra in increments: check_module_docs(extra)
    report["inputs"]={name:m0.digest(ROOT/name) for name in ("VERSION","go.mod","go.sum","deploy/toolchain.json","deploy/profiles/local.json","deploy/dependencies.json","tests/fixtures/manifest.json","tests/contract/api.json","tests/contract/m1.json")}
    for extra in increments:
        name="tests/contract/"+extra["milestone"].lower()+".json"
        report["inputs"][name]=m0.digest(ROOT/name)
    def execute(argv,name=None):
        index=len(report["commands"]);name=name or "e2e-step";log=output/f"{index:02d}-{name}.log";started=time.monotonic()
        result=subprocess.run(argv,cwd=ROOT,env=m0.go_environment(ROOT),capture_output=True,text=True,encoding="utf-8",errors="replace")
        log.write_text(result.stdout+result.stderr,encoding="utf-8")
        report["commands"].append({"name":name,"argv":argv,"exit_code":result.returncode,"log":log.name,"sha256":m0.digest(log),"seconds":round(time.monotonic()-started,3)})
        if result.returncode: raise ValueError("gate failed: "+name+"; see "+str(log))
        if name in {"m0-regression","m1-regression","m2-regression","m3-regression","m4-regression","m5-regression"}:
            required=mapping["python_tests"].values() if name=="m1-regression" else next((x["python_tests"].values() for x in increments if name==x["milestone"].lower()+"-regression"),())
            validate_python_output(result.stdout+result.stderr,required)
        print("PASS",name,flush=True)
        return result.stdout
    try:
        execute([sys.executable,"scripts/m0.py","check"],"inventory")
        (output/"go-list.json").write_text(m0.command(["go","list","-json","./..."]),encoding="utf-8")
        (output/"exclusions.json").write_bytes((ROOT/"tests/coverage/exclusions.json").read_bytes())
        (output/"dependencies.json").write_bytes((ROOT/"deploy/dependencies.json").read_bytes())
        execute(["go","build","./..."],"build")
        execute(["go","vet","./..."],"vet")
        execute(["go","mod","verify"],"go-module-verify")
        execute([sys.executable,"-m","unittest","discover","-v","-s","tests/m0","-p","test_*.py"],"m0-regression")
        execute([sys.executable,"-m","unittest","discover","-v","-s","tests/m1","-p","test_*.py"],"m1-regression")
        for extra in increments:
            name=extra["milestone"].lower()
            execute([sys.executable,"-m","unittest","discover","-v","-s","tests/"+name,"-p","test_*.py"],name+"-regression")
        packages=",".join(p for p in m0.command(["go","list","./..."]).splitlines() if "/internal/testkit/" not in p and "/infrastructure/internal/" not in p)
        unit=execute(["go","test","-count=1","-json","-covermode=atomic","-coverpkg="+packages,"-coverprofile="+str(output/"unit.out"),"./..."],"unit")
        report["tests"]=validate_tests(unit,[*mapping["go_tests"].values(),*(test for extra in increments for test in extra["unit_tests"].values())])
        integration=execute(["go","test","-p=1","-count=1","-json","-tags=integration","-covermode=atomic","-coverpkg="+packages,"-coverprofile="+str(output/"integration.out"),"./tests/m1",*("./tests/"+extra["milestone"].lower() for extra in increments)],"integration")
        report["integration_tests"]=validate_tests(integration,[m0.MODULE+"/tests/m1:TestM1Flow",*(test for extra in increments for test in extra["integration_tests"].values())])
        report["e2e"]=m1_e2e.run(output,args.project,packages,execute)
        report["commands"].append({"name":"e2e","exit_code":0})
        profiles=[output/"unit.out",output/"integration.out",output/"e2e.out"]
        if milestone=="M5":
            execute([sys.executable,"scripts/template_bundle.py","--check"],"template-source")
            cli=output/("pangolin.exe" if os.name=="nt" else "pangolin")
            execute(["go","build","-cover","-coverpkg="+packages,"-o",str(cli),"./cmd/pangolin"],"cli-build")
            covered=output/"cli-cov";covered.mkdir(exist_ok=True)
            previous=os.environ.get("GOCOVERDIR");os.environ["GOCOVERDIR"]=str(covered)
            try:execute([sys.executable,"scripts/m5_templates.py","--output",str(output),"--cli",str(cli)],"templates")
            finally:
                if previous is None:os.environ.pop("GOCOVERDIR",None)
                else:os.environ["GOCOVERDIR"]=previous
            execute(["go","tool","covdata","textfmt","-i="+str(covered),"-o="+str(output/"cli.out")],"cli-coverage")
            profiles.append(output/"cli.out")
            if os.name=="nt" or os.getenv("PANGOLIN_M5_COMPOSE")=="1":
                execute([sys.executable,"scripts/m5_compose.py","--output",str(output),"--project",args.project],"templates-compose")
        if args.race:
            race=execute(["go","test","-p=1","-count=1","-json","-race","-tags=integration","./..."],"race");validate_tests(race)
        sqlc=os.getenv("PANGOLIN_SQLC",str(ROOT/".cache/bin"/("sqlc.exe" if os.name=="nt" else "sqlc")))
        if execute([sqlc,"version"],"generation-version").strip()!="v1.31.1":raise ValueError("wrong sqlc version")
        generated=list((ROOT/"examples/bookstore/infrastructure/internal").glob("*/*.go"));before={str(p.relative_to(ROOT)):m0.digest(p) for p in generated}
        execute([sqlc,"generate"],"generation")
        if before!={str(p.relative_to(ROOT)):m0.digest(p) for p in generated}:raise ValueError("sqlc generated source drift")
        merge_profiles(profiles,output/"merged.out")
        counts=execute(["go","run","./tools/check","-coverage",str(output/"merged.out")],"coverage")
        (output/"coverage.json").write_text(json.dumps(json.loads(counts.splitlines()[0]),indent=2)+"\n",encoding="utf-8")
        execute(["go","tool","cover","-html="+str(output/"merged.out"),"-o="+str(output/"coverage.html")],"coverage-html")
        validate_commands(report["commands"],args.race)
        if source_hash()!=source or m0.revision()!=commit:raise ValueError("source changed during verification")
        report["status"]="passed"
        report["gate"]="development-only" if dirty or platform["GOOS"]=="linux" and not args.race else "evidence-ready"
    except (ValueError,OSError,subprocess.SubprocessError) as error:
        report["error"]=str(error)
        (output/"failure.log").write_text(traceback.format_exc(),encoding="utf-8")
    report["artifacts"]={str(p.relative_to(output)):m0.digest(p) for p in output.rglob("*") if p.is_file() and p.name!="report.json"}
    (output/"report.json").write_text(json.dumps(report,indent=2)+"\n")
    print(json.dumps({"status":report["status"],"error":report.get("error"),"report":str(output/"report.json")},indent=2),flush=True)
    return 0 if report["status"]=="passed" else 1


def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument("action",choices=["verify","gate"]);parser.add_argument("--project");parser.add_argument("--commit");parser.add_argument("--race",action="store_true");args=parser.parse_args()
    if args.action=="gate":
        if not args.commit or len(args.commit)!=40:parser.error("gate requires --commit <full SHA>")
        gate(args.commit);return 0
    if not args.project:parser.error("verify requires --project")
    return verify(args)

if __name__=="__main__":raise SystemExit(main())
