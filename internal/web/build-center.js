"use strict";
window.RunDeskBuildCenter = (() => {
  const base = "/build-center", enc = encodeURIComponent;
  const tr = (zh, en) => document.documentElement.lang === "en" ? en : zh;
  const labels = {draft:"待启动",queued:"排队中",running:"运行中",canceling:"正在取消",canceled:"已取消",interrupted:"状态未确认",failed:"失败",succeeded:"已完成"};
  const active = status => ["queued", "running", "canceling"].includes(status);
  let generation = 0, timer, back, currentTab = "recipes", selected = "", selectedTest = false;
  const receipts = new Map();
  try { for (const row of JSON.parse(sessionStorage.getItem("rundesk-build-center-receipts") || "[]")) if (Array.isArray(row) && /^[a-f0-9]{24}$/.test(row[1])) receipts.set(...row); } catch {}
  const launchInputs = new Map();
  try { for (const row of JSON.parse(sessionStorage.getItem("rundesk-build-center-launch-inputs") || "[]")) if (Array.isArray(row) && typeof row[1]?.name === "string") launchInputs.set(...row); } catch {}
  const inflight = new Map();
  function saveReceipts() { try { sessionStorage.setItem("rundesk-build-center-receipts", JSON.stringify([...receipts])); } catch {} }
  function saveLaunchInputs() { try { sessionStorage.setItem("rundesk-build-center-launch-inputs", JSON.stringify([...launchInputs])); } catch {} }
  async function mutate(path, body = {}, method = "POST", identity) {
    const signature = await submissionSignature("build-center:" + method + path, identity || (path.endsWith("/launch") ? {operation:"launch"} : body));
    if (inflight.has(signature)) return inflight.get(signature);
    if (!receipts.has(signature)) receipts.set(signature, Array.from(crypto.getRandomValues(new Uint8Array(12)), v => v.toString(16).padStart(2, "0")).join(""));
    const requestId = receipts.get(signature); saveReceipts();
    const promise = (async () => {
      try {
        if (body instanceof FormData) body.set("requestId", requestId);
        const value = await api(base + path, {method, body: body instanceof FormData ? body : {...body, requestId}});
        receipts.delete(signature); saveReceipts(); return value;
      } catch (error) {
        if (error.status >= 400 && error.status < 500 && !path.endsWith("/launch") && !["request_in_progress", "request_unconfirmed", "image_operation_unconfirmed", "image_launch_partial", "publication_unconfirmed"].includes(error.code)) { receipts.delete(signature); saveReceipts(); }
        throw error;
      } finally { inflight.delete(signature); }
    })();
    inflight.set(signature, promise); return promise;
  }
  function action(label, fn, cls = "quiet", attrs = {}) {
    let busy = false;
    const node = button(rdText(label), async () => { if (busy) return; busy = true; node.disabled = true; try { await fn(); } finally { busy = false; node.disabled = false; } }, cls);
    for (const [key, value] of Object.entries(attrs)) node.setAttribute(key, value);
    return node;
  }
  const status = value => el("span", {class:"badge"}, rdText(labels[value] || value || "未知"));
  const metadata = rows => el("dl", {class:"image-metadata"}, rows.flatMap(([key, value]) => [el("dt", {}, rdText(key)), el("dd", {}, value || "—")]));
  function dialog(title) {
    const node = el("dialog", {class:"application-register build-dialog build-center-dialog"}), form = el("form");
    form.append(el("div", {class:"dialog-head"}, el("h2", {}, rdText(title)), button(rdText("关闭"), () => node.close(), "quiet")));
    node.append(form); document.body.append(node); node.addEventListener("close", () => node.remove(), {once:true}); node.showModal(); return {node, form};
  }
  function still(g) { return g === generation && state.view === "build-center" && location.hash.startsWith("#build-center"); }
  function close() { ++generation; clearTimeout(timer); const target = back || {view:"applications",route:"#applications"}; setProductPage(target.view); history.replaceState(null, "", target.route); }
  async function open(tab = "recipes", id = "", isTest = false) {
    if (state.view !== "build-center") { stashProductDraft(); back = {view:state.view,route:location.hash.startsWith("#build-center") ? contextRoute() : location.hash || contextRoute()}; }
    const g = ++generation; clearTimeout(timer); currentTab = ["recipes", "builds", "images", "publications"].includes(tab) ? tab : "recipes"; selected = id; selectedTest = isTest;
    if ($("#settings").open) $("#settings").close(); setProductPage("build-center");
    history.replaceState(null, "", "#build-center/" + currentTab + (id ? "/" + enc(id) : "") + (isTest ? "/test" : ""));
    const page = $("#build-center-page"), content = el("div", {class:"build-center-content"}), error = el("p", {class:"error",role:"status","data-center-error":""});
    page.replaceChildren(el("div", {class:"page-intro"}, el("div", {}, button(rdText("← 返回"), close, "quiet back-link"), el("p", {class:"eyebrow"}, "BUILD CENTER"), el("h1", {}, rdText("构建中心")), el("p", {}, rdText("准备工具环境，查看镜像，启动独立实例。配方、插件、Skills 和 MCP 可以按任务自由组合。"))), el("div", {class:"actions",style:"max-width:100%;min-width:0;flex-shrink:1;flex-wrap:wrap"}, action(tr("让 Agent 添加","Ask an Agent to add"), assistant, "primary", {"data-center-action":"assistant"}), action("添加构建配方", upload, "quiet", {"data-center-action":"upload"}), action("下载源设置", sourceCatalog, "quiet", {"data-center-action":"sources"}), action(tr("公开镜像清单","Public image catalog"), publicImages, "quiet", {"data-center-action":"public-images"}))), el("nav", {class:"tabs build-center-tabs","aria-label":rdText("构建中心分类")}, ...[["recipes","构建配方"],["builds","构建与测试"],["images","镜像"],["publications",tr("发布记录","Publications")]].map(([key, label]) => button(rdText(label), () => open(key), key === currentTab ? "selected" : ""))), error, content);
    setLoading(content);
    try {
      if (currentTab === "recipes") await recipes(content, g);
      else if (currentTab === "builds") await builds(content, error, g);
      else if (currentTab === "publications") await publications(content, error, g);
      else await images(content, error, g);
    } catch (e) { if (still(g)) { error.textContent = e.message; content.replaceChildren(action("重试", () => open(currentTab, selected, selectedTest))); } }
  }
  async function recipes(target, g) {
    const data = await api(base + "/recipes"); if (!still(g)) return;
    const filter = el("input", {type:"checkbox",id:"build-center-archived"}), grid = el("div", {class:"application-grid build-center-recipes"});
    const render = () => {
      grid.replaceChildren();
      for (const recipe of data.items.filter(row => filter.checked || !row.archived)) {
        const ops = el("div", {class:"actions"}, action("查看配方", () => inspectRecipe(recipe)));
        if (!recipe.archived) ops.prepend(action("构建镜像", () => buildRecipe(recipe), "primary", {"data-recipe-build":recipe.id}));
        if (recipe.kind === "custom" && !recipe.archived) ops.append(action("归档配方", async () => { if (!confirm(rdText("归档此配方？历史构建和原配置重建仍可查看。"))) return; await api(base + "/recipes/" + enc(recipe.id) + "?revision=" + recipe.revision, {method:"DELETE"}); if (still(g)) await open("recipes"); }));
        grid.append(el("article", {class:"application-card","data-recipe-id":recipe.id}, el("div", {class:"app-card-top"}, el("span", {class:"badge"}, recipe.kind === "builtin" ? rdText("内置") : rdText("自定义")), recipe.archived ? el("span", {class:"badge"}, rdText("已归档")) : null), el("h2", {}, recipe.name), el("p", {class:"app-description"}, recipe.description || ""), el("p", {class:"help"}, "v" + recipe.version + " · r" + recipe.revision + " · " + recipe.dockerfile), ops));
      }
      if (!grid.children.length) grid.append(el("p", {class:"empty"}, rdText("没有构建配方。上传包含 Dockerfile 的 ZIP 添加。")));
    };
    filter.onchange = render; target.replaceChildren(el("label", {class:"build-center-filter"}, filter, rdText("显示已归档配方")), grid); render();
  }
  async function sourceCatalog() {
    const {node, form} = dialog("下载源设置"), editor = el("textarea", {id:"center-sources-json",class:"build-source-json",rows:20,spellcheck:false,"aria-label":rdText("源列表 JSON")}), error = el("p", {class:"error",role:"alert"}), result = el("p", {class:"help",role:"status"}), submit = el("button", {type:"submit",class:"primary",disabled:true}, rdText("保存源列表"));
    let revision = "", busy = false, sequence = 0;
    const load = async () => {
      const seq = ++sequence;
      error.textContent = ""; result.textContent = ""; submit.disabled = true;
      try {
        const value = await api(base + "/sources"); if (!node.open || seq !== sequence) return;
        revision = value.revision;
        editor.value = json({schemaVersion:value.schemaVersion,defaultGroup:value.defaultGroup || "international",types:value.types,groups:value.groups});
        submit.disabled = false;
      } catch (e) { if (node.open) error.textContent = e.message; }
    };
    const merge = action(tr("补充新版内置源选项","Add new built-in source options"), async () => {
      if (busy || !revision) return;
      error.textContent = ""; result.textContent = "";
      let draft;
      try { draft = JSON.parse(editor.value); if (!Array.isArray(draft.types) || !Array.isArray(draft.groups)) throw Error(tr("源列表需要 types 和 groups 数组。","The catalog requires types and groups arrays.")); }
      catch (e) { error.textContent = e.message; return; }
      busy = true; editor.disabled = true; submit.disabled = true; const seq = ++sequence;
      try {
        const defaults = await api(base + "/sources/defaults"); if (!node.open || seq !== sequence) return;
        let added = 0;
        const typeIDs = new Set(draft.types.map(value => value.id)), sourceIDs = new Set(draft.groups.flatMap(value => (value.sources || []).map(source => source.id)));
        for (const type of defaults.types || []) {
          if (!typeIDs.has(type.id)) { draft.types.push(type); typeIDs.add(type.id); added++; continue; }
          const existing = draft.types.find(value => value.id === type.id);
          if (!Array.isArray(existing.fields)) throw Error(tr("源类型缺少 fields 数组，请先修正编辑内容。","A source type has no fields array. Correct the draft first."));
          for (const field of type.fields || []) if (!existing.fields.some(value => value.id === field.id)) { existing.fields.push(field); added++; }
        }
        for (const group of defaults.groups || []) {
          let existing = draft.groups.find(value => value.id === group.id);
          if (!existing) { existing = {...group,sources:[]}; draft.groups.push(existing); added++; }
          if (!Array.isArray(existing.sources)) throw Error(tr("源分组缺少 sources 数组，请先修正编辑内容。","A source group has no sources array. Correct the draft first."));
          for (const source of group.sources || []) if (!sourceIDs.has(source.id)) { existing.sources.push(source); sourceIDs.add(source.id); added++; }
        }
        editor.value = json(draft);
        result.textContent = added ? tr("已补充缺失的内置选项，保留现有地址与默认分组。请检查后保存；尚未写入服务器。","Added missing built-in options, preserving existing addresses and the default group. Review and save; the server has not been changed.") : tr("当前草稿已包含全部内置选项，现有配置未改变。","The draft already includes all built-in options. Existing settings are unchanged.");
      } catch (e) { if (node.open) error.textContent = e.message; }
      finally { busy = false; if (node.open) { editor.disabled = false; submit.disabled = !revision; } }
    }, "quiet", {id:"center-sources-merge-defaults"});
    form.append(el("p", {class:"help"}, rdText("源列表保存在外置配置文件中。可维护国际源和中国镜像；保存后用于新构建，已有构建快照保持原设置。")), el("p", {class:"help"}, rdText("这里只填写公开下载地址，不填写访问令牌、账号或密码。")), editor, error, result, el("div", {class:"actions"}, action("重新读取源列表", load), merge, submit));
    form.onsubmit = async event => {
      event.preventDefault(); if (busy || !revision) return; busy = true; editor.disabled = true; submit.disabled = true; error.textContent = ""; result.textContent = "";
      try {
        const draft = JSON.parse(editor.value);
        const value = await api(base + "/sources", {method:"PUT",body:{schemaVersion:draft.schemaVersion,defaultGroup:draft.defaultGroup || "international",types:draft.types,groups:draft.groups,revision}});
        if (!node.open) return;
        revision = value.revision;
        editor.value = json({schemaVersion:value.schemaVersion,defaultGroup:value.defaultGroup || "international",types:value.types,groups:value.groups});
        result.textContent = rdText("源列表已保存，将用于新构建。");
      } catch (e) { if (node.open) error.textContent = e.status === 409 ? rdText("源列表已被其他操作修改。你的编辑仍保留，请重新读取后合并再保存。") : e.message; }
      finally { busy = false; if (node.open) { editor.disabled = false; submit.disabled = false; } }
    };
    await load();
  }
  async function publicImages(entry) {
    const {node, form} = dialog(tr("公开镜像清单","Public image catalog")), editor = el("textarea", {id:"center-public-images-json",class:"build-source-json",rows:20,spellcheck:false,"aria-label":tr("公开镜像清单 JSON","Public image catalog JSON")}), error = el("p", {class:"error",role:"alert"}), result = el("p", {class:"help",role:"status"}), submit = el("button", {type:"submit",class:"primary",disabled:true,id:"center-public-images-save"}, tr("保存镜像清单","Save image catalog"));
    let revision = "", busy = false, sequence = 0;
    const load = async () => {
      const seq = ++sequence; error.textContent = ""; result.textContent = ""; submit.disabled = true;
      try {
        const value = await api("/setup/public-images"); if (!node.open || seq !== sequence) return;
        revision = value.revision;
        const items = [...(value.items || [])];
        if (entry?.id && entry?.sources?.length) {
          if (!items.some(item => item.id === entry.id)) { items.push(entry); result.textContent = tr("已填入发布结果，尚未保存。请先确认仓库允许匿名访问。","The publication result is added to this draft. Confirm anonymous registry access before saving."); }
          else result.textContent = tr("清单已包含同名版本，请核对后手工合并下载源。","An entry with this ID already exists. Review and merge its download sources manually.");
        }
        editor.value = json({schemaVersion:value.schemaVersion,items}); submit.disabled = false;
      } catch (e) { if (node.open) error.textContent = e.message; }
    };
    const reload = action(tr("重新读取镜像清单","Reload image catalog"), load);
    const download = action(tr("下载已保存清单 JSON","Download saved catalog JSON"), async () => {
      error.textContent = "";
      try {
        const value = await api("/setup/public-images"); if (!node.open) return;
        const blob = new Blob([json({schemaVersion:value.schemaVersion,items:value.items}) + "\n"], {type:"application/json"}), url = URL.createObjectURL(blob), link = el("a", {href:url,download:"public-images.json"});
        document.body.append(link); link.click(); link.remove(); setTimeout(() => URL.revokeObjectURL(url),1000);
        result.textContent = tr("已导出服务器保存的清单。将 public-images.json 放入发行包的 config/ 目录；编辑区尚未保存的改动不在导出文件中。","Exported the saved server catalog. Put public-images.json in the release bundle's config/ directory. Unsaved edits are not included.");
      } catch (e) { if (node.open) error.textContent = e.message; }
    }, "quiet", {id:"center-public-images-download"});
    form.append(el("p", {class:"help"}, tr("这里登记已发布镜像的预设、版本、平台和下载地址。启动向导会读取这份外置清单；空清单表示尚未配置公开镜像。","List published images with their preset, version, platform and download addresses. The setup wizard reads this external catalog; an empty list means no public images are configured.")), el("p", {class:"help"}, tr("每个下载地址必须固定到 sha256 Digest。只填写公开地址，不填写账号、密码或令牌；保存清单不会上传镜像或修改仓库可见性。","Pin each image reference to a sha256 digest. Enter public references only, without credentials. Saving this catalog does not upload images or change registry visibility.")), editor, error, result, el("div", {class:"actions"}, reload, download, submit));
    form.onsubmit = async event => {
      event.preventDefault(); if (busy || !revision) return; busy = true; editor.disabled = true; submit.disabled = true; reload.disabled = true; error.textContent = ""; result.textContent = "";
      try {
        const draft = JSON.parse(editor.value);
        const value = await api("/setup/public-images", {method:"PUT",body:{schemaVersion:draft.schemaVersion,items:draft.items,revision}});
        if (!node.open) return;
        revision = value.revision; entry = null; editor.value = json({schemaVersion:value.schemaVersion,items:value.items});
        result.textContent = tr("镜像清单已保存，新下载会使用这份清单。","Image catalog saved. New downloads will use it.");
      } catch (e) { if (node.open) error.textContent = e.status === 409 ? tr("镜像清单已被其他操作修改。你的编辑仍保留，请重新读取后合并再保存。","The catalog changed elsewhere. Your draft is preserved; reload and merge before saving.") : e.message; }
      finally { busy = false; if (node.open) { editor.disabled = false; submit.disabled = false; reload.disabled = false; } }
    };
    await load();
  }
  const publicationInputs = new Map();
  try { for (const row of JSON.parse(sessionStorage.getItem("rundesk-publication-inputs") || "[]")) if (Array.isArray(row) && typeof row[1]?.target === "string") publicationInputs.set(...row); } catch {}
  function savePublicationInputs() { try { sessionStorage.setItem("rundesk-publication-inputs", JSON.stringify([...publicationInputs])); } catch {} }
  function publicationText(value) {
    return String(value || "").replace(/(Bearer\s+)[^\s"',;]+/gi,"$1[redacted]").replace(/\brd-(?:build|setup)-[a-f0-9]{32,}\b/g,"[redacted]").replace(/((?:api[_-]?key|access[_-]?token|token|password|secret|authorization)["']?\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s&,;"'<>}\]]+)/gi,"$1[redacted]").replace(/(https?:\/\/)[^/\s:@]+:[^@\s/]+@/gi,"$1[redacted]@");
  }
  function publishImage(image, ready) {
    if (!ready.eligible) return;
    const saved = publicationInputs.get(ready.sourceBuildId), {node, form} = dialog(tr("发布到镜像仓库","Publish to registry"));
    const target = el("input", {id:"center-publication-target",required:true,autocomplete:"off",spellcheck:false,placeholder:"ghcr.io/your-account/" + (ready.profileId || "image") + ":" + (ready.profileVersion || "1.0.0"),value:saved?.target || "",disabled:!!saved});
    const confirm = el("input", {id:"center-publication-confirm",type:"checkbox",checked:!!saved,disabled:!!saved}), error = el("p", {class:"error",role:"alert"}), submit = el("button", {id:"center-publication-submit",type:"submit",class:"primary"}, saved ? tr("重试原发布请求","Retry the same publication") : tr("开始发布","Publish image"));
    const reset = action(tr("编辑新的发布请求","Edit a new publication request"), () => { publicationInputs.delete(ready.sourceBuildId); savePublicationInputs(); target.disabled = false; confirm.disabled = false; confirm.checked = false; reset.hidden = true; error.textContent = ""; submit.textContent = tr("开始发布","Publish image"); }); reset.hidden = !saved;
    form.append(metadata([["Image ID",image.imageId],[tr("来源构建","Source build"),ready.sourceBuildId],[tr("平台","Platform"),ready.platform],["Offline",ready.offlineTestId],["Smoke",ready.smokeTestId]]), el("label", {}, tr("仓库地址和固定版本标签","Registry reference and fixed release tag"), target), el("p", {class:"help"}, tr("例如 ghcr.io/账号/镜像:1.1.2。发布使用 RunDesk 服务账户已有的 Docker 登录凭据；需要先在服务器完成 docker login。","For example, ghcr.io/account/image:1.1.2. Publication uses the RunDesk service account's Docker credentials; first run docker login on the server.")), el("label", {class:"build-center-filter"}, confirm, tr("将这个已验证镜像上传到以上仓库","Upload this verified image to the registry above")), el("p", {class:"help"}, tr("上传成功后还需在 GHCR、Docker Hub 等平台将仓库设为公开。RunDesk 不会自动更改仓库可见性，发布结果不代表匿名访问已验证。","After upload, configure public visibility in GHCR, Docker Hub or your registry. RunDesk does not change visibility; a successful push does not verify anonymous access.")), error, el("div", {class:"actions"}, submit, reset));
    let busy = false;
    form.onsubmit = async event => {
      event.preventDefault(); if (busy) return; error.textContent = "";
      if (!confirm.checked) { error.textContent = tr("请确认上传这个镜像。","Confirm the image upload first."); return; }
      const input = publicationInputs.get(ready.sourceBuildId) || {sourceBuildId:ready.sourceBuildId,target:target.value.trim(),confirm:true};
      if (!input.target) { error.textContent = tr("请填写仓库地址和固定版本标签。","Enter the registry reference and fixed release tag."); return; }
      busy = true; submit.disabled = true; target.disabled = true; confirm.disabled = true;
      publicationInputs.set(ready.sourceBuildId,input); savePublicationInputs();
      try {
        const value = await mutate("/publications", input); publicationInputs.delete(ready.sourceBuildId); savePublicationInputs();
        if (node.open) { node.close(); await open("publications",value.id); }
      } catch (e) {
        if (!node.open) return;
        error.textContent = publicationText(e.message); reset.hidden = false; submit.textContent = tr("重试原发布请求","Retry the same publication");
      } finally { busy = false; if (node.open) submit.disabled = false; }
    };
  }
  async function publications(target, error, g) {
    const list = el("div", {class:"image-list"}), detail = el("section", {class:"card image-detail",id:"center-publication-detail"});
    target.replaceChildren(el("div", {class:"actions"}, action(tr("刷新发布记录","Refresh publications"), () => open("publications", selected)), action(tr("公开镜像清单","Public image catalog"), publicImages)), el("div", {class:"image-layout"}, list, detail));
    let selection = 0, rows = [], offset = 0, logText = "";
    function renderList() {
      list.replaceChildren(...rows.map(value => el("article", {class:"image-version card" + (value.id === selected ? " selected" : ""),"data-center-publication-id":value.id}, button(value.target || value.id, () => select(value.id), "image-detail-link"), status(value.status), el("p", {class:"help"}, value.created ? new Date(value.created).toLocaleString() : ""))));
      if (!rows.length) list.append(el("p", {class:"empty"}, tr("尚无发布记录。从镜像详情中发布通过 Offline 和 Smoke 检查的镜像。","No publications yet. Publish an image from its detail page after offline and smoke checks pass.")));
    }
    async function select(id) { ++selection; selected = id; offset = 0; logText = ""; renderList(); setLoading(detail); await refresh(); }
    async function refresh() {
      clearTimeout(timer); const seq = selection;
      try {
        const data = await api(base + "/publications"); if (!still(g) || seq !== selection) return;
        rows = data.items || []; if (!selected && rows.length) selected = rows[0].id; renderList();
        if (!selected) { detail.replaceChildren(el("p", {class:"help"}, tr("选择发布记录查看进度和下载地址。","Select a publication to inspect progress and download references."))); return; }
        const id = selected, value = await api(base + "/publications/" + enc(id)); if (!still(g) || seq !== selection || selected !== id) return;
        const log = await api(base + "/publications/" + enc(id) + "/log?after=" + offset); if (!still(g) || seq !== selection || selected !== id) return;
        offset = log.next; logText = (logText + (log.text || "")).slice(-131072);
        history.replaceState(null, "", "#build-center/publications/" + enc(id));
        detail.replaceChildren(el("h2", {}, value.target || value.id), el("p", {id:"center-publication-status",role:"status"}, status(value.status)), metadata([[tr("来源构建","Source build"),value.sourceBuildId],["Image ID",value.imageId],["Digest",value.digest],[tr("固定下载地址","Pinned download reference"),value.reference]]), ...(value.error ? [el("p", {class:"error"}, publicationText(value.error))] : []));
        if (active(value.status)) detail.append(action(tr("取消发布","Cancel publication"), async () => { try { await mutate("/publications/" + enc(id) + "/cancel"); if (still(g) && selected === id) await refresh(); } catch (e) { if (still(g)) error.textContent = publicationText(e.message); } }, "quiet", {"data-center-action":"cancel-publication"}));
        if (value.status === "interrupted") detail.append(el("p", {class:"help"}, tr("服务重启前的上传结果未确认。请先检查目标仓库；此记录不会自动重新发布。","The upload outcome before restart is unconfirmed. Check the registry first; this record will not be republished automatically.")));
        if (value.status === "succeeded") {
          detail.append(el("p", {class:"help",id:"center-publication-visibility"}, value.publicAccessVerified ? tr("镜像清单的匿名访问检查已通过，可以将这个固定版本加入公开镜像清单。","Anonymous access to the image manifest passed. This pinned release can be added to the public image catalog.") : tr("镜像已上传。仓库是否允许匿名访问尚未验证；请在仓库平台设为公开，然后检查匿名访问。","Image uploaded. Anonymous access has not been verified. Set the registry visibility to public, then check anonymous access.")), ...(value.publicCheckedAt ? [el("p", {class:"help"}, tr("检查时间：","Checked at: ") + value.publicCheckedAt)] : []), ...(value.publicAccessError ? [el("p", {class:"error"}, publicationText(value.publicAccessError))] : []), action(tr("检查匿名访问","Check anonymous access"), async () => { try { await mutate("/publications/" + enc(id) + "/check-public"); if (still(g) && selected === id) await refresh(); } catch (e) { if (still(g)) error.textContent = publicationText(e.message); } }, "quiet", {"data-center-action":"check-publication-access"}));
          detail.append(el("p", {class:"help"}, tr("此检查验证镜像清单可匿名读取；完整镜像层下载与运行检查仍在目标机器上完成。","This checks anonymous access to the image manifest. Complete layer downloads and runtime checks still take place on each target machine.")));
          if (value.reference) {
            const ref = el("input", {id:"center-publication-reference",readonly:true,value:value.reference,"aria-label":tr("固定下载地址","Pinned download reference")});
            detail.append(ref, action(tr("复制下载地址","Copy download reference"), async () => { try { await navigator.clipboard.writeText(value.reference); } catch { ref.focus(); ref.select(); } }));
          }
          if (value.publicImage) {
            const add = action(tr("填入公开镜像清单草稿","Add to public catalog draft"), () => publicImages(value.publicImage), "quiet", {"data-center-action":"publication-catalog-draft"}); add.disabled = value.publicAccessVerified !== true;
            detail.append(el("details", {}, el("summary", {}, tr("查看镜像清单条目","View catalog entry")), el("pre", {id:"center-publication-catalog-entry",style:"white-space:pre-wrap;overflow-wrap:anywhere"}, json(value.publicImage))), add);
          }
        }
        detail.append(el("h3", {}, tr("发布日志","Publication log")), el("pre", {id:"center-publication-log",style:"max-height:340px;overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere"}, publicationText(logText)));
        if (rows.some(row => active(row.status))) timer = setTimeout(refresh,2200);
      } catch (e) { if (still(g) && seq === selection) { error.textContent = publicationText(e.message); detail.append(action(tr("重新读取发布状态","Reload publication status"), refresh)); } }
    }
    await refresh();
  }
  async function inspectRecipe(recipe) {
    const {node, form} = dialog("构建配方"), error = el("p", {class:"error",role:"status"}), code = el("pre", {class:"recipe-dockerfile",id:"center-recipe-dockerfile-content",tabindex:0}, rdText("正在读取 Dockerfile…")), version = el("input", {id:"center-export-codex-version",placeholder:"X.Y.Z",pattern:"[0-9]+\\.[0-9]+\\.[0-9]+",required:recipe.kind === "builtin"}), note = el("p", {class:"help"}), download = el("a", {class:"app-open",download:"",id:"center-recipe-context-download"}, rdText("下载完整构建目录 ZIP"));
    let sequence = 0;
    const suffix = () => recipe.kind === "builtin" && version.value.trim() ? "?codexVersion=" + enc(version.value.trim()) : "";
    const load = async () => {
      const seq = ++sequence; error.textContent = "";
      try {
        const value = await api(base + "/recipes/" + enc(recipe.id) + "/dockerfile" + suffix());
        if (!node.open || seq !== sequence) return;
        code.textContent = value.dockerfile || "";
        note.textContent = value.template ? rdText("这是配方模板。填写 Codex 精确版本后，可预览或下载可直接构建的目录。") : rdText("下载包含 Dockerfile、依赖文件、Skills 和 vendor；可在本地修改后作为新配方导入。");
      } catch (e) { if (node.open && seq === sequence) { code.textContent = ""; error.textContent = e.message; } }
    };
    const updateDownload = () => { download.href = "/api/v1" + base + "/recipes/" + enc(recipe.id) + "/context.zip" + suffix(); };
    version.oninput = updateDownload; updateDownload();
    download.onclick = event => {
      if (recipe.kind === "builtin" && !/^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$/.test(version.value.trim())) {
        event.preventDefault(); error.textContent = rdText("下载内置配方前，请填写 Codex 精确版本。");
      }
    };
    form.append(el("h3", {}, recipe.name), metadata([["版本", recipe.version + " · r" + recipe.revision],["Dockerfile",recipe.dockerfile],["配方 ID",recipe.id],["配方 SHA256",recipe.recipeSha],["上下文 SHA256",recipe.contextSha256]]), el("p", {class:"help"}, rdText("构建保存配方和上下文快照。原配置重建使用该次构建的快照。插件引用为声明，不代表已经安装或加载。")), el("details", {}, el("summary", {}, rdText("声明与简单测试")), el("pre", {}, json({plugins:recipe.plugins || [],smoke:recipe.smoke}))), el("h3", {}, "Dockerfile"), recipe.kind === "builtin" ? el("label", {}, rdText("Codex 精确版本"), version) : null, el("div", {class:"actions"}, action("预览 Dockerfile", load), download), note, error, code);
    await load();
  }
  async function buildRecipe(recipe) {
    const {node, form} = dialog("构建镜像"), version = el("input", {id:"center-codex-version",placeholder:"X.Y.Z",required:recipe.kind === "builtin",pattern:"[0-9]+\\.[0-9]+\\.[0-9]+"}), reference = el("input", {id:"center-build-reference",placeholder:"rundesk-tools:1.0"}), noCache = el("input", {type:"checkbox",id:"center-build-no-cache"}), error = el("p", {class:"error",role:"status"}), sources = el("section", {class:"build-source-panel",id:"center-build-sources"}), submit = el("button", {type:"submit",class:"primary",disabled:true}, rdText("开始构建"));
    let sourceInput, runtimeInput, busy = false, sourceReady = false;
    const runtimePanel = el("section", {class:"build-runtime-panel"});
    if (recipe.kind === "builtin") { runtimeInput = runtimeOptions(runtimePanel, {version:() => version.value.trim()}); version.addEventListener("input", runtimeInput.invalidate); }
    form.append(el("h3", {}, recipe.name + " · r" + recipe.revision), recipe.kind === "builtin" ? el("label", {}, rdText("Codex 精确版本"), version) : null, el("label", {}, rdText("镜像标签（可选）"), reference), recipe.kind === "builtin" ? runtimePanel : null, sources, el("label", {class:"build-checkbox"}, noCache, rdText("不使用缓存")), el("p", {class:"help"}, rdText("在 RunDesk 所在主机使用 Docker 构建。成功后可启动独立实例，现有环境继续使用各自配置。")), error, submit);
    const load = async () => {
      sourceReady = false; submit.disabled = true; setLoading(sources); error.textContent = "";
      try {
        const [catalog, capability] = await Promise.all([api(base + "/sources"),api(base + "/recipes/" + enc(recipe.id) + "/sources")]);
        if (!node.open) return;
        sourceInput = sourceOptions(sources, catalog, capability);
        sources.append(action("重新读取下载源", load));
        sourceReady = true; submit.disabled = busy;
      } catch (e) {
        if (!node.open) return;
        sources.replaceChildren(el("p", {class:"error"}, e.message), action("重试读取下载源", load));
      }
    };
    form.onsubmit = async event => {
      event.preventDefault(); if (busy || !sourceReady) return; busy = true; submit.disabled = true; error.textContent = "";
      try {
        const value = await mutate("/recipes/" + enc(recipe.id) + "/build", {recipeRevision:recipe.revision,recipeSha:recipe.recipeSha,...(recipe.kind === "builtin" ? {codexVersion:version.value.trim(),codexRuntime:runtimeInput.read()} : {}),...(reference.value.trim() ? {reference:reference.value.trim()} : {}),noCache:noCache.checked,buildSources:sourceInput()});
        if (node.open) { node.close(); await open("builds", value.id); }
      } catch (e) { if (node.open) error.textContent = e.message; }
      finally { busy = false; if (node.open) submit.disabled = !sourceReady; }
    };
    await load();
  }
  function runtimeOptions(target, options = {}) {
    const prefix = options.prefix || "center-codex-runtime", initial = options.value || {};
    const modes = [
      ["auto",tr("自动复用服务器缓存，否则在线安装","Reuse server cache, otherwise install online")],
      ["native",tr("仅使用服务器已验证的 Codex 安装包","Require a verified Codex package on the server")],
      ["base",tr("使用已包含 Codex 的本机基础镜像","Use a local base image containing Codex")],
      ["npm",tr("通过 npm 在线安装","Install online through npm")]
    ];
    const mode = el("select", {id:prefix + "-mode"}, ...modes.map(([value,label]) => el("option", {value}, label)));
    mode.value = modes.some(([value]) => value === initial.mode) ? initial.mode : "auto";
    const reference = el("input", {id:prefix + "-reference",value:initial.reference || "",autocomplete:"off",spellcheck:false,placeholder:"rundesk-codex:0.157.1"});
    const baseField = el("label", {}, tr("本机基础镜像引用","Local base image reference"), reference);
    const help = el("p", {class:"help"}), result = el("p", {class:"help",role:"status",id:prefix + "-status"});
    let sequence = 0;
    const invalidate = () => { ++sequence; result.textContent = ""; };
    const read = () => {
      const value = {mode:mode.value};
      if (mode.value === "base") {
        if (!reference.value.trim()) throw Error(tr("请填写包含 Codex 的本机基础镜像。","Enter a local base image containing Codex."));
        value.reference = reference.value.trim();
      }
      return value;
    };
    const check = action(tr("检查服务器 Codex 缓存","Check server Codex cache"), async () => {
      const version = options.version?.() || "", seq = ++sequence;
      result.textContent = "";
      if (!/^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$/.test(version) || version === "0.0.0") {
        result.textContent = tr("先填写 Codex 精确版本。","Enter the exact Codex release first."); return;
      }
      result.textContent = tr("正在检查服务器缓存…","Checking the server cache…");
      try {
        const value = await api(base + "/runtime-options?codexVersion=" + enc(version));
        if (!target.isConnected || seq !== sequence) return;
        result.textContent = (value.nativeAvailable ? tr("发现匹配的已验证安装包","A matching verified package is available") : tr("未发现匹配的已验证安装包","No matching verified package is available")) + (value.platform ? " · " + value.platform : "") + (value.reason ? " — " + value.reason : "");
      } catch (error) { if (target.isConnected && seq === sequence) result.textContent = error.message; }
    }, "quiet", {id:prefix + "-check"});
    const update = () => {
      invalidate(); baseField.hidden = mode.value !== "base"; reference.disabled = mode.value !== "base";
      check.hidden = !["auto","native"].includes(mode.value);
      help.textContent = ({
        auto:tr("按精确版本和 Linux 架构复用 RunDesk 已验证的安装包；没有匹配缓存时使用所选 npm 源。损坏的缓存会报错。","Reuse a RunDesk-verified package matching the exact release and Linux architecture; when no cache matches, use the selected npm source. Corrupt cache entries cause an error."),
        native:tr("没有匹配缓存时停止构建。可先在初始化第二步安装相同版本的 Codex。","Stop if no matching cache exists. Install the same Codex release in setup step two first."),
        base:tr("使用服务器本机已有镜像，不自动拉取。固定镜像身份，并在构建前检查 Codex 版本、平台、Node 22+、npm 和 node 用户。","Use an image already on the server, without pulling it. Pin the image identity and check the Codex release, platform, Node 22+, npm and node user before building."),
        npm:tr("按精确版本从所选 npm 源安装 Codex；业务工具仍按配方安装。","Install the exact Codex release from the selected npm source; install application tools as declared by the recipe.")
      })[mode.value];
      options.onchange?.({mode:mode.value,reference:reference.value.trim()});
    };
    mode.onchange = update; reference.oninput = () => { invalidate(); options.onchange?.({mode:mode.value,reference:reference.value.trim()}); };
    target.replaceChildren(el("h3", {}, tr("Codex 运行时来源","Codex runtime source")), el("label", {}, tr("获取方式","Preparation method"), mode), baseField, help, check, result);
    update(); return {read,invalidate};
  }
  function sourceOptions(target, catalog, capability) {
    const group = el("select", {id:"center-source-group","aria-label":tr("网络配置","Network configuration")}, ...(catalog.groups || []).map(value => el("option", {value:value.id}, value.name))), list = el("div", {class:"build-source-list"}), summary = el("details", {class:"build-source-summary"}, el("summary", {}, rdText("本次构建的下载地址"))), preview = el("div");
    summary.append(preview);
    group.value = catalog.groups.some(value => value.id === catalog.defaultGroup) ? catalog.defaultGroup : catalog.groups.some(value => value.id === "international") ? "international" : catalog.groups[0]?.id || "";
    const all = (catalog.groups || []).flatMap(value => (value.sources || []).map(source => ({...source,groupName:value.name}))), rows = new Map();
    const fields = new Map((catalog.types || []).map(value => [value.id,value]));
    const updatePreview = () => preview.replaceChildren(...[...rows].map(([type,row]) => el("div", {}, el("strong", {}, fields.get(type)?.name || type), metadata([...row.inputs].map(([key,input]) => [row.labels.get(key),input.value.trim() || rdText("沿用配方默认")])))));
    const render = () => {
      rows.clear(); list.replaceChildren();
      for (const info of (capability.types || []).filter(value => value.detected || value.supported)) {
        const type = fields.get(info.type) || {id:info.type,name:info.type,fields:[]}, choices = all.filter(value => value.type === info.type), card = el("fieldset", {class:"build-source-card","data-source-type":info.type});
        card.append(el("legend", {}, type.name));
        const available = (type.fields || []).filter(field => info.supported && (info.arguments || []).includes(field.argument));
        if (!available.length) {
          card.append(el("p", {class:"help",role:"note"}, rdText("配方使用此类下载，但未声明可配置的源参数。请修改配方文件后重新导入。")));
          list.append(card); continue;
        }
        const select = el("select", {id:"center-source-" + info.type,"aria-label":type.name + " " + rdText("下载源")}, el("option", {value:""}, rdText("沿用配方默认 / 手动填写")));
        for (const value of catalog.groups || []) {
          const entries = choices.filter(source => source.groupName === value.name);
          if (entries.length) select.append(el("optgroup", {label:value.name}, ...entries.map(source => el("option", {value:source.id}, source.name))));
        }
        const inputs = new Map(), labels = new Map(), note = el("p", {class:"help"});
        card.append(el("label", {}, rdText("下载源"), select));
        for (const field of available) {
          const input = el("input", {id:"center-source-" + info.type + "-" + field.id,type:"text",spellcheck:false,"data-source-field":field.id});
          inputs.set(field.id,input); labels.set(field.id,field.label);
          input.oninput = updatePreview;
          card.append(el("label", {}, field.label, input));
        }
        card.append(note);
        const apply = () => {
          const choice = choices.find(value => value.id === select.value);
          for (const [key,input] of inputs) input.value = choice?.values?.[key] || "";
          note.textContent = choice?.description || (choice?.requiresConfiguration ? rdText("此条目需要填写适合你的构建环境的地址。") : "");
          updatePreview();
        };
        select.onchange = apply;
        const preferred = choices.find(value => (catalog.groups.find(g => g.id === group.value)?.sources || []).some(source => source.id === value.id));
        select.value = preferred?.id || "";
        rows.set(info.type,{select,inputs,labels,choices}); apply(); list.append(card);
      }
      if (!list.children.length) list.append(el("p", {class:"help"}, rdText("此配方未声明下载源参数，将使用配方中的原始配置。")));
      updatePreview();
    };
    group.onchange = render;
    target.replaceChildren(el("h3", {}, rdText("下载源与网络")), el("label", {}, tr("网络配置","Network configuration"), group), el("p", {class:"help"}, rdText("分组用于快速填入地址，每一类源仍可单独选择或修改。这里只显示配方涉及的下载类型。")), list, summary, el("p", {class:"help"}, rdText("本次构建保存所选源的快照；用原配置重建继续使用原设置。")));
    render();
    return () => {
      const value = {group:group.value,catalogRevision:catalog.revision,selections:{},overrides:{}};
      for (const [type,row] of rows) {
        const selected = row.choices.find(choice => choice.id === row.select.value);
        const values = Object.fromEntries([...row.inputs].map(([key,input]) => [key,input.value.trim()]));
        if (selected?.requiresConfiguration && !Object.values(values).some(Boolean)) throw Error(rdText("请为需要配置的源填写下载地址。"));
        if (row.select.value) value.selections[type] = row.select.value;
        value.overrides[type] = values;
      }
      return value;
    };
  }
  function frozenSourceView(value) {
    if (!value) return null;
    const rows = Object.entries(value.values || {}).flatMap(([type,fields]) => Object.entries(fields || {}).map(([key,address]) => [type + " · " + key,address || rdText("沿用配方默认")]));
    return el("details", {class:"build-frozen-sources"}, el("summary", {}, rdText("本次构建使用的下载源")), metadata([["源分组",value.group],["源列表 SHA256",value.catalogSHA],...rows]), (value.unconfigured || []).length ? el("p", {class:"help"}, rdText("以下类型保留配方原始设置：") + " " + value.unconfigured.join(", ")) : null, el("p", {class:"help"}, rdText("这是提交构建时保存的配置，修改源列表不会改写这份记录。")));
  }
  function frozenRuntimeView(value) {
    if (!value) return null;
    const names = {auto:tr("自动选择","Automatic"),native:tr("已验证安装包","Verified package"),base:tr("已有基础镜像","Existing base image"),npm:tr("npm 在线安装","Online npm installation")};
    const rows = [[tr("请求方式","Requested source"),names[value.requestedMode] || value.requestedMode],[tr("实际来源","Resolved source"),names[value.mode] || value.mode],["Codex",value.version],[tr("平台","Platform"),value.platform]];
    if (value.installId) rows.push([tr("安装记录","Installation record"),value.installId]);
    if (value.assetSha256) rows.push([tr("安装包 SHA256","Package SHA256"),value.assetSha256]);
    if (value.reference) rows.push([tr("基础镜像","Base image"),value.reference]);
    if (value.imageId) rows.push(["Image ID",value.imageId]);
    return el("details", {class:"build-frozen-runtime"}, el("summary", {}, tr("本次构建的 Codex 运行时","Codex runtime for this build")), metadata(rows), value.reason ? el("p", {class:"help"}, value.reason) : null);
  }
  function upload() {
    const {node, form} = dialog("添加构建配方"), name = el("input", {id:"center-recipe-name",required:true,maxlength:120}), version = el("input", {id:"center-recipe-version",value:"1.0.0",required:true,maxlength:64}), description = el("textarea", {id:"center-recipe-description",rows:2,maxlength:4096}), dockerfile = el("input", {id:"center-recipe-dockerfile",value:"Dockerfile",required:true}), file = el("input", {id:"center-recipe-zip",type:"file",accept:".zip,application/zip",required:true}), smoke = el("textarea", {id:"center-recipe-smoke",rows:3,placeholder:'["python", "--version"]'}), timeout = el("input", {id:"center-recipe-smoke-timeout",type:"number",value:60,min:1,max:300}), expected = el("textarea", {id:"center-recipe-expected-files",rows:2,placeholder:"/opt/tools/README.md"}), plugins = el("textarea", {id:"center-recipe-plugins",rows:3,placeholder:"[]"}), error = el("p", {class:"error",role:"status"}), submit = el("button", {type:"submit",class:"primary"}, rdText("保存配方"));
    form.append(el("label", {}, rdText("名称"), name), el("label", {}, rdText("版本"), version), el("label", {}, rdText("说明"), description), el("label", {}, rdText("构建上下文 ZIP"), file), el("label", {}, rdText("Dockerfile 相对路径"), dockerfile), el("details", {}, el("summary", {}, rdText("可选：简单测试与原生插件声明")), el("label", {}, rdText("Smoke 命令（JSON 参数数组）"), smoke), el("label", {}, rdText("超时秒数"), timeout), el("label", {}, rdText("预期文件（每行一个路径）"), expected), el("label", {}, rdText("插件引用（JSON 数组，仅声明）"), plugins)), el("p", {class:"help"}, rdText("保存只登记配方，随后可单独构建。ZIP 根目录作为上下文；最多 32 MiB。")), error, submit);
    let busy = false; form.onsubmit = async event => {
      event.preventDefault(); if (busy) return; busy = true; submit.disabled = true; error.textContent = "";
      try {
        const zip = file.files[0]; if (!zip) throw Error(rdText("请选择 ZIP")); if (zip.size > 32 * 1048576) throw Error(rdText("ZIP 最大 32 MiB"));
        const command = smoke.value.trim() ? JSON.parse(smoke.value) : [], refs = plugins.value.trim() ? JSON.parse(plugins.value) : [];
        if (!Array.isArray(command) || command.some(v => typeof v !== "string") || !Array.isArray(refs)) throw Error(rdText("测试命令须为字符串数组，插件引用须为数组"));
        const manifest = {schemaVersion:1,name:name.value.trim(),version:version.value.trim(),description:description.value.trim(),dockerfile:dockerfile.value.trim(),plugins:refs,smoke:{command,timeoutSeconds:Number(timeout.value),expectedFiles:expected.value.split(/\r?\n/).map(v => v.trim()).filter(Boolean)}};
        // Plain HTTP installations may lack SubtleCrypto. The server always
        // verifies the full content digest; this is only a local retry identity.
        const hash = crypto.subtle ? Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", await zip.arrayBuffer())), v => v.toString(16).padStart(2,"0")).join("") : {name:zip.name,size:zip.size,lastModified:zip.lastModified,type:zip.type};
        const body = new FormData(); body.append("manifest", JSON.stringify(manifest)); body.append("context", zip);
        await mutate("/recipes", body, "POST", {manifest,contextSha256:hash});
        if (node.open) { node.close(); await open("recipes"); }
      } catch (e) { error.textContent = e.message; } finally { busy = false; submit.disabled = false; }
    };
  }
  async function builds(target, error, g) {
    const list = el("div", {class:"build-list"}), detail = el("section", {class:"card build-detail"}); target.replaceChildren(el("div", {class:"build-layout"}, list, detail));
    let offset = 0, text = "", previous = "", signature = "", sequence = 0;
    async function poll() {
      if (!still(g)) return; const seq = ++sequence;
      try {
        const data = await api(base + "/builds"); if (!still(g) || seq !== sequence) return;
        let rows = data.items;
        if (selectedTest && selected && !rows.some(row => row.id === selected)) rows = [await api(base + "/tests/" + enc(selected)), ...rows];
        if (!still(g) || seq !== sequence) return;
        if (!selected && rows.length) selected = rows[0].id;
        if (signature !== JSON.stringify(rows)) {
          signature = JSON.stringify(rows); list.replaceChildren(...rows.map(value => el("article", {class:"card build-card" + (value.id === selected ? " selected" : ""),"data-center-build-id":value.id}, button(value.name || value.reference || value.id, () => open("builds", value.id, value.scope === "image-test"), "build-title"), status(value.status), el("p", {class:"help"}, value.scope === "image-test" ? "test · " + value.testKind : value.recipeId || value.scope || ""), el("p", {class:"help"}, value.created ? new Date(value.created).toLocaleString() : ""))));
          if (!rows.length) list.append(el("p", {class:"empty"}, rdText("还没有构建或测试记录。")));
        }
        const value = rows.find(row => row.id === selected); if (!value) { detail.replaceChildren(el("p", {class:"help"}, rdText("选择记录查看状态与日志。"))); return; }
        const isTest = value.scope === "image-test", path = (isTest ? "/tests/" : "/builds/") + enc(value.id);
        if (previous !== value.id) { offset = 0; text = ""; previous = value.id; }
        const log = await api(base + path + "/log?after=" + offset); if (!still(g) || seq !== sequence) return;
        offset = log.next; text += log.text || ""; error.textContent = "";
        const ops = el("div", {class:"actions"});
        if (!isTest && value.recipeId) ops.append(action("用原配置重建", async () => { const built = await mutate("/builds/" + enc(value.id) + "/rebuild", {}); if (still(g)) await open("builds", built.id); }, "primary", {"data-center-action":"rebuild"}));
        if (!isTest && active(value.status)) { const cancel = action("取消构建", async () => { await api(base + path + "/cancel", {method:"POST",body:{}}); if (still(g)) { clearTimeout(timer); await poll(); } }); cancel.disabled = value.status === "canceling"; ops.append(cancel); }
        if (value.imageId) ops.append(action("查看镜像", async () => { const images = await api(base + "/images"); const image = images.items.find(row => row.imageId === value.imageId); if (still(g)) await open("images", image?.key || ""); }));
        const old = detail.querySelector("pre"), bottom = !old || old.scrollHeight - old.clientHeight - old.scrollTop < 32, scroll = old?.scrollTop || 0, pre = el("pre", {class:"build-log",tabindex:0,id:"center-build-log"}, text || rdText("尚无日志。"));
        const identity = metadata([["配方", value.recipeId ? value.recipeId + " · r" + value.recipeRevision : "—"],["镜像标签",value.reference],["Image ID",value.imageId],["上下文 SHA256",value.contextSha256]]);
        detail.replaceChildren(el("div", {class:"build-heading"}, el("h2", {}, value.name || value.id), status(value.status)), isTest ? el("p", {class:"help"}, value.testKind === "offline" ? rdText("Offline：验证本地工具和预期文件，不调用模型。") : rdText("Smoke：在临时容器执行配方声明的命令；结果以实际日志与退出状态为准。")) : null, identity, frozenSourceView(value.buildSources), frozenRuntimeView(value.codexRuntime), ops, value.error ? el("p", {class:"error"}, value.error) : null, el("h3", {}, rdText("日志")), pre, log.truncated || value.logTruncated ? el("p", {class:"error"}, rdText("日志已截断。")) : null);
        pre.scrollTop = bottom ? pre.scrollHeight : scroll;
      } catch (e) { if (still(g) && seq === sequence) error.textContent = e.message; }
      finally { if (still(g) && seq === sequence) timer = setTimeout(poll, 2000); }
    }
    await poll();
  }
  async function images(target, error, g) {
    const data = await api(base + "/images"); if (!still(g)) return;
    const list = el("div"), detail = el("section", {class:"card image-detail"}), toolbar = el("div", {class:"actions"}, action("刷新镜像", () => open("images", selected)));
    let selection = 0;
    target.replaceChildren(toolbar, el("div", {class:"image-layout"}, list, detail));
    const renderList = () => { list.replaceChildren(...data.items.map(value => el("article", {class:"image-version card" + (value.key === selected ? " selected" : ""),"data-center-image-key":value.key}, button(value.references[0] || value.imageId, () => show(value.key), "image-detail-link"), el("p", {class:"image-reference"}, value.imageId), el("span", {class:"badge"}, value.availability || rdText("未核对")), el("p", {class:"help"}, (value.sources || []).map(source => (source.name || source.id) + (source.status ? " · " + rdText(labels[source.status] || source.status) : "")).join(" · "))))); if (!data.items.length) list.append(el("p", {class:"empty"}, rdText("尚无已登记的本地镜像。完成一次构建后在这里查看。"))); };
    async function show(key) {
      const seq = ++selection; selected = key; renderList(); setLoading(detail);
      try {
        const value = await api(base + "/images/" + enc(key)); if (!still(g) || seq !== selection) return;
        history.replaceState(null, "", "#build-center/images/" + enc(key));
        const ops = el("div", {class:"actions"}, action("核对镜像", async () => { await api(base + "/images/" + enc(key) + "/check", {method:"POST",body:{}}); if (still(g) && selected === key) await open("images", key); }), action("创建并启动独立实例", () => launch(value), "primary", {"data-center-action":"launch"}), action("删除镜像", async () => { if (!confirm(rdText("从本机 Docker 实际删除此镜像，保留历史登记？被现有实例、环境或容器引用时会拒绝删除。"))) return; await mutate("/images/" + enc(key), {imageId:value.imageId}, "DELETE"); if (still(g)) await open("images"); }, "quiet", {"data-center-action":"delete-image"}));
        const tests = el("div", {class:"actions"}, ...["offline","smoke"].map(kind => action(kind === "offline" ? "Offline 检查" : "Smoke 测试", async () => { const test = await mutate("/images/" + enc(key) + "/tests", {kind}); if (still(g)) await open("builds", test.id, true); }, "quiet", {"data-center-test":kind})));
        if (value.availability === "deleted") { for (const node of ops.querySelectorAll('[data-center-action="launch"],[data-center-action="delete-image"]')) node.disabled = true; for (const node of tests.querySelectorAll("button")) node.disabled = true; }
        if (value.lastTest) tests.append(action("上次测试结果", () => open("builds", value.lastTest.id, true)));
        detail.replaceChildren(el("h2", {}, value.references[0] || rdText("镜像详情")), metadata([["Image ID",value.imageId],["标签",value.references.join("\n")],["可用状态",value.availability || "unknown"],["核对时间",value.checkedAt || "—"]]), value.error ? el("p", {class:"error"}, value.error) : null, ops, el("h3", {}, rdText("简单测试")), el("p", {class:"help"}, rdText("Offline 使用内置工具检查；Smoke 执行配方冻结的测试命令。不支持的检查会返回明确原因，不表示通过。")), tests, el("h3", {}, rdText("来源与使用情况")), metadata([["来源",(value.sources || []).map(source => (source.name || source.id) + (source.status ? " · " + rdText(labels[source.status] || source.status) : "") + (source.recipeRevision ? " · r" + source.recipeRevision : "")).join("\n")],["实例引用",(value.selectedBy || []).map(row => row.name + " (" + row.instanceId + ")").join("\n")],["项目环境",(value.environments || []).map(row => row.instanceId + " / " + row.workspaceId + " · " + (row.state || "unknown")).join("\n")]]));
        const publication = el("section", {"data-center-publication-readiness":key}, el("h3", {}, tr("发布镜像","Publish image")), el("p", {class:"help",role:"status"}, tr("正在检查发布条件…","Checking publication requirements…")));
        detail.append(publication);
        api(base + "/images/" + enc(key) + "/publication-readiness").then(ready => {
          if (!still(g) || seq !== selection) return;
          const publish = action(tr("发布到镜像仓库","Publish to registry"), () => publishImage(value, ready), "quiet", {"data-center-action":"publish-image"});
          publish.disabled = !ready.eligible || value.availability === "deleted";
          publication.replaceChildren(el("h3", {}, tr("发布镜像","Publish image")), el("p", {class:"help",role:"status"}, ready.eligible ? tr("此构建的 Offline 和 Smoke 检查已通过，可以发布固定版本。","This build passed offline and smoke checks and can be published as a fixed release.") : ready.reason || tr("先完成同一构建的 Offline 和 Smoke 检查。","Complete offline and smoke checks for the same build first.")), metadata([[tr("来源构建","Source build"),ready.sourceBuildId],["Offline",ready.offlineTestId],["Smoke",ready.smokeTestId]]), publish);
        }).catch(e => { if (still(g) && seq === selection) publication.replaceChildren(el("h3", {}, tr("发布镜像","Publish image")), el("p", {class:"error"}, e.message)); });
      } catch (e) { if (still(g) && seq === selection) detail.replaceChildren(el("p", {class:"error"}, e.message), action("重试", () => show(key))); }
    }
    renderList(); if (!selected && data.items.length) selected = data.items[0].key;
    if (selected) await show(selected); else detail.append(el("p", {class:"help"}, rdText("选择镜像查看详情。")));
  }
  function launch(image) {
    const path = "/images/" + enc(image.key) + "/launch", saved = launchInputs.get(path);
    const {node, form} = dialog("启动独立实例"), name = el("input", {id:"center-launch-name",required:true,maxlength:160,value:saved?.name || image.sources[0]?.name || "工具实例",disabled:!!saved}), error = el("p", {class:"error",role:"status"}), result = el("div", {role:"status"}), submit = el("button", {type:"submit",class:"primary"}, rdText(saved ? "重试原请求" : "创建并启动")), reset = action("开始另一实例请求", async () => { const signature = await submissionSignature("build-center:POST" + path, {operation:"launch"}); receipts.delete(signature); launchInputs.delete(path); saveReceipts(); saveLaunchInputs(); error.textContent = ""; result.replaceChildren(); reset.hidden = true; name.disabled = false; submit.textContent = rdText("创建并启动"); name.focus(); }); reset.hidden = !saved;
    form.append(el("p", {class:"image-reference"}, image.imageId), el("label", {}, rdText("实例名称"), name), el("p", {class:"help"}, rdText("创建专用 Codex 实例和独立项目目录，使用现有环境服务启动容器。登录与模型任务由该实例单独配置。")), error, result, submit, reset);
    let busy = false, submitted = false; form.onsubmit = async event => { event.preventDefault(); if (busy || submitted) return; busy = true; submit.disabled = true; error.textContent = ""; try {
      const input = launchInputs.get(path) || {name:name.value.trim()}; launchInputs.set(path, input); saveLaunchInputs();
      const value = await mutate(path, input); launchInputs.delete(path); saveLaunchInputs(); if (!node.open) return;
      submitted = true; submit.hidden = true; reset.hidden = true; name.disabled = true;
      result.replaceChildren(el("h3", {}, rdText("实例已创建")), metadata([["实例",value.instance.name + " · " + value.instance.id],["项目",value.workspace.name + " · " + value.workspace.id],["容器状态",value.environment.state || "unknown"],["环境",value.environment.id],["Image ID",value.environment.imageId || image.imageId]]), value.environment.error ? el("p", {class:"error"}, value.environment.error) : null, action("打开实例", async () => { await refreshApplicationCatalog(); state.workspaces = await api("/workspaces"); $("#workspace").replaceChildren(...state.workspaces.map(work => el("option", {value:work.id}, work.name))); $("#workspace").value = value.workspace.id; node.close(); await showLegacyApplication(value.instance.id,value.workspace.id); }));
    } catch (e) { error.textContent = e.message; name.disabled = true; reset.hidden = false; submit.textContent = rdText("重试原请求"); result.replaceChildren(el("p", {class:"help"}, rdText("重试会查询同一请求结果。若已创建资源，请按错误中的编号处理；开始另一请求会创建另一实例。"))); } finally { busy = false; submit.disabled = false; } };
  }
  async function assistant() {
    const {node, form} = dialog(tr("让 Agent 添加构建配方","Ask an Agent to add a recipe")), prompt = el("textarea", {id:"center-assistant-prompt",rows:6,required:true,placeholder:tr("描述需要的工具环境、项目地址、依赖和简单测试。Kun 或 Codex 会准备配方并验证。","Describe the environment, project, dependencies and smoke checks. Kun or Codex will prepare and test the recipe.")}), instances = el("select", {id:"center-assistant-instance",required:true}), workspaces = el("select", {id:"center-assistant-workspace",required:true}), error = el("p", {class:"error",role:"status"}), result = el("div", {role:"status"}), submit = el("button", {type:"submit",class:"primary"}, tr("交给 Agent","Start Agent task"));
    form.append(el("label", {}, tr("已有 Kun / Codex 实例","Existing Kun / Codex instance"), instances), el("label", {}, rdText("参考工作区"), workspaces), el("label", {}, rdText("任务要求"), prompt), el("p", {class:"help"}, rdText("参考选定项目，在独立目录创建可追踪任务。构建权限由服务端限制为本次任务，凭据不进入浏览器或提示词。")), error, result, submit); submit.disabled = true;
    try { const [all, works] = await Promise.all([api("/instances"), api("/workspaces")]); if (!node.open) return; const agents = all.filter(row => ["codex","kun"].includes(row.agentRuntime?.kind || "codex")); instances.replaceChildren(...agents.map(row => el("option", {value:row.id}, row.name + " · " + (row.agentRuntime?.kind === "kun" ? "Kun" : "Codex")))); workspaces.replaceChildren(...works.map(row => el("option", {value:row.id}, row.name))); if (agents.some(row => row.id === instance()?.id)) instances.value = instance().id; if (works.some(row => row.id === ws()?.id)) workspaces.value = ws().id; submit.disabled = !instances.value || !workspaces.value; } catch (e) { error.textContent = e.message; }
    let busy = false, runTimer; node.addEventListener("close", () => clearTimeout(runTimer), {once:true});
    async function watch(id) {
      if (!node.open) return;
      try { const value = await api(base + "/assistant-runs/" + enc(id)); if (!node.open) return; renderAssistantResult(result, value); if (!["completed","failed","canceled","interrupted","unconfirmed"].includes(value.status)) runTimer = setTimeout(() => watch(id), 2500); }
      catch (e) { if (node.open) { error.textContent = e.message; runTimer = setTimeout(() => watch(id), 5000); } }
    }
    let submitted = false; form.onsubmit = async event => { event.preventDefault(); if (busy || submitted || !instances.value || !workspaces.value) return; busy = true; submit.disabled = true; error.textContent = ""; try {
      const value = await mutate("/assistant-runs", {prompt:prompt.value.trim(),instanceId:instances.value,workspaceId:workspaces.value}); if (!node.open) return;
      submitted = true; submit.hidden = true; prompt.disabled = true; instances.disabled = true; workspaces.disabled = true;
      renderAssistantResult(result, value); runTimer = setTimeout(() => watch(value.id), 2500);
    } catch (e) { error.textContent = e.message; } finally { busy = false; submit.disabled = false; } };
  }
  function renderAssistantResult(target, value) {
    target.replaceChildren(el("h3", {}, rdText("构建助手任务")), metadata([["任务",value.taskId],["状态",value.status || "unknown"],["实例",value.instanceId],["专用工作区",value.workspaceId]]), value.error ? el("p", {class:"error"}, value.error) : null, value.sessionId ? el("a", {href:"/?session=" + enc(value.sessionId),class:"app-open"}, rdText("查看任务对话与轨迹 ↗")) : null, el("p", {class:"help"}, value.skillBootstrap ? rdText("已准备 Skill 引导；原生插件安装状态未改变。") : rdText("原生插件安装状态未改变。")));
  }
  $("#nav-build-center").onclick = () => safe(() => open());
  return {open, close, sourceOptions, sourceCatalog, publicImages, runtimeOptions, frozenRuntimeView, mutate};
})();
