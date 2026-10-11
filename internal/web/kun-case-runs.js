window.RunDeskKunCaseRuns=(()=>{
 function create({taskSet,isActive=()=>true}={}){
  const node=el('section',{}),notice=el('p',{role:'status'}),detail=el('section',{}),history=el('section',{});
  const chosen=el('select',{'aria-label':'执行案例'},...taskSet.cases.map(c=>el('option',{value:c.key},c.title)));chosen.value=taskSet.cases[0]?.key||'';
  const instance=el('select',{'aria-label':'执行实例'}),workspace=el('select',{'aria-label':'执行工作区'});
  let busy=false,ticket=0,view=null;
  const choices=button('读取执行环境',loadChoices),preview=button('生成案例执行预览',makePreview),list=button('读取案例运行记录',()=>loadList(0));
  function controls(){for(const f of [choices,preview,list,chosen,instance,workspace])f.disabled=busy;preview.disabled=busy||!instance.value||!workspace.value;}
  async function act(fn){if(busy||!isActive())return;busy=true;const t=++ticket;controls();try{await fn(()=>isActive()&&ticket===t)}catch(e){if(isActive()&&ticket===t)notice.textContent=e.message}finally{busy=false;if(isActive())controls()}}
  function reset(){ticket++;view=null;detail.replaceChildren();notice.textContent='选择已变化，请重新预览。';controls()}
  for(const f of [chosen,instance,workspace])f.addEventListener('change',reset);
  async function loadChoices(){return act(async alive=>{const [instances,workspaces]=await Promise.all([api('/instances'),api('/workspaces')]);if(!alive())return;const allowed=instances.filter(i=>i.agentRuntime?.kind==='kun'&&(!i.execution?.mode||i.execution.mode==='local'));instance.replaceChildren(...allowed.map(i=>el('option',{value:i.id},i.name)));workspace.replaceChildren(...workspaces.map(w=>el('option',{value:w.id},w.name)));instance.value=allowed[0]?.id||'';workspace.value=workspaces[0]?.id||'';view=null;detail.replaceChildren();notice.textContent=allowed.length?'选择案例与环境后生成预览。':'需要配置本机 Kun 实例。'})}
  function render(v){
   if(v.preview.taskSetId!==taskSet.id||v.preview.taskSetHash!==taskSet.hash)throw Error('案例预览与当前任务集不匹配。');view=v;
   const p=v.preview,confirm=el('input',{type:'checkbox','aria-label':'确认真实案例执行'}),start=button('执行此固定案例',()=>act(async alive=>{if(view!==v||!confirm.checked)return;start.disabled=true;const session=await api('/kun-case-runs/'+encodeURIComponent(p.id)+'/start',{method:'POST',body:{expectedHash:p.hash,confirmExecute:true}});if(!alive())return;if(session.id!==p.sessionId||session.runId!==p.runId)throw Error('运行身份不匹配。');render({...v,session,status:session.status});notice.textContent='已绑定唯一运行；可刷新状态或打开会话。'}));start.disabled=true;confirm.addEventListener('change',()=>{start.disabled=!confirm.checked||busy||v.status==='deleted'||!!v.session?.runId});
   const refresh=button('刷新案例状态',()=>read(p.id));
   const score=button('读取此案例评分',()=>act(async alive=>{const result=await api('/kun-case-runs/'+encodeURIComponent(p.id)+'/score');if(alive())detailsScore.replaceChildren(el('pre',{},JSON.stringify(result,null,2)))}));
   const detailsScore=el('section',{}),open=button('打开运行会话',async()=>{if(!v.session||!isActive()||busy)return;if(typeof refreshSessions==='function')await refreshSessions();if(isActive()&&typeof selectSession==='function')await selectSession(p.sessionId);});open.disabled=!v.session;
   detail.replaceChildren(el('h4',{},p.case.title+' · '+v.status),el('p',{},'固定运行 '+p.runId),el('p',{},p.executionWorkspace?'独立评测目录：'+p.executionWorkspace+'；仅含固定输入文件，不能读取其他项目文件。':'共享项目工作区'),el('details',{open:true},el('summary',{},'有效输入与运行范围'),el('pre',{},p.effectiveInput),el('pre',{},JSON.stringify({planId:p.id,hash:p.hash,instance:p.instanceName,workspace:p.workspace,config:p.config,approvalPolicy:p.approvalPolicy,mcpServers:p.mcpServers,candidateId:p.candidateId},null,2))),el('label',{},confirm,'确认按此输入和配置执行真实模型／工具'),el('div',{class:'actions'},start,refresh,score,open),detailsScore);
   if(window.RunDeskKunSkills&&!p.candidateId)detail.append(button('从此案例创建 Skill 候选',()=>window.RunDeskKunSkills.open({sourcePlanId:p.id})));
  }
  async function makePreview(){return act(async alive=>{const v=await api('/kun-case-runs',{method:'POST',body:{taskSetId:taskSet.id,expectedTaskSetHash:taskSet.hash,caseKey:chosen.value,instanceId:instance.value,workspaceId:workspace.value}});if(alive()){render(v);notice.textContent='预览已固定，尚未执行。'}})}
  async function read(id){return act(async alive=>{const v=await api('/kun-case-runs/'+encodeURIComponent(id));if(alive()){if(v.preview.id!==id)throw Error('预览身份不匹配。');render(v)}})}
  async function loadList(offset){return act(async alive=>{const page=await api('/kun-case-runs?'+new URLSearchParams({taskSetId:taskSet.id,offset,limit:20}));if(!alive())return;const prev=button('较新的案例记录',()=>loadList(Math.max(0,offset-20))),next=button('更早的案例记录',()=>loadList(page.nextOffset));prev.disabled=offset===0;next.disabled=!page.hasMore;history.replaceChildren(...page.items.map(p=>button(p.caseTitle+' · '+p.createdAt,()=>read(p.id))),el('div',{class:'actions'},prev,next))})}
  node.append(el('h3',{},'案例执行'),el('p',{class:'help'},'每份预览固定输入、环境和一个运行身份。重复点击不会创建新运行；修改输入请新建任务集。'),el('div',{class:'actions'},choices,list),el('label',{},'案例',chosen),el('label',{},'实例',instance),el('label',{},'工作区',workspace),preview,notice,detail,history);controls();return {node};
 }
 return {create};
})();
