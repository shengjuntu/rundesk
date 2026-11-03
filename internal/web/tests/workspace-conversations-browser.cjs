/* Needs Playwright Chromium and RUNDESK_BINARY. Uses a disposable real server;
 * creates idle sessions only and never starts a model, worker or container. */
'use strict';
const fs=require('node:fs'),path=require('node:path'),os=require('node:os'),assert=require('node:assert/strict'),{spawn}=require('node:child_process');
const {chromium,request}=require('playwright');
(async()=>{
 const dir=fs.mkdtempSync(path.join(os.tmpdir(),'rundesk-workspace-browser-')),output=process.env.RUNDESK_BROWSER_OUTPUT;
 if(output)fs.mkdirSync(output,{recursive:true});
 const server=spawn(process.env.RUNDESK_BINARY||path.resolve(__dirname,'../../../bin/rundesk'),['--listen','127.0.0.1:0','--data',dir],{env:{...process.env,CODEX_HOME:path.join(dir,'local-codex'),RUNDESK_TOKEN:'',CODEX_BASE_TOKEN:''},stdio:['ignore','ignore','pipe']});let browser;
 try{
  const base=await new Promise((resolve,reject)=>{let log='';const timer=setTimeout(()=>reject(Error(log||'startup timeout')),15000);server.stderr.on('data',d=>{log+=d;const url=log.match(/RunDesk (http:\/\/127\.0\.0\.1:\d+)/)?.[1];if(url){clearTimeout(timer);resolve(url)}});server.once('error',e=>{clearTimeout(timer);reject(e)});server.once('exit',code=>{clearTimeout(timer);reject(Error('server exited '+code+': '+log))})});
  const client=await request.newContext({baseURL:base});
  const api=async(route,method='GET',data)=>{const r=await client.fetch('/api/v1'+route,{method,data,headers:{'Idempotency-Key':require('node:crypto').randomUUID()}});assert(r.ok(),method+' '+route+': '+r.status()+' '+await r.text());return r.json()};
  const work=await api('/workspaces','POST',{name:'产品视频'}),other=await api('/workspaces','POST',{name:'研究笔记'});
  let kun=await api('/instances','POST',{name:'研究助手'});kun=await api('/instances/'+kun.id+'/agent-runtime','PUT',{revision:kun.revision,config:{kind:'kun',endpoint:'http://127.0.0.1:9/v1',model:'fixture-model'}});
  const first=await api('/sessions','POST',{workspaceId:work.id,instanceId:'default',title:'讨论脚本'});
  const rejected=await client.post('/api/v1/sessions',{data:{workspaceId:work.id,instanceId:kun.id,title:'不能创建日常 Kun 会话'},headers:{'Idempotency-Key':'maintenance-daily-rejected'}});assert.equal(rejected.status(),409);assert.equal((await rejected.json()).code,'kun_maintenance_only');
  const third=await api('/sessions','POST',{workspaceId:other.id,instanceId:'default',title:'整理参考文献'});
  const docks=[];for(const name of ['视频环境 A','视频环境 B']){let i=await api('/instances','POST',{name});i=await api('/instances/'+i.id+'/execution','PUT',{revision:i.revision,spec:{mode:'docker',image:'fixture:v1'}});docks.push(i)}
  browser=await chromium.launch({headless:true});const page=await browser.newPage({viewport:{width:1440,height:960},locale:'zh-CN',bypassCSP:true}),errors=[],requests=[];
  page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>requests.push({url:r.url(),method:r.method()}));
  await page.addInitScript(()=>{localStorage.setItem('rundesk-language','zh');sessionStorage.setItem('rundesk-setup-dismissed','1')});
  await page.goto(base+'/#workspace/'+work.id);
  const group=page.locator('[data-workspace-id="'+work.id+'"]');await group.locator('.session').first().waitFor();assert.equal(await page.locator('#app-version').innerText(),'v'+(await api('/meta')).version);assert.equal(await group.locator('.session').count(),1);assert.equal(await page.locator('.workspace-group').count(),(await api('/workspaces')).length);
  assert((await group.innerText()).includes('通用助手'));assert(!(await group.innerText()).includes('Kun'));assert(await page.locator('#session-engine').isDisabled());assert.equal(await page.locator('#composer-workspace').count(),0);
  await group.locator('[data-session-id="'+first.id+'"]').click();await page.waitForFunction(id=>state.session?.id===id,first.id);
  await page.locator('#new-session').click();await page.waitForFunction(()=>!state.session&&!state.loadingSession);assert.equal(await page.locator('#session-engine').innerText(),'通用助手');
  await page.locator('#prompt').fill('这段文字在换引擎时保留');await page.locator('#session-engine').click();assert.equal(await page.locator('#conversation-engine-choice option[value="'+kun.id+'"]').count(),0);await page.locator('#conversation-engine-choice').selectOption(docks[0].id);await page.locator('.conversation-engine-dialog').waitFor({state:'detached'});assert.equal(await page.locator('#prompt').inputValue(),'这段文字在换引擎时保留');assert.equal(await page.locator('#workspace').inputValue(),work.id);
  // Submit only the idle session creation step; no turns or engine calls.
  await page.evaluate(()=>newSession());assert(await page.locator('#session-engine').isDisabled());assert.equal(await group.locator('.session').count(),2);
  await page.locator('[data-workspace-id="'+other.id+'"] .workspace-heading').click();await page.waitForFunction(id=>state.session?.id===id,third.id);await page.locator('#new-session').click();await page.waitForFunction(()=>!state.session&&!state.loadingSession);assert.equal(await page.locator('#session-engine').innerText(),'通用助手');
  await group.locator('.workspace-heading').click();await page.waitForFunction(id=>state.session?.workspaceId===id,work.id);await page.locator('#new-session').click();await page.waitForFunction(()=>!state.session&&!state.loadingSession);assert((await page.locator('#session-engine').innerText()).includes('视频环境 A'));
  for(const i of docks){await page.locator('#session-engine').click();await page.locator('#conversation-engine-choice').selectOption(i.id);await page.locator('.conversation-engine-dialog').waitFor({state:'detached'});await page.evaluate(()=>newSession());await page.locator('#new-session').click();await page.waitForFunction(()=>!state.session&&!state.loadingSession)}
  const sessions=await api('/sessions'),inWork=sessions.filter(s=>s.workspaceId===work.id);assert.equal(inWork.length,4);assert.equal(new Set(inWork.filter(s=>s.executionMode==='docker').map(s=>s.environmentId)).size,2);
  const beforeReload=(await api('/sessions/'+first.id));assert.equal(beforeReload.instanceId,'default');assert.equal(beforeReload.workspaceId,work.id);
  assert(await page.locator('#recovery-banner').isHidden(),'new draft displayed a false pending recovery');
  if(output)await page.screenshot({path:path.join(output,'workspace-conversations-desktop.png')});
  await page.reload();await group.locator('.session').first().waitFor();assert.equal(await group.locator('.session').count(),4);assert((await page.locator('#session-engine').innerText()).includes('视频环境 B'));
  await page.setViewportSize({width:390,height:844});await page.locator('#sidebar-toggle').click();await page.locator('#new-session').waitFor({state:'visible'});if(output)await page.screenshot({path:path.join(output,'workspace-conversations-mobile.png')});
  assert(await page.locator('#session-list').evaluate(n=>n.scrollWidth<=n.clientWidth),'sidebar horizontal overflow');
  await page.locator('#add-workspace').click();await page.locator('#conversation-workspace-name').fill('客户方案');await page.locator('#conversation-workspace-name').locator('xpath=ancestor::form').getByRole('button',{name:'创建工作区',exact:true}).click();await page.locator('#conversation-workspace-name').waitFor({state:'detached'});await page.waitForFunction(()=>!state.loadingSession&&document.querySelector('#workspace-name').textContent==='客户方案');
  await page.setViewportSize({width:1440,height:960});await page.goto(base+'/#instances');await page.locator('.instance-card').first().waitFor();assert.equal(await page.locator('[data-instance-id="'+kun.id+'"]').count(),0);
  await page.goto(base+'/#build-center/recipes');await page.locator('[data-center-action="assistant"]').click();await page.locator('#center-assistant-prompt').waitFor();assert.equal(await page.locator('#center-assistant-instance').count(),0);assert.equal(await page.locator('#center-assistant-workspace').count(),0);await page.getByText('Kun 维护助手尚未就绪，请从左侧“环境设置”配置并测试模型。仍可手动导入或构建配方。',{exact:true}).waitFor();if(output)await page.screenshot({path:path.join(output,'kun-maintenance-build-dialog.png')});
  assert(!requests.some(r=>r.method==='POST'&&/\/(turns|actions|prepare)$/.test(r.url)),'navigation started a model or container');assert.deepEqual(errors,[]);
  const result={passed:true,checks:['real API rejects daily Kun; Codex environments grouped by workspace','engine picker retains text and workspace','per-workspace engine memory and reload','two Docker environments in one ordinary workspace','historical session bindings unchanged','mobile workspace navigation and creation','Kun hidden from everyday Agent list','maintenance dialog uses setup configuration without instance/workspace selectors'],scope:'real server, idle session and environment metadata only; no worker, model or Docker process started'};
  if(output)fs.writeFileSync(path.join(output,'workspace-conversations-browser.json'),JSON.stringify(result,null,2));console.log(JSON.stringify(result));await client.dispose();
 }finally{if(browser)await browser.close();if(server.pid&&server.exitCode===null)await new Promise(r=>{server.once('exit',r);server.kill('SIGTERM')});fs.rmSync(dir,{recursive:true,force:true})}
})().catch(e=>{console.error(e);process.exitCode=1});
