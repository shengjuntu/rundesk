async function renderKunSettings(target){
 const i=await api("/instances").then(xs=>xs.find(x=>x.id===instance().id));
 const cfg={kind:"codex",endpoint:"http://127.0.0.1:8000/v1",model:"",apiKeyEnv:"",systemPrompt:"",maxSteps:20,timeoutSeconds:120,allowWrite:false,pauseBeforeModel:false,...i.agentRuntime};
 if(!cfg.kind)cfg.kind="codex";
 cfg.budget={maxToolCalls:64,maxTotalTokens:0,maxActiveSeconds:900,maxConsecutiveFailures:3,...cfg.budget};
 const field=(label,value,type="text")=>{const input=el("input",{type,value:value??""});return {input,node:el("label",{},label,input)}};
 const kind=el("select",{},el("option",{value:"codex"},"Codex"),el("option",{value:"kun"},"Kun · 独立进程"));kind.value=cfg.kind;
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
 const status=el("p",{role:"status",class:"help"});
 const form=el("form",{class:"kun-config"},el("h3",{},"Agent 引擎"),el("p",{class:"help"},"选择后端后，新会话使用新引擎。已有会话保留后端归属。Kun 提供文本模型、项目文件工具、显式 Skills、MCP 和调试控制。"),
 el("label",{},"运行后端",kind),endpoint.node,model.node,key.node,steps.node,timeout.node,toolBudget.node,tokenBudget.node,activeBudget.node,failureBudget.node,el("p",{class:"help"},"token 阈值按服务报告的用量，在下一动作前检查；不能保证当前请求不超额。启用后如服务未报告用量，将停止后续执行。费用暂不估算。"),el("label",{},"系统提示词",system),
 el("label",{class:"kun-check"},write,"允许 Kun 写入项目内文件（仅限制内置文件工具）"),
 el("label",{class:"kun-check"},pause,"每次模型请求前暂停，供调试检查"),
 el("p",{class:"help"},"MCP 在工具 MCP 页面配置；其权限独立于内置文件工具。当前不支持 Shell、图像模型、检查点重执行、分叉或轨迹编译。"),
 el("button",{type:"submit",class:"primary"},"保存引擎配置"),status);
 form.onsubmit=async event=>{
  event.preventDefault();status.textContent="保存中…";
  try{const updated=await api("/instances/"+i.id+"/agent-runtime",{method:"PUT",body:{revision:i.revision,config:{kind:kind.value,endpoint:endpoint.input.value.trim(),model:model.input.value.trim(),apiKeyEnv:key.input.value.trim(),systemPrompt:system.value,maxSteps:Number(steps.input.value),timeoutSeconds:Number(timeout.input.value),allowWrite:write.checked,pauseBeforeModel:pause.checked,budget:{maxToolCalls:Number(toolBudget.input.value),maxTotalTokens:Number(tokenBudget.input.value),maxActiveSeconds:Number(activeBudget.input.value),maxConsecutiveFailures:Number(failureBudget.input.value)}}}});
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
  const summary=el("p",{class:"help"}),feedback=el("p",{role:"status"}),list=el("div",{class:"kun-call-list"}),detail=el("pre",{class:"kun-detail"});
  const tabs=el("div",{class:"tabs"}),controls=el("div",{class:"kun-controls"});
  let panel="network",current=null,selected=null,events=[];const snapshots=new Map();
  const inspectState=()=>selected?snapshots.get(selected.data.sequence)?.state:null;
  const loadSelected=async()=>{if(selected&&!snapshots.has(selected.data.sequence)){try{snapshots.set(selected.data.sequence,await api("/sessions/"+sid+"/kun/snapshots/"+selected.data.sequence));}catch(e){feedback.textContent=e.message;}}};
  const selectedDetail=event=>panel==="elements"?{runId:event.data.runId,sequence:event.data.sequence,request:event.data.data.request,snapshot:snapshots.get(event.data.sequence)??"点击步骤以读取持久化快照"}:event.data;
  const kinds={network:"Network · 调用",elements:"Elements · 上下文",sources:"Sources · 控制",performance:"Performance · 用量",application:"Application · MCP",layers:"Layers · 模块"};
  const redraw=()=>{
   list.replaceChildren();detail.textContent="";
   summary.textContent=current?"状态："+current.status+" · "+current.phase+" · 版本 "+current.revision+" · 模型步骤 "+current.step:"历史记录";
   summary.textContent+=(selected?" · 固定快照 #"+selected.data.sequence:" · 跟随现场");
   const inspected=selected?inspectState():current;
   for(const b of tabs.children)b.classList.toggle("selected",b.dataset.panel===panel);
   controls.classList.toggle("hidden",panel!=="sources");
   if(panel==="sources"){detail.textContent=current?JSON.stringify({controlTarget:"当前运行（历史快照只读）",selectedSequence:selected?.data.sequence,status:current.status,phase:current.phase,revision:current.revision,queuedControls:current.queuedControls,approval:current.approval,actions:current.actions,budget:current.budget},null,2):"启动任务后可进行控制。";return;}
   if(panel==="application"){detail.textContent=inspected?JSON.stringify({sequence:selected?.data.sequence,servers:inspected.mcp||[],tools:inspected.mcpTools||[],approvalPolicy:inspected.approvalPolicy},null,2):"当前快照不可用；可在 Network 查看保留的 MCP 记录。";return;}
   if(panel==="layers"){detail.textContent=inspected?JSON.stringify({sequence:selected?.data.sequence,harness:inspected.harness,modules:inspected.modules,budget:inspected.budget},null,2):"请选择可用快照或启动 worker。";return;}
   if(panel==="performance"){
    const rows=events.filter(e=>e.method==="kun/model.completed"||e.method==="kun/tool.completed"||e.method==="kun/mcp.response").map(e=>({type:e.method,runId:e.data.runId,...e.data.data}));
    detail.textContent=JSON.stringify({budget:inspected?.budget,selectedSequence:selected?.data.sequence,calls:rows.filter(r=>!selected||r.runId===selected.data.runId).map(r=>({type:r.type,runId:r.runId,step:r.step,durationMs:r.durationMs,usage:r.usage??"unknown",tool:r.call?.function?.name,server:r.server,method:r.method}))},null,2);return;
   }
   const calls=events.filter(e=>panel==="elements"?e.method==="kun/model.started":/^kun\/(model|tool)\.(started|completed)$/.test(e.method)||/^kun\/mcp\.(request|response)$/.test(e.method));
   for(const event of calls){
    const data=event.data.data||{};
    const b=button("#"+event.data.sequence+" · "+(data.call?.function?.name||(data.server?data.server+" / "+data.method:"模型 "+(data.step||"")))+" · "+event.method.split(".").pop(),async()=>{
     selected=event;
     await loadSelected();
     redraw();
    });b.classList.toggle("selected",selected?.id===event.id);list.append(b);
   }
   if(selected)detail.textContent=JSON.stringify(selectedDetail(selected),null,2);
  };
  for(const [key,label]of Object.entries(kinds)){const b=button(label,async()=>{panel=key;await loadSelected();redraw();});b.dataset.panel=key;tabs.append(b);}
  const refresh=async()=>{
   feedback.textContent="";
   let after=0;events=[];for(let page=0;page<20;page++){
    const value=await api("/sessions/"+sid+"/events?after="+after+"&limit=1000");
    const batch=Array.isArray(value)?value:value.events||value.items||[];
    events.push(...batch.filter(e=>e.method.startsWith("kun/")));if(!batch.length||batch.length<1000)break;after=batch.at(-1).id;
   }
   try{current=await api("/sessions/"+sid+"/kun/state");}catch(e){current=null;feedback.textContent=e.message;}
   redraw();
  };
  for(const [op,label]of [["pause","暂停"],["resume","继续"],["step","单步"],["cancel","停止"],["approve","允许本次工具调用"],["reject","拒绝本次工具调用"]]){
   controls.append(button(label,async()=>{
    if(!current)return;try{
     const receipt=await api("/sessions/"+sid+"/kun/control",{method:"POST",body:{requestId:crypto.randomUUID(),runId:current.runId,expectedStateRevision:current.revision,operation:op,callId:current.approval?.callId}});
     feedback.textContent=receipt.status==="queued"?"命令已接收，等待安全点。":"命令已生效。";
     current=await api("/sessions/"+sid+"/kun/state");redraw();
    }catch(e){feedback.textContent=e.message;}
   }));
  }
  const steer=el("textarea",{rows:"2",placeholder:"给当前运行补充文本指令"});
  controls.append(steer,button("提交补充指令",async()=>{
   if(!current)return;try{const v=await api("/sessions/"+sid+"/kun/control",{method:"POST",body:{requestId:crypto.randomUUID(),runId:current.runId,expectedStateRevision:current.revision,operation:"steer",text:steer.value}});feedback.textContent="补充指令："+v.status;steer.value="";}catch(e){feedback.textContent=e.message;}
  }));
  dialog.append(el("div",{class:"dialog-head"},title,close),summary,tabs,button("刷新记录",refresh),button("跟随现场",()=>{selected=null;redraw();}),controls,feedback,el("div",{class:"kun-inspector"},list,detail));
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
