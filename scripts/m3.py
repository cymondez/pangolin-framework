"""M3 cumulative conformance gate. Pending cases cannot become Verified."""
import argparse
import json
import os
import m0
import m1
import m2
import m3_fixture

ROOT=m0.ROOT
def validate_mapping(mapping):
    expected={s["id"] for g in m0.load(ROOT/"tests/contract/subtests.json")["groups"] if g["milestone"]=="M3" for s in g["subtests"]}
    sets=[set(mapping[k]) for k in ("unit_tests","integration_tests","python_tests")]
    if mapping["milestone"]!="M3" or mapping["baseline"]!="v1.3" or set.union(*sets)!=expected or sum(map(len,sets))!=len(expected):
        raise ValueError("M3 acceptance mapping incomplete or duplicated")
    if mapping.get("pending") or any(not test for key in ("unit_tests","integration_tests","python_tests") for test in mapping[key].values()):
        raise ValueError("M3 still has unimplemented acceptance cases")

def validate_report(report,commit,directory,mapping):
    m1.validate_report(report,commit,directory)
    if report["milestone"]!="M3" or not {"m2-regression","m3-regression"}<={c["name"] for c in report["commands"]}:raise ValueError("M3 cumulative regression missing")
    for extra in (m0.load(ROOT/"tests/contract/m2.json"),mapping):
        for field,key in (("tests","unit_tests"),("integration_tests","integration_tests")):
            if not set(extra[key].values())<=set(report[field]):raise ValueError("required cumulative case evidence missing")
        path="tests/contract/"+extra["milestone"].lower()+".json"
        if report["inputs"].get(path)!=m0.digest(ROOT/path):raise ValueError("acceptance mapping changed")

def gate(commit,mapping):
    root=ROOT/"artifacts"/commit/"m3";reports={}
    for label in ("windows-amd64","linux-amd64"):
        directory=root/label;report=m0.load(directory/"report.json");validate_report(report,commit,directory,mapping)
        if report["platform"]["GOOS"]+"-"+report["platform"]["GOARCH"]!=label:raise ValueError("wrong evidence platform")
        reports[label]=report
    if reports["windows-amd64"]["source_sha256"]!=reports["linux-amd64"]["source_sha256"]:raise ValueError("different source trees")
    result={"milestone":"M3","status":"verified","source_commit":commit,"source_sha256":reports["windows-amd64"]["source_sha256"],"baseline":"v1.3","named_subtests":185,"reports":{label:m0.digest(root/label/"report.json") for label in reports}}
    (root/"gate.json").write_text(json.dumps(result,indent=2)+"\n");print(json.dumps(result,indent=2))

def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument("action",choices=["verify","gate"]);parser.add_argument("--project");parser.add_argument("--commit");parser.add_argument("--race",action="store_true");parser.add_argument("--external-broker-fixture",action="store_true");args=parser.parse_args()
    mapping=m0.load(ROOT/"tests/contract/m3.json");validate_mapping(mapping)
    if args.action=="gate":
        if not args.commit or len(args.commit)!=40:parser.error("gate requires --commit <full SHA>")
        gate(args.commit,mapping);return 0
    if not args.project:parser.error("verify requires --project")
    if args.external_broker_fixture:
        if not os.getenv("PANGOLIN_M3_BROKER_RESTART_DIR"):parser.error("external broker fixture requires barrier directory")
        return m1.verify(args,increment=mapping)
    with m3_fixture.broker_restart(args.project,ROOT/"artifacts"/"m3-broker-barrier") as directory:
        os.environ["PANGOLIN_M3_BROKER_RESTART_DIR"]=str(directory)
        return m1.verify(args,increment=mapping)

if __name__=="__main__":raise SystemExit(main())
