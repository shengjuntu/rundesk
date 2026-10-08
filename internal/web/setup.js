"use strict";
window.RunDeskSetup=(()=>{
 async function maybeOpen(){const v=await api('/setup');if(v.firstRun&&!sessionStorage.getItem('rundesk-setup-dismissed'))await open();}
 async function open(app){
  const v=await api('/setup'),d=el('dialog',{class:'setup-dialog'}),error=el('p',{class:'error',role:'alert'});let revision=v.settings.revision;
  const path=el('input',{value:v.codex,id:'setup-codex'}),model=el('input',{value:v.model,id:'setup-model',placeholder:rdText('沿用 Codex 配置')}),project=el('select',{id:'setup-workspace'},...state.workspaces.map(w=>el('option',{value:w.id},w.name)));
  project.value=app?.workspaceId||v.settings.workspaceId||ws()?.id||state.workspaces[0]?.id;
  const title=app?rdText('应用运行检查'):rdText('开始使用 RunDesk');
  d.append(el('div',{class:'dialog-head'},el('h2',{},title),button(rdText('关闭'),()=>{sessionStorage.setItem('rundesk-setup-dismissed','1');d.close();},'quiet')),el('p',{class:'setup-intro'},app?rdText('逐项检查实际运行环境；检查通过不代表已完成模型推理。'):rdText('沿用已有配置，补齐缺失项。Docker 可稍后设置。')));
  if(!app)d.append(el('div',{class:'setup-context'},el('div',{},rdText('运行账户：'),el('code',{},v.user)),el('div',{},rdText('数据目录：'),el('code',{},v.data)),el('div',{},rdText('Codex 配置目录：'),el('code',{},v.codexHome)),el('div',{},v.protected?rdText('管理员访问已保护'):rdText('仅本机访问；远程部署需设置 RUNDESK_TOKEN'))));
  const grid=el('div',{class:'setup-grid'},el('label',{},rdText('工作项目'),project));
  if(!app)grid.append(el('label',{},rdText('默认模型'),model),el('label',{class:'setup-wide'},rdText('本机 Codex 路径'),path),button(rdText('添加工作目录'),async()=>{
   const name=prompt(rdText('项目名称'));if(!name)return;const dir=prompt(rdText('服务器上的绝对目录；留空自动创建'));if(dir===null)return;
   try{const w=await api('/workspaces',{method:'POST',body:{name,path:dir}});state.workspaces=await api('/workspaces');project.append(el('option',{value:w.id},w.name));project.value=w.id;const o=el('option',{value:w.id},w.name);$('#workspace').append(o);}catch(e){error.textContent=e.message;}
  },'quiet'));
  d.append(grid);
  const checks=el('div',{class:'setup-checks'});d.append(checks);
  for(const [kind,label] of [['codex',rdText('本机 Codex')],['workspace',rdText('工作目录')],['models',rdText('模型与协议')],['account',rdText('账户认证')],['mcp',rdText('MCP 工具')],['docker',rdText('Docker（可选）')]]){
   if(app&&kind==='codex')continue;
   const status=el('span',{class:'check-status'},rdText('未检查')),details=el('details',{hidden:true},el('summary',{},rdText('技术详情'))),row=el('div',{class:'setup-check-row'},el('strong',{},label),status);
   const b=button(rdText('检查'),async()=>{b.disabled=true;status.textContent=rdText('检查中…');error.textContent='';try{details.hidden=false;const r=await api('/setup/check',{method:'POST',body:{kind,workspaceId:project.value,instanceId:app?.instanceId||'default'}});row.dataset.state=r.ok?'ok':'error';status.textContent=rdText(r.ok?rdText('检查完成'):rdText('需要处理'));details.replaceChildren(el('summary',{},rdText('技术详情')),el('pre',{},json(r.result||r.error)),el('small',{},r.checkedAt));if(r.ok&&kind==='models'){status.textContent=rdText('协议可用；推理未测试');}if(r.ok&&kind==='account'){const a=r.result;status.textContent=rdText(a?.account?rdText('已读取账户'):rdText('已读取；请核对认证'));}if(r.ok&&kind==='mcp')status.textContent=rdText('已读取；请核对工具状态');if(r.demo&&['codex','models','account','mcp'].includes(kind))status.textContent=rdText('演示结果，未验证真实环境');}catch(e){row.dataset.state='error';status.textContent=rdText('检查失败');details.replaceChildren(el('summary',{},rdText('技术详情')),el('pre',{},e.message));}finally{b.disabled=false;}});b.dataset.checkKind=kind;row.append(b,details);checks.append(row);
  }
  if(!app){
   d.append(el('p',{class:'setup-notice'},rdText('修改路径和模型后先保存，再检查。缺少 Codex 时可在服务账户下安装：npm install -g @openai/codex@指定版本。认证使用 codex login 或服务进程的环境变量。')));
   const base=el('input',{placeholder:'http://127.0.0.1:8000/v1'}),key=el('input',{placeholder:'OPENAI_API_KEY'});
   d.append(el('details',{class:'setup-provider'},el('summary',{},rdText('自定义模型服务（高级）')),el('p',{class:'help'},rdText('适用于兼容 Responses API 的服务。默认沿用现有 Codex 配置；保存会切换通用助手的模型提供方。密钥通过服务进程环境变量提供，修改后需要重启服务。')),el('div',{class:'setup-grid'},el('label',{},rdText('服务地址'),base),el('label',{},rdText('密钥环境变量名（可留空）'),key)),button(rdText('保存模型服务'),async()=>{try{await api('/setup/provider',{method:'POST',body:{workspaceId:project.value,baseUrl:base.value,envKey:key.value,model:model.value}});toast(rdText('模型服务已保存'));}catch(e){error.textContent=e.message;}},'quiet')));
   const save=async completed=>{error.textContent='';try{const r=await api('/setup',{method:'PUT',body:{codex:path.value,model:model.value,workspaceId:project.value,revision,completed}});revision=r.settings.revision;localStorage.setItem('rundesk-assistant-workspace',project.value);$('#workspace').value=project.value;state.instances=await api('/instances');updateWorkspaceLabel();if(r.reloadWarning){error.textContent=r.reloadWarning;return;}toast(rdText('配置已保存'));if(completed)d.close();}catch(e){error.textContent=e.message;}};
   d.append(error,el('div',{class:'setup-actions'},button(rdText('保存配置'),()=>save(false),'quiet'),button(rdText('完成设置'),()=>save(true),'primary')));
  }else d.append(error);
  document.body.append(d);d.addEventListener('close',()=>d.remove(),{once:true});d.showModal();if(!app)for(const kind of ['codex','workspace','docker'])checks.querySelector('[data-check-kind="'+kind+'"]')?.click();
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
