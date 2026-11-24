"""M5 corrective database acceptance wiring: MySQL overlay contract and DSN seam."""
from pathlib import Path
import json
import sys
import tempfile
import unittest
from unittest import mock

ROOT=Path(__file__).resolve().parents[2]
sys.path.insert(0,str(ROOT/'scripts'))
import m0,m1,m4,m5

OVERLAY=ROOT/'deploy/compose/m5-databases.yaml'
DIGEST='a81d043930e51745522bd5c430961cf8286f0a23f0a9a2a6e0108f7ea71b0902'


class MysqlOverlayContract(unittest.TestCase):
    def setUp(self):
        self.text=OVERLAY.read_text(encoding='utf-8-sig')

    def test_mysql_only_overlay(self):
        self.assertRegex(self.text,r'(?m)^  mysql:$')
        self.assertNotRegex(self.text,r'(?m)^  (postgres|nats|test-idp):$')
        self.assertIn('${PANGOLIN_M5_MYSQL_IMAGE:-mysql:8.4@sha256:'+DIGEST+'}',self.text)
        self.assertIn('- "127.0.0.1:${PANGOLIN_M5_MYSQL_PORT:-15543}:3306"',self.text)

    def test_fixture_database_and_app_user(self):
        self.assertIn('MYSQL_DATABASE: pangolin_db_contracts',self.text)
        self.assertIn('MYSQL_USER: pangolin_database',self.text)
        self.assertIn('MYSQL_PASSWORD: local-database-only',self.text)
        self.assertIn('mysql-data:/var/lib/mysql',self.text)

    def test_healthcheck_named_volume_and_bounds(self):
        self.assertIn('mysqladmin ping',self.text)
        self.assertIn('interval: 2s',self.text)
        self.assertIn('retries: 45',self.text)
        self.assertIn('dev.pangolin.owner: pangolin-framework',self.text)
        self.assertIn('dev.pangolin.purpose: m5-databases',self.text)
        for limit in ('restart: "no"','cpus: 0.5','pids_limit: 128','mem_limit: 512m'):
            self.assertIn(limit,self.text)

    def test_base_m1_compose_untouched(self):
        base=(ROOT/'deploy/compose/m1.yaml').read_text(encoding='utf-8-sig')
        self.assertNotIn('mysql',base.lower())


class DatabaseDsnWiring(unittest.TestCase):
    def test_host_fixture_defaults(self):
        env=m5.database_env({})
        self.assertEqual(env['PANGOLIN_DATABASE_POSTGRES_DSN'],
            'postgres://pangolin_app:local-app-only@127.0.0.1:15542/pangolin_m0?sslmode=disable')
        self.assertEqual(env['PANGOLIN_DATABASE_MYSQL_DSN'],
            'pangolin_database:local-database-only@tcp(127.0.0.1:15543)/pangolin_db_contracts')

    def test_m1_app_dsn_fallback(self):
        env=m5.database_env({'PANGOLIN_M1_APP_DSN':'postgres://pangolin_app:local-app-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'})
        self.assertEqual(env['PANGOLIN_DATABASE_POSTGRES_DSN'],
            'postgres://pangolin_app:local-app-only@127.0.0.1:15542/pangolin_m0?sslmode=disable')

    def test_explicit_values_and_port_override_win(self):
        env=m5.database_env({'PANGOLIN_DATABASE_POSTGRES_DSN':'postgres://app@127.0.0.1:1/x?sslmode=disable',
                             'PANGOLIN_M5_MYSQL_PORT':'15599'})
        self.assertEqual(env['PANGOLIN_DATABASE_POSTGRES_DSN'],'postgres://app@127.0.0.1:1/x?sslmode=disable')
        self.assertEqual(env['PANGOLIN_DATABASE_MYSQL_DSN'],'pangolin_database:local-database-only@tcp(127.0.0.1:15599)/pangolin_db_contracts')

    def test_blank_dsn_falls_back_to_fixture(self):
        env=m5.database_env({'PANGOLIN_DATABASE_POSTGRES_DSN':'  ','PANGOLIN_DATABASE_MYSQL_DSN':''})
        self.assertEqual(env['PANGOLIN_DATABASE_MYSQL_DSN'],
            'pangolin_database:local-database-only@tcp(127.0.0.1:15543)/pangolin_db_contracts')

    def test_unresolved_dsn_or_bad_port_fails_the_gate(self):
        with self.assertRaises(ValueError):m5.database_env({'PANGOLIN_M1_APP_DSN':'   '})
        with self.assertRaises(ValueError):m5.database_env({'PANGOLIN_M5_MYSQL_PORT':'not-a-port'})

    def test_no_root_credentials(self):
        for dsn in m5.database_env({}).values():
            self.assertNotIn('root',dsn.lower())


class IntegrationSeam(unittest.TestCase):
    def test_older_milestones_never_require_database(self):
        for milestone in ('M1','M2','M3','M4'):
            self.assertEqual(m1.database_packages(milestone,ROOT/'tests/database'),[])

    def test_m5_requires_suite_and_appends_it(self):
        with tempfile.TemporaryDirectory() as directory:
            present=Path(directory)/'present';present.mkdir()
            self.assertEqual(m1.database_packages('M5',present),['./tests/database'])
            self.assertEqual(m1.database_packages('M6',present),['./tests/database'])
            with self.assertRaises(ValueError):m1.database_packages('M5',Path(directory)/'tests/database')
            with self.assertRaises(ValueError):m1.database_packages('M6',Path(directory)/'tests/database')


