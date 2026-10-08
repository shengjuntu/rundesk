async function renderKunSettings(target){
 const i=await api("/instances").then(xs=>xs.find(x=>x.id===instance().id));
 const cfg={kind:"codex",endpoint:"http://127.0.0.1:8000/v1",model:"",apiKeyEnv:"",systemPrompt:"",maxSteps:20,timeoutSeconds:120,allowWrite:false,pauseBeforeModel:false,...i.agentRuntime};
 if(!cfg.kind)cfg.kind="codex";
 cfg.budget={maxToolCalls:64,maxTotalTokens:0,maxActiveSeconds:900,maxConsecutiveFailures:3,...cfg.budget};
 // Persisted Codex/legacy configs can contain numeric zero defaults. Match Config.Normalized.
 for(const [field,fallback]of [["maxSteps",20],["timeoutSeconds",120]])if(cfg[field]===0)cfg[field]=fallback;
 for(const [field,fallback]of [["maxToolCalls",64],["maxActiveSeconds",900],["maxConsecutiveFailures",3]])if(cfg.budget[field]===0)cfg.budget[field]=fallback;
 const field=(label,value,type="text")=>{const input=el("input",{type,value:value??""});return {input,node:el("label",{},label,input)}};
 const kind=el("select",{},el("option",{value:"codex"},"Codex"),el("option",{value:"kun"},"Kun · 独立进程"));kind.value=cfg.kind;
 const harness=el("select",{'aria-label':"Kun 模块组合"},el("option",{value:"tool-loop-v1"},"Tool Loop · 按需调用工具"),el("option",{value:"plan-act-v1"},"Plan-Act · 先规划，再执行"));harness.value=cfg.harness?.loopPolicy||"tool-loop-v1";
 const endpoint=field("OpenAI 兼容 API 基础地址（包含 /v1）",cfg.endpoint);
 const model=field("模型名称",cfg.model);
 const key=field("API Key 环境变量名（留空用于无需认证的本地模型）",cfg.apiKeyEnv);
 const steps=field("最大模型调用次数",cfg.maxSteps,"number");
 const timeout=field("单次模型请求超时（秒）",cfg.timeoutSeconds,"number");
 const toolBudget=field("每轮最多工具调用次数",cfg.budget.maxToolCalls,"number");
 const tokenBudget=field("累计已报告 token 阈值（0 关闭）",cfg.budget.maxTotalTokens,"number");
 const activeBudget=field("每轮活动时间上限（秒，不含人工等待）",cfg.budget.maxActiveSeconds,"number");
 const failureBudget=field("连续工具失败上限",cfg.budget.maxConsecutiveFailures,"number");
 for(const [f,min,max] of [[steps,1,100],[timeout,1,600],[toolBudget,1,3200],[tokenBudget,0,1000000000],[activeBudget,1,86400],[failureBudget,1,100]]){f.input.min=min;f.input.max=max;f.input.step=1;f.input.required=true;}
 const system=el("textarea",{rows:"5"},cfg.systemPrompt||"");
 const write=el("input",{type:"checkbox"});write.checked=!!cfg.allowWrite;
 const pause=el("input",{type:"checkbox"});pause.checked=!!cfg.pauseBeforeModel;
 const debuggerConfig=kunPolicyEditor(cfg.debug);
 const status=el("p",{role:"status",class:"help"});
 const form=el("form",{class:"kun-config"},el("h3",{},"Agent 引擎"),el("p",{class:"help"},"选择后端后，新会话使用新引擎。已有会话保留后端归属。Kun 提供文本模型、项目文件工具、显式 Skills、MCP 和调试控制。"),
 el("label",{},"运行后端",kind),el("label",{},"Kun 模块组合",harness),el("p",{class:"help"},"Plan-Act 每个新轮次先生成一份显式计划，规划禁用工具，额外调用计入模型次数、token 和活动时间预算。保存后下一次新轮次生效；当前运行和检查点续跑保留原组合。其余三个模块使用完整历史、参数校验和固定目录。"),endpoint.node,model.node,key.node,steps.node,timeout.node,toolBudget.node,tokenBudget.node,activeBudget.node,failureBudget.node,el("p",{class:"help"},"token 阈值按服务报告的用量，在下一动作前检查；不能保证当前请求不超额。启用后如服务未报告用量，将停止后续执行。费用暂不估算。"),el("label",{},"系统提示词",system),
 el("label",{class:"kun-check"},write,"允许 Kun 写入项目内文件（仅限制内置文件工具）"),
 el("label",{class:"kun-check"},pause,"每次模型请求前暂停，供调试检查"),
 el("p",{class:"help"},"MCP 在工具 MCP 页面配置；其权限独立于内置文件工具。支持 Hybrid 录制回放与需明确确认的 Live 真实分叉。当前不支持 Shell、图像模型、文件回滚或轨迹编译。"),
 debuggerConfig.node,el("button",{type:"submit",class:"primary"},"保存引擎配置"),status);
 form.onsubmit=async event=>{
  event.preventDefault();status.textContent="保存中…";
  try{const updated=await api("/instances/"+i.id+"/agent-runtime",{method:"PUT",body:{revision:i.revision,config:{harness:{loopPolicy:harness.value},kind:kind.value,endpoint:endpoint.input.value.trim(),model:model.input.value.trim(),apiKeyEnv:key.input.value.trim(),systemPrompt:system.value,maxSteps:Number(steps.input.value),timeoutSeconds:Number(timeout.input.value),allowWrite:write.checked,pauseBeforeModel:pause.checked,debug:debuggerConfig.get(),budget:{maxToolCalls:Number(toolBudget.input.value),maxTotalTokens:Number(tokenBudget.input.value),maxActiveSeconds:Number(activeBudget.input.value),maxConsecutiveFailures:Number(failureBudget.input.value)}}}});
   state.instances=state.instances.map(v=>v.id===updated.id?updated:v);if(!state.session&&kind.value==="kun")$("#model").value=updated.agentRuntime.model;status.textContent="已保存，请新建会话使用。";i.revision=updated.revision;
  }catch(e){status.textContent=e.message;}
 };
 target.replaceChildren(form);
}

