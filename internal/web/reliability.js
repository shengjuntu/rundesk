"use strict";

function decisionLabel(value) {
  if (typeof value === "string") return ({accept:rdText("允许本次"), acceptForSession:rdText("会话内允许"), decline:rdText("拒绝本次"), cancel:rdText("取消本次任务")})[value] || value;
  if (value?.acceptWithExecpolicyAmendment) return rdText("允许并保存命令规则");
  if (value?.applyNetworkPolicyAmendment) return rdText("应用网络规则");
  return rdText("提交服务端规则选项");
}

function approvalCard(a, id) {
  const p = a.request.params || {}, method = a.request.method;
  const network = p.networkApprovalContext;
  const title = method === "item/tool/requestUserInput" ? rdText("Codex 需要你的输入") : network ? rdText("请求网络访问") : method === "item/fileChange/requestApproval" ? rdText("请求修改文件") : method === "item/permissions/requestApproval" ? rdText("请求扩展权限") : method === "mcpServer/elicitation/request" ? rdText("MCP 请求确认") : rdText("请求执行命令");
  const card = el("section", {class:"approval"}, el("h3",{},title));
  if (network) card.append(el("p",{},[network.protocol,network.host,network.port].filter(Boolean).join(" · ")));
  else if (p.command) card.append(el("pre",{class:"approval-command"},p.command));
  if (p.cwd) card.append(el("p",{class:"help"},rdText("工作目录：")+p.cwd));
  if (p.reason) card.append(el("p",{},rdText("Codex 提供的说明：")+p.reason));
  if (p.message) card.append(el("p",{},p.message));
  if (p.permissions) card.append(el("pre",{},json(p.permissions)));
  card.append(el("details",{},el("summary",{},rdText("查看完整请求")),el("pre",{},json(p))));
  let answers = () => ({}), content = () => null;
  if (method === "item/tool/requestUserInput") {
    const controls = [];
    for (const q of p.questions || []) {
      const input = el("textarea",{rows:2,placeholder:rdText("输入答案")});
      card.append(el("label",{},q.question));
      if (q.options?.length) {
        const select = el("select",{},el("option",{value:""},rdText("选择或自行填写")),...q.options.map(o=>el("option",{value:o.label},o.label)));
        select.onchange=()=>{input.value=select.value;}; card.append(select);
      }
      card.append(input); controls.push([q.id,input]);
    }
    answers=()=>Object.fromEntries(controls.map(([qid,input])=>[qid,{answers:[input.value]}]));
  }
  if (method === "mcpServer/elicitation/request") {
    if (p.mode === "url") {
      try { const u=new URL(p.url); if (["http:","https:"].includes(u.protocol)) card.append(el("a",{href:u.href,target:"_blank",rel:"noopener noreferrer"},rdText("打开认证页面 ↗"))); } catch {}
    } else {
      const input=el("textarea",{rows:4},"{}");card.append(el("label",{},rdText("按照 requestedSchema 填写 JSON")),el("pre",{},json(p.requestedSchema)),input);content=()=>JSON.parse(input.value);
    }
  }
  const actions=el("div",{class:"approval-actions"});
  let busy=false;
  const submit=async(decision,scope="turn")=>{
    if(busy)return;busy=true;actions.querySelectorAll("button").forEach(b=>b.disabled=true);
    try {
      await api(`/sessions/${id}/approvals/${a.id}`,{method:"POST",body:{decision,scope,answers:answers(),content:decision==="accept"?content():null}});
      await refreshApprovals(id); await refreshSessions(); await refreshRuntime(id);
    } catch(e) { await refreshApprovals(id);throw e; }
    finally { busy=false;actions.querySelectorAll("button").forEach(b=>b.disabled=false); }
  };
  if (method === "item/tool/requestUserInput") actions.append(button(rdText("提交答案"),()=>submit("accept"),"primary"));
  else if (method === "item/permissions/requestApproval") {
    actions.append(button(rdText("拒绝本次"),()=>submit("decline")),button(rdText("本轮允许请求的权限"),()=>submit("accept","turn"),"primary"),button(rdText("本会话允许请求的权限"),()=>submit("accept","session")));
  } else {
    const decisions=a.decisions || (Array.isArray(p.availableDecisions)?p.availableDecisions:["accept","decline","cancel"]);
    for (const decision of decisions) {
      const label=p.mode==="url"&&decision==="accept"?rdText("已完成认证"):decisionLabel(decision);
      const action=button(label,()=>submit(decision),decision==="accept"?"primary":"");
      if(typeof decision === "object" && decision!==null) {
        card.append(el("details",{class:"approval-rule"},el("summary",{},rdText("待提交的规则：")+label),el("p",{class:"help"},rdText("将提交下面的精确规则。命令规则不等于信任整个目录；后续命令仍可能需要审批。")),el("pre",{},json(decision))));
        actions.append(action);
      } else actions.append(action);
    }
    if(!decisions.length) actions.append(el("p",{class:"error"},rdText("服务端没有提供可用决定。可用对话顶部的停止按钮中止任务。")));
  }
  card.append(actions);return card;
}

