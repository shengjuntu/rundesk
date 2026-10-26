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
schemas['ObservationStatus']=obj({'state':st(enum=['pending','configured','observed','degraded','disabled','unavailable','simulated']),'transport':st(enum=['http-pass-through']),'provider':st(),'upstream':st(),'message':st(),'maxBodyBytes':integer,'captureId':st(description='当前观测连接代次标识；用于防止旧队列记录更新新连接状态，不是模型请求 ID。'),'pending':{'type':'boolean','description':'保存的 observeHTTP 偏好尚待下一轮提交应用；不打断当前请求。'},'appliedEnabled':{'type':'boolean','description':'原生线程已确认配置观测代理；开启偏好但不支持采集时仍为 false。不证明请求已完整入库，仍需检查 state 和逐条完整性。'}},['state','transport','message','pending','appliedEnabled'])
schemas['Session']['properties']['observation']=ref('ObservationStatus')
schemas['Session']['properties']['observeHTTP']={'type':'boolean','default':False,'description':'持久化的会话级 Codex HTTP 采集偏好；默认关闭。CLI --observe-http 只决定新建 Codex 会话默认值，不覆盖已有会话；修改在下一轮提交应用。Kun 不允许设为 true。'}
schemas['Session']['required'].append('observeHTTP')
paths={}
requests={
 ('POST','/workspaces/{wid}/mcp-tests'):obj({'server':st(),'tool':st(),'arguments':obj({},extra=True),'cursor':st(),'confirm':bool_},['server','confirm']),
 ('PUT','/setup'):obj({'applicationsOnly':bool_,'codex':st(),'workspaceId':st(),'model':st(),'completed':bool_,'revision':integer,'instanceRevision':integer,'agentRuntime':ref('KunConfig')},['workspaceId','revision']),
 ('POST','/setup/runtime-images'):obj({'profileId':st(enum=['news2douyin','people-search','video-build','hyperframes','video-use','openmontage']),'codexVersion':st(),'source':st(enum=['build','pull','local']),'reference':st()},['profileId','codexVersion','reference']),
 ('POST','/setup/codex-install'):obj({'version':st(pattern=r'^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$',maxLength=64),'confirm':{'const':True}},['version','confirm']),
 ('POST','/setup/check'):obj({'kind':st(enum=['kun','bash','codex','workspace','models','account','mcp','docker']),'workspaceId':st(),'instanceId':st()},['kind']),
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
 ('PATCH','/sessions/{sid}'):obj({'title':st(),'pinned':bool_,'archived':bool_,'observeHTTP':{'type':'boolean','description':'保存此 Codex 会话的 HTTP 采集开关；当前执行继续，下一轮提交自动重连并 resume 原线程应用。Kun 拒绝 true；不删除历史记录。'}}),
 ('POST','/sessions/{sid}/approvals/{aid}'):obj({'decision':{},'scope':st(enum=['turn','session']),'answers':obj(extra=True),'content':{}}),
 ('PUT','/workspaces/{wid}/skills/{name}'):obj({'content':st()},['content']),
 ('POST','/workspaces/{wid}/skills/toggle'):obj({'path':st(),'enabled':bool_},['path','enabled']),
 ('PUT','/workspaces/{wid}/mcp/{name}'):obj({'version':st(),'config':obj(extra=True),'remove':bool_},['version']),
 ('POST','/workspaces/{wid}/mcp/import'):obj({'version':st(),'bundle':obj(extra=True),'overwrite':bool_},['version','bundle']),
}
idempotent={('POST',p) for p in ['/setup/codex-install','/setup/runtime-images','/kun-task-sets','/kun-rubrics','/kun-evaluations','/kun-forks','/kun-forks/{fid}/start','/kun-forks/{fid}/hypotheses','/experiments','/experiments/{eid}/branches','/instances','/workspaces','/sessions','/sessions/{sid}/turns','/sessions/{sid}/recover','/sessions/{sid}/kun/resume','/tasks','/schedules','/workspaces/{wid}/mcp-tests']}
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

session_patch=paths['/sessions/{sid}']['patch']
session_patch['description']+=' observeHTTP 保存会话级偏好，不立即重配当前线程；活动请求继续。下一轮提交时自动重连并恢复同一 Codex thread，应用新值；空闲会话同样下一轮生效。Session.observeHTTP 是保存值，observation.pending/appliedEnabled 区分待应用与已应用状态。关闭时不执行采集配置发现或走观测路由；代理在首个确实启用的线程按需启动。Kun 不允许启用此 Codex 开关；历史证据保持可读。'
session_patch['responses']['400']={'description':'请求字段无效，或尝试为 Kun 开启 Codex HTTP 采集。'}

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
schemas['KunHarnessConfig']['description']='Two installed compositions only: tool-loop-v1 pairs with no-explicit-plan-v1; plan-act-v1 pairs with explicit-plan-v1. Missing fields use the selected preset defaults. Unknown IDs and incompatible pairs are rejected. The admission config remains fixed. Administrators may explicitly switch an ordinary paused run at before_model via set_harness; effective runtimeHarness and versioned module state survive checkpoint recovery and forks. New ordinary runs reset to the configured default.'
schemas['KunConfig']=obj({'harness':ref('KunHarnessConfig'),'kind':st(enum=['codex','kun']),'endpoint':st(),'model':st(),'apiKeyEnv':st(),'systemPrompt':st(maxLength=65536),'maxSteps':{'type':'integer','minimum':1,'maximum':100},'timeoutSeconds':{'type':'integer','minimum':1,'maximum':600},'allowWrite':bool_,'allowBash':bool_,'bashPath':st(maxLength=1024),'bashApproval':st(enum=['prompt','approve']),'bashTimeoutSeconds':{'type':'integer','minimum':0,'maximum':600},'pauseBeforeModel':bool_,'budget':ref('KunBudgetLimits'),'debug':ref('KunDebugPolicy')},['kind'])
schemas['KunControl']=obj({'requestId':st(minLength=8,maxLength=128),'runId':st(),'expectedStateRevision':{'type':'integer','minimum':0},'operation':st(enum=['pause','resume','step','cancel','steer','approve','reject','set_breakpoints','set_harness']),'harness':ref('KunHarnessConfig'),'reason':st(minLength=1,maxLength=2048),'debug':ref('KunDebugPolicy'),'text':st(maxLength=262144),'callId':st(description='Required for approve/reject; must match the pending call.')},['requestId','runId','expectedStateRevision','operation'])
schemas['KunReceipt']=obj({'requestId':st(),'status':st(enum=['queued','applied','rejected']),'revision':integer},['requestId','status','revision'])
schemas['KunToolCall']=obj({'id':st(),'type':st(),'function':obj({'name':st(),'arguments':st()},['name','arguments'])},['id','type','function'])
schemas['KunMessage']=obj({'role':st(),'content':st(),'tool_calls':arr(ref('KunToolCall')),'tool_call_id':st()},['role','content'])
schemas['KunSkill']=obj({'name':st(),'path':st(),'content':st(),'hash':st()},['name','path','content','hash'])
schemas['KunState']=obj({'schemaVersion':integer,'sessionId':st(),'runId':st(),'revision':integer,'status':st(),'phase':st(),'step':integer,'messages':arr(ref('KunMessage')),'pending':arr(ref('KunToolCall')),'actions':{'type':'object','additionalProperties':st()},'config':ref('KunConfig'),'skills':arr(ref('KunSkill')),'queuedControls':arr(ref('KunControl')),'error':st()},['schemaVersion','sessionId','runId','revision','status','phase','step','config','messages'])
schemas['KunMCPStatus']=obj({'name':st(),'configRevision':st(),'transport':st(),'status':st(),'protocolVersion':st(),'toolCount':integer,'error':st()},['name','configRevision','transport','status'])
schemas['KunMCPTool']=obj({'alias':st(),'server':st(),'name':st(),'description':st(),'inputSchema':obj(extra=True),'annotations':obj(extra=True),'approvalMode':st(enum=['approve','prompt'])},['alias','server','name','inputSchema','approvalMode'])
schemas['KunToolApproval']=obj({'kind':st(enum=['builtin']),'callId':st(),'server':st(),'tool':st(),'arguments':st(),'decision':st(enum=['approve','reject'])},['callId','server','tool','arguments'])
schemas['KunState']['properties'].update({'runtimeHarness':ref('KunHarnessConfig'),'debug':ref('KunDebugState'),'harness':ref('KunHarness'),'modules':{'type':'object','additionalProperties':ref('KunModuleState')},'budget':ref('KunBudgetUsage'),'toolDefinitions':arr(obj(extra=True)),'mcp':arr(ref('KunMCPStatus')),'mcpTools':arr(ref('KunMCPTool')),'approval':ref('KunToolApproval'),'approvalPolicy':st(enum=['on-request','never'])})
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
 op['description']='Kun 0.11：本机独立 worker；条件断点、暂停期限、只读结构化查询。checkpoint 可重开离线 worker，仅核对最近安全检查点，不执行模型/MCP；resume 需 selection 与 Idempotency-Key，新 run 沿用上下文和预算、MCP 重连后核对工具并重新审批。旧版本、未知结果、配置变化、过期 selection 不可恢复。无历史回滚；Hybrid/Live 使用独立 fork API。Plan-Act 规划与执行请求共用模型预算和四边界；purpose=plan 的结果不是最终回复。control 需当前 runId/revision；approve/reject 另需 approvals scope。'
 op['responses']['200']['content']['application/json']['schema']=response
 op['responses']['409']={'description':'Worker 离线、状态版本变化、ID 冲突或控制操作不适用；刷新状态后处理。'}
 if path.endswith('/kun/query'):
  op['parameters'] += [{'name':'kind','in':'query','required':True,'schema':st(enum=['run','context','tools','budget','modules','breakpoints','actions','evidence','diff'])},{'name':'sequence','in':'query','required':False,'schema':{'type':'integer','minimum':0},'description':'0 或省略：当前状态；正数：同一会话事件快照。查询不更改状态、不调用模型或工具。'}]
  op['parameters'] += [{'name':'fromSequence','in':'query','required':False,'schema':{'type':'integer','minimum':1},'description':'仅 diff 使用且必填；sequence 也必须大于 0。两份快照属于此会话，可跨运行。evidence 要求正数 sequence。'}]
  op['description'] += ' context 在 model.started 快照返回实际 model_request，其余为 state_context；evidence 返回精确事件；diff 返回 KunSnapshotDiff，先脱敏再比较，有界预览，不执行回滚。需要在线 worker。'
 if path.endswith('/kun/control'):
  op['description'] += ' set_breakpoints 必须带 debug，替换本 run 的规则并重置命中计数；不解除已有暂停，默认配置不变。未知条件字段被拒绝。 set_harness 仅管理员；需要 harness、reason 和精确 runId/revision。仅普通运行在 before_model 已暂停、无待处理工具/审批/控制及未知动作时允许；切换两套内置组合并重置 memory/planning，保留预算、权限、上下文和默认配置，提交后仍暂停。状态 runtimeHarness 表示本 run 的有效组合覆盖，旧快照不变，恢复和分叉保留；普通新轮次恢复默认配置。'
 if body:op['requestBody']={'required':True,'content':{'application/json':{'schema':body}}}
 if path.startswith('/sessions/'):
  op['x-administrator-only']=False
  op['x-application-scopes']=['run' if method=='post' else 'read']
  if path.endswith('/kun/control'):
   op['x-conditional-scopes']={'approve':['approvals'],'reject':['approvals']}
   op['x-administrator-only-operations']=['set_harness']
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

