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
    if (route === '/applications/video-app/project-environments' || route === '/applications/video-app/keys') return [];
    if (route === '/applications/video-app/runtime-presets') return copy(db.applications[0].runtimePresets);
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
load('mcp-policy.js'); load('plugins.js'); load('environments.js'); load('instances.js'); load('workspace-conversations.js');
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
  assert(!$('#nav-instances') && !$('#instances-page'), 'daily navigation still exposes the instance catalog');
  const envCalls=[];
  w.RunDeskBuildCenter.openEnvironment = async (iid,wid,tab) => {
    w.RunDeskInstances.select(iid,wid); w.setProductPage('build-center');
    w.history.replaceState(null,'','#build-center/environment/'+iid+'?workspace='+wid+'&tab='+tab);
    envCalls.push({iid,wid,tab});
    if(tab==='skills'||tab==='mcp')await w.openSettings(tab);
  };
  w.history.replaceState(null,'','#instances');await w.restoreProductRoute();
  assert.deepEqual(w.buildCenterOpen,['workbench']);
  checks.push('legacy inventory reaches build workbench without a separate catalog');

  w.history.replaceState(null,'','#instance/video/settings/mcp?workspace=w-b');await w.restoreProductRoute();
  assert.deepEqual(envCalls.at(-1),{iid:'video',wid:'w-b',tab:'mcp'});
  assertTarget('video','w-b');assert($('#settings').open);
  assert(requests.some(row=>row.route==='/workspaces/w-b/mcp?instanceId=video'));
  w.closeSettings();
  w.history.replaceState(null,'','#build-center/environment/other?workspace=w-other&tab=skills');await w.restoreProductRoute();
  assertTarget('other','w-other');
  assert(requests.some(row=>row.route==='/workspaces/w-other/skills?instanceId=other'));
  w.closeSettings();checks.push('old and canonical settings links retain model/workspace scope');

  w.history.replaceState(null,'','#instance/default?workspace=w-default');await w.restoreProductRoute();
  assert.equal(state.view,'conversation');assertTarget('default','w-default');assert($('#settings').open);w.closeSettings();
  checks.push('legacy native default opens daily settings');

  w.history.replaceState(null,'','#instance/preset?workspace=w-p');await w.restoreProductRoute();
  assert.equal(state.view,'application');assert.equal(w.location.hash,'#application/video-app/preset/p1/settings');assertTarget('preset','w-p');
  w.history.replaceState(null,'','#instance/disabled/conversation?workspace=w-off');await w.restoreProductRoute();
  assert.equal(state.view,'application');assert.equal(w.location.hash,'#application/video-app/preset/off/settings');
  checks.push('application-owned and disabled presets open settings with exact identities');

  w.history.replaceState(null,'','#instance/video/conversation?workspace=w-b');await w.restoreProductRoute();
  assert.equal(state.view,'conversation');assertTarget('video','w-b');await w.newSession();
  const created=requests.filter(row=>row.method==='POST'&&row.route==='/sessions').at(-1);
  assert.equal(created.body.instanceId,'video');assert.equal(created.body.workspaceId,'w-b');
  const writes=requests.filter(row=>row.method!=='GET').length;
  await assert.rejects(()=>w.RunDeskInstances.legacy('other','w-p'),/不能用于/);
  assert.equal($('#instance').value,'');assert.equal($('#workspace').value,'');assert.equal(requests.filter(row=>row.method!=='GET').length,writes);
  checks.push('historical explicit conversation route preserves identity and foreign workspace is rejected');

  heldInstances=deferred();const opening=w.RunDeskInstances.legacy('video','w-b');await tick();
  w.resetConversation();w.setProductPage('tasks');heldInstances.resolve(copy(db.instances));await opening;heldInstances=null;
  assert.equal(state.view,'tasks');checks.push('stale environment request cannot steal navigation');
  console.log(JSON.stringify({passed:true,checks}));dom.window.close();
})().catch(error => { console.error(error); dom.window.close(); process.exitCode=1; });
