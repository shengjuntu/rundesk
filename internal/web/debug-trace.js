// Shared read-only lifecycle inspection. All queries use one fixed host cursor.
window.RunDeskDebug=(()=>{
 const labels={running:'进行中',completed:'调用完成',succeeded:'调用完成',recorded:'已记录',pending:'等待审批',accepted:'已允许',declined:'已拒绝',failed:'失败',unknown:'记录不完整',expired:'已失效',warning:'告警',interrupted:'中断',canceled:'取消',cancelled:'取消',paused:'暂停',waiting:'等待'};
 const associations={observed:'记录明确指定',reconstructed:'由关联标识重建',inferred:'按记录顺序推定',missing:'缺少运行标识'};
 const signals={missing_start:'缺少开始记录',missing_end:'缺少结束记录',missing_identity:'缺少调用标识',ambiguous_pair:'多条记录无法唯一配对',run_unassigned:'未能确定所属运行',invalid_clock:'时间记录不完整或逆序',status_unknown:'状态未知'};
 const txt=v=>JSON.stringify(v,null,2),safe=v=>String(v??'');
 function open(session=state.session,selection={}){
  if(!session?.id){toast('请先选择会话');return;}
  const sid=session.id,base='/sessions/'+encodeURIComponent(sid)+'/debug/';
  let through=null,epoch=0,listTicket=0,detailTicket=0,eventTicket=0,offset=0,next=0,hasMore=false,runOffset=0,runNext=0,runMore=false,closed=false;
  let selectedRun=selection.runId||'',query='',kind='steps',type='',creating=false,capturing=false;
  const diagnosisQuestion=el('textarea',{rows:3,maxLength:4000,'aria-label':'诊断问题',placeholder:'例如：这次调用为何失败？请引用事件并给出待审核建议。'}),diagnosisFeedback=el('p',{role:'status'});
  const diagnosisStart=button('创建诊断并发送问题',startDiagnosis);
  const experimentStart=button('创建离线实验基线',async()=>{if(capturing||through===null||!selectedRun)return;const e=epoch;capturing=true;syncDiagnosis();try{const branch=await api('/experiments',{method:'POST',body:{sessionId:sid,runId:selectedRun,through,title:[...(session.title||'Kun')+' · 离线基线'].slice(0,120).join('')}});if(alive(e))RunDeskExperiments.open({sessionId:sid,branchId:branch.id});else toast('离线基线已保存。');}finally{capturing=false;if(!closed)syncDiagnosis();}});
  function syncDiagnosis(){diagnosisStart.disabled=creating||through===null||!selectedRun||!diagnosisQuestion.value.trim();experimentStart.disabled=capturing||through===null||!selectedRun;}
  diagnosisQuestion.oninput=syncDiagnosis;
  const d=el('dialog',{class:'debug-inspector',id:'debug-inspector'}),info=el('p',{class:'help',role:'status'}),error=el('p',{class:'error',role:'alert'}),stats=el('p',{class:'debug-summary'}),list=el('div',{class:'debug-step-list'}),detail=el('section',{class:'debug-step-detail'});
  const runs=el('select',{'aria-label':'调试轮次'}),types=el('select',{'aria-label':'步骤类型'},...['','commandExecution','mcpToolCall','dynamicToolCall','webSearch','fileChange','agentMessage','reasoning','approval','modelCall','toolCall','toolReplay','mcpExchange'].map(v=>el('option',{value:v},v||'所有类型')));
  const search=el('input',{type:'search',maxLength:1000,placeholder:'搜索已脱敏预览','aria-label':'搜索已脱敏预览'}),issues=el('input',{type:'checkbox'}),capabilities=el('details',{},el('summary',{},'后端能力与来源'));
  const pageLabel=el('span',{}),runPage=el('span',{});
  const read=(q)=>api(base+'query?'+new URLSearchParams(q));
  const alive=e=>!closed&&epoch===e;
  const args=()=>({through, ...(selectedRun?{runId:selectedRun}:{}),...(query?{query}:{}),...(type?{type}: {})});
  const prev=button('上一页',()=>{offset=Math.max(0,offset-20);return loadSteps();}),more=button('下一页',()=>{offset=next;return loadSteps();});
  const runsPrev=button('更早轮次',()=>{runOffset=Math.max(0,runOffset-50);return loadRuns();}),runsNext=button('更多轮次',()=>{runOffset=runNext;return loadRuns();});
  function clearDetail(){detailTicket++;eventTicket++;detail.replaceChildren(el('p',{class:'help'},'选择步骤，查看配对依据和原始事件。'));}
  async function loadRuns(){
   if(through===null)return;
   const e=epoch,requested=runOffset;runsPrev.disabled=runsNext.disabled=true;
   try{
    const r=await read({kind:'runs',through,offset:requested,limit:50});if(!alive(e)||requested!==runOffset)return;
    const rows=r.data.runs;runNext=r.data.nextOffset;runMore=r.data.hasMore;
    runs.replaceChildren(el('option',{value:''},'全部轮次'),...rows.map(row=>el('option',{value:row.id},(row.question||row.id).slice(0,80)+' · '+(labels[row.status]||row.status))));
    if(selectedRun&&!rows.some(row=>row.id===selectedRun))runs.append(el('option',{value:selectedRun},'已选轮次 · '+selectedRun));runs.value=selectedRun;
    runPage.textContent=`轮次 ${r.data.total?requested+1:0}–${runNext} / ${r.data.total}`;
   }catch(err){if(alive(e))error.textContent=err.message;}
   finally{if(alive(e)&&requested===runOffset){runsPrev.disabled=runOffset===0;runsNext.disabled=!runMore;}}
  }
  async function loadSteps(){
   if(through===null)return;const e=epoch,ticket=++listTicket;clearDetail();error.textContent='';prev.disabled=more.disabled=true;
   const selection={...args()};list.setAttribute('aria-busy','true');
   try{
    const [r,summary]=await Promise.all([read({kind,...selection,offset,limit:20}),read({kind:'statistics',...selection})]);
    if(!alive(e)||ticket!==listTicket)return;
    next=r.data.nextOffset;hasMore=r.data.hasMore;
    stats.textContent=`筛选范围：${summary.data.steps} 个步骤 · ${summary.data.attentionSteps} 个需关注 · ${summary.data.withoutCompleteDuration} 个缺少完整耗时。步骤完成不代表任务目标已达成。`;
    pageLabel.textContent=`${r.data.total?offset+1:0}–${next} / ${r.data.total}`;
    list.replaceChildren(...r.data.steps.map(step=>{
     const b=button('',()=>loadDetail(step.id));b.className='debug-step';b.setAttribute('data-step-id',step.id);
     b.append(el('span',{class:'debug-step-kind'},step.type),el('strong',{},step.title||step.type),el('span',{},labels[step.status]||step.status),el('small',{},step.durationMs==null?'完整耗时未知':`${step.durationMs} ms`));
     if(step.issues?.length)b.append(el('small',{class:'debug-signal'},step.issues.map(v=>signals[v]||labels[v]||v).join(' · ')));
     return b;
    }));
    if(!r.data.steps.length)list.append(el('p',{class:'help'},'当前范围没有匹配步骤；预览搜索不代表全文检索。'));
   }catch(err){if(alive(e)&&ticket===listTicket){error.textContent=err.message;list.replaceChildren();stats.textContent='';pageLabel.textContent='';hasMore=false;}}
   finally{if(alive(e)&&ticket===listTicket){list.setAttribute('aria-busy','false');prev.disabled=offset===0;more.disabled=!hasMore;}}
  }
  async function loadDetail(stepId){
   const e=epoch,ticket=++detailTicket;eventTicket++;detail.replaceChildren(el('p',{},'读取步骤…'));
   try{
    const r=await read({kind:'step',through,stepId,...(selectedRun?{runId:selectedRun}:{})});if(!alive(e)||ticket!==detailTicket)return;
    const s=r.data,events=el('div',{class:'debug-event-buttons'}),raw=el('section',{class:'debug-event'});
    detail.replaceChildren(el('h3',{},s.title||s.type),el('p',{class:'help'},`${s.runId} · ${s.id} · ${associations[s.association]||s.association}`),el('p',{},'开始：'+(s.startObserved?s.start:'未记录')+'；结束：'+(s.endObserved?s.end:'未记录')),
     el('p',{class:'help'},s.clock+'；'+(s.durationMs==null?'完整区间耗时未知':`${s.durationMs} ms`)+(s.reportedDurationMs==null?'':`；后端另报告 ${s.reportedDurationMs} ms`)),
     el('p',{class:'debug-signal'},s.issues.map(v=>signals[v]||labels[v]||v).join(' · ')),
     el('p',{class:'help'},s.previewTruncated?'预览已截断。以下事件可按块读取。':'以下是已脱敏预览，完整内容以原始事件为准。'),
     el('details',{},el('summary',{},'请求 / 结果预览'),el('pre',{},txt(s.preview))),el('h4',{},'来源事件'),events,raw);
    if(s.workerSequences?.length)events.append(el('p',{class:'help'},'Kun 序号：'+s.workerSequences.join(', ')+'；下方按钮使用宿主事件 ID。'));
    if(s.evidenceTruncated)events.append(el('p',{class:'help'},'事件引用列表已截断。'));
    for(const id of s.eventIds)events.append(button('事件 #'+id,()=>loadEvent(id,0,raw,e,ticket)));
   }catch(err){if(alive(e)&&ticket===detailTicket)detail.replaceChildren(el('p',{class:'error'},err.message));}
  }
  async function loadEvent(id,start,host,e,detailVersion){
   const ticket=++eventTicket;host.replaceChildren(el('p',{},'读取事件…'));
   try{
    const r=await read({kind:'event',eventId:id,through,offset:start,limit:4000});if(!alive(e)||detailVersion!==detailTicket||ticket!==eventTicket)return;
    const chunk=r.data,back=button('上一块',()=>loadEvent(id,Math.max(0,start-4000),host,e,detailVersion)),forward=button('下一块',()=>loadEvent(id,chunk.nextOffset,host,e,detailVersion));back.disabled=start===0;forward.disabled=!chunk.hasMore;
    host.replaceChildren(el('h4',{},`宿主事件 #${id}`),el('p',{class:'help'},`已脱敏 JSON · 字符 ${start}–${chunk.nextOffset} / ${chunk.totalCharacters} · 单块可能不是完整 JSON`),el('pre',{},chunk.text),el('div',{class:'actions'},back,forward));
   }catch(err){if(alive(e)&&detailVersion===detailTicket&&ticket===eventTicket)host.replaceChildren(el('p',{class:'error'},err.message));}
  }
  async function startDiagnosis(){
   if(creating||through===null||!selectedRun||!diagnosisQuestion.value.trim())return;
   const e=epoch,question=diagnosisQuestion.value.trim(),frozen={sessionId:sid,runId:selectedRun,through};
   creating=true;syncDiagnosis();diagnosisFeedback.textContent='正在建立独立诊断会话…';let created;
   try{
    created=await api('/sessions',{method:'POST',body:{traceAnalysis:frozen}});
    await api('/sessions/'+encodeURIComponent(created.id)+'/turns',{method:'POST',body:{text:question}});
    await refreshSessions();
    if(alive(e)){d.close();await selectSession(created.id);}else toast('诊断已开始，可从会话列表打开。');
   }catch(err){
    if(alive(e)){diagnosisFeedback.replaceChildren(document.createTextNode(err.message));if(created)diagnosisFeedback.append(button('打开已创建的诊断会话',async()=>{d.close();await selectSession(created.id);$('#prompt').value=question;}));}
    else toast('诊断提交未完成：'+err.message);
   }finally{creating=false;if(!closed)syncDiagnosis();}
  }
  async function refresh(keepOrigin=false){
   const e=++epoch;through=null;offset=0;runOffset=0;listTicket++;clearDetail();error.textContent='';info.textContent='固定当前历史范围…';list.replaceChildren();stats.textContent='';latest.disabled=true;syncDiagnosis();
   try{
    const [caps,page]=await Promise.all([api(base+'capabilities'),read({kind:'events',limit:1,...(keepOrigin&&selection.through!=null?{through:selection.through}:{})})]);if(!alive(e))return;
    through=page.data.through;
    info.textContent=`${session.title||sid} · ${caps.backend} · 固定宿主历史 ≤ #${through}，新增记录需手动刷新。`;
    capabilities.replaceChildren(el('summary',{},'后端能力与来源'),el('p',{class:'help'},'步骤由已保留的生命周期记录重建，不是完整模型上下文；未配对记录明确标记，时间来自记录时间。'),el('pre',{},txt(caps.queries)));
    await Promise.all([loadRuns(),loadSteps()]);
    if(keepOrigin&&selection.eventId&&alive(e)){detail.replaceChildren();await loadEvent(selection.eventId,0,detail,e,detailTicket);}
   }catch(err){if(alive(e))error.textContent=err.message;}
   finally{if(alive(e)){latest.disabled=false;syncDiagnosis();}}
  }
  function apply(){selectedRun=runs.value;query=search.value.trim();type=types.value;kind=issues.checked?'issues':'steps';offset=0;syncDiagnosis();return loadSteps();}
  const latest=button('读取最新记录',()=>refresh(false)),filter=button('查询',apply);
  runs.onchange=apply;types.onchange=apply;issues.onchange=apply;search.onkeydown=e=>{if(e.key==='Enter'){e.preventDefault();apply();}};
  d.append(el('div',{class:'dialog-head'},el('h2',{},'只读调试'),button('关闭',()=>d.close(),'quiet')),info,error,capabilities,
   el('div',{class:'debug-filters'},el('label',{},'轮次',runs),el('label',{},'类型',types),el('label',{},'预览搜索',search),el('label',{class:'debug-check'},issues,'仅需关注'),filter,latest),
   el('div',{class:'debug-pages'},runsPrev,runPage,runsNext),el('details',{class:'debug-diagnosis'},el('summary',{},'用独立会话诊断'),el('p',{class:'help'},'请先选择一个轮次。会调用该会话配置的模型，发送固定范围内的证据，用量记在新的诊断会话。结论和建议需审核。'),diagnosisQuestion,diagnosisStart,diagnosisFeedback),stats,el('div',{class:'debug-columns'},el('section',{},list,el('div',{class:'debug-pages'},prev,pageLabel,more)),detail));
  if(session.runtimeKind==='kun')d.append(el('section',{class:'debug-diagnosis'},el('h3',{},'离线记录实验'),el('p',{class:'help'},'先选择轮次。复制固定范围的已脱敏 Kun 记录，不调用模型或工具；每次编辑生成子分支，后续结果标为未验证。'),experimentStart,button('查看已有离线分支',()=>RunDeskExperiments.open({sessionId:sid}))));
  d.onclose=()=>{closed=true;epoch++;d.remove();};document.body.append(d);d.showModal();syncDiagnosis();refresh(true);return d;
 }
 function validSuggestion(p,origin){return !!origin&&p?.status==='suggestion_only'&&p.sessionId===origin.sessionId&&p.runId===origin.runId&&p.through===origin.through&&['inspect','steer','configuration'].includes(p.kind)&&typeof p.text==='string'&&typeof p.reason==='string'&&Array.isArray(p.evidenceIds)&&p.evidenceIds.length>0&&p.evidenceIds.length<=16&&p.evidenceIds.every(id=>Number.isSafeInteger(id)&&id>0&&id<=origin.through);}
 function renderSuggestion(node,p,origin,context={}){
  if(!validSuggestion(p,origin))return;
  node.replaceChildren(el('h3',{},'待审核建议 · '+({inspect:'补充检查',steer:'补充指令',configuration:'配置调整'}[p.kind])),el('p',{},p.reason),el('pre',{},p.text),el('p',{class:'help'},'尚未执行。事件引用已校验归属，模型判断仍需审核；应用前请重新核对当前状态。'),el('div',{class:'debug-event-buttons'},...p.evidenceIds.map(id=>button('来源事件 #'+id,async()=>{const source=await api('/sessions/'+encodeURIComponent(p.sessionId));open(source,{runId:p.runId,through:p.through,eventId:id});}))),el('small',{},p.id));
  node.querySelector('.help').textContent='这是历史建议。事件引用已校验归属，模型判断仍需审核；最新处理情况见审核与执行记录。';
  if(p.kind==='steer'&&context.sessionId&&Number.isSafeInteger(context.eventId))node.append(button('审核与执行记录',()=>RunDeskDiagnosticReview.open(context,p)));
  else node.append(el('p',{class:'help'},'检查和配置建议请人工处理，未自动执行。'));
 }
 return {open,validSuggestion,renderSuggestion};
})();
document.addEventListener('DOMContentLoaded',()=>{
 const anchor=document.querySelector('#debug-button');if(anchor){const b=button('调试检查',()=>window.RunDeskDebug.open());b.id='debug-inspect-open';b.className='quiet';anchor.after(b);}
});
