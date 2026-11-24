"""M5 cumulative platform, CLI and generated Bookstore conformance gate."""
import argparse
import json
import os

import m0
import m1
import m4
import m3_fixture

ROOT=m0.ROOT
FIELDS=('unit_tests','integration_tests','python_tests','template_tests')

def database_env(env=None):
    """Resolve both real backend DSNs; explicit values win over the host fixture."""
    values=dict(os.environ if env is None else env)
    postgres=(values.get('PANGOLIN_DATABASE_POSTGRES_DSN') or '').strip()
    if not postgres:postgres=values.get('PANGOLIN_M1_APP_DSN') or 'postgres://pangolin_app:local-app-only@127.0.0.1:15542/pangolin_m0?sslmode=disable'
    port=(values.get('PANGOLIN_M5_MYSQL_PORT') or '').strip() or '15543';int(port)
    mysql=(values.get('PANGOLIN_DATABASE_MYSQL_DSN') or '').strip() or 'pangolin_database:local-database-only@tcp(127.0.0.1:'+port+')/pangolin_db_contracts'
    if not postgres.strip() or not mysql.strip():raise ValueError('M5 database acceptance requires non-empty PANGOLIN_DATABASE_POSTGRES_DSN and PANGOLIN_DATABASE_MYSQL_DSN')
    return {'PANGOLIN_DATABASE_POSTGRES_DSN':postgres,'PANGOLIN_DATABASE_MYSQL_DSN':mysql}

def validate_mapping(mapping):
    expected={s['id'] for g in m0.load(ROOT/'tests/contract/subtests.json')['groups'] if g['milestone']=='M5' for s in g['subtests']}
    sets=[set(mapping[k]) for k in FIELDS]
    if mapping['milestone']!='M5' or mapping['baseline']!='v1.3' or set.union(*sets)!=expected or sum(map(len,sets))!=len(expected):raise ValueError('M5 acceptance mapping incomplete or duplicated')
    if mapping.get('pending') or any(not test for key in FIELDS for test in mapping[key].values()):raise ValueError('M5 has unimplemented acceptance')
    field=mapping.get('regression_integration_tests',{})
    if any(not name for name in (field.values() if isinstance(field,dict) else field)):raise ValueError('M5 regression integration mapping incomplete')

def validate_report(report,commit,directory,mapping):
    m4.validate_report(report,commit,directory,m0.load(ROOT/'tests/contract/m4.json'),milestone='M5')
    if not {'m5-regression','template-source','cli-build','templates','cli-coverage'}<={c['name'] for c in report['commands']}:raise ValueError('M5 generated-template commands missing')
    for field,key in [('tests','unit_tests'),('integration_tests','integration_tests')]:
        if not set(mapping[key].values())<=set(report[field]):raise ValueError('M5 case evidence missing')
    if not set(mapping.get('regression_tests',[]))<=set(report['tests']):raise ValueError('M5 JSON configuration regression missing')
    field=mapping.get('regression_integration_tests',{})
    if not (set(field.values()) if isinstance(field,dict) else set(field))<=set(report['integration_tests']):raise ValueError('M5 regression integration evidence missing')
    path='tests/contract/m5.json'
    if report['inputs'].get(path)!=m0.digest(ROOT/path):raise ValueError('M5 map changed')
    if not {'cli.out','templates.json'}<=report['artifacts'].keys():raise ValueError('M5 CLI/template raw evidence missing')
    data=m0.load(directory/'templates.json')
    if data['status']!='passed' or data['version']!='0.1.0' or {p['profile'] for p in data['profiles']}!={'tenant-only','organization-enabled'}:raise ValueError('both generated profiles required')
    for p in data['profiles']:
        if not p['build_test_start'] or not p['custom_migration_applied'] or not {'write-tenant-a','write-tenant-b','tenant-hint-denied','owner-dto-rejected','key-conflict','semantic-validation'}<=set(p['checks']):raise ValueError('generated security/UOW contracts missing')
        if p['profile']=='organization-enabled' and not {'org-write-2','org-write-3','complete-control-plane','profile-downgrade-blocked'}<=set(p['checks']):raise ValueError('complete organization template not exercised')
        if not p['reconciliation']['force_rls'] or not p['reconciliation']['non_privileged_app'] or p['reconciliation']['organization_schema']!=(p['profile']=='organization-enabled'):raise ValueError('generated privilege/RLS reconciliation failed')
        expected=6 if p['profile']=='organization-enabled' else 2
        if any(p['reconciliation']['counts'][table]!=expected for table in ('pangolin_audit.records','pangolin_events.outbox','pangolin_idempotency.results')):raise ValueError('generated atomic effects reconciliation missing')
    for command in data['commands']:
        if command['log'] not in report['artifacts']:raise ValueError('template command log missing')
    if report['platform']['GOOS']=='windows':
        if 'compose.json' not in report['artifacts'] or 'templates-compose' not in {c['name'] for c in report['commands']}:raise ValueError('generated Compose execution missing')
        compose=m0.load(directory/'compose.json')
        if compose['status']!='passed' or {p['profile'] for p in compose['profiles']}!={'tenant','organization'} or not all(p['ready'] and p['read_write'] for p in compose['profiles']):raise ValueError('both generated Compose profiles required')
    required=m0.MODULE+'/tests/m3:TestA80PlatformAuthority/'
    if not {required+'current-parent',required+'audit-owner',required+'revoked-ancestry'}<=set(report['integration_tests']):raise ValueError('current ancestry/audit regression missing')

def gate(commit,mapping):
    root=ROOT/'artifacts'/commit/'m5';reports={}
    for label in ('windows-amd64','linux-amd64'):
        directory=root/label;report=m0.load(directory/'report.json');validate_report(report,commit,directory,mapping)
        if report['platform']['GOOS']+'-'+report['platform']['GOARCH']!=label:raise ValueError('wrong platform')
        reports[label]=report
    if reports['windows-amd64']['source_sha256']!=reports['linux-amd64']['source_sha256']:raise ValueError('different source trees')
    result={'milestone':'M5','status':'verified','source_commit':commit,'source_sha256':reports['windows-amd64']['source_sha256'],'baseline':'v1.3','named_subtests':sum(len(mapping[k]) for k in FIELDS),'reports':{label:m0.digest(root/label/'report.json') for label in reports}}
    (root/'gate.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result,indent=2))

def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('action',choices=['verify','gate']);p.add_argument('--project');p.add_argument('--commit');p.add_argument('--race',action='store_true');p.add_argument('--external-broker-fixture',action='store_true');args=p.parse_args()
    mapping=m0.load(ROOT/'tests/contract/m5.json');validate_mapping(mapping)
    if args.action=='gate':
        if not args.commit or len(args.commit)!=40:p.error('gate requires full --commit')
        gate(args.commit,mapping);return 0
    if not args.project:p.error('verify requires --project')
    os.environ.update(database_env())
    if args.external_broker_fixture:
        if not os.getenv('PANGOLIN_M3_BROKER_RESTART_DIR'):p.error('external fixture needs barrier directory')
        return m1.verify(args,increment=mapping)
    with m3_fixture.broker_restart(args.project,ROOT/'artifacts/m5-broker-barrier',milestone=5) as directory:
        os.environ['PANGOLIN_M3_BROKER_RESTART_DIR']=str(directory)
        return m1.verify(args,increment=mapping)
if __name__=='__main__':raise SystemExit(main())
