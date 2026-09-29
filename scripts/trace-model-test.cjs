const assert = require("node:assert/strict");
const fs = require("node:fs");
const { Model } = require("../internal/web/trace-model.js");
let id = 0;
const origin = Date.parse("2026-09-28T10:00:00Z");
function event(ms, method, data, direction = "in") {
  return {
    id: ++id,
    time: new Date(origin + ms).toISOString(),
    method,
    direction,
    data,
  };
}
const events = [
  event(0, "run/input", {
    runId: "r1",
    input: { text: "test", skills: [{ name: "review" }] },
    notes: "check evidence",
  }),
  event(1, "turn/started", { params: { turn: { id: "t1" } } }),
  event(10, "item/started", {
    params: {
      turnId: "t1",
      startedAtMs: origin + 5,
      item: { id: "tool", type: "commandExecution", command: "test" },
    },
  }),
  event(30, "approval/pending", {
    id: "a",
    runId: "r1",
    request: { id: 7, params: { itemId: "tool", command: "test" } },
  }),
  event(40, "approval/pending", {
    id: "b",
    runId: "r1",
    request: { id: 8, params: {} },
  }),
  event(50, "approval/resolved", { id: "a", decision: "cancel" }),
  event(51, "serverRequest/resolved", { params: { requestId: 7 } }),
  event(60, "approval/resolved", { id: "b", decision: "accept" }),
  event(70, "item/completed", {
    params: {
      turnId: "t1",
      completedAtMs: origin + 65,
      item: {
        id: "tool",
        type: "commandExecution",
        command: "test",
        status: "declined",
      },
    },
  }),
  event(75, "item/completed", {
    params: {
      turnId: "t1",
      item: {
        id: "orphan",
        type: "mcpToolCall",
        server: "demo",
        tool: "inspect",
        status: "completed",
      },
    },
  }),
  event(76, "item/started", {
    params: { turnId: "t1", item: { id: "missing-end", type: "reasoning" } },
  }),
  event(80, "thread/tokenUsage/updated", {
    params: {
      turnId: "t1",
      tokenUsage: { total: { totalTokens: 500 }, last: { totalTokens: 300 } },
    },
  }),
  event(90, "turn/completed", {
    params: { turn: { id: "t1", status: "interrupted" } },
  }),
];
const m = new Model().ingest(events.slice(0, 5)).ingest(events.slice(5));
assert.equal(m.runs.length, 1);
assert.equal(m.stats().wait, 30);
assert.equal(m.rows.get("approval-a").status, "declined");
assert.equal(m.rows.get("approval-a").end, origin + 50);
assert.equal(m.rows.get("item-t1-tool").start, origin + 5);
assert.equal(m.rows.get("item-t1-tool").end, origin + 65);
assert.equal(m.rows.get("item-t1-orphan").start, null);
assert.equal(m.rows.get("item-t1-missing-end").end, null);
assert.equal(m.rows.get("item-t1-missing-end").incomplete, true);
assert.equal(m.stats().tokens.total.totalTokens, 500);
const count = m.rows.size;
m.ingest(events);
assert.equal(m.rows.size, count);
assert.equal(m.records, events.length);
const next = [
  event(100, "run/input", { runId: "r2", input: { text: "next" } }),
  event(101, "turn/started", { params: { turn: { id: "t2" } } }),
  event(110, "item/completed", {
    params: {
      turnId: "t2",
      item: { id: "tool", type: "agentMessage", text: "done" },
    },
  }),
];
m.ingest(next);
assert.equal(m.runs.length, 2);
assert.equal(m.list("r1").length, count);
assert.ok(m.rows.has("item-t2-tool"));
m.reconcile({ runId: "r2", status: "interrupted" });
assert.equal(m.runMap.get("r2").incomplete, true);
console.log(
  "PASS: grouped lifecycle, duplicate replay, native times, missing boundaries, overlapping approval union, rejection ordering, cumulative tokens and multiple turns",
);
if (process.env.REAL_TRACE_FILE) {
  const raw = fs
    .readFileSync(process.env.REAL_TRACE_FILE, "utf8")
    .trim()
    .split("\n")
    .map(JSON.parse)
    .filter((e) => e.method !== undefined);
  const wanted = new Set([
    "run/input",
    "turn/start",
    "turn/started",
    "turn/completed",
    "run/state",
    "item/started",
    "item/completed",
    "approval/pending",
    "approval/resolved",
    "approval/expired",
    "serverRequest/resolved",
    "thread/tokenUsage/updated",
    "thread/compacted",
    "error",
    "warning",
    "configWarning",
    "deprecationNotice",
    "runtime/effective",
  ]);
  const real = new Model().ingest(raw.filter((e) => wanted.has(e.method)));
  const approvals = real.list().filter((x) => x.track === "approval");
  assert.equal(
    real.runs.length,
    raw.filter((e) => e.method === "run/input").length,
  );
  assert.equal(
    approvals.length,
    raw.filter((e) => e.method === "approval/pending").length,
  );
  assert.equal(
    real.list().filter((x) => x.start && x.end && x.end < x.start).length,
    0,
  );
  assert.equal(
    real.list().filter((x) => x.type === "commandExecution").length,
    raw.filter(
      (e) =>
        e.method === "item/completed" &&
        e.data.params.item.type === "commandExecution",
    ).length,
  );
  console.log("PASS: private real trace", {
    events: raw.length,
    lifecycle: real.records,
    runs: real.runs.length,
    steps: real.rows.size,
    approvals: approvals.length,
    accepted: approvals.filter((x) => x.status === "accepted").length,
    declined: approvals.filter((x) => x.status === "declined").length,
  });
}

