"use strict";

// Changes are explicit and scoped to the selected server. Discovery never calls a tool.
function mcpPolicyEditor(info, cp, serverName) {
  let config = {}, catalog = new Map(), changes = new Map(), cursor = "", generation = 0, busy = false;
  const rows = el("div", {class:"mcp-policy-rows"});
  const status = el("p", {class:"help",role:"status"});
  const load = button(rdText("读取已保存服务的工具列表"),()=>discover(false),"quiet");
  const more = button(rdText("下一页工具"),()=>discover(true),"quiet"); more.hidden=true;
  const node = el("section",{class:"mcp-policy-editor"},el("h4",{},rdText("工具使用权限")),
    el("p",{class:"help"},rdText("设置仅作用于当前助手／应用。读取列表会连接已保存的服务，不执行工具；连接信息有改动时请先保存。")),
    el("div",{class:"actions"},load,more),status,rows,
    el("p",{class:"help"},info.runtime==="kun"?"Kun 默认每次询问；“始终允许”直接执行。尚不支持 auto/writes 自动判断。已开始的运行保持原有权限快照。":rdText("托管策略仍可能要求审批。已弹出的审批、工具自己的表单及登录请求需单独处理。")));
  function mode(name) {
    if(changes.has(name)) return changes.get(name);
    if(config.disabled_tools?.includes(name)||(Array.isArray(config.enabled_tools)&&!config.enabled_tools.includes(name)))return "disabled";
    return config.tools?.[name]?.approval_mode || "default";
  }
  function render() {
    const names = new Set([...catalog.keys(),...Object.keys(config.tools||{}),...(config.disabled_tools||[]),...(config.enabled_tools||[])]);
    rows.replaceChildren();
    for(const name of [...names].sort()) {
      const select=el("select",{"aria-label":rdFormat("${0} 的使用权限",name),"data-mcp-tool":name},
        el("option",{value:"default"},rdText("启用 · 按默认审批")),el("option",{value:"approve"},rdText("始终允许")),
        el("option",{value:"prompt"},rdText("每次询问")),el("option",{value:"disabled"},rdText("禁用工具")));
      const current=mode(name);
      if(current==="auto"||current==="writes")select.append(el("option",{value:current},current==="auto"?rdText("自动判断（已有配置）"):rdText("仅写入时询问（已有配置）")));
      select.value=current;select.onchange=()=>changes.set(name,select.value);
      const tool=catalog.get(name), label=el("div",{},el("strong",{},name));
      if(tool?.description)label.append(el("p",{class:"help"},tool.description));
      else if(!tool)label.append(el("small",{class:"help"},rdText("已有配置；尚未确认服务是否提供此工具")));
      rows.append(el("div",{class:"mcp-policy-row"},label,select));
    }
    if(!names.size)rows.append(el("p",{class:"help"},rdText("尚无工具列表。保存服务后点击读取，再逐项选择权限。")));
  }
  function fill(value) {
    generation++;busy=false;load.disabled=more.disabled=false;cursor="";more.hidden=true;status.textContent="";
    config=value;changes=new Map();catalog=new Map();
    const server=(info.status?.data||[]).find(s=>s.name===serverName());
    const tools=server?.tools;
    for(const tool of Array.isArray(tools)?tools:Object.values(tools||{})) if(typeof tool?.name==="string")catalog.set(tool.name,tool);
    render();
  }
  async function discover(next) {
    if(busy)return;
    const name=serverName();if(!name||!info.effectiveServers?.[name])throw Error(rdText("请先保存 MCP 服务，再读取工具列表。"));
    const stamp=generation;
    busy=true;load.disabled=more.disabled=true;status.textContent=rdText("正在读取工具列表…");
    try {
      const body={server:name,confirm:true};if(next)body.cursor=cursor;
      const record=await api(cp("/mcp-tests"),{method:"POST",body});
      if(stamp!==generation||name!==serverName())return;
      if(record.status!=="completed")throw Error(record.error?.message||record.error||rdText("无法读取工具列表，请检查 MCP 连接状态。"));
      if(!next)catalog=new Map();
      for(const tool of record.result?.tools||[])if(typeof tool.name==="string")catalog.set(tool.name,tool);
      cursor=record.result?.nextCursor||"";more.hidden=!cursor;
      status.textContent=rdFormat("已读取 ${0} 个工具；选择后点击保存配置。",catalog.size);render();
    } catch(e) {if(stamp===generation)status.textContent=e.message;throw e;}
    finally {if(stamp===generation){busy=false;load.disabled=more.disabled=false;}}
  }
  function apply(value) {
    const result=JSON.parse(JSON.stringify(value)), tools=result.tools||{};
    let disabled=[...(result.disabled_tools||[])], enabled=Array.isArray(result.enabled_tools)?[...result.enabled_tools]:null;
    for(const [name,mode] of changes) {
      if(mode==="disabled") {if(!disabled.includes(name))disabled.push(name);continue;}
      disabled=disabled.filter(n=>n!==name);if(enabled&&!enabled.includes(name))enabled.push(name);
      const policy={...(tools[name]||{})};
      if(mode==="default")delete policy.approval_mode;else policy.approval_mode=mode;
      if(Object.keys(policy).length)Object.defineProperty(tools,name,{value:policy,enumerable:true,writable:true,configurable:true});else delete tools[name];
    }
    if(Object.keys(tools).length)result.tools=tools;else delete result.tools;
    if(disabled.length)result.disabled_tools=disabled;else delete result.disabled_tools;
    if(enabled)result.enabled_tools=enabled;
    return result;
  }
  fill({});return {node,fill,apply};
}
