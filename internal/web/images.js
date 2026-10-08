"use strict";
window.RunDeskImages=(()=>{
 const short=v=>v?v.replace(/^sha256:/,"").slice(0,12):rdText("尚未部署"),date=v=>v?new Date(v).toLocaleString():rdText("未提供"),size=v=>v?`${(v/1024/1024).toFixed(1)} MiB`:rdText("未提供");
 function needsUpdate(v,s){if(s.mode!=="docker")return false;const keys=["mode","image","cpus","memoryMB","pidsLimit","idleMinutes","network"];return keys.some(k=>v.spec[k]!==s[k])||!!(s.imageId&&(v.imageId||v.spec.imageId)!==s.imageId);}
 function pairs(values){return el("dl",{class:"image-metadata"},...values.flatMap(([name,value])=>[el("dt",{},name),el("dd",{},value||rdText("未提供"))]));}
 function endpoint(a){return `/instances/${a.instanceId}/images`;}
 async function overview(a,target){
  try{const c=await api(endpoint(a));if(!target.isConnected)return;const v=c.versions.find(x=>x.id===c.execution.imageVersionId),native=c.execution.mode!=="docker";
   target.replaceChildren(el("div",{},el("span",{class:"eyebrow"},native?rdText("运行方式"):rdText("应用镜像")),el("strong",{},native?rdText("本机 Codex"):c.execution.image),el("p",{class:"help"},native?rdText("可在运行环境中配置 Docker。"):rdFormat("${0} · ${1} 个环境 · ${2} 个待更新",v?rdText("固定版本 ")+short(v.imageId):rdText("尚未登记固定版本"),c.environments.length,c.pendingUpdates))),button(rdText("镜像与构建"),()=>open(a),"quiet"));
  }catch(e){if(target.isConnected)target.replaceChildren(el("p",{class:"error"},e.message),button(rdText("重新加载"),()=>overview(a,target),"quiet"));}
 }
 async function updateEnvironment(a,c,v,after){
  if(!confirm(rdFormat("将此环境更新到 ${0}（${1}）？\n空闲连接将关闭，项目文件、认证、Skills、MCP 配置及历史保留。镜像更新不会回退数据文件。",c.execution.image,short(c.execution.imageId))))return;
  await api(`/environments/${v.id}/actions`,{method:"POST",body:{action:"recreate",revision:v.revision,applicationRevision:c.instanceRevision}});toast(rdText("环境已更新"));await after();
 }
 async function open(a,selectedID){
  if($("#settings").open)$("#settings").close();await refreshApplicationCatalog();renderInstanceOptions(a.instanceId);setProductPage("application");history.replaceState(null,"","#application/"+encodeURIComponent(a.appId)+"/images");
  const page=$("#applications-page");setLoading(page);const c=await api(endpoint(a));if(location.hash!=="#application/"+encodeURIComponent(a.appId)+"/images")return;
  const desired=c.versions.find(v=>v.id===c.execution.imageVersionId),selected=c.versions.find(v=>v.id===selectedID)||desired||c.versions[0],native=c.execution.mode!=="docker";
  const details=el("aside",{class:"card image-detail"}),list=el("div",{class:"image-version-list"});
  function showDetails(v){
   for(const row of list.children)row.classList.toggle("selected",row.dataset.imageVersionId===v.id);
   const users=c.environments.filter(e=>e.imageId===v.imageId);
   details.replaceChildren(el("p",{class:"eyebrow"},rdText("镜像详情")),el("h2",{},v.version||v.reference),el("p",{class:"help"},rdText("以下信息来自本机镜像和构建者声明，不执行镜像内代码。")),pairs([[rdText("镜像引用"),v.reference],["Image ID",v.imageId],[rdText("仓库 Digest"),v.repoDigests.join("\n")],[rdText("平台"),v.os+" / "+v.architecture],[rdText("镜像大小"),size(v.size)],[rdText("镜像创建时间"),date(v.created)],[rdText("登记时间"),date(v.registeredAt)],[rdText("使用情况"),rdFormat("${0} 个环境已部署此镜像",users.length)]]),el("h3",{},rdText("构建来源")),pairs([[rdText("版本"),v.version],[rdText("源码"),v.source],[rdText("提交"),v.revision],[rdText("构建时间（声明）"),v.buildCreated],["Dockerfile",v.dockerfile],[rdText("构建记录"),v.buildUrl]]),el("h3",{},rdText("预装能力")),pairs([["Codex",v.codexVersion],[rdText("工具"),v.tools],[rdText("默认 Skills"),v.skills],[rdText("MCP 服务依赖"),v.mcp]]),el("p",{class:"help"},rdText("预装能力由镜像标签声明；实际启用的 Skills、MCP 连接和认证请在对应项目环境中查看。已有环境配置不会因更新镜像而覆盖。")));
  }
  for(const v of c.versions){
   const error=el("p",{class:"error",role:"status"},v.error||""),isDesired=v.id===c.execution.imageVersionId,used=c.environments.filter(e=>e.imageId===v.imageId).length;
   const select=button(isDesired?rdText("当前应用版本"):rdText("设为应用版本"),async()=>{
    if(!confirm(rdFormat("将应用目标版本设为 ${0}（${1}）？\n已有环境继续使用原版本，需逐个更新。新项目环境使用此版本。",v.reference,short(v.imageId))))return;
    select.disabled=true;error.textContent="";try{await api(endpoint(a)+`/${v.id}/select`,{method:"POST",body:{revision:c.instanceRevision}});toast(rdText("应用目标版本已保存"));await open(a,v.id);}catch(e){error.textContent=e.message;}finally{select.disabled=isDesired||native;}
   },isDesired?"quiet":"primary");select.disabled=isDesired||native;
   const check=button(rdText("检查可用性"),async()=>{check.disabled=true;try{await api(endpoint(a)+`/${v.id}/check`,{method:"POST"});await open(a,v.id);}catch(e){error.textContent=e.message;}finally{check.disabled=false;}},"quiet");
   list.append(el("article",{class:"image-version card","data-image-version-id":v.id},el("div",{class:"image-version-heading"},button(v.version||v.reference,()=>showDetails(v),"image-detail-link"),isDesired?el("span",{class:"badge"},rdText("应用目标")):null),el("p",{class:"image-reference"},v.reference),el("p",{class:"help"},rdFormat("${0} · ${1} 个环境使用 · ${2}",short(v.imageId),used,size(v.size))),el("p",{class:"image-availability "+(v.availability==="available"?"available":"error")},v.availability==="available"?rdText("上次检查可用"):rdText("检查未通过")),el("p",{class:"help"},date(v.checkedAt)),el("div",{class:"actions"},select,check,button(rdText("详情"),()=>showDetails(v),"quiet")),error));
  }
  if(selected)showDetails(selected);else details.replaceChildren(el("h2",{},rdText("保留镜像的来源与版本")),el("p",{},rdText("登记本机已构建或拉取的镜像后，可以查看镜像身份、构建来源和预装能力。未提供的字段会明确标注。")),el("p",{class:"help"},rdText("仅查看或登记镜像不会启动容器、执行构建或调用模型。")));
  if(!c.versions.length)list.append(el("div",{class:"application-empty compact"},el("h2",{},rdText("还没有登记镜像")),el("p",{},rdText("同一个标签指向新镜像时，再次登记会保留为独立版本。")),button(rdText("登记当前镜像"),()=>register(a,c.execution.image||""),"primary")));
  const envList=el("div",{class:"image-environments"});
  for(const v of c.environments){const pending=needsUpdate(v,c.execution),error=el("p",{class:"error",role:"status"}),action=button(pending?rdText("更新此环境"):rdText("重新部署"),async()=>{action.disabled=true;error.textContent="";try{await updateEnvironment(a,c,v,()=>open(a,selected?.id));}catch(e){error.textContent=e.message;}finally{action.disabled=native;}},pending?"primary":"quiet");action.disabled=native;
   envList.append(el("article",{class:"image-environment","data-image-environment-id":v.id},el("div",{},el("strong",{},state.workspaces.find(w=>w.id===v.workspaceId)?.name||v.workspaceId),el("p",{class:"help"},`${v.spec.image} · ${short(v.imageId)} · ${v.deployedAt?rdText("部署于 ")+date(v.deployedAt):rdText("暂无部署时间")}`)),el("span",{class:"badge"},native?rdText("已切换本机"):pending?rdText("待更新"):!v.imageId?rdText("待首次部署"):c.execution.imageId?rdText("版本一致"):rdText("标签未登记")),action,error));
  }
  if(!c.environments.length)envList.append(el("p",{class:"help"},rdText("尚无项目环境。选择镜像后，到运行环境中添加项目。")));
  page.replaceChildren(el("div",{class:"page-intro"},el("div",{},button(rdText("← 返回应用"),()=>showApplication(a.appId),"quiet back-link"),el("p",{class:"eyebrow"},"APPLICATION IMAGES"),el("h1",{},a.name+rdText(" · 镜像与构建")),el("p",{},rdText("登记镜像版本，查看来源，按项目更新环境。"))),el("div",{class:"actions"},button(rdText("构建任务"),()=>RunDeskBuilds.open(a),"primary"),button(rdText("运行环境"),()=>RunDeskEnvironments.open(a),"quiet"),button(rdText("刷新记录"),()=>open(a,selected?.id),"quiet"),button(rdText("登记镜像"),()=>register(a,c.execution.image||""),"primary"))),
   el("section",{class:"image-summary card"},el("div",{},el("span",{class:"eyebrow"},rdText("应用目标")),el("strong",{},native?rdText("本机 Codex"):c.execution.image),el("p",{class:"help"},native?rdText("先到运行环境中启用 Docker，再选择应用版本。"):desired?rdText("固定 Image ID · ")+short(desired.imageId):rdText("按标签配置 · 登记并设为应用版本后固定镜像身份"))),el("div",{},el("strong",{},String(c.versions.length)),el("span",{},rdText("已登记版本"))),el("div",{},el("strong",{},String(c.pendingUpdates)),el("span",{},rdText("待更新环境")))),
   el("section",{class:"image-usage card"},el("h2",{},rdText("环境使用情况")),el("p",{class:"help"},rdText("已有环境继续使用已保存的镜像与资源配置。更新只作用于选定环境；运行中的任务会阻止重建。")),envList),
   el("div",{class:"image-layout"},el("section",{},el("h2",{},rdText("镜像版本")),list),details),
   el("details",{class:"image-build-note"},el("summary",{},rdText("镜像构建与元数据说明")),el("p",{},rdText("可登记外部构建的本机镜像，也可进入构建任务上传目录 ZIP。成功构建自动登记固定镜像 ID，不自动修改应用目标或已有环境。")),el("p",{},rdText("版本、源码和提交信息读取 org.opencontainers.image.* 标签；预装能力读取 io.rundesk.* 标签。示例 Dockerfile 和 IMAGES.md 提供完整约定。Image ID 是本机镜像身份，仓库 Digest 单独展示，二者不混用。")),el("p",{},rdText("检查结果带时间，仅表示上次检查状态。检查不可用可能是镜像缺失或 Docker 服务故障；详情中保留失败原因。"))));
 }
 function register(a,reference){
  const d=el("dialog",{class:"application-register"}),input=el("input",{id:"image-register-reference",value:reference,required:true,maxlength:255,placeholder:rdText("例如 rundesk-news:1.1")}),error=el("p",{class:"error",role:"status"}),submit=el("button",{class:"primary",type:"submit"},rdText("检查并登记"));
  const form=el("form",{},el("div",{class:"dialog-head"},el("h2",{},rdText("登记本机镜像")),button(rdText("关闭"),()=>d.close(),"quiet")),el("label",{},rdText("镜像名称、标签或 Digest"),input),el("p",{class:"help"},rdText("请先在 RunDesk 所在主机构建或拉取镜像。登记读取镜像信息，并检查平台和挂载兼容性；不会更改应用或运行环境。")),error,submit);
  form.onsubmit=async ev=>{ev.preventDefault();submit.disabled=true;try{const v=await api(endpoint(a),{method:"POST",body:{reference:input.value}});d.close();await open(a,v.id);}catch(e){error.textContent=e.message;}finally{submit.disabled=false;}};d.append(form);document.body.append(d);d.addEventListener("close",()=>d.remove(),{once:true});d.showModal();input.focus();
 }
 return{open,overview,needsUpdate,short};
})();
