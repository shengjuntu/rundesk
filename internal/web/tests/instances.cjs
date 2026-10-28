/* Instance navigation regression tests against shipped JS, with a fixture API.
 * Run: NODE_PATH=<directory containing jsdom> node internal/web/tests/instances.cjs
 * No container, external account or model is started by this test. */
'use strict';
const assert = require('node:assert/strict'), fs = require('node:fs'), path = require('node:path'), vm = require('node:vm');
const {JSDOM} = require('jsdom'), {webcrypto} = require('node:crypto'), {TextEncoder} = require('node:util');
const root = path.resolve(__dirname, '..'), copy = value => JSON.parse(JSON.stringify(value));
const dom = new JSDOM(fs.readFileSync(path.join(root, 'index.html'), 'utf8'), {url:'http://localhost/#instances',runScripts:'outside-only'});
const w = dom.window, ctx = dom.getInternalVMContext(), $ = selector => w.document.querySelector(selector);
Object.defineProperty(w, 'crypto', {value:webcrypto}); w.TextEncoder = TextEncoder;
w.rdText = value => value; w.rdFormat = (value, ...args) => value.replace(/\$\{(\d+)\}/g, (_, n) => args[+n]);
w.setInterval = () => 0; w.confirm = () => true;
w.HTMLDialogElement.prototype.showModal = function () { this.open = true; };
w.HTMLDialogElement.prototype.close = function () { this.open = false; this.dispatchEvent(new w.Event('close')); };
const load = (file, transform = value => value) => vm.runInContext(transform(fs.readFileSync(path.join(root, file), 'utf8')), ctx, {filename:file});
load('app.js');
const state = vm.runInContext('state', ctx);
const db = {
  instances:['default','video','other','preset','disabled','kun','empty'].map(id => ({id,name:id,description:'fixture '+id,codexHome:'/fixture/'+id,revision:1,agentRuntime:{kind:id === 'kun' ? 'kun' : 'codex',model:'fixture-model'},execution:{mode:['default','kun'].includes(id) ? 'local' : 'docker',image:'fixture:1'}})),
  workspaces:['w-default','w-a','w-b','w-other','w-p','w-off','w-free'].map(id => ({id,name:id,path:'/fixture/'+id,revision:1})),
  applications:[{appId:'video-app',name:'Video App',instanceId:'video',workspaceId:'w-a',runtimePresets:[{id:'p1',name:'Preset',instanceId:'preset',workspaceId:'w-p',enabled:true},{id:'off',name:'Disabled',instanceId:'disabled',workspaceId:'w-off',enabled:false}]}],
  environments:[['video','w-a'],['video','w-b'],['other','w-other'],['preset','w-p'],['disabled','w-off']].map(([instanceId,workspaceId], n) => ({id:'env-'+n,instanceId,workspaceId,state:'exited',codexHome:'/fixture/env-'+n+'/codex',spec:{mode:'docker',image:'fixture:1'},activeConnections:0})),
  sessions:[],
};
const requests = [], checks = [];
let heldInstances, heldConfiguration;
w.RunDeskDiagnostics = {recent:() => null,request:async (url, options = {}) => {
  const route = url.replace('/api/v1',''), method = options.method || 'GET', body = typeof options.body === 'string' ? JSON.parse(options.body) : options.body;
  requests.push({route,method,body});
  if (method === 'GET') {
    if (route === '/instances') return heldInstances ? heldInstances.promise : copy(db.instances);
    for (const key of ['workspaces','applications','sessions','environments']) if (route === '/'+key) return copy(db[key]);
    if (route.startsWith('/environments?')) { const id = new URL('http://fixture'+route).searchParams.get('instanceId'); return copy(db.environments.filter(row => row.instanceId === id)); }
    const config = route.match(/^\/instances\/([^/]+)\/configuration\?workspaceId=([^&]+)/);
    if (config) return heldConfiguration && route === heldConfiguration.route ? heldConfiguration.promise : {instance:copy(db.instances.find(row => row.id === decodeURIComponent(config[1]))),workspace:copy(db.workspaces.find(row => row.id === decodeURIComponent(config[2]))),sessionCount:0};
    if (/^\/workspaces\/[^/]+\/skills\?instanceId=/.test(route)) return {data:[{skills:[],errors:[]}]};
    if (/^\/workspaces\/[^/]+\/mcp\?instanceId=/.test(route)) return {runtime:'codex',version:'v1',userServers:{},effectiveServers:{},status:{data:[]}};
    if (/^\/instances\/[^/]+\/plugins/.test(route)) return {backend:'codex',readOnly:true,plugins:[],warnings:[]};
    throw Error('Unexpected GET '+route);
  }
  if (method === 'POST' && route === '/sessions') {
    const session = {id:'created-'+(db.sessions.length+1),...body,title:'Fixture session',status:'idle',created:'2026-10-26T00:00:00Z'};
    db.sessions.push(session); return copy(session);
  }
  throw Error('Unexpected mutation '+method+' '+route);
}};
// Leave actual app event handlers and configuration forms intact. Rendering of
// streaming chat content is outside this navigation suite and has its own tests.
w.resetReplyActions = () => {}; w.renderRuntimeBar = () => {}; w.closeConversationSidebar = () => {};
load('applications.js'); load('configuration.js');
load('manage.js', text => text.replace('document.addEventListener("DOMContentLoaded", () => safe(boot));',''));
for (const name of ['renderStatus','renderDebug','renderAttachments']) w[name] = () => {};
w.renderKunSettings = async target => target.replaceChildren(w.el('p',{},'Kun settings'));
w.skillBundleImporter = () => w.el('div');
w.RunDeskNativePlugins = {originNode:() => null,mcpOrigins:() => null};
w.RunDeskTraceUI = {close(){}};
w.RunDeskBuildCenter = {open:async (...args) => { w.buildCenterOpen = args; }};
w.RunDeskImages = {needsUpdate:() => false,overview:async (a,n) => { n.textContent = 'image'; }};
w.RunDeskSetup = {open:async () => {}};
load('mcp-policy.js'); load('plugins.js'); load('environments.js'); load('instances.js');
w.selectSession = async id => { state.session = copy(db.sessions.find(row => row.id === id)); w.renderInstanceOptions(state.session.instanceId); $('#workspace').value = state.session.workspaceId; w.setProductPage('conversation'); };
function sync() {
  for (const key of ['instances','workspaces','applications','sessions']) state[key] = copy(db[key]);
  $('#workspace').replaceChildren(...state.workspaces.map(row => w.el('option',{value:row.id},row.name)));
  w.renderInstanceOptions('default'); $('#workspace').value = 'w-default';
}
const button = (node,text) => [...node.querySelectorAll('button')].find(value => value.textContent.trim() === text);
const tick = () => new Promise(resolve => setImmediate(resolve));
async function until(predicate) { for (let n=0;n<100;n++) { if(predicate())return; await tick(); } assert(predicate(),'async UI did not settle'); }
function assertTarget(iid,wid) { assert.equal($('#instance').value,iid); assert.equal($('#workspace').value,wid); }
function deferred() { let resolve; const promise = new Promise(done => {resolve=done}); return {promise,resolve}; }

