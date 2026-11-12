// Shipped trace timing UI against scoped read-only query fixtures.
'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const {JSDOM}=require('jsdom');
const dom=new JSDOM('<!doctype html><html lang="zh-CN"><body></body></html>',{url:'http://rundesk.test/#trace',runScripts:'outside-only'}),w=dom.window,ctx=dom.getInternalVMContext();
const requests=[],failures=[],clone=value=>JSON.parse(JSON.stringify(value));
w.rdText=value=>value;w.toast=value=>failures.push(String(value));w.safe=async fn=>{try{return await fn();}catch(e){failures.push(e.message)}};
w.el=(tag,attrs={},...children)=>{const node=w.document.createElement(tag);for(const[key,value]of Object.entries(attrs)){if(key==='class')node.className=value;else if(key.startsWith('on'))node.addEventListener(key.slice(2),value);else if(value!==false&&value!=null)node.setAttribute(key,value===true?'':value);}for(const value of children.flat())if(value!=null)node.append(value instanceof w.Node?value:w.document.createTextNode(String(value)));return node;};
w.button=(label,fn,cls='')=>w.el('button',{type:'button',class:cls,onclick:()=>w.safe(fn)},label);
w.HTMLDialogElement.prototype.showModal=function(){this.open=true};w.HTMLDialogElement.prototype.close=function(){this.open=false;this.dispatchEvent(new w.Event('close'))};
w.state={session:{id:'fixture',runtimeKind:'codex',title:'Timing fixture',observeHTTP:false,status:'completed'},sessions:[]};w.scheduleRender=()=>{};
w.RunDeskTraceReader={mount:()=>({setScope:async()=>{},inspectEvent:async()=>{},destroy(){}})};
const steps=Array.from({length:23},(_,n)=>({id:'step-'+n,runId:'run-1',type:'commandExecution',title:n===0?'<img src=x onerror=alert(1)>':'step '+n,status:'completed',durationMs:n===22?undefined:(n+1)*100,issues:[],eventIds:[],startObserved:true,endObserved:true,clock:'host_journal',association:'observed',preview:{}}));
const run=id=>id==='run-2'?{runId:id,status:'running',clock:'host_journal',startMethod:'run/input',startObserved:true,endObserved:false,complete:false,observedElapsedMs:1200,issues:['missing_end']}:{runId:id,status:'completed',clock:id==='run-4'?'kun_worker':'host_journal',startMethod:id==='run-3'?'turn/started':id==='run-4'?'kun/run.started':'run/input',endMethod:id==='run-4'?'kun/run.finished':'run/state',startObserved:true,endObserved:true,complete:true,durationMs:id==='run-3'?500:2000,issues:[]};
const timing=(id,filtered)=>({runs:id?[run(id)]:[run('run-1'),run('run-2')],byType:[{type:'commandExecution',stepCount:3,completeDurationSteps:2,withoutCompleteDuration:1,workDurationMs:filtered?2000:6000},{type:'approval',stepCount:1,completeDurationSteps:0,withoutCompleteDuration:0,workDurationMs:0}],slowestSteps:id&&id!=='run-1'?[]:[{...steps[0],durationMs:3000}],knownWorkDurationMs:filtered?2000:6000,completeDurationSteps:2,withoutCompleteDuration:1,pointSteps:1});
let hold=null;
w.api=async(url,options={})=>{requests.push({url,method:options.method||'GET'});const u=new URL(url,'http://rundesk.test'),q=u.searchParams,kind=q.get('kind');
 if(u.pathname==='/sessions/fixture')return clone(w.state.session);
 if(u.pathname.endsWith('/capabilities'))return {backend:'codex',queries:[]};
 if(u.pathname.endsWith('/configuration')){
  assert.equal(q.get('runId'),'run-1');
  assert.equal(q.get('through'),'17');
  return {run:{engine:'codex',instanceId:'default',configurationRevisionId:'fixture-revision'},configurationRevision:{bindings:[]}};
 }
 if(kind==='events')return {data:{through:17,events:[]}};
 if(kind==='runs')return {data:{runs:[{id:'run-1',question:'First round',status:'completed'},{id:'run-2',question:'Open round',status:'running'},{id:'run-3',question:'Native interval',status:'completed'},{id:'run-4',question:'Worker interval',status:'completed'}],total:4,nextOffset:4,hasMore:false}};
 if(kind==='statistics'){
  const data={steps:23,attentionSteps:0,withoutCompleteDuration:1,timing:timing(q.get('runId'),!!q.get('type'))};
  if(hold){const gate=hold;hold=null;gate.started();await gate.wait;}
  return {data};
 }
 if(kind==='steps'||kind==='issues'){
  let found=clone(steps);if(q.get('sort')==='duration_desc')found.sort((a,b)=>(b.durationMs??-1)-(a.durationMs??-1));if(q.get('sort')==='duration_asc')found.sort((a,b)=>(a.durationMs??Infinity)-(b.durationMs??Infinity));
  const offset=Number(q.get('offset')||0),end=Math.min(found.length,offset+Number(q.get('limit')||20));return {data:{steps:found.slice(offset,end),total:found.length,nextOffset:end,hasMore:end<found.length}};
 }
 if(kind==='step')return {data:{...steps.find(step=>step.id===q.get('stepId')),durationMs:3000}};
 throw Error('Unexpected query '+url);
};
vm.runInContext(fs.readFileSync(path.join(__dirname,'../debug-trace.js'),'utf8'),ctx);
const $=selector=>w.document.querySelector(selector),until=async fn=>{for(let n=0;n<100;n++){if(fn())return;await new Promise(r=>setImmediate(r));}throw Error('UI did not settle')};
const button=text=>[...w.document.querySelectorAll('button')].find(node=>node.textContent===text);
const change=async(selector,value)=>{const node=$(selector);node.value=value;await node.onchange()};
(async()=>{
 w.RunDeskDebug.open(w.state.session);await until(()=>$('#debug-inspector')?.dataset.through==='17');
 $('[data-panel=steps]').click();await until(()=>$('[data-timing-wall]'));
 assert.equal($('[data-timing-wall]').textContent,'未知','all runs were aggregated into one wall time');
 await change('[aria-label="调试轮次"]','run-1');assert.equal($('[data-timing-wall]').textContent,'2 s');assert.equal($('[data-timing-work]').textContent,'6 s');
 const history=$('.debug-run-execution');
 assert(!requests.some(r=>r.url.includes('/configuration?')), 'closed execution details caused a history lookup');
 history.open=true;
 await history.ontoggle();
 assert(history.textContent.includes('fixture-revision') && history.textContent.includes('未记录'));
 assert($('.debug-timing-note').textContent.includes('累计时长不等于本轮总耗时'));assert($('.debug-timing-note').textContent.includes('不叠加 HTTP'));assert(!w.document.querySelector('img,script'));
 await change('[aria-label="步骤排序"]','duration_desc');assert.equal($('.debug-step').dataset.stepId,'step-21');assert(requests.some(r=>new URL(r.url,'http://rundesk.test').searchParams.get('sort')==='duration_desc'));
 button('下一页').click();await until(()=>$('.debug-step')?.dataset.stepId==='step-1');assert($('.debug-step-list').textContent.includes('完整耗时未知'));
 await change('[aria-label="步骤类型"]','commandExecution');assert.equal($('[data-timing-work]').textContent,'6 s','filtered timing replaced the whole-run summary');
 assert(requests.some(r=>{const q=new URL(r.url,'http://rundesk.test').searchParams;return q.get('kind')==='statistics'&&q.get('runId')==='run-1'&&!q.get('type')}));
 $('.debug-slowest-step').click();await until(()=>$('.debug-step-detail h3'));assert($('.debug-step-detail').textContent.includes('<img src=x'));
 await change('[aria-label="调试轮次"]','run-2');assert.equal($('[data-timing-wall]').textContent,'未知');assert($('.debug-timing-summary').textContent.includes('缺少结束记录'));assert($('.debug-timing-summary').textContent.includes('固定快照已记录 1.2 s'));assert.equal($('.debug-slowest-step'),null);
 await change('[aria-label="调试轮次"]','run-3');assert.equal($('[data-timing-wall]').textContent,'500 ms');assert($('[data-timing-wall]').parentNode.textContent.includes('原生执行区间'));assert($('.debug-timing-clock').textContent.includes('turn/started'));
 await change('[aria-label="调试轮次"]','run-4');assert($('[data-timing-wall]').parentNode.textContent.includes('Kun 执行区间'));assert($('.debug-timing-clock').textContent.includes('Kun 工作进程时钟'));
 await change('[aria-label="步骤类型"]','');
 let release,started;const began=new Promise(r=>started=r);hold={started,wait:new Promise(r=>release=r)};
 const old=change('[aria-label="调试轮次"]','run-1');await began;await change('[aria-label="调试轮次"]','run-2');release();await old;
 assert.equal($('[data-timing-wall]').textContent,'未知','late old-run response rewound timing');assert.equal($('.debug-slowest-step'),null);
 const scoped=requests.filter(r=>{const q=new URL(r.url,'http://rundesk.test').searchParams;return ['steps','statistics','step'].includes(q.get('kind'))});assert(scoped.every(r=>new URL(r.url,'http://rundesk.test').searchParams.get('through')==='17'));
 assert(requests.every(r=>r.method==='GET'),'inspection had a side effect');assert.deepEqual(failures,[]);
 w.RunDeskDebug.close();assert.equal($('#debug-inspector'),null);
 console.log(JSON.stringify({passed:true,checks:['wall duration distinct from overlapping runtime work','unknown/open run boundaries remain unknown','host admission, native and worker timing sources labeled separately','server duration sorting before pagination','whole-run summary independent of step filters','slowest step opens existing evidence detail','late scope result cannot rewind timing','fixed source cursor and GET-only inspection'],scope:'shipped UI in JSDOM with scoped query fixtures; browser geometry and live model execution not tested'}));
 dom.window.close();
})().catch(e=>{console.error(e);dom.window.close();process.exitCode=1});
