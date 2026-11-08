// Named offline contracts for shipped views; no browser layout claims.
const assert = require('node:assert/strict'), fs = require('node:fs'), vm = require('node:vm'), path = require('node:path');
const {Element, el, button, all, find, byText, field, get, clone} = require('./fixtures/dom.cjs');
const {runSuite} = require('./fixtures/suite.cjs');

async function runEvaluation() {
// Shipped components, a DOM stub, controlled requests; no browser layout claim.
const pending=[],api=(url,options)=>new Promise((resolve,reject)=>pending.push({url,options,resolve,reject}));
const ctx=vm.createContext({window:{},el,button,api,encodeURIComponent,URLSearchParams,JSON,console});
const root=path.resolve(__dirname,'..');for(const f of ['kun-inspect.js','kun-evaluation.js','kun-compare.js'])vm.runInContext(fs.readFileSync(root+'/internal/web/'+f,'utf8'),ctx);
const preview={id:'branch',hash:'preview-hash',targetSessionId:'target',origin:{sessionId:'source',runId:'source-run',sequence:10,through:14,mode:'hybrid'}};
const rubric={id:'rule',title:'<script>rule</script>',hash:'rule-hash',baselineHash:'baseline-hash',evaluator:'reply-rules-v1',rules:[{kind:'reply_contains',value:'needle'}]};
const side=(id,overrides={})=>({sessionId:id,runId:id+'-run',mode:'hybrid',through:0,status:'no_records',complete:false,eventCount:0,reportedTokens:0,modelCalls:0,planningCalls:0,tools:[],warnings:[],...overrides});
const comparison={previewId:'branch',against:'',hash:'comparison-hash',baseline:{hash:'baseline-hash',runId:'source-run',sequence:10,through:14,budget:{}},left:side('source',{through:12}),right:side('target'),delta:{},warnings:[]};
const result={status:'unknown',passed:0,failed:0,unknown:2,total:2,score:null,reply:{status:'incomplete',bytes:0},results:[{index:0,kind:'run_completed',status:'unknown',reason:'incomplete_records'},{index:1,kind:'reply_contains',status:'unknown',reason:'incomplete_records'}]};
const report={id:'report',previewId:'branch',evaluator:'reply-rules-v1',createdAt:'now',rubric,comparison,left:result,right:result,scoreDelta:null,warnings:['not independent validation']};

 let active=true;const view=ctx.window.RunDeskKunEvaluation.create({preview,isActive:()=>active}),node=view.node;
 const save=byText(node,'保存固定规则'),evaluate=byText(node,'按当前固定范围评分并保存');assert(evaluate.disabled);assert.equal(pending.length,0,'opening scored or loaded automatically');
 await save.click();assert.equal(pending.length,0,'empty rule submitted');field(node,'文本规则内容').value='needle';field(node,'文本规则内容').emit('input');
 const saving=save.click();await save.click();assert.equal(pending.length,1);assert.equal(pending[0].url,'/kun-rubrics');assert.equal(pending[0].options.body.expectedPreviewHash,preview.hash);pending[0].resolve(rubric);await saving;assert(!save.disabled);assert(evaluate.disabled);
 view.setComparison(comparison);assert(!evaluate.disabled);const grading=evaluate.click();await evaluate.click();assert.equal(pending.length,2);assert.equal(pending[1].url,'/kun-evaluations');assert.equal(pending[1].options.body.rightThrough,0);assert.equal(pending[1].options.body.leftThrough,12);assert.equal(pending[1].options.body.expectedComparisonHash,comparison.hash);pending[1].resolve(report);await grading;assert(node.textContent.includes('未知'));assert(node.textContent.includes('不可计算'));assert(node.textContent.includes('<script>rule</script>'));assert(!all(node).some(n=>n.tagName==='SCRIPT'));
 field(node,'文本规则内容').value='new rule';field(node,'文本规则内容').emit('input');assert(evaluate.disabled,'edited rules left previous rubric active');assert(!node.textContent.includes('已保存评分 ·'));
 const choice=field(node,'已保存评分规则');choice.value='rule';const choosing=choice.emit('change');pending[2].resolve(rubric);await choosing;
 const stale=evaluate.click();view.setComparison(null);pending[3].resolve(report);await stale;assert(evaluate.disabled);assert(!node.textContent.includes('已保存评分 ·'),'stale score restored old range');
 const load=byText(node,'读取已保存评分'),loading=load.click();pending[4].resolve({items:[{id:'old-report',title:'saved',createdAt:'before',leftStatus:'pass',rightStatus:'fail'}],nextOffset:1,hasMore:false});await loading;assert(load.disabled);
 const opening=byText(node,'saved · before · 通过 / 未通过').click();assert.equal(pending[5].url,'/kun-evaluations/old-report');pending[5].resolve({...report,id:'old-report'});await opening;assert(node.textContent.includes('已保存评分 ·'),'saved report requires current comparison');
 view.setComparison(comparison);const closing=evaluate.click();active=false;pending[6].resolve({...report,rubric:{...rubric,title:'late closed result'}});await closing;assert(!node.textContent.includes('late closed result'));
 // Full comparison widget supplies/invalidates the fixed range to scoring.
 active=true;const integrated=ctx.window.RunDeskKunCompare.create({preview,isActive:()=>active}).node;assert(integrated.textContent.includes('固定规则评分'));
 const reading=byText(integrated,'读取最新对照').click();pending[7].resolve(comparison);await reading;assert(integrated.textContent.includes('评分将固定使用上方对照的两侧范围。'));
 field(integrated,'对照来源或同源分支').value='other';field(integrated,'对照来源或同源分支').emit('change');assert(byText(integrated,'按当前固定范围评分并保存').disabled);
 assert(pending.every(p=>!p.url.includes('/start')&&!p.url.includes('/turns')),'evaluation started a runtime');
 const validation={version:'0.45.0',passed:true,scope:'Node DOM stub + shipped scoring and comparison components; no browser layout acceptance',checks:['explicit immutable rubric creation','input changes invalidate rubric','duplicate request guard','literal text and unknown scores','zero host cursor retained','fixed comparison and rubric hashes sent','range change/closed responses ignored','saved report opened without source query','comparison integrates and invalidates scoring','no runtime commands']};return validation;
}

