"""M2 cumulative real-service gate; M1 evidence rules remain mandatory."""
import argparse
import json
import m0
import m1

ROOT=m0.ROOT

def validate_mapping(mapping):
    expected={s["id"] for g in m0.load(ROOT/"tests/contract/subtests.json")["groups"] if g["milestone"]=="M2" for s in g["subtests"]}
    sets=[set(mapping[k]) for k in ("unit_tests","integration_tests","python_tests")]
    if mapping["milestone"]!="M2" or mapping["baseline"]!="v1.3" or set.union(*sets)!=expected or sum(map(len,sets))!=len(expected):
        raise ValueError("M2 acceptance mapping incomplete or duplicated")
    if any(not v for key in ("unit_tests","integration_tests","python_tests") for v in mapping[key].values()):raise ValueError("empty case mapping")

def validate_report(report,commit,directory,mapping):
    m1.validate_report(report,commit,directory)
    if report["milestone"]!="M2" or "m2-regression" not in {x["name"] for x in report["commands"]}:
        raise ValueError("M2 cumulative regression missing")
    for field,key in (("tests","unit_tests"),("integration_tests","integration_tests")):
        if not set(mapping[key].values())<=set(report[field]):raise ValueError("M2 required test evidence missing")
    if report["inputs"].get("tests/contract/m2.json")!=m0.digest(ROOT/"tests/contract/m2.json"):raise ValueError("M2 mapping changed")

def gate(commit,mapping):
    root=ROOT/"artifacts"/commit/"m2";reports={}
    for label in ("windows-amd64","linux-amd64"):
        directory=root/label;report=m0.load(directory/"report.json");validate_report(report,commit,directory,mapping)
        if report["platform"]["GOOS"]+"-"+report["platform"]["GOARCH"]!=label:raise ValueError("wrong evidence platform")
        reports[label]=report
    if reports["windows-amd64"]["source_sha256"]!=reports["linux-amd64"]["source_sha256"]:raise ValueError("different source trees")
    result={"milestone":"M2","status":"verified","source_commit":commit,"source_sha256":reports["windows-amd64"]["source_sha256"],"baseline":"v1.3","named_subtests":sum(len(mapping[k]) for k in ("unit_tests","integration_tests","python_tests")),"reports":{label:m0.digest(root/label/"report.json") for label in reports}}
    (root/"gate.json").write_text(json.dumps(result,indent=2)+"\n");print(json.dumps(result,indent=2))

def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument("action",choices=["verify","gate"]);parser.add_argument("--project");parser.add_argument("--commit");parser.add_argument("--race",action="store_true");args=parser.parse_args()
    mapping=m0.load(ROOT/"tests/contract/m2.json");validate_mapping(mapping)
    if args.action=="gate":
        if not args.commit or len(args.commit)!=40:parser.error("gate requires --commit <full SHA>")
        gate(args.commit,mapping);return 0
    if not args.project:parser.error("verify requires --project")
    return m1.verify(args,increment=mapping)

if __name__=="__main__":raise SystemExit(main())
