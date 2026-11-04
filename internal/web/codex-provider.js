"use strict";
// Codex provider settings belong to the selected environment, independently of
// the Kun maintenance model. Never read or store a credential value in the UI.
window.RunDeskCodexProvider = (() => {
  const tr=(zh,en)=>document.documentElement.lang==='en'?en:zh;
  const enc=encodeURIComponent;
  const defaultsPath='/setup/codex-provider';
  function controls(prefix,value={}) {
    const base=el('input',{id:prefix+'-url',type:'url',required:true,value:value.baseUrl||'',placeholder:'http://host.docker.internal:8000/v1'});
    const model=el('input',{id:prefix+'-model',required:true,value:value.model||'',maxlength:160});
    const key=el('input',{id:prefix+'-key',value:value.apiKeyEnv||'',placeholder:tr('可留空；只填环境变量名称','Optional; environment variable name only'),autocomplete:'off'});
    return {nodes:[el('label',{},tr('Responses API 地址','Responses API base URL'),base),el('label',{},tr('模型 ID','Model ID'),model),el('label',{},tr('密钥环境变量名','Credential environment variable'),key)],value:()=>({baseUrl:base.value.trim(),model:model.value.trim(),apiKeyEnv:key.value.trim()})};
  }
  function settings({instanceId,workspaceId}) {
    const host=el('section',{class:'card codex-provider-settings'},el('h3',{},tr('Codex 模型服务','Codex model service')));
    host.append(el('p',{class:'help'},tr('为当前 Agent 与工作区配置模型。这里与初始化向导中的 Kun 维护模型分别保存。','Configure this Agent and workspace. These settings are separate from the Kun maintenance model in setup.')));
    const current=el('div'),defaults=el('details',{},el('summary',{},tr('新建 Docker Agent 的默认模型服务','Default model service for new Docker Agents'))),defaultBody=el('div');defaults.append(defaultBody);host.append(current,defaults);
    if(workspaceId)loadEditor(current,'/instances/'+enc(instanceId)+'/codex-provider?workspaceId='+enc(workspaceId),false);
    else current.append(el('p',{class:'help'},tr('请选择工作区后配置当前环境。','Select a workspace to configure this environment.')));
    loadEditor(defaultBody,defaultsPath,true);
    return host;
  }
  async function loadEditor(host,path,isDefault) {
    host.replaceChildren(el('p',{},tr('正在读取…','Loading…')));
    try {
      let saved=await api(path);
      if(!host.isConnected)return;
      const form=controls(isDefault?'codex-default-provider':'codex-environment-provider',saved),notice=el('p',{class:'help',role:'status'}),error=el('p',{class:'error',role:'alert'});
      const save=el('button',{type:'button',class:'primary'},isDefault?tr('保存新 Agent 默认服务','Save default for new Agents'):tr('保存当前环境','Save this environment'));
      const test=el('button',{type:'button',class:'quiet',disabled:!saved.configured},tr('测试已保存的配置','Test saved configuration'));
      const explain=()=>{notice.textContent=isDefault?(saved.configured?tr('新 Agent 可选择继承此服务；修改默认值不会改写已有环境。','New Agents can inherit this service. Changing the default does not update existing environments.'):tr('尚未设置默认服务；新 Agent 将沿用镜像中的 Codex 配置。','No default service is configured. New Agents use their image’s Codex settings.')):saved.configured?tr('配置已保存；新会话使用此模型。测试将从实际运行环境检查服务。','Configuration saved. New conversations use this model. Testing checks the service from the actual runtime.'):tr('当前沿用原有 Codex 配置；填写并保存后切换提供方。','This environment uses its existing Codex settings. Save these fields to switch provider.');if(!isDefault&&saved.scope==='instance')notice.textContent+=' '+tr('本机实例的配置由其工作区共同使用。','A local instance shares this configuration across its workspaces.');if(saved.state==='recreate_required')notice.textContent+=' '+tr('凭据已变更，需要显式重建此环境后生效。','Credentials changed; explicitly recreate this environment to apply them.');if(saved.warnings?.length)notice.textContent+=' '+saved.warnings.join(' ');};explain();
      host.replaceChildren(...form.nodes,el('p',{class:'help'},tr('Docker 内的 127.0.0.1 指向容器自身。同机宿主服务通常使用 host.docker.internal 或可访问的服务器地址。服务需要支持 Codex 的 Responses API 和工具调用。','Inside Docker, 127.0.0.1 refers to that container. For a service on the host, use host.docker.internal or a reachable server address. The service must support Responses API and tools.')),el('div',{class:'actions'},save,...(!isDefault?[test]:[])),notice,error);
      save.onclick=async()=>{if(!host.isConnected)return;save.disabled=test.disabled=true;error.textContent='';try{saved=await api(path,{method:'PUT',body:{...form.value(),revision:saved.revision}});if(host.isConnected)explain();}catch(e){if(host.isConnected)error.textContent=e.message;}finally{save.disabled=false;test.disabled=!saved.configured;}};
      test.onclick=async()=>{test.disabled=save.disabled=true;error.textContent='';notice.textContent=tr('正在从实际运行环境验证…','Checking from the actual runtime…');try{const [base,query]=path.split('?');const value=await api(base+'/test?'+query,{method:'POST',body:{}});if(!host.isConnected)return;notice.textContent=[value.scope==='docker-container'?tr('验证位置：Docker 容器','Checked in Docker container'):tr('验证位置：RunDesk 主机','Checked on RunDesk host'),'Responses: '+(value.inferenceOK?'✓':'—'),tr('工具调用：','Tools: ')+(value.toolsOK?'✓':'—')].join(' · ');if(value.error)error.textContent=value.error;}catch(e){if(host.isConnected)error.textContent=e.message;}finally{save.disabled=false;test.disabled=!saved.configured;}};
    }catch(e){if(host.isConnected)host.replaceChildren(el('p',{class:'error'},e.message),button(tr('重试读取','Retry'),()=>loadEditor(host,path,isDefault),'quiet'));}
  }
  async function launchFields(form, {saved}={}) {
    const host=el('section',{class:'codex-launch-provider'}),submit=form.querySelector('button[type=submit]');if(submit)form.insertBefore(host,submit);else form.append(host);
    const defaults=await api(defaultsPath);
    const mode=el('select',{id:'center-launch-provider',disabled:!!saved},...((defaults.configured||saved?.providerMode==='inherit-default')?[el('option',{value:'inherit-default'},tr('使用默认 Codex 模型服务','Use default Codex model service'))]:[]),el('option',{value:'existing'},tr('沿用镜像现有 Codex 配置','Keep existing image configuration')));
    if(saved) mode.value=saved.providerMode||'existing';
    const summary=el('p',{class:'help'});
    const describe=()=>{if(saved&&saved.providerMode==='inherit-default'&&saved.providerRevision!==defaults.revision){summary.textContent=tr('此重试保留原请求的默认配置版本，不使用后来修改的默认服务。','This retry retains the original default configuration revision.');return;}summary.textContent=mode.value==='inherit-default'?[defaults.model,defaults.baseUrl].filter(Boolean).join(' · '):tr('创建后可在 Agent 的“模型与认证”中配置。初始化中的 Kun 模型不会自动用于 Codex。','Configure it later in Agent Model & authentication. The Kun setup model is not automatically used by Codex.');};mode.onchange=describe;describe();
    host.append(el('label',{},tr('Codex 模型服务','Codex model service'),mode),summary);
    return {value:()=>saved?{providerMode:saved.providerMode||'existing',...(saved.providerMode==='inherit-default'?{providerRevision:saved.providerRevision}:{})}:mode.value==='inherit-default'?{providerMode:'inherit-default',providerRevision:defaults.revision}:{providerMode:'existing'}};
  }
  return {settings,launchFields};
})();
