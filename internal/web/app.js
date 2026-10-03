"use strict";
const $ = (s) => document.querySelector(s);
const $$ = (s) => [...document.querySelectorAll(s)];
const welcome = $("#messages").firstElementChild.cloneNode(true);
const state = {
  applications: [], view: "conversation", drafts: new Map(), taskAppID: "", draftContext: null,
  workspaces: [],
  instances: [],
  sessions: [],
  session: null,
  sending: false,
  lastSteerRequest: null,
  events: [],
  cursor: 0,
  stream: null,
  files: [],
  skills: [],
  chosenSkills: [],
  uploads: [],
  tab: "events",
  settingsTab: "notes",
  mcp: null,
  approvalSignature: null,
  selection: 0,
  rendering: false,
  demo: false,
  history: null,
  historyQuery: null,
  historyRequest: 0,
  historyMore: false,
  debugPaused: false,
};
const active = (s) =>
  ["starting", "running", "waiting", "stopping"].includes(s);
const statusLabel = {
  idle: "就绪",
  starting: "正在启动",
  running: "正在运行",
  waiting: "等待确认",
  stopping: "正在停止",
  completed: "已完成",
  interrupted: "已停止",
  failed: "运行失败",
};
let toastTimer;
function toast(text) {
  $("#toast").textContent = text;
  $("#toast").classList.remove("hidden");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => $("#toast").classList.add("hidden"), 6500);
}
function el(tag, attrs = {}, ...children) {
  const n = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") n.className = v;
    else if (k.startsWith("on")) n.addEventListener(k.slice(2), v);
    else if (k === "text") n.textContent = v;
    else if (v !== false && v != null) n.setAttribute(k, v === true ? "" : v);
  }
  for (const c of children.flat()) {
    if (c != null)
      n.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return n;
}
function button(text, fn, cls = "") {
  return el(
    "button",
    { type: "button", class: cls, onclick: () => safe(fn) },
    text,
  );
}
async function safe(fn) {
  try {
    return await fn();
  } catch (e) {
    toast(e.message);
    console.error(e);
  }
}
const pendingAPI = new Map();
try { for(const item of JSON.parse(sessionStorage.getItem("rundesk-pending-api")||"[]")) if(Array.isArray(item)&&item.length===2) pendingAPI.set(...item); } catch {}
async function submissionSignature(path, body) {
  const text=path+"\n"+JSON.stringify(body);
  if(!crypto.subtle) return "memory:"+text;
  const hash=await crypto.subtle.digest("SHA-256",new TextEncoder().encode(text));
  return Array.from(new Uint8Array(hash),b=>b.toString(16).padStart(2,"0")).join("");
}
function savePendingAPI() {
  try {sessionStorage.setItem("rundesk-pending-api",JSON.stringify([...pendingAPI].filter(([k])=>!k.startsWith("memory:"))));} catch {}
}
async function hasPendingSubmission(path,body) {return pendingAPI.has(await submissionSignature(path,body));}
async function api(path, options = {}) {
  const headers = { ...options.headers };
  const method=options.method||"GET", body=options.body;
  const deduplicate=method==="POST" && (/^\/(instances|workspaces|sessions)$/.test(path)||/^\/sessions\/[^/]+\/turns$/.test(path));
  let signature;
  if(deduplicate) {
    signature=await submissionSignature(path,body);
    if(!pendingAPI.has(signature)) pendingAPI.set(signature,Array.from(crypto.getRandomValues(new Uint8Array(16)),b=>b.toString(16).padStart(2,"0")).join(""));
    headers["Idempotency-Key"]=pendingAPI.get(signature);savePendingAPI();
  }
  if (body && !(body instanceof FormData)) {
    headers["Content-Type"] = "application/json";
    options={...options,body:JSON.stringify(body)};
  }
  const r = await fetch("/api/v1" + path, { ...options, headers });
  let data;
  try {data = await r.json();} catch {throw Error(`HTTP ${r.status}`);}
  if(signature && (r.ok || r.status<500 && !["request_in_progress","request_unconfirmed"].includes(data.code))) {pendingAPI.delete(signature);savePendingAPI();}
  if (!r.ok) {
    if (r.status === 401) $("#login").classList.remove("hidden");
    const error=Error(data.error || `HTTP ${r.status}`);error.code=data.code;error.requestId=data.requestId;throw error;
  }
  return data;
}
const ws = () => state.workspaces.find((w) => w.id === $("#workspace").value);
const instance = () =>
  state.instances.find((i) => i.id === $("#instance").value);
