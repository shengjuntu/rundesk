// Behavioral regression for the shipped conversation trace reader.
// JSDOM exercises DOM safety and query/selection races, not browser layout or providers.
'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const {JSDOM}=require(process.env.JSDOM_PATH||'jsdom');
const root=path.resolve(__dirname,'..');
const dom=new JSDOM('<!doctype html><html><body><main id="host"></main></body></html>',{url:'http://rundesk.test/',runScripts:'outside-only'});
const w=dom.window,host=w.document.querySelector('#host'),requests=[],failures=[],selectedRuns=[],transports=[],kunSelections=[];
const clone=value=>JSON.parse(JSON.stringify(value));
const tick=()=>new Promise(resolve=>setImmediate(resolve));
async function until(check,message='UI did not settle'){for(let i=0;i<150;i++){if(check())return;await tick();}assert(check(),message);}
const deferred=()=>{let resolve;const promise=new Promise(done=>{resolve=done;});return {promise,resolve};};
w.rdText=value=>value;w.rdFormat=(text,...args)=>text.replace(/\$\{(\d+)\}/g,(_,i)=>String(args[+i]??''));
w.safe=async fn=>{try{return await fn();}catch(error){failures.push(error.message);}};
w.toast=message=>failures.push(String(message));
w.HTMLElement.prototype.scrollIntoView=function(){};
w.el=(tag,attrs={},...children)=>{
 const node=w.document.createElement(tag);
 for(const[key,value]of Object.entries(attrs)){
  if(key==='class')node.className=value;
  else if(key.startsWith('on'))node.addEventListener(key.slice(2),value);
  else if(key==='text')node.textContent=value;
  else if(value!==false&&value!=null)node.setAttribute(key,value===true?'':value);
 }
 for(const child of children.flat())if(child!=null)node.append(child instanceof w.Node?child:w.document.createTextNode(String(child)));
 return node;
};
w.button=(text,fn,cls='')=>w.el('button',{type:'button',class:cls,onclick:()=>w.safe(fn)},text);
// Load the production safe Markdown functions, retaining their actual URL policy.
const app=fs.readFileSync(path.join(root,'internal/web/app.js'),'utf8');
w.replyButton=(_icon,label,fn)=>w.button(label,fn);w.copyReplyText=async()=>{};
vm.runInContext(app.slice(app.indexOf('function inline('),app.indexOf('function renderMessages(')),dom.getInternalVMContext());
vm.runInContext(fs.readFileSync(path.join(root,'internal/web/trace-reader.js'),'utf8'),dom.getInternalVMContext());

