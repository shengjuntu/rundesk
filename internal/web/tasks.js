"use strict";
window.RunDeskTasks=(()=>{
 let rows=[],queue=null,cursor="",filter="",signature="",busy=false,environments=[];
 const labels={...statusLabel,queued:rdText("排队中"),dispatching:rdText("正在接收"),canceled:rdText("已取消"),unconfirmed:rdText("待核对")};
 const pending=t=>["queued","dispatching","starting","running","waiting","stopping"].includes(t.status);
 async function refresh(more=false){if(busy||state.view!=="tasks")return;busy=true;try{const [q,data]=await Promise.all([api("/queue"),api("/tasks?limit="+(more?100:Math.min(500,Math.max(100,rows.length)))+"&status="+encodeURIComponent(filter)+(more&&cursor?"&cursor="+cursor:""))]);if(state.view!=="tasks")return;queue=q;rows=more?[...rows,...data.items]:data.items;cursor=data.nextCursor;render();await window.RunDeskSchedules?.refresh();}catch(e){$("#tasks-error").textContent=e.message;}finally{busy=false;}}
 function render(){const sig=json({rows,queue,cursor,filter});if(sig===signature)return;signature=sig;const opened=new Set([...$("#task-list").querySelectorAll("details[open]")].map(d=>d.dataset.task));
  $("#queue-summary").textContent=rdFormat("全局最多 ${0} 项同时执行 · ${1} · 列表 ${2} 项",queue.maxConcurrent,queue.paused?rdText("调度已暂停"):rdText("调度中"),rows.length);
  $("#queue-toggle").textContent=queue.paused?rdText("恢复调度"):rdText("暂停调度");$("#task-list").replaceChildren(...rows.map(t=>{
   const d=el("details",{class:"queue-task","data-task":t.id,open:opened.has(t.id)},el("summary",{},el("span",{},el("strong",{},t.spec.title),el("small",{},(state.instances.find(i=>i.id===t.spec.instanceId)?.name||t.spec.instanceId||rdText("实例已移除"))+" · "+new Date(t.created).toLocaleString())),el("span",{class:"badge"},labels[t.status]||t.status)),el("p",{class:"help"},t.reason|| (t.spec.notBefore?rdText("最早执行：")+new Date(t.spec.notBefore).toLocaleString():t.status==="queued"?rdText("按提交顺序等待可用执行额度"):pending(t)?rdText("执行过程可在会话中查看"):rdText("原始执行记录已保留，可进入会话查看结果"))),el("pre",{},t.spec.input.text),el("div",{class:"actions"},button(rdText("查看会话与过程"),()=>selectSession(t.sessionId),"quiet"),pending(t)?button(rdText("取消任务"),async()=>{await api("/tasks/"+t.id+"/cancel",{method:"POST",body:{}});await refresh();},"quiet"):null));return d;
  }));if(!rows.length)$("#task-list").append(el("p",{class:"empty"},rdText("这里还没有任务。新建任务后，可离开此页面，后台会继续调度。")));$("#tasks-more").classList.toggle("hidden",!cursor);$("#tasks-error").textContent="";
 }
 async function open(){
  resetConversation();setProductPage("tasks");history.replaceState(null,"","#tasks");signature="";
  const selection=state.selection;
  try{
   const [instances,workspaces,applications,currentEnvironments]=await Promise.all([api("/instances"),api("/workspaces"),api("/applications"),api("/environments")]);
   if(state.view!=="tasks"||state.selection!==selection)return;
   state.instances=instances;state.workspaces=workspaces;state.applications=applications;state.environments=currentEnvironments;environments=currentEnvironments;
   await refresh();
  }catch(e){if(state.view==="tasks"&&state.selection===selection)$("#tasks-error").textContent=e.message;}
 }
 function dialog(title,fields,save){const d=el("dialog",{class:"task-dialog"}),error=el("p",{class:"error",role:"status"}),submit=el("button",{type:"submit",class:"primary"},rdText("保存"));const form=el("form",{},el("div",{class:"dialog-head"},el("h2",{},title),button(rdText("关闭"),()=>d.close(),"quiet")),...fields,error,submit);form.onsubmit=async e=>{e.preventDefault();submit.disabled=true;try{await save();d.close();signature="";await refresh();}catch(e){error.textContent=e.message;}finally{submit.disabled=false;}};d.append(form);document.body.append(d);d.onclose=()=>d.remove();d.showModal();return d;}
 function taskFields(spec={}){
  const bindings=state.applications.flatMap(a=>(a.runtimePresets||[]).map(p=>({a,p})));
  const owner=iid=>bindings.find(({p})=>p.instanceId===iid);
  const title=el("input",{id:"task-title",value:spec.title||"",maxlength:120,required:true});
  const selectedInstance=el("select",{id:"task-instance",required:true},...state.instances.map(i=>el("option",{value:i.id},i.name+" · "+(i.agentRuntime?.kind==="kun"?"Kun":"Codex"))));
  const app=el("select",{id:"task-app"},el("option",{value:""},rdText("手动任务")),...state.applications.map(a=>el("option",{value:a.appId},a.name)));
  const preset=el("select",{id:"task-preset"}),work=el("select",{id:"task-workspace",required:true});
  const text=el("textarea",{id:"task-text",rows:5,required:true},spec.input?.text||""),model=el("input",{id:"task-model",value:spec.model||"",placeholder:rdText("留空使用默认模型")});
  const sourceDetails=el("details",{class:"task-source",open:!!spec.source?.appId},el("summary",{},rdText("应用来源（可选）")),el("p",{class:"help"},rdText("普通任务直接使用所选实例。应用预设使用固定的实例和工作区。")),el("label",{},rdText("来源应用"),app),el("label",{},rdText("制作环境预设"),preset));
  const notice=el("p",{class:"help",role:"status"});
  function chooseInstance(iid){
   if(![...selectedInstance.options].some(o=>o.value===iid))selectedInstance.append(el("option",{value:iid,disabled:true},rdText("历史实例 · ")+iid));
   selectedInstance.value=iid;
  }
  function availableWorkspaces(iid,p){
   if(p)return state.workspaces.filter(w=>w.id===p.workspaceId);
   const docker=state.instances.find(i=>i.id===iid)?.execution?.mode==="docker";
   return state.workspaces.filter(w=>!bindings.some(b=>b.p.workspaceId===w.id)&&(!docker||!(state.environments||environments).some(e=>e.workspaceId===w.id&&e.instanceId!==iid)));
  }
  function updateWorkspaces(preferred,p){
   const iid=selectedInstance.value,available=availableWorkspaces(iid,p),requested=p?.workspaceId||preferred;
   work.replaceChildren(...available.map(w=>el("option",{value:w.id},w.name)));
   work.disabled=!!p;
   if(requested){
    if(!available.some(w=>w.id===requested))work.append(el("option",{value:requested,disabled:true},rdText("工作区不可用 · ")+requested));
    work.value=requested;
   }else{
    const owned=(state.environments||environments).find(e=>e.instanceId===iid&&available.some(w=>w.id===e.workspaceId))?.workspaceId;
    const recent=state.sessions.find(s=>s.instanceId===iid&&available.some(w=>w.id===s.workspaceId))?.workspaceId;
    work.value=owned||recent||available[0]?.id||"";
   }
   notice.textContent=p&&!p.enabled?rdText("此应用预设已停用；保留历史绑定，不能提交新任务。"):!available.length?rdText("没有可用工作区，请先在实例页面创建或选择工作区。"):p?rdText("此实例属于应用预设，任务将使用它的固定工作区和来源。"):rdText("任务使用所选实例和工作区；创建任务前不会启动运行环境。");
  }
  function presetOptions(a,selected=""){
   preset.replaceChildren(el("option",{value:""},rdText("应用默认环境")),...(a?.runtimePresets||[]).filter(p=>p.enabled||p.id===spec.source?.presetId).map(p=>el("option",{value:p.id,disabled:!p.enabled},p.name+(p.enabled?"":rdText(" · 已禁用")))));
   if(selected&&![...preset.options].some(o=>o.value===selected))preset.append(el("option",{value:selected,disabled:true},rdText("历史预设 · ")+selected));
   preset.value=selected;preset.disabled=!a;
  }
  function updateBinding(preferred){
   const a=state.applications.find(a=>a.appId===app.value),p=(a?.runtimePresets||[]).find(p=>p.id===preset.value);
   if(p)chooseInstance(p.instanceId);else if(a&&!preset.value)chooseInstance(a.instanceId);
   updateWorkspaces(p?.workspaceId||preferred||a?.workspaceId,p);
  }
  function updateInstance(){
   const binding=owner(selectedInstance.value);
   if(binding){app.value=binding.a.appId;presetOptions(binding.a,binding.p.id);sourceDetails.open=true;updateWorkspaces(binding.p.workspaceId,binding.p);}
   else{app.value="";presetOptions(null);sourceDetails.open=false;updateWorkspaces(selectedInstance.value===instance()?.id?ws()?.id:undefined);}
  }
  selectedInstance.onchange=updateInstance;
  app.onchange=()=>{
   const a=state.applications.find(a=>a.appId===app.value);presetOptions(a);
   if(a)updateBinding();else updateInstance();
  };
  preset.onchange=()=>updateBinding();
  chooseInstance(spec.instanceId||instance()?.id||"default");
  if(spec.source?.appId){
   if(![...app.options].some(o=>o.value===spec.source.appId))app.append(el("option",{value:spec.source.appId,disabled:true},rdText("历史应用 · ")+spec.source.appId));
   app.value=spec.source.appId;presetOptions(state.applications.find(a=>a.appId===app.value),spec.source.presetId||"");updateBinding(spec.workspaceId);
  }else{updateInstance();if(spec.workspaceId)updateWorkspaces(spec.workspaceId,owner(selectedInstance.value)?.p);}
  return {fields:[el("label",{},rdText("任务名称"),title),el("label",{},rdText("执行实例"),selectedInstance),el("label",{},rdText("工作区"),work),notice,sourceDetails,el("label",{},rdText("任务内容"),text),el("label",{},rdText("模型（可选）"),model)],value:()=>{
   const a=state.applications.find(a=>a.appId===app.value),p=(a?.runtimePresets||[]).find(p=>p.id===preset.value),iid=selectedInstance.value;
   if(preset.value&&!p)throw Error(rdText("历史预设已不可用，请重新选择制作环境"));
   if(app.value&&!a)throw Error(rdText("历史应用已不可用，请重新选择来源"));
   if(!state.instances.some(i=>i.id===iid))throw Error(rdText("实例已不可用，请重新选择"));
   if(p&&!p.enabled&&!(spec.source?.appId===a.appId&&spec.source?.presetId===p.id&&spec.instanceId===p.instanceId&&spec.workspaceId===p.workspaceId))throw Error(rdText("应用预设已停用，不能提交新任务"));
   const reserved=owner(iid);
   if(reserved&&(!p||reserved.a.appId!==a?.appId||reserved.p.id!==p.id))throw Error(rdText("此实例必须使用绑定的应用预设"));
   if(a&&iid!==(p?.instanceId||a.instanceId))throw Error(rdText("实例与应用绑定不一致，请重新选择"));
   if(!availableWorkspaces(iid,p).some(w=>w.id===work.value))throw Error(rdText("工作区不可用，请重新选择"));
   return {title:title.value,workspaceId:p?.workspaceId||work.value,instanceId:iid,model:model.value,source:a?{kind:"application",appId:a.appId,...(p?{presetId:p.id}:{})}:{kind:"human"},input:{text:text.value,files:spec.input?.files||[],skills:spec.input?.skills||[]}};
  }};
 }

 function create(){const f=taskFields(),at=el("input",{id:"task-not-before",type:"datetime-local"});dialog(rdText("新建任务"),[...f.fields,el("label",{},rdText("最早执行时间（可选，本机时区）"),at)],async()=>api("/tasks",{method:"POST",body:{...f.value(),notBefore:at.value?new Date(at.value).toISOString():""}}));}
 function settings(){const max=el("input",{id:"queue-max",type:"number",min:1,max:16,required:true,value:queue.maxConcurrent}),per=state.instances.map(i=>({i,input:el("input",{type:"number",min:1,max:16,value:queue.perInstance?.[i.id]||"",placeholder:rdText("继承全局限制")})}));dialog(rdText("并发设置"),[el("p",{class:"help"},rdText("普通对话、队列任务和等待审批的任务共同占用额度。降低上限不会终止正在执行的任务。")),el("label",{},rdText("全局并发"),max),...per.map(x=>el("label",{},x.i.name,x.input))],async()=>{const limits={...queue.perInstance};for(const x of per){if(x.input.value)limits[x.i.id]=Number(x.input.value);else delete limits[x.i.id];}await api("/queue",{method:"PUT",body:{...queue,maxConcurrent:Number(max.value),perInstance:limits}});});}
 async function banner(){const s=state.session,host=$("#queue-banner");if(!s?.taskId||state.view!=="conversation"){host.classList.add("hidden");return};try{const t=await api("/tasks/"+s.taskId);if(state.session?.id!==s.id)return;host.classList.remove("hidden");host.textContent=rdText("队列任务 · ")+(labels[t.status]||t.status)+(t.reason?" · "+t.reason:"");if(["queued","dispatching"].includes(t.status)){for(const id of ["send","archive-session","delete-session"])$("#"+id).disabled=true;} }catch(e){host.classList.remove("hidden");host.textContent=e.message;}}
 $("#processes-open").onclick=()=>RunDeskProcesses.open();$("#usage-open").onclick=()=>RunDeskUsage.open();$("#nav-tasks").onclick=()=>safe(open);$("#new-task").onclick=create;$("#queue-settings").onclick=settings;$("#queue-toggle").onclick=()=>safe(async()=>{await api("/queue",{method:"PUT",body:{...queue,paused:!queue.paused}});await refresh();});$("#tasks-refresh").onclick=()=>refresh();$("#tasks-more").onclick=()=>refresh(true);$("#task-filter").onchange=e=>{filter=e.target.value;refresh();};setInterval(()=>{if(state.view==="tasks")refresh();else banner();},2500);
 return {open,refresh,dialog,taskFields,banner};
})();
