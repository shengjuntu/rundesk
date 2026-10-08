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

let serial=0,selected=null,active=true,refreshCalls=0,pendingApply=null,fail=false;const writes=[];
const initial={sessionId:'session',runId:'run-one',revision:5,status:'paused',phase:'before_model',step:0,debug:{pause:{phase:'before_model'}},harness:{id:'tool-loop-v1',revision:1},modules:{capability:{phase:'ready'}},actions:{},config:{harness:{loopPolicy:'tool-loop-v1'}},budget:{toolCalls:0}};
let current=clone(initial),refreshWait=null;
const context=vm.createContext({el,button,crypto:{randomUUID:()=>`command-${++serial}`},api:async(url,o)=>{writes.push({url,body:clone(o.body)});if(fail)throw Error('connection lost');return await new Promise(resolve=>pendingApply=resolve);}});
vm.runInContext(fs.readFileSync(path.join(__dirname,'../internal/web/kun-debug.js'),'utf8'),context);
(async()=>{
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
 const report={version:'0.35.0',passed:true,scope:'Node DOM stub; not browser layout acceptance',checks:['read-only preview','reason required','literal text rendering','revision invalidates proposal','target edits invalidate proposal','history is read-only','unsafe and fork/diagnostic states disabled','same request retained after lost response','duplicate apply guarded','apply does not continue','late selection and closed view ignored']};
 fs.writeFileSync(path.join(__dirname,'../docs/kun-runtime-harness-ui-validation.json'),JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report));
})().catch(e=>{console.error(e);process.exitCode=1;});
