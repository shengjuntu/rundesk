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
 let active=true;const taskSet={id:'set',hash:'set-hash',cases:[{key:'one',title:'One',input:'literal'}]};
 const n=ctx.window.RunDeskKunCaseRuns.create({taskSet,isActive:()=>active}).node;assert.equal(requests.length,0);
 const loading=byText(n,'读取执行环境').click();assert.equal(pending.length,2);pending.shift().resolve([{id:'kun',name:'Kun',agentRuntime:{kind:'kun'},execution:{mode:'local'}},{id:'codex',agentRuntime:{kind:'codex'}}]);pending.shift().resolve([{id:'ws',name:'Workspace'}]);await loading;
 assert.equal(field(n,'执行实例').children.length,1);
 const p={id:'plan',hash:'plan-hash',taskSetId:'set',taskSetHash:'set-hash',case:{title:'One'},effectiveInput:'<script>literal</script>',runId:'run-fixed',sessionId:'sid',workspace:'/tmp/project',config:{allowWrite:true},mcpServers:['weather']};
 const making=byText(n,'生成案例执行预览').click();await byText(n,'生成案例执行预览').click();assert.equal(pending.length,1);assert.equal(pending[0].options.body.expectedTaskSetHash,'set-hash');await result(making,{preview:p,status:'planned'});
 assert(n.textContent.includes('<script>literal</script>'));assert(!all(n).some(v=>v.tagName==='SCRIPT'));assert(byText(n,'执行此固定案例').disabled);
 await byText(n,'执行此固定案例').click();assert.equal(pending.length,0,'unchecked case executed');const confirm=field(n,'确认真实案例执行');confirm.checked=true;confirm.emit('change');assert(!byText(n,'执行此固定案例').disabled);
 const starting=byText(n,'执行此固定案例').click();await byText(n,'执行此固定案例').click();assert.equal(pending.length,1);assert.equal(pending[0].options.body.expectedHash,'plan-hash');await result(starting,{id:'sid',runId:'run-fixed',status:'running'});assert(byText(n,'执行此固定案例').disabled);
 const scoring=byText(n,'读取此案例评分').click();assert(pending[0].url.endsWith('/score'));await result(scoring,{evaluation:{status:'unknown',score:null}});assert(n.textContent.includes('unknown'));
 const refresh=byText(n,'刷新案例状态').click();active=false;await result(refresh,{preview:p,status:'completed',session:{id:'sid',runId:'run-fixed'}});assert(!n.textContent.includes('One · completed'),'closed case response applied');
 active=true;const k=ctx.window.RunDeskKunSkills.create({sourcePlanId:'plan',isActive:()=>active}).node;const count=requests.length;assert.equal(pending.length,0);
 input(k,'适用范围','File copy');input(k,'验证任务集 ID','holdout');input(k,'验证任务集指纹','held-hash');input(k,'Skill 标题','Safe <script> Skill');
 const saving=byText(k,'保存 Skill 候选').click();await byText(k,'保存 Skill 候选').click();assert.equal(pending.length,1);assert.equal(pending[0].options.body.sourcePlanIds[0],'plan');assert(!('steps' in pending[0].options.body));
 const candidate={id:'skill',hash:'skill-hash',title:'Safe <script> Skill',workspaceId:'ws',instanceId:'kun',validationSet:{title:'Held out',caseCount:2},program:{steps:[]}};await result(saving,candidate);assert.equal(requests.length,count+1,'save auto executed');
 const batch={id:'batch',hash:'batch-hash',candidateId:'skill',candidateHash:'skill-hash',plans:[{id:'a'},{id:'b'}]};await result(byText(k,'生成固定验证批次').click(),batch);
 await byText(k,'执行验证批次').click();assert.equal(pending.length,0,'unconfirmed batch executed');assert(k.textContent.includes('勾选确认'));const check=field(k,'确认验证批次真实执行');check.checked=true;
 const run=byText(k,'执行验证批次').click();assert.equal(pending[0].options.body.expectedHash,'batch-hash');await result(run,{results:[]});assert.equal(check.checked,false);
 const report={id:'report',hash:'report-hash',candidateId:'skill',candidateHash:'skill-hash',validationId:'batch',total:2,passed:0,failed:0,unknown:2,score:null,status:'unknown'};await result(byText(k,'汇总并保存验证报告').click(),report);assert(!byText(k,'读取可启用应用'));
 const passed={...report,status:'pass',passed:2,unknown:0,score:100};await result(byText(k,'汇总并保存验证报告').click(),passed);assert(byText(k,'读取可启用应用'));
 await result(byText(k,'读取可启用应用').click(),[{appId:'app',name:'App',instanceId:'kun',workspaceId:'ws',revision:7},{appId:'foreign',instanceId:'other',workspaceId:'ws',revision:1}]);assert.equal(field(k,'启用到应用').children.length,1);
 const promote=byText(k,'启用此验证通过的 Skill').click();assert.equal(pending[0].options.body.expectedAppRevision,7);assert.equal(pending[0].options.body.reportHash,'report-hash');await result(promote,{candidateId:'skill'});assert(k.textContent.includes('已启用到'));
 await result(byText(k,'停用所选应用的此 Skill').click(),{});assert(k.textContent.includes('已停用'));
 const late=byText(k,'读取 Skill 候选').click();active=false;await result(late,{...candidate,title:'LATE'});assert(!k.textContent.includes('LATE'));
 active=true;candidate.program.guard={workspaceMode:'isolated-fixture-v1'};await result(byText(k,'汇总并保存验证报告').click(),passed);assert(!byText(k,'读取可启用应用'));assert(k.textContent.includes('独立目录候选只用于评测'));
 const proof={version:'0.45.0',passed:true,scope:'shipped K4 UI in Node DOM fixture; not browser layout acceptance',checks:['no automatic runtime requests','local Kun environment filtering','fixed identity and explicit execution confirmation','duplicate clicks suppressed','closed responses ignored','literal text rendering','fixed validation batch binding','unknown report cannot promote','application environment filtering and revision binding','explicit disable','isolated candidate never offers shared-application promotion']};fs.writeFileSync(root+'/docs/validation/current/kun-k4-ui-validation.json',JSON.stringify(proof,null,2)+'\n');console.log(JSON.stringify(proof));
})().catch(e=>{console.error(e);process.exitCode=1});
