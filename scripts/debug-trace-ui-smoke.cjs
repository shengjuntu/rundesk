// Real browser + RunDesk host + explicit Codex protocol simulation, no real model.
const {chromium}=require(process.env.PLAYWRIGHT_PATH||'playwright');
const {spawn}=require('node:child_process'),fs=require('node:fs'),os=require('node:os'),path=require('node:path'),assert=require('node:assert/strict'),crypto=require('node:crypto');
(async()=>{
 const root=path.resolve(__dirname,'..'),temp=fs.mkdtempSync(path.join(os.tmpdir(),'rd-debug-ui-')),base='http://127.0.0.1:38732';
 const pictures=process.env.UI_SCREENSHOT_DIR||path.join(root,'docs/screenshots/0.49.0');fs.mkdirSync(pictures,{recursive:true});
 let server,browser;const errors=[],requests=[],checks=[];
 const pause=ms=>new Promise(r=>setTimeout(r,ms));
 const api=async(p,body)=>{const r=await fetch(base+'/api/v1'+p,{method:body?'POST':'GET',headers:{'Content-Type':'application/json','Idempotency-Key':crypto.randomUUID()},body:body?JSON.stringify(body):undefined});assert(r.ok,await r.clone().text());return r.json()};
 const finish=async(sid)=>{for(let n=0;n<150;n++){const s=await api('/sessions/'+sid);if(s.status==='completed')return s;assert(!['failed','interrupted'].includes(s.status),JSON.stringify(s));await pause(100)}throw Error('demo turn did not finish')};
 try{
  server=spawn(process.env.RUNDESK_TEST_HOST || root+'/bin/rundesk',[...(process.env.RUNDESK_TEST_KUN?['--kun',process.env.RUNDESK_TEST_KUN]:[]),'--demo','--data',temp,'--listen','127.0.0.1:38732'],{env:{...process.env,RUNDESK_TOKEN:''},stdio:['ignore','ignore','pipe']});
  for(let n=0;n<100;n++){try{await api('/meta');break}catch{}await pause(100)}
  const work=(await api('/workspaces'))[0],session=await api('/sessions',{workspaceId:work.id,title:'Codex 只读调试 · 本地协议模拟'});
  for(let n=0;n<3;n++){await api(`/sessions/${session.id}/turns`,{text:`轨迹 告警 第 ${n+1} 轮 <img src=x onerror="window.debugXSS=true"> `+(n===0?'中'.repeat(10000):'')});await finish(session.id)}
  const initial=await api(`/sessions/${session.id}/debug/query?kind=steps`),through=initial.through;assert(initial.data.total>20);
  const transcript=await api(`/sessions/${session.id}/debug/query?kind=transcript&through=${through}&limit=40`);assert(transcript.data.entries.some(row=>row.role==='user'&&row.blocks.some(block=>block.text.includes('轨迹 告警 第'))),'real transcript projection omitted user prompts');assert(transcript.data.entries.some(row=>row.role==='assistant'),'real transcript projection omitted assistant text');
  browser=await chromium.launch({executablePath:process.env.CHROMIUM_PATH,headless:true,args:['--no-sandbox']});
  const page=await browser.newPage({viewport:{width:1440,height:1050},locale:'zh-CN'});page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>{if(r.url().includes('/debug/'))requests.push({method:r.method(),url:r.url()})});
  await page.addInitScript(()=>{localStorage.setItem('rundesk-language','zh');sessionStorage.setItem('rundesk-setup-dismissed','1')});
  await page.goto(base);await page.waitForFunction(()=>window.RunDeskDebug&&typeof selectSession==='function');await page.evaluate(sid=>selectSession(sid),session.id);
  if(process.env.UI_CJK_FONT_DIR){
   const fontRoot=path.resolve(process.env.UI_CJK_FONT_DIR),fonts=new Map();let css=fs.readFileSync(path.join(fontRoot,'400.css'),'utf8').replace(/,\s*url\([^)]+\.woff\)\s*format\('woff'\)/g,'');
   css=css.replace(/url\((\.\/files\/[^)]+\.woff2)\)/g,(_,file)=>{const name=path.basename(file);fonts.set(name,fs.readFileSync(path.join(fontRoot,file)));return 'url(/__trace_test_font/'+name+')';});
   css+='\nbody{font-family:"Noto Sans SC",Arial,sans-serif}button,input,select,textarea{font-family:inherit}pre,code{font-family:ui-monospace,"Noto Sans SC",monospace}';
   // Serve test-only fonts as same-origin fixtures; keep the real host CSP intact.
   await page.route('**/__trace_test_font/*',route=>{const body=fonts.get(path.basename(new URL(route.request().url()).pathname));return route.fulfill({status:body?200:404,contentType:'font/woff2',body:body||''});});
   await page.route('**/__trace_test_font.css',route=>route.fulfill({contentType:'text/css',body:css}));await page.addStyleTag({url:base+'/__trace_test_font.css'});
  }
  await page.locator('#trace-button').click();const d=page.locator('#debug-inspector');await d.locator('.trace-entry').first().waitFor();await page.evaluate(()=>document.fonts.ready);await page.screenshot({path:path.join(pictures,'codex-conversation-trace.png')});const reading=await d.locator('.trace-content').innerText();assert(reading.includes('轨迹 告警 第'),'Real reader did not show the prompt: '+reading.slice(0,2000));checks.push('real host transcript projection and conversation reading');
  await d.locator('.debug-workbench-tabs [data-panel=steps]').click();await d.locator('.debug-step').first().waitFor();
  assert((await d.innerText()).includes('固定宿主历史'));assert.equal(await d.locator('.debug-step').count(),20);
  await d.getByRole('button',{name:'下一页',exact:true}).click();await page.waitForFunction(()=>document.querySelector('.debug-pages span')&&document.querySelectorAll('.debug-step').length<20);assert(await d.getByRole('button',{name:'上一页',exact:true}).isEnabled());
  await d.getByRole('button',{name:'上一页',exact:true}).click();await page.waitForFunction(()=>document.querySelectorAll('.debug-step').length===20);checks.push('real host projection pagination');
  await d.locator('.debug-step').first().click();await d.getByRole('button',{name:/事件 #/}).first().click();await d.locator('.debug-event pre').waitFor();
  assert(await d.getByRole('button',{name:'下一块',exact:true}).isEnabled());await d.getByRole('button',{name:'下一块',exact:true}).click();await page.waitForFunction(()=>document.querySelector('.debug-event')?.textContent.includes('字符 4000–'));assert.equal(await d.locator('img').count(),0);assert.equal(await page.evaluate(()=>window.debugXSS),undefined);checks.push('bounded Unicode event chunks and literal untrusted content');
  await page.screenshot({path:path.join(pictures,'codex-debug-evidence.png')});
  await api(`/sessions/${session.id}/turns`,{text:'轨迹 新增记录'});await finish(session.id);
  await d.getByRole('combobox',{name:'步骤类型'}).selectOption('commandExecution');await page.waitForFunction(()=>document.querySelectorAll('.debug-step').length===3);
  assert.equal(await d.locator('.debug-step').count(),3);const projectionQueries=requests.filter(r=>r.url.includes('kind=steps')||r.url.includes('kind=statistics'));assert(projectionQueries.every(r=>new URL(r.url).searchParams.get('through')===String(through)));checks.push('new source run excluded from fixed history');
  // Late successful response must not overwrite a newer empty filter result.
  let release,startedResolve;const started=new Promise(r=>startedResolve=r);
  await page.route('**/debug/query?**',async route=>{const u=new URL(route.request().url());if(u.searchParams.get('kind')==='steps'&&u.searchParams.get('query')==='DELAY'){startedResolve();await new Promise(r=>release=r);await route.fulfill({contentType:'application/json',body:JSON.stringify({through,data:{steps:[{id:'stale',title:'STALE RESPONSE',type:'warning',status:'warning',issues:[]}],nextOffset:1,hasMore:false,total:1}})});return}await route.continue()});
  await d.getByRole('searchbox',{name:'搜索已脱敏预览'}).fill('DELAY');await d.getByRole('button',{name:'查询',exact:true}).click();await started;
  await d.getByRole('searchbox',{name:'搜索已脱敏预览'}).fill('no-matching-fixture');await d.getByRole('button',{name:'查询',exact:true}).click();await d.getByText('当前范围没有匹配步骤；预览搜索不代表全文检索。').waitFor();release();await pause(150);assert(!(await d.innerText()).includes('STALE RESPONSE'));await page.unroute('**/debug/query?**');checks.push('stale asynchronous filter response ignored');
  await d.getByRole('searchbox',{name:'搜索已脱敏预览'}).fill('');await d.getByRole('button',{name:'查询',exact:true}).click();await page.waitForFunction(()=>document.querySelectorAll('.debug-step').length===3);
  await d.getByRole('button',{name:'读取最新记录',exact:true}).click();await page.waitForFunction(()=>document.querySelectorAll('.debug-step').length===4);checks.push('explicit refresh advances the host cursor');
  await d.locator('.debug-check input').check();await page.waitForFunction(()=>document.querySelectorAll('.debug-step').length===4);await d.locator('.debug-step').first().click();await d.getByRole('heading',{name:'来源事件'}).waitFor();
  await page.screenshot({path:path.join(pictures,'codex-debug-steps.png')});
  await page.setViewportSize({width:390,height:844});assert(await d.evaluate(n=>n.scrollWidth<=n.clientWidth+1),'dialog overflow');assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'page overflow');await page.screenshot({path:path.join(pictures,'codex-debug-mobile.png')});
  assert(requests.every(r=>r.method==='GET'));assert.deepEqual(errors,[]);checks.push('desktop/mobile layout, GET-only inspection and zero browser exceptions');
  const report={version:'v0.52.0-refactor',passed:true,scope:'real Chromium + RunDesk host + explicit Codex protocol fixture; no real Codex/model calls',checks,pageErrors:errors},reportPath=process.env.UI_REPORT_PATH||path.join(root,'docs/validation/current/debug-ui-validation.json');fs.mkdirSync(path.dirname(reportPath),{recursive:true});fs.writeFileSync(reportPath,JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report));
 }finally{await browser?.close();if(server&&server.exitCode===null){const exited=new Promise(r=>server.once('exit',r));server.kill();await Promise.race([exited,pause(5000)]);if(server.exitCode===null)server.kill('SIGKILL')}fs.rmSync(temp,{recursive:true,force:true})}
})().catch(e=>{console.error(e);process.exitCode=1});