async function runTaskSets() {
const cases=n=>all(n).filter(v=>v.tagName==='FIELDSET');
const pending=[],requests=[],api=(url,options)=>new Promise((resolve,reject)=>{const r={url,options,resolve,reject};pending.push(r);requests.push(r)});
const root=path.resolve(__dirname,'..'),ctx=vm.createContext({window:{},document:{addEventListener(){}},el,button,api,encodeURIComponent,TextEncoder,URLSearchParams,JSON,console});
vm.runInContext(fs.readFileSync(root+'/internal/web/kun-task-sets.js','utf8'),ctx);
const saved=(body,id='set-one')=>({schema:1,evaluator:'reply-rules-v1',id,hash:'hash-'+id,createdAt:'2026-10-08',caseCount:body.cases.length,...structuredClone(body)});
const edit=(node,label,value)=>{const f=field(node,label);f.value=value;f.emit('input');};

 let active=true;const n=ctx.window.RunDeskKunTaskSets.create({isActive:()=>active}).node;assert.equal(requests.length,0);
 const save=byText(n,'保存固定任务集');await save.click();assert.equal(requests.length,0,'invalid input submitted');
 edit(cases(n)[0],'固定输入','  原始输入\n保留空白  ');edit(cases(n)[0],'规则内容','<script>literal</script>');
 byText(n,'增加案例').click();assert.equal(cases(n).length,2);edit(cases(n)[1],'固定输入','second input');edit(cases(n)[1],'规则内容','second reply');
 edit(cases(n)[1],'案例标识','case-1');await save.click();assert.equal(requests.length,0,'duplicate case key submitted');edit(cases(n)[1],'案例标识','case-2');
 edit(cases(n)[0],'规则内容','界'.repeat(2049));await save.click();assert.equal(requests.length,0,'long rule submitted');edit(cases(n)[0],'规则内容','<script>literal</script>');
 byText(cases(n)[0],'增加产物规则').click();assert(field(cases(n)[0],'产物规则值').disabled);edit(cases(n)[0],'产物相对路径','../outside');await save.click();assert.equal(requests.length,0,'artifact traversal submitted');
 edit(cases(n)[0],'产物相对路径','outputs/result.txt');const ak=field(cases(n)[0],'产物规则类型');ak.value='file_equals';ak.emit('change');edit(cases(n)[0],'产物规则值','  <b>literal artifact</b>\n');assert(!field(cases(n)[0],'产物规则值').disabled);
 const useFixture=field(cases(n)[0],'使用独立评测目录');useFixture.checked=true;useFixture.emit('change');byText(cases(n)[0],'增加固定输入文件').click();edit(cases(n)[0],'固定文件路径','../bad');await save.click();assert.equal(requests.length,0,'invalid fixture path submitted');edit(cases(n)[0],'固定文件路径','seed.txt');edit(cases(n)[0],'固定文件内容','  FIXED <script>seed</script>\n');
 const saving=save.click();await save.click();assert.equal(pending.length,1,'duplicate save');const req=pending.shift();assert.equal(req.url,'/kun-task-sets');assert.equal(req.options.method,'POST');assert.equal(req.options.body.cases[0].input,'  原始输入\n保留空白  ');assert.equal(req.options.body.cases[0].artifactRules[0].value,'  <b>literal artifact</b>\n');assert.equal(req.options.body.cases[0].artifactRules[0].path,'outputs/result.txt');assert.equal(req.options.body.cases[0].workspaceFixture.files[0].content,'  FIXED <script>seed</script>\n');const first=saved(req.options.body);req.resolve(first);await saving;
 assert(n.textContent.includes('尚未执行'));assert(n.textContent.includes('<script>literal</script>'));assert(!all(n).some(v=>v.tagName==='SCRIPT'));assert.equal(requests.length,1,'saving executed cases');
 byText(n,'复制为新草稿').click();assert.equal(cases(n).length,2);assert.equal(field(cases(n)[0],'固定输入').value,first.cases[0].input);assert(!n.textContent.includes('已保存 ·'));assert.equal(field(cases(n)[0],'产物规则值').value,first.cases[0].artifactRules[0].value);assert(field(cases(n)[0],'使用独立评测目录').checked);assert.equal(field(cases(n)[0],'固定文件内容').value,first.cases[0].workspaceFixture.files[0].content);
 edit(cases(n)[0],'固定输入','edited clone');const cloneSave=save.click(),cloneReq=pending.shift();assert(!('id' in cloneReq.options.body)&&!('hash' in cloneReq.options.body));cloneReq.resolve(saved(cloneReq.options.body,'set-two'));await cloneSave;assert.equal(first.cases[0].input,'  原始输入\n保留空白  ');
 const load=byText(n,'读取任务集列表'),loading=load.click();await load.click();assert.equal(pending.length,1,'duplicate list read');const lr=pending.shift();assert(lr.url.includes('offset=0'));lr.resolve({items:[{id:first.id,title:'First',caseCount:2}],hasMore:true,nextOffset:20});await loading;
 const more=byText(n,'更早的任务集').click(),mr=pending.shift();assert(mr.url.includes('offset=20'));mr.resolve({items:[{id:first.id,title:'First',caseCount:2}],hasMore:false,nextOffset:21});await more;
 const reading=byText(n,'First · 2 个案例').click();await byText(n,'First · 2 个案例').click();assert.equal(pending.length,1,'duplicate detail read');pending.shift().resolve(first);await reading;assert(n.textContent.includes('已保存 · '+first.title));
 const wrong=byText(n,'First · 2 个案例').click();pending.shift().resolve({...first,id:'other'});await wrong;assert(n.textContent.includes('身份不匹配'));assert(!n.textContent.includes('已保存 ·'));
 const stale=byText(n,'First · 2 个案例').click();active=false;pending.shift().resolve(first);await stale;assert(!n.textContent.includes('已保存 ·'),'closed detail applied');
 active=true;const secondNode=ctx.window.RunDeskKunTaskSets.create({isActive:()=>active}).node;edit(cases(secondNode)[0],'固定输入','late');edit(cases(secondNode)[0],'规则内容','late');const late=byText(secondNode,'保存固定任务集').click(),last=pending.shift();active=false;last.resolve(saved(last.options.body));await late;assert(!secondNode.textContent.includes('已保存 ·'),'closed save applied');
 assert(requests.every(r=>r.url.startsWith('/kun-task-sets')),'unexpected runtime/evaluation endpoint');
 const report={version:'0.45.0',passed:true,scope:'Node DOM stub + shipped task-set editor; not browser layout acceptance',checks:['fixture opt-in, path validation, literal seed content and immutable copy','artifact path validation, literal value preservation, immutable copy and busy controls','explicit bounded definition save','unique case keys and rule limits','literal input whitespace and HTML preserved','duplicate request guards','immutable copy to new draft','metadata pagination and explicit full read','response identity checked','closed detail/save responses ignored','no execution or grading requests']};
 return report;
}

