"""M4 cumulative runtime gate: Jobs, capacity recovery and observability."""
import argparse
import json
import os
import m0
import m1
import m3_fixture

ROOT=m0.ROOT

def validate_mapping(mapping):
    expected={s['id'] for g in m0.load(ROOT/'tests/contract/subtests.json')['groups'] if g['milestone']=='M4' for s in g['subtests']}
    sets=[set(mapping[k]) for k in ('unit_tests','integration_tests','python_tests')]
    if mapping['milestone']!='M4' or mapping['baseline']!='v1.3' or set.union(*sets)!=expected or sum(map(len,sets))!=len(expected):raise ValueError('M4 acceptance mapping incomplete or duplicated')
    if mapping.get('pending') or any(not test for key in ('unit_tests','integration_tests','python_tests') for test in mapping[key].values()):raise ValueError('M4 still has unimplemented acceptance cases')

def validate_report(report,commit,directory,mapping):
    m1.validate_report(report,commit,directory)
    if report['milestone']!='M4' or not {'m2-regression','m3-regression','m4-regression'}<={c['name'] for c in report['commands']}:raise ValueError('M4 cumulative regression missing')
    for extra in (m0.load(ROOT/'tests/contract/m2.json'),m0.load(ROOT/'tests/contract/m3.json'),mapping):
        for field,key in (('tests','unit_tests'),('integration_tests','integration_tests')):
            if not set(extra[key].values())<=set(report[field]):raise ValueError('required cumulative case evidence missing')
        path='tests/contract/'+extra['milestone'].lower()+'.json'
        if report['inputs'].get(path)!=m0.digest(ROOT/path):raise ValueError('acceptance mapping changed')
    if not {'operations.json','resilience.json','drain-normal.json','drain-deadline.json'}<=report['artifacts'].keys():raise ValueError('M4 load/recovery/drain curves missing')
    load=m0.load(directory/'operations.json')
    if load['records']!=10000 or load['concurrency']!=64 or load['reconciled']!=10000 or len(load['samples'])<2:raise ValueError('M4 load/recovery reconciliation incomplete')
    resilience=m0.load(directory/'resilience.json')
    if resilience['sent']!=4 or resilience['lost']!=0 or len(resilience['curve'])<3:raise ValueError('M4 broker recovery reconciliation missing')
    for name,state in (('normal','succeeded'),('deadline','pending')):
        drain=m0.load(directory/('drain-'+name+'.json'))
        if not drain['database_last'] or drain['final_state']!=state:raise ValueError('M4 drain ordering missing')
    required={m0.MODULE+'/tests/m4:'+test for test in ('TestCapacityLoad','TestActiveHTTPWorkerDrain/normal','TestActiveHTTPWorkerDrain/deadline','TestJobAdmissionAndAuthority/revoked-service','TestJobAdmissionAndAuthority/delegated-user')}
    if not required<=set(report['integration_tests']):raise ValueError('M4 load/drain/authority evidence missing')
    profiles={m0.MODULE+'/tests/m3:TestM4PersistentOrganizationJobs/'+name for name in ('organization-enabled','replacement-contract','current-grant-revoked')}
    if not profiles<=set(report['integration_tests']):raise ValueError('persistent organization/replacement Job evidence missing')

def gate(commit,mapping):
    root=ROOT/'artifacts'/commit/'m4';reports={}
    for label in ('windows-amd64','linux-amd64'):
        directory=root/label;report=m0.load(directory/'report.json');validate_report(report,commit,directory,mapping)
        if report['platform']['GOOS']+'-'+report['platform']['GOARCH']!=label:raise ValueError('wrong evidence platform')
        reports[label]=report
    if reports['windows-amd64']['source_sha256']!=reports['linux-amd64']['source_sha256']:raise ValueError('different source trees')
    result={'milestone':'M4','status':'verified','source_commit':commit,'source_sha256':reports['windows-amd64']['source_sha256'],'baseline':'v1.3','named_subtests':18,'reports':{label:m0.digest(root/label/'report.json') for label in reports}}
    (root/'gate.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result,indent=2))

def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('action',choices=['verify','gate']);p.add_argument('--project');p.add_argument('--commit');p.add_argument('--race',action='store_true');p.add_argument('--external-broker-fixture',action='store_true');args=p.parse_args()
    mapping=m0.load(ROOT/'tests/contract/m4.json');validate_mapping(mapping)
    if args.action=='gate':
        if not args.commit or len(args.commit)!=40:p.error('gate requires --commit <full SHA>')
        gate(args.commit,mapping);return 0
    if not args.project:p.error('verify requires --project')
    if args.external_broker_fixture:
        if not os.getenv('PANGOLIN_M3_BROKER_RESTART_DIR'):p.error('external broker fixture requires barrier directory')
        return m1.verify(args,increment=mapping)
    with m3_fixture.broker_restart(args.project,ROOT/'artifacts'/'m4-broker-barrier',milestone=4) as directory:
        os.environ['PANGOLIN_M3_BROKER_RESTART_DIR']=str(directory)
        return m1.verify(args,increment=mapping)

if __name__=='__main__':raise SystemExit(main())
