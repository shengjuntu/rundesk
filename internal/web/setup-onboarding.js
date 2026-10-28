"use strict";
window.RunDeskOnboarding = (() => {
  const tr = (zh, en) => document.documentElement.lang === "en" ? en : zh;
  const enc = encodeURIComponent, activeJob = value => ["queued", "running", "canceling", "starting", "waiting"].includes(value);
  const names = {kun:"Kun",codex:"Codex",docker:"Docker",node:"Node.js",npm:"npm",vllm:"vLLM","llama-server":"llama.cpp"};
  const steps = [
    ["llm", "寻找和配置 LLM", "Find and configure LLM", "发现模型，启用 Kun", "Discover models and enable Kun"],
    ["codex", "配置和安装 Codex", "Configure and install Codex", "复用已有安装或下载安装", "Reuse or install Codex"],
    ["docker", "准备 Docker 环境", "Prepare Docker environments", "下载镜像或本机构建", "Download images or build locally"]
  ];
  let current, preferredInstance = "";
  const statusLabels = () => ({ready:tr("已就绪","Ready"),listed:tr("发现模型，待测试","Models found; untested"),succeeded:tr("已完成","Completed"),failed:tr("需要处理","Needs attention"),running:tr("进行中","Running"),queued:tr("排队中","Queued"),interrupted:tr("已中断","Interrupted"),unavailable:tr("未发现","Not found"),error:tr("检测失败","Check failed"),candidate:tr("候选经验","Candidate"),verified:tr("已验证经验","Verified"),auth_required:tr("需要认证","Authentication required"),empty:tr("未返回模型","No models returned"),incompatible:tr("协议不匹配","Protocol mismatch"),unreachable:tr("未连接","Unreachable"),canceled:tr("已取消","Canceled"),canceling:tr("正在取消","Canceling"),completed:tr("已完成","Completed"),idle:tr("待配置","Not configured"),waiting:tr("等待处理","Waiting"),watching:tr("已开启失败自动修复","Automatic repair enabled"),revalidating:tr("正在复查模型能力","Rechecking model capabilities"),submitted:tr("已交给 Kun","Sent to Kun"),blocked:tr("自动交接需要处理","Handoff needs attention"),disabled:tr("已关闭自动修复","Automatic repair disabled"),inference_only:tr("推理通过，工具未通过","Inference passed; tools failed")});
  const badge = (value, text) => el("span", {class:"ob-status","data-state":value}, text || statusLabels()[value] || value || "—");
  const note = (zh, en) => el("p", {class:"ob-help"}, tr(zh, en));
  function card(title, ...children) { return el("section", {class:"ob-card"}, title ? el("div", {class:"ob-card-head"}, el("h2", {}, title)) : null, ...children); }
  function action(c, label, fn, cls = "quiet", attrs = {}) {
    let busy = false;
    const node = button(label, async () => {
      if (busy) return; busy = true; node.disabled = true; c.error.textContent = "";
      try { await fn(); } catch (error) { if (!c.closed) c.error.textContent = error.message; }
      finally { busy = false; if (node.isConnected) node.disabled = false; }
    }, cls);
    for (const [key, value] of Object.entries(attrs)) node.setAttribute(key, value);
    return node;
  }
  const requestID = () => Array.from(crypto.getRandomValues(new Uint8Array(12)), value => value.toString(16).padStart(2, "0")).join("");
  const sourceAuto = (c,kind,id) => c.autoRows.find(row=>row.kind===kind&&row.sourceId===id&&row.enabled);
  const pendingAuto = (c,kind,id) => !!sourceAuto(c,kind,id)||Object.values(c.autoAuthorizations).some(value=>value.kind===kind&&value.sourceId===id&&value.state!=="registered");
  function receipt(c, key) {
    if (!c.receipts[key]) { c.receipts[key] = requestID(); saveDraft(c); }
    return c.receipts[key];
  }
  function draft() { try { return JSON.parse(sessionStorage.getItem("rundesk-onboarding-draft") || "{}"); } catch { return {}; } }
  function saveDraft(c) {
    try { sessionStorage.setItem("rundesk-onboarding-draft", JSON.stringify({endpoint:c.endpoint,model:c.model,keyEnv:c.keyEnv,kunPath:c.kunPath,codexPath:c.codexPath,version:c.version,group:c.group,selected:[...c.selected],receipts:c.receipts,sourceDrafts:c.sourceDrafts,autoRepair:c.autoRepair,autoAuthorizations:c.autoAuthorizations,installAdoptRequestId:c.installAdoptRequestId,installAdopted:c.installAdopted,dockerMode:c.dockerMode,runtimeDrafts:c.runtimeDrafts,codexRuntime:c.codexRuntime})); } catch {}
  }
  function readyKun(c) { return !!c.kun?.ready; }
  function setStep(c, index) { c.step = Math.max(0, Math.min(steps.length - 1, index)); render(c); c.page.scrollTop = 0; }
  function updateSteps(c) {
    const ready = [readyKun(c),!!c.codexPath,c.builds.some(value => value.status === "succeeded")];
    for (const [index, node] of [...c.nav.children].entries()) {
      node.setAttribute("aria-current", index === c.step ? "step" : "false");
      node.dataset.state = ready[index] ? "ready" : "idle";
      node.querySelector(".ob-step-number").textContent = ready[index] ? "✓" : String(index + 1);
    }
  }
  function header(c, zh, en, descriptionZh, descriptionEn) {
    c.main.replaceChildren(el("p", {class:"ob-eyebrow"}, "RUNDESK SETUP · " + String(c.step + 1).padStart(2, "0")), el("h1", {id:"onboarding-title"}, tr(zh,en)), el("p", {class:"ob-lead"}, tr(descriptionZh,descriptionEn)));
  }
  function render(c) {
    if (c.closed) return; updateSteps(c); c.error.textContent = "";
    if (c.step === 0) { environment(c); kun(c); }
    if (c.step === 1) codex(c);
    if (c.step === 2) { docker(c); finish(c); }
    c.main.append(c.error);
    const footer = el("footer", {class:"ob-footer"}, c.step > 0 ? action(c,tr("上一步","Back"),()=>setStep(c,c.step-1)) : el("span"), el("span", {class:"ob-footer-note"}, tr("配置保存在这台 RunDesk 服务器上。","Configuration is saved on this RunDesk server.")));
    if (c.step < steps.length - 1) footer.append(action(c,tr("下一步","Next"),()=>setStep(c,c.step+1),"primary",{"data-ob-next":""}));
    else footer.append(c.completeButton);
    c.main.append(footer);
  }
  function environment(c) {
    header(c,"寻找和配置 LLM","Find and configure LLM","自动发现 GPU、vLLM、llama.cpp 和本机模型服务，也可以连接已有的远程服务。验证模型后，Kun 就能协助后续安装和构建。","Discover GPUs, vLLM, llama.cpp and local model services, or connect an existing remote service. Once the model is tested, Kun can help with installation and builds.");
    const data = c.discovery;
    if (!data) {
      c.main.append(card(tr("正在扫描本机环境","Scanning the local environment"), el("p", {role:"status",class:"ob-help"}, c.scanning ? tr("正在检查 GPU、可执行文件和本机监听的模型服务…","Checking GPUs, executables and local model services…") : tr("扫描尚未完成，可以重新检查。","The scan has not completed. You can retry.")), action(c,tr("重新扫描","Scan again"),()=>scan(c)))); return;
    }
    const p=data.platform || {}, gpuNames=(data.gpus||[]).map(value=>value.name).filter(Boolean);
    c.main.append(el("div", {class:"ob-metrics"},
      el("div", {class:"ob-metric"},el("span",{},tr("操作系统","Operating system")),el("strong",{},[p.os,p.arch].filter(Boolean).join(" / ")),el("small",{},(p.cpuCount||"—")+" CPU")),
      el("div", {class:"ob-metric"},el("span",{},tr("内存","Memory")),el("strong",{},p.memoryBytes?(p.memoryBytes/1073741824).toFixed(1)+" GB":"—"),el("small",{},tr("服务器可见物理内存","Memory visible to the server"))),
      el("div", {class:"ob-metric"},el("span",{},"GPU"),el("strong",{},gpuNames.length?gpuNames.length+tr(" 张"," detected"):tr("未发现","Not detected")),el("small",{},gpuNames[0]||tr("远程模型服务仍可使用","Remote model services can still be used")))));
    const tools=el("div",{class:"ob-list"},...(data.tools||[]).map(value=>el("div",{class:"ob-tool","data-tool":value.id},el("strong",{},names[value.id]||value.id),badge(value.status,value.status==="ready"?tr("已发现","Detected"):undefined),el("code",{},value.version||value.path||value.error||"—"))));
    const inventory=el("details",{class:"ob-inventory"},el("summary",{},tr("查看设备与运行环境","View hardware and runtime environment")),tools);
    if(data.gpus?.length)inventory.append(el("h3",{style:"margin-top:16px"},tr("GPU 详情","GPU details")),el("pre",{},data.gpus.map((value,index)=>["GPU "+index,value.name,value.memoryMiB?Math.round(value.memoryMiB/1024)+" GiB":"",value.driver?"driver "+value.driver:""].filter(Boolean).join(" · ")).join("\n")));
    if(data.warnings?.length)inventory.append(el("h3",{style:"margin-top:16px"},tr("扫描说明","Scan notes")),el("pre",{},data.warnings.join("\n")));
    c.main.append(inventory);
    const services=serviceList(c);
    services.querySelector(".ob-card-head").append(action(c,c.scanning?tr("扫描中…","Scanning…"):tr("重新扫描","Scan again"),()=>scan(c),"quiet",{id:"onboarding-rescan"}));
    c.main.append(services);
  }
  function serviceList(c) {
    const services=(c.discovery?.services||[]).filter(value=>!["unreachable","incompatible"].includes(value.status));
    const list=el("div",{},...services.map(value=>{
      const node=action(c,"",()=>{chooseService(c,value);render(c);const input=c.page.querySelector("#onboarding-endpoint");input?.focus();input?.scrollIntoView?.({block:"nearest"});},"ob-service"+(c.endpoint===value.endpoint?" selected":""),{"data-service-endpoint":value.endpoint,"aria-pressed":String(c.endpoint===value.endpoint)});
      node.append(el("span",{class:"ob-service-icon","aria-hidden":"true"},"◇"),el("span",{class:"ob-service-info"},el("strong",{},value.endpoint),el("small",{},(value.models||[]).map(model=>model.id).join(" · ")||value.error||tr("打开后配置认证与模型","Configure authentication and model"))),badge(value.authRequired?"auth_required":value.status));return node;
    }));
    if(!services.length)list.append(el("div",{class:"ob-muted-card"},tr("还没有发现可列出模型的服务。可以启动 vLLM、llama.cpp，或填写已有服务地址。","No service with a model list was found. Start vLLM or llama.cpp, or enter an existing service address.")));
    return card(tr("发现的模型服务","Discovered model services"),list,note("发现模型列表后，还要验证推理与工具调用，才能启用 Kun 修复能力。","After discovering a model list, test inference and tool calls before enabling Kun repairs."));
  }
  function chooseService(c,service) { c.endpoint=service.endpoint;c.model=service.models?.[0]?.id||c.model;saveDraft(c); }
  async function scan(c,endpoint) {
    if(c.scanning)return;c.scanning=true;
    if(c.step===0)render(c);
    try {
      c.discovery=await api("/setup/discovery",endpoint?{method:"POST",body:{endpoints:[endpoint],...(c.keyEnv?{apiKeyEnv:c.keyEnv}:{})}}:{});
      if(!c.closed){
        const local=(c.discovery.services||[]).find(value=>value.status==="listed"&&value.models?.length);
        if(!c.endpoint&&local)chooseService(c,local);
        if(endpoint){const normalized=endpoint.replace(/\/+$/,"").replace(/\/v1$/,"")+"/v1",requested=(c.discovery.services||[]).find(value=>value.endpoint===normalized&&value.models?.length);if(requested)chooseService(c,requested);}
        const installed=(c.discovery.tools||[]).find(value=>value.id==="codex"&&value.status==="ready");
        if(!c.codexPath&&installed)c.codexPath=installed.path;
        const worker=(c.discovery.tools||[]).find(value=>value.id==="kun"&&value.status==="ready");
        if(!c.kunPath&&worker)c.kunPath=worker.path;
        updateSteps(c);
        const path=c.page.querySelector("#onboarding-codex-path");if(path&&!path.value)path.value=c.codexPath;
        const status=c.page.querySelector("#onboarding-codex-status");if(status&&installed){status.dataset.state="ready";status.textContent=installed.version||tr("检测到可用安装","Installation found");}
      }
    } finally {c.scanning=false;if(!c.closed&&c.step===0)render(c);}
  }
  function kun(c) {
    if(c.kun?.ready)c.main.append(el("div",{class:"ob-notice",role:"status"},tr("Kun 已通过 worker 协议、模型推理与工具调用检查，可以执行修复任务。","Kun passed the worker protocol, inference and tool-call checks and can run repair tasks.")));
    const endpoint=el("input",{id:"onboarding-endpoint",value:c.endpoint,placeholder:"http://127.0.0.1:8000/v1",autocomplete:"url"});
    const model=el("input",{id:"onboarding-model",value:c.model,placeholder:tr("服务返回的模型 ID","Model ID returned by the service"),list:"onboarding-models"});
    const key=el("input",{id:"onboarding-key-env",value:c.keyEnv,placeholder:"OPENAI_API_KEY",autocomplete:"off"});
    const kunPath=el("input",{id:"onboarding-kun-path",value:c.kunPath,placeholder:tr("RunDesk 同目录的 kun，或填写绝对路径","Adjacent kun executable, or an absolute path")});
    const bind=(node,key)=>node.oninput=()=>{c[key]=node.value.trim();saveDraft(c);};bind(endpoint,"endpoint");bind(model,"model");bind(key,"keyEnv");bind(kunPath,"kunPath");
    const results=el("div",{id:"onboarding-model-result",role:"status"});
    const test=action(c,tr("测试并启用 Kun","Test and enable Kun"),async()=>{
      results.replaceChildren(note("正在实际测试模型推理和工具调用…","Testing actual inference and tool calls…"));
      c.modelTest=await api("/setup/model-test",{method:"POST",body:{endpoint:c.endpoint,model:c.model,...(c.keyEnv?{apiKeyEnv:c.keyEnv}:{})}});
      await saveKun(c);render(c);
      if(!c.modelTest.inferenceOK||!c.modelTest.toolsOK)c.error.textContent=c.modelTest.error||tr("模型需要同时通过推理与工具调用测试。","The model must pass both inference and tool-call tests.");
    },"primary",{id:"onboarding-enable-kun"});
    const config=card(tr("模型连接","Model connection"),el("div",{class:"ob-grid"},el("label",{class:"ob-wide"},tr("Chat Completions API 地址","Chat Completions API base URL"),endpoint),el("label",{},tr("模型","Model"),model),el("label",{},tr("密钥环境变量名（可留空）","Credential environment variable (optional)"),key)),el("datalist",{id:"onboarding-models"},...(c.discovery?.services||[]).flatMap(value=>(value.models||[]).map(item=>el("option",{value:item.id})))),note("地址与环境变量属于服务器。无需认证可留空；需要密钥时，填写已在 RunDesk 服务进程中设置的变量名。","Addresses and environment variables belong to the server. Leave the credential field empty for unauthenticated services; otherwise enter a variable already set for the RunDesk process."),el("details",{},el("summary",{},tr("Kun 可执行文件","Kun executable")),el("label",{},tr("服务器上的 Kun 路径","Kun path on the server"),kunPath),note("将与你当前 RunDesk 兼容的独立 Kun 程序放在 RunDesk 同目录，或在这里指定路径。缺少程序时仍可保存模型配置。","Place a compatible standalone Kun executable next to RunDesk, or specify its path here. The model configuration can be saved before the executable is available.")),el("div",{class:"ob-actions"},test,action(c,tr("探测此服务的模型列表","Discover models at this address"),()=>scan(c,c.endpoint)),action(c,tr("保存配置草稿","Save configuration draft"),async()=>{await saveKun(c);render(c);})),results);
    if(c.modelTest||c.kun?.modelTest){const v=c.modelTest||c.kun.modelTest;results.append(el("p",{class:"ob-inline"},badge(v.inferenceOK?"ready":"failed",tr("模型推理","Inference")),badge(v.toolsOK?"ready":"failed",tr("工具调用","Tool calls"))),v.error?el("p",{class:"ob-error"},v.error):null);}
    const stateCard=card(tr("Kun 状态","Kun status"),el("div",{class:"ob-inline"},badge(c.kun?.ready?"ready":c.kun?.available?"idle":"unavailable",c.kun?.ready?tr("可以执行修复","Ready for repairs"):c.kun?.available?tr("模型尚未通过完整测试","Model checks incomplete"):tr("Kun worker 尚不可用","Kun worker unavailable"))),note("此配置使用独立的初始化 Kun 实例，后续也可以继续用于普通任务。","This uses a separate initialization Kun instance that can also run regular tasks."));
    if(c.kun?.error)stateCard.append(el("p",{class:"ob-error"},c.kun.error));
    if(c.kun?.worker)stateCard.append(note("已连接 worker：","Connected worker: "),el("code",{},[c.kun.worker.engineVersion,"protocol "+c.kun.worker.protocolVersion].filter(Boolean).join(" · ")));
    c.main.append(config,stateCard);
  }
  async function saveKun(c) {
    c.kun=await api("/setup/kun",{method:"POST",body:{endpoint:c.endpoint,model:c.model,apiKeyEnv:c.keyEnv,kunPath:c.kunPath,...(c.kun?.revision!=null?{expectedRevision:c.kun.revision}:{})}});
    c.kunPath=c.kun.kunPath||c.kunPath;saveDraft(c);updateSteps(c);
    if(c.kun.ready){preferredInstance=c.kun.instanceId;localStorage.setItem("rundesk-instance",preferredInstance);}
  }
  function codex(c) {
    header(c,"配置和安装 Codex","Configure and install Codex","自动发现已有安装；需要安装时，下载指定版本到 RunDesk 自己的目录。原生安装不要求预先安装 Node.js 或 npm。","Reuse a discovered installation, or download an exact release into RunDesk's own directory. Native installation does not require Node.js or npm.");
    const installed=(c.discovery?.tools||[]).find(value=>value.id==="codex"&&value.status==="ready");
    const path=el("input",{id:"onboarding-codex-path",value:c.codexPath,placeholder:tr("Codex 可执行文件的绝对路径","Absolute path to the Codex executable")});
    path.oninput=()=>{c.codexPath=path.value.trim();saveDraft(c);};
    const installedStatus=badge(installed?"ready":"unavailable",installed?(installed.version||tr("检测到可用安装","Installation found")):tr("尚未发现可用安装","No working installation found"));installedStatus.id="onboarding-codex-status";
    c.main.append(card(tr("已有 Codex","Existing Codex"),el("div",{class:"ob-inline"},installedStatus),el("label",{style:"margin-top:16px"},tr("使用的可执行文件","Executable to use"),path),note("安装完成后会填入路径，完成初始化时保存。Codex 的模型认证仍需要在实例中配置。","A successful installation fills in this path, which is saved when setup finishes. Codex model authentication is configured separately in the instance.")));
    const version=el("input",{id:"onboarding-codex-version",value:c.version,placeholder:"0.157.1"});version.oninput=()=>{c.version=version.value.trim();saveDraft(c);};
    const method=el("select",{id:"onboarding-install-method"},el("option",{value:"auto"},tr("自动（优先原生下载）","Automatic (prefer native download)")),el("option",{value:"native"},tr("原生下载，无需 Node.js","Native download; no Node.js required")),el("option",{value:"npm"},tr("使用本机 npm","Use the local npm installation")));
    const group=sourceGroup(c,"onboarding-install-group");
    const source=el("select",{id:"onboarding-npm-source"}), registry=el("input",{id:"onboarding-npm-registry",placeholder:"https://registry.npmjs.org/"});
    const fillSources=()=>{
      const choices=(c.catalog?.groups.find(value=>value.id===group.value)?.sources||[]).filter(value=>value.type==="npm");
      source.replaceChildren(...choices.map(value=>el("option",{value:value.id},value.name)));registry.value=choices[0]?.values?.registry||"https://registry.npmjs.org/";
    };
    source.onchange=()=>{registry.value=c.catalog?.groups.flatMap(value=>value.sources||[]).find(value=>value.id===source.value)?.values?.registry||registry.value;};
    group.addEventListener("change",fillSources);fillSources();
    const controls=el("div",{class:"ob-grid"},el("label",{},tr("Codex 精确版本","Exact Codex release"),version),el("label",{},tr("安装方式","Installation method"),method),el("label",{},tr("下载源分组","Download source group"),group),el("label",{},tr("npm 下载源","npm download source"),source),el("label",{class:"ob-wide"},tr("下载地址（可修改）","Registry address (editable)"),registry));
    const install=action(c,tr("下载并安装 Codex","Download and install Codex"),async()=>{
      if(!/^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$/.test(c.version))throw Error(tr("请填写精确版本，例如 0.157.1。","Enter an exact release, such as 0.157.1."));
      const input={version:c.version,confirm:true,method:method.value,sourceGroup:group.value,...(c.catalog?.revision?{catalogRevision:c.catalog.revision}:{}),...(source.value?{npmSourceId:source.value}:{}),registry:registry.value.trim()};
      const key="install:"+JSON.stringify(input),id=receipt(c,key);
      c.installAdoptRequestId=id;c.installAdopted=false;saveDraft(c);
      const intent=autoIntent(c,"codex-install",id);
      const job=await api("/setup/codex-installs",{method:"POST",body:{...input,requestId:id}});
      delete c.receipts[key];saveDraft(c);
      c.installJobs=[job,...c.installJobs.filter(value=>value.id!==job.id)];adoptInstall(c);renderInstallJobs(c);schedule(c);
      if(intent){intent.sourceId=job.id;saveDraft(c);await registerAuto(c,intent);renderRepairs(c);}
    },"primary",{id:"onboarding-install-codex"});
    install.disabled=!c.catalog;
    c.main.append(card(tr("下载安装","Download and install"),controls,note("点击即授权下载安装所选版本。安装在后台执行，刷新页面可继续查看；不会覆盖系统全局安装。","Click to authorize downloading and installing the selected release. Installation runs in the background and remains visible after a refresh; global packages are preserved."),autoControl(c,"onboarding-install-auto-repair"),el("div",{class:"ob-actions"},install,action(c,tr("编辑源列表","Edit source catalog"),()=>editSources(c,loadCatalog))),!c.catalog?el("p",{class:"ob-error"},tr("源列表尚未读取成功，请重新读取。","The source catalog has not loaded. Retry loading it.")):null));
    c.installTarget=card(tr("安装记录","Installation history"),el("div",{id:"onboarding-install-history"}));c.main.append(c.installTarget);renderInstallJobs(c);repairRegion(c);
  }
  function sourceGroup(c,id) {
    const group=el("select",{id},...(c.catalog?.groups||[{id:"international",name:tr("国际源","International")},{id:"china",name:tr("中国镜像","China mirrors")}]).map(value=>el("option",{value:value.id},value.name)));
    group.value=c.group||c.catalog?.defaultGroup||"international";
    if(!group.value)group.value=c.catalog?.defaultGroup||group.options[0]?.value||"";c.group=group.value;
    group.onchange=()=>{c.group=group.value;saveDraft(c);};return group;
  }
  function captureSources(c) {
    for(const target of c.page.querySelectorAll("[data-ob-source-recipe]")){
      const value={group:target.querySelector("select")?.value||c.group,selections:{},overrides:{}};
      for(const fieldset of target.querySelectorAll("[data-source-type]")){
        const type=fieldset.dataset.sourceType,select=fieldset.querySelector("select");
        if(select?.value)value.selections[type]=select.value;
        const inputs=[...fieldset.querySelectorAll("[data-source-field]")];
        if(inputs.length)value.overrides[type]=Object.fromEntries(inputs.map(node=>[node.dataset.sourceField,node.value]));
      }
      c.sourceDrafts[target.dataset.obSourceRecipe]=value;
    }
    saveDraft(c);
  }
  function restoreSources(target,value) {
    if(!value)return;
    const group=target.querySelector("select");
    if(group&&[...group.options].some(option=>option.value===value.group)){group.value=value.group;group.dispatchEvent(new Event("change"));}
    for(const fieldset of target.querySelectorAll("[data-source-type]")){
      const type=fieldset.dataset.sourceType,select=fieldset.querySelector("select"),selected=value.selections?.[type];
      if(select&&selected&&[...select.options].some(option=>option.value===selected)){select.value=selected;select.dispatchEvent(new Event("change"));}
      for(const input of fieldset.querySelectorAll("[data-source-field]")){
        if(Object.prototype.hasOwnProperty.call(value.overrides?.[type]||{},input.dataset.sourceField)){input.value=value.overrides[type][input.dataset.sourceField];input.dispatchEvent(new Event("input"));}
      }
    }
  }
  async function editSources(c, reload) {
    await RunDeskBuildCenter.sourceCatalog();
    const dialog=document.querySelector("#center-sources-json")?.closest("dialog");
    if(dialog?.open)await new Promise(resolve=>dialog.addEventListener("close",resolve,{once:true}));
    if(!c.closed)await reload(c);
  }
  function autoControl(c,id) {
    const checkbox=el("input",{type:"checkbox",id,checked:c.autoRepair,disabled:!readyKun(c)});
    checkbox.onchange=()=>{c.autoRepair=checkbox.checked;saveDraft(c);};
    return el("div",{},el("label",{class:"ob-checkbox"},checkbox,tr("失败时交给 Kun 自动诊断并修复","Automatically ask Kun to diagnose and repair failures")),note(readyKun(c)?"仅对这次提交的任务生效，授权有效 2 小时；需要时自动复查模型再交接，关闭页面后仍继续。每次修复有次数限制，可在下方关闭。":"先启用 Kun，才能开启失败自动修复。已有历史失败不会自动提交。",readyKun(c)?"Applies only to tasks submitted here for two hours. The model is rechecked when needed, and handoff continues after the page closes. Repairs have bounded attempts and can be disabled below.":"Enable Kun first to use automatic repair. Historical failures are not submitted automatically."));
  }
  function autoIntent(c,kind,sourceRequestId) {
    if(!c.autoRepair||!readyKun(c))return null;
    const key=kind+":"+sourceRequestId;
    if(!c.autoAuthorizations[key])c.autoAuthorizations[key]={kind,sourceRequestId,sourceId:"",state:"pending",attempts:0};
    saveDraft(c);schedule(c);return c.autoAuthorizations[key];
  }
  async function registerAuto(c,intent) {
    if(intent.state==="registered"||!intent.sourceId)return;
    const key="auto-register:"+intent.kind+":"+intent.sourceId;
    try{
      intent.attempts++;
      const value=await api("/setup/auto-repairs",{method:"POST",body:{kind:intent.kind,sourceId:intent.sourceId,enabled:true,requestId:receipt(c,key)}});
      intent.state="registered";intent.recordId=value.id;
      c.autoRows=[value,...c.autoRows.filter(row=>row.id!==value.id)];delete c.receipts[key];saveDraft(c);schedule(c);renderInstallJobs(c);renderBuilds(c);renderRepairs(c);
    }catch(error){
      intent.state=error.status>=400&&error.status<500?"blocked":intent.attempts<3?"unconfirmed":"blocked";
      intent.error=error.message;saveDraft(c);schedule(c);
      renderInstallJobs(c);renderBuilds(c);renderRepairs(c);
      throw Error(tr("任务已提交，但自动修复交接尚未确认。下方可以重试注册：","The task was submitted, but automatic repair registration is unconfirmed. Retry registration below: ")+error.message);
    }
  }
  async function reconcileAuto(c) {
    if(c.registering||!readyKun(c))return;c.registering=true;
    try{
      for(const intent of Object.values(c.autoAuthorizations)){
        if(!intent.sourceId){
          const row=(intent.kind==="codex-install"?c.installJobs:c.builds).find(value=>(value.input?.requestId||value.requestId)===intent.sourceRequestId);
          if(row){intent.sourceId=row.id;if(intent.state==="source_unconfirmed")intent.state="pending";}
          else{intent.lookups=(intent.lookups||0)+1;if(intent.lookups>=3&&intent.state==="pending")intent.state="source_unconfirmed";continue;}
        }
        const existing=c.autoRows.find(row=>row.kind===intent.kind&&row.sourceId===intent.sourceId);
        if(existing){intent.state="registered";intent.recordId=existing.id;continue;}
        if(["pending","unconfirmed"].includes(intent.state)&&intent.sourceId)try{await registerAuto(c,intent);}catch(error){if(!c.closed)c.error.textContent=error.message;}
      }
      saveDraft(c);
    }finally{c.registering=false;}
  }
  async function loadCatalog(c) { c.catalog=await api("/build-center/sources");if(!c.group)c.group=c.catalog.defaultGroup||"international";saveDraft(c);if(!c.closed&&(c.step===1||c.step===2))render(c); }
  function adoptInstall(c) {
    if(!c.installAdoptRequestId||c.installAdopted)return;
    const source=c.installJobs.find(value=>value.input?.requestId===c.installAdoptRequestId);if(!source)return;
    const repair=c.repairs.find(value=>value.sourceInstallId===source.id);
    const candidates=[source,...(repair?.attemptIds||[]).map(id=>c.installJobs.find(value=>value.id===id)).filter(Boolean)];
    const success=candidates.find(value=>value.status==="succeeded"&&value.result?.ok&&value.result.codex);
    if(success){c.codexPath=success.result.codex;c.installAdopted=true;saveDraft(c);const input=c.page.querySelector("#onboarding-codex-path");if(input)input.value=c.codexPath;updateSteps(c);}
  }
  function renderInstallJobs(c) {
    const target=c.page.querySelector("#onboarding-install-history");if(!target)return;
    const opened=new Set([...target.querySelectorAll("details[open]")].map(value=>value.dataset.job));
    target.replaceChildren(...c.installJobs.slice(0,8).map(value=>{
      const result=value.result||{}, container=el("article",{class:"ob-job","data-install-id":value.id},el("div",{class:"ob-job-top"},el("strong",{},"Codex "+(value.input?.version||result.version||"")),badge(value.status)));
      if(result.error)container.append(el("p",{class:"ob-error"},result.error));
      if(result.codex)container.append(note("已安装到：","Installed at: "),el("code",{style:"font-size:11px;overflow-wrap:anywhere"},result.codex),el("div",{class:"ob-actions"},action(c,tr("使用此安装","Use this installation"),()=>{c.codexPath=result.codex;saveDraft(c);const path=c.page.querySelector("#onboarding-codex-path");if(path)path.value=c.codexPath;updateSteps(c);})));
      if(["failed","interrupted"].includes(value.status)){
        const repair=action(c,tr("交给 Kun 诊断并修复","Ask Kun to diagnose and repair"),async()=>{
          const v=await api("/setup/repairs",{method:"POST",body:{kind:"codex-install",installId:value.id,requestId:receipt(c,"install-repair:"+value.id)}});
          c.repairs=[v,...c.repairs.filter(row=>row.id!==v.id)];renderRepairs(c);schedule(c);
        },"quiet",{"data-install-repair":value.id});repair.disabled=!readyKun(c)||pendingAuto(c,"codex-install",value.id);container.append(el("div",{class:"ob-actions"},repair));
        if(sourceAuto(c,"codex-install",value.id))container.append(note("已开启后台自动交接，请在 Kun 修复任务中查看状态。","Background handoff is enabled. See Kun repair tasks for its status."));
        if(!readyKun(c))container.append(note("先在第一步“寻找和配置 LLM”中启用 Kun，即可委托修复。","Enable Kun in step 1, Find and configure LLM, to delegate a repair."));
      }
      if(result.log||activeJob(value.status))container.append(el("details",{open:opened.has(value.id)||activeJob(value.status),"data-job":value.id},el("summary",{},tr("安装日志","Installation log")),el("pre",{},result.log||tr("任务在后台执行，正在等待日志。","The task is running in the background; waiting for logs."))));
      return container;
    }));
    if(!c.installJobs.length)target.append(note("尚未提交安装任务。已有 Codex 可以直接继续。","No installation tasks yet. Continue with an existing Codex installation if available."));
  }
  function repairRegion(c) { c.repairTarget=card(tr("Kun 修复任务","Kun repair tasks"),el("div",{id:"onboarding-repairs"}));c.main.append(c.repairTarget);renderRepairs(c); }
  function renderRepairs(c) {
    const target=c.page.querySelector("#onboarding-repairs");if(!target)return;
    const automatic=c.autoRows.slice(0,10).map(value=>{
      const row=el("article",{class:"ob-job","data-auto-repair":value.id},el("div",{class:"ob-job-top"},el("strong",{},value.kind==="image-build"?tr("构建自动修复","Automatic build repair"):tr("Codex 安装自动修复","Automatic Codex installation repair")),badge(value.status)),value.error?el("p",{class:"ob-error"},value.error):null);
      if(value.sessionId)row.append(el("a",{href:"/?session="+enc(value.sessionId),target:"_blank",rel:"noopener",class:"ob-help"},tr("查看对话、执行轨迹与修复产物 ↗","View conversation, trace and repair artifacts ↗")));
      if(value.enabled&&["watching","blocked","failed"].includes(value.status))row.append(el("div",{class:"ob-actions"},action(c,tr("关闭本次自动修复","Disable automatic repair"),async()=>{
        const updated=await api("/setup/auto-repairs",{method:"POST",body:{kind:value.kind,sourceId:value.sourceId,enabled:false,requestId:receipt(c,"auto-disable:"+value.id)}});
        c.autoRows=c.autoRows.map(item=>item.id===value.id?updated:item);renderInstallJobs(c);renderBuilds(c);renderRepairs(c);
      })));
      return row;
    });
    const known=new Set(c.autoRows.map(value=>value.repairId).filter(Boolean));
    target.replaceChildren(...automatic,...c.repairs.filter(value=>!known.has(value.id)).slice(0,8).map(value=>el("article",{class:"ob-job"},el("div",{class:"ob-job-top"},el("strong",{},value.sourceBuildId?tr("构建修复","Build repair"):tr("Codex 安装修复","Codex installation repair")),badge(value.status)),value.error?el("p",{class:"ob-error"},value.error):null,value.sessionId?el("a",{href:"/?session="+enc(value.sessionId),target:"_blank",rel:"noopener",class:"ob-help"},tr("查看对话、执行轨迹与修复产物 ↗","View conversation, trace and repair artifacts ↗")):null)));
    for(const intent of Object.values(c.autoAuthorizations).filter(value=>value.sourceId&&["blocked","unconfirmed"].includes(value.state)))target.append(el("div",{class:"ob-notice"},intent.error||tr("自动修复注册待确认。","Automatic repair registration is unconfirmed."),el("div",{class:"ob-actions"},action(c,tr("重试注册自动修复","Retry automatic repair registration"),async()=>{intent.state="pending";intent.attempts=0;await registerAuto(c,intent);renderRepairs(c);renderInstallJobs(c);renderBuilds(c);}))));
    if(!c.repairs.length&&!c.autoRows.length)target.append(note("失败时可以委托 Kun。修复过程保留任务、修改和验证记录。","Delegate to Kun when a step fails. The task, changes and validation results remain available."));
  }
  function docker(c) {
    header(c,"准备 Docker 环境","Prepare Docker environments","下载已发布的预设镜像，或按需要在本机构建。获取后会检查镜像身份与工具；只准备你选中的环境。","Download published preset images, or build locally when needed. Acquired images are checked for identity and tools; prepare only the environments you choose.");
    const dockerState=c.dockerCheck;
    const status=card("Docker Engine",el("div",{class:"ob-inline"},badge(dockerState?.ok?"ready":dockerState?"unavailable":"idle",dockerState?.ok?tr("Docker 检查通过","Docker check passed"):dockerState?tr("Docker 尚不可用","Docker is unavailable"):tr("尚未检查","Not checked")),action(c,tr("检查 Docker","Check Docker"),async()=>{c.dockerCheck=await api("/setup/check",{method:"POST",body:{kind:"docker"}});render(c);},"quiet",{id:"onboarding-check-docker"})),note("Docker 不可用时可以跳过此步，稍后从构建中心继续。","If Docker is unavailable, skip this step and continue later from the build center."));
    if(dockerState&&!dockerState.ok)status.append(el("p",{class:"ob-error"},dockerState.error||""));c.main.append(status);
    const mode=el("select",{id:"onboarding-docker-mode"},el("option",{value:"pull"},tr("下载预构建镜像（推荐）","Download prebuilt image (recommended)")),el("option",{value:"build"},tr("本机构建","Build locally")),el("option",{value:"local"},tr("使用本机已有镜像","Use an existing local image")));mode.value=c.dockerMode;
    mode.onchange=()=>{captureSources(c);c.dockerMode=mode.value;saveDraft(c);render(c);};
    c.main.append(card(tr("准备方式","Preparation method"),el("label",{},tr("镜像来源","Image source"),mode)));
    if(c.dockerMode==="build")buildDocker(c);else acquireDocker(c);
    c.main.append(card(tr("准备进度","Preparation progress"),el("div",{id:"onboarding-build-history"})));renderBuilds(c);repairRegion(c);
  }
  function buildDocker(c) {
    const dockerState=c.dockerCheck;
    if(!c.recipes.length){c.main.append(card(tr("正在读取预设","Loading presets"),action(c,tr("重新读取","Reload"),()=>loadDocker(c))));return;}
    let runtimeInput;
    const version=el("input",{id:"onboarding-docker-version",value:c.version,placeholder:"0.157.1"});version.oninput=()=>{c.version=version.value.trim();runtimeInput?.invalidate();saveDraft(c);};
    const runtimePanel=el("section",{class:"build-runtime-panel"});
    runtimeInput=RunDeskBuildCenter.runtimeOptions(runtimePanel,{prefix:"onboarding-codex-runtime",value:c.codexRuntime,version:()=>c.version,onchange:value=>{c.codexRuntime=value;saveDraft(c);}});
    const group=sourceGroup(c,"onboarding-docker-group");
    const options=el("div",{class:"ob-recipes"},...c.recipes.filter(value=>!value.archived).map(value=>{
      const input=el("input",{type:"checkbox",value:value.id,checked:c.selected.has(value.id),"data-preset-id":value.id});
      input.onchange=()=>{input.checked?c.selected.add(value.id):c.selected.delete(value.id);saveDraft(c);renderSources();};
      return el("label",{class:"ob-choice"},input,el("span",{},el("strong",{},value.name),el("small",{},value.description||""),el("small",{},"v"+value.version)));
    }));
    const sources=el("div",{class:"ob-sources",id:"onboarding-preset-sources"});c.sourceInputs=new Map();
    const renderSources=(reset=false)=>{
      if(!reset)captureSources(c);else{c.sourceDrafts={};saveDraft(c);}
      sources.replaceChildren();c.sourceInputs.clear();
      for(const id of c.selected){
        const recipe=c.recipes.find(value=>value.id===id), capability=c.capabilities[id];if(!recipe)continue;
        const detail=el("details",{},el("summary",{},recipe.name+" · "+tr("下载地址","Download addresses"))), target=el("div",{"data-ob-source-recipe":id});detail.append(target);
        if(!c.catalog||!capability){target.append(el("p",{class:"ob-error"},tr("此配方的源参数尚未读取成功，不能提交构建。","Source parameters have not loaded for this recipe; it cannot be submitted yet.")));}
        else{
          const input=RunDeskBuildCenter.sourceOptions(target,{...c.catalog,defaultGroup:group.value},capability);c.sourceInputs.set(id,input);
          restoreSources(target,c.sourceDrafts[id]);
          for(const node of target.querySelectorAll("[id]"))node.id="ob-"+id+"-"+node.id;
          target.addEventListener("input",()=>captureSources(c));target.addEventListener("change",()=>captureSources(c));
        }
        sources.append(detail);
      }
    };
    group.addEventListener("change",()=>renderSources(true));
    const start=action(c,tr("构建选中的预设","Build selected presets"),async()=>{
      if(!c.selected.size)throw Error(tr("请先选择至少一个预设。","Select at least one preset."));
      if(!/^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$/.test(c.version))throw Error(tr("请填写镜像内 Codex 的精确版本。","Enter the exact Codex release for the image."));
      // Validate every selection before the first submission. Each successful
      // response is retained immediately so a later failure cannot hide it.
      const inputs=[...c.selected].map(id=>{const r=c.recipes.find(value=>value.id===id),sources=c.sourceInputs.get(id);if(!r||!sources)throw Error(tr("请先重新读取完整的源配置。","Reload the complete source configuration first."));return{r,buildSources:sources(),...(r.kind==="builtin"?{codexRuntime:runtimeInput.read()}:{})};});
      const completedKeys=[],registrationErrors=[];
      for(const {r,buildSources,codexRuntime} of inputs){
        const input={recipeRevision:r.revision,recipeSha:r.recipeSha,...(r.kind==="builtin"?{codexVersion:c.version,codexRuntime}:{}),buildSources};
        const key="build:"+r.id+":"+JSON.stringify(input),id=receipt(c,key),intent=autoIntent(c,"image-build",id);
        const value=await api("/build-center/recipes/"+enc(r.id)+"/build",{method:"POST",body:{...input,requestId:id}});
        completedKeys.push(key);saveDraft(c);
        c.builds=[value,...c.builds.filter(row=>row.id!==value.id)];renderBuilds(c);schedule(c);
        if(intent){intent.sourceId=value.id;saveDraft(c);try{await registerAuto(c,intent);}catch(error){registrationErrors.push(error.message);}renderRepairs(c);}
      }
      for(const key of completedKeys)delete c.receipts[key];saveDraft(c);
      if(registrationErrors.length)throw Error(registrationErrors.join("\n"));
    },"primary",{id:"onboarding-build-selected"});start.disabled=!dockerState?.ok||!c.catalog;
    c.main.append(card(tr("可用预设","Available presets"),options,el("div",{class:"ob-grid",style:"margin-top:20px"},el("label",{},tr("镜像内 Codex 精确版本","Exact Codex release in images"),version),el("label",{},tr("网络配置","Network configuration"),group)),runtimePanel,note("切换下载源分组会重新填入所选预设的地址。单独修改的地址会随草稿保留。","Changing the source group refills the selected presets. Individual address edits are retained in your draft."),sources,autoControl(c,"onboarding-build-auto-repair"),el("div",{class:"ob-actions"},start,action(c,tr("编辑源列表","Edit source catalog"),()=>editSources(c,loadDocker))),note("构建在后台运行，关闭初始化页面不会停止任务。每个构建会保存本次配方和下载源快照。","Builds run in the background and continue when setup closes. Each build keeps its recipe and source configuration snapshot.")));renderSources();
  }
  function runtimeProfiles(c) {
    return c.runtimeProfiles?.items || c.recipes.filter(value=>value.kind==="builtin"&&value.profileId).map(value=>({id:value.profileId,name:value.name}));
  }
  function runtimePlatform(c) { return c.runtimeProfiles?.platform || (c.discovery?.platform?.arch ? "linux/"+c.discovery.platform.arch : ""); }
  function runtimeEntries(c,profileId) {
    const recipe=c.recipes.find(value=>value.kind==="builtin"&&value.profileId===profileId),platform=runtimePlatform(c);
    return (c.publicCatalog?.items||[]).filter(value=>value.profileId===profileId&&value.platform===platform&&(!recipe||value.profileVersion===recipe.version));
  }
  function setRuntimeEntry(c,draft,entry,sourceID) {
    const source=entry?.sources?.find(value=>value.id===sourceID)||entry?.sources?.[0];
    draft.entryId=entry?.id||"";draft.sourceId=source?.id||"";
    if(entry&&source){draft.version=entry.codexVersion;draft.reference=source.reference;}
    saveDraft(c);
  }
  async function editPublicImages(c) {
    if(!window.RunDeskBuildCenter?.publicImages)throw Error(tr("公开镜像列表编辑器尚未加载，请刷新页面。","The public image editor has not loaded. Refresh the page."));
    await RunDeskBuildCenter.publicImages();
    const dialog=document.querySelector("#center-public-images-json")?.closest("dialog");
    if(dialog?.open)await new Promise(resolve=>dialog.addEventListener("close",resolve,{once:true}));
    if(!c.closed)await loadDocker(c);
  }
  function acquireDocker(c) {
    const pulling=c.dockerMode==="pull",profiles=runtimeProfiles(c);
    if(!profiles.length){c.main.append(card(tr("正在读取预设","Loading presets"),action(c,tr("重新读取","Reload"),()=>loadDocker(c))));return;}
    const draft=c.runtimeDrafts[c.dockerMode]||=( {profileId:profiles[0].id,version:c.version,reference:""} );
    if(!profiles.some(value=>value.id===draft.profileId))draft.profileId=profiles[0].id;
    const profile=el("select",{id:"onboarding-runtime-profile"},...profiles.map(value=>el("option",{value:value.id},value.name||value.id)));profile.value=draft.profileId;
    profile.onchange=()=>{draft.profileId=profile.value;draft.entryId=undefined;draft.sourceId="";draft.reference="";saveDraft(c);render(c);};
    const entries=pulling?runtimeEntries(c,draft.profileId):[];
    if(pulling&&draft.entryId===undefined&&entries.length)setRuntimeEntry(c,draft,entries.find(value=>value.codexVersion===draft.version)||entries[0]);
    let entry=entries.find(value=>value.id===draft.entryId);
    if(pulling&&draft.entryId&&!entry){draft.entryId="";draft.sourceId="";saveDraft(c);}
    if(entry)setRuntimeEntry(c,draft,entry,draft.sourceId);
    const version=el("input",{id:"onboarding-runtime-version",value:draft.version||c.version,placeholder:"0.157.1",readonly:!!entry});
    const reference=el("input",{id:"onboarding-runtime-reference",value:draft.reference||"",placeholder:pulling?tr("填写发布方提供的镜像地址","Image reference supplied by the publisher"):tr("本机镜像标签或 Digest","Local image tag or digest"),readonly:!!entry,autocomplete:"off"});
    version.oninput=()=>{draft.version=version.value.trim();saveDraft(c);};reference.oninput=()=>{draft.reference=reference.value.trim();draft.entryId="";saveDraft(c);};
    const fields=el("div",{class:"ob-grid"},el("label",{},tr("预设","Preset"),profile));
    if(pulling){
      const released=el("select",{id:"onboarding-public-image"},...entries.map(value=>el("option",{value:value.id},"v"+value.profileVersion+" · Codex "+value.codexVersion+" · "+value.platform)),el("option",{value:""},tr("手工填写镜像地址","Enter an image reference manually")));released.value=entry?.id||"";
      released.onchange=()=>{setRuntimeEntry(c,draft,entries.find(value=>value.id===released.value));render(c);};
      fields.append(el("label",{},tr("已发布版本","Published release"),released));
      if(entry){
        const source=el("select",{id:"onboarding-public-source"},...(entry.sources||[]).map(value=>el("option",{value:value.id},value.name||value.id)));source.value=draft.sourceId;
        source.onchange=()=>{setRuntimeEntry(c,draft,entry,source.value);render(c);};fields.append(el("label",{class:"ob-wide"},tr("镜像下载地址","Image download source"),source));
      }
    }
    fields.append(el("label",{},tr("镜像内 Codex 精确版本","Exact Codex release in image"),version),el("label",{class:"ob-wide"},tr("镜像地址","Image reference"),reference));
    const submit=action(c,pulling?tr("下载并检查镜像","Download and check image"):tr("检查已有镜像","Check existing image"),async()=>{
      if(!/^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$/.test(draft.version||""))throw Error(tr("请填写镜像内 Codex 的精确版本。","Enter the exact Codex release in this image."));
      if(!draft.reference?.trim())throw Error(tr("请先选择已发布版本，或填写真实镜像地址。","Select a published release or enter an existing image reference."));
      const selected=entries.find(value=>value.id===draft.entryId),source=selected?.sources?.find(value=>value.id===draft.sourceId);
      const input={profileId:draft.profileId,codexVersion:draft.version,source:c.dockerMode,reference:draft.reference.trim(),...(pulling&&selected&&source?{publicImageId:selected.id,publicSourceId:source.id,catalogRevision:c.publicCatalog.revision}:{})};
      const key="runtime-image:"+JSON.stringify(input),id=receipt(c,key);
      const job=await api("/setup/runtime-images",{method:"POST",body:{...input,requestId:id}});
      delete c.receipts[key];saveDraft(c);c.builds=[job,...c.builds.filter(value=>value.id!==job.id)];renderBuilds(c);schedule(c);
    },"primary",{id:"onboarding-prepare-image"});submit.disabled=!c.dockerCheck?.ok||c.runtimeProfiles?.supported===false;
    const panel=card(pulling?tr("下载预构建镜像","Download a prebuilt image"):tr("复用本机镜像","Reuse a local image"),fields);
    if(pulling&&!entries.length)panel.append(el("p",{class:"ob-notice",id:"onboarding-public-catalog-status"},c.publicCatalogError?tr("公开镜像列表读取失败：","Could not load the public image catalog: ")+c.publicCatalogError:tr("当前列表还没有与此预设和平台匹配的公开镜像。填写发布方提供的真实地址，或切换到本机构建。","No public image in the current catalog matches this preset and platform. Enter a real publisher-provided reference, or switch to a local build.")));
    if(c.runtimeProfiles?.supported===false)panel.append(note("此功能需要 Linux 本机 Docker Engine；可以先完成初始化，稍后准备镜像。","This requires a local Docker Engine on Linux. You can finish setup and prepare images later."));
    panel.append(note(pulling?"公开版本按 Digest 固定；手工地址也会校验预设、Codex 版本和离线工具。拉取失败时可更换地址，不会自动改成本机构建。":"只检查本机已有镜像，不下载或重新构建。预设、Codex 版本和离线工具检查通过后，才能继续使用。",pulling?"Published releases are pinned by digest. Manual references are also checked for preset identity, Codex release and offline tools. Failed downloads never switch to a local build automatically.":"Only inspect an existing local image; do not download or rebuild it. Preset, Codex release and offline tool checks must pass before use."),el("div",{class:"ob-actions"},submit,...(pulling?[action(c,tr("编辑公开镜像列表","Edit public image catalog"),()=>editPublicImages(c),"quiet",{id:"onboarding-edit-public-images"}),action(c,tr("刷新列表","Refresh catalog"),()=>loadDocker(c),"quiet",{id:"onboarding-refresh-public-images"})]:[])));
    c.main.append(panel);saveDraft(c);
  }
  async function loadDocker(c) {
    const [recipes,catalog,builds,check,published,profiles]=await Promise.allSettled([api("/build-center/recipes"),api("/build-center/sources"),api("/build-center/builds"),api("/setup/check",{method:"POST",body:{kind:"docker"}}),api("/setup/public-images"),api("/setup/runtime-profiles")]);
    if(c.closed)return;
    if(recipes.status==="fulfilled")c.recipes=recipes.value.items||[];
    if(catalog.status==="fulfilled"){c.catalog=catalog.value;if(!c.group)c.group=c.catalog.defaultGroup||"international";}
    if(builds.status==="fulfilled")c.builds=builds.value.items||[];
    if(check.status==="fulfilled")c.dockerCheck=check.value;
    if(published.status==="fulfilled"){c.publicCatalog=published.value;c.publicCatalogError="";}else c.publicCatalogError=published.reason?.message||String(published.reason);
    if(profiles.status==="fulfilled")c.runtimeProfiles=profiles.value;
    const capabilities=await Promise.allSettled(c.recipes.map(async value=>[value.id,await api("/build-center/recipes/"+enc(value.id)+"/sources")]));
    for(const result of capabilities)if(result.status==="fulfilled")c.capabilities[result.value[0]]=result.value[1];
    if(!c.closed){if(c.step===2)render(c);schedule(c);}
  }
  function renderBuilds(c) {
    const target=c.page.querySelector("#onboarding-build-history");if(!target)return;
    target.replaceChildren(...c.builds.slice(0,12).map(value=>{
      const result=el("article",{class:"ob-job","data-build-id":value.id},el("div",{class:"ob-job-top"},el("strong",{},value.name||value.profileId||value.id),badge(value.status)),value.error?el("p",{class:"ob-error"},value.error):null);
      const ops=el("div",{class:"ob-actions"},action(c,tr("查看日志","View log"),async()=>{
        await readBuildLog(c,value.id);renderBuilds(c);schedule(c);
      }));
      if(value.codexRuntime)result.append(RunDeskBuildCenter.frozenRuntimeView(value.codexRuntime));
      const acquired=value.source==="pull"||value.source==="local";
      if(acquired)result.append(el("p",{class:"ob-help"},(value.source==="pull"?tr("仓库拉取","Registry download"):tr("本机镜像检查","Local image check"))+" · "+(value.reference||"")));
      if(activeJob(value.status))ops.append(action(c,tr("取消准备","Cancel preparation"),async()=>{const updated=await RunDeskBuildCenter.mutate("/builds/"+enc(value.id)+"/cancel",{});c.builds=c.builds.map(row=>row.id===value.id?updated:row);renderBuilds(c);schedule(c);},"quiet",{"data-image-cancel":value.id}));
      if(["failed","interrupted","canceled"].includes(value.status)){
        if(acquired){
          ops.append(action(c,value.source==="pull"?tr("更换地址或重试","Change source or retry"):tr("重新选择本机镜像","Choose a local image again"),()=>{
            captureSources(c);c.dockerMode=value.source;c.runtimeDrafts[value.source]={profileId:value.profileId,version:value.codexVersion,reference:value.reference,entryId:"",sourceId:""};saveDraft(c);setStep(c,2);c.page.querySelector("#onboarding-runtime-reference")?.focus();
          },"quiet",{"data-runtime-retry":value.id}));
          result.append(note("可以查看日志后调整镜像地址或登录权限。此任务不会自动触发本机构建。","Review the log, then adjust the image reference or registry access. This task never triggers a local build automatically."));
        }else{
          ops.append(action(c,tr("原配置重试","Retry original configuration"),async()=>{const v=await RunDeskBuildCenter.mutate("/builds/"+enc(value.id)+"/rebuild",{});c.builds=[v,...c.builds.filter(row=>row.id!==v.id)];renderBuilds(c);schedule(c);}));
          const repair=action(c,tr("交给 Kun 修复","Ask Kun to repair"),async()=>{
            const v=await RunDeskBuildCenter.mutate("/assistant-runs",{sourceBuildId:value.id,instanceId:c.kun.instanceId,workspaceId:c.kun.workspaceId,prompt:tr("诊断并修复这次失败构建。保持工具能力与验证要求，生成独立新配方，实际重建并完成offline/smoke验证，记录适用范围明确的修复经验。","Diagnose and repair this failed build. Preserve the required capabilities and tests. Produce a separate revised recipe, rebuild it, run offline/smoke checks, and record the repair with its applicability conditions.")});
            c.repairs=[{...v,sourceBuildId:value.id},...c.repairs.filter(row=>row.id!==v.id)];renderRepairs(c);schedule(c);
          },"quiet",{"data-build-repair":value.id});repair.disabled=!readyKun(c)||pendingAuto(c,"image-build",value.id);ops.append(repair);
        }
      }
      if(value.status==="succeeded"){
        result.append(note("镜像已准备；应用认证和实际业务验证仍需完成。","The image is prepared; application authentication and business validation remain to be completed."));
        if(value.imageId)ops.append(action(c,tr("完成初始化并查看镜像","Finish setup and view image"),async()=>{
          const catalog=await api("/build-center/images"),image=catalog.items?.find(row=>row.imageId===value.imageId);
          if(!image)throw Error(tr("镜像尚未出现在构建中心，请稍后刷新。","The image is not in the build center yet. Refresh shortly."));
          await completeSetup(c,image.key);
        },"quiet",{"data-runtime-open-image":value.id}));
      }
      result.append(ops);
      if(c.logs[value.id])result.append(el("pre",{"data-build-log":value.id},c.logs[value.id].text||tr("尚无日志。","No logs yet.")));
      return result;
    }));
    if(!c.builds.length)target.append(note("尚未提交镜像准备任务。选择预设下载或构建，也可以稍后继续。","No image preparation tasks yet. Download or build a preset now, or continue later."));
  }
  async function readBuildLog(c,id) {
    const previous=c.logs[id]||{next:0,text:""};
    const value=await api("/build-center/builds/"+enc(id)+"/log?after="+previous.next);
    const text=previous.text+(value.text||"");
    c.logs[id]={next:value.next,text:text.length>131072?tr("[显示日志末尾]\n","[Showing the end of the log]\n")+text.slice(-131072):text};
  }
  function finish(c) {
    const succeeded=c.builds.filter(value=>value.status==="succeeded").length,running=c.builds.filter(value=>activeJob(value.status)).length;
    const summary=el("details",{class:"ob-setup-summary"},el("summary",{},tr("查看三步初始化结果","Review the three setup steps")),
      summaryRow(tr("本机环境","Environment"),c.discovery?tr("已扫描","Scanned"):tr("尚未扫描","Not scanned")),
      summaryRow("Kun",readyKun(c)?badge("ready",tr("可执行修复任务","Ready for repair tasks")):badge("idle",tr("尚未就绪，可稍后配置","Not ready; configure later"))),
      summaryRow("Codex",c.codexPath?c.codexPath:tr("已跳过","Skipped")),
      summaryRow(tr("Docker 镜像","Docker images"),succeeded+tr(" 个准备成功"," ready")+(running?" · "+running+tr(" 个进行中"," running"):"")));
    c.main.append(summary);
    const work=el("select",{id:"onboarding-workspace"},...state.workspaces.map(value=>el("option",{value:value.id},value.name)));work.value=c.workspaceId||state.workspaces[0]?.id||"";work.onchange=()=>{c.workspaceId=work.value;};
    c.completeButton=action(c,tr("完成并进入 RunDesk","Finish and enter RunDesk"),()=>completeSetup(c),"primary",{id:"onboarding-complete"});
    c.main.append(card(tr("完成初始化","Finish setup"),el("label",{},tr("默认工作区","Default workspace"),work),note("点击下方“完成并进入 RunDesk”保存配置。Docker 环境可以稍后准备；已经提交的安装、拉取、构建和修复任务会继续在后台运行。","Finish and enter RunDesk below to save your configuration. Docker preparation is optional; submitted installation, pull, build and repair tasks continue in the background.")));
    const examples=card(tr("积累的修复经验","Repair experience"),el("div",{id:"onboarding-experiences"}));c.main.append(examples);renderExperiences(c);
  }
  async function completeSetup(c,imageKey="") {
    const workspaceId=c.workspaceId||state.workspaces[0]?.id||"";
    const saved=await api("/setup/complete",{method:"POST",body:{workspaceId,revision:c.setup.settings.revision,...(c.codexPath?{codex:c.codexPath}:{}),completed:true}});
    c.setup.settings=saved.settings||saved;sessionStorage.removeItem("rundesk-setup-dismissed");
    if(Object.values(c.autoAuthorizations).every(value=>value.state==="registered"))sessionStorage.removeItem("rundesk-onboarding-draft");else saveDraft(c);
    if(readyKun(c)){preferredInstance=c.kun.instanceId;localStorage.setItem("rundesk-instance",preferredInstance);}
    localStorage.setItem("rundesk-assistant-workspace",workspaceId);if($("#workspace"))$("#workspace").value=workspaceId;
    if(imageKey)history.replaceState(null,"","#build-center/images/"+enc(imageKey));
    close(c,true);
    if(imageKey&&!c.initial)await RunDeskBuildCenter.open("images",imageKey);
  }
  function summaryRow(key,value) { return el("div",{class:"ob-summary-row"},el("span",{},key),el("span",{},value)); }
  function experienceText(value,limit=6000) {
    const text=String(value||"").slice(0,limit).replace(/(Bearer\s+)[^\s"',;]+/gi,"$1[redacted]")
      .replace(/\brd-(?:build|setup)-[a-f0-9]{32,}\b/g,"[redacted]")
      .replace(/((?:api[_-]?key|access[_-]?token|token|password|secret|authorization)["']?\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s&,;"'<>}\]]+)/gi,"$1[redacted]")
      .replace(/(https?:\/\/)[^/\s:@]+:[^@\s/]+@/gi,"$1[redacted]@");
    return text+(String(value||"").length>limit?tr("\n[展示内容已截断]","\n[Display truncated]"):"");
  }
  function experienceDetails(value) {
    const detail=el("details",{"data-experience-details":value.id},el("summary",{},tr("查看问题、改动与验证","Review issue, changes and validation")));
    detail.append(el("h3",{style:"margin-top:16px"},tr("发现的问题","Observed problem")),el("pre",{},experienceText(value.failure?.error||value.failure?.logExcerpt||value.summary,4000)),el("h3",{style:"margin-top:16px"},tr("实际改动","Changes made")),el("pre",{},experienceText(value.changes||tr("未记录改动说明。","No change description recorded."))));
    const validation=value.validation||{},check=(label,passed)=>el("div",{class:"ob-summary-row"},el("span",{},label),badge(passed?"ready":"idle",passed?tr("已通过","Passed"):tr("未验证","Not verified")));
    detail.append(el("h3",{style:"margin-top:16px"},tr("验证结果","Validation results")),check(tr("安装版本核验","Installed version verification"),validation.installVersionVerified===true),check("Offline",validation.offline===true),check("Smoke",validation.smoke===true));
    if(value.status!=="verified")detail.append(note("这是候选经验，尚未通过所需的完整验证。","This is a candidate experience; the required full validation has not passed."));
    const patch=value.patch;
    if(patch){
      detail.append(el("h3",{style:"margin-top:16px"},tr("文件改动","File changes")+" · "+(patch.changedFiles||0)));
      if(patch.available){
        detail.append(el("pre",{},(patch.files||[]).slice(0,12).map(file=>experienceText(file.change+" "+file.path,512)).join("\n")||tr("没有文件差异。","No file differences.")));
        if(patch.files?.length>12||patch.truncated)detail.append(note("这里只展示部分文件，请结合修复任务产物审阅。","Only some files are shown; review the repair task artifacts for the complete changes."));
        if(patch.dockerfileDiff)detail.append(el("h3",{style:"margin-top:16px"},"Dockerfile diff"),el("pre",{"data-experience-diff":value.id},experienceText(patch.dockerfileDiff,12000)));
      }else detail.append(el("p",{class:"ob-help"},experienceText(patch.error||tr("当前没有可供比较的文件快照。","No file snapshots are available for comparison."),2000)));
    }
    const records=el("div",{class:"ob-actions"});
    for(const [label,record] of [[tr("失败记录","Failure record"),value.failure],[tr("成功记录","Success record"),value.success]])if(record?.id){
      const href=value.kind==="image-build"?"/api/v1/build-center/builds/"+enc(record.id)+"/log?after=0":"/api/v1/setup/codex-installs/"+enc(record.id);
      records.append(el("a",{href,target:"_blank",rel:"noopener",class:"ob-help"},label+" · "+record.id+" ↗"));
    }
    if(records.children.length)detail.append(records);
    if(value.warnings?.length)detail.append(el("pre",{},experienceText(value.warnings.join("\n"),3000)));
    return detail;
  }
  function renderExperiences(c) {
    const target=c.page.querySelector("#onboarding-experiences");if(!target)return;
    const opened=new Set([...target.querySelectorAll("details[open]")].map(node=>node.dataset.experienceDetails));
    target.replaceChildren(...c.experiences.slice(0,8).map(value=>{const detail=experienceDetails(value);if(opened.has(value.id))detail.open=true;return el("article",{class:"ob-job","data-experience-id":value.id},el("div",{class:"ob-job-top"},el("strong",{},experienceText(value.summary||value.kind,2048)),badge(value.status)),note("适用范围：","Applies to: "),el("code",{style:"font-size:11px;overflow-wrap:anywhere"},Object.values(value.scope||{}).filter(Boolean).join(" · ")),detail);}));
    if(!c.experiences.length)target.append(note("验证过的修复会按平台、版本和问题条件保存，供下一次修复参考。一次成功不会直接改写所有配方。","Validated repairs are saved with platform, version and failure conditions for future reference. A single success does not rewrite every recipe."));
  }
  function schedule(c) { if(c.closed)return;clearTimeout(c.timer);if(c.installJobs.some(value=>activeJob(value.status))||c.builds.some(value=>activeJob(value.status))||c.repairs.some(value=>activeJob(value.status))||c.autoRows.some(value=>["watching","revalidating","submitted"].includes(value.status))||Object.values(c.autoAuthorizations).some(value=>["pending","unconfirmed"].includes(value.state)))c.timer=setTimeout(()=>poll(c),2200); }
  async function poll(c) {
    if(c.closed)return;
    try{
      const results=await Promise.allSettled([api("/setup/codex-installs"),api("/build-center/builds"),api("/setup/repairs"),api("/setup/experiences"),api("/setup/auto-repairs")]);
      if(c.closed)return;
      if(results[0].status==="fulfilled")c.installJobs=results[0].value.items||[];
      if(results[1].status==="fulfilled")c.builds=results[1].value.items||[];
      if(results[2].status==="fulfilled"){
        const setupRows=results[2].value.items||[];
        const builds=await Promise.allSettled(c.repairs.filter(value=>value.sourceBuildId).map(async value=>({...await api("/build-center/assistant-runs/"+enc(value.id)),sourceBuildId:value.sourceBuildId})));
        c.repairs=[...setupRows,...builds.filter(value=>value.status==="fulfilled").map(value=>value.value)];
      }
      if(results[3].status==="fulfilled")c.experiences=results[3].value.items||[];
      if(results[4].status==="fulfilled")c.autoRows=results[4].value.items||[];
      await reconcileAuto(c);
      adoptInstall(c);
      await Promise.allSettled(Object.keys(c.logs).map(id=>readBuildLog(c,id)));
      renderInstallJobs(c);renderBuilds(c);renderRepairs(c);renderExperiences(c);updateSteps(c);
    }catch(error){if(!c.closed)c.error.textContent=error.message;}finally{schedule(c);}
  }
  async function load(c) {
    const results=await Promise.allSettled([api("/setup/kun"),api("/setup/codex-installs"),api("/setup/repairs"),api("/setup/experiences"),api("/build-center/sources"),api("/setup/auto-repairs")]);
    if(c.closed)return;
    if(results[0].status==="fulfilled"){
      c.kun=results[0].value;
      if(!c.endpoint)c.endpoint=c.kun.endpoint||"";if(!c.model)c.model=c.kun.model||"";if(!c.keyEnv)c.keyEnv=c.kun.apiKeyEnv||"";if(!c.kunPath)c.kunPath=c.kun.kunPath||"";
    }
    if(results[1].status==="fulfilled")c.installJobs=results[1].value.items||[];
    if(results[2].status==="fulfilled")c.repairs=results[2].value.items||[];
    if(results[3].status==="fulfilled")c.experiences=results[3].value.items||[];
    if(results[4].status==="fulfilled"){c.catalog=results[4].value;if(!c.group)c.group=c.catalog.defaultGroup||"international";}
    if(results[5].status==="fulfilled")c.autoRows=results[5].value.items||[];
    await reconcileAuto(c);
    adoptInstall(c);
    saveDraft(c);render(c);schedule(c);
    await Promise.allSettled([scan(c),loadDocker(c)]);
  }
  function close(c,completed=false) {
    if(c.closed)return;c.closed=true;clearTimeout(c.timer);c.page.remove();document.body.classList.remove("rd-onboarding","rd-starting");$("#startup-loading")?.remove();
    if(!completed)sessionStorage.setItem("rundesk-setup-dismissed","1");current=null;c.resolve?.(completed);
  }
  async function open(options={}) {
    if(current){current.page.focus();return current.promise;}
    const saved=draft(),c={step:0,closed:false,initial:!!options.initial,setup:options.setup||await api("/setup"),discovery:null,scanning:false,kun:null,installJobs:[],builds:[],logs:{},repairs:[],experiences:[],recipes:[],capabilities:{},catalog:null,publicCatalog:null,publicCatalogError:"",runtimeProfiles:null,dockerMode:["pull","build","local"].includes(saved.dockerMode)?saved.dockerMode:"pull",runtimeDrafts:saved.runtimeDrafts||{},codexRuntime:saved.codexRuntime||{mode:"auto"},dockerCheck:null,receipts:saved.receipts||{},sourceDrafts:saved.sourceDrafts||{},autoRepair:saved.autoRepair!==false,autoAuthorizations:saved.autoAuthorizations||{},autoRows:[],installAdoptRequestId:saved.installAdoptRequestId||"",installAdopted:!!saved.installAdopted,selected:new Set(saved.selected||[]),endpoint:saved.endpoint||"",model:saved.model||"",keyEnv:saved.keyEnv||"",kunPath:saved.kunPath||"",codexPath:saved.codexPath||"",version:saved.version||"0.157.1",group:saved.group||""};
    c.workspaceId=c.setup.settings.workspaceId||ws()?.id||state.workspaces[0]?.id||"";
    if(!c.codexPath&&c.setup.settings.codex)c.codexPath=c.setup.settings.codex;
    c.promise=new Promise(resolve=>{c.resolve=resolve;});current=c;
    c.page=el("main",{id:"setup-onboarding",class:"onboarding",tabindex:-1,"aria-labelledby":"onboarding-title"});
    c.error=el("p",{class:"ob-error",role:"alert",id:"onboarding-error"});
    c.nav=el("nav",{class:"ob-steps","aria-label":tr("初始化步骤","Setup steps")},...steps.map((value,index)=>button("",()=>setStep(c,index),"ob-step")));
    [...c.nav.children].forEach((node,index)=>{node.dataset.setupStep=steps[index][0];node.append(el("span",{class:"ob-step-number"},index+1),el("span",{},el("strong",{},tr(steps[index][1],steps[index][2])),el("small",{},tr(steps[index][3],steps[index][4]))));});
    c.main=el("div",{class:"ob-main"});
    const language=action(c,document.documentElement.lang==="en"?"中文":"English",()=>{const next=document.documentElement.lang==="en"?"zh":"en";localStorage.setItem("rundesk-language",next);saveDraft(c);location.reload();},"ob-language");
    c.page.append(el("header",{class:"ob-header"},el("div",{class:"ob-brand"},el("span",{class:"brandmark"},"r."),"RunDesk",el("small",{},tr("初始化","SETUP"))),el("div",{class:"ob-head-actions"},language,action(c,c.initial?tr("稍后配置","Set up later"):tr("返回工作台","Back to RunDesk"),()=>close(c),"quiet",{id:"onboarding-dismiss"}))),el("div",{class:"ob-layout"},el("aside",{class:"ob-sidebar"},el("p",{},tr("准备你的工作环境","PREPARE YOUR WORKSPACE")),c.nav,el("p",{class:"ob-sidebar-note"},tr("三步完成准备。每一步都可以暂时跳过，之后回来继续。","Get ready in three steps. You can skip any step for now and return later."))),c.main));
    document.body.classList.add("rd-onboarding");document.body.classList.remove("rd-starting");document.body.append(c.page);$("#startup-loading")?.remove();render(c);c.page.focus();
    load(c).catch(error=>{if(!c.closed)c.error.textContent=error.message;});return c.promise;
  }
  function bootFailed(error) {
    const node=$("#startup-loading");if(!node)return;
    if(error.status===401){node.classList.add("hidden");return;}
    node.replaceChildren(el("span",{class:"brandmark"},"r."),el("strong",{},"RunDesk"),el("p",{class:"startup-message"},error.message),button(tr("重新连接","Reconnect"),()=>location.reload()));
  }
  return {open,bootFailed,get preferredInstance(){return preferredInstance;}};
})();
