"use strict";
window.RunDeskEnvironments=(()=>{
 const states={not_created:"尚未创建",created:"待启动",running:"运行中",exited:"已停止",dead:"异常退出",unknown:"待核对",paused:"已暂停",restarting:"正在重启"};
 async function effective(i=instance(),w=ws()){return i.execution?.mode==="docker"?api(`/instances/${i.id}/configuration?workspaceId=${w.id}`):{instance:i};}
 function scopeLabel(i=instance()){return i?.execution?.mode==="docker"?"当前应用 · 当前项目环境":"当前助手／应用";}
 function loginDetails(current,environment){
  const q=v=>"'"+v.replaceAll("'","'\\''")+"'",ps=v=>"'"+v.replaceAll("'","''")+"'";
  const d=el("details",{},el("summary",{},"登录命令与配置目录"));
  if(environment)d.append(el("p",{class:"help"},"先在运行环境页面启动容器，再在 RunDesk 所在主机执行。认证只保存在这个项目环境，不复制通用助手的账户。"),el("pre",{},`docker exec -it ${q(environment.containerName)} codex login --device-auth`),el("p",{class:"help"},"自定义 Unix socket 需给 docker 添加 --host 参数。"));
  else d.append(el("strong",{},"Ubuntu / macOS"),el("pre",{},"env CODEX_HOME="+q(current.codexHome)+" codex login"),el("strong",{},"Windows PowerShell"),el("pre",{},"$env:CODEX_HOME = "+ps(current.codexHome)+"\ncodex login"));
  d.append(el("p",{class:"help"},"当前 CODEX_HOME："+current.codexHome));return d;
 }
 function projectOptions(){return state.workspaces.map(w=>el("option",{value:w.id},w.name));}
 async function open(a){
  if($("#settings").open)$("#settings").close();await refreshApplicationCatalog();state.workspaces=await api("/workspaces");renderInstanceOptions(a.instanceId);setProductPage("application");history.replaceState(null,"","#application/"+encodeURIComponent(a.appId)+"/environments");
  const i=state.instances.find(v=>v.id===a.instanceId),page=$("#applications-page");setLoading(page);const environments=await api("/environments?instanceId="+i.id),spec=i.execution||{mode:"local"};
  const mode=el("select",{id:"execution-mode"},el("option",{value:"docker"},"Docker 应用环境"),el("option",{value:"local"},"本机 Codex（兼容已有应用）"));mode.value=spec.mode||"local";
  const image=el("input",{id:"execution-image",value:spec.image||"",placeholder:"例如 rundesk-news:1.0",maxlength:255});
  const cpus=el("input",{type:"number",min:1,max:32,value:spec.cpus||2}),memory=el("input",{type:"number",min:256,max:65536,value:spec.memoryMB||2048}),pids=el("input",{type:"number",min:32,max:4096,value:spec.pidsLimit||256}),idle=el("input",{type:"number",min:1,max:1440,value:spec.idleMinutes||15}),network=el("select",{},el("option",{value:"bridge"},"允许容器网络"),el("option",{value:"none"},"禁用网络"));network.value=spec.network||"bridge";
  const error=el("p",{class:"error",role:"status"}),engine=el("pre",{class:"environment-engine hidden"}),save=el("button",{type:"submit",class:"primary"},"保存运行配置");
  const fields=el("div",{class:"environment-fields"},el("label",{},"应用镜像",image),el("p",{class:"help"},"在“镜像与构建”中登记和选择固定版本。此处也可填写本机镜像标签；修改标签会取消应用目标版本的固定。"),el("details",{},el("summary",{},"资源与空闲回收"),el("div",{class:"environment-resources"},...[["CPU 核数",cpus],["内存 MiB",memory],["进程上限",pids],["空闲分钟",idle],["网络",network]].map(([n,v])=>el("label",{},n,v)))));
  function updateMode(){fields.classList.toggle("hidden",mode.value!=="docker");image.required=mode.value==="docker";}mode.onchange=updateMode;updateMode();
  const form=el("form",{class:"card execution-form"},el("h2",{},"应用运行配置"),el("label",{},"运行方式",mode),fields,el("p",{class:"help"},"配置用于新环境；已有 Docker 项目沿用原镜像与资源，逐个更新后生效。切换运行方式后请新建会话。"),error,save);
  form.onsubmit=async ev=>{ev.preventDefault();save.disabled=true;error.textContent="";try{await api(`/instances/${i.id}/execution`,{method:"PUT",body:{revision:i.revision,spec:mode.value==="local"?{mode:"local"}:{mode:"docker",image:image.value,...(image.value.trim()===spec.image?{imageId:spec.imageId,imageVersionId:spec.imageVersionId}:{}),cpus:Number(cpus.value),memoryMB:Number(memory.value),pidsLimit:Number(pids.value),idleMinutes:Number(idle.value),network:network.value}}});toast("运行配置已保存");await open(a);}catch(e){error.textContent=e.message;}finally{save.disabled=false;}};
  const projects=el("select",{id:"environment-project","aria-label":"选择项目"},...projectOptions());projects.value=ws()?.id||state.workspaces[0]?.id;
  const create=button("添加项目环境",async()=>{await api("/environments",{method:"POST",body:{instanceId:i.id,workspaceId:projects.value}});await open(a);},"primary"),addProject=button("新建独立项目",()=>newProject(a,i),"quiet");create.disabled=addProject.disabled=i.execution?.mode!=="docker";
  const list=el("div",{class:"environment-list"});
  for(const v of environments){
   const w=state.workspaces.find(w=>w.id===v.workspaceId),failure=el("p",{class:"error",role:"status"},v.error||""),actions=el("div",{class:"actions"});
   for(const [label,action]of [["核对状态","inspect"],["启动","start"],["停止","stop"],["重建容器","recreate"],["移除容器","remove"]]){
    const b=button(label,async()=>{if((action==="recreate"||action==="remove")&&!confirm(action==="recreate"?`用当前应用镜像 ${spec.image}（${RunDeskImages.short(spec.imageId)}）重建容器？项目文件、认证、技能和历史都会保留。`:"移除空闲容器？项目数据保留，下次任务可重新创建。"))return;b.disabled=true;failure.textContent="";try{if(action==="inspect")await api(`/environments/${v.id}`);else await api(`/environments/${v.id}/actions`,{method:"POST",body:{action,revision:v.revision,applicationRevision:i.revision}});await open(a);}catch(e){failure.textContent=e.message;}finally{b.disabled=false;}},action==="start"?"primary":"quiet");actions.append(b);
   }
   const details=el("details",{},el("summary",{},"数据目录与镜像身份"),el("dl",{class:"environment-paths"},...[["容器",v.containerName],["固定镜像",v.imageId||"首次启动时固定"],["上次部署",v.deployedAt?new Date(v.deployedAt).toLocaleString():"未记录"],["Codex 数据",v.codexHome],["项目文件",w?.path||v.workspaceId],["项目编号",v.workspaceId]].flatMap(([k,val])=>[el("dt",{},k),el("dd",{},val)])));
   const settings=button("配置此项目的能力",async()=>{$("#workspace").replaceChildren(...projectOptions());$("#workspace").value=v.workspaceId;updateWorkspaceLabel();await openSettings("overview");},"quiet");
   const integration=button("复制接入信息",async()=>{await navigator.clipboard.writeText(json({apiBase:location.origin+"/api/v1",appId:a.appId,instanceId:i.id,workspaceId:v.workspaceId}));toast("已复制当前项目的接入信息");},"quiet");
   list.append(el("article",{class:"card environment-card","data-environment-id":v.id},el("div",{class:"environment-card-heading"},el("div",{},el("h3",{},w?.name||v.workspaceId),el("p",{class:"help"},v.spec.image)),el("span",{class:"badge"},states[v.state]||v.state)),el("p",{class:"help"},`${v.activeConnections} 个连接 · `+(v.observedAt?"上次记录 "+new Date(v.observedAt).toLocaleString():"尚未连接 Docker")),RunDeskImages.needsUpdate(v,spec)?el("p",{class:"image-update-notice"},"待更新 · 当前继续使用原版本及资源配置，可在镜像与构建页查看目标版本。"):null,v.oomKilled?el("p",{class:"error"},"容器曾因内存不足被终止，请检查任务用量和内存配额。"):null,details,el("div",{class:"actions"},settings,integration),actions,failure));
  }
  if(!environments.length)list.append(el("div",{class:"application-empty compact"},el("h3",{},"还没有项目环境"),el("p",{},"配置镜像后添加项目。首次任务自动启动容器，同一项目的后续会话复用环境。")));
  page.replaceChildren(el("div",{class:"page-intro"},el("div",{},button("← 返回应用",()=>showApplication(a.appId),"quiet back-link"),el("p",{class:"eyebrow"},"APPLICATION RUNTIME"),el("h1",{},a.name+" · 运行环境"),el("p",{},"一个应用镜像，多个项目环境。数据保存在宿主机，容器可停止和重建。")),el("div",{class:"actions"},button("镜像与构建",()=>RunDeskImages.open(a),"quiet"),button("刷新列表",()=>open(a),"quiet"),button("检查 Docker",async()=>{engine.classList.remove("hidden");engine.textContent="正在检查…";try{engine.textContent=json(await api("/docker/status"));}catch(e){engine.textContent=e.message;}},"quiet"))),el("div",{class:"environment-layout"+(environments.length?" has-environments":"")},form,el("section",{class:"environment-projects"},el("div",{class:"environment-add"},projects,create,addProject),el("p",{class:"help"},"技能、MCP、认证与原生记忆在每个项目环境中独立保存。停止和重建只允许在任务空闲时执行。空闲回收保留数据，请定期备份并按业务需要清理。"),list)),engine);
 }
 function newProject(a,i){
  const d=el("dialog",{class:"application-register"}),name=el("input",{id:"environment-project-name",required:true,maxlength:160,placeholder:"例如 新闻研究 · 项目 A"}),error=el("p",{class:"error",role:"status"}),submit=el("button",{type:"submit",class:"primary"},"创建项目环境");let wid;
  const form=el("form",{},el("div",{class:"dialog-head"},el("h2",{},"新建独立项目"),button("关闭",()=>d.close(),"quiet")),el("label",{},"项目名称",name),el("p",{class:"help"},"自动分配独立的宿主机目录。应用提交任务时使用这个项目编号。"),error,submit);
  form.onsubmit=async ev=>{ev.preventDefault();submit.disabled=true;try{if(!wid)wid=(await api("/workspaces",{method:"POST",body:{name:name.value}})).id;await api("/environments",{method:"POST",body:{instanceId:i.id,workspaceId:wid}});d.close();await open(a);}catch(e){error.textContent=e.message;}finally{submit.disabled=false;}};d.append(form);document.body.append(d);d.addEventListener("close",()=>d.remove(),{once:true});d.showModal();
 }
 return{open,effective,scopeLabel,loginDetails};
})();