DB='github.com/cymondez/pangolin-framework/tests/database:TestBackendContracts/'


class RegressionIntegrationEnforcement(unittest.TestCase):
    def increment(self):
        mapping=m0.load(ROOT/'tests/contract/m5.json')
        return dict(mapping,regression_integration_tests={'db/backend-x':DB+'backend-x'})

    def test_required_integration_tests_composition(self):
        self.assertEqual(m1.required_integration_tests([]),[m0.MODULE+'/tests/m1:TestM1Flow'])
        increment={'integration_tests':{'case':'pkg:TestCase'},'regression_integration_tests':{'db/y':'pkg:TestDbY'}}
        self.assertEqual(m1.required_integration_tests([increment]),[m0.MODULE+'/tests/m1:TestM1Flow','pkg:TestCase','pkg:TestDbY'])
        listed=dict(increment,regression_integration_tests=['pkg:TestDbList'])
        self.assertIn('pkg:TestDbList',m1.required_integration_tests([listed]))

    def test_integration_collection_requires_exact_passed_new_case(self):
        increment=self.increment()
        required=m1.required_integration_tests([increment])
        name=increment['regression_integration_tests']['db/backend-x']
        package,test=name.split(':',1)
        events=lambda list_:"\n".join(json.dumps(event) for event in list_)
        missing=[{'Action':'pass','Package':value.rsplit(':',1)[0],'Test':value.rsplit(':',1)[1]} for value in required if value!=name]
        self.assertNotEqual(len(missing),len(required))
        with self.assertRaises(ValueError):m1.validate_tests(events(missing),required)
        skipped=[*missing,{'Action':'skip','Test':test}]
        with self.assertRaises(ValueError):m1.validate_tests(events(skipped),required)
        self.assertEqual(m1.validate_tests(events([*missing,{'Action':'pass','Package':package,'Test':test}]),required),sorted(required))

    def valid_report(self):
        increment=self.increment()
        directory=Path(self.enterContext(tempfile.TemporaryDirectory()))
        counts=('pangolin_audit.records','pangolin_events.outbox','pangolin_idempotency.results')
        templates={'status':'passed','version':'0.1.0','profiles':[],'commands':[]}
        for profile,org in [('tenant-only',False),('organization-enabled',True)]:
            checks={'write-tenant-a','write-tenant-b','tenant-hint-denied','owner-dto-rejected','key-conflict','semantic-validation'}
            if org:checks|={'org-write-2','org-write-3','complete-control-plane','profile-downgrade-blocked'}
            templates['profiles'].append({'profile':profile,'build_test_start':True,'custom_migration_applied':True,
                'checks':sorted(checks),'reconciliation':{'force_rls':True,'non_privileged_app':True,'organization_schema':org,'counts':{table:(6 if org else 2) for table in counts}}})
        (directory/'templates.json').write_text(json.dumps(templates),encoding='utf-8')
        ancestry={m0.MODULE+'/tests/m3:TestA80PlatformAuthority/'+name for name in ('current-parent','audit-owner','revoked-ancestry')}
        return directory,increment,{'platform':{'GOOS':'linux','GOARCH':'amd64'},'source_sha256':'a'*64,
            'commands':[{'name':name} for name in ('m5-regression','template-source','cli-build','templates','cli-coverage')],
            'tests':sorted(set(increment['unit_tests'].values())|set(increment.get('regression_tests',[]))),
            'integration_tests':sorted(set(increment['integration_tests'].values())|ancestry),
            'inputs':{'tests/contract/m5.json':m0.digest(ROOT/'tests/contract/m5.json')},
            'artifacts':{'cli.out':'0'*64,'templates.json':'1'*64}}

    def test_report_requires_new_case_only_when_mapping_declares_it(self):
        with mock.patch.object(m4,'validate_report',lambda *args,**kwargs:None),mock.patch.object(m1,'validate_report',lambda *args,**kwargs:None):
            directory,increment,report=self.valid_report()
            live=m0.load(ROOT/'tests/contract/m5.json')
            # The copied mapping declares the field, so absence is only
            # constructed here by removing it from the in-memory fixture.
            dormant={key:value for key,value in live.items() if key!='regression_integration_tests'}
            self.assertNotIn('regression_integration_tests',dormant)
            # Without the new field nothing is fabricated; with it the same report must fail.
            m5.validate_report(dict(report),'a'*40,directory,dormant)
            with self.assertRaises(ValueError):m5.validate_report(dict(report),'a'*40,directory,increment)
            report['integration_tests'].append(increment['regression_integration_tests']['db/backend-x'])
            m5.validate_report(report,'a'*40,directory,increment)
            # A blank name in the mapping is rejected before any evidence is accepted.
            with self.assertRaises(ValueError):m5.validate_mapping(dict(dormant,regression_integration_tests={'db/blank':''}))


if __name__=='__main__':unittest.main()
