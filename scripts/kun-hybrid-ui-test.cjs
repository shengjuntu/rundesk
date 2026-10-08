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
const origin={sessionId:'source',runId:'original',sequence:8,through:40,bundleHash:'bundle',mode:'hybrid'};
const preview=id=>({id,title:id==='one'?'<img onerror=alert(1)>':id,hash:'hash-'+id,origin:{...origin,previewId:id},targetSessionId:'target-'+id,instruction:'literal <script>text</script>',phase:'before_model',step:0,pending:0,budget:{toolCalls:0,reportedTokens:0},limits:{maxToolCalls:64},maxSteps:4,recordCount:2});
let writes=[],reads=[],delayed=null,started=0,selected=[],created=0;
const api=async(p,o={})=>{
 if(o.method==='POST'){writes.push({path:p,body:structuredClone(o.body)});if(p==='/kun-forks'){created++;return preview('one')}started++;if(delayed)return delayed.promise;return {id:'target-one'}}
 reads.push(p);if(p.includes('/sources/'))return {selection:{sourceRunId:'original',through:40,expectedStateRevision:99,workerEpoch:'epoch'},items:[{sequence:8,phase:'before_model',step:0,pending:0}],nextOffset:1,hasMore:false};
 if(p.includes('?'))return {items:[preview('one'),preview('two')],hasMore:false,nextOffset:2};
 if(p==='/kun-forks/one'&&delayed)return delayed.promise;
 return preview(p.split('/').at(-1));
};
const ctx=vm.createContext({window:{},document,el,button,api,encodeURIComponent,URLSearchParams,JSON,console,state:{},toast(){},refreshSessions:async()=>{},selectSession:async id=>selected.push(id)});
const root=path.resolve(__dirname,'..');vm.runInContext(fs.readFileSync(root+'/internal/web/kun-inspect.js','utf8'),ctx);vm.runInContext(fs.readFileSync(root+'/internal/web/kun-forks.js','utf8'),ctx);
const buttons=d=>d.querySelectorAll('button'),byText=(d,text)=>buttons(d).find(n=>n.textContent===text),field=(d,name)=>[...d.querySelectorAll('input'),...d.querySelectorAll('textarea'),...d.querySelectorAll('select')].find(n=>n.attrs['aria-label']===name);
(async()=>{
 let d=ctx.window.RunDeskKunForks.open({sessionId:'source'});await flush();assert.equal(writes.length,0);assert.equal(field(d,'安全分叉边界').value,8);
 field(d,'分支标题').value='Experiment';field(d,'分支补充指令').value='new instruction';await byText(d,'生成固定预览').click();
 assert.equal(created,1);assert.equal(started,0);assert.deepEqual(writes[0].body,{sessionId:'source',title:'Experiment',instruction:'new instruction',selection:{sourceRunId:'original',through:40,expectedStateRevision:99,workerEpoch:'epoch',sequence:8}});
 assert(d.textContent.includes('未命中立即停止'));assert(d.textContent.includes('继承模型调用'));assert.equal(d.querySelectorAll('img').length,0);assert.equal(d.querySelectorAll('script').length,0);
 const staleButton=byText(d,'启动或打开 Hybrid 分支');field(d,'分支补充指令').value='edited';field(d,'分支补充指令').emit('input');await staleButton.click();assert.equal(started,0,'edited preview was started');
 await byText(d,'生成固定预览').click();delayed=deferred();const start=byText(d,'启动或打开 Hybrid 分支'),pending=start.click();assert(start.disabled);await start.click();assert.equal(started,1,'double click sent twice');
 assert.equal(writes.at(-1).body.expectedHash,'hash-one');delayed.resolve({id:'target-one'});await pending;delayed=null;assert.deepEqual(selected,['target-one']);assert(!d.isConnected);
 // A slow preview read cannot overwrite a subsequent selection.
 d=ctx.window.RunDeskKunForks.open();await flush();delayed=deferred();const first=buttons(d).find(n=>n.textContent.startsWith('<img')),second=buttons(d).find(n=>n.textContent.startsWith('two ·'));
 const old=first.click();await second.click();delayed.resolve(preview('one'));await old;delayed=null;assert.equal(d.querySelector('main').querySelector('h3').textContent,'two');
 // Closing while start is pending never navigates away from a later selection.
 delayed=deferred();const last=byText(d,'启动或打开 Hybrid 分支').click();d.close();delayed.resolve({id:'target-two'});await last;delayed=null;assert.deepEqual(selected,['target-one']);
 assert(writes.every(v=>v.path==='/kun-forks'||/^\/kun-forks\/[^/]+\/start$/.test(v.path)),'dialog controlled source');
 const report={version:'0.36.0',passed:true,scope:'Node DOM stub + shipped view code; browser layout not tested',checks:['preview does not start models','fixed source selection and expected hash','edits invalidate preview','duplicate start click guarded','literal text rendering','stale preview response ignored','closing dialog suppresses late navigation','no source control routes']};
 fs.writeFileSync(root+'/docs/kun-hybrid-ui-validation.json',JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report));
})().catch(e=>{console.error(e);process.exitCode=1});
