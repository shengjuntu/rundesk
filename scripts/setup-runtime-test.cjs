// Named offline contracts for shipped views; no browser layout claims.
const assert = require('node:assert/strict'), fs = require('node:fs'), vm = require('node:vm'), path = require('node:path');
const {Element, el, button, all, find, byText, field, get, clone} = require('./fixtures/dom.cjs');
const {runSuite} = require('./fixtures/suite.cjs');

async function runRuntimeSettings() {
// Real setup view with a DOM fixture: interaction contracts, not visual layout.
const root=path.resolve(__dirname,'..');
function fixture(runtimePatch={}){
 const storage=new Map(),body=el('body'),select=el('select',{value:'work'}),requests=[];
 let inst={id:'default',revision:2,defaultModel:'',agentRuntime:{kind:'codex',maxSteps:12,allowBash:false,harness:{loopPolicy:'plan-act-v1'},budget:{maxToolCalls:8},...runtimePatch},execution:{mode:'local'}};
 let setup={settings:{revision:0,completed:false,workspaceId:'work'},instance:inst,codex:'absent-codex',user:'fixture',data:'/tmp/fixture',firstRun:true};
 let environment={ready:false,demo:false,checks:[{id:'codex',required:true,status:'unavailable',detail:'missing'}]},pendingInstall=null;
 const state={workspaces:[{id:'work',name:'Fixture'}],instances:[inst]};
 const api=async(url,options={})=>{requests.push({url,options:clone(options)});if(url==='/setup'&&options.method==='PUT'){assert.equal(options.body.instanceRevision,inst.revision);inst={...inst,revision:inst.revision+1,agentRuntime:options.body.agentRuntime};state.instances=[inst];setup={...setup,settings:{...setup.settings,revision:setup.settings.revision+1,completed:options.body.completed},instance:inst};environment={ready:true,demo:false,checks:[{id:'kun',required:true,status:'ready',detail:'fixture'}]};return {settings:setup.settings};}if(url==='/setup')return clone(setup);if(url.startsWith('/setup/environment'))return clone(environment);if(url==='/instances')return clone(state.instances);if(url==='/setup/codex-install')return new Promise(resolve=>pendingInstall=resolve);throw Error('unexpected '+url)};
 const context=vm.createContext({window:{},document:{body,documentElement:{lang:'zh'}},el,button,api,state,rdText:s=>s,json:JSON.stringify,ws:()=>state.workspaces[0],$:()=>select,updateWorkspaceLabel(){},toast(){},encodeURIComponent,sessionStorage:{getItem:k=>storage.get(k),setItem:(k,v)=>storage.set(k,v)},localStorage:{setItem(){}},prompt(){throw Error('unexpected prompt')}});
 vm.runInContext(fs.readFileSync(root+'/internal/web/setup.js','utf8'),context);
 return {context,body,requests,state,storage,resolveInstall:value=>pendingInstall(value)};
}

 const f=fixture();await f.context.window.RunDeskSetup.maybeOpen();let dialog=f.body.children[0];assert(dialog.open,'missing dependency did not open wizard');
 assert(f.requests.some(x=>x.url.startsWith('/setup/environment')),'startup did not check environment');assert(!f.requests.some(x=>x.options.method==='POST'),'startup ran an action');
 const selector=get(dialog,'setup-runtime');selector.value='kun';selector.emit('change');assert(get(dialog,'setup-install').hidden,'Kun still shows Codex installation');
 get(dialog,'setup-model').value='local-model';get(dialog,'setup-endpoint').value='http://localhost:9000/v1';
 await byText(dialog,'保存配置').click();const save=f.requests.find(x=>x.options.method==='PUT').options.body;
 assert.equal(save.agentRuntime.kind,'kun');assert.equal(save.agentRuntime.model,'local-model');assert.equal(save.agentRuntime.maxSteps,12);assert.equal(save.agentRuntime.harness.loopPolicy,'plan-act-v1');assert.equal(save.agentRuntime.allowBash,false);assert.equal(save.agentRuntime.budget.maxToolCalls,8);assert.equal(save.codex,'absent-codex');
 await byText(dialog,'完成设置').click();assert.equal(f.requests.filter(x=>x.options.method==='PUT').at(-1).options.body.instanceRevision,3);assert.equal(f.body.children.length,0);
 const g=fixture();await g.context.window.RunDeskSetup.open();dialog=g.body.children[0];
 await byText(dialog,'安装指定版本').click();assert(!g.requests.some(x=>x.url==='/setup/codex-install'),'unchecked installation ran');
 get(dialog,'setup-install-version').value='1.2.3';get(dialog,'setup-install-confirm').checked=true;
 const install=byText(dialog,'安装指定版本').click();await Promise.resolve();assert(byText(dialog,'安装指定版本').disabled);await byText(dialog,'完成设置').click();assert(!g.requests.some(x=>x.options.method==='PUT'),'saved during installation');
 g.resolveInstall({ok:true,codex:'/managed/codex',log:'fixture log',error:''});await install;assert.equal(get(dialog,'setup-codex').value,'/managed/codex');assert(!g.requests.some(x=>x.options.method==='PUT'),'installation selected itself');
 await byText(dialog,'关闭').click();await g.context.window.RunDeskSetup.maybeOpen();assert.equal(g.body.children.length,0,'dismissed wizard reopened');
 // The retired everyday Kun editor's configuration assertions now exercise
 // the retained setup save path. No UI policy editor is fabricated here.
 const h=fixture({harness:{loopPolicy:'tool-loop-v1'},allowBash:true,bashApproval:'approve',bashTimeoutSeconds:120});
 await h.context.window.RunDeskSetup.open();const maintenance=h.body.children[0];
 const runtime=get(maintenance,'setup-runtime');runtime.value='kun';runtime.emit('change');
 await byText(maintenance,'保存配置').click();const advanced=h.requests.find(x=>x.options.method==='PUT').options.body.agentRuntime;
 assert.equal(advanced.harness.loopPolicy,'tool-loop-v1');
 assert.equal(Object.keys(advanced.harness).length,1,'setup added stale planning module configuration');
 assert.equal(advanced.allowBash,true);assert.equal(advanced.bashApproval,'approve');assert.equal(advanced.bashTimeoutSeconds,120);
 await byText(maintenance,'关闭').click();
 const proof={version:fs.readFileSync(root+'/internal/app/api_v1.go','utf8').match(/const Version = "([^"]+)"/)[1],passed:true,scope:'real setup JS in Node DOM fixture; browser layout not tested',checks:['startup prerequisite probe without installation/model calls','Kun selection hides optional Codex install','saved configuration preserves advanced Kun settings and carries both revisions','unchecked installation is inert','save blocked during install','installed executable requires explicit save','dismissal respected','explicit advanced Harness preset and Bash grant, approval and timeout survive setup save']};
 return proof;
}

