async function renderKunSettings(target){
 target.replaceChildren(el("p",{class:"help"},rdText("日常会话和应用统一使用 Codex。Kun 维护配置请从左侧环境设置进入。")));
}

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
 target.replaceChildren(el("section",{class:"card kun-approval"},el("h3",{},approval.kind==="builtin"?"Kun 请求执行 Bash 命令":"Kun 请求调用 MCP 工具"),el("p",{},approval.server+" / "+approval.tool),el("pre",{},approval.arguments),el("p",{class:"help"},approval.kind==="builtin"?"本次命令尚未执行。批准后使用宿主权限；可在 Agent 引擎配置中设置 Bash 的审批方式，下一轮生效。":"本次工具尚未执行。要允许后续自动调用，可在工具 MCP 页面将该工具设为“始终允许”，下一轮生效。"),el("div",{class:"actions"},button("允许本次",()=>decide("approve"),"primary"),button("拒绝本次",()=>decide("reject"))),status));
}