window.RunDeskKun={
 async open(){
  if(!state.session||state.session.runtimeKind!=="kun")throw Error("请先选择 Kun 会话");
  const sid=state.session.id;
  let dialog=document.querySelector("#kun-devtools");if(dialog){dialog.close();dialog.remove();}
  dialog=el("dialog",{id:"kun-devtools",class:"kun-devtools"});
  const title=el("h2",{},"Kun Agent DevTools");
  const close=button("关闭",()=>{dialog.close();dialog.remove();});
  const summary=el("p",{class:"help"}),feedback=el("p",{role:"status"}),list=el("div",{class:"kun-call-list"}),detail=el("div",{class:"kun-detail"});
  const tabs=el("div",{class:"tabs"}),controls=el("div",{class:"kun-controls"}),controlHint=el("p",{class:"help kun-control-hint",role:"status"});
  const controlButtons=new Map();let controlBusy=false,currentFloor=0;
  let panel="network",current=null,selected=null,events=[],recoveryCheck=null;const snapshots=new Map();
  const acceptCurrent=value=>{if(value.revision>=currentFloor){current=value;currentFloor=value.revision;}return current;};
  const inspectState=()=>selected?snapshots.get(selected.data.sequence)?.state:null;
  const loadSelected=async()=>{const event=selected;if(event&&!snapshots.has(event.data.sequence)){try{
   snapshots.set(event.data.sequence,await api("/sessions/"+sid+"/kun/snapshots/"+event.data.sequence));
   while(snapshots.size>12){const key=[...snapshots.keys()].find(k=>k!==selected?.data.sequence);if(key===undefined)break;snapshots.delete(key);}
  }catch(e){if(selected===event)feedback.textContent=e.message;}}};
  const selectEvent=async event=>{selected=event;redraw();await loadSelected();redraw();};
  const openEvidence=event=>{panel="network";return selectEvent(event);};
  const kinds={network:"Network · 调用",elements:"Elements · 上下文",sources:"Sources · 控制",performance:"Performance · 用量",application:"Application · MCP",layers:"Layers · 模块",console:"Console · 查询"};
  let detailKey="";
  const redraw=()=>{
   const key=panel+":"+(selected?.data.sequence||"live");
   const same=key===detailKey;
   const opened=new Map(same?[...detail.querySelectorAll("details")].map(d=>[kunDetailKey(d),d.open]):[]);
   const scroll=same?detail.scrollTop:0;
   paint();
   if(same)[...detail.querySelectorAll("details")].forEach(d=>{d.open=!!opened.get(kunDetailKey(d));});
   detail.scrollTop=scroll;detailKey=key;
  };
  const paint=()=>{
   list.replaceChildren();detail.replaceChildren();diffView.update();updateControlButtons();
   dialog.dataset.panel=panel;list.classList.toggle("hidden",!["network","elements"].includes(panel));
   summary.textContent=current?"状态："+current.status+" · "+current.phase+" · 版本 "+current.revision+" · 模型步骤 "+current.step:"历史记录";
   if(current?.fork)summary.textContent+=current.fork.origin.mode==="live"?" · Live · 真实工具执行":" · Hybrid · 工具仅回放";
   if(historyLimited)summary.textContent+=" · 缓存已截断，部分事件可能缺失";
   if(historyPending)summary.textContent+=" · 正在补取历史事件";
   summary.textContent+=(selected?" · 固定快照 #"+selected.data.sequence:" · 跟随现场");
   const inspected=selected?inspectState():current;
   for(const b of tabs.children)b.classList.toggle("selected",b.dataset.panel===panel);
   controls.classList.toggle("hidden",panel!=="sources");
   consoleView.node.classList.toggle("hidden",panel!=="console");consoleView.update();
   if(current?.debug?.pause)summary.textContent+=" · 暂停："+(current.debug.pause.ruleIds?.join(", ")||current.debug.pause.reason)+(current.debug.pause.deadline?" · 到期 "+new Date(current.debug.pause.deadline).toLocaleTimeString():"");
   if(panel==="console")return;
   if(panel==="sources"){detail.append(kunRenderSources(current,selected,events,openEvidence));return;}
   if(panel==="application"){detail.append(kunRenderApplication(inspected,selected,events,openEvidence,()=>{panel="sources";redraw();}));return;}
   if(panel==="layers"){harnessView.update();detail.append(kunRenderLayers(inspected,selected,events,openEvidence),harnessView.node);return;}
   if(panel==="performance"){detail.append(kunRenderPerformance(inspected,selected,events,openEvidence));return;}
   if(panel==="elements"){
    for(const event of events.filter(e=>e.method==="kun/model.started")){
     const b=button("#"+event.data.sequence+" · "+(event.data.data.purpose==="plan"?"规划模型 ":"执行模型 ")+event.data.data.step+" · "+event.data.runId,()=>selectEvent(event));
     b.classList.toggle("selected",selected?.data.sequence===event.data.sequence);list.append(b);
    }
    detail.append(kunRenderContext(selected,selected?snapshots.get(selected.data.sequence):null,current));return;
   }
   const rows=kunCallRows(selected&&!events.some(e=>e.data.sequence===selected.data.sequence)?[selected,...events]:events);
   for(const row of rows){
    const event=row.end||row.start;
    const b=button("#"+event.data.sequence+" · "+kunCallName(row)+" · "+kunCallStatus(row)+" · "+row.runId,()=>selectEvent(event));
    b.classList.toggle("selected",row.events.some(e=>e.data.sequence===selected?.data.sequence));list.append(b);
   }
   const row=selected&&rows.find(r=>r.events.some(e=>e.data.sequence===selected.data.sequence));
   if(row){
    detail.append(kunRenderCall(row,selected.data.sequence));
    if(row.start)detail.append(button("查看请求快照 #"+row.start.data.sequence,()=>selectEvent(row.start)));
    if(row.end)detail.append(button("查看结果快照 #"+row.end.data.sequence,()=>selectEvent(row.end)));
   }else if(selected){detail.append(kunRenderEvent(selected));list.prepend(button("选定事件 #"+selected.data.sequence+" · "+selected.method,()=>selectEvent(selected)));}
   else detail.append(el("p",{},"选择左侧调用，检查请求、结果及证据序号。"));
  };
  for(const [key,label]of Object.entries(kinds)){const b=button(label,async()=>{panel=key;await loadSelected();redraw();});b.dataset.panel=key;tabs.append(b);}
  let refreshPending=null,lastEventId=0,historyLimited=false,historyPending=false,retainedBytes=0;const eventSizes=new WeakMap();
  const refresh=()=>{
   if(refreshPending)return refreshPending;
   refreshPending=(async()=>{
    // Incremental host-event cursor; commit only complete fetches, and serialize
    // manual/timer refreshes so an older response cannot rewind the cursor.
    let after=lastEventId,lastBatchSize=0;const incoming=[];
    for(let page=0;page<2;page++){
     const value=await api("/sessions/"+sid+"/events?after="+after+"&limit=1000");
     const batch=Array.isArray(value)?value:value.events||value.items||[];lastBatchSize=batch.length;
     if(!batch.length)break;
     incoming.push(...batch.filter(e=>e.method.startsWith("kun/")));after=batch.at(-1).id;
     if(batch.length<1000)break;
    }
    historyPending=lastBatchSize===1000;
    for(const event of incoming){const bytes=JSON.stringify(event).length*2;eventSizes.set(event,bytes);retainedBytes+=bytes;events.push(event);}lastEventId=after;
    let trim=0;while(events.length-trim>1&&(events.length-trim>20000||retainedBytes>64*1024*1024)){retainedBytes-=eventSizes.get(events[trim++])||0;historyLimited=true;}
    if(trim)events=events.slice(trim);
    try{acceptCurrent(await api("/sessions/"+sid+"/kun/state"));}catch(e){current=null;feedback.textContent=e.message;}
    redraw();

   })().finally(()=>{refreshPending=null;});
   return refreshPending;
  };
  const updateControlButtons=()=>{
   for(const [op,b]of controlButtons){let reason=controlBusy?"控制提交中…":kunControlReason(current,op);
    if(op==="set_breakpoints"&&!reason){if(!policyBase)reason="请先读取当前断点";else if(policyBase.runId!==current.runId||policyBase.revision!==current.revision)reason="读取后运行状态已变化，请重新读取断点";}
    b.disabled=!!reason;b.title=reason;
   }
   if(policyBase&&current&&(policyBase.runId!==current.runId||policyBase.revision!==current.revision))policyHint.textContent="读取后状态已变化，应用前请重新读取；草稿仍保留。";else policyHint.textContent="规则仅作用于当前运行及其检查点续跑。";
   recoverButton.disabled=controlBusy||!recoveryCheck?.eligible||!current||recoveryCheck.selection.sourceRunId!==current.runId||recoveryCheck.selection.expectedStateRevision!==current.revision;
   controlHint.textContent=controlBusy?"正在提交当前运行控制…":!current?"worker 离线。控制不可用；历史记录仍可检查。":current.approval?(current.approval.decision?"审批决定已提交，等待引擎处理。":"MCP 审批须明确允许或拒绝；继续/单步不可代替审批。"):!["running","paused","pausing"].includes(current.status)?"运行已结束。可检查历史或展开恢复检查。":"操作会校验当前运行和状态版本，过期时需刷新。";
  };
  const sendControl=async(operation,text)=>{
   const reason=controlBusy?"控制提交中…":kunControlReason(current,operation);if(reason){feedback.textContent=reason;return;}
   const command={requestId:crypto.randomUUID(),runId:current.runId,expectedStateRevision:current.revision,operation};
   if(operation==="approve"||operation==="reject")command.callId=current.approval.callId;
   if(operation==="steer")command.text=text;
   controlBusy=true;updateControlButtons();
   try{
    const receipt=await api("/sessions/"+sid+"/kun/control",{method:"POST",body:command});
    feedback.textContent=receipt.status==="queued"?"命令已接收，等待安全点。":"命令已生效。";
    if(operation==="steer")steer.value="";
    try{acceptCurrent(await api("/sessions/"+sid+"/kun/state"));}catch(e){current=null;feedback.textContent+=" "+e.message;}
   }catch(e){feedback.textContent=e.message;}
   finally{controlBusy=false;redraw();}
  };
  for(const [op,label]of [["pause","暂停"],["resume","继续"],["step","单步"],["cancel","停止"],["approve","允许本次工具调用"],["reject","拒绝本次工具调用"]]){
   const b=button(label,()=>sendControl(op));controlButtons.set(op,b);controls.append(b);
  }
  controls.append(controlHint);
  const runtimePolicy=kunPolicyEditor(),policyStatus=el("p",{role:"status"}),policyHint=el("p",{class:"help"});let policyBase=null;
  const policyLoad=button("读取当前断点",async()=>{try{acceptCurrent(await api("/sessions/"+sid+"/kun/state"));runtimePolicy.set(current.debug?.policy);policyBase={runId:current.runId,revision:current.revision};policyStatus.textContent="已读取规则版本 "+(current.debug?.revision||0)+"，状态版本 "+current.revision;redraw();}catch(e){policyStatus.textContent=e.message;}});
  const policyApply=button("应用断点到当前运行",async()=>{
   if(controlBusy||!policyBase||kunControlReason(current,"set_breakpoints"))return;
   try{
    const debug=runtimePolicy.get();controlBusy=true;updateControlButtons();
    const receipt=await api("/sessions/"+sid+"/kun/control",{method:"POST",body:{requestId:crypto.randomUUID(),runId:policyBase.runId,expectedStateRevision:policyBase.revision,operation:"set_breakpoints",debug}});
    policyStatus.textContent="规则已更新："+receipt.status+"。已有暂停需显式继续。";policyBase=null;await refresh();
   }catch(e){policyStatus.textContent=e.message;}finally{controlBusy=false;updateControlButtons();}
  });controlButtons.set("set_breakpoints",policyApply);
  controls.append(el("details",{class:"kun-control-section"},el("summary",{},"编辑当前断点"),runtimePolicy.node,
   el("p",{class:"help"},"先读取当前规则再编辑。清空规则不会解除已有暂停，也不改变模型前固定暂停。重新读取会替换当前草稿。"),policyLoad,policyApply,policyHint,policyStatus));
  const diffView=kunDiffView({sid,getSelected:()=>selected});
  const harnessView=kunHarnessCompare({sid,getSelected:()=>selected});
  const consoleView=kunConsole({sid,getCurrent:()=>current,getSelected:()=>selected,refresh});
  const steer=el("textarea",{rows:"2",placeholder:"给当前运行补充文本指令"});
  const recoveryStatus=el("p",{role:"status",class:"help"});
  const recoveryReasons={live_use_new_fork:"Live 分支需重新创建固定预览",hybrid_use_new_fork:"Hybrid 分支需重新创建固定预览",run_not_stopped:"当前运行尚未结束",run_still_closing:"运行正在关闭，请稍后重试",inflight_action_or_unapplied_control:"模型/工具结果未知，或有未应用指令",no_safe_checkpoint:"没有可用安全检查点（旧版本记录不能恢复）",runtime_manifest_changed:"配置、项目版本、凭据或技能已变化",checkpoint_incompatible:"引擎或模块版本不兼容",checkpoint_consumed:"检查点已使用",no_run:"会话没有运行记录"};
  const recoverButton=button("从检查点继续",async()=>{
   if(controlBusy||!recoveryCheck?.eligible)return;controlBusy=true;updateControlButtons();
   try{const session=await api("/sessions/"+sid+"/kun/resume",{method:"POST",body:recoveryCheck.selection});
    if(state.session?.id===sid)state.session=session;
    state.sessions=state.sessions.map(s=>s.id===sid?session:s);
    recoveryStatus.textContent="恢复已提交：新运行 "+session.runId+"。MCP 将重连并核对工具定义，需要审批的调用会重新询问。";
    recoveryCheck=null;await refresh();
   }catch(e){recoveryStatus.textContent=e.message;}finally{controlBusy=false;updateControlButtons();}
  });recoverButton.disabled=true;
  controls.append(el("details",{class:"kun-control-section"},el("summary",{},"检查点恢复"),el("p",{class:"help"},"中断恢复沿用最近安全检查点和剩余预算，不回滚项目文件。检查只读取状态；恢复提交后才会重连 MCP 并验证工具定义。"),button("检查恢复条件",async()=>{
   recoveryCheck=null;recoverButton.disabled=true;
   try{recoveryCheck=await api("/sessions/"+sid+"/kun/checkpoint");
    try{acceptCurrent(await api("/sessions/"+sid+"/kun/state"));}catch{current=null;}updateControlButtons();
    recoveryStatus.textContent=recoveryCheck.eligible?"可提交恢复：检查点 #"+recoveryCheck.selection.sequence+" · 待执行工具 "+recoveryCheck.pending+" · 已用工具调用 "+recoveryCheck.budget.toolCalls+"。保留既有工具结果，旧的一次性审批不复用。":"不能恢复："+(recoveryReasons[recoveryCheck.reason]||(recoveryCheck.reason.startsWith("budget_")?"执行预算已用尽："+recoveryCheck.reason.slice(7):recoveryCheck.reason));
   }catch(e){recoveryStatus.textContent=e.message;}
  }),recoverButton,recoveryStatus));
  controls.append(button("预览运行时分叉",()=>window.RunDeskKunForks.open({sessionId:sid})));
  const steerButton=button("提交补充指令",()=>{if(!steer.value.trim()){feedback.textContent="请输入补充指令。";return;}return sendControl("steer",steer.value);});
  controlButtons.set("steer",steerButton);controls.append(steer,steerButton);
  dialog.append(el("div",{class:"dialog-head"},title,close),summary,tabs,button("刷新记录",refresh),button("跟随现场",()=>{selected=null;redraw();}),controls,consoleView.node,feedback,diffView.node,el("div",{class:"kun-inspector"},list,detail));
  document.body.append(dialog);dialog.showModal();await refresh();
  let refreshing=false;
  const timer=setInterval(async()=>{if(!dialog.isConnected||!dialog.open){clearInterval(timer);return;}if(refreshing)return;refreshing=true;try{await refresh();}catch(e){feedback.textContent=e.message;}finally{refreshing=false;}},1500);
  dialog.addEventListener("close",()=>clearInterval(timer),{once:true});
 }
};
// Keep the existing conversation layout; add one explicit debugger entry.
document.addEventListener("DOMContentLoaded",()=>{
 const anchor=document.querySelector("#debug-button");
 const b=button("Kun DevTools",()=>window.RunDeskKun.open());b.id="kun-debug-open";b.className="quiet hidden";
 if(anchor)anchor.after(b);else document.querySelector("#messages")?.before(b);
 renderCapabilityStrip();
});

