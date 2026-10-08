"use strict";

function renderCapabilityStrip() {
  const box=$("#capability-strip"); if(!box) return;
  const current=instance(), session=state.session;
  if(!current) {box.replaceChildren();return;}
  const model=session ? (session.model || rdText("Codex 自动选择")) : ($("#model").value.trim() || current.defaultModel || rdText("Codex 默认模型"));
  const open=button(rdText("查看能力"),openCapabilityDialog,"capability-link");open.id="view-capabilities";
  box.replaceChildren(el("span",{class:"capability-instance"},contextTitle()),el("span",{class:"capability-model",title:model},model),open);
  if(session?.source?.kind==="application") box.append(el("span",{class:"capability-source",title:session.source.taskId||""},rdText("来自 ")+session.source.appId));
}
function scopeLabel(scope) { return {instance:rdText("当前助手／应用"),project:rdText("项目"),other:rdText("其他来源")}[scope]||scope; }
function capabilityInventory(data) {
  const result=el("div",{class:"capability-inventory"});
  for(const [kind,message] of Object.entries(data.errors||{})) result.append(el("p",{class:"error"},kind+"："+message));
  if(data.skills) {
    const card=el("section",{class:"card"},el("h3",{},rdText("当前可用 Skills")));
    for(const skill of data.skills) card.append(el("div",{class:"capability-row"},el("strong",{},skill.name),el("span",{class:"badge"},scopeLabel(skill.sourceScope)),el("span",{class:"muted"},skill.enabled===false?rdText("已禁用"):rdText("已启用"))));
    if(!data.skills.length) card.append(el("p",{class:"help"},rdText("这个助手或应用在当前项目没有发现 Skill。")));
    if(data.skillWarnings?.length) card.append(el("p",{class:"error"},rdText("部分 Skill 加载失败，请在 Skills 设置中查看详细错误。")));
    result.append(card);
  }
  if(data.mcp) {
    const card=el("section",{class:"card"},el("h3",{},rdText("当前 MCP 服务")));
    const status=data.mcp.status, known=status?.data||[], names=new Map(known.map(x=>[x.name,x]));
    for(const server of data.mcp.servers||[]) {
      const observed=names.get(server.name);
      const label=!server.enabled?rdText("已禁用"):observed?rdFormat("${0} 个已发现工具",Object.keys(observed.tools||{}).length):rdText("未发现工具状态");
      card.append(el("div",{class:"capability-row"},el("strong",{},server.name),el("span",{class:"muted"},label)));
    }
    if(!data.mcp.servers?.length)card.append(el("p",{class:"help"},rdText("这个助手或应用在当前项目没有配置 MCP 服务。")));
    if(status?.error)card.append(el("p",{class:"error"},status.error));
    if(status?.nextCursor)card.append(el("p",{class:"help"},rdText("当前仅显示首批状态；更多内容请在 MCP 设置中查看。")));
    card.append(el("p",{class:"help"},rdText("这里显示配置连接发现的能力，不代表当前会话已调用，也不代表外部服务已通过真实执行测试。")));
    result.append(card);
  }
  return result;
}
async function renderConfigurationOverview(target) {
  const current=instance(), work=ws(), iid=current.id, wid=work.id;
  const data=await api(`/instances/${iid}/configuration?workspaceId=${wid}`);
  if(state.settingsTab!=="overview") return;
  const openTab=(tab)=>{state.settingsTab=tab;return renderSettings();};
  const count=data.sessionCount;
  const tiles=el("div",{class:"configuration-grid"});
  for(const [label,description,tab] of [
    [rdText("模型与认证"),current.defaultModel||rdText("沿用 Codex 模型配置"),"instances"],
    ["Skills",rdText("完整技能目录与项目约定"),"skills"],
    [rdText("MCP 工具"),rdText("连接服务、配置凭据与查看状态"),"mcp"],
    [rdText("运行诊断"),rdText("权限、生效值与连接问题"),"runtime"],
  ]) {
    const b=button("",()=>openTab(tab),"configuration-tile");b.append(el("strong",{},label),el("span",{},description));tiles.append(b);
  }
  const inventory=el("div");
  const scan=button(rdText("读取当前能力"),async()=>{
    scan.disabled=true;setLoading(inventory);
    try {const value=await api(`/instances/${iid}/configuration?workspaceId=${wid}&probe=1`);if(target.isConnected)inventory.replaceChildren(capabilityInventory(value));}
    catch(e){inventory.replaceChildren(el("p",{class:"error"},e.message));}
    finally{scan.disabled=false;}
  });scan.id="scan-instance-capabilities";
  target.replaceChildren(
    el("section",{class:"configuration-intro"},el("p",{class:"eyebrow"},"YOUR ASSISTANT"),el("h3",{},contextTitle()),el("p",{},current.description||rdText("为这个助手配置长期使用的模型、技能和工具。")),el("p",{class:"help"},rdFormat("${0} 个关联会话 · 当前工作区：${1}",count,work.name))),
    tiles,
    el("p",{class:"configuration-scope"},current.execution?.mode==="docker"?rdText("模型默认值用于新对话。Docker 应用的技能、MCP、认证和原生记忆按项目环境独立保存。"):rdText("模型默认值用于新对话。技能与工具由当前助手或应用共享，项目配置也可能参与；会话中可查看实际运行情况。")),
    el("div",{class:"actions"},scan,button(rdText("模型、认证与权限"),()=>openTab("instances"))),
    inventory,
    el("details",{class:"card integration-links"},el("summary",{},rdText("开发者接入说明")),el("h3",{},rdText("应用接入")),el("p",{},rdText("应用与此 WebUI 使用 /api/v1。支持任务提交去重、事件续传和接收回执查询。")),el("a",{href:"/api/v1/openapi.json",target:"_blank",rel:"noopener"},rdText("打开 OpenAPI 定义")),el("p",{class:"help"},rdText("现有 /api 接口继续兼容。应用可使用独立凭据，绑定应用与允许访问的项目；凭据在应用页面管理。"))),
  );
}
async function openCapabilityDialog() {
  if(!instance()||!ws())return;
  const dialog=$("#capabilities-dialog"),body=$("#capabilities-content");
  const iid=instance().id,wid=ws().id,sid=state.session?.id,selection=state.selection;
  dialog.showModal();setLoading(body);
  try {
    const path=sid?`/sessions/${sid}/configuration?probe=1`:`/instances/${iid}/configuration?workspaceId=${wid}&probe=1`;
    const data=await api(path);
    if(!dialog.open||selection!==state.selection||iid!==instance()?.id)return;
    const source=data.session?.source;
    const heading=el("div",{class:"card"},el("h3",{},data.instance.name),el("p",{},rdText("工作区：")+data.workspace.name),el("p",{},rdText("会话模型：")+(data.session?data.session.model||rdText("Codex 自动选择"):data.instance.defaultModel||rdText("Codex 默认模型"))));
    if(source?.kind==="application")heading.append(el("p",{class:"help"},rdText("来源应用：")+source.appId+(source.taskId?rdText(" · 业务任务：")+source.taskId:"")));
    if(data.session)heading.append(el("p",{class:"help"},rdText("这条对话使用所属助手或应用的配置。这里可查看当前能力及上次实际运行情况。")));
    const actions=el("div",{class:"actions"},button(rdText("打开设置"),async()=>{dialog.close();await openSettings("overview");}));
    const last=data.lastSubmission;
    const submitted=el("section",{class:"card"},el("h3",{},rdText("上次提交与实际运行")));
    if(last)submitted.append(el("p",{},rdText("提交时选用的 Skills：")+(last.skills?.map(x=>x.name).join("、")||rdText("未显式指定，由 Codex 按需发现"))),el("p",{class:"help"},rdText("运行编号：")+last.runId+" · "+new Date(last.time).toLocaleString()));
    else submitted.append(el("p",{class:"help"},rdText("尚无任务提交记录。")));
    if(data.runtime)submitted.append(runtimeSnapshot(data.runtime));
    body.replaceChildren(heading,actions,capabilityInventory(data),submitted);
  } catch(e) {body.replaceChildren(el("p",{class:"error"},e.message),button(rdText("重试"),()=>{dialog.close();return openCapabilityDialog();}));}
}
$("#close-capabilities").onclick=()=>$("#capabilities-dialog").close();
$("#model").addEventListener("input",renderCapabilityStrip);
