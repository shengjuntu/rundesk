// Executes the shipped Hybrid dialog with a DOM stub and controlled HTTP
// responses. Covers interaction semantics; this is not a browser/layout test.
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
class Element{
 constructor(tag,attrs={},...children){this.tagName=tag.toUpperCase();this.attrs=attrs;this.dataset={};this.children=[];this.listeners={};this.value=attrs.value??'';this.disabled=false;this.isConnected=true;this.open=false;this.append(...children);}
 append(...children){for(const n of children.flat()){if(n==null)continue;if(n instanceof Element)n.parent=this;this.children.push(n)}if(this.tagName==='SELECT'&&!this.value)this.value=this.children[0]?.attrs.value??'';}
 replaceChildren(...children){this.children=[];if(this.tagName==='SELECT')this.value='';this.append(...children);}
 set textContent(v){this.children=[v]}get textContent(){return this.children.map(n=>n instanceof Element?n.textContent:String(n??'')).join('')}
 querySelectorAll(q){return this.children.filter(n=>n instanceof Element).flatMap(n=>[...(q.startsWith('#')?n.attrs.id===q.slice(1):q.startsWith('.')?(n.attrs.class||'').split(' ').includes(q.slice(1)):n.tagName===q.toUpperCase())?[n]:[],...n.querySelectorAll(q)])}
 querySelector(q){return this.querySelectorAll(q)[0]||null}
 addEventListener(type,fn){this.listeners[type]=fn}emit(type){this.listeners[type]?.()}
 showModal(){this.open=true}close(){this.open=false;this.onclose?.()}remove(){this.isConnected=false;if(this.parent)this.parent.children=this.parent.children.filter(n=>n!==this)}
}
const el=(...args)=>new Element(...args),button=(label,click)=>{const b=el('button',{},label);b.click=click;return b};
const document={body:el('body'),addEventListener(){},querySelector(q){return this.body.querySelector(q)}};
const deferred=()=>{let resolve;const promise=new Promise(r=>resolve=r);return {promise,resolve}};
const flush=()=>new Promise(r=>setImmediate(r));

const preview=()=>({id:'live',title:'Live <script>literal</script>',hash:'live-hash',origin:{sessionId:'source',runId:'source-run',sequence:5,through:30,mode:'live'},targetSessionId:'live-target',instruction:'new instruction',phase:'after_model',step:1,pending:1,budget:{toolCalls:0,reportedTokens:7},limits:{maxToolCalls:10},maxSteps:10,recordCount:0,live:{workspace:'/project/current',allowWrite:true,mcpTools:2,approvalPolicy:'on-request',pendingTools:[{callId:'old-call',name:'write_file',argumentsPreview:'{"path":"proof.txt","content":"<img literal>"}',truncated:false}]}});
const writes=[],reads=[],selected=[];let delayed=null;
const api=async(p,o={})=>{
 if(o.method==='POST'){writes.push({path:p,body:structuredClone(o.body)});if(p==='/kun-forks')return preview();if(delayed)return delayed.promise;return {id:'live-target'}}
 reads.push(p);if(p.includes('/sources/'))return {selection:{sourceRunId:'source-run',through:30,expectedStateRevision:50,workerEpoch:'epoch'},items:[{sequence:5,phase:'after_model',step:1,pending:1}],hasMore:false,nextOffset:1};
 if(p.includes('?'))return {items:[preview()],hasMore:false,nextOffset:1};return preview();
};
const ctx=vm.createContext({window:{},document,el,button,api,encodeURIComponent,URLSearchParams,JSON,console,state:{},toast(){},refreshSessions:async()=>{},selectSession:async id=>selected.push(id)});
const root=path.resolve(__dirname,'..');for(const file of ['kun-inspect.js','kun-forks.js'])vm.runInContext(fs.readFileSync(root+'/internal/web/'+file,'utf8'),ctx);
const byText=(d,text)=>d.querySelectorAll('button').find(n=>n.textContent===text),field=(d,name)=>[...d.querySelectorAll('input'),...d.querySelectorAll('textarea'),...d.querySelectorAll('select')].find(n=>n.attrs['aria-label']===name);
(async()=>{
 let d=ctx.window.RunDeskKunForks.open({sessionId:'source'});await flush();assert.equal(writes.length,0);
 const mode=field(d,'分叉执行模式');assert.equal(mode.value,'hybrid');mode.value='live';mode.emit('input');await byText(d,'生成固定预览').click();assert.equal(writes.at(-1).body.mode,'live');
 assert(d.textContent.includes('/project/current'));assert(d.textContent.includes('重复来源运行'));assert(d.textContent.includes('不会改写这些待处理动作'));assert(d.textContent.includes('write_file'));assert(!d.querySelector('script'));assert(!d.querySelector('img'));
 let start=byText(d,'确认启动或打开 Live 分支'),confirmation=field(d,'确认 Live 真实执行');assert(start.disabled);await start.click();assert.equal(writes.length,1,'unconfirmed Live sent a request');
 confirmation.checked=true;confirmation.emit('change');assert(!start.disabled);
 mode.value='hybrid';mode.emit('input');await start.click();assert.equal(writes.length,1,'changed mode reused Live preview');
 mode.value='live';mode.emit('input');await byText(d,'生成固定预览').click();start=byText(d,'确认启动或打开 Live 分支');confirmation=field(d,'确认 Live 真实执行');assert(!confirmation.checked&&start.disabled,'new preview inherited confirmation');
 confirmation.checked=true;confirmation.emit('change');delayed=deferred();const running=start.click();await start.click();assert.equal(writes.filter(w=>w.path.endsWith('/start')).length,1);assert.deepEqual(writes.at(-1).body,{expectedHash:'live-hash',confirmLive:true});assert(start.disabled&&confirmation.disabled);
 delayed.resolve({id:'live-target'});await running;delayed=null;assert.deepEqual(selected,['live-target']);
 d=ctx.window.RunDeskKunForks.open({previewId:'live'});await flush();assert(!field(d,'确认 Live 真实执行').checked);assert(byText(d,'确认启动或打开 Live 分支').disabled);d.close();
 const report={version:'0.36.0',passed:true,scope:'Node DOM stub + shipped dialog; no browser/layout acceptance',checks:['Hybrid remains default','Live preview fixes selected mode','current workspace and pending actions visible','unconfirmed start makes no request','mode edits invalidate preview and consent','fresh preview resets confirmation','duplicate start guarded','fixed hash and confirmLive sent together','reopened preview requires fresh explicit confirmation','literal content is not HTML']};fs.writeFileSync(root+'/docs/kun-live-ui-validation.json',JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report));
})().catch(e=>{console.error(e);process.exitCode=1});
