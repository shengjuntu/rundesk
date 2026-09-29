// Development-only UI fixtures are written into a disposable database.
const { chromium } = require(process.env.PLAYWRIGHT_PATH || "playwright");
const { spawn, spawnSync } = require("node:child_process");
const fs = require("node:fs"),
  os = require("node:os"),
  path = require("node:path"),
  assert = require("node:assert/strict");
(async () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), "rundesk-trace-")),
    port = 35000 + Math.floor(Math.random() * 12000),
    base = `http://127.0.0.1:${port}`,
    token = "trace-ui-test-only-32-character-token";
  const server = spawn(
    path.resolve("bin/rundesk"),
    ["--demo", "--data", temp, "--listen", `127.0.0.1:${port}`],
    {
      env: { ...process.env, RUNDESK_TOKEN: token },
      stdio: ["ignore", "ignore", "pipe"],
    },
  );
  let logs = "",
    browser;
  server.stderr.on("data", (b) => (logs += b));
  const call = async (p, body) => {
    const r = await fetch(base + "/api" + p, {
      method: body ? "POST" : "GET",
      headers: {
        Authorization: "Bearer " + token,
        "Content-Type": "application/json",
      },
      body: body ? JSON.stringify(body) : undefined,
    });
    assert.ok(r.ok, await r.clone().text());
    return r.json();
  };
  try {
    for (let i = 0; i < 100; i++) {
      try {
        await call("/meta");
        break;
      } catch {
        await new Promise((r) => setTimeout(r, 100));
      }
    }
    const w = (await call("/workspaces"))[0],
      s = await call("/sessions", {
        workspaceId: w.id,
        title: "检查视觉服务 · 轨迹演示",
      }),
      long = await call("/sessions", {
        workspaceId: w.id,
        title: "长轨迹 · 2,000 个工具步骤",
      });
    const events = [],
      origin = Date.parse("2026-09-28T12:00:00Z");
    const add = (t, method, data) =>
      events.push({
        time: new Date(origin + t).toISOString(),
        method,
        data,
        direction:
          method.startsWith("run/") || method.startsWith("approval/")
            ? "internal"
            : "in",
      });
    const item = (t, id, type, extra = {}, done = false, turn = "turn-one") =>
      add(t, done ? "item/completed" : "item/started", {
        params: {
          turnId: turn,
          [done ? "completedAtMs" : "startedAtMs"]: origin + t,
          item: { id, type, ...extra },
        },
      });
    add(0, "run/input", {
      runId: "run-one",
      input: {
        text: "检查视觉服务连接，定位测试失败并整理修复建议。",
        skills: [{ name: "service-review" }],
      },
      notes: "只检查测试目录；保留错误证据。",
      notesRevision: 2,
    });
    add(20, "turn/started", {
      params: { turn: { id: "turn-one", status: "inProgress" } },
    });
    item(500, "reason", "reasoning");
    item(
      3800,
      "reason",
      "reasoning",
      { summary: ["先确认服务入口、模型配置和当前连接状态。"] },
      true,
    );
    item(4200, "command", "commandExecution", {
      command: "python checks.py --service vision",
    });
    add(4700, "approval/pending", {
      id: "approve-one",
      runId: "run-one",
      request: {
        id: "req-one",
        params: {
          itemId: "command",
          command: "python checks.py --service vision",
          reason: "需要读取测试服务状态",
          cwd: "/demo/project",
        },
      },
    });
    item(6000, "inventory", "mcpToolCall", {
      server: "resource-catalog",
      tool: "list_services",
    });
    item(
      11200,
      "inventory",
      "mcpToolCall",
      {
        server: "resource-catalog",
        tool: "list_services",
        status: "completed",
        arguments: { scope: "workspace" },
        result: {
          services: ["rust-onnx-infer", "image-tools"],
          source: "演示数据",
        },
      },
      true,
    );
    add(18300, "approval/resolved", {
      id: "approve-one",
      decision: "accept",
      scope: "turn",
    });
    add(18310, "serverRequest/resolved", { params: { requestId: "req-one" } });
    item(
      22800,
      "command",
      "commandExecution",
      {
        command: "python checks.py --service vision",
        status: "failed",
        exitCode: 1,
        aggregatedOutput:
          "连接检查失败\nMODEL_ENDPOINT 未配置。\n这是一段演示输出，没有执行真实命令。",
      },
      true,
    );
    item(24600, "reason-two", "reasoning");
    item(
      27800,
      "reason-two",
      "reasoning",
      { summary: ["错误来自端点配置。补齐环境变量后可以重新检查。"] },
      true,
    );
    item(28900, "retry", "commandExecution", {
      command: "python checks.py --config demo.toml",
    });
    item(
      34100,
      "retry",
      "commandExecution",
      {
        command: "python checks.py --config demo.toml",
        status: "completed",
        exitCode: 0,
        aggregatedOutput: "演示：服务入口可访问；发现 3 个可用接口。",
      },
      true,
    );
    item(35800, "compact", "contextCompaction");
    item(40900, "compact", "contextCompaction", {}, true);
    item(42300, "response", "agentMessage");
    item(
      49500,
      "response",
      "agentMessage",
      {
        text: "已定位配置缺失，并记录验证方法。审批等待与工具执行区间均已保留。\n\n<svg onload=alert(1)> 作为文本显示。",
      },
      true,
    );
    add(49600, "thread/tokenUsage/updated", {
      params: {
        turnId: "turn-one",
        tokenUsage: {
          total: { totalTokens: 8420, inputTokens: 6400, outputTokens: 2020 },
          last: { totalTokens: 1720 },
          modelContextWindow: 128000,
        },
      },
    });
    add(50000, "turn/completed", {
      params: { turn: { id: "turn-one", status: "completed" } },
    });
    add(62000, "run/input", {
      runId: "run-two",
      input: { text: "继续检查输出目录并生成报告。" },
    });
    add(62050, "turn/started", { params: { turn: { id: "turn-two" } } });
    item(
      63000,
      "report",
      "commandExecution",
      { command: "python report.py --dry-run" },
      false,
      "turn-two",
    );
    item(
      70600,
      "report",
      "commandExecution",
      {
        command: "python report.py --dry-run",
        status: "completed",
        exitCode: 0,
        aggregatedOutput: "演示报告已生成。",
      },
      true,
      "turn-two",
    );
    item(71000, "response-two", "agentMessage", {}, false, "turn-two");
    item(
      75800,
      "response-two",
      "agentMessage",
      { text: "报告生成流程已检查，所有结果均为演示数据。" },
      true,
      "turn-two",
    );
    add(76000, "turn/completed", {
      params: { turn: { id: "turn-two", status: "completed" } },
    });
    // Thousands of deltas are excluded by the trace API.
    for (let i = 0; i < 3000; i++)
      add(42500 + i, "item/agentMessage/delta", {
        params: { turnId: "turn-one", itemId: "response", delta: "字" },
      });
    events.sort((a, b) => a.time.localeCompare(b.time));
    const many = [
      {
        time: new Date(origin).toISOString(),
        method: "run/input",
        direction: "internal",
        data: { runId: "long-run", input: { text: "批量工具检查" } },
      },
    ];
    for (let i = 0; i < 2000; i++)
      for (const done of [false, true])
        many.push({
          time: new Date(origin + i * 40 + (done ? 30 : 0)).toISOString(),
          method: done ? "item/completed" : "item/started",
          direction: "in",
          data: {
            params: {
              turnId: "long-turn",
              item: {
                id: "tool-" + i,
                type: "commandExecution",
                command: "check-resource " + i,
                status: done ? "completed" : "inProgress",
                aggregatedOutput: done ? "ok" : "",
              },
            },
          },
        });
    const seeded = spawnSync(
      "python3",
      [
        "-c",
        `import json,sqlite3,sys\nx=json.load(sys.stdin)\ndb=sqlite3.connect(x['db'])\nfor session,events in x['sessions']:\n for e in events: db.execute('INSERT INTO events(session,time,direction,method,data) VALUES(?,?,?,?,?)',(session,e['time'],e['direction'],e['method'],json.dumps(e['data'],ensure_ascii=False).encode('utf-8')))\ndb.commit()`,
      ],
      {
        input: JSON.stringify({
          db: path.join(temp, "demo/state.db"),
          sessions: [
            [s.id, events],
            [long.id, many],
          ],
        }),
        encoding: "utf8",
      },
    );
    assert.equal(seeded.status, 0, seeded.stderr);
    browser = await chromium.launch({
      headless: true,
      executablePath: process.env.CHROME_PATH || undefined,
      args: ["--no-sandbox", "--disable-dev-shm-usage", "--disable-gpu"],
    });
    const page = await browser.newPage({
        viewport: { width: 1600, height: 1100 },
      }),
      errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    page.on("console", (m) => {
      if (m.type() === "error" && !/401|favicon|需要登录/.test(m.text()))
        errors.push(m.text());
    });
    await page.goto(base);
    await page.locator("#token").fill(token);
    await page.locator("#login-form button").click();
    await page.locator("#login").waitFor({ state: "hidden" });
    await page.waitForFunction(
      () => document.querySelector("#session-list").children.length > 0,
    );
    await page.evaluate((id) => selectSession(id), s.id);
    await page.locator("#trace-button").click();
    await page
      .waitForFunction(
        () => document.querySelector("#trace-run").options.length === 3,
        null,
        { timeout: 10000 },
      )
      .catch(async (e) => {
        console.log(
          "TRACE STATE",
          await page.evaluate(() => ({
            load: document.querySelector("#trace-load").textContent,
            options: document.querySelector("#trace-run").innerText,
          })),
          errors,
        );
        await page.screenshot({ path: "docs/screenshots/trace-debug.png" });
        throw e;
      });
    await page.locator(".trace-step.selected").waitFor();
    assert.match(
      await page.locator("#trace-detail-content").innerText(),
      /MODEL_ENDPOINT/,
    );
    assert.equal(
      await page.locator("#trace-stats strong").nth(1).innerText(),
      "13.60 s",
    );
    const shots = path.resolve("docs/screenshots");
    fs.mkdirSync(shots, { recursive: true });
    await page.screenshot({
      path: path.join(shots, "trace-workspace.png"),
      fullPage: true,
    });
    const oldRange = await page.locator("#trace-range-label").innerText();
    await page.locator("#trace-zoom-in").click();
    assert.notEqual(
      await page.locator("#trace-range-label").innerText(),
      oldRange,
    );
    await page.locator("#trace-fit").click();
    assert.ok(await page.locator("#trace-next-error").isDisabled());
    await page.locator("#trace-order").selectOption("duration");
    assert.equal(
      await page.locator(".trace-step").first().getAttribute("data-row"),
      "item-turn-one-command",
    );
    await page.locator("#trace-longest").click();
    assert.match(
      await page.locator("#trace-detail-content").innerText(),
      /18.60 s/,
    );
    await page.screenshot({
      path: path.join(shots, "trace-duration.png"),
      fullPage: true,
    });
    // A completed trace refresh must preserve mounted rows and keyboard focus.
    await page.locator("#trace-order").focus();
    await page.evaluate(() => {
      window.traceRefreshBefore = document.querySelector(".trace-step");
    });
    const refreshed = page.waitForResponse((r) => r.url().includes("/trace?"));
    await page.evaluate(() => document.querySelector("#trace-refresh").click());
    await refreshed;
    assert.ok(
      await page.evaluate(
        () =>
          window.traceRefreshBefore === document.querySelector(".trace-step"),
      ),
    );
    assert.equal(
      await page.evaluate(() => document.activeElement.id),
      "trace-order",
    );
    await page.locator("#trace-run").selectOption("run-one");
    await page.locator("#trace-errors").check();
    await page.waitForFunction(
      () => document.querySelector("#trace-count").textContent === "1 步",
    );
    await page.locator(".trace-step").click();
    await page.locator(".trace-source").last().click();
    await page.locator(".trace-sources details").waitFor();
    assert.match(
      await page.locator(".trace-sources details").innerText(),
      /exitCode/,
    );
    const selected = await page
      .locator(".trace-step.selected")
      .getAttribute("data-row");
    await page.reload();
    await page.locator("#trace-workspace").waitFor({ state: "visible" });
    await page.locator(".trace-step.selected").waitFor();
    assert.equal(await page.locator("#trace-run").inputValue(), "run-one");
    assert.equal(
      await page.locator(".trace-step.selected").getAttribute("data-row"),
      selected,
    );
    assert.ok(await page.locator("#trace-errors").isChecked());
    assert.equal(await page.locator("#trace-order").inputValue(), "duration");
    await page.locator("#trace-order").selectOption("time");
    await page.locator("#trace-errors").uncheck();
    await page.locator("#trace-search").fill("resource-catalog");
    await page.waitForFunction(
      () => document.querySelector("#trace-count").textContent === "1 步",
    );
    await page.locator(".trace-step").click();
    assert.match(
      await page.locator("#trace-detail-content").innerText(),
      /list_services/,
    );
    await page.locator("#trace-search").fill("");
    await page.locator("#trace-steps").click();
    assert.match(
      await page.locator("#trace-range-label").innerText(),
      /宽度不代表耗时/,
    );
    await page.locator("#trace-next").click();
    await page.locator("#trace-time").click();
    await page.locator("#trace-focus").click();
    await page.locator("#trace-detail").waitFor({ state: "hidden" });
    await page.locator("#trace-focus").click();
    const download = page.waitForEvent("download");
    await page.locator("#trace-export").click();
    const file = await download;
    const payload = JSON.parse(fs.readFileSync(await file.path(), "utf8"));
    assert.equal(payload.format, "rundesk-trace/1");
    assert.equal(payload.runs.length, 1);
    await page.setViewportSize({ width: 390, height: 900 });
    await page.locator("#trace-fit").click();
    await page.locator("#trace-errors").check();
    await page.locator(".trace-step").click();
    await page.screenshot({
      path: path.join(shots, "trace-mobile.png"),
      fullPage: true,
    });
    assert.ok(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    );
    await page.locator("#trace-detail-close").click();
    await page.locator("#trace-close").click();
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.evaluate((id) => selectSession(id), long.id);
    await page.locator("#trace-button").click();
    await page.waitForFunction(
      () => document.querySelector("#trace-count").textContent === "2001 步",
      null,
      { timeout: 20000 },
    );
    assert.ok((await page.locator(".trace-step").count()) < 60);
    await page
      .locator("#trace-list")
      .evaluate((e) => (e.scrollTop = e.scrollHeight));
    await page
      .locator(".trace-step")
      .filter({ hasText: "check-resource 1999" })
      .waitFor();
    assert.ok((await page.locator(".trace-step").count()) < 60);
    await page.locator("#trace-close").click();
    const live = await call("/sessions", {
      workspaceId: w.id,
      title: "实时轨迹检查",
    });
    await page.evaluate((id) => selectSession(id), live.id);
    await call("/sessions/" + live.id + "/turns", { text: "轨迹 审批" });
    await page.locator(".approval").waitFor();
    await page.locator("#trace-button").click();
    await page.waitForFunction(
      () =>
        document
          .querySelector("#trace-detail-content")
          .innerText.includes("等待确认") ||
        document.querySelector("#trace-count").textContent !== "0 步",
    );
    await page.locator("#trace-refresh").click();
    await page.locator("#trace-close").click();
    await page.getByRole("button", { name: "允许本次", exact: true }).click();
    await page.waitForFunction(
      () => document.querySelector("#run-status").textContent === "已完成",
    );
    await page.locator("#trace-button").click();
    await page.waitForFunction(
      () => document.querySelector("#trace-count").textContent === "7 步",
    );
    await page.locator("#trace-search").fill("demo-inventory");
    await page.waitForFunction(
      () => document.querySelector("#trace-count").textContent === "1 步",
    );
    assert.equal(errors.length, 0, errors.join("\n"));
    console.log(
      "PASS: trace paging, 3,000 delta exclusion, 2,001-step virtualization, filters, zoom, sources, refresh restoration, export, focus and 390px layout; no JS errors",
    );
  } finally {
    if (browser) await browser.close();
    server.kill("SIGTERM");
    await new Promise((r) =>
      server.exitCode !== null ? r() : server.once("exit", r),
    );
    fs.rmSync(temp, { recursive: true, force: true });
  }
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
