"use strict";
function bundlePath(wid,iid,name,scope,suffix=""){
 return `/workspaces/${encodeURIComponent(wid)}/skill-bundles${name?"/"+encodeURIComponent(name):""}${suffix}?instanceId=${encodeURIComponent(iid)}&scope=${encodeURIComponent(scope)}`;
}
function downloadSkillBundle(wid,iid,name,scope){location.href="/api/v1"+bundlePath(wid,iid,name,scope,"/export");}
function skillBundleImporter(wid,iid){
 const folder=el("input",{id:"skill-directory-input",type:"file",webkitdirectory:true,multiple:true,class:"hidden"}),zip=el("input",{id:"skill-zip-input",type:"file",accept:".zip,application/zip",class:"hidden"});
 folder.onchange=()=>safe(()=>{if(folder.files.length)showSkillImport([...folder.files],"directory",wid,iid);folder.value="";});zip.onchange=()=>safe(()=>{if(zip.files.length)showSkillImport([...zip.files],"zip",wid,iid);zip.value="";});
 return el("div",{class:"skill-import-bar"},el("div",{},el("strong",{},"完整技能目录"),el("p",{class:"help"},"一起管理 SKILL.md、scripts、references 和模板。目录结构会完整保留。")),el("div",{class:"actions"},button("导入文件夹",()=>folder.click(),"primary"),button("导入 ZIP",()=>zip.click())),folder,zip);
}
function showSkillImport(files,kind,wid,iid){
 const directory=kind==="directory",guess=directory?files[0].webkitRelativePath.split("/")[0]:files[0].name.replace(/\.zip$/i,"");
 const name=el("input",{id:"skill-bundle-name",required:true,pattern:"[A-Za-z0-9_-]{1,64}",value:/^[A-Za-z0-9_-]{1,64}$/.test(guess)?guess:"",placeholder:"例如 news-research"}),scope=el("select",{id:"skill-bundle-scope"},el("option",{value:"instance"},RunDeskEnvironments.scopeLabel()),el("option",{value:"project"},"项目技能 · 共享目录"));
 const replace=el("input",{type:"checkbox",id:"skill-bundle-replace"}),error=el("p",{class:"error",role:"status"}),submit=el("button",{type:"submit",class:"primary",id:"skill-bundle-submit"},"导入完整技能"),d=el("dialog",{class:"application-register skill-import-dialog"});
 const total=files.reduce((n,f)=>n+f.size,0),list=el("ul",{class:"import-file-list"},...files.slice(0,15).map(f=>el("li",{},f.webkitRelativePath||f.name)));if(files.length>15)list.append(el("li",{},`另有 ${files.length-15} 个文件`));
 const form=el("form",{},el("div",{class:"dialog-head"},el("h2",{},directory?"导入技能文件夹":"导入技能 ZIP"),button("关闭",()=>d.close(),"quiet")),el("p",{class:"help"},`配置对象：${contextTitle()} · ${files.length} 个${directory?"文件":"压缩包"} · ${prettySize(total)}`),el("label",{},"技能目录名称",name),el("label",{},"保存范围",scope),el("details",{},el("summary",{},"已选择的文件"),list),el("label",{class:"check-inline"},replace,"替换同名技能（原目录会完整备份）"),el("p",{class:"help"},"每次导入一个技能，根目录需包含 SKILL.md；最多 1000 个文件、解压后 32 MiB。"),error,submit);
 form.onsubmit=async event=>{event.preventDefault();submit.disabled=true;error.textContent="";try{
   let body,headers={};if(directory){body=new FormData();for(const f of files)body.append(f.webkitRelativePath||f.name,f,f.name);}else{body=files[0];headers["Content-Type"]="application/zip";}
   const p=bundlePath(wid,iid,"",scope.value)+"&name="+encodeURIComponent(name.value)+"&replace="+(replace.checked?"1":"0");
   const result=await api(p,{method:"POST",body,headers});
   if(d.open){d.close();toast(`已导入 ${result.files.length} 个文件`+(result.backupPath?"；原目录已备份":""));if(instance()?.id===iid&&ws()?.id===wid)await renderSettings();}
  }catch(e){error.textContent=e.message;}finally{submit.disabled=false;}};
 d.append(form);document.body.append(d);d.addEventListener("close",()=>d.remove(),{once:true});d.showModal();
}
async function openSkillDirectory(wid,iid,name,scope){
 const data=await api(bundlePath(wid,iid,name,scope)),d=el("dialog",{class:"skill-directory-dialog"}),preview=el("div",{class:"skill-file-preview"},el("p",{class:"help"},"选择左侧文件查看内容。")),list=el("nav",{class:"skill-file-list","aria-label":"技能目录文件"});
 let selection=0;
 for(const f of data.files){const b=button("",async()=>{const current=++selection;setLoading(preview);const v=await api(bundlePath(wid,iid,name,scope,"/file")+"&path="+encodeURIComponent(f.path));if(!d.open||current!==selection)return;for(const node of list.children)node.classList.toggle("selected",node===b);preview.replaceChildren(el("h3",{},v.path),v.binary?el("p",{class:"help"},"二进制文件，完整内容包含在导出的 ZIP 中。"):el("pre",{},v.text),v.truncated?el("p",{class:"help"},"预览显示前 256 KiB；导出包含完整文件。"):null);},"skill-file");b.append(el("span",{},f.path),el("small",{},prettySize(f.size)+(f.executable?" · 可执行":"")));list.append(b);}
 d.append(el("div",{class:"dialog-head"},el("div",{},el("p",{class:"eyebrow"},"SKILL DIRECTORY"),el("h2",{},name),el("p",{class:"help"},`${data.files.length} 个文件 · ${prettySize(data.size)} · ${scope==="project"?"项目技能":contextTitle()}`)),button("关闭",()=>d.close(),"quiet")),el("div",{class:"skill-directory-body"},list,preview),el("div",{class:"skill-directory-footer"},button("导出完整 ZIP",()=>downloadSkillBundle(wid,iid,name,scope),"primary")));
 document.body.append(d);d.addEventListener("close",()=>d.remove(),{once:true});d.showModal();
}
