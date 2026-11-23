"""Build and run both generated Bookstores against isolated real databases."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

import m0
import template_openapi

ROOT=m0.ROOT

def validate_openapi(document,organization):
    # Compare the complete checked-in route/schema contract, not just valid JSON.
    if document!=template_openapi.document(organization):raise ValueError('OpenAPI route/schema contract drift')
    ids=set()
    def refs(value):
        if isinstance(value,dict):
            if '$ref' in value:
                current=document
                for part in value['$ref'].removeprefix('#/').split('/'):current=current[part]
            for child in value.values():refs(child)
        elif isinstance(value,list):
            for child in value:refs(child)
    refs(document)
    for path,methods in document['paths'].items():
        for method,operation in methods.items():
            if operation['operationId'] in ids:raise ValueError('duplicate OpenAPI operation')
            ids.add(operation['operationId'])
            for p in operation.get('parameters',[]):
                if p['in']=='path' and ('{'+p['name']+'}' not in path or not p['required']):raise ValueError('invalid path parameter')
            if not operation['responses']:raise ValueError('missing responses')

def request(base,path,token=None,body=None,headers=None,method=None):
    h={'Content-Type':'application/json',**(headers or {})}
    if token:h['Authorization']='Bearer '+token
    raw=None if body is None else json.dumps(body).encode()
    r=urllib.request.Request(base+path,data=raw,headers=h,method=method or ('POST' if body is not None else 'GET'))
    try:
        with urllib.request.urlopen(r,timeout=8) as response:return response.status,response.read()
    except urllib.error.HTTPError as error:return error.code,error.read()

def free_port():
    with socket.socket() as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]

def wait_ready(base,process):
    for _ in range(100):
        if process.poll() is not None:raise ValueError('generated API exited before Ready')
        try:
            if request(base,'/readyz')[0]==204:return
        except (OSError,urllib.error.URLError):pass
        time.sleep(.1)
    raise ValueError('generated API readiness deadline')

def run(output,cli):
    output=Path(output).resolve();output.mkdir(parents=True,exist_ok=True)
    work=output/'templates';work.mkdir(exist_ok=True)
    fixture=output/('template-db.exe' if os.name=='nt' else 'template-db')
    commands=[]
    def execute(argv,cwd=ROOT,env=None,expect=0,timeout=180):
        result=subprocess.run(list(map(str,argv)),cwd=cwd,env=env or m0.go_environment(ROOT),capture_output=True,text=True,encoding='utf-8',errors='replace',timeout=timeout)
        path=output/f'template-{len(commands):02d}.log';path.write_text(result.stdout+result.stderr,encoding='utf-8')
        commands.append({'argv':[str(x) for x in argv],'cwd':str(cwd),'exit_code':result.returncode,'log':path.name})
        if (result.returncode==0)!=(expect==0):raise ValueError('template command failed: '+str(argv)+'; see '+str(path))
        return result.stdout
    execute(['go','build','-o',fixture,'./internal/testkit/cmd/template-db'])
    reports=[]
    for name,org in [('tenant',False),('organization',True)]:
        project_name='bookstore-org' if org else 'bookstore'
        directory=work/project_name
        if directory.exists():raise ValueError('template output must be a clean runner directory')
        execute([cli,'new',project_name,'--template','modular-monolith',*(['--with','organization'] if org else [])],cwd=work)
        before={p.relative_to(directory).as_posix():hashlib.sha256(p.read_bytes()).hexdigest() for p in directory.rglob('*') if p.is_file()}
        execute([cli,'new',project_name],cwd=work,expect=1)
        if before!={p.relative_to(directory).as_posix():hashlib.sha256(p.read_bytes()).hexdigest() for p in directory.rglob('*') if p.is_file()}:raise ValueError('new reentry corrupted output')
        execute([cli,'module','add','catalog','--depends','persistence,authorization'],cwd=directory)
        execute([cli,'migrate','create','add_catalog','--module','catalog'],cwd=directory)
        execute([cli,'generate','openapi'],cwd=directory)
        validate_openapi(m0.load(directory/'openapi.json'),org)
        if not org and 'modules/organization' in execute(['go','list','-deps','./cmd/api'],cwd=directory):raise ValueError('tenant template imports organization')
        manifest=m0.load(directory/'runtime-manifest.json')
        for path,expected in manifest.items():
            if m0.digest(directory/path)!=expected:raise ValueError('bundled runtime hash mismatch')
        execute(['go','test','-count=1','./...'],cwd=directory)
        executable=directory/('api.exe' if os.name=='nt' else 'api')
        execute(['go','build','-o',executable,'./cmd/api'],cwd=directory)
        db='m5_template_'+name+'_'+uuid.uuid4().hex[:12]
        execute([fixture,'create',db])
        environment=m0.go_environment(ROOT)
        for source,target in [('PANGOLIN_M1_ADMIN_DSN','PANGOLIN_MIGRATION_DSN'),('PANGOLIN_M1_APP_DSN','PANGOLIN_DSN'),('PANGOLIN_M1_WORKER_DSN','PANGOLIN_AUTHORITY_DSN')]:
            u=urllib.parse.urlsplit(os.environ[source]);environment[target]=urllib.parse.urlunsplit(u._replace(path='/'+db))
        issuer=os.environ['PANGOLIN_M1_ISSUER'];token_url=os.getenv('PANGOLIN_M1_TOKEN_URL',issuer+'/token')
        environment['PANGOLIN_ISSUER']=issuer;environment['PANGOLIN_JWKS_URL']=os.getenv('PANGOLIN_JWKS_URL',issuer+'/jwks.json')
        environment['PANGOLIN_LISTEN']='127.0.0.1:'+str(free_port());base='http://'+environment['PANGOLIN_LISTEN']
        process=None;log=None
        try:
            execute(['go','run','./cmd/migrate','--seed'],cwd=directory,env=environment)
            execute(['go','run','./cmd/migrate','--seed'],cwd=directory,env=environment)
            status=execute([cli,'migrate','status'],cwd=directory,env=environment)
            if '5000001 applied=true' not in status:raise ValueError('custom module migration not applied')
            execute(['go','test','-count=1','-tags=integration','./contracts'],cwd=directory,env=environment)
            log=(output/('template-'+name+'-api.log')).open('w',encoding='utf-8')
            process=subprocess.Popen([str(executable)],cwd=directory,env=environment,stdout=log,stderr=subprocess.STDOUT)
            wait_ready(base,process)
            tokens={}
            for tenant in ('tenant-a','tenant-b'):
                code,raw=request(token_url,'',body={'subject':'alice','tenant_id':tenant})
                if code!=200:raise ValueError('fixture token unavailable')
                tokens[tenant]=json.loads(raw)['access_token']
            checks=[]
            def check(label,method,path,tenant='tenant-a',body=None,headers=None,status=200):
                code,raw=request(base,path,tokens[tenant],body,headers,method)
                if code!=status:raise ValueError(label+': expected '+str(status)+' got '+str(code)+' '+raw.decode())
                checks.append(label);return json.loads(raw) if raw else None
            for tenant in ('tenant-a','tenant-b'):
                body={'id':'same-book','title':tenant};headers={'Idempotency-Key':'same-key'}
                check('write-'+tenant,'POST','/books',tenant,body,headers)
                check('replay-'+tenant,'POST','/books',tenant,body,headers)
                value=check('read-'+tenant,'GET','/books/same-book',tenant)
                if value['title']!=tenant:raise ValueError('tenant data crossed owner')
            check('key-conflict','POST','/books',body={'id':'changed','title':'changed'},headers={'Idempotency-Key':'same-key'},status=409)
            malformed=check('owner-dto-rejected','POST','/books',body={'id':'x','title':'x','tenant_id':'tenant-b'},headers={'Idempotency-Key':'body-owner'},status=400)
            if malformed['code']!='MALFORMED_REQUEST':raise ValueError('malformed request code mismatch')
            invalid=check('semantic-validation','POST','/books',body={'id':'invalid','title':''},headers={'Idempotency-Key':'semantic'},status=422)
            if invalid['code']!='VALIDATION_FAILED':raise ValueError('semantic validation code mismatch')
            check('tenant-hint-denied','GET','/books/same-book',headers={'X-Tenant-ID':'tenant-b'},status=403)
            if request(base,'/books/same-book')[0]!=401:raise ValueError('unauthenticated request accepted')
            if org:
                for suffix in ('2','3'):
                    headers={'X-Scope-Kind':'org.organization','X-Scope-ID':'00000000-0000-4000-8000-00000000000'+suffix,'Idempotency-Key':'same-key'}
                    check('org-write-'+suffix,'POST','/books',body={'id':'same-book','title':'org-'+suffix},headers=headers)
                    value=check('org-read-'+suffix,'GET','/books/same-book',headers=headers)
                    if value['title']!='org-'+suffix:raise ValueError('scope data crossed owner')
                check('complete-control-plane','GET','/organizations?limit=10')
                enterprise=check('create-enterprise','POST','/enterprises',body={'slug':'created-enterprise'},headers={'Idempotency-Key':'create-enterprise'})
                check('create-organization','POST','/organizations',body={'slug':'created-org','enterprise_id':enterprise['Ref']['ID']},headers={'Idempotency-Key':'create-organization'})
                check('parent-is-not-union','GET','/books/same-book',headers={'X-Scope-Kind':'org.enterprise','X-Scope-ID':'00000000-0000-4000-8000-000000000001'},status=404)
                check('missing-selector','GET','/books/same-book',headers={'X-Scope-Kind':'org.organization'},status=400)
            else:check('unsupported-org','GET','/books/same-book',headers={'X-Scope-Kind':'org.organization','X-Scope-ID':'00000000-0000-4000-8000-000000000002'},status=400)
            reconciliation=json.loads(execute([fixture,'reconcile',db,name]))
            if org:
                tenant_dir=work/'bookstore';tenant_api=tenant_dir/('api.exe' if os.name=='nt' else 'api')
                execute([tenant_api],cwd=tenant_dir,env=environment,expect=1,timeout=8)
                checks.append('profile-downgrade-blocked')
            reports.append({'profile':'organization-enabled' if org else 'tenant-only','checks':checks,'runtime_files':len(manifest),'custom_migration_applied':True,'build_test_start':True,'reconciliation':reconciliation})
        finally:
            if process is not None and process.poll() is None:
                process.terminate()
                try:process.wait(timeout=20)
                except subprocess.TimeoutExpired:process.kill();process.wait(timeout=5)
            if log:log.close()
            execute([fixture,'drop',db])
    result={'status':'passed','version':'0.1.0','profiles':reports,'commands':commands}
    (output/'templates.json').write_text(json.dumps(result,indent=2)+'\n',encoding='utf-8')
    print('Both generated Bookstore profiles passed build/test/start and real SQL/HTTP contracts',flush=True)
    return result

def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--output',required=True);p.add_argument('--cli',required=True);a=p.parse_args();run(a.output,Path(a.cli).resolve())
if __name__=='__main__':main()
