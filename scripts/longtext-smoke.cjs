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
    const longText = "  原文😀\r\n".repeat(1500) + "尾部  ";
    await page.locator("#prompt").fill("审批：请分析附件。");
    await page.evaluate((text) => {
      const p = document.querySelector("#prompt");
      p.setSelectionRange(p.value.length, p.value.length);
      const d = new DataTransfer();
      d.setData("text/plain", text);
      p.dispatchEvent(
        new ClipboardEvent("paste", {
          clipboardData: d,
          bubbles: true,
          cancelable: true,
        }),
      );
    }, longText);
    assert.equal(
      await page.locator("#prompt").inputValue(),
      "审批：请分析附件。",
    );
    assert.equal(await page.locator(".attachment-preview").count(), 1);
    await page.locator(".attachment-preview").click();
    assert.equal(
      await page.locator("#preview-content pre").textContent(),
      longText,
    );
    await page.locator("#close-preview").click();
    await page.route("**/api/workspaces/*/uploads", (route) =>
      route.fulfill({
        status: 500,
        contentType: "application/json",
        body: JSON.stringify({ error: "upload-test-failure" }),
      }),
    );
    await page.locator("#send").click();
    await page.waitForFunction(
      () =>
        !state.sending &&
        document.querySelector("#send-feedback").textContent.includes("未确认"),
    );
    assert.equal(await page.locator(".attachment-preview").count(), 1);
    assert.equal(
      await page.locator("#prompt").inputValue(),
      "审批：请分析附件。",
    );
    await page.unroute("**/api/workspaces/*/uploads");
    let uploads = 0;
    page.on("request", (r) => {
      if (r.url().endsWith("/uploads")) uploads++;
    });
    await page.locator("#send").click();
    await page.waitForFunction(
      () => state.session?.status === "waiting" && !state.sending,
    );
    assert.equal(uploads, 1);
    const observed = await page.evaluate(async () => {
      const e = state.events.find((e) => e.method === "run/input");
      const content = await fetch(
        `/api/workspaces/${ws().id}/file?path=${encodeURIComponent(e.data.input.files[0])}`,
      ).then((r) => r.text());
      return { input: e.data.input, content };
    });
    assert.equal(observed.content, longText);
    assert.equal(observed.input.text, "审批：请分析附件。");
    assert.equal(observed.input.files.length, 1);
    assert.equal(await page.locator(".attachment-preview").count(), 0);
    // Directly typed long input also converts, and travels through steering.
    await page.locator("#prompt").fill(longText);
    await page.locator("#send").click();
    await page.waitForFunction(
      () =>
        !state.sending && state.events.some((e) => e.method === "run/steer"),
    );
    assert.equal(uploads, 2);
    const steer = await page.evaluate(
      () => state.events.find((e) => e.method === "run/steer").data.input,
    );
    assert.equal(steer.files.length, 1);
    assert.ok(steer.text.length < 100);
    assert.equal(await page.locator("#prompt").inputValue(), "");
    await page.evaluate((text) => stageLongText(text), longText);
    await page.locator("#attachments .chip > button").last().click();
    assert.equal(await page.locator(".attachment-preview").count(), 0);
    await page.locator("#stop").click();
    await page.waitForFunction(() => state.session?.status === "interrupted");
    assert.deepEqual(
      errors.filter(
        (e) => !e.includes("500") && !e.includes("upload-test-failure"),
      ),
      [],
    );
    console.log(
      "PASS long text: paste staging/preview/removal, UTF-8 exact upload, failure retained/retry, new session attachments, direct input steering, concise prompt",
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
