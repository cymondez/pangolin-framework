"""Exercise generated Compose projects and remove only their owned resources."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import time

import m0
from m5_templates import request

def run(output,project):
    if not re.fullmatch(r'pangolin-m[567]-[a-z0-9-]+',project):raise ValueError('dedicated M5/M6/M7 project required')
    output=Path(output).resolve();reports=[]
    for name in ('tenant','organization'):
        directory=output/'templates'/('bookstore-org' if name=='organization' else 'bookstore')
        owned=project+'-template-'+name
        compose=['docker','compose','-p',owned,'-f',str(directory/'compose.yaml')]
        commands=[]
        def execute(args,check=True):
            result=subprocess.run(args,cwd=directory,capture_output=True,text=True,encoding='utf-8',errors='replace',env={**os.environ,'PANGOLIN_PG_PORT':'15642','PANGOLIN_IDP_PORT':'18280','PANGOLIN_API_PORT':'18281'})
            log=output/f'compose-{name}-{len(commands):02d}.log';log.write_text(result.stdout+result.stderr,encoding='utf-8');commands.append({'argv':args,'exit_code':result.returncode,'log':log.name})
            if check and result.returncode:raise ValueError('generated Compose failed; see '+str(log))
            return result.stdout
        # Never adopt an existing project belonging to another invocation.
        if execute([*compose,'ps','-a','-q']).strip():raise ValueError('template Compose project already exists')
        started=False
        try:
            execute([*compose,'config','--quiet']);started=True
            execute([*compose,'up','-d','--build','--wait','--wait-timeout','90'])
            base='http://localhost:18281'
            for attempt in range(100):
                try:
                    if request(base,'/readyz')[0]==204:break
                except OSError:pass
                time.sleep(.1)
            else:raise ValueError('generated Compose API not Ready')
            code,raw=request('http://localhost:18280','/token',body={'subject':'alice','tenant_id':'tenant-a'})
            if code!=200:raise ValueError('generated Compose IdP unavailable')
            token=json.loads(raw)['access_token'];headers={'Idempotency-Key':'compose-book'}
            if name=='organization':headers.update({'X-Scope-Kind':'org.organization','X-Scope-ID':'00000000-0000-4000-8000-000000000002'})
            book={'id':'compose-book','title':name}
            if request(base,'/books',token,book,headers)[0]!=200:raise ValueError('generated Compose write failed')
            code,raw=request(base,'/books/compose-book',token,headers=headers)
            if code!=200 or json.loads(raw)!=book:raise ValueError('generated Compose read reconciliation failed')
            reports.append({'profile':name,'ready':True,'read_write':True,'commands':commands})
        finally:
            if started:
                execute([*compose,'logs','--no-color'],check=False)
                # Compose labels and this invocation's exclusive project name own
                # all containers/networks/volumes/images removed by this command.
                execute([*compose,'down','--volumes','--rmi','local'])
    result={'status':'passed','profiles':reports};(output/'compose.json').write_text(json.dumps(result,indent=2)+'\n',encoding='utf-8')
    print('Both generated Compose profiles passed and their disposable resources were removed',flush=True)

def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--output',required=True);p.add_argument('--project',required=True);a=p.parse_args();run(a.output,a.project)
if __name__=='__main__':main()