# K3-B/C safe, single-run Hybrid and Live forks. Executable bundles never cross HTTP.
schemas['KunForkSelection']=obj({'sourceRunId':st(),'sequence':{'type':'integer','minimum':1},'through':{'type':'integer','minimum':1},'expectedStateRevision':integer,'workerEpoch':st()},['sourceRunId','sequence','through','expectedStateRevision','workerEpoch'])
schemas['KunForkCursor']=copy.deepcopy(schemas['KunForkSelection'])
schemas['KunForkCursor']['properties']['sequence']={'type':'integer','const':0,'description':'列表游标尚未选择边界，创建预览时使用 items 中的正序号。'}
schemas['KunForkOrigin']=obj({'previewId':st(),'sessionId':st(),'runId':st(),'sequence':integer,'through':integer,'bundleHash':st(),'mode':st(enum=['hybrid','live'])},['previewId','sessionId','runId','sequence','through','bundleHash','mode'])
schemas['KunLiveToolPreview']=obj({'callId':st(),'name':st(),'argumentsPreview':st(maxLength=2048),'truncated':bool_},['callId','name','argumentsPreview','truncated'])
schemas['KunLivePreview']=obj({'workspace':st(),'allowBash':bool_,'allowWrite':bool_,'approvalPolicy':st(enum=['on-request','never']),'mcpTools':integer,'pendingTools':arr(ref('KunLiveToolPreview'))},['workspace','allowWrite','approvalPolicy','mcpTools','pendingTools'])
hypothesis_props={**{k:st() for k in ['parentPreviewId','parentHash','parentBundleHash','recordHash','originalOutputHash','outputHash']},'position':{'type':'integer','minimum':0,'maximum':127},'sourceSequence':integer,'output':st(description='完整替换文本，可为空；UTF-8 字节数最多 65536。'),'reason':st(minLength=1,maxLength=1000)}
schemas['KunReplayHypothesis']=obj(hypothesis_props,list(hypothesis_props))
schemas['KunHypothesisSummary']=obj({k:v for k,v in hypothesis_props.items() if k not in ['output','reason']},[k for k in hypothesis_props if k not in ['output','reason']])
schemas['KunForkPreview']=obj({'hypothesis':ref('KunReplayHypothesis'),'live':ref('KunLivePreview'),'id':st(),'title':st(),'createdAt':st(),'hash':st(),'origin':ref('KunForkOrigin'),'targetSessionId':st(),'instruction':st(maxLength=16000),'phase':st(),'step':integer,'pending':integer,'budget':ref('KunBudgetUsage'),'limits':ref('KunBudgetLimits'),'maxSteps':integer,'recordCount':integer},['id','title','createdAt','hash','origin','targetSessionId','instruction','phase','step','pending','budget','limits','maxSteps','recordCount'])
schemas['KunForkSummary']=obj({k:v for k,v in schemas['KunForkPreview']['properties'].items() if k not in ['instruction','live']},[k for k in schemas['KunForkPreview']['required'] if k not in ['instruction','live']])
schemas['KunForkSummary']['properties']['hypothesis']=ref('KunHypothesisSummary')
schemas['KunForkState']=obj({'hypothesisHash':st(),'origin':ref('KunForkOrigin'),'inheritedStep':integer,'inheritedBudget':ref('KunBudgetUsage'),'replayCursor':integer,'replayTotal':integer},['origin','inheritedStep','inheritedBudget','replayCursor','replayTotal'])
schemas['KunState']['properties']['fork']=ref('KunForkState')
schemas['Session']['properties']['kunFork']=ref('KunForkOrigin')
for path,method,response,body in [
 ('/kun-forks/sources/{sid}','get',obj({'selection':ref('KunForkCursor'),'items':arr(obj({'sequence':integer,'phase':st(),'step':integer,'pending':integer},['sequence','phase','step','pending'])),'nextOffset':integer,'hasMore':bool_},['selection','items','nextOffset','hasMore']),None),
 ('/kun-forks','get',obj({'items':arr(ref('KunForkSummary')),'nextOffset':integer,'hasMore':bool_},['items','nextOffset','hasMore']),None),
 ('/kun-forks','post',ref('KunForkPreview'),obj({'mode':st(enum=['hybrid','live']),'sessionId':st(),'selection':ref('KunForkSelection'),'title':st(minLength=1,maxLength=120),'instruction':st(maxLength=16000)},['sessionId','selection','title'])),
 ('/kun-forks/{fid}','get',ref('KunForkPreview'),None),
 ('/kun-forks/{fid}/start','post',ref('Session'),obj({'expectedHash':st(minLength=1),'confirmLive':bool_},['expectedHash'])),
]:
 op=paths[path][method];op['x-administrator-only']=True
 op['summary']='Kun runtime fork: '+path.rsplit('/',1)[-1]
 op['description']='K3-B/C，仅管理员。来源为普通 Kun 最近已停止轮次的兼容安全边界；不支持 Codex、诊断或嵌套分叉。mode 省略为 hybrid，支持 live；固定预览绑定模式与哈希，不调用模型/MCP。Live start 必须 confirmLive=true，使用当前项目文件、重新解析技能/MCP 凭据并校验配置，重连并核对工具目录；旧的一次性审批不复用，允许重复真实副作用，不回滚文件或外部服务。已有待处理工具沿用原参数，补充指令在下一次模型请求前应用。Hybrid 工具按严格录制匹配，未命中停止，不回退 Live。继承预算和上下文；每份预览只创建一个独立单次运行，重复返回同一会话，删除后不重建。普通新轮次、检查点续跑及嵌套分叉拒绝。HTTP 不收发私有执行包；列表省略指令、Live 动作详情及假设 output/reason。假设详情包含用户填写的替换全文，录制摘要须单独读取有界节选。'
 op['parameters']=[v for v in op['parameters'] if v['in']!='query']
 op['responses']['200']['content']['application/json']['schema']=response
 for code,desc in [('400','未知/重复参数、分页/标题/指令无效、缺少指纹、Live 明确确认或幂等 Key。'),('404','来源或预览不存在。'),('409','来源未停止、边界/版本/录制不兼容、指纹或配置改变、目标已删除、运行/进程容量限制。')]:op['responses'][code]={'description':desc}
 if body is not None:op['requestBody']={'required':True,'content':{'application/json':{'schema':body}}}
 if method=='get' and path!='/kun-forks/{fid}':
  op['parameters'] += [{'name':'offset','in':'query','schema':{'type':'integer','minimum':0,'maximum':1000000 if 'sources' in path else 20971520}},{'name':'limit','in':'query','schema':{'type':'integer','minimum':1,'maximum':50,'default':50 if 'sources' in path else 20}}]

# One immutable counterfactual output, derived exclusively from a private tape.
review_props={'position':integer,'sequence':integer,'tool':st(),'status':st(enum=['succeeded','failed']),'recordHash':st(),'outputHash':st(),'outputBytes':integer,'preview':st(maxLength=2048),'redacted':bool_,'truncated':bool_}
schemas['KunReplayReview']=obj(review_props,list(review_props))
for path,method,response,body in [
 ('/kun-forks/recordings/{fid}','get',obj({'previewId':st(),'previewHash':st(),'items':arr(ref('KunReplayReview'))},['previewId','previewHash','items']),None),
 ('/kun-forks/{fid}/hypotheses','post',ref('KunForkPreview'),obj({'expectedHash':st(minLength=1),'position':{'type':'integer','minimum':0,'maximum':127},'recordHash':st(minLength=1),'output':hypothesis_props['output'],'reason':hypothesis_props['reason'],'title':st(minLength=1,maxLength=120)},['expectedHash','position','recordHash','output','reason','title']))
]:
 op=paths[path][method];op['x-administrator-only']=True
 op['summary']='Kun 固定录制的单条假设替换'
 op['description']='仅当前版本、未含假设的 Hybrid 预览。GET 返回最多 128 条有界摘要，每条最多 2048 Unicode 字符，JSON 常见敏感字段脱敏；自由文本不保证脱敏。recordHash 绑定完整原录制，节选不是可执行原文。POST 必须 expectedHash/position/recordHash/title/reason/output（空字符串有效，缺省或 null 无效）；UTF-8 输出最多 64 KiB；拒绝与原文相同的替换。保存独立不可变子预览，原工具顺序、身份、参数、成功/失败状态、继承预算和指令不变；不启动会话、worker、模型或工具。随后通过原 start 接口核对固定 child hash 启动。保留原始 tape，并将替换标为 hypothetical/ executed=false。未命中停止；禁止 Live、叠加替换、导入 K3-A 脱敏实验。'
 op['parameters']=[v for v in op['parameters'] if v['in']!='query']
 op['responses']['200']['content']['application/json']['schema']=response
 for code,desc in [('400','缺少字段、无效文本/标题、同值替换、未知字段/参数或幂等 Key。'),('403','仅管理员可读写假设预览。'),('404','父预览不存在。'),('409','父指纹或录制指纹不符、版本不兼容、Live 或已有假设。'),('413','新分叉包超过 4 MiB，或响应超限。')]:op['responses'][code]={'description':desc}
 if body is not None:op['requestBody']={'required':True,'content':{'application/json':{'schema':body}}}

