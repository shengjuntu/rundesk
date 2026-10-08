"use strict";
window.RunDeskUsers=(()=>{
 async function open(){
  if($("#settings").open)$("#settings").close();resetConversation();setProductPage("users");history.replaceState(null,"","#users");$("#page-label").textContent=rdText("用户与授权");
  const page=$("#applications-page");setLoading(page);
  const [users,apps,workspaces,environments]=await Promise.all([api("/users"),api("/applications"),api("/workspaces"),api("/environments")]);
  if(location.hash!=="#users")return;
  const options=environments.map(v=>{const a=apps.find(a=>a.instanceId===v.instanceId),w=workspaces.find(w=>w.id===v.workspaceId);return a&&w?{appId:a.appId,workspaceId:w.id,label:a.name+" · "+w.name}:null;}).filter(Boolean);
  const list=el("div",{class:"user-grid"});
  for(const u of users){
   const error=el("p",{class:"error",role:"status"}),update=async body=>{try{await api(`/users/${u.id}`,{method:"PUT",body:{...body,revision:u.revision}});await open();}catch(e){error.textContent=e.message;}};
   list.append(el("article",{class:"card user-card","data-user-id":u.id},el("div",{class:"user-heading"},el("h2",{},u.name),el("span",{class:"badge"},u.enabled?rdText("已启用"):rdText("已停用"))),el("p",{class:"help"},rdText("用户编号 · ")+u.id),el("div",{class:"user-grants"},...(u.grants.length?u.grants.map(g=>el("p",{},el("span",{},options.find(x=>x.appId===g.appId&&x.workspaceId===g.workspaceId)?.label||g.appId+" · "+g.workspaceId),el("strong",{},g.role==="runner"?rdText("可执行"):rdText("只读")))):[el("p",{class:"help"},rdText("尚未分配项目"))])),el("div",{class:"actions"},button(rdText("编辑授权"),()=>edit(u,options),"quiet"),button(u.enabled?rdText("停用"):rdText("启用"),async()=>{if(u.enabled&&!confirm(rdText("停用此用户？旧登录立即失效；已接受的任务不会自动取消。")))return;await update({...u,enabled:!u.enabled,name:u.name,grants:u.grants,created:undefined,updated:undefined,id:undefined});},"quiet"),button(rdText("重置访问码"),async()=>{if(!confirm(rdText("生成新的个人访问码？旧访问码和全部旧登录将失效。")))return;try{const result=await api(`/users/${u.id}/access-code`,{method:"POST",body:{revision:u.revision}});await open();showCode(result);}catch(e){error.textContent=e.message;}},"quiet")),error));
  }
  if(!users.length)list.append(el("div",{class:"application-empty compact"},el("h2",{},rdText("把应用项目分配给协作者")),el("p",{},rdText("先为应用创建独立项目环境，再创建用户并授权。不同用户可共享同一项目，或使用不同项目隔离数据。"))));
  page.replaceChildren(el("div",{class:"page-intro"},el("div",{},el("p",{class:"eyebrow"},"PEOPLE & PROJECT ACCESS"),el("h1",{},rdText("用户与授权")),el("p",{},rdText("管理员维护运行配置；成员在获授权的应用项目中查看或执行任务。"))),el("div",{class:"actions"},button(rdText("刷新"),open,"quiet"),button(rdText("创建用户"),()=>edit(null,options),"primary"))),el("div",{class:"user-policy card"},el("strong",{},rdText("项目是共享边界")),el("p",{class:"help"},rdText("同一应用项目的成员可查看该项目的应用任务与文件。需要独立数据时，先创建不同项目环境。通用助手与运行配置暂仅管理员可用。"))),list);
 }
 function edit(u,options){
  const d=el("dialog",{class:"user-editor"}),name=el("input",{id:"user-name",required:true,maxlength:120,value:u?.name||"",placeholder:rdText("例如 新闻编辑")}),rows=[];
  const list=el("div",{class:"user-project-picker"});for(const option of options){const prev=u?.grants.find(g=>g.appId===option.appId&&g.workspaceId===option.workspaceId),check=el("input",{type:"checkbox",checked:!!prev}),role=el("select",{"aria-label":option.label+rdText(" 权限")},el("option",{value:"viewer"},rdText("只读")),el("option",{value:"runner"},rdText("可执行")));role.value=prev?.role||"viewer";role.disabled=!check.checked;check.onchange=()=>role.disabled=!check.checked;rows.push({option,check,role});list.append(el("div",{class:"user-project-option"},el("label",{},check,el("span",{},option.label)),role));}
  if(!options.length)list.append(el("p",{class:"help"},rdText("还没有应用项目环境。可先创建账号，稍后分配项目。")));
  const error=el("p",{class:"error",role:"status"}),submit=el("button",{type:"submit",class:"primary"},u?rdText("保存授权"):rdText("创建并生成访问码")),form=el("form",{},el("div",{class:"dialog-head"},el("h2",{},u?rdText("编辑用户授权"):rdText("创建用户")),button(rdText("关闭"),()=>d.close(),"quiet")),el("label",{},rdText("用户名称"),name),el("h3",{},rdText("应用项目与权限")),list,el("p",{class:"help"},rdText("只读：查看任务、过程和项目文件。可执行：另外允许新建任务、继续、停止、上传与处理审批。修改授权会使旧登录失效。")),error,submit);
  form.onsubmit=async e=>{e.preventDefault();submit.disabled=true;try{const body={name:name.value,enabled:u?.enabled??true,grants:rows.filter(x=>x.check.checked).map(x=>({appId:x.option.appId,workspaceId:x.option.workspaceId,role:x.role.value})),revision:u?.revision||0},result=await api(u?`/users/${u.id}`:"/users",{method:u?"PUT":"POST",body});d.close();await open();if(!u)showCode(result);}catch(e){error.textContent=e.message;}finally{submit.disabled=false;}};d.append(form);document.body.append(d);d.addEventListener("close",()=>d.remove(),{once:true});d.showModal();
 }
 function showCode(result){
  const d=el("dialog",{class:"user-editor user-code-dialog"}),code=el("textarea",{id:"user-access-code",readOnly:true,rows:4},result.accessCode);
  d.append(el("div",{class:"dialog-head"},el("h2",{},rdText("保存个人访问码")),button(rdText("关闭"),()=>d.close(),"quiet")),el("p",{},result.user.name+rdText(" 的访问码只在这里显示一次。请自行交给对应用户。")),code,el("p",{class:"help"},rdText("用户打开同一 RunDesk 地址，在登录页输入此码。丢失后可重置；不要分享管理员令牌。")),button(rdText("复制访问码"),async()=>{await navigator.clipboard.writeText(code.value);toast(rdText("已复制"));},"primary"));document.body.append(d);d.addEventListener("close",()=>{code.value="";d.remove();},{once:true});d.showModal();
 }
 $("#nav-users").onclick=()=>safe(open);return{open};
})();
