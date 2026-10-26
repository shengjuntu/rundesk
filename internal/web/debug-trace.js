// Conversation reading and evidence inspection share one fixed source cursor.
window.RunDeskDebug=(()=>{
 const labels={running:'进行中',completed:'调用完成',succeeded:'调用完成',recorded:'已记录',pending:'等待审批',accepted:'已允许',declined:'已拒绝',failed:'失败',unknown:'记录不完整',expired:'已失效',warning:'告警',interrupted:'中断',canceled:'取消',cancelled:'取消',paused:'暂停',waiting:'等待'};
 const associations={observed:'记录明确指定',reconstructed:'由关联标识重建',inferred:'按记录顺序推定',missing:'缺少运行标识'};
 const signals={missing_start:'缺少开始记录',missing_end:'缺少结束记录',missing_identity:'缺少调用标识',ambiguous_pair:'多条记录无法唯一配对',run_unassigned:'未能确定所属运行',invalid_clock:'时间记录不完整或逆序',status_unknown:'状态未知'};
 const txt=v=>JSON.stringify(v,null,2);
 let active=null;
 function open(session=state.session,selection={}){
  if(!session?.id){toast('请先选择会话');return;}
  if(active?.sid===session.id){active.select(selection);return active.dialog;}
  active?.close();
  const sid=session.id,base='/sessions/'+encodeURIComponent(sid)+'/debug/';
  let through=null,epoch=0,listTicket=0,detailTicket=0,eventTicket=0,httpTicket=0,httpDetailTicket=0,evidenceTicket=0,offset=0,next=0,hasMore=false,runOffset=0,runNext=0,runMore=false,closed=false;
  let selectedRun=selection.runId||'',query='',kind='steps',type='',creating=false,capturing=false,panel=selection.panel||'trace',kunView=null,kunMountPending=null,kunSwitchTicket=0,httpOffset=0,httpNext=0,httpMore=false,caps=null;
  const d=el('dialog',{class:'debug-inspector debug-workbench',id:'debug-inspector','aria-label':rdText('运行轨迹')}),info=el('p',{class:'help',role:'status'}),error=el('p',{class:'error',role:'alert'}),stats=el('p',{class:'debug-summary'}),list=el('div',{class:'debug-step-list'}),detail=el('section',{class:'debug-step-detail'}),analysisStats=el('p',{class:'debug-summary'});
  d.dataset.sessionId=sid;
  const tabs=el('nav',{class:'debug-workbench-tabs','aria-label':'调试视图'}),tracePanel=el('section',{'data-debug-panel':'trace'}),stepsPanel=el('section',{'data-debug-panel':'steps',class:'hidden'}),httpPanel=el('section',{'data-debug-panel':'http',class:'hidden'}),analysisPanel=el('section',{'data-debug-panel':'analysis',class:'hidden'}),kunPanel=el('section',{'data-debug-panel':'kun',class:'hidden'});
  const diagnosisQuestion=el('textarea',{rows:4,maxLength:4000,'aria-label':'诊断问题',placeholder:'例如：本轮使用了哪些工具？请求参数是什么？请引用来源事件分析失败原因。'}),diagnosisFeedback=el('p',{role:'status'});
  const diagnosisStart=button('创建诊断并发送问题',startDiagnosis);
  const experimentStart=button('创建离线实验基线',async()=>{if(capturing||through===null||!selectedRun)return;const e=epoch;capturing=true;syncDiagnosis();try{const branch=await api('/experiments',{method:'POST',body:{sessionId:sid,runId:selectedRun,through,title:[...(session.title||'Kun')+' · 离线基线'].slice(0,120).join('')}});if(alive(e)){d.close();RunDeskExperiments.open({sessionId:sid,branchId:branch.id});}else toast('离线基线已保存。');}finally{capturing=false;if(!closed)syncDiagnosis();}});
  function syncDiagnosis(){diagnosisStart.disabled=creating||through===null||!selectedRun||!diagnosisQuestion.value.trim();experimentStart.disabled=capturing||through===null||!selectedRun;}
  diagnosisQuestion.oninput=syncDiagnosis;
  const runs=el('select',{'aria-label':'调试轮次'}),types=el('select',{'aria-label':'步骤类型'},...['','commandExecution','mcpToolCall','dynamicToolCall','webSearch','fileChange','agentMessage','reasoning','approval','modelCall','toolCall','toolReplay','mcpExchange'].map(v=>el('option',{value:v},v||'所有类型')));
  const search=el('input',{type:'search',maxLength:1000,placeholder:'搜索已脱敏预览','aria-label':'搜索已脱敏预览'}),issues=el('input',{type:'checkbox'}),capabilities=el('details',{},el('summary',{},'记录来源与后端能力'));
  const pageLabel=el('span',{}),runPage=el('span',{});
  const read=q=>api(base+'query?'+new URLSearchParams(q));
  const alive=e=>!closed&&epoch===e;
  const scope=()=>({through,...(selectedRun?{runId:selectedRun}:{})});
  const args=()=>({...scope(),...(query?{query}:{}),...(type?{type}:{})});
  const reader=RunDeskTraceReader.mount({host:tracePanel,read,scope,session,
   onSelectRun:async runId=>{selectedRun=runId;if(runId&&![...runs.options].some(v=>v.value===runId))runs.append(el('option',{value:runId},runId));runs.value=runId;await apply();},
   onTransport:async eventId=>{const e=epoch;await switchPanel('http');if(alive(e)){const r=await read({kind:'llm_request',...scope(),eventId,section:'metadata'});if(alive(e))await loadHTTPDetail({...r.data,eventId});}},
   ...(session.runtimeKind==='kun'?{onKun:sequence=>switchPanel('kun',{sequence})}:{})});
  const prev=button('上一页',()=>{offset=Math.max(0,offset-20);return loadSteps();}),more=button('下一页',()=>{offset=next;return loadSteps();});
  const runsPrev=button('更早轮次',()=>{runOffset=Math.max(0,runOffset-50);return loadRuns();}),runsNext=button('更多轮次',()=>{runOffset=runNext;return loadRuns();});
  function clearDetail(){detailTicket++;eventTicket++;evidenceTicket++;detail.replaceChildren(el('p',{class:'help'},'选择步骤，查看参数、结果与全部来源事件。'));}
  async function loadRuns(){
   if(through===null)return;
   const e=epoch,requested=runOffset;runsPrev.disabled=runsNext.disabled=true;
   try{
    const r=await read({kind:'runs',through,offset:requested,limit:50});if(!alive(e)||requested!==runOffset)return;
    const rows=r.data.runs;runNext=r.data.nextOffset;runMore=r.data.hasMore;
    runs.replaceChildren(el('option',{value:''},'全部轮次'),...rows.map(row=>el('option',{value:row.id},(row.question||row.id).slice(0,100)+' · '+(labels[row.status]||row.status))));
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
    analysisStats.textContent=stats.textContent;pageLabel.textContent=`${r.data.total?offset+1:0}–${next} / ${r.data.total}`;
    list.replaceChildren(...r.data.steps.map(step=>{
     const b=button('',()=>loadDetail(step.id));b.className='debug-step';b.setAttribute('data-step-id',step.id);
     b.append(el('span',{class:'debug-step-kind'},step.type+' · '+step.runId),el('strong',{},step.title||step.type),el('span',{},labels[step.status]||step.status),el('small',{},step.durationMs==null?'完整耗时未知':`${step.durationMs} ms`));
     if(step.issues?.length)b.append(el('small',{class:'debug-signal'},step.issues.map(v=>signals[v]||labels[v]||v).join(' · ')));
     return b;
    }));
    if(!r.data.steps.length)list.append(el('p',{class:'help'},'当前范围没有匹配步骤；预览搜索不代表全文检索。'));
   }catch(err){if(alive(e)&&ticket===listTicket){error.textContent=err.message;list.replaceChildren();stats.textContent='';analysisStats.textContent='';pageLabel.textContent='';hasMore=false;}}
   finally{if(alive(e)&&ticket===listTicket){list.setAttribute('aria-busy','false');prev.disabled=offset===0;more.disabled=!hasMore;}}
  }
  async function loadDetail(stepId){
   const e=epoch,ticket=++detailTicket;eventTicket++;evidenceTicket++;detail.replaceChildren(el('p',{},'读取步骤…'));
   list.querySelectorAll('.debug-step').forEach(b=>b.classList.toggle('selected',b.dataset.stepId===stepId));
   try{
    const r=await read({kind:'step',stepId,...scope()});if(!alive(e)||ticket!==detailTicket)return;
    const s=r.data,events=el('div',{class:'debug-event-buttons'}),raw=el('section',{class:'debug-event'}),evidence=el('section',{class:'debug-evidence'});
    detail.replaceChildren(el('h3',{},s.title||s.type),el('p',{class:'help'},`${s.runId} · ${s.id} · ${associations[s.association]||s.association}`),el('p',{},'开始：'+(s.startObserved?s.start:'未记录')+'；结束：'+(s.endObserved?s.end:'未记录')),
     el('p',{class:'help'},s.clock+'；'+(s.durationMs==null?'完整区间耗时未知':`${s.durationMs} ms`)+(s.reportedDurationMs==null?'':`；后端另报告 ${s.reportedDurationMs} ms`)),
     el('p',{class:'debug-signal'},(s.issues||[]).map(v=>signals[v]||labels[v]||v).join(' · ')),
     el('p',{class:'help'},s.previewTruncated?'预览已截断。读取完整结构会合并加载开始、结束及其他全部来源事件。':'完整参数与结果保存在来源事件中；预览仅供定位。'),
     el('details',{},el('summary',{},'请求 / 结果预览'),el('pre',{},txt(s.preview))),
     button('读取完整结构（所有来源事件）',()=>loadEvidence(s,e,ticket,evidence)),evidence,el('h4',{},'来源事件'),events,raw);
    if(s.workerSequences?.length){events.append(el('p',{class:'help'},'Kun 序号：'+s.workerSequences.join(', ')+'；下方按钮使用宿主事件 ID。'));events.append(button('在 Kun 高级面板检查',()=>switchPanel('kun',{sequence:s.workerSequences[0]})));}
    if(s.evidenceTruncated)events.append(el('p',{class:'debug-signal'},'来源事件引用列表已截断，不能声称已获取此步骤的全部证据。'));
    for(const id of s.eventIds||[])events.append(button('事件 #'+id,()=>loadEvent(id,0,raw,e,ticket)));
   }catch(err){if(alive(e)&&ticket===detailTicket)detail.replaceChildren(el('p',{class:'error'},err.message));}
  }
  async function loadEvidence(step,e,version,host){
   const token=++evidenceTicket,valid=()=>alive(e)&&version===detailTicket&&token===evidenceTicket;
   host.replaceChildren(el('p',{role:'status'},'逐块读取全部来源事件…'));
   let total=0;const nodes=[];
   try{
    for(const id of step.eventIds||[]){
     let start=0,body='',more=true;
     while(more){
      const r=await read({kind:'event',eventId:id,through,offset:start,limit:16000});if(!valid())return;
      body+=r.data.text;total+=r.data.text.length;more=r.data.hasMore;
      if(more&&r.data.nextOffset<=start)throw Error('事件分页没有前进，请使用来源事件分页读取。');
      start=r.data.nextOffset;
      if(total>32*1024*1024)throw Error('完整结构超过此页面的 32M 字符显示范围；已完成的事件保留，请使用来源事件分页继续读取。');
     }
     // Keep the server JSON text verbatim: JSON.parse/stringify would round large integer arguments.
     nodes.push(el('details',{open:true},el('summary',{},`来源事件 #${id} · ${body.length} 字符`),el('pre',{},body)));
    }
    if(valid())host.replaceChildren(el('p',{role:'status'},step.evidenceTruncated?'已读完列出的来源事件；引用列表不完整。':`已完整读取 ${nodes.length} 个来源事件（已脱敏）。`),...nodes);
   }catch(err){if(valid())host.replaceChildren(el('p',{class:'error'},'结构读取未完成：'+err.message),...nodes);}
  }
  async function loadEvent(id,start,host,e,detailVersion){
   const ticket=++eventTicket;host.replaceChildren(el('p',{},'读取事件…'));
   try{
    const r=await read({kind:'event',eventId:id,through,offset:start,limit:4000});if(!alive(e)||detailVersion!==detailTicket||ticket!==eventTicket)return;
    const chunk=r.data,back=button('上一块',()=>loadEvent(id,Math.max(0,start-4000),host,e,detailVersion)),forward=button('下一块',()=>loadEvent(id,chunk.nextOffset,host,e,detailVersion));back.disabled=start===0;forward.disabled=!chunk.hasMore;
    host.replaceChildren(el('h4',{},`宿主事件 #${id}`),el('p',{class:'help'},`已脱敏 JSON · 字符 ${start}–${chunk.nextOffset} / ${chunk.totalCharacters} · 单块可能不是完整 JSON`),el('pre',{},chunk.text),el('div',{class:'actions'},back,forward));
   }catch(err){if(alive(e)&&detailVersion===detailTicket&&ticket===eventTicket)host.replaceChildren(el('p',{class:'error'},err.message));}
  }
  const captureState=el('p',{class:'debug-capture-state',role:'status'});
  const captureSettings=el('section',{class:'debug-capture-settings','aria-label':'此会话的 HTTP 采集设置'}),captureToggle=el('input',{type:'checkbox',id:'debug-observe-http','aria-label':'为此会话启用 HTTP 采集'}),captureSaved=el('strong'),captureApplied=el('p',{class:'help'}),captureFeedback=el('p',{role:'status'});
  let captureSession=session,captureReady=false,captureBusy=false,captureLoading=false,captureTicket=0,captureDraft=false;
  const captureSupported=session.runtimeKind!=='kun';
  const captureRefresh=button('核对采集状态',()=>readCaptureSettings());
  function captureMessage(value){
   const status=value?.observation;
   captureState.textContent=status?`采集记录状态：${({active:'已接入',configured:'已配置，待核对请求',observed:'已有采集记录',pending:'设置等待下一轮生效',degraded:'采集有缺口',unavailable:'不可用',disabled:'采集关闭',simulated:'协议模拟'}[status.state]||status.state)} · ${status.message||''}${status.maxBodyBytes?' · 每方向正文上限 '+status.maxBodyBytes+' bytes':''}`:'此会话未报告 HTTP 代理接入状态，不能据此认定采集完整。';
  }
  function renderCaptureSettings(){
   const saved=captureSession.observeHTTP===true,status=captureSession.observation,applied=status?.appliedEnabled,pending=status?.pending===true;
   captureToggle.checked=captureBusy?captureDraft:saved;
   captureToggle.disabled=!captureSupported||!captureReady||captureBusy||captureLoading;
   captureRefresh.disabled=captureBusy||captureLoading;
   captureSaved.textContent='已保存设置：'+(saved?'开启':'关闭');
   captureSettings.setAttribute('aria-busy',captureBusy||captureLoading?'true':'false');
   if(!captureSupported){captureApplied.textContent='Kun 不使用 Codex HTTP 采集开关。Kun 的模型与工具记录可在轨迹和 Kun 高级调试中查看。';return;}
   let actual=applied===true?'最近核对的连接：已应用直通代理配置。':applied===false?'最近核对的连接：未配置 RunDesk 采集代理。':'尚未报告实际连接是否已应用采集代理。';
   if(pending){
    if(!saved&&applied===true)actual+=' 关闭设置尚未生效，'+(['starting','running','waiting','stopping'].includes(captureSession.status)?'当前轮':'已有线程')+'仍沿用代理配置；下一轮提交时关闭。';
    else if(saved&&applied===false)actual+=' 开启设置尚未生效，下一轮提交时应用。';
    else actual+=' 设置等待下一轮提交时应用。';
   }else if(saved&&applied===false)actual+=' 开启偏好已保存，但尚未应用代理；请核对下方状态。';
   else if(!saved&&applied===true)actual+=' 关闭偏好已保存，当前连接仍有代理配置；请核对下一轮的生效状态。';
   if(status?.state==='observed')actual+=' 已存在采集记录，各请求是否完整以记录标记为准。';
   else actual+=' 设置或连接配置成功不代表已采集到真实请求。';
   captureApplied.textContent=actual;
  }
  function acceptCaptureSettings(value){
   if(value?.id!==sid||(captureSupported&&typeof value.observeHTTP!=='boolean'))throw Error('会话设置响应与目标不符或缺少采集开关，请重新核对。');
   captureSession=value;captureReady=true;captureMessage(value);
   // Only update these settings: a delayed PATCH must not rewind a live turn/status.
   if(state.session?.id===sid)state.session={...state.session,observeHTTP:value.observeHTTP,observation:value.observation};
   if(Array.isArray(state.sessions))state.sessions=state.sessions.map(item=>item.id===sid?{...item,observeHTTP:value.observeHTTP,observation:value.observation}:item);
  }
  async function readCaptureSettings(){
   if(captureBusy)return;const ticket=++captureTicket;captureLoading=true;renderCaptureSettings();
   try{
    const value=await api('/sessions/'+encodeURIComponent(sid));if(closed||!d.open||active?.dialog!==d||ticket!==captureTicket)return;
    acceptCaptureSettings(value);captureFeedback.textContent='已核对服务端保存的设置与连接状态。';
   }catch(err){if(!closed&&d.open&&active?.dialog===d&&ticket===captureTicket)captureFeedback.textContent='核对失败：'+err.message;}
   finally{if(!closed&&d.open&&active?.dialog===d&&ticket===captureTicket){captureLoading=false;renderCaptureSettings();}}
  }
  async function saveCaptureSettings(){
   if(!captureSupported||!captureReady||captureBusy||captureLoading){renderCaptureSettings();return;}
   captureDraft=captureToggle.checked;const ticket=++captureTicket;captureBusy=true;captureFeedback.textContent='正在保存此会话设置…';renderCaptureSettings();
   try{
    const value=await api('/sessions/'+encodeURIComponent(sid),{method:'PATCH',body:{observeHTTP:captureDraft}});if(closed||!d.open||active?.dialog!==d||ticket!==captureTicket)return;
    acceptCaptureSettings(value);captureFeedback.textContent='设置已保存，下一轮提交时应用；不会中断当前轮。';
   }catch(err){if(!closed&&d.open&&active?.dialog===d&&ticket===captureTicket)captureFeedback.textContent='未确认保存：'+err.message+' 已恢复上次确认的显示，请点击“核对采集状态”确认服务端设置。';}
   finally{if(!closed&&d.open&&active?.dialog===d&&ticket===captureTicket){captureBusy=false;renderCaptureSettings();}}
  }
  captureToggle.onchange=()=>safe(saveCaptureSettings);
  captureSettings.append(el('h3',{},'此会话的 HTTP 采集'),captureSupported?el('label',{class:'debug-capture-toggle'},captureToggle,el('span',{},'启用直通代理采集'),captureSaved):el('strong',{},'Kun 会话不提供此开关'),captureSupported?el('p',{class:'help'},'修改只保存此会话的偏好，下一轮提交时生效；不打断当前请求，也不删除既有采集记录。'):null,captureApplied,el('div',{class:'actions'},captureRefresh),captureFeedback);
  captureMessage(session);renderCaptureSettings();
  const httpList=el('div',{class:'debug-http-list'}),httpDetail=el('section',{class:'debug-http-detail'}),httpStatus=el('p',{class:'help',role:'status'}),httpPage=el('span',{});
  const httpPrev=button('上一批请求',()=>{httpOffset=Math.max(0,httpOffset-20);return loadHTTP();}),httpMoreButton=button('下一批请求',()=>{httpOffset=httpNext;return loadHTTP();});
  const completeness=r=>r.complete&&!r.truncated&&!r.incomplete&&!r.captureLoss?'完整采集':r.truncated?'已截断':r.error?'采集 / 请求异常':'记录不完整';
  async function loadHTTP(){
   if(through===null)return;const e=epoch,ticket=++httpTicket;httpDetailTicket++;httpPrev.disabled=httpMoreButton.disabled=true;httpStatus.textContent='读取 HTTP 观测…';httpDetail.replaceChildren(el('p',{class:'help'},'选择请求，查看原始 Request / Response、头部及流片段。'));
   try{
    const r=await read({kind:'llm_requests',...scope(),offset:httpOffset,limit:20});if(!alive(e)||ticket!==httpTicket)return;
    const rows=r.data.requests||[];httpNext=r.data.nextOffset;httpMore=r.data.hasMore;
    httpStatus.textContent=rows.length?'记录来自内置直通代理；列表仅包含实际经过代理并留存的请求。WebSocket 长连接可能跨轮次，无法确认轮次归属的记录请在“全部轮次”查看。':'当前范围没有 HTTP 采集。历史会话、未接入代理的连接、仍在进行中的请求或未写入的观测不会出现在这里。跨轮次的 WebSocket 连接记录请切换“全部轮次”。';
    httpPage.textContent=`${rows.length?httpOffset+1:0}–${httpNext??httpOffset+rows.length}${r.data.total==null?'':' / '+r.data.total}`;
    httpList.replaceChildren(...rows.map(row=>{
     const b=button('',()=>loadHTTPDetail(row));b.className='debug-step';b.dataset.requestEventId=row.eventId;
     b.append(el('span',{class:'debug-step-kind'},`${row.method||'HTTP'} · ${row.statusCode||'无状态码'} · #${row.eventId}`),el('strong',{},row.url||row.id),el('span',{class:row.complete?'':'debug-signal'},completeness(row)),el('small',{},`${row.durationMs??'?'} ms · ${row.requestBytes??'?'} / ${row.responseBytes??'?'} bytes`));return b;
    }));
   }catch(err){if(alive(e)&&ticket===httpTicket){httpStatus.textContent=err.message;httpList.replaceChildren();httpMore=false;}}
   finally{if(alive(e)&&ticket===httpTicket){httpPrev.disabled=httpOffset===0;httpMoreButton.disabled=!httpMore;}}
  }
  async function loadHTTPDetail(row){
   const e=epoch,ticket=++httpDetailTicket,valid=()=>alive(e)&&ticket===httpDetailTicket;httpDetail.replaceChildren(el('p',{},'读取请求…'));
   httpList.querySelectorAll('.debug-step').forEach(b=>b.classList.toggle('selected',b.dataset.requestEventId===String(row.eventId)));
   try{
    const r=await read({kind:'llm_request',...scope(),eventId:row.eventId,section:'metadata'});if(!valid())return;
    const bodies=el('div'),messageControls=el('div',{class:'debug-ws-messages'}),section=el('select',{'aria-label':'HTTP 采集内容'},...Object.entries({request:'Request body',response:'Response body',request_chunks:'Request 流片段',response_chunks:'Response 流片段',websocket_upstream_messages:'WebSocket 请求消息（可读）',websocket_client_messages:'WebSocket 响应消息（可读）',websocket_client:'WebSocket 上游 → 客户端帧',websocket_upstream:'WebSocket 客户端 → 上游帧',websocket_client_chunks:'WebSocket 上游 → 客户端片段',websocket_upstream_chunks:'WebSocket 客户端 → 上游片段'}).map(([value,label])=>el('option',{value},label)));let bodyTicket=0,messageTicket=0,payloadSelection=null,selectedMessage=null;
    const bodyArgs=()=>payloadSelection?{section:payloadSelection.section,messageIndex:payloadSelection.messageIndex}:{section:section.value};
    async function body(start=0){
     const bTicket=++bodyTicket,choice=bodyArgs(),part=choice.section;bodies.replaceChildren(el('p',{},'读取采集内容…'));
     try{
      const v=await read({kind:'llm_request',...scope(),eventId:row.eventId,...choice,offset:start,limit:part.endsWith('_chunks')?50:16000});if(!valid()||bTicket!==bodyTicket)return;
      const data=v.data,chunked=part.endsWith('_chunks'),step=chunked?50:16000,compressed=data.compressed||selectedMessage?.compressed;
      const back=button('上一段',()=>body(Math.max(0,start-step))),next=button('下一段',()=>body(data.nextOffset));back.disabled=start===0;next.disabled=!data.hasMore;
      bodies.replaceChildren(el('p',{class:'help'},chunked?'按观测到的顺序记录的读取片段；不代表 HTTP 分块或 SSE 事件边界。':`${part.endsWith('_payload')?'解析后的 WebSocket 消息负载':'已保存的正文副本'} · 字节 ${start}–${data.nextOffset} / ${data.totalBytes} · ${data.complete&&!data.truncated?'正文采集完整':'正文未完整采集'}${data.redacted?' · 已脱敏':''}。分页边界可能位于 JSON 或 UTF-8 字符中间。`),el('pre',{},chunked?txt(data):compressed?'压缩消息尚未解压，以下为去掩码后的压缩字节 Base64：\n'+(data.bodyBase64||''):(data.text||data.totalBytes===0?data.text:'二进制负载 Base64：\n'+(data.bodyBase64||''))),el('div',{class:'actions'},back,next));
      if(!chunked&&data.bodyBase64)bodies.append(el('details',{},el('summary',{},part.endsWith('_payload')?'消息负载 Base64（精确分页）':'原始字节 Base64（精确分页）'),el('pre',{},data.bodyBase64)));
     }catch(err){if(valid()&&bTicket===bodyTicket)bodies.replaceChildren(el('p',{class:'error'},err.message));}
    }
    async function wholeBody(){
     const token=++bodyTicket,choice=bodyArgs(),part=choice.section;if(part.endsWith('_chunks')||part.endsWith('_messages'))return;
     bodies.replaceChildren(el('p',{role:'status'},'读取并按字节顺序拼接已保存正文…'));let start=0,total=0,parts=[],last;
     try{
      do{
       const v=await read({kind:'llm_request',...scope(),eventId:row.eventId,...choice,offset:start,limit:16000});if(!valid()||token!==bodyTicket)return;
       last=v.data;if(last.totalBytes>32*1024*1024)throw Error('正文超过页面的 32 MiB 完整显示范围，请分页查看。');
       const binary=atob(last.bodyBase64||''),bytes=Uint8Array.from(binary,c=>c.charCodeAt(0));parts.push(bytes);total+=bytes.length;
       if(last.hasMore&&last.nextOffset<=start)throw Error('正文分页没有前进。');start=last.nextOffset;
      }while(last.hasMore);
      const bytes=new Uint8Array(total);let cursor=0;for(const part of parts){bytes.set(part,cursor);cursor+=part.length;}
      let display='';if(last.compressed||selectedMessage?.compressed)display='压缩消息尚未解压；可下载去掩码后的压缩负载字节。';else{try{display=new TextDecoder('utf-8',{fatal:true}).decode(bytes);}catch{display='内容是非 UTF-8 二进制字节，请使用原始字节下载。';}}
      const save=button(part.endsWith('_payload')?'下载消息 Payload 字节':'下载已保存原始字节',()=>{const url=URL.createObjectURL(new Blob([bytes],{type:'application/octet-stream'})),a=el('a',{href:url,download:`rundesk-http-${row.eventId}-${part}${choice.messageIndex==null?'':'-'+choice.messageIndex}.bin`});a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);});
      bodies.replaceChildren(el('p',{role:'status'},`已读取全部 ${total} 个保存字节；${last.complete&&!last.truncated&&!last.incomplete?'正文采集完整':'来源采集不完整，以上仅是已保存部分'}。`),el('pre',{},display),save);
     }catch(err){if(valid()&&token===bodyTicket)bodies.replaceChildren(el('p',{class:'error'},'完整正文读取未完成：'+err.message));}
    }
    async function loadMessages(start=0){
     const token=++messageTicket,part=section.value;bodyTicket++;payloadSelection=null;selectedMessage=null;completeBody.disabled=true;messageControls.replaceChildren(el('p',{},'解析已保存的 WebSocket 帧…'));bodies.replaceChildren();
     try{
      const v=await read({kind:'llm_request',...scope(),eventId:row.eventId,section:part,offset:start,limit:50});if(!valid()||token!==messageTicket)return;
      const data=v.data,decode=data.decode||{},items=data.messages||[],choose=el('select',{'aria-label':'WebSocket 消息'},...items.map(m=>el('option',{value:m.index},`消息 #${m.index} · ${m.type||'opcode '+m.opcode} · ${m.payloadBytes??'?'} bytes · ${m.compressed?'压缩未解码':m.complete?'消息完整':'消息不完整'}`)));
      const messageMeta=el('pre');
      const back=button('上一批消息',()=>loadMessages(Math.max(0,start-50))),next=button('下一批消息',()=>loadMessages(data.nextOffset));back.disabled=start===0;next.disabled=!data.hasMore;
      const selectMessage=async()=>{const m=items.find(item=>String(item.index)===choose.value);if(!m)return;messageMeta.textContent=txt(m);selectedMessage=m;payloadSelection={section:part.replace('_messages','_payload'),messageIndex:m.index};if(m.payloadAvailable===false){completeBody.disabled=true;bodies.replaceChildren(el('p',{class:'debug-signal'},m.error||'未保留此消息负载，请查看原始帧和采集完整性。'));return;}completeBody.disabled=false;completeBody.textContent='读取完整消息负载';await body();};
      choose.onchange=selectMessage;
      messageControls.replaceChildren(el('p',{class:'help'},'只读解析：去除客户端帧掩码并合并消息分片；原始帧仍可单独查看。连接可能跨轮次，消息完整不代表整个模型响应已完整采集。'),el('p',{class:'debug-signal'},[decode.error||data.error,decode.compressionUnsupported?'存在未支持的压缩消息':'',data.incomplete||data.truncated||decode.truncated?'来源帧采集或解析存在缺口':''].filter(Boolean).join(' · ')),el('label',{},'选择消息',choose),el('div',{class:'actions'},back,next),el('details',{},el('summary',{},'所选消息与原始帧位置'),messageMeta));
      if(items.length)await selectMessage();else bodies.replaceChildren(el('p',{class:'help'},'当前范围没有可解析的 WebSocket 消息；请检查原始帧和完整性标记。'));
     }catch(err){if(valid()&&token===messageTicket)messageControls.replaceChildren(el('p',{class:'error'},err.message));}
    }
    const completeBody=button('读取完整已保存正文',wholeBody);
    for(const option of section.options)if(option.value.startsWith('websocket_')&&!r.data.websocket)option.disabled=true;
    section.onchange=()=>{messageTicket++;payloadSelection=null;selectedMessage=null;messageControls.replaceChildren();completeBody.textContent='读取完整已保存正文';completeBody.disabled=section.value.endsWith('_chunks')||section.value.endsWith('_messages');return section.value.endsWith('_messages')?loadMessages():body();};
    httpDetail.replaceChildren(el('h3',{},`${row.method||'HTTP'} ${row.statusCode||''}`),el('p',{},row.url||row.id),el('p',{class:row.complete&&!row.truncated&&!row.incomplete&&!row.captureLoss?'':'debug-signal'},completeness(row)),el('details',{},el('summary',{},'请求与响应元数据 / Headers'),el('pre',{},txt(r.data))),el('label',{},'正文与流片段',section),messageControls,completeBody,bodies);
    if(r.data.websocket){section.value='websocket_upstream_messages';await loadMessages();}else await body();
   }catch(err){if(valid())httpDetail.replaceChildren(el('p',{class:'error'},err.message));}
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
  async function switchPanel(value,extra={}){
   if(!['trace','steps','http','analysis','kun'].includes(value)||value==='kun'&&session.runtimeKind!=='kun')value='trace';panel=value;
   for(const node of [tracePanel,stepsPanel,httpPanel,analysisPanel,kunPanel])node.classList.toggle('hidden',node.dataset.debugPanel!==panel);
   for(const b of tabs.children){b.classList.toggle('selected',b.dataset.panel===panel);b.setAttribute('aria-current',b.dataset.panel===panel?'page':'false');}
   d.dataset.panel=panel;const ticket=++kunSwitchTicket;
   if(panel==='trace')await reader.setScope(scope());
   if(panel==='steps'||panel==='analysis')await loadSteps();
   if(panel==='http')await loadHTTP();
   if(panel==='kun'&&through!==null){
    if(!kunView){if(!kunMountPending)kunMountPending=RunDeskKun.mount({host:kunPanel,session,scope:scope(),selection:extra});kunView=await kunMountPending;if(closed){kunView?.destroy();kunView=null;return;}}
    if(ticket===kunSwitchTicket)await kunView.setScope({...scope(),...extra});
   }
  }
  async function refresh(keepOrigin=false){
   const e=++epoch;through=null;offset=0;runOffset=0;httpOffset=0;listTicket++;httpTicket++;httpDetailTicket++;reader.setScope(scope());clearDetail();error.textContent='';info.textContent='固定当前历史范围…';list.replaceChildren();stats.textContent='';latest.disabled=true;syncDiagnosis();
   try{
    const values=await Promise.all([api(base+'capabilities'),read({kind:'events',limit:1,...(keepOrigin&&selection.through!=null?{through:selection.through}:{})}),readCaptureSettings()]);if(!alive(e))return;
    caps=values[0];through=values[1].data.through;d.dataset.through=through;
    info.textContent=`${session.title||sid} · ${caps.backend} · 固定宿主历史 ≤ #${through}，新增记录需手动刷新。`;
    capabilities.replaceChildren(el('summary',{},'记录来源与后端能力'),el('p',{class:'help'},'轨迹与摘要共用后端结构化查询。未配对记录明确标记；HTTP 正文只有经过代理才可观测。历史缺失不会被推测成完整记录。'),el('pre',{},txt(caps.queries)));
    await loadRuns();
    if(!alive(e))return;
    if(kunView)await kunView.setScope(scope());await switchPanel(panel);
    if(keepOrigin&&selection.eventId&&alive(e)){await switchPanel('trace');await reader.inspectEvent(selection.eventId);}
   }catch(err){if(alive(e))error.textContent=err.message;}
   finally{if(alive(e)){latest.disabled=false;syncDiagnosis();}}
  }
  async function apply(){httpTicket++;httpDetailTicket++;selectedRun=runs.value;query=search.value.trim();type=types.value;kind=issues.checked?'issues':'steps';offset=0;httpOffset=0;d.dataset.runId=selectedRun;syncDiagnosis();if(kunView)await kunView.setScope(scope());await reader.setScope(scope());if(panel==='steps'||panel==='analysis')await loadSteps();if(panel==='http')await loadHTTP();}
  const latest=button(rdText('读取最新记录'),()=>refresh(false)),filter=button('查询',apply);
  runs.onchange=apply;types.onchange=apply;issues.onchange=apply;search.onkeydown=e=>{if(e.key==='Enter'){e.preventDefault();apply();}};
  for(const [key,label]of Object.entries({trace:'会话轨迹',steps:'步骤检查',http:'LLM 请求 / 响应',analysis:'摘要与问答',...(session.runtimeKind==='kun'?{kun:'Kun 高级调试'}:{})})){const b=button(rdText(label),()=>switchPanel(key));b.dataset.panel=key;tabs.append(b);}
  stepsPanel.append(el('div',{class:'debug-filters'},el('label',{},'类型',types),el('label',{},'预览搜索',search),el('label',{class:'debug-check'},issues,'仅需关注'),filter),stats,el('div',{class:'debug-columns'},el('section',{},list,el('div',{class:'debug-pages'},prev,pageLabel,more)),detail));
  httpPanel.append(captureSettings,captureState,el('p',{class:'help'},'直通观测保留原协议。Request 与 Response 来自旁路副本，凭据头会脱敏，正文保留实际字节；未采集、截断或丢失会明确标记。'),httpStatus,el('div',{class:'debug-columns'},el('section',{},httpList,el('div',{class:'debug-pages'},httpPrev,httpPage,httpMoreButton)),httpDetail));
  analysisPanel.append(el('h3',{},'当前范围摘要'),analysisStats,el('p',{class:'help'},'以上为结构化记录统计，沿用轨迹中的步骤筛选。模型分析会通过只读查询工具按需读取选定轮次的固定历史，并引用来源事件。'),el('section',{class:'debug-diagnosis'},el('h3',{},'询问这段轨迹'),el('p',{class:'help'},'请先选择一个轮次。创建独立诊断会话后使用原会话配置的模型；用量记入诊断会话。诊断结论和建议需要审核。'),diagnosisQuestion,diagnosisStart,diagnosisFeedback));
  d.append(el('div',{class:'dialog-head'},el('h2',{},rdText('运行轨迹')),el('div',{class:'debug-heading-actions'},latest,button(rdText('返回对话'),()=>d.close(),'quiet'))),info,error,
   el('div',{class:'debug-scope'},el('label',{},'轮次',runs)),el('div',{class:'debug-pages debug-run-pages'},runsPrev,runPage,runsNext),tabs,tracePanel,stepsPanel,httpPanel,analysisPanel,kunPanel,capabilities);
  d.onclose=()=>{closed=true;epoch++;captureTicket++;reader.destroy();kunView?.destroy();if(active?.dialog===d)active=null;d.remove();if(location.hash==='#trace')history.replaceState(null,'',location.pathname+location.search+'#session/'+encodeURIComponent(sid));if(typeof scheduleRender==='function')scheduleRender();};
  active={sid,dialog:d,close:()=>d.close(),select:async value=>{if(value.runId!==undefined){selectedRun=value.runId;if(selectedRun&&![...runs.options].some(v=>v.value===selectedRun))runs.append(el('option',{value:selectedRun},'已选轮次 · '+selectedRun));runs.value=selectedRun;await apply();}if(value.through!=null&&value.through!==through){selection=value;await refresh(true);}await switchPanel(value.eventId?'trace':value.panel||panel,value);if(value.eventId&&through!==null)await reader.inspectEvent(value.eventId);}};
  document.body.append(d);d.showModal();syncDiagnosis();switchPanel(panel);refresh(true);return d;
 }
 function validSuggestion(p,origin){return !!origin&&p?.status==='suggestion_only'&&p.sessionId===origin.sessionId&&p.runId===origin.runId&&p.through===origin.through&&['inspect','steer','configuration'].includes(p.kind)&&typeof p.text==='string'&&typeof p.reason==='string'&&Array.isArray(p.evidenceIds)&&p.evidenceIds.length>0&&p.evidenceIds.length<=16&&p.evidenceIds.every(id=>Number.isSafeInteger(id)&&id>0&&id<=origin.through);}
 function renderSuggestion(node,p,origin,context={}){
  if(!validSuggestion(p,origin))return;
  node.replaceChildren(el('h3',{},'待审核建议 · '+({inspect:'补充检查',steer:'补充指令',configuration:'配置调整'}[p.kind])),el('p',{},p.reason),el('pre',{},p.text),el('p',{class:'help'},'尚未执行。事件引用已校验归属，模型判断仍需审核；应用前请重新核对当前状态。'),el('div',{class:'debug-event-buttons'},...p.evidenceIds.map(id=>button('来源事件 #'+id,async()=>{const source=await api('/sessions/'+encodeURIComponent(p.sessionId));open(source,{runId:p.runId,through:p.through,eventId:id});}))),el('small',{},p.id));
  node.querySelector('.help').textContent='这是历史建议。事件引用已校验归属，模型判断仍需审核；最新处理情况见审核与执行记录。';
  if(p.kind==='steer'&&context.sessionId&&Number.isSafeInteger(context.eventId))node.append(button('审核与执行记录',()=>RunDeskDiagnosticReview.open(context,p)));
  else node.append(el('p',{class:'help'},'检查和配置建议请人工处理，未自动执行。'));
 }
 return {open,close:()=>active?.close(),isOpen:()=>!!active,sessionId:()=>active?.sid,validSuggestion,renderSuggestion};
})();