# K3 descriptive comparisons use immutable previews and retained host events only.
nullable_int={'type':['integer','null']}
schemas['KunCompareReply']=obj({'text':st(maxLength=2048),'characters':integer,'truncated':bool_,'hash':st(),'eventId':integer,'sequence':integer},['text','characters','truncated','hash','eventId','sequence'])
schemas['KunCompareTool']=obj({'name':st(),**{k:integer for k in ['dispatched','replayed','hypothetical','failed','declined','unsettled']}},['name','dispatched','replayed','hypothetical','failed','declined','unsettled'])
compare_hypothesis={**{k:st() for k in ['parentPreviewId','hash','originalOutputHash','outputHash']},'position':integer,'sourceSequence':integer,'status':st(enum=['applied','not_reached','unknown']),'eventId':integer}
schemas['KunCompareHypothesis']=obj(compare_hypothesis,[k for k in compare_hypothesis if k!='eventId'])
compare_side={'hypothesis':ref('KunCompareHypothesis'),'previewId':st(),**{k:st() for k in ['sessionId','runId','mode','status']},**{k:integer for k in ['through','afterSequence','lastSequence','eventCount','firstEventId','lastEventId','modelCalls','planningCalls','modelCompletions','reportedTokens','usageMissing']},'complete':bool_,'harness':ref('KunHarness'),'tokenUsageComplete':bool_,'activeMillis':nullable_int,'waitMillis':nullable_int,'tools':arr(ref('KunCompareTool')),'lastReply':{'anyOf':[ref('KunCompareReply'),{'type':'null'}]},'warnings':arr(st())}
schemas['KunCompareSide']=obj(compare_side,[k for k in compare_side if k not in ['previewId','hypothesis']])
schemas['KunForkComparison']=obj({'hash':st(),'schema':{'type':'integer','const':1},'previewId':st(),'against':st(),'readOnly':{'const':True},'benchmark':{'const':False},'baseline':obj({'hash':st(),'sessionId':st(),'runId':st(),'sequence':integer,'through':integer,'model':st(),'harness':ref('KunHarness'),'step':integer,'budget':ref('KunBudgetUsage')},['hash','sessionId','runId','sequence','through','model','harness','step','budget']),'left':ref('KunCompareSide'),'right':ref('KunCompareSide'),'delta':obj({k:nullable_int for k in ['modelCalls','reportedTokens','activeMillis','waitMillis']},['modelCalls','reportedTokens','activeMillis','waitMillis']),'warnings':arr(st())},['hash','schema','previewId','against','readOnly','benchmark','baseline','left','right','delta','warnings'])
op=paths['/kun-forks/comparisons/{fid}']['get']
op['x-administrator-only']=True
op['summary']='Kun 来源与分支的固定范围对照'
op['description']='只读管理员接口，不启动/恢复 worker，不连接模型/MCP，不读取项目文件。默认左侧为来源安全点之后至预览 through 的执行，右侧为目标分支；against 可指定同一来源、快照与截止的另一预览。不比较任意会话。leftThrough/rightThrough 是各自宿主日志上界，省略时固定当前值，重复查询应保留响应值（含零）；不是 worker 序号。完整且已结束的日志才计算调用差值；用量缺失、结束预算缺失或记录缺口时返回 null，不按零计算。区分真实工具派发/原录制回放/人工假设替换/失败/拒绝/未见结果；假设包含指纹和 applied/not_reached/unknown 及使用事件；最后新增回复最多 2048 Unicode 字符，带事件证据。继承用量单列。没有任务质量评分、实际费用、优化收益结论、文件产物对比或外部环境快照。每侧最多 4096 事件、32 MiB 总载荷、8 MiB 单事件，超限返回 413；全响应最多 4 MiB。'
op['parameters'] += [{'name':'against','in':'query','schema':st(maxLength=128),'description':'同一基线的另一分叉预览 ID；省略或空串表示来源运行。'}]+[{'name':k,'in':'query','schema':{'type':'integer','minimum':0},'description':'固定宿主事件上界，零也是有效的空范围。'} for k in ['leftThrough','rightThrough']]
op['responses']['200']['content']['application/json']['schema']=ref('KunForkComparison')
for code,description in [('400','无效、未知或重复参数；同一分支与自身比较。'),('403','应用和成员不允许读取该管理员对照。'),('404','分叉预览不存在。'),('409','基线不同、游标无效或证据身份不一致。'),('413','保留记录或响应超过有界读取限制。')]:op['responses'][code]={'description':description}

# Fixed host-side rubric scoring; no execution or independent validation claim.
rule_kind=st(enum=['reply_contains','reply_not_contains','reply_equals'])
rule_status=st(enum=['pass','fail','unknown'])
score_number={'type':['number','null'],'minimum':0,'maximum':100}
schemas['KunReplyRule']=obj({'kind':rule_kind,'value':st(minLength=1,maxLength=2048)},['kind','value'])
rubric_props={'schema':{'const':1},'evaluator':{'const':'reply-rules-v1'},**{k:st() for k in ['id','hash','title','createdAt','previewId','baselineHash']},'ruleCount':{'type':'integer','minimum':2,'maximum':13},'rules':{'type':'array','items':ref('KunReplyRule'),'minItems':1,'maxItems':12}}
schemas['KunRubric']=obj(rubric_props,list(rubric_props))
schemas['KunRubricSummary']=obj({k:rubric_props[k] for k in ['id','hash','title','createdAt','baselineHash','ruleCount']},['id','hash','title','createdAt','baselineHash','ruleCount'])
schemas['KunRuleResult']=obj({'index':integer,'kind':st(enum=['run_completed','reply_contains','reply_not_contains','reply_equals','file_exists','file_contains','file_equals','file_sha256','workspace_fixture_ready']),'status':rule_status,'reason':st(),'eventId':integer},['index','kind','status','reason'])
schemas['KunEvaluationReply']=obj({'status':st(enum=['available','missing','incomplete','too_large']),'hash':st(),'eventId':integer,'sequence':integer,'bytes':integer},['status','bytes'])
evaluation_side={'status':rule_status,**{k:integer for k in ['passed','failed','unknown','total']},'score':score_number,'reply':ref('KunEvaluationReply'),'results':arr(ref('KunRuleResult'))}
schemas['KunEvaluationSide']=obj(evaluation_side,list(evaluation_side))
evaluation_props={'schema':{'const':1},'evaluator':{'const':'reply-rules-v1'},**{k:st() for k in ['id','hash','createdAt','previewId']},'rubric':ref('KunRubric'),'comparison':ref('KunForkComparison'),'left':ref('KunEvaluationSide'),'right':ref('KunEvaluationSide'),'scoreDelta':{'type':['number','null'],'minimum':-100,'maximum':100},'independentValidation':{'const':False},'warnings':arr(st())}
schemas['KunEvaluation']=obj(evaluation_props,list(evaluation_props))
evaluation_summary={**{k:st() for k in ['id','hash','title','createdAt','against']},'leftStatus':rule_status,'rightStatus':rule_status}
schemas['KunEvaluationSummary']=obj(evaluation_summary,list(evaluation_summary))
for path,method,response,body in [
 ('/kun-rubrics','post',ref('KunRubric'),obj({'previewId':st(minLength=1),'expectedPreviewHash':st(minLength=1),'title':st(minLength=1,maxLength=120),'rules':rubric_props['rules']},['previewId','expectedPreviewHash','title','rules'])),
 ('/kun-rubrics','get',obj({'items':arr(ref('KunRubricSummary')),'nextOffset':integer,'hasMore':bool_},['items','nextOffset','hasMore']),None),
 ('/kun-rubrics/{rid}','get',ref('KunRubric'),None),
 ('/kun-evaluations','post',ref('KunEvaluation'),obj({**{k:st(minLength=1) for k in ['rubricId','expectedRubricHash','previewId','expectedComparisonHash']},'against':st(maxLength=128),**{k:{'type':'integer','minimum':0} for k in ['leftThrough','rightThrough']}},['rubricId','expectedRubricHash','previewId','expectedComparisonHash','leftThrough','rightThrough'])),
 ('/kun-evaluations','get',obj({'items':arr(ref('KunEvaluationSummary')),'nextOffset':integer,'hasMore':bool_},['items','nextOffset','hasMore']),None),
 ('/kun-evaluations/{eid}','get',ref('KunEvaluation'),None)
]:
 op=paths[path][method];op['x-administrator-only']=True
 op['summary']='Kun 固定规则与评分报告'
 op['description']='管理员专用；不启动 worker/模型/工具，不读项目文件。规则绑定完整来源基线，预览 hash 必須一致；1–12 条文本规则，逐字匹配（大小写、空白与 Unicode 编码敏感），每条最多 2048 字符，合计最多 16 KiB，禁止重复和全空白规则。另固定加入运行成功结束项，所有项等权。评分请求显式提交 rubric hash、comparison hash 和两侧宿主上界，零不得省略；同一次有界日志读取产生摘要与完整回复评分，记录变化则 409。评分只用最后一次非规划执行响应的新增 assistant 文本，后续请求/空响应/工具调用/拒绝会使早先答案不再合格，不评继承文本。回复最多 256 KiB，超限或日志不完整为 unknown；完整日志没有最终回复则文本规则失败（含不包含规则）。有未知项时 score/scoreDelta 为 null；有确定失败时总体 fail，否则有未知为 unknown，全部通过才 pass。分数只反映规则项通过比例，不是事实验证、产物验收、独立测试集或优化收益。报告保存规则、比较摘要、结果及证据指纹，不保存完整回复；GET 已保存报告不依赖来源日志仍存在，但事件链接依赖留存。规则及报告不可改写，无删除/自动执行/模型裁判接口。'
 op['parameters']=[v for v in op['parameters'] if v['in']!='query']
 if method=='get' and '{' not in path:
  op['parameters'] += [{'name':'previewId','in':'query','required':True,'schema':st(minLength=1,maxLength=128),'description':'rubrics 列出同基线规则，evaluations 列出以此预览为右侧的报告。'},{'name':'offset','in':'query','schema':{'type':'integer','minimum':0,'maximum':20971520}},{'name':'limit','in':'query','schema':{'type':'integer','minimum':1,'maximum':50,'default':20}}]
 op['responses']['200']['content']['application/json']['schema']=response
 for code,desc in [('400','缺少/未知字段、规则/查询参数无效、缺少明确固定上界或幂等 Key。'),('403','应用与成员不得访问评分规则和报告。'),('404','规则、预览或报告不存在。'),('409','来源基线不符、指纹改变、日志删除/变更或持久化内容校验失败。'),('413','日志有界读取或 4 MiB 响应上限。')]:op['responses'][code]={'description':desc}
 if body is not None:op['requestBody']={'required':True,'content':{'application/json':{'schema':body}}}

