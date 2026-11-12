// Real runtime view in jsdom; interaction/state tests, not browser layout.
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const {JSDOM}=require('jsdom');
const source=fs.readFileSync(require('node:path').join(__dirname,'../internal/web/manage.js'),'utf8');
const start=source.indexOf('async function renderRuntime(target) {'),end=source.indexOf('\n$("#session-search")',start);
assert(start>=0&&end>start);
function fixture(kind='codex',lang='zh'){
 const dom=new JSDOM('<!doctype html><html lang="'+lang+'"><body><main id="target"></main><datalist id="models"></datalist></body></html>');
 const document=dom.window.document,target=document.querySelector('#target'),requests=[];
 let current={id:'default',agentRuntime:{kind},execution:{mode:'local'}},work={id:'work',path:'/fixture/work'};
 const state={selection:1,settingsTab:'runtime'};
 const el=(tag,attrs={},...children)=>{const node=document.createElement(tag);for(const[k,v]of Object.entries(attrs)){if(k==='class')node.className=v;else if(k.startsWith('on'))node[k]=v;else node.setAttribute(k,v);}for(const child of children.flat())if(child!=null)node.append(child instanceof dom.window.Node?child:String(child));return node;};
 const button=(text,fn,cls='')=>el('button',{type:'button',class:cls,onclick:fn},text);
 const pending=[];
 const context=vm.createContext({document,state,el,button,rdText:x=>x,ws:()=>work,instance:()=>current,json:x=>JSON.stringify(x,null,2),configPath:(wid,iid)=>suffix=>`/workspaces/${wid}${suffix}?instanceId=${iid}`,api:path=>{requests.push(path);return new Promise((resolve,reject)=>pending.push({resolve,reject}));},$:s=>document.querySelector(s),toast(){},rdFormat:x=>x,instanceRuntimeCard:async()=>el('div')});
 vm.runInContext(source.slice(start,end),context);
 return {context,target,state,requests,pending,document,switch(){current={id:'other'};state.selection++;},switchWorkspace(){work={id:'other-work',path:'/fixture/other'};}};
}
const tick=()=>new Promise(resolve=>setImmediate(resolve));
const saved=(model,meta={})=>({config:{model},layers:[],origins:{},snapshot:{source:'snapshot',syncedAt:'2026-11-08T12:00:00Z',lastAttemptAt:'2026-11-08T12:00:00Z',stale:false,refreshIntervalSeconds:300,...meta}});
(async()=>{
 const f=fixture();await f.context.renderRuntime(f.target);
 const read=f.document.querySelector('#runtime-config-read'),raw=f.document.querySelector('#runtime-config-value'),status=f.document.querySelector('#runtime-config-status');
 assert.equal(f.requests.length,1,'opening the Codex runtime tab must read its local snapshot');
 assert.equal(f.requests[0],'/workspaces/work/config?instanceId=default&view=snapshot');
 assert(read.disabled);assert.match(status.textContent,/本地配置快照/);assert.equal(read.textContent,'立即同步');
 await read.onclick();assert.equal(f.requests.length,1,'repeated activation must share the pending read');
 f.pending.shift().resolve(saved('local-model'));await tick();
 assert(!read.disabled);assert.match(raw.textContent,/local-model/);assert(!raw.textContent.includes('syncedAt'),'metadata belongs in the status, not the native configuration JSON');assert.match(status.textContent,/上次同步/);assert(!status.classList.contains('error'));
 const refresh=read.onclick();assert.equal(f.requests.at(-1),'/workspaces/work/config?instanceId=default&view=snapshot&refresh=true');assert(read.disabled);assert.match(status.textContent,/正在同步/);assert.match(raw.textContent,/local-model/,'refresh must keep the available snapshot visible');
 f.pending.shift().resolve(saved('local-model',{stale:true,errorCode:'sync_failed',lastAttemptAt:'2026-11-08T12:05:00Z'}));await refresh;
 assert(!read.disabled);assert.match(status.textContent,/同步失败/);assert(status.classList.contains('error'));assert.match(raw.textContent,/local-model/);
 const failed=read.onclick();f.pending.shift().reject(Error('fixture timeout'));await failed;
 assert(!read.disabled);assert.match(status.textContent,/同步失败/);assert.match(status.textContent,/fixture timeout/);assert.match(raw.textContent,/local-model/,'failed synchronization must preserve the explicitly stale previous configuration');
 const retry=read.onclick();f.pending.shift().resolve(saved('recovered',{source:'live'}));await retry;assert.match(raw.textContent,/recovered/);assert(!status.classList.contains('error'));
 const stale=read.onclick();f.switch();f.pending.shift().resolve({config:{model:'old-agent'}});await stale;
 assert(!raw.textContent.includes('old-agent'),'old context result must not overwrite a changed Agent');
 const g=fixture();await g.context.renderRuntime(g.target);g.target.remove();g.pending.shift().reject(Error('detached failure'));await tick();assert(!g.target.textContent.includes('detached failure'));
 const h=fixture();await h.context.renderRuntime(h.target);h.pending.shift().reject(Error('first read failed'));await tick();assert.equal(h.document.querySelector('#runtime-config-value').textContent,'');assert.match(h.document.querySelector('#runtime-config-status').textContent,/读取失败/);assert(!h.document.querySelector('#runtime-config-read').disabled);
 const empty=fixture();await empty.context.renderRuntime(empty.target);empty.pending.shift().resolve({config:{},layers:[],origins:{},snapshot:{source:'snapshot',syncedAt:'',lastAttemptAt:'',stale:true,errorCode:'not_synced',refreshIntervalSeconds:300}});await tick();
 const emptyStatus=empty.document.querySelector('#runtime-config-status'),emptyRead=empty.document.querySelector('#runtime-config-read'),emptyRaw=empty.document.querySelector('#runtime-config-value');
 assert.equal(empty.requests.length,1,'a missing snapshot must never trigger an automatic live read');assert.equal(emptyRaw.textContent,'');assert.match(emptyStatus.textContent,/暂无配置快照/);assert(!emptyStatus.classList.contains('error'),'an unsynchronized Agent is not a failed synchronization');assert(!emptyStatus.textContent.includes('同步时间未知'));assert(!emptyRead.disabled);
 const initialSync=emptyRead.onclick();assert.equal(empty.requests.at(-1),'/workspaces/work/config?instanceId=default&view=snapshot&refresh=true');empty.pending.shift().reject(Error('native unavailable'));await initialSync;assert.equal(emptyRaw.textContent,'');assert.match(emptyStatus.textContent,/读取失败/);assert(!emptyRead.disabled);
 const initialRetry=emptyRead.onclick();empty.pending.shift().resolve(saved('first-live',{source:'live'}));await initialRetry;assert.match(emptyRaw.textContent,/first-live/);assert(!emptyStatus.classList.contains('error'));
 const saveFailure=emptyRead.onclick();empty.pending.shift().resolve(saved('new-unsaved-live',{source:'live',stale:true,errorCode:'snapshot_save_failed'}));await saveFailure;assert.match(emptyRaw.textContent,/new-unsaved-live/);assert.match(emptyStatus.textContent,/已读取当前配置，但本地快照保存失败/);assert(!emptyStatus.textContent.includes('显示上次保存'));assert(emptyStatus.classList.contains('error'));assert(!emptyRead.disabled);
 for(const change of [value=>value.switchWorkspace(),value=>value.state.settingsTab='skills']){const changed=fixture();await changed.context.renderRuntime(changed.target);change(changed);changed.pending.shift().resolve(saved('foreign-context'));await tick();assert(!changed.target.textContent.includes('foreign-context'));}
 const kun=fixture('kun');await kun.context.renderRuntime(kun.target);assert.equal(kun.requests.length,0,'Kun config must remain an explicit native read');const kunRead=kun.document.querySelector('#runtime-config-read');assert.equal(kunRead.textContent,'读取配置');const manual=kunRead.onclick();assert.equal(kun.requests[0],'/workspaces/work/config?instanceId=default');kun.pending.shift().resolve({config:{model:'maintenance'}});await manual;assert.match(kun.document.querySelector('#runtime-config-value').textContent,/maintenance/);
 const english=fixture('codex','en');await english.context.renderRuntime(english.target);english.pending.shift().resolve(saved('english',{stale:true}));await tick();assert.equal(english.document.querySelector('#runtime-config-read').textContent,'Synchronize now');assert.match(english.document.querySelector('#runtime-config-status').textContent,/snapshot needs updating/);
 const englishEmpty=fixture('codex','en');await englishEmpty.context.renderRuntime(englishEmpty.target);englishEmpty.pending.shift().resolve(saved('',{syncedAt:'',lastAttemptAt:'',stale:true,errorCode:'not_synced'}));await tick();assert.match(englishEmpty.document.querySelector('#runtime-config-status').textContent,/No configuration snapshot yet/);assert.equal(englishEmpty.document.querySelector('#runtime-config-value').textContent,'');
 console.log(JSON.stringify({passed:true,checks:['automatic disk-only snapshot read with no UI polling timers','single pending activation','timestamp and redacted-view metadata status','forced synchronization keeps previous data visible','failed sync reports retained stale configuration','explicit retry recovers','Agent/workspace/settings context guards','detached target guard','first read failure enables retry','missing snapshot stays neutral until explicit native sync','first sync failure and retry recover without empty JSON masquerading as config','failed persistence keeps freshly read native config visible with accurate status','Kun remains manual native read','English snapshot and missing-snapshot status'],scope:'Real runtime view in jsdom; no native Codex or visual-layout claims'}));
})().catch(e=>{console.error(e);process.exitCode=1;});
