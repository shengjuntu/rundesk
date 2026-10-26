"use strict";
function applicationBindings(a){return [{appId:a.appId,name:a.name,instanceId:a.instanceId,workspaceId:a.workspaceId,presetId:""},...(a.runtimePresets||[]).map(p=>({appId:a.appId,name:a.name+" · "+p.name,instanceId:p.instanceId,workspaceId:p.workspaceId,presetId:p.id,presetName:p.name,enabled:p.enabled}))];}
function applicationForInstance(id){return state.applications.find(a=>applicationBindings(a).some(b=>b.instanceId===id));}
function currentApplication(){return applicationForInstance(instance()?.id);}
function currentApplicationBinding(){const a=currentApplication();return a?applicationBindings(a).find(b=>b.instanceId===instance()?.id&&(!b.presetId||b.workspaceId===ws()?.id)):null;}
function applicationBindingRoute(a){return "#application/"+encodeURIComponent(a.appId)+(a.presetId?"/preset/"+encodeURIComponent(a.presetId):"");}
function contextTitle(){return instance()?.name||rdText("实例");}
function sessionInContext(s){return s.instanceId===instance()?.id&&(s.instanceId!=="default"||(state.taskAppID?s.source?.appId===state.taskAppID:s.source?.kind!=="application"));}
function draftKey(){return instance()?.id+":"+ws()?.id+":"+(state.session?.id||"new");}
function stashProductDraft(){if(!state.draftContext||state.view!=="conversation")return;state.drafts.set(state.draftContext,{text:$("#prompt").value,uploads:[...state.uploads],skills:[...state.chosenSkills]});}
function restoreProductDraft(){state.draftContext=draftKey();const d=state.drafts.get(state.draftContext);if(d){$("#prompt").value=d.text;state.uploads=[...d.uploads];state.chosenSkills=[...d.skills];renderAttachments();}}
function updateProductNavigation(){
 const view=state.view,conversation=view==="conversation",apps=["applications","application","users"].includes(view);
 for(const [id,on] of [["nav-instances",["instances","instance","capability"].includes(view)],["nav-users",view==="users"],["nav-build-center",view==="build-center"],["nav-assistant",conversation],["nav-applications",["applications","application"].includes(view)],["nav-tasks",view==="tasks"]]){
  const node=$("#"+id);if(!node)continue;node.classList.toggle("selected",on);if(on)node.setAttribute("aria-current","page");else node.removeAttribute("aria-current");
 }
 $("#application-count").textContent=state.applications.length||"";
 $("#page-label").textContent=({instances:rdText("实例"),"build-center":rdText("构建中心"),users:rdText("用户与授权"),tasks:rdText("任务"),applications:rdText("应用接入")})[view]||contextTitle();
 $("#history-label").textContent=contextTitle();
 $("#settings-button").textContent=rdText("实例设置");$("#settings-button").classList.toggle("hidden",!["conversation","instance","capability","application"].includes(view)||!ws()||!!state.instanceResolving);
 $("#new-session").classList.toggle("hidden",!conversation);
 $("#back-applications").classList.toggle("hidden",!["conversation","instance","capability","application"].includes(view));
 $("#back-applications").textContent=apps?rdText("应用 /"):rdText("实例 /");
 $("#back-applications").setAttribute("aria-label",apps?rdText("返回应用"):rdText("返回实例"));
 const fixed=window.RunDeskInstances?.binding(instance()?.id)?.presetId;
 $("#workspace").disabled=!!fixed;$("#add-workspace").disabled=!!fixed;
 for(const q of ["#trace-button","#debug-button","#workspace-name",".breadcrumb-divider",".sidebar>.workspace",".instance-context",".session-filters","#session-list","#history-label","#sidebar-toggle","#mode"])$(q)?.classList.toggle("hidden",!conversation);
}
function setProductPage(view){
 state.view=view;document.body.dataset.view=view;if(!["instances","instance"].includes(view))state.instanceResolving=false;
 for(const [selector,shown] of [["#tasks-page",view==="tasks"],[".main-row",view==="conversation"],["#instances-page",["instances","instance"].includes(view)],["#applications-page",["applications","application","users"].includes(view)],["#capability-page",view==="capability"],["#build-center-page",view==="build-center"]])$(selector)?.classList.toggle("hidden",!shown);
 updateProductNavigation();closeConversationSidebar();
}
async function refreshApplicationCatalog(){const selection=state.selection;const [a,i,s]=await Promise.all([api("/applications"),api("/instances"),api("/sessions")]);if(selection!==state.selection)return;const id=instance()?.id||"default";state.applications=a;state.instances=i;state.sessions=s;renderInstanceOptions(id);}
async function enterAssistant(){resetConversation();renderInstanceOptions("default");const wid=localStorage.getItem("rundesk-assistant-workspace");if(state.workspaces.some(w=>w.id===wid))$("#workspace").value=wid;setProductPage("conversation");updateWorkspaceLabel();history.replaceState(null,"","#assistant");const sid=localStorage.getItem("rundesk-session"),last=state.sessions.find(s=>s.id===sid&&s.instanceId==="default"&&s.source?.kind!=="application");if(state.drafts.has(draftKey())){restoreProductDraft();renderStatus();}else if(last)await selectSession(last.id);else await selectFirst();renderSessions();}
async function showApplications(){resetConversation();setProductPage("applications");history.replaceState(null,"","#applications");setLoading($("#applications-page"));await refreshApplicationCatalog();if(state.view==="applications")renderApplications();}
function applicationMark(name){return el("div",{class:"application-mark","aria-hidden":"true"},name.trim().slice(0,1).toUpperCase());}
function applicationLink(a){if(!a.entryUrl)return null;try{const u=new URL(a.entryUrl);if(!["http:","https:"].includes(u.protocol)||u.username||u.password)return null;}catch{return null;}return el("a",{class:"app-open",href:a.entryUrl,target:"_blank",rel:"noopener noreferrer"},rdText("打开应用 ↗"));}
function renderApplications(){
 const grid=el("div",{class:"application-grid"});for(const a of state.applications)grid.append(el("article",{class:"application-card","data-app-id":a.appId},el("div",{class:"app-card-top"},applicationMark(a.name),el("span",{class:"app-state"+(a.activeCount?" busy":"")},a.activeCount?rdFormat("${0} 项运行中",a.activeCount):a.sessionCount?rdText("任务已同步"):rdText("已登记"))),el("h2",{},a.name),el("p",{class:"app-description"},a.description||rdText("在专用应用中操作，在这里查看任务与管理能力。")),el("div",{class:"app-metadata"},rdFormat("${0} 条任务记录",a.sessionCount),el("span",{},a.lastActivity?new Date(a.lastActivity).toLocaleDateString():rdText("等待首次任务"))),el("div",{class:"app-card-actions"},button(rdText("查看任务"),()=>showApplication(a.appId),"primary"),button(rdText("设置"),async()=>{await showApplication(a.appId);await openSettings();},"quiet"),applicationLink(a))));
 if(!state.applications.length)grid.append(el("div",{class:"application-empty"},applicationMark("A"),el("h2",{},rdText("把专用应用接进来")),el("p",{},rdText("新闻研究、视频制作等应用接入后，会在这里集中展示。通用助手可直接使用。")),el("p",{class:"help"},rdText("请在新闻研究或视频应用中连接 RunDesk，应用会自动出现在这里。"))));
 $("#applications-page").replaceChildren(el("div",{class:"page-intro"},el("div",{},el("p",{class:"eyebrow"},"YOUR APPLICATIONS"),el("h1",{},rdText("应用接入")),el("p",{},rdText("应用连接已配置的实例，提交任务并获取制品。模型和能力在实例中管理。"))),el("div",{class:"actions"},button(rdText("刷新"),showApplications,"quiet"))),grid,applicationAdvanced());updateProductNavigation();
}
function taskRow(s){const b=button("",()=>selectSession(s.id),"application-task");b.append(el("div",{},el("strong",{},s.title),el("small",{},(state.workspaces.find(w=>w.id===s.workspaceId)?.name||rdText("项目"))+" · "+new Date(s.updated||s.created).toLocaleString())),el("span",{class:"badge"},(s.archived?rdText("已归档 · "):"")+(statusLabel[s.status]||s.status)));return b;}
function applicationAdvanced(){
 const assigned=new Set(state.applications.flatMap(a=>applicationBindings(a).map(b=>b.instanceId))),orphan=state.instances.filter(i=>i.id!=="default"&&!assigned.has(i.id)),legacy=state.sessions.filter(s=>s.instanceId==="default"&&s.source?.kind==="application");
 const d=el("details",{id:"application-advanced",class:"advanced-applications"},el("summary",{},rdText("接入说明与历史配置")),el("p",{class:"help"},rdText("旧版应用提交带来源的首条任务后自动归类。没有任务或归属有冲突的配置保留在这里，历史数据不会移动。")),el("details",{class:"integration-notes"},el("summary",{},rdText("开发者接入")),el("p",{},rdText("应用调用 POST /api/v1/applications/{appId}/connect 自动注册并获得绑定。管理员在这里优化配置，无须手动创建实例。")),el("a",{href:"/api/v1/openapi.json",target:"_blank",rel:"noopener"},rdText("查看 API 定义"))));
 for(const i of orphan)d.append(el("div",{class:"legacy-application"},el("div",{},el("strong",{},i.name),el("p",{class:"help"},rdFormat("${0} 条历史对话 · 尚未归类",state.sessions.filter(s=>s.instanceId===i.id).length))),button(rdText("查看"),()=>showLegacyApplication(i.id)),button(rdText("登记为应用"),()=>registerApplicationDialog(i.id))));
 if(legacy.length)d.append(el("h3",{},rdText("默认配置中的历史应用任务")),el("p",{class:"help"},rdText("这些任务仍沿用通用助手配置。新应用应使用专用配置。")),...legacy.map(taskRow));
 if(!orphan.length&&!legacy.length)d.append(el("p",{class:"help"},rdText("没有待归类的历史配置。")));return d;
}
function applicationConnection(a){const info={apiBase:location.origin+"/api/v1",appId:a.appId,instanceId:a.instanceId,workspaceId:a.workspaceId||ws()?.id,...(a.presetId?{presetId:a.presetId}:{})};return el("details",{class:"application-connection"},el("summary",{},rdText("接入信息")),el("p",{class:"help"},rdText("在应用侧使用这些编号连接已有配置。应用任务仍需明确传入工作区；默认查看项目仅用于此管理界面。")),el("pre",{},json(info)),button(rdText("复制接入信息"),async()=>{await navigator.clipboard.writeText(json(info));toast(rdText("已复制接入信息"));},"quiet"));}
async function showApplication(id,presetId=""){
 const a=state.applications.find(a=>a.appId===id);if(!a)throw Error(rdText("应用不存在，请刷新列表"));const bindings=applicationBindings(a);if(presetId&&presetId!=="__default__"&&!bindings.some(b=>b.presetId===presetId))throw Error(rdText("制作环境预设不存在，请刷新列表"));const binding=bindings.find(b=>b.presetId===presetId)||bindings[0],ids=new Set(bindings.map(b=>b.instanceId));resetConversation();const selection=state.selection;renderInstanceOptions(binding.instanceId);const ss=state.sessions.filter(s=>ids.has(s.instanceId)&&(!presetId||s.instanceId===binding.instanceId));const wid=binding.workspaceId||ss[0]?.workspaceId||state.workspaces[0]?.id;if(wid)$("#workspace").value=wid;setProductPage("application");updateWorkspaceLabel();history.replaceState(null,"",applicationBindingRoute(binding));
 $("#applications-page").replaceChildren(el("div",{class:"page-intro application-heading"},el("div",{},applicationMark(a.name),el("h1",{},binding.name),el("p",{},a.description||rdText("专用助手与任务记录"))),el("div",{class:"actions"},applicationLink(a),!binding.presetId?button(rdText("选用已有能力"),()=>chooseApplicationCapabilities(a),"quiet"):null,button(rdText("API 凭据"),()=>RunDeskKeys.open(a),"quiet"),button(rdText("编辑应用信息"),()=>registerApplicationDialog(a.instanceId,a),"quiet"))),el("div",{class:"application-settings-grid"},...[ [rdText("运行环境"),rdText("镜像、项目隔离与容器状态"),"environments"],[rdText("模型与认证"),rdText("模型、登录与运行权限"),"instances"],[rdText("原生插件"),rdText("原生资源与组件归属（只读）"),"plugins"],[rdText("技能 Skills"),rdText("完整目录与工作方法"),"skills"],[rdText("工具 MCP"),rdText("业务接口与外部服务"),"mcp"] ].map(([name,help,tab])=>{const b=button("",()=>tab==="environments"?RunDeskEnvironments.open({...a,...binding}):openSettings(tab),"configuration-tile");b.append(el("strong",{},name),el("span",{},help));return b;})),el("section",{class:"application-image-summary",id:"application-image-summary","aria-label":rdText("应用镜像")},rdText("正在读取镜像信息…")),applicationConnection({...a,...binding}),el("div",{class:"task-section-heading"},el("h2",{},rdText("任务记录")),el("span",{class:"muted"},rdFormat("${0} 条",ss.length))),el("div",{class:"application-tasks"},...(ss.length?ss.map(taskRow):[el("div",{class:"application-empty compact"},el("h3",{},rdText("还没有任务")),el("p",{},rdText("从应用发起业务操作后，运行记录会出现在这里。")))])));
 const summary=$("#application-image-summary");if(summary){if(binding.presetId)summary.replaceChildren(el("strong",{},binding.presetName),el("p",{},state.instances.find(i=>i.id===binding.instanceId)?.execution?.image||rdText("镜像待配置")));else await RunDeskImages.overview(a,summary);}
 if(selection!==state.selection)return;const page=$("#applications-page"),grid=page.querySelector('.application-settings-grid'),connection=page.querySelector('.application-connection');
 const config=el('details',{class:'application-config'},el('summary',{},rdText('配置与运行环境')));
 if(grid)config.append(grid);if(summary)config.append(summary);if(connection)config.append(connection);page.append(config);const taskFilter=el("select",{id:"application-preset-filter","aria-label":rdText("任务环境筛选")},el("option",{value:""},rdText("全部制作环境")),el("option",{value:"__default__"},rdText("应用默认环境")),...(a.runtimePresets||[]).map(p=>el("option",{value:p.id},p.name+(p.enabled?"":rdText(" · 已禁用")))));taskFilter.value=presetId;taskFilter.onchange=()=>safe(()=>showApplication(a.appId,taskFilter.value));page.querySelector(".task-section-heading").append(taskFilter);await RunDeskRuntimePresets.render(a,config);if(selection!==state.selection)return;
 page.querySelector('.application-heading').after(el('div',{class:'application-health'},el('span',{},rdText('运行条件尚未检查')),button(rdText('检查运行条件'),()=>RunDeskSetup.open({...a,...binding}),'quiet')));
}
async function showLegacyApplication(iid,wid){return RunDeskInstances.show(iid,wid);}
async function registerApplicationDialog(iid,existing=null){
 const fresh=!iid,i=state.instances.find(x=>x.id===iid)||{name:"",description:""},d=el("dialog",{class:"application-register"});const id=el("input",{id:"register-app-id",value:existing?.appId||"",placeholder:rdText("例如 news2douyin"),required:true,pattern:"[A-Za-z0-9][A-Za-z0-9_.:-]{0,79}",disabled:!!existing});const name=el("input",{id:"register-app-name",value:existing?.name||i.name,required:true,maxlength:160}),description=el("textarea",{rows:2,maxlength:4096},existing?.description||i.description),url=el("input",{type:"url",value:existing?.entryUrl||"",placeholder:"http://127.0.0.1:18080"}),work=el("select",{},...state.workspaces.map(w=>el("option",{value:w.id},w.name)));work.value=existing?.workspaceId||ws()?.id;let createdWorkspaceID="";if(fresh){work.prepend(el("option",{value:"__new__"},rdText("新建独立项目（推荐）")));work.value="__new__";}
 const runtime=el("select",{id:"register-runtime"},el("option",{value:"docker"},rdText("Docker 应用环境")),el("option",{value:"local"},rdText("本机 Codex（兼容已有应用）"))),image=el("input",{id:"register-image",placeholder:rdText("例如 rundesk-news:1.0")}),runtimeFields=el("div",{},el("label",{},rdText("运行方式"),runtime),el("label",{},rdText("应用镜像"),image));image.required=fresh;runtime.onchange=()=>{image.required=fresh&&runtime.value==="docker";image.disabled=runtime.value!=="docker";};
 const error=el("p",{class:"error",role:"status"}),submit=el("button",{type:"submit",class:"primary"},existing?rdText("保存应用信息"):fresh?rdText("添加应用"):rdText("登记应用"));const form=el("form",{},el("div",{class:"dialog-head"},el("h2",{},existing?rdText("应用信息"):fresh?rdText("添加应用"):rdText("登记已有应用")),button(rdText("关闭"),()=>d.close(),"quiet")),el("p",{class:"help"},fresh?rdText("创建应用专用配置。应用软件需单独运行，并通过 API 连接这里的配置。"):rdText("复用现有配置与会话，只增加应用归属信息。")),el("label",{},rdText("应用编号"),id),el("label",{},rdText("显示名称"),name),el("label",{},rdText("用途说明"),description),el("label",{},rdText("默认查看项目"),work),el("label",{},rdText("应用入口（可选）"),url),fresh?runtimeFields:null,error,submit);
 form.onsubmit=async event=>{event.preventDefault();submit.disabled=true;try{if(!iid){const created=await api("/instances",{method:"POST",body:{name:name.value,description:description.value}});iid=created.id;}if(fresh){const current=(await api("/instances")).find(x=>x.id===iid);await api(`/instances/${iid}/execution`,{method:"PUT",body:{revision:current.revision,spec:runtime.value==="docker"?{mode:"docker",image:image.value}:{mode:"local"}}});}if(work.value==="__new__"&&!createdWorkspaceID)createdWorkspaceID=(await api("/workspaces",{method:"POST",body:{name:name.value}})).id;await api("/applications/"+encodeURIComponent(id.value),{method:"PUT",body:{name:name.value,description:description.value,instanceId:iid,workspaceId:work.value==="__new__"?createdWorkspaceID:work.value,entryUrl:url.value,...(existing?{revision:existing.revision}:{})}});state.workspaces=await api("/workspaces");const previousWorkspace=ws()?.id;$("#workspace").replaceChildren(...state.workspaces.map(w=>el("option",{value:w.id},w.name)));if(previousWorkspace)$("#workspace").value=previousWorkspace;await refreshApplicationCatalog();if(d.open){d.close();await showApplication(id.value);}}catch(e){error.textContent=e.message;}finally{submit.disabled=false;}};d.append(form);document.body.append(d);d.addEventListener("close",()=>d.remove(),{once:true});d.showModal();
}
async function renderAssistantSettings(target){
 const requested=instance(),work=ws(),selection=state.selection;
 const resolved=await RunDeskEnvironments.effective(requested,work);if(!target.isConnected||selection!==state.selection||instance()?.id!==requested.id||ws()?.id!==work.id)return;
 const current=resolved.instance,cp=configPath(work.id,current.id),model=el("input",{id:"assistant-model",type:"text",value:current.defaultModel,list:"models",placeholder:rdText("留空沿用 Codex 配置")}),permissions=permissionEditor(current),account=el("pre",{id:"assistant-account"},rdText("点击读取登录状态，检查这个助手或应用使用的账户。")),quote=v=>"'"+v.replaceAll("'","'\\''")+"'",ps=v=>"'"+v.replaceAll("'","''")+"'";
 target.replaceChildren(el("section",{class:"card model-settings"},el("h3",{},rdText("默认模型")),el("p",{},rdText("用于新建对话或应用任务。已有会话保留原来选择的模型。")),el("label",{for:"assistant-model"},rdText("模型名称")),model,button(rdText("读取可用模型"),async()=>{const raw=await api(cp("/models"));$("#models").replaceChildren(...(raw.data||[]).map(m=>el("option",{value:m.id||m.model},m.displayName||m.id||m.model)));toast(rdText("模型列表已更新"));},"quiet"),el("details",{class:"advanced-permissions"},el("summary",{},rdText("高级运行权限")),permissions.node),button(rdText("保存配置"),async()=>{await api("/instances/"+current.id,{method:"PATCH",body:{defaultModel:model.value,permissions:permissions.value(),revision:current.revision}});const selected=instance()?.id;state.instances=await api("/instances");renderInstanceOptions(selected);updateWorkspaceLabel();renderStatus();toast(rdText("已保存：默认模型用于新会话，权限在下一轮生效"));await renderSettings();},"primary")),el("section",{class:"card authentication-settings"},el("h3",{},rdText("账户与认证")),el("p",{},resolved.environment?rdText("当前项目环境使用独立认证，下方提供容器登录命令。"):rdText("首次接入需在运行 RunDesk 的电脑上完成 Codex 登录。")),el("div",{class:"actions"},button(rdText("读取登录状态"),async()=>account.textContent=json(await api(cp("/account")))),button(rdText("重新加载"),async()=>{const r=await api("/instances/"+current.id+"/reload",{method:"POST"});toast(rdFormat("已重载 ${0} 个空闲连接，保留 ${1} 个运行中连接",r.closedConnections,r.busyConnections));account.textContent=json(await api(cp("/account")));},"quiet")),account,RunDeskEnvironments.loginDetails(current,resolved.environment)));
}
function contextRoute(){
 if(state.session&&state.view==="conversation")return "#session/"+encodeURIComponent(state.session.id);
 if(["instances","instance","capability"].includes(state.view)||location.hash.startsWith("#instance/"))return location.hash||"#instances";
 if(state.view==="applications")return "#applications";
 if(state.view==="application"&&currentApplicationBinding())return applicationBindingRoute(currentApplicationBinding());
 return window.RunDeskInstances?RunDeskInstances.route(instance()?.id||"default",ws()?.id):"#assistant";
}
async function showCapabilityPage(tab){
 if(state.view!=="capability"){stashProductDraft();state.capabilityReturn={view:state.view,route:contextRoute()};}
 if($("#settings").open)$("#settings").close();state.settingsTab=tab;setProductPage("capability");
 const applicationContext=state.capabilityReturn?.view==="application"&&currentApplicationBinding();
 const owner=applicationContext?applicationBindingRoute(currentApplicationBinding()):"#instance/"+encodeURIComponent(instance().id);
 history.replaceState(null,"",owner+"/settings/"+tab+(!applicationContext?"?"+new URLSearchParams({workspace:ws().id}):""));
 const page=$("#capability-page");page.replaceChildren(el("div",{class:"page-intro capability-heading"},el("div",{},button(rdText("← 返回"),closeSettings,"quiet back-link"),el("p",{class:"eyebrow"},tab==="plugins"?"NATIVE PLUGINS":tab==="skills"?"SKILL LIBRARY":"CONNECTED TOOLS"),el("h1",{},tab==="plugins"?rdText("原生插件"):tab==="skills"?rdText("技能 Skills"):rdText("工具 MCP")),el("p",{class:"capability-owner"},rdFormat("配置对象：${0} · 当前项目：${1}",instance()?.id==="default"&&state.taskAppID?rdText("通用助手（历史任务共享；保存会影响通用助手）"):contextTitle(),ws()?.name||""))),el("nav",{class:"actions","aria-label":rdText("能力分类")},...[["plugins","插件"],["skills","Skills"],["mcp","MCP"]].map(([key,label])=>button(rdText(label),()=>showCapabilityPage(key),key===tab?"selected":"quiet")))),el("div",{id:"capability-content"}));await renderSettings();
}
function closeSettings(){if($("#settings").open)$("#settings").close();if(state.view==="capability"){const back=state.capabilityReturn||{view:currentApplication()?"application":"conversation",route:currentApplication()?applicationBindingRoute(currentApplicationBinding()):"#assistant"};setProductPage(back.view);history.replaceState(null,"",back.route);}}
function skillCanBeSelected(){return state.view==="conversation"||state.view==="capability"&&state.capabilityReturn?.view==="conversation";}
function compactConfigurationForms(target,kind){for(const card of [...target.children]){const title=card.querySelector(":scope>h3")?.textContent;if(!title||!(kind==="skills"&&title.includes(rdText("编辑 SKILL.md"))||kind==="mcp"&&title===rdText("添加 / 编辑 MCP")))continue;const d=el("details",{class:"card configuration-editor"},el("summary",{},kind==="skills"?rdText("编辑 SKILL.md / 创建简单技能"):rdText("添加 / 编辑 MCP 服务")));card.before(d);card.classList.remove("card");card.querySelector(":scope>h3").remove();d.append(card);}}
async function restoreProductRoute(){
 const [route,query=""]=location.hash.slice(1).split("?");const params=new URLSearchParams(query),parts=route.split("/");let id;try{id=decodeURIComponent(parts[1]||"");}catch{return false;}
 if(parts[0]==="instances"){await RunDeskInstances.open();return true;}
 if(parts[0]==="instance"){
  const wid=params.get("workspace")||undefined;
  if(parts[2]==="conversation")await RunDeskInstances.conversation(id,wid);
  else {const context=await RunDeskInstances.show(id,wid);if(!context?.workspace)return true;
   if(parts[2]==="settings"&&["plugins","skills","mcp"].includes(parts[3]))await showCapabilityPage(parts[3]);
   if(parts[2]==="environments")await RunDeskEnvironments.open({...RunDeskInstances.binding(id),instanceId:id,name:instance().name,workspaceId:ws().id,instanceContext:true});
  }return true;
 }
 if(parts[0]==="assistant"&&!parts[1]){await enterAssistant();return true;}
 if(parts[0]==="build-center"){let selected="";try{selected=decodeURIComponent(parts[2]||"");}catch{return false;}await RunDeskBuildCenter.open(parts[1]||"recipes",selected,parts[3]==="test");return true;}
 if(parts[0]==="tasks"){await RunDeskTasks.open();return true;}
 if(parts[0]==="trace"){const sid=sessionStorage.getItem("rundesk-trace-session")||localStorage.getItem("rundesk-session");if(state.sessions.some(s=>s.id===sid)){await selectSession(sid);window.RunDeskTraceUI?.open();return true;}}
 if(parts[0]==="assistant"&&parts[1]==="settings"&&["plugins","skills","mcp"].includes(parts[2])){await enterAssistant();await showCapabilityPage(parts[2]);return true;}
 if(parts[0]==="users"){await RunDeskUsers.open();return true;}if(parts[0]==="applications"){await showApplications();return true;}
 if(parts[0]==="application"&&state.applications.some(a=>a.appId===id)){let presetId="";if(parts[2]==="preset"){try{presetId=decodeURIComponent(parts[3]||"");}catch{return false;}}await showApplication(id,presetId);const tail=parts[2]==="preset"?parts.slice(4):parts.slice(2),binding={...state.applications.find(a=>a.appId===id),...currentApplicationBinding()};if(tail[0]==="builds"){await RunDeskBuilds.open(state.applications.find(a=>a.appId===id));return true;}if(tail[0]==="images"){await RunDeskImages.open(state.applications.find(a=>a.appId===id));return true;}if(tail[0]==="environments"){await RunDeskEnvironments.open(binding);return true;}if(tail[0]==="settings"&&["plugins","skills","mcp"].includes(tail[1]))await showCapabilityPage(tail[1]);return true;}
 if(parts[0]==="legacy"&&state.instances.some(i=>i.id===id)){if(id==="default")await enterAssistant();else await showLegacyApplication(id);if(parts[2]==="settings"&&["plugins","skills","mcp"].includes(parts[3]))await showCapabilityPage(parts[3]);return true;}
 if(parts[0]==="session"&&state.sessions.some(s=>s.id===id)){await selectSession(id);return true;}return false;
}
$("#nav-instances").onclick=()=>safe(()=>RunDeskInstances.open());
$("#nav-assistant").onclick=()=>safe(()=>RunDeskInstances.conversation(instance()?.id||"default"));
$("#nav-applications").onclick=()=>safe(showApplications);
$("#back-applications").onclick=()=>safe(()=>state.view==="application"?showApplications():RunDeskInstances.open());
$("#assistant-settings").onclick=()=>safe(()=>openSettings());
window.addEventListener("hashchange",()=>{if(state.booted)safe(restoreProductRoute);});