# Immutable fixed-input task sets; these routes never execute a case.
task_case={'key':st(minLength=1,maxLength=64,pattern='^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$'),'title':st(minLength=1,maxLength=120),'input':st(minLength=1,maxLength=8192),'rules':rubric_props['rules']}
schemas['KunArtifactRule']=obj({'path':st(minLength=1,maxLength=512,description='Workspace-relative path using /; traversal, device names and special paths rejected'),'kind':st(enum=['file_exists','file_contains','file_equals','file_sha256']),'value':st(maxLength=8192,description='At most 8 KiB UTF-8; omitted/empty for exists; empty equals is allowed')},['path','kind'])
schemas['KunArtifactResult']=obj({'index':integer,'path':st(),'kind':st(),'status':rule_status,'reason':st(),'bytes':integer,'sha256':st()},['index','path','kind','status','reason'])
schemas['KunArtifactEvidence']=obj({'rulesHash':st(),'results':arr(ref('KunArtifactResult'))},['rulesHash','results'])
schemas['KunFixtureFile']=obj({'path':st(minLength=1,maxLength=512),'content':st(maxLength=32768,description='UTF-8 text up to 32 KiB; no NUL')},['path','content'])
schemas['KunWorkspaceFixture']=obj({'files':{'type':'array','maxItems':16,'items':ref('KunFixtureFile'),'description':'At most 128 KiB content total. Empty list requests an empty fresh evaluation directory.'}},['files'])
schemas['KunWorkspaceFixtureState']=obj({'mode':{'const':'isolated-fixture-v1'},'root':st(),'hash':st(),'fileCount':integer,'bytes':integer},['mode','root','hash','fileCount','bytes'])
schemas['KunTaskCase']=obj(task_case,list(task_case))
schemas['KunTaskCase']['properties']['workspaceFixture']=ref('KunWorkspaceFixture')
schemas['KunTaskCase']['properties']['artifactRules']={'type':'array','items':ref('KunArtifactRule'),'maxItems':8,'description':'Optional predeclared file checks, captured once at successful completion. Missing evidence is unknown. Each regular file is limited to 1 MiB.'}
task_input={'title':st(minLength=1,maxLength=120),'cases':{'type':'array','items':ref('KunTaskCase'),'minItems':1,'maxItems':20}}
schemas['KunTaskSetInput']=obj(task_input,list(task_input))
task_props={'schema':{'const':1},'evaluator':{'const':'reply-rules-v1'},**{k:st() for k in ['id','hash','title','createdAt']},'caseCount':{'type':'integer','minimum':1,'maximum':20},'cases':task_input['cases']}
schemas['KunTaskSet']=obj(task_props,list(task_props))
task_summary={k:task_props[k] for k in ['id','hash','title','createdAt','caseCount']}
schemas['KunTaskSetSummary']=obj(task_summary,list(task_summary))
for path,method,response in [('/kun-task-sets','post',ref('KunTaskSet')),('/kun-task-sets','get',obj({'items':arr(ref('KunTaskSetSummary')),'nextOffset':integer,'hasMore':bool_},['items','nextOffset','hasMore'])),('/kun-task-sets/{tid}','get',ref('KunTaskSet'))]:
 op=paths[path][method];op['x-administrator-only']=True;op['summary']='Kun 固定输入任务集'
 op['description']='管理员专用。保存不可变的 1–20 个有序案例，每个案例包含唯一 ASCII 标识、标题、完整固定输入与 1–12 条文本规则；固定要求运行成功结束。标识大小写敏感，禁止重复；输入最多 8192 Unicode 字符/32 KiB UTF-8，每案例规则文本最多 16 KiB，总文本最多 256 KiB。保留原有空白，不执行输入、正则、脚本、模型或工具。创建只接受 title/cases，身份、指纹、时间均由服务端生成；无原位修改或删除；执行与评分通过单独的案例运行接口。查看完整对象校验指纹；列表只投影有界元数据，不返回输入或规则正文。定义本身不是 K4 成绩；K4 另行固定验证批次和评分报告。'
 op['parameters']=[v for v in op['parameters'] if v['in']!='query']
 if method=='get' and '{' not in path:
  op['parameters'] += [{'name':'offset','in':'query','schema':{'type':'integer','minimum':0,'maximum':20971520}},{'name':'limit','in':'query','schema':{'type':'integer','minimum':1,'maximum':50,'default':20}}]
 if method=='post':op['requestBody']={'required':True,'content':{'application/json':{'schema':ref('KunTaskSetInput')}}}
 op['responses']['200']['content']['application/json']['schema']=response
 for code,desc in [('400','字段、案例、规则、总量或查询参数无效；缺少幂等 Key。'),('403','应用与成员不得访问任务集。'),('404','任务集不存在。'),('409','持久化内容校验失败或幂等 Key 用于不同请求。')]:op['responses'][code]={'description':desc}

