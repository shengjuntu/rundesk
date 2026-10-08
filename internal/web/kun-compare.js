// Fixed host-log comparisons never start a worker, model or tool.
window.RunDeskKunCompare=(()=>{
 const enc=encodeURIComponent,modes={source:'来源后续执行',hybrid:'Hybrid · 工具回放',live:'Live · 真实执行'};
 const known=v=>v===null||v===undefined?'未知':String(v);
 const delta=v=>v===null||v===undefined?'不可计算':(v>0?'+':'')+v;
 function table(headers,rows){return el('div',{class:'kun-comparison-table'},el('table',{},el('thead',{},el('tr',{},...headers.map(v=>el('th',{},v)))),el('tbody',{},...rows.map(row=>el('tr',{},...row.map(v=>el('td',{},v)))))));}
 function render(report,openEvidence){
  const a=report.left,b=report.right,d=report.delta,base=report.baseline;
  const observed=(side,key)=>side.eventCount?known(side[key]):'未记录';
  const result=el('section',{class:'kun-comparison-result'},el('h4',{},'固定范围对照'),
   el('p',{class:'help'},'只统计来源安全点之后或分支启动之后的新增执行。差值为右侧 − 左侧；运行结束不等于业务任务通过。'),
   kunFacts([['来源轮次',base.runId],['共同安全点','#'+base.sequence],['来源记录截止','#'+base.through],['模型',base.model],['继承模型调用',base.step],['继承已报告 token',base.budget.reportedTokens],['继承工具预算',base.budget.toolCalls]]),
   table(['项目','左侧','右侧','差值'],[
    ['执行模式',modes[a.mode],modes[b.mode],'—'],['会话',a.sessionId,b.sessionId,'—'],['轮次',a.runId||'尚未建立',b.runId||'尚未建立','—'],
    ['最后记录状态',a.status,b.status,'—'],['记录覆盖',a.complete?'完整到结束':'未完整',b.complete?'完整到结束':'未完整','—'],
    ['固定宿主上界',a.through,b.through,'—'],['结束时组合',a.harness?.id||'未知',b.harness?.id||'未知','—'],
    ['新增模型调用（已记录）',observed(a,'modelCalls'),observed(b,'modelCalls'),delta(d.modelCalls)],
    ['其中规划调用',observed(a,'planningCalls'),observed(b,'planningCalls'),'—'],
    ['新增已报告 token',observed(a,'reportedTokens')+(a.tokenUsageComplete?'':'（不完整）'),observed(b,'reportedTokens')+(b.tokenUsageComplete?'':'（不完整）'),delta(d.reportedTokens)],
    ['新增活动时间 / ms',known(a.activeMillis),known(b.activeMillis),delta(d.activeMillis)],['新增人工等待 / ms',known(a.waitMillis),known(b.waitMillis),delta(d.waitMillis)]
   ]));
  for(const warning of report.warnings)result.append(el('p',{class:'help'},warning));
  const columns=el('div',{class:'kun-comparison-columns'});
  for(const [name,side]of [['左侧',a],['右侧',b]]){
   const col=el('section',{},el('h4',{},name+' · '+modes[side.mode]));
   for(const warning of side.warnings)col.append(el('p',{class:'help'},warning));
   col.append(el('h4',{},'工具结果（已记录）'),table(['工具','真实派发','回放','失败结果','拒绝','未见结果'],side.tools.map(t=>[t.name,t.dispatched,t.replayed,t.failed,t.declined,t.unsettled])));
   if(!side.tools.length)col.append(el('p',{},'此范围没有工具记录。'));
   col.append(el('h4',{},'最后一条新增回复'));
   if(side.lastReply){const r=side.lastReply;col.append(el('pre',{},r.text),el('p',{class:'help'},`${r.characters} 字符${r.truncated?' · 已截断，完整内容见事件':''} · 文本指纹 ${r.hash}`),button('查看'+name+'回复事件 #'+r.eventId,()=>openEvidence(side,r.eventId)));}
   else col.append(el('p',{},'未记录新增回复；显式规划不计为回复。'));
   columns.append(col);
  }
  result.append(columns);return result;
 }
 function create({preview,isActive=()=>true}){
  let generation=0,busy=false,fixed=null,offset=0,more=true,listBusy=false;
  const choice=el('select',{'aria-label':'对照来源或同源分支'},el('option',{value:''},'来源运行 · 安全点之后'));choice.value='';
  const info=el('p',{role:'status'}),result=el('div',{}),candidates=el('p',{class:'help'}),loaded=new Set();
  const latest=button('读取最新对照',()=>read(false)),repeat=button('重读固定范围',()=>read(true)),loadMore=button('加载同源分支',loadCandidates);
  function update(){latest.disabled=busy;repeat.disabled=busy||!fixed;loadMore.disabled=listBusy||!more;}
  function invalidate(){generation++;busy=false;fixed=null;result.replaceChildren();info.textContent='对照对象已变化，请重新读取。';update();}
  choice.addEventListener('change',invalidate);
  async function read(pinned){
   if(busy||!isActive()||(pinned&&!fixed))return;
   const ticket=++generation,against=choice.value,params=new URLSearchParams();if(against)params.set('against',against);
   if(pinned){params.set('leftThrough',fixed.left.through);params.set('rightThrough',fixed.right.through);}
   busy=true;update();info.textContent='读取保留记录…';
   try{
    const report=await api('/kun-forks/comparisons/'+enc(preview.id)+(params.size?'?'+params:''));
    if(!isActive()||ticket!==generation)return;
    if(report.previewId!==preview.id||report.against!==against||report.right.sessionId!==preview.targetSessionId||report.baseline.sequence!==preview.origin.sequence||report.baseline.runId!==preview.origin.runId||report.baseline.through!==preview.origin.through||(!against&&report.left.sessionId!==preview.origin.sessionId)||(against&&report.left.previewId!==against))throw Error('对照响应身份不匹配，请重新读取。');
    fixed=report;result.replaceChildren(render(report,async(side,eventId)=>{
     if(!isActive()||ticket!==generation)return;
     try{const session=await api('/sessions/'+enc(side.sessionId));if(isActive()&&ticket===generation)window.RunDeskDebug.open(session,{runId:side.runId,through:side.through,eventId});}
     catch(e){if(isActive()&&ticket===generation)info.textContent=e.message;}
    }));info.textContent='已固定两侧宿主记录上界。新事件不会自动改变当前对照。';
   }catch(e){if(isActive()&&ticket===generation)info.textContent=e.message;}
   finally{if(isActive()&&ticket===generation){busy=false;update();}}
  }
  async function loadCandidates(){
   if(listBusy||!more||!isActive())return;listBusy=true;update();
   try{
    const page=await api('/kun-forks?'+new URLSearchParams({offset,limit:50}));if(!isActive())return;
    for(const p of page.items){const a=p.origin,b=preview.origin;if(p.id!==preview.id&&!loaded.has(p.id)&&a.sessionId===b.sessionId&&a.runId===b.runId&&a.sequence===b.sequence&&a.through===b.through){loaded.add(p.id);choice.append(el('option',{value:p.id},p.title+' · '+modes[a.mode]));}}
    offset=page.nextOffset;more=page.hasMore;candidates.textContent='已找到 '+loaded.size+' 个同源预览。'+(more?'可继续加载后续预览。':'已读取全部预览。');
   }catch(e){if(isActive())candidates.textContent=e.message;}
   finally{listBusy=false;if(isActive())update();}
  }
  const node=el('section',{class:'kun-comparison'},el('h3',{},'来源与分支对照'),el('p',{class:'help'},'无需启动分支即可检查已有记录。可对照同一安全点的其他 Hybrid / Live 分支；未运行或记录缺失时明确显示未知。'),el('label',{},'左侧对照对象',choice),el('div',{class:'actions'},loadMore,latest,repeat),candidates,info,result);update();return {node};
 }
 return {create,render};
})();
