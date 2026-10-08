// Executes the real view builders and DevTools entry point in a small DOM stub.
// This verifies data/interaction semantics, not browser layout or accessibility.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
class Element{
 constructor(tag,attrs={},...children){
  this.tagName=tag.toUpperCase();this.attrs=attrs;this.dataset={};this.children=[];this.disabled=false;this.open=false;this.scrollTop=0;this.isConnected=true;this.value=attrs.value??'';
  for(const [key,value]of Object.entries(attrs))if(key.startsWith('data-'))this.dataset[key.slice(5).replace(/-([a-z])/g,(_,c)=>c.toUpperCase())]=String(value);
  this.classes=new Set((attrs.class||'').split(' '));this.classList={add:(c)=>this.classes.add(c),remove:(c)=>this.classes.delete(c),contains:c=>this.classes.has(c),toggle:(c,v)=>{if(v??!this.classes.has(c))this.classes.add(c);else this.classes.delete(c);}};
  this.append(...children);
 }
 append(...children){for(const child of children.flat()){if(child==null)continue;if(child instanceof Element)child.parentElement=this;this.children.push(child);}}
 prepend(...children){const old=this.children;this.children=[];this.append(...children,...old);}
 replaceChildren(...children){this.children=[];this.append(...children);}
 set textContent(value){this.children=[value];}
 get textContent(){return this.children.map(c=>c instanceof Element?c.textContent:String(c??'')).join('');}
 get lastElementChild(){return this.children.findLast(n=>n instanceof Element);}
 querySelectorAll(selector){const out=[];for(const child of this.children){if(!(child instanceof Element))continue;if(matches(child,selector))out.push(child);out.push(...child.querySelectorAll(selector));}return out;}
 querySelector(selector){if(selector===':scope > summary')return this.children.find(n=>n instanceof Element&&n.tagName==='SUMMARY');return this.querySelectorAll(selector)[0]||null;}
 addEventListener(){}
 checkValidity(){return true;}
 showModal(){this.open=true;}
 close(){this.open=false;}
 remove(){this.isConnected=false;if(this.parentElement)this.parentElement.children=this.parentElement.children.filter(n=>n!==this);}
}
function matches(node,selector){return selector.startsWith('.')?node.classes.has(selector.slice(1)):selector.startsWith('#')?node.attrs.id===selector.slice(1):node.tagName===selector.toUpperCase();}
const el=(...args)=>new Element(...args),button=(label,click)=>{const node=el('button',{},label);node.click=click;return node;};
const all=node=>[node,...node.children.filter(c=>c instanceof Element).flatMap(all)];
const find=(node,p)=>all(node).find(p),byText=(node,text)=>find(node,n=>n.tagName==='BUTTON'&&n.textContent===text);
const clone=v=>JSON.parse(JSON.stringify(v));
const event=(sequence,runId,kind,data)=>({id:sequence,method:'kun/'+kind,data:{sequence,revision:sequence,sessionId:'session',runId,type:'kun/'+kind,time:'2026-10-05T00:00:00Z',data}});
const modules={memory:{implementation:{id:'full-history-v1',version:'1.0.0',stateSchemaVersion:1},phase:'before_model',data:{messageCount:1,requestBytes:100,compression:'none',truncated:false}},planning:{phase:'before_model',data:{status:'not_requested',reason:'no extra model'}},action:{phase:'after_tool',data:{callId:'tool-one',tool:'write_file',status:'succeeded'}},capability:{phase:'ready',data:{toolCount:1}}};
const old={sessionId:'session',runId:'old-run',revision:6,status:'completed',phase:'after_model',step:1,messages:[{role:'user',content:'input'}],modules,harness:{id:'tool-loop-v1',version:'1.0.0',revision:1},config:{maxSteps:3,budget:{maxToolCalls:5,maxTotalTokens:0,maxActiveSeconds:9,maxConsecutiveFailures:2}},budget:{toolCalls:1,reportedTokens:0,unreportedModelCalls:1,activeMillis:1500,waitMillis:2500,consecutiveFailures:0},mcp:[{name:'weather',configRevision:'config-1',transport:'streamable-http',protocolVersion:'2025-11-25',status:'closed',toolCount:1}],mcpTools:[{alias:'mcp_weather_one',server:'weather',name:'lookup',approvalMode:'prompt',description:'<script>literal</script>',inputSchema:{type:'object'}}],approvalPolicy:'never',actions:{'tool-one':'succeeded'},debug:{revision:1,policy:{breakpoints:[],pauseTimeoutSeconds:0}}};
const records=[
 event(1,'old-run','context.built',{memory:modules.memory,planning:modules.planning}),
 event(2,'old-run','capability.ready',modules.capability),
 event(3,'old-run','model.started',{step:1,request:{model:'fixture',messages:old.messages,tools:[]}}),
 event(4,'old-run','tool.completed',{step:1,call:{id:'tool-one',function:{name:'write_file',arguments:'{}'}},status:'succeeded',durationMs:300}),
 event(5,'old-run','mcp.response',{server:'weather',exchangeId:1,method:'tools/call',durationMs:200,result:{text:'done'}}),
 event(6,'old-run','model.completed',{step:1,message:{role:'assistant',content:'done'},durationMs:1000}),
 event(7,'old-run','model.completed',{step:2,message:{content:'future answer'},durationMs:999,usage:{total_tokens:123456789}}),
 event(8,'other-run','model.completed',{step:1,message:{content:'other answer'},durationMs:999,usage:{total_tokens:99999}}),
];
let live={...clone(old),runId:'current-run',revision:20,status:'paused',phase:'before_model',approvalPolicy:'prompt',approval:null};
const writes=[],reads=[];let pendingControl;
const api=async(url,options)=>{
 if(options?.method==='POST'){writes.push({url,body:clone(options.body)});return new Promise(resolve=>{pendingControl=resolve;});}
 reads.push(url);
 if(url.includes('/events?'))return Number(new URL('http://fixture'+url).searchParams.get('after'))===0?clone(records):[];
 if(url.endsWith('/kun/state'))return clone(live);
 if(url.includes('/kun/snapshots/')){const sequence=Number(url.split('/').at(-1));return {sequence,state:{...clone(old),revision:sequence}};}
 throw Error('Unexpected read '+url);
};
const document={body:el('body'),addEventListener(){},querySelector(selector){return this.body.querySelector(selector);}};
const timers=[];
const context=vm.createContext({el,button,api,console,document,state:{session:{id:'session',runtimeKind:'kun'},sessions:[]},window:{},crypto:{randomUUID:()=>`control-${writes.length+1}-fixture`},setInterval:fn=>{timers.push(fn);return timers.length;},clearInterval(){}});
for(const name of ['kun-inspect.js','kun-panels.js','kun-debug.js','kun.js'])vm.runInContext(fs.readFileSync(path.join(__dirname,'../internal/web',name),'utf8'),context);

