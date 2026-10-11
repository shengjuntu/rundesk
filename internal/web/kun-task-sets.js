// Immutable authored inputs and rules, with separately confirmed case execution.
window.RunDeskKunTaskSets=(()=>{
 const kinds={reply_contains:'最终回复包含',reply_not_contains:'最终回复不包含',reply_equals:'最终回复完全等于'};
 const artifactKinds={file_exists:'文件存在',file_contains:'文件包含',file_equals:'文件完全等于',file_sha256:'文件 SHA-256'};
 const bytes=s=>new TextEncoder().encode(s).length;
 function validate(value){
  const title=s=>typeof s==='string'&&s.trim()&&[...s].length<=120;
  if(!title(value.title))return '任务集标题需要 1–120 个字符。';
  if(!value.cases.length||value.cases.length>20)return '任务集需要 1–20 个案例。';
  let total=bytes(value.title);const keys=new Set();
  for(const c of value.cases){
   if(c.key!==c.key.trim()||!/^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/.test(c.key)||keys.has(c.key))return '案例标识需唯一，使用 1–64 位字母/数字/点/下划线/连字符，并以字母或数字开头。';keys.add(c.key);
   if(!title(c.title))return '每个案例标题需要 1–120 个字符。';
   if(!c.input.trim()||[...c.input].length>8192||bytes(c.input)>32768)return '每个固定输入需要 1–8192 字符，最多 32 KiB。';
   if(!c.rules.length||c.rules.length>12)return '每个案例需要 1–12 条文本规则。';
   let ruleBytes=0;const seen=new Set();
   for(const r of c.rules){const key=JSON.stringify(r);if(!kinds[r.kind]||!r.value.trim()||[...r.value].length>2048||seen.has(key))return '文本规则需为非空、最多 2048 字符且不重复。';seen.add(key);ruleBytes+=bytes(r.value);total+=bytes(r.kind)+bytes(r.value);}
   if(ruleBytes>16384)return '单个案例的规则文本合计不能超过 16 KiB。';
   const artifacts=c.artifactRules||[];if(artifacts.length>8)return '每个案例最多 8 条产物规则。';
   const artifactSeen=new Set();
   for(const r of artifacts){
    const parts=r.path.split('/'),key=JSON.stringify(r);
    if(!r.path||bytes(r.path)>512||/[\\:\x00-\x1f\x7f<>"|?*]/.test(r.path)||parts.some(p=>!p||p==='.'||p==='..'||/[. ]$/.test(p)||/^(CON|PRN|AUX|NUL|COM[0-9]|LPT[0-9])(?:\.|$)/i.test(p)))return '产物路径必须是工作区内的相对文件路径，使用 /，不能包含上级目录或设备名。';
    if(!artifactKinds[r.kind]||bytes(r.value)>8192||artifactSeen.has(key)||(r.kind==='file_exists'&&r.value!=='')||(r.kind==='file_contains'&&!r.value)||(r.kind==='file_sha256'&&!/^[0-9a-f]{64}$/.test(r.value)))return '请检查产物规则类型、内容、SHA-256 和重复项；值最多 8 KiB。';
    artifactSeen.add(key);total+=bytes(r.path)+bytes(r.kind)+bytes(r.value);
   }
   total+=bytes(c.key)+bytes(c.title)+bytes(c.input);
  }
  return total>262144?'任务集文本合计不能超过 256 KiB。':'';
 }
 function create({isActive=()=>true}={}){
  let busy=false,listBusy=false,generation=0,listTicket=0,offset=0,nextCase=1,selected=null;
  const node=el('section',{class:'kun-task-sets'}),notice=el('p',{role:'status'}),list=el('section',{}),fixed=el('section',{}),casesNode=el('div',{}),cases=[];
  const title=el('input',{'aria-label':'任务集标题',value:'固定评测任务集'});
  const save=button('保存固定任务集',saveSet),add=button('增加案例',()=>addCase()),fresh=button('新建空白任务集',()=>draft()),load=button('读取任务集列表',()=>loadList(0));
  function alive(g){return isActive()&&g===generation;}
  function update(){
   save.disabled=add.disabled=fresh.disabled=title.disabled=busy;add.disabled=busy||cases.length>=20;
   for(const c of cases){for(const f of [c.key,c.title,c.input,c.add])f.disabled=busy;c.add.disabled=busy||c.rules.length>=12;c.addArtifact.disabled=busy||c.artifacts.length>=8;for(const r of c.artifacts){r.path.disabled=r.kind.disabled=r.remove.disabled=busy;r.value.disabled=busy||r.kind.value==='file_exists';}c.remove.disabled=busy||cases.length===1;for(const r of c.rules){r.kind.disabled=r.value.disabled=busy;r.remove.disabled=busy||c.rules.length===1;}}
  }
  function changed(){generation++;selected=null;fixed.replaceChildren();notice.textContent='草稿已变化；保存会生成新的固定任务集。';}
  title.addEventListener('input',changed);
  function addRule(c,value={kind:'reply_contains',value:''}){
   if(busy||c.rules.length>=12)return;
   const kind=el('select',{'aria-label':'规则类型'},...Object.entries(kinds).map(([v,text])=>el('option',{value:v},text)));kind.value=value.kind;
   const text=el('textarea',{'aria-label':'规则内容',rows:2});text.value=value.value;
   const r={kind,value:text},remove=button('移除规则',()=>{if(busy||c.rules.length===1)return;c.rules.splice(c.rules.indexOf(r),1);c.ruleNode.replaceChildren(...c.rules.map(x=>x.node));changed();update();});
   r.remove=remove;r.node=el('div',{class:'kun-rule-row'},kind,text,remove);c.rules.push(r);c.ruleNode.append(r.node);
   kind.addEventListener('change',changed);text.addEventListener('input',changed);changed();update();
  }
  function addArtifact(c,value={path:'',kind:'file_exists',value:''}){
   if(busy||c.artifacts.length>=8)return;
   const path=el('input',{'aria-label':'产物相对路径',value:value.path}),kind=el('select',{'aria-label':'产物规则类型'},...Object.entries(artifactKinds).map(([v,text])=>el('option',{value:v},text))),text=el('textarea',{'aria-label':'产物规则值',rows:2});kind.value=value.kind;text.value=value.value||'';
   const r={path,kind,value:text};r.remove=button('移除产物规则',()=>{if(busy)return;c.artifacts.splice(c.artifacts.indexOf(r),1);c.artifactNode.replaceChildren(...c.artifacts.map(v=>v.node));changed();update();});
   r.node=el('div',{class:'kun-rule-row'},el('label',{},'产物相对路径',path),el('label',{},'规则',kind),el('label',{},'验收值',text),r.remove);c.artifacts.push(r);c.artifactNode.append(r.node);
   path.addEventListener('input',changed);text.addEventListener('input',changed);kind.addEventListener('change',()=>{if(kind.value==='file_exists')text.value='';changed();update();});changed();update();
  }
  function addCase(value){
   if(busy||cases.length>=20)return;
   if(!value){while(cases.some(c=>c.key.value==='case-'+nextCase))nextCase++;value={key:'case-'+nextCase,title:'案例 '+nextCase++,input:'',rules:[{kind:'reply_contains',value:''}]};}
   const c={key:el('input',{'aria-label':'案例标识',value:value.key}),title:el('input',{'aria-label':'案例标题',value:value.title}),input:el('textarea',{'aria-label':'固定输入',rows:4}),rules:[],ruleNode:el('div',{}),artifacts:[],artifactNode:el('div',{})};c.input.value=value.input;
   c.remove=button('移除案例',()=>{if(busy||cases.length===1)return;cases.splice(cases.indexOf(c),1);casesNode.replaceChildren(...cases.map(x=>x.node));changed();update();});
   c.add=button('增加规则',()=>addRule(c));c.addArtifact=button('增加产物规则',()=>addArtifact(c));
   c.node=el('fieldset',{class:'kun-task-case'},el('legend',{},'案例'),el('label',{},'案例标识',c.key),el('label',{},'案例标题',c.title),el('label',{},'固定输入',c.input),el('p',{class:'help'},'验收规则：固定要求运行成功结束，再检查以下完整最终回复规则。'),c.ruleNode,el('div',{class:'actions'},c.add),el('p',{class:'help'},'可选产物规则：运行结束时检查工作区相对文件；每个文件最多 1 MiB。保存摘要与结果，之后改文件不改历史评分。此检查不证明文件由本次运行创建。'),c.artifactNode,el('div',{class:'actions'},c.addArtifact,c.remove));
   cases.push(c);casesNode.append(c.node);for(const f of [c.key,c.title,c.input])f.addEventListener('input',changed);for(const r of value.rules)addRule(c,r);for(const r of value.artifactRules||[])addArtifact(c,r);changed();update();
  }
  function draft(value){
   if(busy)return;generation++;selected=null;fixed.replaceChildren();cases.length=0;casesNode.replaceChildren();nextCase=1;title.value=value?value.title:'固定评测任务集';
   for(const c of value?.cases||[undefined])addCase(c);notice.textContent=value?'已复制为新草稿；原任务集保留。':'填写固定输入与规则后保存。';update();
  }
  function render(value){
   selected=value;
   const copy=button('复制为新草稿',()=>{if(!busy&&selected===value)draft(value);});
   fixed.replaceChildren(el('h3',{},'已保存 · '+value.title),el('p',{},value.caseCount+' 个案例 · '+value.createdAt),el('p',{class:'help'},'任务集已固定；新预览尚未执行，运行与评分请在下方读取。'),el('details',{},el('summary',{},'身份与内容指纹'),el('pre',{},JSON.stringify({id:value.id,hash:value.hash,evaluator:value.evaluator},null,2))),copy,
    ...value.cases.map(c=>el('details',{},el('summary',{},c.key+' · '+c.title),el('h4',{},'固定输入'),el('pre',{},c.input),el('p',{},'固定要求：运行成功结束'),...c.rules.map(r=>el('div',{},el('strong',{},kinds[r.kind]),el('pre',{},r.value))),...(c.artifactRules||[]).map(r=>el('div',{},el('strong',{},artifactKinds[r.kind]+' · '+r.path),el('pre',{},r.value||''))))));
   if(window.RunDeskKunCaseRuns)fixed.append(window.RunDeskKunCaseRuns.create({taskSet:value,isActive:()=>isActive()&&selected===value}).node);
  }
  function validResponse(value,id){return value&&value.id&&(!id||id===value.id)&&value.hash&&value.schema===1&&value.evaluator==='reply-rules-v1'&&Array.isArray(value.cases)&&value.caseCount===value.cases.length;}
  async function saveSet(){
   if(busy)return;
   const value={title:title.value,cases:cases.map(c=>({key:c.key.value,title:c.title.value,input:c.input.value,rules:c.rules.map(r=>({kind:r.kind.value,value:r.value.value})),...(c.artifacts.length?{artifactRules:c.artifacts.map(r=>({path:r.path.value,kind:r.kind.value,value:r.value.value}))}:{})}))},error=validate(value);
   if(error){notice.textContent=error;return;}
   const g=generation;busy=true;update();notice.textContent='保存固定任务集…';
   try{const result=await api('/kun-task-sets',{method:'POST',body:value});if(!alive(g))return;if(!validResponse(result))throw Error('任务集响应身份不匹配。');render(result);notice.textContent='任务集已保存；可在列表中重新打开。';}
   catch(e){if(alive(g))notice.textContent=e.message;}
   finally{busy=false;if(isActive())update();}
  }
  async function read(id){
   if(busy)return;const g=++generation;busy=true;selected=null;fixed.replaceChildren();update();notice.textContent='读取固定任务集…';
   try{const result=await api('/kun-task-sets/'+encodeURIComponent(id));if(!alive(g))return;if(!validResponse(result,id))throw Error('任务集响应身份不匹配。');render(result);notice.textContent='已读取固定定义；查看内容不会执行案例。';}
   catch(e){if(alive(g))notice.textContent=e.message;}
   finally{busy=false;if(isActive())update();}
  }
  async function loadList(start=offset){
   if(listBusy)return;listBusy=true;const ticket=++listTicket;load.disabled=true;
   try{const page=await api('/kun-task-sets?'+new URLSearchParams({offset:start,limit:20}));if(!isActive()||ticket!==listTicket)return;offset=start;
    const prev=button('较新的任务集',()=>loadList(Math.max(0,start-20))),next=button('更早的任务集',()=>loadList(page.nextOffset));prev.disabled=start===0;next.disabled=!page.hasMore;
    list.replaceChildren(el('h3',{},'已保存任务集'),...page.items.map(v=>button(v.title+' · '+v.caseCount+' 个案例',()=>read(v.id),'experiment-entry')),el('div',{class:'actions'},prev,next));if(!page.items.length)list.append(el('p',{},'尚无任务集。'));
   }catch(e){if(isActive()&&ticket===listTicket)notice.textContent=e.message;}
   finally{listBusy=false;if(isActive()&&ticket===listTicket)load.disabled=false;}
  }
  node.append(el('p',{class:'help'},'把多个案例的固定输入、回复和产物验收规则保存为不可变任务集。在已保存任务集中创建固定案例执行预览；独立验证批次和评分在 K4 Skill 工作台管理。'),el('div',{class:'actions'},load,fresh),list,notice,fixed,el('details',{open:true},el('summary',{},'任务集草稿'),el('label',{},'任务集标题',title),casesNode,el('div',{class:'actions'},add,save)));
  draft();return {node};
 }
 function open(){let closed=false;const d=el('dialog',{id:'kun-task-sets-workbench',class:'debug-inspector experiment-workbench'});d.append(el('div',{class:'dialog-head'},el('h2',{},'评测任务集'),button('关闭',()=>d.close(),'quiet')),create({isActive:()=>!closed}).node);d.onclose=()=>{closed=true;d.remove();};document.body.append(d);d.showModal();return d;}
 return {create,open};
})();
document.addEventListener('DOMContentLoaded',()=>{const anchor=document.querySelector('#debug-button');if(anchor){const b=button('评测任务集',()=>window.RunDeskKunTaskSets.open());b.id='kun-task-sets-open';b.className='quiet';anchor.after(b);}});