function permissionEditor(current) {
  const p=current.permissions || {};
  const select=(id,values,value)=>{const node=el("select",{id},...values.map(([v,t])=>el("option",{value:v},t)));node.value=value;return node;};
  const sandbox=select("permission-sandbox",[["workspace-write",rdText("工作区可写")],["read-only",rdText("只读沙箱")],["danger-full-access",rdText("完整访问（关闭沙箱）")]],p.sandbox||"workspace-write");
  const policy=select("permission-policy",[["on-request",rdText("需要时请求审批")],["never",rdText("不请求审批（受限操作可能失败）")]],p.approvalPolicy||"on-request");
  const reviewer=select("permission-reviewer",[["user",rdText("由我审批")],["auto_review",rdText("Codex 自动审查（需模型支持）")]],p.reviewer||"user");
  const network=select("permission-network",[["inherit",rdText("沿用 Codex 配置")],["false",rdText("禁止直接联网")],["true",rdText("允许直接联网")]],p.networkAccess===undefined?"inherit":String(p.networkAccess));
  const note=el("p",{class:"help"});
  const update=()=>{
    network.disabled=sandbox.value!=="workspace-write";
    reviewer.disabled=policy.value==="never";
    note.className=sandbox.value==="danger-full-access"?"error":"help";
    note.textContent=sandbox.value==="danger-full-access"?rdText("完整访问会移除文件和网络沙箱边界；执行仍受系统用户权限与服务端要求约束。"):policy.value==="never"?rdText("不请求审批不会扩大沙箱权限。超出边界的操作可能直接失败。"):rdText("默认在沙箱内执行。自动审查由 Codex 提供，可能增加模型调用，服务端也可能拒绝此配置。");
  };
  sandbox.onchange=policy.onchange=update;update();
  return {
    node:el("fieldset",{class:"permission-fields"},el("legend",{},rdText("运行权限")),el("label",{for:"permission-sandbox"},rdText("沙箱边界")),sandbox,el("label",{for:"permission-policy"},rdText("审批策略")),policy,el("label",{for:"permission-reviewer"},rdText("审批处理方")),reviewer,el("label",{for:"permission-network"},rdText("工作区沙箱网络")),network,note,el("p",{class:"help"},rdText("保存后在该助手或应用各会话的下一轮生效；正在执行或等待审批的任务保持原配置。不会修改你的 config.toml。生效值以 App Server 返回为准。"))),
    value:()=>({sandbox:sandbox.value,approvalPolicy:policy.value,reviewer:reviewer.value,...(sandbox.value==="workspace-write"&&network.value!=="inherit"?{networkAccess:network.value==="true"}:{})})
  };
}

