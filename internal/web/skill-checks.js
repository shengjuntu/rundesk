"use strict";
window.RunDeskSkillChecks = (() => {
  const base = "/skill-center", enc = encodeURIComponent, maxReport = 4 << 20;
  const tr = (zh, en) => document.documentElement.lang === "en" ? en : zh;
  const states = {queued:["排队中","Queued"],running:["运行中","Running"],completed:["已结束","Finished"],failed:["执行失败","Execution failed"],canceled:["已取消","Canceled"],interrupted:["已中断","Interrupted"]};
  const verdicts = {pass:["用例通过","Cases passed"],fail:["用例失败","Cases failed"],error:["执行错误","Execution error"],incomplete:["结果不完整","Incomplete result"]};
  const changes = {fixed:["修好","Fixed"],regressed:["退化","Regressed"],unchanged:["未变","Unchanged"],new:["新增","New"],removed:["删除","Removed"],unknown:["未知","Unknown"]};
  const variants = {default:["默认","Default"],with_skill:["启用技能","With skill"],without_skill:["未启用技能","Without skill"]};
  let generation = 0, disposeCurrent;
  const text = value => value === undefined || value === null || value === "" ? "—" : String(value);
  const translated = (map, key) => map[key] ? tr(...map[key]) : text(key);
  // A terminal execution state does not mean its container has been removed.
  // Keep observing and keep admission closed until cleanup is confirmed.
  const active = job => ["queued", "running"].includes(job?.status) || job?.cleanupPending === true;
  const errorText = error => error?.message || tr("操作失败，请重试。", "The operation failed. Try again.");
  const badge = (value, label) => el("span", {class:"skill-check-badge", "data-check-status":value}, label || text(value));
  const date = value => { const d = value && new Date(value); return d && !Number.isNaN(d.getTime()) ? d.toLocaleString(document.documentElement.lang === "en" ? "en" : "zh-CN") : text(value); };
  function notice(node, message, kind = "help") { node.className = kind; node.textContent = message; node.hidden = !message; }
  function table(headers, rows) {
    return el("div", {class:"skill-check-table-wrap"}, el("table", {class:"skill-check-table"}, el("thead", {}, el("tr", {}, ...headers.map(h => el("th", {scope:"col"}, h)))), el("tbody", {}, ...rows)));
  }
  function reportLabel(row) {
    return [row.report?.skillName || row.id, date(row.createdAt), String(row.digest || "").slice(0, 22), row.source === "external_unverified" ? tr("外部未核验", "External, unverified") : tr("测试运行", "Test run")].join(" · ");
  }
  function renderReport(target, record) {
    if (!record?.report) { target.replaceChildren(el("p", {class:"help"}, tr("选择一份报告查看逐用例结果。", "Choose a report to inspect case results."))); return; }
    const report = record.report, summary = report.summary || {}, status = Object.hasOwn(verdicts, report.status) ? report.status : "incomplete";
    const header = el("div", {class:"skill-check-report-heading"}, el("h4", {}, report.skillName || tr("效果报告", "Evaluation report")), badge(status, translated(verdicts, status)));
    const source = record.source === "external_unverified" ? tr("外部导入，未核验执行来源；仅供对照，不作为安全通过依据。", "Imported externally; execution provenance is unverified. This is a comparison reference, not security approval.") : tr("此报告记录测试结果，不替代安全检查。", "These test results do not replace security inspection.");
    const rows = [];
    for (const item of report.cases || []) {
      const state = ["PASS", "FAIL", "ERROR", "SKIP", "UNKNOWN"].includes(item.status) ? item.status : "UNKNOWN";
      const explanation = el("div", {}, item.error ? el("p", {class:"skill-check-case-error"}, item.error) : null);
      if (item.assertions?.length) explanation.append(el("details", {}, el("summary", {}, tr("断言：", "Assertions: ") + item.assertions.length), el("ul", {class:"skill-check-assertions"}, ...item.assertions.map(assertion => {
        const result = assertion.skipped ? "SKIP" : assertion.passed ? "PASS" : "FAIL";
        return el("li", {}, badge(result), " ", assertion.text || "", assertion.evidence ? el("pre", {}, assertion.evidence) : null);
      }))));
      rows.push(el("tr", {}, el("td", {}, el("strong", {}, item.title || item.caseId), el("small", {}, item.caseId || "")), el("td", {}, translated(variants, item.variant)), el("td", {}, badge(state)), el("td", {}, text(item.durationMs) + " ms"), el("td", {}, explanation)));
    }
    target.replaceChildren(header, el("p", {class:"help"}, source),
      el("p", {class:"skill-check-identity"}, [tr("引擎：", "Engine: ") + text(report.engineName), tr("模型：", "Model: ") + text(report.modelName), tr("配置身份：", "Configuration identity: ") + text(report.configurationId)].join(" · ")),
      el("div", {class:"skill-check-summary"}, ...[["total",tr("主用例", "Primary cases")],["pass","PASS"],["fail","FAIL"],["error","ERROR"],["skip","SKIP"],["unknown","UNKNOWN"]].map(([key,label]) => el("span", {}, label + " ", el("strong", {}, Number.isFinite(summary[key]) ? summary[key] : "—")))),
      el("p", {class:"help"}, tr("汇总按主结果计数；启用与未启用技能的结果在下表分别保留。", "The summary counts primary results. With-skill and without-skill outcomes remain separate below.")),
      ...(report.warnings || []).map(value => el("p", {class:"skill-check-notice"}, value)),
      rows.length ? table([tr("用例", "Case"),tr("变体", "Variant"),tr("结果", "Result"),tr("耗时", "Duration"),tr("原因与断言", "Reasons and assertions")],rows) : el("p", {class:"skill-check-notice"}, tr("没有可核查的用例结果，不能视作通过。", "No verifiable case results are available. This is not a passing result.")));
  }
  function renderComparison(target, value) {
    const result = value?.comparison;
    if (!result) throw Error(tr("服务未返回有效的对比结果。", "The service did not return a valid comparison."));
    const gaps = ["new","removed","unknown"].some(key => result.summary?.[key] > 0) || (result.cases || []).some(row => ["new","removed","unknown"].includes(row.change));
    const verdict = result.comparable && !gaps && ["pass","fail"].includes(result.verdict) ? result.verdict : "incomplete";
    target.replaceChildren(el("div", {class:"skill-check-report-heading"}, el("h4", {}, tr("版本对比", "Version comparison")), badge(verdict, translated(verdicts,verdict))),
      result.reason ? el("p", {class:"skill-check-notice"}, result.reason) : null,
      ...(result.warnings || []).map(value => el("p", {class:"skill-check-notice"}, value)),
      el("div", {class:"skill-check-summary"}, ...Object.entries(changes).map(([key,label]) => el("span", {}, tr(...label) + " ", el("strong", {}, Number.isFinite(result.summary?.[key]) ? result.summary[key] : 0)))),
      el("p", {class:"help"}, tr("新增、删除、缺失和未知结果不会算作通过；模型、配置或输入不一致时，以可比性说明为准。", "New, removed, missing and unknown results are not passes. Check comparability when model, configuration or inputs differ.")),
      table([tr("用例", "Case"),tr("变体", "Variant"),tr("基线", "Baseline"),tr("当前", "Candidate"),tr("变化", "Change")], (result.cases || []).map(row => el("tr", {}, el("td", {}, row.caseId || ""), el("td", {}, translated(variants,row.variant)), el("td", {}, row.beforeStatus || tr("缺失", "Missing")), el("td", {}, row.afterStatus || tr("缺失", "Missing")), el("td", {}, badge(row.change, translated(changes,row.change)), row.reason ? el("p",{class:"help"},row.reason) : null)))));
  }
  function mount(target, pack, options = {}) {
    disposeCurrent?.();
    const g = ++generation;
    let timer, disposed = false, busy = false, importBusy = false, importValue = null, jobs = [], records = [], baselines = [], engines = [], available = false, selectedReport = "", currentJob = null, reportSequence = 0, detailSequence = 0, compareSequence = 0, fileSequence = 0;
    const current = () => !disposed && g === generation && target.isConnected && (!options.isCurrent || options.isCurrent());
    const dispose = () => { disposed = true; clearTimeout(timer); };
    disposeCurrent = dispose;
    const packId=pack.id,digest=pack.digest;
    const path = base + "/packs/" + enc(packId), hasSuite = (pack.files || []).some(file => file.path === "evals/eval.yaml");
    const section = el("section", {class:"skill-checks", id:"skill-checks"});
    target.append(section);
    if (typeof state !== "undefined" && state.identity?.kind !== "administrator") { section.append(el("p", {class:"help"}, tr("效果测试目前仅向管理员开放。", "Evaluation is currently available to administrators."))); return {dispose}; }
    const error = el("p", {class:"error",id:"skill-check-error",role:"alert",hidden:true});
    const engineNote = el("p", {class:"help",id:"skill-check-engine-note"}, tr("正在读取测试环境…", "Loading test environments…"));
    const environment = el("select", {id:"skill-check-environment","aria-label":tr("测试环境", "Test environment")});
    const ack = el("input", {type:"checkbox",id:"skill-check-ack"}), network = el("input", {type:"checkbox",id:"skill-check-network"});
    const validate = button(tr("校验用例", "Validate cases"), () => start("validate"), "quiet"); validate.id = "skill-check-validate";
    const run = button(tr("运行测试", "Run evaluation"), () => start("run"), "primary"); run.id = "skill-check-run";
    const jobTarget = el("div", {id:"skill-check-jobs",class:"skill-check-jobs","aria-live":"polite"});
    const reportSelect = el("select", {id:"skill-check-report-select","aria-label":tr("选择效果报告", "Choose an evaluation report")});
    const reportDetail = el("div", {id:"skill-check-report-detail"});
    const baseline = el("select", {id:"skill-check-baseline","aria-label":tr("基线报告", "Baseline report")}), candidate = el("select", {id:"skill-check-candidate","aria-label":tr("当前报告", "Candidate report")});
    const comparison = el("div", {id:"skill-check-comparison"});
    const compare = button(tr("比较报告", "Compare reports"), compareReports, "quiet"); compare.id="skill-check-compare";
    const file = el("input", {id:"skill-check-import-file",type:"file",accept:".json,application/json","aria-label":tr("Skill-Up result.json 报告", "Skill-Up result.json report")});
    const importAck = el("input", {id:"skill-check-import-ack",type:"checkbox"});
    const importNote = el("p", {class:"help",id:"skill-check-import-note"});
    const importButton = button(tr("导入报告", "Import report"), importReport, "quiet"); importButton.id="skill-check-import";
    const reload = button(tr("刷新测试记录", "Refresh evaluations"), refresh, "quiet"); reload.id="skill-check-refresh";
    section.append(el("div", {class:"skill-check-heading"}, el("div", {}, el("h3", {}, tr("效果测试（Skill-Up）", "Evaluation (Skill-Up)")), el("p", {class:"help"}, tr("校验只读取用例；运行测试会调用 Agent、脚本与 Judge。结果不替代安全检查。", "Validation reads cases. Evaluation can invoke Agents, scripts and judges. Results do not replace security checks."))), reload),
      !hasSuite ? el("p", {class:"skill-check-notice",role:"status"}, tr("此快照没有 evals/eval.yaml，暂不能校验或运行。仍可导入已有报告查看。", "This snapshot has no evals/eval.yaml, so validation and evaluation are unavailable. Existing reports can still be imported.")) : el("p", {class:"help"}, tr("用例入口：evals/eval.yaml", "Case entry: evals/eval.yaml")),
      el("div", {class:"skill-check-execution"}, el("label", {}, tr("测试环境", "Test environment"),environment), engineNote,
        el("label", {class:"skill-check-choice"}, ack, tr("我确认在所选隔离环境中校验或运行这些用例。", "I confirm validation or evaluation in the selected isolated environment.")),
        el("label", {class:"skill-check-choice"}, network, tr("允许测试容器访问网络（默认关闭）", "Allow network access for the test container (off by default)")),
        el("div", {class:"actions"}, validate,run)), error, jobTarget,
      el("details", {class:"skill-check-import"}, el("summary", {}, tr("导入已有 Skill-Up 报告", "Import an existing Skill-Up report")),
        el("p", {class:"help"}, tr("选择 result.json（最多 4 MiB）；原始提示词和响应不在此显示。", "Choose result.json (up to 4 MiB). Raw prompts and responses are not displayed here.")),file,
        el("label", {class:"skill-check-choice"}, importAck,tr("我确认这是外部未核验报告，仅用于查看和对比。", "I acknowledge that this externally supplied report is unverified and is only for inspection and comparison.")),importNote,importButton),
      el("div", {id:"skill-check-reports",class:"skill-check-reports"}, el("h4", {}, tr("效果报告", "Evaluation reports")),reportSelect,reportDetail),
      el("div", {class:"skill-check-comparison"}, el("h4", {}, tr("对比修改前后", "Compare revisions")),el("div", {class:"skill-check-compare-controls"},el("label", {},tr("基线（可选其他版本）", "Baseline (other revisions allowed)"),baseline),el("label", {},tr("当前快照的报告", "Report for this snapshot"),candidate),compare),comparison));
    function sync() {
      const selected = engines.find(row => row.instanceId === environment.value);
      const cleanupPending=jobs.some(row=>row.cleanupError || row.cleanupPending);
      const blocked = busy || active(currentJob) || cleanupPending || !hasSuite || !available || !selected?.available || !ack.checked;
      validate.disabled = blocked; run.disabled = blocked;
      environment.disabled = busy || active(currentJob) || !engines.length;
      ack.disabled = busy || active(currentJob); network.disabled = busy || active(currentJob);
      importButton.disabled = importBusy || !importAck.checked || importValue === null;
      file.disabled=importBusy; importAck.disabled=importBusy;
      compare.disabled = !baseline.value || !candidate.value || baseline.value === candidate.value;
    }
    function drawJobs() {
      jobTarget.replaceChildren(...jobs.slice(0,10).map(job => {
        const row = el("article", {class:"skill-check-job", "data-check-job":job.id}, el("div", {}, badge(job.status,translated(states,job.status)), " ", job.action === "validate" ? tr("用例校验", "Case validation") : tr("效果测试", "Evaluation"), " · ", job.environmentName || "", " · ", date(job.createdAt)), job.error ? el("p", {class:"error"},job.error) : null);
        if (job.output) row.append(el("details", {}, el("summary", {}, tr("执行输出", "Execution output")),el("pre", {},job.output)));
        if (job.validation) row.append(el("p",{class:"help"}, badge(job.validation.status,job.validation.status==="success"?tr("校验通过", "Validation passed"):job.validation.status==="failed"?tr("校验失败", "Validation failed"):tr("校验不可用", "Validation unavailable")), " · ",tr("用例数：", "Cases: ")+text(job.validation.cases)),...(job.validation.warnings || []).map(message=>el("p",{class:"skill-check-notice"},message)));
        if (job.cleanupError) row.append(el("p",{class:"error",role:"alert"},tr("容器清理尚未确认，新的测试暂不可启动：", "Container cleanup is unconfirmed. New evaluations are blocked: ")+job.cleanupError),button(tr("重试清理", "Retry cleanup"),()=>cancelJob(job),"quiet"));
        else if (["queued","running"].includes(job.status)) row.append(button(tr("取消测试", "Cancel evaluation"), () => cancelJob(job), "quiet"));
        if (job.cleanupPending && !job.cleanupError) row.append(el("p",{class:"skill-check-notice",role:"status"},tr("正在确认容器清理，新的测试暂不可启动。", "Container cleanup is being confirmed. New evaluations remain blocked.")));
        if (job.status === "completed" && job.action === "run" && !job.reportId) row.append(el("p", {class:"skill-check-notice"},tr("运行结束但没有有效报告，不能视为用例通过。", "The run ended without a valid report. This is not a passing result.")));
        return row;
      }));
      if (jobs.length > 10) jobTarget.append(el("p", {class:"help"},tr("仅显示最近 10 项任务。", "Only the 10 most recent jobs are shown.")));
    }
    function optionRows(select, values, preferred) {
      select.replaceChildren(...values.slice(0,100).map(row => el("option", {value:row.id}, reportLabel(row))));
      if (values.some(row=>row.id===preferred)) select.value=preferred;
      if (!values.length) select.append(el("option", {value:""}, tr("暂无报告", "No reports")));
    }
    function drawRecords(preferred) {
      const oldBaseline=baseline.value, oldCandidate=candidate.value;
      if (preferred) selectedReport=preferred;
      optionRows(reportSelect,records,selectedReport); selectedReport=reportSelect.value;
      showReport(selectedReport);
      optionRows(candidate,records,oldCandidate || selectedReport);
      const name=records.find(row=>row.id===candidate.value)?.report?.skillName;
      const sorted=[...baselines].sort((a,b)=>Number(b.report?.skillName===name)-Number(a.report?.skillName===name));
      const best=oldBaseline || sorted.find(row=>row.id!==candidate.value)?.id;
      optionRows(baseline,sorted,best); sync();
    }
    async function showReport(id) {
      const sequence=++detailSequence;
      if (!id) { renderReport(reportDetail,null); return; }
      reportDetail.replaceChildren(el("p",{class:"help"},tr("正在读取逐用例结果…", "Loading case results…")));
      try {
        const record=await api(base+"/check-reports/"+enc(id));
        if (!current() || sequence!==detailSequence || selectedReport!==id) return;
        if (record.id!==id || record.packId!==packId || record.digest!==digest || record.casesOmitted) throw Error(tr("报告身份与当前技能快照不一致，或完整报告尚未加载。", "The report identity differs from this snapshot, or the complete report has not loaded."));
        renderReport(reportDetail,record);
      } catch(problem) { if(current() && sequence===detailSequence) reportDetail.replaceChildren(el("p",{class:"error"},errorText(problem)),button(tr("重试读取报告", "Retry reading report"),()=>showReport(id),"quiet")); }
    }
    async function loadRecords(preferred) {
      const sequence=++reportSequence;
      const [local,all]=await Promise.all([api(path+"/check-reports"),api(base+"/check-reports")]);
      if (!current() || sequence!==reportSequence) return;
      records=local.items || []; baselines=all.items || []; drawRecords(preferred);
    }
    async function loadJobs() {
      const value=await api(path+"/checks");
      if (!current()) return;
      jobs=value.items || []; currentJob=jobs.find(active) || null; drawJobs(); sync();
      if (currentJob) schedule();
    }
    function schedule() { clearTimeout(timer); if (current() && currentJob) timer=setTimeout(poll,1000); }
    async function poll() {
      if (!current() || !currentJob) return;
      const id=currentJob.id;
      try {
        const value=await api(base+"/check-jobs/"+enc(id));
        if (!current() || currentJob?.id!==id) return;
        currentJob=value; jobs=[value,...jobs.filter(row=>row.id!==id)]; drawJobs(); sync();
        if (active(value)) schedule();
        else { currentJob=null; sync(); await loadRecords(value.reportId); }
      } catch (error) {
        if (current()) { notice(errorNode(),errorText(error),"error"); jobTarget.append(button(tr("重试读取任务", "Retry reading job"),poll,"quiet")); }
      }
    }
    function errorNode() { return error; }
    async function cancelJob(job) {
      if (!current() || !active(job) && !job.cleanupError) return;
      try {
        const value=await api(base+"/check-jobs/"+enc(job.id)+"/cancel",{method:"POST",body:{}});
        if (!current()) return;
        jobs=[value,...jobs.filter(row=>row.id!==job.id)]; currentJob=active(value)?value:null; clearTimeout(timer); notice(error,""); drawJobs(); sync();
        if (currentJob) schedule(); else await loadRecords(value.reportId);
      } catch (problem) { if (current()) notice(error,errorText(problem),"error"); }
    }
    async function start(action) {
      if (!current() || busy || active(currentJob) || jobs.some(row=>row.cleanupError || row.cleanupPending) || !hasSuite || !available || !ack.checked || !engines.find(row=>row.instanceId===environment.value)?.available) return;
      const binding={digest,action,instanceId:environment.value,allowNetwork:network.checked,acknowledgeExecution:true};
      busy=true; sync(); notice(error,"");
      try {
        const result=await api(path+"/checks",{method:"POST",body:binding});
        if (!current()) return;
        const job=result.job;
        if (!job?.id) throw Error(tr("服务未返回测试任务。", "The service did not return an evaluation job."));
        jobs=[job,...jobs.filter(row=>row.id!==job.id)]; currentJob=active(job)?job:null; drawJobs();
        if (currentJob) schedule(); else await loadRecords(job.reportId);
      } catch (problem) { if (current()) notice(error,errorText(problem),"error"); }
      finally { busy=false; if (current()) sync(); }
    }
    async function readFile(value) {
      if (value.text) return value.text();
      return new Promise((resolve,reject)=>{ const reader=new FileReader(); reader.onload=()=>resolve(reader.result); reader.onerror=()=>reject(Error(tr("无法读取文件。", "Cannot read the file."))); reader.readAsText(value); });
    }
    file.addEventListener("change",async()=>{
      const sequence=++fileSequence;
      importValue=null; importAck.checked=false; sync(); notice(importNote,"");
      const selected=file.files?.[0]; if (!selected) return;
      if (selected.size>maxReport) { notice(importNote,tr("报告超过 4 MiB 限制。", "The report exceeds the 4 MiB limit."),"error"); return; }
      try {
        const value=JSON.parse(await readFile(selected));
        if (!current() || sequence!==fileSequence || file.files?.[0]!==selected) return;
        if (!value || typeof value!=="object" || Array.isArray(value)) throw Error(tr("请选择 Skill-Up result.json 对象。", "Choose a Skill-Up result.json object."));
        importValue=value; notice(importNote,tr("已读取：", "Read: ")+selected.name); sync();
      } catch (problem) { if (current() && sequence===fileSequence) notice(importNote,errorText(problem),"error"); }
    });
    async function importReport() {
      if (!current() || importBusy || !importAck.checked || importValue===null) return;
      importBusy=true; sync(); notice(error,"");
      const body={digest,report:importValue,acknowledgeImport:true};
      try {
        const record=await api(path+"/check-reports",{method:"POST",body});
        if (!current()) return;
        if (!record?.id) throw Error(tr("服务未返回保存的报告。", "The service did not return the saved report."));
        importValue=null; importAck.checked=false; file.value=""; notice(importNote,tr("报告已导入，执行来源仍标记为外部未核验。", "The report was imported. Its execution provenance remains externally supplied and unverified."));
        await loadRecords(record.id);
      } catch (problem) { if (current()) notice(error,errorText(problem),"error"); }
      finally { importBusy=false; if (current()) sync(); }
    }
    async function compareReports() {
      if (!current() || compare.disabled) return;
      const before=baseline.value,after=candidate.value,sequence=++compareSequence; compare.disabled=true; notice(error,"");
      try {
        const value=await api(path+"/check-compare?baseline="+enc(before)+"&candidate="+enc(after));
        if (!current() || sequence!==compareSequence || baseline.value!==before || candidate.value!==after) return;
        renderComparison(comparison,value);
      } catch (problem) { if (current()) notice(error,errorText(problem),"error"); }
      finally { if (current()) sync(); }
    }
    async function refresh() {
      if (!current()) return;
      notice(error,"");
      const results=await Promise.allSettled([loadJobs(),loadRecords(),(async()=>{
        const result=await api(base+"/check-engines");
        if (!current()) return;
        available=result.available===true; engines=result.items || [];
        const old=environment.value;
        environment.replaceChildren(...engines.map(row=>el("option",{value:row.instanceId,disabled:!row.available},row.name || row.instanceId)));
        if (engines.some(row=>row.instanceId===old && row.available)) environment.value=old;
        else environment.value=engines.find(row=>row.available)?.instanceId || "";
        notice(engineNote,result.message || (!available ? tr("测试环境不可用；可以导入和查看报告。", "Evaluation environments are unavailable. Reports can still be imported and inspected.") : tr("测试使用独立 Docker 容器。", "Evaluation uses a separate Docker container.")));
        sync();
      })()]);
      if (!current()) return;
      const failures=results.filter(row=>row.status==="rejected");
      if (failures.length) notice(error,failures.map(row=>errorText(row.reason)).join("\n"),"error");
    }
    environment.addEventListener("change",()=>{ const value=engines.find(row=>row.instanceId===environment.value); if(value?.message) notice(engineNote,value.message); sync(); });
    ack.addEventListener("change",sync); network.addEventListener("change",sync); importAck.addEventListener("change",sync);
    reportSelect.addEventListener("change",()=>{selectedReport=reportSelect.value;showReport(selectedReport);});
    for(const selector of [baseline,candidate]) selector.addEventListener("change",()=>{++compareSequence;comparison.replaceChildren();sync();});
    sync(); renderReport(reportDetail,null); refresh();
    return {dispose,refresh};
  }
  return {mount,dispose:()=>disposeCurrent?.()};
})();
