/* A question, its recorded attempts, and its reply share one workspace. */
"use strict";
(function () {
  const {Model, bad, duration, short} = RunDeskTrace;
  const {inspect, replyRows, links, text} = RunDeskProcess;
  const labels = {completed:"执行结束", running:"进行中", inProgress:"进行中", pending:"等待确认", accepted:"已允许", resolved:"已处理", declined:"已拒绝", failed:"失败", interrupted:"已中断", expired:"已失效", warning:"告警", unknown:"记录不完整", cancelled:"已取消", canceled:"已取消"};
  const types = {input:"问题与补充", tools:"工具调用", model:"公开摘要", approval:"人工确认", context:"技能与上下文", system:"状态记录"};
  const PAGE_SIZE = 60;
  let model = new Model(), sid = null, run = null, selected = null, opened = false;
  let epoch = 0, cursor = 0, busy = false, timer, query = "", filter = "all", page = 0;
  let followLatest = true, currentSession = null, full = new Map(), fetching = new Set();
  let lastSignature = "", detailSignature = "", restored = false;
  let creatingAnalysis = false;
  const root = el("section", {id:"trace-workspace", class:"trace-workspace hidden", "aria-label":"任务过程与追问"});
  root.innerHTML = `<header class="trace-head"><div class="trace-heading"><span class="trace-symbol" aria-hidden="true">◉</span><div><p id="trace-owner" class="trace-eyebrow"></p><h1>任务过程 <span id="trace-session-title"></span></h1></div></div><div class="trace-head-actions"><span id="trace-demo" class="trace-demo hidden">演示记录</span><button id="trace-refresh" title="重新读取记录">刷新</button><button id="trace-export">导出记录</button><button id="trace-close">返回对话</button></div></header>
<div class="trace-layout"><aside class="trace-rounds"><div class="trace-rounds-heading"><h2>本次对话</h2><span id="trace-round-count"></span></div><p class="trace-rail-note">每次提问是一轮，过程和回复一起保留。</p><nav id="trace-runs" aria-label="选择问题轮次"></nav><p id="trace-load" role="status"></p></aside>
<div class="trace-center"><div id="trace-scroll" class="trace-scroll"><section class="trace-question"><div class="trace-section-line"><span id="trace-round-label" class="trace-eyebrow">当前问题</span><span id="trace-run-state" class="trace-state"></span></div><h2 id="trace-question-text"></h2><div id="trace-run-meta" class="trace-run-meta"></div><div id="trace-context"></div><div id="trace-analyses"></div></section>
<section class="trace-process-section" aria-label="执行过程"><div class="trace-section-line"><h2>执行过程 <span id="trace-count"></span></h2><button id="trace-find-issue">定位待处理步骤</button></div><p class="trace-caption">按实际发生顺序记录。步骤完成与是否查到目标分别展示。</p><div class="trace-filters"><div role="group" aria-label="筛选步骤"><button data-filter="all" aria-pressed="true">全部</button><button data-filter="tools" aria-pressed="false">工具</button><button data-filter="issues" aria-pressed="false">需关注</button></div><input id="trace-search" type="search" placeholder="搜索步骤或返回内容" aria-label="搜索过程"/></div><div id="trace-steps-list" aria-label="执行步骤"></div><div class="trace-pagination"><button id="trace-page-prev">上一页</button><span id="trace-page-label"></span><button id="trace-page-next">下一页</button></div></section>
<section id="trace-outcome" class="trace-outcome" aria-label="本轮回复"></section>
<details id="trace-evidence" class="trace-evidence"><summary>工具返回的链接 <span id="trace-link-count"></span></summary><p class="trace-caption">这些网址来自工具返回内容，最多展示 100 项；链接出现本身不代表内容已核实。点击对应步骤可检查原始记录。</p><div id="trace-links"></div></details>
<details class="trace-evidence"><summary>会话文件</summary><p class="trace-caption">当前会话的文件，不推定属于所选轮次。</p><div id="trace-files"></div></details>
</div><section class="trace-dock"><div class="trace-compose-heading"><strong>问问这段过程</strong><span id="trace-compose-context">新建独立分析会话，由 Codex 使用只读工具查询原任务记录。</span><button id="trace-continue">继续原任务</button></div><form id="trace-analysis-form"><textarea id="trace-analysis-question" rows="2" aria-label="轨迹分析问题" placeholder="例如：为什么没查到？哪个步骤出了问题？" required maxlength="16000"></textarea><div class="trace-analysis-actions"><span>保留原任务 · 关联所选轮次与步骤</span><button id="trace-create-analysis" type="submit">新建分析并提问</button></div></form><p id="trace-analysis-feedback" role="status"></p></section></div>
<aside id="trace-detail" class="trace-detail hidden" aria-label="步骤详情"><div class="trace-detail-heading"><strong>步骤详情</strong><button id="trace-detail-close" aria-label="关闭步骤详情">×</button></div><div id="trace-detail-content"></div></aside></div>`;
  document.body.append(root);
  const q = s => root.querySelector(s);
  const fmt = n => n == null ? "未提供完整耗时" : n < 1000 ? Math.round(n)+" ms" : n < 60000 ? (n/1000).toFixed(1)+" s" : (n/60000).toFixed(1)+" min";
  const stamp = n => n ? new Date(n).toLocaleString("zh-CN",{month:"2-digit",day:"2-digit",hour:"2-digit",minute:"2-digit",second:"2-digit",hour12:false}) : "时间未提供";
  const isIssue = r => bad(r.status) || ["warning", "pending", "unknown"].includes(r.status);
  const currentRun = () => model.runMap.get(run);
  const rows = () => model.list(run || "missing");
  const hydrated = row => {
    const record = full.get(row.refs.at(-1));
    return record?.data?.params?.item ? {...row, detail:{...row.detail,...record.data.params.item}} : row;
  };
  function save() {
    if (!sid) return;
    try {sessionStorage.setItem("rundesk-process-"+sid, JSON.stringify({run, selected, query, filter, page, followLatest, scroll:q("#trace-scroll").scrollTop, draft:q("#trace-analysis-question").value}));} catch {}
  }
  function icon(row) {
    const paths = {tools:'<path d="m8 3 1 5-5-1 3 3 4-1 6 6 2-2-6-6 1-4-3-3Z"/>', model:'<path d="M4 5h12v9H9l-4 3v-3H4Z"/><path d="M7 8h6M7 11h4"/>', approval:'<path d="M10 2 17 5v5c0 4-7 8-7 8S3 14 3 10V5Z"/><path d="m6 10 3 3 5-6"/>', context:'<path d="M4 3h8l4 4v10H4Z"/><path d="M12 3v5h4M7 11h6M7 14h4"/>', input:'<path d="M3 4h14v10H8l-5 4Z"/>', system:'<circle cx="10" cy="10" r="7"/><path d="M10 6v5M10 14v.1"/>'};
    const n = el("span", {class:"trace-step-icon "+row.track, "aria-hidden":"true"});
    n.innerHTML = '<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round">'+(paths[row.track]||paths.system)+'</svg>';
    return n;
  }
  function status(row) {
    if (row.title === "显式选中 Skills") return "已提交";
    if (row.point && row.status === "completed") return "已记录";
    if (row.track === "tools" && row.status === "completed") return "调用完成";
    if (row.type === "agentMessage" && row.status === "completed") return "已回复";
    return labels[row.status] || row.status || "未提供状态";
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
      (filter === "all" || filter === "tools" && r.track === "tools" || filter === "issues" && isIssue(r)) &&
      (!query || [r.title,r.body,text(r.detail)].join(" ").toLowerCase().includes(query.toLowerCase())));
  }
  function selectRun(id) {
    if (!model.runMap.has(id)) return;
    run = id; selected = null; page = 0; query = ""; filter = "all";
    followLatest = id === model.runs.at(-1)?.id;
    q("#trace-search").value = "";
    q("#trace-scroll").scrollTop = 0;
    hideDetail(); lastSignature = ""; render(); save();
  }
  function selectStep(id) {
    const row = model.rows.get(id);
    if (!row) return;
    if (run !== row.runId) selectRun(row.runId);
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
    q("#trace-round-count").textContent=model.runs.length;
    const host=q("#trace-runs"), existing=new Map([...host.children].map(n=>[n.dataset.run,n]));
    const nodes=model.runs.map((r,i)=>{
      let n=existing.get(r.id);
      if(!n){n=button("",()=>selectRun(r.id),"trace-round");n.dataset.run=r.id;}
      const sig=JSON.stringify([r.title,r.status,r.id===run]);
      if(n._sig!==sig){n.replaceChildren(el("span",{class:"trace-round-number"},String(i+1).padStart(2,"0")),el("span",{class:"trace-round-label"},el("strong",{},r.title),el("small",{},labels[r.status]||r.status)));n._sig=sig;}
      n.classList.toggle("selected",r.id===run);n.setAttribute("aria-current",r.id===run?"step":"false");return n;
    });
    const keep=new Set(nodes);for(const child of [...host.children])if(!keep.has(child))child.remove();
    nodes.forEach((n,i)=>{if(host.children[i]!==n)host.insertBefore(n,host.children[i]||null);});
  }
  function renderQuestion() {
    const r=currentRun(), list=rows(), index=model.runs.findIndex(x=>x.id===run);
    q("#trace-round-label").textContent=r?`第 ${index+1} 轮 · ${stamp(r.start)}`:"当前会话";
    q("#trace-question-text").textContent=r?.inputRow?.body || r?.title || "还没有运行记录";
    q("#trace-run-state").textContent=r?(labels[r.status]||r.status):"等待提问";
    q("#trace-run-state").className="trace-state "+(r&&bad(r.status)?"issue":"");
    const toolCount=list.filter(r=>r.track==="tools").length;
    q("#trace-create-analysis").disabled=!r||creatingAnalysis;
    const related=state.sessions.filter(s=>s.traceOrigin?.sessionId===sid && s.traceOrigin.runId===run);
    const relatedHost=q("#trace-analyses"),relatedSignature=JSON.stringify(related.map(s=>[s.id,s.title,s.updated]));
    if(relatedHost._sig!==relatedSignature){relatedHost._sig=relatedSignature;relatedHost.replaceChildren(...related.slice(0,5).map(s=>button("打开分析会话 · "+stamp(Date.parse(s.created)),()=>selectSession(s.id),"trace-context-chip")));}
    q("#trace-run-meta").textContent=r?[`${toolCount} 次工具调用`,`${list.filter(isIssue).length} 个需关注步骤`,r.end!=null?`本轮 ${fmt(r.end-r.start)}`:"运行边界尚未完整"].join(" · "):"发送问题后，实际执行的步骤会出现在这里。";
    const host=q("#trace-context"), skills=list.filter(x=>x.title==="显式选中 Skills");
    const sig=JSON.stringify(skills.map(x=>[x.id,x.body]));
    if(host._sig!==sig){host.replaceChildren(...skills.map(row=>button("已选技能 · 查看提交记录",()=>selectStep(row.id),"trace-context-chip")));host._sig=sig;}
  }
  function renderSteps() {
    const list=visibleRows();page=Math.min(page,Math.max(0,Math.ceil(list.length/PAGE_SIZE)-1));
    q("#trace-count").textContent=`${list.length} 步`;
    q("#trace-find-issue").disabled=!rows().some(isIssue);
    for(const b of root.querySelectorAll("[data-filter]"))b.setAttribute("aria-pressed",b.dataset.filter===filter);
    const host=q("#trace-steps-list"), existing=new Map([...host.children].map(n=>[n.dataset.row,n]));
    const shown=list.slice(page*PAGE_SIZE,(page+1)*PAGE_SIZE);
    const nodes=shown.map(row=>{
      let n=existing.get(row.id);
      if(!n){n=button("",()=>selectStep(row.id),"trace-step");n.dataset.row=row.id;}
      const sig=JSON.stringify([row.title,row.body,row.status,row.end,row.id===selected]);
      if(n._sig!==sig){
        const value=inspect(hydrated(row));
        let excerpt=short(value.error||value.output||value.input||row.body,150);
        if(!excerpt)excerpt=row.end==null?"等待工具或模型返回内容…":"未记录可展示的返回内容";
        n.replaceChildren(icon(row),el("span",{class:"trace-step-copy"},el("strong",{},row.title),el("span",{},excerpt)),el("span",{class:"trace-step-meta"},el("span",{class:"trace-state "+(isIssue(row)?"issue":"")},status(row)),el("small",{},row.point?stamp(row.start).split(" ").at(-1):row.end==null?"等待结束记录":fmt(duration(row)))));
        n._sig=sig;
      }
      n.classList.toggle("selected",row.id===selected);n.classList.toggle("needs-attention",isIssue(row));return n;
    });
    const keep=new Set(nodes);for(const child of [...host.children])if(!keep.has(child))child.remove();
    nodes.forEach((n,i)=>{if(host.children[i]!==n)host.insertBefore(n,host.children[i]||null);});
    if(!nodes.length)host.replaceChildren(el("p",{class:"trace-empty"},model.runs.length?"没有匹配的步骤。可以调整筛选条件。":"尚未开始。这里会保留成功、失败和中断的过程。"));
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
      full.set(id,record);renderOutcome();renderEvidence();detailSignature="";renderDetail();
    } catch(e) {if(generation===epoch)q("#trace-load").textContent="完整记录读取失败，可刷新重试："+e.message;}
    finally {if(generation===epoch)fetching.delete(id);}
  }
  function renderOutcome() {
    const r=currentRun(), answers=replyRows(rows()), host=q("#trace-outcome");
    const sig=JSON.stringify([run,r?.status,answers.map(a=>[a.id,inspect(hydrated(a)).output,liveText(a),full.has(a.refs.at(-1))])]);
    if(host._sig===sig)return;host._sig=sig;
    host.replaceChildren(el("div",{class:"trace-section-line"},el("h2",{},"本轮回复"),el("span",{class:"trace-caption"},"来自助手的实际输出")));
    if(!answers.length){
      host.append(el("p",{class:"trace-no-reply"},r&&bad(r.status)?"这一轮已中断或失败，尚未记录最终回复。已发生的步骤仍可查看。":r?.status==="completed"?"执行已结束，但没有记录最终回复。可以检查步骤，或继续提问。":"尚未记录回复，执行过程会持续更新。"));
    }
    for(const row of answers){
      const output=inspect(hydrated(row)).output||liveText(row);
      const note=row.end==null?"正在生成 · 实时片段":row.detail?.phase==="final_answer"?"最终回复":"最新助手回复 · 接口未标明最终阶段";
      host.append(el("p",{class:"trace-caption"},note),markdown(output.slice(0,200000)||"等待回复内容…"));
      if(output.includes("[预览已截断]")&&!full.has(row.refs.at(-1)))host.append(el("p",{class:"trace-caption"},"正在读取完整回复…"));
      if(output.length>200000)host.append(el("p",{class:"trace-caption"},"页面显示前 200,000 字符；可下载完整事件。"));
      const actions=el("div",{class:"trace-answer-actions"},button("查看回复记录",()=>selectStep(row.id)));
      if(row.end!=null)actions.append(button("复制回复",async()=>{await readFull(row);const result=inspect(hydrated(row)).output;if(!full.has(row.refs.at(-1)))throw Error("完整回复未读取，未复制截断内容");await copyReplyText(result);}));
      host.append(actions);
      if(row.end!=null&&!full.has(row.refs.at(-1)))readFull(row);
    }
    const suggestions=el("div",{class:"trace-question-suggestions"});
    if(r)suggestions.append(button("解释这一轮的结果",()=>suggest(`请解释第 ${model.runs.indexOf(r)+1} 轮「${r.title}」的已有结果与限制，区分已确认内容和未确认内容。请先分析已有记录，不要重新执行工具。`)));
    if(rows().some(isIssue))suggestions.append(button("分析未完成的原因",()=>{
      const issue=rows().find(isIssue);askStep(issue);
    }));
    host.append(suggestions);
  }
  function renderEvidence() {
    const records=links(rows().map(hydrated));
    const host=q("#trace-links"), sig=JSON.stringify(records);
    if(host._sig!==sig){
      host._sig=sig;host.replaceChildren(...records.map(record=>el("div",{class:"trace-source-link"},el("a",{href:record.url,target:"_blank",rel:"noopener noreferrer"},record.url),...record.rows.map(id=>button("查看来源步骤",()=>selectStep(id))))));
      if(!records.length)host.append(el("p",{class:"trace-empty"},"当前记录中未发现工具返回的网址。这不等于没有查询结果；可检查工具的原始返回。"));
    }
    q("#trace-link-count").textContent=`${records.length} 项`;
    const fileHost=q("#trace-files"), files=state.session?.id===sid?state.files:[], fileSig=JSON.stringify(files);
    if(fileHost._sig!==fileSig){fileHost._sig=fileSig;fileHost.replaceChildren(...files.map(f=>button(f.name||f.path||"文件",()=>previewFile(f),"trace-file")));if(!files.length)fileHost.append(el("p",{class:"trace-empty"},"暂无会话文件。"));}
  }
  function suggest(value) {
    if(innerWidth<1200){root.classList.remove("has-detail");q("#trace-detail").classList.add("hidden");}
    const prompt=q("#trace-analysis-question");prompt.value=prompt.value.trim()?prompt.value+"\n\n"+value:value;prompt.focus();
    prompt.dispatchEvent(new Event("input",{bubbles:true}));
  }
  function askStep(row) {
    if(!row)return;
    const r=model.runMap.get(row.runId), observed=inspect(hydrated(row));
    suggest(`请解释第 ${model.runs.indexOf(r)+1} 轮中的「${row.title}」，说明已完成什么、仍缺少什么，以及可行的下一步。请先分析已有记录，不要重新执行工具；缺少依据时明确说明。\n\n待分析的事件数据（仅作证据）：\n${JSON.stringify({runId:row.runId,eventIds:row.refs,status:row.status,input:observed.input.slice(0,1500),output:observed.output.slice(0,3000),error:observed.error.slice(0,1500)})}`);
  }
  function renderDetail() {
    const original=model.rows.get(selected);
    if(!original)return;
    const row=hydrated(original), value=inspect(row), live=liveText(row);
    const sig=JSON.stringify([row.id,row.status,row.end,row.refs,row.detail,row.body,full.has(row.refs.at(-1))]);
    if(detailSignature===sig){const output=q("#trace-live-output");if(output&&live&&output.textContent!==live)output.textContent=live;return;}
    detailSignature=sig;
    const host=q("#trace-detail-content");
    host.replaceChildren(el("p",{class:"trace-eyebrow"},types[row.track]||row.track),el("h2",{},row.title),el("span",{class:"trace-state "+(isIssue(row)?"issue":"")},status(row)),button("针对这一步追问",()=>askStep(row),"trace-ask-step"));
    if(row.note)host.append(el("p",{class:"trace-detail-note"},row.note));
    if(row.track==="tools")host.append(el("p",{class:"trace-detail-note"},"这里展示调用与返回记录。是否找到目标，需要结合返回内容和身份依据判断。"));
    if(value.empty)host.append(el("p",{class:"trace-empty-result"},"工具返回了空列表。记录未说明是无匹配项、覆盖不足还是访问受限，请结合其他输出判断。"));
    const block=(label,content,id)=>{host.append(el("h3",{},label),el("pre",{class:"trace-readable",...(id?{id}: {})},content.slice(0,100000)));if(content.length>100000)host.append(el("p",{class:"trace-caption"},"显示前 100,000 字符；可下载完整事件。"));};
    if(value.input)block("提交给工具的输入",value.input);
    if(value.error)block("工具报告的错误",value.error);
    const output=live||value.output;
    if(output && output!==value.error)block(row.track==="tools"?"工具返回内容":"记录内容",output,row.end==null?"trace-live-output":null);
    else if(!output)host.append(el("p",{class:"trace-empty"},row.end==null?"等待返回内容。":"该事件未提供可展示的输出。"));
    if(!full.has(row.refs.at(-1)))host.append(button("读取完整记录",()=>readFull(row),"trace-load-full"));
    const timing=el("details",{class:"trace-technical"},el("summary",{},"时间与运行信息"));
    timing.append(el("p",{},`开始：${stamp(row.start)}\n结束：${stamp(row.end)}\n耗时：${row.point?"瞬时记录":fmt(duration(row))}\n时间依据：${row.timeSource}${row.endSource&&row.endSource!==row.timeSource?" → "+row.endSource:""}`));
    if(row.incomplete)timing.append(el("p",{},"缺少完整边界或时间有冲突，不推定完整耗时。"));
    if(row.exitCode!=null)timing.append(el("p",{},"退出码："+row.exitCode));
    if(currentRun()?.effective)timing.append(el("pre",{},text(currentRun().effective)));
    host.append(timing);
    const raw=el("details",{class:"trace-technical"},el("summary",{},"原始事件与下载"),el("p",{class:"trace-caption"},"完整事件包含工具参数和输出；导出前请检查敏感信息。"));
    for(const id of row.refs)raw.append(button("#"+id,async()=>{
      const generation=epoch, session=sid, chosen=selected;
      const record=full.get(id)||await api(`/sessions/${encodeURIComponent(session)}/events/${id}`);
      if(generation!==epoch||session!==sid||chosen!==selected)return;
      full.set(id,record);
      const section=el("details",{open:true},el("summary",{},`#${id} · ${record.method}`),el("pre",{},text(record).slice(0,100000)),button("下载完整事件",()=>downloadText(`rundesk-event-${id}.json`,text(record),"application/json")));
      raw.append(section);
    }));
    host.append(raw);
  }
  function syncComposer() {
    if(!opened)return;
    q("#trace-create-analysis").disabled=!currentRun()||creatingAnalysis;
    q("#trace-analysis-question").disabled=creatingAnalysis;
  }
  async function startAnalysis() {
    if(creatingAnalysis||!currentRun())return;
    const question=q("#trace-analysis-question").value.trim();if(!question)return;
    creatingAnalysis=true;syncComposer();
    const source=sid,generation=epoch,chosen=model.rows.get(selected);
    const selection={sessionId:source,runId:run,...(chosen?{eventIds:chosen.refs}:{})};
    let created;
    q("#trace-analysis-feedback").textContent="正在建立关联的分析会话…";
    try {
      created=await api("/sessions",{method:"POST",body:{traceAnalysis:selection}});
      await refreshSessions();
      if(!opened||sid!==source||generation!==epoch){toast("分析会话已创建，可从会话列表打开。");return;}
      q("#trace-analysis-question").value="";save();
      await selectSession(created.id);
      if(state.session?.id!==created.id)return;
      $("#prompt").value=question;
      await sendMessage();
    } catch(e) {
      if(created&&state.session?.id===created.id){$("#send-feedback").textContent="分析会话已创建，问题尚未确认发送；可在此重试。";toast(e.message);}
      else if(opened&&sid===source)q("#trace-analysis-feedback").textContent="未完成创建，问题已保留："+e.message;
    } finally {creatingAnalysis=false;syncComposer();}
  }
  function render() {
    if(!opened)return;
    renderRounds();renderQuestion();renderSteps();renderOutcome();renderEvidence();renderDetail();syncComposer();
  }
  async function refresh() {
    if(!opened||busy||!sid)return;
    busy=true;const generation=epoch, session=sid;let through=0;
    q("#trace-load").textContent="读取记录…";
    try{
      do{
        const result=await api(`/sessions/${encodeURIComponent(session)}/trace?`+new URLSearchParams({after:cursor,through,limit:250}));
        if(generation!==epoch||!opened)return;
        model.ingest(result.events);cursor=result.nextCursor;through=result.hasMore?result.snapshot:0;
        currentSession=result.session;
        if(!through){
          model.reconcile(currentSession);
          if(followLatest||!model.runMap.has(run))run=model.runs.at(-1)?.id||null;
          if(selected&&!model.rows.has(selected))hideDetail();
          const signature=JSON.stringify([model.records,currentSession.status,run,selected]);
          if(signature!==lastSignature){lastSignature=signature;render();}
          if(!restored){restored=true;let saved;try{saved=JSON.parse(sessionStorage.getItem("rundesk-process-"+sid)||"null");}catch{}q("#trace-scroll").scrollTop=saved?.scroll||0;if(selected) {root.classList.add("has-detail");q("#trace-detail").classList.remove("hidden");renderDetail();}}
        }
        q("#trace-load").textContent=`${model.records} 条记录 · ${active(currentSession.status)?"持续更新":"已同步"}`;
        if(through)await new Promise(resolve=>setTimeout(resolve,0));
      }while(through);
      save();
    }catch(e){if(generation===epoch)q("#trace-load").textContent="读取失败，已显示的记录保留："+e.message;}
    finally{if(generation===epoch)busy=false;}
  }
  function open() {
    if(!state.session){toast("请先发起一次对话或选择已有任务。");return;}
    if(opened)return;
    sid=state.session.id;model=new Model();cursor=0;epoch++;busy=false;full=new Map();fetching=new Set();lastSignature="";detailSignature="";restored=false;currentSession=state.session;
    let saved;try{saved=JSON.parse(sessionStorage.getItem("rundesk-process-"+sid)||"null");}catch{}
    run=saved?.run||null;selected=saved?.selected||null;query=saved?.query||"";filter=["all","tools","issues"].includes(saved?.filter)?saved.filter:"all";page=Number.isInteger(saved?.page)&&saved.page>=0?saved.page:0;followLatest=saved?.followLatest!==false;
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
  q("#trace-close").onclick=()=>close();q("#trace-refresh").onclick=()=>{lastSignature="";refresh();};
  q("#trace-detail-close").onclick=()=>{const id=selected;hideDetail();renderSteps();save();[...root.querySelectorAll(".trace-step")].find(n=>n.dataset.row===id)?.focus();};
  q("#trace-search").oninput=e=>{query=e.target.value;page=0;renderSteps();save();};
  for(const b of root.querySelectorAll("[data-filter]"))b.onclick=()=>{filter=b.dataset.filter;page=0;renderSteps();save();};
  q("#trace-page-prev").onclick=()=>{page--;renderSteps();save();q(".trace-process-section").scrollIntoView({block:"start"});};
  q("#trace-page-next").onclick=()=>{page++;renderSteps();save();q(".trace-process-section").scrollIntoView({block:"start"});};
  q("#trace-find-issue").onclick=()=>{const row=rows().find(isIssue);if(row)selectStep(row.id);};
  q("#trace-continue").onclick=()=>close();
  q("#trace-analysis-form").onsubmit=e=>{e.preventDefault();startAnalysis();};
  q("#trace-export").onclick=()=>downloadText(`rundesk-process-${sid}.json`,text({version:2,sessionId:sid,runId:run,exportedAt:new Date().toISOString(),note:"事件预览与可见过程；完整事件可按 refs 单独下载，不包含隐藏推理。",run:currentRun(),steps:rows().map(hydrated),links:links(rows().map(hydrated))}),"application/json");
  root.addEventListener("keydown",e=>{if(e.key!=="Escape")return;if(!q("#trace-detail").classList.contains("hidden")){q("#trace-detail-close").click();e.preventDefault();}else if(!e.target.matches("input,textarea,select")){close();e.preventDefault();}});
  window.addEventListener("beforeunload",save);
  $("#trace-button").onclick=open;
  window.RunDeskTraceUI={open,close,isOpen:()=>opened,syncComposer,updateLive:()=>{if(opened){renderOutcome();renderDetail();syncComposer();}},openOrigin:async origin=>{await selectSession(origin.sessionId);sessionStorage.setItem("rundesk-process-"+origin.sessionId,JSON.stringify({run:origin.runId,followLatest:false}));open();},sessionChanged:()=>{if(opened&&sid!==state.session?.id)close(false);if(location.hash==="#trace"&&state.session)open();}};
})();