// Pure data/view regressions, including missing/zero usage and a pinned cutoff.
const selected=records[5],seen=[];
assert.equal(context.kunScopedEvents(records,old,selected).length,6);
const performance=context.kunRenderPerformance(old,selected,records,e=>seen.push(e));
assert(!performance.textContent.includes('123456789'));assert(!performance.textContent.includes('99999'));
assert(performance.textContent.includes('未报告'));assert(performance.textContent.includes('2.500 s'));assert(performance.textContent.includes('未估算'));
assert(!context.kunRenderPerformance(null,null,records,()=>{}).textContent.includes('old-run'),'offline live view mixed arbitrary historical runs');
assert.equal(context.kunReportedUsage({total_tokens:0}),'0');assert.equal(context.kunReportedUsage({prompt_tokens:3,completion_tokens:4}),'7（输入+输出）');assert.equal(context.kunReportedUsage({total_tokens:-1}),'未报告有效总量');assert.equal(context.kunReportedUsage(null),'未报告');
assert.equal(context.kunReportedUsage({total_tokens:0,prompt_tokens:1.5}),'未报告有效总量');
const legacy=context.kunRenderPerformance({...old,harness:{}},selected,records,()=>{});assert(legacy.textContent.includes('累计预算按未知显示'));assert.equal(legacy.querySelectorAll('progress').length,1,'legacy zero defaults appeared as measured budget');
assert.equal(context.kunMetric('unknown',undefined,10).querySelectorAll('progress').length,0);
assert.equal(context.kunMetric('disabled',0,0,String,true).querySelectorAll('progress').length,0);
assert.equal(context.kunMetric('over',12,10).querySelector('progress').attrs.value,10);
const app=context.kunRenderApplication(old,selected,records,e=>seen.push(e),()=>{});
assert(app.textContent.includes('因此拒绝'));assert(app.textContent.includes('已关闭'));assert(!app.querySelector('script'),'MCP description became markup');
const layers=context.kunRenderLayers(old,selected,records,e=>seen.push(e));
assert.equal(layers.querySelectorAll('.kun-module-card').length,4);assert(layers.textContent.includes('未请求独立规划'));
byText(layers,'证据 #1').click();assert.equal(seen.at(-1).data.sequence,1);
const moduleRaw=layers.querySelectorAll('details').filter(n=>n.querySelector(':scope > summary')?.textContent==='模块原始状态');
assert.equal(new Set(moduleRaw.map(n=>context.kunDetailKey(n))).size,4,'expansion state collided across modules');
const noEvidence=context.kunRenderLayers({...old,runId:'resumed-run'},null,records,()=>{});
assert(!noEvidence.textContent.includes('证据 #1'),'inherited state linked to wrong run without evidence');
assert(context.kunControlReason(null,'cancel'));assert(context.kunControlReason({...old,status:'completed'},'resume'));
assert.equal(context.kunControlReason(live,'resume'),'');assert.equal(context.kunControlReason(live,'cancel'),'');
assert(context.kunControlReason({...live,approval:{callId:'x'}},'step'));
assert.equal(context.kunControlReason({...live,approval:{callId:'x'}},'approve'),'');
assert(context.kunControlReason({...live,approval:{callId:'x',decision:'approve'}},'approve'));
assert(context.kunControlReason({...live,status:'running',queuedControls:[{operation:'pause'}]},'pause'));

