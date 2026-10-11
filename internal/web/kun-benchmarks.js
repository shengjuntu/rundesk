window.RunDeskKunBenchmarks = (() => {
  function create({candidate, isActive = () => true}) {
    const node = el('section', {}), notice = el('p', {role: 'status'});
    const listing = el('section', {}), batchNode = el('section', {}), reportNode = el('section', {});
    const title = el('input', {'aria-label': '对照标题', value: candidate.title + ' 重复对照'});
    const repetitions = el('input', {type: 'number', min: 2, max: 5, value: 2, 'aria-label': '每例重复次数'});
    const readID = el('input', {'aria-label': '对照批次 ID'});
    let busy = false, ticket = 0, batch = null;
    const buttons = [];
    function action(label, fn) {
      const b = button(label, () => run(fn));
      buttons.push(b);
      b.disabled = busy;
      return b;
    }
    async function run(fn) {
      if (busy || !isActive()) return;
      busy = true;
      const t = ++ticket;
      buttons.forEach(b => b.disabled = true);
      try { await fn(() => isActive() && t === ticket); }
      catch (e) { if (isActive() && t === ticket) notice.textContent = e.message; }
      finally { busy = false; if (isActive()) buttons.forEach(b => b.disabled = false); }
    }
    const fmt = v => v == null ? '未知' : Number(v).toLocaleString('zh-CN', {maximumFractionDigits: 2});
    function metric(v) { return v ? fmt(v.mean) + '（已知 ' + v.count + '，缺失 ' + v.missing + '）' : '未知'; }
    function report(v) {
      if (!batch || v.benchmarkId !== batch.id || v.benchmarkHash !== batch.hash || v.candidateId !== candidate.id || v.candidateHash !== candidate.hash) throw Error('对照报告与当前批次不匹配。');
      const row = (label, arm) => el('tr', {}, el('th', {}, label), ...[
        arm.passed + ' / ' + arm.total, arm.failed, arm.unknown,
        arm.passRate == null ? '未知' : fmt(arm.passRate) + '%', arm.fastCompleted, arm.fallbacks,
        metric(arm.metrics.modelCalls), metric(arm.metrics.reportedTokens), metric(arm.metrics.runMillis)
      ].map(value => el('td', {}, String(value))));
      reportNode.replaceChildren(el('h4', {}, '对照报告 · ' + (v.status === 'complete' ? '记录完整' : '记录未完整')),
        el('p', {}, '快速路径回退后成功也计为通过。此报告用于比较，不能启用候选。'),
        el('div', {style: 'overflow-x:auto'}, el('table', {},
          el('thead', {}, el('tr', {}, ...['执行组', '通过', '失败', '未知', '通过率', '快速完成', '回退', '平均模型调用', '平均已报告 token', '平均运行毫秒'].map(x => el('th', {}, x)))),
          el('tbody', {}, row('动态执行', v.dynamic), row('Skill 快速路径', v.fast)))),
        el('p', {}, '配对平均差（快速 − 动态）：模型调用 ' + metric(v.pairedDelta.modelCalls) + '；token ' + metric(v.pairedDelta.reportedTokens) + '；运行毫秒 ' + metric(v.pairedDelta.runMillis)),
        ...v.warnings.map(x => el('p', {class: 'help'}, x)),
        el('details', {}, el('summary', {}, '逐次结果、配对与固定证据'), el('pre', {}, JSON.stringify(v, null, 2))));
    }
    function show(view) {
      const b = view.benchmark, x = view.execution;
      if (b.candidateId !== candidate.id || b.candidateHash !== candidate.hash) throw Error('对照批次与候选不匹配。');
      const changed = !batch || batch.id !== b.id || batch.hash !== b.hash;
      batch = b;
      readID.value = b.id;
      if (changed) reportNode.replaceChildren();
      const confirm = el('input', {type: 'checkbox', 'aria-label': '确认重复对照真实执行'});
      const reports = el('section', {}), detail = el('section', {});
      async function history(offset, alive) {
        const page = await api('/kun-benchmark-reports?' + new URLSearchParams({benchmarkId: b.id, offset, limit: 20}));
        if (!alive()) return;
        reports.replaceChildren(...page.items.map(v => action(v.createdAt + ' · 报告 ' + v.id, async live => {
          const value = await api('/kun-benchmark-reports/' + encodeURIComponent(v.id));
          if (live()) report(value);
        })));
        if (offset > 0) reports.append(action('较新的对照报告', live => history(Math.max(0, offset - 20), live)));
        if (page.hasMore) reports.append(action('更早的对照报告', live => history(page.nextOffset, live)));
      }
      const plan = el('select', {'aria-label': '查看对照案例'}, ...b.plans.map((p, n) => el('option', {value: p.planId}, (n + 1) + ' · ' + p.caseKey + ' · 第 ' + p.sample + ' 次 · ' + p.arm)));
      plan.value = b.plans[0]?.planId || '';
      const status = {planned:'尚未执行', running:'执行中', waiting:'等待并发名额', stopped:'已停止后续调度', interrupted:'宿主中断', blocked:'需要检查案例', completed:'执行完毕'}[x.status] || x.status;
      batchNode.replaceChildren(el('h4', {}, b.title), el('p', {}, status + ' · 已越过 ' + x.next + ' / ' + x.total + ' 次运行'),
        el('p', {}, x.error || ''), el('p', {}, '固定批次 ' + b.id + ' · 当前会话 ' + (x.sessionId || '无')),
        el('details', {}, el('summary', {}, '核对全部输入、规则、重复次数、顺序和环境'), el('pre', {}, JSON.stringify(b, null, 2))),
        el('label', {}, '逐次执行预览', plan), action('读取所选对照案例', async alive => {
          const p = b.plans.find(x => x.planId === plan.value); if (!p) throw Error('请选择案例。');
          const v = await api('/kun-case-runs/' + encodeURIComponent(p.planId));
          if (alive()) { if (v.preview.hash !== p.planHash || v.preview.benchmarkId !== b.id) throw Error('案例预览绑定不匹配。'); detail.replaceChildren(el('pre', {}, JSON.stringify(v, null, 2))); }
        }), detail,
        el('label', {}, confirm, '我已核对整个批次，确认 ' + b.plans.length + ' 次运行会在同一工作区重复执行真实工具'),
        action('开始或继续整个对照批次', async alive => {
          if (batch !== b || !confirm.checked) throw Error('请先核对整个批次并勾选确认。');
          const v = await api('/kun-benchmarks/' + encodeURIComponent(b.id) + '/start', {method:'POST', body:{expectedHash:b.hash, confirmExecute:true}});
          if (alive()) { show(v); notice.textContent = '批次将顺序执行，关闭窗口不停止。审批仍在对应会话处理。'; }
        }),
        action('停止后续对照调度', async alive => {
          const v = await api('/kun-benchmarks/' + encodeURIComponent(b.id) + '/stop', {method:'POST', body:{expectedHash:b.hash}});
          if (alive()) { show(v); notice.textContent = '不再启动后续案例。已开始的运行继续；可在对应会话停止。'; }
        }),
        action('刷新对照进度', async alive => { const v = await api('/kun-benchmarks/' + encodeURIComponent(b.id)); if (alive()) { if (v.benchmark.id !== b.id || v.benchmark.hash !== b.hash) throw Error('批次身份变化。'); show(v); } }),
        action('汇总并保存对照报告', async alive => {
          const v = await api('/kun-benchmarks/' + encodeURIComponent(b.id) + '/evaluate', {method:'POST', body:{expectedHash:b.hash}});
          if (alive()) report(v);
        }), action('读取对照报告记录', alive => history(0, alive)), reports);
    }
    async function list(offset, alive) {
      const page = await api('/kun-benchmarks?' + new URLSearchParams({candidateId:candidate.id, offset, limit:20}));
      if (!alive()) return;
      listing.replaceChildren(...page.items.map(v => action(v.title + ' · ' + v.createdAt, async live => { const x = await api('/kun-benchmarks/' + encodeURIComponent(v.id)); if (live()) { if (x.benchmark.id !== v.id) throw Error('批次身份不匹配。'); show(x); } })));
      if (offset > 0) listing.append(action('较新的对照批次', live => list(Math.max(0, offset - 20), live)));
      if (page.hasMore) listing.append(action('更早的对照批次', live => list(page.nextOffset, live)));
    }
    node.append(el('h3', {}, '多次采样对照'),
      el('p', {class:'help'}, '在该候选的固定验证集上，对比动态执行与快速路径。每例重复 2–5 次，总计最多 100 次。顺序交替，文件和外部服务不会自动重置。'),
      el('label', {}, '标题', title), el('label', {}, '每例重复次数', repetitions),
      action('生成固定对照预览', async alive => {
        const n = Number(repetitions.value); if (!Number.isInteger(n) || n < 2 || n > 5) throw Error('每例重复次数须为 2–5 的整数。');
        const v = await api('/kun-benchmarks', {method:'POST', body:{title:title.value, candidateId:candidate.id, expectedCandidateHash:candidate.hash, repetitions:n}});
        if (alive()) { show(v); notice.textContent = '所有案例与顺序已固定，尚未执行。'; }
      }), action('读取对照批次记录', alive => list(0, alive)), listing,
      el('label', {}, '批次 ID', readID), action('读取对照批次', async alive => { const id = readID.value.trim(), v = await api('/kun-benchmarks/' + encodeURIComponent(id)); if (alive()) { if (v.benchmark.id !== id) throw Error('批次身份不匹配。'); show(v); } }),
      notice, batchNode, reportNode);
    return {node};
  }
  return {create};
})();
