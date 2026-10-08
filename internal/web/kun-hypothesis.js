// A child preview is immutable. Creating it never starts its session.
window.RunDeskKunHypothesis=(()=>{
 const enc=encodeURIComponent;
 function create({preview,isActive=()=>true,onCreated=()=>{}}){
  const node=el('section',{class:'kun-comparison'},el('h3',{},'修改工具结果 → Hybrid 重算'));
  if(preview.hypothesis){
   const h=preview.hypothesis;
   node.append(el('p',{class:'experiment-mode'},'包含人工假设 · 工具不真实执行'),kunFacts([['父预览',h.parentPreviewId],['录制位置',h.position+1],['来源结果','#'+h.sourceSequence],['原输出指纹',h.originalOutputHash],['假设输出指纹',h.outputHash]]),el('h4',{},'替换理由'),el('pre',{},h.reason),el('h4',{},'固定替换全文'),el('pre',{},h.output===''?'（空字符串）':h.output),el('p',{class:'help'},'工具状态与参数沿用原录制。只有执行到这条结果且严格匹配时才使用替换；是否使用请读取下方对照。需要另一个假设时，从父预览创建。'));
   return {node};
  }
  if(preview.origin.mode!=='hybrid'){node.append(el('p',{},'假设替换仅支持 Hybrid 预览。'));return {node};}
  let generation=0,busy=false,records=null;
  const info=el('p',{role:'status'}),form=el('div',{}),load=button('读取可替换结果',read);
  async function read(){
   if(busy||!isActive())return;const t=++generation;busy=true;load.disabled=true;form.replaceChildren();info.textContent='读取固定录制摘要…';
   try{
    const data=await api('/kun-forks/recordings/'+enc(preview.id));if(!isActive()||t!==generation)return;
    if(data.previewId!==preview.id||data.previewHash!==preview.hash)throw Error('录制摘要身份不匹配，请重新打开预览。');
    records=data.items;info.textContent=records.length?'请选择一条结果，并填写完整替换文本。':'此安全点之后没有可替换结果。';
    if(records.length)renderForm();
   }catch(e){if(isActive()&&t===generation)info.textContent=e.message;}
   finally{if(isActive()&&t===generation){busy=false;load.disabled=false;}}
  }
  function renderForm(){
   const select=el('select',{'aria-label':'替换哪条工具结果'},...records.map(r=>el('option',{value:String(r.position)},`${r.position+1}. ${r.tool} · #${r.sequence} · ${r.status}`)));select.value=String(records[0].position);
   const original=el('div',{}),title=el('input',{'aria-label':'假设分支标题',maxLength:120,value:'工具结果假设'}),reason=el('textarea',{'aria-label':'替换理由',rows:2,maxLength:1000}),output=el('textarea',{'aria-label':'完整假设输出',rows:6,placeholder:'填写完整新输出；允许空字符串。原文节选不会自动作为替换值。'}),size=el('p',{class:'help'});
   const selected=()=>records.find(r=>r.position===Number(select.value));
   function describe(){const r=selected();original.replaceChildren(kunFacts([['原始状态',r.status],['原输出字节',r.outputBytes],['原输出指纹',r.outputHash],['节选脱敏',r.redacted?'是':'未发现结构化敏感字段'],['节选截断',r.truncated?'是':'否']]),el('pre',{},r.preview||'（空输出）'));}
   function edited(){generation++;size.textContent=new TextEncoder().encode(output.value).length+' / 65536 字节';}
   select.addEventListener('change',()=>{edited();output.value='';edited();describe();});
   for(const n of [title,reason,output])n.addEventListener('input',edited);
   const save=button('保存假设固定预览',async()=>{
    if(busy||!isActive())return;
    if(!title.value.trim()||!reason.value.trim()){info.textContent='请填写标题和替换理由。';return;}
    if(new TextEncoder().encode(output.value).length>65536){info.textContent='完整假设输出最多 64 KiB。';return;}
    const r=selected(),t=++generation,body={expectedHash:preview.hash,position:r.position,recordHash:r.recordHash,title:title.value,reason:reason.value,output:output.value};
    busy=true;for(const n of [load,save,select,title,reason,output])n.disabled=true;info.textContent='保存独立假设预览…';
    try{
     const child=await api('/kun-forks/'+enc(preview.id)+'/hypotheses',{method:'POST',body});if(!isActive()||t!==generation)return;
     if(child.hypothesis?.parentPreviewId!==preview.id||child.hypothesis?.parentHash!==preview.hash||child.hypothesis?.recordHash!==r.recordHash||child.hypothesis?.output!==body.output||child.origin?.mode!=='hybrid')throw Error('新预览身份不匹配，请重新读取。');
     info.textContent='假设预览已保存，尚未运行。';await onCreated(child);
    }catch(e){if(isActive()&&t===generation)info.textContent=e.message;}
    finally{busy=false;for(const n of [load,save,select,title,reason,output])n.disabled=false;}
   });
   describe();size.textContent='0 / 65536 字节';form.replaceChildren(el('label',{},'选择录制结果',select),el('p',{class:'help'},'以下最多展示 2048 字符。JSON 中常见敏感字段脱敏；自由文本不保证脱敏。节选仅供核对，不能自动当作完整原文。'),original,el('label',{},'新分支标题',title),el('label',{},'为什么替换',reason),el('label',{},'完整假设输出（可为空）',output),size,el('p',{class:'help'},'只替换选定输出；成功/失败状态、工具身份、参数、预算和补充指令均继承父预览。保存后先核对固定全文，再启动模型重算。'),save);
  }
  node.append(el('p',{class:'help'},'从当前未修改的 Hybrid 预览生成一个新分支，保留原录制。每个分支最多替换一条结果。'),load,info,form);return {node};
 }
 return {create};
})();
