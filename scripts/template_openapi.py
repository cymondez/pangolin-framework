"""Generate profile-specific OpenAPI 3.1 from the shipped template route inventory."""
import json
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]

def document(organization):
    schemas={
        'Book':{'type':'object','additionalProperties':False,'required':['id','title'],'properties':{'id':{'type':'string','minLength':1,'maxLength':128},'title':{'type':'string','minLength':1,'maxLength':300}}},
        'Problem':{'type':'object','required':['type','title','status','code'],'properties':{k:{'type':'integer' if k=='status' else 'string'} for k in ('type','title','status','detail','instance','traceId','code','operationId','outcome')}},
        'GovernanceCommand':{'type':'object','additionalProperties':False,'properties':{'slug':{'type':'string'},'enterprise_id':{'type':['string','null']},'expected_version':{'type':'integer','minimum':1},'principal':{'type':'object','properties':{'ID':{'type':'string'},'Kind':{'enum':['user','service']}}},'role':{'enum':['owner','member','manager']}}},
    }
    result={'openapi':'3.1.0','info':{'title':'Bookstore','version':'0.1.0'},'security':[{'bearerAuth':[]}],'paths':{},'components':{'securitySchemes':{'bearerAuth':{'type':'http','scheme':'bearer','bearerFormat':'JWT'}},'schemas':schemas}}
    def operation(path,method,body=None,summary='Bookstore operation'):
        params=[{'name':part[1:-1],'in':'path','required':True,'schema':{'type':'string'}} for part in path.split('/') if part.startswith('{')]
        params.extend({'name':name,'in':'header','required':False,'schema':{'type':'string'}} for name in ('X-Tenant-ID','X-Scope-Kind','X-Scope-ID'))
        if method not in ('get','head'):params.append({'name':'Idempotency-Key','in':'header','required':True,'schema':{'type':'string','minLength':1,'maxLength':128}})
        op={'operationId':method+'_'+path.strip('/').replace('/','_').replace('{','').replace('}',''),'summary':summary,'parameters':params,'responses':{'200':{'description':'Confirmed result','content':{'application/json':{'schema':{'$ref':'#/components/schemas/Book'} if path.startswith('/books') else {'type':'object'}}}}}}
        for status in ('400','401','403','404','409','422','500','503'):
            op['responses'][status]={'description':'Problem Details','content':{'application/problem+json':{'schema':{'$ref':'#/components/schemas/Problem'}}}}
        if body:op['requestBody']={'required':True,'content':{'application/json':{'schema':{'$ref':'#/components/schemas/'+body}}}}
        result['paths'].setdefault(path,{})[method]=op
    operation('/books','post','Book','Create a book with durable idempotency, audit and Outbox')
    operation('/books/{id}','get',summary='Read the current exact owner book')
    for path in ('/livez','/readyz'):result['paths'][path]={'get':{'operationId':path[1:],'security':[],'responses':{'204':{'description':'Healthy'},'503':{'description':'Not ready'}}}}
    if organization:
        routes=[('/enterprises','post'),('/organizations','post'),('/organizations','get'),('/enterprises/{id}/organizations','get'),('/organizations/{id}/memberships','put'),('/organizations/{id}/memberships','delete'),('/enterprises/{id}/roles','put'),('/organizations/{id}/enterprise','put'),('/organizations/{id}/join-as-owner','post')]
        routes.extend(('/scopes/{kind}/{id}/'+action,'post') for action in ('suspend','resume','archive','recover'))
        for path,method in routes:
            operation(path,method,None if method=='get' else 'GovernanceCommand', 'Organization control plane')
            if method=='get':result['paths'][path][method]['parameters'].extend([{'name':'limit','in':'query','required':True,'schema':{'type':'integer','minimum':1,'maximum':100}},{'name':'cursor','in':'query','required':False,'schema':{'type':'string'}}])
    return result

def main():
    for profile in ('tenant-only','organization-enabled'):
        path=ROOT/'internal/scaffold/assets'/profile/'openapi.json.tmpl'
        path.write_text(json.dumps(document(profile=='organization-enabled'),indent=2)+'\n',encoding='utf-8')
if __name__=='__main__':main()
