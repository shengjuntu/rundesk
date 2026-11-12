// Session management, full-history diagnostics and configuration controls.
function renderSessions() {
  if(window.RunDeskConversations){RunDeskConversations.renderTree();return;}
  const query = $("#session-search").value.trim().toLowerCase();
  const archived = $("#session-scope").value === "archived";
  const sessions = state.sessions.filter(
    (s) =>
      s.workspaceId === ws()?.id &&
      sessionInContext(s) &&
      !!s.archived === archived &&
      (!query || s.title.toLowerCase().includes(query)),
  );
  $("#session-count").textContent = sessions.length;
  $("#session-list").replaceChildren(
    ...sessions.map((s) =>
      button(
        (s.pinned ? "◆ " : active(s.status) ? "• " : "") + s.title,
        () => selectSession(s.id),
        "session" + (state.session?.id === s.id ? " active" : ""),
      ),
    ),
  );
  if (!sessions.length)
    $("#session-list").append(
      el(
        "p",
        { class: "empty" },
        query
          ? rdText("没有匹配的会话")
          : archived
            ? rdText("没有已归档会话")
            : rdText("从一个新对话开始"),
      ),
    );
}
async function selectFirst() {
  const archived = $("#session-scope").value === "archived";
  const first = state.sessions.find(
    (s) =>
      s.workspaceId === ws()?.id &&
      sessionInContext(s) &&
      !!s.archived === archived,
  );
  if (first) await selectSession(first.id);
  else {
    resetConversation();
    $("#model").value = instance()?.defaultModel || "";
    restoreProductDraft();
  }
}
function renderStatus() {
  renderCapabilityStrip();
  const s = state.session;
  const diagnosticOnly=s?.runtimeKind==="kun"&&(!!s.traceOrigin||!!s.kunFork);
  const forkFinished=!!s?.kunFork&&!active(s.status);
  const maintenanceOnly=s?.runtimeKind==="kun"&&!s.traceOrigin;
  $("#attach").disabled=diagnosticOnly;$("#skill-picker").disabled=diagnosticOnly;
  $("#session-title").textContent = s?.title || rdText("新会话");
  $("#run-status").textContent = s?.archived
    ? rdText("已归档")
    : s?.retry && active(s.status) ? rdText("Codex 正在重试") : statusLabel[s?.status] || s?.status || rdText("就绪");
  $("#stop").classList.toggle("hidden", !active(s?.status));
  const transitioning = ["starting", "stopping"].includes(s?.status);
  const steering = active(s?.status) && !transitioning;
  $("#send").disabled =
    maintenanceOnly || forkFinished || state.loadingSession || state.sending || transitioning || (steering && !s?.turnId) || !!s?.archived;
  $("#send").title = steering ? rdText("补充指令到当前任务") : rdText("发送");
  $("#send").setAttribute("aria-label", $("#send").title);
  $("#send").classList.toggle("steering", steering);
  $("#send").textContent = steering ? rdText("补充") : "↑";
  $("#stop").disabled = s?.status === "stopping";
  $("#prompt").disabled = maintenanceOnly || forkFinished || state.loadingSession || !!s?.archived;
  $("#prompt").placeholder = maintenanceOnly ? rdText("Kun 维护与历史记录；日常任务请新建 Codex 会话。") : forkFinished ? "分叉已结束；请从来源创建新预览。" : s?.archived
    ? rdText("从会话菜单恢复后，可继续对话。")
    : steering
      ? rdText("补充要求或调整当前任务的方向…")
      : s?.traceOrigin ? "询问固定来源证据…" : s?.runtimeKind === "kun" ? "给 Kun 一个任务…" : rdText("给 Codex 一个任务…");
  $("#model").disabled = !!s;
  $("#session-menu").classList.toggle("hidden", !s);
  $("#pin-session").textContent = s?.pinned ? rdText("取消置顶") : rdText("置顶");
  $("#archive-session").textContent = s?.archived ? rdText("恢复会话") : rdText("归档");
  $("#archive-session").disabled = active(s?.status);
  $("#delete-session").disabled = active(s?.status);
  window.RunDeskTraceUI?.syncComposer();
  renderAnalysisBanner();
  window.RunDeskRecovery?.render();
 window.RunDeskTasks?.banner();
 window.RunDeskConversations?.renderEngine();
}
function renderAnalysisBanner() {
  const host=$("#analysis-banner"),fork=state.session?.kunFork,origin=state.session?.traceOrigin;
  if(fork){host.classList.remove("hidden");const signature=json(fork);if(host._signature===signature)return;host._signature=signature;
   host.replaceChildren(el("strong",{},fork.mode==="live"?"Live · 真实执行分支":"Hybrid · 独立分支"),el("span",{},(fork.mode==="live"?"工具访问当前项目和真实 MCP；可能重复来源副作用，文件不会回滚。来源边界 #":"模型重新调用；工具仅回放录制结果，未执行真实工具。来源边界 #")+fork.sequence),button("查看固定预览",()=>RunDeskKunForks.open({previewId:fork.previewId}),"quiet"));return;}

  host.classList.toggle("hidden",!origin);
  if(!origin){host.replaceChildren();host._signature="";return;}
  const signature=json(origin);if(host._signature===signature)return;host._signature=signature;
  host.replaceChildren(el("strong",{},rdText("过程分析 · 独立会话")),el("span",{},rdFormat("来源：${0} · 截至 ${1} 的记录",origin.title,new Date(origin.capturedAt).toLocaleString())),button(rdText("查看原任务过程"),async()=>{const source=await api("/sessions/"+encodeURIComponent(origin.sessionId));RunDeskDebug.open(source,{runId:origin.runId,through:origin.through});},"quiet"),el("span",{},rdText("固定来源证据；沿用配置的模型与认证，用量记在本诊断会话。建议不会自动执行，原任务单独运行。")));
}
async function patchCurrent(patch) {
  if (!state.session) return;
  const id = state.session.id;
  const result = await api(`/sessions/${id}`, { method: "PATCH", body: patch });
  if (state.session?.id === id) state.session = result;
  $("#session-menu").open = false;
  await refreshSessions();
  if (patch.archived !== undefined) {
    $("#session-scope").value = patch.archived ? "archived" : "active";
    renderSessions();
  }
  renderStatus();
}
function downloadText(name, text, type = "text/plain;charset=utf-8") {
  const url = URL.createObjectURL(new Blob([text], { type }));
  const link = el("a", { href: url, download: name });
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
function debugParams() {
  const q = new URLSearchParams({ limit: "250" });
  for (const [key, id] of [
    ["q", "event-query"],
    ["category", "event-category"],
    ["direction", "event-direction"],
    ["method", "event-method"],
  ]) {
    const value = $("#" + id).value.trim();
    if (value) q.set(key, value);
  }
  for (const name of ["from", "to"]) {
    const value = $("#event-" + name).value;
    if (value) q.set(name, new Date(value).toISOString());
  }
  return q;
}
async function searchEvents(more = false) {
  if (!state.session) {
    toast(rdText("请先选择一个会话"));
    return;
  }
  const id = state.session.id;
  const q =
    more && state.historyQuery
      ? new URLSearchParams(state.historyQuery)
      : debugParams();
  if (more) q.set("after", state.history?.at(-1)?.id || 0);
  const request = ++state.historyRequest;
  $("#event-search").disabled = true;
  try {
    const rows = await api(`/sessions/${id}/events?${q}`);
    if (id !== state.session?.id || request !== state.historyRequest) return;
    state.history = more ? [...state.history, ...rows] : rows;
    state.historyMore = rows.length === 250;
    state.historyQuery = q.toString();
    state.debugPaused = false;
    $("#event-pause").textContent = rdText("暂停滚动");
    renderDebug();
  } finally {
    if (request === state.historyRequest) $("#event-search").disabled = false;
  }
}
function renderDebug() {
  if ($("#debug").classList.contains("hidden")) return;
  const box = $("#debug-content");
  $("#event-controls").classList.toggle("hidden", state.tab !== "events");
  $("#event-count").textContent =
    state.history !== null && state.tab === "events"
      ? rdFormat("已查询 ${0} 条",state.history.length)
      : rdFormat("已载入 ${0} 条",state.events.length);
  if (state.tab === "events") {
    $("#event-mode").textContent =
      state.history !== null
        ? rdText("历史查询 · 按时间顺序 · 不随新事件变化")
        : state.debugPaused
          ? rdText("已暂停显示；后台继续接收事件")
          : rdText("实时 · 显示最近 250 条");
    if (state.debugPaused) return;
    const opened = new Set(
      [...box.querySelectorAll("details[open]")].map((n) => n.dataset.eventId),
    );
    const list =
      state.history !== null
        ? state.history
        : state.events.slice(-250).reverse();
    const nodes = list.map((ev) =>
      el(
        "details",
        {
          class: "event",
          "data-event-id": String(ev.id),
          open: opened.has(String(ev.id)),
        },
        el(
          "summary",
          {},
          el(
            "span",
            { class: "direction" },
            { in: "IN", out: "OUT", internal: "APP", stderr: "ERR" }[
              ev.direction
            ] || ev.direction,
          ),
          el(
            "span",
            { class: "method" },
            ev.method || (ev.data.error ? "RPC error" : "RPC result"),
          ),
          el(
            "time",
            {},
            new Date(ev.time).toLocaleTimeString("zh-CN", { hour12: false }),
          ),
        ),
        el("pre", {}, json(ev)),
        button(
          rdText("复制事件 JSON"),
          async () => {
            await navigator.clipboard.writeText(json(ev));
            toast(rdText("事件已复制"));
          },
          "event-copy",
        ),
      ),
    );
    box.replaceChildren(...nodes);
    if (!nodes.length)
      box.append(
        el(
          "p",
          { class: "empty" },
          state.history !== null
            ? rdText("没有匹配的历史事件。")
            : rdText("发送一条消息，查看运行事件。"),
        ),
      );
    if (state.history !== null && state.historyMore)
      box.append(
        button(rdText("加载更多历史事件"), () => searchEvents(true), "query-more"),
      );
  } else if (state.tab === "context") {
    const last = (method) => state.events.findLast((e) => e.method === method);
    const input = last("run/input"),
      submitted = state.events.findLast(
        (e) => e.method === "turn/start" && e.direction === "out",
      );
    const tokens = last("thread/tokenUsage/updated");
    const compactions = state.events.filter(
      (e) =>
        e.method === "item/completed" &&
        e.data.params?.item?.type === "contextCompaction",
    );
    const end = state.events.findLast(
      (e) => e.method === "run/state" && (!input || e.id > input.id),
    );
    const elapsed = input
      ? Math.max(
          0,
          ((end ? new Date(end.time) : new Date()) - new Date(input.time)) /
            1000,
        ).toFixed(1) + rdText(" 秒")
      : rdText("尚未运行");
    box.replaceChildren(
      el("h3", {}, rdText("本次运行")),
      el(
        "pre",
        {},
        json({
          instanceId: state.session?.instanceId,
          workspaceId: state.session?.workspaceId,
          runId: state.session?.runId,
          threadId: state.session?.threadId,
          status: state.session?.status,
          elapsed,
        }),
      ),
      el("h3", {}, rdText("输入与项目笔记")),
      el("pre", {}, input ? json(input.data) : rdText("尚未运行")),
      el("h3", {}, rdText("实际提交的 turn/start")),
      el("pre", {}, submitted ? json(submitted.data.params) : rdText("尚未提交")),
      el("h3", {}, rdText("原生 Token 用量")),
      el(
        "pre",
        {},
        tokens ? json(tokens.data.params) : rdText("接口尚未提供 Token 用量"),
      ),
      el(
        "p",
        { class: "help" },
        rdText("total 是累计用量，last 是最近一轮的用量；均不代表完整上下文快照。耗时包含启动和等待审批。"),
      ),
      el("h3", {}, rdFormat("原生上下文压缩 · ${0} 次",compactions.length)),
      ...compactions.map((e) => el("pre", {}, json(e.data.params.item))),
    );
  } else {
    box.replaceChildren(
      button(rdText("刷新文件"), loadFiles, "quiet"),
      ...state.files.map((f) =>
        el(
          "div",
          { class: "file-card" },
          el("strong", {}, f.name),
          el(
            "div",
            { class: "row" },
            el("small", {}, prettySize(f.size)),
            el(
              "div",
              { class: "row" },
              button(rdText("预览"), () => previewFile(f), "quiet"),
              el("a", { href: fileURL(f.path) }, rdText("下载 ↓")),
            ),
          ),
        ),
      ),
    );
    if (!state.files.length)
      box.append(el("p", { class: "empty" }, rdText("产物目录中还没有文件。")));
  }
}

async function renderSkills(target) {
  const requested = instance(), work = ws(), selection = state.selection;
  const valid = () => target.isConnected && selection === state.selection && instance()?.id === requested.id && ws()?.id === work.id && state.settingsTab === "skills";
  const current = (await RunDeskEnvironments.effective(requested, work)).instance;
  if (!valid()) return;
  const cp = configPath(work.id, current.id);
  const raw = await api(cp("/skills"));
  if (!valid()) return;
  state.skills = (raw.data || []).flatMap((d) => d.skills || []);
  const errors = (raw.data || []).flatMap((d) => d.errors || []);
  const scope = el(
    "select",
    { id: "skill-scope" },
    el("option", { value: "instance" }, RunDeskEnvironments.scopeLabel()),
    el("option", { value: "project" }, rdText("项目技能 · 共享目录")),
  );
  const name = el("input", {
    type: "text",
    id: "skill-name",
    placeholder: rdText("例如 project-guide"),
  });
  const text = el(
    "textarea",
    { rows: 12, id: "skill-content" },
    rdText("---\nname: project-guide\ndescription: 本项目的工作约定\n---\n\n在修改代码前，先阅读 README，并说明验证方式。\n"),
  );
  const list = el(
    "div",
    { class: "card" },
    el("h3", {}, rdText("当前实例发现的 Skills")),
    el(
      "p",
      {},
      current.execution?.mode==="docker"?rdText("当前实例在所选工作区的技能。镜像内置、环境追加和项目技能可能共同生效。"):rdText("实例技能供当前 Agent 使用；项目技能在所属工作区中可见。"),
    ),
  );
  for (const sk of state.skills) {
    const toggle = el("input", {
      type: "checkbox",
      checked: sk.enabled !== false,
      disabled: !!sk.pluginId,
      "aria-label": rdText("启用 ") + sk.name,
    });
    toggle.onchange = () =>
      safe(async () => {
        toggle.disabled = true;
        try {
          await api(cp("/skills/toggle"), {
            method: "POST",
            body: { path: sk.path, enabled: toggle.checked },
          });
          toast(rdText("配置已保存；下次运行读取"));
          await renderSettings();
        } catch (e) {
          toggle.checked = !toggle.checked;
          throw e;
        } finally {
          toggle.disabled = false;
        }
      });
    const use = button(
      rdText("用于下次消息"),
      () => {
        if (!state.chosenSkills.some((s) => s.path === sk.path))
          state.chosenSkills.push({ name: sk.name, path: sk.path });
        renderAttachments();
        closeSettings();
      },
      "quiet",
    );
    use.disabled = sk.enabled === false || !skillCanBeSelected();
    if(!skillCanBeSelected())use.textContent=rdText("在任务中选用");
    use.classList.add("skill-use");
    const ops = el("div", { class: "skill-actions" }, use, el("label",{class:"skill-enable"},toggle,rdText(sk.pluginId?"由插件管理":"启用")));
    const normalized = sk.path.replaceAll("\\", "/");
    const projectRoot = work.path.replaceAll("\\", "/") + "/.agents/skills/";
    const instanceRoot = current.codexHome.replaceAll("\\", "/") + "/skills/";
    const skScope = normalized.startsWith(projectRoot)
      ? "project"
      : normalized.startsWith(instanceRoot)
        ? "instance"
        : "external";
    const relative = normalized.slice(
      (skScope === "project" ? projectRoot : instanceRoot).length,
    );
    const managed =
      !sk.pluginId && skScope !== "external" &&
      /^[a-zA-Z0-9_-]{1,64}\/SKILL\.md$/.test(relative);
    if (managed) {
      const slug = sk.path.replaceAll("\\", "/").split("/").at(-2);
      ops.prepend(
        button(
          rdText("编辑"),
          async () => {
            const v = await api(
              cp("/skills/" + encodeURIComponent(slug), skScope),
            );
            scope.value = skScope;
            name.value = slug;
            text.value = v.content;
            const editor=text.closest("details.configuration-editor");if(editor)editor.open=true;
            text.focus();
          },
          "quiet",
        ),
      );
      ops.append(button(rdText("目录"),()=>openSkillDirectory(work.id,current.id,slug,skScope),"quiet"),button(rdText("导出 ZIP"),()=>downloadSkillBundle(work.id,current.id,slug,skScope),"quiet"),button(rdText("移除"),async()=>{
        if(!confirm(rdFormat("移除技能「${0}」的完整目录？所有文件都会备份。",sk.name)))return;
        const result=await api(cp("/skill-bundles/"+encodeURIComponent(slug),skScope),{method:"DELETE"});
        state.chosenSkills=state.chosenSkills.filter(s=>s.path!==sk.path);renderAttachments();toast(rdText("完整目录已备份：")+result.backupPath);await renderSettings();
      },"quiet"));
    }
    list.append(
      el(
        "div",
        { class: "skill-row" },
        el(
          "div",
          {},
          el("strong", {}, sk.name),
          RunDeskNativePlugins.originNode(sk),
          sk.description?.length>220?el("details",{class:"skill-description"},el("summary",{},sk.description.slice(0,160)+"…"),el("p",{},sk.description)):el("p", {}, sk.description || ""),
          el(
            "span",
            { class: "badge" },
            managed
              ? skScope === "instance"
                ? RunDeskEnvironments.scopeLabel()
                : rdText("项目技能 · 共享")
              : sk.scope === "system"
                ? rdText("Codex 内置")
                : rdText("其他来源"),
          ),
          el("details",{class:"skill-path"},el("summary",{},rdText("查看文件位置")),el("small",{},sk.path)),
        ),
        ops,
      ),
    );
  }
  if (!state.skills.length)
    list.append(
      el("p", { class: "empty" }, rdText("还没有可用技能。导入完整目录，或展开下方编辑器创建简单技能。")),
    );
  const file = el("input", {
    type: "file",
    accept: ".md,text/markdown,text/plain",
    class: "hidden",
    id: "skill-import-file",
  });
  file.onchange = () =>
    safe(async () => {
      const f = file.files[0];
      if (!f) return;
      if (f.size > 256 * 1024) throw Error(rdText("SKILL.md 最多 256 KiB"));
      text.value = await f.text();
      const match = text.value.match(/^name:\s*["']?([\w-]+)/m);
      if (match) name.value = match[1];
      toast(rdText("已载入编辑器，确认后点击保存。"));
    });
  target.replaceChildren(
    skillBundleImporter(work.id,current.id),
    list,
    ...errors.map((e) => el("pre", { class: "error" }, json(e))),
    el(
      "div",
      { class: "card" },
      el("h3", {}, rdText("编辑 SKILL.md / 创建简单技能")),
      el(
        "p",
        {},
        rdText("这里只编辑 SKILL.md；已有脚本、参考资料和模板会保留。完整技能请使用上方目录导入。"),
      ),
      el("label", { for: "skill-scope" }, rdText("保存范围")),
      scope,
      el("label", { for: "skill-name" }, rdText("目录名称")),
      name,
      el("label", { for: "skill-content" }, "SKILL.md"),
      text,
      file,
      el(
        "div",
        { class: "actions" },
        button(
          rdText("保存并重新扫描"),
          async () => {
            await api(
              cp(
                "/skills/" + encodeURIComponent(name.value.trim()),
                scope.value,
              ),
              {
                method: "PUT",
                body: { content: text.value.replaceAll("\r\n", "\n") },
              },
            );
            toast(rdText("Skill 已保存"));
            await renderSettings();
          },
          "primary",
        ),
        button(rdText("导入 SKILL.md"), () => file.click()),
        button(rdText("导出编辑内容"), () =>
          downloadText(
            (name.value.trim() || "project") + "-SKILL.md",
            text.value,
          ),
        ),
      ),
    ),
  );
}

async function renderMCP(target) {
  const current = instance(), work = ws(), selection = state.selection,
    cp = configPath(work.id, current.id);
  const info = await api(cp("/mcp"));
  if (!target.isConnected || selection !== state.selection || instance()?.id !== current.id || ws()?.id !== work.id || state.settingsTab !== "mcp") return;
  state.mcp = info;
  const name = el("input", {
    type: "text",
    id: "mcp-name",
    placeholder: rdText("例如 filesystem"),
  });
  const type = el(
    "select",
    { id: "mcp-type" },
    el("option", { value: "stdio" }, rdText("本地命令 · stdio")),
    el("option", { value: "http" }, rdText("远程服务 · HTTP")),
  );
  const command = el("input", {
    type: "text",
    id: "mcp-command",
    placeholder: rdText("例如 npx / uvx / 可执行文件路径"),
  });
  const args = el("textarea", {
    rows: 3,
    id: "mcp-args",
    placeholder: rdText("每行一个参数，不经过 shell 拆分"),
  });
  const env = el("textarea", { rows: 3, id: "mcp-env" }, "{}");
  const url = el("input", {
    type: "text",
    id: "mcp-url",
    placeholder: "https://example.com/mcp",
  });
  const tokenEnv = el("input", {
    type: "text",
    id: "mcp-token-env",
    placeholder: rdText("例如 MCP_API_TOKEN（环境变量名）"),
  });
  const enabled = el("input", {
    type: "checkbox",
    checked: true,
    id: "mcp-enabled",
  });
  const policies = mcpPolicyEditor(info, cp, () => name.value.trim());
  const advanced = el("input", { type: "checkbox", id: "mcp-advanced" });
  const config = el(
    "textarea",
    { rows: 11, id: "mcp-json" },
    json({ command: "npx", args: [], enabled: true }),
  );
  const local = el(
    "div",
    {},
    el("label", { for: "mcp-command" }, rdText("启动命令")),
    command,
    el("label", { for: "mcp-args" }, rdText("参数（每行一个）")),
    args,
    el("label", { for: "mcp-env" }, rdText("环境变量 JSON")),
    env,
  );
  const remote = el(
    "div",
    { class: "hidden" },
    el("label", { for: "mcp-url" }, rdText("MCP 地址")),
    url,
    el("label", { for: "mcp-token-env" }, rdText("Bearer Token 的环境变量名")),
    tokenEnv,
  );
  const fields = el(
    "div",
    { class: "mcp-fields" },
    el("label", { for: "mcp-type" }, rdText("连接类型")),
    type,
    local,
    remote,
    el("label", {}, enabled, rdText(" 启用服务")),
    policies.node,
  );
  const jsonPanel = el(
    "div",
    { class: "hidden" },
    el("label", { for: "mcp-json" }, rdText("完整配置 JSON")),
    config,
  );
  let original = {};
  function fill(value) {
    original = { ...value };
    type.value = value.url ? "http" : "stdio";
    command.value = value.command || "";
    args.value = (value.args || []).join("\n");
    env.value = json(value.env || {});
    url.value = value.url || "";
    tokenEnv.value = value.bearer_token_env_var || "";
    enabled.checked = value.enabled !== false;
    policies.fill(value);
    config.value = json(value);
    switchType();
  }
  function switchType() {
    local.classList.toggle("hidden", type.value !== "stdio");
    remote.classList.toggle("hidden", type.value !== "http");
  }
  function formValue() {
    const v = policies.apply({ ...original, enabled: enabled.checked });
    if (type.value === "stdio") {
      delete v.url;
      delete v.bearer_token_env_var;
      delete v.http_headers;
      delete v.env_http_headers;
      v.command = command.value.trim();
      v.args = args.value ? args.value.split("\n") : [];
      v.env = JSON.parse(env.value || "{}");
    } else {
      delete v.command;
      delete v.args;
      delete v.env;
      delete v.env_vars;
      v.url = url.value.trim();
      if (tokenEnv.value.trim()) v.bearer_token_env_var = tokenEnv.value.trim();
      else delete v.bearer_token_env_var;
    }
    return v;
  }
  type.onchange = switchType;
  advanced.onchange = () =>
    safe(() => {
      try {
        if (advanced.checked) config.value = json(formValue());
        else fill(JSON.parse(config.value));
        fields.classList.toggle("hidden", advanced.checked);
        jsonPanel.classList.toggle("hidden", !advanced.checked);
      } catch (e) {
        advanced.checked = !advanced.checked;
        throw e;
      }
    });
  const list = el(
    "div",
    { class: "card" },
    el("h3", {}, rdText("MCP 工具配置")),
    el("p", {class:"help"}, rdFormat("正在编辑 ${0} · ${1}。${2}",contextTitle(),RunDeskEnvironments.scopeLabel(),info.runtime==="kun"?"Kun 配置按实例独立保存，当前运行保持原有快照，下一轮生效；Codex 配置可通过导出／导入迁移。":current.execution?.mode==="docker"?rdText("此项目环境的 MCP 在容器中执行，其他项目独立配置。"):rdText("项目层配置可能覆盖这里的值。"))),

  );
  const save = async (n, value, remove = false) => {
    const r = await api(cp("/mcp/" + encodeURIComponent(n)), {
      method: "PUT",
      body: { version: info.version, config: value, remove },
    });
    toast(
      r.reloadError
        ? rdText("已保存；重载失败：") + r.reloadError
        : rdText("已保存；下一轮任务会重载 MCP"),
    );
    await renderSettings();
  };
  for (const [n, v] of Object.entries(info.userServers || {})) {
    const toggle = el("input", {
      type: "checkbox",
      checked: v.enabled !== false,
      "aria-label": rdText("启用 MCP ") + n,
    });
    toggle.onchange = () =>
      safe(async () => {
        toggle.disabled = true;
        try {
          await save(n, { ...v, enabled: toggle.checked });
        } catch (e) {
          toggle.checked = !toggle.checked;
          throw e;
        } finally {
          toggle.disabled = false;
        }
      });
    list.append(
      el(
        "div",
        { class: "skill-row" },
        el(
          "div",
          {},
          el("strong", {}, n),
          el("p", {}, v.command || v.url || ""),
        ),
        el(
          "div",
          { class: "row" },
          toggle,
          button(
            rdText("编辑"),
            () => {
              name.value = n;
              fill(v);
              const editor=name.closest("details.configuration-editor");if(editor)editor.open=true;
              name.focus();
            },
            "quiet",
          ),
          button(
            rdText("删除"),
            async () => {
              if (confirm(rdFormat("删除用户 MCP「${0}」？项目或托管层配置仍可能生效。",n)))
                await save(n, undefined, true);
            },
            "quiet",
          ),
        ),
      ),
    );
  }
  if (!Object.keys(info.userServers || {}).length)
    list.append(el("p", { class: "empty" }, rdText("当前助手／应用还没有用户层 MCP 配置。")));
  list.append(
    el(
      "div",
      { class: "actions" },
      button(rdText("刷新状态"), renderSettings),
      button(rdText("导出配置 JSON"), () => {
        location.href = "/api/v1" + cp("/mcp/export");
      }),
    ),
  );
  const file = el("input", {
    type: "file",
    accept: ".json,application/json",
    id: "mcp-import-file",
  });
  const importText = el("textarea", {
    rows: 7,
    id: "mcp-import-json",
    placeholder: rdText("选择 RunDesk 导出的 JSON 文件；也可粘贴配置。"),
  });
  const overwrite = el("input", { type: "checkbox", id: "mcp-overwrite" });
  file.onchange = () =>
    safe(async () => {
      const f = file.files[0];
      if (!f) return;
      if (f.size > 512 * 1024) throw Error(rdText("配置文件最多 512 KiB"));
      importText.value = await f.text();
      const bundle = JSON.parse(importText.value);
      toast(
        rdFormat("已载入 ${0} 个服务，确认后点击导入。",Object.keys(bundle.servers || {}).length),
      );
    });
  target.replaceChildren(
    mcpTestPanel(info, cp),
    RunDeskNativePlugins.mcpOrigins(info),
    list,
    el(
      "div",
      { class: "card" },
      el("h3", {}, rdText("添加 / 编辑 MCP")),
      el("label", { for: "mcp-name" }, rdText("服务名称")),
      name,
      el(
        "label",
        { class: "mcp-editor-mode" },
        advanced,
        rdText(" 使用完整 JSON 编辑"),
      ),
      fields,
      jsonPanel,
      el(
        "p",
        { class: "help" },
        info.runtime==="kun"?"已有密钥以 [redacted] 隐藏。MCP 命令以 RunDesk 服务账号在本机执行，不受内置文件工具的工作区边界限制。":rdText("已有密钥以 [redacted] 隐藏，保留它可沿用原值。MCP 命令在运行 Codex 的主机上执行。"),
      ),
      el(
        "div",
        { class: "actions" },
        button(
          rdText("保存配置"),
          () =>
            save(
              name.value.trim(),
              advanced.checked ? JSON.parse(config.value) : formValue(),
            ),
          "primary",
        ),
      ),
    ),
    el(
      "details",
      { class: "card mcp-import" },
      el("summary", {}, rdText("导入 MCP 配置")),
      el(
        "p",
        { class: "help" },
        rdText("合并导入，保留其他服务。env/http_headers 的密钥会隐藏；新环境需要补充。参数和 URL 中的敏感内容仍需自行检查。"),
      ),
      file,
      importText,
      el("label", {}, overwrite, rdText(" 允许覆盖同名服务")),
      button(
        rdText("确认导入"),
        async () => {
          const bundle = JSON.parse(importText.value);
          const r = await api(cp("/mcp/import"), {
            method: "POST",
            body: {
              version: info.version,
              bundle,
              overwrite: overwrite.checked,
            },
          });
          toast(
            r.reloadError
              ? rdText("配置已导入；重载失败：") + r.reloadError
              : rdText("配置已导入"),
          );
          await renderSettings();
        },
        "primary",
      ),
    ),
    el(
      "details",
      { class: "card" },
      el("summary", {}, rdText("连接状态与工具清单")),
      el("pre", {}, json(info.status)),
    ),
    el(
      "details",
      { class: "card" },
      el("summary", {}, rdText("有效配置与来源")),
      el(
        "pre",
        {},
        json({ effective: info.effectiveServers, origins: info.origins }),
      ),
    ),
  );
}

async function renderRuntime(target) {
  const iid=instance().id, wid=ws().id, selection=state.selection, settingsTab=state.settingsTab;
  const cp = configPath(wid, iid);
  const useSnapshot=instance().agentRuntime?.kind!=="kun";
  const configLabel=(zh,en)=>document.documentElement.lang==="en"?en:zh;
  const sameConfigContext=()=>target.isConnected&&selection===state.selection&&settingsTab===state.settingsTab&&iid===instance()?.id&&wid===ws()?.id;
  const result = el("div", { class: "runtime-checks" }),
    raw = el("pre", {id:"runtime-config-value","aria-live":"polite"}, rdText("点击下方按钮读取。")),
    configStatus=el("p",{id:"runtime-config-status",class:"help","aria-live":"polite"});
  let readingConfig=false,hasConfig=false;
  const snapshotStatus=meta=>{
    if(meta?.errorCode==="not_synced")return configLabel("暂无配置快照，点击“立即同步”读取。","No configuration snapshot yet. Select Synchronize now to read it.");
    if(meta?.errorCode==="snapshot_save_failed")return configLabel("已读取当前配置，但本地快照保存失败；请稍后重试。","The current configuration was read, but the local snapshot could not be saved. Please try again later.");
    const date=new Date(meta?.syncedAt),syncedAt=Number.isNaN(date.getTime())?"":date.toLocaleString();
    const time=syncedAt?configLabel("上次同步：","Last synchronized: ")+syncedAt:configLabel("同步时间未知。","Synchronization time is unknown.");
    if(meta?.errorCode)return configLabel("同步失败，显示上次保存的配置。","Synchronization failed; showing the last saved configuration. ")+time;
    if(meta?.stale)return configLabel("本地快照待更新。","The local snapshot needs updating. ")+time;
    return configLabel("本地配置快照 · ","Local configuration snapshot · ")+time;
  };
  async function loadConfig(refresh=false){
    if(readingConfig||!sameConfigContext())return;
    readingConfig=true;readConfig.disabled=true;
    configStatus.className="help";
    configStatus.textContent=useSnapshot?(refresh?configLabel("正在同步 Codex 配置…","Synchronizing Codex configuration…"):configLabel("正在读取本地配置快照…","Reading the local configuration snapshot…")):"";
    if(!hasConfig)raw.textContent=useSnapshot&&!refresh?configLabel("正在读取本地配置快照…","Reading the local configuration snapshot…"):rdText("正在读取 Codex 配置…");
    try{
      const value=await api(cp("/config")+(useSnapshot?"&view=snapshot"+(refresh?"&refresh=true":""):""));
      if(sameConfigContext()){
        const {snapshot,...config}=value;
        hasConfig=!useSnapshot||snapshot?.errorCode!=="not_synced";
        raw.textContent=hasConfig?json(useSnapshot?config:value):"";
        configStatus.textContent=useSnapshot?snapshotStatus(snapshot):"";
        configStatus.className="help"+(snapshot?.errorCode&&snapshot.errorCode!=="not_synced"?" error":"");
      }
    }catch(error){
      if(sameConfigContext()){
        const message=useSnapshot?(hasConfig?configLabel("同步失败，保留上次显示的配置。可点击“立即同步”重试：","Synchronization failed; keeping the previous configuration. Select Synchronize now to retry: "):configLabel("读取失败，可点击“立即同步”重试：","Could not read configuration. Select Synchronize now to retry: ")):configLabel("读取失败，可点击“读取配置”重试：","Could not read configuration. Select Read configuration to retry: ");
        configStatus.textContent=message+error.message;configStatus.className="help error";
        if(!hasConfig)raw.textContent="";
      }
    }finally{readingConfig=false;readConfig.disabled=false;}
  }
  const readConfig=button(useSnapshot?configLabel("立即同步","Synchronize now"):rdText("读取配置"),()=>loadConfig(true));
  readConfig.id="runtime-config-read";
  const check = button(
    rdText("运行连接诊断"),
    async () => {
      check.disabled = true;
      result.replaceChildren(
        el("p", { class: "loading" }, rdText("正在检查可执行文件、工作区和原生接口…")),
      );
      try {
        const r = await api(cp("/diagnostics"));
        result.replaceChildren(
          ...r.checks.map((c) =>
            el(
              "div",
              { class: "check" },
              el(
                "div",
                { class: "row" },
                el("strong", {}, c.name),
                el(
                  "span",
                  {
                    class:
                      c.status === "ok"
                        ? "check-ok"
                        : c.status === "error"
                          ? "check-error"
                          : "muted",
                  },
                  {
                    ok: rdText("通过"),
                    error: rdText("失败"),
                    warning: rdText("警告"),
                    skipped: rdText("未执行"),
                  }[c.status] || c.status,
                ),
              ),
              el("p", {}, c.detail),
              c.hint ? el("p", { class: "help" }, c.hint) : null,
              c.command
                ? el(
                    "details",
                    {},
                    el("summary", {}, rdText("命令与原始输出")),
                    el(
                      "pre",
                      {},
                      json({
                        command: c.command,
                        exitCode: c.exitCode,
                        output: c.output || "",
                      }),
                    ),
                  )
                : null,
              el("small", { class: "muted" }, c.durationMs + " ms"),
            ),
          ),
          el("p", { class: "help" }, r.note),
          await instanceRuntimeCard(instance().id),
        );
      } finally {
        check.disabled = false;
      }
    },
    "primary",
  );
  target.replaceChildren(
    el(
      "div",
      { class: "card" },
      el("h3", {}, rdText("连接诊断")),
      el(
        "p",
        {},
        rdText("检查工作区读写、Codex、沙箱实际执行及 App Server 接口，不调用模型。"),
      ),
      check,
      result,
    ),
    el(
      "div",
      { class: "card" },
      el("h3", {}, rdText("运行约定")),
      el("p", {}, rdText("工作区：") + ws().path),
      el(
        "p",
        {},
        rdText("任务遵循当前助手或应用的沙箱与审批配置。浏览器关闭后任务继续；后台退出则中断当前运行。"),
      ),
      button(rdText("读取可用模型"), async () => {
        const r = await api(cp("/models"));
        const models = r.data || [];
        $("#models").replaceChildren(
          ...models.map((m) =>
            el(
              "option",
              { value: m.model || m.id },
              m.displayName || m.model || m.id,
            ),
          ),
        );
        toast(rdFormat("已载入 ${0} 个模型；新建会话前选择模型。",models.length));
      }),
      el(
        "p",
        { class: "help" },
        rdText("模型与认证页可查看登录状态和登录命令。模型在创建会话时确定。"),
      ),
    ),
    el(
      "details",
      { class: "card" },
      el("summary", {}, rdText("Codex 有效配置")),
      el("p",{class:"help"},useSnapshot?configLabel("打开此页只读取 RunDesk 保存的脱敏快照；后台每 5 分钟尝试同步已连接的 Codex。点击“立即同步”会连接 Codex，可能需要准备 Docker 环境。","Opening this page only reads the sanitized snapshot saved by RunDesk. Background synchronization runs every 5 minutes for connected Codex instances. Synchronize now connects to Codex and may need to prepare a Docker environment."):configLabel("按需读取当前配置。","Read the current configuration when needed.")),
      readConfig,
      configStatus,
      raw,
    ),
  );
  if(useSnapshot)void loadConfig();
}

$("#session-search").oninput = renderSessions;
$("#session-scope").onchange = renderSessions;
$("#rename-session").onclick = () =>
  safe(async () => {
    if (!state.session) return;
    const title = prompt(rdText("会话名称"), state.session.title);
    if (title !== null) await patchCurrent({ title });
  });
$("#pin-session").onclick = () =>
  safe(() => patchCurrent({ pinned: !state.session?.pinned }));
$("#archive-session").onclick = () =>
  safe(() => patchCurrent({ archived: !state.session?.archived }));
$("#delete-session").onclick = () =>
  safe(async () => {
    if (
      !state.session ||
      !confirm(
        rdText("删除此会话及 RunDesk 事件记录？原生 Codex 线程、上传文件和产物会保留。"),
      )
    )
      return;
    const id = state.session.id;
    await api("/sessions/" + id, { method: "DELETE" });
    if (state.session?.id === id) {
      resetConversation();
      localStorage.removeItem("rundesk-session");
    }
    await refreshSessions();
    await selectFirst();
  });
$("#export-markdown").onclick = () => {
  if (state.session)
    location.href =
      "/api/v1/sessions/" + state.session.id + "/export?format=markdown";
  $("#session-menu").open = false;
};
$("#event-search").onclick = () => safe(() => searchEvents());
$("#event-query").onkeydown = (e) => {
  if (e.key === "Enter") {
    e.preventDefault();
    safe(() => searchEvents());
  }
};
$("#event-live").onclick = () => {
  state.historyRequest++;
  state.history = null;
  state.historyQuery = null;
  state.debugPaused = false;
  $("#event-search").disabled = false;
  $("#event-pause").textContent = rdText("暂停滚动");
  renderDebug();
};
$("#event-pause").onclick = () => {
  state.debugPaused = !state.debugPaused;
  $("#event-pause").textContent = state.debugPaused ? rdText("恢复滚动") : rdText("暂停滚动");
  renderDebug();
};
wireSuggestions();
document.addEventListener("DOMContentLoaded", () => safe(boot));

function renderInstanceOptions(selected) {
  $("#instance").replaceChildren(
    ...state.instances.map((i) => el("option", { value: i.id }, i.name)),
  );
  $("#instance").value = state.instances.some((i) => i.id === selected)
    ? selected
    : "default";
}
