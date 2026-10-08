"""Generate the shipped v1 contract from registered routes and curated schemas."""
from pathlib import Path
import json,re,copy
root=Path(__file__).resolve().parents[1]
def obj(props=None,required=(),extra=False):
 return dict(type='object',properties=props or {},required=list(required),additionalProperties=extra)
def st(**kwargs):return dict(type='string',**kwargs)
def ref(name):return {'$ref':'#/components/schemas/'+name}
def arr(item):return {'type':'array','items':item}
bool_={'type':'boolean'};integer={'type':'integer'}
schemas={
 'ApplicationConnect':obj({'name':st(),'description':st(),'installationId':st(),'entryUrl':st(),'instanceId':st(description='迁移旧配置时可选'),'workspaceId':st(),'defaults':obj({'model':st(),'skills':{'type':'object','additionalProperties':{'type':'object','additionalProperties':st()}},'mcp':{'type':'object','additionalProperties':{'type':'object','additionalProperties':True}}})},['name','installationId']),
 'ApplicationConnectionResult':obj({'application':ref('Application'),'status':st(enum=['connected','needs_attention']),'message':st(),'managementPath':st()},['application','status','message','managementPath']),

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
 'TraceSelection':obj({'sessionId':st(),'runId':st(),'eventIds':arr(integer),'through':{'type':'integer','minimum':0,'description':'可选固定宿主游标；省略取创建时最新，显式 0 不推进。空历史不能诊断。'}},['sessionId','runId']),
 'TraceOrigin':obj({'sessionId':st(),'runId':st(),'eventIds':arr(integer),'through':integer,'capturedAt':st(),'title':st()},['sessionId','runId','through','capturedAt']),
 'CreateSession':{'oneOf':[obj({'workspaceId':st(),'instanceId':st(description='管理员默认 default；应用凭据默认绑定的专用配置'),'title':st(),'model':st(),'source':ref('Source')},['workspaceId']),obj({'traceAnalysis':ref('TraceSelection')},['traceAnalysis'])]},
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
 'Task':obj({k:st() for k in ['id','sessionId','runId','status','reason','created','updated','startedAt','finishedAt','scheduleId','scheduledFor','fileOwner','submittingKeyId']}|{'spec':ref('TaskSpec'),'cancelRequested':bool_},['id','sessionId','status','spec']),
 'QueueSettings':obj({'revision':integer,'paused':bool_,'maxConcurrent':{'type':'integer','minimum':1,'maximum':16},'perInstance':{'type':'object','additionalProperties':{'type':'integer','minimum':1,'maximum':16}}},['revision','paused','maxConcurrent','perInstance']),
 'TaskPage':obj({'items':arr(ref('Task')),'nextCursor':st()},['items','nextCursor'])
})
schemas.update({
 'ScheduleSpec':obj({'name':st(),'cron':st(description='五段：分 时 日 月 周'),'timezone':st(description='IANA 时区，禁止 Local'),'enabled':bool_,'misfire':st(enum=['skip','once']),'overlap':st(enum=['skip','queue']),'task':ref('TaskSpec')},['name','cron','timezone','enabled','task']),
 'Schedule':obj({k:st() for k in ['id','created','updated','nextAt','lastAt','lastTaskId','lastReason','submittingKeyId']}|{'spec':ref('ScheduleSpec'),'revision':integer},['id','spec','revision']),
})
schemas.update({
 'ApplicationKey':obj({k:st() for k in ['id','appId','instanceId','name','created','expiresAt','revokedAt']}|{'workspaceIds':arr(st()),'scopes':arr(st(enum=['read','run','schedules','files','approvals']))},['id','appId','instanceId','name','created','workspaceIds','scopes']),
 'KeyInput':obj({'name':st(minLength=1,maxLength=120),'workspaceIds':arr(st()),'scopes':arr(st(enum=['read','run','schedules','files','approvals'])),'expiresAt':st(format='date-time')},['name','workspaceIds','scopes']),
 'WorkspaceSummary':obj({'id':st(),'name':st()},['id','name'])
})
schemas.update({
 'ExecutionSpec':obj({'mode':st(enum=['local','docker']),'image':st(maxLength=255),'imageId':st(description='登记版本的固定 Image ID，必须与 imageVersionId 一致'),'imageVersionId':st(),'cpus':{'type':'integer','minimum':1,'maximum':32},'memoryMB':{'type':'integer','minimum':256,'maximum':65536},'pidsLimit':{'type':'integer','minimum':32,'maximum':4096},'idleMinutes':{'type':'integer','minimum':1,'maximum':1440},'network':st(enum=['bridge','none'])},['mode']),
 'Environment':obj({k:st() for k in ['id','instanceId','workspaceId','containerName','containerId','imageId','codexHome','state','error','created','observedAt','lastUsed','deployedAt']}|{'spec':ref('ExecutionSpec'),'exitCode':integer,'oomKilled':bool_,'revision':integer,'activeConnections':integer},['id','instanceId','workspaceId','spec','revision','state']),
})
schemas['ImageVersion']=obj({k:st() for k in ['id','instanceId','reference','imageId','os','architecture','created','version','source','revision','buildCreated','dockerfile','buildUrl','codexVersion','tools','skills','mcp','registeredAt','checkedAt','error']}|{'repoDigests':arr(st()),'size':integer,'availability':st(enum=['available','unavailable'])},['id','instanceId','reference','imageId','registeredAt','checkedAt','availability'])
schemas['ImageCatalog']=obj({'versions':arr(ref('ImageVersion')),'execution':ref('ExecutionSpec'),'instanceRevision':integer,'environments':arr(ref('Environment')),'pendingUpdates':integer},['versions','execution','instanceRevision','environments','pendingUpdates'])
schemas['Instance']['properties']['execution']=ref('ExecutionSpec')
schemas['Session']['properties'].update({'environmentId':st(),'executionMode':st(enum=['local','docker'])})
paths={}
requests={
 ('POST','/workspaces/{wid}/mcp-tests'):obj({'server':st(),'tool':st(),'arguments':obj({},extra=True),'cursor':st(),'confirm':bool_},['server','confirm']),
 ('PUT','/setup'):obj({'codex':st(),'workspaceId':st(),'model':st(),'completed':bool_,'revision':integer},['codex','workspaceId','revision']),
 ('POST','/setup/check'):obj({'kind':st(enum=['codex','workspace','models','account','mcp','docker']),'workspaceId':st(),'instanceId':st()},['kind']),
 ('POST','/setup/provider'):obj({'workspaceId':st(),'baseUrl':st(),'envKey':st(),'model':st()},['workspaceId','baseUrl','model']),
 ('POST','/instances/{iid}/images'):obj({'reference':st()},['reference']),
 ('POST','/instances/{iid}/images/{vid}/select'):obj({'revision':integer},['revision']),
 ('PUT','/instances/{iid}/execution'):obj({'revision':integer,'spec':ref('ExecutionSpec')},['revision','spec']),
 ('POST','/environments'):obj({'instanceId':st(),'workspaceId':st()},['instanceId','workspaceId']),
 ('POST','/environments/{eid}/actions'):obj({'revision':integer,'applicationRevision':integer,'action':st(enum=['start','stop','recreate','remove','inspect'])},['revision','action']),
 ('POST','/applications/{appId}/keys'):ref('KeyInput'),
 ('POST','/schedules'):ref('ScheduleSpec'),('PUT','/schedules/{id}'):obj({'spec':ref('ScheduleSpec'),'revision':integer},['spec','revision']),('DELETE','/schedules/{id}'):obj({'revision':integer},['revision']),('POST','/schedules/preview'):obj({'cron':st(),'timezone':st()},['cron','timezone']),
 ('PUT','/queue'):ref('QueueSettings'),('POST','/tasks'):ref('TaskSpec'),
 ('PUT','/applications/{appId}'):ref('ApplicationInput'),
 ('POST','/applications/{appId}/connect'):ref('ApplicationConnect'),
 ('POST','/applications/{appId}/capabilities'):obj({'skills':arr(st()),'mcp':arr(st())}),
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
idempotent={('POST',p) for p in ['/kun-forks','/kun-forks/{fid}/start','/experiments','/experiments/{eid}/branches','/instances','/workspaces','/sessions','/sessions/{sid}/turns','/sessions/{sid}/recover','/sessions/{sid}/kun/resume','/tasks','/schedules','/workspaces/{wid}/mcp-tests']}
summaries={
 '/instances/{iid}/images':'管理员：GET 读取持久化镜像目录及环境差异；POST 检查并登记本机镜像，不构建、不拉取、不启动容器',
 '/instances/{iid}/images/{vid}/check':'管理员：按固定 Image ID 核对可用性；失败仍返回记录，检查 availability 和 error',
 '/instances/{iid}/images/{vid}/select':'管理员：选择应用目标版本，需应用 revision；已有环境继续沿用原版本，需显式更新',
 '/docker/status':'管理员：检查本机 Docker Engine；不启动模型',
 '/instances/{iid}/execution':'管理员：修改应用运行方式和镜像；default 只能 local，需 revision',
 '/environments':'管理员：按应用列出环境；POST 幂等地取得应用与项目对应环境，只创建元数据',
 '/environments/{eid}':'管理员：从 Docker 核对状态、退出码和 OOM 标记',
 '/environments/{eid}/actions':'管理员：启动、停止、重建或移除空闲容器；始终保留宿主机数据',
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
  if path=='/sessions':response=arr(ref('Session')) if method=='GET' else ref('Session')
  if path=='/sessions/{sid}' and method!='DELETE' or path.endswith(('/turns','/recover')):response=ref('Session')
  if path.endswith('/recovery/check'):response=ref('RecoveryPlan')
  if path=='/applications':response=arr(ref('Application'))
  if path=='/applications/{appId}':response=ref('Application')
  if path=='/applications/{appId}/connect':response=ref('ApplicationConnectionResult')
  if '/skill-bundles' in path:response=ref('SkillBundle')
  if '/skill-bundles/' in path and path.endswith('/file'):response=ref('SkillFilePreview')
  if '/skill-bundles/' in path and method=='DELETE':response=obj({'backupPath':st()},['backupPath'])
  if path=='/instances':response=arr(ref('Instance')) if method=='GET' else ref('Instance')
  if path=='/instances/{iid}' or path=='/instances/{iid}/execution':response=ref('Instance')
  if path=='/instances/{iid}/images':response=ref('ImageCatalog') if method=='GET' else ref('ImageVersion')
  if path=='/instances/{iid}/images/{vid}/check':response=ref('ImageVersion')
  if path=='/instances/{iid}/images/{vid}/select':response=ref('Instance')
  if path=='/environments':
   response=arr(ref('Environment')) if method=='GET' else ref('Environment')
   if method=='GET':query('instanceId')
  if path.startswith('/environments/'):response=ref('Environment')
  if path=='/workspaces':response=arr({'oneOf':[ref('Workspace'),ref('WorkspaceSummary')]}) if method=='GET' else ref('Workspace')
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
  if path=='/schedules':response=arr(ref('Schedule')) if method=='GET' else ref('Schedule')
  if path=='/schedules/{id}' and method!='DELETE':response=ref('Schedule')
  if path=='/schedules/preview':response=obj({'times':arr(st(format='date-time')),'timezone':st()},['times','timezone'])
  if path=='/whoami':response={'oneOf':[obj({'kind':st(const='administrator')},['kind']),obj({'kind':st(const='application'),'credential':ref('ApplicationKey')},['kind','credential'])]}
  if path=='/applications/{appId}/keys':response=arr(ref('ApplicationKey')) if method=='GET' else obj({'credential':ref('ApplicationKey'),'token':st(description='只在创建响应显示一次，丢失需撤销重建')},['credential','token']);status='201' if method=='POST' else '200'
  if path=='/applications/{appId}/keys/{keyId}':response=ref('ApplicationKey')
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
  scopes=None
  if method=='GET' and path in ['/meta','/openapi.json','/whoami','/workspaces','/sessions','/tasks','/tasks/{tid}','/requests/{key}']:scopes=['read']
  if path.startswith('/schedules'):scopes=['read','run','schedules']
  if method=='POST' and path in ['/tasks','/tasks/{tid}/cancel','/sessions']:scopes=['read','run']
  if path.startswith('/sessions/{sid}'):
   if method=='GET' and path in ['/sessions/{sid}','/sessions/{sid}/events','/sessions/{sid}/trace','/sessions/{sid}/export','/sessions/{sid}/feedback','/sessions/{sid}/approvals','/sessions/{sid}/events/{eid}','/sessions/{sid}/messages/{eid}']:scopes=['read']
   if (method in ['PATCH','DELETE'] and path=='/sessions/{sid}') or method=='POST' and path in ['/sessions/{sid}/turns','/sessions/{sid}/steer','/sessions/{sid}/stop','/sessions/{sid}/recover','/sessions/{sid}/recovery/check']:scopes=['read','run']
   if path=='/sessions/{sid}/approvals/{aid}' and method=='POST':scopes=['read','run','approvals']
  if (method=='GET' and path in ['/sessions/{sid}/files','/workspaces/{wid}/file']) or method=='POST' and path=='/workspaces/{wid}/uploads':scopes=['read','files']
  op['x-administrator-only']=scopes is None and path!='/login'
  if scopes:op['x-application-scopes']=scopes;op['description']='应用凭据还必须符合服务端的应用、专用配置和项目归属校验。'
  paths.setdefault(path,{})[method.lower()]=op

# Human member API is a constrained projection of application routes.
schemas['UserGrant']=obj({'id':st(),'appId':st(),'workspaceId':st(),'role':st(enum=['viewer','runner'])},['appId','workspaceId','role'])
schemas['UserInput']=obj({'name':st(maxLength=120),'enabled':bool_,'revision':integer,'grants':arr(ref('UserGrant'))},['name','grants'])
schemas['User']=obj({'id':st(),'name':st(),'enabled':bool_,'revision':integer,'grants':arr(ref('UserGrant')),'created':st(),'updated':st()},['id','name','enabled','revision','grants'])
schemas['UserCode']=obj({'user':ref('User'),'accessCode':st(description='仅本次返回；服务端只保存哈希。')},['user','accessCode'])
for path,ops in paths.items():
 for method,op in ops.items():
  schema=None
  if path=='/users':schema=arr(ref('User')) if method=='get' else ref('UserCode')
  if path=='/users/{uid}':schema=ref('User')
  if path=='/users/{uid}/access-code':schema=ref('UserCode')
  if schema:op['responses']['200']['content']['application/json']['schema']=schema
  if path in ['/users','/users/{uid}'] and method in ['post','put']:
   op['requestBody']={'required':True,'content':{'application/json':{'schema':ref('UserInput')}}}
   op['description']='仅管理员。创建始终启用账号；更新必须带当前 revision，变更会使现有浏览器登录失效。'
  if path=='/users/{uid}/access-code':op['requestBody']={'required':True,'content':{'application/json':{'schema':obj({'revision':integer},['revision'])}}}
  if path=='/whoami':op['responses']['200']['content']['application/json']['schema']['oneOf'].append(obj({'kind':st(const='user'),'user':ref('User')},['kind','user']))
  if path=='/member/catalog':
   op['x-administrator-only']=False;op['x-personal-user-only']=True
   op['responses']['200']['content']['application/json']['schema']=obj({'user':ref('User'),'projects':arr(obj({'grant':ref('UserGrant'),'applicationName':st(),'projectName':st(),'canRun':bool_},['grant','applicationName','projectName','canRun']))},['user','projects'])
# Kun's own protocol is distinct from native Codex objects.
schemas['KunBudgetLimits']=obj({'maxToolCalls':{'type':'integer','minimum':0,'maximum':3200,'default':64},'maxTotalTokens':{'type':'integer','minimum':0,'maximum':1000000000,'default':0,'description':'Reported usage threshold, not a hard per-request token or billing cap.'},'maxActiveSeconds':{'type':'integer','minimum':0,'maximum':86400,'default':900},'maxConsecutiveFailures':{'type':'integer','minimum':0,'maximum':100,'default':3}})
schemas['KunBudgetUsage']=obj({'toolCalls':integer,'reportedTokens':integer,'unreportedModelCalls':integer,'consecutiveFailures':integer,'activeMillis':integer,'waitMillis':integer,'stopReason':st()})
schemas['KunModuleVersion']=obj({'id':st(),'version':st(),'stateSchemaVersion':integer},['id','version','stateSchemaVersion'])
schemas['KunModuleState']=obj({'implementation':ref('KunModuleVersion'),'phase':st(),'data':obj(extra=True)},['implementation','phase','data'])
schemas['KunHarness']=obj({'id':st(),'version':st(),'revision':integer,'modules':{'type':'object','additionalProperties':ref('KunModuleVersion')}})

schemas['KunBreakpoint']=obj({'id':st(pattern='^[A-Za-z0-9_-]{1,64}$'),'phase':st(enum=['before_model','after_model','before_tool','after_tool']),'tool':st(maxLength=256),'model':st(maxLength=160),'minStep':{'type':'integer','minimum':0,'maximum':100},'minToolCalls':{'type':'integer','minimum':0,'maximum':3200},'minFailures':{'type':'integer','minimum':0,'maximum':100},'minReportedTokens':{'type':'integer','minimum':0,'maximum':1000000000},'once':bool_},['id','phase'])
schemas['KunDebugPolicy']=obj({'breakpoints':{'type':['array','null'],'items':ref('KunBreakpoint'),'maxItems':16},'pauseTimeoutSeconds':{'type':'integer','minimum':0,'maximum':86400}})
schemas['KunDebugPause']=obj({'reason':st(),'phase':st(),'ruleIds':arr(st()),'callId':st(),'policyRevision':integer,'deadline':st(format='date-time')},['reason','phase','policyRevision'])
schemas['KunDebugState']=obj({'revision':integer,'policy':ref('KunDebugPolicy'),'hits':{'type':'object','additionalProperties':integer},'pause':ref('KunDebugPause')},['revision','policy'])
schemas['KunSnapshotRef']=obj({'sequence':integer,'runId':st(),'revision':integer},['sequence','runId','revision'])
schemas['KunValuePreview']=obj({'type':st(),'bytes':integer,'preview':st(maxLength=512),'truncated':bool_},['type','bytes','preview','truncated'])
schemas['KunSnapshotChange']=obj({'path':st(maxLength=512,description='JSON Pointer unless pathTruncated is true; bounded at 512 Unicode characters.'),'pathTruncated':bool_,'change':st(enum=['add','remove','replace']),'before':ref('KunValuePreview'),'after':ref('KunValuePreview')},['path','change'])
schemas['KunSnapshotDiff']=obj({'from':ref('KunSnapshotRef'),'to':ref('KunSnapshotRef'),'changes':arr(ref('KunSnapshotChange')),'truncated':bool_,'limit':integer,'nodeLimit':integer,'previewLimit':integer,'arrayMatch':st(enum=['position']),'redaction':st()},['from','to','changes','truncated','limit','nodeLimit','previewLimit','arrayMatch','redaction'])
schemas['KunDebugResult']=obj({'sessionId':st(),'runId':st(),'revision':integer,'sequence':integer,'kind':st(enum=['run','context','tools','budget','modules','breakpoints','actions','evidence','diff']),'data':obj(extra=True)},['sessionId','runId','revision','sequence','kind','data'])
schemas['KunHarnessConfig']=obj({'loopPolicy':st(enum=['tool-loop-v1','plan-act-v1']),'memory':st(enum=['full-history-v1']),'planning':st(enum=['no-explicit-plan-v1','explicit-plan-v1']),'action':st(enum=['schema-action-v1']),'capability':st(enum=['fixed-catalog-v1'])},[])
schemas['KunHarnessConfig']['description']='Two installed compositions only: tool-loop-v1 pairs with no-explicit-plan-v1; plan-act-v1 pairs with explicit-plan-v1. Missing fields use the selected preset defaults. Unknown IDs and incompatible pairs are rejected. New ordinary runs may select a new composition; active runs, resume and Hybrid retain their recorded version.'
schemas['KunConfig']=obj({'harness':ref('KunHarnessConfig'),'kind':st(enum=['codex','kun']),'endpoint':st(),'model':st(),'apiKeyEnv':st(),'systemPrompt':st(maxLength=65536),'maxSteps':{'type':'integer','minimum':1,'maximum':100},'timeoutSeconds':{'type':'integer','minimum':1,'maximum':600},'allowWrite':bool_,'pauseBeforeModel':bool_,'budget':ref('KunBudgetLimits'),'debug':ref('KunDebugPolicy')},['kind'])
schemas['KunControl']=obj({'requestId':st(minLength=8,maxLength=128),'runId':st(),'expectedStateRevision':{'type':'integer','minimum':0},'operation':st(enum=['pause','resume','step','cancel','steer','approve','reject','set_breakpoints']),'debug':ref('KunDebugPolicy'),'text':st(maxLength=262144),'callId':st(description='Required for approve/reject; must match the pending call.')},['requestId','runId','expectedStateRevision','operation'])
schemas['KunReceipt']=obj({'requestId':st(),'status':st(enum=['queued','applied','rejected']),'revision':integer},['requestId','status','revision'])
schemas['KunToolCall']=obj({'id':st(),'type':st(),'function':obj({'name':st(),'arguments':st()},['name','arguments'])},['id','type','function'])
schemas['KunMessage']=obj({'role':st(),'content':st(),'tool_calls':arr(ref('KunToolCall')),'tool_call_id':st()},['role','content'])
schemas['KunSkill']=obj({'name':st(),'path':st(),'content':st(),'hash':st()},['name','path','content','hash'])
schemas['KunState']=obj({'schemaVersion':integer,'sessionId':st(),'runId':st(),'revision':integer,'status':st(),'phase':st(),'step':integer,'messages':arr(ref('KunMessage')),'pending':arr(ref('KunToolCall')),'actions':{'type':'object','additionalProperties':st()},'config':ref('KunConfig'),'skills':arr(ref('KunSkill')),'queuedControls':arr(ref('KunControl')),'error':st()},['schemaVersion','sessionId','runId','revision','status','phase','step','config','messages'])
schemas['KunMCPStatus']=obj({'name':st(),'configRevision':st(),'transport':st(),'status':st(),'protocolVersion':st(),'toolCount':integer,'error':st()},['name','configRevision','transport','status'])
schemas['KunMCPTool']=obj({'alias':st(),'server':st(),'name':st(),'description':st(),'inputSchema':obj(extra=True),'annotations':obj(extra=True),'approvalMode':st(enum=['approve','prompt'])},['alias','server','name','inputSchema','approvalMode'])
schemas['KunToolApproval']=obj({'callId':st(),'server':st(),'tool':st(),'arguments':st(),'decision':st(enum=['approve','reject'])},['callId','server','tool','arguments'])
schemas['KunState']['properties'].update({'debug':ref('KunDebugState'),'harness':ref('KunHarness'),'modules':{'type':'object','additionalProperties':ref('KunModuleState')},'budget':ref('KunBudgetUsage'),'toolDefinitions':arr(obj(extra=True)),'mcp':arr(ref('KunMCPStatus')),'mcpTools':arr(ref('KunMCPTool')),'approval':ref('KunToolApproval'),'approvalPolicy':st(enum=['on-request','never'])})
schemas['KunCheckpointSelection']=obj({'sourceRunId':st(),'sequence':{'type':'integer','minimum':1},'expectedStateRevision':{'type':'integer','minimum':1},'workerEpoch':st()},['sourceRunId','sequence','expectedStateRevision','workerEpoch'])
schemas['KunRunManifest']=obj({k:st() for k in ['engineVersion','workspace','configHash','mcpHash','skillsHash','harnessHash','contextRevision']},['engineVersion','workspace','configHash','mcpHash','skillsHash','harnessHash','contextRevision'])
schemas['KunCheckpointCheck']=obj({'eligible':bool_,'reason':st(),'selection':ref('KunCheckpointSelection'),'phase':st(),'step':integer,'pending':integer,'budget':ref('KunBudgetUsage')},['eligible','reason','selection','step','pending','budget'])
schemas['KunDiagnosticScope']=obj({'sessionId':st(),'runId':st(),'through':{'type':'integer','minimum':1}},['sessionId','runId','through'])
schemas['KunState']['properties']['diagnostic']=ref('KunDiagnosticScope')
schemas['KunState']['properties'].update({'manifest':ref('KunRunManifest'),'resumedFrom':ref('KunCheckpointSelection')})
schemas['KunQueuedControl']=copy.deepcopy(schemas['KunControl'])
schemas['KunQueuedControl']['properties']['expectedStateRevision']['minimum']=-1
schemas['KunState']['properties']['queuedControls']=arr(ref('KunQueuedControl'))
schemas['Session']['properties']['runtimeKind']=st(enum=['codex','kun'])
schemas['Instance']['properties']['agentRuntime']=ref('KunConfig')
for path,method,response,body in [
 ('/instances/{iid}/agent-runtime','put',ref('Instance'),obj({'revision':integer,'config':ref('KunConfig')},['revision','config'])),
 ('/sessions/{sid}/kun/state','get',ref('KunState'),None),
 ('/sessions/{sid}/kun/query','get',ref('KunDebugResult'),None),
 ('/sessions/{sid}/kun/checkpoint','get',ref('KunCheckpointCheck'),None),
 ('/sessions/{sid}/kun/resume','post',ref('Session'),ref('KunCheckpointSelection')),
 ('/sessions/{sid}/kun/snapshots/{sequence}','get',obj({'sequence':integer,'state':ref('KunState')},['sequence','state']),None),
 ('/sessions/{sid}/kun/control','post',ref('KunReceipt'),ref('KunControl')),
]:
 op=paths[path][method]
 op['summary']='Kun independent worker: '+path.rsplit('/',1)[-1]
 op['description']='Kun 0.9：本机独立 worker；条件断点、暂停期限、只读结构化查询。checkpoint 可重开离线 worker，仅核对最近安全检查点，不执行模型/MCP；resume 需 selection 与 Idempotency-Key，新 run 沿用上下文和预算、MCP 重连后核对工具并重新审批。旧版本、未知结果、配置变化、过期 selection 不可恢复。无历史回滚；Hybrid 使用独立 fork API。Plan-Act 规划与执行请求共用模型预算和四边界；purpose=plan 的结果不是最终回复。control 需当前 runId/revision；approve/reject 另需 approvals scope。'
 op['responses']['200']['content']['application/json']['schema']=response
 op['responses']['409']={'description':'Worker 离线、状态版本变化、ID 冲突或控制操作不适用；刷新状态后处理。'}
 if path.endswith('/kun/query'):
  op['parameters'] += [{'name':'kind','in':'query','required':True,'schema':st(enum=['run','context','tools','budget','modules','breakpoints','actions','evidence','diff'])},{'name':'sequence','in':'query','required':False,'schema':{'type':'integer','minimum':0},'description':'0 或省略：当前状态；正数：同一会话事件快照。查询不更改状态、不调用模型或工具。'}]
  op['parameters'] += [{'name':'fromSequence','in':'query','required':False,'schema':{'type':'integer','minimum':1},'description':'仅 diff 使用且必填；sequence 也必须大于 0。两份快照属于此会话，可跨运行。evidence 要求正数 sequence。'}]
  op['description'] += ' context 在 model.started 快照返回实际 model_request，其余为 state_context；evidence 返回精确事件；diff 返回 KunSnapshotDiff，先脱敏再比较，有界预览，不执行回滚。需要在线 worker。'
 if path.endswith('/kun/control'):
  op['description'] += ' set_breakpoints 必须带 debug，替换本 run 的规则并重置命中计数；不解除已有暂停，默认配置不变。未知条件字段被拒绝。'
 if body:op['requestBody']={'required':True,'content':{'application/json':{'schema':body}}}
 if path.startswith('/sessions/'):
  op['x-administrator-only']=False
  op['x-application-scopes']=['run' if method=='post' else 'read']
  if path.endswith('/kun/control'):op['x-conditional-scopes']={'approve':['approvals'],'reject':['approvals']}
 else:op['x-administrator-only']=True

# Administrator review records bind a retained suggestion to an immutable command.
schemas['DiagnosticProposal']=obj({'id':st(),'status':st(enum=['suggestion_only']),'sessionId':st(),'runId':st(),'through':integer,'kind':st(enum=['inspect','steer','configuration']),'reason':st(),'text':st(),'evidenceIds':arr(integer),'note':st()},['id','status','sessionId','runId','through','kind','reason','text','evidenceIds'])
schemas['DiagnosticReviewInput']=obj({'proposalEventId':{'type':'integer','minimum':1},'text':st(minLength=1,maxLength=4000)},['proposalEventId','text'])
schemas['DiagnosticReview']=obj({'id':st(),'diagnosticSessionId':st(),'proposalEventId':integer,'proposal':ref('DiagnosticProposal'),'command':ref('KunControl'),'observedStatus':st(),'observedPhase':st(),'createdAt':st(),'submittedAt':st(),'outcome':st(enum=['preview','unknown','queued','applied','rejected']),'error':st(),'receipt':ref('KunReceipt'),'receiptEventId':integer},['id','diagnosticSessionId','proposalEventId','proposal','command','observedStatus','observedPhase','createdAt','outcome'])
for path,method,response,body in [
 ('/sessions/{sid}/diagnostic/reviews','get',arr(ref('DiagnosticReview')),None),
 ('/sessions/{sid}/diagnostic/reviews','post',ref('DiagnosticReview'),ref('DiagnosticReviewInput')),
 ('/sessions/{sid}/diagnostic/reviews/{rid}/apply','post',ref('DiagnosticReview'),obj()),
]:
 op=paths[path][method]
 op['summary']='Kun diagnostic suggestion review: '+('history' if method=='get' else 'apply' if path.endswith('/apply') else 'preview')
 op['x-administrator-only']=True
 op['description']='仅管理员。只接通成功的 Kun trace_propose steer 建议；目标由宿主原始事件与固定来源确定。preview 保存审核文本和当前 runId/revision，不控制来源。apply 仅接受空对象，使用不可变预览和相同 worker requestId；状态变化拒绝，不重定向新运行。发送只排队，不继续运行或审批。queued 不表示已生效，applied 不表示业务成功。结果未知时刷新记录或重试同一 rid；不要另建预览重复发送。GET 返回此建议最近 20 条记录，结合宿主保留回执，不启动 worker。'
 op['responses']['200']['content']['application/json']['schema']=response
 op['responses']['409']={'description':'来源已改变、旧轮次、worker 离线、过期预览或控制被拒绝。'}
 op['responses']['502']={'description':'发送结果未知。查询记录或用同一 rid 重试。'}
 if body is not None:op['requestBody']={'required':True,'content':{'application/json':{'schema':body}}}
 if method=='get':op['parameters'].append({'name':'proposalEventId','in':'query','required':True,'schema':{'type':'integer','minimum':1}})

# K3-A offline recordings. These resources are not sessions or resumable workers.
schemas['ExperimentSource']=obj({'sessionId':st(),'runId':st(),'through':integer,'workspaceId':st(),'instanceId':st(),'title':st(),'backend':st(enum=['kun'])},['sessionId','runId','through','workspaceId','instanceId','title','backend'])
schemas['ExperimentPatch']=obj({'eventId':integer,'text':st(maxLength=16000),'reason':st(maxLength=2000),'branchId':st()},['eventId','text','reason','branchId'])
schemas['ExperimentChange']=obj({'eventId':{'type':'integer','minimum':1},'operation':st(enum=['replace','restore']),'text':st(maxLength=16000),'reason':st(minLength=1,maxLength=2000)},['eventId','operation','reason'])
schemas['ExperimentBranch']=obj({'schema':{'const':1},'id':st(),'rootId':st(),'parentId':st(),'parentHash':st(),'bundleHash':st(),'contentHash':st(),'source':ref('ExperimentSource'),'title':st(),'createdAt':st(),'mode':st(enum=['record_edit']),'depth':integer,'eventCount':integer,'patches':arr(ref('ExperimentPatch')),'change':ref('ExperimentChange')},['schema','id','rootId','bundleHash','contentHash','source','title','createdAt','mode','depth','eventCount','patches'])
schemas['ExperimentSummary']=obj({k:v for k,v in schemas['ExperimentBranch']['properties'].items() if k not in ['schema','parentHash','bundleHash','patches','change']}|{'patchCount':integer},['id','rootId','contentHash','source','title','createdAt','mode','depth','eventCount','patchCount'])
schemas['ExperimentCounts']=obj({'recorded':integer,'edited':integer,'stale':integer},['recorded','edited','stale'])
schemas['ExperimentEvent']=obj({'id':integer,'index':integer,'time':st(),'method':st(),'classification':st(enum=['recorded','edited','stale']),'downstreamUnverified':bool_,'editable':bool_,'toolName':st(),'patch':ref('ExperimentPatch')},['id','index','time','method','classification','downstreamUnverified','editable'])
schemas['ExperimentDifference']=obj({'eventId':integer,'method':st(),'before':ref('ExperimentEvent'),'after':ref('ExperimentEvent')},['eventId','method','before','after'])
schemas['ExperimentCreate']=obj({'sessionId':st(),'runId':st(),'through':{'type':'integer','minimum':1},'title':st(minLength=1,maxLength=120)},['sessionId','runId','title'])
schemas['ExperimentFork']=obj({'title':st(minLength=1,maxLength=120),'expectedParentHash':st(),'change':ref('ExperimentChange')},['title','expectedParentHash','change'])
for path,method,response,body in [
 ('/experiments','post',ref('ExperimentBranch'),ref('ExperimentCreate')),
 ('/experiments','get',obj({'items':arr(ref('ExperimentSummary')),'nextOffset':integer,'hasMore':bool_},['items','nextOffset','hasMore']),None),
 ('/experiments/{eid}','get',obj({'branch':ref('ExperimentBranch'),'counts':ref('ExperimentCounts'),'lineage':arr(ref('ExperimentSummary')),'invalidation':st()},['branch','counts','lineage','invalidation']),None),
 ('/experiments/{eid}/branches','post',ref('ExperimentBranch'),ref('ExperimentFork')),
 ('/experiments/{eid}/events','get',obj({'items':arr(ref('ExperimentEvent')),'total':integer,'nextOffset':integer,'hasMore':bool_,'counts':ref('ExperimentCounts')},['items','total','nextOffset','hasMore','counts']),None),
 ('/experiments/{eid}/events/{eventId}','get',obj({'event':ref('ExperimentEvent'),'originalChunk':st(),'offset':integer,'nextOffset':integer,'totalCharacters':integer,'hasMore':bool_,'note':st()},['event','originalChunk','offset','nextOffset','totalCharacters','hasMore','note']),None),
 ('/experiments/{eid}/diff','get',obj({'before':ref('ExperimentSummary'),'after':ref('ExperimentSummary'),'items':arr(ref('ExperimentDifference')),'total':integer,'nextOffset':integer,'hasMore':bool_},['before','after','items','total','nextOffset','hasMore']),None),
]:
 op=paths[path][method];op['x-administrator-only']=True
 op['parameters']=[v for v in op['parameters'] if v['in']!='query']
 op['summary']='Offline record experiment: '+path.rsplit('/',1)[-1]
 op['description']='K3-A 仅管理员：Kun 固定单轮次已脱敏记录，非执行会话或 checkpoint。只捕获有明确 runId 的宿主记录，最多 2000 条、原始 16 MiB、单条 4 MiB，脱敏包最多 20 MiB。基线及子分支不可变；替代成功/失败工具返回文本会保守地将之后所有记录标为 stale，不自动重算依赖。每次 replace/restore 新建子分支，最多 32 层及 32 个替代值。读取、逐条查看、差异和分叉不启动 worker/模型/工具，不回滚文件或外部系统。源会话删除后记录仍保留，不提供实验删除/导入/执行接口。'
 op['responses']['200']['content']['application/json']['schema']=response
 op['responses']['400']={'description':'未知/重复参数、无效分页、标题、固定上界或缺少幂等 Key。'}
 op['responses']['404']={'description':'分支或固定记录不存在。'}
 op['responses']['409']={'description':'记录上限、后端不支持、不可编辑事件、父指纹/内容校验失败、跨基线比较或分支深度超限。'}
 if body is not None:op['requestBody']={'required':True,'content':{'application/json':{'schema':body}}}
 if method=='get' and path!='/experiments/{eid}':
  op['parameters'] += [{'name':'offset','in':'query','schema':{'type':'integer','minimum':0,'maximum':20971520}},{'name':'limit','in':'query','schema':{'type':'integer','minimum':1,'maximum':16000 if path.endswith('/{eventId}') else 16 if path.endswith('/diff') else 50,'default':4000 if path.endswith('/{eventId}') else 16 if path.endswith('/diff') else 20}}]
 if path=='/experiments' and method=='get':op['parameters'].append({'name':'sessionId','in':'query','schema':st()})
 if path.endswith('/diff'):op['parameters'].append({'name':'against','in':'query','schema':st(),'description':'同一 rootId 的分支；省略对比父分支或自身基线。'})
 # This path lists record metadata, never the session event SSE API.
 if path.endswith('/events'):op['responses']['200']['content'].pop('text/event-stream',None)

# K3-B safe, single-run Hybrid forks. Executable bundles never cross HTTP.
schemas['KunForkSelection']=obj({'sourceRunId':st(),'sequence':{'type':'integer','minimum':1},'through':{'type':'integer','minimum':1},'expectedStateRevision':integer,'workerEpoch':st()},['sourceRunId','sequence','through','expectedStateRevision','workerEpoch'])
schemas['KunForkCursor']=copy.deepcopy(schemas['KunForkSelection'])
schemas['KunForkCursor']['properties']['sequence']={'type':'integer','const':0,'description':'列表游标尚未选择边界，创建预览时使用 items 中的正序号。'}
schemas['KunForkOrigin']=obj({'previewId':st(),'sessionId':st(),'runId':st(),'sequence':integer,'through':integer,'bundleHash':st(),'mode':st(enum=['hybrid'])},['previewId','sessionId','runId','sequence','through','bundleHash','mode'])
schemas['KunForkPreview']=obj({'id':st(),'title':st(),'createdAt':st(),'hash':st(),'origin':ref('KunForkOrigin'),'targetSessionId':st(),'instruction':st(maxLength=16000),'phase':st(),'step':integer,'pending':integer,'budget':ref('KunBudgetUsage'),'limits':ref('KunBudgetLimits'),'maxSteps':integer,'recordCount':integer},['id','title','createdAt','hash','origin','targetSessionId','instruction','phase','step','pending','budget','limits','maxSteps','recordCount'])
schemas['KunForkSummary']=obj({k:v for k,v in schemas['KunForkPreview']['properties'].items() if k!='instruction'},[k for k in schemas['KunForkPreview']['required'] if k!='instruction'])
schemas['KunForkState']=obj({'origin':ref('KunForkOrigin'),'inheritedStep':integer,'inheritedBudget':ref('KunBudgetUsage'),'replayCursor':integer,'replayTotal':integer},['origin','inheritedStep','inheritedBudget','replayCursor','replayTotal'])
schemas['KunState']['properties']['fork']=ref('KunForkState')
schemas['Session']['properties']['kunFork']=ref('KunForkOrigin')
for path,method,response,body in [
 ('/kun-forks/sources/{sid}','get',obj({'selection':ref('KunForkCursor'),'items':arr(obj({'sequence':integer,'phase':st(),'step':integer,'pending':integer},['sequence','phase','step','pending'])),'nextOffset':integer,'hasMore':bool_},['selection','items','nextOffset','hasMore']),None),
 ('/kun-forks','get',obj({'items':arr(ref('KunForkSummary')),'nextOffset':integer,'hasMore':bool_},['items','nextOffset','hasMore']),None),
 ('/kun-forks','post',ref('KunForkPreview'),obj({'sessionId':st(),'selection':ref('KunForkSelection'),'title':st(minLength=1,maxLength=120),'instruction':st(maxLength=16000)},['sessionId','selection','title'])),
 ('/kun-forks/{fid}','get',ref('KunForkPreview'),None),
 ('/kun-forks/{fid}/start','post',ref('Session'),obj({'expectedHash':st(minLength=1)},['expectedHash'])),
]:
 op=paths[path][method];op['x-administrator-only']=True
 op['summary']='Hybrid fork: '+path.rsplit('/',1)[-1]
 op['description']='K3-B 首批，仅管理员。来源为普通 Kun 最近已停止轮次的兼容安全边界；不支持 Codex、诊断或 Hybrid 嵌套分叉。预览校验 epoch/revision/through 与配置指纹，保存固定私有执行包，不调用模型或 MCP。start 显式调用模型；工具仅按名称/Schema/环境/目录/参数/顺序匹配录制，未命中 replay_miss 停止，没有真实工具回退。继承所选时点预算和上下文，不复制/回滚项目文件。每份预览绑定一个独立会话和一次运行；重复启动返回同一会话，删除后拒绝重新创建；普通新轮次与恢复均拒绝。预览可在来源删除和宿主重启后启动，配置漂移拒绝。HTTP 不接收或返回私有可执行 bundle，不导入 K3-A 假设。'
 op['parameters']=[v for v in op['parameters'] if v['in']!='query']
 op['responses']['200']['content']['application/json']['schema']=response
 for code,desc in [('400','未知/重复参数、分页/标题/指令无效、缺少指纹或幂等 Key。'),('404','来源或预览不存在。'),('409','来源未停止、边界/版本/录制不兼容、指纹或配置改变、目标已删除、运行/进程容量限制。')]:op['responses'][code]={'description':desc}
 if body is not None:op['requestBody']={'required':True,'content':{'application/json':{'schema':body}}}
 if method=='get' and path!='/kun-forks/{fid}':
  op['parameters'] += [{'name':'offset','in':'query','schema':{'type':'integer','minimum':0,'maximum':1000000 if 'sources' in path else 20971520}},{'name':'limit','in':'query','schema':{'type':'integer','minimum':1,'maximum':50,'default':50 if 'sources' in path else 20}}]

# Unified read-only debug facade. Existing Kun aliases retain their response shape.
debug_kinds=['overview','run','events','event','context','tools','budget','modules','breakpoints','actions','snapshot','evidence','diff','runs','steps','step','issues','statistics']
schemas['DebugCapability']=obj({'supported':bool_,'available':bool_,'reason':st()},['supported','available'])
schemas['DebugCapabilities']=obj({'sessionId':st(),'backend':st(enum=['codex','kun']),'readOnly':{'const':True},'queries':{'type':'object','additionalProperties':ref('DebugCapability')},'hostEventCursor':st(),'workerSequence':st()},['sessionId','backend','readOnly','queries','hostEventCursor','workerSequence'])
schemas['DebugEventMeta']=obj({'id':integer,'time':st(),'direction':st(),'method':st(),'bytes':integer},['id','time','direction','method','bytes'])
schemas['DebugEventPage']=obj({'events':arr(ref('DebugEventMeta')),'through':integer,'nextCursor':integer,'hasMore':bool_},['events','through','nextCursor','hasMore'])
schemas['DebugEventChunk']=obj({'eventId':integer,'text':st(),'offset':integer,'nextOffset':integer,'totalCharacters':integer,'hasMore':bool_,'encoding':st()},['eventId','text','offset','nextOffset','totalCharacters','hasMore','encoding'])
schemas['DebugRun']=obj({'id':st(),'question':st(),'status':st(),'start':st(),'end':st(),'inputEventId':integer,'association':st(enum=['observed','reconstructed','inferred','missing']),'eventIds':arr(integer)},['id','question','status','start','inputEventId','association','eventIds'])
schemas['DebugStep']=obj({'id':st(),'runId':st(),'type':st(),'title':st(),'status':st(),'start':st(),'end':st(),'durationMs':integer,'reportedDurationMs':integer,'point':bool_,'eventIds':arr(integer),'preview':obj(extra=True),'previewOnly':bool_,'clock':st(),'association':st(enum=['observed','reconstructed','inferred','missing']),'startObserved':bool_,'endObserved':bool_,'missing':arr(st()),'issues':arr(st()),'previewTruncated':bool_,'evidenceTruncated':bool_,'workerSequences':arr(integer)},['id','runId','type','title','status','point','eventIds','preview','previewOnly','clock','association','startObserved','endObserved','missing','issues','previewTruncated','evidenceTruncated'])
schemas['DebugResult']=obj({'sessionId':st(),'backend':st(enum=['codex','kun']),'source':st(enum=['host_session','host_journal','host_projection','kun_worker']),'kind':st(enum=debug_kinds),'through':integer,'runId':st(),'revision':integer,'sequence':integer,'data':{}},['sessionId','backend','source','kind','data'])
for suffix,response in [('capabilities','DebugCapabilities'),('query','DebugResult')]:
 op=paths['/sessions/{sid}/debug/'+suffix]['get']
 op['summary']='Unified read-only debug '+suffix
 op['description']='只读；应用凭据需 read 与会话归属，成员需项目授权。Codex 支持 overview/run（宿主概况，无 revision/sequence）、events/event（宿主保留事件）及 runs/steps/step/issues/statistics（生命周期记录重建）；内部快照/上下文等仍返回 409 debug_unsupported。Kun 内部查询需在线 worker；不启动/恢复进程，不调用模型或工具，不写入对话。source 区分 host_session、host_journal、host_projection、kun_worker。重建步骤不等于完整模型内部状态。先查询 capabilities；supported 与 available 区分支持情况与当前可用性。响应最大 4 MiB，超限返回 413。'
 op['x-administrator-only']=False;op['x-application-scopes']=['read']
 op['responses']['200']['content']['application/json']['schema']=ref(response)
 op['responses']['400']={'description':'无效、重复、未知、不适用参数或越界游标'}
 op['responses']['404']={'description':'会话或此会话的宿主事件不存在'}
 op['responses']['409']={'description':'不支持此能力，Kun worker 离线或快照查询被拒绝'}
 op['responses']['413']={'description':'响应超过 4 MiB 或单个宿主事件解析前超过 8 MiB；重建超过 20000 生命周期事件或 64 MiB'}
 if suffix=='capabilities':continue
 op['parameters'].append({'name':'kind','in':'query','required':True,'schema':st(enum=debug_kinds)})
 selectors=[('sequence',0,None,'Kun worker 序号。run/context/tools/budget/modules/breakpoints/actions 省略或 0 表示当前；snapshot/evidence/diff 要求正数。不是宿主事件 ID。'),('fromSequence',1,None,'仅 diff 使用且必填；与 sequence 属于同一会话。'),('after',0,None,'仅 events；宿主事件 ID 下界，不包含；默认 0。'),('through',0,None,'用于 events/event 与五种重建查询；省略时固定当前宿主事件上界（event 默认不限上界）。后续分页必须原样保留响应 through，包括 0。不接受大于当前已保留最大 ID 的值。'),('eventId',1,None,'仅 event，必填，严格属于此会话的宿主事件 ID。'),('offset',0,2147483647,'event 为脱敏 JSON 的 Unicode 字符偏移；runs/steps/issues 为记录偏移。默认 0。'),('limit',1,16000,'events 默认 50、最大 200；event 默认 4000、最大 16000 字符；runs/steps/issues 默认 50、最大 50。其他 kind 不接受。')]
 for name,minimum,maximum,description in selectors:
  schema={'type':'integer','minimum':minimum}
  if maximum is not None:schema['maximum']=maximum
  op['parameters'].append({'name':name,'in':'query','required':False,'schema':schema,'description':description})
 for name,maximum,description in [('runId',256,'steps/step/issues/statistics 的所属轮次，存在性与 step 归属均核对。'),('type',256,'steps/issues/statistics 的准确类型匹配。'),('status',256,'steps/issues/statistics 的准确状态匹配。'),('query',1000,'steps/issues/statistics 的脱敏预览字面子串搜索，不是完整输出搜索。'),('stepId',256,'step 必填，由 steps 返回的 step-宿主事件ID。')]:
  op['parameters'].append({'name':name,'in':'query','required':False,'schema':st(maxLength=maximum),'description':description})
 op['description']+=' runs/steps/step/issues/statistics 的 through 是宿主固定范围，source=host_projection，最多 20000 生命周期记录/64 MiB、单记录 8 MiB、查询索引期限 8 秒；超限 413，不返回不完整统计。 step.data 为 DebugStep；列表按首次来源事件顺序，issues 为确定性信号而非根因诊断。'
 op['description']+=' events.data 为 DebugEventPage，仅元数据，不加载正文；event.data 为 DebugEventChunk，单事件超过 8 MiB 返回 413。历史查询从 through/sequence 固定，但不同请求不承诺组成跨数据源原子快照。未知和不适用字段即使为 0 也拒绝。'

for path,ops in list(paths.items()):
 if path.split('/')[1] not in ['sessions','workspaces','requests']:continue
 for method,op in ops.items():
  if method not in ['get','post'] or not op.get('x-application-scopes'):continue
  projected=copy.deepcopy(op);projected['operationId']='member_'+op['operationId']
  projected.pop('x-application-scopes',None);projected['x-personal-user-only']=True
  projected['parameters'].insert(0,{'name':'grantId','in':'path','required':True,'schema':st()})
  projected['description']='个人访问码或成员 Cookie。grantId 必须属于当前用户；仅允许授权应用项目。viewer 仅 GET；runner 可提交、上传、审批。任务和文件在同项目共享。变更为本机执行后禁止成员写入。'
  paths.setdefault('/member/{grantId}'+path,{})[method]=projected


schemas['ImageBuild']=obj({k:st() for k in ['id','instanceId','name','dockerfile','reference','created','updated','started','finished','contextSha256','imageId','imageVersionId','error']}|{'status':st(enum=['draft','queued','running','canceling','canceled','interrupted','failed','succeeded']),'files':integer,'contextBytes':integer,'timeoutMinutes':integer,'noCache':bool_,'logTruncated':bool_},['id','instanceId','name','status','dockerfile','reference','contextSha256','files','contextBytes','timeoutMinutes','noCache','logTruncated'])
for path,ops in paths.items():
 if '/builds' not in path:continue
 for method,op in ops.items():
  op['x-administrator-only']=True
  op['description']='仅管理员。上传只创建 draft；显式 start 入队，全局串行。重复 start 返回同一任务，不重复执行。重启后未完成任务标为 interrupted，不自动重跑。完成后清理上下文，保留元数据与最多 4 MiB 日志。构建结果不自动选择或部署。'
  schema=arr(ref('ImageBuild')) if method=='get' and path.endswith('/builds') else ref('ImageBuild')
  if path.endswith('/log'):
   schema=obj({'text':st(),'next':integer,'status':st(),'truncated':bool_},['text','next','status','truncated'])
   op['parameters'].append({'name':'after','in':'query','schema':{'type':'integer','minimum':0,'maximum':4194304},'description':'字节偏移；每次最多返回 64 KiB，next 为后续偏移。'})
  if method=='delete':schema=obj({'ok':bool_},['ok'])
  op['responses']['200']['content']['application/json']['schema']=schema
  if method=='post' and path.endswith('/builds'):
   op['requestBody']={'required':True,'content':{'multipart/form-data':{'schema':obj({'file':st(format='binary',description='ZIP 最大 32 MiB；解压 128 MiB，4000 条目。禁止符号链接及路径穿越。'),'name':st(maxLength=120),'dockerfile':st(default='Dockerfile'),'timeoutMinutes':{'type':'integer','minimum':1,'maximum':120,'default':30},'noCache':bool_},['file'])}}}


schemas['PersonalFile']=obj({'id':st(),'owner':st(),'name':st(),'size':integer,'sha256':st(),'created':st(),'kind':st(enum=['upload','generated']),'sessionId':st(),'deleted':bool_},['id','owner','name','size','sha256','created','kind','deleted'])
for path,ops in paths.items():
 if not path.startswith('/library'):continue
 for method,op in ops.items():
  op['x-administrator-only']=False;op['x-personal-user-only']=True
  op['description']='管理员或个人用户可访问自己的文件库；应用凭据不可用。不允许通过参数指定他人的 owner。删除保留历史引用墓碑，内容读取与再次加入返回 410。'
  schema=arr(ref('PersonalFile')) if method=='get' else ref('PersonalFile')
  if method=='delete':schema=obj({'ok':bool_},['ok'])
  if path.endswith('/deleted-references'):schema=arr(obj({'fileId':st(),'workspaceId':st(),'path':st()},['fileId','workspaceId','path']))
  if path.endswith('/content'):
   op['responses']['200']['content']={'application/octet-stream':{'schema':st(format='binary')}}
   op['parameters'].append({'name':'preview','in':'query','schema':st(enum=['0','1'])});continue
  op['responses']['200']['content']['application/json']['schema']=schema
  if method=='get' and path=='/library':op['parameters'].append({'name':'q','in':'query','schema':st(),'description':'按文件名搜索'})
  if method=='post':op['requestBody']={'required':True,'content':{'multipart/form-data':{'schema':obj({'file':st(format='binary',description='上传最大 32 MiB')},['file'])}}}
for path,ops in paths.items():
 if not path.endswith('/uploads'):continue
 op=ops.get('post')
 if op:op['requestBody']['content']['multipart/form-data']['schema']={'oneOf':[obj({'file':st(format='binary')},['file']),obj({'libraryFileId':st(description='本人的个人库文件；服务端复制到当前获授权工作区，不新增库条目。')},['libraryFileId'])]}

# Collaboration management is administrator-only; A2A also accepts scoped application keys.
for path, methods in {
 '/collaboration/config':['get','put'], '/collaboration/discover':['post'],
 '/collaborations':['get','post'], '/collaborations/{cid}':['get'],
 '/collaborations/{cid}/actions':['post'], '/collaborations/{cid}/board':['post'],
 '/collaborations/{cid}/work/{work}/reconcile':['post'],
 '/a2a/{agent}/agent-card.json':['get'], '/a2a/{agent}':['post'],
}.items():
 paths[path]={}
 for method in methods:
  params=[{'name':name,'in':'path','required':True,'schema':st()} for name in __import__('re').findall(r'\{(\w+)\}',path)]
  op={'summary':'A2A 0.3 JSON-RPC' if '/a2a/' in path else '协作任务与黑板', 'description':'具体契约见 docs/COLLABORATION.md。协作管理仅限管理员；A2A 仅允许绑定相同应用配置和项目的应用凭据，任务按凭据隔离。', 'parameters':params,'responses':{'200':{'description':'成功；A2A 协议错误在 JSON-RPC error 中返回','content':{'application/json':{'schema':obj(extra=True)}}},'403':{'description':'禁止访问'}}}
  if method in ['post','put']:op['requestBody']={'required':True,'content':{'application/json':{'schema':obj(extra=True)}}}
  if path=='/collaborations' and method=='get':op['responses']['200']['content']['application/json']['schema']=arr(obj(extra=True))
  paths[path][method]=op

if '/processes' in paths:
 paths['/processes']['get']['summary']='Administrator-only live App Server connection processes; not an OS process tree'
 paths['/processes']['get']['responses']['200']['content']['application/json']['schema']=obj({'serverPID':integer,'platform':st(),'cleanupMode':st(enum=['process-group','direct-process']),'scope':st(),'observedAt':st(),'items':arr(obj({'pid':integer,'startedAt':st(),'cleanupMode':st(),'closing':bool_,'exited':bool_,'cleanupError':st(),'connectionId':st(),'instanceId':st(),'workspaceId':st(),'sessionId':st(),'kind':st(),'executionMode':st()},extra=False))},extra=False)
if '/usage' in paths:
 paths['/usage']['get']['summary']='Administrator-only observed usage by application/project; not billing'
 paths['/usage']['get']['parameters']=[{'name':n,'in':'query','required':False,'schema':st(),'description':desc} for n,desc in [('from','Inclusive RFC3339 timestamp; defaults to 30 days before to'),('to','Exclusive RFC3339 timestamp; defaults to now, maximum range 366 days'),('appId','Filter by source application ID'),('workspaceId','Filter by workspace ID')]]
 paths['/usage']['get']['responses']['200']['content']['application/json']['schema']=obj({'rows':arr(obj({'appId':st(),'instanceId':st(),'workspaceId':st(),'tokens':{'type':'object','additionalProperties':{'type':['integer','null']}},'runs':integer,'reportedRuns':integer,'unknownRuns':integer,'statuses':{'type':'object','additionalProperties':integer},'issues':{'type':'object','additionalProperties':integer},'tokenEvents':integer},extra=False)),'from':st(),'to':st(),'generatedAt':st(),'throughEventId':integer,'basis':st(),'demo':bool_},extra=False)
if '/setup/docker-template' in paths:
 paths['/setup/docker-template']['get']['parameters']=[{'name':'version','in':'query','required':True,'schema':st(),'description':'Exact Codex release'}]
 paths['/setup/docker-template']['get']['responses']['200']={'description':'Pinned base image build context','content':{'application/zip':{'schema':st(format='binary')}}}
spec={'openapi':'3.1.0','info':{'title':'RunDesk Application API','version':re.search(r'const Version = "([^"]+)"',(root/'internal/app/api_v1.go').read_text()).group(1),'description':'RunDesk 0.33.0：Kun 0.9 / 协议 v9；Plan-Act 显式规划、两套内置模块组合与 Harness 固定快照对照； 安全检查点独立 Hybrid 分支，模型重算、工具严格录制回放、未命中停止；K3-A 离线记录基线、不可变实验分支、保守失效与谱系差异；建议审核、当前状态控制预览与可追溯发送；Kun 0.7 / 协议 v7 独立诊断会话；固定来源只读工具与有证据的建议（不自动执行）；Codex/Kun 宿主轨迹重建与统一只读检查界面，轮次/步骤/异常/统计查询；统一只读 DebugService 与固定会话 stdio MCP，Codex 提供宿主概况/保留事件，Kun 提供内部检查；结构化控制、用量、MCP 与模块视图；调用证据、有序请求上下文与快照差异；Kun 条件断点与结构化 Console；Kun 显式安全检查点恢复；Kun 四模块、执行预算、MCP 参数校验；Kun 独立进程、文本模型/文件工具循环、MCP 连接与逐工具审批、上下文快照和调试控制；管理员运行进程观测、内核目录锁与原生进程组清理；管理员按应用项目查询保留事件中的用量；管理员 MCP 独立测试、持久化结果与幂等回执；支持的协议及边界见 README。应用主动注册、初始配置仅安装一次、管理员选用已有能力；通用助手默认协调。新增管理员协作工作台、A2A 0.3 JSON-RPC 和 Gitea Issue 黑板。新增个人文件库，上传和会话产物自动保存，跨会话引用及删除。管理员可上传 ZIP 并显式执行持久化镜像构建，支持日志、取消和结果登记。新增个人访问码、应用项目授权和成员入口。新增镜像版本目录和固定目标，镜像管理仅限管理员。Docker 环境按应用与项目隔离，管理接口仅限管理员。应用与专用 instance 一对一绑定，default 保留给通用助手。支持完整技能目录和关联的轨迹分析会话。/api/v1 是稳定的应用入口，旧 /api 保留。NativeObject 透传原生 Codex 结果，其内部字段受原生版本影响。管理员 Token/Cookie 保留；应用使用独立 Bearer 凭据、允许项目和操作 scopes。应用凭据由服务端绑定 Source。API 权限不是操作系统沙箱或完整多用户隔离。'},'servers':[{'url':'/api/v1'}],'security':[{'BearerAuth':[]},{'BrowserCookie':[]}],'paths':paths,'components':{'securitySchemes':{'BearerAuth':{'type':'http','scheme':'bearer'},'BrowserCookie':{'type':'apiKey','in':'cookie','name':'rundesk'}},'schemas':schemas}}
(root/'internal/app/openapi.json').write_text(json.dumps(spec,ensure_ascii=False,indent=2)+'\n')
print(f'{len(paths)} paths, {sum(len(v) for v in paths.values())} operations')
