/* Full-window trace workspace; no frontend build or runtime dependencies. */
"use strict";
(function () {
  const { Model, TRACKS, bad, duration, orderRows, nextRow } = RunDeskTrace;
  // Use individual CSSOM properties; RunDesk's CSP intentionally blocks style attributes.
  function te(tag, attrs = {}, ...children) {
    const styles = attrs.style,
      clean = { ...attrs };
    delete clean.style;
    const n = el(tag, clean, ...children);
    if (styles)
      for (const pair of styles.split(";")) {
        const at = pair.indexOf(":");
        if (at > 0)
          n.style.setProperty(
            pair.slice(0, at).trim(),
            pair.slice(at + 1).trim(),
          );
      }
    return n;
  }

  const labels = {
    completed: "完成",
    running: "进行中",
    inProgress: "进行中",
    pending: "等待确认",
    accepted: "已允许",
    resolved: "已处理",
    declined: "已拒绝",
    failed: "失败",
    interrupted: "已中断",
    expired: "已失效",
    warning: "告警",
    unknown: "时间不完整",
    cancelled: "已取消",
  };
  const colors = {
    input: "#87adff",
    model: "#c2a2ff",
    tools: "#57d4bf",
    approval: "#ffca79",
    context: "#dcaff1",
    system: "#ff98a6",
  };
  let model = new Model(),
    sid = null,
    cursor = 0,
    epoch = 0,
    busy = false,
    opened = false,
    run = "all",
    selected = null,
    view = null,
    follow = true,
    mode = "time",
    query = "",
    listOrder = "time",
    lastSessionStatus = "",
    runOptionsSignature = "",
    errorsOnly = false,
    tracks = new Set(TRACKS.map((x) => x[0])),
    detailKey = "",
    refreshTimer = null,
    restorePosition = true;
  const root = te("section", {
    id: "trace-workspace",
    class: "trace-workspace hidden",
    "aria-label": "运行轨迹工作台",
  });
  root.innerHTML = `<header class="trace-head"><div class="trace-brand"><span class="brandmark">r.</span><div><p class="eyebrow">RUN TRACE</p><h1>运行轨迹 <span id="trace-session-title"></span></h1></div></div><div class="trace-head-actions"><span id="trace-demo" class="badge hidden">DEMO · 模拟事件</span><button id="trace-refresh">刷新</button><button id="trace-focus" aria-pressed="false">专注模式</button><button id="trace-close">返回对话 ↗</button></div></header>
<div class="trace-toolbar"><label>运行 <select id="trace-run" aria-label="选择运行"><option value="all">全部运行</option></select></label><input id="trace-search" type="search" placeholder="查找命令、工具或内容…" aria-label="搜索轨迹"/><label class="trace-check"><input id="trace-errors" type="checkbox"/>仅异常</label><details class="trace-layer-menu"><summary>轨道 ▾</summary><div id="trace-layers"></div></details><div class="trace-modes" role="group" aria-label="排列方式"><button id="trace-time" aria-pressed="true">时间比例</button><button id="trace-steps" aria-pressed="false">按步骤</button></div><span id="trace-load" role="status"></span></div>
<div class="trace-stats" id="trace-stats"></div>
<div class="trace-body"><div class="trace-main"><section class="trace-timeline-card"><div class="trace-section-head"><strong>全程总览</strong><span id="trace-range-label">拖动选区定位 · 滚轮缩放</span></div><div class="trace-overview" id="trace-overview"><canvas aria-label="全程轨迹总览"></canvas><div id="trace-window"><i class="trace-grip left" data-edge="left"></i><i class="trace-grip right" data-edge="right"></i></div></div><div class="trace-axis-head"><span>执行轨道</span><div id="trace-axis"></div></div><div id="trace-lanes"></div><p class="trace-timing-note">区间按原生事件时间或后台接收时间绘制；虚线表示端点不完整。模型轨道仅展示接口公开的阶段。</p></section><section class="trace-list-card"><div class="trace-section-head"><strong>步骤索引 <span id="trace-count"></span></strong><div class="trace-list-actions"><select id="trace-order" aria-label="步骤排序"><option value="time">发生顺序</option><option value="duration">耗时从长到短</option></select><button id="trace-longest" title="只比较完整起止区间">定位最长步骤</button></div></div><div class="trace-list-head"><span>时间</span><span>步骤 / 内容</span><span>状态</span><span>耗时</span></div><div id="trace-list" tabindex="0" aria-label="轨迹步骤列表"><div id="trace-list-inner"></div></div></section></div><aside id="trace-detail" class="trace-detail"><div class="trace-detail-head"><strong>步骤详情</strong><button id="trace-detail-close" aria-label="收起步骤详情">×</button></div><div id="trace-detail-content"><div class="trace-empty">选择一个步骤<br/><small>时间轴与列表会同步定位。</small></div></div></aside></div>
<footer class="trace-footer"><div><button id="trace-prev" title="上一事件，快捷键 ←">← 上一个</button><button id="trace-next" title="下一事件，快捷键 →">下一个 →</button><button id="trace-prev-error">上一异常</button><button id="trace-next-error">下一异常</button></div><span id="trace-position"></span><div><button id="trace-zoom-out" aria-label="缩小时间轴">−</button><button id="trace-zoom-in" aria-label="放大时间轴">＋</button><button id="trace-fit">适应全程</button><button id="trace-follow" aria-pressed="true">跟随实时 ●</button><button id="trace-export">导出轨迹</button></div></footer>`;
  document.body.append(root);
  const q = (s) => root.querySelector(s),
    dur = (n) =>
      n == null
        ? "未提供"
        : n < 1000
          ? Math.max(0, n).toFixed(0) + " ms"
          : n < 60000
            ? (n / 1000).toFixed(2) + " s"
            : (n / 60000).toFixed(1) + " min";
  const relative = (t) => dur(t - model.bounds(run)[0]);
  const rowTime = (x) => x.start ?? x.end;
  function save() {
    if (sid)
      sessionStorage.setItem(
        "rundesk-trace-" + sid,
        JSON.stringify({
          run,
          selected,
          view,
          follow,
          mode,
          query,
          listOrder,
          errorsOnly,
          tracks: [...tracks],
        }),
      );
  }
  function visible() {
    return model
      .list(run)
      .filter(
        (x) =>
          tracks.has(x.track) &&
          (!errorsOnly || bad(x.status) || x.status === "warning") &&
          (!query ||
            [x.title, x.body, x.type, x.itemId]
              .join(" ")
              .toLowerCase()
              .includes(query.toLowerCase())),
      );
  }
  function extent() {
    return mode === "steps"
      ? [0, Math.max(1, visible().length)]
      : model.bounds(run);
  }
  function fit() {
    view = extent();
    save();
    render();
  }
  function clamp(v, b = extent()) {
    let width = Math.max(
      mode === "time" ? 1 : 0.1,
      Math.min(v[1] - v[0], b[1] - b[0]),
    );
    let left = Math.max(b[0], Math.min(v[0], b[1] - width));
    return [left, left + width];
  }
  function frame() {
    return view ? clamp(view) : extent();
  }
  function setFollow(v) {
    follow = v;
    q("#trace-follow").textContent = v ? "跟随实时 ●" : "跟随实时";
    q("#trace-follow").setAttribute("aria-pressed", String(v));
    save();
  }
  function zoom(factor, anchor = 0.5) {
    const b = frame(),
      width = (b[1] - b[0]) * factor,
      at = b[0] + (b[1] - b[0]) * anchor;
    view = clamp([at - width * anchor, at + width * (1 - anchor)]);
    setFollow(false);
    render();
  }
  function renderStats() {
    const s = model.stats(run),
      b = s.bounds;
    const cards = [
      ["观测跨度", model.runs.length ? dur(b[1] - b[0]) : "—"],
      ["审批等待", dur(s.wait)],
      ["异常步骤", String(s.errors)],
      [
        "线程累计 Token",
        s.tokens?.total?.totalTokens == null
          ? "未提供"
          : Number(s.tokens.total.totalTokens).toLocaleString(),
      ],
    ];
    q("#trace-stats").replaceChildren(
      ...cards.map(([name, value]) =>
        te("div", {}, te("span", {}, name), te("strong", {}, value)),
      ),
    );
    q("#trace-stats").append(
      te(
        "p",
        {},
        "累计 Token 并非上下文占用；审批等待为已闭合区间的合并时长。",
      ),
    );
  }
  function renderRuns() {
    const select = q("#trace-run");
    const signature = JSON.stringify([
      run,
      model.runs.map((r) => [r.id, r.title, r.status]),
    ]);
    if (runOptionsSignature === signature) return;
    runOptionsSignature = signature;
    select.replaceChildren(
      te("option", { value: "all" }, "全部运行 · " + model.runs.length),
      ...model.runs.map((r, i) =>
        te(
          "option",
          { value: r.id },
          `${String(i + 1).padStart(2, "0")} · ${r.title.slice(0, 55)} · ${labels[r.status] || r.status}`,
        ),
      ),
    );
    select.value = run;
    if (!select.value) {
      select.append(te("option", { value: run }, "正在恢复运行选择…"));
      select.value = run;
    }
  }
  function boxes(rows) {
    const b = frame(),
      den = b[1] - b[0],
      last = model.lastTime;
    return rows.map((row, i) => {
      const start = mode === "steps" ? i : (row.start ?? row.end),
        end = mode === "steps" ? i + 0.85 : model.endpoint(row);
      return {
        row,
        start,
        end,
        left: ((start - b[0]) / den) * 100,
        width: Math.max(
          mode === "steps" ? 0 : 0.3,
          ((end - start) / den) * 100,
        ),
      };
    });
  }
  function renderTimeline(rows) {
    const b = frame(),
      bounds = extent(),
      width = b[1] - b[0];
    q("#trace-range-label").textContent =
      mode === "steps"
        ? "按步骤排列 · 宽度不代表耗时"
        : `${relative(b[0])} — ${relative(b[1])} · 滚轮缩放 / 拖动平移`;
    q("#trace-axis").replaceChildren(
      ...Array.from({ length: 6 }, (_, i) =>
        te(
          "span",
          { style: `left:${i * 20}%` },
          mode === "steps"
            ? Math.floor(b[0] + (width * i) / 5) + 1
            : relative(b[0] + (width * i) / 5),
        ),
      ),
    );
    const items = boxes(rows);
    const lanes = q("#trace-lanes");
    lanes.replaceChildren();
    for (const [track, label] of TRACKS) {
      if (!tracks.has(track)) continue;
      const line = te("div", { class: "trace-lane" }),
        content = te("div", {
          class: "trace-lane-content",
          "data-track": track,
        });
      line.append(
        te(
          "div",
          { class: "trace-lane-label" },
          te("i", { style: "background:" + colors[track] }),
          label,
        ),
        content,
      );
      const candidates = items.filter(
        (x) => x.row.track === track && x.left + x.width >= 0 && x.left <= 100,
      );
      const right = [];
      let hidden = 0;
      for (const box of candidates) {
        const { row } = box,
          left = Math.max(0, box.left),
          w = Math.min(100 - left, box.width + Math.min(0, box.left));
        let level = right.findIndex((x) => x < left);
        if (level < 0) level = right.length;
        if (level >= 3) {
          hidden++;
          continue;
        }
        right[level] = left + Math.max(w, 2);
        const node = te(
          "button",
          {
            class:
              "trace-block " +
              (row.id === selected ? "selected " : "") +
              (row.incomplete ? "incomplete " : "") +
              (bad(row.status) ? "bad " : ""),
            style: `left:${left}%;width:max(10px,${w}%);top:${level * 26 + 7}px;--track:${colors[track]}`,
            "data-row": row.id,
            title: `${row.title} · ${labels[row.status] || row.status}`,
            "aria-label": `${label}：${row.title}，${labels[row.status] || row.status}`,
            onclick: () => choose(row.id),
          },
          te(
            "span",
            {},
            (bad(row.status) ? "! " : row.status === "pending" ? "◷ " : "") +
              row.title,
          ),
        );
        content.append(node);
      }
      const chosen = items.find((x) => x.row.id === selected);
      if (chosen && chosen.left >= 0 && chosen.left <= 100)
        content.append(
          te("i", { class: "trace-playhead", style: `left:${chosen.left}%` }),
        );
      content.style.height =
        Math.max(42, Math.min(3, right.length) * 26 + 14) + "px";
      if (hidden)
        content.append(
          te("span", { class: "trace-density" }, `+${hidden} 步 · 缩放查看`),
        );
      lanes.append(line);
    }
    drawOverview(rows, bounds, b);
  }
  function drawOverview(rows, bounds, b) {
    const canvas = q("#trace-overview canvas"),
      rect = canvas.getBoundingClientRect(),
      dpr = window.devicePixelRatio || 1;
    canvas.width = Math.max(1, rect.width * dpr);
    canvas.height = 64 * dpr;
    const ctx = canvas.getContext("2d");
    ctx.scale(dpr, dpr);
    ctx.clearRect(0, 0, rect.width, 64);
    const span = bounds[1] - bounds[0];
    rows.forEach((row, i) => {
      const a = mode === "steps" ? i : (row.start ?? row.end),
        z = mode === "steps" ? i + 0.8 : model.endpoint(row),
        x = ((a - bounds[0]) / span) * rect.width,
        w = Math.max(2, ((z - a) / span) * rect.width);
      ctx.fillStyle = bad(row.status) ? "#e56d72" : colors[row.track];
      ctx.fillRect(
        x,
        TRACKS.findIndex((t) => t[0] === row.track) * 9 + 5,
        w,
        5,
      );
    });
    const chosen = rows.findIndex((x) => x.id === selected);
    if (chosen >= 0) {
      const row = rows[chosen],
        at = mode === "steps" ? chosen : (row.start ?? row.end),
        x = ((at - bounds[0]) / span) * rect.width;
      ctx.strokeStyle = "#fff";
      ctx.lineWidth = 2;
      ctx.beginPath();
      ctx.moveTo(x, 0);
      ctx.lineTo(x, 64);
      ctx.stroke();
    }
    const win = q("#trace-window");
    win.style.left = ((b[0] - bounds[0]) / span) * 100 + "%";
    win.style.width = Math.max(0.5, ((b[1] - b[0]) / span) * 100) + "%";
  }
  let cachedRows = [];
  function renderList() {
    const viewport = q("#trace-list"),
      inner = q("#trace-list-inner"),
      height = 54,
      rows = cachedRows;
    const from = Math.max(0, Math.floor(viewport.scrollTop / height) - 5),
      to = Math.min(
        rows.length,
        from + Math.ceil((viewport.clientHeight || 250) / height) + 12,
      );
    inner.style.height = rows.length * height + "px";
    inner.replaceChildren();
    if (!rows.length) {
      inner.style.height = "150px";
      inner.append(
        te(
          "div",
          { class: "trace-empty" },
          model.runs.length
            ? "没有匹配的步骤。调整筛选或显示其他轨道。"
            : "这个会话还没有运行记录。",
        ),
      );
      return;
    }
    for (let i = from; i < to; i++) {
      const row = rows[i],
        n = te(
          "button",
          {
            class: "trace-step " + (row.id === selected ? "selected" : ""),
            style: `top:${i * height}px`,
            "data-row": row.id,
            onclick: () => choose(row.id),
          },
          te("span", { class: "trace-step-time" }, relative(rowTime(row))),
          te(
            "span",
            { class: "trace-step-title" },
            te("i", { style: "background:" + colors[row.track] }),
            te(
              "span",
              {},
              te("strong", {}, row.title),
              te(
                "small",
                {},
                String(row.body || "")
                  .replace(/\s+/g, " ")
                  .slice(0, 150),
              ),
            ),
          ),
          te(
            "span",
            {
              class:
                "trace-state " +
                (bad(row.status)
                  ? "is-error"
                  : row.status === "pending"
                    ? "is-wait"
                    : ""),
            },
            labels[row.status] || row.status,
          ),
          te(
            "span",
            { class: "trace-step-duration" },
            row.point
              ? "—"
              : row.start != null && row.end != null && !row.timeConflict
                ? dur(row.end - row.start)
                : ["pending", "running", "inProgress"].includes(row.status)
                  ? "观测中"
                  : "端点不完整",
          ),
        );
      inner.append(n);
    }
  }
  function choose(id, scroll = true) {
    selected = id;
    detailKey = "";
    root.classList.remove("focus-mode");
    q("#trace-focus").setAttribute("aria-pressed", false);
    root.classList.add("show-detail");
    const row = model.rows.get(id);
    if (!row) return;
    setFollow(false);
    const rows = visible(),
      index = rows.findIndex((x) => x.id === id);
    const b = frame(),
      at = mode === "steps" ? index : rowTime(row);
    if (at < b[0] || at > b[1]) {
      const width = b[1] - b[0];
      view = clamp([at - width * 0.25, at + width * 0.75]);
    }
    const listIndex = orderRows(rows, listOrder).findIndex((x) => x.id === id);
    if (scroll && listIndex >= 0)
      q("#trace-list").scrollTop = Math.max(
        0,
        listIndex * 54 - q("#trace-list").clientHeight / 2,
      );
    save();
    render();
  }
  function detail() {
    const row = model.rows.get(selected);
    if (!row) {
      q("#trace-detail-content").replaceChildren(
        te("div", { class: "trace-empty" }, "选择一个步骤查看详情。"),
      );
      return;
    }
    const key = JSON.stringify([
      row.id,
      row.status,
      row.refs,
      row.body,
      row.end,
    ]);
    if (key === detailKey) return;
    detailKey = key;
    const content = q("#trace-detail-content");
    content.replaceChildren(
      te(
        "p",
        { class: "trace-detail-track" },
        TRACKS.find((x) => x[0] === row.track)?.[1] || row.track,
      ),
      te("h2", {}, row.title),
      te(
        "span",
        { class: "trace-state " + (bad(row.status) ? "is-error" : "") },
        labels[row.status] || row.status,
      ),
    );
    const meta = te("dl", { class: "trace-meta" });
    for (const [name, value] of [
      [
        "开始",
        row.start == null
          ? "未提供"
          : new Date(row.start).toLocaleTimeString("zh-CN", { hour12: false }),
      ],
      [
        "耗时",
        row.point
          ? "瞬时事件"
          : row.start != null && row.end != null && !row.timeConflict
            ? dur(row.end - row.start)
            : ["pending", "running", "inProgress"].includes(row.status)
              ? "观测中"
              : "端点不完整",
      ],
      [
        "时间依据",
        row.timeSource +
          (row.endSource && row.endSource !== row.timeSource
            ? " → " + row.endSource
            : ""),
      ],
      ["Item ID", row.itemId],
      ["退出码", row.exitCode],
    ])
      if (value != null) meta.append(te("dt", {}, name), te("dd", {}, value));
    content.append(meta);
    if (row.incomplete)
      content.append(
        te(
          "p",
          { class: "trace-detail-note" },
          "缺少完整的开始或结束事件；虚线范围仅表示已观测部分，不计为完整耗时。",
        ),
      );
    if (row.note)
      content.append(te("p", { class: "trace-detail-note" }, row.note));
    if (row.body)
      content.append(
        te("h3", {}, row.track === "tools" ? "输出 / 结果" : "内容"),
        te("pre", { class: "trace-readable" }, row.body),
      );
    if (row.detail) {
      const d = te(
        "details",
        { class: "trace-json" },
        te("summary", {}, "结构化输入与结果"),
        te("pre", {}, json(row.detail)),
      );
      content.append(d);
    }
    const sources = te(
      "div",
      { class: "trace-sources" },
      te("h3", {}, "原始事件"),
      te("p", {}, "预览可能截断；点击读取对应完整记录。"),
    );
    for (const id of row.refs)
      sources.append(
        button(
          "#" + id,
          async () => {
            const session = sid,
              chosen = selected;
            const ev = await api(
              `/sessions/${encodeURIComponent(session)}/events/${id}`,
            );
            if (sid !== session || selected !== chosen) return;
            const panel = te(
              "details",
              { open: true, class: "trace-json" },
              te("summary", {}, `#${id} · ${ev.method}`),
              te(
                "pre",
                {},
                json(ev).slice(0, 100000) +
                  (json(ev).length > 100000
                    ? "\n…显示前 100,000 字符，请下载完整事件。"
                    : ""),
              ),
            );
            const download = button("下载此事件", () =>
              downloadJSON(ev, `rundesk-event-${id}.json`),
            );
            panel.append(download);
            sources.append(panel);
          },
          "trace-source",
        ),
      );
    content.append(sources);
  }
  function render() {
    if (!opened) return;
    q("#trace-detail").style.top =
      innerWidth < 900 ? q(".trace-body").offsetTop + "px" : "";
    q("#trace-detail").style.bottom =
      innerWidth < 900 ? q(".trace-footer").offsetHeight + 12 + "px" : "";
    renderStats();
    renderRuns();
    const chronological = visible();
    cachedRows = orderRows(chronological, listOrder);
    renderTimeline(chronological);
    for (const [id, delta, onlyBad] of [
      ["#trace-prev", -1, false],
      ["#trace-next", 1, false],
      ["#trace-prev-error", -1, true],
      ["#trace-next-error", 1, true],
    ])
      q(id).disabled = !nextRow(
        onlyBad ? chronological : cachedRows,
        selected,
        delta,
        onlyBad,
      );
    q("#trace-longest").disabled = !chronological.some(
      (r) => duration(r) != null,
    );
    q("#trace-count").textContent = cachedRows.length + " 步";
    renderList();
    detail();
    q("#trace-position").textContent = selected
      ? `${Math.max(0, cachedRows.findIndex((x) => x.id === selected) + 1)} / ${cachedRows.length}`
      : `${cachedRows.length} 步`;
    q("#trace-time").setAttribute("aria-pressed", mode === "time");
    q("#trace-steps").setAttribute("aria-pressed", mode === "steps");
    setFollow(follow);
  }
  async function refresh() {
    if (!opened || busy || !sid) return;
    busy = true;
    const token = epoch,
      session = sid;
    let through = 0;
    q("#trace-load").textContent = "正在读取…";
    try {
      do {
        const page = await api(
          `/sessions/${encodeURIComponent(session)}/trace?` +
            new URLSearchParams({ after: cursor, through, limit: 250 }),
        );
        if (token !== epoch || !opened) return;
        const previous = extent(),
          entire = !view || (view[0] <= previous[0] && view[1] >= previous[1]);
        model.ingest(page.events);
        if (!page.hasMore) model.reconcile(page.session);
        if (active(page.session.status))
          model.lastTime = Math.max(
            model.lastTime,
            Date.parse(page.observedAt) || 0,
          );
        cursor = page.nextCursor;
        through = page.hasMore ? page.snapshot : 0;
        if (!through && run !== "all" && !model.runMap.has(run)) {
          run = "all";
          view = null;
        }
        if (!selected && !through) {
          selected =
            model.list(run).find((x) => bad(x.status) || x.status === "pending")
              ?.id || model.list(run).at(-1)?.id;
        }
        if (follow) {
          const b = extent();
          if (entire || !view || view[1] - view[0] >= b[1] - b[0]) view = b;
          else view = [Math.max(b[0], b[1] - (view[1] - view[0])), b[1]];
        }
        const unchanged =
          !page.events.length &&
          !active(page.session.status) &&
          lastSessionStatus === page.session.status &&
          !restorePosition;
        if (!through) lastSessionStatus = page.session.status;
        if (!unchanged) render();
        if (!through && restorePosition && selected) {
          const index = cachedRows.findIndex((x) => x.id === selected);
          if (index >= 0)
            q("#trace-list").scrollTop = Math.max(
              0,
              index * 54 - q("#trace-list").clientHeight / 2,
            );
          renderList();
          restorePosition = false;
        }
        q("#trace-load").textContent = `${model.records} 条生命周期事件`;
        if (through) await new Promise((resolve) => setTimeout(resolve, 0));
      } while (through);
      save();
    } catch (e) {
      if (token === epoch)
        q("#trace-load").textContent = "读取失败：" + e.message;
    } finally {
      if (token === epoch) busy = false;
    }
  }
  function restore() {
    try {
      const saved = JSON.parse(
        sessionStorage.getItem("rundesk-trace-" + sid) || "null",
      );
      if (saved) {
        run = saved.run || "all";
        selected = saved.selected;
        view =
          Array.isArray(saved.view) &&
          saved.view.length === 2 &&
          saved.view.every(Number.isFinite)
            ? saved.view
            : null;
        follow = saved.follow !== false;
        mode = saved.mode === "steps" ? "steps" : "time";
        query = saved.query || "";
        listOrder = saved.listOrder === "duration" ? "duration" : "time";
        errorsOnly = !!saved.errorsOnly;
        tracks = new Set(
          (saved.tracks || TRACKS.map((x) => x[0])).filter((x) =>
            TRACKS.some((t) => t[0] === x),
          ),
        );
      }
    } catch {}
  }
  function syncControls() {
    q("#trace-search").value = query;
    q("#trace-order").value = listOrder;
    q("#trace-errors").checked = errorsOnly;
    for (const input of q("#trace-layers").querySelectorAll("input"))
      input.checked = tracks.has(input.value);
  }
  function open() {
    if (!state.session) {
      toast("请先选择一个已有会话。");
      return;
    }
    if (sid !== state.session.id) {
      sid = state.session.id;
      model = new Model();
      cursor = 0;
      epoch++;
      busy = false;
      run = "all";
      selected = null;
      view = null;
      follow = true;
      query = "";
      listOrder = "time";
      lastSessionStatus = "";
      runOptionsSignature = "";
      mode = "time";
      errorsOnly = false;
      tracks = new Set(TRACKS.map((x) => x[0]));
      restore();
    }
    sessionStorage.setItem("rundesk-trace-session",state.session.id);
    opened = true;
    restorePosition = true;
    $("#shell").inert = true;
    detailKey = "";
    root.classList.remove("hidden");
    q("#trace-session-title").textContent = state.session.title;
    q("#trace-demo").classList.toggle("hidden", !state.demo);
    history.replaceState(
      null,
      "",
      location.pathname + location.search + "#trace",
    );
    syncControls();
    render();
    refresh();
    clearInterval(refreshTimer);
    refreshTimer = setInterval(refresh, 1800);
    q("#trace-close").focus();
  }
  function close() {
    opened = false;
    $("#shell").inert = false;
    epoch++;
    busy = false;
    clearInterval(refreshTimer);
    root.classList.add("hidden");
    history.replaceState(null, "", location.pathname + location.search+"#session/"+encodeURIComponent(state.session.id));
    $("#trace-button").focus();
    save();
    scheduleRender();
  }
  function navigate(delta, onlyBad = false) {
    const rows = onlyBad ? visible() : orderRows(visible(), listOrder);
    const target = nextRow(rows, selected, delta, onlyBad);
    if (target) choose(target.id);
  }

  function downloadJSON(data, name) {
    const a = document.createElement("a"),
      url = URL.createObjectURL(
        new Blob([JSON.stringify(data, null, 2)], { type: "application/json" }),
      );
    a.href = url;
    a.download = name;
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  q("#trace-close").onclick = close;
  q("#trace-refresh").onclick = refresh;
  q("#trace-run").onchange = (e) => {
    root.classList.remove("show-detail");
    run = e.target.value;
    selected = null;
    view = null;
    detailKey = "";
    q("#trace-list").scrollTop = 0;
    fit();
  };
  q("#trace-search").oninput = (e) => {
    root.classList.remove("show-detail");
    query = e.target.value;
    q("#trace-list").scrollTop = 0;
    save();
    render();
  };
  q("#trace-errors").onchange = (e) => {
    root.classList.remove("show-detail");
    errorsOnly = e.target.checked;
    q("#trace-list").scrollTop = 0;
    save();
    render();
  };
  for (const [id, label] of TRACKS)
    q("#trace-layers").append(
      te(
        "label",
        {},
        te("input", {
          type: "checkbox",
          checked: true,
          value: id,
          onchange: (e) => {
            e.target.checked ? tracks.add(id) : tracks.delete(id);
            save();
            render();
          },
        }),
        label,
      ),
    );
  q("#trace-time").onclick = () => {
    mode = "time";
    view = null;
    fit();
  };
  q("#trace-steps").onclick = () => {
    mode = "steps";
    view = null;
    fit();
  };
  q("#trace-order").onchange = (e) => {
    listOrder = e.target.value;
    setFollow(false);
    q("#trace-list").scrollTop = 0;
    save();
    render();
  };
  q("#trace-longest").onclick = () => {
    const row = orderRows(visible(), "duration").find(
      (r) => duration(r) != null,
    );
    if (row) choose(row.id);
  };
  q("#trace-fit").onclick = fit;
  q("#trace-zoom-in").onclick = () => zoom(0.5);
  q("#trace-zoom-out").onclick = () => zoom(2);
  q("#trace-follow").onclick = () => {
    setFollow(!follow);
    if (follow) {
      view = null;
      refresh();
      render();
    }
  };
  q("#trace-prev").onclick = () => navigate(-1);
  q("#trace-next").onclick = () => navigate(1);
  q("#trace-prev-error").onclick = () => navigate(-1, true);
  q("#trace-next-error").onclick = () => navigate(1, true);
  q("#trace-focus").onclick = () => {
    root.classList.toggle("focus-mode");
    q("#trace-focus").setAttribute(
      "aria-pressed",
      root.classList.contains("focus-mode"),
    );
    render();
  };
  q("#trace-detail-close").onclick = () => {
    root.classList.remove("show-detail");
    root.classList.add("focus-mode");
    q("#trace-focus").setAttribute("aria-pressed", true);
    render();
  };
  q("#trace-export").onclick = () =>
    downloadJSON(
      {
        format: "rundesk-trace/1",
        sessionId: sid,
        exportedAt: new Date().toISOString(),
        filter: { run, query, listOrder, errorsOnly, tracks: [...tracks] },
        runs: model.runs
          .filter((r) => run === "all" || r.id === run)
          .map(({ inputRow, ...r }) => r),
        rows: orderRows(visible(), listOrder),
        note: "生命周期事件投影；完整源事件使用会话 JSONL 导出。",
      },
      "rundesk-trace.json",
    );
  q("#trace-list").addEventListener("scroll", renderList);
  q("#trace-lanes").addEventListener(
    "wheel",
    (e) => {
      e.preventDefault();
      const rect = q("#trace-axis").getBoundingClientRect();
      zoom(
        e.deltaY > 0 ? 1.25 : 0.8,
        Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width)),
      );
    },
    { passive: false },
  );
  function drag(target, overview) {
    target.addEventListener("pointerdown", (e) => {
      if (e.button !== 0 || (!overview && e.target.closest("button"))) return;
      const rect = (
          overview ? q("#trace-overview") : q("#trace-axis")
        ).getBoundingClientRect(),
        startX = e.clientX,
        b = frame(),
        ext = extent(),
        span = overview ? ext[1] - ext[0] : b[1] - b[0],
        edge = e.target.dataset.edge;
      setFollow(false);
      target.setPointerCapture(e.pointerId);
      let base = b;
      if (overview && !e.target.closest("#trace-window")) {
        const t = ext[0] + ((e.clientX - rect.left) / rect.width) * span,
          w = b[1] - b[0];
        base = clamp([t - w / 2, t + w / 2]);
        view = base;
        render();
      }
      const move = (ev) => {
        const d =
          ((ev.clientX - startX) / rect.width) * span * (overview ? 1 : -1);
        view =
          edge === "left"
            ? clamp([Math.min(base[1] - 1, base[0] + d), base[1]])
            : edge === "right"
              ? clamp([base[0], Math.max(base[0] + 1, base[1] + d)])
              : clamp([base[0] + d, base[1] + d]);
        render();
      };
      const end = () => {
        target.removeEventListener("pointermove", move);
        target.removeEventListener("pointerup", end);
        target.removeEventListener("pointercancel", end);
        save();
      };
      target.addEventListener("pointermove", move);
      target.addEventListener("pointerup", end);
      target.addEventListener("pointercancel", end);
    });
  }
  drag(q("#trace-overview"), true);
  drag(q("#trace-lanes"), false);
  root.addEventListener("keydown", (e) => {
    if (e.target.matches("input,select,textarea")) return;
    if (e.key === "Escape") {
      if (root.classList.contains("show-detail") && innerWidth < 900) {
        root.classList.remove("show-detail");
        return;
      }
      close();
    }
    if (e.key === "ArrowLeft") {
      e.preventDefault();
      navigate(-1, e.shiftKey);
    }
    if (e.key === "ArrowRight") {
      e.preventDefault();
      navigate(1, e.shiftKey);
    }
    if (e.key === "+" || e.key === "=") zoom(0.5);
    if (e.key === "-") zoom(2);
  });
  new ResizeObserver(() => {
    if (opened) render();
  }).observe(q(".trace-main"));
  $("#trace-button").onclick = open;
  window.RunDeskTraceUI = {
    open,
    isOpen: () => opened,
    sessionChanged: () => {
      if (opened && sid !== state.session?.id) close();
      if (location.hash === "#trace" && state.session) open();
    },
  };
})();
