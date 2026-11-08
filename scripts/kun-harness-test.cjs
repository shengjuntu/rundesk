// Named offline contracts for shipped views; no browser layout claims.
const assert = require('node:assert/strict'), fs = require('node:fs'), vm = require('node:vm'), path = require('node:path');
const {Element, el, button, all, find, byText, field, get, clone} = require('./fixtures/dom.cjs');
const {runSuite} = require('./fixtures/suite.cjs');

async function runHarnessInspection() {
// Executes the real view builders and DevTools entry point in a small DOM stub.
// This verifies data/interaction semantics, not browser layout or accessibility.
const event=(sequence,runId,kind,data)=>({id:sequence,method:'kun/'+kind,data:{sequence,revision:sequence,sessionId:'session',runId,type:'kun/'+kind,time:'2026-10-05T00:00:00Z',data}});

let selected=null,delay=false,pending=[];const reads=[],writes=[];
const source={sessionId:'session',runId:'run-a',status:'completed',phase:'after_model',step:1,harness:{id:'tool-loop-v1',version:'1.0.0',revision:1,modules:{planning:{id:'no-explicit-plan-v1',version:'1.0.0',stateSchemaVersion:1}}},modules:{planning:{data:{status:'not_requested'}}},config:{model:'fixture'},budget:{toolCalls:0,reportedTokens:0,unreportedModelCalls:1,activeMillis:10}};
const target={...clone(source),runId:'run-b',step:2,harness:{id:'plan-act-v1',version:'1.0.0',revision:2,modules:{planning:{id:'explicit-plan-v1',version:'1.0.0',stateSchemaVersion:1}}},modules:{planning:{implementation:{id:'explicit-plan-v1'},phase:'ready',data:{status:'ready',step:1,plan:'<script>literal plan</script>'}}},budget:{toolCalls:0,reportedTokens:10,unreportedModelCalls:0,activeMillis:20}};
const instanceValue={id:'instance',revision:1,agentRuntime:{kind:'kun',model:'fixture',endpoint:'http://fixture/v1',harness:{loopPolicy:'plan-act-v1'}}};
const api=async(url,options)=>{
 if(options){writes.push({url,body:clone(options.body),method:options.method});return {...instanceValue,revision:2,agentRuntime:options.body.config};}
 reads.push(url);
 if(url==='/instances')return [clone(instanceValue)];
 const sequence=Number(url.split('/').at(-1));assert([10,20].includes(sequence));
 const value={sequence,state:clone(sequence===10?source:target)};
 if(delay)return new Promise(resolve=>pending.push(()=>resolve(value)));
 return value;
};
const document={body:el('body'),addEventListener(){},querySelector(){return null;}};
const state={instances:[instanceValue],session:{id:'session'}};
const context=vm.createContext({el,button,api,document,state,window:{},instance:()=>instanceValue,rdText:value=>value,console});
for(const name of ['kun-inspect.js','kun-panels.js','kun-debug.js','kun.js'])vm.runInContext(fs.readFileSync(path.join(__dirname,'../internal/web',name),'utf8'),context);

 const view=context.kunHarnessCompare({sid:'session',getSelected:()=>selected});view.update();
 const set=byText(view.node,'设为 Harness 对照起点'),compare=byText(view.node,'对照所选 Harness 快照');
 assert(set.disabled&&compare.disabled);
 selected={data:{sequence:10}};set.click();selected={data:{sequence:20}};view.update();await compare.click();
 assert.equal(reads.length,2);assert.equal(writes.length,0);assert(view.node.textContent.includes('run-a'));assert(view.node.textContent.includes('run-b'));assert(view.node.textContent.includes('不能据此判定优化收益'));assert(view.node.textContent.includes('explicit-plan-v1'));
 selected=null;view.update();assert(view.node.textContent.includes('run-b'),'changing selection rewrote fixed result');assert(compare.disabled);
 selected={data:{sequence:20}};delay=true;const task=compare.click();const duplicate=compare.click();await duplicate;assert.equal(pending.length,2,'duplicate compare ran');
 selected={data:{sequence:10}};set.click();pending.splice(0).forEach(resolve=>resolve());await task;assert(!view.node.querySelector('.kun-harness-comparison'),'stale response restored invalidated result');delay=false;
 const evidence=event(11,'run-b','model.completed',{purpose:'plan',step:1,message:{content:'plan'}});evidence.data.revision=1;
 const layers=context.kunRenderLayers({...target,revision:2},null,[evidence],()=>{});
 assert(layers.textContent.includes('<script>literal plan</script>'));assert(!layers.querySelector('script'));assert(byText(layers,'证据 #11'));
 const missing=context.kunRenderHarnessComparison({sequence:10,state:{...source,harness:{}}},{sequence:20,state:target});assert(missing.textContent.includes('未记录'),'legacy budget shown as known');
 // Everyday settings now redirect Kun configuration to internal maintenance.
 // The preserved advanced-config serialization assertions live in
 // setup-runtime-test.cjs; runtime policy changes are tested by the next case.
 const settings=el('div');await context.renderKunSettings(settings);
 assert(settings.textContent.includes('Kun 维护配置请从左侧环境设置进入'));
 assert.equal(settings.querySelectorAll('form').length,0,'daily settings restored a Kun editor');
 assert.equal(writes.length,0,'opening daily Kun settings changed configuration');
 const Trace=require('../internal/web/trace-model.js').Model,trace=new Trace();
 const planEvent=event(1,'run-b','model.completed',{step:1,purpose:'plan',message:{content:'proposed plan'}}),actEvent=event(2,'run-b','model.completed',{step:2,purpose:'act',message:{content:'final reply'}});
 trace.ingest([planEvent,actEvent].map(e=>({...e,time:e.data.time})));const traceRows=[...trace.rows.values()];assert.equal(traceRows.filter(r=>r.type==='agentMessage').length,1);assert(traceRows.some(r=>r.title==='Kun 显式规划'));
 const report={version:'0.45.0',passed:true,scope:'Node DOM stub + shipped view code; browser layout not tested',checks:['fixed snapshots and read-only requests','duplicate compare guarded','late response invalidated','missing budget stays unknown','plan literal text and evidence link','daily settings redirect Kun configuration to maintenance without a writable form','timeline does not classify a plan as a reply']};
 return report;
}

