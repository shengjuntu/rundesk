"use strict";

// Workspaces organize conversations; the execution identity belongs to each
// session. Hidden legacy selects remain adapters for configuration/API code.
window.RunDeskConversations = (() => {
  const newButton = $("#new-session"), expanded = new Set();
  const lastWorkspaceKey = "rundesk-conversation-workspace";
  const engineKey = wid => "rundesk-workspace-engine:" + wid;
  const sessionKey = wid => "rundesk-workspace-session:" + wid;
  let generation = 0;
  const read = key => {try {return localStorage.getItem(key) || "";} catch {return "";}};
  const write = (key,value) => {try {localStorage.setItem(key,value);} catch {}};
  const ordered = rows => [...rows].sort((a,b) => Number(!!b.pinned)-Number(!!a.pinned) || (b.updated||b.created||"").localeCompare(a.updated||a.created||""));
  const daily = i => (i?.agentRuntime?.kind || "codex") === "codex";
  const ordinary = w => w.purpose !== "maintenance";
  const label = (i,kind) => {
    const engine = (kind || i?.agentRuntime?.kind) === "kun" ? "Kun" : "Codex";
    return engine === "Kun" ? "Kun · " + rdText("维护记录") : i?.id === "default" || !i ? rdText("通用助手") : i.name;
  };
  function owner(wid) {
    const project = (state.projectEnvironments || []).find(p => p.workspaceId === wid);
    if (project) return {...project,project:true};
    for (const a of state.applications) {
      const p = (a.runtimePresets || []).find(p => p.workspaceId === wid);
      if (p) return {...p,appId:a.appId,presetId:p.id};
    }
    const app = state.applications.find(a => a.workspaceId === wid);
    return app ? {...app,presetId:""} : null;
  }
  function available(wid) {
    if (!state.workspaces.some(w => w.id === wid && ordinary(w))) return [];
    const bound = owner(wid);
    return state.instances.filter(i => {
      if (!daily(i)) return false;
      if (bound && bound.instanceId !== i.id) return false;
      const p = RunDeskInstances.binding(i.id);
      if (p?.presetId) return p.enabled && (wid === p.workspaceId || bound?.project && bound.instanceId === i.id && bound.presetId === p.presetId);
      return true;
    });
  }
  function source(iid,wid) {
    if (!available(wid).some(i => i.id === iid)) throw Error(rdText("此工作区不能使用所选引擎，请重新选择。"));
    const bound = owner(wid);
    if (bound?.project) return {kind:"application",appId:bound.appId,projectId:bound.projectId,...(bound.presetId?{presetId:bound.presetId}:{})};
    return RunDeskInstances.source(iid,wid);
  }
  function remember(wid,iid,sid) {
    if (!wid) return;
    expanded.add(wid);write(lastWorkspaceKey,wid);
    if (iid && available(wid).some(i => i.id === iid)) write(engineKey(wid),iid);
    if (sid) write(sessionKey(wid),sid);
  }
  function choose(wid,preferred) {
    const choices = available(wid);
    if (preferred) {
      const i = choices.find(i => i.id === preferred);
      if (!i) throw Error(rdText("此工作区不能使用所选引擎，请重新选择。"));
      return i;
    }
    const recent = ordered(state.sessions.filter(s => s.workspaceId === wid && s.runtimeKind !== "kun" && !s.archived));
    return [read(engineKey(wid)),...recent.map(s => s.instanceId),"default",...choices.map(i => i.id)].map(id => choices.find(i => i.id === id)).find(Boolean);
  }
  function select(wid,iid) {
    $("#workspace").replaceChildren(...state.workspaces.map(w => el("option",{value:w.id},w.name)));
    $("#workspace").value = wid || "";
    renderInstanceOptions(iid || "");if(!iid)$("#instance").value="";
    state.skills=[];state.mcp=null;$("#models")?.replaceChildren();
    updateWorkspaceLabel();remember(wid,iid);
  }
  async function load(g,selection) {
    const [instances,workspaces,sessions,applications,environments] = await Promise.all([api("/instances"),api("/workspaces"),api("/sessions"),api("/applications"),api("/environments")]);
    const projects = await Promise.all(applications.map(a => api("/applications/"+encodeURIComponent(a.appId)+"/project-environments")));
    if(g!==generation || selection!==state.selection)return false;
    Object.assign(state,{instances,workspaces,sessions,applications,environments,projectEnvironments:projects.flat()});return true;
  }
  async function open(wid,{fresh=false,instanceId,refresh=true}={}) {
    if(state.sending || state.loadingSession)return;
    resetConversation();const selection=state.selection,g=++generation;
    setProductPage("conversation");
    state.loadingSession=true;renderStatus();
    try {
      if(refresh && !await load(g,selection))return;
      if(g!==generation || selection!==state.selection)return;
      const requested=wid || read(lastWorkspaceKey) || read("rundesk-assistant-workspace");
      const work=state.workspaces.find(w=>w.id===requested) || (!wid?state.workspaces.find(w=>ordinary(w)&&!owner(w.id))||state.workspaces.find(ordinary):null);
      if(wid&&!work)throw Error(rdText("工作区不存在，请刷新列表。"));
      const i=work?choose(work.id,instanceId):null;
      select(work?.id,i?.id);
      history.replaceState(null,"",work?"#workspace/"+encodeURIComponent(work.id):"#conversations");
      $("#session-scope").value="active";$("#session-search").value="";
      if(!fresh&&work){
        const sessions=ordered(state.sessions.filter(s=>s.runtimeKind!=="kun"&&s.workspaceId===work.id&&!s.archived&&(!instanceId||s.instanceId===instanceId)));
        const previous=sessions.find(s=>s.id===read(sessionKey(work.id)))||sessions[0];
        if(previous){state.loadingSession=false;return selectSession(previous.id);}
      }
      restoreProductDraft();renderSessions();$("#prompt").focus();
    } catch(error) {
      if(g===generation&&selection===state.selection){$("#instance").value="";state.skills=[];renderSessions();throw error;}
    } finally {
      if(g===generation&&selection===state.selection){state.loadingSession=false;renderStatus();}
    }
  }
  async function changeEngine(iid) {
    if(state.session || state.sending || state.loadingSession) return;
    const wid=ws()?.id,i=choose(wid,iid),text=$("#prompt").value;
    if(i.id===instance()?.id)return;
    resetConversation();select(wid,i.id);
    history.replaceState(null,"","#workspace/"+encodeURIComponent(wid));
    if(!state.drafts.has(draftKey()))state.drafts.set(draftKey(),{text,uploads:[],skills:[]});
    restoreProductDraft();renderSessions();renderStatus();$("#prompt").focus();
  }
  function enginePicker() {
    if(state.session||state.sending||state.loadingSession||!ws())return;
    const wid=ws().id,selection=state.selection;
    const picker=el("select",{id:"conversation-engine-choice","aria-label":rdText("会话运行环境")},...available(wid).map(i=>el("option",{value:i.id},label(i)+" · "+(i.execution?.mode==="docker"?"Docker":rdText("本机")))));
    picker.value=instance()?.id||"";
    const dialog=el("dialog",{class:"conversation-engine-dialog","aria-labelledby":"conversation-engine-title"},el("div",{class:"dialog-head"},el("h2",{id:"conversation-engine-title"},rdText("选择会话环境")),button(rdText("关闭"),()=>dialog.close(),"quiet")),el("div",{class:"engine-picker-body"},el("p",{class:"help"},rdText("所有日常会话均由 Codex 执行。这里选择本条新会话使用的配置与工具环境。")),picker,(state.uploads.length||state.chosenSkills.length)?el("p",{class:"help"},rdText("已选附件和技能保留在原环境的草稿中。")):null));
    picker.onchange=()=>safe(async()=>{if(selection!==state.selection||wid!==ws()?.id){dialog.close();return;}await changeEngine(picker.value);dialog.close();});
    document.body.append(dialog);dialog.addEventListener("close",()=>dialog.remove(),{once:true});dialog.showModal();picker.focus();
  }
  function renderEngine() {
    const button=$("#session-engine");if(!button)return;
    const s=state.session,i=state.instances.find(i=>i.id===(s?.instanceId||instance()?.id));
    newButton.disabled=!ws()||!available(ws()?.id).length||state.loadingSession||state.sending;
    button.textContent=i||s?label(i,s?.runtimeKind):rdText("尚无可用 Codex 环境");
    button.disabled=!!s||!ws()||!available(ws()?.id).length||state.sending||state.loadingSession;
    button.classList.toggle("locked",!!s);button.title=s?rdText("此会话的环境已固定；新建会话可选择其他环境。"):rdText("选择此会话的运行环境");
    button.setAttribute("aria-label",button.title+" · "+button.textContent);button.onclick=enginePicker;
    $("#prompt").placeholder=!s?(i?.agentRuntime?.kind==="kun"?rdText("给 Kun 一个任务…"):rdText("给 Codex 一个任务…")):$("#prompt").placeholder;
    if(!ws()||!i)$("#send").disabled=true;
  }
  function renderTree() {
    const host=$("#session-list");if(!host)return;
    const selected=ws()?.id,query=$("#session-search").value.trim().toLowerCase(),archived=$("#session-scope").value==="archived";
    const rows=ordered(state.sessions.filter(s=>s.runtimeKind!=="kun"&&!!s.archived===archived));
    const matching=s=>!query||[s.title,label(state.instances.find(i=>i.id===s.instanceId),s.runtimeKind)].join(" ").toLowerCase().includes(query);
    const scrollTop=host.scrollTop;
    host.replaceChildren();let total=0;
    for(const work of state.workspaces.filter(ordinary).sort((a,b)=>(a.name||a.id).localeCompare(b.name||b.id)||a.id.localeCompare(b.id))){
      const sessions=rows.filter(s=>s.workspaceId===work.id&&matching(s));
      if(query&&!sessions.length&&!work.name.toLowerCase().includes(query))continue;
      total+=sessions.length;
      const isOpen=expanded.has(work.id)||!!query;
      const group=el("section",{class:"workspace-group"+(work.id===selected?" selected":""),"data-workspace-id":work.id});
      const toggle=button("",async()=>{
        if(state.sending||state.loadingSession)return;
        if(work.id!==ws()?.id){expanded.add(work.id);await open(work.id,{refresh:false});}
        else {if(expanded.has(work.id))expanded.delete(work.id);else expanded.add(work.id);renderTree();}
      },"workspace-heading");
      toggle.setAttribute("aria-expanded",String(isOpen));toggle.setAttribute("aria-controls","workspace-sessions-"+work.id);
      toggle.append(el("span",{class:"workspace-chevron","aria-hidden":"true"},isOpen?"▾":"▸"),el("span",{class:"workspace-title"},work.name),el("span",{class:"workspace-count"},sessions.length));
      const children=el("div",{class:"workspace-sessions",id:"workspace-sessions-"+work.id,hidden:!isOpen});
      if(work.id===selected){newButton.disabled=!available(work.id).length||state.loadingSession||state.sending;children.append(newButton);}
      for(const s of sessions){
        const item=button("",()=>selectSession(s.id),"session"+(state.session?.id===s.id?" active":""));item.dataset.sessionId=s.id;
        item.append(el("span",{class:"session-name"},(s.pinned?"◆ ":active(s.status)?"• ":"")+s.title),el("span",{class:"session-engine-label"},label(state.instances.find(i=>i.id===s.instanceId),s.runtimeKind)));
        item.title=s.title+" · "+label(state.instances.find(i=>i.id===s.instanceId),s.runtimeKind);children.append(item);
      }
      if(!sessions.length)children.append(el("p",{class:"empty"},query?rdText("没有匹配的会话"):archived?rdText("没有已归档会话"):rdText("从一个新会话开始")));
      group.append(toggle,children);host.append(group);
    }
    if(!newButton.isConnected)$("#conversation-control-store").append(newButton);
    $("#session-count").textContent=String(total);
    host.scrollTop=scrollTop;
    if(!host.children.length)host.append(el("p",{class:"empty"},query?rdText("没有匹配的会话"):rdText("先创建一个工作区，再开始会话。")));
  }
  function createWorkspace() {
    if(state.sending||state.loadingSession)return;
    const pendingKey="rundesk-conversation-workspace-create";
    let body;try{body=JSON.parse(sessionStorage.getItem(pendingKey)||"null");}catch{}
    const dialog=el("dialog",{class:"application-register"}),name=el("input",{id:"conversation-workspace-name",required:true,maxlength:160,value:body?.name||"",disabled:!!body,placeholder:rdText("例如：产品视频")}),path=el("input",{id:"conversation-workspace-path",value:body?.path||"",disabled:!!body,placeholder:rdText("留空自动创建")}),error=el("p",{class:"error",role:"alert"}),submit=el("button",{type:"submit",class:"primary"},rdText("创建工作区")),close=button(rdText("关闭"),()=>dialog.close(),"quiet");
    const form=el("form",{},el("div",{class:"dialog-head"},el("h2",{},rdText("新建工作区")),close),el("label",{},rdText("工作区名称"),name),el("p",{class:"help"},rdText("一个工作区保存同一件事的文件，可以包含多个使用不同 Codex 环境的会话。")),el("details",{},el("summary",{},rdText("使用已有目录（可选）")),el("label",{},rdText("服务器上的绝对路径"),path)),error,submit);
    let busy=false;const selection=state.selection;
    if(body)error.textContent=rdText("上次创建结果尚未确认；重试会继续同一次创建，不会重复添加。");
    form.onsubmit=async e=>{e.preventDefault();if(busy)return;busy=true;submit.disabled=close.disabled=name.disabled=path.disabled=true;error.textContent="";body||={name:name.value.trim(),path:path.value.trim()};sessionStorage.setItem(pendingKey,JSON.stringify(body));
      try {const work=await api("/workspaces",{method:"POST",body});sessionStorage.removeItem(pendingKey);dialog.close();if(selection===state.selection)await open(work.id,{fresh:true});else toast(rdText("工作区已创建，可在会话页查看。"));}
      catch(e){error.textContent=e.message;if(e.status>=400&&e.status<500&&!["request_in_progress","request_unconfirmed"].includes(e.code)){body=null;sessionStorage.removeItem(pendingKey);name.disabled=path.disabled=false;}}
      finally{busy=false;submit.disabled=close.disabled=false;}
    };
    dialog.addEventListener("cancel",e=>{if(busy)e.preventDefault();});dialog.append(form);document.body.append(dialog);dialog.addEventListener("close",()=>dialog.remove(),{once:true});dialog.showModal();name.focus();
  }
  return {open,available,source,owner,remember,renderTree,renderEngine,changeEngine,createWorkspace,label};
})();
