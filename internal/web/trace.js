/* One chronological axis; each turn expands in place without losing its neighbours. */
"use strict";
(function () {
  const {Model, bad, duration, short} = RunDeskTrace;
  const {inspect, replyRows, links, text} = RunDeskProcess;
  const labels = {completed:rdText("执行结束"), running:rdText("进行中"), inProgress:rdText("进行中"), pending:rdText("等待确认"), accepted:rdText("已允许"), resolved:rdText("已处理"), declined:rdText("已拒绝"), failed:rdText("失败"), interrupted:rdText("已中断"), expired:rdText("已失效"), warning:rdText("告警"), unknown:rdText("记录不完整"), cancelled:rdText("已取消"), canceled:rdText("已取消")};
  const types = {input:rdText("问题与补充"), tools:rdText("工具调用"), model:rdText("公开摘要"), approval:rdText("人工确认"), context:rdText("技能与上下文"), system:rdText("状态记录")};
  const PAGE_SIZE = 60;
  let model = new Model(), sid = null, run = null, selected = null, opened = false;
  let epoch = 0, cursor = 0, busy = false, timer, query = "", filter = "all", page = 0;
  let currentSession = null, full = new Map(), fetching = new Set();
  let lastSignature = "", detailSignature = "", restored = false;
  let creatingAnalysis = false, expanded = false, answerExpanded = false;
  let turnQuery = "", onlyAttention = false, turnCacheKey = "", turnData = new Map();
  const root = el("section", {id:"trace-workspace", class:"trace-workspace hidden", "aria-label":rdText("任务过程与追问")});
  root.innerHTML = rdFormat("<header class=\"trace-head\"><div class=\"trace-heading\"><span class=\"trace-symbol\" aria-hidden=\"true\">◉</span><div><p id=\"trace-owner\" class=\"trace-eyebrow\"></p><h1>任务轨迹 <span id=\"trace-session-title\"></span></h1></div></div><div class=\"trace-head-actions\"><span id=\"trace-demo\" class=\"trace-demo hidden\">演示记录</span><button id=\"trace-refresh\" title=\"重新读取记录\">刷新</button><button id=\"trace-export\" title=\"导出当前选中轮次的全部步骤\">导出本轮</button><button id=\"trace-diagnostics-button\" data-diagnostics-open>错误记录</button><button id=\"trace-close\">返回对话</button></div></header>\n<div class=\"trace-layout\"><div class=\"trace-center\">\n<div class=\"trace-toolbar\"><div class=\"trace-timeline-heading\"><strong>会话时间轴</strong><span id=\"trace-round-count\"></span></div><div class=\"trace-timeline-controls\"><input id=\"trace-turn-search\" type=\"search\" placeholder=\"搜索问题、工具或返回摘要\" aria-label=\"搜索所有轮次\"/><button id=\"trace-turn-issues\" aria-pressed=\"false\">仅需关注</button><button id=\"trace-next-issue\">下一处需关注</button><button id=\"trace-latest\">最新一轮</button><button id=\"trace-overview\">收起过程</button></div></div>\n<div id=\"trace-scroll\" class=\"trace-scroll\"><p class=\"trace-timeline-note\">从上到下，按发生顺序阅读。每个节点是一轮提问，点开查看过程。</p><ol id=\"trace-runs\" class=\"trace-timeline\" aria-label=\"按发生顺序排列的轮次\"></ol><p id=\"trace-no-turns\" class=\"trace-empty hidden\"></p><div id=\"trace-turn-parking\" class=\"hidden\"><div id=\"trace-turn-body\" class=\"trace-turn-body\"><section class=\"trace-question\"><div class=\"trace-section-line\"><span id=\"trace-round-label\" class=\"trace-eyebrow\">当前问题</span><span id=\"trace-run-state\" class=\"trace-state\"></span></div><h2 id=\"trace-question-text\"></h2><div id=\"trace-run-meta\" class=\"trace-run-meta\"></div><div id=\"trace-context\"></div><div id=\"trace-analyses\"></div></section>\n<section id=\"trace-outcome\" class=\"trace-outcome\" aria-label=\"本轮回复\"></section>\n<section class=\"trace-process-section\" aria-label=\"执行过程\"><div class=\"trace-section-line\"><h2>执行过程 <span id=\"trace-count\"></span></h2><button id=\"trace-find-issue\">定位需关注步骤</button></div><p class=\"trace-caption\">按实际发生顺序记录。步骤完成与是否查到目标分别展示。</p><div class=\"trace-filters\"><div role=\"group\" aria-label=\"筛选步骤\"><button data-filter=\"all\" aria-pressed=\"true\">全部</button><button data-filter=\"tools\" aria-pressed=\"false\">工具</button><button data-filter=\"issues\" aria-pressed=\"false\">需关注</button></div><input id=\"trace-search\" type=\"search\" placeholder=\"搜索本轮步骤预览\" aria-label=\"搜索过程\"/></div><div id=\"trace-steps-list\" aria-label=\"执行步骤\"></div><div class=\"trace-pagination\"><button id=\"trace-page-prev\">上一页</button><span id=\"trace-page-label\"></span><button id=\"trace-page-next\">下一页</button></div></section>\n\n<details id=\"trace-evidence\" class=\"trace-evidence\"><summary>工具返回的链接 <span id=\"trace-link-count\"></span></summary><p class=\"trace-caption\">这些网址来自工具返回内容，最多展示 100 项；链接出现本身不代表内容已核实。点击对应步骤可检查原始记录。</p><div id=\"trace-links\"></div></details>\n<details class=\"trace-evidence\"><summary>会话文件</summary><p class=\"trace-caption\">当前会话的文件，不推定属于所选轮次。</p><div id=\"trace-files\"></div></details>\n</div></div><p id=\"trace-load\" role=\"status\"></p></div><section class=\"trace-dock\"><div class=\"trace-compose-heading\"><strong>分析 <span id=\"trace-analysis-scope\">本次过程</span></strong><span id=\"trace-compose-context\">独立会话 · 只读查询原任务记录</span><button id=\"trace-continue\">继续原任务</button></div><form id=\"trace-analysis-form\"><textarea id=\"trace-analysis-question\" rows=\"1\" aria-label=\"轨迹分析问题\" placeholder=\"例如：为什么没查到？哪个步骤出了问题？\" required maxlength=\"16000\"></textarea><div class=\"trace-analysis-actions\"><button id=\"trace-create-analysis\" type=\"submit\">新建分析并提问</button></div></form><p id=\"trace-analysis-feedback\" role=\"status\"></p></section></div>\n<aside id=\"trace-detail\" class=\"trace-detail hidden\" aria-label=\"步骤详情\"><div class=\"trace-detail-heading\"><strong>步骤详情</strong><button id=\"trace-detail-close\" aria-label=\"关闭步骤详情\">×</button></div><div id=\"trace-detail-content\"></div></aside></div>");
  document.body.append(root);
  root.querySelector(".trace-head-actions").prepend(button("调试检查",()=>window.RunDeskDebug.open(currentSession,{runId:run})));
  window.RunDeskDiagnostics?.sync();
  const q = s => root.querySelector(s);
  const fmt = n => n == null ? rdText("未提供完整耗时") : n < 1000 ? Math.round(n)+" ms" : n < 60000 ? (n/1000).toFixed(1)+" s" : (n/60000).toFixed(1)+" min";
  const stamp = n => n ? new Date(n).toLocaleString("zh-CN",{month:"2-digit",day:"2-digit",hour:"2-digit",minute:"2-digit",second:"2-digit",hour12:false}) : rdText("时间未提供");
  const isIssue = r => bad(r.status) || ["warning", "pending", "unknown"].includes(r.status);
  const currentRun = () => model.runMap.get(run);
  const needsAttention = row => isIssue(row) || inspect(hydrated(row)).empty;
  function cacheTurns() {
    const key = `${model.records}/${currentSession?.status}/${full.size}`;
    if (key === turnCacheKey) return;
    turnCacheKey = key;
    turnData = new Map(model.runs.map(r => [r.id, {list:[], tools:0, issues:0, empty:0, searchable:r.inputRow?.body || r.title}]));
    for (const row of model.list()) {
      const data = turnData.get(row.runId); if (!data) continue;
      data.list.push(row);
      if (row.track === "tools") data.tools++;
      if (isIssue(row)) data.issues++;
      if (inspect(hydrated(row)).empty) data.empty++;
    }
    for (const [id, data] of turnData) {
      data.searchable = (data.searchable + " " + data.list.map(row => [row.title,row.body,text(row.detail)].join(" ")).join(" ")).toLowerCase();
      const answer = replyRows(data.list).at(-1);
      data.reply = answer ? short(inspect(hydrated(answer)).output.split(/\n\s*\n/)[0], 260) : "";
      const r = model.runMap.get(id);
      data.attention = data.issues > 0 || data.empty > 0 || bad(r.status) || r.status === "unknown";
    }
  }
  const turnMatches = r => {
    const data = turnData.get(r.id);
    return (!onlyAttention || data?.attention) && (!turnQuery || data?.searchable.includes(turnQuery.toLowerCase()));
  };
  const turnNode = id => [...q("#trace-runs").children].find(n => n.dataset.run === id);
  function scrollToRun(id) {
    const node = turnNode(id), scroll = q("#trace-scroll");
    if (node) scroll.scrollTop += node.getBoundingClientRect().top - scroll.getBoundingClientRect().top - 16;
  }
  function collapseTurn() {
    expanded = false; hideDetail(); render(); scrollToRun(run); save();
    turnNode(run)?.querySelector(".trace-round")?.focus({preventScroll:true});
  }
  const rows = () => model.list(run || "missing");
  const hydrated = row => {
    const record = full.get(row.refs.at(-1));
    return record?.data?.params?.item ? {...row, detail:{...row.detail,...record.data.params.item}} : row;
  };
  function save() {
    if (!sid) return;
    try {sessionStorage.setItem("rundesk-process-"+sid, JSON.stringify({run, selected, query, filter, page, expanded, answerExpanded, turnQuery, onlyAttention, scroll:q("#trace-scroll").scrollTop, draft:q("#trace-analysis-question").value}));} catch {}
  }
  function icon(row) {
    const paths = {tools:'<path d="m8 3 1 5-5-1 3 3 4-1 6 6 2-2-6-6 1-4-3-3Z"/>', model:'<path d="M4 5h12v9H9l-4 3v-3H4Z"/><path d="M7 8h6M7 11h4"/>', approval:'<path d="M10 2 17 5v5c0 4-7 8-7 8S3 14 3 10V5Z"/><path d="m6 10 3 3 5-6"/>', context:'<path d="M4 3h8l4 4v10H4Z"/><path d="M12 3v5h4M7 11h6M7 14h4"/>', input:'<path d="M3 4h14v10H8l-5 4Z"/>', system:'<circle cx="10" cy="10" r="7"/><path d="M10 6v5M10 14v.1"/>'};
    const n = el("span", {class:"trace-step-icon "+row.track, "aria-hidden":"true"});
    n.innerHTML = '<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round">'+(paths[row.track]||paths.system)+'</svg>';
    return n;
  }
  function status(row) {
    if (row.title === rdText("显式选中 Skills")) return rdText("已提交");
    if (row.point && row.status === "completed") return rdText("已记录");
    if (row.track === "tools" && row.status === "completed") return rdText("调用完成");
    if (row.type === "agentMessage" && row.status === "completed") return rdText("已回复");
    return labels[row.status] || row.status || rdText("未提供状态");
  }
  function liveText(row) {
    if (row.end != null || state.session?.id !== sid) return "";
    const methods = {agentMessage:"item/agentMessage/delta", commandExecution:"item/commandExecution/outputDelta", reasoning:"item/reasoning/summaryTextDelta"};
    let output = "";
    for (const e of state.events) {
      const p = e.data?.params || {};
      if (e.method === methods[row.type] && p.itemId === row.itemId && p.turnId === row.turnId) output += p.delta || "";
    }
    return output.slice(-100000);
  }
  function visibleRows() {
    return rows().filter(r => !(r === currentRun()?.inputRow || r.id === currentRun()?.inputRow?.id) &&
      (filter === "all" || filter === "tools" && r.track === "tools" || filter === "issues" && needsAttention(r)) &&
      (!query || [r.title,r.body,text(r.detail)].join(" ").toLowerCase().includes(query.toLowerCase())));
  }
  function selectRun(id, scroll = true) {
    if (!model.runMap.has(id)) return;
    run = id; expanded = true; answerExpanded = false; selected = null; page = 0; query = ""; filter = "all";
    q("#trace-search").value = "";
    cacheTurns();
    if (!turnMatches(currentRun())) {turnQuery="";onlyAttention=false;q("#trace-turn-search").value="";}
    hideDetail(); lastSignature = ""; render(); if(scroll)scrollToRun(id); save();
  }
  function selectStep(id) {
    const row = model.rows.get(id);
    if (!row) return;
    cacheTurns();
    if (run !== row.runId || !expanded || !turnMatches(model.runMap.get(row.runId))) selectRun(row.runId);
    selected = id;
    if (!visibleRows().some(r=>r.id===id)) {filter="all";query="";q("#trace-search").value="";}
    const index = visibleRows().findIndex(r=>r.id===id);
    if(index>=0)page=Math.floor(index/PAGE_SIZE);
    root.classList.add("has-detail"); q("#trace-detail").classList.remove("hidden");
    detailSignature="";render();save();q("#trace-detail-close").focus();
  }
  function hideDetail() {
    selected=null;detailSignature="";root.classList.remove("has-detail");q("#trace-detail").classList.add("hidden");
  }
  function renderRounds() {
    cacheTurns();
    const all=model.runs, visible=all.filter(turnMatches);
    q("#trace-round-count").textContent = visible.length===all.length ? rdFormat("${0} 轮",all.length) : rdFormat("${0} / ${1} 轮",visible.length,all.length);
    q("#trace-turn-issues").setAttribute("aria-pressed",onlyAttention);
    q("#trace-next-issue").disabled=!all.some(r=>turnData.get(r.id)?.attention);
    q("#trace-latest").disabled=!all.length;
    q("#trace-overview").disabled=!expanded;
    const host=q("#trace-runs"), body=q("#trace-turn-body"), parking=q("#trace-turn-parking");
    // Move the one detail body before removing a filtered or obsolete card.
    const destination = expanded && visible.some(r=>r.id===run) ? turnNode(run)?.querySelector(".trace-turn-card") : parking;
    if (!destination || body.parentElement!==destination) parking.append(body);
    const existing=new Map([...host.children].map(n=>[n.dataset.run,n]));
    const nodes=visible.map(r=>{
      const i=all.indexOf(r), data=turnData.get(r.id), isOpen=expanded&&r.id===run;
      let node=existing.get(r.id);
      if(!node){
        node=el("li",{class:"trace-turn", "data-run":r.id});
        const toggle=button("",()=>expanded&&run===r.id?collapseTurn():selectRun(r.id),"trace-round");
        toggle.dataset.run=r.id;
        node.append(el("span",{class:"trace-turn-dot","aria-hidden":"true"},String(i+1).padStart(2,"0")),el("article",{class:"trace-turn-card"},toggle));
      }
      const toggle=node.querySelector(".trace-round");
      const sig=JSON.stringify([r.title,r.status,r.start,r.end,data.tools,data.issues,data.empty,data.reply,isOpen,r.id===run,i,r.recovery]);
      if(toggle._sig!==sig){
        const meta=el("span",{class:"trace-turn-meta"},`Turn ${i+1}`,el("span",{},stamp(r.start)),el("span",{class:"trace-state "+(bad(r.status)?"issue":active(r.status)?"running":"")},labels[r.status]||r.status));
        const stats=el("span",{class:"trace-turn-stats"},el("span",{},rdFormat("${0} 次工具调用",data.tools)),el("span",{},r.end!=null&&r.start!=null&&r.end>=r.start?fmt(r.end-r.start):rdText("耗时尚未完整")));
        if(r.recovery)stats.append(el("span",{class:"trace-context-chip"},rdText("恢复轮次")));
        if(data.issues)stats.append(el("span",{class:"trace-turn-alert"},rdFormat("${0} 个需关注步骤",data.issues)));
        if(data.empty)stats.append(el("span",{class:"trace-turn-empty"},rdFormat("${0} 个空返回",data.empty)));
        stats.append(el("span",{class:"trace-turn-action"},isOpen?rdText("收起过程"):rdText("查看过程")));
        const title=r.inputRow?.body||r.title;
        toggle.replaceChildren(meta,el("strong",{class:"trace-turn-title"},title),el("span",{class:"trace-turn-preview"},data.reply?rdText("回复摘录 · ")+data.reply:bad(r.status)?rdText("本轮已中断或失败，未记录最终回复。"):r.status==="completed"?rdText("执行已结束，未记录最终回复。"):rdText("尚未记录最终回复，可展开查看已有步骤。")),stats);
        toggle._sig=sig;
      }
      toggle.setAttribute("aria-expanded",isOpen);
      toggle.setAttribute("aria-current",r.id===run?"step":"false");
      if(isOpen)toggle.setAttribute("aria-controls","trace-turn-body");else toggle.removeAttribute("aria-controls");
      node.classList.toggle("expanded",isOpen);node.classList.toggle("selected",r.id===run);node.classList.toggle("attention",data.attention);
      node.classList.toggle("running",active(r.status));
      return node;
    });
    const keep=new Set(nodes);for(const child of [...host.children])if(!keep.has(child))child.remove();
    nodes.forEach((n,i)=>{if(host.children[i]!==n)host.insertBefore(n,host.children[i]||null);});
    const target=expanded ? turnNode(run)?.querySelector(".trace-turn-card") : null;
    if(target && body.parentElement!==target)target.append(body);
    q("#trace-no-turns").classList.toggle("hidden",visible.length>0);
    q("#trace-no-turns").textContent=all.length?rdText("没有匹配的轮次。可以清空搜索或关闭筛选。"):rdText("尚未开始。发送问题后，实际发生的过程会出现在这里。");
  }
  function renderQuestion() {
    const r=currentRun(), list=rows(), index=model.runs.findIndex(x=>x.id===run);
    q("#trace-round-label").textContent=r?rdFormat("第 ${0} 轮 · ${1}",index+1,stamp(r.start)):rdText("当前会话");
    q("#trace-question-text").textContent=r?.inputRow?.body || r?.title || rdText("还没有运行记录");
    q("#trace-run-state").textContent=r?(labels[r.status]||r.status):rdText("等待提问");
    q("#trace-run-state").className="trace-state "+(r&&bad(r.status)?"issue":"");
    const toolCount=list.filter(r=>r.track==="tools").length;
    q("#trace-create-analysis").disabled=!r||creatingAnalysis;
    q("#trace-analysis-scope").textContent=r?`Turn ${index+1}${turnMatches(r)?"":rdText("（不在筛选结果中）")}`:rdText("本次过程");
    const related=state.sessions.filter(s=>s.traceOrigin?.sessionId===sid && s.traceOrigin.runId===run);
    const relatedHost=q("#trace-analyses"),relatedSignature=JSON.stringify(related.map(s=>[s.id,s.title,s.updated]));
    if(relatedHost._sig!==relatedSignature){relatedHost._sig=relatedSignature;relatedHost.replaceChildren(...related.slice(0,5).map(s=>button(rdText("打开分析会话 · ")+stamp(Date.parse(s.created)),()=>selectSession(s.id),"trace-context-chip")));}
    q("#trace-run-meta").textContent=r?[rdFormat("${0} 次工具调用",toolCount),rdFormat("${0} 个需关注步骤",list.filter(isIssue).length),r.end!=null?rdFormat("本轮 ${0}",fmt(r.end-r.start)):rdText("运行边界尚未完整")].join(" · "):rdText("发送问题后，实际执行的步骤会出现在这里。");
    const host=q("#trace-context"), skills=list.filter(x=>x.title===rdText("显式选中 Skills"));
    const sig=JSON.stringify([skills.map(x=>[x.id,x.body]),r?.recovery]);
    if(host._sig!==sig){host.replaceChildren(...skills.map(row=>button(rdText("已选技能 · 查看提交记录"),()=>selectStep(row.id),"trace-context-chip")));if(r?.recovery)host.append(button(rdText("查看来源轮次"),()=>selectRun(r.recovery.sourceRunId),"trace-context-chip"));host._sig=sig;}
  }
  function renderSteps() {
    const list=visibleRows();page=Math.min(page,Math.max(0,Math.ceil(list.length/PAGE_SIZE)-1));
    q("#trace-count").textContent=rdFormat("${0} 步",list.length);
    q("#trace-find-issue").disabled=!rows().some(needsAttention);
    for(const b of root.querySelectorAll("[data-filter]"))b.setAttribute("aria-pressed",b.dataset.filter===filter);
    const host=q("#trace-steps-list"), existing=new Map([...host.children].map(n=>[n.dataset.row,n]));
    const shown=list.slice(page*PAGE_SIZE,(page+1)*PAGE_SIZE);
    const nodes=shown.map(row=>{
      let n=existing.get(row.id);
      if(!n){n=button("",()=>selectStep(row.id),"trace-step");n.dataset.row=row.id;}
      const sig=JSON.stringify([row.title,row.body,row.status,row.end,row.id===selected,full.has(row.refs.at(-1))]);
      if(n._sig!==sig){
        const value=inspect(hydrated(row));
        let excerpt=short(value.error||value.output||value.input||row.body,150);
        if(!excerpt)excerpt=row.end==null?rdText("等待工具或模型返回内容…"):rdText("未记录可展示的返回内容");
        n.replaceChildren(icon(row),el("span",{class:"trace-step-copy"},el("strong",{},row.title),el("span",{},excerpt)),el("span",{class:"trace-step-meta"},el("span",{class:"trace-state "+(isIssue(row)?"issue":"")},value.empty?rdText("空返回"):status(row)),el("small",{},row.point?stamp(row.start).split(" ").at(-1):row.end==null?rdText("等待结束记录"):fmt(duration(row)))));
        n._sig=sig;
      }
      n.classList.toggle("selected",row.id===selected);n.classList.toggle("needs-attention",needsAttention(row));return n;
    });
    const keep=new Set(nodes);for(const child of [...host.children])if(!keep.has(child))child.remove();
    nodes.forEach((n,i)=>{if(host.children[i]!==n)host.insertBefore(n,host.children[i]||null);});
    if(!nodes.length)host.replaceChildren(el("p",{class:"trace-empty"},model.runs.length?rdText("没有匹配的步骤。可以调整筛选条件。"):rdText("尚未开始。这里会保留成功、失败和中断的过程。")));
    q(".trace-pagination").classList.toggle("hidden",list.length<=PAGE_SIZE);
    q("#trace-page-prev").disabled=page===0;q("#trace-page-next").disabled=(page+1)*PAGE_SIZE>=list.length;
    q("#trace-page-label").textContent=`${page*PAGE_SIZE+1}–${Math.min((page+1)*PAGE_SIZE,list.length)} / ${list.length}`;
  }
  async function readFull(row) {
    const id=row.refs.at(-1), session=sid, generation=epoch;
    if(full.has(id)||fetching.has(id))return;
    fetching.add(id);
    try {
      const record=await api(`/sessions/${encodeURIComponent(session)}/events/${id}`);
      if(generation!==epoch||sid!==session)return;
      full.set(id,record);renderRounds();renderSteps();renderOutcome();renderEvidence();detailSignature="";renderDetail();
    } catch(e) {if(generation===epoch)q("#trace-load").textContent=rdText("完整记录读取失败，可刷新重试：")+e.message;}
    finally {if(generation===epoch)fetching.delete(id);}
  }
  function renderOutcome() {
    const r=currentRun(), answers=replyRows(rows()), host=q("#trace-outcome");
    const sig=JSON.stringify([run,r?.status,answerExpanded,answers.map(a=>[a.id,inspect(hydrated(a)).output,liveText(a),full.has(a.refs.at(-1))])]);
    if(host._sig===sig)return;host._sig=sig;
    host.replaceChildren(el("div",{class:"trace-section-line"},el("h2",{},rdText("本轮回复")),el("span",{class:"trace-caption"},rdText("来自助手的实际输出"))));
    if(!answers.length){
      host.append(el("p",{class:"trace-no-reply"},r&&bad(r.status)?rdText("这一轮已中断或失败，尚未记录最终回复。已发生的步骤仍可查看。"):r?.status==="completed"?rdText("执行已结束，但没有记录最终回复。可以检查步骤，或继续提问。"):rdText("尚未记录回复，执行过程会持续更新。")));
    }
    for(const row of answers){
      const output=inspect(hydrated(row)).output||liveText(row);
      const note=row.end==null?rdText("正在生成 · 实时片段"):row.detail?.phase==="final_answer"?rdText("最终回复"):rdText("最新助手回复 · 接口未标明最终阶段");
      host.append(el("p",{class:"trace-caption"},note),markdown(output.slice(0,answerExpanded?200000:800)||rdText("等待回复内容…")));
      if(output.includes(rdText("[预览已截断]"))&&!full.has(row.refs.at(-1)))host.append(el("p",{class:"trace-caption"},rdText("正在读取完整回复…")));
      if(output.length>200000)host.append(el("p",{class:"trace-caption"},rdText("页面显示前 200,000 字符；可下载完整事件。")));
      const actions=el("div",{class:"trace-answer-actions"});
      if(output.length>800){const toggle=button(answerExpanded?rdText("收起完整回复"):rdText("展开完整回复"),()=>{answerExpanded=!answerExpanded;renderOutcome();save();});toggle.id="trace-answer-toggle";toggle.setAttribute("aria-expanded",answerExpanded);actions.append(toggle);}
      actions.append(button(rdText("查看回复记录"),()=>selectStep(row.id)));
      if(row.end!=null)actions.append(button(rdText("复制回复"),async()=>{await readFull(row);const result=inspect(hydrated(row)).output;if(!full.has(row.refs.at(-1)))throw Error(rdText("完整回复未读取，未复制截断内容"));await copyReplyText(result);}));
      host.append(actions);
      if(row.end!=null&&!full.has(row.refs.at(-1)))readFull(row);
    }
    const suggestions=el("div",{class:"trace-question-suggestions"});
    if(r)suggestions.append(button(rdText("解释这一轮的结果"),()=>suggest(rdFormat("请解释第 ${0} 轮「${1}」的已有结果与限制，区分已确认内容和未确认内容。请先分析已有记录，不要重新执行工具。",model.runs.indexOf(r)+1,r.title))));
    if(rows().some(needsAttention))suggestions.append(button(rdText("分析未完成的原因"),()=>{
      const issue=rows().find(needsAttention);askStep(issue);
    }));
    host.append(suggestions);
  }
  function renderEvidence() {
    const records=links(rows().map(hydrated));
    const host=q("#trace-links"), sig=JSON.stringify(records);
    if(host._sig!==sig){
      host._sig=sig;host.replaceChildren(...records.map(record=>el("div",{class:"trace-source-link"},el("a",{href:record.url,target:"_blank",rel:"noopener noreferrer"},record.url),...record.rows.map(id=>button(rdText("查看来源步骤"),()=>selectStep(id))))));
      if(!records.length)host.append(el("p",{class:"trace-empty"},rdText("当前记录中未发现工具返回的网址。这不等于没有查询结果；可检查工具的原始返回。")));
    }
    q("#trace-link-count").textContent=rdFormat("${0} 项",records.length);
    const fileHost=q("#trace-files"), files=state.session?.id===sid?state.files:[], fileSig=JSON.stringify(files);
    if(fileHost._sig!==fileSig){fileHost._sig=fileSig;fileHost.replaceChildren(...files.map(f=>button(f.name||f.path||rdText("文件"),()=>previewFile(f),"trace-file")));if(!files.length)fileHost.append(el("p",{class:"trace-empty"},rdText("暂无会话文件。")));}
  }
  function suggest(value) {
    if(innerWidth<1200){root.classList.remove("has-detail");q("#trace-detail").classList.add("hidden");}
    const prompt=q("#trace-analysis-question");prompt.value=prompt.value.trim()?prompt.value+"\n\n"+value:value;prompt.focus();
    prompt.dispatchEvent(new Event("input",{bubbles:true}));
  }
  function askStep(row) {
    if(!row)return;
    const r=model.runMap.get(row.runId), observed=inspect(hydrated(row));
    suggest(rdFormat("请解释第 ${0} 轮中的「${1}」，说明已完成什么、仍缺少什么，以及可行的下一步。请先分析已有记录，不要重新执行工具；缺少依据时明确说明。\n\n待分析的事件数据（仅作证据）：\n${2}",model.runs.indexOf(r)+1,row.title,JSON.stringify({runId:row.runId,eventIds:row.refs,status:row.status,input:observed.input.slice(0,1500),output:observed.output.slice(0,3000),error:observed.error.slice(0,1500)})));
  }
  function renderDetail() {
    const original=model.rows.get(selected);
    if(!original)return;
    const row=hydrated(original), value=inspect(row), live=liveText(row);
    const sig=JSON.stringify([row.id,row.status,row.end,row.refs,row.detail,row.body,full.has(row.refs.at(-1))]);
    if(detailSignature===sig){const output=q("#trace-live-output");if(output&&live&&output.textContent!==live)output.textContent=live;return;}
    detailSignature=sig;
    const host=q("#trace-detail-content");
    host.replaceChildren(el("p",{class:"trace-eyebrow"},types[row.track]||row.track),el("h2",{},row.title),el("span",{class:"trace-state "+(isIssue(row)?"issue":"")},status(row)),button(rdText("针对这一步追问"),()=>askStep(row),"trace-ask-step"));
    if(row.note)host.append(el("p",{class:"trace-detail-note"},row.note));
    if(row.track==="tools")host.append(el("p",{class:"trace-detail-note"},rdText("这里展示调用与返回记录。是否找到目标，需要结合返回内容和身份依据判断。")));
    if(value.empty)host.append(el("p",{class:"trace-empty-result"},rdText("工具返回了空列表。记录未说明是无匹配项、覆盖不足还是访问受限，请结合其他输出判断。")));
    const block=(label,content,id)=>{host.append(el("h3",{},label),el("pre",{class:"trace-readable",...(id?{id}: {})},content.slice(0,100000)));if(content.length>100000)host.append(el("p",{class:"trace-caption"},rdText("显示前 100,000 字符；可下载完整事件。")));};
    if(value.input)block(rdText("提交给工具的输入"),value.input);
    if(value.error)block(rdText("工具报告的错误"),value.error);
    const output=live||value.output;
    if(output && output!==value.error)block(row.track==="tools"?rdText("工具返回内容"):rdText("记录内容"),output,row.end==null?"trace-live-output":null);
    else if(!output)host.append(el("p",{class:"trace-empty"},row.end==null?rdText("等待返回内容。"):rdText("该事件未提供可展示的输出。")));
    if(!full.has(row.refs.at(-1)))host.append(button(rdText("读取完整记录"),()=>readFull(row),"trace-load-full"));
    const timing=el("details",{class:"trace-technical"},el("summary",{},rdText("时间与运行信息")));
    timing.append(el("p",{},rdFormat("开始：${0}\n结束：${1}\n耗时：${2}\n时间依据：${3}${4}",stamp(row.start),stamp(row.end),row.point?rdText("瞬时记录"):fmt(duration(row)),row.timeSource,row.endSource&&row.endSource!==row.timeSource?" → "+row.endSource:"")));
    if(row.incomplete)timing.append(el("p",{},rdText("缺少完整边界或时间有冲突，不推定完整耗时。")));
    if(row.exitCode!=null)timing.append(el("p",{},rdText("退出码：")+row.exitCode));
    if(currentRun()?.effective)timing.append(el("pre",{},text(currentRun().effective)));
    host.append(timing);
    const raw=el("details",{class:"trace-technical"},el("summary",{},rdText("原始事件与下载")),el("p",{class:"trace-caption"},rdText("完整事件包含工具参数和输出；导出前请检查敏感信息。")));
    for(const id of row.refs)raw.append(button("#"+id,async()=>{
      const generation=epoch, session=sid, chosen=selected;
      const record=full.get(id)||await api(`/sessions/${encodeURIComponent(session)}/events/${id}`);
      if(generation!==epoch||session!==sid||chosen!==selected)return;
      full.set(id,record);
      const section=el("details",{open:true},el("summary",{},`#${id} · ${record.method}`),el("pre",{},text(record).slice(0,100000)),button(rdText("下载完整事件"),()=>downloadText(`rundesk-event-${id}.json`,text(record),"application/json")));
      raw.append(section);
    }));
    host.append(raw);
  }
  function syncComposer() {
    if(!opened)return;
    q("#trace-create-analysis").disabled=!currentRun()||!turnMatches(currentRun())||creatingAnalysis;
    q("#trace-export").disabled=!currentRun();
    q("#trace-analysis-question").disabled=creatingAnalysis;
  }
  async function startAnalysis() {
    if(creatingAnalysis||!currentRun()||!turnMatches(currentRun()))return;
    const question=q("#trace-analysis-question").value.trim();if(!question)return;
    creatingAnalysis=true;syncComposer();
    const source=sid,generation=epoch,chosen=model.rows.get(selected);
    const selection={sessionId:source,runId:run,...(chosen?{eventIds:chosen.refs}:{})};
    let created;
    q("#trace-analysis-feedback").textContent=rdText("正在建立关联的分析会话…");
    try {
      created=await api("/sessions",{method:"POST",body:{traceAnalysis:selection}});
      await refreshSessions();
      if(!opened||sid!==source||generation!==epoch){toast(rdText("分析会话已创建，可从会话列表打开。"));return;}
      q("#trace-analysis-question").value="";save();
      await selectSession(created.id);
      if(state.session?.id!==created.id)return;
      $("#prompt").value=question;
      await sendMessage();
    } catch(e) {
      if(created&&state.session?.id===created.id){$("#send-feedback").textContent=rdText("分析会话已创建，问题尚未确认发送；可在此重试。");toast(e.message);}
      else if(opened&&sid===source)q("#trace-analysis-feedback").textContent=rdText("未完成创建，问题已保留：")+e.message;
    } finally {creatingAnalysis=false;syncComposer();}
  }
  function render() {
    if(!opened)return;
    renderRounds();renderQuestion();if(expanded){renderSteps();renderOutcome();renderEvidence();renderDetail();}syncComposer();
  }
  async function refresh() {
    if(!opened||busy||!sid)return;
    busy=true;const generation=epoch, session=sid;let through=0;
    q("#trace-load").textContent=rdText("读取记录…");
    try{
      do{
        const result=await api(`/sessions/${encodeURIComponent(session)}/trace?`+new URLSearchParams({after:cursor,through,limit:250}));
        if(generation!==epoch||!opened)return;
        model.ingest(result.events);cursor=result.nextCursor;through=result.hasMore?result.snapshot:0;
        currentSession=result.session;
        if(!through){
          model.reconcile(currentSession);
          // New turns update the overview but never steal the selected historical turn.
          if(!model.runMap.has(run))run=model.runs.at(-1)?.id||null;
          if(selected&&!model.rows.has(selected))hideDetail();
          const signature=JSON.stringify([model.records,currentSession.status,run,selected]);
          if(signature!==lastSignature){lastSignature=signature;render();}
          if(!restored){restored=true;let saved;try{saved=JSON.parse(sessionStorage.getItem("rundesk-process-"+sid)||"null");}catch{}q("#trace-scroll").scrollTop=saved?.scroll||0;if(saved?.origin)scrollToRun(run);if(selected&&expanded) {root.classList.add("has-detail");q("#trace-detail").classList.remove("hidden");renderDetail();}}
        }
        q("#trace-load").textContent=rdFormat("${0} 条记录 · ${1}",model.records,active(currentSession.status)?rdText("持续更新"):rdText("已同步"));
        if(through)await new Promise(resolve=>setTimeout(resolve,0));
      }while(through);
      save();
    }catch(e){if(generation===epoch)q("#trace-load").textContent=rdText("读取失败，已显示的记录保留：")+e.message;}
    finally{if(generation===epoch)busy=false;}
  }
  function open() {
    if(!state.session){toast(rdText("请先发起一次对话或选择已有任务。"));return;}
    if(opened)return;
    sid=state.session.id;model=new Model();cursor=0;epoch++;busy=false;full=new Map();fetching=new Set();lastSignature="";detailSignature="";restored=false;currentSession=state.session;turnCacheKey="";turnData=new Map();
    let saved;try{saved=JSON.parse(sessionStorage.getItem("rundesk-process-"+sid)||"null");}catch{}
    run=saved?.run||null;selected=saved?.selected||null;query=saved?.query||"";filter=["all","tools","issues"].includes(saved?.filter)?saved.filter:"all";page=Number.isInteger(saved?.page)&&saved.page>=0?saved.page:0;
    expanded=saved?.expanded===true || !!saved?.selected;answerExpanded=saved?.answerExpanded===true;
    turnQuery=saved?.turnQuery||"";onlyAttention=saved?.onlyAttention===true;
    q("#trace-turn-search").value=turnQuery;
    q("#trace-search").value=query;
    q("#trace-analysis-question").value=saved?.draft||"";
    q("#trace-analysis-feedback").textContent="";
    opened=true;$("#shell").inert=true;root.classList.remove("hidden");root.classList.remove("has-detail");q("#trace-detail").classList.add("hidden");
    q("#trace-owner").textContent=[$("#page-label").textContent,ws()?.name].filter(Boolean).join(" / ");q("#trace-session-title").textContent=state.session.title;
    q("#trace-demo").classList.toggle("hidden",!state.demo);
    sessionStorage.setItem("rundesk-trace-session",sid);history.replaceState(null,"",location.pathname+location.search+"#trace");
    render();refresh();clearInterval(timer);timer=setInterval(refresh,1400);q("#trace-close").focus();
  }
  function close(navigate=true) {
    if(!opened)return;
    save();opened=false;epoch++;busy=false;clearInterval(timer);
    $("#shell").inert=false;root.classList.add("hidden");
    if(navigate&&sid){history.replaceState(null,"",location.pathname+location.search+"#session/"+encodeURIComponent(sid));$("#trace-button").focus();}
    renderStatus();scheduleRender();
  }
  const filterTurns=()=>{hideDetail();render();q("#trace-scroll").scrollTop=0;save();};
  q("#trace-turn-search").oninput=e=>{turnQuery=e.target.value;filterTurns();};
  q("#trace-turn-issues").onclick=()=>{onlyAttention=!onlyAttention;filterTurns();};
  q("#trace-overview").onclick=collapseTurn;
  q("#trace-latest").onclick=()=>{turnQuery="";onlyAttention=false;q("#trace-turn-search").value="";selectRun(model.runs.at(-1)?.id);};
  q("#trace-next-issue").onclick=()=>{
    cacheTurns();const items=model.list().filter(needsAttention), index=items.findIndex(row=>row.id===selected);
    const row=items[(index+1)%items.length];
    if(row)selectStep(row.id);
    else {const turns=model.runs.filter(r=>turnData.get(r.id)?.attention);const next=turns[(turns.findIndex(r=>r.id===run)+1)%turns.length];if(next)selectRun(next.id);}
  };
  q("#trace-close").onclick=()=>close();q("#trace-refresh").onclick=()=>{lastSignature="";refresh();};
  q("#trace-detail-close").onclick=()=>{const id=selected;hideDetail();renderSteps();save();[...root.querySelectorAll(".trace-step")].find(n=>n.dataset.row===id)?.focus();};
  q("#trace-search").oninput=e=>{query=e.target.value;page=0;renderSteps();save();};
  for(const b of root.querySelectorAll("[data-filter]"))b.onclick=()=>{filter=b.dataset.filter;page=0;renderSteps();save();};
  q("#trace-page-prev").onclick=()=>{page--;renderSteps();save();q(".trace-process-section").scrollIntoView({block:"start"});};
  q("#trace-page-next").onclick=()=>{page++;renderSteps();save();q(".trace-process-section").scrollIntoView({block:"start"});};
  q("#trace-find-issue").onclick=()=>{const row=rows().find(needsAttention);if(row)selectStep(row.id);};
  q("#trace-continue").onclick=()=>close();
  q("#trace-analysis-form").onsubmit=e=>{e.preventDefault();startAnalysis();};
  q("#trace-export").onclick=()=>downloadText(`rundesk-process-${sid}.json`,text({version:2,sessionId:sid,runId:run,exportedAt:new Date().toISOString(),note:rdText("事件预览与可见过程；完整事件可按 refs 单独下载，不包含隐藏推理。"),run:currentRun(),steps:rows().map(hydrated),links:links(rows().map(hydrated))}),"application/json");
  root.addEventListener("keydown",e=>{if(e.key!=="Escape")return;if(!q("#trace-detail").classList.contains("hidden")){q("#trace-detail-close").click();e.preventDefault();}else if(!e.target.matches("input,textarea,select")){close();e.preventDefault();}});
  window.addEventListener("beforeunload",save);
  $("#trace-button").onclick=open;
  window.RunDeskTraceUI={open,close,isOpen:()=>opened,syncComposer,updateLive:()=>{if(opened){renderOutcome();renderDetail();syncComposer();}},openOrigin:async origin=>{await selectSession(origin.sessionId);sessionStorage.setItem("rundesk-process-"+origin.sessionId,JSON.stringify({run:origin.runId,expanded:true,origin:true}));open();},sessionChanged:()=>{if(opened&&sid!==state.session?.id)close(false);if(location.hash==="#trace"&&state.session)open();}};
})();
