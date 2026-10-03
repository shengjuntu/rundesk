// Session management, full-history diagnostics and configuration controls.
function renderSessions() {
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
          ? "没有匹配的会话"
          : archived
            ? "没有已归档会话"
            : "从一个新对话开始",
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
  $("#session-title").textContent = s?.title || "新对话";
  $("#run-status").textContent = s?.archived
    ? "已归档"
    : statusLabel[s?.status] || s?.status || "就绪";
  $("#stop").classList.toggle("hidden", !active(s?.status));
  const transitioning = ["starting", "stopping"].includes(s?.status);
  const steering = active(s?.status) && !transitioning;
  $("#send").disabled =
    state.loadingSession || state.sending || transitioning || (steering && !s?.turnId) || !!s?.archived;
  $("#send").title = steering ? "补充指令到当前任务" : "发送";
  $("#send").setAttribute("aria-label", $("#send").title);
  $("#send").classList.toggle("steering", steering);
  $("#send").textContent = steering ? "补充" : "↑";
  $("#stop").disabled = s?.status === "stopping";
  $("#prompt").disabled = state.loadingSession || !!s?.archived;
  $("#prompt").placeholder = s?.archived
    ? "从会话菜单恢复后，可继续对话。"
    : steering
      ? "补充要求或调整当前任务的方向…"
      : "给 Codex 一个任务…";
  $("#model").disabled = !!s;
  $("#session-menu").classList.toggle("hidden", !s);
  $("#pin-session").textContent = s?.pinned ? "取消置顶" : "置顶";
  $("#archive-session").textContent = s?.archived ? "恢复会话" : "归档";
  $("#archive-session").disabled = active(s?.status);
  $("#delete-session").disabled = active(s?.status);
  window.RunDeskTraceUI?.syncComposer();
  renderAnalysisBanner();
}
function renderAnalysisBanner() {
  const host=$("#analysis-banner"),origin=state.session?.traceOrigin;
  host.classList.toggle("hidden",!origin);
  if(!origin){host.replaceChildren();host._signature="";return;}
  const signature=json(origin);if(host._signature===signature)return;host._signature=signature;
  host.replaceChildren(el("strong",{},"过程分析 · 独立会话"),el("span",{},`来源：${origin.title} · 截至 ${new Date(origin.capturedAt).toLocaleString()} 的记录`),button("查看原任务过程",()=>RunDeskTraceUI.openOrigin(origin),"quiet"),el("span",{},"只读轨迹工具仅用于此分析会话；沿用原配置的模型与认证。原任务单独运行。"));
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
    toast("请先选择一个会话");
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
    $("#event-pause").textContent = "暂停滚动";
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
      ? `已查询 ${state.history.length} 条`
      : `已载入 ${state.events.length} 条`;
  if (state.tab === "events") {
    $("#event-mode").textContent =
      state.history !== null
        ? "历史查询 · 按时间顺序 · 不随新事件变化"
        : state.debugPaused
          ? "已暂停显示；后台继续接收事件"
          : "实时 · 显示最近 250 条";
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
          "复制事件 JSON",
          async () => {
            await navigator.clipboard.writeText(json(ev));
            toast("事件已复制");
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
            ? "没有匹配的历史事件。"
            : "发送一条消息，查看运行事件。",
        ),
      );
    if (state.history !== null && state.historyMore)
      box.append(
        button("加载更多历史事件", () => searchEvents(true), "query-more"),
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
        ).toFixed(1) + " 秒"
      : "尚未运行";
    box.replaceChildren(
      el("h3", {}, "本次运行"),
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
      el("h3", {}, "输入与项目笔记"),
      el("pre", {}, input ? json(input.data) : "尚未运行"),
      el("h3", {}, "实际提交的 turn/start"),
      el("pre", {}, submitted ? json(submitted.data.params) : "尚未提交"),
      el("h3", {}, "原生 Token 用量"),
      el(
        "pre",
        {},
        tokens ? json(tokens.data.params) : "接口尚未提供 Token 用量",
      ),
      el(
        "p",
        { class: "help" },
        "total 是累计用量，last 是最近一轮的用量；均不代表完整上下文快照。耗时包含启动和等待审批。",
      ),
      el("h3", {}, `原生上下文压缩 · ${compactions.length} 次`),
      ...compactions.map((e) => el("pre", {}, json(e.data.params.item))),
    );
  } else {
    box.replaceChildren(
      button("刷新文件", loadFiles, "quiet"),
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
              button("预览", () => previewFile(f), "quiet"),
              el("a", { href: fileURL(f.path) }, "下载 ↓"),
            ),
          ),
        ),
      ),
    );
    if (!state.files.length)
      box.append(el("p", { class: "empty" }, "产物目录中还没有文件。"));
  }
}

