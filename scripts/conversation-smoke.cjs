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
    await page.evaluate(() => {
      state.session = { id: "conversation-regression" };
      state.events = [
        {
          id: 1,
          method: "run/input",
          data: {
            runId: "run1",
            input: { text: "检查项目结构，说明执行过程。" },
          },
        },
        {
          id: 2,
          direction: "in",
          method: "item/started",
          data: {
            params: {
              turnId: "t1",
              item: {
                id: "reason",
                type: "reasoning",
                summary: [{ text: "先查看目录和配置，再核对入口。" }],
              },
            },
          },
        },
        {
          id: 3,
          direction: "in",
          method: "item/started",
          data: {
            params: {
              turnId: "t1",
              item: {
                id: "cmd",
                type: "commandExecution",
                command: "rg --files src",
                cwd: "/workspace/project",
                status: "inProgress",
                aggregatedOutput: "src/main.go\nsrc/web/app.js",
              },
            },
          },
        },
        {
          id: 4,
          direction: "in",
          method: "item/started",
          data: {
            params: {
              turnId: "t1",
              item: {
                id: "reply",
                type: "agentMessage",
                text: "正在检查项目结构。",
              },
            },
          },
        },
      ];
      renderMessages();
      window.cards = [...document.querySelectorAll("#messages > details")];
    });
    const cards = page.locator("#messages > details");
    await cards.nth(0).locator(":scope > summary").click();
    await cards.nth(1).locator(":scope > summary").click();
    await cards.nth(1).locator(".tool-raw > summary").click();
    for (let i = 0; i < 12; i++) {
      await page.evaluate((i) => {
        state.events.push({
          id: 10 + i,
          direction: "in",
          method: "item/commandExecution/outputDelta",
          data: {
            params: {
              turnId: "t1",
              itemId: "cmd",
              delta: "\nstream line " + i,
            },
          },
        });
        state.events.push({
          id: 30 + i,
          direction: "in",
          method: "item/reasoning/summaryTextDelta",
          data: {
            params: {
              turnId: "t1",
              itemId: "reason",
              summaryIndex: 0,
              delta: " · 检查中",
            },
          },
        });
        scheduleRender();
      }, i);
      await page.waitForTimeout(110);
      assert.equal(await cards.nth(0).getAttribute("open"), "");
      assert.equal(await cards.nth(1).getAttribute("open"), "");
    }
    await page.evaluate(() => {
      const output = window.cards[1].querySelector("pre");
      output.scrollTop = 30;
      window.outputScroll = output.scrollTop;
      state.events.push({
        id: 50,
        direction: "in",
        method: "item/completed",
        data: {
          params: {
            turnId: "t1",
            item: {
              id: "cmd",
              type: "commandExecution",
              command: "rg --files src",
              cwd: "/workspace/project",
              status: "completed",
              exitCode: 0,
              aggregatedOutput: Array.from(
                { length: 30 },
                (_, i) => "src/module-" + i + ".go",
              ).join("\n"),
            },
          },
        },
      });
      renderMessages();
      if (
        !window.cards.every(
          (card, i) =>
            card === document.querySelectorAll("#messages > details")[i] &&
            card.open,
        )
      )
        throw Error("remounted or collapsed");
      if (!window.cards[1].querySelector(".tool-raw").open)
        throw Error("nested disclosure reset");
      if (output.scrollTop !== window.outputScroll)
        throw Error("output scroll reset");
    });
    await cards.nth(1).locator(":scope > summary").click();
    await page.evaluate(() => renderMessages());
    assert.equal(await cards.nth(1).getAttribute("open"), null);
    assert.equal(await page.locator(".message.assistant .avatar").count(), 0);
    assert.equal(
      await page
        .locator(".message.assistant")
        .evaluate((n) => getComputedStyle(n).fontSize),
      "16px",
    );
    await cards.nth(1).locator(":scope > summary").click();
    await cards.nth(1).locator(".tool-raw > summary").click();
    await page.evaluate(() => {
      state.events.push({
        id: 55,
        direction: "in",
        method: "item/completed",
        data: {
          params: {
            turnId: "t1",
            item: {
              id: "cmd",
              type: "commandExecution",
              command: "rg --files src",
              cwd: "/workspace/project",
              status: "completed",
              exitCode: 0,
              aggregatedOutput:
                "src/main.go\nsrc/web/app.js\nsrc/web/style.css",
            },
          },
        },
      });
      state.events.push({
        id: 56,
        direction: "in",
        method: "item/completed",
        data: {
          params: {
            turnId: "t1",
            item: {
              id: "reason",
              type: "reasoning",
              summary: [{ text: "先查看目录和配置，再核对入口。" }],
            },
          },
        },
      });
      renderMessages();
      document.querySelector("#messages").scrollTop = 0;
      window.cards[1].querySelector("pre").scrollTop = 0;
    });
    await page.screenshot({
      path: path.join(screenshots, "conversation-stable.png"),
      fullPage: true,
    });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({
      path: path.join(screenshots, "conversation-stable-mobile.png"),
      fullPage: true,
    });
    assert.ok(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    );
    await page.evaluate(() => {
      state.events.push({
        id: 60,
        direction: "in",
        method: "item/started",
        data: {
          params: {
            turnId: "t2",
            item: { id: "cmd", type: "commandExecution", command: "pwd" },
          },
        },
      });
      renderMessages();
    });
    assert.equal(await cards.count(), 3);
    assert.equal(await cards.nth(2).getAttribute("open"), null);
    await page.evaluate(() => {
      resetConversation();
      state.session = { id: "other" };
      state.events = [
        {
          id: 1,
          direction: "in",
          method: "item/started",
          data: {
            params: {
              turnId: "t1",
              item: { id: "cmd", type: "commandExecution", command: "pwd" },
            },
          },
        },
      ];
      renderMessages();
    });
    assert.equal(await cards.nth(0).getAttribute("open"), null);
    assert.deepEqual(errors, []);
    console.log(
      "PASS conversation: stream/completion node identity, nested expansion, manual close, output scroll, reused IDs, session isolation, larger fonts, mobile width",
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
