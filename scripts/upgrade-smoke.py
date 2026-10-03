#!/usr/bin/env python3
"""Read-only baseline preservation check across an actual old/new RunDesk binary.
Usage: python scripts/upgrade-smoke.py --old /path/to/0.6.1 --new bin/rundesk
Both processes run --demo in an isolated temporary data directory.
"""
import argparse,importlib.util,json,pathlib,socket,subprocess,tempfile,time,urllib.request
parser=argparse.ArgumentParser();parser.add_argument('--old',required=True);parser.add_argument('--old-version',default='0.6.1');parser.add_argument('--new-version',default='0.7.0');parser.add_argument('--new',default='bin/rundesk');parser.add_argument('--output',default='docs/upgrade-0.7.0.json');args=parser.parse_args()
root=pathlib.Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('application_client',root/'examples/application_client.py');module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
checks=[]
with tempfile.TemporaryDirectory(prefix='rundesk-upgrade-') as data:
 with socket.socket() as sock:sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
 base=f'http://127.0.0.1:{port}'
 def call(method,path,body=None):
  req=urllib.request.Request(base+'/api'+path,method=method,data=json.dumps(body).encode() if body is not None else None,headers={'Content-Type':'application/json'})
  with urllib.request.urlopen(req,timeout=20) as r:return json.load(r)
 def start(binary):
  process=subprocess.Popen([str(pathlib.Path(binary).resolve()),'--demo','--data',data,'--listen',f'127.0.0.1:{port}'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  for _ in range(100):
   try:call('GET','/meta');return process
   except OSError:time.sleep(.1)
  process.kill();raise RuntimeError('server failed to start')
 def complete(sid):
  for _ in range(100):
   session=call('GET','/sessions/'+sid)
   if session['status']=='completed':return session
   if session['status']=='failed':raise RuntimeError(session)
   time.sleep(.1)
  raise RuntimeError('task did not finish')
 old=start(args.old)
 try:
  assert call('GET','/meta')['version']==args.old_version
  work=call('GET','/workspaces')[0]
  instance=call('POST','/instances',{'name':'Existing instance','defaultModel':'demo-fixture'})
  cp=f"/workspaces/{work['id']}"
  call('PUT',cp+'/notes',{'text':'Keep these project notes','revision':0})
  call('PUT',cp+f"/skills/old-guide?instanceId={instance['id']}&scope=instance",{'content':'---\nname: old-guide\ndescription: Keep existing skill\n---\nPreserve this file.'})
  session=call('POST','/sessions',{'workspaceId':work['id'],'instanceId':instance['id'],'title':'Existing task'})
  call('POST',f"/sessions/{session['id']}/turns",{'text':'Before upgrade'})
  before=complete(session['id']);events=call('GET',f"/sessions/{session['id']}/events")
  app_task=call('POST','/sessions',{'workspaceId':work['id'],'instanceId':instance['id'],'title':'Existing app task','source':{'kind':'application','appId':'news2douyin','taskId':'preserve-business-task'}})
  orphan=call('POST','/instances',{'name':'Unassigned old configuration'})
  extra=pathlib.Path(instance['codexHome'])/'skills'/'old-guide'/'references';extra.mkdir();(extra/'keep.txt').write_text('Old sibling preserved')
 finally:old.terminate();old.wait(timeout=10)
 new=start(args.new)
 try:
  client=module.RunDesk(base)
  assert client.call('GET','/meta')['version']==args.new_version;checks.append('new binary reports '+args.new_version)
  apps=client.call('GET','/applications');assert len(apps)==1 and apps[0]['appId']=='news2douyin' and apps[0]['instanceId']==instance['id'] and apps[0]['sessionCount']==2;checks.append('old application source migrates to a one-to-one registry')
  assert client.session(app_task['id'])==app_task;assert any(i['id']==orphan['id'] for i in client.call('GET','/instances'));checks.append('application source and unassigned configuration preserved')
  bundle=client.call('GET',cp+f"/skill-bundles/old-guide?instanceId={instance['id']}&scope=instance");assert {f['path'] for f in bundle['files']}=={'SKILL.md','references/keep.txt'};checks.append('existing skill directory and attached reference are readable without conversion')
  after=client.session(session['id']);assert after['threadId']==before['threadId'] and after['instanceId']==instance['id'] and after['workspaceId']==work['id'] and after['model']=='demo-fixture';checks.append('session, thread, instance, workspace and model preserved')
  assert client.call('GET','/workspaces')[0]['notes']=='Keep these project notes';checks.append('workspace notes preserved')
  skill=client.call('GET',cp+f"/skills/old-guide?instanceId={instance['id']}&scope=instance");assert 'Preserve this file.' in skill['content'];checks.append('instance skill preserved')
  assert client.events(session['id'])[:len(events)]==events;checks.append('historical events preserved; shutdown may append lifecycle events')
  run=client.start(session['id'],'After upgrade','upgrade-continue-01');after=complete(session['id']);assert after['threadId']==before['threadId'] and after['runId']==run['runId'] and after['runId']!=before['runId'];checks.append('v1 client resumes existing native thread')
  assert client.receipt('upgrade-continue-01')['response']['runId']==run['runId'];assert client.start(session['id'],'After upgrade','upgrade-continue-01')['runId']==run['runId'];checks.append('Python client receipt and retry preserve run')
  tail=client.events(session['id'],events[-1]['id']);assert tail and all(e['id']>events[-1]['id'] for e in tail);checks.append('incremental event cursor continues across upgrade')
 finally:new.terminate();new.wait(timeout=10)
report={'passed':len(checks),'checks':checks,'fixture':'Codex demo protocol only'}
out=pathlib.Path(args.output);out.parent.mkdir(parents=True,exist_ok=True);out.write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report))
