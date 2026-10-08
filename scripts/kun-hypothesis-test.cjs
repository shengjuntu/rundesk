// Shipped DOM behavior. No browser layout claims.
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
class Element{
 constructor(tag,attrs={},...children){this.tagName=tag.toUpperCase();this.attrs=attrs;this.children=[];this.listeners={};this.value=attrs.value??'';this.disabled=false;this.append(...children);}
 append(...children){this.children.push(...children.flat().filter(n=>n!=null));}
 replaceChildren(...children){this.children=[];this.append(...children);}
 set textContent(v){this.children=[v]}get textContent(){return this.children.map(n=>n instanceof Element?n.textContent:String(n??'')).join('')}
 addEventListener(type,fn){this.listeners[type]=fn}emit(type){this.listeners[type]?.()}
}
const el=(...a)=>new Element(...a),button=(label,click)=>{const b=el('button',{},label);b.click=click;return b};
const all=n=>[n,...n.children.filter(c=>c instanceof Element).flatMap(all)],byText=(n,s)=>all(n).find(v=>v.tagName==='BUTTON'&&v.textContent===s),field=(n,label)=>all(n).find(v=>v.attrs['aria-label']===label);
const pending=[],api=(url,options)=>new Promise((resolve,reject)=>pending.push({url,options,resolve,reject}));
const ctx=vm.createContext({window:{},el,button,api,encodeURIComponent,TextEncoder,JSON,console});
const root=path.resolve(__dirname,'..');for(const f of ['kun-inspect.js','kun-hypothesis.js'])vm.runInContext(fs.readFileSync(root+'/internal/web/'+f,'utf8'),ctx);
const preview={id:'parent',hash:'parent-hash',origin:{mode:'hybrid'}},record={position:0,sequence:12,tool:'read_file',status:'failed',recordHash:'record-hash',outputHash:'original-hash',outputBytes:9000,preview:'<script>original</script>',redacted:true,truncated:true};
const reviews={previewId:preview.id,previewHash:preview.hash,items:[record,{...record,position:1,sequence:20,recordHash:'second-record'}]};
const child=output=>({id:'child',hash:'child-hash',origin:{mode:'hybrid'},hypothesis:{parentPreviewId:'parent',parentHash:'parent-hash',recordHash:'record-hash',sourceSequence:12,position:0,reason:'test',output}});
(async()=>{
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
 const validation={version:'0.37.0',passed:true,scope:'Node DOM stub + shipped view; no browser layout acceptance',checks:['explicit bounded record reads','response identity bound to parent hash','literal text rendering','display snippets never copied into replacement','UTF-8 byte limit and reason','selection clears replacement','explicit empty replacement','fixed child preview only, no automatic start','duplicate requests guarded','late/closed responses ignored','Live and nested edits disabled']};fs.writeFileSync(root+'/docs/kun-hypothesis-ui-validation.json',JSON.stringify(validation,null,2)+'\n');console.log(JSON.stringify(validation));
})().catch(e=>{console.error(e);process.exitCode=1});