const evidence=(id,readWith='event')=>({eventId:id,readWith});
const activity=(id,kind='message',extra={})=>({id,sessionId:'s1',runId:'run-1',source:'codex_app_server',sourceBasis:'native_method',kind,type:'agentMessage',title:kind,status:'completed',start:'2026-10-20T12:00:00Z',end:'2026-10-20T12:00:01Z',clock:'host_journal',durationMs:1000,point:false,startObserved:true,endObserved:true,association:'observed',associationBasis:'native_identifiers',native:{turnId:'turn-1',itemId:id},evidence:[evidence(11)],issues:[],missing:[],...extra});
const entry=(id,role,title,text,extra={})=>({id,activity:activity(id,role==='tool'?'tool':role==='reasoning'?'reasoning':'message'),role,title,blocks:[{kind:'text',text}],contentTruncated:false,contentUnavailable:false,...extra});
const baseEntries=[
 entry('user-1','user','用户消息','请修复布局 <img src=x onerror="window.injection=true"> [unsafe](javascript:alert(1))'),
 entry('answer-1','assistant','助手回复','## 检查布局\n\n已找到 **布局问题**。\n\n```js\nconst text = "<script>literal</script>";\n```'),
 entry('tool-1','tool','读取 README.md','file content',{activity:activity('tool-1','tool',{type:'commandExecution'}),blocks:[{kind:'code',text:'cat README.md',language:'sh'},{kind:'text',text:'document content'}]}),
 entry('reason-1','reasoning','可见推理','可见的推理摘要',{activity:activity('reason-1','reasoning',{type:'reasoning'})}),
 entry('failed-1','tool','测试失败','expected result did not match',{activity:activity('failed-1','tool',{type:'commandExecution',status:'failed',issues:['failed']})}),
 entry('approval-1','system','等待审批','等待允许写入',{activity:activity('approval-1','approval',{type:'approval',status:'pending',endObserved:false,end:undefined,issues:['missing_end'],missing:['end']})}),
 entry('http-1','system','HTTP POST 200','',{activity:activity('http-1','transport',{source:'observeproxy',type:'llmExchange',evidence:[evidence(31,'llm_request')],association:'missing',associationBasis:'unassigned',runId:'',transport:{method:'POST',statusCode:200,complete:false,truncated:true,incomplete:false,captureLoss:true,headersRedacted:true,requestBytes:20,responseBytes:40}})}),
 entry('kun-1','tool','Kun 工具','recorded output',{activity:activity('kun-1','tool',{source:'kun_sdk',type:'toolReplay',status:'replayed',native:{step:1,purpose:'act',workerSequence:4},evidence:[{eventId:40,readWith:'event',workerSequence:4}]})}),
 entry('long-1','assistant','长回复','本页只提供已脱敏的截断内容',{contentTruncated:true}),
 entry('long-local','user','长输入','长内容起点 '+ '汉'.repeat(6000)+' 完整内容终点'),
 entry('unavailable-1','system','旧记录','',{contentUnavailable:true,activity:activity('unavailable-1','event',{status:'unknown',issues:['missing_identity'],missing:['identity']})}),
 ...Array.from({length:46},(_,i)=>entry('extra-'+i,'assistant','助手回复','分页消息 '+i)),
];
const runs=[{id:'run-1',question:'修复布局',status:'completed'},{id:'run-2',question:'复核第二轮',status:'completed'}];
const raw='{ "exact":9007199254740993,"text":"<img src=x onerror=alert(1)> '+ '汉'.repeat(6000)+'" }';
let hold=null,holdEvent=null,view,scope={through:99,runId:''};
const pageFor=q=>{
 const rows=q.through===0?[]:q.runId==='run-2'?[entry('second-run','assistant','第二轮回答','只属于第二轮',{activity:activity('second-run','message',{runId:'run-2'})})]:baseEntries;
 const offset=Number(q.offset||0),limit=Number(q.limit||20),end=Math.min(rows.length,offset+limit);
 return {schemaVersion:1,sessionId:'s1',snapshotThrough:q.through,entries:clone(rows.slice(offset,end)),total:rows.length,offset,nextOffset:end,hasMore:end<rows.length,coverage:'retained journal; source capture may be incomplete'};
};
const read=async q=>{
 requests.push(clone(q));
 if(q.kind==='runs'){const rows=q.through===0?[]:runs;return {data:{runs:clone(rows),total:rows.length,offset:0,nextOffset:rows.length,hasMore:false}};}
 if(q.kind==='transcript'){
  const answer=pageFor(q);
  if(hold&&(!hold.runId||hold.runId===q.runId)){const gate=hold;hold=null;gate.started.resolve();await gate.release.promise;return {data:answer};}
  return {data:answer};
 }
 if(q.kind==='event'){
  const chars=Array.from(raw),offset=Number(q.offset||0),end=Math.min(chars.length,offset+Number(q.limit||4000));
  if(holdEvent){const gate=holdEvent;holdEvent=null;gate.started.resolve();await gate.release.promise;}
  return {data:{eventId:q.eventId,text:chars.slice(offset,end).join(''),totalCharacters:chars.length,nextOffset:end,hasMore:end<chars.length}};
 }
 throw Error('Unexpected reader query '+JSON.stringify(q));
};
const buttons=(node=host)=>[...node.querySelectorAll('button')];
const findButton=(text,node=host)=>buttons(node).find(button=>typeof text==='string'?button.textContent.trim()===text:text.test(button.textContent));
const rendered=id=>host.querySelector('[data-entry-id="'+id+'"]');
const click=async button=>{assert(button,'Expected a UI button');assert(!button.disabled,'Expected enabled button');button.click();await tick();};
const toggle=async name=>{const box=host.querySelector('input[aria-label="'+name+'"]')||[...host.querySelectorAll('label')].find(label=>label.textContent.includes(name))?.querySelector('input');assert(box,'Missing checkbox '+name);box.checked=!box.checked;box.dispatchEvent(new w.Event('change',{bubbles:true}));await tick();return box;};

