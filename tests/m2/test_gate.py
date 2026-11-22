"""M2 structural contracts and mutation checks for cumulative evidence."""
import copy
import json
from pathlib import Path
import re
import sys
import unittest

ROOT=Path(__file__).resolve().parents[2]
sys.path.insert(0,str(ROOT/"scripts"))
import m0
import m1
import m2

class M2Gates(unittest.TestCase):
    def test_mapping_rejects_missing_and_duplicate(self):
        mapping=m0.load(ROOT/"tests/contract/m2.json");m2.validate_mapping(mapping);m1.check_module_docs(mapping)
        altered=copy.deepcopy(mapping);key=next(iter(altered["unit_tests"]));del altered["unit_tests"][key]
        with self.assertRaises(ValueError):m2.validate_mapping(altered)
        altered=copy.deepcopy(mapping);altered["integration_tests"][key]="duplicate"
        with self.assertRaises(ValueError):m2.validate_mapping(altered)

    def test_A82_real_import_graph(self):
        packages=m0.json_stream(m0.command(["go","list","-json","./..."]));m0.check_imports(packages)
        dependencies=m0.command(["go","list","-deps","./internal/testkit/assemblies/tenant","./examples/bookstore/cmd/api"])
        self.assertNotIn(m0.MODULE+"/modules/organization",dependencies)
        m0.command(["go","build","./internal/testkit/assemblies/tenant","./internal/testkit/assemblies/organization","./internal/testkit/assemblies/replacement"])
        for area in ("kernel/runtime","persistence/uow","security/scope","events/distributed"):
            with self.subTest(area=area),self.assertRaises(ValueError):m0.check_imports([{"ImportPath":m0.MODULE+"/"+area,"Imports":[m0.MODULE+"/modules/organization"]}])

    def test_A27_no_public_transaction_control(self):
        surface=m0.command(["go","doc","-all","./persistence/uow"])
        self.assertNotRegex(surface,r"func\s+(?:\([^)]*\)\s*)?(?:Savepoint|Suppress)\b")
        contract=(ROOT/"persistence/uow/contracts.go").read_text(encoding="utf-8-sig")
        scope=contract.split("type Scope interface {",1)[1].split("}",1)[0]
        self.assertNotRegex(scope,r"\b(?:Commit|Rollback|Close|Complete|Savepoint|Suppress)\s*\(")

    def test_missing_execution_or_skip_is_not_evidence(self):
        event=json.dumps({"Action":"pass","Package":"pkg","Test":"TestOne"})
        with self.assertRaises(ValueError):m1.validate_tests(event,["pkg:TestMissing"])
        with self.assertRaises(ValueError):m1.validate_tests(event.replace('"pass"','"skip"'))

if __name__=="__main__":unittest.main()
