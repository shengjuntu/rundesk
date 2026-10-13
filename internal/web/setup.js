"use strict";
window.RunDeskSetup=(()=>{
 const tr=(zh,en)=>document.documentElement.lang==='en'?en:zh;
 async function maybeOpen(){
  const v=await api('/setup');
  const wid=v.settings.workspaceId||ws()?.id||state.workspaces[0]?.id||'';
  const environment=await api('/setup/environment?workspaceId='+encodeURIComponent(wid));
  if(!environment.demo&&(v.firstRun||!environment.ready)&&!sessionStorage.getItem('rundesk-setup-dismissed'))await open();
 }
 async function open(app){
  const v=await api('/setup'), configured=app?state.instances.find(i=>i.id===app.instanceId):v.instance;
  const i=configured||v.instance, cfg={...i.agentRuntime}, d=el('dialog',{class:'setup-dialog'}),error=el('p',{class:'error',role:'alert'});
  let revision=v.settings.revision,instanceRevision=i.revision,busy=false;
  const path=el('input',{value:v.codex,id:'setup-codex'}),model=el('input',{value:i.defaultModel||cfg.model||'',id:'setup-model'});
  const endpoint=el('input',{id:'setup-endpoint',value:cfg.endpoint||'http://127.0.0.1:8000/v1'}),key=el('input',{id:'setup-key-env',value:cfg.apiKeyEnv||'',placeholder:'OPENAI_API_KEY'});
  const kind=el('select',{id:'setup-runtime'},el('option',{value:'kun'},'Kun'),el('option',{value:'codex'},'Codex'),...(!app?[el('option',{value:'applications'},tr('仅使用 Docker 应用','Docker applications only'))]:[]));kind.value=(!app&&v.settings.applicationsOnly)?'applications':(cfg.kind||'codex');kind.disabled=!!app;
  const project=el('select',{id:'setup-workspace'},...state.workspaces.map(w=>el('option',{value:w.id},w.name)));
  project.value=app?.workspaceId||v.settings.workspaceId||ws()?.id||state.workspaces[0]?.id;
  const close=()=>{if(busy){error.textContent=tr('请等待安装或保存完成。','Wait for installation or saving to finish.');return;}sessionStorage.setItem('rundesk-setup-dismissed','1');d.close();};
  d.append(el('div',{class:'dialog-head'},el('h2',{},app?rdText('应用运行检查'):rdText('开始使用 RunDesk')),button(rdText('关闭'),close,'quiet')),
   el('p',{class:'setup-intro'},tr('选择运行后端并检查环境。仅使用 Kun 时无需安装 Codex；两个后端可通过不同实例同时使用。','Choose a backend and check its environment. Kun does not require Codex; separate instances can use both backends.')));
  d.addEventListener('cancel',e=>{if(busy)e.preventDefault();else sessionStorage.setItem('rundesk-setup-dismissed','1');});
  if(!app)d.append(el('div',{class:'setup-context'},el('div',{},rdText('运行账户：'),el('code',{},v.user)),el('div',{},rdText('数据目录：'),el('code',{},v.data))));
  const grid=el('div',{class:'setup-grid'},el('label',{},tr('运行后端','Agent backend'),kind),el('label',{},rdText('工作项目'),project));
  const kunFields=el('div',{class:'setup-grid'},el('label',{},tr('Kun 模型 API 基础地址','Kun model API base URL'),endpoint),el('label',{},tr('密钥环境变量名（可留空）','Credential environment variable (optional)'),key));
  const codexFields=el('div',{class:'setup-grid'},el('label',{class:'setup-wide'},rdText('本机 Codex 路径'),path));
  if(!app){
   grid.append(el('label',{},rdText('默认模型'),model),button(rdText('添加工作目录'),async()=>{
    const name=prompt(rdText('项目名称'));if(!name)return;const dir=prompt(rdText('服务器上的绝对目录；留空自动创建'));if(dir===null)return;
    try{const w=await api('/workspaces',{method:'POST',body:{name,path:dir}});state.workspaces=await api('/workspaces');project.append(el('option',{value:w.id},w.name));project.value=w.id;$('#workspace').append(el('option',{value:w.id},w.name));await refreshEnvironment();}catch(e){error.textContent=e.message;}
   },'quiet'));
  }
  d.append(grid);if(!app)d.append(kunFields,codexFields);
  const environment=el('div',{class:'setup-checks',id:'setup-environment',role:'status'});
  const refreshEnvironment=async()=>{
   const r=await api('/setup/environment?instanceId='+encodeURIComponent(i.id)+'&workspaceId='+encodeURIComponent(project.value||''));
   environment.replaceChildren(el('p',{},tr('以下为已保存配置的本机依赖检查；模型推理尚未测试。','Local prerequisites for the saved configuration; model inference has not been tested.')),
    ...r.checks.map(c=>el('div',{class:'setup-check-row','data-state':c.status==='ready'?'ok':c.required?'error':'optional'},el('strong',{},c.id+(c.required?tr('（必需）',' (required)'):tr('（可选）',' (optional)'))),el('span',{},c.status),el('code',{},c.detail))));
  };
  d.append(environment,button(tr('重新检查环境','Recheck environment'),()=>refreshEnvironment().catch(e=>error.textContent=e.message),'quiet'));
  const checks=el('div',{class:'setup-checks'});d.append(checks);
  function renderChecks(){
   checks.replaceChildren();
   const rows=kind.value==='applications'?[['docker','Docker'],['workspace',rdText('工作目录')]]:kind.value==='kun'?[['kun',tr('Kun 版本','Kun version')],['workspace',rdText('工作目录')],['mcp',rdText('MCP 工具')]]:[['codex',rdText('本机 Codex')],['workspace',rdText('工作目录')],['models',rdText('模型与协议')],['account',rdText('账户认证')],['mcp',rdText('MCP 工具')]];
   if(cfg.allowBash&&kind.value==='kun')rows.push(['bash','Bash']);
   if(i.execution?.mode==='docker')rows.push(['docker','Docker']);
   for(const [check,label] of rows){
    const status=el('span',{class:'check-status'},rdText('未检查')),details=el('details',{hidden:true},el('summary',{},rdText('技术详情'))),row=el('div',{class:'setup-check-row'},el('strong',{},label),status);
    const b=button(rdText('检查'),async()=>{b.disabled=true;status.textContent=rdText('检查中…');try{const r=await api('/setup/check',{method:'POST',body:{kind:check,workspaceId:project.value,instanceId:i.id}});row.dataset.state=r.ok?'ok':'error';status.textContent=rdText(r.ok?'检查完成':'需要处理');details.hidden=false;details.replaceChildren(el('summary',{},rdText('技术详情')),el('pre',{},json(r.result||r.error)));if(r.demo)status.textContent=rdText('演示结果，未验证真实环境');}catch(e){status.textContent=rdText('检查失败');error.textContent=e.message;}finally{b.disabled=false;}});b.dataset.checkKind=check;row.append(b,details);checks.append(row);
   }
  }
  const install=el('details',{class:'setup-provider',id:'setup-install'});
  if(!app){
   const version=el('input',{id:'setup-install-version',placeholder:'X.Y.Z'}),consent=el('input',{id:'setup-install-confirm',type:'checkbox'}),output=el('pre',{id:'setup-install-log',role:'status'});
   const installButton=button(tr('安装指定版本','Install selected release'),async()=>{
    if(!consent.checked){error.textContent=tr('请勾选安装授权。','Select the installation checkbox.');return;}
    busy=true;installButton.disabled=true;error.textContent='';output.textContent=tr('安装中，最长 5 分钟。完成后显示日志；不会自动替换当前配置。','Installing, up to 5 minutes. Logs appear when finished; the active configuration is not changed automatically.');
    try{const r=await api('/setup/codex-install',{method:'POST',body:{version:version.value.trim(),confirm:true}});output.textContent=r.log+'\n'+(r.error||tr('安装完成。保存配置后生效。','Installed. Save configuration to use it.'));if(r.ok){path.value=r.codex;await refreshEnvironment();}}catch(e){error.textContent=e.message;output.textContent=tr('请求结果未确认时，请在请求诊断中核对原幂等回执；不要换版本重复安装。','If the outcome is unknown, inspect the original request receipt before starting another installation.');}finally{busy=false;installButton.disabled=false;}
   },'quiet');
   install.append(el('summary',{},tr('安装 Codex（可选）','Install Codex (optional)')),el('p',{},tr('需要服务账户已安装 Node.js 和 npm。安装到 RunDesk 数据目录，不修改系统全局安装。请选择精确版本；认证随后通过 codex login 或服务环境变量完成。','Requires Node.js and npm for the service account. Installs into the RunDesk data directory without modifying global packages. Choose an exact version, then authenticate with codex login or service environment variables.')),
    el('label',{},tr('Codex 版本','Codex version'),version),el('label',{},consent,tr('下载并安装所选 Codex 版本','Download and install the selected Codex version')),installButton,output,
    el('p',{},tr('也可以手工安装，再填写可执行文件路径：','Or install manually, then enter the executable path:')),el('code',{},'npm install -g @openai/codex@X.Y.Z'));
   d.append(install,el('p',{class:'help'},tr('Kun 使用 Chat Completions API；Codex 沿用自己的提供方配置。密钥只填写环境变量名称。保存后，新会话使用所选后端；旧会话保留原后端。','Kun uses Chat Completions; Codex keeps its provider configuration. Enter credential variable names, not secrets. New sessions use the saved backend; existing sessions retain their backend.')));
   if(window.RunDeskRuntimeSetup)d.append(window.RunDeskRuntimeSetup.panel(d));
   const save=async completed=>{
    if(busy)return;busy=true;error.textContent='';
    try{const applicationsOnly=kind.value==='applications';const runtime=applicationsOnly?null:{...cfg,kind:kind.value};if(kind.value==='kun')Object.assign(runtime,{endpoint:endpoint.value.trim(),model:model.value.trim(),apiKeyEnv:key.value.trim()});
     const r=await api('/setup',{method:'PUT',body:{codex:path.value,model:model.value,workspaceId:project.value,revision,instanceRevision,agentRuntime:runtime,applicationsOnly,completed}});
     revision=r.settings.revision;state.instances=await api('/instances');instanceRevision=state.instances.find(x=>x.id===i.id).revision;if(runtime)Object.assign(cfg,runtime);
     localStorage.setItem('rundesk-assistant-workspace',project.value);$('#workspace').value=project.value;updateWorkspaceLabel();await refreshEnvironment();
     if(r.reloadWarning){error.textContent=r.reloadWarning;return;}toast(rdText('配置已保存'));if(completed)d.close();
    }catch(e){error.textContent=e.message;}finally{busy=false;}
   };
   d.append(error,el('div',{class:'setup-actions'},button(rdText('保存配置'),()=>save(false),'quiet'),button(rdText('完成设置'),()=>save(true),'primary')));
  }else d.append(error);
  const selectBackend=()=>{model.disabled=kind.value==='applications';kunFields.hidden=kind.value!=='kun';codexFields.hidden=kind.value!=='codex';install.hidden=kind.value!=='codex';renderChecks();};
  kind.addEventListener('change',selectBackend);project.addEventListener('change',()=>refreshEnvironment().catch(e=>error.textContent=e.message));selectBackend();
  document.body.append(d);d.addEventListener('close',()=>d.remove(),{once:true});d.showModal();await refreshEnvironment();
 }
 async function template(app){
  const d=el('dialog',{class:'setup-dialog'}),version=el('input',{id:'template-version',required:true,placeholder:'X.Y.Z'}),error=el('p',{class:'error',role:'alert'}),submit=el('button',{type:'submit',class:'primary'},rdText('创建构建草稿'));
  const form=el('form',{},el('div',{class:'dialog-head'},el('h2',{},rdText('使用基础模板')),button(rdText('取消'),()=>d.close(),'quiet')),el('p',{class:'setup-intro'},rdText('使用项目内置 Dockerfile，安装指定版本的 Codex 和常用工具。先创建草稿，再查看并启动构建。')),el('label',{},rdText('Codex 版本'),version),error,el('div',{class:'setup-actions'},submit));
  form.onsubmit=async e=>{e.preventDefault();submit.disabled=true;error.textContent='';try{
   const response=await fetch('/api/v1/setup/docker-template?version='+encodeURIComponent(version.value),{credentials:'same-origin'});
   if(!response.ok)throw Error(await response.text());const blob=await response.blob();
   const data=new FormData();data.append('file',blob,'rundesk-codex.zip');data.append('name','RunDesk Codex '+version.value);data.append('dockerfile','Dockerfile');data.append('timeoutMinutes','30');data.append('noCache','false');
   const result=await api(`/instances/${app.instanceId}/builds`,{method:'POST',body:data});d.close();await RunDeskBuilds.open(app,result.id);
  }catch(e){error.textContent=e.message;}finally{submit.disabled=false;}};
  d.append(form);document.body.append(d);d.addEventListener('close',()=>d.remove(),{once:true});d.showModal();version.focus();
 }

 return{open,maybeOpen,template};
})();
