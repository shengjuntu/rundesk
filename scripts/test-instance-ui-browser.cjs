/* Real browser + disposable RunDesk --demo backend. No Docker or external model.
 * RUNDESK_TEST_BINARY, CHROMIUM_PATH and optional CHROMIUM_ARGS_JSON select tools.
 * UI_SCREENSHOT_DIR is only for local visual review; screenshots are not reports. */
'use strict';
const {chromium} = require(process.env.PLAYWRIGHT_PATH || 'playwright');
const {spawn} = require('node:child_process'), fs = require('node:fs'), path = require('node:path'), os = require('node:os'), assert = require('node:assert/strict'), crypto = require('node:crypto');
(async () => {
  const data = fs.mkdtempSync(path.join(os.tmpdir(),'rundesk-instance-ui-'));
  const port = 35000 + Math.floor(Math.random()*15000), base = `http://127.0.0.1:${port}`, token = crypto.randomBytes(24).toString('hex');
  const checks = [], pageErrors = [], shots = process.env.UI_SCREENSHOT_DIR;
  if (shots) fs.mkdirSync(shots,{recursive:true});
  const testHome = path.join(data,'home'); fs.mkdirSync(testHome);
  const server = spawn(path.resolve(process.env.RUNDESK_TEST_BINARY || 'bin/rundesk'),['--demo','--data',data,'--listen',`127.0.0.1:${port}`],{env:{...process.env,HOME:testHome,CODEX_HOME:path.join(testHome,'codex'),RUNDESK_TOKEN:token},stdio:['ignore','pipe','pipe']});
  let browser, serverLog = ''; server.stdout.on('data',s => {serverLog+=s}); server.stderr.on('data',s => {serverLog+=s});
  async function call(method,route,body) {
    const response = await fetch(base+'/api/v1'+route,{method,headers:{Authorization:'Bearer '+token,'Content-Type':'application/json','Idempotency-Key':crypto.randomUUID()},body:body ? JSON.stringify(body) : undefined});
    const value = await response.json(); assert(response.ok,`${method} ${route}: ${response.status} ${JSON.stringify(value)}`); return value;
  }
  async function screenshot(page,name) { if(shots) await page.screenshot({path:path.join(shots,name+'.png'),fullPage:true}); }
  async function fits(page,selector) {
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth+1),'document overflows horizontally');
    assert(await page.locator(selector).evaluate(n => n.scrollWidth <= n.clientWidth+1),selector+' overflows horizontally');
  }
  try {
    let started = false;
    for(let n=0;n<150;n++) {
      if(server.exitCode !== null) throw Error('demo server exited: '+serverLog);
      try { if((await fetch(base+'/api/v1/meta')).status===401) {started=true;break} } catch {}
      await new Promise(resolve => setTimeout(resolve,60));
    }
    assert(started,'demo server did not become ready');
    const i = await call('POST','/instances',{name:'新闻研究助手',description:'配置采集与研究能力，在不同项目中开展任务。'});
    const wa = await call('POST','/workspaces',{name:'新闻选题 A'}), wb = await call('POST','/workspaces',{name:'新闻选题 B'});
    await call('POST','/instances',{name:'视频制作助手',description:'独立管理视频任务的技能和外部服务。'});
    browser = await chromium.launch({executablePath:process.env.CHROMIUM_PATH,headless:true,args:JSON.parse(process.env.CHROMIUM_ARGS_JSON || '["--no-sandbox"]')});
    const page = await browser.newPage({viewport:{width:1440,height:1000},locale:'zh-CN'});
    page.setDefaultTimeout(15000); page.on('pageerror',error => pageErrors.push(error.message));
    await page.goto(base); await page.locator('#token').fill(token); await page.locator('#login-form button').click();
    await page.waitForFunction(() => state.view === 'instances' && document.querySelector('.instance-card'));
    assert.equal(await page.locator('.instance-card').count(),3);
    const header = await page.locator('main>header').innerText();
    for(const label of ['Kun 分叉','评测任务集','K4 Skill 工作台','离线实验']) assert(!header.includes(label),'research launcher remains in ordinary header: '+label);
    await fits(page,'#instances-page'); await screenshot(page,'instances-desktop');
    checks.push('real login opens the instance catalog; independent instances are visible and research launchers are absent');

    await page.locator(`.instance-card[data-instance-id="${i.id}"]`).getByRole('button',{name:'管理实例',exact:true}).click();
    await page.locator('#instance-workspace').selectOption(wb.id);
    await page.waitForFunction(wid => document.querySelector('#instance-workspace')?.value === wid && location.hash.includes('workspace='+wid),wb.id);
    await page.locator('.instance-capability').filter({has:page.getByText('Skills',{exact:true})}).click();
    await page.locator('#skill-scope').waitFor({state:'attached'});
    assert.equal(await page.locator('#instance').inputValue(),i.id); assert.equal(await page.locator('#workspace').inputValue(),wb.id);
    assert((await page.locator('.capability-owner').innerText()).includes('新闻选题 B'));
    await page.reload(); await page.locator('#skill-scope').waitFor({state:'attached'});
    assert.equal(await page.locator('#instance').inputValue(),i.id); assert.equal(await page.locator('#workspace').inputValue(),wb.id);
    await page.locator('#capability-page').getByRole('button',{name:'← 返回',exact:true}).click();
    await page.locator('#instance-workspace').waitFor();
    await page.locator('.instance-capability').filter({has:page.getByText('MCP',{exact:true})}).click();
    await page.locator('#mcp-name').waitFor({state:'attached'});
    assert.equal(await page.locator('#workspace').inputValue(),wb.id);
    await screenshot(page,'instance-mcp-desktop');
    await page.locator('#capability-page').getByRole('button',{name:'← 返回',exact:true}).click();
    checks.push('Skills and MCP operate on the chosen instance/workspace; deep-link reload and back preserve context');

    await page.locator('#instances-page').getByRole('button',{name:'新建对话',exact:true}).click();
    await page.locator('#prompt').fill('这是实例界面的协议模拟测试，请返回完成。');
    await page.locator('#send').click();
    await page.waitForFunction(() => state.session && state.session.status === 'completed');
    const session = await page.evaluate(() => ({id:state.session.id,instanceId:state.session.instanceId,workspaceId:state.session.workspaceId}));
    assert.equal(session.instanceId,i.id); assert.equal(session.workspaceId,wb.id);
    const stored = await call('GET','/sessions/'+session.id); assert.equal(stored.instanceId,i.id); assert.equal(stored.workspaceId,wb.id);
    await page.locator('#new-session').click();
    await page.waitForFunction(() => !state.session && state.view === 'conversation');
    assert.equal(await page.locator('#instance').inputValue(),i.id); assert.equal(await page.locator('#workspace').inputValue(),wb.id);
    await page.locator('#assistant-settings').click(); await page.locator('#settings[open]').waitFor();
    assert((await page.locator('#settings-title').innerText()).includes(i.name));
    await page.locator('#close-settings').click();
    checks.push('an actual demo turn completes on a nondefault instance; New conversation and Settings retain its identity');

    await page.goto(base+'/#instance/'+i.id+'?workspace='+wa.id);
    await page.locator('#instance-workspace').waitFor();
    assert.equal(await page.locator('#instance-workspace').inputValue(),wa.id);
    assert(await page.locator('#instances-page').evaluate(n => ![...n.childNodes].some(node => node.nodeType===Node.TEXT_NODE && node.textContent.trim()==='null')),'instance without an application rendered literal null');
    await screenshot(page,'instance-detail-desktop');
    await page.setViewportSize({width:390,height:844}); await fits(page,'#instances-page'); await screenshot(page,'instance-detail-mobile');
    await page.locator('#nav-instances').click(); await page.locator('.instance-card').first().waitFor();
    await fits(page,'#instances-page'); await screenshot(page,'instances-mobile');
    checks.push('instance catalog and detail fit a 390px mobile viewport');

    await page.setViewportSize({width:1440,height:1000});
    await page.getByRole('button',{name:'从预设创建实例',exact:true}).click();
    await page.waitForFunction(() => state.view === 'build-center');
    await page.locator('[data-recipe-id]').first().waitFor();
    checks.push('creating an instance leads to the existing embedded build presets');

    await page.locator('#nav-instances').click(); await page.locator('.instance-card').first().waitFor();
    await page.locator('#language-toggle').click();
    await page.waitForFunction(() => document.documentElement.lang === 'en' && typeof state !== 'undefined' && state.view === 'instances' && document.querySelector('.instance-card'));
    assert.equal(await page.locator('#instances-page h1').innerText(),'Instances');
    const buttons = await page.locator('#instances-page button').allTextContents();
    assert(buttons.every(text => !/[\u3400-\u9fff]/.test(text)),'untranslated controls in English instance catalog: '+buttons.join(' | '));
    await screenshot(page,'instances-english');
    checks.push('English mode translates the new instance controls');
    assert.deepEqual(pageErrors,[]);
    console.log(JSON.stringify({passed:true,checks,pageErrors,scope:'Chromium + actual RunDesk demo server, authenticated API, disposable data; no Docker daemon, external provider or real model.'}));
  } catch(error) {
    if(browser && shots) for(const context of browser.contexts()) for(const page of context.pages()) await screenshot(page,'failure').catch(()=>{});
    console.error(error); console.error('pageErrors:',JSON.stringify(pageErrors)); process.exitCode=1;
  } finally {
    await browser?.close(); if(server.exitCode===null) {server.kill();await new Promise(resolve => server.once('exit',resolve));}
    fs.rmSync(data,{recursive:true,force:true});
  }
})();
