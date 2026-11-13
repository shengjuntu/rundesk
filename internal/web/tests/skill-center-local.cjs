/* Local acquisition uses the shipped transport and never installs selected files. */
'use strict';
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path'), vm = require('node:vm');
const {JSDOM} = require('jsdom');
const root = path.resolve(__dirname, '..'), copy = value => JSON.parse(JSON.stringify(value));
const malicious = '<img src=x onerror="window.localSkillSourceExecuted=true"><script>window.localSkillSourceExecuted=true</script>';
const report = {engine:'builtin',engineVersion:'fixture-1',digest:'sha256:frozen-local-content',status:'incomplete',recommendation:'incomplete',findings:[],coverage:{filesSeen:3,filesScanned:2,filesSkipped:1,skipped:[{file:'assets/opaque.bin',reason:'binary content'}]},warnings:['Incomplete coverage is not a safety pass']};
const pack = {id:'local-pack',name:'local-reader',sourceUrl:'local-upload://local-reader',revision:report.digest,digest:report.digest,createdAt:'2026-11-12T12:00:00Z',files:[{path:'SKILL.md',size:80},{path:'references/guide.md',size:30},{path:'assets/opaque.bin',size:4}],scan:report,reports:{builtin:report}};
const diagnostic = {id:'nvidia-diagnostic',target:'skillspector',overall:'ready',checkedAt:'2026-11-12T12:00:00Z',checks:[{id:'version',label:'CLI version',status:'ok',message:malicious,version:'2.13.4',durationMs:16}]};
const dockerEnvironment = {id:'docker-fixture',name:'Docker test environment',managed:true,execution:{mode:'docker',image:'fixture/image'}};
const flush = () => new Promise(resolve => setImmediate(resolve));
async function until(check, message = 'Local Skill UI did not settle') { for (let n=0;n<300;n++) { if (check()) return; await flush(); } assert(check(),message); }
function deferred() { let resolve,reject;const promise=new Promise((yes,no)=>{resolve=yes;reject=no;});return {promise,resolve,reject}; }

