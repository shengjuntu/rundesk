"use strict";
window.RunDeskRuntimeSetup=(()=>{
 const tr=(zh,en)=>document.documentElement.lang==='en'?en:zh;
 const active=s=>['queued','running','canceling'].includes(s);
 const base='/instances/default/builds';
 function panel(dialog){
  const box=el('details',{class:'setup-provider',id:'setup-runtime-images'});
  const profile=el('select',{id:'runtime-image-profile'}),source=el('select',{id:'runtime-image-source'},
   el('option',{value:'build'},tr('本机构建','Build locally')),el('option',{value:'pull'},tr('从仓库拉取','Pull from registry')),el('option',{value:'local'},tr('检查已有镜像','Check local image')));
  source.value='build';
  const version=el('input',{id:'runtime-image-version',placeholder:'X.Y.Z'}),reference=el('input',{id:'runtime-image-reference'});
  const error=el('p',{class:'error',role:'alert'}),list=el('div',{}),detail=el('div',{}),status=el('p',{role:'status'}),log=el('pre',{id:'runtime-image-log',style:'max-height:260px;overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere'});
  let catalog=[],selected='',timer,closed=false,submitting=false,offset=0,logText='',epoch=0;
  const labels={queued:tr('排队中','Queued'),running:tr('准备中','Preparing'),canceling:tr('正在取消','Canceling'),canceled:tr('已取消','Canceled'),interrupted:tr('已中断，请检查后重试','Interrupted; inspect before retrying'),failed:tr('失败','Failed'),succeeded:tr('镜像就绪，应用认证待完成','Image ready; application authentication pending')};
  function select(v){selected=v.id;offset=0;logText='';++epoch;clearTimeout(timer);return refresh();}
  const prepare=button(tr('准备镜像','Prepare image'),async()=>{
   if(submitting)return;submitting=true;prepare.disabled=true;error.textContent='';
   try{const v=await api('/setup/runtime-images',{method:'POST',body:{profileId:profile.value,source:source.value,codexVersion:version.value.trim(),reference:reference.value.trim()}});await select(v);}
   catch(e){error.textContent=e.message;}finally{submitting=false;prepare.disabled=false;}
  },'primary');
  const check=button(tr('检查 Docker','Check Docker'),async()=>{check.disabled=true;try{const r=await api('/setup/check',{method:'POST',body:{kind:'docker'}});status.textContent=r.ok?tr('Docker 检查完成。','Docker check completed.'):r.error||tr('Docker 不可用。','Docker unavailable.');}catch(e){error.textContent=e.message;}finally{check.disabled=false;}},'quiet');
  box.append(el('summary',{},tr('应用 Docker 环境：在初始化时准备','Application Docker environments: prepare during setup')),
   el('p',{},tr('先准备可复用镜像，再连接 news2douyin。新闻研究默认 Exa + Agent-Reach，不安装 Wigolo 或浏览器。关闭向导不会停止后台任务。','Prepare a reusable image before connecting news2douyin. News research uses Exa + Agent-Reach without Wigolo or a browser. Closing this wizard does not stop background jobs.')),
   el('div',{class:'setup-grid'},el('label',{},tr('典型配置','Profile'),profile),el('label',{},tr('镜像来源','Image source'),source),el('label',{},tr('镜像内 Codex 精确版本','Exact Codex release in image'),version),el('label',{},tr('本机标签或远程镜像地址','Local tag or registry image reference'),reference)),
   el('p',{class:'help'},tr('拉取时填写已发布的真实镜像地址；目前没有内置公共镜像地址。镜像必须与所选配置和 Codex 版本匹配。无需在宿主机安装 Node、Python 或 Codex。','For pulls, enter an existing published image; no public image is configured by default. It must match this profile and Codex release. Host Node, Python and Codex are not required.')),
   el('div',{class:'actions'},check,prepare),error,status,list,detail,log);
  profile.addEventListener('change',()=>{if(source.value==='build')reference.value=catalog.find(p=>p.id===profile.value)?.reference||'';});
  async function refresh(){
   const generation=++epoch;clearTimeout(timer);
   try{
    const rows=(await api(base)).filter(v=>v.scope==='setup');if(closed||generation!==epoch)return;
    if(!selected&&rows.length)selected=rows[0].id;
    list.replaceChildren(...rows.slice(0,20).map(v=>button(v.name+' · '+(labels[v.status]||v.status),()=>select(v),'quiet')));
    const v=rows.find(v=>v.id===selected);
    if(v){
     const r=await api(base+'/'+v.id+'/log?after='+offset);if(closed||generation!==epoch)return;
     offset=r.next;logText+=r.text;log.textContent=logText;
     detail.replaceChildren(el('strong',{},labels[v.status]||v.status),el('p',{},el('code',{},v.reference)),el('p',{class:'help'},v.error||''));
     if(active(v.status))detail.append(button(tr('取消准备','Cancel preparation'),async()=>{try{await api(base+'/'+v.id+'/cancel',{method:'POST'});await refresh();}catch(e){error.textContent=e.message;}},'quiet'));
     if(v.status==='succeeded')detail.append(el('p',{},tr('将上面的镜像地址填入 news2douyin 的研究设置，再连接并完成该环境的模型认证。离线工具检查已通过，公网搜索和真实研究尚未验证。','Enter this image reference in the application research settings, connect, then authenticate that environment. Offline tools passed; live search and research remain untested.')),el('code',{},v.imageId));
     if(!active(v.status))detail.append(button(tr('删除记录','Delete record'),async()=>{try{await api(base+'/'+v.id,{method:'DELETE'});selected='';offset=0;logText='';await refresh();}catch(e){error.textContent=e.message;}},'quiet'));
     if(['failed','interrupted','canceled'].includes(v.status))detail.append(button(tr('用原配置重试','Retry same configuration'),()=>{profile.value=v.profileId;source.value=v.source;version.value=v.codexVersion;reference.value=v.reference;return prepare.click();},'quiet'));
     if(active(v.status)||r.text.length&&offset<4194304)timer=setTimeout(()=>refresh(),1500);
    }else {detail.replaceChildren();log.textContent='';}
   }catch(e){if(!closed)error.textContent=e.message;}
  }
  dialog.addEventListener('close',()=>{closed=true;++epoch;clearTimeout(timer);},{once:true});
  (async()=>{try{const c=await api('/setup/runtime-profiles');if(closed)return;catalog=c.items;profile.replaceChildren(...catalog.map(p=>el('option',{value:p.id},p.name)));profile.value=catalog[0]?.id||'';reference.value=catalog[0]?.reference||'';if(!c.supported){prepare.disabled=true;status.textContent=tr('此功能需要 Linux 本机 Docker Engine。','Requires a local Docker Engine on Linux.');}await refresh();}catch(e){error.textContent=e.message;}})();
  return box;
 }
 return{panel};
})();