// v0.5.1: unknown endings must not grow with subsequent runs or wall-clock polls.
{
  const {
    duration,
    orderRows,
    nextRow,
  } = require("../internal/web/trace-model.js");
  const interrupted = new Model().ingest([
    event(1000, "run/input", { runId: "old", input: { text: "old" } }),
    event(1010, "item/started", {
      params: {
        turnId: "old-turn",
        item: {
          id: "unfinished",
          type: "commandExecution",
          command: "inspect",
        },
      },
    }),
  ]);
  interrupted.lastTime = origin + 5000;
  interrupted.reconcile({ runId: "old", status: "interrupted" });
  assert.equal(interrupted.bounds("old")[1], origin + 1010);
  const oldRow = interrupted.rows.get("item-old-turn-unfinished");
  interrupted.ingest([
    event(6000, "run/input", { runId: "new", input: { text: "next" } }),
  ]);
  interrupted.lastTime = origin + 20000;
  assert.equal(interrupted.bounds("old")[1], origin + 1010);
  assert.equal(interrupted.endpoint(oldRow), origin + 1010);
  assert.equal(oldRow.status, "interrupted");
  assert.equal(duration(oldRow), null);
  const rows = [
    { id: "a", start: 10, end: 20, status: "completed", refs: [1] },
    { id: "b", start: 30, end: 90, status: "failed", refs: [2] },
    { id: "c", start: 100, end: null, status: "running", refs: [3] },
    {
      id: "d",
      start: 120,
      end: 120,
      point: true,
      status: "completed",
      refs: [4],
    },
  ];
  assert.deepEqual(
    orderRows(rows, "duration").map((r) => r.id),
    ["b", "a", "c", "d"],
  );
  assert.equal(nextRow(rows, "c", 1, true), null);
  assert.equal(nextRow(rows, "a", -1, true), null);
  assert.equal(nextRow(rows, "a", 1, true).id, "b");
  assert.equal(nextRow(rows, "d", -1, true).id, "b");
  assert.equal(nextRow(rows, "b", 1, true), null);
  const large = new Model();
  for (let i = 0; i < 150000; i++)
    large.rows.set(String(i), {
      runId: "large",
      start: origin + i,
      end: origin + i + 1,
    });
  assert.deepEqual(large.bounds(), [origin, origin + 150000]);
  console.log(
    "PASS: frozen incomplete runs, duration ordering, navigation boundaries and 150,000-row bounds calculation",
  );
}

{
  const model = new Model();
  model.ingest([
    event(0,"run/input",{runId:"steered-run",input:{text:"initial"}},"internal"),
    event(10,"turn/started",{params:{turn:{id:"steered-turn"}}}),
    event(20,"run/steer",{runId:"steered-run",turnId:"steered-turn",input:{text:"adjust direction"},status:"accepted"},"internal")
  ]);
  assert.equal(model.runs.length,1);
  const row=[...model.rows.values()].find(row=>row.title.includes("补充指令"));
  assert.ok(row);
  assert.equal(row.runId,"steered-run");
  assert.equal(row.body,"adjust direction");
  console.log("PASS: steering remains an input event in the original run");
}
