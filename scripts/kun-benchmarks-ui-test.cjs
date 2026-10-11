const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
class Element{
 constructor(tag,attrs={},...children){this.tagName=tag.toUpperCase();this.attrs=attrs;this.children=[];this.listeners={};this.value=attrs.value??'';this.disabled=false;this.append(...children);}
 append(...children){this.children.push(...children.flat().filter(n=>n!=null));}
 replaceChildren(...children){this.children=[];this.append(...children);}
 set textContent(v){this.children=[v]}get textContent(){return this.children.map(n=>n instanceof Element?n.textContent:String(n??'')).join('')}
 addEventListener(type,fn){this.listeners[type]=fn}emit(type){this.listeners[type]?.()}
}
const el=(...a)=>new Element(...a),button=(label,click)=>{const b=el('button',{},label);b.click=click;return b};
const all=n=>[n,...n.children.filter(c=>c instanceof Element).flatMap(all)],byText=(n,s)=>all(n).find(v=>v.tagName==='BUTTON'&&v.textContent===s),field=(n,label)=>all(n).find(v=>v.attrs['aria-label']===label),cases=n=>all(n).filter(v=>v.tagName==='FIELDSET');
const pending=[],requests=[],api=(url,options)=>new Promise((resolve,reject)=>{const r={url,options,resolve,reject};pending.push(r);requests.push(r)});
const root=path.resolve(__dirname,'..'),ctx=vm.createContext({window:{},document:{addEventListener(){}},el,button,api,encodeURIComponent,TextEncoder,URLSearchParams,JSON,console});
for(const name of ['kun-case-runs','kun-benchmarks','kun-skills'])vm.runInContext(fs.readFileSync(root+'/internal/web/'+name+'.js','utf8'),ctx);
const input=(node,label,value,type='input')=>{const f=field(node,label);f.value=value;f.emit(type);return f};
const result=async(task,value)=>{assert(pending.length);pending.shift().resolve(value);await task};
(async()=>{
 let active=true;
 const candidate={id:'skill',hash:'skill-hash',title:'Safe <script> Skill'};
 const node=ctx.window.RunDeskKunBenchmarks.create({candidate,isActive:()=>active}).node;
 assert.equal(pending.length,0);
 const create=byText(node,'生成固定对照预览').click();await byText(node,'生成固定对照预览').click();assert.equal(pending.length,1);assert.equal(pending[0].options.body.expectedCandidateHash,'skill-hash');assert.equal(pending[0].options.body.repetitions,2);
 const batch={id:'paired',hash:'paired-hash',title:'Paired <script>',candidateId:'skill',candidateHash:'skill-hash',plans:[{planId:'p1',planHash:'ph',caseKey:'a',sample:1,arm:'dynamic'},{planId:'p2',planHash:'ph2',caseKey:'a',sample:1,arm:'fast'}]};
 const view={benchmark:batch,execution:{status:'planned',next:0,total:2}};await result(create,view);assert.equal(pending.length,0,'preview automatically executed');assert(node.textContent.includes('Paired <script>'));assert(!all(node).some(n=>n.tagName==='SCRIPT'));
 await byText(node,'开始或继续整个对照批次').click();assert.equal(pending.length,0,'unchecked batch executed');
 field(node,'确认重复对照真实执行').checked=true;const start=byText(node,'开始或继续整个对照批次').click();assert.equal(pending[0].options.body.confirmExecute,true);assert.equal(pending[0].options.body.expectedHash,'paired-hash');await result(start,{...view,execution:{status:'running',next:0,total:2,sessionId:'s1'}});assert.equal(field(node,'确认重复对照真实执行').checked,undefined);assert(node.textContent.includes('关闭窗口不停止'));
 const stop=byText(node,'停止后续对照调度').click();assert(!('confirmExecute' in pending[0].options.body));await result(stop,{...view,execution:{status:'stopped',next:0,total:2}});assert(node.textContent.includes('已开始的运行继续'));
 const metric={count:0,missing:1,mean:null,min:null,max:null,total:null},arm={passed:0,total:1,failed:0,unknown:1,passRate:null,fastCompleted:0,fallbacks:0,metrics:{modelCalls:metric,reportedTokens:metric,runMillis:metric}};
 const report={id:'report',benchmarkId:'paired',benchmarkHash:'paired-hash',candidateId:'skill',candidateHash:'skill-hash',status:'partial',dynamic:arm,fast:arm,pairedDelta:arm.metrics,warnings:['Missing <script>']};
 await result(byText(node,'汇总并保存对照报告').click(),report);assert(node.textContent.includes('未知'));assert(node.textContent.includes('缺失 1'));assert(!byText(node,'启用此验证通过的 Skill'));
 const prior=node.textContent;await result(byText(node,'汇总并保存对照报告').click(),{...report,benchmarkHash:'foreign',id:'FOREIGN'});assert(node.textContent.includes('不匹配'));assert(!node.textContent.includes('FOREIGN'));
 const detail=byText(node,'读取所选对照案例').click();assert.equal(pending[0].url,'/kun-case-runs/p1');await result(detail,{preview:{hash:'ph',benchmarkId:'paired',effectiveInput:'literal <script>'}});assert(node.textContent.includes('literal <script>'));
 const late=byText(node,'刷新对照进度').click();active=false;await result(late,{benchmark:{...batch,title:'LATE'},execution:{status:'completed',next:2,total:2}});assert(!node.textContent.includes('LATE'));
 const proof={version:'0.41.0',passed:true,scope:'shipped UI in Node DOM fixture; browser layout not tested',checks:['preview makes no execution request','duplicate clicks suppressed','whole-batch consent and exact hash','stop does not claim in-flight cancellation','unknown metrics show known/missing counts','reports cannot promote candidates','response bindings checked','literal rendering and closed response suppression']};
 fs.writeFileSync(root+'/docs/kun-benchmarks-ui-validation.json',JSON.stringify(proof,null,2)+'\n');console.log(JSON.stringify(proof));
})().catch(e=>{console.error(e);process.exitCode=1});