(async () => {
  sync();
  await w.RunDeskInstances.open();
  assert.equal(state.view,'instances'); assert.equal(w.location.hash,'#instances');
  button($('#instances-page'),'从镜像添加 Agent').click();assert.deepEqual(w.buildCenterOpen,['images']);
  button($('#instances-page'),'准备 Docker 镜像').click();assert.deepEqual(w.buildCenterOpen,['recipes']);
  for (const id of ['default','video','other','kun']) assert($('#instances-page').textContent.includes(id),'missing instance '+id);
  assert.equal(requests.filter(row => row.method !== 'GET').length,0,'opening instances changed runtime');
  checks.push('all local, Docker and Kun identities appear without application registration or runtime writes');

  // Never inherit another instance\'s globally selected project.
  $('#workspace').value = 'w-other';
  await w.RunDeskInstances.show('video');
  assertTarget('video','w-a');
  await w.RunDeskInstances.show('video','w-b'); assertTarget('video','w-b');
  await w.openSettings('skills');
  assert(requests.some(row => row.route === '/workspaces/w-b/skills?instanceId=video'),'Skills request lost the selected target');
  assert.equal(state.view,'instance');assert($('#settings').open);assert(w.location.hash.includes('workspace=w-b'));
  await w.closeSettings(); assertTarget('video','w-b'); assert.equal(state.view,'instance');
  await w.openSettings('mcp');
  assert(requests.some(row => row.route === '/workspaces/w-b/mcp?instanceId=video'),'MCP request lost the selected target');
  await w.closeSettings();
  checks.push('multiple workspaces retain exact instance/workspace through Skills, MCP and back navigation');

  await w.RunDeskInstances.show('other','w-other');
  assert(![...$('#instances-page').childNodes].some(node=>node.nodeType===3&&node.textContent.trim()==='null'),'an instance without an application rendered a literal null association');
  await w.RunDeskInstances.show('video'); assertTarget('video','w-b');
  checks.push('workspace memory belongs to its instance rather than the last global selection');

  await w.RunDeskInstances.conversation('video','w-b',true);
  assertTarget('video','w-b'); assert.equal(state.view,'conversation');
  assert.equal($('#composer-workspace').value,'w-b');assert($('#composer-agent').textContent.includes('video'));
  $('#composer-workspace').value='w-a';$('#composer-workspace').dispatchEvent(new w.Event('change'));
  await until(() => $('#workspace').value==='w-a' && $('#composer-workspace').value==='w-a');
  await w.RunDeskInstances.conversation('video','w-b',true);
  $('#new-session').click(); await until(() => state.view === 'conversation' && !state.session);
  assertTarget('video','w-b');
  $('#assistant-settings').click(); await until(() => $('#settings').open);
  assertTarget('video','w-b'); $('#settings').close();
  await w.newSession();
  let created = requests.filter(row => row.method === 'POST' && row.route === '/sessions').at(-1);
  assert.equal(created.body.instanceId,'video'); assert.equal(created.body.workspaceId,'w-b');
  checks.push('composer workspace choice, new chat, settings and actual session POST keep the chosen Agent and workspace');

  await w.RunDeskInstances.conversation('preset','w-p',true);
  await w.newSession();
  created = requests.filter(row => row.method === 'POST' && row.route === '/sessions').at(-1);
  assert.equal(created.body.instanceId,'preset'); assert.equal(created.body.workspaceId,'w-p');
  assert.deepEqual(copy(created.body.source),{kind:'application',appId:'video-app',presetId:'p1'});
  const writes = requests.filter(row => row.method !== 'GET').length;
  await w.RunDeskInstances.conversation('disabled','w-off',true);
  assert.equal(state.view,'instance','disabled preset exposed a new-task composer');
  assert.throws(() => w.RunDeskInstances.source('disabled','w-off'));
  assert.throws(() => w.RunDeskInstances.select('preset','w-other'));
  assert.throws(() => w.RunDeskInstances.select('video','missing-workspace'));
  assert.equal(requests.filter(row => row.method !== 'GET').length,writes);
  checks.push('preset session carries exact app and preset source; disabled or mismatched contexts fail without mutations');

  w.history.replaceState(null,'','#instance/video/settings/mcp?workspace=w-b');
  assert.equal(await w.restoreProductRoute(),true);
  assertTarget('video','w-b'); assert.equal(state.view,'instance');assert($('#settings').open);
  await w.closeSettings();
  w.history.replaceState(null,'','#instance/video/conversation?workspace=w-a');
  assert.equal(await w.restoreProductRoute(),true);
  assertTarget('video','w-a'); assert.equal(state.view,'conversation');
  w.history.replaceState(null,'','#instances'); assert.equal(await w.restoreProductRoute(),true);
  checks.push('instance, conversation and capability deep links restore explicit workspace context');

  // The old build launch continuation delegates to the new instance page.
  await w.showLegacyApplication('video','w-b');
  assertTarget('video','w-b'); assert.equal(state.view,'instance');
  checks.push('legacy/build launch instance navigation enters the instance page');

  await w.RunDeskInstances.show('video','missing-workspace');
  assert.equal($('#instance').value,'','failed selection retained a usable previous instance');
  assert.equal($('#workspace').value,'','failed selection retained a usable previous workspace');
  assert($('#settings-button').classList.contains('hidden'));
  await assert.rejects(() => w.newSession());
  checks.push('invalid workspace clears the previous context and cannot submit a task against it');

  await w.RunDeskInstances.show('video','w-a');
  heldConfiguration = {...deferred(),route:'/instances/video/configuration?workspaceId=w-a'};
  const settings = w.openSettings('skills'); await tick();
  await w.RunDeskInstances.show('other','w-other');
  heldConfiguration.resolve({instance:copy(db.instances.find(row=>row.id==='video')),workspace:copy(db.workspaces.find(row=>row.id==='w-a')),sessionCount:0});
  await settings; heldConfiguration = null;
  assert(!requests.some(row=>row.route==='/workspaces/w-other/skills?instanceId=video'),'late Skills response combined an old instance with the newly selected workspace');
  assertTarget('other','w-other');
  checks.push('late Skills configuration resolution cannot combine identities from two selected contexts');

  // A slow inventory response must not bring the user back after leaving it.
  heldInstances = deferred();
  const opening = w.RunDeskInstances.open();
  await tick(); w.resetConversation(); w.setProductPage('tasks');
  heldInstances.resolve(copy(db.instances)); await opening; heldInstances = null;
  assert.equal(state.view,'tasks');
  checks.push('late inventory responses cannot steal navigation');

  const report = {passed:true,checks,scope:'Shipped JavaScript in JSDOM with fixture API; streaming message rendering, Docker and live models are not exercised.'};
  console.log(JSON.stringify(report)); dom.window.close();
})().catch(error => { console.error(error); dom.window.close(); process.exitCode=1; });