function fixture(options = {}) {
  const dom = new JSDOM(fs.readFileSync(path.join(root,'index.html'),'utf8'),{url:'http://localhost/#conversations',runScripts:'outside-only'}),w=dom.window,ctx=dom.getInternalVMContext();
  const $ = selector => w.document.querySelector(selector), $$ = selector => [...w.document.querySelectorAll(selector)], requests=[];
  w.setInterval=()=>0;w.setTimeout=()=>0;w.clearTimeout=()=>{};
  w.HTMLDialogElement.prototype.showModal=function(){this.open=true;};w.HTMLDialogElement.prototype.close=function(){this.open=false;this.dispatchEvent(new w.Event('close'));};
  w.rdText=value=>value;w.rdFormat=(value,...args)=>value.replace(/\$\{(\d+)\}/g,(_,n)=>args[+n]);
  const app=fs.readFileSync(path.join(root,'app.js'),'utf8');vm.runInContext(app.slice(0,app.indexOf('async function boot()')),ctx);
  const state=vm.runInContext('state',ctx);
  Object.assign(state,{identity:{kind:'administrator'},instances:[{id:'default',name:'Codex'},{id:'other-engine',name:'Other environment'},...(options.environments||[])],workspaces:[{id:'workspace',name:'Research'},{id:'other',name:'Other project'}],session:{id:'session-running',status:'running'},sending:true,uploads:[{name:'draft attachment'}]});
  for(const [selector,rows,selected] of [['#instance',state.instances,'default'],['#workspace',state.workspaces,'workspace']]) {
    $(selector).replaceChildren(...rows.map(row=>{const n=w.document.createElement('option');n.value=row.id;n.textContent=row.name;return n;}));$(selector).value=selected;
  }
  $('#prompt').value='Preserve this conversation draft';state.draftContext='default:workspace:session-running';
  w.closeConversationSidebar=()=>{};w.RunDeskLayout={setView(){}};w.RunDeskTraceUI={close(){}};w.RunDeskConversations={open:async()=>vm.runInContext('setProductPage("conversation")',ctx)};
  w.RunDeskDiagnostics={recent:()=>null,request:async(url,request={})=>{
    const route=url.replace('/api/v1',''),method=request.method||'GET',body=typeof request.body==='string'?JSON.parse(request.body):request.body;
    const entry={route,method,body,headers:request.headers};requests.push(entry);
    if(options.request) {const value=await options.request(entry);if(value!==undefined)return value;}
    if(method==='GET') {
      if(route==='/skill-center/sources')return {items:[]};
      if(route==='/skill-center/packs')return {items:[copy(pack)]};
      if(route==='/skill-center/packs/local-pack')return copy(pack);
      if(route==='/skill-center/engines')return {items:[{id:'builtin',available:true,message:'Built in'},{id:'skillspector',available:false,message:'Not configured'}]};
      if(route.startsWith('/skill-center/installed?')) {const q=new URLSearchParams(route.split('?')[1]);return {items:[{name:'local-reader'},{name:malicious}],workspaceId:q.get('workspaceId'),instanceId:q.get('instanceId'),scope:q.get('scope'),warnings:[malicious]};}
      if(route==='/skill-center/diagnostics')return {items:copy(options.diagnostics||[])};
      if(route==='/skill-center/check-engines')return {available:false,message:'Docker unavailable',items:[]};
    }
    if(method==='POST' && (route.startsWith('/skill-center/import') || route==='/skill-center/from-installed'))return {pack:copy(pack),scan:copy(report)};
    if(method==='POST'&&route==='/skill-center/diagnostics')return body.target==='skillspector'?copy(diagnostic):{id:'skillup-diagnostic',target:'skillup',instanceId:body.instanceId,overall:'incomplete',checkedAt:diagnostic.checkedAt,checks:[{id:'cli',label:'CLI protocol',status:'ok',message:'Controlled sample verified'},{id:'model',label:'Model access',status:'unknown',message:'Model, authentication and network have not been checked'}]};
    throw Error('Unexpected local Skill request '+method+' '+route);
  }};
  vm.runInContext(fs.readFileSync(path.join(root,'applications.js'),'utf8'),ctx);
  vm.runInContext(fs.readFileSync(path.join(root,'skill-center.js'),'utf8'),ctx);
  const imports=()=>requests.filter(r=>r.method==='POST'&&r.route.startsWith('/skill-center/import'));
  const snapshots=()=>requests.filter(r=>r.method==='POST'&&r.route==='/skill-center/from-installed');
  const installs=()=>requests.filter(r=>r.route.endsWith('/install')||r.route.includes('/skill-bundles'));
  const diagnosticPosts=()=>requests.filter(r=>r.method==='POST'&&r.route==='/skill-center/diagnostics');
  const makeFile=(data,name,relative,size)=>{const value=new w.File([data],name,{type:name.endsWith('.zip')?'application/zip':'application/octet-stream'});if(relative!==undefined)Object.defineProperty(value,'webkitRelativePath',{value:relative});if(size!==undefined)Object.defineProperty(value,'size',{value:size});return value;};
  const selectFiles=(selector,files)=>{const input=$(selector);assert(input,'Missing input '+selector);Object.defineProperty(input,'files',{value:files,configurable:true});input.dispatchEvent(new w.Event('change',{bubbles:true}));return input;};
  const change=(selector,value)=>{$(selector).value=value;$(selector).dispatchEvent(new w.Event('change',{bubbles:true}));};
  const open=async()=>{await w.RunDeskSkillCenter.open('search');await until(()=>!!$('#skill-center-local-zip-submit'),$('#skill-center-page').textContent);};
  return {dom,w,ctx,state,$,$$,requests,imports,snapshots,installs,diagnosticPosts,makeFile,selectFiles,change,open};
}

async function readFile(w,file) { return new Promise((resolve,reject)=>{const reader=new w.FileReader();reader.onload=()=>resolve(reader.result);reader.onerror=reject;reader.readAsText(file);}); }