async function chooseApplicationCapabilities(a){
 const data=await api('/applications/'+encodeURIComponent(a.appId)+'/available-capabilities');
 const d=el('dialog',{class:'application-register'}),form=el('form'),choices=[];
 form.append(el('h2',{},rdText('选用已有能力')),el('p',{class:'help'},rdText('从通用助手的已安装目录选用，关闭的能力也可选择。只在此应用启用；已有同名配置保留。认证与容器依赖仍需在目标环境可用。')));
 for(const [kind,rows]of [['skills',data.skills.map(s=>s.name)],['mcp',data.mcp]]){form.append(el('h3',{},kind==='skills'?'Skills':'MCP'));for(const name of rows){const input=el('input',{type:'checkbox'});form.append(el('label',{},input,name));choices.push({kind,name,input})}}
 const error=el('p',{role:'status'}),submit=el('button',{type:'submit'},rdText('应用所选能力'));form.append(error,el('div',{class:'actions'},button(rdText('取消'),()=>d.close(),'quiet'),submit));
 form.onsubmit=async e=>{e.preventDefault();submit.disabled=true;try{await api('/applications/'+encodeURIComponent(a.appId)+'/capabilities',{method:'POST',body:{skills:choices.filter(x=>x.kind==='skills'&&x.input.checked).map(x=>x.name),mcp:choices.filter(x=>x.kind==='mcp'&&x.input.checked).map(x=>x.name)}});d.close();toast(rdText('已为此应用启用所选能力'))}catch(e){error.textContent=e.message}finally{submit.disabled=false}};
 d.append(form);document.body.append(d);d.addEventListener('close',()=>d.remove(),{once:true});d.showModal();
}

