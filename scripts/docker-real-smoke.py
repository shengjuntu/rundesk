#!/usr/bin/env python3
"""Opt-in real Docker acceptance. Use a disposable RunDesk data directory.
Creates one app/project; tests config RPC and host persistence. No model turn.
"""
import argparse,json,os,pathlib,uuid,urllib.request
p=argparse.ArgumentParser();p.add_argument('--url',default='http://127.0.0.1:3210');p.add_argument('--image',required=True);p.add_argument('--report',default='docker-real-validation.json');a=p.parse_args()
base=a.url.rstrip('/')+'/api/v1';token=os.getenv('RUNDESK_TOKEN','');suffix=uuid.uuid4().hex[:10]
def call(method,path,body=None):
 headers={'Content-Type':'application/json','Idempotency-Key':uuid.uuid4().hex}
 if token:headers['Authorization']='Bearer '+token
 data=None if body is None else json.dumps(body).encode();req=urllib.request.Request(base+path,data=data,headers=headers,method=method)
 with urllib.request.urlopen(req,timeout=120) as r:return json.load(r)
checks=[];v=None
try:
 call('GET','/docker/status');checks.append('real Docker Engine reachable')
 w=call('POST','/workspaces',{'name':'Docker acceptance '+suffix})
 i=call('POST','/instances',{'name':'Docker acceptance '+suffix})
 i=call('PUT','/instances/'+i['id']+'/execution',{'revision':i['revision'],'spec':{'mode':'docker','image':a.image}})
 call('PUT','/applications/docker-acceptance-'+suffix,{'name':'Docker acceptance','instanceId':i['id'],'workspaceId':w['id']})
 v=call('POST','/environments',{'instanceId':i['id'],'workspaceId':w['id']})
 def action(name):
  global v
  v=call('POST','/environments/'+v['id']+'/actions',{'revision':v['revision'],'action':name});return v
 action('start');assert v['state']=='running' and v['imageId'].startswith('sha256:');checks.append('managed container started with pinned image')
 config=call('GET',f"/instances/{i['id']}/configuration?workspaceId={w['id']}&probe=1")
 assert not config['errors'],config['errors'];assert config['instance']['codexHome']==v['codexHome'];checks.append('real Codex initialize/config/skills/MCP status')
 marker=pathlib.Path(v['codexHome'])/('acceptance-'+suffix+'.txt');marker.write_text('persistent-host-state')
 pinned=v['imageId'];action('remove');action('start');assert v['imageId']==pinned and marker.read_text()=='persistent-host-state';checks.append('remove/start retains host data and image identity')
 action('recreate');assert marker.read_text()=='persistent-host-state';checks.append('explicit recreate retains host data');marker.unlink()
 report={'mode':'real local Docker + real Codex; no model turn','passed':len(checks),'checks':checks,'environmentId':v['id'],'imageId':v['imageId']};pathlib.Path(a.report).write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report))
finally:
 if v:
  # Only our newly-created environment; never remove data or another container.
  latest=call('GET','/environments/'+v['id']);call('POST','/environments/'+v['id']+'/actions',{'revision':latest['revision'],'action':'remove'})
