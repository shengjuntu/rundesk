"use strict";

function decisionLabel(value) {
  if (typeof value === "string") return ({accept:"允许本次", acceptForSession:"会话内允许", decline:"拒绝本次", cancel:"取消本次任务"})[value] || value;
  if (value?.acceptWithExecpolicyAmendment) return "允许并保存命令规则";
  if (value?.applyNetworkPolicyAmendment) return "应用网络规则";
  return "提交服务端规则选项";
}

function approvalCard(a, id) {
  const p = a.request.params || {}, method = a.request.method;
  const network = p.networkApprovalContext;
  const title = method === "item/tool/requestUserInput" ? "Codex 需要你的输入" : network ? "请求网络访问" : method === "item/fileChange/requestApproval" ? "请求修改文件" : method === "item/permissions/requestApproval" ? "请求扩展权限" : method === "mcpServer/elicitation/request" ? "MCP 请求确认" : "请求执行命令";
  const card = el("section", {class:"approval"}, el("h3",{},title));
  if (network) card.append(el("p",{},[network.protocol,network.host,network.port].filter(Boolean).join(" · ")));
  else if (p.command) card.append(el("pre",{class:"approval-command"},p.command));
  if (p.cwd) card.append(el("p",{class:"help"},"工作目录："+p.cwd));
  if (p.reason) card.append(el("p",{},"Codex 提供的说明："+p.reason));
  if (p.message) card.append(el("p",{},p.message));
  if (p.permissions) card.append(el("pre",{},json(p.permissions)));
  card.append(el("details",{},el("summary",{},"查看完整请求"),el("pre",{},json(p))));
  let answers = () => ({}), content = () => null;
  if (method === "item/tool/requestUserInput") {
    const controls = [];
    for (const q of p.questions || []) {
      const input = el("textarea",{rows:2,placeholder:"输入答案"});
      card.append(el("label",{},q.question));
      if (q.options?.length) {
        const select = el("select",{},el("option",{value:""},"选择或自行填写"),...q.options.map(o=>el("option",{value:o.label},o.label)));
        select.onchange=()=>{input.value=select.value;}; card.append(select);
      }
      card.append(input); controls.push([q.id,input]);
    }
    answers=()=>Object.fromEntries(controls.map(([qid,input])=>[qid,{answers:[input.value]}]));
  }
  if (method === "mcpServer/elicitation/request") {
    if (p.mode === "url") {
      try { const u=new URL(p.url); if (["http:","https:"].includes(u.protocol)) card.append(el("a",{href:u.href,target:"_blank",rel:"noopener noreferrer"},"打开认证页面 ↗")); } catch {}
    } else {
      const input=el("textarea",{rows:4},"{}");card.append(el("label",{},"按照 requestedSchema 填写 JSON"),el("pre",{},json(p.requestedSchema)),input);content=()=>JSON.parse(input.value);
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
  if (method === "item/tool/requestUserInput") actions.append(button("提交答案",()=>submit("accept"),"primary"));
  else if (method === "item/permissions/requestApproval") {
    actions.append(button("拒绝本次",()=>submit("decline")),button("本轮允许请求的权限",()=>submit("accept","turn"),"primary"),button("本会话允许请求的权限",()=>submit("accept","session")));
  } else {
    const decisions=a.decisions || (Array.isArray(p.availableDecisions)?p.availableDecisions:["accept","decline","cancel"]);
    for (const decision of decisions) {
      const label=p.mode==="url"&&decision==="accept"?"已完成认证":decisionLabel(decision);
      const action=button(label,()=>submit(decision),decision==="accept"?"primary":"");
      if(typeof decision === "object" && decision!==null) {
        card.append(el("details",{class:"approval-rule"},el("summary",{},"待提交的规则："+label),el("p",{class:"help"},"将提交下面的精确规则。命令规则不等于信任整个目录；后续命令仍可能需要审批。"),el("pre",{},json(decision))));
        actions.append(action);
      } else actions.append(action);
    }
    if(!decisions.length) actions.append(el("p",{class:"error"},"服务端没有提供可用决定。可用对话顶部的停止按钮中止任务。"));
  }
  card.append(actions);return card;
}

function permissionEditor(current) {
  const p=current.permissions || {};
  const select=(id,values,value)=>{const node=el("select",{id},...values.map(([v,t])=>el("option",{value:v},t)));node.value=value;return node;};
  const sandbox=select("permission-sandbox",[["workspace-write","工作区可写"],["read-only","只读沙箱"],["danger-full-access","完整访问（关闭沙箱）"]],p.sandbox||"workspace-write");
  const policy=select("permission-policy",[["on-request","需要时请求审批"],["never","不请求审批（受限操作可能失败）"]],p.approvalPolicy||"on-request");
  const reviewer=select("permission-reviewer",[["user","由我审批"],["auto_review","Codex 自动审查（需模型支持）"]],p.reviewer||"user");
  const network=select("permission-network",[["inherit","沿用 Codex 配置"],["false","禁止直接联网"],["true","允许直接联网"]],p.networkAccess===undefined?"inherit":String(p.networkAccess));
  const note=el("p",{class:"help"});
  const update=()=>{
    network.disabled=sandbox.value!=="workspace-write";
    reviewer.disabled=policy.value==="never";
    note.className=sandbox.value==="danger-full-access"?"error":"help";
    note.textContent=sandbox.value==="danger-full-access"?"完整访问会移除文件和网络沙箱边界；执行仍受系统用户权限与服务端要求约束。":policy.value==="never"?"不请求审批不会扩大沙箱权限。超出边界的操作可能直接失败。":"默认在沙箱内执行。自动审查由 Codex 提供，可能增加模型调用，服务端也可能拒绝此配置。";
  };
  sandbox.onchange=policy.onchange=update;update();
  return {
    node:el("fieldset",{class:"permission-fields"},el("legend",{},"实例运行权限"),el("label",{for:"permission-sandbox"},"沙箱边界"),sandbox,el("label",{for:"permission-policy"},"审批策略"),policy,el("label",{for:"permission-reviewer"},"审批处理方"),reviewer,el("label",{for:"permission-network"},"工作区沙箱网络"),network,note,el("p",{class:"help"},"保存后在该实例各会话的下一轮生效；正在执行或等待审批的任务保持原配置。不会修改你的 config.toml。生效值以 App Server 返回为准。")),
    value:()=>({sandbox:sandbox.value,approvalPolicy:policy.value,reviewer:reviewer.value,...(sandbox.value==="workspace-write"&&network.value!=="inherit"?{networkAccess:network.value==="true"}:{})})
  };
}

function runtimeSnapshot(r) {
  const box=el("div",{class:"runtime-snapshot"});
  if(!r?.connectionId) {box.append(el("p",{class:"help"},"会话尚未收到运行配置。启动任务后显示 App Server 返回的生效值。"));return box;}
  box.append(el("p",{class:"help"},(r.live?"当前连接":"上次连接记录")+" · "+new Date(r.connectedAt).toLocaleString()));
  const effective=r.effective || {}, sandbox=effective.sandbox;
  const pairs=[["模型",effective.model],["Provider",effective.modelProvider],["工作目录",effective.cwd],["沙箱",typeof sandbox==="string"?sandbox:sandbox?.type],["审批策略",effective.approvalPolicy],["审批处理方",effective.approvalsReviewer],["网络",sandbox&&typeof sandbox==="object"&&"networkAccess" in sandbox?(sandbox.networkAccess?"允许直接联网":"禁止直接联网"):"未返回"],["CODEX_HOME",r.codexHome],["运行版本",r.userAgent]];
  const fields=el("dl",{class:"runtime-fields"});
  for(const [label,value] of pairs)fields.append(el("dt",{},label),el("dd",{},value==null?"未返回":typeof value==="string"?value:json(value)));
  box.append(fields);
  if(r.requested)box.append(el("details",{},el("summary",{},"请求配置与原始生效配置"),el("pre",{},json({requested:r.requested,effective:r.effective,observedAt:r.observedAt}))));
  if(r.notices?.length) {
    box.append(el("h3",{},"此连接的告警与执行结果"));
    for(const n of [...r.notices].reverse()) box.append(el("section",{class:"runtime-notice "+n.level},el("strong",{},n.kind+" · "+({warning:"警告",error:"错误",info:"信息"}[n.level]||n.level)+(n.count>1?` · ${n.count} 次`:"")),el("pre",{},n.message),el("small",{class:"muted"},n.source+" · "+new Date(n.lastSeen).toLocaleString())));
  } else box.append(el("p",{class:"help"},"此连接尚未记录告警；不代表所有工具已验证。"));
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
  bar.replaceChildren(el("span",{class:"runtime-model"},r?.effective?.model?"运行模型："+r.effective.model:"运行配置待获取"),button(notices.length?`运行状态 · ${notices.length} 项告警`:"查看运行配置",()=>{
    $("#runtime-detail").replaceChildren(runtimeSnapshot(state.runtime));$("#runtime-dialog").showModal();
  },notices.length?"notice-button":""));
}

async function instanceRuntimeCard(iid) {
  const rows=await api(`/instances/${iid}/runtime`);
  const box=el("div",{class:"card"},el("h3",{},"实例运行记录"),el("p",{class:"help"},"显示最近 50 个连接。告警按连接去重，旧连接记录保留时间标记。"));
  for(const r of rows)box.append(el("details",{},el("summary",{},(r.id.startsWith("config-")?"配置连接":"会话 "+r.id.slice(0,8))+" · "+(r.live?"在线":"历史")+" · "+r.notices.length+" 项记录"),runtimeSnapshot(r)));
  if(!rows.length)box.append(el("p",{},"还没有连接记录。运行诊断后可查看启动告警。"));
  return box;
}
