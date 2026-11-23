"""Reproduce the CLI's same-version runtime and local development IdP bundle."""
from pathlib import Path
import argparse
import hashlib
import json

ROOT=Path(__file__).resolve().parents[1]
DEST=ROOT/'internal/scaffold/assets'
OWNERS=('application','kernel','persistence','security','tenancy','events','platform','observability','transport','modules')

def bundle():
    result={}
    paths=[ROOT/'go.mod',ROOT/'go.sum',ROOT/'VERSION']
    for owner in OWNERS:
        paths.extend(p for p in (ROOT/owner).rglob('*') if p.is_file() and (p.suffix in ('.go','.sql') or p.name=='README.md') and not p.name.endswith('_test.go'))
    for p in paths:result['runtime/'+p.relative_to(ROOT).as_posix()+'.tmpl']=p.read_bytes()
    result['common/internal/devidp/idp.go.tmpl']=(ROOT/'internal/testkit/idp/idp.go').read_bytes()
    result['common/deploy/init/001-roles.sql.tmpl']=(ROOT/'deploy/compose/postgres-init/001-roles.sql').read_bytes()
    gomod=(ROOT/'go.mod').read_text().replace('module github.com/cymondez/pangolin-framework','module {{MODULE}}',1)
    gomod+='\nrequire github.com/cymondez/pangolin-framework v0.1.0\n\nreplace github.com/cymondez/pangolin-framework => ./framework\n'
    result['common/go.mod.tmpl']=gomod.encode()
    result['common/go.sum.tmpl']=(ROOT/'go.sum').read_bytes()
    manifest={k:hashlib.sha256(v).hexdigest() for k,v in sorted(result.items())}
    result['common/runtime-manifest.json.tmpl']=(json.dumps(manifest,indent=2)+'\n').encode()
    return result

def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--check',action='store_true');a=p.parse_args()
    expected=bundle()
    if a.check:
        stale=[name for name,raw in expected.items() if not (DEST/name).is_file() or (DEST/name).read_bytes()!=raw]
        extras=[p.relative_to(DEST).as_posix() for p in (DEST/'runtime').rglob('*.tmpl') if p.relative_to(DEST).as_posix() not in expected]
        if stale or extras:raise SystemExit('template source drift: '+', '.join(stale+extras))
        print('same-version runtime bundle verified');return
    for name,raw in expected.items():
        path=DEST/name;path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes(raw)
    print('bundled',len(expected),'same-version sources')
if __name__=='__main__':main()
