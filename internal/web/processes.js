window.RunDeskProcesses=(()=>{
 const tr=rdText;
 function open(){
  const d=el('dialog',{class:'usage-dialog process-dialog'}),content=el('div',{}),error=el('p',{class:'error',role:'status'});let busy=false;
  const refresh=button(tr('刷新进程'),load);
  async function load(){if(busy)return;busy=true;refresh.disabled=true;error.textContent='';try{
   const r=await api('/processes');
   const protection=r.cleanupMode==='process-group'?tr('独立进程组清理'):tr('仅清理直接进程');
   content.replaceChildren(el('p',{class:'help'},`RunDesk PID ${r.serverPID} · ${r.platform} · ${protection} · ${new Date(r.observedAt).toLocaleString()}`),el('p',{class:'help'},tr('这里只列出 RunDesk 管理的 App Server 连接；不枚举全部子进程、构建进程或 MCP 测试进程。Docker 行的 PID 属于宿主机 docker exec，容器内仍由执行租约负责。')),
    ...r.items.map(p=>{
     const app=state.applications.find(a=>a.instanceId===p.instanceId)?.name||(p.instanceId==='default'?tr('通用助手'):p.instanceId),work=state.workspaces.find(w=>w.id===p.workspaceId)?.name||p.workspaceId;
     return el('div',{class:'process-row'},el('div',{},el('strong',{},app+' · '+work),el('p',{class:'help'},`PID ${p.pid} · ${p.executionMode||'local'} · `+(p.kind==='session'?tr('会话连接'):tr('配置连接'))+' · '+(p.closing?tr('关闭中'):tr('已连接'))),el('small',{},tr('启动时间：')+new Date(p.startedAt).toLocaleString()),p.cleanupError?el('p',{class:'error'},p.cleanupError):null),p.sessionId?button(tr('查看会话'),async()=>{d.close();await selectSession(p.sessionId);},'quiet'):null);
    }),...(!r.items.length?[el('p',{class:'empty'},tr('当前没有已登记的 App Server 连接。'))]:[]));
  }catch(e){error.textContent=e.message;}finally{busy=false;refresh.disabled=false;}}
  d.append(el('div',{class:'dialog-head'},el('h2',{},tr('运行进程')),button(tr('关闭'),()=>d.close(),'quiet')),el('p',{class:'help'},tr('Linux / macOS 在关闭连接时清理其进程组。自行脱离进程组的后台服务不在此保证内；生产部署使用系统服务管理器。Windows 当前不保证子孙进程清理。')),refresh,error,content);d.onclose=()=>d.remove();document.body.append(d);d.showModal();load();return d;
 }
 return {open};
})();
