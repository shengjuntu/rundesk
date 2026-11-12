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
    await page.waitForFunction(() => state.view === 'conversation' && !state.loadingSession);
    assert.equal(await page.locator('#nav-instances').count(),0);
    assert.equal(await page.locator('#instance').inputValue(),'default');
    await screenshot(page,'default-conversation');
    checks.push('real login opens the ready default Codex conversation');

    await page.goto(base+'/#instance/'+i.id+'/settings/skills?workspace='+wb.id);
    await page.locator('#settings[open]').waitFor();
    assert.equal(await page.locator('#instance').inputValue(),i.id);
    assert.equal(await page.locator('#workspace').inputValue(),wb.id);
    assert((await page.evaluate(()=>location.hash)).startsWith('#build-center/environment/'));
    await page.locator('#close-settings').click();
    checks.push('old capability link opens the same environment/workspace in the shared editor');

    await page.goto(base+'/#instance/'+i.id+'/conversation?workspace='+wb.id);
    await page.waitForFunction(id => state.view === 'conversation' && !state.loadingSession && document.querySelector('#instance').value===id,i.id);
    await page.locator('#prompt').fill('这是界面的协议模拟测试，请返回完成。');
    await page.locator('#send').click();
    await page.waitForFunction(() => state.session?.status === 'completed');
    const session = await page.evaluate(() => ({id:state.session.id,instanceId:state.session.instanceId,workspaceId:state.session.workspaceId}));
    const stored = await call('GET','/sessions/'+session.id);
    assert.equal(stored.instanceId,i.id);assert.equal(stored.workspaceId,wb.id);
    await page.locator('#new-session').click();
    await page.waitForFunction(() => !state.session && !state.loadingSession);
    assert.equal(await page.locator('#instance').inputValue(),'default');
    checks.push('historical explicit scope survives while the next ordinary draft defaults to local Codex');

    await page.goto(base+'/#instances');await page.waitForFunction(() => state.view === 'build-center');
    await fits(page,'#build-center-page');await screenshot(page,'workbench-desktop');
    await page.setViewportSize({width:390,height:844});
    await fits(page,'#build-center-page');await screenshot(page,'workbench-mobile');
    checks.push('legacy catalog opens the workbench on desktop and mobile');
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