(async()=>{
  const zip=fixture(),session=zip.state.session;await zip.open();
  assert(zip.$('#skill-center-local-message'));
  assert(/不会自动安装|不.*安装/.test(zip.$('#skill-center-page').textContent),'Acquisition must not imply installation');
  const zipped=zip.makeFile('original zipped bytes','selected.zip');zip.selectFiles('#skill-center-local-zip',[zipped]);zip.$('#skill-center-local-name').value='my-local-skill';
  zip.$('#skill-center-local-zip-submit').click();await until(()=>zip.w.location.hash==='#skills/pack/local-pack');
  assert.equal(zip.imports().length,1);assert.equal(zip.imports()[0].body,zipped,'ZIP bytes were serialized or replaced');assert.equal(zip.imports()[0].headers['Content-Type'],'application/zip');
  assert.equal(new URLSearchParams(zip.imports()[0].route.split('?')[1]).get('name'),'my-local-skill');
  assert.equal(zip.installs().length,0);assert(zip.$('#skill-center-report').textContent.includes('opaque.bin'));assert(zip.$('#skill-center-report [data-skill-state="incomplete"]'));
  assert(!zip.$('#skill-center-report [data-skill-state="no_findings"]'),'Binary coverage gap became a safety pass');
  assert.equal(zip.state.session,session);assert.equal(zip.state.sending,true);assert.equal(zip.$('#prompt').value,'Preserve this conversation draft');

  const folder=fixture();await folder.open();
  const files=[folder.makeFile('---\nname: local-reader\n---\nRead documents.','SKILL.md','local-reader/SKILL.md'),folder.makeFile('Nested exact reference bytes','guide.md','local-reader/references/guide.md')];
  folder.selectFiles('#skill-center-local-directory',files);folder.$('#skill-center-local-directory-submit').click();await until(()=>folder.imports().length===1&&folder.w.location.hash==='#skills/pack/local-pack');
  const form=folder.imports()[0].body;assert(form instanceof folder.w.FormData);const entries=[...form.entries()];
  assert.deepEqual(entries.map(([name])=>name),files.map(file=>file.webkitRelativePath));
  assert.equal(await readFile(folder.w,entries[0][1]),'---\nname: local-reader\n---\nRead documents.');assert.equal(await readFile(folder.w,entries[1][1]),'Nested exact reference bytes');
  assert.equal(folder.imports()[0].headers?.['Content-Type'],undefined,'Multipart boundary must remain owned by the browser');assert.equal(folder.installs().length,0);

  const missingPath=fixture();await missingPath.open();missingPath.selectFiles('#skill-center-local-directory',[missingPath.makeFile('read','SKILL.md')]);missingPath.$('#skill-center-local-directory-submit').click();await flush();
  assert.equal(missingPath.imports().length,0,'Directory files were silently flattened');assert(missingPath.$('#skill-center-local-message').textContent.trim());
  const oversized=fixture();await oversized.open();oversized.selectFiles('#skill-center-local-zip',[oversized.makeFile('tiny','large.zip',undefined,(34<<20)+1)]);oversized.$('#skill-center-local-zip-submit').click();await flush();assert.equal(oversized.imports().length,0);
  const zipEnvelope=fixture();await zipEnvelope.open();zipEnvelope.selectFiles('#skill-center-local-zip',[zipEnvelope.makeFile('tiny','envelope.zip',undefined,(32<<20)+104858)]);zipEnvelope.$('#skill-center-local-zip-submit').click();await until(()=>zipEnvelope.imports().length===1&&zipEnvelope.w.location.hash==='#skills/pack/local-pack');assert.equal(zipEnvelope.installs().length,0,'Permitting ZIP envelope overhead installed the pack');
  const many=fixture();await many.open();many.selectFiles('#skill-center-local-directory',Array.from({length:1001},(_,i)=>many.makeFile('x',i+'.md','too-many/'+i+'.md')));many.$('#skill-center-local-directory-submit').click();await flush();assert.equal(many.imports().length,0);
  const largeFolder=fixture();await largeFolder.open();largeFolder.selectFiles('#skill-center-local-directory',[largeFolder.makeFile('x','SKILL.md','large/SKILL.md',16<<20),largeFolder.makeFile('y','guide.md','large/guide.md',(16<<20)+1)]);largeFolder.$('#skill-center-local-directory-submit').click();await flush();assert.equal(largeFolder.imports().length,0);

  const failedUpload=fixture({request:entry=>{if(entry.method==='POST'&&entry.route.startsWith('/skill-center/import'))throw Error(malicious);}});await failedUpload.open();const preserved=failedUpload.makeFile('original','retry.zip');failedUpload.selectFiles('#skill-center-local-zip',[preserved]);failedUpload.$('#skill-center-local-name').value='retry-name';failedUpload.$('#skill-center-local-zip-submit').click();await until(()=>failedUpload.$('#skill-center-local-message').textContent.includes(malicious));assert.equal(failedUpload.$('#skill-center-local-zip').files[0],preserved);assert.equal(failedUpload.$('#skill-center-local-name').value,'retry-name');assert.equal(failedUpload.w.location.hash,'#skills/search');assert(!failedUpload.$('#skill-center-page img'));assert.equal(failedUpload.installs().length,0);

  const pending=deferred(),duplicate=fixture({request:entry=>entry.method==='POST'&&entry.route.startsWith('/skill-center/import')?pending.promise:undefined});await duplicate.open();
  const original=duplicate.makeFile('frozen zip','original.zip');duplicate.selectFiles('#skill-center-local-zip',[original]);duplicate.$('#skill-center-local-zip-submit').click();duplicate.$('#skill-center-local-zip-submit').click();await until(()=>duplicate.imports().length===1);
  assert(duplicate.$('#skill-center-local-zip-submit').disabled,'Pending upload still accepted clicks');
  await duplicate.w.RunDeskSkillCenter.open('library');pending.resolve({pack:copy(pack),scan:copy(report)});await flush();await flush();assert.equal(duplicate.w.location.hash,'#skills/library','An upload response stole later navigation');assert.equal(duplicate.installs().length,0);

  const installed=fixture();await installed.open();installed.$('#skill-center-installed-load').click();await until(()=>installed.$('#skill-center-installed-name').options.length>1);
  const list=installed.requests.find(r=>r.route.startsWith('/skill-center/installed?'));assert(list);const selection=new URLSearchParams(list.route.split('?')[1]);assert.equal(selection.get('workspaceId'),'workspace');assert.equal(selection.get('instanceId'),'default');assert.equal(selection.get('scope'),'project');
  assert(installed.$('#skill-center-installed-name').textContent.includes(malicious));assert(!installed.$('#skill-center-page img'));assert(!installed.$('#skill-center-page script'));assert.equal(installed.w.localSkillSourceExecuted,undefined);
  installed.change('#skill-center-installed-name','local-reader');installed.$('#skill-center-installed-snapshot').click();await until(()=>installed.snapshots().length===1&&installed.w.location.hash==='#skills/pack/local-pack');
  assert.deepEqual(installed.snapshots()[0].body,{workspaceId:'workspace',instanceId:'default',scope:'project',name:'local-reader'});assert.equal(installed.installs().length,0);

  const changed=fixture();await changed.open();changed.$('#skill-center-installed-load').click();await until(()=>changed.$('#skill-center-installed-name').options.length>1);changed.change('#skill-center-installed-name','local-reader');changed.change('#workspace','other');changed.$('#skill-center-installed-snapshot').click();await flush();assert.equal(changed.snapshots().length,0,'An old installed selection was rebound to another workspace');
  const delayedList=deferred(),stale=fixture({request:entry=>entry.method==='GET'&&entry.route.startsWith('/skill-center/installed?')?delayedList.promise:undefined});await stale.open();stale.$('#skill-center-installed-load').click();await until(()=>stale.requests.some(r=>r.route.startsWith('/skill-center/installed?')));stale.change('#workspace','other');delayedList.resolve({items:[{name:'old-workspace-skill'}],workspaceId:'workspace',instanceId:'default',scope:'project'});await flush();await flush();assert(!stale.$('#skill-center-installed-name').textContent.includes('old-workspace-skill'),'Stale installed results entered a different context');
  const snapshotDelay=deferred(),pendingSnapshot=fixture({request:entry=>entry.method==='POST'&&entry.route==='/skill-center/from-installed'?snapshotDelay.promise:undefined});await pendingSnapshot.open();pendingSnapshot.change('#skill-center-installed-scope','instance');pendingSnapshot.$('#skill-center-installed-load').click();await until(()=>pendingSnapshot.$('#skill-center-installed-name').options.length>1);pendingSnapshot.change('#skill-center-installed-name','local-reader');pendingSnapshot.$('#skill-center-installed-snapshot').click();pendingSnapshot.$('#skill-center-installed-snapshot').click();await until(()=>pendingSnapshot.snapshots().length===1);assert.deepEqual(pendingSnapshot.snapshots()[0].body,{workspaceId:'workspace',instanceId:'default',scope:'instance',name:'local-reader'});assert(pendingSnapshot.$('#skill-center-installed-snapshot').disabled);pendingSnapshot.change('#workspace','other');snapshotDelay.resolve({pack:copy(pack),scan:copy(report)});await flush();await flush();assert.equal(pendingSnapshot.w.location.hash,'#skills/search','A completed snapshot opened against a different workspace');assert.equal(pendingSnapshot.installs().length,0);

  const untested=fixture();await untested.open();await until(()=>!!untested.$('#skill-center-diagnostics'));assert.equal(untested.$('#skill-center-diagnostics').open,false);assert(untested.requests.some(r=>r.method==='GET'&&r.route==='/skill-center/diagnostics'));assert.equal(untested.diagnosticPosts().length,0,'Opening the center executed a diagnostic');assert(untested.$('#skill-center-diagnostic-results').textContent.includes('尚未诊断'));assert(untested.$('#skill-center-diagnostics').textContent.includes('Go 快速检查'));untested.change('#skill-center-diagnostic-target','skillup');assert(untested.$('#skill-center-diagnostic-run').disabled,'Skill-Up ran without a managed Docker environment');assert.equal(untested.diagnosticPosts().length,0);

  const pendingDiagnostic=deferred(),explicit=fixture({request:entry=>entry.method==='POST'&&entry.route==='/skill-center/diagnostics'?pendingDiagnostic.promise:undefined});await explicit.open();explicit.$('#skill-center-diagnostic-run').click();explicit.$('#skill-center-diagnostic-run').click();await until(()=>explicit.diagnosticPosts().length===1);assert.deepEqual(explicit.diagnosticPosts()[0].body,{target:'skillspector'});assert(explicit.$('#skill-center-diagnostic-target').disabled);assert(explicit.$('#skill-center-diagnostic-environment').disabled);assert(explicit.$('#skill-center-diagnostic-run').disabled);pendingDiagnostic.resolve(copy(diagnostic));await until(()=>explicit.$('#skill-center-diagnostic-results').textContent.includes('静态工具协议已验证'));assert(!explicit.$('#skill-center-diagnostic-run').disabled);assert(explicit.$('#skill-center-diagnostic-results').textContent.includes('2.13.4'));assert(explicit.$('#skill-center-diagnostic-results').textContent.includes(malicious));assert(!explicit.$('#skill-center-page img'));assert(!explicit.$('#skill-center-page script'));assert.equal(explicit.w.localSkillSourceExecuted,undefined);

  const cached=fixture({diagnostics:[{...copy(diagnostic),overall:'unavailable',checks:[{id:'missing',status:'missing',label:'Binary',message:'Absent'},{id:'incompatible',status:'incompatible',label:'Version',message:'Unsupported protocol'},{id:'failed',status:'failed',label:'CLI invocation',message:'Process failed'},{id:'model',status:'unknown',label:'Model access',message:'Not checked'}]}]});await cached.open();await until(()=>!!cached.$('[data-diagnostic-status="missing"]'));const cachedText=cached.$('#skill-center-diagnostic-results').textContent;for(const label of ['未安装或缺失','不兼容','执行失败','未检查'])assert(cachedText.includes(label));assert.equal(cached.diagnosticPosts().length,0,'Reading cached diagnostics triggered execution');

  const skillup=fixture({environments:[dockerEnvironment,{id:'unmanaged-docker',name:'Unmanaged Docker',managed:false,execution:{mode:'docker'}},{id:'managed-local',name:'Managed local',managed:true,execution:{mode:'local'}}]});await skillup.open();skillup.change('#skill-center-diagnostic-target','skillup');assert.equal(skillup.$('#skill-center-diagnostic-environment').options.length,1);assert.equal(skillup.$('#skill-center-diagnostic-environment').value,'docker-fixture');skillup.$('#skill-center-diagnostic-run').click();await until(()=>!!skillup.$('[data-diagnostic-status="unknown"]'));assert.deepEqual(skillup.diagnosticPosts()[0].body,{target:'skillup',instanceId:'docker-fixture'});assert(skillup.$('#skill-center-diagnostic-results').textContent.includes('诊断未完整覆盖'));assert(skillup.$('#skill-center-diagnostic-results').textContent.includes('未检查'));assert(!skillup.$('#skill-center-diagnostic-results').textContent.includes('静态工具协议已验证'));

  const lateResult=deferred(),diagnosticStale=fixture({request:entry=>entry.method==='POST'&&entry.route==='/skill-center/diagnostics'?lateResult.promise:undefined});await diagnosticStale.open();diagnosticStale.$('#skill-center-diagnostic-run').click();await until(()=>diagnosticStale.diagnosticPosts().length===1);await diagnosticStale.w.RunDeskSkillCenter.open('library');lateResult.resolve(copy(diagnostic));await flush();await flush();assert.equal(diagnosticStale.w.location.hash,'#skills/library');assert(!diagnosticStale.$('#skill-center-diagnostic-results').textContent.includes('2.13.4'),'A stale diagnostic wrote into the new page');

  const lateRead=deferred(),diagnosticReadRace=fixture({request:entry=>entry.method==='GET'&&entry.route==='/skill-center/diagnostics'?lateRead.promise:undefined});await diagnosticReadRace.open();diagnosticReadRace.$('#skill-center-diagnostic-run').click();await until(()=>diagnosticReadRace.$('#skill-center-diagnostic-results').textContent.includes('2.13.4'));lateRead.resolve({items:[{...copy(diagnostic),id:'old-cached-diagnostic',overall:'unavailable',checks:[{id:'missing',label:'Old cached result',status:'missing',message:'Older read'}]}]});await flush();await flush();assert(diagnosticReadRace.$('#skill-center-diagnostic-results').textContent.includes('2.13.4'),'An old cached read replaced the explicit diagnostic');assert(!diagnosticReadRace.$('#skill-center-diagnostic-results').textContent.includes('Older read'));

  let cleanupAttempts=0;const cleanup=fixture({diagnostics:[{...copy(diagnostic),overall:'failed',cleanupJobId:'cleanup-job'}],request:entry=>{if(entry.method==='POST'&&entry.route==='/skill-center/check-jobs/cleanup-job/cancel'){cleanupAttempts++;if(cleanupAttempts===1)throw Error(malicious);return {cleanupPending:false};}}});await cleanup.open();await until(()=>cleanup.$$('#skill-center-diagnostic-results button').length===1);cleanup.$$('#skill-center-diagnostic-results button')[0].click();await until(()=>cleanup.$('#skill-center-diagnostic-message').textContent.includes(malicious));assert(cleanup.$('#skill-center-diagnostic-results').textContent.includes('2.13.4'),'Failed cleanup discarded the original diagnostic');assert(!cleanup.$('#skill-center-page img'));cleanup.$$('#skill-center-diagnostic-results button')[0].click();await until(()=>cleanup.$('#skill-center-diagnostic-message').textContent.includes('容器清理已确认'));assert.equal(cleanupAttempts,2);assert.equal(cleanup.diagnosticPosts().length,0,'Cleanup silently reran diagnostics');assert(cleanup.$('#skill-center-diagnostic-results').textContent.includes('2.13.4'));

  for(const test of [zip,folder,missingPath,oversized,zipEnvelope,many,largeFolder,failedUpload,duplicate,installed,changed,stale,pendingSnapshot,untested,explicit,cached,skillup,diagnosticStale,diagnosticReadRace,cleanup])test.dom.window.close();
  console.log(JSON.stringify({passed:true,checks:['ZIP acquisition preserves the original File and never installs','folder acquisition preserves every relative path and byte','binary inspection remains incomplete','file count/byte limits and missing directory paths are rejected','failed imports preserve the original form state','pending uploads reject duplicates and stale responses cannot navigate','installed snapshots freeze the workspace/environment/scope selection','source and diagnostic text is never interpreted as HTML','ordinary conversation identity and draft remain unchanged','diagnostics run only after an explicit click and lock their selection','missing, incompatible, failed, verified and unchecked results remain distinct','Skill-Up requires a managed Docker environment and leaves model access unchecked','stale diagnostic responses and cached reads cannot overwrite newer results','cleanup retries retain diagnostic evidence and never run a new diagnostic'],scope:'shipped native JavaScript in JSDOM; no backend archive or browser geometry acceptance'}));
})().catch(error=>{console.error(error);process.exitCode=1;});
