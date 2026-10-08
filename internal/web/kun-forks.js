// Hybrid starts always use an immutable server preview and a fixed target ID.
window.RunDeskKunForks=(()=>{
 const enc=encodeURIComponent;
 function open({sessionId='',previewId=''}={}){
  document.querySelector('#kun-fork-workbench')?.close();
  let closed=false,ticket=0,pointTicket=0,listTicket=0,busy=false,current=null,points=null,pointOffset=0,listOffset=0;
  const d=el('dialog',{id:'kun-fork-workbench',class:'debug-inspector experiment-workbench'}),notice=el('p',{role:'status'}),list=el('section',{class:'experiment-list'}),source=el('section',{class:'experiment-detail'}),detail=el('section',{class:'experiment-detail'});
  const title=el('input',{'aria-label':'Hybrid 分支标题',maxLength:120,value:'Hybrid 分叉实验'}),instruction=el('textarea',{'aria-label':'分支补充指令',rows:3,maxLength:16000,placeholder:'可选；在下一次模型调用前加入。'}),select=el('select',{'aria-label':'安全分叉边界'});
  const create=button('生成固定预览',createPreview),pointStatus=el('p',{role:'status'}),paging=el('div',{class:'actions'});
  const alive=t=>!closed&&t===ticket;
  const apiGet=p=>api('/kun-forks'+p);
  function invalidate(){ticket++;current=null;detail.replaceChildren();notice.textContent='输入已变化，请重新生成预览。';}
  for(const input of [title,instruction,select])input.addEventListener('input',invalidate);
  function setBusy(value){busy=value;for(const input of [title,instruction,select,create])input.disabled=value;for(const b of paging.querySelectorAll('button'))b.disabled=value||b.dataset.unavailable==='true';}
  async function loadPoints(){
   if(!sessionId)return;const t=++pointTicket,offset=pointOffset;points=null;select.replaceChildren();create.disabled=true;
   pointStatus.textContent='读取来源最近已停止运行的安全边界…';
   try{
    const p=await apiGet('/sources/'+enc(sessionId)+'?'+new URLSearchParams({offset,limit:50}));if(closed||t!==pointTicket)return;
    points=p;for(const v of p.items)select.append(el('option',{value:v.sequence},'#'+v.sequence+' · '+v.phase+' · 模型步骤 '+v.step+' · 待处理工具 '+v.pending));
    const prev=button('较新的边界',()=>{pointOffset=Math.max(0,offset-50);invalidate();return loadPoints();}),next=button('更早的边界',()=>{pointOffset=p.nextOffset;invalidate();return loadPoints();});
    prev.disabled=offset===0;next.disabled=!p.hasMore;prev.dataset.unavailable=String(prev.disabled);next.dataset.unavailable=String(next.disabled);paging.replaceChildren(prev,next);
    create.disabled=busy||!p.items.length;pointStatus.textContent=p.items.length?'来源轮次 '+p.selection.sourceRunId+' · 固定记录 ≤ #'+p.selection.through:'此页没有兼容的安全边界。';
   }catch(e){if(!closed&&t===pointTicket)pointStatus.textContent=e.message;}
  }
  async function loadList(){
   const t=++listTicket,offset=listOffset;
   try{
    const p=await apiGet('?'+new URLSearchParams({offset,limit:20}));if(closed||t!==listTicket)return;
    const prev=button('较新的预览',()=>{listOffset=Math.max(0,offset-20);return loadList();}),next=button('更早的预览',()=>{listOffset=p.nextOffset;return loadList();});prev.disabled=offset===0;next.disabled=!p.hasMore;
    list.replaceChildren(el('h3',{},'已保存的预览'),...p.items.map(v=>button(v.title+' · #'+v.origin.sequence,()=>selectPreview(v.id),'experiment-entry')),el('div',{class:'actions'},prev,next));
    if(!p.items.length)list.append(el('p',{class:'help'},'尚无预览。选择已停止的普通 Kun 会话后，可创建分支。'));
   }catch(e){if(!closed&&t===listTicket)notice.textContent=e.message;}
  }
  async function selectPreview(id){
   if(busy)return;const t=++ticket;current=null;detail.replaceChildren();notice.textContent='读取固定预览…';
   try{const p=await apiGet('/'+enc(id));if(alive(t)){current=p;renderPreview(p);notice.textContent='预览已固定。启动会调用模型；再次打开同一预览会返回同一分支会话。';}}
   catch(e){if(alive(t))notice.textContent=e.message;}
  }
  async function createPreview(){
   if(busy||!points||!select.value)return;if(!title.value.trim()){pointStatus.textContent='请填写分支标题。';return;}
   const t=++ticket,body={sessionId,title:title.value,instruction:instruction.value,selection:{...points.selection,sequence:Number(select.value)}};current=null;detail.replaceChildren();setBusy(true);notice.textContent='捕获固定预览…';
   try{const p=await api('/kun-forks',{method:'POST',body});if(alive(t)){current=p;renderPreview(p);notice.textContent='预览已保存，尚未调用模型。';await loadList();}}
   catch(e){if(alive(t))notice.textContent=e.message;}
   finally{setBusy(false);}
  }
  function renderPreview(p){
   const start=button('启动或打开 Hybrid 分支',async()=>{
    if(busy||current?.id!==p.id)return;const t=ticket;setBusy(true);start.disabled=true;notice.textContent='提交固定分支…';
    try{
     const s=await api('/kun-forks/'+enc(p.id)+'/start',{method:'POST',body:{expectedHash:p.hash}});
     if(alive(t)){await refreshSessions();if(alive(t)){d.close();await selectSession(s.id);}}
     else toast('Hybrid 分支已提交，可从会话列表打开。');
    }catch(e){if(alive(t))notice.textContent=e.message;}
    finally{setBusy(false);start.disabled=false;}
   });start.className='primary';
   detail.replaceChildren(el('h3',{},p.title),el('p',{class:'experiment-mode'},'Hybrid · 模型重新调用 · 工具仅录制回放'),
    kunFacts([['来源会话',p.origin.sessionId],['来源轮次',p.origin.runId],['安全边界','#'+p.origin.sequence+' · '+p.phase],['记录截止','#'+p.origin.through],['待处理工具',p.pending],['可回放结果',p.recordCount],['继承模型调用',p.step+' / '+p.maxSteps],['继承工具预算',p.budget.toolCalls+' / '+p.limits.maxToolCalls],['继承已报告 token',p.budget.reportedTokens],['目标会话',p.targetSessionId]]),
    el('p',{},'新增模型调用会消耗用量。继承所选时点的预算；工具名称、定义、参数、目录和顺序必须匹配录制，未命中立即停止。不会连接真实 MCP、读写项目文件或回滚外部系统。'),
    el('h4',{},'上下文变化'),el('p',{class:'help'},'自动加入 Hybrid 模拟说明。补充指令在下一次模型调用前加入；已有待处理工具先按录制回放。'),el('pre',{},p.instruction||'无补充指令'),
    el('p',{class:'help'},'每份预览只运行一次；中断后请创建新预览。删除目标会话后，此预览不能重新启动。'),
    el('details',{},el('summary',{},'固定来源与指纹'),el('pre',{},JSON.stringify({previewId:p.id,previewHash:p.hash,origin:p.origin},null,2))),start);
  }
  if(sessionId)source.append(el('h3',{},'从当前 Kun 会话创建'),el('p',{class:'help'},'选择最近已停止运行中的安全边界。预览不调用模型或工具；记录编辑实验中的假设值不会导入。'),el('label',{},'安全分叉边界',select),paging,pointStatus,el('label',{},'Hybrid 分支标题',title),el('label',{},'分支补充指令',instruction),create);
  else source.append(el('p',{class:'help'},'请先选择已停止的普通 Kun 会话，再打开此面板创建预览。已有预览可直接打开。'));
  d.append(el('div',{class:'dialog-head'},el('h2',{},'Hybrid 分叉'),button('关闭',()=>d.close(),'quiet')),el('p',{class:'help'},'K3-B · 独立执行分支。先检查固定预览，再明确启动模型调用。'),notice,el('div',{class:'experiment-layout'},list,el('main',{},source,detail)));
  d.onclose=()=>{closed=true;ticket++;pointTicket++;listTicket++;d.remove();};document.body.append(d);d.showModal();loadList();loadPoints();if(previewId)selectPreview(previewId);return d;
 }
 return {open};
})();
document.addEventListener('DOMContentLoaded',()=>{const anchor=document.querySelector('#debug-button');if(anchor){const b=button('Hybrid 分叉',()=>RunDeskKunForks.open({sessionId:state.session?.runtimeKind==='kun'&&!state.session.traceOrigin&&!state.session.kunFork?state.session.id:''}));b.id='kun-forks-open';b.className='quiet';anchor.after(b);}});
