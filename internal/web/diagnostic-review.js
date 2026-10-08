// Administrator-driven review. The diagnostic model has no path to this API.
window.RunDeskDiagnosticReview=(()=>{
 const labels={preview:'已预览，尚未发送',unknown:'发送结果未知，请核对或重试同一预览',queued:'已排队，尚未生效',applied:'已写入来源运行上下文',rejected:'已拒绝，未生效'};
 function open(context,proposal){
  const base='/sessions/'+encodeURIComponent(context.sessionId)+'/diagnostic/reviews';
  let closed=false,busy=false,current=null,ticket=0;
  const d=el('dialog',{id:'diagnostic-review',class:'debug-inspector diagnostic-review'}),feedback=el('p',{role:'status'}),preview=el('section',{class:'diagnostic-review-preview'}),history=el('section',{class:'diagnostic-review-history'});
  const text=el('textarea',{rows:5,maxLength:4000,'aria-label':'审核后的补充指令'});text.value=proposal.text;
  function sync(){text.disabled=busy;prepare.disabled=busy||!text.value.trim();apply.disabled=busy||!current||!['preview','unknown'].includes(current.outcome);reload.disabled=busy;apply.textContent=current?.outcome==='unknown'?'重试发送同一预览':'确认发送到来源运行';}
  function show(v){
   current=v;text.value=v.command.text;
   preview.replaceChildren(el('h3',{},'已固定的控制预览'),el('p',{},labels[v.outcome]||v.outcome),el('p',{class:'help'},'来源会话：'+v.proposal.sessionId+'；运行：'+v.command.runId+'；状态版本：'+v.command.expectedStateRevision+'；预览时：'+v.observedStatus+' / '+v.observedPhase),el('pre',{},v.command.text),el('small',{},'请求编号：'+v.command.requestId));
   sync();
  }
  async function loadHistory(){
   const n=++ticket;
   try{
    const rows=await api(base+'?proposalEventId='+context.eventId);if(closed||n!==ticket)return;
    history.replaceChildren(el('h3',{},'审核与执行记录（最近 20 条）'));
    if(!rows.length)history.append(el('p',{class:'help'},'尚未创建审核预览。'));
    for(const row of rows){
     const item=el('details',{},el('summary',{},(labels[row.outcome]||row.outcome)+' · '+row.createdAt),el('pre',{},row.command.text),el('p',{class:'help'},'运行 '+row.command.runId+' · 版本 '+row.command.expectedStateRevision+' · '+row.command.requestId));
     if(row.error)item.append(el('p',{class:'error'},row.error));
     if(row.receiptEventId)item.append(button('执行回执 #'+row.receiptEventId,async()=>{const source=await api('/sessions/'+encodeURIComponent(row.proposal.sessionId));RunDeskDebug.open(source,{runId:row.command.runId,eventId:row.receiptEventId,through:row.receiptEventId});}));
     if(['preview','unknown'].includes(row.outcome))item.append(button('载入此预览',()=>{if(!busy){show(row);feedback.textContent='已载入保存的文本和目标版本。';}}));
     history.append(item);
     if(current?.id===row.id)show(row);
    }
   }catch(e){if(!closed)feedback.textContent=e.message;}
  }
  const prepare=button('预览当前控制目标',async()=>{
   if(busy)return;busy=true;current=null;sync();preview.replaceChildren();feedback.textContent='正在核对当前运行…';
   try{const v=await api(base,{method:'POST',body:{proposalEventId:context.eventId,text:text.value}});if(closed)return;show(v);feedback.textContent='预览已保存，尚未发送。请核对文本与目标后确认。';await loadHistory();}
   catch(e){if(!closed)feedback.textContent=e.message;}
   finally{busy=false;if(!closed)sync();}
  });
  const apply=button('确认发送到来源运行',async()=>{
   if(busy||!current)return;const chosen=current;busy=true;sync();feedback.textContent='正在提交此预览…';
   try{const v=await api(base+'/'+encodeURIComponent(chosen.id)+'/apply',{method:'POST',body:{}});if(closed)return;show(v);feedback.textContent=labels[v.outcome]||v.outcome;}
   catch(e){if(!closed){feedback.textContent=e.message;current={...chosen,outcome:'unknown'};}}
   finally{busy=false;if(!closed){await loadHistory();sync();}}
  });
  const reload=button('刷新执行记录',loadHistory);
  text.oninput=()=>{current=null;preview.replaceChildren();feedback.textContent='文本已修改，请重新预览。';sync();};
  d.append(el('div',{class:'dialog-head'},el('h2',{},'审核补充指令'),button('关闭',()=>d.close(),'quiet')),el('p',{},proposal.reason),el('p',{class:'help'},'建议依据固定历史 ≤ #'+proposal.through+'。发送前会核对来源的当前运行与状态版本；版本变化后需重新预览。'),el('p',{class:'help'},'发送只将指令排队，不会解除暂停或代替工具审批；“已生效”也不代表任务已完成。结果未知时请重试同一预览，避免重复发送。'),el('label',{},'审核后的补充指令',text),el('div',{class:'actions'},prepare,apply,reload),feedback,preview,history);
  d.onclose=()=>{closed=true;ticket++;d.remove();};document.body.append(d);d.showModal();sync();loadHistory();return d;
 }
 return {open};
})();
