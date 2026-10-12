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
vm.runInContext(fs.readFileSync(root+'/internal/web/kun-task-sets.js','utf8'),ctx);
const saved=(body,id='set-one')=>({schema:1,evaluator:'reply-rules-v1',id,hash:'hash-'+id,createdAt:'2026-10-08',caseCount:body.cases.length,...structuredClone(body)});
const edit=(node,label,value)=>{const f=field(node,label);f.value=value;f.emit('input');};
(async()=>{
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
 const report={version:'0.44.0',passed:true,scope:'Node DOM stub + shipped task-set editor; not browser layout acceptance',checks:['fixture opt-in, path validation, literal seed content and immutable copy','artifact path validation, literal value preservation, immutable copy and busy controls','explicit bounded definition save','unique case keys and rule limits','literal input whitespace and HTML preserved','duplicate request guards','immutable copy to new draft','metadata pagination and explicit full read','response identity checked','closed detail/save responses ignored','no execution or grading requests']};
 fs.writeFileSync(root+'/docs/kun-task-sets-ui-validation.json',JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report));
})().catch(e=>{console.error(e);process.exitCode=1});
