/* Run with NODE_PATH pointing at a JSDOM installation. No browser or model calls. */
'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const {JSDOM}=require('jsdom'),{webcrypto}=require('node:crypto'),{TextEncoder}=require('node:util');
const root=path.resolve(__dirname,'..'),clone=x=>JSON.parse(JSON.stringify(x));
const dom=new JSDOM(fs.readFileSync(path.join(root,'index.html'),'utf8'),{url:'http://localhost/',runScripts:'outside-only'}),w=dom.window,ctx=dom.getInternalVMContext();
Object.defineProperty(w,'crypto',{value:webcrypto});w.TextEncoder=TextEncoder;w.setInterval=()=>0;
w.HTMLDialogElement.prototype.showModal=function(){this.open=true};w.HTMLDialogElement.prototype.close=function(){this.open=false;this.dispatchEvent(new w.Event('close'))};
w.rdText=s=>s;w.rdFormat=(s,...args)=>s.replace(/\$\{(\d+)\}/g,(_,i)=>args[+i]);w.confirm=()=>true;
const appCode=fs.readFileSync(path.join(root,'app.js'),'utf8');vm.runInContext(appCode.slice(0,appCode.indexOf('async function boot()')),ctx);
const state=vm.runInContext('state',ctx),$=s=>w.document.querySelector(s),$$=s=>[...w.document.querySelectorAll(s)];
const presets=[{id:'video-use',appId:'video',name:'Video Use',description:'剪辑',instanceId:'preset-i',workspaceId:'preset-w',enabled:true,revision:1},{id:'disabled',appId:'video',name:'旧环境',description:'保留历史',instanceId:'old-i',workspaceId:'old-w',enabled:false,revision:1}];
const db={applications:[{appId:'video',name:'Video',instanceId:'base-i',workspaceId:'base-w',sessionCount:3,activeCount:0,runtimePresets:presets},{appId:'other',name:'Other',instanceId:'other-i',workspaceId:'other-w',runtimePresets:[{id:'owned',instanceId:'foreign-i',workspaceId:'foreign-w',enabled:true,name:'Foreign'}]}],instances:['default','base-i','preset-i','old-i','other-i','foreign-i','free-i','kun-i'].map(id=>({id,name:id,revision:1,agentRuntime:{kind:id==='kun-i'?'kun':'codex'},execution:{mode:id==='default'?'local':'docker',image:'video:stable'}})),workspaces:['base-w','preset-w','old-w','other-w','foreign-w','loose-w'].map(id=>({id,name:id,path:'/fixture/'+id})),sessions:[{id:'base-s',instanceId:'base-i',workspaceId:'base-w',title:'默认任务',status:'completed'},{id:'preset-s',instanceId:'preset-i',workspaceId:'preset-w',title:'预设任务',status:'completed'},{id:'old-s',instanceId:'old-i',workspaceId:'old-w',title:'历史任务',status:'completed'}],keys:[],users:[{id:"member",name:"Member",enabled:true,revision:1,grants:[{appId:"video",workspaceId:"base-w",role:"viewer"},{appId:"video",presetId:"video-use",workspaceId:"preset-w",role:"runner"},{appId:"video",presetId:"disabled",workspaceId:"old-w",role:"viewer"}]}]};
const requests=[],receipts=new Map();let workspaceLost=true,presetLost=true,instanceWrites=0,workspaceWrites=0;
function fail(message,status,code){return Object.assign(new Error(message),{status,code})}
w.RunDeskDiagnostics={recent:()=>null,request:async(url,options)=>{
 const route=url.replace('/api/v1',''),method=options.method||'GET',body=options.body?JSON.parse(options.body):undefined;
 requests.push({route,method,body,headers:options.headers});
 if(method==='GET'){
  if(route==='/applications')return clone(db.applications);
  if(route==='/instances')return clone(db.instances);
  if(route==='/workspaces')return clone(db.workspaces);
  if(route==='/sessions')return clone(db.sessions);
  if(route==='/applications/video/runtime-presets')return clone(presets);
  if(route==='/applications/video/project-environments')return [{appId:'video',projectId:'aaaaaaaaaaaaaaaaaaaaaaaa',presetId:'video-use',name:'新闻视频',instanceId:'preset-i',workspaceId:'project-w',created:'2026-10-28T00:00:00Z'}];
  if(route==='/applications/video/keys')return clone(db.keys);
  if(route==='/users')return clone(db.users);
  if(route==='/environments')return [{instanceId:'base-i',workspaceId:'base-w'},{instanceId:'preset-i',workspaceId:'preset-w'},{instanceId:'old-i',workspaceId:'old-w'},{instanceId:'base-i',workspaceId:'preset-w'},{instanceId:'preset-i',workspaceId:'other-w'}];
  if(route.startsWith('/environments?'))return [];
  throw Error('unexpected GET '+route);
 }
 const key=options.headers['Idempotency-Key'];
 if(method==='POST'&&route==='/instances'){
  assert(key,'instance creation requires receipt key');if(receipts.has(key))return clone(receipts.get(key));
  const value={id:'created-i'+(++instanceWrites),name:body.name,revision:1,agentRuntime:{kind:'codex'},execution:{mode:'local'}};db.instances.push(value);receipts.set(key,clone(value));return clone(value);
 }
 if(method==='PUT'&&/^\/instances\/[^/]+\/execution$/.test(route)){
  const id=route.split('/')[2],value=db.instances.find(i=>i.id===id);assert.equal(body.revision,value.revision);value.revision++;value.execution=body.spec;return clone(value);
 }
 if(method==='POST'&&route==='/workspaces'){
  assert(key,'workspace creation requires receipt key');if(receipts.has(key))return clone(receipts.get(key));
  const value={id:'created-w'+(++workspaceWrites),name:body.name,path:'/fixture/new'};db.workspaces.push(value);receipts.set(key,clone(value));
  if(workspaceLost){workspaceLost=false;throw fail('workspace response lost')};return clone(value);
 }
 if(method==='PUT'&&route.startsWith('/applications/video/runtime-presets/')){
  const id=decodeURIComponent(route.split('/').at(-1)),existing=presets.find(p=>p.id===id);
  if(existing){assert.equal(body.instanceId,existing.instanceId);assert.equal(body.workspaceId,existing.workspaceId);assert.equal(body.revision,existing.revision);Object.assign(existing,body,{revision:existing.revision+1});return clone(existing)}
  const value={id,appId:'video',...body,revision:1};presets.push(value);
  if(presetLost){presetLost=false;throw fail('preset response lost')};return clone(value);
 }
 if(method==='PUT'&&route==='/users/member'){const member=db.users.find(u=>u.id==='member');assert.equal(body.revision,member.revision);Object.assign(member,body,{revision:member.revision+1});return clone(member);}
 if(method==='POST'&&route==='/users'){const user={id:'new-member',...body,revision:1};db.users.push(user);return {user:clone(user),accessCode:'test-only-access-code'};}
 if(method==='POST'&&route==='/applications/video/keys'){
  assert(!body.workspaceIds.some(id=>presets.some(p=>p.workspaceId===id)),'preset workspace leaked into legacy grants');
  const value={id:'key-1',name:body.name,scopes:body.scopes,workspaceIds:body.workspaceIds,presetBindings:body.presetIds.map(id=>{const p=presets.find(p=>p.id===id);return {presetId:id,instanceId:p.instanceId,workspaceId:p.workspaceId}})};db.keys.push(value);return {key:value,token:'test-only-token'};
 }
 throw Error('unexpected mutation '+method+' '+route);
}};
w.renderInstanceOptions=id=>{$('#instance').replaceChildren(...state.instances.map(i=>w.document.createElement('option')).map((o,n)=>{o.value=state.instances[n].id;o.textContent=state.instances[n].name;return o}));$('#instance').value=id};
w.resetConversation=()=>{state.session=null;state.selection++};w.updateWorkspaceLabel=()=>{};w.closeConversationSidebar=()=>{};w.openSettings=async tab=>{w.lastSettings={tab,instanceId:$('#instance').value,workspaceId:$('#workspace').value}};w.selectSession=async()=>{};
w.RunDeskImages={overview:async(a,n)=>{n.textContent='default image'},short:s=>s,needsUpdate:()=>false};w.RunDeskSetup={open:async()=>{}};
for(const file of ['applications.js','environments.js','application-keys.js','tasks.js','users.js'])vm.runInContext(fs.readFileSync(path.join(root,file),'utf8'),ctx);
function sync(){Object.assign(state,{applications:clone(db.applications),instances:clone(db.instances),workspaces:clone(db.workspaces),sessions:clone(db.sessions)});$('#workspace').replaceChildren(...state.workspaces.map(v=>{const o=w.document.createElement('option');o.value=v.id;o.textContent=v.name;return o}));$('#workspace').value='base-w';w.renderInstanceOptions('base-i')}
const button=(scope,text)=>[...scope.querySelectorAll('button')].find(b=>b.textContent===text);
const flush=()=>new Promise(resolve=>setImmediate(resolve));async function until(check){for(let i=0;i<100;i++){if(check())return;await flush()}assert(check(),'async UI did not settle')}
(async()=>{
 sync();
 assert.equal(vm.runInContext("applicationForInstance('preset-i').appId",ctx),'video');assert.equal(vm.runInContext("applicationForInstance('old-i').appId",ctx),'video');
 const advanced=w.applicationAdvanced();assert(!advanced.textContent.includes('preset-i'),'preset instance appeared orphaned');assert(!advanced.textContent.includes('old-i'),'disabled preset instance appeared orphaned');
 await w.showApplication('video');assert.equal($$('.application-task').length,3,'default application page omitted preset history');
 assert($('.application-project-host').textContent.includes('新闻视频'),'application project bindings were hidden');
 assert(!$('#runtime-presets').closest('details'),'available Agent environments were hidden in advanced settings');
 assert($('#runtime-presets .runtime-default-card').textContent.includes('base-i'));
 assert(button($('#runtime-presets'),'查看全局 Agent'));
 assert.equal($('[data-preset-id="video-use"] [data-key-count]').dataset.keyCount,'0');
 assert(!$('[data-preset-id="disabled"]').textContent.includes('null'),'disabled environment rendered a literal null');
 await w.showApplication('video','disabled');assert.equal($('#instance').value,'old-i');assert.equal($('#workspace').value,'old-w');assert.equal($$('.application-task').length,1);assert($('.application-tasks').textContent.includes('历史任务'));
 assert(w.location.hash.includes('/preset/disabled'),'preset route was lost');assert.equal(vm.runInContext('currentApplication().appId',ctx),'video');
 const enabled=$('[data-preset-id="video-use"]');button(enabled,'Skills').click();await until(()=>w.lastSettings?.tab==='skills');assert.deepEqual(w.lastSettings,{tab:'skills',instanceId:'preset-i',workspaceId:'preset-w'});
 await w.RunDeskEnvironments.open(w.RunDeskRuntimePresets.binding(state.applications[0],state.applications[0].runtimePresets[0]));
 assert.equal($('#environment-project').options.length,1,'preset environment offered another workspace');assert.equal($('#environment-project').value,'preset-w');assert($('#execution-mode').disabled,'preset offered local runtime');assert(!requests.some(r=>r.route.includes('/actions')),'opening preset changed container state');
 // Bind an already configured unoccupied Codex Docker identity without writes to it.
 let bound=await w.RunDeskRuntimePresets.edit(state.applications[0],null,async()=>{});
 assert.deepEqual([...$('#preset-instance').options].map(o=>o.value),['free-i'],'assigned/default/Kun instance offered for binding');
 $('#preset-id').value='existing-docker';$('#preset-name').value='Existing Docker';$('#preset-mode').value='existing';$('#preset-mode').onchange();$('#preset-instance').value='free-i';$('#preset-workspace').value='loose-w';
 await bound.querySelector('form').onsubmit({preventDefault(){}});assert(!bound.open);assert.equal(instanceWrites,0);assert.equal(workspaceWrites,0);assert(!requests.some(r=>r.method==='PUT'&&r.route.includes('/execution')),'binding existing preset changed runtime');presetLost=true;
 // Lost responses and reopening the dialog retain creation identities and keys.
 let dialog=await w.RunDeskRuntimePresets.edit(state.applications[0],null,async()=>{});
 $('#preset-id').value='hyperframes';$('#preset-name').value='HyperFrames';$('#preset-image').value='video:hyperframes';
 await dialog.querySelector('form').onsubmit({preventDefault(){}});
 assert(dialog.open);assert(dialog.querySelector('.error').textContent.includes('response lost'));assert.equal(instanceWrites,1);assert.equal(workspaceWrites,1);assert($('#preset-name').disabled,'uncertain retry permitted changing creation body');
 dialog.close();dialog=await w.RunDeskRuntimePresets.edit(state.applications[0],null,async()=>{});assert.equal($('#preset-id').value,'hyperframes');assert($('#preset-id').disabled);
 await dialog.querySelector('form').onsubmit({preventDefault(){}});
 assert(!dialog.open,'PUT response loss was not reconciled');assert.equal(instanceWrites,1,'retry duplicated instance');assert.equal(workspaceWrites,1,'retry duplicated directory');assert.equal(presets.filter(p=>p.id==='hyperframes').length,1);
 const workspaceCalls=requests.filter(r=>r.route==='/workspaces'&&r.method==='POST');assert.equal(workspaceCalls.length,2);assert.equal(workspaceCalls[0].headers['Idempotency-Key'],workspaceCalls[1].headers['Idempotency-Key']);
 // Editing names/enabled preserves identity and performs no runtime actions.
 const hp=state.applications[0].runtimePresets.find(p=>p.id==='hyperframes');dialog=await w.RunDeskRuntimePresets.edit(state.applications[0],hp,async()=>{});
 $('#preset-name').value='HyperFrames 旧版';$('#preset-enabled').checked=false;await dialog.querySelector('form').onsubmit({preventDefault(){}});
 assert.equal(presets.find(p=>p.id==='hyperframes').enabled,false);assert.equal(instanceWrites,1);assert(!requests.some(r=>r.route.includes('/actions')));
 // Queue forms retain preset identity and lock its dedicated workspace.
 sync();let fields=w.RunDeskTasks.taskFields();const holder=w.document.createElement('div');holder.append(...fields.fields);w.document.body.append(holder);$('#task-app').value='video';$('#task-app').onchange();$('#task-preset').value='video-use';$('#task-preset').onchange();
 const spec=fields.value();assert.equal(spec.instanceId,'preset-i');assert.equal(spec.workspaceId,'preset-w');assert.equal(spec.source.presetId,'video-use');assert($('#task-workspace').disabled);assert(![...$('#task-preset').options].some(o=>o.value==='disabled'));holder.remove();
 fields=w.RunDeskTasks.taskFields({source:{appId:'video',presetId:'disabled'},workspaceId:'old-w',instanceId:'old-i'});holder.replaceChildren(...fields.fields);w.document.body.append(holder);assert.equal(fields.value().instanceId,'old-i','editing disabled preset silently switched to default');holder.remove();
 fields=w.RunDeskTasks.taskFields({source:{appId:'video',presetId:'missing'},workspaceId:'missing-w',instanceId:'missing-i'});holder.replaceChildren(...fields.fields);w.document.body.append(holder);assert.throws(()=>fields.value(),/历史预设/,'unknown historical preset silently switched to default');holder.remove();
 // Issue explicit frozen preset grants; legacy workspace choices stay separate.
 await w.RunDeskKeys.open(state.applications[0]);button($('dialog.key-dialog[open]'), '创建凭据').click();await until(()=>!!$('#key-name'));
 assert(![...$('#key-workspaces').querySelectorAll('input')].some(n=>['preset-w','old-w','foreign-w'].includes(n.value)));
 $('#key-name').value='Video service';assert($('#key-presets [data-preset-id="video-use"]').checked);assert($('#key-presets [data-preset-id="existing-docker"]').checked,'new credentials should preselect enabled Agent environments');
 $('#key-presets [data-preset-id="existing-docker"]').checked=false;
 await $('#key-name').closest('form').onsubmit({preventDefault(){}});
 const keyRequest=requests.find(r=>r.route==='/applications/video/keys'&&r.method==='POST');assert.deepEqual(keyRequest.body.presetIds,['video-use']);assert(!keyRequest.body.workspaceIds.includes('preset-w'));assert.equal(db.keys[0].presetBindings[0].instanceId,'preset-i');assert($('#key-secret'),'credential was not displayed once');
 for(const d of $$('dialog[open]'))d.close();await w.showApplication('video');assert.equal($('[data-preset-id="video-use"] [data-key-count]').dataset.keyCount,'1');
 // Member grants match app + optional preset + workspace, including disabled history.
 await w.RunDeskUsers.open();
 const member=$('[data-user-id="member"]');assert(member.querySelector('.user-grants').textContent.includes('旧环境'));assert(member.querySelector('.user-grants').textContent.includes('已禁用'));
 button(member,'编辑授权').click();await until(()=>!!$('#user-name'));
 const grantChecks=$$('.user-project-picker input[type="checkbox"]');assert.equal(grantChecks.length,3,'mismatched environments created extra grant choices');assert(grantChecks.every(c=>c.checked),'existing preset grants were lost during edit');
 assert.equal(grantChecks.find(c=>c.dataset.workspaceId==='preset-w').dataset.presetId,'video-use');assert.equal(grantChecks.find(c=>c.dataset.workspaceId==='old-w').dataset.presetId,'disabled');assert.equal(grantChecks.find(c=>c.dataset.workspaceId==='base-w').dataset.presetId,'');
 $('#user-name').value='Member renamed';await $('#user-name').closest('form').onsubmit({preventDefault(){}});
 const savedGrants=requests.filter(r=>r.route==='/users/member'&&r.method==='PUT').at(-1).body.grants;
 assert.deepEqual(savedGrants,[{appId:'video',workspaceId:'base-w',role:'viewer'},{appId:'video',presetId:'video-use',workspaceId:'preset-w',role:'runner'},{appId:'video',presetId:'disabled',workspaceId:'old-w',role:'viewer'}]);
 button($('#applications-page'),'创建用户').click();await until(()=>!!$('#user-name'));$('#user-name').value='New member';
 const newChecks=$$('.user-project-picker input[type="checkbox"]');for(const check of newChecks)if(check.dataset.workspaceId==='base-w'||check.dataset.presetId==='video-use'){check.checked=true;check.onchange();}
 await $('#user-name').closest('form').onsubmit({preventDefault(){}});
 const createdGrants=requests.find(r=>r.route==='/users'&&r.method==='POST').body.grants;
 assert.deepEqual(createdGrants,[{appId:'video',workspaceId:'base-w',role:'viewer'},{appId:'video',presetId:'video-use',workspaceId:'preset-w',role:'viewer'}]);assert($('#user-access-code'),'new user access code was not displayed');
 assert(!requests.some(r=>r.route==='/interrupt'||r.route.includes('/actions')),'preset management interrupted an environment');
 console.log(JSON.stringify({passed:true,checks:['preset navigation and disabled history','environment scoped to fixed binding','lost POST and PUT responses reuse identities across dialog reopen','rename/disable does not mutate runtime','task source.presetId and exact workspace','explicit key presetIds and frozen bindings','member grants preserve exact preset identity in POST and PUT, including disabled history'],scope:'real shipped JavaScript, JSDOM and mocked authenticated API; browser layout and Docker not exercised'}));
 dom.window.close();
})().catch(error=>{console.error(error);dom.window.close();process.exitCode=1});
