// Offline recording experiments: no worker, model, tool or control endpoint.
window.RunDeskExperiments=(()=>{
 const labels={recorded:'原始记录',edited:'假设结果',stale:'后续未验证'};
 const short=(s,n=100)=>[...String(s||'')].slice(0,n).join('');
 function open({sessionId='',branchId=''}={}){
  let closed=false,epoch=0,listTicket=0,eventTicket=0,diffTicket=0,current=null,index=0,listOffset=0,eventOffset=0,diffOffset=0,creating=false;
  const d=el('dialog',{id:'experiment-workbench',class:'debug-inspector experiment-workbench'}),notice=el('p',{role:'status'}),list=el('section',{class:'experiment-list'}),head=el('section',{class:'experiment-head'}),rows=el('section',{class:'experiment-events'}),detail=el('section',{class:'experiment-detail'}),diff=el('section',{class:'experiment-diff'});
  const alive=e=>!closed&&e===epoch;
  const get=(path)=>api('/experiments'+path);
  const enc=encodeURIComponent;
  async function loadList(){
   const ticket=++listTicket,offset=listOffset;
   try{
    const p=await get('?'+new URLSearchParams({offset,limit:20,...(sessionId?{sessionId}:{})}));if(closed||ticket!==listTicket)return;
    const prev=button('较新的分支',()=>{listOffset=Math.max(0,offset-20);return loadList();}),next=button('更早的分支',()=>{listOffset=p.nextOffset;return loadList();});prev.disabled=offset===0;next.disabled=!p.hasMore;
    list.replaceChildren(el('h3',{},sessionId?'此来源的实验分支':'全部离线实验'),...p.items.map(b=>button(b.title+' · '+(b.patchCount?b.patchCount+' 个假设':b.parentId?'无替代值':'原始基线'),()=>selectBranch(b.id),'experiment-entry')),el('div',{class:'actions'},prev,next));
    if(!p.items.length)list.append(el('p',{class:'help'},'尚无离线实验。在 Kun“调试检查”中选择轮次后创建。'));
   }catch(e){if(!closed&&ticket===listTicket)notice.textContent=e.message;}
  }
  async function selectBranch(id){
   const e=++epoch;current=null;eventTicket++;diffTicket++;index=0;eventOffset=diffOffset=0;head.replaceChildren();rows.replaceChildren();detail.replaceChildren();diff.replaceChildren();notice.textContent='正在读取固定记录包…';
   try{
    const result=await get('/'+enc(id));if(!alive(e))return;current=result.branch;
    const lineage=el('div',{class:'experiment-lineage'},...result.lineage.map(v=>button(v.depth+' · '+short(v.title,40),()=>selectBranch(v.id),'quiet')));
    head.replaceChildren(el('h3',{},current.title),el('p',{class:'experiment-mode'},'记录编辑回放 · 未执行模型或工具'),el('p',{class:'help'},current.source.title+' · '+current.source.runId+' · 固定宿主记录 ≤ #'+current.source.through),el('p',{},result.counts.recorded+' 条原始记录 · '+result.counts.edited+' 个假设结果 · '+result.counts.stale+' 条后续未验证'),el('h4',{},'来源谱系'),lineage,el('details',{},el('summary',{},'固定来源与内容指纹'),el('pre',{},JSON.stringify({branchId:current.id,rootId:current.rootId,parentId:current.parentId,source:current.source,bundleHash:current.bundleHash,contentHash:current.contentHash},null,2))));
    notice.textContent='编辑工具返回文本会创建子分支；原始事件及父分支保持不变。';
    await Promise.all([loadEvents(e),loadDiff(e)]);
   }catch(err){if(alive(e))notice.textContent=err.message;}
  }
  async function loadEvents(e=epoch){
   if(!current)return;const b=current,offset=eventOffset;
   try{
    const p=await get('/'+enc(b.id)+'/events?'+new URLSearchParams({offset,limit:20}));if(!alive(e)||current.id!==b.id||offset!==eventOffset)return;
    const prev=button('上一页记录',()=>{eventOffset=Math.max(0,offset-20);return loadEvents();}),next=button('下一页记录',()=>{eventOffset=p.nextOffset;return loadEvents();});prev.disabled=offset===0;next.disabled=!p.hasMore;
    rows.replaceChildren(el('h4',{},'固定事件序列'),...p.items.map(v=>{const n=button('',()=>selectEvent(v));n.className='experiment-event '+v.classification;n.append(el('strong',{},'#'+v.id+' · '+(v.toolName||v.method)),el('small',{},labels[v.classification]+(v.downstreamUnverified&&v.classification==='edited'?' · 上游也有假设':'')));return n;}),el('div',{class:'actions'},prev,el('span',{},(p.total?offset+1:0)+'–'+p.nextOffset+' / '+p.total),next));
   }catch(err){if(alive(e))notice.textContent=err.message;}
  }
  async function move(delta){
   if(!current)return;const e=epoch,b=current,target=index+delta,ticket=++eventTicket;
   if(target<0||target>=b.eventCount)return;
   const p=await get('/'+enc(b.id)+'/events?'+new URLSearchParams({offset:target,limit:1}));if(alive(e)&&ticket===eventTicket&&current.id===b.id&&p.items.length)await selectEvent(p.items[0]);
  }
  async function selectEvent(v,start=0){
   if(!current)return;const e=epoch,ticket=++eventTicket,b=current;index=v.index;detail.replaceChildren(el('p',{},'读取固定事件…'));
   try{
    const p=await get('/'+enc(b.id)+'/events/'+v.id+'?'+new URLSearchParams({offset:start,limit:4000}));if(!alive(e)||ticket!==eventTicket)return;
    const prior=button('上一条',()=>move(-1)),next=button('下一条',()=>move(1)),back=button('上一块原文',()=>selectEvent(v,Math.max(0,start-4000))),more=button('下一块原文',()=>selectEvent(v,p.nextOffset));prior.disabled=index===0;next.disabled=index===b.eventCount-1;back.disabled=start===0;more.disabled=!p.hasMore;
    detail.replaceChildren(el('h4',{},'事件 #'+v.id+' · '+labels[p.event.classification]),el('div',{class:'actions'},prior,el('span',{},'回放位置 '+(index+1)+' / '+b.eventCount),next),el('p',{class:'help'},p.event.downstreamUnverified?'此记录位于编辑点之后，未沿新路径重算，不能作为分支成功的证据。':'这是固定原始记录；假设值不会改变已经发生的事实。'),el('p',{class:'help'},'原始已脱敏 JSON · 字符 '+start+'–'+p.nextOffset+' / '+p.totalCharacters),el('pre',{class:'experiment-original'},p.originalChunk),el('div',{class:'actions'},back,more));
    if(p.event.patch)detail.append(el('h4',{},'当前假设返回值（未执行）'),el('pre',{class:'experiment-replacement'},p.event.patch.text),el('p',{},'编辑理由：'+p.event.patch.reason));
    if(p.event.editable){
     const title=el('input',{'aria-label':'子分支标题',maxLength:120,value:short(b.title+' · 修改 #'+v.id,120)}),text=el('textarea',{'aria-label':'假设返回文本',rows:4,maxLength:16000,placeholder:'输入假设的工具返回文本；可留空模拟空返回。'}),reason=el('textarea',{'aria-label':'编辑理由',rows:2,maxLength:2000,placeholder:'说明为什么修改，以及想检验什么。'});text.value=p.event.patch?.text||'';
     const status=el('p',{role:'status'});
     const save=button('保存为子分支',()=>fork('replace')),restore=button('恢复原值并新建子分支',()=>fork('restore'));restore.disabled=!p.event.patch;
     async function fork(operation){
      if(creating)return;if(!title.value.trim()||!reason.value.trim()){status.textContent='请填写子分支标题和编辑理由。';return;}
      creating=true;save.disabled=restore.disabled=true;status.textContent='保存离线子分支…';
      try{const child=await api('/experiments/'+enc(b.id)+'/branches',{method:'POST',body:{title:title.value,expectedParentHash:b.contentHash,change:{eventId:v.id,operation,reason:reason.value,...(operation==='replace'?{text:text.value}:{})}}});if(alive(e)){await loadList();await selectBranch(child.id);}else toast('子分支已保存，可从离线实验列表打开。');}
      catch(err){if(alive(e))status.textContent=err.message;}
      finally{creating=false;if(alive(e)){save.disabled=false;restore.disabled=!p.event.patch;}}
     }
     detail.append(el('h4',{},'编辑观察结果'),el('p',{class:'help'},'仅替代实验中的返回文本，不改调用参数、审批或真实结果。编辑点之后的所有记录将保守标为未验证。'),el('label',{},'子分支标题',title),el('label',{},'假设返回文本',text),el('label',{},'编辑理由',reason),el('div',{class:'actions'},save,restore),status);
    }
   }catch(err){if(alive(e)&&ticket===eventTicket)detail.replaceChildren(el('p',{class:'error'},err.message));}
  }
  async function loadDiff(e=epoch){
   if(!current)return;const b=current,ticket=++diffTicket,offset=diffOffset;
   try{
    const p=await get('/'+enc(b.id)+'/diff?'+new URLSearchParams({offset,limit:16}));if(!alive(e)||ticket!==diffTicket)return;
    const prev=button('上一页差异',()=>{diffOffset=Math.max(0,offset-16);return loadDiff();}),next=button('下一页差异',()=>{diffOffset=p.nextOffset;return loadDiff();});prev.disabled=offset===0;next.disabled=!p.hasMore;
    diff.replaceChildren(el('h4',{},'相对父分支的差异 · '+p.total+' 条'),el('p',{class:'help'},'原始事件不变；这里比较假设文本和验证状态，不推算成功率、成本或性能改善。'));
    for(const v of p.items){const item=el('details',{},el('summary',{},'#'+v.eventId+' · '+labels[v.before.classification]+' → '+labels[v.after.classification]));if(v.before.patch||v.after.patch)item.append(el('p',{},'修改前'),el('pre',{},v.before.patch?.text??'使用原始记录'),el('p',{},'修改后'),el('pre',{},v.after.patch?.text??'使用原始记录'));item.append(button('查看事件 #'+v.eventId,()=>selectEvent(v.after)));diff.append(item);}
    diff.append(el('div',{class:'actions'},prev,next));
   }catch(err){if(alive(e)&&ticket===diffTicket)diff.replaceChildren(el('p',{class:'error'},err.message));}
  }
  d.append(el('div',{class:'dialog-head'},el('h2',{},'离线实验'),button('关闭',()=>d.close(),'quiet')),el('p',{class:'help'},'K3-A · 记录编辑与谱系。没有重新计算模型、执行工具、恢复文件或回滚外部系统。'),el('div',{class:'actions'},button('刷新分支列表',loadList),button('显示全部来源',()=>{sessionId='';listOffset=0;return loadList();})),notice,el('div',{class:'experiment-layout'},list,el('main',{},head,el('div',{class:'experiment-columns'},rows,detail),diff)));
  d.onclose=()=>{closed=true;epoch++;listTicket++;d.remove();};document.body.append(d);d.showModal();loadList();if(branchId)selectBranch(branchId);return d;
 }
 return {open};
})();
document.addEventListener('DOMContentLoaded',()=>{const anchor=document.querySelector('#debug-button');if(anchor){const b=button('离线实验',()=>RunDeskExperiments.open());b.id='experiments-open';b.className='quiet';anchor.after(b);}});
