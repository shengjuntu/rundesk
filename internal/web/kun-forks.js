// Fork starts always use an immutable server preview and a fixed target ID.
window.RunDeskKunForks=(()=>{
 const enc=encodeURIComponent;
 function open({sessionId='',previewId=''}={}){
  document.querySelector('#kun-fork-workbench')?.close();
  let closed=false,ticket=0,pointTicket=0,listTicket=0,busy=false,current=null,points=null,pointOffset=0,listOffset=0;
  const d=el('dialog',{id:'kun-fork-workbench',class:'debug-inspector experiment-workbench'}),notice=el('p',{role:'status'}),list=el('section',{class:'experiment-list'}),source=el('section',{class:'experiment-detail'}),detail=el('section',{class:'experiment-detail'});
  const mode=el('select',{'aria-label':'分叉执行模式'},el('option',{value:'hybrid'},'Hybrid · 工具录制回放'),el('option',{value:'live'},'Live · 真实工具执行'));mode.value='hybrid';
  const title=el('input',{'aria-label':'分支标题',maxLength:120,value:'分叉实验'}),instruction=el('textarea',{'aria-label':'分支补充指令',rows:3,maxLength:16000,placeholder:'可选；在下一次模型调用前加入。'}),select=el('select',{'aria-label':'安全分叉边界'});
  const create=button('生成固定预览',createPreview),pointStatus=el('p',{role:'status'}),paging=el('div',{class:'actions'});
  const alive=t=>!closed&&t===ticket;
  const apiGet=p=>api('/kun-forks'+p);
  function invalidate(){ticket++;current=null;detail.replaceChildren();notice.textContent='输入已变化，请重新生成预览。';}
  for(const input of [title,instruction,select,mode])input.addEventListener('input',invalidate);
  function setBusy(value){busy=value;for(const input of [title,instruction,select,mode,create])input.disabled=value;for(const b of paging.querySelectorAll('button'))b.disabled=value||b.dataset.unavailable==='true';}
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
    list.replaceChildren(el('h3',{},'已保存的预览'),...p.items.map(v=>button(v.title+' · #'+v.origin.sequence+' · '+(v.origin.mode==='live'?'Live':'Hybrid'),()=>selectPreview(v.id),'experiment-entry')),el('div',{class:'actions'},prev,next));
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
   const t=++ticket,body={...(mode.value==='live'?{mode:'live'}:{}),sessionId,title:title.value,instruction:instruction.value,selection:{...points.selection,sequence:Number(select.value)}};current=null;detail.replaceChildren();setBusy(true);notice.textContent='捕获固定预览…';
   try{const p=await api('/kun-forks',{method:'POST',body});if(alive(t)){current=p;renderPreview(p);notice.textContent='预览已保存，尚未调用模型。';await loadList();}}
   catch(e){if(alive(t))notice.textContent=e.message;}
   finally{setBusy(false);}
  }
  function renderPreview(p){
   const live=p.origin.mode==='live';
   if(!['hybrid','live'].includes(p.origin.mode)||(live&&!p.live)){detail.replaceChildren(el('p',{},'不支持的分叉预览，请更新两端并重新创建。'));return;}
   const confirm=el('input',{type:'checkbox','aria-label':'确认 Live 真实执行'});confirm.checked=false;
   const start=button(live?'确认启动或打开 Live 分支':'启动或打开 Hybrid 分支',async()=>{
    if(busy||current?.id!==p.id)return;
    if(live&&!confirm.checked){notice.textContent='请先核对工作区及待处理动作，勾选真实执行确认。';return;}
    const t=ticket;setBusy(true);start.disabled=true;confirm.disabled=true;notice.textContent='提交固定分支…';
    try{
     const s=await api('/kun-forks/'+enc(p.id)+'/start',{method:'POST',body:{expectedHash:p.hash,...(live?{confirmLive:true}:{})}});
     if(alive(t)){await refreshSessions();if(alive(t)){d.close();await selectSession(s.id);}}
     else toast('分支已提交，可从会话列表打开。');
    }catch(e){if(alive(t))notice.textContent=e.message;}
    finally{setBusy(false);confirm.disabled=false;start.disabled=live&&!confirm.checked;}
   });start.className='primary';start.disabled=live;
   confirm.addEventListener('change',()=>{start.disabled=busy||!confirm.checked;});
   detail.replaceChildren(el('h3',{},p.title),el('p',{class:'experiment-mode'},live?'Live · 模型和工具真实执行':'Hybrid · 模型重新调用 · 工具仅录制回放'),
    kunFacts([['来源会话',p.origin.sessionId],['来源轮次',p.origin.runId],['安全边界','#'+p.origin.sequence+' · '+p.phase],['记录截止','#'+p.origin.through],['待处理工具',p.pending],['可回放结果',live?'不使用录制结果':p.recordCount],['继承模型调用',p.step+' / '+p.maxSteps],['继承工具预算',p.budget.toolCalls+' / '+p.limits.maxToolCalls],['继承已报告 token',p.budget.reportedTokens],['目标会话',p.targetSessionId]]),
    el('p',{},live?'Live 将访问项目当前文件和真实 MCP，可能产生费用、覆盖文件或重复来源运行已经执行的外部动作。只有会话和执行记录独立；文件系统与外部服务不回滚、不复制。': '新增模型调用会消耗用量。继承所选时点的预算；工具名称、定义、参数、目录和顺序必须匹配录制，未命中立即停止。不会连接真实 MCP、读写项目文件或回滚外部系统。'),
    el('h4',{},'上下文变化'),el('p',{class:'help'},live?'自动标记历史观测与当前 Live 执行。已有待处理工具保持原参数，先核对当前目录和审批再执行；补充指令在下一次模型调用前加入，不会改写这些待处理动作。':'自动加入 Hybrid 模拟说明。补充指令在下一次模型调用前加入；已有待处理工具先按录制回放。'),el('pre',{},p.instruction||'无补充指令'));
   if(live){
    detail.append(el('h4',{},'真实执行范围'),kunFacts([['当前项目工作区',p.live.workspace],['内置文件写入',p.live.allowWrite?'允许':'禁止'],['当前 MCP 工具数',p.live.mcpTools],['审批策略',p.live.approvalPolicy]]),el('p',{class:'help'},'MCP 权限独立于内置文件写入。重新读取当前配置及凭据，重连并核对工具目录；需逐次审批的工具重新询问。参数仅作有界预览，常见密钥字段脱敏，截断内容应在来源快照核对。'));
    for(const call of p.live.pendingTools||[])detail.append(el('details',{},el('summary',{},call.name+' · '+call.callId),el('pre',{},call.argumentsPreview+(call.truncated?'\n…已截断，请核对来源快照':''))));
    detail.append(el('label',{class:'kun-check'},confirm,'我确认以上工作区、权限和待处理动作，允许真实工具执行及可能重复的副作用'));
   }
   detail.append(el('p',{class:'help'},'每份预览只运行一次；中断后请创建新预览。删除目标会话后，此预览不能重新启动。'),el('details',{},el('summary',{},'固定来源与指纹'),el('pre',{},JSON.stringify({previewId:p.id,previewHash:p.hash,origin:p.origin},null,2))),start);
  }
  if(sessionId)source.append(el('h3',{},'从当前 Kun 会话创建'),el('p',{class:'help'},'选择最近已停止运行中的安全边界。预览不调用模型或工具；记录编辑实验中的假设值不会导入。'),el('label',{},'执行模式',mode),el('label',{},'安全分叉边界',select),paging,pointStatus,el('label',{},'分支标题',title),el('label',{},'分支补充指令',instruction),create);
  else source.append(el('p',{class:'help'},'请先选择已停止的普通 Kun 会话，再打开此面板创建预览。已有预览可直接打开。'));
  d.append(el('div',{class:'dialog-head'},el('h2',{},'Kun 运行时分叉'),button('关闭',()=>d.close(),'quiet')),el('p',{class:'help'},'选择 Hybrid 录制回放或 Live 真实执行。先核对固定预览，再启动独立会话。'),notice,el('div',{class:'experiment-layout'},list,el('main',{},source,detail)));
  d.onclose=()=>{closed=true;ticket++;pointTicket++;listTicket++;d.remove();};document.body.append(d);d.showModal();loadList();loadPoints();if(previewId)selectPreview(previewId);return d;
 }
 return {open};
})();
document.addEventListener('DOMContentLoaded',()=>{const anchor=document.querySelector('#debug-button');if(anchor){const b=button('Kun 分叉',()=>RunDeskKunForks.open({sessionId:state.session?.runtimeKind==='kun'&&!state.session.traceOrigin&&!state.session.kunFork?state.session.id:''}));b.id='kun-forks-open';b.className='quiet';anchor.after(b);}});
