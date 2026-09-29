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
    await page.locator("#prompt").fill("审批：等待确认期间测试补充指令。");
    await page.locator("#send").click();
    await page.waitForFunction(
      () => state.session?.status === "waiting" && state.session.turnId,
    );
    const before = await page.evaluate(() => ({
      id: state.session.id,
      turn: state.session.turnId,
      run: state.session.runId,
    }));
    assert.ok(await page.locator("#send").isEnabled());
    assert.equal(await page.locator("#send").innerText(), "补充");
    let submissions = 0;
    await page.route("**/api/sessions/*/steer", async (route) => {
      submissions++;
      await new Promise((resolve) => setTimeout(resolve, 250));
      await route.continue();
    });
    await page.locator("#prompt").fill("先解释原因，不要修改文件。");
    await page.locator("#prompt").press("Enter");
    await page.locator("#prompt").press("Enter");
    await page.waitForFunction(() =>
      document.querySelector("#send-feedback").textContent.includes("已接收"),
    );
    assert.equal(submissions, 1);
    await page.waitForFunction(() =>
      document
        .querySelector("#messages")
        .textContent.includes("补充指令 · 已接收"),
    );
    const after = await page.evaluate(() => ({
      id: state.session.id,
      turn: state.session.turnId,
      run: state.session.runId,
    }));
    assert.deepEqual(after, before);
    assert.equal(await page.locator("#prompt").inputValue(), "");
    assert.equal(await page.locator("#stop").isVisible(), true);
    const projected = await page.evaluate(async () => {
      const r = await api(`/sessions/${state.session.id}/trace`);
      return JSON.stringify(r);
    });
    assert.ok(projected.includes("run/steer"));
    await page.reload();
    await page.waitForFunction(() =>
      document
        .querySelector("#messages")
        .textContent.includes("先解释原因，不要修改文件。"),
    );
    assert.equal(await page.locator(".steer-label").count(), 1);
    // Deterministic transport rejection: preserve the draft and never auto-fallback to start.
    await page.route("**/api/sessions/*/steer", (route) =>
      route.fulfill({
        status: 409,
        contentType: "application/json",
        body: JSON.stringify({ error: "任务已结束，补充指令未发送" }),
      }),
    );
    await page.locator("#prompt").fill("失败后保留这段文字");
    await page.locator("#send").click();
    await page.waitForFunction(() =>
      document.querySelector("#send-feedback").textContent.includes("未确认"),
    );
    assert.equal(
      await page.locator("#prompt").inputValue(),
      "失败后保留这段文字",
    );
    await page.unroute("**/api/sessions/*/steer");
    await page.locator("#stop").click();
    await page.waitForFunction(() => state.session?.status === "interrupted");
    assert.equal(await page.locator("#send").innerText(), "↑");
    await page.locator("#send").click();
    await page.waitForFunction(
      () => document.querySelector("#run-status").textContent === "已完成",
      {},
      { timeout: 15000 },
    );
    assert.equal(await page.locator("#prompt").inputValue(), "");
    assert.ok(
      await page.evaluate((old) => state.session.turnId !== old, before.turn),
    );
    // A normal 409 is expected during the rejection check; no JS runtime errors.
    assert.deepEqual(
      errors.filter(
        (e) =>
          !e.includes("409") &&
          !e.startsWith("Error: 任务已结束，补充指令未发送"),
      ),
      [],
    );
    console.log(
      "PASS steer UI: live append, double-submit guard, same run/turn, event replay, trace, rejection retains draft, stop and next normal turn",
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
