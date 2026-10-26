/* Real task form and queue JavaScript; authenticated API fixtures, no model/Docker. */
'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const {JSDOM}=require('jsdom'),{webcrypto}=require('node:crypto'),{TextEncoder}=require('node:util');
const root=path.resolve(__dirname,'..'),clone=x=>JSON.parse(JSON.stringify(x));
const dom=new JSDOM(fs.readFileSync(path.join(root,'index.html'),'utf8'),{url:'http://localhost/',runScripts:'outside-only'}),w=dom.window,ctx=dom.getInternalVMContext();
Object.defineProperty(w,'crypto',{value:webcrypto});w.TextEncoder=TextEncoder;w.setInterval=()=>0;
w.rdText=s=>s;w.rdFormat=(s,...args)=>s.replace(/\$\{(\d+)\}/g,(_,i)=>args[+i]);
w.HTMLDialogElement.prototype.showModal=function(){this.open=true};w.HTMLDialogElement.prototype.close=function(){this.open=false;this.dispatchEvent(new w.Event('close'))};
const app=fs.readFileSync(path.join(root,'app.js'),'utf8');vm.runInContext(app.slice(0,app.indexOf('async function boot()')),ctx);
const state=vm.runInContext('state',ctx),$=s=>w.document.querySelector(s);
const db={instances:['default','base','preset','disabled','free','kun','foreign'].map(id=>({id,name:'Instance '+id,agentRuntime:{kind:id==='kun'?'kun':'codex'},execution:{mode:id==='default'||id==='kun'?'local':'docker'}})),workspaces:['base-w','preset-w','disabled-w','free-w','foreign-w','loose-w'].map(id=>({id,name:id})),applications:[{appId:'video',name:'Video',instanceId:'base',workspaceId:'base-w',runtimePresets:[{id:'video-use',instanceId:'preset',workspaceId:'preset-w',name:'Video Use',enabled:true},{id:'old',instanceId:'disabled',workspaceId:'disabled-w',name:'Old',enabled:false}]}],environments:[{instanceId:'base',workspaceId:'base-w'},{instanceId:'free',workspaceId:'free-w'},{instanceId:'foreign',workspaceId:'foreign-w'}],sessions:[]};
Object.assign(state,clone(db));
for(const [id,values] of [['instance',db.instances],['workspace',db.workspaces]])$('#'+id).replaceChildren(...values.map(v=>{const o=w.document.createElement('option');o.value=v.id;o.textContent=v.name;return o}));
$('#instance').value='default';$('#workspace').value='loose-w';
w.resetConversation=()=>{state.session=null;state.selection++};w.setProductPage=v=>{state.view=v};w.selectSession=async()=>{};
const requests=[];
w.RunDeskDiagnostics={recent:()=>null,request:async(url,options)=>{
 const route=url.replace('/api/v1',''),method=options.method||'GET',body=options.body?JSON.parse(options.body):undefined;requests.push({route,method,body});
 if(method==='GET'){
  if(route==='/queue')return {maxConcurrent:2,paused:false,perInstance:{free:3,retired:4}};
  if(route.startsWith('/tasks?'))return {items:[{id:'task-free',created:'2026-10-26T00:00:00Z',status:'completed',sessionId:'session-free',spec:{instanceId:'free',workspaceId:'free-w',title:'Independent task',input:{text:'Research'}}}],nextCursor:''};
  if(route==='/schedules')return [];
  const key=route.slice(1);if(key in db)return clone(db[key]);
 }
 if(method==='POST'&&route==='/tasks')return {id:'new-task'};
 if(method==='PUT'&&route==='/queue')return body;
 throw Error('unexpected API '+method+' '+route);
}};
vm.runInContext(fs.readFileSync(path.join(root,'tasks.js'),'utf8'),ctx);
const holder=w.document.createElement('div');w.document.body.append(holder);
function fields(spec){const f=w.RunDeskTasks.taskFields(spec);holder.replaceChildren(...f.fields);return f;}
const change=(id,value)=>{$('#'+id).value=value;$('#'+id).onchange()};
const normalize=value=>JSON.parse(JSON.stringify(value));
(async()=>{
 let f=fields();assert.equal($('#task-instance').options.length,7);
 change('task-instance','free');let value=f.value();assert.equal(value.instanceId,'free');assert.equal(value.workspaceId,'free-w');assert.deepEqual(normalize(value.source),{kind:'human'});
 assert(![...$('#task-workspace').options].some(o=>['preset-w','disabled-w','foreign-w'].includes(o.value)),'reserved workspace offered to independent Docker instance');
 change('task-instance','kun');assert.equal(f.value().instanceId,'kun');assert.deepEqual(normalize(f.value().source),{kind:'human'});
 change('task-instance','preset');value=f.value();assert.deepEqual(normalize(value.source),{kind:'application',appId:'video',presetId:'video-use'});assert.equal(value.workspaceId,'preset-w');assert($('#task-workspace').disabled);
 change('task-app','');assert.equal(f.value().source.presetId,'video-use','preset identity was cleared by choosing manual source');
 change('task-instance','free');assert.equal($('#task-app').value,'');assert.equal($('#task-preset').value,'');assert(!$('#task-workspace').disabled);assert.equal(f.value().instanceId,'free');
 change('task-app','video');assert.equal(f.value().instanceId,'base');change('task-preset','video-use');assert.equal(f.value().instanceId,'preset');
 change('task-instance','disabled');assert.throws(()=>f.value(),/已停用/,'disabled preset accepted a new task');
 f=fields({source:{kind:'application',appId:'video',presetId:'old'},instanceId:'disabled',workspaceId:'disabled-w'});assert.equal(f.value().instanceId,'disabled','existing disabled schedule lost historical identity');
 f=fields({source:{appId:'video',presetId:'missing'},instanceId:'missing-i',workspaceId:'missing-w'});assert.throws(()=>f.value(),/历史预设/);
 f=fields({instanceId:'missing-i',workspaceId:'free-w'});assert.throws(()=>f.value(),/实例已不可用/);
 f=fields({instanceId:'free',workspaceId:'foreign-w'});assert.throws(()=>f.value(),/工作区不可用/);
 holder.remove();
 await w.RunDeskTasks.open();assert($('#task-list').textContent.includes('Instance free'),'independent task was mislabeled as general assistant');
 $('#new-task').click();change('task-instance','free');$('#task-title').value='New independent task';$('#task-text').value='Build report';
 await $('#task-title').closest('form').onsubmit({preventDefault(){}});
 const submitted=requests.find(r=>r.method==='POST'&&r.route==='/tasks').body;assert.equal(submitted.instanceId,'free');assert.equal(submitted.workspaceId,'free-w');assert.deepEqual(submitted.source,{kind:'human'});
 $('#queue-settings').click();const form=$('#queue-max').closest('form');assert(form.textContent.includes('Instance free'));assert(form.textContent.includes('Instance kun'));await form.onsubmit({preventDefault(){}});
 const queue=requests.find(r=>r.method==='PUT'&&r.route==='/queue').body;assert.equal(queue.perInstance.free,3);assert.equal(queue.perInstance.retired,4,'unlisted historical limit was erased');
 assert(!requests.some(r=>r.route.includes('/actions')||r.method==='POST'&&r.route==='/environments'),'configuration form started or created an environment');
 console.log(JSON.stringify({passed:true,checks:['all instances in task creation and concurrency','independent Codex and Kun human tasks','preset identity, disabled and missing history','reserved workspace rejection','actual task POST retains chosen target','queue labels and unrelated limits'],scope:'shipped JavaScript in JSDOM with API fixtures; no browser, model or Docker'}));
 dom.window.close();
})().catch(e=>{console.error(e);dom.window.close();process.exitCode=1});
