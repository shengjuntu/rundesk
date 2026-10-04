"""Generate the shipped v1 contract from registered routes and curated schemas."""
from pathlib import Path
import json,re
root=Path(__file__).resolve().parents[1]
def obj(props=None,required=(),extra=False):
 return dict(type='object',properties=props or {},required=list(required),additionalProperties=extra)
def st(**kwargs):return dict(type='string',**kwargs)
def ref(name):return {'$ref':'#/components/schemas/'+name}
def arr(item):return {'type':'array','items':item}
bool_={'type':'boolean'};integer={'type':'integer'}
schemas={
 'ApplicationInput':obj({'name':st(minLength=1,maxLength=160),'description':st(maxLength=4096),'instanceId':st(),'workspaceId':st(),'entryUrl':st(maxLength=2048),'revision':integer},['name','instanceId']),
 'Application':obj({'appId':st(),'name':st(),'description':st(),'instanceId':st(),'workspaceId':st(),'entryUrl':st(),'origin':st(),'revision':integer,'created':st(),'sessionCount':integer,'activeCount':integer,'lastActivity':st()},['appId','name','instanceId','revision']),
 'SkillBundle':obj({'name':st(),'files':arr(obj({'path':st(),'size':integer,'executable':bool_},['path','size','executable'])),'size':integer,'backupPath':st()},['name','files','size']),
 'SkillFilePreview':obj({'path':st(),'size':integer,'binary':bool_,'text':st(),'truncated':bool_},['path','size','binary']),
 'ErrorDetails':obj({'time':st(),'origin':st(enum=['rundesk','codex_rpc','codex_transport']),'method':st(),'path':st(),'durationMs':{'type':'integer'},'causes':arr(st()),'transportOperation':st(),'rpcCode':{'type':'integer'},'rpcData':{}},['time','origin','causes']),
 'Error':obj({'error':st(),'code':st(),'requestId':st(),'retryable':bool_,'details':ref('ErrorDetails')},['error','code','requestId','retryable']),
 'Skill':obj({'name':st(),'path':st()},['name','path']),
 'Input':obj({'text':st(minLength=1,maxLength=262144),'files':arr(st()),'skills':arr(ref('Skill'))},['text']),
 'RetryNotice':obj({'message':st(),'time':st()},['message','time']),
 'RecoveryOrigin':obj({'planId':st(),'sourceRunId':st(),'sourceTurnId':st(),'rootRunId':st(),'checkedAt':st()},['planId','sourceRunId','rootRunId','checkedAt']),
 'RecoveryStep':obj({'eventId':integer,'name':st(),'status':st()},['eventId','name','status']),
 'NativeRecovery':obj({'status':st(),'turnId':st(),'turnStatus':st(),'reply':st()},['status']),
 'RecoveryRequest':obj({'planId':st(),'expectedRunId':st(),'reviewedEffects':bool_,'issueResolved':bool_,'note':st(description='最多 4000 UTF-8 字节。')},['planId','expectedRunId']),
 'RecoveryPlan':obj({k:st() for k in ['id','sessionId','sourceRunId','sourceTurnId','threadId','rootRunId','checkedAt','sourceUpdated','category','reason','failure']}|{'instanceRevision':integer,'canContinue':bool_,'requiresReview':bool_,'requiresFix':bool_,'taskInput':ref('Input'),'steps':arr(ref('RecoveryStep')),'artifacts':arr(st()),'truncated':bool_,'submitted':bool_,'native':ref('NativeRecovery')},['id','sessionId','sourceRunId','checkedAt','category','reason','canContinue','requiresReview','requiresFix','taskInput','steps','artifacts','native']),
 'SteerInput':obj({'text':st(),'files':arr(st()),'skills':arr(ref('Skill')),'expectedTurnId':st(),'requestId':st(minLength=8,maxLength=128)},['text','expectedTurnId','requestId']),
 'Source':obj({'kind':st(enum=['human','application']),'appId':st(maxLength=80),'taskId':st(maxLength=200)},extra=False),
 'TraceSelection':obj({'sessionId':st(),'runId':st(),'eventIds':arr(integer)},['sessionId','runId']),
 'TraceOrigin':obj({'sessionId':st(),'runId':st(),'eventIds':arr(integer),'through':integer,'capturedAt':st(),'title':st()},['sessionId','runId','through','capturedAt']),
 'CreateSession':{'oneOf':[obj({'workspaceId':st(),'instanceId':st(default='default'),'title':st(),'model':st(),'source':ref('Source')},['workspaceId']),obj({'traceAnalysis':ref('TraceSelection')},['traceAnalysis'])]},
 'Session':obj({k:st() for k in ['id','instanceId','workspaceId','title','threadId','model','status','runId','turnId','error','created','updated']}|{'pinned':bool_,'archived':bool_,'source':ref('Source'),'traceOrigin':ref('TraceOrigin'),'recovery':ref('RecoveryOrigin'),'retry':ref('RetryNotice')},['id','instanceId','workspaceId','status'],True),
 'Permissions':obj({'sandbox':st(enum=['workspace-write','read-only','danger-full-access']),'approvalPolicy':st(enum=['on-request','never']),'reviewer':st(enum=['user','auto_review']),'networkAccess':bool_}),
 'InstanceInput':obj({'name':st(minLength=1),'description':st(),'defaultModel':st(),'permissions':ref('Permissions'),'revision':integer},['name']),
 'Instance':obj({'id':st(),'name':st(),'description':st(),'defaultModel':st(),'codexHome':st(),'revision':integer,'managed':bool_,'created':st(),'permissions':ref('Permissions')},['id','name','revision'],True),
 'Workspace':obj({'id':st(),'name':st(),'path':st(),'notes':st(),'revision':integer},['id','name','path'],True),
 'Event':obj({'id':{'type':'integer','format':'int64'},'sessionId':st(),'time':st(),'direction':st(),'method':st(),'data':{}},['id','sessionId','time','direction','method','data'],True),
 'Receipt':obj({'key':st(),'method':st(),'path':st(),'state':st(enum=['processing','completed','unconfirmed']),'httpStatus':integer,'response':{},'created':st()},['key','state','response'],True),
 'Configuration':obj({'instance':ref('Instance'),'workspace':obj(extra=True),'session':ref('Session'),'sessionCount':integer,'probed':bool_,'observedAt':st(),'errors':{'type':'object','additionalProperties':st()},'skills':arr(obj(extra=True)),'skillWarnings':arr({}),'mcp':obj(extra=True),'runtime':obj(extra=True),'lastSubmission':obj(extra=True)},['instance','workspace','probed','observedAt','errors'],True),
 'Reply':obj({'sessionId':st(),'eventId':integer,'itemId':st(),'turnId':st(),'text':st(),'time':st()},['sessionId','eventId','itemId','turnId','text','time']),
 'FeedbackInput':obj({'rating':st(enum=['up','down','none']),'comment':st(maxLength=2000)},['rating']),
 'MessageFeedback':obj({'sessionId':st(),'eventId':integer,'rating':st(enum=['up','down','none']),'comment':st(),'updated':st()},['sessionId','eventId','rating','comment','updated']),
 'NativeObject':obj(extra=True),
}
schemas.update({
 'TaskSpec':obj({'title':st(maxLength=120),'workspaceId':st(),'instanceId':st(),'model':st(),'source':ref('Source'),'input':ref('Input'),'notBefore':st(format='date-time')},['workspaceId','input']),
 'Task':obj({k:st() for k in ['id','sessionId','runId','status','reason','created','updated','startedAt','finishedAt','scheduleId','scheduledFor']}|{'spec':ref('TaskSpec'),'cancelRequested':bool_},['id','sessionId','status','spec']),
 'QueueSettings':obj({'revision':integer,'paused':bool_,'maxConcurrent':{'type':'integer','minimum':1,'maximum':16},'perInstance':{'type':'object','additionalProperties':{'type':'integer','minimum':1,'maximum':16}}},['revision','paused','maxConcurrent','perInstance']),
 'TaskPage':obj({'items':arr(ref('Task')),'nextCursor':st()},['items','nextCursor'])
})
paths={}
requests={
 ('PUT','/queue'):ref('QueueSettings'),('POST','/tasks'):ref('TaskSpec'),
 ('PUT','/applications/{appId}'):ref('ApplicationInput'),
 ('PUT','/sessions/{sid}/messages/{eid}/feedback'):ref('FeedbackInput'),
 ('POST','/login'):obj({'token':st()},['token']),
 ('POST','/instances'):ref('InstanceInput'),('PATCH','/instances/{iid}'):obj({'name':st(),'description':st(),'defaultModel':st(),'permissions':ref('Permissions'),'revision':integer},['revision']),
 ('POST','/workspaces'):obj({'name':st(),'path':st()}),
 ('PUT','/workspaces/{wid}/notes'):obj({'text':st(),'revision':integer},['text','revision']),
 ('POST','/sessions'):ref('CreateSession'),('POST','/sessions/{sid}/turns'):ref('Input'),('POST','/sessions/{sid}/steer'):ref('SteerInput'),
 ('POST','/sessions/{sid}/recovery/check'):obj({'expectedRunId':st()},['expectedRunId']),
 ('POST','/sessions/{sid}/recover'):ref('RecoveryRequest'),
 ('POST','/sessions/{sid}/stop'):obj({'expectedRunId':st()},['expectedRunId']),
 ('PATCH','/sessions/{sid}'):obj({'title':st(),'pinned':bool_,'archived':bool_}),
 ('POST','/sessions/{sid}/approvals/{aid}'):obj({'decision':{},'scope':st(enum=['turn','session']),'answers':obj(extra=True),'content':{}}),
 ('PUT','/workspaces/{wid}/skills/{name}'):obj({'content':st()},['content']),
 ('POST','/workspaces/{wid}/skills/toggle'):obj({'path':st(),'enabled':bool_},['path','enabled']),
 ('PUT','/workspaces/{wid}/mcp/{name}'):obj({'version':st(),'config':obj(extra=True),'remove':bool_},['version']),
 ('POST','/workspaces/{wid}/mcp/import'):obj({'version':st(),'bundle':obj(extra=True),'overwrite':bool_},['version','bundle']),
}
idempotent={('POST',p) for p in ['/instances','/workspaces','/sessions','/sessions/{sid}/turns','/sessions/{sid}/recover','/tasks']}
summaries={
 '/sessions/{sid}/recovery/check':'核对当前失败或中断轮次；15 分钟内有效，每会话只保留最近一次核对',
 '/sessions/{sid}/recover':'按服务端核对结果继续原会话；重新核对原生状态，新轮次关联失败来源',
 '/applications':'应用列表与任务统计','/applications/{appId}':'读取或登记应用；绑定不可变，修改信息需 revision','/workspaces/{wid}/skill-bundles':'导入完整技能 ZIP 或目录；替换会完整备份原目录',
 '/meta':'版本、运行模式与能力发现','/instances':'实例列表与创建','/sessions':'会话列表与创建','/sessions/{sid}/turns':'提交新任务，返回原始接收回执','/sessions/{sid}/steer':'向当前轮追加要求','/sessions/{sid}/stop':'请求停止当前任务','/sessions/{sid}/events':'持久化事件分页或 SSE 订阅','/requests/{key}':'查询幂等提交回执','/instances/{iid}/configuration':'实例配置总览','/sessions/{sid}/configuration':'会话配置与上次执行记录','/openapi.json':'OpenAPI 3.1 契约',
}
for file in sorted((root/'internal/app').glob('*.go')):
 if file.name.endswith('_test.go'):continue
 for method,path in re.findall(r'mux.HandleFunc\("(GET|POST|PUT|PATCH|DELETE) /api([^" ]*)"',file.read_text()):
  params=[{'name':n,'in':'path','required':True,'schema':st()} for n in re.findall(r'\{(.*?)\}',path)]
  def query(name,schema=st(),required=False):params.append({'name':name,'in':'query','required':required,'schema':schema})
  if path.startswith('/workspaces/{wid}/') and any('/'+x in path for x in ['skills','skill-bundles','mcp','models','config','diagnostics','account']):query('instanceId')
  if path=='/workspaces/{wid}/skills/{name}':query('scope',st(enum=['instance','project'],default='project'))
  if '/skill-bundles' in path:
   query('scope',st(enum=['instance','project'],default='instance'))
   if path.endswith('/skill-bundles'):query('name',required=True);query('replace',st(enum=['0','1'],default='0'))
  if path=='/tasks' and method=='GET':
   for n in ['status','appId','instanceId','cursor']:query(n)
   query('limit',integer)
  if path=='/sessions':
   if method=='GET':
    for n in ['instanceId','workspaceId','appId','taskId']:query(n)
  if path.endswith('/configuration'):
   query('probe',st(enum=['0','1'],default='0'))
   if '/instances/' in path:query('workspaceId',required=True)
  if path.endswith('/events'):
   for n in ['after','limit']:query(n,integer)
   query('stream',st(enum=['0','1']))
   for n in ['direction','method','q','category','from','to']:query(n)
   params.append({'name':'Last-Event-ID','in':'header','schema':st()})
  if path.endswith('/trace'):
   for n in ['after','limit','through']:query(n,integer)
  if path.endswith('/export'):query('format',st(enum=['markdown','jsonl']))
  if path.endswith('/file'):query('path',required=True);query('preview',st(enum=['0','1']))
  params.append({'name':'X-Request-ID','in':'header','schema':st(),'description':'可选关联编号；响应回传有效编号，否则由服务器生成。'})
  if (method,path) in idempotent:params.append({'name':'Idempotency-Key','in':'header','required':True,'schema':st(pattern='^[A-Za-z0-9][A-Za-z0-9_.:-]{7,127}$'),'description':'每个逻辑操作唯一。相同请求重试必须复用，不能将 Key 用于不同请求。'})
  response=ref('NativeObject'); status='202' if path.endswith(('/turns','/recover')) else '200'
  if path=='/tasks' and method=='GET':
   for n in ['status','appId','instanceId','cursor']:query(n)
   query('limit',integer)
  if path=='/sessions':response=arr(ref('Session')) if method=='GET' else ref('Session')
  if path=='/sessions/{sid}' and method!='DELETE' or path.endswith(('/turns','/recover')):response=ref('Session')
  if path.endswith('/recovery/check'):response=ref('RecoveryPlan')
  if path=='/applications':response=arr(ref('Application'))
  if path=='/applications/{appId}':response=ref('Application')
  if '/skill-bundles' in path:response=ref('SkillBundle')
  if '/skill-bundles/' in path and path.endswith('/file'):response=ref('SkillFilePreview')
  if '/skill-bundles/' in path and method=='DELETE':response=obj({'backupPath':st()},['backupPath'])
  if path=='/instances':response=arr(ref('Instance')) if method=='GET' else ref('Instance')
  if path=='/instances/{iid}':response=ref('Instance')
  if path=='/workspaces':response=arr(ref('Workspace')) if method=='GET' else ref('Workspace')
  if path.endswith('/configuration'):response=ref('Configuration')
  if path=='/sessions/{sid}/messages/{eid}':response=ref('Reply')
  if path=='/sessions/{sid}/feedback':response=arr(ref('MessageFeedback'))
  if path=='/sessions/{sid}/messages/{eid}/feedback':response=ref('MessageFeedback')
  if path.startswith('/requests/'):response=ref('Receipt')
  if path.endswith('/events'):response=arr(ref('Event'))
  if path.endswith('/approvals') or path.endswith('/files') or path=='/instances/{iid}/runtime':response=arr(ref('NativeObject'))
  if path.endswith('/events/{eid}'):response=ref('Event')
  if path=='/queue':response=ref('QueueSettings')
  if path=='/tasks':response=ref('TaskPage') if method=='GET' else ref('Task');status='202' if method=='POST' else '200'
  if path.startswith('/tasks/'):response=ref('Task')
  content={'application/json':{'schema':response}}
  if path.endswith('/events'):content['text/event-stream']={'schema':st(),'example':'id: 123\ndata: {"id":123,"sessionId":"...","method":"run/state","data":{}}\n\n'}
  if path.endswith('/file') or path.endswith('/export') and not '/mcp/' in path:content={'application/octet-stream':{'schema':st(format='binary')}}
  if '/skill-bundles/' in path and path.endswith('/file'):content={'application/json':{'schema':response}}
  if '/skill-bundles/' in path and path.endswith('/export'):content={'application/zip':{'schema':st(format='binary')}}
  if path.endswith('/skills/{name}') and method=='GET':content={'application/json':{'schema':obj({'content':st()},['content'])}}
  op={'operationId':method.lower()+'_'+re.sub(r'[^A-Za-z0-9]+','_',path).strip('_'),'summary':summaries.get(path,path),'parameters':params,'responses':{status:{'description':'成功。202 仅表示任务已接收。','content':content,'headers':{'X-Request-ID':{'schema':st()},'RunDesk-API-Version':{'schema':st(enum=['v1'])},'Idempotency-Replayed':{'schema':st(enum=['true','false']),'description':'仅适用于幂等提交；true 表示返回原始回执。'}}},'default':{'description':'结构化错误。retryable=true 仅允许按相同操作与 Key 重试。','content':{'application/json':{'schema':ref('Error')}}}}}
  if path=='/login':op['security']=[]
  if (method,path) in requests:op['requestBody']={'required':True,'content':{'application/json':{'schema':requests[(method,path)]}}}
  if path.endswith('/uploads'):op['requestBody']={'required':True,'content':{'multipart/form-data':{'schema':obj({'file':st(format='binary')},['file'])}}}
  if path.endswith('/skill-bundles') and method=='POST':op['requestBody']={'required':True,'content':{'application/zip':{'schema':st(format='binary')},'multipart/form-data':{'schema':{'type':'object','additionalProperties':st(format='binary'),'description':'每个 multipart 字段名为文件相对路径；根部 SKILL.md 或一层技能文件夹。最多 1000 文件、解压后 32 MiB。'}}}}
  paths.setdefault(path,{})[method.lower()]=op
spec={'openapi':'3.1.0','info':{'title':'RunDesk Application API','version':'1.5.0','description':'RunDesk 0.9.0。应用与专用 instance 一对一绑定，default 保留给通用助手。支持完整技能目录和关联的轨迹分析会话。/api/v1 是稳定的应用入口，旧 /api 保留。NativeObject 透传原生 Codex 结果，其内部字段受原生版本影响。Source 是调用方声明的业务标签，不代表应用鉴权或隔离。此版本继续使用后台 Token/Cookie。'},'servers':[{'url':'/api/v1'}],'security':[{'BearerAuth':[]},{'BrowserCookie':[]}],'paths':paths,'components':{'securitySchemes':{'BearerAuth':{'type':'http','scheme':'bearer'},'BrowserCookie':{'type':'apiKey','in':'cookie','name':'rundesk'}},'schemas':schemas}}
(root/'internal/app/openapi.json').write_text(json.dumps(spec,ensure_ascii=False,indent=2)+'\n')
print(f'{len(paths)} paths, {sum(len(v) for v in paths.values())} operations')
