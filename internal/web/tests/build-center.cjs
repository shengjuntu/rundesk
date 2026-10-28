/* Shipped JavaScript behavior tests; no Docker daemon, model or live account. */
'use strict';
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path'), vm = require('node:vm');
const {JSDOM} = require('jsdom'), {webcrypto} = require('node:crypto'), {TextEncoder} = require('node:util');
const root = path.resolve(__dirname, '..'), copy = value => JSON.parse(JSON.stringify(value));
const dom = new JSDOM(fs.readFileSync(path.join(root, 'index.html'), 'utf8'), {url:'http://localhost/#assistant',runScripts:'outside-only'}), w = dom.window, ctx = dom.getInternalVMContext();
Object.defineProperty(w, 'crypto', {value:webcrypto,configurable:true}); w.TextEncoder = TextEncoder;
const timers = new Map(); let timerID = 0;
w.setTimeout = (fn, delay) => { timers.set(++timerID, {fn,delay}); return timerID; }; w.clearTimeout = id => timers.delete(id); w.setInterval = () => 0;
w.HTMLDialogElement.prototype.showModal = function () { this.open = true; };
w.HTMLDialogElement.prototype.close = function () { this.open = false; this.dispatchEvent(new w.Event('close')); };
w.rdText = value => value; w.rdFormat = (value, ...args) => value.replace(/\$\{(\d+)\}/g, (_, i) => args[+i]); w.confirm = () => true;
const code = fs.readFileSync(path.join(root, 'app.js'), 'utf8');
vm.runInContext(code.slice(0, code.indexOf('async function boot()')), ctx);
vm.runInContext(code.slice(code.indexOf('async function openSettings('), code.indexOf('function renderNotes(')), ctx);
const state = vm.runInContext('state', ctx), $ = selector => w.document.querySelector(selector), $$ = selector => [...w.document.querySelectorAll(selector)];
w.closeConversationSidebar = () => {}; w.resetConversation = () => { state.selection++; state.session = null; }; w.updateWorkspaceLabel = () => {};
w.renderInstanceOptions = id => { $('#instance').replaceChildren(...state.instances.map(row => { const node = w.document.createElement('option'); node.value = row.id; node.textContent = row.name; return node; })); $('#instance').value = id; };
w.skillBundleImporter = () => w.document.createElement('div'); w.RunDeskTraceUI = {close(){}};
w.RunDeskEnvironments = {effective:async () => ({instance:state.instances[0]}),scopeLabel:() => '当前实例'};
const profiles = ['news2douyin','people-search','video-build','hyperframes','video-use','openmontage'];
const db = {recipes:profiles.map(id => ({schemaVersion:1,id:'builtin-'+id,profileId:id,kind:'builtin',name:id,version:'1.0.0',revision:1,description:'工具配方',dockerfile:'Dockerfile',contextSha256:'context-'+id,recipeSha:'recipe-'+id,plugins:[],smoke:{command:[],timeoutSeconds:60,expectedFiles:[]},archived:false})),builds:[],images:[],instances:[{id:'default',name:'Codex',codexHome:'/home/codex',agentRuntime:{kind:'codex'},execution:{mode:'local'}},{id:'kun',name:'Kun',agentRuntime:{kind:'kun'},execution:{mode:'local'}}],workspaces:[{id:'workspace',name:'项目',path:'/work/project'}],sessions:[]};
const image = (key, name) => ({key,imageId:'sha256:'+key.repeat(3),references:[name],sources:[{kind:'build',id:'build-1',name:'工具镜像',recipeId:'builtin-video-build'}],registrations:[],selectedBy:[],environments:[],availability:'available'});
db.images = [image('a'.repeat(24),'video:1'), image('b'.repeat(24),'video:2')];
const sourceTypes = [
  {id:'docker',name:'Docker',fields:[{id:'baseImage',label:'基础镜像',argument:'RD_BASE_IMAGE'}]},
  {id:'apt',name:'APT',fields:[{id:'mirror',label:'APT 主仓',argument:'RD_APT_MIRROR'},{id:'securityMirror',label:'APT 安全仓',argument:'RD_APT_SECURITY_MIRROR'}]},
  {id:'npm',name:'npm',fields:[{id:'registry',label:'npm registry',argument:'RD_NPM_REGISTRY'}]},
  {id:'pip',name:'pip',fields:[{id:'indexUrl',label:'pip index',argument:'RD_PIP_INDEX_URL'}]},
  {id:'conda',name:'Conda',fields:[{id:'channels',label:'频道',argument:'RD_CONDA_CHANNELS'}]},
  {id:'github',name:'GitHub',fields:[{id:'archiveBase',label:'源码归档地址',argument:'RD_GITHUB_ARCHIVE_BASE'}]}
];
db.catalog={schemaVersion:1,defaultGroup:'international',revision:'catalog-1',types:sourceTypes,groups:[
  {id:'international',name:'国际（美国为主）',sources:[
    {id:'docker-original',name:'原始基础镜像',type:'docker',values:{baseImage:''}},
    {id:'debian',name:'Debian 官方',type:'apt',values:{mirror:'https://deb.debian.org/debian',securityMirror:'https://security.debian.org/debian-security'}},
    {id:'npm-official',name:'npm 官方',type:'npm',values:{registry:'https://registry.npmjs.org/'}},
    {id:'pypi',name:'PyPI',type:'pip',values:{indexUrl:'https://pypi.org/simple/'}}
  ]},
  {id:'china',name:'中国镜像',sources:[
    {id:'docker-keep',name:'保留原镜像',type:'docker',values:{baseImage:''}},
    {id:'debian-cn',name:'Debian 镜像',type:'apt',values:{mirror:'https://mirror.example/debian',securityMirror:'https://security.debian.org/debian-security'}},
    {id:'npm-cn',name:'npm 镜像',type:'npm',values:{registry:'https://registry.npmmirror.com/'}},
    {id:'pypi-cn',name:'PyPI 镜像',type:'pip',values:{indexUrl:'https://pypi.tuna.tsinghua.edu.cn/simple/'}}
  ]}
]};
const sourceCapability={types:[
 {type:'docker',detected:true,supported:true,arguments:['RD_BASE_IMAGE']},
 {type:'apt',detected:true,supported:true,arguments:['RD_APT_MIRROR','RD_APT_SECURITY_MIRROR']},
 {type:'npm',detected:true,supported:true,arguments:['RD_NPM_REGISTRY']},
 {type:'pip',detected:true,supported:true,arguments:['RD_PIP_INDEX_URL']},
 {type:'github',detected:true,supported:false,arguments:[]}
]};
let runtimeAvailable=false, runtimeGate, catalogConflict=false, sourceReadError=false, publicCatalogConflict=false, publicationLost=true, publicationReady=false, publicCheckAllowed=false;
db.publicCatalog={schemaVersion:1,revision:'public-1',items:[]};db.publications=[];
const publicEntry={id:'news-1',profileId:'news2douyin',profileVersion:'1.1.2',codexVersion:'0.157.1',platform:'linux/amd64',sources:[{id:'ghcr',name:'GHCR',reference:'ghcr.io/example/news2douyin@sha256:'+'c'.repeat(64)}]};
const requests = [], receipts = new Map(); let uploadLost = true, launchCount = 0, launchError = false, checkCount = 0, deleted = false, imageGate, recipeGate, pluginGate;
const fail = (text, status, code) => Object.assign(new Error(text), {status,code});
function build(id, extra = {}) { return {id,name:'构建 '+id,status:'succeeded',scope:'recipe',recipeId:'builtin-video-build',recipeRevision:1,reference:'video:1',imageId:db.images[0]?.imageId || 'sha256:fixed',contextSha256:'frozen-context',created:'2026-10-20T10:00:00Z',...extra}; }
w.RunDeskDiagnostics = {recent:() => null,request:async (url, options = {}) => {
  const route = url.replace('/api/v1',''), method = options.method || 'GET', body = typeof options.body === 'string' ? JSON.parse(options.body) : options.body;
  requests.push({route,method,body,headers:options.headers});
  if (method === 'GET') {
    if(route==='/setup/public-images')return copy(db.publicCatalog);
    if(/\/build-center\/images\/[^/]+\/publication-readiness$/.test(route))return{eligible:publicationReady,sourceBuildId:'build-verified',imageId:db.images[0].imageId,reason:publicationReady?'':'Smoke must pass for the same build',offlineTestId:'offline-verified',smokeTestId:publicationReady?'smoke-verified':'',profileId:'news2douyin',profileVersion:'1.1.2',codexVersion:'0.157.1',platform:'linux/amd64'};
    if(route==='/build-center/publications')return{items:copy(db.publications)};
    if(/^\/build-center\/publications\/[^/]+\/log\?after=/.test(route))return{text:route.endsWith('=0')?'upload done token=do-not-display\n':'',next:40};
    if(/^\/build-center\/publications\/[^/]+$/.test(route))return copy(db.publications.find(v=>route.endsWith('/'+v.id)));

    if (route.startsWith('/build-center/runtime-options?')) { if(runtimeGate)return runtimeGate.promise; return {platform:'linux/amd64',version:'0.163.0',nativeAvailable:runtimeAvailable,reason:runtimeAvailable?'verified fixture package':'no exact matching package'}; }
    if (route === '/build-center/sources/defaults') { const value=copy(db.catalog);value.defaultGroup='international';value.types.find(t=>t.id==='github').fields.push({id:'archiveUrl',label:'完整归档地址',argument:'RD_GITHUB_ARCHIVE_URL',kind:'url'});value.groups[1].sources.push({id:'aliyun-debian',name:'阿里云 Debian',type:'apt',values:{mirror:'http://mirrors.aliyun.com/debian',securityMirror:'http://mirrors.aliyun.com/debian-security'}});return value; }
    if (route === '/build-center/sources') { if(sourceReadError) throw fail('source catalog unreadable',500,'build_sources_invalid'); return copy(db.catalog); }
    if (/^\/build-center\/recipes\/[^/]+\/sources$/.test(route)) return copy(sourceCapability);
    if (/^\/build-center\/recipes\/[^/]+\/dockerfile/.test(route)) return {path:'Dockerfile',template:!route.includes('codexVersion='),dockerfile:'FROM node:22-bookworm-slim\nARG CODEX_VERSION='+(route.includes('codexVersion=')?'0.157.1':'0.0.0')};
    if (route === '/instances') return copy(db.instances);
    if (route === '/workspaces') return copy(db.workspaces);
    if (route === '/sessions' || route === '/applications') return [];
    if (route === '/build-center/recipes') { if (recipeGate) return recipeGate.promise; return {items:copy(db.recipes)}; }
    if (route === '/build-center/images') return {items:copy(db.images)};
    if (/^\/build-center\/images\/[ab]{24}$/.test(route)) { if (imageGate && route.endsWith('a'.repeat(24))) return imageGate.promise; return copy(db.images.find(row => route.endsWith(row.key))); }
    if (route === '/build-center/builds') return {items:copy(db.builds)};
    if (/\/build-center\/(?:builds|tests)\/[^/]+\/log\?after=/.test(route)) return {text:route.endsWith('=0') ? 'real frozen build log\n' : '',next:22,status:'succeeded',truncated:false};
    if (/^\/build-center\/tests\//.test(route)) return copy(db.builds.find(row => route.endsWith(row.id)));
    if (/^\/build-center\/assistant-runs\//.test(route)) return {id:'run-1',taskId:'task-1',sessionId:'session-1',instanceId:'default',workspaceId:'assistant-work',status:'completed',skillBootstrap:true,nativePluginInstalled:false};
    if (route.startsWith('/instances/default/plugins')) { if (pluginGate) return pluginGate.promise; return {backend:'codex',runtime:'local',support:{nativeInventory:false,nativeMutation:false},readOnly:true,plugins:[{id:'editor@example',displayName:'Editor',configured:true,enabled:true,loaded:'unknown',inventoryState:'unknown',skills:[{name:'cut',path:'/plugins/cut/SKILL.md'}],mcpServers:[{name:'media'}]}],warnings:[]}; }
    if (route === '/workspaces/workspace/skills?instanceId=default') return {data:[{skills:[{name:'cut',path:'/home/codex/skills/cut/SKILL.md',pluginId:'editor@example',enabled:true},{name:'local',path:'/home/codex/skills/local/SKILL.md',enabled:true}]}]};
    throw Error('Unexpected GET '+route);
  }
  if(method==='PUT'&&route==='/setup/public-images'){
    if(publicCatalogConflict||body.revision!==db.publicCatalog.revision)throw fail('catalog changed',409,'public_images_changed');
    db.publicCatalog={schemaVersion:body.schemaVersion,items:copy(body.items),revision:'public-'+(Number(db.publicCatalog.revision.split('-')[1])+1)};return copy(db.publicCatalog);
  }
  if(method==='POST'&&route==='/build-center/publications'){
    assert.equal(body.confirm,true);assert.equal(body.sourceBuildId,'build-verified');assert.match(body.requestId,/^[a-f0-9]{24}$/);assert(options.headers['Idempotency-Key']);
    let value=receipts.get(body.requestId);if(!value){value={id:'publication-1',sourceBuildId:body.sourceBuildId,target:body.target,status:'running',imageId:db.images[0].imageId,created:'2026-10-27T10:00:00Z',publicAccessVerified:false};db.publications.push(value);receipts.set(body.requestId,value);}
    if(publicationLost){publicationLost=false;throw fail('publication response lost');}return copy(value);
  }
  if(method==='POST'&&/^\/build-center\/publications\/[^/]+\/check-public$/.test(route)){
    assert(options.headers['Idempotency-Key']);const value=db.publications.find(v=>route.includes('/'+v.id+'/'));value.publicAccessVerified=publicCheckAllowed;value.publicCheckedAt='2026-10-27T11:00:00Z';value.publicAccessError=publicCheckAllowed?'':'anonymous denied';return copy(value);
  }
  if(method==='POST'&&/^\/build-center\/publications\/[^/]+\/cancel$/.test(route)){
    assert(options.headers['Idempotency-Key']);const value=db.publications.find(v=>route.includes('/'+v.id+'/'));value.status='canceled';return copy(value);
  }
  if (method === 'PUT' && route === '/build-center/sources') {
    if(catalogConflict || body.revision!==db.catalog.revision) throw fail('catalog changed',409,'build_sources_changed');
    assert.equal(body.schemaVersion,1); assert(['international','china'].includes(body.defaultGroup)); assert(Array.isArray(body.types)); db.catalog={...copy(body),revision:'catalog-'+(Number(db.catalog.revision.split('-')[1])+1),types:copy(body.types)}; return copy(db.catalog);
  }
  if (method === 'POST' && route === '/build-center/recipes') {
    assert(body instanceof w.FormData); const manifest = JSON.parse(body.get('manifest')), id = body.get('requestId'); assert.match(id,/^[0-9a-f]{24}$/); assert(body.get('context'));
    let value = receipts.get(id); if (!value) { value = {...manifest,id:'custom-'+id,revision:1,kind:'custom',archived:false,contextSha256:'custom-context',recipeSha:'custom-recipe'}; db.recipes.push(value); receipts.set(id,copy(value)); }
    if (uploadLost) { uploadLost = false; throw fail('response lost'); } return copy(value);
  }
  if (method === 'POST' && /^\/build-center\/recipes\/[^/]+\/build$/.test(route)) {
    assert.match(body.requestId,/^[0-9a-f]{24}$/); assert.equal(body.recipeRevision,1); assert.equal(body.recipeSha,'recipe-video-build'); assert.equal(body.codexVersion,'0.163.0'); const value = build('build-'+(db.builds.length+1),{buildSources:{group:body.buildSources.group,catalogSHA:db.catalog.revision,selections:copy(body.buildSources.selections),values:copy(body.buildSources.overrides)}}); db.builds.unshift(value); return copy(value);
  }
  if (method === 'POST' && /\/build-center\/builds\/[^/]+\/rebuild$/.test(route)) {
    assert.match(body.requestId,/^[0-9a-f]{24}$/); const value = build('rebuild-'+(db.builds.length+1)); db.builds.unshift(value); return copy(value);
  }
  if (method === 'POST' && /\/build-center\/images\/[^/]+\/check$/.test(route)) { checkCount++; return copy(db.images.find(row => route.includes(row.key))); }
  if (method === 'POST' && /\/build-center\/images\/[^/]+\/launch$/.test(route)) {
    assert(!body.instanceId && !body.workspaceId, 'default launch must not modify existing targets'); assert.match(body.requestId,/^[a-f0-9]{24}$/); if(launchError) throw fail('pending instanceId=partial-instance workspaceId=partial-work',409,'image_operation_unconfirmed'); launchCount++;
    return {instance:{id:'dedicated',name:body.name},workspace:{id:'dedicated-work',name:'独立项目'},environment:{id:'dedicated-env',state:'exited',instanceId:'dedicated',workspaceId:'dedicated-work',imageId:'sha256:fixed',error:'container exited'}};
  }
  if (method === 'POST' && /\/build-center\/images\/[^/]+\/tests$/.test(route)) { assert(['offline','smoke'].includes(body.kind)); const value = build('test-'+body.kind,{scope:'image-test',testKind:body.kind,status:body.kind==='smoke'?'failed':'succeeded',error:body.kind==='smoke'?'declared command failed':''}); db.builds.unshift(value); return copy(value); }
  if (method === 'DELETE' && /\/build-center\/images\/[ab]{24}$/.test(route)) { assert.equal(body.imageId,db.images.find(row=>route.endsWith(row.key)).imageId); assert.match(body.requestId,/^[a-f0-9]{24}$/); deleted = true; db.images.find(row=>route.endsWith(row.key)).availability='deleted'; return {deleted:true,imageId:body.imageId}; }
  if (method === 'POST' && route === '/build-center/assistant-runs') { assert.equal(body.instanceId,'default'); assert.equal(body.workspaceId,'workspace'); assert.match(body.requestId,/^[a-f0-9]{24}$/); return {id:'run-1',instanceId:'default',workspaceId:'assistant-work',taskId:'task-1',sessionId:'session-1',status:'queued',skillBootstrap:true,nativePluginInstalled:false}; }
  if (method === 'POST' && route === '/plugins/inspect') { assert(body.get('file')); return {readOnly:true,installSupported:false,format:'agent-plugin',manifestPath:'.agents/plugin.json',manifest:{name:'Editor',version:'1'},components:[{kind:'skill',name:'cut',path:'skills/cut'}],dependencies:[],unknownFields:[],warnings:[]}; }
  throw Error('Unexpected mutation '+method+' '+route);
}};
for (const name of ['applications.js','plugins.js','build-center.js']) vm.runInContext(fs.readFileSync(path.join(root,name),'utf8'),ctx);
const manage = fs.readFileSync(path.join(root,'manage.js'),'utf8'); vm.runInContext(manage.slice(manage.indexOf('async function renderSkills('),manage.indexOf('async function renderMCP(')),ctx);
Object.assign(state,{instances:copy(db.instances),workspaces:copy(db.workspaces)}); w.renderInstanceOptions('default'); $('#workspace').append(...db.workspaces.map(row => {const node=w.document.createElement('option');node.value=row.id;return node;})); $('#workspace').value='workspace';
const flush = () => new Promise(resolve => setImmediate(resolve)); async function until(check) { for(let n=0;n<500;n++){if(check())return;await new Promise(resolve=>setTimeout(resolve,5));}assert(check(),'UI did not settle'); }
const findButton = (scope,text) => [...scope.querySelectorAll('button')].find(node=>node.textContent===text);
const submit = node => node.closest('form').onsubmit({preventDefault(){}});
function setFile(selector,name='context.zip') { const file = new w.File(['docker context'],name,{type:'application/zip'}); file.arrayBuffer=async()=>new TextEncoder().encode('docker context').buffer; Object.defineProperty($(selector),'files',{configurable:true,value:[file]}); return file; }
function deferred() { let resolve; const promise=new Promise(done=>{resolve=done;});return{promise,resolve}; }
(async()=>{
  assert($('#nav-build-center'),'permanent navigation is missing');
  await w.RunDeskBuildCenter.open(); assert.equal($$('[data-recipe-id]').length,6); assert.equal(state.applications.length,0); assert($('#nav-build-center').classList.contains('selected')); assert(!$('#nav-assistant').classList.contains('selected'));
  // The catalog is editable without losing a draft on an optimistic-concurrency conflict.
  $('[data-center-action="sources"]').click(); await until(()=>!!$('#center-sources-json')&&!$('.build-center-dialog button[type=submit]').disabled);
  const editor=$('#center-sources-json'), catalogForm=editor.closest('form');
  const draft=JSON.parse(editor.value);draft.types.push({id:'gomod',name:'Go Modules',fields:[{id:'proxy',label:'GOPROXY',argument:'RD_GOPROXY',kind:'url'}]});draft.groups[1].sources.find(s=>s.id==='npm-cn').values.registry='https://mirror.company.example/npm/';editor.value=JSON.stringify(draft);
  const draftText=editor.value;catalogConflict=true;await submit(editor);assert.equal(editor.value,draftText);assert(catalogForm.querySelector('.error').textContent.includes('你的编辑仍保留'));
  catalogConflict=false;await submit(editor);assert(catalogForm.querySelector('[role=status]').textContent.includes('已保存'));assert.equal(db.catalog.groups[1].sources.find(s=>s.id==='npm-cn').values.registry,'https://mirror.company.example/npm/');assert(db.catalog.types.some(t=>t.id==='gomod'));$('.build-center-dialog').close();
  // Upgrades add missing defaults to the draft, without replacing edited addresses or saving automatically.
  $('[data-center-action="sources"]').click();await until(()=>!!$('#center-sources-json')&&!$('.build-center-dialog button[type=submit]').disabled);
  const mergeDraft=JSON.parse($('#center-sources-json').value);mergeDraft.defaultGroup='china';$('#center-sources-json').value=JSON.stringify(mergeDraft);
  const savesBeforeMerge=requests.filter(r=>r.method==='PUT'&&r.route==='/build-center/sources').length;
  $('#center-sources-merge-defaults').click();await until(()=>JSON.parse($('#center-sources-json').value).groups[1].sources.some(v=>v.id==='aliyun-debian'));
  const merged=JSON.parse($('#center-sources-json').value);assert.equal(merged.defaultGroup,'china');assert.equal(merged.groups[1].sources.find(v=>v.id==='npm-cn').values.registry,'https://mirror.company.example/npm/');assert(merged.types.find(t=>t.id==='github').fields.some(v=>v.id==='archiveUrl'));assert.equal(requests.filter(r=>r.method==='PUT'&&r.route==='/build-center/sources').length,savesBeforeMerge);assert($('.build-center-dialog [role=status]').textContent.includes('尚未写入'));
  $('#center-sources-merge-defaults').click();await until(()=>$('.build-center-dialog [role=status]').textContent.includes('全部内置选项'));assert.equal(JSON.parse($('#center-sources-json').value).groups[1].sources.filter(v=>v.id==='aliyun-debian').length,1);$('.build-center-dialog').close();
  // A user can view the actual Dockerfile and export a version-resolved context.
  findButton($('[data-recipe-id="builtin-news2douyin"]'),'查看配方').click();await until(()=>$('#center-recipe-dockerfile-content')?.textContent.includes('FROM node'));
  const download=$('#center-recipe-context-download'), blocked=new w.MouseEvent('click',{bubbles:true,cancelable:true});download.dispatchEvent(blocked);assert(blocked.defaultPrevented);assert($('.build-center-dialog .error').textContent.includes('精确版本'));
  $('#center-export-codex-version').value='0.157.1';$('#center-export-codex-version').dispatchEvent(new w.Event('input'));assert(download.href.endsWith('/context.zip?codexVersion=0.157.1'));findButton($('.build-center-dialog'),'预览 Dockerfile').click();await until(()=>$('#center-recipe-dockerfile-content').textContent.includes('0.157.1'));$('.build-center-dialog').close();
  // A catalog error disables submission instead of silently building with different sources.
  sourceReadError=true;$('[data-recipe-build="builtin-video-build"]').click();await until(()=>!!findButton($('.build-center-dialog'),'重试读取下载源'));assert($('.build-center-dialog button[type=submit]').disabled);$('.build-center-dialog').close();sourceReadError=false;
  $('[data-center-action="upload"]').click(); await until(()=>!!$('#center-recipe-name')); $('#center-recipe-name').value='Media'; setFile('#center-recipe-zip'); $('#center-recipe-smoke').value='["python","--version"]';
  await submit($('#center-recipe-name')); assert($('.build-center-dialog .error').textContent.includes('response lost')); await submit($('#center-recipe-name')); await until(()=>$$('[data-recipe-id]').length===7);
  const uploads=requests.filter(row=>row.route==='/build-center/recipes'&&row.method==='POST'); assert.equal(uploads.length,2); assert.equal(uploads[0].body.get('requestId'),uploads[1].body.get('requestId'),'uncertain upload duplicated receipt'); assert.equal(db.recipes.length,7);
  Object.defineProperty(w,'crypto',{value:{getRandomValues:array=>webcrypto.getRandomValues(array)},configurable:true}); $('[data-center-action="upload"]').click(); await until(()=>!!$('#center-recipe-name')); $('#center-recipe-name').value='Plain HTTP tools'; setFile('#center-recipe-zip'); await submit($('#center-recipe-name')); await until(()=>$$('[data-recipe-id]').length===8); Object.defineProperty(w,'crypto',{value:webcrypto,configurable:true});
  $('[data-recipe-build="builtin-video-build"]').click(); await until(()=>!!$('#center-source-group')&&!$('.build-center-dialog button[type=submit]').disabled); $('#center-codex-version').value='0.163.0';
  assert.equal($('#center-codex-runtime-mode').value,'auto');assert($('#center-codex-runtime-reference').disabled);
  $('#center-codex-runtime-check').click();await until(()=>$('#center-codex-runtime-status').textContent.includes('未发现匹配'));assert($('#center-codex-runtime-status').textContent.includes('no exact matching'));
  runtimeAvailable=true;$('#center-codex-runtime-check').click();await until(()=>$('#center-codex-runtime-status').textContent.includes('发现匹配的已验证'));
  runtimeGate=deferred();$('#center-codex-runtime-check').click();await until(()=>$('#center-codex-runtime-status').textContent.includes('正在检查'));$('#center-codex-version').dispatchEvent(new w.Event('input'));runtimeGate.resolve({nativeAvailable:true,reason:'STALE_CACHE_RESULT'});runtimeGate=null;await flush();assert.equal($('#center-codex-runtime-status').textContent,'','a stale readiness response must not describe a different version');
  $('#center-codex-runtime-mode').value='base';$('#center-codex-runtime-mode').dispatchEvent(new w.Event('change'));assert(!$('#center-codex-runtime-reference').disabled);assert($('#center-codex-runtime-check').hidden);
  const beforeMissingBase=requests.filter(r=>r.route.endsWith('/build')&&r.method==='POST').length;await submit($('#center-codex-version'));assert.equal(requests.filter(r=>r.route.endsWith('/build')&&r.method==='POST').length,beforeMissingBase);assert($('.build-center-dialog .error').textContent.includes('本机基础镜像'));
  $('#center-codex-runtime-reference').value='rundesk-codex:0.163.0';
  assert.equal($('#center-source-npm-registry').value,'https://registry.npmjs.org/');assert(!$('[data-source-type="conda"]'),'unused source type leaked into build form');assert($('[data-source-type="github"]').textContent.includes('未声明可配置'));assert(!$('#center-source-github-archiveBase'));
  $('#center-source-group').value='china';$('#center-source-group').dispatchEvent(new w.Event('change'));assert.equal($('#center-source-npm-registry').value,'https://mirror.company.example/npm/');
  // Per-type choices can mix the groups, and a manually edited address is sent as an override.
  $('#center-source-pip').value='pypi';$('#center-source-pip').dispatchEvent(new w.Event('change'));assert.equal($('#center-source-pip-indexUrl').value,'https://pypi.org/simple/');
  $('#center-source-npm-registry').value='https://build.company.example/npm/';$('#center-source-npm-registry').dispatchEvent(new w.Event('input'));
  const form=$('#center-codex-version').closest('form');await Promise.all([form.onsubmit({preventDefault(){}}),form.onsubmit({preventDefault(){}})]);await until(()=>!!$('#center-build-log'));assert.equal(requests.filter(row=>row.route.endsWith('/build')&&row.method==='POST').length,1);assert($('#center-build-log').textContent.includes('real frozen build log'));
  const firstBuild=requests.find(row=>row.route.endsWith('/build')&&row.method==='POST').body;assert.deepEqual(firstBuild.codexRuntime,{mode:'base',reference:'rundesk-codex:0.163.0'});const submitted=firstBuild.buildSources;assert.equal(submitted.group,'china');assert.equal(submitted.selections.pip,'pypi');assert.equal(submitted.overrides.npm.registry,'https://build.company.example/npm/');assert(!submitted.overrides.github);assert(!submitted.overrides.conda);assert($('.build-frozen-sources').textContent.includes('https://build.company.example/npm/'));

  await w.RunDeskBuildCenter.open('recipes');$('[data-center-action="sources"]').click();await until(()=>!!$('#center-sources-json')&&!$('.build-center-dialog button[type=submit]').disabled);
  const defaults=JSON.parse($('#center-sources-json').value);defaults.defaultGroup='china';$('#center-sources-json').value=JSON.stringify(defaults);await submit($('#center-sources-json'));assert.equal(db.catalog.defaultGroup,'china');$('.build-center-dialog').close();
  $('[data-recipe-build="builtin-video-build"]').click();await until(()=>!!$('#center-source-group')&&!$('.build-center-dialog button[type=submit]').disabled);assert.equal($('#center-source-group').value,'china');assert.equal($('#center-source-npm-registry').value,'https://mirror.company.example/npm/');$('.build-center-dialog').close();
  await w.RunDeskBuildCenter.open('builds',db.builds[0].id);
  const original=db.builds[0].id; $('[data-center-action="rebuild"]').click(); await until(()=>db.builds[0].id!==original&&$('#center-build-log')); assert(requests.some(row=>row.route==='/build-center/builds/'+original+'/rebuild')); assert.notEqual(db.builds[0].id,original);
  // Model, source-code and Conda downloads use the same declared-argument UI.
  const oldCatalog=copy(db.catalog), oldCapabilities=copy(sourceCapability.types);
  db.catalog.types.push({id:'huggingface',name:'Hugging Face',fields:[{id:'endpoint',label:'Hub endpoint',argument:'RD_HF_ENDPOINT',kind:'url'}]});
  db.catalog.types.find(t=>t.id==='github').fields.push({id:'rawBase',label:'Raw files',argument:'RD_GITHUB_RAW_BASE',kind:'url'});
  db.catalog.groups[0].sources.push({id:'conda-official',name:'conda-forge',type:'conda',values:{channels:'https://conda.anaconda.org/conda-forge'}},{id:'hf-official',name:'Hugging Face',type:'huggingface',values:{endpoint:'https://huggingface.co'}},{id:'github-official',name:'GitHub',type:'github',values:{archiveBase:'https://codeload.github.com',rawBase:'https://raw.githubusercontent.com'}});
  db.catalog.groups[1].sources.push({id:'conda-cn',name:'Conda 镜像',type:'conda',values:{channels:'https://mirror.example/conda-forge'}},{id:'hf-cn',name:'HF 镜像',type:'huggingface',values:{endpoint:'https://hf-mirror.com'},description:'第三方模型下载镜像'},{id:'github-enterprise',name:'企业源码代理',type:'github',values:{archiveBase:''},requiresConfiguration:true});
  sourceCapability.types=[{type:'conda',detected:true,supported:true,arguments:['RD_CONDA_CHANNELS']},{type:'huggingface',detected:true,supported:true,arguments:['RD_HF_ENDPOINT']},{type:'github',detected:true,supported:true,arguments:['RD_GITHUB_ARCHIVE_BASE']}];
  await w.RunDeskBuildCenter.open('recipes');$('[data-recipe-build="builtin-video-build"]').click();await until(()=>!!$('#center-source-huggingface-endpoint')&&!$('.build-center-dialog button[type=submit]').disabled);$('#center-codex-version').value='0.163.0';
  assert.equal($('#center-source-huggingface-endpoint').value,'https://hf-mirror.com');assert($('[data-source-type="huggingface"]').textContent.includes('第三方'));assert(!$('#center-source-github-rawBase'),'an undeclared GitHub argument must not be offered');
  const beforeSourceBuilds=requests.filter(r=>r.route.endsWith('/build')&&r.method==='POST').length;await submit($('#center-codex-version'));assert.equal(requests.filter(r=>r.route.endsWith('/build')&&r.method==='POST').length,beforeSourceBuilds);assert($('.build-center-dialog .error').textContent.includes('填写下载地址'));
  $('#center-source-github-archiveBase').value='https://git-download.company.example/archives';$('#center-source-huggingface-endpoint').value='https://models.company.example';await submit($('#center-codex-version'));await until(()=>!!$('#center-build-log'));
  const sourceBuild=requests.filter(r=>r.route.endsWith('/build')&&r.method==='POST').at(-1).body.buildSources;assert.equal(sourceBuild.overrides.huggingface.endpoint,'https://models.company.example');assert(sourceBuild.overrides.conda.channels.includes('conda-forge'));assert(!('rawBase' in sourceBuild.overrides.github));
  db.catalog=oldCatalog;sourceCapability.types=oldCapabilities;
  await w.RunDeskBuildCenter.open('images',db.images[0].key); const beforeCheck=findButton($('.image-detail'),'核对镜像'); beforeCheck.click(); await until(()=>checkCount===1&&!beforeCheck.isConnected&&!!$('[data-center-action="launch"]'));
  $('[data-center-action="launch"]').click(); await until(()=>!!$('#center-launch-name')); await submit($('#center-launch-name')); assert.equal(launchCount,1); assert($('.build-center-dialog').textContent.includes('dedicated-work')); assert($('.build-center-dialog').textContent.includes('exited')); assert($('.build-center-dialog').textContent.includes('container exited'),'actual failed/exited runtime must remain visible'); $('.build-center-dialog').close();
  launchError=true; $('[data-center-action="launch"]').click(); await until(()=>!!$('#center-launch-name')); $('#center-launch-name').value='Uncertain instance'; await submit($('#center-launch-name')); assert($('.build-center-dialog .error').textContent.includes('partial-instance')); $('.build-center-dialog').close(); $('[data-center-action="launch"]').click(); await until(()=>!!$('#center-launch-name')); assert.equal($('#center-launch-name').value,'Uncertain instance'); assert($('#center-launch-name').disabled); await submit($('#center-launch-name')); const uncertain=requests.filter(row=>row.route.endsWith('/launch')).slice(-2); assert.equal(uncertain[0].body.requestId,uncertain[1].body.requestId,'409 unconfirmed launch lost its receipt'); findButton($('.build-center-dialog'),'开始另一实例请求').click(); await until(()=>!$('#center-launch-name').disabled); $('.build-center-dialog').close(); launchError=false;
  $('[data-center-test="smoke"]').click(); await until(()=>!!$('#center-build-log')&&$('#build-center-page').textContent.includes('declared command failed')); assert($('#build-center-page').textContent.includes('失败'));
  await w.RunDeskBuildCenter.open('images',db.images[0].key); $('[data-center-test="offline"]').click(); await until(()=>!!$('#center-build-log')&&$('#build-center-page').textContent.includes('Offline：'));
  // Public catalog keeps edits on CAS conflict and never invents published addresses.
  await w.RunDeskBuildCenter.publicImages();assert.deepEqual(JSON.parse($('#center-public-images-json').value).items,[]);
  $('#center-public-images-json').value=JSON.stringify({schemaVersion:1,items:[publicEntry]});const publicDraft=$('#center-public-images-json').value;
  publicCatalogConflict=true;await submit($('#center-public-images-json'));assert.equal($('#center-public-images-json').value,publicDraft);assert($('.build-center-dialog .error').textContent.includes('你的编辑仍保留'));
  publicCatalogConflict=false;await submit($('#center-public-images-json'));assert.equal(db.publicCatalog.items[0].sources[0].reference,publicEntry.sources[0].reference);$('.build-center-dialog').close();
  // Publication is gated by evidence; an uncertain submission is replayed after dialog reopen.
  await w.RunDeskBuildCenter.open('images',db.images[0].key);await until(()=>!!$('[data-center-action="publish-image"]'));assert($('[data-center-action="publish-image"]').disabled);assert($('.image-detail').textContent.includes('Smoke must pass'));
  publicationReady=true;await w.RunDeskBuildCenter.open('images',db.images[0].key);await until(()=>!!$('[data-center-action="publish-image"]')&&!$('[data-center-action="publish-image"]').disabled);$('[data-center-action="publish-image"]').click();
  $('#center-publication-target').value='ghcr.io/example/news2douyin:1.1.2';await submit($('#center-publication-target'));assert.equal(db.publications.length,0);assert($('.build-center-dialog .error').textContent.includes('请确认上传'));
  $('#center-publication-confirm').checked=true;await submit($('#center-publication-target'));assert($('.build-center-dialog .error').textContent.includes('response lost'));assert($('#center-publication-target').disabled);$('.build-center-dialog').close();
  $('[data-center-action="publish-image"]').click();assert.equal($('#center-publication-target').value,'ghcr.io/example/news2douyin:1.1.2');assert($('#center-publication-target').disabled);await submit($('#center-publication-target'));await until(()=>!!$('#center-publication-log'));
  const publicationPosts=requests.filter(r=>r.route==='/build-center/publications'&&r.method==='POST');assert.equal(publicationPosts.length,2);assert.equal(publicationPosts[0].body.requestId,publicationPosts[1].body.requestId);assert.equal(db.publications.length,1);assert(!$('#center-publication-log').textContent.includes('do-not-display'));assert($('#center-publication-log').textContent.includes('[redacted]'));
  assert.equal(w.location.hash,'#build-center/publications/publication-1');$('[data-center-action="cancel-publication"]').click();await until(()=>$('#center-publication-status')?.textContent.includes('已取消'));assert.equal(db.publications[0].status,'canceled');
  Object.assign(db.publications[0],{status:'succeeded',digest:'sha256:'+'c'.repeat(64),reference:publicEntry.sources[0].reference,publicImage:publicEntry});await w.RunDeskBuildCenter.open('publications','publication-1');
  assert(![...$('#center-publication-detail').childNodes].some(node=>node.nodeType===3&&node.textContent.trim()==='null'),'optional publication fields must not render null text');
  assert($('#center-publication-visibility').textContent.includes('匿名访问尚未验证'));assert.equal($('#center-publication-reference').value,publicEntry.sources[0].reference);assert($('#center-publication-catalog-entry').textContent.includes('news2douyin'));
  assert($('[data-center-action="publication-catalog-draft"]').disabled);$('[data-center-action="check-publication-access"]').click();await until(()=>$('#center-publication-detail').textContent.includes('anonymous denied'));assert($('[data-center-action="publication-catalog-draft"]').disabled);
  publicCheckAllowed=true;$('[data-center-action="check-publication-access"]').click();await until(()=>$('#center-publication-visibility')?.textContent.includes('检查已通过'));assert(!$('[data-center-action="publication-catalog-draft"]').disabled);
  $('[data-center-action="publication-catalog-draft"]').click();await until(()=>!!$('#center-public-images-json')&&!$('#center-public-images-save').disabled);assert($('.build-center-dialog [role=status]').textContent.includes('同名版本'));$('.build-center-dialog').close();
  const publicSaves=requests.filter(r=>r.route==='/setup/public-images'&&r.method==='PUT');assert.equal(publicSaves.length,2,'viewing publication must not save catalog automatically');
  await w.RunDeskBuildCenter.open('images',db.images[0].key); $('[data-center-action="delete-image"]').click(); await until(()=>deleted); await until(()=>$('.image-detail').textContent.includes('deleted')); assert($('[data-center-action="launch"]').disabled); assert($('[data-center-action="delete-image"]').disabled); assert(!requests.some(row=>row.route.includes('/prune')));
  // A late image response cannot replace a newer selection.
  db.images=[image('a'.repeat(24),'late:1'),image('b'.repeat(24),'current:2')]; imageGate=deferred(); const pending=w.RunDeskBuildCenter.open('images',db.images[0].key); await until(()=>$('[data-center-image-key="'+db.images[1].key+'"]')?.textContent.includes('current:2')); findButton($('[data-center-image-key="'+db.images[1].key+'"]'),'current:2').click(); await until(()=>$('.image-detail h2')?.textContent==='current:2'); imageGate.resolve(copy(db.images[0])); await pending; imageGate=null; assert.equal($('.image-detail h2').textContent,'current:2');
  // A response from an abandoned page must not repaint another tab.
  recipeGate=deferred(); const stale=w.RunDeskBuildCenter.open('recipes'); await flush(); await w.RunDeskBuildCenter.open('images',db.images[1].key); recipeGate.resolve({items:copy(db.recipes)}); await stale; recipeGate=null; assert($('.image-detail')); assert.equal($$('[data-recipe-id]').length,0);
  $('[data-center-action="assistant"]').click(); await until(()=>!!$('#center-assistant-instance option')); assert($('#center-assistant-instance option[value="kun"]'));  $('#center-assistant-prompt').value='Add a media tool'; await submit($('#center-assistant-prompt')); assert($('.build-center-dialog').textContent.includes('task-1')); assert($('.build-center-dialog a[href="/?session=session-1"]')); const poll=[...timers].find(([,value])=>value.delay===2500); timers.delete(poll[0]); await poll[1].fn(); await until(()=>$('.build-center-dialog').textContent.includes('completed')); $('.build-center-dialog').close();
  assert(!JSON.stringify(requests.filter(row=>!(row.body instanceof w.FormData))).includes('hostadminToken'));
  w.RunDeskBuildCenter.close(); assert.equal(state.view,'conversation'); assert.equal(w.location.hash,'#assistant');
  w.history.replaceState(null,'','#build-center/recipes'); await w.restoreProductRoute(); assert.equal(state.view,'build-center'); w.RunDeskBuildCenter.close(); assert.equal(w.location.hash,'#assistant','deep link return must leave the build center');
  // Flat configuration tabs and truthful plugin states, using shipped renderSettings.
  await w.showCapabilityPage('plugins'); assert.equal(state.settingsTab,'plugins'); assert($('.native-plugin-row').textContent.includes('配置已启用')); assert($('.native-plugin-row').textContent.includes('加载状态未知')); assert(!$('.native-plugin-row').textContent.includes('安装成功')); assert.equal($$('#capability-page nav[aria-label="能力分类"] button').length,3);
  assert(!$$('#capability-content button').some(node=>/安装|卸载/.test(node.textContent)),'read-only plugin view offered mutation');
  setFile('#native-plugin-zip','plugin.zip'); await submit($('#native-plugin-zip')); assert($('.plugin-inspection-result').textContent.includes('静态检查完成')); assert($('.plugin-inspection-result').textContent.includes('未执行'));
  await w.showCapabilityPage('skills'); const pluginRow=$$('.skill-row').find(row=>row.textContent.includes('editor@example')); assert(pluginRow); assert(pluginRow.querySelector('input[type=checkbox]').disabled); assert(!findButton(pluginRow,'编辑')); assert(!findButton(pluginRow,'移除')); assert(findButton(pluginRow,'用于下次消息'));
  const origins=w.RunDeskNativePlugins.mcpOrigins({componentOrigins:{media:{type:'native_plugin',pluginId:'editor@example'}},status:{data:[{name:'media',pluginId:'editor@example'}]}}); assert.equal(origins.querySelectorAll('.skill-row').length,1); assert(origins.textContent.includes('由插件管理'));
  // A plugin response arriving after tab switch is ignored.
  pluginGate=deferred(); state.settingsTab='plugins'; const host=w.document.createElement('div'); $('#capability-content').replaceChildren(host); const old=w.RunDeskNativePlugins.render(host); state.settingsTab='skills'; pluginGate.resolve({backend:'codex',plugins:[{id:'stale'}],warnings:[]}); await old; assert.equal(host.children.length,0); pluginGate=null;
  assert(!requests.some(row=>/plugin\/(list|install|uninstall)|\/plugins\/(install|uninstall)/.test(row.route)));
  assert(!fs.readFileSync(path.join(root,'build-center.js'),'utf8').includes('workflowId'));
  console.log('build center / native plugin shipped JS behavior: passed');
})().catch(error=>{console.error(error);console.error(requests.slice(-8).map(row=>({route:row.route,method:row.method})));console.error($('#toast').textContent);process.exitCode=1;}).finally(()=>dom.window.close());
