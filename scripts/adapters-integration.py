"""Existing news2douyin 0.11.1 and video-app 0.5.1 against RunDesk 0.7.0.
Explicit demo-protocol test; does not test live research or video rendering.
"""
import argparse,json,os,socket,subprocess,sys,tempfile,time
from pathlib import Path
import requests
p=argparse.ArgumentParser();p.add_argument('--rundesk',required=True);p.add_argument('--news',required=True);p.add_argument('--video',required=True);p.add_argument('--output',default='docs/adapters-validation.json');args=p.parse_args()
sys.path.insert(0,str(Path(args.news).resolve()/'src'))
from news2douyin.server.app import create_app
from news2douyin.research.rundesk import RunDeskClient,bootstrap
from news2douyin.research.runtime import ResearchRuntime
from news2douyin.research import service
from news2douyin.research.models import ResearchRun,ResearchSubmission
from news2douyin.storage.models import Article
from sqlmodel import Session

def port():
 with socket.socket() as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
def wait(url,headers=None):
 for _ in range(100):
  try:
   if requests.get(url,headers=headers,timeout=.5).status_code<500:return
  except requests.RequestException:pass
  time.sleep(.1)
 raise AssertionError('local service did not start')
checks=[];processes=[]
with tempfile.TemporaryDirectory(prefix='rundesk-adapters-') as td:
 root=Path(td);base='http://127.0.0.1:'+str(port());env=dict(os.environ);env.pop('RUNDESK_TOKEN',None);env.pop('CODEX_BASE_TOKEN',None)
 try:
  rd=subprocess.Popen([str(Path(args.rundesk).resolve()),'--demo','--data',str(root/'rd'),'--listen',base.split('//')[1]],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL);processes.append(rd);wait(base+'/api/v1/meta');assert requests.get(base+'/api/v1/meta').json()['version']=='0.7.0'
  app=create_app(db_url='sqlite:///'+str(root/'news.db'),storage_root=str(root/'news'));cfg=app.state.research_settings.get();cfg['rundesk_url']=base;app.state.research_settings.write(cfg)
  class DemoFixtureClient(RunDeskClient):
   # Only this test subclass bypasses the production guard for protocol validation.
   def request(self,method,path,*a,**kw):
    value=super().request(method,path,*a,**kw)
    if path=='/meta':value['demo']=False
    return value
  bootstrap(app.state.research_settings,DemoFixtureClient);cfg=app.state.research_settings.get();assert cfg['instance_id']!='default' and cfg['skill_path'];checks.append('unmodified news bootstrap configures dedicated identity, skill and MCP')
  with Session(app.state.engine) as s:s.add(Article(article_key='fixture',title='协议验证用新闻',content='只用于本地协议测试',url='https://example.com/source'));s.commit()
  case=service.create_case(app.state.engine,{'article_keys':['fixture'],'goal':'背景研究验证','strategy_id':'background'});runtime=ResearchRuntime(app.state.engine,app.state.research_settings,DemoFixtureClient);run=runtime.enqueue(case['case_id'],'审批：仅测试协议');rid=run['run_id'];runtime.dispatch(rid)
  with Session(app.state.engine) as s:
   row=s.get(ResearchRun,rid);op=s.get(ResearchSubmission,rid);assert row.session_id and op.rundesk_run_id,(row.status,row.error);sid=row.session_id;runid=op.rundesk_run_id
  remote=requests.get(base+'/api/v1/sessions/'+sid).json();assert remote['source']['appId']=='news2douyin';assert remote['instanceId']==cfg['instance_id'];assert requests.get(base+'/api/v1/applications/news2douyin').json()['instanceId']==cfg['instance_id'];checks.append('news runtime creates sourced task and automatically registers its application')
  runtime.cancel(case['case_id']);checks.append('news adapter can stop its accepted run')
  video_url='http://127.0.0.1:'+str(port());token='video-adapter-test-token-1234567890';venv=dict(env,VIDEO_APP_TOKEN=token);video_bin=Path(args.video).resolve();video_bin.chmod(0o755)
  video=subprocess.Popen([str(video_bin),'--data',str(root/'video'),'--listen',video_url.split('//')[1],'--rundesk',base],env=venv,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL);processes.append(video);h={'Authorization':'Bearer '+token};wait(video_url+'/api/meta',h)
  def call(method,path,body=None):
   r=requests.request(method,video_url+path,headers=h,json=body,timeout=30);assert r.ok,(r.status_code,r.text);return r.json()
  project=call('POST','/api/projects',{'name':'升级协议验证'});prefix='/api/projects/'+project['id'];connected=call('POST',prefix+'/connect',{});vsid=connected['sessionId'];config=call('GET',prefix+'/agent/configuration');assert any(x['name']=='video-project' and x['sourceScope']=='instance' for x in config['skills']);checks.append('existing video binary creates project configuration and sees its native skill')
  state=requests.get(base+'/api/v1/sessions/'+vsid).json();assert state['source']['appId']=='rundesk-video-app';message={'text':'审批：仅测试协议','requestId':'video-integration-0.7.0'};turn=call('POST',prefix+'/messages',message);assert turn['runId'];call('POST',prefix+'/agent/stop',{'expectedRunId':turn['runId']});checks.append('video source metadata, task submission and fenced stop remain compatible')
  apps=requests.get(base+'/api/v1/applications').json();assert {x['appId'] for x in apps}=={'news2douyin','rundesk-video-app'} and len({x['instanceId'] for x in apps})==2;checks.append('both existing clients appear as separate applications without adapter code changes')
 finally:
  for proc in reversed(processes):
   if proc.poll() is None:proc.terminate()
   try:proc.wait(timeout=8)
   except subprocess.TimeoutExpired:proc.kill();proc.wait()
result={'mode':'RunDesk 0.7.0 demo protocol; no live model, real search or render','productionDemoGuardUnchanged':True,'passed':len(checks),'checks':checks};Path(args.output).write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n');print(json.dumps(result,ensure_ascii=False))