// Kun approvals belong to its persisted loop state, not to Codex server requests.
async function refreshKunApproval(sid){
 let current;
 try{current=await api("/sessions/"+sid+"/kun/state");}catch(e){if(e.code!=="kun_offline")throw e;}
 if(state.session?.id!==sid)return;
 const target=$("#approvals"),approval=current?.approval;
 if(!approval||approval.decision){target.replaceChildren();state.approvalSignature=null;return;}
 const signature=JSON.stringify([current.runId,current.revision,approval.callId]);
 if(signature===state.approvalSignature)return;state.approvalSignature=signature;
 const status=el("p",{role:"status"});
 const decide=async operation=>{
  try{await api("/sessions/"+sid+"/kun/control",{method:"POST",body:{requestId:crypto.randomUUID(),runId:current.runId,expectedStateRevision:current.revision,operation,callId:approval.callId}});await refreshKunApproval(sid);}catch(e){status.textContent=e.message;state.approvalSignature=null;}
 };
 target.replaceChildren(el("section",{class:"card kun-approval"},el("h3",{},"Kun 请求调用 MCP 工具"),el("p",{},approval.server+" / "+approval.tool),el("pre",{},approval.arguments),el("p",{class:"help"},"本次工具尚未执行。要允许后续自动调用，可在工具 MCP 页面将该工具设为“始终允许”，下一轮生效。"),el("div",{class:"actions"},button("允许本次",()=>decide("approve"),"primary"),button("拒绝本次",()=>decide("reject"))),status));
}
