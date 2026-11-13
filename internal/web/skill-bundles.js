"use strict";
function bundlePath(wid,iid,name,scope,suffix=""){
 return `/workspaces/${encodeURIComponent(wid)}/skill-bundles${name?"/"+encodeURIComponent(name):""}${suffix}?instanceId=${encodeURIComponent(iid)}&scope=${encodeURIComponent(scope)}`;
}
function downloadSkillBundle(wid,iid,name,scope){location.href="/api/v1"+bundlePath(wid,iid,name,scope,"/export");}
function skillBundleImporter(wid,iid){
 const folder=el("input",{id:"skill-directory-input",type:"file",webkitdirectory:true,multiple:true,class:"hidden"}),zip=el("input",{id:"skill-zip-input",type:"file",accept:".zip,application/zip",class:"hidden"});
 folder.onchange=()=>safe(()=>{if(folder.files.length)showSkillImport([...folder.files],"directory",wid,iid);folder.value="";});zip.onchange=()=>safe(()=>{if(zip.files.length)showSkillImport([...zip.files],"zip",wid,iid);zip.value="";});
 return el("div",{class:"skill-import-bar"},el("div",{},el("strong",{},rdText("完整技能目录")),el("p",{class:"help"},rdText("一起管理 SKILL.md、scripts、references 和模板。目录结构会完整保留。"))),el("div",{class:"actions"},button(rdText("导入文件夹"),()=>folder.click(),"primary"),button(rdText("导入 ZIP"),()=>zip.click())),folder,zip);
}
function showSkillImport(files,kind,wid,iid){
 const tr=(zh,en)=>document.documentElement.lang==="en"?en:zh;
 const directory=kind==="directory",guess=directory?files[0].webkitRelativePath.split("/")[0]:files[0].name.replace(/\.zip$/i,"");
 const name=el("input",{id:"skill-bundle-name",required:true,pattern:"[A-Za-z0-9_-]{1,64}",value:/^[A-Za-z0-9_-]{1,64}$/.test(guess)?guess:"",placeholder:rdText("例如 news-research")}),scope=el("select",{id:"skill-bundle-scope"},el("option",{value:"instance"},RunDeskEnvironments.scopeLabel()),el("option",{value:"project"},rdText("项目技能 · 共享目录")));
 const replace=el("input",{type:"checkbox",id:"skill-bundle-replace"}),error=el("p",{class:"error",role:"status"}),submit=el("button",{type:"submit",class:"primary",id:"skill-bundle-submit"},rdText("导入完整技能")),d=el("dialog",{class:"application-register skill-import-dialog"});
 const total=files.reduce((n,f)=>n+f.size,0),list=el("ul",{class:"import-file-list"},...files.slice(0,15).map(f=>el("li",{},f.webkitRelativePath||f.name)));if(files.length>15)list.append(el("li",{},rdFormat("另有 ${0} 个文件",files.length-15)));
 let pending=null,busy=false,gate=false;
 const report=el("div",{class:"skill-center-report"}),acknowledge=el("input",{type:"checkbox",id:"skill-bundle-review-ack"}),reason=el("textarea",{id:"skill-bundle-review-reason",rows:2,maxlength:2000});
 const override=el("button",{type:"button",class:"primary",id:"skill-bundle-review-continue",disabled:true},tr("记录理由并继续导入","Record reason and continue importing"));
 const review=el("section",{class:"skill-center-install-review",hidden:true},el("p",{class:"skill-center-notice"},tr("导入前检查需要人工复核。请查看报告，再决定是否继续。","The inspection requires review before import. Read the report before deciding whether to continue.")),report,el("label",{class:"check-inline"},acknowledge,tr("我已查看检查结果，并确认导入这些文件。","I reviewed the inspection results and confirm importing these files.")),el("label",{},tr("复核理由","Review reason"),reason),override);
 const updateReview=()=>{override.disabled=busy||!gate||!acknowledge.checked||!reason.value.trim();};
 acknowledge.onchange=updateReview;reason.oninput=updateReview;
 const form=el("form",{},el("div",{class:"dialog-head"},el("h2",{},directory?rdText("导入技能文件夹"):rdText("导入技能 ZIP")),button(rdText("关闭"),()=>d.close(),"quiet")),el("p",{class:"help"},rdFormat("配置对象：${0} · ${1} 个${2} · ${3}",contextTitle(),files.length,directory?rdText("文件"):rdText("压缩包"),prettySize(total))),el("label",{},rdText("技能目录名称"),name),el("label",{},rdText("保存范围"),scope),el("details",{},el("summary",{},rdText("已选择的文件")),list),el("label",{class:"check-inline"},replace,rdText("替换同名技能（原目录会完整备份）")),el("p",{class:"help"},rdText("每次导入一个技能，根目录需包含 SKILL.md；最多 1000 个文件、解压后 32 MiB。")),error,submit,review);
 async function apply(accepted){
  if(busy||!form.reportValidity()||accepted&&(!gate||!acknowledge.checked||!reason.value.trim())||!accepted&&gate)return;
  if(instance()?.id!==iid||ws()?.id!==wid){error.textContent=tr("当前工作区或运行环境已经改变，请关闭后重新导入。","The workspace or environment changed. Close this dialog and start again.");return;}
  if(!pending){let body,headers={};if(directory){body=new FormData();for(const f of files)body.append(f.webkitRelativePath||f.name,f,f.name);}else{body=files[0];headers["Content-Type"]="application/zip";}pending={body,headers,path:bundlePath(wid,iid,"",scope.value)+"&name="+encodeURIComponent(name.value)+"&replace="+(replace.checked?"1":"0")};}
  busy=true;submit.disabled=true;updateReview();error.textContent="";
  try{
   const p=pending.path+(accepted?"&acknowledge=1&reason="+encodeURIComponent(reason.value.trim()):"");
   const result=await api(p,{method:"POST",body:pending.body,headers:pending.headers});
   if(d.open){d.close();toast(rdFormat("已导入 ${0} 个文件",result.files.length)+(result.backupPath?rdText("；原目录已备份"):""));if(instance()?.id===iid&&ws()?.id===wid)await renderSettings();}
  }catch(e){
   if(!d.open)return;error.textContent=e.message;
   if(e.code==="skill_review_required"){
    gate=true;review.hidden=false;submit.hidden=true;acknowledge.checked=false;
    name.disabled=true;scope.disabled=true;replace.disabled=true;
    const value=e.details?.report||e.details?.scan;
    if(window.RunDeskSkillCenter?.renderReport)RunDeskSkillCenter.renderReport(report,value);
    else report.replaceChildren(el("h3",{},tr("检查报告","Inspection report")),el("p",{class:"help"},tr("未发现规则命中不代表安全，请核对检查范围和未检查项。","A report with no rule findings does not guarantee safety. Check coverage and uninspected files.")),el("pre",{},value?JSON.stringify(value,null,2):tr("服务未返回报告，请关闭后重试。","The service did not return a report. Close this dialog and retry.")));
   }else if(!gate)pending=null;
  }finally{busy=false;submit.disabled=gate;updateReview();}
 }
 form.onsubmit=event=>{event.preventDefault();return apply(false);};override.onclick=()=>safe(()=>apply(true));
 d.append(form);document.body.append(d);d.addEventListener("close",()=>d.remove(),{once:true});d.showModal();
}
async function openSkillDirectory(wid,iid,name,scope){
 const data=await api(bundlePath(wid,iid,name,scope)),d=el("dialog",{class:"skill-directory-dialog"}),preview=el("div",{class:"skill-file-preview"},el("p",{class:"help"},rdText("选择左侧文件查看内容。"))),list=el("nav",{class:"skill-file-list","aria-label":rdText("技能目录文件")});
 let selection=0;
 for(const f of data.files){const b=button("",async()=>{const current=++selection;setLoading(preview);const v=await api(bundlePath(wid,iid,name,scope,"/file")+"&path="+encodeURIComponent(f.path));if(!d.open||current!==selection)return;for(const node of list.children)node.classList.toggle("selected",node===b);preview.replaceChildren(el("h3",{},v.path),v.binary?el("p",{class:"help"},rdText("二进制文件，完整内容包含在导出的 ZIP 中。")):el("pre",{},v.text),v.truncated?el("p",{class:"help"},rdText("预览显示前 256 KiB；导出包含完整文件。")):null);},"skill-file");b.append(el("span",{},f.path),el("small",{},prettySize(f.size)+(f.executable?rdText(" · 可执行"):"")));list.append(b);}
 d.append(el("div",{class:"dialog-head"},el("div",{},el("p",{class:"eyebrow"},"SKILL DIRECTORY"),el("h2",{},name),el("p",{class:"help"},rdFormat("${0} 个文件 · ${1} · ${2}",data.files.length,prettySize(data.size),scope==="project"?rdText("项目技能"):contextTitle()))),button(rdText("关闭"),()=>d.close(),"quiet")),el("div",{class:"skill-directory-body"},list,preview),el("div",{class:"skill-directory-footer"},button(rdText("导出完整 ZIP"),()=>downloadSkillBundle(wid,iid,name,scope),"primary")));
 document.body.append(d);d.addEventListener("close",()=>d.remove(),{once:true});d.showModal();
}