// Runtime presets are dedicated instance/workspace identities. This panel only
// manages metadata and configuration; it never starts/stops existing containers.
window.RunDeskRuntimePresets=(()=>{
 const endpoint=a=>"/applications/"+encodeURIComponent(a.appId)+"/runtime-presets";
 const draftKey=a=>"rundesk-runtime-preset-draft:"+a.appId;
 function savedDraft(a){try{return JSON.parse(sessionStorage.getItem(draftKey(a))||"null");}catch{return null;}}
 function remember(a,value){try{if(value)sessionStorage.setItem(draftKey(a),JSON.stringify(value));else sessionStorage.removeItem(draftKey(a));}catch{}}
 async function load(a){const rows=await api(endpoint(a));a.runtimePresets=rows;const stored=state.applications.find(v=>v.appId===a.appId);if(stored)stored.runtimePresets=rows;return rows;}
 function binding(a,p){return {...a,appId:a.appId,name:a.name+" · "+p.name,presetId:p.id,presetName:p.name,instanceId:p.instanceId,workspaceId:p.workspaceId};}
 async function select(a,p){resetConversation();state.workspaces=await api("/workspaces");$("#workspace").replaceChildren(...state.workspaces.map(w=>el("option",{value:w.id},w.name)));renderInstanceOptions(p.instanceId);$("#workspace").value=p.workspaceId;updateWorkspaceLabel();return binding(a,p);}
 function ownership(){const all=state.applications.flatMap(applicationBindings);return {instances:new Set(all.map(v=>v.instanceId)),workspaces:new Set(all.map(v=>v.workspaceId).filter(Boolean))};}
 async function persist(a,id,value){
  try{return await api(endpoint(a)+"/"+encodeURIComponent(id),{method:"PUT",body:value});}
  catch(error){
   // PUT may have committed before its response was lost. Reconcile the exact
   // desired binding and fields; never create another instance to retry it.
   try{const found=(await load(a)).find(p=>p.id===id);if(found&&["name","description","instanceId","workspaceId","enabled"].every(k=>(found[k]??"")===(value[k]??"")))return found;}catch{}
   throw error;
  }
 }
 async function render(a,target){
  const section=el("section",{class:"runtime-presets",id:"runtime-presets","aria-label":rdText("制作环境预设")});target.append(section);
  async function refresh(){
   section.replaceChildren(el("h2",{},rdText("制作环境预设")),el("p",{class:"help"},rdText("每个预设使用独立的 Codex Docker 实例与工作目录。分别配置镜像、模型、Skills 和 MCP，任务通过预设编号选择。")));
   try{
    const rows=await load(a),list=el("div",{class:"runtime-preset-list"});
    section.append(el("div",{class:"actions"},button(savedDraft(a)?rdText("继续创建未完成预设"):rdText("添加制作环境预设"),()=>edit(a,null,refresh),"primary"),button(rdText("刷新预设"),refresh,"quiet")));
    for(const p of rows){
     const i=state.instances.find(v=>v.id===p.instanceId),tasks=state.sessions.filter(s=>s.instanceId===p.instanceId&&s.workspaceId===p.workspaceId);
     list.append(el("article",{class:"card runtime-preset-card","data-preset-id":p.id},el("div",{class:"environment-card-heading"},el("h3",{},p.name),el("span",{class:"badge"},p.enabled?rdText("已启用"):rdText("已禁用 · 保留历史"))),el("p",{},p.description||""),el("p",{class:"help"},p.id+" · "+(i?.execution?.image||rdText("镜像待配置"))+" · "+rdFormat("${0} 条任务记录",tasks.length)),el("div",{class:"actions"},button(rdText("查看此预设任务"),()=>showApplication(a.appId,p.id),"quiet"),button(rdText("运行环境"),async()=>RunDeskEnvironments.open(await select(a,p)),"quiet"),...[[rdText("模型与认证"),"instances"],["Skills","skills"],["MCP","mcp"]].map(([label,tab])=>button(label,async()=>{await select(a,p);await openSettings(tab);},"quiet")),button(rdText("名称与启停"),()=>edit(a,p,refresh),"quiet"))));
    }
    if(!rows.length)list.append(el("p",{class:"help"},rdText("尚未配置预设，应用现有默认环境继续可用。")));
    section.append(list,el("p",{class:"help"},rdText("保存预设不会启动或重建容器。禁用预设阻止新任务，已有任务和历史记录保留。")));
   }catch(error){section.append(el("p",{class:"error",role:"status"},error.message),button(rdText("重试读取预设"),refresh,"quiet"));}
  }
  await refresh();return section;
 }
 async function edit(a,existing,refresh){
  await refreshApplicationCatalog();state.workspaces=await api("/workspaces");
  const occupied=ownership(),available=state.instances.filter(i=>i.id!=="default"&&!occupied.instances.has(i.id)&&i.execution?.mode==="docker"&&(!i.agentRuntime?.kind||i.agentRuntime.kind==="codex")),availableWorkspaces=state.workspaces.filter(w=>!occupied.workspaces.has(w.id)),saved=existing?null:savedDraft(a);
  const d=el("dialog",{class:"application-register runtime-preset-dialog"}),id=el("input",{id:"preset-id",required:true,pattern:"[A-Za-z0-9][A-Za-z0-9_.-]{0,127}",maxlength:128,value:existing?.id||saved?.id||"",disabled:!!existing,placeholder:"hyperframes"}),name=el("input",{id:"preset-name",required:true,maxlength:160,value:existing?.name||saved?.name||""}),description=el("textarea",{id:"preset-description",rows:2,maxlength:4096},existing?.description||saved?.description||""),enabled=el("input",{id:"preset-enabled",type:"checkbox",checked:existing?existing.enabled:saved?saved.enabled:true}),mode=el("select",{id:"preset-mode"},el("option",{value:"new"},rdText("新建独立环境")),el("option",{value:"existing",disabled:!available.length||!availableWorkspaces.length},rdText("绑定未占用的 Docker 环境"))),image=el("input",{id:"preset-image",placeholder:"rundesk-video:0.23.1-hyperframes",maxlength:255,value:saved?.image||""}),iid=el("select",{id:"preset-instance"},...available.map(i=>el("option",{value:i.id},i.name))),wid=el("select",{id:"preset-workspace"},...availableWorkspaces.map(w=>el("option",{value:w.id},w.name)));
  mode.value=saved?.mode||"new";if(saved?.instanceId)iid.value=saved.instanceId;if(saved?.workspaceId)wid.value=saved.workspaceId;
  const newFields=el("div",{},el("label",{},rdText("Docker 镜像"),image)),existingFields=el("div",{},el("label",{},rdText("专用实例"),iid),el("label",{},rdText("专用工作目录"),wid)),error=el("p",{class:"error",role:"status"}),submit=el("button",{type:"submit",class:"primary"},rdText("保存预设"));
  let pending=false,plan=saved;
  const updateMode=()=>{newFields.hidden=mode.value!=="new";existingFields.hidden=mode.value!=="existing";image.required=!existing&&mode.value==="new";};mode.onchange=updateMode;updateMode();
  const inputs=[id,name,description,enabled,mode,image,iid,wid];
  const freeze=()=>inputs.forEach(input=>input.disabled=true);
  if(plan)freeze();
  const close=button(rdText("关闭"),()=>{if(!pending)d.close();},"quiet"),form=el("form",{},el("div",{class:"dialog-head"},el("h2",{},existing?rdText("修改制作环境预设"):rdText("添加制作环境预设")),close),el("label",{},rdText("预设编号"),id),el("label",{},rdText("显示名称"),name),el("label",{},rdText("用途说明"),description),el("label",{class:"key-choice"},enabled,rdText("允许新任务选择此预设")),existing?el("p",{class:"help"},rdText("实例和工作目录在登记后固定；修改名称或启停不会迁移历史任务。")):el("div",{},el("label",{},rdText("环境来源"),mode),newFields,existingFields),el("p",{class:"help"},rdText("新环境会创建专用实例和目录；错误重试复用已创建的资源。镜像需在 RunDesk 主机可用。")),error,submit);
  d.addEventListener("cancel",event=>{if(pending)event.preventDefault();});
  form.onsubmit=async event=>{
   event.preventDefault();if(pending)return;pending=true;submit.disabled=true;close.disabled=true;error.textContent="";
   try{
    if(existing){await persist(a,existing.id,{name:name.value.trim(),description:description.value,enabled:enabled.checked,instanceId:existing.instanceId,workspaceId:existing.workspaceId,revision:existing.revision});}
    else{
     if(!plan){if((a.runtimePresets||[]).some(p=>p.id===id.value.trim()))throw Error(rdText("预设编号已存在，请使用新编号"));plan={id:id.value.trim(),name:name.value.trim(),description:description.value,enabled:enabled.checked,mode:mode.value,image:image.value.trim(),instanceId:mode.value==="existing"?iid.value:"",workspaceId:mode.value==="existing"?wid.value:"",executionSaved:mode.value==="existing"};remember(a,plan);freeze();}
     if(!id.disabled){plan.id=id.value.trim();remember(a,plan);freeze();}if(!plan.instanceId){plan.instanceId=(await api("/instances",{method:"POST",body:{name:plan.name,description:plan.description}})).id;remember(a,plan);}
     if(!plan.executionSaved){const current=(await api("/instances")).find(v=>v.id===plan.instanceId);if(!current)throw Error(rdText("创建的实例尚未读到，请重试"));if(current.execution?.mode!=="docker"||current.execution?.image!==plan.image)await api("/instances/"+plan.instanceId+"/execution",{method:"PUT",body:{revision:current.revision,spec:{mode:"docker",image:plan.image}}});plan.executionSaved=true;remember(a,plan);}
     if(!plan.workspaceId){plan.workspaceId=(await api("/workspaces",{method:"POST",body:{name:plan.name}})).id;remember(a,plan);}
     await persist(a,plan.id,{name:plan.name,description:plan.description,enabled:plan.enabled,instanceId:plan.instanceId,workspaceId:plan.workspaceId});remember(a,null);
    }
    await refreshApplicationCatalog();state.workspaces=await api("/workspaces");d.close();await refresh();
   }catch(failure){error.textContent=failure.message;if(plan&&!plan.instanceId&&!plan.workspaceId&&failure.status>=400&&failure.status<500&&!["request_in_progress","request_unconfirmed"].includes(failure.code)){plan=null;remember(a,null);inputs.forEach(input=>input.disabled=false);updateMode();}else if(plan&&["runtime_preset_binding_immutable","invalid_runtime_preset"].includes(failure.code)){id.disabled=false;error.textContent+=" · "+rdText("请修改预设编号后重试，已创建的环境继续复用。");}}
   finally{pending=false;submit.disabled=false;close.disabled=false;}
  };
  d.append(form);document.body.append(d);d.addEventListener("close",()=>d.remove(),{once:true});d.showModal();return d;
 }
 return {load,render,edit,select,binding};
})();
