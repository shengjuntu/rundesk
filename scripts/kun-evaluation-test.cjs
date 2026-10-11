// Shipped components, a DOM stub, controlled requests; no browser layout claim.
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
class Element{
 constructor(tag,attrs={},...children){this.tagName=tag.toUpperCase();this.attrs=attrs;this.children=[];this.listeners={};this.value=attrs.value??'';this.disabled=false;this.append(...children);}
 append(...children){this.children.push(...children.flat().filter(n=>n!=null));}
 replaceChildren(...children){this.children=[];this.append(...children);}
 set textContent(v){this.children=[v]}get textContent(){return this.children.map(n=>n instanceof Element?n.textContent:String(n??'')).join('')}
 addEventListener(type,fn){this.listeners[type]=fn}emit(type){return this.listeners[type]?.()}
}
const el=(...a)=>new Element(...a),button=(label,click)=>{const b=el('button',{},label);b.click=click;return b};
const all=n=>[n,...n.children.filter(c=>c instanceof Element).flatMap(all)],byText=(n,s)=>all(n).find(v=>v.tagName==='BUTTON'&&v.textContent===s),field=(n,label)=>all(n).find(v=>v.attrs['aria-label']===label);
const pending=[],api=(url,options)=>new Promise((resolve,reject)=>pending.push({url,options,resolve,reject}));
const ctx=vm.createContext({window:{},el,button,api,encodeURIComponent,URLSearchParams,JSON,console});
const root=path.resolve(__dirname,'..');for(const f of ['kun-inspect.js','kun-evaluation.js','kun-compare.js'])vm.runInContext(fs.readFileSync(root+'/internal/web/'+f,'utf8'),ctx);
const preview={id:'branch',hash:'preview-hash',targetSessionId:'target',origin:{sessionId:'source',runId:'source-run',sequence:10,through:14,mode:'hybrid'}};
const rubric={id:'rule',title:'<script>rule</script>',hash:'rule-hash',baselineHash:'baseline-hash',evaluator:'reply-rules-v1',rules:[{kind:'reply_contains',value:'needle'}]};
const side=(id,overrides={})=>({sessionId:id,runId:id+'-run',mode:'hybrid',through:0,status:'no_records',complete:false,eventCount:0,reportedTokens:0,modelCalls:0,planningCalls:0,tools:[],warnings:[],...overrides});
const comparison={previewId:'branch',against:'',hash:'comparison-hash',baseline:{hash:'baseline-hash',runId:'source-run',sequence:10,through:14,budget:{}},left:side('source',{through:12}),right:side('target'),delta:{},warnings:[]};
const result={status:'unknown',passed:0,failed:0,unknown:2,total:2,score:null,reply:{status:'incomplete',bytes:0},results:[{index:0,kind:'run_completed',status:'unknown',reason:'incomplete_records'},{index:1,kind:'reply_contains',status:'unknown',reason:'incomplete_records'}]};
const report={id:'report',previewId:'branch',evaluator:'reply-rules-v1',createdAt:'now',rubric,comparison,left:result,right:result,scoreDelta:null,warnings:['not independent validation']};
(async()=>{
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
 const validation={version:'0.41.0',passed:true,scope:'Node DOM stub + shipped scoring and comparison components; no browser layout acceptance',checks:['explicit immutable rubric creation','input changes invalidate rubric','duplicate request guard','literal text and unknown scores','zero host cursor retained','fixed comparison and rubric hashes sent','range change/closed responses ignored','saved report opened without source query','comparison integrates and invalidates scoring','no runtime commands']};fs.writeFileSync(root+'/docs/kun-evaluation-ui-validation.json',JSON.stringify(validation,null,2)+'\n');console.log(JSON.stringify(validation));
})().catch(e=>{console.error(e);process.exitCode=1});