(async()=>{
 view=w.RunDeskTraceReader.mount({host,read,scope:()=>scope,session:{id:'s1',runtimeKind:'codex'},onSelectRun:async runId=>{selectedRuns.push(runId);scope={...scope,runId};await view.setScope(scope);},onTransport:row=>transports.push(row),onKun:row=>kunSelections.push(row)});
 await view.refresh();await until(()=>!!rendered('answer-1'));
 assert(host.querySelector('.trace-reader'));
 assert(host.querySelector('.trace-run-nav'));
 assert(host.querySelector('.trace-page-status'));
 assert.equal(requests.filter(q=>q.kind==='event'||q.kind==='llm_request').length,0,'initial reading eagerly fetched raw evidence');
 assert(!host.querySelector('img,script'),'untrusted message produced an executable element');
 assert(![...host.querySelectorAll('a')].some(a=>a.href.startsWith('javascript:')),'unsafe Markdown URL became a link');
 assert(rendered('user-1').textContent.includes('<img src=x'),'literal user content was lost');
 assert(rendered('answer-1').querySelector('strong'),'assistant Markdown was not rendered');
 assert(rendered('tool-1').querySelector('details'),'tool output has no disclosure');
 assert(!rendered('tool-1').querySelector('details').open,'ordinary tool output should start folded');
 assert(rendered('reason-1').querySelector('details'),'reasoning has no disclosure');
 assert(!rendered('reason-1').querySelector('details').open,'reasoning should start folded');
 assert(rendered('failed-1').textContent.includes('测试失败'),'failure hidden from the main trace');
 assert(rendered('approval-1').textContent.includes('等待审批'),'approval hidden from the main trace');
 assert(rendered('long-1').textContent.includes('截断'),'truncated content was labelled complete');
 const longMessage=rendered('long-local'),longDisclosure=longMessage.querySelector('.trace-long-content'),beforeExpand=requests.length;
 assert(longDisclosure&&!longDisclosure.open,'long message was expanded by default');assert(!longMessage.textContent.includes('完整内容终点'),'long message rendered full text eagerly');
 longDisclosure.open=true;await until(()=>longMessage.textContent.includes('完整内容终点'));assert.equal(requests.length,beforeExpand,'expanding already-loaded text fetched more evidence');
 longDisclosure.open=false;await until(()=>longDisclosure.querySelector('summary').textContent.includes('展开长内容'));assert(!longMessage.querySelector('.trace-long-message>.trace-prose').hidden,'collapsing long text did not restore excerpt');
 const requested=requests.find(q=>q.kind==='transcript');
 assert(Number.isInteger(Number(requested.limit))&&requested.limit>0&&requested.limit<=100,'transcript page is not bounded');
 assert(host.querySelectorAll('.trace-entry').length<=requested.limit,'page accumulated more rows than requested');

 await click(findButton('查看证据',rendered('tool-1')));
 await until(()=>!!host.querySelector('.trace-evidence'));
 const panel=host.querySelector('.trace-evidence');
 assert(![...panel.querySelector('.trace-evidence-body').childNodes].some(n=>n.nodeType===w.Node.TEXT_NODE&&n.textContent==='null'),'optional evidence warning rendered as literal null');
 if(!requests.some(q=>q.kind==='event'))await click(findButton(/#11/,panel));
 await until(()=>requests.some(q=>q.kind==='event'));
 await until(()=>panel.textContent.includes('9007199254740993'));
 assert(!panel.querySelector('img,script'),'raw evidence inserted markup');
 assert(panel.textContent.includes('codex_app_server')||panel.textContent.includes('Codex'),'source provenance missing in evidence');
 assert(requests.filter(q=>q.kind==='event').every(q=>q.through===99),'evidence escaped fixed snapshot');
 await click(findButton('收起证据'));

 await toggle('显示推理');assert(!rendered('reason-1'),'reasoning hide toggle left the row visible');
 await toggle('显示推理');
 const reasoning=rendered('reason-1');assert(reasoning,'visible reasoning toggle removed recorded reasoning');
 await toggle('显示通信记录');
 const transport=rendered('http-1');assert(transport,'transport record not reachable');
 assert(/截断|缺口|丢失|不完整/.test(transport.textContent),'capture loss was hidden');
 const openTransport=findButton(/通信|请求|响应/,transport);if(openTransport)await click(openTransport);

 await click(findButton('下一页'));
 await until(()=>!rendered('user-1')&&requests.some(q=>q.kind==='transcript'&&q.offset>0));
 assert(!host.textContent.includes('已完整读取'),'old evidence leaked to next page');
 assert(host.querySelectorAll('.trace-entry').length<=requested.limit,'paging accumulated all history');
 await click(findButton('上一页'));await until(()=>!!rendered('user-1'));

 const runButton=findButton(/复核第二轮/,host.querySelector('.trace-run-nav'));
 await click(runButton);await until(()=>!!rendered('second-run'));
 assert.equal(selectedRuns.at(-1),'run-2');assert(!rendered('user-1'),'another run remained after navigation');

 // A late successful response from a previous scope must not replace the current run.
 const gate={runId:'run-1',started:deferred(),release:deferred()};hold=gate;
 scope={through:99,runId:'run-1'};const old=view.setScope(scope);await gate.started.promise;
 scope={through:99,runId:'run-2'};await view.setScope(scope);await until(()=>!!rendered('second-run'));
 gate.release.resolve();await old;await tick();assert(rendered('second-run'));assert(!rendered('user-1'),'stale response replaced selected run');

 await view.inspectEvent(11);await until(()=>host.querySelector('.trace-evidence')?.textContent.includes('9007199254740993'));
 assert(requests.every(q=>q.through===99),'one reader query escaped the fixed history boundary');
 const eventGate={started:deferred(),release:deferred()};holdEvent=eventGate;
 const staleEvent=view.inspectEvent(11);await eventGate.started.promise;
 const beforeNoSnapshot=requests.length;scope={through:null,runId:'run-2'};await view.setScope(scope);
 assert.equal(requests.length,beforeNoSnapshot,'unknown cutoff issued an unbounded query');
 assert.equal(host.querySelectorAll('.trace-entry').length,0,'old content remained while fixed snapshot was unavailable');
 assert(host.querySelector('.trace-evidence').hidden,'evidence remained visible under an unavailable snapshot');
 eventGate.release.resolve();await staleEvent;await tick();
 assert(!host.querySelector('.trace-raw-event')?.textContent.includes('9007199254740993'),'stale evidence repainted after cutoff was cleared');
 scope={through:0,runId:''};await view.setScope(scope);
 assert.equal(host.querySelectorAll('.trace-entry').length,0,'explicit empty snapshot showed messages');
 assert(requests.slice(beforeNoSnapshot).filter(q=>q.kind==='transcript'||q.kind==='runs').every(q=>q.through===0),'explicit zero cutoff was replaced by latest');
 const beforeInvalidEvent=requests.length;await view.inspectEvent(11);assert.equal(requests.length,beforeInvalidEvent,'event outside snapshot was read');
 scope={through:99,runId:'run-2'};await view.setScope(scope);await until(()=>!!rendered('second-run'));
 assert.deepEqual(failures,[]);
 const finalGate={started:deferred(),release:deferred()};hold=finalGate;
 const pending=view.refresh();await finalGate.started.promise;view.destroy();finalGate.release.resolve();await pending;await tick();
 assert.equal(host.querySelector('.trace-reader'),null,'destroy left a mounted reader');
 assert.equal(host.querySelector('.trace-entry'),null,'late response repainted a destroyed reader');
 console.log(JSON.stringify({passed:true,scope:'JSDOM + shipped trace reader + explicit read-only query fixture; no provider or layout claims',checks:['conversation/tool folding','safe Markdown and literal raw evidence','bounded page replacement','failure and approval visibility','source and truncation labels','lazy evidence reads','round navigation','fixed and explicit zero snapshots','unknown snapshot clears stale content without queries','stale page and evidence response rejection','teardown']}));
})().catch(error=>{console.error(error);process.exitCode=1;}).finally(()=>dom.window.close());
