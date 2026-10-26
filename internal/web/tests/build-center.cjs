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
const requests = [], receipts = new Map(); let uploadLost = true, launchCount = 0, launchError = false, checkCount = 0, deleted = false, imageGate, recipeGate, pluginGate;
const fail = (text, status, code) => Object.assign(new Error(text), {status,code});
function build(id, extra = {}) { return {id,name:'构建 '+id,status:'succeeded',scope:'recipe',recipeId:'builtin-video-build',recipeRevision:1,reference:'video:1',imageId:db.images[0]?.imageId || 'sha256:fixed',contextSha256:'frozen-context',created:'2026-10-20T10:00:00Z',...extra}; }
w.RunDeskDiagnostics = {recent:() => null,request:async (url, options = {}) => {
  const route = url.replace('/api/v1',''), method = options.method || 'GET', body = typeof options.body === 'string' ? JSON.parse(options.body) : options.body;
  requests.push({route,method,body,headers:options.headers});
  if (method === 'GET') {
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
  if (method === 'POST' && route === '/build-center/recipes') {
    assert(body instanceof w.FormData); const manifest = JSON.parse(body.get('manifest')), id = body.get('requestId'); assert.match(id,/^[0-9a-f]{24}$/); assert(body.get('context'));
    let value = receipts.get(id); if (!value) { value = {...manifest,id:'custom-'+id,revision:1,kind:'custom',archived:false,contextSha256:'custom-context',recipeSha:'custom-recipe'}; db.recipes.push(value); receipts.set(id,copy(value)); }
    if (uploadLost) { uploadLost = false; throw fail('response lost'); } return copy(value);
  }
  if (method === 'POST' && /^\/build-center\/recipes\/[^/]+\/build$/.test(route)) {
    assert.match(body.requestId,/^[0-9a-f]{24}$/); assert.equal(body.recipeRevision,1); assert.equal(body.codexVersion,'0.163.0'); const value = build('build-'+(db.builds.length+1)); db.builds.unshift(value); return copy(value);
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
  $('[data-center-action="upload"]').click(); await until(()=>!!$('#center-recipe-name')); $('#center-recipe-name').value='Media'; setFile('#center-recipe-zip'); $('#center-recipe-smoke').value='["python","--version"]';
  await submit($('#center-recipe-name')); assert($('.build-center-dialog .error').textContent.includes('response lost')); await submit($('#center-recipe-name')); await until(()=>$$('[data-recipe-id]').length===7);
  const uploads=requests.filter(row=>row.route==='/build-center/recipes'&&row.method==='POST'); assert.equal(uploads.length,2); assert.equal(uploads[0].body.get('requestId'),uploads[1].body.get('requestId'),'uncertain upload duplicated receipt'); assert.equal(db.recipes.length,7);
  Object.defineProperty(w,'crypto',{value:{getRandomValues:array=>webcrypto.getRandomValues(array)},configurable:true}); $('[data-center-action="upload"]').click(); await until(()=>!!$('#center-recipe-name')); $('#center-recipe-name').value='Plain HTTP tools'; setFile('#center-recipe-zip'); await submit($('#center-recipe-name')); await until(()=>$$('[data-recipe-id]').length===8); Object.defineProperty(w,'crypto',{value:webcrypto,configurable:true});
  $('[data-recipe-build="builtin-video-build"]').click(); await until(()=>!!$('#center-codex-version')); $('#center-codex-version').value='0.163.0'; const form=$('#center-codex-version').closest('form'); await Promise.all([form.onsubmit({preventDefault(){}}),form.onsubmit({preventDefault(){}})]); await until(()=>!!$('#center-build-log')); assert.equal(requests.filter(row=>row.route.endsWith('/build')&&row.method==='POST').length,1); assert($('#center-build-log').textContent.includes('real frozen build log'));
  const original=db.builds[0].id; $('[data-center-action="rebuild"]').click(); await until(()=>db.builds[0].id!==original&&$('#center-build-log')); assert(requests.some(row=>row.route==='/build-center/builds/'+original+'/rebuild')); assert.notEqual(db.builds[0].id,original);
  await w.RunDeskBuildCenter.open('images',db.images[0].key); const beforeCheck=findButton($('.image-detail'),'核对镜像'); beforeCheck.click(); await until(()=>checkCount===1&&!beforeCheck.isConnected&&!!$('[data-center-action="launch"]'));
  $('[data-center-action="launch"]').click(); await until(()=>!!$('#center-launch-name')); await submit($('#center-launch-name')); assert.equal(launchCount,1); assert($('.build-center-dialog').textContent.includes('dedicated-work')); assert($('.build-center-dialog').textContent.includes('exited')); assert($('.build-center-dialog').textContent.includes('container exited'),'actual failed/exited runtime must remain visible'); $('.build-center-dialog').close();
  launchError=true; $('[data-center-action="launch"]').click(); await until(()=>!!$('#center-launch-name')); $('#center-launch-name').value='Uncertain instance'; await submit($('#center-launch-name')); assert($('.build-center-dialog .error').textContent.includes('partial-instance')); $('.build-center-dialog').close(); $('[data-center-action="launch"]').click(); await until(()=>!!$('#center-launch-name')); assert.equal($('#center-launch-name').value,'Uncertain instance'); assert($('#center-launch-name').disabled); await submit($('#center-launch-name')); const uncertain=requests.filter(row=>row.route.endsWith('/launch')).slice(-2); assert.equal(uncertain[0].body.requestId,uncertain[1].body.requestId,'409 unconfirmed launch lost its receipt'); findButton($('.build-center-dialog'),'开始另一实例请求').click(); await until(()=>!$('#center-launch-name').disabled); $('.build-center-dialog').close(); launchError=false;
  $('[data-center-test="smoke"]').click(); await until(()=>!!$('#center-build-log')&&$('#build-center-page').textContent.includes('declared command failed')); assert($('#build-center-page').textContent.includes('失败'));
  await w.RunDeskBuildCenter.open('images',db.images[0].key); $('[data-center-test="offline"]').click(); await until(()=>!!$('#center-build-log')&&$('#build-center-page').textContent.includes('Offline：'));
  await w.RunDeskBuildCenter.open('images',db.images[0].key); $('[data-center-action="delete-image"]').click(); await until(()=>deleted); await until(()=>$('.image-detail').textContent.includes('deleted')); assert($('[data-center-action="launch"]').disabled); assert($('[data-center-action="delete-image"]').disabled); assert(!requests.some(row=>row.route.includes('/prune')));
  // A late image response cannot replace a newer selection.
  db.images=[image('a'.repeat(24),'late:1'),image('b'.repeat(24),'current:2')]; imageGate=deferred(); const pending=w.RunDeskBuildCenter.open('images',db.images[0].key); await until(()=>$('[data-center-image-key="'+db.images[1].key+'"]')?.textContent.includes('current:2')); findButton($('[data-center-image-key="'+db.images[1].key+'"]'),'current:2').click(); await until(()=>$('.image-detail h2')?.textContent==='current:2'); imageGate.resolve(copy(db.images[0])); await pending; imageGate=null; assert.equal($('.image-detail h2').textContent,'current:2');
  // A response from an abandoned page must not repaint another tab.
  recipeGate=deferred(); const stale=w.RunDeskBuildCenter.open('recipes'); await flush(); await w.RunDeskBuildCenter.open('images',db.images[1].key); recipeGate.resolve({items:copy(db.recipes)}); await stale; recipeGate=null; assert($('.image-detail')); assert.equal($$('[data-recipe-id]').length,0);
  $('[data-center-action="assistant"]').click(); await until(()=>!!$('#center-assistant-instance option')); assert.equal($('#center-assistant-instance option[value="kun"]'),null); $('#center-assistant-prompt').value='Add a media tool'; await submit($('#center-assistant-prompt')); assert($('.build-center-dialog').textContent.includes('task-1')); assert($('.build-center-dialog a[href="/?session=session-1"]')); const poll=[...timers].find(([,value])=>value.delay===2500); timers.delete(poll[0]); await poll[1].fn(); await until(()=>$('.build-center-dialog').textContent.includes('completed')); $('.build-center-dialog').close();
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
