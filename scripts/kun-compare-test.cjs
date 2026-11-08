// Named offline contracts for shipped views; no browser layout claims.
const assert = require('node:assert/strict'), fs = require('node:fs'), vm = require('node:vm'), path = require('node:path');
const {Element, el, button, all, find, byText, field, get, clone} = require('./fixtures/dom.cjs');
const {runSuite} = require('./fixtures/suite.cjs');

async function runComparison() {
// Shipped view + DOM stub. This checks semantics, not browser layout.
const pending=[],api=(url,options)=>{assert(!options?.method||options.method==='GET','comparison sent a write');return new Promise((resolve,reject)=>pending.push({url,resolve,reject}))};
const ctx=vm.createContext({window:{},el,button,api,encodeURIComponent,URLSearchParams,JSON,console});
const root=path.resolve(__dirname,'..');for(const f of ['kun-inspect.js','kun-compare.js'])vm.runInContext(fs.readFileSync(root+'/internal/web/'+f,'utf8'),ctx);
const preview={id:'one',targetSessionId:'target',origin:{sessionId:'source',runId:'source-run',sequence:5,through:30,mode:'hybrid'}};
const side=(overrides={})=>({sessionId:'source',runId:'source-run',mode:'source',through:10,eventCount:5,status:'completed',complete:true,harness:{id:'tool-loop-v1'},modelCalls:1,planningCalls:0,reportedTokens:0,tokenUsageComplete:false,activeMillis:null,waitMillis:null,tools:[],lastReply:null,warnings:['usage missing'],...overrides});
const report=(against='')=>({schema:1,previewId:'one',against,baseline:{runId:'source-run',sequence:5,through:30,model:'model',step:4,budget:{reportedTokens:50,toolCalls:3}},left:side(against?{sessionId:'other-target',mode:'live',previewId:against}:{}),right:side({sessionId:'target',mode:'hybrid',through:0,eventCount:0,complete:false,lastReply:{text:'<script>literal</script>',characters:24,truncated:false,hash:'hash',eventId:2}}),delta:{modelCalls:null,reportedTokens:null,activeMillis:null,waitMillis:null},warnings:['not a benchmark']});

 let active=true;const view=ctx.window.RunDeskKunCompare.create({preview,isActive:()=>active}),node=view.node;
 const latest=byText(node,'读取最新对照'),repeat=byText(node,'重读固定范围'),load=byText(node,'加载同源分支'),choice=all(node).find(n=>n.tagName==='SELECT');
 assert(repeat.disabled);assert.equal(pending.length,0,'opening comparison queried or executed');
 const first=latest.click();await latest.click();assert.equal(pending.length,1,'duplicate query');assert.equal(pending[0].url,'/kun-forks/comparisons/one');pending[0].resolve(report());await first;
 assert(node.textContent.includes('不可计算'));assert(node.textContent.includes('未知'));assert(node.textContent.includes('不完整'));assert(node.textContent.includes('<script>literal</script>'));assert(!all(node).some(n=>n.tagName==='SCRIPT'));assert(!repeat.disabled);
 const fixed=repeat.click();assert(pending[1].url.includes('leftThrough=10'));assert(pending[1].url.includes('rightThrough=0'),'empty cursor omitted');pending[1].resolve(report());await fixed;
 const candidates=load.click();await load.click();assert.equal(pending.length,3);pending[2].resolve({items:[{id:'one',origin:preview.origin},{id:'other',title:'same checkpoint',origin:{...preview.origin,mode:'live'}},{id:'foreign',title:'wrong source',origin:{...preview.origin,sessionId:'foreign'}}],nextOffset:3,hasMore:false});await candidates;
 assert(choice.children.some(n=>n.attrs.value==='other'));assert(!choice.children.some(n=>n.attrs.value==='one'||n.attrs.value==='foreign'));assert(load.disabled);
 const late=latest.click();choice.value='other';choice.emit('change');assert(repeat.disabled);pending[3].resolve(report());await late;assert(!node.textContent.includes('<script>literal</script>'),'stale result restored old selection');
 const pair=latest.click();assert.equal(pending[4].url,'/kun-forks/comparisons/one?against=other');pending[4].resolve(report('other'));await pair;assert(node.textContent.includes('other-target'));
 const closed=latest.click();active=false;pending[5].resolve({...report('other'),baseline:{...report('other').baseline,model:'late closed result'}});await closed;assert(!node.textContent.includes('late closed result'));
 const validation={version:'0.45.0',passed:true,scope:'Node DOM stub + shipped view; no browser layout acceptance',checks:['GET only and explicit read','duplicate request guard','unknown and partial usage','literal reply text','zero cursor retained','same-source candidates','selection invalidates pinned view','late and closed responses ignored','branch response identity']};return validation;
}

