// Optional development test: npm install --no-save playwright && npx playwright install chromium
// node scripts/ui-smoke.cjs
const { chromium } = require(process.env.PLAYWRIGHT_PATH || "playwright");
const { spawn } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const assert = require("node:assert/strict");
(async () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), "rundesk-ui-"));
  const port = 32000 + Math.floor(Math.random() * 20000),
    base = `http://127.0.0.1:${port}`;
  const token = "ui-test-only-token-with-32-characters";
  const binary = path.resolve(process.env.RUNDESK_BINARY || "bin/rundesk");
  const server = spawn(
    binary,
    ["--demo", "--data", temp, "--listen", `127.0.0.1:${port}`],
    {
      env: { ...process.env, RUNDESK_TOKEN: token },
      stdio: ["ignore", "ignore", "pipe"],
    },
  );
  let logs = "";
  server.stderr.on("data", (b) => (logs += b));
  let browser;
  try {
    for (let n = 0; n < 100; n++) {
      try {
        const r = await fetch(base + "/api/meta", {
          headers: { Authorization: "Bearer " + token },
        });
        if (r.ok) break;
      } catch {}
      await new Promise((r) => setTimeout(r, 100));
    }
    browser = await chromium.launch({
      headless: true,
      executablePath: process.env.CHROME_PATH || undefined,
      args: ["--no-sandbox", "--disable-dev-shm-usage", "--disable-gpu"],
    });
    const page = await browser.newPage({
      viewport: { width: 1440, height: 1000 },
    });
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    page.on("console", (m) => {
      if (m.type() === "error" && !/401|需要登录|favicon/.test(m.text())) {
        errors.push(m.text());
        console.error("BROWSER:", m.text());
      }
    });
    await page.goto(base);
    await page.locator("#token").fill(token);
    await page.locator("#login-form button").click();
    await page.locator("#login").waitFor({ state: "hidden" });
    await page.waitForFunction(
      () => document.querySelector("#workspace").options.length > 0,
    );
    const screenshots = path.resolve("docs/screenshots");
    fs.mkdirSync(screenshots, { recursive: true });
    await page.screenshot({
      path: path.join(screenshots, "workspace.png"),
      fullPage: true,
    });
    await page.locator("#prompt").fill("检查运行后台的连接。");
    await page.locator("#send").click();
    await page.waitForFunction(
      () => document.querySelector("#run-status").textContent === "已完成",
      {},
      { timeout: 15000 },
    );
    await page.locator("#debug-button").click();
    await page.locator('[data-debug-tab="files"]').click();
    await page.locator(".file-card").waitFor();
    assert.equal(await page.locator(".file-card").count(), 1);
    await page.getByRole("button", { name: "预览", exact: true }).click();
    await page.locator("#preview-content pre").waitFor();
    assert.match(
      await page.locator("#preview-content").innerText(),
      /协议模拟器/,
    );
    await page.locator("#close-preview").click();
    await page.reload();
    await page.locator(".message.assistant").waitFor();
    assert.match(
      await page.locator(".message.assistant").innerText(),
      /演示模式/,
    );
    await page.locator("#prompt").fill("演示审批流程。");
    await page.locator("#send").click();
    await page.locator(".approval").waitFor();
    await page.getByRole("button", { name: "允许本次", exact: true }).click();
    await page.waitForFunction(
      () => document.querySelector("#run-status").textContent === "已完成",
      {},
      { timeout: 15000 },
    );
    await page.locator("#debug-button").click();
    await page.screenshot({
      path: path.join(screenshots, "conversation.png"),
      fullPage: true,
    });
    await page.locator("#prompt").fill("演示审批与告警");
    await page.locator("#send").click();
    await page.getByRole("button", {name:"允许并保存命令规则", exact:true}).waitFor();
    assert.equal(await page.getByRole("button",{name:"会话内允许",exact:true}).count(),0);
    await page.reload();
    await page.getByRole("button",{name:"允许并保存命令规则",exact:true}).waitFor();
    await page.locator(".message.user").last().waitFor();
    await page.screenshot({path:path.join(screenshots,"approval-rules.png"),fullPage:true});
    await page.getByRole("button",{name:"允许并保存命令规则",exact:true}).click();
    await page.waitForFunction(()=>document.querySelector("#run-status").textContent==="已完成");
    await page.locator("#runtime-strip button").click();
    await page.locator("#runtime-dialog .runtime-notice").waitFor();
    assert.match(await page.locator("#runtime-detail").innerText(),/2 次/);
    await page.screenshot({path:path.join(screenshots,"runtime-status.png"),fullPage:true});
    await page.locator("#runtime-dialog button").click();
    await page.locator("#settings-button").click();
    await page.locator('[data-settings-tab="notes"]').click();
    const notes = page.locator("#settings-content textarea");
    await notes.fill("UI smoke test notes");
    await page.getByRole("button", { name: "保存笔记", exact: true }).click();
    await page.getByText("版本 1 · 最多 16 KiB", { exact: true }).waitFor();
    await page.locator('[data-settings-tab="skills"]').click();
    await page.getByRole("button", { name: "保存并重新扫描" }).waitFor();
    await page.locator("#skill-scope").selectOption("project");
    await page.locator("#settings-content input[type=text]").fill("ui-guide");
    await page
      .locator("#settings-content textarea")
      .fill(
        "---\nname: ui-guide\ndescription: UI test guidance\n---\n\nInspect only.\n",
      );
    await page.getByRole("button", { name: "保存并重新扫描" }).click();
    await page
      .locator(".skill-row strong")
      .filter({ hasText: "ui-guide" })
      .waitFor();
    await page.locator('[data-settings-tab="mcp"]').click();
    await page.getByRole("button", { name: "保存配置", exact: true }).waitFor();
    await page.locator("#mcp-name").fill("ui-mcp");
    await page.locator("#mcp-command").fill("unused-demo-command");
    await page.locator("#mcp-enabled").uncheck();
    await page.getByRole("button", { name: "保存配置", exact: true }).click();
    await page
      .locator(".skill-row strong")
      .filter({ hasText: "ui-mcp" })
      .waitFor();
    await page.screenshot({
      path: path.join(screenshots, "configuration.png"),
      fullPage: true,
    }); // Export and import MCP through the browser without automatically executing a service.
    let downloadPromise = page.waitForEvent("download");
    await page
      .getByRole("button", { name: "导出配置 JSON", exact: true })
      .click();
    let download = await downloadPromise;
    let bundle = JSON.parse(fs.readFileSync(await download.path(), "utf8"));
    assert.equal(bundle.kind, "rundesk.mcp");
    bundle.servers = { "ui-imported": bundle.servers["ui-mcp"] };
    await page.getByText("导入 MCP 配置", { exact: true }).click();
    await page.locator("#mcp-import-json").fill(JSON.stringify(bundle));
    await page.getByRole("button", { name: "确认导入", exact: true }).click();
    await page
      .locator(".skill-row strong")
      .filter({ hasText: "ui-imported" })
      .waitFor();
    await page.locator('[data-settings-tab="skills"]').click();
    await page
      .locator(".skill-row strong")
      .filter({ hasText: "ui-guide" })
      .waitFor();
    await page
      .getByRole("checkbox", { name: "启用 ui-guide", exact: true })
      .uncheck();
    await page.waitForFunction(
      () =>
        document.querySelector(".skill-row button:nth-of-type(2)")?.disabled ===
        true,
    );
    await page
      .getByRole("checkbox", { name: "启用 ui-guide", exact: true })
      .check();
    downloadPromise = page.waitForEvent("download");
    await page.getByRole("button", { name: "导出", exact: true }).click();
    download = await downloadPromise;
    assert.match(fs.readFileSync(await download.path(), "utf8"), /ui-guide/);
    page.once("dialog", (d) => d.accept());
    await page.getByRole("button", { name: "移除", exact: true }).click();
    await page
      .locator(".skill-row strong")
      .filter({ hasText: "ui-guide" })
      .waitFor({ state: "hidden" });
    await page.locator('[data-settings-tab="runtime"]').click();
    await page
      .getByRole("button", { name: "运行连接诊断", exact: true })
      .click();
    await page.waitForFunction(
      () => document.querySelectorAll(".check-ok").length === 5,
    );
    await page.locator("#close-settings").click();
    await page.locator("#prompt").fill("演示审批，再停止。");
    await page.locator("#send").click();
    await page.locator(".approval").waitFor();
    await page.locator("#stop").click();
    await page.waitForFunction(
      () => document.querySelector("#run-status").textContent === "已停止",
      {},
      { timeout: 15000 },
    );
    await page.locator(".approval").waitFor({ state: "hidden" });
    await page.locator("#session-menu summary").click();
    page.once("dialog", (d) => d.accept("RunDesk v0.5 会话测试"));
    await page.locator("#rename-session").click();
    await page.waitForFunction(
      () =>
        document.querySelector("#session-title").textContent ===
        "RunDesk v0.5 会话测试",
    );
    await page.locator("#session-menu summary").click();
    await page.locator("#pin-session").click();
    await page.waitForFunction(
      () => document.querySelector("#pin-session").textContent === "取消置顶",
    );
    await page.locator("#session-search").fill("不匹配");
    assert.equal(await page.locator("#session-list .session").count(), 0);
    await page.locator("#session-search").fill("会话测试");
    assert.equal(await page.locator("#session-list .session").count(), 1);
    await page.locator("#session-search").fill("");
    await page.locator("#session-menu summary").click();
    await page.locator("#archive-session").click();
    await page.waitForFunction(
      () => document.querySelector("#run-status").textContent === "已归档",
    );
    assert.equal(await page.locator("#prompt").isDisabled(), true);
    await page.locator("#session-menu summary").click();
    await page.locator("#archive-session").click();
    await page.waitForFunction(
      () => !document.querySelector("#prompt").disabled,
    );
    if (!(await page.locator('[data-debug-tab="events"]').isVisible())) await page.locator("#debug-button").click();
    await page.locator('[data-debug-tab="events"]').click();
    await page.locator("#event-category").selectOption("approvals");
    await page.locator("#event-direction").selectOption("in");
    await page.locator("#event-search").click();
    await page.waitForFunction(() =>
      document.querySelector("#event-mode").textContent.startsWith("历史查询"),
    );
    assert.ok((await page.locator(".event").count()) >= 2);
    for (const text of await page.locator(".event .method").allTextContents())
      assert.match(text, /requestApproval/);
    await page.screenshot({
      path: path.join(screenshots, "history-query.png"),
      fullPage: true,
    });
    await page.locator("#event-live").click();
    await page.locator("#session-menu summary").click();
    downloadPromise = page.waitForEvent("download");
    await page.locator("#export-markdown").click();
    download = await downloadPromise;
    const markdown = fs.readFileSync(await download.path(), "utf8");
    assert.match(markdown, /RunDesk v0.5 会话测试/);
    assert.doesNotMatch(markdown, /UI smoke test notes/);
    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({
      path: path.join(screenshots, "mobile.png"),
      fullPage: true,
    });
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
      true,
      "mobile horizontal overflow",
    );
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.locator("#session-menu summary").click();
    page.once("dialog", (d) => d.accept());
    await page.locator("#delete-session").click();
    await page.waitForFunction(
      () => document.querySelectorAll("#session-list .session").length === 0,
    );
    // Instance controls: isolate configurations in one workspace and retain binding on reload.
    await page.locator("#manage-instances").click();
    await page.locator('[data-settings-tab="instances"]').click();
    await page.locator("#new-instance-name").fill("UI 独立助手");
    await page.getByRole("button", { name: "创建并切换", exact: true }).click();
    await page.waitForFunction(
      () =>
        document.querySelector("#instance").selectedOptions[0]?.textContent ===
        "UI 独立助手",
    );
    await page.locator("#instance-model").fill("demo-fixture");
    await page.locator("#permission-sandbox").selectOption("read-only");
    await page.locator("#permission-policy").selectOption("never");
    await page.getByRole("button", { name: "保存实例", exact: true }).click();
    await page.waitForFunction(()=>document.querySelector("#permission-sandbox")?.value==="read-only");
    await page.screenshot({path:path.join(screenshots,"instance-permissions.png"),fullPage:true});

    await page.locator("#new-instance-name").waitFor();
    await page
      .getByRole("button", { name: "读取登录状态", exact: true })
      .click();
    await page.waitForFunction(() =>
      document
        .querySelector("#instance-account")
        .textContent.includes('"account"'),
    );
    await page.locator("#settings-content").evaluate((n) => (n.scrollTop = 0));
    await page.screenshot({
      path: path.join(screenshots, "instances.png"),
      fullPage: true,
    });
    await page.locator('[data-settings-tab="skills"]').click();
    await page.locator("#skill-name").fill("private-guide");
    await page
      .locator("#skill-content")
      .fill(
        "---\nname: private-guide\ndescription: instance skill\n---\nInspect only.\n",
      );
    await page.getByRole("button", { name: "保存并重新扫描" }).click();
    await page
      .locator(".skill-row strong")
      .filter({ hasText: "private-guide" })
      .waitFor();
    await page.locator('[data-settings-tab="mcp"]').click();
    await page.locator("#mcp-name").waitFor();
    assert.equal(await page.locator(".skill-row").count(), 0);
    await page.locator("#mcp-name").fill("instance-mcp");
    await page.locator("#mcp-command").fill("unused-instance-command");
    await page.locator("#mcp-enabled").uncheck();
    await page.getByRole("button", { name: "保存配置", exact: true }).click();
    await page
      .locator(".skill-row strong")
      .filter({ hasText: "instance-mcp" })
      .waitFor();
    await page.locator("#close-settings").click();
    const iid = await page.locator("#instance").inputValue();
    await page.locator("#prompt").fill("instance run");
    await page.locator("#send").click();
    await page.waitForFunction(
      () => document.querySelector("#run-status").textContent === "已完成",
    );
    await page.reload();
    await page.locator(".message.assistant").waitFor();
    assert.equal(await page.locator("#instance").inputValue(), iid);
    assert.equal(await page.locator("#model").inputValue(), "demo-fixture");
    await page.locator("#instance").selectOption("default");
    await page.waitForFunction(
      () => document.querySelector("#session-count").textContent === "0",
    );
    await page.locator("#settings-button").click();
    await page.locator('[data-settings-tab="skills"]').click();
    await page.locator("#skill-name").waitFor();
    assert.equal(
      await page
        .locator(".skill-row strong")
        .filter({ hasText: "private-guide" })
        .count(),
      0,
    );
    await page.locator('[data-settings-tab="mcp"]').click();
    await page.locator("#mcp-name").waitFor();
    assert.equal(
      await page
        .locator(".skill-row strong")
        .filter({ hasText: "instance-mcp" })
        .count(),
      0,
    );
    assert.equal(
      await page
        .locator(".skill-row strong")
        .filter({ hasText: "ui-mcp" })
        .count(),
      1,
    );
    await page.locator("#close-settings").click();
    await page.locator("#instance").selectOption(iid);
    await page.locator(".message.assistant").waitFor();
    await page.locator("#manage-instances").click();
    await page.locator('[data-settings-tab="instances"]').click();
    await page
      .getByRole("button", { name: "重新加载空闲连接", exact: true })
      .click();
    await page.waitForFunction(() =>
      document
        .querySelector("#instance-account")
        .textContent.includes('"account"'),
    );
    await page.locator("#close-settings").click();
    console.log(
      "PASS: instance create/edit/switch, private Skill/MCP, default model, immutable session binding after refresh, idle reload",
    );
    assert.deepEqual(errors, []);
    console.log(
      "PASS: v0.5 rename/pin/archive/delete/search, history filters, Markdown export, MCP bundle import/export, Skill backup removal, diagnostics; login, streamed turn, file preview, reload replay, approval, notes, Skills, MCP, stop, responsive layout; no JS page errors.",
    );
  } finally {
    if (browser) await browser.close();
    server.kill("SIGTERM");
    await new Promise((resolve) => {
      if (server.exitCode !== null) return resolve();
      const timer = setTimeout(() => {
        server.kill("SIGKILL");
        resolve();
      }, 6000);
      server.once("exit", () => {
        clearTimeout(timer);
        resolve();
      });
    });
    fs.rmSync(temp, { recursive: true, force: true });
    if (server.exitCode !== 0 && server.exitCode !== null) console.error(logs);
  }
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
