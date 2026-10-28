"use strict";

// Instance configuration and workspace execution are separate identities. Opening
// these pages only reads the registry; probing and starting runtimes is explicit.
window.RunDeskInstances = (() => {
  const enc = encodeURIComponent;
  const rememberedKey = iid => "rundesk-instance-workspace:" + iid;
  const environmentStates = {not_created:"尚未创建",created:"待启动",running:"运行中",exited:"已停止",dead:"异常退出",unknown:"待核对",paused:"已暂停",restarting:"正在重启"};
  let generation = 0;

  function route(iid, wid, tail = "") {
    return "#instance/" + enc(iid) + (tail ? "/" + tail : "") + (wid ? "?workspace=" + enc(wid) : "");
  }
  function remember(iid, wid) {
    if (!iid || !wid) return;
    try { localStorage.setItem(rememberedKey(iid), wid); } catch {}
  }
  function remembered(iid) {
    try { return localStorage.getItem(rememberedKey(iid)); } catch { return ""; }
  }
  function binding(iid) {
    for (const a of state.applications) {
      const p = (a.runtimePresets || []).find(p => p.instanceId === iid);
      if (p) return {...a, instanceId:p.instanceId, workspaceId:p.workspaceId, presetId:p.id, presetName:p.name, enabled:p.enabled};
    }
    const a = state.applications.find(a => a.instanceId === iid);
    return a ? {...a, presetId:""} : null;
  }
  function source(iid, wid = ws()?.id) {
    const b = binding(iid);
    if (b?.presetId && !b.enabled) throw Error(rdText("关联的应用预设已停用，历史仍可查看。"));
    if (b?.presetId && wid !== b.workspaceId) throw Error(rdText("这个工作区不能用于当前实例，请重新选择。"));
    return b?.presetId ? {kind:"application",appId:b.appId,presetId:b.presetId} : undefined;
  }
  function workspacesFor(i) {
    const b = binding(i.id);
    if (b?.presetId) return state.workspaces.filter(w => w.id === b.workspaceId);
    const reserved = new Set(state.applications.flatMap(a => (a.runtimePresets || []).map(p => p.workspaceId)));
    return state.workspaces.filter(w => !reserved.has(w.id) && (i.execution?.mode !== "docker" || !(state.environments || []).some(e => e.workspaceId === w.id && e.instanceId !== i.id)));
  }
  function chooseWorkspace(i, wid) {
    const choices = workspacesFor(i), b = binding(i.id);
    if (wid && !choices.some(w => w.id === wid)) throw Error(rdText("这个工作区不能用于当前实例，请重新选择。"));
    const candidates = [wid, b?.presetId ? b.workspaceId : "", remembered(i.id), ...(state.environments || []).filter(e => e.instanceId === i.id).map(e => e.workspaceId), ...state.sessions.filter(s => s.instanceId === i.id).map(s => s.workspaceId), b?.workspaceId, ...choices.map(w => w.id)];
    return choices.find(w => w.id === candidates.find(id => choices.some(w => w.id === id))) || null;
  }
  function select(iid, wid) {
    const i = state.instances.find(i => i.id === iid);
    if (!i) throw Error(rdText("实例不存在，请刷新列表。"));
    const w = chooseWorkspace(i, wid);
    renderInstanceOptions(i.id);
    state.skills = [];state.mcp = null;$("#models")?.replaceChildren();
    $("#workspace").replaceChildren(...workspacesFor(i).map(w => el("option", {value:w.id}, w.name)));
    $("#workspace").value = w?.id || "";
    if (w) remember(i.id, w.id);
    state.instanceResolving = false;
    updateWorkspaceLabel();
    return {instance:i,workspace:w,binding:binding(i.id)};
  }
  async function load(g, selection) {
    const [instances,workspaces,sessions,applications,environments] = await Promise.all([api("/instances"),api("/workspaces"),api("/sessions"),api("/applications"),api("/environments")]);
    if (g !== generation || selection !== state.selection) return false;
    Object.assign(state, {instances,workspaces,sessions,applications,environments});
    return true;
  }
  function current(g, selection, view) { return g === generation && selection === state.selection && state.view === view; }
  function clearSelection() {
    $("#instance").value = "";
    $("#workspace").value = "";
    state.instanceResolving = true;
  }
  function heading(title, description, action) {
    return el("div", {class:"page-intro instance-heading"}, el("div", {}, el("p", {class:"eyebrow"}, "AGENT INSTANCES"), el("h1", {}, title), el("p", {}, description)), action);
  }
  function failure(page, error, retry) {
    page.replaceChildren(el("div", {class:"instance-empty",role:"alert"}, el("h2", {}, rdText("暂时无法读取实例")), el("p", {}, error.message), button(rdText("重试"), retry, "primary"), button(rdText("返回实例列表"), open, "quiet")));
  }
  function engine(i) { return i.agentRuntime?.kind === "kun" ? "Kun" : "Codex"; }
  function mode(i) { return i.execution?.mode === "docker" ? "Docker" : rdText("本机"); }
  function model(i) { return i.agentRuntime?.kind === "kun" ? i.agentRuntime.model || rdText("尚未配置模型") : i.defaultModel || rdText("沿用 Agent 配置"); }
  function mark(i) { return el("span", {class:"instance-mark","aria-hidden":"true"}, engine(i) === "Kun" ? "K" : "C"); }

  async function open() {
    resetConversation();
    state.instanceResolving = false;
    const g = ++generation, selection = state.selection;
    setProductPage("instances"); history.replaceState(null, "", "#instances");
    const page = $("#instances-page"); setLoading(page);
    try {
      if (!await load(g, selection) || !current(g, selection, "instances")) return;
      const query = el("input", {type:"search",id:"instance-search",placeholder:rdText("搜索 Agent、引擎或镜像…"),"aria-label":rdText("搜索 Agent")});
      const filter = el("select", {id:"instance-engine-filter","aria-label":rdText("实例引擎筛选")}, el("option", {value:""}, rdText("全部引擎")), el("option", {value:"codex"}, "Codex"), el("option", {value:"kun"}, "Kun"));
      const grid = el("div", {class:"instance-grid"});
      const paint = () => {
        const text = query.value.trim().toLowerCase();
        const rows = state.instances.filter(i => (!filter.value || (i.agentRuntime?.kind || "codex") === filter.value) && [i.name,i.description,engine(i),i.execution?.image].join(" ").toLowerCase().includes(text));
        grid.replaceChildren(...rows.map(card));
        if (!rows.length) grid.append(el("div", {class:"instance-empty"}, el("h2", {}, rdText("没有匹配的 Agent")), el("p", {}, rdText("调整筛选条件，或先准备 Docker 镜像。"))));
      };
      query.oninput = paint; filter.onchange = paint;
      page.replaceChildren(heading(rdText("Agents"), rdText("Agent 保存模型和运行配置；选定工作区后开始对话。Docker 容器按 Agent 与工作区组合创建。"), el("div", {class:"actions"}, button(rdText("刷新"), open, "quiet"), button(rdText("准备 Docker 镜像"), () => RunDeskBuildCenter.open("recipes"), "quiet"), button(rdText("从镜像添加 Agent"), () => RunDeskBuildCenter.open("images"), "primary"))), el("div", {class:"instance-toolbar"}, query, filter, el("span", {class:"muted"}, rdFormat("${0} 个 Agent",state.instances.length))), grid);
      paint(); updateProductNavigation();
    } catch (error) { if (current(g, selection, "instances")) failure(page,error,open); }
  }
  function card(i) {
    const sessions = state.sessions.filter(s => s.instanceId === i.id), count = sessions.filter(s => active(s.status)).length;
    const workCount = new Set([...(state.environments || []).filter(e => e.instanceId === i.id).map(e => e.workspaceId), ...sessions.map(s => s.workspaceId)]).size;
    const b = binding(i.id);
    const tags = el("div", {class:"instance-tags"}, el("span", {class:"badge"}, engine(i)), el("span", {class:"badge"}, mode(i)), i.id === "default" ? el("span", {class:"badge"}, rdText("默认 Agent")) : null);
    const chat = button(rdText("开始对话"), () => conversation(i.id,undefined,true), "quiet");
    chat.disabled = !!b?.presetId && !b.enabled;
    return el("article", {class:"instance-card","data-instance-id":i.id}, el("div", {class:"instance-card-top"},mark(i), el("span", {class:"instance-status"+(count?" busy":"")}, count ? rdFormat("${0} 个任务进行中",count) : rdText("暂无进行中的任务"))), el("h2", {}, i.name), tags, el("p", {class:"instance-description"},i.description || (i.id === "default" ? rdText("本机默认 Agent，可直接开始对话。") : rdText("独立管理模型、Skills、MCP 和运行环境。"))), el("div", {class:"instance-card-metadata"}, el("span", {}, rdFormat("${0} 个工作区",workCount)), el("span", {}, rdFormat("${0} 条会话",sessions.length))), el("div", {class:"instance-card-actions"}, button(rdText("配置 Agent"), () => show(i.id), "primary"), chat), b?.presetId && !b.enabled ? el("p", {class:"help"},rdText("关联的应用预设已停用，历史仍可查看。")) : null);
  }

  async function show(iid, wid) {
    resetConversation();
    clearSelection();
    const g = ++generation, selection = state.selection;
    setProductPage("instance"); history.replaceState(null, "", route(iid,wid));
    const page = $("#instances-page"); setLoading(page);
    try {
      if (!await load(g, selection) || !current(g,selection,"instance")) return;
      const context = select(iid,wid);
      history.replaceState(null, "", route(iid,context.workspace?.id));
      renderDetail(page,context);
      return context;
    } catch (error) { if (current(g,selection,"instance")) failure(page,error,() => show(iid)); }
    return null;
  }
  function renderDetail(page, {instance:i,workspace:w,binding:b}) {
    const choices = workspacesFor(i), preset = !!b?.presetId;
    const work = el("select", {id:"instance-workspace","aria-label":rdText("当前工作区"),disabled:preset||!choices.length}, ...choices.map(x => el("option", {value:x.id}, x.name)));
    work.value = w?.id || ""; work.onchange = () => safe(() => show(i.id,work.value));
    const chat = button(rdText("新建对话"), () => conversation(i.id,w?.id,true), "primary");
    chat.disabled = !w || (preset && !b.enabled);
    const capabilities = el("div", {class:"instance-capabilities"});
    for (const [title,description,tab,icon] of [["模型与认证","模型、账号与运行权限","instances","◉"],["Skills","管理完整技能目录与项目技能","skills","S"],["MCP","配置外部工具与服务连接","mcp","M"],["原生插件","查看原生包与组件归属（只读）","plugins","P"]]) {
      const action = button("", () => openSettings(tab), "instance-capability");
      action.disabled = !w || (tab === "plugins" && engine(i) === "Kun");
      action.append(el("span", {class:"instance-capability-icon","aria-hidden":"true"},icon), el("span", {}, el("strong", {},rdText(title)),el("small", {},rdText(tab === "plugins" && engine(i) === "Kun" ? "Kun 不支持 Codex 原生插件" : description))), el("span", {class:"instance-arrow","aria-hidden":"true"},"↗"));
      capabilities.append(action);
    }
    const workspaceActions = el("div", {class:"instance-workspace-actions"}, work);
    if (!preset) workspaceActions.append(button(rdText("新建工作区"),() => createWorkspace(i.id),"quiet"));
    const runtime = (state.environments || []).find(e => e.instanceId === i.id && e.workspaceId === w?.id);
    const maintenance = button(rdText("管理运行环境"), () => RunDeskEnvironments.open({...b,instanceId:i.id,name:i.name,workspaceId:w?.id,instanceContext:true}), "quiet");
    maintenance.disabled = i.id === "default" || engine(i) === "Kun";
    const facts = el("dl", {class:"instance-facts"}, ...[["Agent",engine(i)],["运行方式",mode(i)],["默认模型",model(i)],["运行状态",i.execution?.mode === "docker" ? rdText(environmentStates[runtime?.state] || "尚未创建") : rdText("按任务启动")]].flatMap(([k,v]) => [el("dt", {},rdText(k)),el("dd", {},v)]));
    const sessions = state.sessions.filter(s => s.instanceId === i.id && (!w || s.workspaceId === w.id));
    const tasks = el("div", {class:"application-tasks instance-sessions"}, ...(sessions.length ? sessions.slice(0,30).map(taskRow) : [el("div", {class:"instance-empty compact"}, el("h3", {},rdText("这个工作区还没有会话")),el("p", {},rdText("开始对话后，可在这里查看任务、轨迹和制品。")))]));
    const association = b ? el("div", {class:"instance-association"},el("span", {},rdText("连接的应用：") + b.name + (b.presetId ? " · " + b.presetName : "")), button(rdText("查看应用接入"),() => showApplication(b.appId,b.presetId||""),"quiet")) : null;
    page.replaceChildren(el("div", {class:"instance-back"},button(rdText("← 所有 Agent"),open,"quiet back-link")), heading(i.name,i.description||rdText("配置 Agent、选择工作区并查看运行记录。"),el("div", {class:"actions"},button(rdText("刷新"),() => show(i.id,w?.id),"quiet"),chat)),
      el("div", {class:"instance-detail-grid"},el("section", {class:"instance-overview"},el("div", {class:"instance-section-title"},mark(i),el("h2", {},rdText("实例概况"))),facts,i.execution?.image ? el("p", {class:"instance-image",title:i.execution.image},rdText("镜像：")+i.execution.image) : null,maintenance),
      el("section", {class:"instance-workspace"},el("h2", {},rdText("当前工作区")),workspaceActions,w ? el("p", {class:"instance-workspace-path"},w.path||w.name) : el("p", {class:"help"},rdText("还没有可用工作区，请创建一个工作区后配置能力。")),el("p", {class:"help"},preset ? rdText("此实例关联应用预设，使用固定工作区。") : rdText("工作区保存项目文件和制品；切换工作区不会切换实例。")),preset && !b.enabled ? el("p", {class:"error"},rdText("关联的应用预设已停用，历史仍可查看。")) : null)),
      el("section", {class:"instance-capability-section"},el("div", {class:"instance-section-title"},el("h2", {},rdText("实例能力")),el("span", {class:"muted"},w?.name||rdText("尚未选择工作区"))),capabilities,el("p", {class:"instance-scope-note"},i.execution?.mode === "docker" ? rdText("当前版本中，Docker 的 Skills、MCP 和认证仍按工作区环境保存；这里修改的是上方选中的工作区，尚未跨工作区同步。") : rdText("实例级能力由这个 Agent 使用；工作区还可以提供自己的项目技能。"))),...(association?[association]:[]),
      el("section", {class:"instance-history"},el("div", {class:"task-section-heading"},el("h2", {},rdText("会话与任务记录")),el("span", {class:"muted"},rdFormat("${0} 条",sessions.length))),tasks,sessions.length > 30 ? el("p", {class:"help"},rdText("这里显示最近 30 条会话，进入对话后可搜索完整会话列表。")) : null));
    updateProductNavigation();
  }
  function createWorkspace(iid) {
    const i = state.instances.find(i => i.id === iid);
    if (!i || binding(iid)?.presetId) return;
    const pendingKey = "rundesk-instance-workspace-create:" + iid;
    let requestBody;
    try {const pending = JSON.parse(sessionStorage.getItem(pendingKey)||"null");if(typeof pending?.name === "string")requestBody = pending;} catch {}
    const rememberRequest = value => {try {if(value)sessionStorage.setItem(pendingKey,JSON.stringify(value));else sessionStorage.removeItem(pendingKey);} catch {}};
    const dialog = el("dialog", {class:"application-register"}), name = el("input", {id:"instance-workspace-name",required:true,maxlength:160,value:requestBody?.name||i.name+" · "+rdText("工作区"),disabled:!!requestBody});
    const error = el("p", {class:"error",role:"status"}), submit = el("button", {type:"submit",class:"primary"},rdText("创建工作区")), close = button(rdText("关闭"),() => dialog.close(),"quiet");
    if (requestBody) error.textContent = rdText("上次创建结果尚未确认；重试会继续同一次创建，不会重复添加。");
    const form = el("form", {},el("div", {class:"dialog-head"},el("h2", {},rdText("新建工作区")),close),el("label", {},rdText("工作区名称"),name),el("p", {class:"help"},rdText("创建独立的项目目录。容器将在需要运行任务时启动。")),error,submit);
    const contextSelection = state.selection;
    let busy = false;
    form.onsubmit = async event => {
      event.preventDefault(); if (busy) return; busy = true; submit.disabled = true; close.disabled = true; name.disabled = true; error.textContent = "";
      requestBody ||= {name:name.value.trim()};
      rememberRequest(requestBody);
      try {
        const w = await api("/workspaces",{method:"POST",body:requestBody}); rememberRequest(null);remember(iid,w.id);
        const sameContext = dialog.open && contextSelection === state.selection && state.view === "instance" && instance()?.id === iid;
        dialog.close();
        if (sameContext) await show(iid,w.id);
        else toast(rdText("工作区已创建，可从实例页面选择。"));
      }
      catch (failure) { error.textContent = failure.message; if (failure.status >= 400 && failure.status < 500 && !["request_in_progress","request_unconfirmed"].includes(failure.code)) {requestBody = undefined;rememberRequest(null);name.disabled = false;} }
      finally {busy = false;submit.disabled = false;close.disabled = false;}
    };
    dialog.append(form);document.body.append(dialog);dialog.addEventListener("close",() => dialog.remove(),{once:true});dialog.showModal();name.focus();
  }
  async function conversation(iid, wid, fresh = false) {
    resetConversation();
    clearSelection();
    const g = ++generation, selection = state.selection;
    // Use the instance page while resolving the requested identity so a failed
    // selection cannot leave a composer attached to the previous instance.
    setProductPage("instance"); const page = $("#instances-page");setLoading(page);
    try {
      if (!await load(g,selection) || !current(g,selection,"instance")) return;
      const c = select(iid,wid);
      if (!c.workspace) { renderDetail(page,c);history.replaceState(null,"",route(iid));return; }
      if (c.binding?.presetId && !c.binding.enabled) { renderDetail(page,c);history.replaceState(null,"",route(iid,c.workspace.id));return; }
      setProductPage("conversation");history.replaceState(null,"",route(iid,c.workspace.id,"conversation"));
      $("#session-scope").value = "active";$("#session-search").value = "";
      if (!fresh) {
        const recent = state.sessions.find(s => s.instanceId === iid && s.workspaceId === c.workspace.id && !s.archived);
        if (recent) {await selectSession(recent.id);return;}
      }
      restoreProductDraft();renderSessions();renderStatus();$("#prompt").focus();
    } catch (error) { if (current(g,selection,"instance")) failure(page,error,() => conversation(iid,undefined,fresh));else throw error; }
  }
  return {open,show,conversation,select,remember,route,binding,source,workspacesFor};
})();
