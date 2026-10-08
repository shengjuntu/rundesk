// Recovery is an explicit new turn after native-state reconciliation.
(() => {
  let dialog, content, submit, notice, plan, sessionId, runId, opening = 0, sending = false, opener, pending;
  const names = {commandExecution:rdText('命令'),mcpToolCall:rdText('MCP 工具'),fileChange:rdText('文件修改'),dynamicToolCall:rdText('应用工具')};
  const statuses = {completed:rdText('已结束，需核对结果'),failed:rdText('失败'),interrupted:rdText('中断'),inProgress:rdText('尚无结束记录'),unconfirmed:rdText('状态待核对'),idle:rdText('空闲'),notLoaded:rdText('已保存'),systemError:rdText('原生错误'),not_created:rdText('尚未创建线程'),active:rdText('仍在运行')};
  function build() {
    if(dialog)return;
    dialog=el('dialog',{id:'recovery-dialog','aria-labelledby':'recovery-title'});
    const close=button('×',()=>dialog.close(),'quiet');close.setAttribute('aria-label',rdText('关闭任务恢复'));
    content=el('div',{class:'recovery-content',id:'recovery-content'});
    notice=el('p',{class:'recovery-notice',role:'status'});
    submit=button(rdText('继续未完成任务'),resume,'primary');submit.id='recovery-submit';submit.disabled=true;
    dialog.append(el('header',{},el('div',{},el('small',{},rdText('任务恢复')),el('h2',{id:'recovery-title'},rdText('核对后，继续未完成的部分'))),close),content,notice,el('footer',{},button(rdText('重新核对'),check,'quiet'),submit));
    document.body.append(dialog);
    dialog.addEventListener('close',()=>{opening++;opener?.focus();});
  }
  function section(title,...children){return el('section',{class:'recovery-section'},el('h3',{},title),...children);}
  function eligible(){return state.session?.id===sessionId&&state.session?.runId===runId&&!state.session?.archived&&['failed','interrupted'].includes(state.session.status);}
  function update() {
    if(!submit)return;
    const reviewed=!plan?.requiresReview||!!dialog.querySelector('#recovery-reviewed')?.checked;
    const fixed=!plan?.requiresFix||!!dialog.querySelector('#recovery-fixed')?.checked;
    submit.textContent=pending?rdText('重试同一请求'):rdText('继续未完成任务');
    submit.disabled=sending||(pending?state.session?.id!==pending.sessionId:!plan?.canContinue||!eligible()||!reviewed||!fixed);
    dialog.querySelector('footer button').disabled=sending||!!pending;
    for(const field of dialog.querySelectorAll('input,textarea'))field.disabled=sending||!!pending;
    if(dialog.open&&plan&&!eligible()&&!sending&&!pending)notice.textContent=rdText('会话状态已变化，请重新选择当前失败任务。');
  }
  function checkbox(id,text){const box=el('input',{type:'checkbox',id});box.addEventListener('change',update);return el('label',{class:'recovery-check'},box,el('span',{},text));}
  function renderPlan() {
    const p=plan;
    const result=el('div',{class:'recovery-result '+(p.canContinue?'ready':'blocked')},el('strong',{},p.canContinue?rdText('可以准备继续'):rdText('暂不重复执行')),el('p',{},p.reason));
    const goal=section(rdText('原任务'),el('pre',{class:'recovery-goal'},p.taskInput?.text||rdText('未记录')));
    if(p.taskInput?.files?.length)goal.append(el('p',{},rdText('原附件：')+p.taskInput.files.join('、')));
    if(p.taskInput?.skills?.length)goal.append(el('p',{},rdText('原选中技能：')+p.taskInput.skills.map(s=>s.name).join('、')));
    const native=section(rdText('原生执行状态'),el('p',{},`${statuses[p.native.status]||p.native.status||rdText('未确认')} · ${statuses[p.native.turnStatus]||p.native.turnStatus||rdText('无对应轮次')}`));
    if(p.native.reply)native.append(el('pre',{class:'recovery-goal'},p.native.reply));
    if(p.failure){const d=el('details',{},el('summary',{},rdText('查看原错误')),el('pre',{},p.failure));native.append(d);}
    const steps=section(rdText('已有步骤与文件'),el('p',{},rdText('记录只说明观察到的步骤和路径；应用中的实际结果还需要核对。')));
    const list=el('ul',{class:'recovery-evidence'});
    for(const step of p.steps||[]) {const parts=step.name.split(' ');parts[0]=names[parts[0]]||parts[0];list.append(el('li',{},el('span',{},parts.join(' ')),el('span',{},statuses[step.status]||step.status)));}
    for(const file of p.artifacts||[])list.append(el('li',{},button(file,async()=>{
      if(state.session?.id!==sessionId)return;
      const files=await api(`/sessions/${sessionId}/files`),found=files.find(f=>f.path===file);
      if(found)await previewFile(found);else toast(rdText('文件已变化，请重新核对。'));
    },'recovery-file'),el('span',{},rdText('查看文件'))));
    if(!list.children.length)steps.append(el('p',{},rdText('尚未记录工具步骤或会话产物；这不代表外部操作一定没有发生。')));else steps.append(list);
    if(p.truncated)steps.append(el('p',{class:'recovery-warning'},rdText('记录较多，这里只展示部分，请在原任务过程中继续核对。')));
    steps.append(button(rdText('查看原任务过程'),()=>{dialog.close();RunDeskTraceUI.openOrigin({sessionId,runId});},'quiet'));
    const choices=section(rdText('继续前确认'),el('p',{},rdText('将在原会话新增一轮，沿用此会话的模型和当前应用配置。Codex 会先检查已有结果，再继续剩余工作。')));
    if(p.requiresReview)choices.append(checkbox('recovery-reviewed',rdText('我已核对已完成的操作及应用中的结果')));
    if(p.requiresFix)choices.append(checkbox('recovery-fixed',rdText('原错误对应的登录、额度或配置问题已处理')));
    const note=el('textarea',{id:'recovery-note',rows:'3',placeholder:rdText('例如：草稿已保存；发布动作尚未执行。'),maxlength:'4000'});
    choices.append(el('label',{for:'recovery-note'},rdText('补充已完成或待完成的内容（可选）')),note);
    content.replaceChildren(result,goal,native,steps,...(p.canContinue?[choices]:[]));
    notice.textContent=rdFormat("核对时间 ${0} · 15 分钟内有效。不会恢复到原进程的精确执行位置。",new Date(p.checkedAt).toLocaleTimeString());
    update();
  }
  async function check(){
    if(sending||pending)return;
    const generation=++opening;plan=null;submit.disabled=true;
    content.replaceChildren(el('p',{class:'recovery-loading',role:'status'},rdText('正在核对 Codex 线程、已执行步骤和会话产物…')));notice.textContent='';
    if(!eligible()){content.replaceChildren(el('p',{},rdText('当前任务已变化，请关闭窗口并选择失败任务。')));return;}
    try{
      const p=await api(`/sessions/${sessionId}/recovery/check`,{method:'POST',body:{expectedRunId:runId}});
      if(generation!==opening||!dialog.open)return;
      plan=p;renderPlan();
    }catch(e){if(generation===opening&&dialog.open){content.replaceChildren(el('p',{class:'recovery-warning'},e.message));notice.textContent=rdText('核对未完成，未发起任何恢复任务。');}}
  }
  async function resume(){
    if(submit.disabled||sending||!plan)return;
    const target=pending?.sessionId||sessionId, body=pending?.body||{planId:plan.id,expectedRunId:runId,reviewedEffects:!!dialog.querySelector('#recovery-reviewed')?.checked,issueResolved:!!dialog.querySelector('#recovery-fixed')?.checked,note:dialog.querySelector('#recovery-note')?.value||''};
    sending=true;update();notice.textContent=rdText('再次核对原生状态并提交继续请求…');
    try{
      const result=await api(`/sessions/${target}/recover`,{method:'POST',body});
      pending=null;
      if(state.session?.id===target)state.session=result;
      dialog.close();await refreshSessions();renderStatus();toast(rdText('已在原会话继续；原失败轮次仍保留在过程中。'));
    }catch(e){
      pending=await hasPendingSubmission(`/sessions/${target}/recover`,body)?{sessionId:target,body}:null;
      notice.textContent=e.message+(pending?rdText('。可重试同一请求以确认回执，沿用原请求编号。'):rdText('。请重新核对恢复条件。'));
    }
    finally{sending=false;update();render();}
  }
  function open(){
    const s=state.session;if(sending||!s||(!['failed','interrupted'].includes(s.status)&&pending?.sessionId!==s.id))return;
    if(pending&&pending.sessionId!==s.id)pending=null;
    build();sessionId=s.id;runId=s.runId;opener=document.activeElement;
    if(!dialog.open)dialog.showModal();if(pending){update();return;}check();
  }
  function render(){
    const host=document.querySelector('#recovery-banner');if(!host)return;
    const s=state.session, retry=s?.retry&&active(s.status), stopped=s&&!s.archived&&['failed','interrupted'].includes(s.status), origin=s?.recovery;
    const pendingHere=pending?.sessionId===s?.id;
    const sig=JSON.stringify([s?.id,s?.status,s?.runId,retry,origin,s?.archived,pendingHere]);update();if(host._sig===sig)return;host._sig=sig;
    host.classList.toggle('hidden',!retry&&!stopped&&!origin&&!pendingHere);host.replaceChildren();
    if(pendingHere){host.append(el("div",{},el("strong",{},rdText("继续请求的响应尚未确认"))),button(rdText("核对提交结果"),open,"quiet"));}
    else if(retry){host.append(el('span',{class:'recovery-icon','aria-hidden':'true'},'↻'),el('div',{},el('strong',{},rdText('Codex 正在重试')),el('p',{},s.retry.message||rdText('请等待原生执行恢复。'))));}
    else if(stopped){host.append(el('span',{class:'recovery-icon','aria-hidden':'true'},'!'),el('div',{},el('strong',{},rdText('任务已停止，已有过程仍保留')),el('p',{},rdText('先核对已有结果，再继续未完成的部分。'))),button(rdText('核对并继续'),open,'quiet'));}
    else if(origin){host.append(el('div',{},el('strong',{},rdText('此轮继续自上次未完成的任务'))),button(rdText('查看来源轮次'),()=>RunDeskTraceUI.openOrigin({sessionId:s.id,runId:origin.sourceRunId}),'quiet'));}
  }
  window.RunDeskRecovery={open,render};
})();