async function runHarnessSwitch() {
// Executes the real view builders and DevTools entry point in a small DOM stub.
// This verifies data/interaction semantics, not browser layout or accessibility.
const event=(sequence,runId,kind,data)=>({id:sequence,method:'kun/'+kind,data:{sequence,revision:sequence,sessionId:'session',runId,type:'kun/'+kind,time:'2026-10-05T00:00:00Z',data}});

let serial=0,selected=null,active=true,refreshCalls=0,pendingApply=null,fail=false;const writes=[];
const initial={sessionId:'session',runId:'run-one',revision:5,status:'paused',phase:'before_model',step:0,debug:{pause:{phase:'before_model'}},harness:{id:'tool-loop-v1',revision:1},modules:{capability:{phase:'ready'}},actions:{},config:{harness:{loopPolicy:'tool-loop-v1'}},budget:{toolCalls:0}};
let current=clone(initial),refreshWait=null;
const context=vm.createContext({el,button,crypto:{randomUUID:()=>`command-${++serial}`},api:async(url,o)=>{writes.push({url,body:clone(o.body)});if(fail)throw Error('connection lost');return await new Promise(resolve=>pendingApply=resolve);}});
vm.runInContext(fs.readFileSync(path.join(__dirname,'../internal/web/kun-debug.js'),'utf8'),context);

 const view=context.kunHarnessSwitch({sid:'session',getCurrent:()=>current,getSelected:()=>selected,isActive:()=>active,refresh:async()=>{refreshCalls++;if(refreshWait)await new Promise(resolve=>refreshWait=resolve);}});
 const preview=byText(view.node,'预览组合切换'),apply=byText(view.node,'应用已预览的组合'),choice=find(view.node,n=>n.attrs['aria-label']==='当前运行的新组合'),reason=find(view.node,n=>n.attrs['aria-label']==='组合切换原因');
 assert(apply.disabled);await preview.click();assert(apply.disabled);assert.equal(writes.length,0);
 reason.value='<script>compare the policies</script>';reason.oninput();await preview.click();assert(!apply.disabled);assert.equal(writes.length,0);assert(view.node.textContent.includes('新Harness版本'));assert(!view.node.querySelector('script'));
 current.revision++;view.update();assert(apply.disabled,'stale proposal remained executable');await apply.click();assert.equal(writes.length,0);
 await preview.click();choice.value='tool-loop-v1';choice.onchange();assert(apply.disabled);await preview.click();assert(apply.disabled,'same composition preview accepted');
 choice.value='plan-act-v1';choice.onchange();await preview.click();selected={data:{sequence:1}};view.update();assert(preview.disabled&&apply.disabled);await apply.click();assert.equal(writes.length,0);
 selected=null;view.update();
 for(const change of [{fork:{origin:{mode:'live'}}},{diagnostic:{}},{status:'running'},{phase:'before_tool'},{pending:[{}]},{approval:{decision:'approve'}},{queuedControls:[{operation:'steer'}]},{actions:{x:'outcome_unknown'}}]){current={...clone(initial),...change};view.update();assert(preview.disabled,JSON.stringify(change));}
 current=clone(initial);view.update();await preview.click();fail=true;await apply.click();assert(!apply.disabled);const first=clone(writes.at(-1).body);fail=false;
 const running=apply.click();await Promise.resolve();await apply.click();assert.equal(writes.length,2,'duplicate apply sent');assert.deepEqual(writes.at(-1).body,first,'retry changed identity');assert.equal(first.operation,'set_harness');assert.equal(first.expectedStateRevision,5);assert.equal(first.harness.loopPolicy,'plan-act-v1');assert.equal(first.reason,reason.value);
 pendingApply({status:'applied',revision:6});await running;assert(apply.disabled);assert(view.node.textContent.includes('仍保持暂停'));assert.equal(writes.length,2,'switch auto-resumed');
 refreshWait=true;const late=preview.click();await Promise.resolve();selected={data:{sequence:9}};refreshWait();refreshWait=null;await late;assert(apply.disabled,'historical selection accepted late preview');selected=null;
 refreshWait=true;view.update();const closed=preview.click();await Promise.resolve();active=false;refreshWait();refreshWait=null;await closed;assert(apply.disabled,'closed widget retained preview');
 const report={version:'0.45.0',passed:true,scope:'Node DOM stub; not browser layout acceptance',checks:['read-only preview','reason required','literal text rendering','revision invalidates proposal','target edits invalidate proposal','history is read-only','unsafe and fork/diagnostic states disabled','same request retained after lost response','duplicate apply guarded','apply does not continue','late selection and closed view ignored']};
 return report;
}

module.exports = {runHarnessInspection, runHarnessSwitch};
if (require.main === module) runSuite(__filename, [
  {name: 'harness-inspection', run: runHarnessInspection, report: 'kun-harness-ui-validation.json'},
  {name: 'runtime-harness-switch', run: runHarnessSwitch, report: 'kun-runtime-harness-ui-validation.json'},
]).catch(error => {console.error(error); process.exitCode = 1;});
