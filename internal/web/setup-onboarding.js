"use strict";
window.RunDeskOnboarding = (() => {
  const tr = (zh, en) => document.documentElement.lang === "en" ? en : zh;
  const enc = encodeURIComponent, activeJob = value => ["queued", "running", "canceling", "starting", "waiting", "dispatching", "stopping"].includes(value);
  const names = {kun:"Kun",codex:"Codex",docker:"Docker",node:"Node.js",npm:"npm",vllm:"vLLM","llama-server":"llama.cpp"};
  const steps = [
    ["llm", "寻找和配置 LLM", "Find and configure LLM", "发现模型，启用 Kun", "Discover models and enable Kun"],
    ["codex", "配置和安装 Codex", "Configure and install Codex", "复用已有安装或下载安装", "Reuse or install Codex"],
    ["docker", "准备 Docker 环境（可选）", "Prepare Docker environments (optional)", "下载镜像或本机构建，也可稍后再做", "Download, build locally, or do this later"]
  ];
  let current, preferredInstance = "";
  const statusLabels = () => ({ready:tr("已就绪","Ready"),listed:tr("发现模型，待测试","Models found; untested"),succeeded:tr("已完成","Completed"),failed:tr("需要处理","Needs attention"),running:tr("进行中","Running"),queued:tr("排队中","Queued"),interrupted:tr("已中断","Interrupted"),unavailable:tr("未发现","Not found"),error:tr("检测失败","Check failed"),candidate:tr("候选经验","Candidate"),verified:tr("已验证经验","Verified"),auth_required:tr("需要认证","Authentication required"),empty:tr("未返回模型","No models returned"),incompatible:tr("模型列表协议不匹配","Model-list protocol mismatch"),unreachable:tr("连接失败","Connection failed"),timeout:tr("探测超时","Probe timed out"),published:tr("已发布端口","Ports published"),host:tr("共享宿主网络","Host network"),unexposed:tr("未发布端口","No published port"),unsupported:tr("暂不支持此网络","Unsupported network"),inspect_failed:tr("容器检查失败","Container inspection failed"),canceled:tr("已取消","Canceled"),canceling:tr("正在取消","Canceling"),completed:tr("已完成","Completed"),idle:tr("待配置","Not configured"),waiting:tr("等待处理","Waiting"),watching:tr("已开启失败自动修复","Automatic repair enabled"),revalidating:tr("正在复查模型能力","Rechecking model capabilities"),submitted:tr("已交给 Kun","Sent to Kun"),blocked:tr("自动交接需要处理","Handoff needs attention"),disabled:tr("已关闭自动修复","Automatic repair disabled"),inference_only:tr("推理通过，工具未通过","Inference passed; tools failed")});
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
  function setStep(c, index, fromHistory = false) {
    c.step = Math.max(0, Math.min(steps.length - 1, index));
    if (!fromHistory) history.pushState(null, "", "#setup/" + steps[c.step][0]);
    render(c); c.page.scrollTop = 0;
  }
  function updateSteps(c) {
    const ready = [readyKun(c),!!c.codexPath,preparationCounts(c).ready>0];
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
    if (c.step === 1) footer.append(action(c,tr("完成基本设置，稍后准备 Docker","Finish setup; prepare Docker later"),()=>completeSetup(c),"quiet",{"data-ob-skip-docker":""}));
    if (c.step < steps.length - 1) footer.append(action(c,c.step===1?tr("继续：准备 Docker（可选）","Continue: Docker (optional)"):tr("下一步","Next"),()=>setStep(c,c.step+1),"primary",{"data-ob-next":""}));
    else footer.append(c.completeButton);
    c.main.append(footer);
  }
  function environment(c) {
    header(c,"寻找和配置 LLM","Find and configure LLM","自动发现 GPU、本机及 Docker 发布的模型服务，也可以手动连接已有服务。这里配置 Kun 维护助手；日常 Codex 的模型服务单独配置。","Discover GPUs and local or Docker-published model services, or enter an existing service. Configure the Kun maintenance assistant here; everyday Codex providers are configured separately.");
    const data = c.discovery;
    if (!data) {
      c.main.append(card(tr("正在扫描本机环境","Scanning the local environment"), el("p", {role:"status",class:"ob-help"}, c.scanning ? tr("正在检查 GPU、运行环境、本机端口和 Docker 发布的模型服务…","Checking GPUs, executables, local ports and Docker-published model services…") : tr("扫描尚未完成，可以重新检查。","The scan has not completed. You can retry.")), action(c,tr("重新扫描","Scan again"),()=>scan(c)))); return;
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
  function normalizedEndpoint(value) {
    try {
      const url=new URL(value.trim());let path=url.pathname.replace(/\/+$/,"");
      for(const suffix of ["/chat/completions","/models"])if(path.endsWith(suffix))path=path.slice(0,-suffix.length);
      url.pathname=path||"/v1";return url.toString().replace(/\/+$/,"");
    } catch { return value.trim().replace(/\/+$/,""); }
  }
  const modelConfigKey = c => JSON.stringify([normalizedEndpoint(c.endpoint),c.model,c.keyEnv]);
  function savedModelMatches(c) { return !!c.kun&&normalizedEndpoint(c.kun.endpoint||"")===normalizedEndpoint(c.endpoint)&&c.kun.model===c.model&&(c.kun.apiKeyEnv||"")===c.keyEnv; }
  function modelEvidence(c) {
    if(c.modelTest&&c.modelTestKey===modelConfigKey(c))return c.modelTest;
    return savedModelMatches(c)?c.kun?.modelTest:null;
  }
  function serviceList(c) {
    const services=c.discovery?.services||[],containers=c.discovery?.containers||[];
    const list=el("div",{id:"onboarding-discovered-services"},...services.map(value=>{
      const origins=(value.containerIds||[]).map(id=>containers.find(row=>row.id===id)).filter(Boolean);
      const source=value.source==="docker"||origins.length?"Docker" : value.source==="explicit"?tr("手动地址","Manual address"):tr("本机端口","Local port");
      const failed=["unreachable","incompatible","timeout"].includes(value.status),displayStatus=value.status==="unreachable"&&/timeout|deadline|timed out/i.test(value.error||"")?"timeout":value.status;
      const selected=normalizedEndpoint(c.endpoint)===normalizedEndpoint(value.endpoint);
      const node=action(c,"",()=>{chooseService(c,value);render(c);const input=c.page.querySelector("#onboarding-endpoint");input?.focus();input?.scrollIntoView?.({block:"nearest"});},"ob-service"+(selected?" selected":""),{"data-service-endpoint":value.endpoint,"aria-pressed":String(selected)});
      const info=el("span",{class:"ob-service-info"},el("strong",{},value.endpoint),el("small",{},source+(origins.length?" · "+origins.map(row=>row.name||row.id).join("、"):"")));
      if(origins.length)info.append(el("small",{},origins.map(row=>[row.image,row.networkMode].filter(Boolean).join(" · ")).join("；")));
      if(value.models?.length)info.append(el("small",{},value.models.map(model=>model.id).join(" · ")));
      else if(value.authRequired)info.append(el("small",{},tr("选择后填写密钥环境变量名，再探测模型列表。","Select this address, enter a credential environment variable, then retry model discovery.")));
      node.append(el("span",{class:"ob-service-icon","aria-hidden":"true"},"◇"),info,badge(value.authRequired?"auth_required":displayStatus));
      const row=el("article",{"data-service-state":displayStatus,"data-service-source":value.source||""},node);
      if(value.error)row.append(el("p",{class:failed?"ob-error":"ob-help",role:"status"},value.error));
      if(failed||value.authRequired)row.append(el("div",{class:"ob-actions"},action(c,tr("重新探测此地址","Retry this address"),()=>scan(c,value.endpoint),"quiet",{"data-service-retry":value.endpoint})));
      return row;
    }));
    if(!services.length)list.append(el("div",{class:"ob-muted-card"},tr("还没有发现可列出模型的服务。可检查下方 Docker 容器状态，或手动填写服务地址。","No service with a model list was found. Check Docker container status below, or enter a service address.")));
    const inventory=containers.length?el("details",{class:"ob-inventory",open:containers.some(row=>["unexposed","unsupported","inspect_failed"].includes(row.status))},el("summary",{},tr("Docker 容器与端口","Docker containers and ports")),...containers.map(row=>el("article",{class:"ob-muted-card","data-discovery-container":row.id},el("div",{class:"ob-inline"},el("strong",{},row.name||row.id),badge(row.status)),el("p",{class:"ob-help"},[row.image,row.networkMode].filter(Boolean).join(" · ")),row.endpoints?.length?el("p",{class:"ob-help"},row.endpoints.join(" · ")):note(row.status==="unexposed"?"容器没有发布宿主端口，尚不能从此处连接；可以手动填写 RunDesk 可访问的服务地址。":"此容器尚无可探测的发布地址，模型可用性需由实际接口确认。",row.status==="unexposed"?"No host port is published. Enter a service address reachable by RunDesk, or publish the intended port.":"No published endpoint was available to probe; model readiness requires an actual API response."),row.warning?el("p",{class:"ob-help",role:"status"},row.warning):null))):null;
    const warnings=c.discovery?.warnings?.length?el("details",{class:"ob-inventory",open:true},el("summary",{},tr("扫描说明","Scan notes")),...c.discovery.warnings.map(value=>el("p",{class:"ob-help",role:"status"},value))):null;
    return card(tr("发现的模型服务","Discovered model services"),list,inventory,warnings,note("这里只读取模型列表，不会自动调用模型。列表可用后仍需验证推理与工具调用，才能启用 Kun 维护。","Discovery only reads model lists and does not invoke a model. Inference and tool-call checks are still required before enabling Kun maintenance."));
  }
  function chooseService(c,service) {
    const same=normalizedEndpoint(c.endpoint)===normalizedEndpoint(service.endpoint);
    c.endpoint=service.endpoint;
    if(service.models?.length&&!service.models.some(row=>row.id===c.model))c.model=service.models[0].id;
    else if(!same&&!service.models?.length)c.model="";
    saveDraft(c);
  }
  async function scan(c,endpoint) {
    if(c.scanning)return;
    const before=modelConfigKey(c);c.scanning=true;
    if(c.step===0)render(c);
    try {
      const found=await api("/setup/discovery",endpoint?{method:"POST",body:{endpoints:[endpoint],...(c.keyEnv?{apiKeyEnv:c.keyEnv}:{})}}:{});
      if(!c.closed){
        c.discovery=found;
        const local=(found.services||[]).find(value=>value.status==="listed"&&value.models?.length);
        if(!c.endpoint&&local&&modelConfigKey(c)===before)chooseService(c,local);
        if(endpoint&&modelConfigKey(c)===before&&normalizedEndpoint(c.endpoint)===normalizedEndpoint(endpoint)&&!c.model){
          const requested=(found.services||[]).find(value=>normalizedEndpoint(value.endpoint)===normalizedEndpoint(endpoint)&&value.models?.length);
          if(requested){c.model=requested.models[0].id;saveDraft(c);}
        }
        const installed=(found.tools||[]).find(value=>value.id==="codex"&&value.status==="ready");
        if(!c.codexPath&&installed)c.codexPath=installed.path;
        const worker=(found.tools||[]).find(value=>value.id==="kun"&&value.status==="ready");
        if(!c.kunPath&&worker)c.kunPath=worker.path;
        updateSteps(c);
        const path=c.page.querySelector("#onboarding-codex-path");if(path&&!path.value)path.value=c.codexPath;
        const status=c.page.querySelector("#onboarding-codex-status");if(status&&installed){status.dataset.state="ready";status.textContent=installed.version||tr("检测到可用安装","Installation found");}
      }
    } finally {c.scanning=false;if(!c.closed&&c.step===0)render(c);}
  }
  function kun(c) {
    if(c.kun?.ready&&savedModelMatches(c))c.main.append(el("div",{class:"ob-notice",role:"status"},tr("Kun 维护助手已通过 worker 协议、模型推理与工具调用检查。","The Kun maintenance assistant passed worker protocol, inference and tool-call checks.")));
    const endpoint=el("input",{id:"onboarding-endpoint",value:c.endpoint,placeholder:"http://127.0.0.1:8000/v1",autocomplete:"url"});
    const model=el("input",{id:"onboarding-model",value:c.model,placeholder:tr("服务返回的模型 ID","Model ID returned by the service"),list:"onboarding-models"});
    const key=el("input",{id:"onboarding-key-env",value:c.keyEnv,placeholder:"OPENAI_API_KEY",autocomplete:"off"});
    const kunPath=el("input",{id:"onboarding-kun-path",value:c.kunPath,placeholder:tr("RunDesk 同目录的 kun，或填写绝对路径","Adjacent kun executable, or an absolute path")});
    const bind=(node,key)=>node.oninput=()=>{c[key]=node.value.trim();if(key!=="kunPath"){results.replaceChildren(note("连接草稿已修改，请重新测试后启用。","The connection draft changed. Test it again before enabling maintenance."));}saveDraft(c);};bind(endpoint,"endpoint");bind(model,"model");bind(key,"keyEnv");bind(kunPath,"kunPath");
    const results=el("div",{id:"onboarding-model-result",role:"status"});
    const test=action(c,tr("测试并启用 Kun","Test and enable Kun"),async()=>{
      results.replaceChildren(note("正在实际测试模型推理和工具调用…","Testing actual inference and tool calls…"));
      const key=modelConfigKey(c),body={endpoint:c.endpoint,model:c.model,...(c.keyEnv?{apiKeyEnv:c.keyEnv}:{})};
      const tested=await api("/setup/model-test",{method:"POST",body});
      if(c.closed)return;
      if(modelConfigKey(c)!==key){render(c);c.error.textContent=tr("模型配置在测试期间发生变化；请重新测试当前地址与模型。","The model configuration changed during testing. Test the current address and model again.");return;}
      c.modelTest=tested;c.modelTestKey=key;await saveKun(c);render(c);
      if(!c.modelTest.inferenceOK||!c.modelTest.toolsOK)c.error.textContent=c.modelTest.error||tr("模型需要同时通过推理与工具调用测试。","The model must pass both inference and tool-call tests.");
    },"primary",{id:"onboarding-enable-kun"});
    const config=card(tr("Kun 维护模型连接","Kun maintenance model connection"),el("div",{class:"ob-grid"},el("label",{class:"ob-wide"},tr("Chat Completions API 地址","Chat Completions API base URL"),endpoint),el("label",{},tr("模型","Model"),model),el("label",{},tr("密钥环境变量名（可留空）","Credential environment variable (optional)"),key)),el("datalist",{id:"onboarding-models"},...(c.discovery?.services||[]).flatMap(value=>(value.models||[]).map(item=>el("option",{value:item.id})))),note("地址与环境变量属于服务器。无需认证可留空；需要密钥时，填写已在 RunDesk 服务进程中设置的变量名。","Addresses and environment variables belong to the server. Leave the credential field empty for unauthenticated services; otherwise enter a variable already set for the RunDesk process."),el("details",{},el("summary",{},tr("Kun 可执行文件","Kun executable")),el("label",{},tr("服务器上的 Kun 路径","Kun path on the server"),kunPath),note("将与你当前 RunDesk 兼容的独立 Kun 程序放在 RunDesk 同目录，或在这里指定路径。缺少程序时仍可保存模型配置。","Place a compatible standalone Kun executable next to RunDesk, or specify its path here. The model configuration can be saved before the executable is available.")),el("div",{class:"ob-actions"},test,action(c,tr("探测此服务的模型列表","Discover models at this address"),()=>scan(c,c.endpoint)),action(c,tr("保存配置草稿","Save configuration draft"),async()=>{await saveKun(c);render(c);})),results);
    const checked=modelEvidence(c);if(checked){const v=checked;results.append(el("p",{class:"ob-inline"},badge(v.inferenceOK?"ready":"failed",tr("模型推理","Inference")),badge(v.toolsOK?"ready":"failed",tr("工具调用","Tool calls"))),v.error?el("p",{class:"ob-error"},v.error):null);}
    const stateCard=card(tr("已保存的 Kun 维护状态","Saved Kun maintenance status"),el("div",{class:"ob-inline"},badge(c.kun?.ready?"ready":c.kun?.available?"idle":"unavailable",c.kun?.ready?tr("可以执行修复","Ready for repairs"):c.kun?.available?tr("模型尚未通过完整测试","Model checks incomplete"):tr("Kun worker 尚不可用","Kun worker unavailable"))),note("此配置仅供 Kun 初始化、安装修复和内部维护使用。日常会话与应用使用 Codex，模型服务在 Codex 设置中配置。","This configuration serves Kun initialization, installation repair and internal maintenance. Everyday conversations and applications use Codex, whose provider is configured separately."));
    if(c.kun?.error)stateCard.append(el("p",{class:"ob-error"},c.kun.error));
    if(c.kun?.worker)stateCard.append(note("已连接 worker：","Connected worker: "),el("code",{},[c.kun.worker.engineVersion,"protocol "+c.kun.worker.protocolVersion].filter(Boolean).join(" · ")));
    c.main.append(config,stateCard);
  }
  async function saveKun(c) {
    c.kun=await api("/setup/kun",{method:"POST",body:{endpoint:c.endpoint,model:c.model,apiKeyEnv:c.keyEnv,kunPath:c.kunPath,...(c.kun?.revision!=null?{expectedRevision:c.kun.revision}:{})}});
    c.kunPath=c.kun.kunPath||c.kunPath;saveDraft(c);updateSteps(c);
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
      if(intent.kind==="image-build"){await hydrateBuildRepairs(c,false);renderBuilds(c);}
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
    target.replaceChildren(...automatic,...c.repairs.filter(value=>!known.has(value.id)).slice(0,8).map(value=>el("article",{class:"ob-job"},el("div",{class:"ob-job-top"},el("strong",{},value.sourceBuildId?tr("构建修复","Build repair"):tr("Codex 安装修复","Codex installation repair")),badge(value.status)),value.error?el("p",{class:"ob-error"},value.error):null,value.maintenance?RunDeskBuildCenter.maintenanceView(value.maintenance):null,value.sessionId?el("a",{href:"/?session="+enc(value.sessionId),target:"_blank",rel:"noopener",class:"ob-help"},tr("查看对话、执行轨迹与修复产物 ↗","View conversation, trace and repair artifacts ↗")):null)));
    for(const intent of Object.values(c.autoAuthorizations).filter(value=>value.sourceId&&["blocked","unconfirmed"].includes(value.state)))target.append(el("div",{class:"ob-notice"},intent.error||tr("自动修复注册待确认。","Automatic repair registration is unconfirmed."),el("div",{class:"ob-actions"},action(c,tr("重试注册自动修复","Retry automatic repair registration"),async()=>{intent.state="pending";intent.attempts=0;await registerAuto(c,intent);renderRepairs(c);renderInstallJobs(c);renderBuilds(c);}))));
    if(!c.repairs.length&&!c.autoRows.length)target.append(note("失败时可以委托 Kun。修复过程保留任务、修改和验证记录。","Delegate to Kun when a step fails. The task, changes and validation results remain available."));
  }
  function docker(c) {
    header(c,"准备 Docker 环境","Prepare Docker environments","选择一种方式准备镜像。下载使用现成环境；本机构建会执行配方，失败后可由 Kun 修复。此步可以稍后再做。","Choose how to prepare an image. Download a ready-made environment, or build a recipe locally with optional Kun repair. You can do this step later.");
    const dockerState=c.dockerCheck;
    const status=el("section",{class:"ob-docker-check","aria-label":tr("Docker 状态","Docker status")},el("div",{class:"ob-inline"},el("strong",{},"Docker Engine"),badge(dockerState?.ok?"ready":dockerState?"unavailable":"idle",dockerState?.ok?tr("可用","Available"):dockerState?tr("尚不可用","Unavailable"):tr("尚未检查","Not checked"))),action(c,tr("重新检查","Check again"),async()=>{c.dockerCheck=await api("/setup/check",{method:"POST",body:{kind:"docker"}});render(c);},"quiet",{id:"onboarding-check-docker"}));
    if(dockerState&&!dockerState.ok)status.append(el("p",{class:"ob-error"},dockerState.error||""));c.main.append(status);
    const mode=el("fieldset",{id:"onboarding-docker-mode",class:"ob-methods"},el("legend",{},tr("怎样准备镜像？","How would you like to prepare the image?")));
    for(const [value,zh,en,descriptionZh,descriptionEn] of [
      ["pull","下载现成镜像","Download a ready image","已有发布镜像，直接下载并检查，无需执行构建配方。","Download and check a published image without running a build recipe."],
      ["build","从配方本机构建","Build a recipe locally","在这台服务器安装依赖、生成镜像；失败时可交给 Kun 修复。","Install dependencies and create an image on this server, with optional Kun repair on failure."],
      ["local","使用本机已有镜像","Use an existing local image","只检查，不下载或重新构建。","Check an existing image without downloading or rebuilding."]
    ]){
      const input=el("input",{type:"radio",name:"onboarding-docker-mode",value,checked:c.dockerMode===value});
      input.onchange=()=>{if(!input.checked)return;captureSources(c);c.dockerMode=value;saveDraft(c);render(c);c.page.querySelector('input[name="onboarding-docker-mode"]:checked')?.focus({preventScroll:true});};
      mode.append(el("label",{class:"ob-method"+(value==="local"?" ob-method-local":""),"data-docker-method":value},input,el("span",{},el("strong",{},tr(zh,en)),el("small",{},tr(descriptionZh,descriptionEn)))));
    }
    c.main.append(mode);
    if(c.dockerMode==="build")buildDocker(c);else acquireDocker(c);
    const progress=card(tr("准备进度","Preparation progress"),note("每个任务的进度、失败原因和 Kun 修复记录都在同一处。","Progress, failures and linked Kun repairs stay together for each task."),el("div",{id:"onboarding-build-history"}));progress.id="onboarding-preparation-progress";c.main.append(progress);renderBuilds(c);
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
        c.builds=[value,...c.builds.filter(row=>row.id!==value.id)];renderBuilds(c);revealPreparation(c,value.id);schedule(c);
        if(intent){intent.sourceId=value.id;saveDraft(c);try{await registerAuto(c,intent);}catch(error){registrationErrors.push(error.message);}renderRepairs(c);}
      }
      for(const key of completedKeys)delete c.receipts[key];saveDraft(c);
      await hydrateBuildRepairs(c);renderBuilds(c);
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
      delete c.receipts[key];saveDraft(c);c.builds=[job,...c.builds.filter(value=>value.id!==job.id)];renderBuilds(c);revealPreparation(c,job.id);schedule(c);
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
    await hydrateBuildRepairs(c);
    const capabilities=await Promise.allSettled(c.recipes.map(async value=>[value.id,await api("/build-center/recipes/"+enc(value.id)+"/sources")]));
    for(const result of capabilities)if(result.status==="fulfilled")c.capabilities[result.value[0]]=result.value[1];
    if(!c.closed){if(c.step===2)render(c);schedule(c);}
  }
  const acquiredBuild = value => value.scope !== "image-test" && (value.source === "pull" || value.source === "local");
  const repairTime = value => Date.parse(value.created)||0;
  const repairFraction = value => (/\.(\d+)(?:Z|[+-]\d\d:\d\d)$/.exec(value.created||"")?.[1]||"").padEnd(9,"0");
  const repairRuns = (c,id) => c.repairs.filter(value=>value.sourceBuildId===id).sort((a,b)=>repairTime(b)-repairTime(a)||repairFraction(b).localeCompare(repairFraction(a))||b.id.localeCompare(a.id));
  function repairInProgress(run) {
    if(activeJob(run.status))return true;
    if(["failed","interrupted","canceled","cancelled","stopped"].includes(run.status))return false;
    return ["building","verifying","replanning"].includes(run.maintenance?.phase);
  }
  function pendingBuildRepair(c,sourceId) {
    if(Object.values(c.autoAuthorizations).some(value=>value.kind==="image-build"&&value.sourceId===sourceId&&value.state!=="registered"))return true;
    const automatic=sourceAuto(c,"image-build",sourceId);
    if(!automatic)return false;
    if(automatic.status!=="submitted")return true;
    const linked=c.repairs.find(value=>value.id===automatic.repairId&&value.sourceBuildId===sourceId);
    // "submitted" records a completed handoff, not the current worker state.
    // An ended, unverified run can be retried explicitly without authorizing
    // another background launch from the original automatic handoff.
    return !linked||repairInProgress(linked)||!["completed","succeeded","failed","interrupted","canceled","cancelled","stopped"].includes(linked.status);
  }
  function verifiedRepair(c,run) {
    const maintenance=run.maintenance;
    if(maintenance?.phase!=="verified"||!maintenance.verifiedBuildId)return null;
    const attempt=(maintenance.attempts||[]).find(value=>value.buildId===maintenance.verifiedBuildId&&value.status==="succeeded"&&value.offline&&value.smoke&&value.imageId);
    if(!attempt)return null;
    const build=c.builds.find(value=>value.id===attempt.buildId);
    if(build&&(build.scope==="image-test"||build.status!=="succeeded"||build.ownerRunId&&build.ownerRunId!==run.id||build.rebuildOf&&build.rebuildOf!==run.sourceBuildId||build.imageId&&build.imageId!==attempt.imageId))return null;
    return build||{id:attempt.buildId,imageId:attempt.imageId,status:"succeeded",ownerRunId:run.id,rebuildOf:run.sourceBuildId};
  }
  function preparationProjection(c) {
    const jobs=new Map(c.builds.map(value=>[value.id,value])),claims=new Map();
    for(const id of new Set([...c.repairs.map(value=>value.sourceBuildId),...c.autoRows.filter(value=>value.kind==="image-build").map(value=>value.sourceId)].filter(Boolean)))if(!jobs.has(id))jobs.set(id,{id,name:tr("原始构建记录不可用","Original build record unavailable"),status:"unknown",scope:"repair-source",_missingSource:true});
    for(const run of c.repairs.filter(value=>value.sourceBuildId&&jobs.has(value.sourceBuildId)))for(const attempt of run.maintenance?.attempts||[]){
      const build=jobs.get(attempt.buildId);
      if(!build||build.scope==="image-test"||build.id===run.sourceBuildId||build.ownerRunId&&build.ownerRunId!==run.id||build.rebuildOf&&build.rebuildOf!==run.sourceBuildId)continue;
      if(!claims.has(build.id))claims.set(build.id,new Set());claims.get(build.id).add(run.sourceBuildId);
    }
    const parent=new Map([...claims].filter(([,ids])=>ids.size===1).map(([id,ids])=>[id,[...ids][0]]));
    // Tests only attach through their explicit source candidate; their sourceBuildId
    // is not a repair parent. Unknown records remain visible as separate records.
    for(const value of c.builds.filter(value=>value.scope==="image-test")){
      const run=c.repairs.find(run=>jobs.has(run.sourceBuildId)&&(!value.ownerRunId||value.ownerRunId===run.id)&&(run.maintenance?.attempts||[]).some(attempt=>attempt.buildId===value.sourceBuildId&&(attempt.verificationIds||[]).includes(value.id)));
      if(run)parent.set(value.id,run.sourceBuildId);
    }
    const roots=[...jobs.values()].filter(value=>!parent.has(value.id));
    const newest=value=>Math.max(Date.parse(value.created||value.updated)||0,...c.repairs.filter(run=>run.sourceBuildId===value.id).map(run=>Date.parse(run.created)||0),...c.builds.filter(child=>parent.get(child.id)===value.id).map(child=>Date.parse(child.created||child.updated)||0));
    roots.sort((a,b)=>newest(b)-newest(a));
    return {roots,parent};
  }
  function preparationState(c,value) {
    if(value.scope==="image-test")return value.status;
    if(value._missingSource)return repairRuns(c,value.id).some(repairInProgress)?"repairing":"unknown";
    if(repairRuns(c,value.id).some(run=>verifiedRepair(c,run)))return "ready";
    if(value.status==="succeeded"&&!value.ownerRunId)return "ready";
    if(repairRuns(c,value.id).some(repairInProgress))return "repairing";
    const automatic=sourceAuto(c,"image-build",value.id);
    const linked=automatic?.repairId&&c.repairs.find(run=>run.id===automatic.repairId&&run.sourceBuildId===value.id);
    if(["failed","interrupted","canceled"].includes(value.status)&&automatic&&!linked&&["watching","revalidating","submitted"].includes(automatic.status))return "repairing";
    return value.status;
  }
  function preparationCounts(c) {
    const roots=preparationProjection(c).roots.filter(value=>value.scope!=="image-test"&&!value._missingSource);
    return {ready:roots.filter(value=>preparationState(c,value)==="ready").length,running:roots.filter(value=>activeJob(preparationState(c,value))||preparationState(c,value)==="repairing").length};
  }
  function refreshPreparationSummary(c) {
    const counts=preparationCounts(c),target=c.page.querySelector("#onboarding-docker-summary");
    if(target)target.textContent=counts.ready+tr(" 个准备成功"," ready")+(counts.running?" · "+counts.running+tr(" 个进行中"," running"):"");
  }
  function revealPreparation(c,id) {
    const node=[...c.page.querySelectorAll("[data-build-id]")].find(value=>value.dataset.buildId===id)||c.page.querySelector("#onboarding-preparation-progress");
    node?.scrollIntoView?.({block:"nearest",behavior:"smooth"});
  }
  function mergeBuildRepair(c,value,sourceId) {
    if(!value?.id||!value.sourceBuildId||sourceId&&value.sourceBuildId!==sourceId)return false;
    c.repairs=[value,...c.repairs.filter(row=>row.id!==value.id)];return true;
  }
  function acceptBuildRepair(key,request,run,sourceId) {
    // A reply can arrive after setup was closed and reopened. Settle only its
    // own receipt, and publish the accepted task into the live controller.
    // Never write the closed controller's older input snapshot over a new draft.
    const live=current&&!current.closed?current:null;
    if(live){
      if(live.receipts[key]===request){delete live.receipts[key];saveDraft(live);}
      mergeBuildRepair(live,run,sourceId);renderBuilds(live);updateSteps(live);schedule(live);
      return;
    }
    const saved=draft();
    if(saved&&typeof saved==="object"&&saved.receipts?.[key]===request){
      delete saved.receipts[key];
      try{sessionStorage.setItem("rundesk-onboarding-draft",JSON.stringify(saved));}catch{}
    }
  }
  async function hydrateBuildRepairs(c,discover=true) {
    if(c.repairHydrating)return c.repairHydrating;
    c.buildRepairLists||={};c.buildRepairErrors||={};
    c.repairHydrating=(async()=>{
      const ids=new Map(c.repairs.filter(value=>value.sourceBuildId).map(value=>[value.id,value.sourceBuildId]));
      for(const row of c.autoRows.filter(value=>value.kind==="image-build"&&value.repairId))ids.set(row.repairId,row.sourceId);
      const roots=discover?preparationProjection(c).roots.filter(value=>value.scope!=="image-test"&&!value._missingSource&&!acquiredBuild(value)&&["failed","interrupted","canceled"].includes(value.status)).slice(0,12):[];
      const requests=[...ids].map(async([id,sourceId])=>{
        try{
          const value=await api("/build-center/assistant-runs/"+enc(id));if(c.closed)return;
          if(value.id!==id||!mergeBuildRepair(c,value,sourceId))throw Error(tr("修复记录的身份或来源不一致，未合并到此任务。","The repair identity or source does not match; this record was not merged into the task."));
          delete c.buildRepairErrors[id];
        }catch(error){if(!c.closed)c.buildRepairErrors[id]=error.message;}
      });
      for(const source of roots){
        if(c.buildRepairLists[source.id]?.loaded)continue;
        c.buildRepairLists[source.id]={loading:true};
        requests.push((async()=>{
          try{
            const result=await api("/build-center/assistant-runs?sourceBuildId="+enc(source.id));if(c.closed)return;
            if(!Array.isArray(result.items))throw Error(tr("修复列表格式无效。","The repair list response is invalid."));
            let mismatch=false;for(const value of result.items)if(!mergeBuildRepair(c,value,source.id))mismatch=true;
            c.buildRepairLists[source.id]={loaded:true,hasMore:!!result.hasMore,...(mismatch?{error:tr("部分修复记录的来源不一致，未合并。","Some repair records have a different source and were not merged.")}: {})};
          }catch(error){if(!c.closed)c.buildRepairLists[source.id]={error:error.message};}
        })());
      }
      await Promise.allSettled(requests);
    })();
    try{await c.repairHydrating;}finally{c.repairHydrating=null;}
  }
  function repairPhase(c,run) {
    if(run.maintenance?.phase==="verified"&&!verifiedRepair(c,run))return tr("修复结果尚未确认通过检查","Repair checks are not confirmed");
    if(!repairInProgress(run)&&run.maintenance?.phase!=="verified")return tr("修复仍需处理","Repair needs attention");
    const labels={planning:tr("Kun 正在诊断","Kun is diagnosing"),building:tr("正在重新构建","Rebuilding"),verifying:tr("正在检查修复结果","Checking the repair"),replanning:tr("Kun 正在调整方案","Kun is revising the plan"),verified:tr("修复与检查已通过","Repair and checks passed"),blocked:tr("修复仍需处理","Repair needs attention")};
    return labels[run.maintenance?.phase]||statusLabels()[run.status]||run.status||tr("正在读取修复状态","Reading repair status");
  }
  function repairLink(sessionId,focusKey) {
    return el("a",{href:"/?session="+enc(sessionId),target:"_blank",rel:"noopener",class:"ob-repair-link","data-ob-focus":focusKey},tr("查看 Kun 修复过程与轨迹 ↗","View Kun repair and trace ↗"));
  }
  function buildLogControl(c,id,label=tr("查看日志","View log")) {
    return action(c,label,async()=>{await readBuildLog(c,id);renderBuilds(c);schedule(c);},"quiet",{"data-ob-focus":"log-button:"+id});
  }
  function buildLog(c,id,label) {
    if(!c.logs[id])return null;
    return el("details",{open:true,"data-ob-detail":"log:"+id,class:"ob-task-log"},el("summary",{},label),el("pre",{"data-build-log":id},c.logs[id].text||tr("尚无日志。","No logs yet.")));
  }
  function appendRepairRun(c,target,run) {
    const verified=verifiedRepair(c,run),maintenance=run.maintenance;
    const row=el("section",{class:"ob-linked-repair","data-repair-id":run.id},el("div",{class:"ob-job-top"},el("strong",{},repairPhase(c,run)),badge(verified?"ready":repairInProgress(run)?"repairing":"failed",verified?tr("已验证","Verified"):tr("Kun 修复","Kun repair"))));
    if(c.buildRepairErrors?.[run.id])row.append(el("p",{class:"ob-error",role:"status"},tr("修复状态读取失败，显示上次记录：","Could not refresh the repair; showing its last record: ")+c.buildRepairErrors[run.id]));
    if(run.error)row.append(el("p",{class:"ob-error"},run.error));
    if(run.sessionId)row.append(repairLink(run.sessionId,"repair-link:"+run.id));
    if(maintenance?.stopReason)row.append(el("p",{class:"ob-error"},maintenance.stopReason));
    const attempts=maintenance?.attempts||[],earlier=attempts.length>1?el("details",{"data-ob-detail":"earlier-attempts:"+run.id},el("summary",{},tr("之前的改进（","Earlier attempts (")+(attempts.length-1)+tr("）",")"))):null;
    for(const [index,attempt] of attempts.entries()){
      const job=c.builds.find(value=>value.id===attempt.buildId),matches=!job||(!job.ownerRunId||job.ownerRunId===run.id)&&(!job.rebuildOf||job.rebuildOf===run.sourceBuildId);
      const child=el("article",{class:"ob-build-attempt","data-build-attempt":attempt.buildId||"","data-attempt-state":verified?.id===attempt.buildId?"ready":attempt.status||"unknown"},el("div",{class:"ob-job-top"},el("strong",{},tr("第 ","Attempt ")+(index+1)+tr(" 次改进","")),badge(verified?.id===attempt.buildId?"ready":attempt.verificationFailed?"failed":attempt.status,verified?.id===attempt.buildId?tr("构建与检查通过","Build and checks passed"):attempt.status==="succeeded"?tr("镜像已构建，等待检查通过","Image built; checks pending"):statusLabels()[attempt.status]||attempt.status)));
      if(!matches)child.append(el("p",{class:"ob-error"},tr("候选记录关联冲突，未将其作为此任务的结果。","The candidate association conflicts; it is not treated as this task's result.")));
      if(attempt.plan?.goal)child.append(el("p",{class:"ob-help"},attempt.plan.goal));
      if(attempt.error)child.append(el("p",{class:"ob-error"},attempt.error));
      child.append(el("div",{class:"ob-attempt-checks"},badge(attempt.offline?"ready":"idle","Offline · "+(attempt.offline?tr("通过","passed"):tr("未通过检查","not verified"))),badge(attempt.smoke?"ready":"idle","Smoke · "+(attempt.smoke?tr("通过","passed"):tr("未通过检查","not verified")))));
      if(attempt.buildId){child.append(el("p",{class:"ob-task-id"},attempt.buildId),buildLogControl(c,attempt.buildId,tr("重新构建日志","Rebuild log")));const log=buildLog(c,attempt.buildId,tr("重新构建日志","Rebuild log"));if(log)child.append(log);if(!job)child.append(note("此处显示修复任务保留的候选快照；构建记录未出现在当前列表中。","This candidate comes from the repair snapshot; its build record is not in the current list."));}
      const details=el("details",{"data-ob-detail":"plan:"+run.id+":"+index},el("summary",{},tr("改进方案与检查记录","Plan and check records")),el("p",{class:"ob-help"},attempt.plan?.hypothesis||""),el("p",{class:"ob-help"},tr("保留：","Preserve: ")+(attempt.plan?.preserve||[]).join("；")),el("p",{class:"ob-help"},tr("验收：","Checks: ")+(attempt.plan?.checks||[]).join("；")));
      for(const id of attempt.verificationIds||[]){details.append(el("div",{class:"ob-inline"},el("code",{},id),buildLogControl(c,id,tr("检查日志","Check log"))));const log=buildLog(c,id,tr("检查日志","Check log"));if(log)details.append(log);}
      child.append(details);if(index===attempts.length-1)row.append(child);else earlier.append(child);
    }
    if(earlier)row.append(earlier);
    if(maintenance?.experienceId)row.append(note("本次修复经验已保存，后续可按适用条件参考。","This repair experience was saved for future tasks with matching conditions."));
    target.append(row);
  }
  function appendBuildRepair(c,target,value) {
    const runs=repairRuns(c,value.id),autos=c.autoRows.filter(row=>row.kind==="image-build"&&row.sourceId===value.id),list=c.buildRepairLists?.[value.id];
    const region=el("section",{class:"ob-build-repair","data-build-repair-region":value.id});
    for(const automatic of autos){
      const row=el("div",{class:"ob-handoff","data-auto-repair":automatic.id},el("div",{class:"ob-inline"},el("strong",{},tr("失败自动交接","Automatic failure handoff")),badge(automatic.status)));
      if(automatic.error)row.append(el("p",{class:"ob-error"},automatic.error));
      if(automatic.sessionId&&!runs.some(run=>run.id===automatic.repairId&&run.sessionId))row.append(repairLink(automatic.sessionId,"auto-link:"+automatic.id));
      if(automatic.repairId&&!runs.some(run=>run.id===automatic.repairId))row.append(el("p",{class:"ob-help",role:"status"},c.buildRepairErrors?.[automatic.repairId]||tr("已交接，正在读取对应修复任务。","Handed off; reading the linked repair task.")));
      if(automatic.enabled&&["watching","blocked","failed"].includes(automatic.status))row.append(action(c,tr("关闭本次自动交接","Disable this automatic handoff"),async()=>{
        const updated=await api("/setup/auto-repairs",{method:"POST",body:{kind:automatic.kind,sourceId:automatic.sourceId,enabled:false,requestId:receipt(c,"auto-disable:"+automatic.id)}});
        c.autoRows=c.autoRows.map(item=>item.id===automatic.id?updated:item);renderBuilds(c);schedule(c);
      },"quiet",{"data-auto-disable":value.id,"data-ob-focus":"auto-disable:"+automatic.id}));
      region.append(row);
    }
    for(const intent of Object.values(c.autoAuthorizations).filter(row=>row.kind==="image-build"&&row.sourceId===value.id&&row.state!=="registered")){
      const row=el("div",{class:"ob-notice","data-auto-registration":value.id},intent.error||tr("正在确认失败自动交接。","Confirming automatic failure handoff."));
      if(["blocked","unconfirmed","source_unconfirmed"].includes(intent.state))row.append(el("div",{class:"ob-actions"},action(c,tr("重试注册自动修复","Retry automatic repair registration"),async()=>{intent.state="pending";intent.attempts=0;await registerAuto(c,intent);await hydrateBuildRepairs(c,false);renderBuilds(c);},"quiet",{"data-auto-registration-retry":value.id,"data-ob-focus":"auto-register:"+value.id})));
      region.append(row);
    }
    if(list?.error)region.append(el("p",{class:"ob-error",role:"status"},tr("对应修复记录读取失败：","Could not read linked repairs: ")+list.error),action(c,tr("重新读取修复记录","Reload linked repairs"),async()=>{delete c.buildRepairLists[value.id];await hydrateBuildRepairs(c);renderBuilds(c);},"quiet",{"data-repair-reload":value.id,"data-ob-focus":"repair-reload:"+value.id}));
    if(list?.hasMore)region.append(note("这里只显示最近 20 次对应修复；当前已读取的修复继续保留。","Only the latest 20 linked repairs are listed; already loaded repairs are retained."));
    if(!list?.loaded&&!list?.error&&["failed","interrupted","canceled"].includes(value.status))region.append(el("p",{class:"ob-help",role:"status"},tr("正在读取对应修复记录…","Reading linked repairs…")));
    if(runs.length)appendRepairRun(c,region,runs[0]);
    if(runs.length>1){const previous=el("details",{"data-ob-detail":"repair-history:"+value.id},el("summary",{},tr("之前的修复记录（","Earlier repairs (")+(runs.length-1)+tr("）",")")));for(const run of runs.slice(1))appendRepairRun(c,previous,run);region.append(previous);}
    if(c.buildRepairActionErrors?.[value.id])region.append(el("p",{class:"ob-error",role:"alert"},c.buildRepairActionErrors[value.id]));
    if(region.children.length)target.append(region);
  }
  function finishImageAction(c,value) {
    return action(c,tr("完成初始化并查看镜像","Finish setup and view image"),async()=>{
      const catalog=await api("/build-center/images"),image=catalog.items?.find(row=>row.imageId===value.imageId);
      if(!image)throw Error(tr("镜像尚未出现在构建中心，请稍后刷新。","The image is not in the build center yet. Refresh shortly."));
      await completeSetup(c,image.key);
    },"primary",{"data-runtime-open-image":value.id,"data-ob-focus":"open-image:"+value.id});
  }
  function preparationCard(c,value) {
    const state=preparationState(c,value),acquired=acquiredBuild(value),runs=repairRuns(c,value.id),verified=value._missingSource?null:runs.map(run=>verifiedRepair(c,run)).find(Boolean);
    const method=value._missingSource?tr("修复关联记录","Linked repair record"):value.scope==="image-test"?tr("独立镜像检查","Separate image check"):value.source==="pull"?tr("下载镜像","Image download"):value.source==="local"?tr("本机镜像检查","Local image check"):tr("本机构建","Local build");
    const status=state==="repairing"?tr("Kun 正在修复","Kun is repairing"):state==="ready"?tr("环境已准备","Environment prepared"):state==="unknown"?tr("记录不完整","Incomplete record"):statusLabels()[state]||state;
    const result=el("article",{class:"ob-preparation",tabindex:-1,"data-build-id":value.id,"data-preparation-state":state},el("div",{class:"ob-job-top"},el("div",{class:"ob-task-heading"},el("span",{class:"ob-method-label"},method),el("strong",{},value.recipeName||value.name||value.profileId||value.id)),badge(state,status)));
    if(value.baseImages?.layers?.length){const layers=value.baseImages.layers,ready=layers.filter(layer=>layer.imageId).length;result.append(el("p",{class:"ob-help","data-base-progress":value.id},tr("公共基础层已准备 ","Shared base layers prepared ")+ready+" / "+layers.length+tr("；此任务会自动构建或复用，详情见日志。","; this job builds or reuses them automatically. See the log for details.")));}
    if(value._missingSource)result.append(note("当前没有原始构建记录，以下保留对应的修复状态与候选证据；不计入准备成功。","The original build record is unavailable. Linked repair status and candidate evidence are retained below; this does not count as a prepared environment."));
    if(value.ownerRunId&&!runs.length&&!verified)result.append(note("这是 Kun 生成的候选记录，尚未确认对应的完整修复验证。","This is a Kun candidate record; a complete linked repair verification is not yet confirmed."));
    if(value.scope==="image-test")result.append(note("没有找到此检查对应的准备任务，暂作为独立记录显示；它不计入准备成功。","The preparation task for this check is not available. It is shown separately and does not count as a prepared environment."));
    if(value.error)result.append(el("p",{class:"ob-error"},(state==="repairing"||verified?tr("最初准备失败：","Initial preparation failed: "):"")+value.error));
    if(acquired)result.append(el("p",{class:"ob-help"},value.reference||""));
    if(!acquired&&["failed","interrupted","canceled"].includes(value.status))result.append(el("div",{class:"ob-preparation-path","aria-label":tr("准备与修复流程","Preparation and repair flow")},el("span",{"data-step-state":"failed"},tr("① 原始构建失败","① Initial build failed")),el("span",{"data-step-state":state==="repairing"?"running":runs.length?"ready":"idle"},tr("② Kun 诊断修复","② Kun repair")),el("span",{"data-step-state":verified?"ready":"idle"},verified?tr("③ 重新构建与检查通过","③ Rebuild and checks passed"):tr("③ 重新构建与检查","③ Rebuild and checks"))));
    const ops=el("div",{class:"ob-actions ob-task-actions"},value._missingSource?null:buildLogControl(c,value.id));
    if(activeJob(value.status))ops.append(action(c,tr("取消准备","Cancel preparation"),async()=>{const updated=await RunDeskBuildCenter.mutate("/builds/"+enc(value.id)+"/cancel",{});c.builds=c.builds.map(row=>row.id===value.id?updated:row);renderBuilds(c);schedule(c);},"quiet",{"data-image-cancel":value.id,"data-ob-focus":"cancel:"+value.id}));
    if(["failed","interrupted","canceled"].includes(value.status)){
      if(acquired){
        ops.append(action(c,value.source==="pull"?tr("更换地址或重试","Change source or retry"):tr("重新选择本机镜像","Choose a local image again"),()=>{captureSources(c);c.dockerMode=value.source;c.runtimeDrafts[value.source]={profileId:value.profileId,version:value.codexVersion,reference:value.reference,entryId:"",sourceId:""};saveDraft(c);setStep(c,2);c.page.querySelector("#onboarding-runtime-reference")?.focus();},"primary",{"data-runtime-retry":value.id,"data-ob-focus":"runtime-retry:"+value.id}));
        result.append(note("检查日志后可更换镜像地址或调整仓库登录权限；下载失败不会自动改成本机构建。","Review the log and change the reference or registry access. A failed download never switches to a local build automatically."));
      }else if(value.scope!=="image-test"){
        const repair=action(c,tr("交给 Kun 修复","Ask Kun to repair"),async()=>{
          if(c.buildRepairPending?.[value.id])return;(c.buildRepairPending||={})[value.id]=true;(c.buildRepairActionErrors||={})[value.id]="";renderBuilds(c);
          const receiptKey="build-repair:"+value.id,request=receipt(c,receiptKey);
          try{
            const run=await RunDeskBuildCenter.mutate("/assistant-runs",{sourceBuildId:value.id,instanceId:c.kun.instanceId,workspaceId:c.kun.workspaceId,requestId:request,prompt:tr("诊断并修复这次失败构建。保持工具能力与验证要求，生成独立新配方，实际重建并完成offline/smoke验证，记录适用范围明确的修复经验。","Diagnose and repair this failed build. Preserve the required capabilities and tests. Produce a separate revised recipe, rebuild it, run offline/smoke checks, and record the repair with its applicability conditions.")});
            if(!run?.id||run.sourceBuildId!==value.id)throw Error(tr("修复任务来源不一致，请重新读取记录。","The repair task has a different source. Reload the records."));
            acceptBuildRepair(receiptKey,request,run,value.id);
          }catch(error){if(!c.closed)c.buildRepairActionErrors[value.id]=error.message;}
          finally{if(!c.closed){c.buildRepairPending[value.id]=false;renderBuilds(c);revealPreparation(c,value.id);schedule(c);}}
        },"primary",{"data-build-repair":value.id,"data-ob-focus":"repair:"+value.id});
        repair.disabled=!readyKun(c)||pendingBuildRepair(c,value.id)||!!c.buildRepairPending?.[value.id]||!c.buildRepairLists?.[value.id]?.loaded||!!c.buildRepairLists?.[value.id]?.error||runs.some(run=>repairInProgress(run)||verifiedRepair(c,run));ops.append(repair);
        ops.append(action(c,tr("原配置重试","Retry original configuration"),async()=>{const job=await RunDeskBuildCenter.mutate("/builds/"+enc(value.id)+"/rebuild",{});c.builds=[job,...c.builds.filter(row=>row.id!==job.id)];await hydrateBuildRepairs(c);renderBuilds(c);revealPreparation(c,job.id);schedule(c);},"quiet",{"data-build-retry":value.id,"data-ob-focus":"rebuild:"+value.id}));
        if(!readyKun(c))result.append(note("先在第一步启用 Kun，即可在这里委托修复。","Enable Kun in step 1 to delegate a repair here."));
      }
    }
    result.append(ops);const originalLog=buildLog(c,value.id,tr("原始准备日志","Original preparation log"));if(originalLog)result.append(originalLog);
    if(!acquired&&value.scope!=="image-test")appendBuildRepair(c,result,value);
    const prepared=verified||(state==="ready"?value:null);
    if(prepared){result.append(note("镜像已准备；应用认证和实际业务验证仍需完成。","The image is prepared; application authentication and business validation remain to be completed."));if(prepared.imageId)result.append(el("div",{class:"ob-actions"},finishImageAction(c,prepared)));}
    if(value.codexRuntime)result.append(el("details",{"data-ob-detail":"runtime:"+value.id},el("summary",{},tr("本次构建配置","Frozen build configuration")),RunDeskBuildCenter.frozenRuntimeView(value.codexRuntime)));
    return result;
  }
  function renderBuilds(c) {
    const target=c.page.querySelector("#onboarding-build-history");if(!target)return;
    refreshPreparationSummary(c);
    const signature=JSON.stringify([c.builds,c.repairs,c.autoRows,c.autoAuthorizations,c.buildRepairLists,c.buildRepairErrors,c.buildRepairPending,c.buildRepairActionErrors,Object.entries(c.logs).map(([id,value])=>[id,value.next,value.text])]);
    if(target._signature===signature)return;target._signature=signature;
    const opened=new Map([...target.querySelectorAll("details[data-ob-detail]")].map(value=>[value.dataset.obDetail,value.open]));
    const scrolls=new Map([...target.querySelectorAll("[data-build-log]")].map(value=>[value.dataset.buildLog,{top:value.scrollTop,left:value.scrollLeft,tail:value.clientHeight>0&&value.scrollHeight-value.clientHeight-value.scrollTop<=4}]));
    const active=document.activeElement,inside=target.contains(active),focusKey=inside?active.closest("[data-ob-focus]")?.dataset.obFocus:null,detailKey=inside&&active.tagName==="SUMMARY"?active.parentElement.dataset.obDetail:null,focusSource=inside?active.closest("[data-build-id]")?.dataset.buildId:null;
    const {roots}=preparationProjection(c);target.replaceChildren(...roots.slice(0,12).map(value=>preparationCard(c,value)));
    if(!roots.length)target.append(note("尚未提交任务。选择一种方式准备镜像，也可以直接完成初始化。","No preparation tasks yet. Choose a method, or finish setup now."));
    if(roots.length>12)target.append(note("这里只显示最近 12 个准备任务，已关联的改进与检查合并在原任务内。","Only the latest 12 preparation tasks are shown; linked repairs and checks are grouped with their source task."));
    for(const detail of target.querySelectorAll("details[data-ob-detail]"))if(opened.has(detail.dataset.obDetail))detail.open=opened.get(detail.dataset.obDetail);
    for(const node of target.querySelectorAll("[data-build-log]")){const previous=scrolls.get(node.dataset.buildLog);if(previous){node.scrollTop=previous.tail?node.scrollHeight:previous.top;node.scrollLeft=previous.left;}}
    if(inside){let restored=focusKey?[...target.querySelectorAll("[data-ob-focus]")].find(value=>value.dataset.obFocus===focusKey):detailKey?[...target.querySelectorAll("details[data-ob-detail]")].find(value=>value.dataset.obDetail===detailKey)?.firstElementChild:null;if(!restored||restored.disabled)restored=[...target.querySelectorAll("[data-build-id]")].find(value=>value.dataset.buildId===focusSource);restored?.focus?.({preventScroll:true});}
  }
  async function readBuildLog(c,id) {
    const previous=c.logs[id]||{next:0,text:""};
    const value=await api("/build-center/builds/"+enc(id)+"/log?after="+previous.next);
    const text=previous.text+(value.text||"");
    c.logs[id]={next:value.next,text:text.length>131072?tr("[显示日志末尾]\n","[Showing the end of the log]\n")+text.slice(-131072):text};
  }
  function dailyWorkspace(c) {
    const choices=state.workspaces.filter(value=>value.purpose!=="maintenance");
    return choices.find(value=>value.id===c.workspaceId)||choices.find(value=>value.id===c.setup.settings.workspaceId)||choices[0];
  }
  function finish(c) {
    const {ready:succeeded,running}=preparationCounts(c);
    const summary=el("details",{class:"ob-setup-summary"},el("summary",{},tr("查看三步初始化结果","Review the three setup steps")),
      summaryRow(tr("本机环境","Environment"),c.discovery?tr("已扫描","Scanned"):tr("尚未扫描","Not scanned")),
      summaryRow("Kun",readyKun(c)?badge("ready",tr("可执行修复任务","Ready for repair tasks")):badge("idle",tr("尚未就绪，可稍后配置","Not ready; configure later"))),
      summaryRow("Codex",c.codexPath?c.codexPath:tr("已跳过","Skipped")),
      summaryRow(tr("Docker 镜像","Docker images"),succeeded+tr(" 个准备成功"," ready")+(running?" · "+running+tr(" 个进行中"," running"):"")));
    summary.lastElementChild.lastElementChild.id="onboarding-docker-summary";
    c.main.append(summary);
    const work=el("select",{id:"onboarding-workspace"},...state.workspaces.filter(value=>value.purpose!=="maintenance").map(value=>el("option",{value:value.id},value.name)));work.value=dailyWorkspace(c)?.id||"";work.onchange=()=>{c.workspaceId=work.value;};
    c.completeButton=action(c,tr("完成并进入 RunDesk","Finish and enter RunDesk"),()=>completeSetup(c),"primary",{id:"onboarding-complete"});
    c.main.append(card(tr("完成初始化","Finish setup"),el("label",{},tr("默认工作区","Default workspace"),work),note("点击下方“完成并进入 RunDesk”保存配置。Docker 环境可以稍后准备；已经提交的安装、拉取、构建和修复任务会继续在后台运行。","Finish and enter RunDesk below to save your configuration. Docker preparation is optional; submitted installation, pull, build and repair tasks continue in the background.")));
    const examples=card(tr("积累的修复经验","Repair experience"),el("div",{id:"onboarding-experiences"}));c.main.append(examples);renderExperiences(c);
  }
  async function completeSetup(c,imageKey="") {
    let workspaceId=dailyWorkspace(c)?.id||"";
    const saved=await api("/setup/complete",{method:"POST",body:{workspaceId,revision:c.setup.settings.revision,...(c.codexPath?{codex:c.codexPath}:{}),completed:true}});
    c.setup.settings=saved.settings||saved;workspaceId=c.setup.settings.workspaceId||workspaceId;
    if(workspaceId&&!state.workspaces.some(value=>value.id===workspaceId)){
      state.workspaces=await api("/workspaces");
      $("#workspace").replaceChildren(...state.workspaces.map(value=>el("option",{value:value.id},value.name)));
    }
    sessionStorage.removeItem("rundesk-setup-dismissed");
    if(Object.values(c.autoAuthorizations).every(value=>value.state==="registered"))sessionStorage.removeItem("rundesk-onboarding-draft");else saveDraft(c);
    // The maintenance model is never the default for everyday conversations.
    const selected=state.instances.find(value=>value.id===$("#instance")?.value&&(value.agentRuntime?.kind||"codex")==="codex");
    preferredInstance=selected?.id||"default";localStorage.setItem("rundesk-instance",preferredInstance);
    localStorage.setItem("rundesk-conversation-workspace",workspaceId);
    localStorage.setItem("rundesk-assistant-workspace",workspaceId);if($("#workspace"))$("#workspace").value=workspaceId;
    if(imageKey)history.replaceState(null,"","#build-center/images/"+enc(imageKey));
    else if(c.initial&&(!c.returnHash||c.setupRoute))c.returnHash="#conversations";
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
        c.repairs=[...setupRows,...c.repairs.filter(value=>value.sourceBuildId)];
      }
      if(results[3].status==="fulfilled")c.experiences=results[3].value.items||[];
      if(results[4].status==="fulfilled")c.autoRows=results[4].value.items||[];
      await reconcileAuto(c);
      await hydrateBuildRepairs(c);
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
    if(results[2].status==="fulfilled")c.repairs=[...(results[2].value.items||[]),...c.repairs.filter(value=>value.sourceBuildId)];
    if(results[3].status==="fulfilled")c.experiences=results[3].value.items||[];
    if(results[4].status==="fulfilled"){c.catalog=results[4].value;if(!c.group)c.group=c.catalog.defaultGroup||"international";}
    if(results[5].status==="fulfilled")c.autoRows=results[5].value.items||[];
    await reconcileAuto(c);
    await hydrateBuildRepairs(c,false);
    adoptInstall(c);
    saveDraft(c);render(c);schedule(c);
    await Promise.allSettled([scan(c),loadDocker(c)]);
  }
  function close(c,completed=false,restoreRoute=true) {
    if(c.closed)return;c.closed=true;clearTimeout(c.timer);c.page.remove();document.body.classList.remove("rd-onboarding","rd-starting");$("#startup-loading")?.remove();
    if(restoreRoute && location.hash.startsWith("#setup/"))history.replaceState(null,"",c.returnHash||location.pathname+location.search);
    if(!completed)sessionStorage.setItem("rundesk-setup-dismissed","1");current=null;c.resolve?.(completed);
  }
  async function open(options={}) {
    if(current){current.page.focus();return current.promise;}
    const saved=draft(),requestedStep=options.step||/^#setup\/(llm|codex|docker)$/.exec(location.hash)?.[1],c={step:Math.max(0,steps.findIndex(value=>value[0]===requestedStep)),returnHash:location.hash.startsWith("#setup/")?"#conversations":location.hash,setupRoute:location.hash.startsWith("#setup/"),closed:false,initial:!!options.initial,setup:options.setup||await api("/setup"),discovery:null,scanning:false,kun:null,installJobs:[],builds:[],logs:{},repairs:[],experiences:[],recipes:[],capabilities:{},catalog:null,publicCatalog:null,publicCatalogError:"",runtimeProfiles:null,dockerMode:["pull","build","local"].includes(saved.dockerMode)?saved.dockerMode:"pull",runtimeDrafts:saved.runtimeDrafts||{},codexRuntime:saved.codexRuntime||{mode:"auto"},dockerCheck:null,receipts:saved.receipts||{},sourceDrafts:saved.sourceDrafts||{},autoRepair:saved.autoRepair!==false,autoAuthorizations:saved.autoAuthorizations||{},autoRows:[],installAdoptRequestId:saved.installAdoptRequestId||"",installAdopted:!!saved.installAdopted,selected:new Set(saved.selected||[]),endpoint:saved.endpoint||"",model:saved.model||"",keyEnv:saved.keyEnv||"",kunPath:saved.kunPath||"",codexPath:saved.codexPath||"",version:saved.version||"0.157.1",group:saved.group||""};
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
    document.body.classList.add("rd-onboarding");document.body.classList.remove("rd-starting");document.body.append(c.page);$("#startup-loading")?.remove();history.pushState(null,"","#setup/"+steps[c.step][0]);render(c);c.page.focus();
    load(c).catch(error=>{if(!c.closed)c.error.textContent=error.message;});return c.promise;
  }
  window.addEventListener("hashchange",()=>{
    if(!current||current.closed)return;
    const match=/^#setup\/(llm|codex|docker)$/.exec(location.hash);
    if(!match){close(current,false,false);return;}
    const index=steps.findIndex(step=>step[0]===match[1]);
    if(index!==current.step)setStep(current,index,true);
  });
  function bootFailed(error) {
    const node=$("#startup-loading");if(!node)return;
    if(error.status===401){node.classList.add("hidden");return;}
    node.replaceChildren(el("span",{class:"brandmark"},"r."),el("strong",{},"RunDesk"),el("p",{class:"startup-message"},error.message),button(tr("重新连接","Reconnect"),()=>location.reload()));
  }
  return {open,bootFailed,get preferredInstance(){return preferredInstance;}};
})();