(async()=>{
 await context.window.RunDeskKun.open();
 const dialog=document.querySelector('#kun-devtools');assert(dialog.open);
 const tab=label=>byText(dialog,label).click();
 await tab('Elements · 上下文');const list=dialog.querySelector('.kun-call-list');await list.querySelector('button').click();
 const sequence=dialog.querySelector('.kun-context').dataset.sequence;assert.equal(sequence,'3');
 await tab('Layers · 模块');assert.equal(dialog.querySelector('.kun-layers').dataset.sequence,sequence);assert.equal(dialog.querySelector('.kun-layers').dataset.runId,'old-run');
 const raw=dialog.querySelector('.kun-module-card').querySelector('details');raw.open=true;
 await byText(dialog,'刷新记录').click();assert(dialog.querySelector('.kun-module-card').querySelector('details').open,'refresh collapsed module details');
 await byText(dialog.querySelector('.kun-layers'),'证据 #1').click();assert.equal(dialog.dataset.panel,'network');assert.equal(dialog.querySelector('.kun-event-evidence').dataset.sequence,'1');
 await tab('Application · MCP');assert.equal(dialog.querySelector('.kun-application').dataset.sequence,'1');
 await tab('Performance · 用量');assert.equal(dialog.querySelector('.kun-performance').dataset.sequence,'1');
 await tab('Sources · 控制');assert.equal(dialog.querySelector('.kun-sources-state').dataset.runId,'current-run');
 assert.equal(writes.length,0,'read-only panel navigation wrote to the session');
 assert(byText(dialog,'允许本次工具调用').disabled);assert(!byText(dialog,'继续').disabled);
 assert(byText(dialog,'应用断点到当前运行').disabled,'unread policy draft can be applied');
 const submission=byText(dialog,'继续').click();assert.equal(writes.length,1);assert.equal(writes[0].body.runId,'current-run');assert.equal(writes[0].body.expectedStateRevision,20);
 assert(byText(dialog,'继续').disabled);await byText(dialog,'继续').click();assert.equal(writes.length,1,'double click submitted duplicate control');
 live={...live,revision:21,status:'completed',phase:'after_model'};pendingControl({status:'applied',revision:21});await submission;
 assert(byText(dialog,'继续').disabled);assert(byText(dialog,'停止').disabled);assert.equal(dialog.querySelector('.kun-sources-state').dataset.status,'completed');
 // Older refresh data must not rewind the current control target.
 live={...live,revision:19,status:'paused'};await byText(dialog,'刷新记录').click();assert.equal(dialog.querySelector('.kun-sources-state').dataset.revision,'21');
 live={...live,revision:22,status:'paused',phase:'approval',approval:{callId:'current-approval',server:'weather',tool:'lookup',arguments:'{}'}};
 await byText(dialog,'刷新记录').click();assert(byText(dialog,'继续').disabled);assert(!byText(dialog,'允许本次工具调用').disabled);
 await byText(dialog,'读取当前断点').click();assert(!byText(dialog,'应用断点到当前运行').disabled);
 live={...live,revision:23};await byText(dialog,'刷新记录').click();assert(byText(dialog,'应用断点到当前运行').disabled,'stale policy enabled');
 const approval=byText(dialog,'允许本次工具调用').click();assert.equal(writes[1].body.callId,'current-approval');assert.equal(writes[1].body.expectedStateRevision,23);assert.equal(writes[1].body.runId,'current-run');
 live={...live,revision:24,status:'completed',approval:null};pendingControl({status:'applied',revision:24});await approval;
 assert(byText(dialog,'允许本次工具调用').disabled);assert.equal(writes.length,2);
 console.log('Kun panel regressions passed: scope/cutoff, missing usage, nested timing, permissions display, evidence navigation, expansion keys, actual DevTools initialization, live control target, busy guard, stale state.');
})().catch(e=>{console.error(e);process.exitCode=1;});
