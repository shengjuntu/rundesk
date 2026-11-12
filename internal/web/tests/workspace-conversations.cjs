/* Real conversation navigation with a fixture API; no model or container calls. */
'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const {JSDOM}=require('jsdom'),{webcrypto}=require('node:crypto'),{TextEncoder}=require('node:util');
const root=path.resolve(__dirname,'..'),copy=x=>JSON.parse(JSON.stringify(x));
const dom=new JSDOM(fs.readFileSync(path.join(root,'index.html'),'utf8'),{url:'http://localhost/',runScripts:'outside-only'}),w=dom.window,ctx=dom.getInternalVMContext(),$=s=>w.document.querySelector(s),$$=s=>[...w.document.querySelectorAll(s)];
Object.defineProperty(w,'crypto',{value:webcrypto});w.TextEncoder=TextEncoder;w.setInterval=()=>0;w.confirm=()=>true;
w.rdText=s=>s;w.rdFormat=(s,...v)=>s.replace(/\$\{(\d+)\}/g,(_,n)=>v[+n]);
w.HTMLDialogElement.prototype.showModal=function(){this.open=true};w.HTMLDialogElement.prototype.close=function(){this.open=false;this.dispatchEvent(new w.Event('close'))};
w.EventSource=class{close(){}};
const load=file=>vm.runInContext(fs.readFileSync(path.join(root,file),'utf8').replace('document.addEventListener("DOMContentLoaded", () => safe(boot));',''),ctx,{filename:file});
load('app.js');load('applications.js');load('manage.js');load('instances.js');load('workspace-conversations.js');load('recovery.js');
const state=vm.runInContext('state',ctx);
for(const name of ['resetReplyActions','renderRuntimeBar','closeConversationSidebar','renderCapabilityStrip','renderAttachments','renderDebug'])w[name]=()=>{};
w.loadReplyFeedback=async()=>{};w.refreshRuntime=async()=>{};w.loadFiles=async()=>{};w.refreshKunApproval=async()=>{};
const db={instances:['default','kun','docker-a','docker-b','preset','off','app-base'].map(id=>({id,name:id,revision:1,agentRuntime:{kind:id==='kun'?'kun':'codex',model:'kun-model'},execution:{mode:id==='default'||id==='kun'?'local':'docker',image:'fixture:1'}})),workspaces:['project','second','preset-work','off-work','app-work','app-project'].map(id=>({id,name:id,path:'/fixture/'+id})),applications:[{appId:'video',instanceId:'app-base',workspaceId:'app-work',runtimePresets:[{id:'p',instanceId:'preset',workspaceId:'preset-work',enabled:true},{id:'off',instanceId:'off',workspaceId:'off-work',enabled:false}]}],environments:[{instanceId:'docker-a',workspaceId:'project'}],sessions:[{id:'c1',title:'脚本',instanceId:'default',workspaceId:'project',runtimeKind:'codex',status:'idle',updated:'2026-10-30T00:00:00Z'},{id:'k1',title:'素材',instanceId:'kun',workspaceId:'project',runtimeKind:'kun',status:'idle',updated:'2026-10-31T00:00:00Z'},{id:'other',title:'另一个项目',instanceId:'default',workspaceId:'second',runtimeKind:'codex',status:'idle'},{id:'archived',title:'归档记录',instanceId:'kun',workspaceId:'project',runtimeKind:'kun',archived:true,status:'completed'}]};
const projects=[{appId:'video',instanceId:'preset',workspaceId:'app-project',presetId:'p',projectId:'aaaaaaaaaaaaaaaaaaaaaaaa'}],requests=[];let hold;
w.RunDeskDiagnostics={recent:()=>null,request:async(url,options={})=>{const route=url.replace('/api/v1',''),method=options.method||'GET',body=options.body?JSON.parse(options.body):null;requests.push({route,method,body});
 if(method==='GET'){
  if(route==='/whoami')return {kind:'administrator'};
  if(route==='/meta')return {demo:true,version:'fixture'};
  if(route==='/instances'&&hold)return hold.promise;
  if(route.slice(1) in db)return copy(db[route.slice(1)]);
  if(route==='/applications/video/project-environments')return copy(projects);
  if(route.endsWith('/approvals'))return [];
  if(route.startsWith('/sessions/'))return copy(db.sessions.find(s=>s.id===route.split('/')[2]));
 }
 if(method==='POST'&&route==='/sessions'){const s={...body,id:'new-'+db.sessions.length,title:'新会话',status:'idle',runtimeKind:db.instances.find(i=>i.id===body.instanceId).agentRuntime.kind};db.sessions.push(s);return copy(s)}
 if(method==='POST'&&route==='/workspaces'){const created={id:'new-work',...body};db.workspaces.push(created);return copy(created)}
 throw Error(method+' '+route);
}};
const c=w.RunDeskConversations;
const tick=()=>new Promise(resolve=>setImmediate(resolve));async function until(fn){for(let n=0;n<100;n++){if(fn())return;await tick()}assert(fn(),'UI did not settle')}
(async()=>{
 await c.open('project');
 assert.equal(state.session.id,'c1');assert.equal($('#workspace').value,'project');assert.equal($('#instance').value,'default');
 assert.equal($$('[data-workspace-id="project"] .session').length,1,'maintenance history leaked into daily conversations');
 assert.equal($$('[data-workspace-id="second"] .session').length,1);assert($('[data-session-id="c1"]').textContent.includes('Codex'));assert(!$('[data-session-id="k1"]'));assert(!c.available('project').some(i=>i.id==='kun'));
 assert.equal($$('#new-session').length,1);assert(!$('#composer-workspace'));assert($('#instance').closest('[hidden]'));
 const before=requests.filter(r=>r.method!=='GET').length;
 assert.equal($('#session-engine').tagName,'SPAN');assert.equal($('#nav-instances'),null);assert.equal(requests.filter(r=>r.method!=='GET').length,before);
 w.localStorage.setItem('rundesk-workspace-engine:project','docker-b');
 $('#new-session').click();await until(()=>!state.session&&!state.loadingSession);assert.equal($('#instance').value,'default');assert(!$('#new-session').disabled);assert($('#recovery-banner').classList.contains('hidden'),'new draft displayed a false pending recovery');
 $('#prompt').value='保留我的草稿';state.uploads=[{id:'private-upload'}];state.chosenSkills=[{name:'private-skill'}];
 await c.open('project',{fresh:true,instanceId:'docker-b'});assert.equal($('#instance').value,'docker-b');assert.equal(state.uploads.length,0);assert.equal(state.chosenSkills.length,0);
 await w.newSession();const created=requests.filter(r=>r.route==='/sessions'&&r.method==='POST').at(-1);assert.equal(created.body.workspaceId,'project');assert.equal(created.body.instanceId,'docker-b');
 await c.open('project',{fresh:true});assert.equal($('#instance').value,'default');assert.equal(state.uploads[0].id,'private-upload');assert.equal($('#prompt').value,'保留我的草稿');
 await c.open('second',{fresh:true});assert.equal($('#instance').value,'default');await assert.rejects(()=>c.open('second',{fresh:true,instanceId:'kun'}));
 $('#session-search').value='脚本';w.renderSessions();assert.equal($$('.workspace-group').length,1);assert.equal($$('.workspace-group .session').length,1);
 $('#session-search').value='';$('#session-scope').value='archived';w.renderSessions();assert.equal($$('.workspace-group .session').length,0);assert(!$('[data-session-id="archived"]'));
 assert.deepEqual(Array.from(c.available('preset-work'),i=>i.id),['preset']);assert.deepEqual(Array.from(c.available('app-project'),i=>i.id),['preset']);assert.equal(c.available('off-work').length,0);assert.deepEqual(copy(c.source('preset','app-project')),{kind:'application',appId:'video',projectId:'aaaaaaaaaaaaaaaaaaaaaaaa',presetId:'p'});assert.throws(()=>c.source('default','preset-work'));
 await c.open('app-project',{fresh:true});await w.newSession();assert.equal(requests.filter(r=>r.route==='/sessions'&&r.method==='POST').at(-1).body.source.projectId,projects[0].projectId);
 w.history.replaceState(null,'','#workspace/project');await w.restoreProductRoute();assert.equal($('#workspace').value,'project');assert.equal($('#instance').value,'docker-b');
 c.createWorkspace();$('#conversation-workspace-name').value='新工作区';await $('#conversation-workspace-name').closest('form').onsubmit({preventDefault(){}});assert.equal($('#workspace').value,'new-work');assert.equal($('#instance').value,'default');
 let resolve;hold={promise:new Promise(r=>resolve=r)};const opening=c.open('project');await tick();w.resetConversation();w.setProductPage('tasks');resolve(copy(db.instances));await opening;hold=null;assert.equal(state.view,'tasks','stale response stole navigation');
 // A first daily session works with a blank Agent control; maintenance
 // preferences from older setup cannot turn it into a Kun session.
 db.workspaces.unshift({id:'kun-work',name:'Kun maintenance',path:'/fixture/kun',purpose:'maintenance'});
 w.resetConversation();Object.assign(state,copy(db));
 $('#workspace').replaceChildren(...db.workspaces.map(v=>new w.Option(v.name,v.id)));$('#workspace').value='project';
 $('#instance').value='';w.localStorage.setItem('rundesk-workspace-engine:project','kun');
 state.uploads=[];state.chosenSkills=[];await w.newSession();
 let submitted=requests.filter(r=>r.route==='/sessions'&&r.method==='POST').at(-1).body;
 assert.equal(submitted.instanceId,'default');assert.equal(submitted.workspaceId,'project');
 w.resetConversation();$('#workspace').value='kun-work';$('#instance').value='kun';
 w.localStorage.setItem('rundesk-conversation-workspace','kun-work');w.localStorage.setItem('rundesk-assistant-workspace','kun-work');
 $('#new-session').click();await until(()=>!state.loadingSession&&$('#workspace').value!=='kun-work');
 assert.equal($('#workspace').value,'project');assert.equal($('#instance').value,'default');
 await assert.rejects(()=>c.open('kun-work',{fresh:true}),/工作区不存在/);
 // Missing Agent recovery must not bind foreign attachments to another engine.
 await c.open('project',{fresh:true});$('#instance').value='';state.draftContext='kun:project:new';state.uploads=[{id:'private-maintenance-upload'}];
 const posts=requests.filter(r=>r.route==='/sessions'&&r.method==='POST').length;
 await assert.rejects(()=>w.newSession(),/不能使用所选引擎/);
 assert.equal(requests.filter(r=>r.route==='/sessions'&&r.method==='POST').length,posts);state.uploads=[];
 // Existing maintenance history remains on its own immutable identity.
 db.sessions.find(value=>value.id==='k1').workspaceId='kun-work';
 await w.selectSession('k1');const maintenanceSession=state.session;
 c.ensureDraftContext();assert.equal(state.session,maintenanceSession);assert.equal($('#workspace').value,'kun-work');assert.equal($('#instance').value,'kun');
 // Exercise the actual boot function, not just a selector helper. With no
 // sessions and stale maintenance-only preferences, plain startup is ready to
 // type directly into the default Codex conversation.
 db.sessions.length=0;w.resetConversation();w.history.replaceState(null,'','/');
 w.localStorage.setItem('rundesk-instance','kun');w.localStorage.setItem('rundesk-conversation-workspace','kun-work');w.localStorage.setItem('rundesk-assistant-workspace','kun-work');
 w.RunDeskSetup={maybeOpen:async()=>{}};w.RunDeskOnboarding={preferredInstance:'kun'};w.RunDeskLanguage={restoreDraft:async()=>false};
 await w.boot();assert.equal(state.view,'conversation');assert.equal(state.session,null);assert.equal($('#workspace').value,'project');assert.equal($('#instance').value,'default');assert(!$('#send').disabled);assert(!$('#new-session').disabled);
 $('#prompt').value='Ready after initialization';await w.newSession();submitted=requests.filter(r=>r.route==='/sessions'&&r.method==='POST').at(-1).body;
 assert.equal(submitted.instanceId,'default');assert.equal(submitted.workspaceId,'project');
 // No Codex installation identity is a visible unavailable state, never an
 // implicit permission to start a daily conversation through Kun.
 const allInstances=db.instances;db.instances=db.instances.filter(value=>value.agentRuntime.kind==='kun');
 await c.open('project',{fresh:true});assert.equal($('#instance').value,'');assert($('#session-engine').textContent.includes('尚无可用 Codex 环境'));assert($('#send').disabled);assert($('#new-session').disabled);
 const unavailablePosts=requests.filter(r=>r.route==='/sessions'&&r.method==='POST').length;
 await assert.rejects(()=>w.newSession(),/尚无可用 Codex 环境/);assert.equal(requests.filter(r=>r.route==='/sessions'&&r.method==='POST').length,unavailablePosts);
 db.instances=allInstances;
 console.log(JSON.stringify({passed:true,checks:['missing Codex stays unavailable and explicit maintenance history keeps its identity','actual boot recovers stale maintenance preferences into a ready default Codex draft','blank Agent recovery preserves scope and rejects foreign attachments','two-level workspace tree with Kun maintenance excluded','existing session identity locked','new daily sessions always default to local Codex','draft text and attachments stay with original scope','explicit compatibility route preserves Docker identity','search and archived history across workspaces','application preset/project ownership and source preserved','workspace deep links and managed workspace creation','stale response cannot steal navigation']}));
 dom.window.close();
})().catch(e=>{console.error(e);dom.window.close();process.exitCode=1});