async function runHypothesis() {
// Shipped DOM behavior. No browser layout claims.
const pending=[],api=(url,options)=>new Promise((resolve,reject)=>pending.push({url,options,resolve,reject}));
const ctx=vm.createContext({window:{},el,button,api,encodeURIComponent,TextEncoder,JSON,console});
const root=path.resolve(__dirname,'..');for(const f of ['kun-inspect.js','kun-hypothesis.js'])vm.runInContext(fs.readFileSync(root+'/internal/web/'+f,'utf8'),ctx);
const preview={id:'parent',hash:'parent-hash',origin:{mode:'hybrid'}},record={position:0,sequence:12,tool:'read_file',status:'failed',recordHash:'record-hash',outputHash:'original-hash',outputBytes:9000,preview:'<script>original</script>',redacted:true,truncated:true};
const reviews={previewId:preview.id,previewHash:preview.hash,items:[record,{...record,position:1,sequence:20,recordHash:'second-record'}]};
const child=output=>({id:'child',hash:'child-hash',origin:{mode:'hybrid'},hypothesis:{parentPreviewId:'parent',parentHash:'parent-hash',recordHash:'record-hash',sourceSequence:12,position:0,reason:'test',output}});

 let active=true,created=[];
 const make=()=>ctx.window.RunDeskKunHypothesis.create({preview,isActive:()=>active,onCreated:p=>created.push(p)}).node;
 const node=make(),load=byText(node,'读取可替换结果');assert.equal(pending.length,0);
 const reading=load.click();await load.click();assert.equal(pending.length,1);assert.equal(pending[0].url,'/kun-forks/recordings/parent');pending[0].resolve(reviews);await reading;assert(!load.disabled,'loading never released');
 assert(node.textContent.includes('<script>original</script>'));assert(!all(node).some(n=>n.tagName==='SCRIPT'));
 const output=field(node,'完整假设输出'),reason=field(node,'替换理由'),save=byText(node,'保存假设固定预览'),select=field(node,'替换哪条工具结果');
 assert.equal(output.value,'','display snippet silently became editable original');await save.click();assert.equal(pending.length,1,'missing reason sent');
 reason.value='reason';reason.emit('input');output.value='界'.repeat(22000);output.emit('input');await save.click();assert.equal(pending.length,1,'UTF-8 byte limit bypassed');
 output.value='different';output.emit('input');select.value='1';select.emit('change');assert.equal(output.value,'','old replacement survived record selection');select.value='0';select.emit('change');
 const saving=save.click();await save.click();assert.equal(pending.length,2);assert.equal(pending[1].url,'/kun-forks/parent/hypotheses');assert.equal(pending[1].options.method,'POST');assert.equal(pending[1].options.body.output,'');assert.equal(pending[1].options.body.position,0);assert.equal(pending[1].options.body.expectedHash,'parent-hash');assert.equal(pending[1].options.body.recordHash,'record-hash');pending[1].resolve(child(''));await saving;assert.equal(created.length,1);assert.equal(pending.length,2,'preview automatically started branch');
 const fixed=ctx.window.RunDeskKunHypothesis.create({preview:child('<script>assumed</script>')}).node;assert(fixed.textContent.includes('人工假设'));assert(fixed.textContent.includes('<script>assumed</script>'));assert(!all(fixed).some(n=>n.tagName==='SCRIPT'||n.tagName==='TEXTAREA'));assert(!byText(fixed,'读取可替换结果'),'nested editor allowed');
 const failed=load.click();pending[2].resolve({...reviews,previewHash:'wrong'});await failed;assert(node.textContent.includes('身份不匹配'));assert(!field(node,'完整假设输出'));
 const stale=load.click();active=false;pending[3].resolve(reviews);await stale;assert(!field(node,'完整假设输出'),'closed view applied late response');
 active=true;const node2=make();const read2=byText(node2,'读取可替换结果').click();pending[4].resolve(reviews);await read2;
 field(node2,'替换理由').value='reason';const post2=byText(node2,'保存假设固定预览').click();active=false;pending[5].resolve(child(''));await post2;assert.equal(created.length,1,'stale child changed active preview');
 const live=ctx.window.RunDeskKunHypothesis.create({preview:{origin:{mode:'live'}}}).node;assert(!byText(live,'读取可替换结果'));
 const validation={version:'0.45.0',passed:true,scope:'Node DOM stub + shipped view; no browser layout acceptance',checks:['explicit bounded record reads','response identity bound to parent hash','literal text rendering','display snippets never copied into replacement','UTF-8 byte limit and reason','selection clears replacement','explicit empty replacement','fixed child preview only, no automatic start','duplicate requests guarded','late/closed responses ignored','Live and nested edits disabled']};return validation;
}

module.exports = {runComparison, runHypothesis};
if (require.main === module) runSuite(__filename, [
  {name: 'fixed-comparison', run: runComparison, report: 'kun-compare-ui-validation.json'},
  {name: 'output-hypothesis', run: runHypothesis, report: 'kun-hypothesis-ui-validation.json'},
]).catch(error => {console.error(error); process.exitCode = 1;});
