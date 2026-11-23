"""M5 gate rejects missing, skipped, stale-template and incomplete profile evidence."""
import copy
import json
from pathlib import Path
import sys
import unittest
ROOT=Path(__file__).resolve().parents[2]
sys.path.insert(0,str(ROOT/'scripts'))
import m0,m1,m1_e2e,m5,m5_templates,template_openapi,template_bundle

class M5Gates(unittest.TestCase):
    def test_mapping_and_docs(self):
        mapping=m0.load(ROOT/'tests/contract/m5.json');m5.validate_mapping(mapping);m1.check_module_docs(mapping)
        self.assertEqual(sum(len(mapping[k]) for k in m5.FIELDS),25)
    def test_missing_duplicate_pending_blank(self):
        original=m0.load(ROOT/'tests/contract/m5.json');key=next(iter(original['unit_tests']))
        for mode in ('missing','duplicate','pending','blank'):
            mapping=copy.deepcopy(original)
            if mode=='missing':del mapping['unit_tests'][key]
            elif mode=='duplicate':mapping['template_tests'][key]='duplicate'
            elif mode=='pending':mapping['pending']=[key]
            else:mapping['unit_tests'][key]=''
            with self.assertRaises(ValueError):m5.validate_mapping(mapping)
    def test_required_skip_fails(self):
        for event in ({'Action':'skip','Test':'T'},{'Action':'fail'},{'Action':'pass','Package':'p','Test':'other'}):
            with self.assertRaises(ValueError):m1.validate_tests(json.dumps(event),['p:T'])
    def test_openapi_contract(self):
        for org in (False,True):
            document=template_openapi.document(org);m5_templates.validate_openapi(document,org)
            del document['paths']['/books']['post']['requestBody']
            with self.assertRaises(ValueError):m5_templates.validate_openapi(document,org)
    def test_runtime_bundle_same_source(self):
        for path,expected in template_bundle.bundle().items():self.assertEqual((template_bundle.DEST/path).read_bytes(),expected,path)
    def test_e2e_namespace_guard(self):
        class Accepted(Exception):pass
        def stop(*args):raise Accepted()
        with self.assertRaises(Accepted):m1_e2e.run(ROOT/'artifacts','pangolin-m5-guard','',stop)
        for project in ('pangolin-docs','pangolin-m6-dev'):
            with self.assertRaises(ValueError):m1_e2e.run(ROOT/'artifacts',project,'',stop)

if __name__=='__main__':unittest.main()