async function renderSkills(target) {
  const current = instance(),
    work = ws(),
    cp = configPath(work.id, current.id);
  const raw = await api(cp("/skills"));
  if (state.settingsTab !== "skills") return;
  state.skills = (raw.data || []).flatMap((d) => d.skills || []);
  const errors = (raw.data || []).flatMap((d) => d.errors || []);
  const scope = el(
    "select",
    { id: "skill-scope" },
    el("option", { value: "instance" }, "当前助手／应用"),
    el("option", { value: "project" }, "项目技能 · 共享目录"),
  );
  const name = el("input", {
    type: "text",
    id: "skill-name",
    placeholder: "例如 project-guide",
  });
  const text = el(
    "textarea",
    { rows: 12, id: "skill-content" },
    "---\nname: project-guide\ndescription: 本项目的工作约定\n---\n\n在修改代码前，先阅读 README，并说明验证方式。\n",
  );
  const list = el(
    "div",
    { class: "card" },
    el("h3", {}, "Codex 发现的 Skills"),
    el(
      "p",
      {},
      "专用技能随助手或应用使用；项目技能在共享项目中可见。",
    ),
  );
  for (const sk of state.skills) {
    const toggle = el("input", {
      type: "checkbox",
      checked: sk.enabled !== false,
      "aria-label": "启用 " + sk.name,
    });
    toggle.onchange = () =>
      safe(async () => {
        toggle.disabled = true;
        try {
          await api(cp("/skills/toggle"), {
            method: "POST",
            body: { path: sk.path, enabled: toggle.checked },
          });
          toast("配置已保存；下次运行读取");
          await renderSettings();
        } catch (e) {
          toggle.checked = !toggle.checked;
          throw e;
        } finally {
          toggle.disabled = false;
        }
      });
    const use = button(
      "用于下次消息",
      () => {
        if (!state.chosenSkills.some((s) => s.path === sk.path))
          state.chosenSkills.push({ name: sk.name, path: sk.path });
        renderAttachments();
        closeSettings();
      },
      "quiet",
    );
    use.disabled = sk.enabled === false || !skillCanBeSelected();
    if(!skillCanBeSelected())use.textContent="在任务中选用";
    use.classList.add("skill-use");
    const ops = el("div", { class: "skill-actions" }, use, el("label",{class:"skill-enable"},toggle,"启用"));
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
      skScope !== "external" &&
      /^[a-zA-Z0-9_-]{1,64}\/SKILL\.md$/.test(relative);
    if (managed) {
      const slug = sk.path.replaceAll("\\", "/").split("/").at(-2);
      ops.prepend(
        button(
          "编辑",
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
      ops.append(button("目录",()=>openSkillDirectory(work.id,current.id,slug,skScope),"quiet"),button("导出 ZIP",()=>downloadSkillBundle(work.id,current.id,slug,skScope),"quiet"),button("移除",async()=>{
        if(!confirm(`移除技能「${sk.name}」的完整目录？所有文件都会备份。`))return;
        const result=await api(cp("/skill-bundles/"+encodeURIComponent(slug),skScope),{method:"DELETE"});
        state.chosenSkills=state.chosenSkills.filter(s=>s.path!==sk.path);renderAttachments();toast("完整目录已备份："+result.backupPath);await renderSettings();
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
          sk.description?.length>220?el("details",{class:"skill-description"},el("summary",{},sk.description.slice(0,160)+"…"),el("p",{},sk.description)):el("p", {}, sk.description || ""),
          el(
            "span",
            { class: "badge" },
            managed
              ? skScope === "instance"
                ? "当前助手／应用"
                : "项目技能 · 共享"
              : sk.scope === "system"
                ? "Codex 内置"
                : "其他来源",
          ),
          el("details",{class:"skill-path"},el("summary",{},"查看文件位置"),el("small",{},sk.path)),
        ),
        ops,
      ),
    );
  }
  if (!state.skills.length)
    list.append(
      el("p", { class: "empty" }, "还没有可用技能。导入完整目录，或展开下方编辑器创建简单技能。"),
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
      if (f.size > 256 * 1024) throw Error("SKILL.md 最多 256 KiB");
      text.value = await f.text();
      const match = text.value.match(/^name:\s*["']?([\w-]+)/m);
      if (match) name.value = match[1];
      toast("已载入编辑器，确认后点击保存。");
    });
  target.replaceChildren(
    skillBundleImporter(work.id,current.id),
    list,
    ...errors.map((e) => el("pre", { class: "error" }, json(e))),
    el(
      "div",
      { class: "card" },
      el("h3", {}, "编辑 SKILL.md / 创建简单技能"),
      el(
        "p",
        {},
        "这里只编辑 SKILL.md；已有脚本、参考资料和模板会保留。完整技能请使用上方目录导入。",
      ),
      el("label", { for: "skill-scope" }, "保存范围"),
      scope,
      el("label", { for: "skill-name" }, "目录名称"),
      name,
      el("label", { for: "skill-content" }, "SKILL.md"),
      text,
      file,
      el(
        "div",
        { class: "actions" },
        button(
          "保存并重新扫描",
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
            toast("Skill 已保存");
            await renderSettings();
          },
          "primary",
        ),
        button("导入 SKILL.md", () => file.click()),
        button("导出编辑内容", () =>
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
  const current = instance(),
    cp = configPath(ws().id, current.id);
  const info = await api(cp("/mcp"));
  if (state.settingsTab !== "mcp") return;
  state.mcp = info;
  const name = el("input", {
    type: "text",
    id: "mcp-name",
    placeholder: "例如 filesystem",
  });
  const type = el(
    "select",
    { id: "mcp-type" },
    el("option", { value: "stdio" }, "本地命令 · stdio"),
    el("option", { value: "http" }, "远程服务 · HTTP"),
  );
  const command = el("input", {
    type: "text",
    id: "mcp-command",
    placeholder: "例如 npx / uvx / 可执行文件路径",
  });
  const args = el("textarea", {
    rows: 3,
    id: "mcp-args",
    placeholder: "每行一个参数，不经过 shell 拆分",
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
    placeholder: "例如 MCP_API_TOKEN（环境变量名）",
  });
  const enabled = el("input", {
    type: "checkbox",
    checked: true,
    id: "mcp-enabled",
  });
  const advanced = el("input", { type: "checkbox", id: "mcp-advanced" });
  const config = el(
    "textarea",
    { rows: 11, id: "mcp-json" },
    json({ command: "npx", args: [], enabled: true }),
  );
  const local = el(
    "div",
    {},
    el("label", { for: "mcp-command" }, "启动命令"),
    command,
    el("label", { for: "mcp-args" }, "参数（每行一个）"),
    args,
    el("label", { for: "mcp-env" }, "环境变量 JSON"),
    env,
  );
  const remote = el(
    "div",
    { class: "hidden" },
    el("label", { for: "mcp-url" }, "MCP 地址"),
    url,
    el("label", { for: "mcp-token-env" }, "Bearer Token 的环境变量名"),
    tokenEnv,
  );
  const fields = el(
    "div",
    { class: "mcp-fields" },
    el("label", { for: "mcp-type" }, "连接类型"),
    type,
    local,
    remote,
    el("label", {}, enabled, " 启用服务"),
  );
  const jsonPanel = el(
    "div",
    { class: "hidden" },
    el("label", { for: "mcp-json" }, "完整配置 JSON"),
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
    config.value = json(value);
    switchType();
  }
  function switchType() {
    local.classList.toggle("hidden", type.value !== "stdio");
    remote.classList.toggle("hidden", type.value !== "http");
  }
  function formValue() {
    const v = { ...original, enabled: enabled.checked };
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
    el("h3", {}, "MCP 工具配置"),
    el("p", {class:"help"}, `正在编辑 ${contextTitle()}。保存用于这个助手或应用的后续任务，项目层配置可能覆盖这里的值。`),

  );
  const save = async (n, value, remove = false) => {
    const r = await api(cp("/mcp/" + encodeURIComponent(n)), {
      method: "PUT",
      body: { version: info.version, config: value, remove },
    });
    toast(
      r.reloadError
        ? "已保存；重载失败：" + r.reloadError
        : "已保存；下一轮任务会重载 MCP",
    );
    await renderSettings();
  };
  for (const [n, v] of Object.entries(info.userServers || {})) {
    const toggle = el("input", {
      type: "checkbox",
      checked: v.enabled !== false,
      "aria-label": "启用 MCP " + n,
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
            "编辑",
            () => {
              name.value = n;
              fill(v);
              const editor=name.closest("details.configuration-editor");if(editor)editor.open=true;
              name.focus();
            },
            "quiet",
          ),
          button(
            "删除",
            async () => {
              if (confirm(`删除用户 MCP「${n}」？项目或托管层配置仍可能生效。`))
                await save(n, undefined, true);
            },
            "quiet",
          ),
        ),
      ),
    );
  }
  if (!Object.keys(info.userServers || {}).length)
    list.append(el("p", { class: "empty" }, "当前助手／应用还没有用户层 MCP 配置。"));
  list.append(
    el(
      "div",
      { class: "actions" },
      button("刷新状态", renderSettings),
      button("导出配置 JSON", () => {
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
    placeholder: "选择 RunDesk 导出的 JSON 文件；也可粘贴配置。",
  });
  const overwrite = el("input", { type: "checkbox", id: "mcp-overwrite" });
  file.onchange = () =>
    safe(async () => {
      const f = file.files[0];
      if (!f) return;
      if (f.size > 512 * 1024) throw Error("配置文件最多 512 KiB");
      importText.value = await f.text();
      const bundle = JSON.parse(importText.value);
      toast(
        `已载入 ${Object.keys(bundle.servers || {}).length} 个服务，确认后点击导入。`,
      );
    });
  target.replaceChildren(
    list,
    el(
      "div",
      { class: "card" },
      el("h3", {}, "添加 / 编辑 MCP"),
      el("label", { for: "mcp-name" }, "服务名称"),
      name,
      el(
        "label",
        { class: "mcp-editor-mode" },
        advanced,
        " 使用完整 JSON 编辑",
      ),
      fields,
      jsonPanel,
      el(
        "p",
        { class: "help" },
        "已有密钥以 [redacted] 隐藏，保留它可沿用原值。MCP 命令在运行 Codex 的主机上执行。",
      ),
      el(
        "div",
        { class: "actions" },
        button(
          "保存配置",
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
      el("summary", {}, "导入 MCP 配置"),
      el(
        "p",
        { class: "help" },
        "合并导入，保留其他服务。env/http_headers 的密钥会隐藏；新环境需要补充。参数和 URL 中的敏感内容仍需自行检查。",
      ),
      file,
      importText,
      el("label", {}, overwrite, " 允许覆盖同名服务"),
      button(
        "确认导入",
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
              ? "配置已导入；重载失败：" + r.reloadError
              : "配置已导入",
          );
          await renderSettings();
        },
        "primary",
      ),
    ),
    el(
      "details",
      { class: "card" },
      el("summary", {}, "连接状态与工具清单"),
      el("pre", {}, json(info.status)),
    ),
    el(
      "details",
      { class: "card" },
      el("summary", {}, "有效配置与来源"),
      el(
        "pre",
        {},
        json({ effective: info.effectiveServers, origins: info.origins }),
      ),
    ),
  );
}

async function renderRuntime(target) {
  const cp = configPath(ws().id, instance().id);
  const result = el("div", { class: "runtime-checks" }),
    raw = el("pre", {}, "点击下方按钮读取。");
  const check = button(
    "运行连接诊断",
    async () => {
      check.disabled = true;
      result.replaceChildren(
        el("p", { class: "loading" }, "正在检查可执行文件、工作区和原生接口…"),
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
                    ok: "通过",
                    error: "失败",
                    warning: "警告",
                    skipped: "未执行",
                  }[c.status] || c.status,
                ),
              ),
              el("p", {}, c.detail),
              c.hint ? el("p", { class: "help" }, c.hint) : null,
              c.command
                ? el(
                    "details",
                    {},
                    el("summary", {}, "命令与原始输出"),
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
      el("h3", {}, "连接诊断"),
      el(
        "p",
        {},
        "检查工作区读写、Codex、沙箱实际执行及 App Server 接口，不调用模型。",
      ),
      check,
      result,
    ),
    el(
      "div",
      { class: "card" },
      el("h3", {}, "运行约定"),
      el("p", {}, "工作区：" + ws().path),
      el(
        "p",
        {},
        "单用户后台；workspace-write 沙箱、on-request 审批。浏览器关闭后任务继续；后台退出则中断当前运行。",
      ),
      button("读取可用模型", async () => {
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
        toast(`已载入 ${models.length} 个模型；新建会话前选择模型。`);
      }),
      el(
        "p",
        { class: "help" },
        "模型与认证页可查看登录状态和登录命令。模型在创建会话时确定。",
      ),
    ),
    el(
      "details",
      { class: "card" },
      el("summary", {}, "Codex 有效配置"),
      button("读取配置", async () => {
        raw.textContent = json(await api(cp("/config")));
      }),
      raw,
    ),
  );
}

$("#session-search").oninput = renderSessions;
$("#session-scope").onchange = renderSessions;
$("#rename-session").onclick = () =>
  safe(async () => {
    if (!state.session) return;
    const title = prompt("会话名称", state.session.title);
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
        "删除此会话及 RunDesk 事件记录？原生 Codex 线程、上传文件和产物会保留。",
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
  $("#event-pause").textContent = "暂停滚动";
  renderDebug();
};
$("#event-pause").onclick = () => {
  state.debugPaused = !state.debugPaused;
  $("#event-pause").textContent = state.debugPaused ? "恢复滚动" : "暂停滚动";
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
async function switchInstance() {
  resetConversation();
  state.skills = [];
  $("#models").replaceChildren();
  updateWorkspaceLabel();
  renderSessions();
  if(instance().id==="default")await enterAssistant();
  else if(currentApplication())await showApplication(currentApplication().appId);
  else await showLegacyApplication(instance().id);
}
$("#instance").onchange = () => safe(switchInstance);
$("#manage-instances").onclick = () => safe(() => openSettings("overview"));

async function renderInstances(target) {
  const current = instance(),
    cp = configPath(ws().id, current.id);
  const select = el(
    "select",
    { id: "settings-instance" },
    ...state.instances.map((i) => el("option", { value: i.id }, i.name)),
  );
  select.value = current.id;
  select.onchange = () =>
    safe(async () => {
      $("#instance").value = select.value;
      await switchInstance();
      await renderSettings();
    });
  const name = el("input", {
    id: "instance-name",
    type: "text",
    value: current.name,
    maxlength: 160,
  });
  const description = el(
    "textarea",
    { id: "instance-description", rows: 2 },
    current.description,
  );
  const model = el("input", {
    id: "instance-model",
    type: "text",
    value: current.defaultModel,
    list: "models",
    placeholder: "留空使用 Codex 配置",
  });
  const permissions = permissionEditor(current);
  const newName = el("input", {
    id: "new-instance-name",
    type: "text",
    placeholder: "例如：视觉助手",
    maxlength: 160,
  });
  const account = el(
    "pre",
    { id: "instance-account" },
    "点击读取当前实例的登录状态。",
  );
  const shellQuote = (value) => "'" + value.replaceAll("'", "'\\''") + "'";
  const psQuote = (value) => "'" + value.replaceAll("'", "''") + "'";
  target.replaceChildren(
    el(
      "div",
      { class: "card" },
      el("label", { for: "settings-instance" }, "当前实例"),
      select,
      el("h3", {}, current.name),
      el(
        "p",
        {},
        current.managed
          ? "独立 Codex 配置目录；此实例可以在多个工作区创建会话。"
          : "默认实例沿用启动 RunDesk 时的 CODEX_HOME 和原有会话。",
      ),
      el("label", { for: "instance-name" }, "实例名称"),
      name,
      el("label", { for: "instance-description" }, "用途说明"),
      description,
      el("label", { for: "instance-model" }, "新会话默认模型"),
      model,
      button("读取可用模型", async()=>{const raw=await api(cp("/models"));$("#models").replaceChildren(...(raw.data||[]).map(m=>el("option",{value:m.id||m.model},m.displayName||m.id||m.model)));toast("已读取模型，选择后保存实例");}),
      permissions.node,
      button(
        "保存实例",
        async () => {
          await api("/instances/" + current.id, {
            method: "PATCH",
            body: {
              name: name.value,
              description: description.value,
              defaultModel: model.value,
              permissions: permissions.value(),
              revision: current.revision,
            },
          });
          state.instances = await api("/instances");
          renderInstanceOptions(current.id);
          updateWorkspaceLabel();
          toast("实例已保存；权限将在下一轮生效，默认模型用于新会话");
          await renderSettings();
        },
        "primary",
      ),
      el("p", { class: "help" }, "配置目录：" + current.codexHome),
      el(
        "p",
        { class: "help" },
        "会话创建后固定绑定实例与工作区。实例共用本机操作系统用户；全局技能、项目文件和服务环境变量仍可能共享。",
      ),
    ),
    el(
      "div",
      { class: "card" },
      el("h3", {}, "登录与重新加载"),
      el(
        "p",
        {},
        "新实例需要配置凭据。请以运行后台的系统用户，在终端执行对应命令；登录完成后重新加载空闲连接。",
      ),
      el(
        "details",
        {},
        el("summary", {}, "Ubuntu / macOS 登录命令"),
        el(
          "pre",
          {},
          "env CODEX_HOME=" + shellQuote(current.codexHome) + " codex login",
        ),
      ),
      el(
        "details",
        {},
        el("summary", {}, "Windows PowerShell 登录命令"),
        el(
          "pre",
          {},
          "$env:CODEX_HOME = " + psQuote(current.codexHome) + "\ncodex login",
        ),
      ),
      el(
        "p",
        { class: "help" },
        "如后台使用 --codex 指定可执行文件，请将命令中的 codex 替换为该路径。凭据存储由 Codex 管理；使用系统密钥环时不保证账号隔离。",
      ),
      el(
        "div",
        { class: "actions" },
        button("读取登录状态", async () => {
          account.textContent = json(await api(cp("/account")));
        }),
        button("重新加载空闲连接", async () => {
          const r = await api("/instances/" + current.id + "/reload", {
            method: "POST",
          });
          toast(
            `已关闭 ${r.closedConnections} 个空闲连接，保留 ${r.busyConnections} 个忙碌连接`,
          );
          account.textContent = json(await api(cp("/account")));
        }),
      ),
      account,
    ),
    el(
      "div",
      { class: "card" },
      el("h3", {}, "新建实例"),
      el("p", {}, "创建独立配置目录，然后在 Skills 和 MCP 页配置这个助手。"),
      el("label", { for: "new-instance-name" }, "名称"),
      newName,
      button(
        "创建并切换",
        async () => {
          const created = await api("/instances", {
            method: "POST",
            body: { name: newName.value },
          });
          state.instances = await api("/instances");
          renderInstanceOptions(created.id);
          await switchInstance();
          await renderSettings();
          toast("已创建实例：" + created.name);
        },
        "primary",
      ),
    ),
  );
}