function runtimeSnapshot(r) {
  const box=el("div",{class:"runtime-snapshot"});
  if(!r?.connectionId) {box.append(el("p",{class:"help"},rdText("会话尚未收到运行配置。启动任务后显示 App Server 返回的生效值。")));return box;}
  box.append(el("p",{class:"help"},(r.live?rdText("当前连接"):rdText("上次连接记录"))+" · "+new Date(r.connectedAt).toLocaleString()));
  const effective=r.effective || {}, sandbox=effective.sandbox;
  const pairs=[[rdText("模型"),effective.model],["Provider",effective.modelProvider],[rdText("工作目录"),effective.cwd],[rdText("沙箱"),typeof sandbox==="string"?sandbox:sandbox?.type],[rdText("审批策略"),effective.approvalPolicy],[rdText("审批处理方"),effective.approvalsReviewer],[rdText("网络"),sandbox&&typeof sandbox==="object"&&"networkAccess" in sandbox?(sandbox.networkAccess?rdText("允许直接联网"):rdText("禁止直接联网")):rdText("未返回")],["CODEX_HOME",r.codexHome],[rdText("运行版本"),r.userAgent]];
  const fields=el("dl",{class:"runtime-fields"});
  for(const [label,value] of pairs)fields.append(el("dt",{},label),el("dd",{},value==null?rdText("未返回"):typeof value==="string"?value:json(value)));
  box.append(fields);
  if(r.requested)box.append(el("details",{},el("summary",{},rdText("请求配置与原始生效配置")),el("pre",{},json({requested:r.requested,effective:r.effective,observedAt:r.observedAt}))));
  if(r.notices?.length) {
    box.append(el("h3",{},rdText("此连接的告警与执行结果")));
    for(const n of [...r.notices].reverse()) box.append(el("section",{class:"runtime-notice "+n.level},el("strong",{},n.kind+" · "+({warning:rdText("警告"),error:rdText("错误"),info:rdText("信息")}[n.level]||n.level)+(n.count>1?rdFormat(" · ${0} 次",n.count):"")),el("pre",{},n.message),el("small",{class:"muted"},n.source+" · "+new Date(n.lastSeen).toLocaleString())));
  } else box.append(el("p",{class:"help"},rdText("此连接尚未记录告警；不代表所有工具已验证。")));
  return box;
}

async function refreshRuntime(id=state.session?.id) {
  if(!id)return;
  const r=await api(`/sessions/${id}/runtime`);
  if(state.session?.id!==id)return;
  const changed=json(state.runtime)!==json(r);state.runtime=r;
  renderRuntimeBar();
  if(changed && $("#runtime-dialog")?.open)$("#runtime-detail").replaceChildren(runtimeSnapshot(r));
}

function renderRuntimeBar() {
  const bar=$("#runtime-strip"), r=state.runtime;
  if(!bar)return;
  bar.classList.toggle("hidden",!state.session);
  const notices=r?.notices?.filter(n=>n.level!=="info")||[];
  bar.replaceChildren(el("span",{class:"runtime-model"},r?.effective?.model?rdText("运行模型：")+r.effective.model:rdText("运行配置待获取")),button(notices.length?rdFormat("运行状态 · ${0} 项告警",notices.length):rdText("查看运行配置"),()=>{
    $("#runtime-detail").replaceChildren(runtimeSnapshot(state.runtime));$("#runtime-dialog").showModal();
  },notices.length?"notice-button":""));
}

async function instanceRuntimeCard(iid) {
  const rows=await api(`/instances/${iid}/runtime`);
  const box=el("div",{class:"card"},el("h3",{},rdText("实例运行记录")),el("p",{class:"help"},rdText("显示最近 50 个连接。告警按连接去重，旧连接记录保留时间标记。")));
  for(const r of rows)box.append(el("details",{},el("summary",{},(r.id.startsWith("config-")?rdText("配置连接"):rdText("会话 ")+r.id.slice(0,8))+" · "+(r.live?rdText("在线"):rdText("历史"))+" · "+r.notices.length+rdText(" 项记录")),runtimeSnapshot(r)));
  if(!rows.length)box.append(el("p",{},rdText("还没有连接记录。运行诊断后可查看启动告警。")));
  return box;
}
