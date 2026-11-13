"use strict";
window.RunDeskSkillCenter = (() => {
  const base = "/skill-center", enc = encodeURIComponent;
  const tr = (zh, en) => document.documentElement.lang === "en" ? en : zh;
  const sourceNames = {clawhub:"ClawHub", skillsmp:"SkillsMP", "skills.sh":"skills.sh", github:"GitHub"};
  const sourceStates = {ok:["已查询","Queried"],cached:["使用缓存","Cached"],stale:["过期缓存","Older cache"],error:["查询失败","Failed"],disabled:["未查询","Not queried"]};
  const jobStates = {queued:["排队中","Queued"],running:["检查中","Checking"],completed:["已完成","Completed"],failed:["检查失败","Failed"],unavailable:["引擎未配置","Engine unavailable"],canceled:["已取消","Canceled"],interrupted:["检查中断","Interrupted"]};
  const recommendationNames = {no_findings:["未发现规则命中","No rule findings"],review:["需要人工复核","Review required"],incomplete:["检查不完整","Incomplete inspection"],SAFE:["工具建议允许","Tool recommends allowing"],CAUTION:["工具建议谨慎处理","Tool recommends caution"],DO_NOT_INSTALL:["工具不建议安装","Tool advises against installing"]};
  const severityNames = {critical:["严重","Critical"],high:["高","High"],medium:["中","Medium"],low:["低","Low"],info:["提示","Info"]};
  const metricNames = {installs:["安装量","Installs"],downloads:["下载量","Downloads"],github_stars:["GitHub stars","GitHub stars"]};
  const reportEngines = {builtin:["Go 快速检查", "Go quick check"],skillspector:["NVIDIA SkillSpector", "NVIDIA SkillSpector"]};
  let generation = 0, searchSequence = 0, currentTab = "search", selected = "", back;
  let query = "", sourceSelection = ["clawhub", "skillsmp", "skills.sh"], searchResult = null;
  let enginesPromise, pollTimer, requestController, diagnosticsController;
  const packJobs = new Map();
  const page = () => $("#skill-center-page");
  const isAdministrator = () => state.identity?.kind === "administrator";
  const still = g => g === generation && state.view === "skill-center" && location.hash.startsWith("#skills");
  const text = value => value == null || value === "" ? "—" : String(value);
  const translated = (map, key) => map[key] ? tr(...map[key]) : text(key);
  const errorText = error => error?.message || tr("操作未完成，请重试。", "The operation did not complete. Try again.");
  const context = () => ({workspaceId:ws()?.id || "",instanceId:instance()?.id || "default",workspaceName:ws()?.name || "",environmentName:instance()?.name || "Codex"});

  function badge(value, label) { return el("span", {class:"skill-center-badge", "data-skill-state":value}, label || text(value)); }
  function action(label, fn, cls = "quiet", attrs = {}) {
    let busy = false;
    const node = button(label, async () => {
      if (busy) return;
      busy = true; node.disabled = true;
      try { await fn(); } finally { busy = false; node.disabled = false; }
    }, cls);
    for (const [key, value] of Object.entries(attrs)) node.setAttribute(key, value);
    return node;
  }
  function link(label, value) {
    try {
      const url = new URL(value);
      if (!['https:', 'http:'].includes(url.protocol) || url.username || url.password) return null;
      return el("a", {href:url.href,target:"_blank",rel:"noopener noreferrer",class:"skill-center-link"}, label);
    } catch { return null; }
  }
  function date(value) {
    if (!value) return "—";
    const d = new Date(value);
    return Number.isNaN(d.getTime()) ? text(value) : d.toLocaleString(document.documentElement.lang === "en" ? "en" : "zh-CN");
  }
  function acquisitionSource(pack) {
    if (String(pack.sourceUrl || "").startsWith("local-upload://")) return tr("本地 ZIP／文件夹", "Local ZIP / folder");
    if (String(pack.sourceUrl || "").startsWith("rundesk://")) return tr("已安装技能目录的独立快照", "Independent snapshot of an installed skill directory");
    return pack.sourceUrl || "—";
  }
  function message(target, value, type = "help") {
    target.className = type; target.textContent = value; target.hidden = !value;
  }
  function meta(rows) {
    return el("dl", {class:"skill-center-metadata"}, rows.flatMap(([name, value]) => [el("dt", {}, name), el("dd", {}, text(value))]));
  }
  function activeJob(status) { return ["queued", "running"].includes(status); }
  function close() {
    ++generation; clearTimeout(pollTimer); requestController?.abort();
    diagnosticsController?.abort();
    window.RunDeskSkillChecks?.dispose();
    const target = back || {view:"conversation",route:"#conversations"};
    setProductPage(target.view); history.replaceState(null, "", target.route); return restoreProductRoute();
  }
  async function open(tab = "search", id = "") {
    if (state.view !== "skill-center") {
      stashProductDraft();
      back = {view:state.view,route:location.hash.startsWith("#skills") ? contextRoute() : location.hash || contextRoute()};
    }
    const g = ++generation; ++searchSequence; clearTimeout(pollTimer); requestController?.abort();
    diagnosticsController?.abort();
    window.RunDeskSkillChecks?.dispose();
    currentTab = tab === "library" || tab === "pack" ? tab : "search"; selected = id;
    if ($("#settings")?.open) $("#settings").close();
    setProductPage("skill-center");
    history.replaceState(null, "", "#skills/" + currentTab + (id ? "/" + enc(id) : ""));
    const target = page(), content = el("div", {class:"skill-center-content"});
    target.replaceChildren(el("div", {class:"page-intro"}, el("div", {},
      action(tr("← 返回", "← Back"), close, "quiet back-link"),
      el("p", {class:"eyebrow"}, "SKILLS"), el("h1", {}, tr("技能中心", "Skill center")),
      el("p", {}, tr("发现技能，查看完整源码，检查后添加到工作区。", "Discover skills, inspect their source, and check them before adding them to a workspace."))),
      action(tr("刷新", "Refresh"), () => open(currentTab, selected))),
      el("nav", {class:"tabs skill-center-tabs","aria-label":tr("技能中心分类", "Skill center sections")},
        button(tr("搜索技能", "Discover"), () => open("search"), currentTab === "search" ? "selected" : ""),
        button(tr("已获取技能", "Library"), () => open("library"), currentTab !== "search" ? "selected" : "")), content);
    if (!isAdministrator()) {
      content.append(el("div", {class:"skill-center-empty",role:"status"}, tr("技能中心目前仅向管理员开放。", "The skill center is currently available to administrators.")));
      return;
    }
    try {
      if (currentTab === "search") await renderSearch(content, g);
      else if (currentTab === "library") await renderLibrary(content, g);
      else if (id) await renderPack(content, id, g);
      else await open("library");
      if (still(g)) mountDiagnostics(content, g);
    } catch (error) {
      if (still(g)) content.replaceChildren(el("p", {class:"error",role:"alert"}, errorText(error)), action(tr("重试", "Retry"), () => open(currentTab, selected)));
    }
  }
  function providerRows(result) {
    const box = el("section", {class:"skill-center-providers", "aria-label":tr("搜索来源状态", "Search source status")});
    for (const provider of result.providers || []) {
      box.append(el("article", {class:"skill-center-provider","data-provider-state":provider.status},
        el("div", {}, el("strong", {}, sourceNames[provider.source] || provider.source),
          badge(provider.status, translated(sourceStates, provider.status))),
        el("p", {}, provider.message || ""),
        typeof provider.count === "number" ? el("small", {}, tr("来源结果：", "Source results: ") + provider.count) : null));
    }
    return box;
  }
  function resultCard(item) {
    const isRepository = item.kind === "repository";
    const title = el("div", {class:"skill-center-card-title"}, el("h3", {}, item.name || item.repository || tr("未命名技能", "Unnamed skill")),
      el("div", {}, badge("unscanned", tr("未检查", "Not checked")), isRepository ? badge("repository", tr("候选仓库", "Candidate repository")) : null));
    const card = el("article", {class:"skill-center-card"}, title,
      el("p", {class:"skill-center-description"}, item.description || tr("来源未提供描述。", "The source did not provide a description.")));
    const origins = item.origins?.length ? item.origins : [{source:item.source,url:item.url}];
    card.append(el("div", {class:"skill-center-origins"}, el("span", {}, tr("来源", "Sources")), ...origins.map(origin => link(sourceNames[origin.source] || origin.source, origin.url) || el("span", {}, sourceNames[origin.source] || origin.source))));
    if (item.metrics?.length) card.append(el("div", {class:"skill-center-metrics"}, ...item.metrics.map(metric =>
      el("span", {}, el("strong", {}, typeof metric.value === "number" ? metric.value.toLocaleString() : text(metric.value)), " ", translated(metricNames, metric.name),
        el("small", {}, sourceNames[metric.source] || metric.source || item.source)))));
    const actions = el("div", {class:"actions"}, link(tr("查看来源 ↗", "Open source ↗"), item.url));
    if (item.canImport !== false && (item.importUrl || item.repositoryUrl || item.url)) {
      actions.append(action(isRepository ? tr("选择技能并检查", "Select a skill and check") : tr("获取并检查", "Get and check"), () => fetchPack(item.importUrl || item.repositoryUrl || item.url), "primary"));
    } else actions.append(el("small", {class:"help"}, tr("尚无可直接获取的源码地址。", "No directly retrievable source address is available.")));
    card.append(item.repository ? el("small", {class:"muted"}, item.repository) : null, actions);
    if (item.updatedAt) card.insertBefore(el("small", {class:"muted"}, tr("来源更新时间：", "Source updated: ") + date(item.updatedAt)), actions);
    return card;
  }
  function displayResults(target, result) {
    target.replaceChildren(providerRows(result));
    if (!result.complete) target.append(el("p", {class:"skill-center-notice",role:"status"}, tr("部分来源未完成，当前结果可能不完整。", "Some sources did not complete. These results may be incomplete.")));
    for (const warning of result.warnings || []) target.append(el("p", {class:"help"}, text(warning)));
    if (!result.items?.length) {
      target.append(el("div", {class:"skill-center-empty"}, result.complete ? tr("没有找到匹配技能。可以换个关键词，或直接填写来源链接。", "No matching skills found. Try another query or enter a source URL.") : tr("当前没有可展示的结果，请查看各来源状态。", "No results can currently be displayed. Check the source statuses above.")));
      return;
    }
    target.append(el("p", {class:"help"}, tr("只合并已确认相同来源目录的条目；不同来源的热度指标分别保留。", "Only confirmed matching source directories are merged. Popularity metrics remain separate by source.")),
      el("div", {class:"skill-center-grid"}, ...result.items.map(resultCard)));
  }
  async function renderSearch(target, g) {
    const input = el("input", {id:"skill-center-query",type:"search",value:query,maxlength:200,placeholder:tr("搜索 PDF、新闻采集、代码审查…", "Search PDF, news gathering, code review…"),"aria-label":tr("搜索技能", "Search skills")});
    const submit = el("button", {type:"submit",class:"primary",id:"skill-center-search-submit"}, tr("搜索", "Search"));
    const error = el("p", {class:"help",role:"status",id:"skill-center-search-message",hidden:true});
    const sources = el("fieldset", {class:"skill-center-sources"}, el("legend", {}, tr("搜索来源", "Sources")));
    for (const [id, name] of Object.entries(sourceNames)) {
      const checkbox = el("input", {type:"checkbox",value:id,name:"skill-center-source",checked:sourceSelection.includes(id)});
      checkbox.onchange = () => { sourceSelection = [...sources.querySelectorAll("input:checked")].map(node => node.value); };
      sources.append(el("label", {}, checkbox, name));
    }
    const results = el("div", {id:"skill-center-results"});
    const form = el("form", {class:"skill-center-search",id:"skill-center-search"}, el("div", {class:"skill-center-search-row"}, input, submit), sources, error);
    form.onsubmit = async event => {
      event.preventDefault(); query = input.value.trim();
      if (!query) { message(error, tr("请输入搜索关键词。", "Enter a search query."), "error"); return; }
      if (!sourceSelection.length) { message(error, tr("请选择至少一个搜索来源。已获取的技能请在技能库中查看。", "Select at least one source. Find already retrieved skills in the Library."), "error"); return; }
      const sequence = ++searchSequence; requestController?.abort(); requestController = new AbortController();
      submit.disabled = true; message(error, tr("正在查询所选来源…", "Querying selected sources…"));
      try {
        const result = await api(base + "/search?" + new URLSearchParams({q:query,sources:sourceSelection.join(",")}), {signal:requestController.signal});
        if (!still(g) || sequence !== searchSequence) return;
        searchResult = result; displayResults(results, result); message(error, "");
      } catch (problem) { if (still(g) && sequence === searchSequence && problem.name !== "AbortError") message(error, errorText(problem), "error"); }
      finally { if (still(g) && sequence === searchSequence) submit.disabled = false; }
    };
    const url = el("input", {id:"skill-center-source-url",type:"url",required:true,placeholder:"https://github.com/owner/repo/tree/main/skills/example","aria-label":tr("技能来源网址", "Skill source URL")});
    const fetch = el("button", {type:"submit",class:"quiet"}, tr("获取并检查", "Get and check"));
    const direct = el("form", {class:"skill-center-direct"}, el("strong", {}, tr("已经有技能链接？", "Already have a skill URL?")), el("div", {class:"skill-center-search-row"}, url, fetch),
      el("small", {class:"help"}, tr("支持 GitHub 与 ClawHub。获取内容不会自动安装到运行环境。", "Supports GitHub and ClawHub. Retrieving content does not install it into a runtime.")));
    direct.onsubmit = async event => { event.preventDefault(); fetch.disabled = true; try { await fetchPack(url.value); } finally { fetch.disabled = false; } };
    const config = el("details", {class:"skill-center-source-config"}, el("summary", {}, tr("来源与配额说明", "Source and quota information")), el("p", {class:"help"}, tr("正在读取…", "Loading…")));
    target.replaceChildren(form, direct, localAcquisition(g), config, results);
    if (searchResult) displayResults(results, searchResult);
    else results.append(el("div", {class:"skill-center-empty"}, tr("搜索结果会保留来源；获取源码后，再查看我们的安全检查结果。", "Search results retain their origin. Retrieve the source before viewing our inspection results.")));
    try {
      const data = await api(base + "/sources");
      if (still(g)) config.replaceChildren(el("summary", {}, tr("来源与配额说明", "Source and quota information")), ...((data.items || []).map(source => el("p", {}, el("strong", {}, source.label || source.id), " · ", source.description || ""))));
    } catch (problem) { if (still(g)) config.replaceChildren(el("summary", {}, tr("来源与配额说明", "Source and quota information")), el("p", {class:"help"}, errorText(problem))); }
  }
  function localAcquisition(g) {
    let busy = false, installedBinding = null, listSequence = 0;
    const name = el("input", {id:"skill-center-local-name",type:"text",maxlength:64,pattern:"[A-Za-z0-9_-]{1,64}",placeholder:tr("可选：技能名称", "Optional skill name"),"aria-label":tr("本地技能名称", "Local skill name"),"aria-describedby":"skill-center-local-name-help"});
    const zip = el("input", {id:"skill-center-local-zip",type:"file",accept:".zip,application/zip","aria-label":tr("选择技能 ZIP", "Choose a skill ZIP")});
    const directory = el("input", {id:"skill-center-local-directory",type:"file",multiple:true,webkitdirectory:true,"aria-label":tr("选择技能文件夹", "Choose a skill folder")});
    directory.setAttribute("webkitdirectory", "");
    const status = el("p", {id:"skill-center-local-message",class:"help",role:"status",hidden:true});
    const scope = el("select", {id:"skill-center-installed-scope","aria-label":tr("已安装技能范围", "Installed skill scope")},el("option",{value:"project"},tr("当前工作区", "Current workspace")),el("option",{value:"instance"},tr("当前运行环境", "Current environment")));
    const installed = el("select", {id:"skill-center-installed-name","aria-label":tr("选择已安装技能", "Choose an installed skill")});
    const sameContext = binding => context().workspaceId === binding.workspaceId && context().instanceId === binding.instanceId;
    const controls = [];
    function sync() {
      for (const node of controls) node.disabled = busy;
      zipSubmit.disabled = busy || !zip.files?.length;
      directorySubmit.disabled = busy || !directory.files?.length;
      installed.disabled = busy || !installed.options.length;
      snapshot.disabled = busy || !installedBinding || !installed.value;
      load.disabled = busy || !context().workspaceId;
    }
    async function acquired(request, binding) {
      if (busy) return;
      busy = true; sync(); message(status,tr("正在保存完整目录并进行快速检查…", "Saving the complete directory and running the quick check…"));
      try {
        const value = await request();
        if (!still(g)) return;
        const pack = value.pack || value;
        if (!pack.id) throw Error(tr("服务未返回技能快照。", "The service did not return a skill snapshot."));
        if (!sameContext(binding)) { message(status,tr("快照已保存到技能库；当前工作区或运行环境已改变，请从“已获取技能”查看。", "The snapshot was saved to the Library. The current workspace or environment changed; open it from retrieved skills.")); return; }
        await open("pack",pack.id);
      } catch (problem) { if (still(g)) message(status,errorText(problem),"error"); }
      finally { busy = false; if (still(g)) sync(); }
    }
    async function upload(kind) {
      if (busy || !name.reportValidity()) return;
      const files = [...(kind === "zip" ? zip.files || [] : directory.files || [])];
      if (!files.length) { message(status,tr("请先选择文件。", "Choose files first."),"error"); return; }
      const byteLimit = (kind === "zip" ? 34 : 32)*1024*1024;
      if (files.length > 1000 || files.reduce((total,file) => total + file.size,0) > byteLimit) {
        message(status,kind === "zip" ? tr("ZIP 上传最多 34 MiB；解压后最多 1000 个文件、32 MiB，由服务端校验。", "ZIP uploads are limited to 34 MiB. The server checks expanded content against 1,000 files and 32 MiB.") : tr("文件夹一次最多 1000 个文件、总计 32 MiB。", "A folder can contain up to 1,000 files and 32 MiB."),"error"); return;
      }
      let body, headers;
      if (kind === "zip") { if (files.length !== 1) return; body = files[0]; headers = {"Content-Type":"application/zip"}; }
      else {
        if (files.some(file => !file.webkitRelativePath)) { message(status,tr("请选择完整的技能文件夹，以保留文件相对路径。", "Choose a complete skill folder so relative file paths are preserved."),"error"); return; }
        body = new FormData();
        for (const file of files) body.append(file.webkitRelativePath,file,file.name);
      }
      const binding = context(), selectedName = name.value.trim();
      const path = base + "/import" + (selectedName ? "?" + new URLSearchParams({name:selectedName}) : "");
      return acquired(() => api(path,{method:"POST",body,headers}),binding);
    }
    const zipSubmit = button(tr("获取 ZIP 并检查", "Get ZIP and check"), () => upload("zip"),"quiet"); zipSubmit.id = "skill-center-local-zip-submit";
    const directorySubmit = button(tr("获取文件夹并检查", "Get folder and check"), () => upload("directory"),"quiet"); directorySubmit.id = "skill-center-local-directory-submit";
    const load = button(tr("读取已安装技能", "Load installed skills"), async () => {
      if (busy) return;
      const binding = {...context(),scope:scope.value}, sequence = ++listSequence;
      if (!binding.workspaceId) { message(status,tr("请先选择工作区。", "Choose a workspace first."),"error"); return; }
      busy = true; installedBinding = null; installed.replaceChildren(); sync(); message(status,tr("正在读取当前范围的技能目录…", "Reading skill directories in the selected scope…"));
      try {
        const value = await api(base + "/installed?" + new URLSearchParams({workspaceId:binding.workspaceId,instanceId:binding.instanceId,scope:binding.scope}));
        if (!still(g) || sequence !== listSequence) return;
        if (!sameContext(binding) || scope.value !== binding.scope) { message(status,tr("工作区、环境或范围已改变，请重新读取。", "The workspace, environment or scope changed. Load the list again."),"error"); return; }
        if (value.workspaceId !== binding.workspaceId || value.instanceId !== binding.instanceId || value.scope !== binding.scope) throw Error(tr("服务返回的技能范围与所选范围不一致，请重试。", "The returned skill scope does not match the selection. Retry."));
        const items = value.items || [];
        installed.replaceChildren(...items.map(item => el("option",{value:item.name},item.name)));
        installedBinding = binding;
        const warnings = (value.warnings || []).map(text).join(" ");
        message(status,(items.length ? tr("选择技能后创建独立快照，已安装内容保持原样。", "Choose a skill to create an independent snapshot. The installed content is preserved.") : tr("当前范围没有可获取的完整技能目录。", "There are no complete skill directories in this scope.")) + (value.truncated ? tr(" 列表已截断。", " The list was truncated.") : "") + (warnings ? " "+warnings : ""));
      } catch (problem) { if (still(g) && sequence === listSequence) message(status,errorText(problem),"error"); }
      finally { busy = false; if (still(g)) sync(); }
    },"quiet"); load.id = "skill-center-installed-load";
    const snapshot = button(tr("创建快照并检查", "Create snapshot and check"), async () => {
      if (busy || !installedBinding || !installed.value) return;
      const binding = {...installedBinding}, selectedName = installed.value;
      if (!sameContext(binding) || scope.value !== binding.scope) { installedBinding = null; sync(); message(status,tr("工作区、环境或范围已改变，请重新读取已安装技能。", "The workspace, environment or scope changed. Reload installed skills."),"error"); return; }
      const body = {workspaceId:binding.workspaceId,instanceId:binding.instanceId,scope:binding.scope,name:selectedName};
      return acquired(() => api(base + "/from-installed",{method:"POST",body}),binding);
    },"quiet"); snapshot.id = "skill-center-installed-snapshot";
    controls.push(name,zip,directory,scope,load,installed,snapshot,zipSubmit,directorySubmit);
    zip.onchange = directory.onchange = installed.onchange = sync;
    scope.onchange = () => { ++listSequence; installedBinding = null; installed.replaceChildren(); sync(); };
    sync();
    return el("details", {id:"skill-center-local",class:"skill-center-local"},el("summary",{},tr("从本地获取", "Retrieve local skills")),
      el("p",{class:"help"},tr("先获取完整目录、生成固定快照并检查，不会自动安装。目录中需包含 SKILL.md；二进制或未覆盖内容会明确标记。需要保留原始可执行权限时，请使用带权限信息的 ZIP。", "Retrieve the full directory as a fixed snapshot and inspect it before installation. SKILL.md is required; binary or uninspected content remains clearly marked. Use a ZIP carrying permissions when preserving original executable flags matters.")),
      name,el("small",{id:"skill-center-local-name-help",class:"help"},tr("名称可留空自动读取；手动填写时使用字母、数字、下划线或连字符，最多 64 字符。", "Leave the name empty to derive it from the skill. An explicit name can use letters, digits, underscores or hyphens, up to 64 characters.")),
      el("div",{class:"skill-center-local-upload"},el("label",{},tr("技能 ZIP", "Skill ZIP"),zip),zipSubmit),
      el("div",{class:"skill-center-local-upload"},el("label",{},tr("技能文件夹", "Skill folder"),directory),directorySubmit),
      el("div",{class:"skill-center-installed"},el("strong",{},tr("从已安装技能创建快照", "Create a snapshot from an installed skill")),
        el("p",{class:"help"},tr("使用当前工作区和运行环境读取完整目录及附件。", "Read the complete directory and attachments from the current workspace and environment.")),
        el("div",{class:"skill-center-local-row"},scope,load),el("div",{class:"skill-center-local-row"},installed,snapshot)),status);
  }
  function mountDiagnostics(target, g) {
    let busy = false, history = [], revision = 0;
    diagnosticsController = new AbortController();
    const signal = diagnosticsController.signal;
    const overallNames = {ready:["静态工具协议已验证", "Static tool protocol verified"],unavailable:["工具或环境不可用", "Tool or environment unavailable"],incompatible:["版本或协议不兼容", "Incompatible version or protocol"],failed:["诊断失败", "Diagnostic failed"],incomplete:["诊断未完整覆盖", "Incomplete diagnostics"]};
    const checkNames = {ok:["已验证", "Verified"],missing:["未安装或缺失", "Missing"],incompatible:["不兼容", "Incompatible"],failed:["执行失败", "Failed"],unknown:["未检查", "Not checked"],skipped:["未启用", "Not enabled"]};
    const type = el("select", {id:"skill-center-diagnostic-target","aria-label":tr("诊断工具", "Diagnostic tool")},el("option",{value:"skillspector"},"NVIDIA SkillSpector"),el("option",{value:"skillup"},"Skill-Up"));
    const environments = (state.instances || []).filter(item => item.managed && item.execution?.mode === "docker");
    const environment = el("select", {id:"skill-center-diagnostic-environment","aria-label":tr("诊断测试环境", "Diagnostic test environment")}, ...environments.map(item => el("option",{value:item.id},item.name || item.id)));
    if (environments.some(item => item.id === context().instanceId)) environment.value = context().instanceId;
    const environmentGroup = el("label", {class:"skill-center-diagnostic-environment",hidden:true},tr("测试环境", "Test environment"),environment);
    const status = el("p", {id:"skill-center-diagnostic-message",class:"help",role:"status",hidden:true});
    const results = el("div", {id:"skill-center-diagnostic-results"});
    const run = button(tr("运行受控诊断", "Run controlled diagnostics"), async () => {
      if (busy || type.value === "skillup" && !environment.value) return;
      const selection = {target:type.value,...(type.value === "skillup" ? {instanceId:environment.value} : {})};
      const sequence = ++revision;
      busy = true; sync(); message(status,tr("正在核对工具版本和受控样本协议…", "Checking the tool version and a controlled sample protocol…"));
      try {
        const value = await api(base + "/diagnostics",{method:"POST",body:selection,signal});
        if (!still(g) || sequence !== revision) return;
        if (value.target !== selection.target || selection.instanceId && value.instanceId !== selection.instanceId) throw Error(tr("诊断返回的工具或环境与选择不一致。", "The diagnostic result does not match the selected tool or environment."));
        history = [value,...history.filter(item => item.id !== value.id)];
        draw(); message(status,tr("诊断已记录。模型认证与网络未检查项仍需单独确认。", "The diagnostic was recorded. Unchecked model authentication and network access still require separate confirmation."));
      } catch (problem) { if (still(g) && problem.name !== "AbortError") message(status,errorText(problem),"error"); }
      finally { busy = false; if (still(g)) sync(); }
    },"quiet"); run.id = "skill-center-diagnostic-run";
    function sync() {
      type.disabled = environment.disabled = busy;
      environmentGroup.hidden = type.value !== "skillup";
      run.disabled = busy || type.value === "skillup" && !environment.value;
    }
    function draw() {
      const matches = history.filter(item => item.target === type.value && (type.value !== "skillup" || !environment.value || item.instanceId === environment.value)).sort((a,b) => String(b.checkedAt || "").localeCompare(String(a.checkedAt || "")));
      const result = matches[0];
      if (!result) {
        results.replaceChildren(el("p", {class:"help"},type.value === "skillup" && !environment.value ? tr("尚无已配置的受管 Docker 测试环境。准备后可运行工具诊断；模型和认证不会自动检查。", "There is no configured managed Docker test environment. Prepare one before diagnosing its tools; model access and authentication are not checked automatically.") : tr("尚未诊断。点击按钮检查固定样本，不会执行用户 Skill 或调用模型。", "No diagnostic has run. Use the button to check a controlled sample without executing a user skill or calling a model.")));
        return;
      }
      const engine = result.target === "skillup" ? "Skill-Up" : "NVIDIA SkillSpector";
      results.replaceChildren(el("div", {class:"skill-center-diagnostic-heading"},el("strong",{},engine),badge(result.overall,translated(overallNames,result.overall))),
        meta([[tr("最近检查", "Last checked"),date(result.checkedAt)],...(result.instanceId ? [[tr("测试环境", "Test environment"),environments.find(item => item.id === result.instanceId)?.name || result.instanceId]] : [])]),
        el("p",{class:"help"},tr("这是工具和协议的诊断记录，不是 Skill 安全证明，也不代表模型、认证和网络已经就绪。", "This is a tool and protocol diagnostic, not proof of skill safety or model, authentication and network readiness.")),
        el("div",{class:"skill-center-diagnostic-checks"},...(result.checks || []).map(check => el("article",{class:"skill-center-diagnostic-check","data-diagnostic-status":check.status},
          el("div",{},el("strong",{},check.label || check.id),badge(check.status,translated(checkNames,check.status))),
          check.version ? el("small",{},tr("版本：", "Version: ")+check.version) : null,
          el("p",{},check.message || ""),typeof check.durationMs === "number" ? el("small",{},`${check.durationMs} ms`) : null))));
      if (result.cleanupJobId) {
        const cleanup = button(tr("重试清理", "Retry cleanup"),async () => {
          if (busy) return;
          busy = true; sync(); cleanup.disabled = true;
          try {
            const job = await api(base + "/check-jobs/" + enc(result.cleanupJobId) + "/cancel",{method:"POST",body:{},signal});
            if (!still(g)) return;
            message(status,job.cleanupError || (job.cleanupPending ? tr("清理仍未确认，请稍后重试。", "Cleanup is not yet confirmed. Retry shortly.") : tr("容器清理已确认。原诊断记录保留；需要时可重新运行诊断。", "Container cleanup was confirmed. The original diagnostic record remains; run another diagnostic when needed.")),job.cleanupError || job.cleanupPending ? "error" : "help");
          } catch (problem) { if (still(g) && problem.name !== "AbortError") message(status,errorText(problem),"error"); }
          finally { busy = false; if (still(g)) { sync(); cleanup.disabled = false; } }
        },"quiet");
        results.append(el("p",{class:"skill-center-notice"},tr("此诊断有待确认的容器清理，新的测试可能被阻断。", "Container cleanup for this diagnostic has not been confirmed. New tests may remain blocked.")),cleanup);
      }
    }
    type.onchange = environment.onchange = () => { sync(); draw(); };
    sync(); draw();
    target.append(el("details",{id:"skill-center-diagnostics",class:"skill-center-diagnostics"},el("summary",{},tr("检查工具与环境", "Inspect tools and environments")),
      el("p",{class:"help"},tr("Go 快速检查随 RunDesk 提供，无需额外安装。外部工具只有经过显式诊断后才会记录已验证状态。", "The Go quick check is included with RunDesk. External tools are only recorded as verified after an explicit diagnostic.")),
      el("div",{class:"skill-center-local-row"},type,environmentGroup,run),status,results,
      el("details",{class:"skill-center-diagnostic-help"},el("summary",{},tr("配置说明", "Configuration help")),
        el("p",{class:"help"},tr("SkillSpector 使用运行 RunDesk 的服务主机上的 RUNDESK_SKILLSPECTOR_BIN；Skill-Up 使用所选 Docker 镜像内的 /usr/local/bin/skill-up，或服务配置的 RUNDESK_SKILLUP_CONTAINER_BIN。修改服务环境变量后需重启 RunDesk。诊断不会安装工具或登录模型。", "SkillSpector uses RUNDESK_SKILLSPECTOR_BIN on the RunDesk service host. Skill-Up uses /usr/local/bin/skill-up in the selected Docker image, or the service's RUNDESK_SKILLUP_CONTAINER_BIN. Restart RunDesk after changing service environment variables. Diagnostics do not install tools or sign in to model providers.")))));
    const sequence = revision;
    api(base + "/diagnostics",{signal}).then(value => {
      if (!still(g) || sequence !== revision) return;
      history = value.items || []; draw();
    }).catch(problem => { if (still(g) && sequence === revision && problem.name !== "AbortError") message(status,errorText(problem),"error"); });
  }
  async function fetchPack(url) {
    const g = generation, content = page().querySelector(".skill-center-content");
    for (const previous of content.querySelectorAll(".skill-center-candidates")) previous.remove();
    let progress = $("#skill-center-fetch-progress");
    if (!progress) { progress = el("p", {id:"skill-center-fetch-progress",class:"skill-center-notice",role:"status"}); content.prepend(progress); }
    message(progress, tr("正在获取固定版本源码并检查，请稍候…", "Retrieving a fixed source revision and checking it…"), "skill-center-notice");
    try {
      const value = await api(base + "/fetch", {method:"POST",body:{url}});
      if (!still(g)) return;
      const pack = value.pack || value;
      if (!pack.id) throw Error(tr("服务未返回技能记录。", "The service did not return a skill record."));
      await open("pack", pack.id);
    } catch (error) {
      if (!still(g)) return;
      const details = error.details || {};
      const candidates = error.code === "multiple_skills" ? details.candidates : error.code === "source_handoff" && (details.githubUrl || details.github_url) ? [{name:tr("固定版本 GitHub 源码", "Pinned GitHub source"),url:details.githubUrl || details.github_url}] : [];
      message(progress, errorText(error), "error");
      if (Array.isArray(candidates) && candidates.length) {
        const list = el("div", {class:"skill-center-candidates"}, el("strong", {}, tr("选择一个具体技能继续获取：", "Select a specific skill to retrieve:")));
        for (const candidate of candidates) {
          const value = typeof candidate === "string" ? {name:candidate,url:candidate} : candidate;
          if (value?.url) list.append(action(value.name || value.url, () => fetchPack(value.url), "quiet"));
        }
        progress.after(list);
      }
    }
  }
  async function renderLibrary(target, g) {
    target.append(el("p", {class:"help"}, tr("正在读取已获取技能…", "Loading retrieved skills…")));
    const value = await api(base + "/packs");
    if (!still(g)) return;
    const filter = el("input", {type:"search",placeholder:tr("筛选名称、描述或来源…", "Filter name, description, or source…"),"aria-label":tr("筛选已获取技能", "Filter retrieved skills")});
    const list = el("div", {class:"skill-center-grid",id:"skill-center-packs"});
    const draw = () => {
      const q = filter.value.trim().toLocaleLowerCase();
      const packs = (value.items || []).filter(pack => [pack.name,pack.description,pack.sourceUrl,pack.revision].join(" ").toLocaleLowerCase().includes(q));
      list.replaceChildren(...packs.map(pack => el("article", {class:"skill-center-card"},
        el("div", {class:"skill-center-card-title"}, el("h3", {}, pack.name || pack.id), reportBadge(pack.scan)),
        el("p", {class:"skill-center-description"}, pack.description || pack.sourceUrl || ""),
        meta([[tr("版本", "Revision"),pack.revision],[tr("获取时间", "Retrieved"),date(pack.createdAt)]]),
        action(tr("查看源码与检查", "Source and checks"), () => open("pack", pack.id), "primary"))));
      if (!packs.length) list.append(el("div", {class:"skill-center-empty"}, q ? tr("没有匹配的技能。", "No matching skills.") : tr("还没有获取技能。从搜索页选择一个来源开始。", "No retrieved skills yet. Start with a source on Discover.")));
    };
    filter.oninput = draw;
    target.replaceChildren(el("div", {class:"skill-center-library-toolbar"}, filter, el("small", {class:"help"}, tr("这里只保存已经获取的内容；不代表已经安装或启用。", "Retrieved content is kept here. It is not necessarily installed or enabled."))), list); draw();
  }
  function reportBadge(report) {
    if (!report) return badge("unscanned", tr("未检查", "Not checked"));
    if (report.status !== "completed") return badge(report.status || "incomplete", report.status === "unavailable" ? tr("引擎未配置", "Engine unavailable") : tr("检查未完成", "Inspection incomplete"));
    return badge(report.recommendation || "review", translated(recommendationNames, report.recommendation || "review"));
  }
  function renderReport(target, report) {
    target.replaceChildren(el("div", {class:"skill-center-report-heading"}, el("h3", {}, tr("检查报告", "Inspection report")), reportBadge(report)));
    if (!report) { target.append(el("p", {class:"help"}, tr("尚未运行检查。", "No inspection has run."))); return; }
    const coverage = report.coverage || {};
    target.append(meta([[tr("检查引擎", "Engine"),translated(reportEngines, report.engine)],[tr("引擎版本", "Engine version"),report.engineVersion],
      [tr("内容摘要", "Content digest"),report.digest],
      [tr("检查文件", "Files inspected"),typeof coverage.filesScanned === "number" ? `${coverage.filesScanned} / ${coverage.filesSeen ?? "?"}` : "—"],
      [tr("未检查文件", "Files not inspected"),coverage.filesSkipped ?? "—"]]),
      el("p", {class:"help"}, tr("未发现规则命中不代表安全。报告只适用于这里记录的内容与检查范围。", "A report with no rule findings does not guarantee safety. It applies only to the recorded content and inspection scope.")));
    for (const warning of report.warnings || []) target.append(el("p", {class:"skill-center-notice"}, text(warning)));
    if (coverage.skipped?.length) target.append(el("details", {}, el("summary", {}, tr("查看未检查项", "View inspection gaps")), el("ul", {}, ...coverage.skipped.map(item => el("li", {}, text(item.file), " · ", text(item.reason))))));
    const filter = el("select", {"aria-label":tr("按严重程度筛选", "Filter severity")}, el("option", {value:""}, tr("所有严重程度", "All severities")), ...Object.entries(severityNames).map(([key, label]) => el("option", {value:key}, tr(...label))));
    const list = el("div", {class:"skill-center-findings"});
    const draw = () => {
      const findings = (report.findings || []).filter(finding => !filter.value || String(finding.severity).toLowerCase() === filter.value);
      list.replaceChildren(...findings.map(finding => {
        const severity = String(finding.severity || "info").toLowerCase();
        return el("article", {class:"skill-center-finding","data-severity":severity},
          el("div", {class:"skill-center-finding-heading"}, badge(severity, translated(severityNames, severity)), el("strong", {}, finding.ruleId || finding.category || "")),
          el("code", {}, `${finding.file || "—"}${finding.line ? ":" + finding.line : ""}`),
          finding.evidence ? el("pre", {}, finding.evidence) : null,
          finding.reason ? el("p", {}, finding.reason) : null,
          finding.recommendation ? el("p", {class:"help"}, finding.recommendation) : null);
      }));
      if (!findings.length) list.append(el("p", {class:"help"}, filter.value ? tr("此严重程度没有发现项。", "No findings at this severity.") : tr("当前报告没有发现项，请同时核对检查完整度。", "This report has no findings. Also check inspection completeness.")));
    };
    filter.onchange = draw;
    const download = action(tr("下载 JSON", "Download JSON"), () => {
      const url = URL.createObjectURL(new Blob([JSON.stringify(report, null, 2)], {type:"application/json"}));
      const a = el("a", {href:url,download:"skill-inspection.json"}); document.body.append(a); a.click(); a.remove(); setTimeout(() => URL.revokeObjectURL(url), 1000);
    });
    target.append(el("div", {class:"skill-center-report-toolbar"}, filter, download), list); draw();
  }
  function renderReviewReports(target, details, fallback) {
    const reports = [], seen = new Set();
    const add = (value, engine) => {
      if (!value) return;
      const report = {...value,engine:value.engine || engine || "unknown"};
      const key = report.engine + "\u0000" + (report.digest || details?.digest || "");
      if (seen.has(key)) return;
      seen.add(key); reports.push(report);
    };
    add(details?.report || details?.scan || fallback, "builtin");
    for (const [engine, report] of Object.entries(details?.reports || {})) add(report, engine);
    target.replaceChildren(...reports.map(report => {
      const section = el("section", {class:"skill-center-report","data-review-engine":report.engine});
      renderReport(section, report); return section;
    }));
  }
  async function loadEngines() {
    if (!enginesPromise) enginesPromise = api(base + "/engines").then(value => value.items || []).finally(() => { enginesPromise = null; });
    return enginesPromise;
  }
  function scanInstallState(pack, g, active) {
    if (!still(g) || selected !== pack.id) return;
    const installButton = $("#skill-center-install"), note = $("#skill-center-install-status");
    if (installButton) installButton.disabled = active;
    if (note) note.hidden = !active;
  }
  function packReportView(pack) {
    const report = el("section", {class:"skill-center-report",id:"skill-center-report"});
    const selector = el("div", {class:"actions skill-center-report-selector",role:"group","aria-label":tr("检查引擎报告", "Inspection engine reports")});
    let engine = pack.reports?.skillspector ? "skillspector" : "builtin", pendingEngine = "";
    const reports = () => ({builtin:pack.scan,...pack.reports});
    function draw(preferred) {
      const values = reports();
      if (preferred && values[preferred]) engine = preferred;
      if (!values[engine]) engine = values.skillspector ? "skillspector" : "builtin";
      selector.replaceChildren(...Object.entries(reportEngines).filter(([key]) => values[key]).map(([key, label]) => {
        const node = button(tr(...label), () => draw(key), key === engine ? "selected" : "quiet");
        node.setAttribute("aria-pressed", String(key === engine)); node.setAttribute("data-report-engine", key);
        return node;
      }));
      renderReport(report, values[engine]);
    }
    function pending(key, force = false) {
      if (!reportEngines[key]) return;
      if (!force && pendingEngine === key) return;
      pendingEngine = key;
      pack.reports = {...pack.reports,[key]:{engine:key,digest:pack.digest,status:"incomplete",recommendation:"incomplete",findings:[],coverage:{filesSeen:pack.files?.length || 0,filesScanned:0},warnings:[tr("本次扫描尚未完成，不能沿用先前结论。", "This scan has not completed. Its previous verdict is not applicable.")]}};
      draw(key);
    }
    draw();
    return {report,selector,pending,refresh:preferred => { pendingEngine = ""; draw(preferred); }};
  }
  async function refreshPackReports(pack, view, g, engine) {
    const latest = await api(base + "/packs/" + enc(pack.id));
    if (!still(g)) return null;
    // A different snapshot must never inherit a report or enable installation
    // through a response returned for an earlier page state.
    if (latest.id !== pack.id || latest.digest !== pack.digest) throw Error(tr("技能快照已改变，请刷新详情页。", "The skill snapshot changed. Refresh its details."));
    pack.scan = latest.scan; pack.reports = latest.reports; pack.activeJob = latest.activeJob;
    view.refresh(engine);
    return latest;
  }
  async function renderPack(target, id, g) {
    target.append(el("p", {class:"help"}, tr("正在读取技能…", "Loading skill…")));
    const pack = await api(base + "/packs/" + enc(id));
    if (!still(g)) return;
    const view = packReportView(pack);
    if (pack.activeJob?.id && activeJob(pack.activeJob.status)) { packJobs.set(pack.id, pack.activeJob.id); view.pending(pack.activeJob.engine); }
    const job = el("div", {class:"skill-center-job",role:"status",id:"skill-center-job",hidden:true});
    const builtin = action(tr("重新快速检查", "Run quick check"), () => startScan(pack, "builtin", job, view, g));
    const external = action(tr("SkillSpector 静态检查", "SkillSpector static scan"), () => startScan(pack, "skillspector", job, view, g));
    external.disabled = true;
    const engineNote = el("p", {class:"help",id:"skill-center-engine-note"}, tr("正在检查扫描引擎配置…", "Checking inspection engine configuration…"));
    const files = el("nav", {class:"skill-center-files","aria-label":tr("技能文件", "Skill files")});
    const preview = el("div", {class:"skill-center-preview"}, el("p", {class:"help"}, tr("选择文件查看源码预览。", "Select a file to preview its source.")));
    let fileSequence = 0;
    for (const file of pack.files || []) {
      const node = action(file.path, async () => {
        const sequence = ++fileSequence;
        try {
          const value = await api(base + "/packs/" + enc(id) + "/file?path=" + enc(file.path));
          if (!still(g) || sequence !== fileSequence) return;
          for (const other of files.children) other.classList.toggle("selected", other === node);
          preview.replaceChildren(el("h3", {}, value.path || file.path), value.binary ? el("p", {class:"help"}, tr("二进制内容不在页面中解释执行。", "Binary content is not interpreted or executed in this view.")) : el("pre", {tabindex:0}, value.text || ""), value.truncated ? el("p", {class:"skill-center-notice"}, tr("预览已截断；检查与内容摘要使用保存的完整文件。", "Preview is truncated. Inspection and the digest use the complete stored file.")) : null);
        } catch (error) { if (still(g) && sequence === fileSequence) preview.replaceChildren(el("p", {class:"error"}, errorText(error))); }
      }, "skill-center-file");
      node.append(el("small", {}, prettySize(file.size || 0) + (file.executable ? tr(" · 可执行", " · executable") : ""))); files.append(node);
    }
    target.replaceChildren(el("section", {class:"skill-center-pack"},
      el("div", {class:"skill-center-pack-heading"}, el("div", {}, el("h2", {}, pack.name || pack.id), el("p", {class:"help"}, pack.description || "")),
        el("div", {class:"actions"}, link(tr("来源页面 ↗", "Source page ↗"), pack.sourceUrl), action(tr("安装到工作区", "Install into workspace"), () => install(pack), "primary", {id:"skill-center-install"}))),
      el("p", {class:"skill-center-notice",id:"skill-center-install-status",role:"status",hidden:true}, tr("该技能正在检查，任务结束后才能安装。", "This skill is being checked. Installation is available after the job finishes.")),
      meta([[tr("获取来源", "Acquisition source"),acquisitionSource(pack)],[tr("固定版本", "Pinned revision"),pack.revision],[tr("内容摘要", "Content digest"),pack.digest],[tr("获取时间", "Retrieved"),date(pack.createdAt)]]),
      el("div", {class:"skill-center-source-preview"}, files, preview)),
      el("section", {class:"skill-center-checks"}, el("div", {class:"actions"}, builtin, external), engineNote, job, view.selector, view.report));
    window.RunDeskSkillChecks?.mount(target, pack, {isCurrent: () => still(g) && selected === pack.id});
    scanInstallState(pack, g, packJobs.has(pack.id));
    try {
      const engines = await loadEngines();
      if (still(g)) {
        const available = engines.find(engine => engine.id === "skillspector");
        external.disabled = !available?.available;
        engineNote.textContent = available?.message || (available?.available ? tr("SkillSpector 已就绪。此处仅运行静态检查，不启用 LLM。", "SkillSpector is ready. These checks are static and do not enable an LLM.") : tr("SkillSpector 未配置，快速检查仍可使用。", "SkillSpector is not configured. Quick checks remain available."));
      }
    } catch (error) { if (still(g)) { external.disabled = true; engineNote.textContent = errorText(error); } }
    if (packJobs.has(pack.id)) await pollJob(pack, packJobs.get(pack.id), job, view, g, 0);
  }
  async function startScan(pack, engine, jobTarget, view, g) {
    scanInstallState(pack, g, true);
    view.pending(engine, true);
    try {
      const value = await api(base + "/packs/" + enc(pack.id) + "/scan", {method:"POST",body:{engine,allowLLM:false}});
      if (!still(g)) return;
      const job = value.job || value;
      if (!job.id) throw Error(tr("服务未返回检查任务。", "The service did not return an inspection job."));
      packJobs.set(pack.id, job.id); await pollJob(pack, job.id, jobTarget, view, g, 0);
    } catch (error) { if (still(g)) { message(jobTarget, errorText(error), "error"); scanInstallState(pack, g, error.code === "skill_scan_running" || packJobs.has(pack.id)); } }
  }
  async function pollJob(pack, id, target, view, g, attempt) {
    clearTimeout(pollTimer);
    if (!still(g)) return;
    try {
      const job = await api(base + "/jobs/" + enc(id));
      if (!still(g) || packJobs.get(pack.id) !== id) return;
      target.hidden = false; target.className = "skill-center-job";
      target.replaceChildren(badge(job.status, translated(jobStates, job.status)), el("span", {}, job.error || job.message || ""));
      if (activeJob(job.status)) {
        scanInstallState(pack, g, true); view.pending(job.engine || pack.activeJob?.engine || "builtin");
        target.append(action(tr("取消检查", "Cancel check"), async () => { await api(base + "/jobs/" + enc(id) + "/cancel", {method:"POST",body:{}}); await pollJob(pack, id, target, view, g, 0); }));
        if (attempt < 300) pollTimer = setTimeout(() => pollJob(pack, id, target, view, g, attempt + 1), 1000);
        else target.append(action(tr("刷新任务状态", "Refresh job status"), () => pollJob(pack, id, target, view, g, 0)));
      } else {
        // Read persisted verdicts after every terminal outcome. In particular,
        // cancellation cannot reveal a cached successful external report.
        scanInstallState(pack, g, true);
        const latest = await refreshPackReports(pack, view, g, job.engine);
        if (!latest || packJobs.get(pack.id) !== id) return;
        if (latest.activeJob?.id && activeJob(latest.activeJob.status)) {
          packJobs.set(pack.id, latest.activeJob.id);
          await pollJob(pack, latest.activeJob.id, target, view, g, 0);
        } else { packJobs.delete(pack.id); scanInstallState(pack, g, false); }
      }
    } catch (error) {
      if (still(g) && packJobs.get(pack.id) === id) target.replaceChildren(el("p", {class:"error"}, errorText(error)), action(tr("重试读取任务", "Retry reading job"), () => pollJob(pack, id, target, view, g, 0)));
    }
  }
  function install(pack) {
    const binding = context();
    let busy = false, installed = false;
    const modal = el("dialog", {class:"application-register skill-center-install-dialog"}), error = el("p", {class:"error",role:"status"});
    const name = el("input", {id:"skill-center-install-name",required:true,pattern:"[A-Za-z0-9_-]{1,64}",maxlength:64,value:String(pack.name || "skill").replace(/[^A-Za-z0-9_-]+/g, "-").replace(/^-+|-+$/g, "").slice(0,64) || "skill"});
    const scope = el("select", {id:"skill-center-install-scope"}, el("option", {value:"project"}, tr("仅当前项目", "Current project only")), el("option", {value:"instance"}, tr("当前运行环境", "Current runtime environment")));
    const replace = el("input", {type:"checkbox",id:"skill-center-install-replace"});
    const submit = el("button", {type:"submit",class:"primary",id:"skill-center-install-submit",disabled:!binding.workspaceId}, tr("安装此版本", "Install this revision"));
    const review = el("section", {class:"skill-center-install-review",hidden:true}), report = el("div", {class:"skill-center-review-reports"});
    const acknowledge = el("input", {type:"checkbox",id:"skill-center-install-ack"});
    const reason = el("textarea", {id:"skill-center-install-reason",rows:2,maxlength:2000,placeholder:tr("说明已复核的用途、授权或接受风险的理由", "Describe the reviewed purpose, authorization, or reason for accepting this risk")});
    const override = el("button", {type:"button",class:"primary",id:"skill-center-install-override",disabled:true}, tr("记录理由并继续安装", "Record reason and continue installing"));
    const checkOverride = () => { override.disabled = busy || installed || !acknowledge.checked || !reason.value.trim(); };
    acknowledge.onchange = checkOverride; reason.oninput = checkOverride;
    review.append(el("p", {class:"skill-center-notice"}, tr("此版本需要复核。请阅读报告后再决定是否安装。", "This revision needs review. Read the report before deciding whether to install it.")), report,
      el("label", {class:"check-inline"}, acknowledge, tr("我已查看检查结果，并确认此次安装。", "I reviewed the inspection results and confirm this installation.")),
      el("label", {}, tr("复核理由", "Review reason"), reason), override);
    const form = el("form", {}, el("div", {class:"dialog-head"}, el("h2", {}, tr("安装技能", "Install skill")), button(tr("关闭", "Close"), () => modal.close(), "quiet")),
      el("p", {class:"help"}, tr("安装目标：", "Install target: ") + (binding.workspaceName || tr("尚无工作区", "No workspace")) + " · " + binding.environmentName),
      el("p", {class:"skill-center-digest"}, tr("内容摘要：", "Content digest: ") + text(pack.digest)),
      el("label", {}, tr("技能目录名称", "Skill directory name"), name), el("label", {}, tr("安装范围", "Installation scope"), scope),
      el("label", {class:"check-inline"}, replace, tr("替换同名技能（原目录备份）", "Replace a skill with this name (back up the existing directory)")),
      !binding.workspaceId ? el("p", {class:"help"}, tr("先从会话页选择或创建工作区，再返回安装。", "Choose or create a workspace in Conversations before installing.")) : null,
      error, submit, review);
    async function apply(accepted) {
      if (busy || installed || !accepted && !review.hidden) return;
      if (ws()?.id !== binding.workspaceId || instance()?.id !== binding.instanceId) {
        message(error, tr("当前工作区或运行环境已经改变，请关闭后重新安装。", "The workspace or environment changed. Close this dialog and start again."), "error"); return;
      }
      if (!form.reportValidity() || accepted && (!acknowledge.checked || !reason.value.trim())) return;
      busy = true; submit.disabled = true; override.disabled = true; message(error, "");
      try {
        const value = await api(base + "/packs/" + enc(pack.id) + "/install", {method:"POST",body:{workspaceId:binding.workspaceId,instanceId:binding.instanceId,scope:scope.value,name:name.value,replace:replace.checked,digest:pack.digest,acknowledge:accepted,reason:accepted ? reason.value.trim() : ""}});
        if (!modal.open) return;
        installed = true; review.hidden = true; submit.hidden = true;
        name.disabled = true; scope.disabled = true; replace.disabled = true;
        const receipt = value.receipt || value.installation || {};
        form.append(el("p", {class:"skill-center-success",role:"status"}, tr("此版本已安装到所选范围。", "This revision was installed into the selected scope.")),
          meta([[tr("工作区", "Workspace"),binding.workspaceName],[tr("内容摘要", "Content digest"),receipt.digest || pack.digest]]));
      } catch (problem) {
        if (!modal.open) return;
        message(error, errorText(problem), "error");
        if (problem.code === "skill_review_required") {
          renderReviewReports(report, problem.details, pack.scan);
          review.hidden = false; submit.hidden = true; acknowledge.checked = false; checkOverride();
          name.disabled = true; scope.disabled = true; replace.disabled = true;
        }
      } finally { busy = false; if (modal.open) { submit.disabled = installed || !binding.workspaceId; checkOverride(); } }
    }
    form.onsubmit = event => { event.preventDefault(); return apply(false); };
    override.onclick = () => safe(() => apply(true));
    modal.append(form); document.body.append(modal); modal.addEventListener("close", () => modal.remove(), {once:true}); modal.showModal();
  }
  $("#nav-skills")?.addEventListener("click", () => safe(() => open("search")));
  if (document.documentElement.lang === "en") {
    const nav = $("#nav-skills");
    if (nav) { nav.title = "Skills"; nav.setAttribute("aria-label", "Skills"); const label = nav.querySelector(".rail-label"); if (label) label.textContent = "Skills"; }
  }
  return {open,close,renderReport,label:() => tr("技能中心", "Skill center")};
})();
