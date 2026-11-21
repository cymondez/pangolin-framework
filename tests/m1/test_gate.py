"""Regression guards for M1 engineering gates, never substitute for real services."""
import copy
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[2]
sys.path.insert(0,str(ROOT/"scripts"))
import dependencies
import m0
import m1


class EngineeringGates(unittest.TestCase):
    def test_A45_real_import_graph_injection(self):
        with tempfile.TemporaryDirectory(dir=ROOT/".cache") as directory:
            root=Path(directory)
            (root/"go.mod").write_text("module "+m0.MODULE+"/fixture\n\ngo 1.27.0\n")
            domain=root/"domain";domain.mkdir()
            source=domain/"domain.go"
            for imported,allowed in [("context",True),("net/http",False),("context",True)]:
                source.write_text('package domain\nimport _ "'+imported+'"\n')
                packages=m0.json_stream(m0.command(["go","list","-json","./..."],root))
                if allowed: m0.check_imports(packages)
                else:
                    with self.assertRaises(ValueError):m0.check_imports(packages)
            infrastructure=root/"infrastructure";infrastructure.mkdir();(infrastructure/"store.go").write_text("package infrastructure\ntype Store struct{}\n")
            application=root/"application";application.mkdir();(application/"app.go").write_text('package application\nimport _ "'+m0.MODULE+'/fixture/infrastructure"\n')
            with self.assertRaises(ValueError):m0.check_imports(m0.json_stream(m0.command(["go","list","-json","./..."],root)))
            for imported in (m0.MODULE+"/examples/bookstore/infrastructure",m0.MODULE+"/events/nats"):
                with self.subTest(imported=imported),self.assertRaises(ValueError):m0.check_imports([{"ImportPath":m0.MODULE+"/examples/bookstore/application","Imports":[imported]}])

    def test_A69_failure_skip_and_missing_case(self):
        event={"Action":"pass","Package":"pkg","Test":"TestRequired"}
        self.assertIn("pkg:TestRequired",m1.validate_tests(json.dumps(event),["pkg:TestRequired"]))
        for action in ("fail","skip"):
            with self.subTest(action=action),self.assertRaises(ValueError):m1.validate_tests(json.dumps(dict(event,Action=action)))
        for text in ("",json.dumps(event)):
            with self.subTest(text=text),self.assertRaises(ValueError):m1.validate_tests(text,["pkg:TestMissing"])
        m1.validate_python_output("Ran 6 tests in 0.1s\n\nOK\n")
        for text in ("Ran 0 tests in 0s\n\nOK\n","Ran 6 tests in 0s\n\nOK (skipped=1)\n",""):
            with self.subTest(text=text),self.assertRaises(ValueError):m1.validate_python_output(text)

    def test_A69_independent_gates(self):
        m1.check_module_docs(m0.load(ROOT/"tests/contract/m1.json"))
        with self.assertRaises(ValueError):m1.check_module_docs({"module_docs":["missing-module/README.md"]})
        names=["inventory","build","vet","go-module-verify","unit","m0-regression","m1-regression","integration","e2e","coverage","generation","race"]
        commands=[{"name":name,"exit_code":0} for name in names]
        m1.validate_commands(commands,True)
        for name in names:
            altered=copy.deepcopy(commands);next(c for c in altered if c["name"]==name)["exit_code"]=1
            with self.subTest(name=name),self.assertRaises(ValueError):m1.validate_commands(altered,True)
        with self.assertRaises(ValueError):m1.validate_commands(commands[:-1],True)

    def test_A69_dependency_license_mutation(self):
        reviewed=m0.load(ROOT/"deploy/dependencies.json")
        self.assertEqual(len(reviewed["modules"]),dependencies.check()["locked_modules"])
        actual=dependencies.inventory()
        self.assertTrue(all(x["sum"] and x["license_sha256"] for x in actual))
        for key in ("version","sum","license_sha256","license"):
            altered=copy.deepcopy(reviewed);altered["modules"][0][key]="unreviewed"
            with self.subTest(key=key),self.assertRaises(ValueError):dependencies.validate(altered,actual)

    def test_A71_coverage_union_metadata_and_previous_case(self):
        with tempfile.TemporaryDirectory(dir=ROOT/".cache") as directory:
            root=Path(directory);a=root/"a.out";b=root/"b.out";merged=root/"merged.out"
            a.write_text("mode: atomic\nx/a.go:1.1,2.1 2 1\nx/a.go:3.1,4.1 8 0\n")
            b.write_text("mode: atomic\nx/a.go:1.1,2.1 2 2\nx/a.go:3.1,4.1 8 1\n")
            m1.merge_profiles([a,b],merged)
            self.assertEqual(len(merged.read_text().splitlines()),3)
            b.write_text("mode: atomic\nx/a.go:1.1,2.1 3 1\n")
            with self.assertRaises(ValueError):m1.merge_profiles([a,b],merged)
            b.write_text("")
            with self.assertRaises(ValueError):m1.merge_profiles([a,b],merged)
        previous={"Action":"pass","Package":"old","Test":"TestPrevious"}
        new={"Action":"pass","Package":"new","Test":"TestNew"}
        m1.validate_tests(json.dumps(previous)+"\n"+json.dumps(new),["old:TestPrevious","new:TestNew"])
        with self.assertRaises(ValueError):m1.validate_tests(json.dumps(new),["old:TestPrevious","new:TestNew"])

    def test_A71_same_commit_and_artifact_integrity(self):
        with tempfile.TemporaryDirectory(dir=ROOT/".cache") as directory:
            root=Path(directory)
            names=["unit.out","integration.out","e2e.out","merged.out","coverage.json","coverage.html","go-list.json","exclusions.json","dependencies.json","command.log"]
            for name in names:(root/name).write_text("fixture evidence\n")
            artifacts={name:m0.digest(root/name) for name in names}
            commands=[{"name":name,"exit_code":0,"log":"command.log","sha256":artifacts["command.log"]} for name in ("inventory","build","vet","go-module-verify","unit","m0-regression","m1-regression","integration","e2e","coverage","generation")]
            e2e={k:True for k in ("producer_consumer_processes","signed_jwt","commit_before_2xx","cross_tenant_rejected","crash_after_broker_ack")};e2e.update(outbox_attempts=2,inbox_processed=1)
            report={"commit":"a"*40,"baseline":"v1.3","working_tree_dirty":False,"status":"passed","platform":{"GOOS":"windows"},"commands":commands,"e2e":e2e,"artifacts":artifacts,"tests":list(m0.load(ROOT/"tests/contract/m1.json")["go_tests"].values())}
            m1.validate_report(report,"a"*40,root)
            for key,value in (("commit","b"*40),("working_tree_dirty",True),("status","failed"),("baseline","wrong"),("tests",[])):
                changed=copy.deepcopy(report);changed[key]=value
                with self.subTest(key=key),self.assertRaises(ValueError):m1.validate_report(changed,"a"*40,root)
            (root/"unit.out").write_text("tampered")
            with self.assertRaises(ValueError):m1.validate_report(report,"a"*40,root)


if __name__=="__main__":unittest.main()