# Unified read-only debug facade. Existing Kun aliases retain their response shape.
debug_kinds=['overview','run','events','event','context','tools','budget','modules','breakpoints','actions','snapshot','evidence','diff','runs','steps','step','issues','statistics','llm_requests','llm_request']
schemas['DebugCapability']=obj({'supported':bool_,'available':bool_,'reason':st()},['supported','available'])
schemas['DebugCapabilities']=obj({'sessionId':st(),'backend':st(enum=['codex','kun']),'readOnly':{'const':True},'queries':{'type':'object','additionalProperties':ref('DebugCapability')},'hostEventCursor':st(),'workerSequence':st()},['sessionId','backend','readOnly','queries','hostEventCursor','workerSequence'])
schemas['DebugEventMeta']=obj({'id':integer,'time':st(),'direction':st(),'method':st(),'bytes':integer},['id','time','direction','method','bytes'])
schemas['DebugEventPage']=obj({'events':arr(ref('DebugEventMeta')),'through':integer,'nextCursor':integer,'hasMore':bool_},['events','through','nextCursor','hasMore'])
schemas['DebugEventChunk']=obj({'eventId':integer,'text':st(),'offset':integer,'nextOffset':integer,'totalCharacters':integer,'hasMore':bool_,'encoding':st()},['eventId','text','offset','nextOffset','totalCharacters','hasMore','encoding'])
schemas['DebugRun']=obj({'id':st(),'question':st(),'status':st(),'start':st(),'end':st(),'inputEventId':integer,'association':st(enum=['observed','reconstructed','inferred','missing']),'eventIds':arr(integer)},['id','question','status','start','inputEventId','association','eventIds'])
schemas['DebugStep']=obj({'id':st(),'runId':st(),'type':st(),'title':st(),'status':st(),'start':st(),'end':st(),'durationMs':integer,'reportedDurationMs':integer,'point':bool_,'eventIds':arr(integer),'preview':obj(extra=True),'previewOnly':bool_,'clock':st(),'association':st(enum=['observed','reconstructed','inferred','missing']),'startObserved':bool_,'endObserved':bool_,'missing':arr(st()),'issues':arr(st()),'previewTruncated':bool_,'evidenceTruncated':bool_,'workerSequences':arr(integer)},['id','runId','type','title','status','point','eventIds','preview','previewOnly','clock','association','startObserved','endObserved','missing','issues','previewTruncated','evidenceTruncated'])
schemas['DebugResult']=obj({'sessionId':st(),'backend':st(enum=['codex','kun']),'source':st(enum=['host_session','host_journal','host_projection','http_observation','kun_worker']),'kind':st(enum=debug_kinds),'through':integer,'runId':st(),'revision':integer,'sequence':integer,'data':{}},['sessionId','backend','source','kind','data'])
for suffix,response in [('capabilities','DebugCapabilities'),('query','DebugResult')]:
 op=paths['/sessions/{sid}/debug/'+suffix]['get']
 op['summary']='Unified read-only debug '+suffix
 op['description']='只读；应用凭据需 read 与会话归属，成员需项目授权。Codex 支持 overview/run（宿主概况，无 revision/sequence）、events/event（宿主保留事件）、runs/steps/step/issues/statistics（生命周期记录重建）及 llm_requests/llm_request（已保留的 HTTP 观测）；内部快照/上下文等仍返回 409 debug_unsupported。Kun 内部查询需在线 worker；不启动/恢复进程，不调用模型或工具，不写入对话。source 区分 host_session、host_journal、host_projection、http_observation、kun_worker。重建步骤不等于完整模型内部状态。先查询 capabilities；supported 与 available 区分支持情况与当前可用性。响应最大 4 MiB，超限返回 413。'
 op['x-administrator-only']=False;op['x-application-scopes']=['read']
 op['responses']['200']['content']['application/json']['schema']=ref(response)
 op['responses']['400']={'description':'无效、重复、未知、不适用参数或越界游标'}
 op['responses']['404']={'description':'会话或此会话的宿主事件不存在'}
 op['responses']['409']={'description':'不支持此能力，Kun worker 离线或快照查询被拒绝'}
 op['responses']['413']={'description':'响应超过 4 MiB 或单个宿主事件解析前超过 8 MiB；重建超过 20000 生命周期事件或 64 MiB'}
 if suffix=='capabilities':continue
 op['parameters'].append({'name':'kind','in':'query','required':True,'schema':st(enum=debug_kinds)})
 selectors=[('sequence',0,None,'Kun worker 序号。run/context/tools/budget/modules/breakpoints/actions 省略或 0 表示当前；snapshot/evidence/diff 要求正数。不是宿主事件 ID。'),('fromSequence',1,None,'仅 diff 使用且必填；与 sequence 属于同一会话。'),('after',0,None,'仅 events；宿主事件 ID 下界，不包含；默认 0。'),('through',0,None,'用于 events/event、五种重建查询和 llm_requests/llm_request；省略时固定当前宿主事件上界（event 默认不限上界）。后续分页必须原样保留响应 through，包括 0。不接受大于当前已保留最大 ID 的值。'),('eventId',1,None,'event/llm_request 必填，严格属于此会话的宿主事件 ID。llm_request 仅接受 llm/exchange 事件。'),('offset',0,2147483647,'event 为脱敏 JSON 的 Unicode 字符偏移；llm_request 正文为字节偏移，metadata 仅接受 0，*_chunks 为采集片段偏移，*_messages 为消息偏移，*_payload 为选定消息的字节偏移；runs/steps/issues/llm_requests 为记录偏移。默认 0。'),('limit',1,16000,'events 默认 50、最大 200；event 默认 4000、最大 16000 字符；runs/steps/issues 默认 50、最大 50；llm_requests 默认 25、最大 50；llm_request 正文默认 4000、最大 16000 字节，*_chunks 和 *_messages 默认/最大 50，*_payload 默认 4000、最大 16000 字节。其他 kind 不接受。')]
 for name,minimum,maximum,description in selectors:
  schema={'type':'integer','minimum':minimum}
  if maximum is not None:schema['maximum']=maximum
  op['parameters'].append({'name':name,'in':'query','required':False,'schema':schema,'description':description})
 for name,maximum,description in [('runId',256,'steps/step/issues/statistics 的所属轮次，存在性与 step 归属均核对；llm_requests/llm_request 按已记录 labels.runId 精确筛选，不猜测缺失关联。'),('type',256,'steps/issues/statistics 的准确类型匹配。'),('status',256,'steps/issues/statistics 的准确状态匹配。'),('query',1000,'steps/issues/statistics 的脱敏预览字面子串搜索，不是完整输出搜索。'),('stepId',256,'step 必填，由 steps 返回的 step-宿主事件ID。')]:
  op['parameters'].append({'name':name,'in':'query','required':False,'schema':st(maxLength=maximum),'description':description})
 op['parameters'].append({'name':'section','in':'query','required':False,'schema':st(enum=['metadata','request','response','websocket_upstream','websocket_client','request_chunks','response_chunks','websocket_upstream_chunks','websocket_client_chunks','websocket_upstream_messages','websocket_client_messages','websocket_upstream_payload','websocket_client_payload']),'description':'仅 llm_request；默认 metadata。正文返回 bodyBase64（精确字节）和 text（UTF-8 显示），*_chunks 读取采集片段元数据；采集片段不是 SSE/WebSocket 消息边界。*_messages 为只读解码消息列表，*_payload 按 messageIndex 读取解码载荷；压缩消息保留压缩字节并显式标记，不伪造文本。'})
 op['parameters'].append({'name':'messageIndex','in':'query','required':False,'schema':{'type':'integer','minimum':0,'maximum':4095},'description':'仅 llm_request 的 websocket_upstream_payload/websocket_client_payload；默认 0。其余 section 即使显式传 0 也拒绝。'})
 op['description']+=' llm_requests/llm_request 的 source=http_observation。列表只返回已保留观测的摘要；缺记录不代表没有上游请求。请求详情以 metadata、request/response 和 websocket 方向分块读取；正文 offset/limit 为字节，跨块精确恢复须拼接 bodyBase64 解码后的字节，不可拼接可能替换无效 UTF-8 的 text。标记 complete、truncated、incomplete 与 captureLoss，不能将有限采集判为完整。元数据上限 512 KiB，查询期限 8 秒。WebSocket 只读解码最多读取 32 MiB 原始帧，聚合载荷最多 16 MiB、4096 消息和 16384 帧；部分/压缩/非法帧明确标记。'
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


schemas['ImageBuild']=obj({k:st() for k in ['id','instanceId','name','dockerfile','reference','created','updated','started','finished','contextSha256','imageId','imageVersionId','error','scope','source','profileId','codexVersion']}|{'offlineReady':bool_,'status':st(enum=['draft','queued','running','canceling','canceled','interrupted','failed','succeeded']),'files':integer,'contextBytes':integer,'timeoutMinutes':integer,'noCache':bool_,'logTruncated':bool_},['id','instanceId','name','status','dockerfile','reference','contextSha256','files','contextBytes','timeoutMinutes','noCache','logTruncated'])
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
# K4 fixed cases, parameterized candidates and independent validation gates.
schemas['KunCompareTool']['properties']['reused']=integer
schemas['Input']['properties']['fastSkillId']=st(description='可选：仅已启用指定 Skill 的应用会话；需要 JSON 输入。')
schemas['KunFastValue']=obj({'ref':st(),'literal':{}},extra=False)
schemas['KunFastInstruction']=obj({'op':st(enum=['tool','extract','assert','branch','finish']),'store':st(),'tool':st(),'args':{'type':'object','additionalProperties':ref('KunFastValue')},'value':ref('KunFastValue'),'pointer':st(),'schema':{},'equals':{},'then':integer,'else':integer},['op'])
schemas['KunFastGuard']=obj({k:st() for k in ['engineVersion','configHash','skillsHash','mcpHash','workspace','catalogHash','approvalPolicy','harnessHash','contextRevision']},['engineVersion','configHash','skillsHash','mcpHash','workspace','catalogHash','approvalPolicy','harnessHash','contextRevision'])
schemas['KunFastProgram']=obj({'schema':{'const':1},'domain':st(maxLength=120),'inputSchema':{},'guard':ref('KunFastGuard'),'steps':{'type':'array','items':ref('KunFastInstruction'),'minItems':1,'maxItems':64}},['schema','domain','inputSchema','guard','steps'])
schemas['KunState']['properties']['fast']=obj({'candidateId':st(),'candidateHash':st(),'program':ref('KunFastProgram'),'input':{},'status':st(enum=['active','completed','fallback']),'reason':st(),'pc':integer,'values':obj(extra=True),'evidence':obj(extra=True),'pendingStore':st(),'completed':obj(extra=True)},extra=False)
case_preview={k:st() for k in ['id','hash','createdAt','engineVersion','taskSetId','taskSetHash','taskSetTitle','sessionId','runId','instanceId','instanceName','workspaceId','workspace','effectiveInput','inputHash','approvalPolicy']}
case_preview.update({'schema':{'const':1},'protocolVersion':integer,'case':ref('KunTaskCase'),'config':ref('KunConfig'),'mcpServers':arr(st()),'candidateId':st(),'candidateHash':st()})
schemas['KunCaseRunPreview']=obj(case_preview,[k for k in case_preview if k not in ['candidateId','candidateHash']])
schemas['KunCaseRunView']=obj({'preview':ref('KunCaseRunPreview'),'status':st(),'session':ref('Session')},['preview','status'])
schemas['KunCaseRunInput']=obj({k:st() for k in ['taskSetId','expectedTaskSetHash','caseKey','workspaceId','instanceId','candidateId']},['taskSetId','expectedTaskSetHash','caseKey','workspaceId'])
schemas['Session']['properties']['kunCase']=obj({k:st() for k in ['planId','taskSetId','taskSetHash','caseKey','inputHash','runId']},['planId','taskSetId','taskSetHash','caseKey','inputHash','runId'])
skill_in={'title':st(maxLength=120),'domain':st(maxLength=120),'applicability':st(maxLength=8192),'failureCases':{'type':'array','items':st(maxLength=2048),'minItems':1,'maxItems':20},'sourcePlanIds':{'type':'array','items':st(),'minItems':1,'maxItems':10},'validationTaskSetId':st(),'expectedValidationHash':st(),'inputSchema':{},'steps':arr(ref('KunFastInstruction'))}
schemas['KunSkillInput']=obj(skill_in,[k for k in skill_in if k!='steps'])
source=obj({k:st() for k in ['planId','sessionId','runId','inputHash','evidenceHash']}|{'through':integer},['planId','sessionId','runId','inputHash','evidenceHash','through'])
skill={k:st() for k in ['id','hash','title','createdAt','applicability','instanceId','workspaceId']};skill.update({'schema':{'const':1},'failureCases':arr(st()),'sources':arr(source),'validationSet':ref('KunTaskSet'),'program':ref('KunFastProgram')})
schemas['KunSkillCandidate']=obj(skill,list(skill))
validation={k:st() for k in ['id','hash','candidateId','candidateHash','createdAt']}|{'plans':arr(ref('KunCaseRunPreview'))}
schemas['KunSkillValidation']=obj(validation,list(validation))
case_score={'planId':st(),'caseKey':st(),'summary':ref('KunCompareSide'),'evaluation':ref('KunEvaluationSide'),'fastCompleted':bool_,'evidenceHash':st()}
schemas['KunCaseScore']=obj(case_score,list(case_score))
schemas['KunCaseScore']['properties']['workspaceFixture']=ref('KunWorkspaceFixtureState')
schemas['KunCaseRunPreview']['properties']['executionWorkspace']=st()
schemas['KunCaseScore']['properties']['artifacts']=ref('KunArtifactEvidence')
report={k:st() for k in ['id','hash','candidateId','candidateHash','validationId','createdAt','status']}|{k:integer for k in ['passed','failed','unknown','total']}|{'score':{'type':['number','null']},'cases':arr(ref('KunCaseScore'))}
schemas['KunSkillReport']=obj(report,list(report))
cap={k:st() for k in ['appId','candidateId','candidateHash','reportId','reportHash','updatedAt']}|{'enabled':bool_}
schemas['KunFastCapability']=obj(cap,list(cap))
expect=obj({'expectedHash':st()},['expectedHash'])
start=obj({'expectedHash':st(),'confirmExecute':{'const':True}},['expectedHash','confirmExecute'])
promotion=obj({'expectedHash':st(),'reportId':st(),'reportHash':st(),'appId':st(),'expectedAppRevision':integer},['expectedHash','reportId','reportHash','appId','expectedAppRevision'])
page=obj({'items':arr(obj(extra=True)),'nextOffset':integer,'hasMore':bool_},['items','nextOffset','hasMore'])
k4routes=[
 ('/kun-case-runs','post',ref('KunCaseRunInput'),ref('KunCaseRunView')),
 ('/kun-case-runs','get',None,page),('/kun-case-runs/{cid}','get',None,ref('KunCaseRunView')),
 ('/kun-case-runs/{cid}/start','post',start,ref('Session')),('/kun-case-runs/{cid}/score','get',None,ref('KunCaseScore')),
 ('/kun-skills','post',ref('KunSkillInput'),ref('KunSkillCandidate')),('/kun-skills','get',None,page),
 ('/kun-skills/{kid}','get',None,ref('KunSkillCandidate')),('/kun-skills/{kid}/validations','post',expect,ref('KunSkillValidation')),
 ('/kun-skills/{kid}/promote','post',promotion,ref('KunFastCapability')),('/kun-skills/{kid}/disable','post',obj({'appId':st()},['appId']),ref('KunFastCapability')),
 ('/kun-skill-validations','get',None,page),('/kun-skill-validations/{vid}','get',None,ref('KunSkillValidation')),
 ('/kun-skill-validations/{vid}/start','post',start,obj({'results':arr(obj({'planId':st(),'session':ref('Session'),'error':st()},['planId','session']))},['results'])),
 ('/kun-skill-validations/{vid}/evaluate','post',expect,ref('KunSkillReport')),('/kun-skill-reports/{rid}','get',None,ref('KunSkillReport'))]
