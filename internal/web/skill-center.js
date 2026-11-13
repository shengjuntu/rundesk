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
  let generation = 0, searchSequence = 0, currentTab = "search", selected = "", back;
  let query = "", sourceSelection = ["clawhub", "skillsmp", "skills.sh"], searchResult = null;
  let enginesPromise, pollTimer, requestController;
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
  function message(target, value, type = "help") {
    target.className = type; target.textContent = value; target.hidden = !value;
  }
  function meta(rows) {
    return el("dl", {class:"skill-center-metadata"}, rows.flatMap(([name, value]) => [el("dt", {}, name), el("dd", {}, text(value))]));
  }
  function activeJob(status) { return ["queued", "running"].includes(status); }
  function close() {
    ++generation; clearTimeout(pollTimer); requestController?.abort();
    const target = back || {view:"conversation",route:"#conversations"};
    setProductPage(target.view); history.replaceState(null, "", target.route); return restoreProductRoute();
  }
  async function open(tab = "search", id = "") {
    if (state.view !== "skill-center") {
      stashProductDraft();
      back = {view:state.view,route:location.hash.startsWith("#skills") ? contextRoute() : location.hash || contextRoute()};
    }
    const g = ++generation; ++searchSequence; clearTimeout(pollTimer); requestController?.abort();
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
    target.replaceChildren(form, direct, config, results);
    if (searchResult) displayResults(results, searchResult);
    else results.append(el("div", {class:"skill-center-empty"}, tr("搜索结果会保留来源；获取源码后，再查看我们的安全检查结果。", "Search results retain their origin. Retrieve the source before viewing our inspection results.")));
    try {
      const data = await api(base + "/sources");
      if (still(g)) config.replaceChildren(el("summary", {}, tr("来源与配额说明", "Source and quota information")), ...((data.items || []).map(source => el("p", {}, el("strong", {}, source.label || source.id), " · ", source.description || ""))));
    } catch (problem) { if (still(g)) config.replaceChildren(el("summary", {}, tr("来源与配额说明", "Source and quota information")), el("p", {class:"help"}, errorText(problem))); }
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
    target.append(meta([[tr("检查引擎", "Engine"),report.engine],[tr("引擎版本", "Engine version"),report.engineVersion],
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
  async function renderPack(target, id, g) {
    target.append(el("p", {class:"help"}, tr("正在读取技能…", "Loading skill…")));
    const pack = await api(base + "/packs/" + enc(id));
    if (!still(g)) return;
    const report = el("section", {class:"skill-center-report",id:"skill-center-report"});
    renderReport(report, pack.scan);
    const job = el("div", {class:"skill-center-job",role:"status",id:"skill-center-job",hidden:true});
    const builtin = action(tr("重新快速检查", "Run quick check"), () => startScan(pack, "builtin", job, report, g));
    const external = action(tr("SkillSpector 静态检查", "SkillSpector static scan"), () => startScan(pack, "skillspector", job, report, g));
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
      meta([[tr("固定版本", "Pinned revision"),pack.revision],[tr("内容摘要", "Content digest"),pack.digest],[tr("获取时间", "Retrieved"),date(pack.createdAt)]]),
      el("div", {class:"skill-center-source-preview"}, files, preview)),
      el("section", {class:"skill-center-checks"}, el("div", {class:"actions"}, builtin, external), engineNote, job, report));
    scanInstallState(pack, g, packJobs.has(pack.id));
    try {
      const engines = await loadEngines();
      if (still(g)) {
        const available = engines.find(engine => engine.id === "skillspector");
        external.disabled = !available?.available;
        engineNote.textContent = available?.message || (available?.available ? tr("SkillSpector 已就绪。此处仅运行静态检查，不启用 LLM。", "SkillSpector is ready. These checks are static and do not enable an LLM.") : tr("SkillSpector 未配置，快速检查仍可使用。", "SkillSpector is not configured. Quick checks remain available."));
      }
    } catch (error) { if (still(g)) { external.disabled = true; engineNote.textContent = errorText(error); } }
    if (packJobs.has(pack.id)) await pollJob(pack, packJobs.get(pack.id), job, report, g, 0);
  }
  async function startScan(pack, engine, jobTarget, reportTarget, g) {
    scanInstallState(pack, g, true);
    try {
      const value = await api(base + "/packs/" + enc(pack.id) + "/scan", {method:"POST",body:{engine,allowLLM:false}});
      if (!still(g)) return;
      const job = value.job || value;
      if (!job.id) throw Error(tr("服务未返回检查任务。", "The service did not return an inspection job."));
      packJobs.set(pack.id, job.id); await pollJob(pack, job.id, jobTarget, reportTarget, g, 0);
    } catch (error) { if (still(g)) { message(jobTarget, errorText(error), "error"); scanInstallState(pack, g, error.code === "skill_scan_running" || packJobs.has(pack.id)); } }
  }
  async function pollJob(pack, id, target, reportTarget, g, attempt) {
    clearTimeout(pollTimer);
    if (!still(g)) return;
    try {
      const job = await api(base + "/jobs/" + enc(id));
      if (!still(g) || packJobs.get(pack.id) !== id) return;
      target.hidden = false; target.className = "skill-center-job";
      target.replaceChildren(badge(job.status, translated(jobStates, job.status)), el("span", {}, job.error || job.message || ""));
      scanInstallState(pack, g, activeJob(job.status));
      if (job.report) renderReport(reportTarget, job.report);
      if (activeJob(job.status)) {
        target.append(action(tr("取消检查", "Cancel check"), async () => { await api(base + "/jobs/" + enc(id) + "/cancel", {method:"POST",body:{}}); await pollJob(pack, id, target, reportTarget, g, 0); }));
        if (attempt < 300) pollTimer = setTimeout(() => pollJob(pack, id, target, reportTarget, g, attempt + 1), 1000);
        else target.append(action(tr("刷新任务状态", "Refresh job status"), () => pollJob(pack, id, target, reportTarget, g, 0)));
      } else packJobs.delete(pack.id);
    } catch (error) {
      if (still(g) && packJobs.get(pack.id) === id) target.replaceChildren(el("p", {class:"error"}, errorText(error)), action(tr("重试读取任务", "Retry reading job"), () => pollJob(pack, id, target, reportTarget, g, 0)));
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
    const review = el("section", {class:"skill-center-install-review",hidden:true}), report = el("div", {class:"skill-center-report"});
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
          renderReport(report, problem.details?.report || problem.details?.scan || pack.scan);
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