const configPath = (wid, iid) => (suffix, scope) =>
  "/workspaces/" +
  encodeURIComponent(wid) +
  suffix +
  "?" +
  new URLSearchParams({ instanceId: iid, ...(scope ? { scope } : {}) });
const wpath = (suffix) => "/workspaces/" + ws().id + suffix;
const json = (x) => JSON.stringify(x, null, 2);
function setLoading(target) {
  target.replaceChildren(
    el("div", { class: "loading" }, "正在读取 Codex 配置…"),
  );
}
function prettySize(n) {
  return n > 1048576
    ? (n / 1048576).toFixed(1) + " MB"
    : n > 1024
      ? (n / 1024).toFixed(1) + " KB"
      : n + " B";
}
async function boot() {
  const meta = await api("/meta");
  state.demo = meta.demo;
  $("#toast").classList.add("hidden");
  $("#demo-banner").classList.toggle("hidden", !meta.demo);
  $("#mode").textContent = meta.demo ? "DEMO" : "CODEX";
  $("#login").classList.add("hidden");
  state.instances = await api("/instances");
  renderInstanceOptions("default");
  state.applications = await api("/applications");
  state.workspaces = await api("/workspaces");
  const last =
    localStorage.getItem("rundesk-assistant-workspace") ||
    localStorage.getItem("rundesk-workspace") ||
    localStorage.getItem("codex-base-workspace");
  $("#workspace").replaceChildren(
    ...state.workspaces.map((w) => el("option", { value: w.id }, w.name)),
  );
  if (state.workspaces.some((w) => w.id === last)) $("#workspace").value = last;
  updateWorkspaceLabel();
  await refreshSessions();
  if (await restoreProductRoute()) return;
  setProductPage("conversation");
  const sid =
    localStorage.getItem("rundesk-session") ||
    localStorage.getItem("codex-base-session");
  const session = state.sessions.find(
    (s) =>
      s.id === sid &&
      s.workspaceId === ws().id &&
      s.instanceId === instance()?.id && s.source?.kind !== "application",
  );
  if (session) await selectSession(session.id);
  else await selectFirst();
}
function updateWorkspaceLabel() {
  const w = ws();
  $("#workspace-name").textContent = w?.name || "默认项目";
  updateProductNavigation();
  localStorage.setItem("rundesk-instance", instance()?.id || "default");
  if (!state.session) $("#model").value = instance()?.defaultModel || "";
  localStorage.setItem("rundesk-workspace", w?.id || "");
  if(instance()?.id==="default"&&!state.taskAppID)localStorage.setItem("rundesk-assistant-workspace",w?.id||"");
}
async function refreshSessions() {
  state.sessions = await api("/sessions");
  renderSessions();
  if (state.session) {
    const updated = state.sessions.find((s) => s.id === state.session.id);
    if (updated) {
      state.session = updated;
      renderStatus();
    }
  }
}
function wireSuggestions() {
  $$("[data-prompt]").forEach(
    (b) =>
      (b.onclick = () => {
        $("#prompt").value = b.dataset.prompt;
        $("#prompt").focus();
      }),
  );
}
function resetConversation() {
  state.loadingSession=false;
  stashProductDraft(); state.draftContext=null; $("#prompt").value=""; state.taskAppID="";
  resetReplyActions();
  state.historyRequest++;
  $("#send-feedback").textContent = "";
  state.history = null;
  state.historyQuery = null;
  state.debugPaused = false;
  $("#event-search").disabled = false;
  $("#event-pause").textContent = "暂停滚动";
  state.selection++;
  state.approvalSignature = null;
  if (state.stream) state.stream.close();
  state.stream = null;
  state.session = null;
  state.runtime = null;
  renderRuntimeBar();
  state.events = [];
  state.cursor = 0;
  state.files = [];
  state.uploads = [];
  state.chosenSkills = [];
  $("#messages").replaceChildren(welcome.cloneNode(true));
  $("#approvals").replaceChildren();
  wireSuggestions();
  renderStatus();
  renderAttachments();
  renderDebug();
}
async function newSession() {
  if(instance()?.id!=="default"||state.taskAppID)throw Error("请从应用发起新的业务任务");
  const creationSelection=state.selection;
  const model = $("#model").value.trim();
  const s = await api("/sessions", {
    method: "POST",
    body: { workspaceId: ws().id, instanceId: instance().id, model },
  });
  await refreshSessions();
  if(state.selection!==creationSelection)throw Error("会话已创建；页面已切换，消息尚未发送");
  await selectSession(s.id);
  $("#prompt").focus();
  return s;
}
async function selectSession(id) {
  resetConversation();
  const selection = state.selection;
  state.loadingSession=true;renderStatus();
  let session;
  try {session=await api("/sessions/"+id);} finally {if(selection===state.selection){state.loadingSession=false;renderStatus();}}
  if (selection !== state.selection) return;
  state.session = session;
  state.taskAppID=session.source?.kind==="application"?session.source.appId:"";
  setProductPage("conversation");
  history.replaceState(null,"","#session/"+encodeURIComponent(session.id));
  safe(() => loadReplyFeedback(id, selection));
  $("#session-scope").value = session.archived ? "archived" : "active";
  $("#workspace").value = session.workspaceId;
  $("#instance").value = session.instanceId;
  updateWorkspaceLabel();
  $("#model").value = session.model || "";
  if(session.instanceId==="default"&&session.source?.kind!=="application")localStorage.setItem("rundesk-session",id);
  restoreProductDraft();
  renderSessions();
  renderStatus();
  const stream = new EventSource(`/api/v1/sessions/${id}/events?stream=1`);
  state.stream = stream;
  stream.onopen = () => {
    $("#connection").textContent = "后台已连接";
  };
  stream.onerror = () => {
    $("#connection").textContent = "正在重连…";
  };
  stream.onmessage = (e) => {
    if (state.session?.id !== id) return;
    let event;
    try {
      event = JSON.parse(e.data);
    } catch {
      return;
    }
    if (event.id <= state.cursor) return;
    state.cursor = event.id;
    state.events.push(event);
    if (state.events.length > 12000) state.events.splice(0, 1000);
    scheduleRender();
    if (
      event.method.startsWith("approval/") ||
      event.method === "serverRequest/resolved" ||
      event.method === "turn/completed"
    )
      safe(() => refreshApprovals(id));
    if (
      event.method === "turn/started" ||
      event.method === "turn/completed" ||
      event.method === "run/state"
    ) {
      safe(() => refreshRuntime(id));
      safe(refreshSessions);
      safe(loadFiles);
    }
  };
  await refreshApprovals(id);
  await refreshRuntime(id);
  await loadFiles();
  window.RunDeskTraceUI?.sessionChanged();
}
function scheduleRender() {
  if (state.rendering) return;
  state.rendering = true;
  setTimeout(() => {
    state.rendering = false;
    if (window.RunDeskTraceUI?.isOpen()) return;
    renderMessages();
    renderDebug();
  }, 90);
}
// A deliberately small Markdown renderer. It creates DOM nodes, never inserts HTML.
function inline(text, node) {
  const re = /(\*\*([^*]+)\*\*|`([^`]+)`|\[([^\]]+)\]\(([^)]+)\))/g;
  let pos = 0,
    match;
  while ((match = re.exec(text))) {
    node.append(document.createTextNode(text.slice(pos, match.index)));
    if (match[2]) node.append(el("strong", {}, match[2]));
    else if (match[3]) node.append(el("code", {}, match[3]));
    else {
      let u;
      try {
        u = new URL(match[5], location.href);
      } catch {}
      if (u && ["https:", "http:"].includes(u.protocol))
        node.append(
          el(
            "a",
            { href: u.href, target: "_blank", rel: "noopener noreferrer" },
            match[4],
          ),
        );
      else node.append(document.createTextNode(match[0]));
    }
    pos = re.lastIndex;
  }
  node.append(document.createTextNode(text.slice(pos)));
}
function markdown(text) {
  const root = el("div", { class: "body" });
  const parts = text.split("```");
  parts.forEach((part, i) => {
    if (i % 2) {
      const first = part.indexOf("\n");
      const lang = first >= 0 ? part.slice(0, first) : "";
      const code = first >= 0 ? part.slice(first + 1) : part;
      root.append(el("div", {class:"code-block"},
        el("div", {class:"code-heading"}, el("span",{},lang.trim() || "代码"), replyButton("copy","复制代码",()=>copyReplyText(code))),
        el("pre", { "aria-label": lang || "代码" }, el("code", {}, code))));
      return;
    }
    for (const para of part.split(/\n\s*\n/)) {
      if (!para.trim()) continue;
      let n;
      if (/^#{1,4} /.test(para)) {
        n = el("h3");
        inline(para.replace(/^#{1,4} /, ""), n);
      } else {
        n = el("p");
        para.split("\n").forEach((line, index) => {
          if (index) n.append(el("br"));
          inline(line, n);
        });
      }
      root.append(n);
    }
  });
  return root;
}
function renderMessages() {
  if (!state.session) return;
  const box = $("#messages");
  const atBottom = box.scrollHeight - box.scrollTop - box.clientHeight < 130;
  const items = [],
    map = new Map();
  let runKey = "history";
  for (const ev of state.events) {
    const p = ev.data.params || {};
    if (ev.method === "run/input") {
      runKey = ev.data.runId || ev.id;
      items.push({
        _key: `input:${ev.id}`,
        type: "user",
        text: ev.data.input.text,
        files: ev.data.input.files,
      });
    } else if (ev.method === "run/steer") {
      items.push({
        _key: `steer:${ev.data.requestId || ev.id}`,
        type: "user",
        text: ev.data.input.text,
        files: ev.data.input.files,
        steering: true,
      });
    } else if (
      ev.direction === "in" &&
      (ev.method === "item/started" || ev.method === "item/completed")
    ) {
      const item = p.item;
      if (!item || item.type === "userMessage") continue;
      const key = `${p.turnId || runKey}:${item.id}`;
      let row = map.get(key);
      if (!row) {
        row = { ...item, _key: key };
        map.set(key, row);
        items.push(row);
      } else Object.assign(row, item);
      if (ev.method === "item/completed") row._eventId = ev.id;
    } else if (
      ev.direction === "in" &&
      ev.method === "item/agentMessage/delta"
    ) {
      const key = `${p.turnId || runKey}:${p.itemId}`;
      let row = map.get(key);
      if (!row) {
        row = { _key: key, id: p.itemId, type: "agentMessage", text: "" };
        map.set(key, row);
        items.push(row);
      }
      row.text = (row.text || "") + (p.delta || "");
    } else if (
      ev.direction === "in" &&
      ev.method === "item/commandExecution/outputDelta"
    ) {
      const key = `${p.turnId || runKey}:${p.itemId}`;
      let row = map.get(key);
      if (!row) {
        row = { _key: key, id: p.itemId, type: "commandExecution" };
        map.set(key, row);
        items.push(row);
      }
      row.aggregatedOutput = (row.aggregatedOutput || "") + (p.delta || "");
    } else if (
      ev.direction === "in" &&
      ev.method === "item/reasoning/summaryTextDelta"
    ) {
      const key = `${p.turnId || runKey}:${p.itemId}`;
      let row = map.get(key);
      if (!row) {
        row = { _key: key, id: p.itemId, type: "reasoning", summary: [] };
        map.set(key, row);
        items.push(row);
      }
      const index =
        Number.isInteger(p.summaryIndex) &&
        p.summaryIndex >= 0 &&
        p.summaryIndex < 1000
          ? p.summaryIndex
          : 0;
      row.summary = [...(row.summary || [])];
      const previous = row.summary[index];
      row.summary[index] = {
        type: "summary_text",
        text:
          (typeof previous === "string" ? previous : previous?.text || "") +
          (p.delta || ""),
      };
    } else if (ev.method === "run/state" && ev.data.error) {
      items.push({
        _key: `error:${ev.id}`,
        type: "error",
        text: ev.data.error,
      });
    }
  }
  // Keep mounted cards: native disclosure state, focus and output scroll belong
  // to the reader, not the incoming event stream.
  const existing = new Map(
    [...box.children].map((node) => [node.dataset.messageKey, node]),
  );
  const nodes = [];
  for (const item of items.slice(-200)) {
    const { _key, ...payload } = item;
    const key = JSON.stringify([state.session.id, _key, item.type]);
    let node = existing.get(key);
    const signature = JSON.stringify(payload);
    if (!node) {
      if (item.type === "user") node = el("div", { class: "message user" });
      else if (item.type === "agentMessage")
        node = el("article", { class: "message assistant" });
      else if (item.type === "error")
        node = el("p", { class: "run-error error" });
      else
        node = el(
          "details",
          { class: "tool" },
          el("summary"),
          el("div", { class: "tool-content" }),
        );
      node.dataset.messageKey = key;
    }
    if (node._signature !== signature) {
      if (item.type === "user")
        node.replaceChildren(
          document.createTextNode(item.text || ""),
          ...(item.files?.length
            ? [
                el(
                  "div",
                  { class: "help" },
                  `附件：${item.files.map((path) => path.split("/").pop()).join("、")}`,
                ),
              ]
            : []),
        );
      else if (item.type === "agentMessage")
        renderAssistant(node, item);
      else if (item.type === "error") node.textContent = item.text;
      else {
        toolSummary(node.firstElementChild, item);
        const content = node.lastElementChild;
        // Human-readable primary content; retain the full protocol item on demand.
        let text = "";
        if (item.type === "commandExecution")
          text = [
            item.command,
            item.cwd ? `工作目录：${item.cwd}` : "",
            item.aggregatedOutput,
            item.exitCode != null ? `退出码：${item.exitCode}` : "",
          ]
            .filter(Boolean)
            .join("\n\n");
        else if (item.type === "reasoning")
          text = (item.summary?.length ? item.summary : item.content || [])
            .map((part) => (typeof part === "string" ? part : part.text || ""))
            .join("\n\n");
        if (!content.firstElementChild)
          content.append(
            el("pre", { class: "tool-readable" }),
            el(
              "details",
              { class: "tool-raw" },
              el("summary", {}, "原始数据"),
              el("pre"),
            ),
          );
        const primary = content.firstElementChild;
        const display =
          text ||
          (item.type === "reasoning" ? "暂无可显示的推理摘要" : json(payload));
        if (primary.textContent !== display) primary.textContent = display;
        const raw = content.lastElementChild.lastElementChild;
        if (raw.textContent !== json(payload)) raw.textContent = json(payload);
      }
      if (item.type === "user" && item.steering)
        node.append(
          el("div", { class: "help steer-label" }, "补充指令 · 已接收"),
        );
      node._signature = signature;
    }
    nodes.push(node);
  }
  if (!nodes.length) {
    if (!box.querySelector(".welcome")) {
      box.replaceChildren(welcome.cloneNode(true));
      wireSuggestions();
    }
  } else {
    const keep = new Set(nodes);
    for (const child of [...box.children]) if (!keep.has(child)) child.remove();
    nodes.forEach((node, index) => {
      if (box.children[index] !== node)
        box.insertBefore(node, box.children[index] || null);
    });
  }
  if (speechReply.node && !speechReply.node.isConnected) stopReplySpeech();
  // Reading an open disclosure must not be pulled back to the streaming tail.
  if (atBottom && !box.querySelector("details[open]"))
    box.scrollTop = box.scrollHeight;
}
const LONG_TEXT_THRESHOLD = 8000;
function stageLongText(text) {
  const blob = new Blob([text], { type: "text/plain;charset=utf-8" });
  if (blob.size > 32 * 1024 * 1024)
    throw Error("长文本超过 32 MiB，请拆分后发送");
  const attachment = {
    name: `pasted-text-${Date.now()}-${state.uploads.length + 1}.txt`,
    text,
    chars: [...text].length,
    size: blob.size,
  };
  state.uploads.push(attachment);
  renderAttachments();
  return attachment;
}
$("#prompt").addEventListener("paste", (event) => {
  const text = event.clipboardData?.getData("text/plain");
  if (!text || [...text].length < LONG_TEXT_THRESHOLD) return;
  event.preventDefault();
  if (state.sending) {
    toast("正在发送，请稍后粘贴长文本");
    return;
  }
  try {
    stageLongText(text);
    const prompt = $("#prompt");
    prompt.setRangeText("", prompt.selectionStart, prompt.selectionEnd, "end");
    toast("长文本已转为附件，可预览或移除后再发送");
  } catch (err) {
    toast(err.message);
  }
});
async function sendMessage() {
  if (state.sending) return;
  if ([...$("#prompt").value].length >= LONG_TEXT_THRESHOLD) {
    stageLongText($("#prompt").value);
    $("#prompt").value = "";
  }
  const text =
    $("#prompt").value.trim() ||
    (state.uploads.length ? "请阅读附件，并根据其中的内容和要求处理。" : "");
  if (!text) return;
  if (state.session?.archived) throw Error("请先恢复已归档会话");
  if (["starting", "stopping"].includes(state.session?.status)) {
    toast("任务正在启动或停止，请稍后发送");
    return;
  }
  const attachments = [...state.uploads],
    files = [],
    skills = [...state.chosenSkills];
  const sourceSelection = state.selection,
    workspaceId = ws().id;
  const draft = $("#prompt").value, sourceDraftKey=draftKey();
  state.sending = true;
  renderStatus();
  let id, selection;
  try {
    for (const attachment of attachments) {
      if (!attachment.path) {
        $("#send-feedback").textContent = "正在上传长文本附件…";
        const form = new FormData();
        form.append(
          "file",
          new Blob([attachment.text], { type: "text/plain;charset=utf-8" }),
          attachment.name,
        );
        const result = await api(`/workspaces/${workspaceId}/uploads`, {
          method: "POST",
          body: form,
        });
        attachment.path = result.path;
      }
      files.push(attachment.path);
    }
    if (state.selection !== sourceSelection || ws().id !== workspaceId)
      throw Error("会话已切换，消息未发送");
    if (!state.session) {
      const created=await newSession();
      if(state.session?.id!==created.id)throw Error("会话已切换，消息尚未发送");
      $("#prompt").value=draft;
      state.uploads = attachments;
      state.chosenSkills = skills;
      renderAttachments();
    }
    id = state.session.id;
    selection = state.selection;
    const body = { text, files, skills };
    const retryStart=await hasPendingSubmission(`/sessions/${id}/turns`,body);
    const steering = active(state.session.status) && !retryStart;
    if (steering && !state.session.turnId)
      throw Error("正在确认当前任务，请稍后发送");
    $("#send-feedback").textContent = steering
      ? "正在提交补充指令…"
      : "正在发送…";
    if (steering) {
      body.expectedTurnId = state.session.turnId;
      const signature = JSON.stringify([id, body]);
      if (state.lastSteerRequest?.signature !== signature) {
        const bytes = crypto.getRandomValues(new Uint8Array(16));
        state.lastSteerRequest = {
          signature,
          requestId: Array.from(bytes, (b) =>
            b.toString(16).padStart(2, "0"),
          ).join(""),
        };
      }
      body.requestId = state.lastSteerRequest.requestId;
      await api(`/sessions/${id}/steer`, { method: "POST", body });
      state.lastSteerRequest = null;
    } else {
      await api(`/sessions/${id}/turns`, { method: "POST", body });
    }
    if(state.drafts.get(sourceDraftKey)?.text===draft)state.drafts.delete(sourceDraftKey);
    if (state.session?.id === id && state.selection === selection) {
      // Do not clear a draft typed while the request was in flight.
      if ($("#prompt").value === draft) $("#prompt").value = "";
      state.uploads = state.uploads.filter((f) => !files.includes(f.path));
      state.chosenSkills = state.chosenSkills.filter(
        (sk) => !skills.includes(sk),
      );
      renderAttachments();
      $("#send-feedback").textContent = steering
        ? "补充指令已接收，将由当前任务处理。"
        : "已发送";
    }
    await refreshSessions();
  } catch (err) {
    if (
      (!id || state.session?.id === id) &&
      (selection == null || state.selection === selection)
    )
      $("#send-feedback").textContent = "发送未确认，输入已保留。";
    throw err;
  } finally {
    state.sending = false;
    renderStatus();
  }
}
function renderAttachments() {
  $("#attachments").replaceChildren(
    ...state.uploads.map((f, i) =>
      el(
        "span",
        { class: "chip" },
        f.text != null
          ? button(
              `▧ ${f.name} · ${f.chars.toLocaleString()} 字符`,
              () => {
                $("#preview-title").textContent = f.name;
                $("#preview-content").replaceChildren(
                  el("pre", {}, f.text.slice(0, 20000)),
                  ...(f.text.length > 20000
                    ? [
                        el(
                          "p",
                          { class: "help" },
                          "预览仅显示前 20,000 个 UTF-16 单元；附件保留完整原文。",
                        ),
                      ]
                    : []),
                );
                $("#preview").showModal();
              },
              "attachment-preview",
            )
          : "▧ " + f.name,
        button("×", () => {
          if (state.sending) return;
          state.uploads.splice(i, 1);
          renderAttachments();
        }),
      ),
    ),
    ...state.chosenSkills.map((sk, i) =>
      el(
        "span",
        { class: "chip" },
        "$" + sk.name,
        button("×", () => {
          state.chosenSkills.splice(i, 1);
          renderAttachments();
        }),
      ),
    ),
  );
}
async function uploadFiles(files) {
  const wid = ws().id;
  for (const file of files) {
    const form = new FormData();
    form.append("file", file);
    const result = await api("/workspaces/" + wid + "/uploads", {
      method: "POST",
      body: form,
    });
    if (wid === ws().id) state.uploads.push(result);
  }
  renderAttachments();
  toast("附件已上传");
}
async function refreshApprovals(id = state.session?.id) {
  if (!id) return;
  const approvals = await api(`/sessions/${id}/approvals`);
  if (state.session?.id !== id) return;
  const signature = JSON.stringify(approvals.map((a) => a.id));
  if (signature === state.approvalSignature) return;
  state.approvalSignature = signature;
  $("#approvals").replaceChildren(...approvals.map((a) => approvalCard(a, id)));
}

async function loadFiles() {
  const id = state.session?.id;
  if (!id) {
    state.files = [];
    return;
  }
  const files = await api(`/sessions/${id}/files`);
  if (state.session?.id === id) {
    state.files = files;
    if (state.tab === "files") renderDebug();
  }
}
function fileURL(path, preview = false) {
  return (
    "/api/v1" +
    wpath("/file") +
    "?path=" +
    encodeURIComponent(path) +
    (preview ? "&preview=1" : "")
  );
}
async function previewFile(file) {
  $("#preview-title").textContent = file.name;
  const target = $("#preview-content");
  target.replaceChildren(el("p", { class: "loading" }, "正在加载…"));
  $("#preview").showModal();
  const ext = file.name.split(".").pop().toLowerCase();
  if (["png", "jpg", "jpeg", "gif", "webp"].includes(ext)) {
    target.replaceChildren(
      el("img", { src: fileURL(file.path, true), alt: file.name }),
    );
  } else if (["mp3", "wav", "mp4"].includes(ext)) {
    target.replaceChildren(
      el(ext === "mp4" ? "video" : "audio", {
        src: fileURL(file.path, true),
        controls: true,
      }),
    );
  } else if (
    ["txt", "md", "json", "log"].includes(ext) &&
    file.size < 1048576
  ) {
    const r = await fetch(fileURL(file.path, true));
    if (!r.ok) throw Error("文件读取失败");
    target.replaceChildren(el("pre", {}, await r.text()));
  } else {
    target.replaceChildren(
      el(
        "p",
        { class: "help" },
        "该文件可下载后查看。HTML、SVG 等主动内容不在管理页面执行。",
      ),
      el("a", { href: fileURL(file.path) }, "下载文件 ↓"),
    );
  }
}
async function openSettings(tab = "overview") {
  state.settingsTab = tab;
  if(["skills","mcp"].includes(tab))return showCapabilityPage(tab);
  if(state.view==="capability")closeSettings();
  $("#settings").showModal();
  await renderSettings();
}
async function renderSettings() {
  if(["skills","mcp"].includes(state.settingsTab)&&state.view!=="capability")return showCapabilityPage(state.settingsTab);
  const shared=instance()?.id==="default"&&!!state.taskAppID;
  $("#settings-title").textContent=(shared?"通用助手（历史任务共享）":contextTitle())+" · 设置";
  $("#settings-eyebrow").textContent=instance()?.id==="default"?"ASSISTANT SETTINGS":"APPLICATION SETTINGS";
  $("#settings-context").textContent=shared?"这条历史应用任务沿用通用助手配置；保存也影响通用助手。":`配置对象：${contextTitle()} · 当前项目：${ws()?.name||""}`;
  const target = el("div");
  $(state.view==="capability"?"#capability-content":"#settings-content").replaceChildren(target);
  $$("[data-settings-tab]").forEach((b) =>
    b.classList.toggle("selected", b.dataset.settingsTab === state.settingsTab),
  );
  setLoading(target);
  const tab = state.settingsTab;
  try {
    if (tab === "overview") await renderConfigurationOverview(target);
    else if (tab === "instances") await renderAssistantSettings(target);
    else if (tab === "advanced") await renderInstances(target);
    else if (tab === "notes") renderNotes(target);
    else if (tab === "skills") await renderSkills(target);
    else if (tab === "mcp") await renderMCP(target);
    else await renderRuntime(target);
    compactConfigurationForms(target,tab);
  } catch (e) {
    target.replaceChildren(
      el("div", { class: "run-error error" }, e.message),
      el(
        "p",
        { class: "help" },
        "检查 Codex 是否安装、登录，以及运行后台是否能找到 codex 可执行文件。",
      ),
      button("重试", renderSettings),
    );
  }
}
function renderNotes(target) {
  const w = ws();
  const input = el(
    "textarea",
    { rows: 12, placeholder: "项目约定、偏好或持续需要遵循的背景…" },
    w.notes || "",
  );
  target.replaceChildren(
    el(
      "div",
      { class: "card" },
      el("h3", {}, "项目笔记"),
      el(
        "p",
        {},
        "由你维护的持久笔记。每次提交任务时显式注入；调试台记录当时的内容和版本。Codex 原生上下文仍由 Codex 管理。",
      ),
      input,
      el(
        "div",
        { class: "actions" },
        button(
          "保存笔记",
          async () => {
            const updated = await api("/workspaces/" + w.id + "/notes", {
              method: "PUT",
              body: { text: input.value, revision: w.revision },
            });
            Object.assign(w, updated);
            toast(`已保存 · 版本 ${updated.revision}`);
            renderNotes(target);
          },
          "primary",
        ),
        el("span", { class: "help" }, `版本 ${w.revision} · 最多 16 KiB`),
      ),
    ),
  );
}
$("#login-form").onsubmit = (e) => {
  e.preventDefault();
  safe(async () => {
    try {
      await api("/login", {
        method: "POST",
        body: { token: $("#token").value },
      });
      $("#token").value = "";
      $("#login-error").textContent = "";
      await boot();
    } catch (e) {
      $("#login-error").textContent = e.message;
    }
  });
};
$("#logout").onclick = () =>
  safe(async () => {
    await api("/logout", { method: "POST" });
    if (state.stream) state.stream.close();
    location.reload();
  });
$("#new-session").onclick = () =>
  safe(async () => {
    if(instance()?.id!=="default"||state.taskAppID||state.view!=="conversation")await enterAssistant();
    resetConversation();
    restoreProductDraft();history.replaceState(null,"","#assistant");
    $("#session-scope").value = "active";
    renderSessions();
    $("#model").value = instance()?.defaultModel || "";
    $("#prompt").focus();
  });
$("#workspace").onchange = () =>
  safe(async () => {
    resetConversation();
    updateWorkspaceLabel();
    renderSessions();
    await selectFirst();
  });
$("#add-workspace").onclick = () =>
  safe(async () => {
    const name = prompt("工作区名称");
    if (!name) return;
    const path = prompt("已有目录的绝对路径；留空会创建托管目录。", "");
    if (path === null) return;
    const w = await api("/workspaces", {
      method: "POST",
      body: { name, path },
    });
    state.workspaces.push(w);
    $("#workspace").append(el("option", { value: w.id }, w.name));
    $("#workspace").value = w.id;
    resetConversation();
    updateWorkspaceLabel();
    renderSessions();
  });
$("#composer").onsubmit = (e) => {
  e.preventDefault();
  safe(sendMessage);
};
$("#prompt").onkeydown = (e) => {
  if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
    e.preventDefault();
    safe(sendMessage);
  }
};
$("#stop").onclick = () =>
  safe(async () => {
    if (state.session) {
      await api(`/sessions/${state.session.id}/stop`, { method: "POST", body:{expectedRunId:state.session.runId} });
      await refreshSessions();
    }
  });
