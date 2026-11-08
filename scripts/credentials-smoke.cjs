// Real Chromium and an isolated RunDesk demo service; no model or Docker calls.
const {chromium} = require(process.env.PLAYWRIGHT_PATH || 'playwright');
const {spawn} = require('node:child_process');
const fs = require('node:fs'), os = require('node:os'), path = require('node:path');
const assert = require('node:assert/strict'), crypto = require('node:crypto');
const qaOutput = require('./qa-output.cjs')(__filename);
const {runSuite} = require('./fixtures/suite.cjs');

async function runCredentials() {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'rundesk-key-'));
  const root = path.resolve(__dirname, '..');
  const binary = path.resolve(process.env.RUNDESK_TEST_HOST || path.join(root, 'bin/rundesk'));
  const token = 'key-demo-admin-token-1234567890', checks = [], errors = [];
  const server = spawn(binary, ['--demo', '--data', temp, '--listen', '127.0.0.1:0'], {
    env: {...process.env, RUNDESK_TOKEN: token, CODEX_HOME: path.join(temp, 'codex')},
    stdio: ['ignore', 'ignore', 'pipe'],
  });
  let browser;
  try {
    const base = await new Promise((resolve, reject) => {
      let log = '';
      const timer = setTimeout(() => reject(Error('demo service did not start: ' + log)), 15000);
      server.stderr.on('data', data => {
        log += data;
        const url = log.match(/RunDesk (http:\/\/127\.0\.0\.1:\d+)/)?.[1];
        if (url) { clearTimeout(timer); resolve(url); }
      });
      server.once('error', error => { clearTimeout(timer); reject(error); });
      server.once('exit', code => { clearTimeout(timer); reject(Error('demo service exited ' + code + ': ' + log)); });
    });
    const call = async (method, route, body, auth = token) => {
      const response = await fetch(base + '/api/v1' + route, {
        method, headers: {Authorization: 'Bearer ' + auth, 'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID()},
        body: body ? JSON.stringify(body) : undefined,
      });
      return {status: response.status, body: await response.json()};
    };
    const w = (await call('GET', '/workspaces')).body[0];
    const i = (await call('POST', '/instances', {name: '新闻研究'})).body;
    assert.equal((await call('PUT', '/applications/news2douyin', {name: '新闻研究', instanceId: i.id, workspaceId: w.id})).status, 200);
    browser = await chromium.launch({headless: true, executablePath: process.env.CHROME_PATH || process.env.CHROMIUM_PATH, args: JSON.parse(process.env.CHROMIUM_ARGS_JSON || '["--no-sandbox"]')});
    const page = await browser.newPage({locale: 'zh-CN', viewport: {width: 1440, height: 1000}});
    page.setDefaultTimeout(15000);
    page.on('pageerror', error => errors.push(error.message));
    await page.addInitScript(() => {
      localStorage.setItem('rundesk-language', 'zh');
      sessionStorage.setItem('rundesk-setup-dismissed', '1');
    });
    await page.goto(base);
    await page.locator('#token').fill(token);
    await page.locator('#login-form button').click();
    await page.waitForFunction(() => state.workspaces.length > 0);
    await page.locator('#nav-applications').click();
    await page.locator('[data-app-id=news2douyin]').getByRole('button', {name: '设置', exact: true}).click();
    await page.locator('.application-config > summary').click();
    await page.getByRole('button', {name: 'API 凭据', exact: true}).click();
    await page.getByRole('button', {name: '创建凭据', exact: true}).click();
    await page.locator('#key-name').fill('新闻后台测试凭据');
    await page.locator('.key-dialog form button[type=submit]').click();
    await page.locator('#key-secret').waitFor();
    const secret = await page.locator('#key-secret').inputValue();
    assert(secret.startsWith('rd_app_'));
    checks.push('application settings creates a one-time credential for the selected workspace');
    const identity = await call('GET', '/whoami', null, secret);
    assert.equal(identity.body.kind, 'application');
    assert.equal(identity.body.credential.appId, 'news2douyin');
    assert.equal((await call('GET', '/instances', null, secret)).status, 403);
    checks.push('credential authenticates and cannot read administrator configuration');
    const task = await call('POST', '/tasks', {workspaceId: w.id, title: '新闻后台任务', input: {text: '新闻背景分析'}}, secret);
    assert.equal(task.status, 202);
    assert.equal(task.body.spec.instanceId, i.id);
    checks.push('application creates a bound queue task without declaring an instance');
    const pictures = process.env.UI_SCREENSHOT_DIR || qaOutput('screenshots');
    fs.mkdirSync(pictures, {recursive: true});
    await page.screenshot({path: path.join(pictures, 'credential-once.png'), mask: [page.locator('#key-secret')]});
    await page.locator('.key-dialog').filter({hasText: '保存应用凭据'}).getByRole('button', {name: '关闭', exact: true}).click();
    await page.locator('#key-secret').waitFor({state: 'detached'});
    assert.equal(await page.locator('#key-secret').count(), 0);
    assert(!await page.evaluate(value => Object.values(localStorage).some(item => item.includes(value)) || Object.values(sessionStorage).some(item => item.includes(value)), secret));
    checks.push('closing removes the displayed secret and browser storage contains no secret');
    await page.screenshot({path: path.join(pictures, 'credentials-desktop.png')});
    await page.setViewportSize({width: 390, height: 844});
    await page.screenshot({path: path.join(pictures, 'credentials-mobile.png')});
    assert(await page.evaluate(() => document.querySelector('dialog[open]').scrollWidth <= document.querySelector('dialog[open]').clientWidth));
    checks.push('mobile credential dialog fits');
    // Admission's two additional assertions survive the duplicate-flow merge.
    const confirmation = page.waitForEvent('dialog');
    const revoking = page.getByRole('button', {name: '撤销', exact: true}).click();
    const dialog = await confirmation;
    try {
      assert(dialog.message().includes('0.19.2'));
      assert(dialog.message().includes('单独停止'));
    } finally { await dialog.accept(); }
    await revoking;
    checks.push('revocation confirmation describes queued-task admission and separate running-task cancellation');
    await page.locator('.schedule-card').filter({hasText: '已撤销'}).waitFor();
    assert.equal((await call('GET', '/whoami', null, secret)).status, 401);
    checks.push('UI revocation rejects later application requests');
    assert.deepEqual(errors, []);
    checks.push('no uncaught browser errors');
    return {passed: true, mode: 'real Chromium and native demo JSONL; disposable credentials; no model calls', checks};
  } finally {
    await browser?.close();
    if (server.pid && server.exitCode === null) await new Promise(resolve => {server.once('exit', resolve); server.kill();});
    fs.rmSync(temp, {recursive: true, force: true});
  }
}

module.exports = {runCredentials};
if (require.main === module) runSuite(__filename, [
  {name: 'credential-lifecycle-and-admission', run: runCredentials, report: 'credentials-validation.json'},
]).catch(error => {console.error(error); process.exitCode = 1;});