for path,method,body,response in k4routes:
 params=[{'name':n,'in':'path','required':True,'schema':st()} for n in re.findall(r'\{(.*?)\}',path)]
 if method=='post':params.append({'name':'Idempotency-Key','in':'header','required':True,'schema':st(pattern='^[A-Za-z0-9][A-Za-z0-9_.:-]{7,127}$')})
 if method=='get' and path in ['/kun-case-runs','/kun-skills','/kun-skill-validations']:
  params += [{'name':'offset','in':'query','schema':{'type':'integer','minimum':0,'maximum':20971520}},{'name':'limit','in':'query','schema':{'type':'integer','minimum':1,'maximum':50,'default':20}}]
  if path=='/kun-case-runs':params.append({'name':'taskSetId','in':'query','required':True,'schema':st()})
  else:params.append({'name':'candidateId','in':'query','required':path=='/kun-skill-validations','schema':st()})
 op={'summary':'Kun K4 案例、Skill 候选与独立验证','description':'管理员专用。预览不执行。案例固定输入/环境/运行身份；同预览重复启动返回同一运行。候选从成功案例参数化提炼，或接受受限 IR 草稿；不复制最终答案。验证集至少两个案例，拒绝与来源相同的输入；验证批次固定所有案例。评分读取完整固定宿主记录，缺失不按零。全部案例评分通过且快速路径完成后才可启用到环境匹配的指定应用。启用不修改全局 Skill 文件。运行以现有工具校验、审批、预算和动作账本为准；守卫偏离保留完成结果并回退，不重复派发相同已完成副作用。验证执行可能部分启动：检查每项 error，已绑定运行不会重跑；不同逻辑重试使用新幂等 Key。','x-administrator-only':True,'parameters':params,'responses':{'200':{'description':'成功','content':{'application/json':{'schema':response}}},'400':{'description':'参数、输入、IR 或幂等 Key 无效'},'403':{'description':'仅管理员'},'404':{'description':'对象不存在'},'409':{'description':'指纹、环境、绑定、执行确认或独立验证门槛不满足'},'413':{'description':'有界读取或响应限制'}}}
 if body is not None:op['requestBody']={'required':True,'content':{'application/json':{'schema':body}}}
 paths.setdefault(path,{})[method]=op


# K4 repeated, fixed paired execution. It is descriptive and cannot promote Skills.
paths.pop('/kun-benchmarks/{bid}/',None) # Dynamic route prefix is not an endpoint.
schemas['KunCaseRunPreview']['properties']['benchmarkId']=st()
schemas['KunCaseScore']['properties']['fastFallback']=bool_
bi={'title':st(maxLength=120),'candidateId':st(),'expectedCandidateHash':st(),'repetitions':{'type':'integer','minimum':2,'maximum':5}}
schemas['KunBenchmarkInput']=obj(bi,list(bi))
bp={k:st() for k in ['planId','planHash','sessionId','runId','caseKey']}|{'sample':integer,'arm':st(enum=['dynamic','fast'])}
schemas['KunBenchmarkPlan']=obj(bp,list(bp))
bm={k:st() for k in ['id','hash','title','candidateId','candidateHash','createdAt','environmentHash']}|{'schema':{'const':1},'repetitions':integer,'taskSet':ref('KunTaskSet'),'environment':ref('KunCaseRunPreview'),'plans':arr(ref('KunBenchmarkPlan'))}
schemas['KunBenchmark']=obj(bm,list(bm))
bx={'status':st(enum=['planned','running','waiting','stopped','interrupted','blocked','completed']),'next':integer,'total':integer}|{k:st() for k in ['planId','sessionId','error','updatedAt']}
schemas['KunBenchmarkExecution']=obj(bx,['status','next','total'])
schemas['KunBenchmarkView']=obj({'benchmark':ref('KunBenchmark'),'execution':ref('KunBenchmarkExecution')},['benchmark','execution'])
nullable_number={'type':['number','null']}
metric={'count':integer,'missing':integer}|{k:nullable_number for k in ['total','mean','min','max']}
schemas['KunBenchmarkMetric']=obj(metric,list(metric))
metrics={k:ref('KunBenchmarkMetric') for k in ['modelCalls','reportedTokens','runMillis']}
schemas['KunBenchmarkMetrics']=obj(metrics,list(metrics))
ba={k:integer for k in ['total','passed','failed','unknown','fastCompleted','fallbacks']}|{'passRate':nullable_number,'metrics':ref('KunBenchmarkMetrics')}
schemas['KunBenchmarkArm']=obj(ba,list(ba))
br={'plan':ref('KunBenchmarkPlan'),'score':ref('KunCaseScore')}
schemas['KunBenchmarkResult']=obj(br,list(br))
pair={k:st() for k in ['caseKey','dynamicPlanId','fastPlanId']}|{'sample':integer,'outcome':st(enum=['unknown','both_pass','both_fail','fast_only_pass','dynamic_only_pass'])}
schemas['KunBenchmarkPair']=obj(pair,list(pair))
report={k:st() for k in ['id','hash','createdAt','benchmarkId','benchmarkHash','candidateId','candidateHash']}|{'schema':{'const':1},'status':st(enum=['complete','partial']),'dynamic':ref('KunBenchmarkArm'),'fast':ref('KunBenchmarkArm'),'pairedDelta':ref('KunBenchmarkMetrics'),'pairs':arr(ref('KunBenchmarkPair')),'results':arr(ref('KunBenchmarkResult')),'warnings':arr(st())}
schemas['KunBenchmarkReport']=obj(report,list(report))
for path,method,body,response in [
 ('/kun-benchmarks','post',ref('KunBenchmarkInput'),ref('KunBenchmarkView')),
 ('/kun-benchmarks','get',None,page),('/kun-benchmarks/{bid}','get',None,ref('KunBenchmarkView')),
 ('/kun-benchmarks/{bid}/start','post',start,ref('KunBenchmarkView')),
 ('/kun-benchmarks/{bid}/stop','post',expect,ref('KunBenchmarkView')),
 ('/kun-benchmarks/{bid}/evaluate','post',expect,ref('KunBenchmarkReport')),
 ('/kun-benchmark-reports','get',None,page),('/kun-benchmark-reports/{rid}','get',None,ref('KunBenchmarkReport'))]:
 params=[{'name':n,'in':'path','required':True,'schema':st()} for n in re.findall(r'\{(.*?)\}',path)]
 if method=='post':params.append({'name':'Idempotency-Key','in':'header','required':True,'schema':st(pattern='^[A-Za-z0-9][A-Za-z0-9_.:-]{7,127}$')})
 if method=='get' and path in ['/kun-benchmarks','/kun-benchmark-reports']:
  params += [{'name':'offset','in':'query','schema':{'type':'integer','minimum':0,'maximum':20971520}},{'name':'limit','in':'query','schema':{'type':'integer','minimum':1,'maximum':50,'default':20}},{'name':'candidateId' if path=='/kun-benchmarks' else 'benchmarkId','in':'query','required':True,'schema':st()}]
 op={'summary':'Kun K4 多次采样对照','description':'管理员专用。原子保存全部固定案例、环境和交替执行顺序；每例重复 2–5 次，总运行最多 100。整批一次确认后后台逐次运行，遵守共享容量与审批。stop 只停止后续派发，在途运行继续；宿主重启不自动继续，中断/失败/删除的样本不会重跑。逐次案例不能单独启动。报告不可变、逐次固定日志证据；回退成功计为通过并单列回退次数；未知不按零，总值须完整，均值及范围仅覆盖 count 个已知样本，pairedDelta 为 fast − dynamic。此描述性结果不证明因果或统计显著性，不重置工作区/外部服务，不计算货币成本，也不能替代独立验证启用门槛。','x-administrator-only':True,'parameters':params,'responses':{'200':{'description':'成功；start 仅接收批次，使用 GET 查询执行状态','content':{'application/json':{'schema':response}}},'400':{'description':'输入、分页或幂等 Key 无效'},'403':{'description':'仅管理员'},'404':{'description':'对象不存在'},'409':{'description':'指纹/绑定变化、未确认、另一批次在执行或先前运行结果不确定'},'413':{'description':'预览或报告超出界限'}}}
 if body is not None:op['requestBody']={'required':True,'content':{'application/json':{'schema':body}}}
 paths.setdefault(path,{})[method]=op

