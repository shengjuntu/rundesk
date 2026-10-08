/* Semantic projection of persisted App Server events. Shared by browser and tests. */
(function (root, factory) {
  if (typeof module === "object" && module.exports) module.exports = factory();
  else root.RunDeskTrace = factory();
})(typeof globalThis !== "undefined" ? globalThis : this, function () {
  "use strict";
const rdText=globalThis.rdText||((value)=>value);
const rdFormat=globalThis.rdFormat||((key,...values)=>key.replace(/\$\{(\d+)\}/g,(_,n)=>String(values[Number(n)]??"")));
  const TRACKS = [
    ["input", rdText("输入与回复")],
    ["model", rdText("可见模型阶段")],
    ["tools", rdText("命令与 MCP")],
    ["approval", rdText("审批与确认")],
    ["context", rdText("上下文与注入")],
    ["system", rdText("状态与异常")],
  ];
  const bad = (s) =>
    [
      "failed",
      "declined",
      "rejected",
      "cancelled",
      "canceled",
      "interrupted",
      "expired",
      "error",
    ].includes(s);
  const short = (v, n = 100) =>
    String(v ?? "")
      .replace(/\s+/g, " ")
      .slice(0, n);
  const plain = (v) => (typeof v === "string" ? v : JSON.stringify(v, null, 2));
  const ms = (v) => {
    const n = typeof v === "number" ? v : Date.parse(v);
    return Number.isFinite(n) && n > 0 ? n : null;
  };
  const duration = (row) =>
    row &&
    !row.point &&
    !row.timeConflict &&
    row.start != null &&
    row.end != null &&
    row.end >= row.start
      ? row.end - row.start
      : null;
  function orderRows(rows, order = "time") {
    return order === "duration"
      ? [...rows].sort(
          (a, b) =>
            (duration(b) ?? -1) - (duration(a) ?? -1) ||
            (a.start ?? a.end) - (b.start ?? b.end) ||
            a.refs[0] - b.refs[0],
        )
      : rows;
  }
  function nextRow(rows, selected, delta, onlyBad = false) {
    const start = rows.findIndex((r) => r.id === selected);
    for (
      let i = start < 0 ? (delta > 0 ? 0 : rows.length - 1) : start + delta;
      i >= 0 && i < rows.length;
      i += delta
    ) {
      if (!onlyBad || bad(rows[i].status) || rows[i].status === "warning")
        return rows[i];
    }
    return null;
  }
  class Model {
    constructor() {
      this.rows = new Map();
      this.runs = [];
      this.runMap = new Map();
      this.turns = new Map();
      this.requests = new Map();
      this.current = null;
      this.lastID = 0;
      this.lastTime = 0;
      this.tokens = null;
      this.records = 0;
      this.boundsCache = new Map();
    }
    runFor(e, p) {
      const tid = p.turnId || p.turn?.id;
      let r = this.runMap.get(e.data?.runId) || (tid && this.turns.get(tid));
      if (!r) r = this.current;
      if (!r) {
        r = {
          id: "observed-" + e.id,
          title: rdText("已有原生事件"),
          start: ms(e.time),
          end: null,
          status: "unknown",
          refs: [],
          input: false,
        };
        this.runs.push(r);
        this.runMap.set(r.id, r);
        this.current = r;
      }
      if (tid && !this.turns.has(tid)) {
        this.turns.set(tid, r);
        r.turnId = tid;
      }
      r.lastObserved = Math.max(r.lastObserved || r.start, ms(e.time) || 0);
      return r;
    }
    add(e, r, info) {
      const row = {
        id: "event-" + e.id,
        runId: r.id,
        start: ms(e.time),
        end: ms(e.time),
        status: "completed",
        track: "system",
        title: e.method,
        body: "",
        refs: [e.id],
        timeSource: rdText("后台接收时间"),
        point: true,
        ...info,
      };
      this.rows.set(row.id, row);
      return row;
    }
    ref(row, e) {
      if (!row.refs.includes(e.id)) {
        row.refs.push(e.id);
        if (row.refs.length > 16) row.refs.splice(1, 1);
      }
    }
    closeRun(r, t, status) {
      if (r.end == null) r.end = t;
      r.status = status;
      for (const row of this.rows.values()) {
        if (row.runId === r.id && row.end == null) {
          row.observedUntil = r.end;
          row.incomplete = true;
          if (["running", "pending", "inProgress"].includes(row.status))
            row.status = bad(status) ? status : "unknown";
        }
      }
    }
    freeze(r, status = "unknown") {
      this.boundsCache.clear();
      r.status = status;
      r.incomplete = true;
      r.observedUntil ??= r.lastObserved || r.start;
      for (const row of this.rows.values())
        if (row.runId === r.id && row.end == null) {
          row.incomplete = true;
          row.observedUntil ??= r.observedUntil;
          if (["running", "pending", "inProgress"].includes(row.status))
            row.status = status;
        }
    }
    endpoint(row) {
      const r = this.runMap.get(row.runId);
      return (
        row.end ??
        row.observedUntil ??
        r?.end ??
        r?.observedUntil ??
        this.lastTime
      );
    }
    ingest(events) {
      if (events.length) this.boundsCache.clear();
      for (const e of events) {
        if (e.id <= this.lastID) continue;
        this.lastID = e.id;
        this.records++;
        const t = ms(e.time);
        if (!t) continue;
        this.lastTime = Math.max(this.lastTime, t);
        const d = e.data || {},
          p = d.params || {};
        if (e.method === "run/input") {
          if (this.current && this.current.end == null)
            this.freeze(
              this.current,
              bad(this.current.status) ? this.current.status : "unknown",
            );
          const r = {
            id: d.runId || "run-" + e.id,
            title: short(d.input?.text || rdText("新运行"), 160),
            start: t,
            lastObserved: t,
            end: null,
            status: "running",
            refs: [e.id],
            input: true,
            recovery: d.recovery,
          };
          this.runs.push(r);
          this.runMap.set(r.id, r);
          this.current = r;
          r.inputRow = this.add(e, r, {
            track: "input",
            title: rdText("用户输入"),
            body: d.input?.text || "",
            detail: d.input,
          });
          if (d.notes)
            this.add(e, r, {
              id: "notes-" + e.id,
              track: "context",
              title: rdText("项目笔记注入"),
              body: d.notes,
              detail: { revision: d.notesRevision, cwd: d.cwd },
            });
          if (d.input?.skills?.length)
            this.add(e, r, {
              id: "skills-" + e.id,
              track: "context",
              title: rdText("显式选中 Skills"),
              body: plain(d.input.skills),
              note: rdText("记录提交给 Codex 的技能，不代表已经执行。"),
            });
          continue;
        }
        const r = this.runFor(e, p);
        if(e.method.startsWith("kun/")){
          const data=d.data||{};
          if(e.method==="kun/run.finished"){this.closeRun(r,t,data.status||"unknown");if(data.error)this.add(e,r,{title:"Kun 运行错误",status:"failed",body:data.error});}
          else if(e.method==="kun/approval.requested"){r.status="waiting";this.add(e,r,{id:"kun-approval-"+r.id+"-"+data.callId,title:"MCP 工具审批",body:data.server+" / "+data.tool,track:"approval",status:"pending",point:false,end:null,detail:data});}
          else if(e.method==="kun/run.paused"){r.status="waiting";this.add(e,r,{title:"Kun 已暂停",body:data.phase||"",track:"system"});}
          else if(e.method==="kun/control.queued"&&data.command?.operation==="steer"){this.add(e,r,{title:"补充指令",body:data.command.text||"",detail:data});}
          else if(e.method==="kun/control.applied"){if(["resume","step","approve","reject"].includes(data.command?.operation))r.status="running";if(["approve","reject"].includes(data.command?.operation)){const a=this.rows.get("kun-approval-"+r.id+"-"+data.command.callId);if(a){a.end=t;a.status=data.command.operation==="approve"?"approved":"declined";this.ref(a,e);}}this.add(e,r,{title:"调试命令已生效",body:data.command?.operation||"",detail:data});}
          else if(e.method==="kun/mcp.request"||e.method==="kun/mcp.response"){
            const key="kun-mcp-"+r.id+"-"+data.exchangeId,done=e.method==="kun/mcp.response";
            let row=this.rows.get(key);if(!row)row=this.add(e,r,{id:key,track:"tools",title:"MCP "+data.server+" / "+data.method,start:done?null:t,end:null,point:false,status:"running"});
            this.ref(row,e);row.detail={...row.detail,...data};if(done){row.end=t;row.status=data.error?"failed":"completed";row.body=data.error||plain(data.result||{});}
          }
          else if(e.method==="kun/mcp.ready"){this.add(e,r,{track:"context",title:"MCP 工具清单快照",body:data.server?.name||"",detail:data});}
          else if(e.method==="kun/model.started"||e.method==="kun/model.completed"||e.method==="kun/tool.started"||e.method==="kun/tool.completed"){
            const model=e.method.includes("/model."),done=e.method.endsWith(".completed");
            const key="kun-"+r.id+"-"+(model?"model-"+data.step:"tool-"+data.call?.id);
            let row=this.rows.get(key);
            if(!row)row=this.add(e,r,{id:key,track:model?"model":"tools",title:model?"Kun 模型调用":data.call?.function?.name||"Kun 工具",start:done?null:t,end:null,point:false,status:"running"});
            this.ref(row,e);row.detail={...row.detail,...data,kunSequence:d.sequence};row.body=model?(data.message?.content||plain(data.request||{})):(data.output||data.call?.function?.arguments||"");
            if(done){row.end=t;row.status=data.status||(data.isError?"failed":"completed");if(model&&data.message?.content){this.add(e,r,{id:key+"-reply",track:"input",type:"agentMessage",title:"Kun 回复",body:data.message.content,detail:data.message});}}
          }
          continue;
        }
        switch (e.method) {
          case "run/steer":
            this.add(e, r, {
              track: "input",
              title: rdText("运行中补充指令 · 已接收"),
              body: d.input?.text || "",
              detail: d.input,
            });
            break;
          case "turn/start":
            if (e.direction === "out") {
              r.refs.push(e.id);
              if (r.inputRow) this.ref(r.inputRow, e);
            }
            break;
          case "turn/started":
            r.turnId = p.turn?.id || p.turnId;
            r.status = "running";
            if (r.turnId) this.turns.set(r.turnId, r);
            r.refs.push(e.id);
            break;
          case "turn/completed":
            this.closeRun(r, t, p.turn?.status || "completed");
            r.refs.push(e.id);
            if (p.turn?.error)
              this.add(e, r, {
                title: rdText("运行失败"),
                status: "failed",
                body: plain(p.turn.error),
              });
            break;
          case "run/retry":
            this.add(e, r, {title: d.willRetry ? rdText("Codex 正在重试") : rdText("重试状态已更新"), body: rdText("原生 Codex 通知；未新建任务轮次。")});
            break;
          case "run/state":
            this.closeRun(r, t, d.status || "unknown");
            if (d.error)
              this.add(e, r, {
                title: rdText("后台运行错误"),
                status: "failed",
                body: d.error,
              });
            break;
          case "item/started":
          case "item/completed": {
            let item = p.item || {};
            const iid = item.id;
            if (!iid) break;
            if (item.type === "userMessage" && r.inputRow) {
              this.ref(r.inputRow, e);
              break;
            }
            const key = "item-" + (p.turnId || r.id) + "-" + iid,
              done = e.method === "item/completed";
            let row = this.rows.get(key);
            // Completed events may omit arguments supplied by the start event.
            item = { ...(row?.detail || {}), ...item };
            const type = item.type || "unknown";
            const track =
              {
                userMessage: "input",
                agentMessage: "input",
                reasoning: "model",
                commandExecution: "tools",
                mcpToolCall: "tools",
                dynamicToolCall: "tools",
                fileChange: "tools",
                webSearch: "tools",
                imageView: "tools",
                imageGeneration: "tools",
                contextCompaction: "context",
              }[type] || "system";
            const title =
              {
                userMessage: rdText("用户消息"),
                agentMessage: item.phase === "commentary" ? rdText("阶段说明") : rdText("助手回复"),
                reasoning: rdText("可见推理"),
                commandExecution: short(item.command),
                mcpToolCall:
                  (item.server || "MCP") + " / " + (item.tool || rdText("工具")),
                dynamicToolCall: item.tool || rdText("动态工具"),
                fileChange: rdText("文件变更"),
                webSearch: rdText("搜索"),
                imageView: rdText("查看图像"),
                imageGeneration: rdText("生成图像"),
                contextCompaction: rdText("上下文压缩"),
              }[type] || type;
            if (!row) {
              const native = ms(done ? p.completedAtMs : p.startedAtMs);
              row = this.add(e, r, {
                id: key,
                track,
                title,
                itemId: iid,
                turnId: p.turnId,
                type,
                start: done ? null : native || t,
                end: null,
                point: false,
                status: "running",
                timeSource: native ? rdText("原生事件时间") : rdText("后台接收时间"),
                incomplete: done,
              });
            }
            this.ref(row, e);
            row.title = title;
            row.detail = item;
            row.body =
              item.text ||
              item.aggregatedOutput ||
              plain(
                item.summary ||
                  item.content ||
                  item.contentItems ||
                  item.result ||
                  item.changes ||
                  item.arguments ||
                  item.error ||
                  "",
              );
            if (done) {
              row.end = ms(p.completedAtMs) || t;
              row.status = p.item?.status || "completed";
              if (
                item.exitCode != null &&
                item.exitCode !== 0 &&
                row.status !== "declined"
              )
                row.status = "failed";
              if (item.success === false) row.status = "failed";
              if (item.error || item.result?.isError === true) row.status = "failed";
              row.exitCode = item.exitCode;
              row.timeConflict = row.start != null && row.end < row.start;
              row.incomplete = row.start == null || row.timeConflict;
              row.endSource = ms(p.completedAtMs)
                ? rdText("原生事件时间")
                : rdText("后台接收时间");
            }
            break;
          }
          case "approval/pending": {
            const req = d.request || {},
              q = req.params || {};
            const row = this.add(e, r, {
              id: "approval-" + d.id,
              track: "approval",
              title: q.command ? short(q.command) : q.reason || rdText("等待人工确认"),
              body: q.reason || rdText("等待审批或用户输入"),
              detail: q,
              start: t,
              end: null,
              point: false,
              status: "pending",
              requestId: req.id,
              itemId: q.itemId,
              approvalId: d.id,
            });
            this.requests.set(JSON.stringify(req.id), row);
            break;
          }
          case "serverRequest/resolved": {
            const row = this.requests.get(JSON.stringify(p.requestId));
            if (row) {
              if (row.end == null) row.end = t;
              if (row.status === "pending") row.status = "resolved";
              this.ref(row, e);
            }
            break;
          }
          case "approval/resolved": {
            const row = this.rows.get("approval-" + d.id);
            if (row) {
              if (row.end == null) row.end = t;
              row.decision = d.decision;
              row.scope = d.scope;
              row.status = ["decline", "cancel"].includes(d.decision)
                ? "declined"
                : "accepted";
              row.body +=
                (row.body ? "\n\n" : "") +
                rdText("决定：") +
                plain(d.decision) +
                (d.scope ? rdText("\n范围：") + d.scope : "");
              this.ref(row, e);
            }
            break;
          }
          case "approval/expired":
            for (const row of this.rows.values())
              if (
                row.runId === r.id &&
                row.track === "approval" &&
                row.end == null
              ) {
                row.end = t;
                row.status = "expired";
                this.ref(row, e);
              }
            break;
          case "thread/tokenUsage/updated":
            this.tokens = p.tokenUsage;
            r.tokens = p.tokenUsage;
            break;
          case "thread/compacted":
            if (
              ![...this.rows.values()].some(
                (x) => x.runId === r.id && x.type === "contextCompaction",
              )
            )
              this.add(e, r, {
                track: "context",
                title: rdText("上下文已压缩"),
                body: rdText("接口只报告完成事件，未提供开始时间。"),
              });
            break;
          case "runtime/effective":
            r.effective = d.effective;
            break;
          case "warning":
          case "configWarning":
          case "deprecationNotice":
            this.add(e, r, {
              title: e.method === "configWarning" ? rdText("配置告警") : rdText("运行告警"),
              status: "warning",
              body:
                p.message ||
                [p.summary, p.details].filter(Boolean).join("\n") ||
                plain(p),
            });
            break;
          case "library/error":
            this.add(e,r,{title:rdText("文件自动保存失败"),status:"failed",body:plain(d.error)});
            break;
          case "error":
            this.add(e, r, {
              title: p.willRetry ? rdText("原生错误 · 将重试") : rdText("运行错误"),
              status: p.willRetry ? "warning" : "failed",
              body: plain(p.error || p),
            });
            break;
        }
      }
      return this;
    }
    reconcile(session) {
      if (!session) return;
      const r = this.runMap.get(session.runId);
      if (
        r &&
        ["interrupted", "failed"].includes(session.status) &&
        r.end == null
      ) {
        this.freeze(r, session.status);
      }
    }
    list(runId = "all") {
      return [...this.rows.values()]
        .filter((x) => runId === "all" || x.runId === runId)
        .sort(
          (a, b) =>
            (a.start ?? a.end) - (b.start ?? b.end) || a.refs[0] - b.refs[0],
        );
    }
    bounds(runId = "all") {
      const cached = this.boundsCache.get(runId);
      if (cached && cached.clock === this.lastTime) return cached.value;
      const runs = this.runs.filter((r) => runId === "all" || r.id === runId);
      const rows = [...this.rows.values()].filter(
        (r) => runId === "all" || r.runId === runId,
      );
      let start = Infinity,
        end = -Infinity;
      for (const r of runs) {
        start = Math.min(start, r.start);
        end = Math.max(end, r.end ?? r.observedUntil ?? this.lastTime);
      }
      for (const row of rows) {
        start = Math.min(start, row.start ?? row.end);
        end = Math.max(end, this.endpoint(row));
      }
      const value = Number.isFinite(start)
        ? [start, Math.max(start + 1, end)]
        : [0, 1];
      this.boundsCache.set(runId, { clock: this.lastTime, value });
      return value;
    }
    stats(runId = "all") {
      const rows = this.list(runId),
        intervals = rows
          .filter(
            (r) => r.track === "approval" && r.start != null && r.end != null,
          )
          .map((r) => [r.start, r.end])
          .sort((a, b) => a[0] - b[0]);
      let wait = 0,
        end = 0;
      for (const [a, b] of intervals) {
        wait += Math.max(0, b - Math.max(end, a));
        end = Math.max(end, b);
      }
      const runs = this.runs.filter((r) => runId === "all" || r.id === runId),
        tokens = runId === "all" ? this.tokens : runs[0]?.tokens;
      return {
        steps: rows.length,
        errors: rows.filter((r) => bad(r.status) || r.status === "warning")
          .length,
        wait,
        tokens,
        bounds: this.bounds(runId),
      };
    }
  }
  return { Model, TRACKS, bad, short, duration, orderRows, nextRow };
});