$("#attach").onclick = () => $("#upload").click();
$("#upload").onchange = () =>
  safe(async () => {
    await uploadFiles([...$("#upload").files]);
    $("#upload").value = "";
  });
$("#skill-picker").onclick = () => safe(() => openSettings("skills"));
$("#settings-button").onclick = () => safe(() => openSettings());
$("#close-settings").onclick = () => closeSettings();
$("#close-preview").onclick = () => $("#preview").close();
function toggleDebug() {
  $("#debug").classList.toggle("hidden");
  renderDebug();
  if (!$("#debug").classList.contains("hidden")) safe(loadFiles);
}
$("#debug-button").onclick = toggleDebug;
$("#close-debug").onclick = toggleDebug;
$$("[data-debug-tab]").forEach(
  (b) =>
    (b.onclick = () => {
      state.tab = b.dataset.debugTab;
      $$("[data-debug-tab]").forEach((x) =>
        x.classList.toggle("selected", x === b),
      );
      renderDebug();
      if (state.tab === "files") safe(loadFiles);
    }),
);
$$("[data-settings-tab]").forEach(
  (b) =>
    (b.onclick = () =>
      safe(async () => {
        state.settingsTab = b.dataset.settingsTab;
        await renderSettings();
      })),
);
$("#export").onclick = () => {
  if (state.session)
    location.href = "/api/v1/sessions/" + state.session.id + "/export";
  else toast("请先创建会话");
};
document.onkeydown = (e) => {
  if ((e.metaKey || e.ctrlKey) && e.key === "j") {
    e.preventDefault();
    toggleDebug();
  }
  if ((e.metaKey || e.ctrlKey) && e.key === "k") {
    e.preventDefault();
    $("#new-session").click();
  }
};
setInterval(() => {
  if (!document.hidden && $("#login").classList.contains("hidden"))
    safe(async () => {
      await refreshSessions();
      if (active(state.session?.status)) await refreshApprovals();
      if (state.session) await refreshRuntime();
    });
}, 4000);