schemas['KunFastGuard']['properties']['workspaceMode']=st(enum=['isolated-fixture-v1'])

# Startup and backend discovery describe prerequisites, never inference readiness.
schemas['BackendInfo']=obj({'kind':st(),'executionModes':arr(st()),'capabilities':arr(st())},['kind','executionModes','capabilities'])
schemas['EnvironmentCheck']=obj({'id':st(),'required':bool_,'status':st(enum=['ready','unavailable','unsupported','unconfigured','unchecked','skipped']),'detail':st()},['id','required','status','detail'])
schemas['SetupEnvironment']=obj({'instanceId':st(),'backend':st(),'ready':bool_,'inferenceTested':{'const':False},'demo':bool_,'checks':arr(ref('EnvironmentCheck'))},['instanceId','backend','ready','inferenceTested','demo','checks'])
schemas['CodexInstallResult']=obj({'ok':bool_,'version':st(),'codex':st(),'log':st(),'error':st()},['ok','version','codex','log','error'])
for path,schema in [('/backends',obj({'backends':arr(ref('BackendInfo')),'availability':st()},['backends','availability'])),('/setup/environment',ref('SetupEnvironment')),('/setup/codex-install',ref('CodexInstallResult'))]:
 method='post' if path.endswith('codex-install') else 'get'
 paths[path][method]['responses']['200']['content']['application/json']['schema']=schema
 paths[path][method]['x-administrator-only']=True
paths['/setup/environment']['get']['parameters'] += [{'name':name,'in':'query','schema':st()} for name in ['instanceId','workspaceId']]
paths['/setup/codex-install']['post']['description']='Explicit administrator confirmation; pinned release; managed local prefix; requires Node/npm. Up to 5 minutes. Does not activate the binary or log in. Result ok=false records a failed installation; HTTP acceptance alone is not success. Unknown outcome must be reconciled with the original idempotency receipt.'
paths['/setup']['put']['description']='Atomic setup and default-instance update. agentRuntime requires instanceRevision. Kun completion requires the worker and any configured credential variable, not Codex. Existing sessions keep backend identity.'

# Application-owned, repeatable Docker startup; no image/configuration writes.
schemas['ApplicationRuntimeInput']=obj({'instanceId':st(),'workspaceId':st(),'probe':bool_},['instanceId','workspaceId'])
schemas['ApplicationRuntime']=obj({'appId':st(),'instanceId':st(),'workspaceId':st(),'executionMode':st(enum=['local','docker']),'instanceRevision':integer,'image':st(),'probed':bool_,'modelTested':{'const':False},'accountReady':bool_,'servers':{'type':'object','additionalProperties':arr(st())},'errors':{'type':'object','additionalProperties':st()},'environment':obj({'id':st(),'state':st(),'imageId':st(),'revision':integer,'observedAt':st(),'pendingUpdate':bool_},['id','state','revision','pendingUpdate'])},['appId','instanceId','workspaceId','executionMode','probed','modelTested'])
for path,method in [('/applications/{appId}/runtime','get'),('/applications/{appId}/runtime/prepare','post')]:
 op=paths[path][method];op['x-administrator-only']=False;op['x-application-scopes']=['read'] if method=='get' else ['read','run']
 op['summary']='读取应用运行环境快照' if method=='get' else '启动或复用管理员选定的 Docker 环境'
 op['description']='仅应用绑定的实例和工作区。GET 不启动容器，状态为最近观察值。POST 需显式传入匹配的 instanceId/workspaceId，可安全重试；不选择镜像、不部署待更新版本、不关闭已有会话、不调用模型。probe=true 检查认证状态与 MCP 工具发现，返回名称而不返回密钥、URL 或宿主路径；探测成功不代表模型或业务质量通过。镜像构建、选择、认证与配置仍需管理员。'
 op['responses']['200']['content']['application/json']['schema']=ref('ApplicationRuntime')
 if method=='post':op['requestBody']={'required':True,'content':{'application/json':{'schema':ref('ApplicationRuntimeInput')}}}

paths['/setup/runtime-images']['post']['responses']['200']={'description':'Background job; acceptance is not readiness','content':{'application/json':{'schema':ref('ImageBuild')}}}
paths['/setup/runtime-images']['post']['description']='管理员在应用注册前准备共享镜像。build/pull/local 均异步入串行队列；相同在途请求复用任务。GET /instances/default/builds 与 log/cancel/delete 查看和管理。succeeded + offlineReady 仅代表离线工具检查，不表示模型认证、网络或研究通过；不会修改任何应用绑定。宿主机不需要 Python/npm。'
paths['/setup/runtime-profiles']['get']['description']='内置配置目录、宿主平台和支持状态。只读；不会构建、拉取或登录。'
# Fixed, scoped diagnostics: offline container probe, optional shared challenge,
# and fixed read-only MCP image tool. Never a general shell/MCP execution API.
schemas.update({'ApplicationRuntimeDiagnosticsInput': {'type': 'object',
                                        'additionalProperties': False,
                                        'required': ['instanceId', 'workspaceId', 'nonce'],
                                        'properties': {'instanceId': {'type': 'string'},
                                                       'workspaceId': {'type': 'string'},
                                                       'nonce': {'type': 'string', 'pattern': '^[0-9a-f]{24}$'},
                                                       'challenge': {'type': 'object',
                                                                     'additionalProperties': False,
                                                                     'required': ['path', 'nonce'],
                                                                     'properties': {'path': {'type': 'string',
                                                                                             'pattern': '^outputs/[A-Za-z0-9_-]{1,128}/\\.rundesk-probes/[0-9a-f]{24}/request\\.json$'},
                                                                                    'nonce': {'type': 'string',
                                                                                              'pattern': '^[0-9a-f]{24}$'}}},
                                                       'mcpImageServer': {'type': 'string', 'pattern': '^[a-zA-Z0-9_-]{1,64}$'}}},
 'ApplicationRuntimeDiagnostics': {'type': 'object',
                                   'required': ['schemaVersion',
                                                'appId',
                                                'instanceId',
                                                'workspaceId',
                                                'instanceRevision',
                                                'executionMode',
                                                'checkedAt',
                                                'modelTested',
                                                'modelVisionVerified',
                                                'runtime',
                                                'errors'],
                                   'properties': {'schemaVersion': {'const': 1},
                                                  'appId': {'type': 'string'},
                                                  'instanceId': {'type': 'string'},
                                                  'workspaceId': {'type': 'string'},
                                                  'instanceRevision': {'type': 'integer'},
                                                  'executionMode': {'const': 'docker'},
                                                  'checkedAt': {'type': 'string'},
                                                  'modelTested': {'const': False},
                                                  'modelVisionVerified': {'const': False},
                                                  'runtime': {'type': 'object',
                                                              'properties': {'status': {'enum': ['ready', 'incomplete']},
                                                                             'profileId': {'type': 'string'},
                                                                             'profileVersion': {'type': 'string'},
                                                                             'adapterContractVersion': {'type': 'integer'},
                                                                             'videoUseCommit': {'type': 'string'},
                                                                             'pythonVersion': {'type': 'string'},
                                                                             'ffmpegVersion': {'type': 'string'},
                                                                             'checks': {'type': 'object',
                                                                                        'additionalProperties': {'type': 'string'}}}},
                                                  'challenge': {'type': 'object',
                                                                'properties': {'status': {'enum': ['verified', 'failed']},
                                                                               'path': {'type': 'string'},
                                                                               'responsePath': {'type': 'string'},
                                                                               'nonce': {'type': 'string', 'pattern': '^[0-9a-f]{24}$'},
                                                                               'requestSHA256': {'type': 'string'}}},
                                                  'imageTransport': {'type': 'object',
                                                                     'properties': {'state': {'enum': ['verified',
                                                                                                       'failed',
                                                                                                       'unsupported']},
                                                                                    'nonce': {'type': 'string',
                                                                                              'pattern': '^[0-9a-f]{24}$'},
                                                                                    'imageSHA256': {'type': 'string'},
                                                                                    'mimeType': {'type': 'string'},
                                                                                    'width': {'type': 'integer'},
                                                                                    'height': {'type': 'integer'},
                                                                                    'code': {'type': 'string'}}},
                                                  'errors': {'type': 'object', 'additionalProperties': {'type': 'string'}}}}})
