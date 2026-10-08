// Shipped view + DOM stub. This checks semantics, not browser layout.
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
class Element{
 constructor(tag,attrs={},...children){this.tagName=tag.toUpperCase();this.attrs=attrs;this.children=[];this.listeners={};this.value=attrs.value??'';this.disabled=false;this.append(...children);}
 append(...children){this.children.push(...children.flat().filter(n=>n!=null));}
 replaceChildren(...children){this.children=[];this.append(...children);}
 set textContent(v){this.children=[v]}get textContent(){return this.children.map(n=>n instanceof Element?n.textContent:String(n??'')).join('')}
 addEventListener(type,fn){this.listeners[type]=fn}emit(type){this.listeners[type]?.()}
}
const el=(...a)=>new Element(...a),button=(label,click)=>{const b=el('button',{},label);b.click=click;return b};
const all=n=>[n,...n.children.filter(c=>c instanceof Element).flatMap(all)],byText=(n,s)=>all(n).find(v=>v.tagName==='BUTTON'&&v.textContent===s);
const pending=[],api=(url,options)=>{assert(!options?.method||options.method==='GET','comparison sent a write');return new Promise((resolve,reject)=>pending.push({url,resolve,reject}))};
const ctx=vm.createContext({window:{},el,button,api,encodeURIComponent,URLSearchParams,JSON,console});
const root=path.resolve(__dirname,'..');for(const f of ['kun-inspect.js','kun-compare.js'])vm.runInContext(fs.readFileSync(root+'/internal/web/'+f,'utf8'),ctx);
const preview={id:'one',targetSessionId:'target',origin:{sessionId:'source',runId:'source-run',sequence:5,through:30,mode:'hybrid'}};
const side=(overrides={})=>({sessionId:'source',runId:'source-run',mode:'source',through:10,eventCount:5,status:'completed',complete:true,harness:{id:'tool-loop-v1'},modelCalls:1,planningCalls:0,reportedTokens:0,tokenUsageComplete:false,activeMillis:null,waitMillis:null,tools:[],lastReply:null,warnings:['usage missing'],...overrides});
const report=(against='')=>({schema:1,previewId:'one',against,baseline:{runId:'source-run',sequence:5,through:30,model:'model',step:4,budget:{reportedTokens:50,toolCalls:3}},left:side(against?{sessionId:'other-target',mode:'live',previewId:against}:{}),right:side({sessionId:'target',mode:'hybrid',through:0,eventCount:0,complete:false,lastReply:{text:'<script>literal</script>',characters:24,truncated:false,hash:'hash',eventId:2}}),delta:{modelCalls:null,reportedTokens:null,activeMillis:null,waitMillis:null},warnings:['not a benchmark']});
(async()=>{
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
 const validation={version:'0.37.0',passed:true,scope:'Node DOM stub + shipped view; no browser layout acceptance',checks:['GET only and explicit read','duplicate request guard','unknown and partial usage','literal reply text','zero cursor retained','same-source candidates','selection invalidates pinned view','late and closed responses ignored','branch response identity']};fs.writeFileSync(root+'/docs/kun-compare-ui-validation.json',JSON.stringify(validation,null,2)+'\n');console.log(JSON.stringify(validation));
})().catch(e=>{console.error(e);process.exitCode=1});