async function runBenchmarks() {
const cases=n=>all(n).filter(v=>v.tagName==='FIELDSET');
const pending=[],requests=[],api=(url,options)=>new Promise((resolve,reject)=>{const r={url,options,resolve,reject};pending.push(r);requests.push(r)});
const root=path.resolve(__dirname,'..'),ctx=vm.createContext({window:{},document:{addEventListener(){}},el,button,api,encodeURIComponent,TextEncoder,URLSearchParams,JSON,console});
for(const name of ['kun-case-runs','kun-benchmarks','kun-skills'])vm.runInContext(fs.readFileSync(root+'/internal/web/'+name+'.js','utf8'),ctx);
const input=(node,label,value,type='input')=>{const f=field(node,label);f.value=value;f.emit(type);return f};
const result=async(task,value)=>{assert(pending.length);pending.shift().resolve(value);await task};

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
 const proof={version:'0.45.0',passed:true,scope:'shipped UI in Node DOM fixture; browser layout not tested',checks:['preview makes no execution request','duplicate clicks suppressed','whole-batch consent and exact hash','stop does not claim in-flight cancellation','unknown metrics show known/missing counts','reports cannot promote candidates','response bindings checked','literal rendering and closed response suppression']};
 return proof;
}

module.exports = {runEvaluation, runTaskSets, runBenchmarks};
if (require.main === module) runSuite(__filename, [
  {name: 'fixed-evaluation', run: runEvaluation, report: 'kun-evaluation-ui-validation.json'},
  {name: 'immutable-task-sets', run: runTaskSets, report: 'kun-task-sets-ui-validation.json'},
  {name: 'repeated-benchmarks', run: runBenchmarks, report: 'kun-benchmarks-ui-validation.json'},
]).catch(error => {console.error(error); process.exitCode = 1;});