async function runImagePreparation() {
// Exercise real initialization UI code; this is a DOM contract test, not browser layout.
const requests=[],timers=new Map();let jobs=[],timerID=0;
const api=async(url,options={})=>{
 requests.push({url,options});
 if(url==='/setup/runtime-profiles')return {supported:true,items:[{id:'news2douyin',name:'新闻研究',reference:'rundesk-news2douyin:exa-agent-reach'}]};
 if(url==='/instances/default/builds')return structuredClone(jobs);
 if(url==='/setup/runtime-images'){assert.equal(options.body.profileId,'news2douyin');assert.equal(options.body.codexVersion,'0.144.6');const v={id:'fixture',name:'news',scope:'setup',source:options.body.source,profileId:'news2douyin',codexVersion:'0.144.6',reference:options.body.reference,status:'queued'};jobs=[v];return structuredClone(v)}
 if(url.includes('/log?after='))return {text:'',next:0};
 if(url.endsWith('/cancel')){jobs[0].status='canceled';return structuredClone(jobs[0])}
 throw Error('unexpected '+url);
};
const context=vm.createContext({window:{},document:{documentElement:{lang:'zh'}},api,el:(...a)=>new Element(...a),button:(label,click)=>{const b=new Element('button',{},label);b.click=click;return b},setTimeout:(fn)=>{timers.set(++timerID,fn);return timerID},clearTimeout:id=>timers.delete(id)});
vm.runInContext(fs.readFileSync(path.join(__dirname,'../internal/web/setup-images.js'),'utf8'),context);
const flush=async()=>{for(let n=0;n<8;n++)await Promise.resolve()};

 const dialog=new Element('dialog'),panel=context.window.RunDeskRuntimeSetup.panel(dialog);await flush();
 assert(!requests.some(x=>x.options.method==='POST'),'initialization performed an installation');
 assert.equal(get(panel,'runtime-image-reference').value,'rundesk-news2douyin:exa-agent-reach');
 get(panel,'runtime-image-version').value='0.144.6';await byText(panel,'准备镜像').click();assert(byText(panel,'取消准备'));
 assert(timers.size>0);await byText(panel,'取消准备').click();assert(!byText(panel,'取消准备'));assert(byText(panel,'用原配置重试'));
 await byText(panel,'用原配置重试').click();await flush();assert.equal(jobs[0].status,'queued');
 dialog.emit('close');assert.equal(timers.size,0);assert.equal(jobs[0].status,'queued','closing wizard canceled background work');
 jobs[0].status='succeeded';jobs[0].imageId='sha256:fixture';jobs[0].offlineReady=true;
 const reopened=new Element('dialog'),next=context.window.RunDeskRuntimeSetup.panel(reopened);await flush();assert(next.textContent.includes('公网搜索和真实研究尚未验证'));assert(next.textContent.includes('sha256:fixture'));
 reopened.emit('close');return {passed:true,scope:'real setup-images.js in Node DOM fixture; browser layout not tested',checks:['load without install','background submit','cancel','original-configuration retry','closing retains background work and stops polling','reopen shows completed identity','offline and real research readiness stay distinct']};
}

module.exports = {runRuntimeSettings, runImagePreparation};
if (require.main === module) runSuite(__filename, [
  {name: 'runtime-settings', run: runRuntimeSettings, report: 'setup-ui.json'},
  {name: 'image-preparation', run: runImagePreparation, report: 'setup-images-ui.json'},
]).catch(error => {console.error(error); process.exitCode = 1;});
