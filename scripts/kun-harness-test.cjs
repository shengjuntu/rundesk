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
const context=vm.createContext({el,button,api,document,state,window:{},instance:()=>instanceValue,console});
for(const name of ['kun-inspect.js','kun-panels.js','kun-debug.js','kun.js'])vm.runInContext(fs.readFileSync(path.join(__dirname,'../internal/web',name),'utf8'),context);
(async()=>{
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
 const settings=el('div');await context.renderKunSettings(settings);const selects=settings.querySelectorAll('select');const harness=selects.find(s=>s.attrs['aria-label']==='Kun 模块组合');assert.equal(harness.value,'plan-act-v1');
 harness.value='tool-loop-v1';await settings.querySelector('form').onsubmit({preventDefault(){}});assert.equal(writes.at(-1).body.config.harness.loopPolicy,'tool-loop-v1');assert.equal(Object.keys(writes.at(-1).body.config.harness).length,1,'old planning module accidentally persisted with new policy');
 const Trace=require('../internal/web/trace-model.js').Model,trace=new Trace();
 const planEvent=event(1,'run-b','model.completed',{step:1,purpose:'plan',message:{content:'proposed plan'}}),actEvent=event(2,'run-b','model.completed',{step:2,purpose:'act',message:{content:'final reply'}});
 trace.ingest([planEvent,actEvent].map(e=>({...e,time:e.data.time})));const traceRows=[...trace.rows.values()];assert.equal(traceRows.filter(r=>r.type==='agentMessage').length,1);assert(traceRows.some(r=>r.title==='Kun 显式规划'));
 const report={version:'0.37.0',passed:true,scope:'Node DOM stub + shipped view code; browser layout not tested',checks:['fixed snapshots and read-only requests','duplicate compare guarded','late response invalidated','missing budget stays unknown','plan literal text and evidence link','settings roundtrip sends compatible preset','timeline does not classify a plan as a reply']};
 fs.writeFileSync(path.join(__dirname,'../docs/kun-harness-ui-validation.json'),JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report));
})().catch(error=>{console.error(error);process.exitCode=1;});