schemas['ApplicationRuntimeDiagnostics']['properties']['runtime']['properties']['ffprobeVersion'] = st()
schemas['ApplicationRuntimeDiagnostics']['properties']['runtime']['properties'].update({'hyperframesVersion': st(), 'gsapVersion': st(), 'nodeVersion': st(), 'browserVersion': st(), 'hasVideoUse': bool_})
schemas['ApplicationRuntimeDiagnostics']['properties'].update({'probed': bool_, 'accountReady': bool_, 'servers': {'type':'object','additionalProperties':arr(st())}})
schemas['ApplicationRuntimeDiagnostics']['required'] += ['probed', 'accountReady', 'servers']
paths['/applications/{appId}/runtime/diagnostics'] = {'post': {'operationId': 'post_applications_appId_runtime_diagnostics',
          'summary': '诊断已选运行容器、共享目录和固定 MCP 图片传输',
          'parameters': [{'name': 'appId', 'in': 'path', 'required': True, 'schema': {'type': 'string'}},
                         {'name': 'X-Request-ID',
                          'in': 'header',
                          'schema': {'type': 'string'},
                          'description': '可选关联编号；响应回传有效编号，否则由服务器生成。'}],
          'responses': {'200': {'description': '成功。202 仅表示任务已接收。',
                                'content': {'application/json': {'schema': {'$ref': '#/components/schemas/ApplicationRuntimeDiagnostics'}}},
                                'headers': {'X-Request-ID': {'schema': {'type': 'string'}},
                                            'RunDesk-API-Version': {'schema': {'type': 'string', 'enum': ['v1']}},
                                            'Idempotency-Replayed': {'schema': {'type': 'string', 'enum': ['true', 'false']},
                                                                     'description': '仅适用于幂等提交；true 表示返回原始回执。'}}},
                        'default': {'description': '结构化错误。retryable=true 仅允许按相同操作与 Key 重试。',
                                    'content': {'application/json': {'schema': {'$ref': '#/components/schemas/Error'}}}}},
          'x-administrator-only': False,
          'x-application-scopes': ['read', 'run'],
          'description': '需要 application-runtime-diagnostics 能力。只在已运行且绑定一致的 Codex Docker 容器执行固定隔离 Python；不会启动、选择、重建环境或回退宿主机，不调用模型。nonce '
                         '必须为24位小写十六进制。可选 challenge 路径必须属于本应用现有会话，challenge.nonce 等于顶层 nonce；请求文件为 {schemaVersion:1,nonce}，返回同目录原子创建 '
                         'response.json 的路径及原始请求 SHA-256。可选 mcpImageServer 只能选择有效配置的服务，调用固定只读工具 rundesk_diagnostic_image({nonce})；支持 HTTP '
                         'JSON，SSE/stdio 明确返回 unsupported。凭据只经容器 stdin/环境传递，返回图片摘要而非原图、URL、密钥或宿主路径。调用方须验证共享响应与预期图片摘要；modelVisionVerified '
                         '始终 false。诊断前后重新检查绑定、配置和容器。',
          'requestBody': {'required': True,
                          'content': {'application/json': {'schema': {'$ref': '#/components/schemas/ApplicationRuntimeDiagnosticsInput'}}}}}}

# Application runtime presets select administrator-approved, isolated bindings.
# Separate from /setup/runtime-profiles (image build recipes).
preset_id=st(pattern=r'^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$',maxLength=128)
schemas['ApplicationRuntimePreset']=obj({
 'id':preset_id,'appId':st(),'name':st(),'description':st(),'instanceId':st(),
 'workspaceId':st(),'enabled':bool_,'revision':integer,'created':st()
},['id','appId','name','instanceId','workspaceId','enabled','revision','created'])
schemas['ApplicationRuntimePresetInput']=obj({
 'name':st(minLength=1,maxLength=160),'description':st(maxLength=4096),
 'instanceId':st(),'workspaceId':st(),'enabled':bool_,'revision':integer
},['name','instanceId','workspaceId'])
schemas['ApplicationPresetBinding']=obj({'presetId':preset_id,'instanceId':st(),'workspaceId':st()},['presetId','instanceId','workspaceId'])
schemas['Application']['properties'].update({'runtimePresets':arr(ref('ApplicationRuntimePreset')),'presetId':st(),'presetRevision':integer,'presetEnabled':bool_})
schemas['UserGrant']['properties']['presetId']=preset_id
schemas['ApplicationConnect']['properties']['presetId']={'anyOf':[{'const':''},preset_id],'description':'选择管理员已登记的运行预设；省略或空值沿用原默认绑定，不接收任意镜像。'}
schemas['ApplicationConnectionResult']['properties']['presetId']=st()
schemas['Source']['properties']['presetId']=st(maxLength=128,description='应用运行预设；与冻结的实例/工作区精确匹配，省略保留旧默认语义。')
schemas['KeyInput']['properties']['presetIds']=dict(type='array',items=preset_id,maxItems=100,uniqueItems=True)
schemas['KeyInput']['properties']['workspaceIds'].update(maxItems=100,uniqueItems=True)
schemas['KeyInput']['required']=['name','scopes']
schemas['KeyInput']['anyOf']=[{'required':['workspaceIds'],'properties':{'workspaceIds':{'minItems':1}}},{'required':['presetIds'],'properties':{'presetIds':{'minItems':1}}}]
schemas['ApplicationKey']['properties']['presetBindings']=arr(ref('ApplicationPresetBinding'))
schemas['ApplicationRuntimeInput']['properties']['presetId']=st(maxLength=128)
schemas['ApplicationRuntime']['properties']['presetId']=st(maxLength=128)
schemas['ApplicationRuntimeDiagnosticsInput']['properties']['presetId']=st(maxLength=128)
schemas['ApplicationRuntimeDiagnostics']['properties']['presetId']=st(maxLength=128)
for method,path in [('get','/applications/{appId}/runtime-presets'),('put','/applications/{appId}/runtime-presets/{presetId}')]:
 op=paths.setdefault(path,{}).setdefault(method,{'operationId':method+'_'+re.sub(r'[^A-Za-z0-9]+','_',path).strip('_'),'parameters':[],'responses':{}})
 for name in re.findall(r'\{([^}]+)\}',path):
  if not any(p.get('name')==name and p.get('in')=='path' for p in op['parameters']):op['parameters'].append({'name':name,'in':'path','required':True,'schema':preset_id if name=='presetId' else st()})
 op['x-administrator-only']=method=='put'
 if method=='get':op['x-application-scopes']=['read']
 op['summary']='读取应用批准的运行预设' if method=='get' else '管理员登记或启停应用运行预设'
 op['description']='预设绑定独立 Codex Docker 实例及工作区；创建后归属不可改。禁用保留历史并阻止新执行，已经接收的执行继续。应用凭据仅访问明确授权的精确预设绑定，不因新增预设自动扩权。镜像配置、凭据和工作区创建属于管理员；此接口不构建或运行模型。'
 response=arr(ref('ApplicationRuntimePreset')) if method=='get' else ref('ApplicationRuntimePreset')
 op['responses']['200']={'description':'成功','content':{'application/json':{'schema':response}}}
 op['responses']['default']={'description':'归属、修订或权限错误','content':{'application/json':{'schema':ref('Error')}}}
 if method=='put':op['requestBody']={'required':True,'content':{'application/json':{'schema':ref('ApplicationRuntimePresetInput')}}}
paths['/applications/{appId}/runtime']['get']['parameters'].append({'name':'presetId','in':'query','schema':st(maxLength=128)})
for path,method in [('/applications/{appId}/runtime','get'),('/applications/{appId}/runtime/prepare','post'),('/applications/{appId}/runtime/diagnostics','post')]:
 paths[path][method]['description']+=' 可选 presetId 选择已批准且获授权的精确绑定；省略保持原默认绑定。禁用预设仍可读取历史，不能开始新的执行。'
op=paths['/applications/{appId}/connect']['post']
op['x-administrator-only']=False
op['x-application-scopes']=['read']
op['description']='默认连接与初始 Skills/MCP 种子配置由管理员执行。presetId 非空时选择已登记的精确绑定；应用凭据只能只读确认已由管理员初始化的预设连接，不应用 defaults。重复连接不覆盖管理员配置或切换原默认绑定。'



schemas['ApplicationRuntimeSkills']=obj({'appId':st(),'presetId':st(),'instanceId':st(),'workspaceId':st(),'skills':arr(obj({'name':st(),'path':st(),'enabled':bool_},['name','path','enabled']))},['appId','instanceId','workspaceId','skills'])
path='/applications/{appId}/runtime/skills'
op=paths.setdefault(path,{}).setdefault('get',{'operationId':'get_application_runtime_skills','parameters':[{'name':'appId','in':'path','required':True,'schema':st()}],'responses':{}})
op.update({'summary':'读取所选绑定的技能资源引用','x-administrator-only':False,'x-application-scopes':['read'],'description':'仅返回精确应用/预设绑定的实际技能name/path/enabled，不返回模型、MCP原始配置或凭据。复用已准备的连接，不因只读请求启动环境；未准备时明确报错。path是Codex技能调用必需的资源引用。'})
op['parameters'].append({'name':'presetId','in':'query','schema':st(maxLength=128)})
op['responses']['200']={'description':'成功','content':{'application/json':{'schema':ref('ApplicationRuntimeSkills')}}}
op['responses']['default']={'description':'归属、权限或环境未准备错误','content':{'application/json':{'schema':ref('Error')}}}

spec={'openapi':'3.1.0','info':{'title':'RunDesk Control API (RDCA)','version':re.search(r'const Version = "([^"]+)"',(root/'internal/app/api_v1.go').read_text()).group(1),'description':'RunDesk Control API（RDCA），RunDesk 自有的应用与管理契约；不依赖 HarnessRouter，当前未实现或声明 UHP 兼容。/api/v1 为稳定入口；新增可选字段不改变既有语义。应用凭据绑定应用、实例、项目和 scopes；x-administrator-only 标识管理接口，x-application-scopes 标识应用所需权限。任务接收、执行终态和业务评测分开；事件保留后端原生载荷。独立 Kun 0.17.0 / worker 协议 16；Kun 与 Codex 的内部调试能力不同。当前应用接入、错误、幂等及示例见 docs/API-V1.md。'},'servers':[{'url':'/api/v1'}],'security':[{'BearerAuth':[]},{'BrowserCookie':[]}],'paths':paths,'components':{'securitySchemes':{'BearerAuth':{'type':'http','scheme':'bearer'},'BrowserCookie':{'type':'apiKey','in':'cookie','name':'rundesk'}},'schemas':schemas}}
(root/'internal/app/openapi.json').write_text(json.dumps(spec,ensure_ascii=False,indent=2)+'\n')
print(f'{len(paths)} paths, {sum(len(v) for v in paths.values())} operations')
