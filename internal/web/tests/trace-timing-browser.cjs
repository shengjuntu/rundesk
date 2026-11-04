/* Actual isolated service and shipped assets with a read-only DebugService
 * response fixture. No model, worker or Docker execution. */
'use strict';
const fs=require('node:fs'),path=require('node:path'),os=require('node:os'),assert=require('node:assert/strict'),{spawn}=require('node:child_process');
const {chromium,request}=require('playwright');
(async()=>{
 const dir=fs.mkdtempSync(path.join(os.tmpdir(),'rundesk-timing-browser-')),out=process.env.RUNDESK_BROWSER_OUTPUT;
 if(out)fs.mkdirSync(out,{recursive:true});
 const server=spawn(process.env.RUNDESK_BINARY||path.resolve(__dirname,'../../../bin/rundesk'),['--demo','--listen','127.0.0.1:0','--data',dir],{env:{...process.env,RUNDESK_TOKEN:'',CODEX_BASE_TOKEN:''},stdio:['ignore','ignore','pipe']});let browser;
 try{
  const base=await new Promise((resolve,reject)=>{let log='';const timer=setTimeout(()=>reject(Error(log||'startup timeout')),20000);server.stderr.on('data',d=>{log+=d;const m=log.match(/RunDesk (http:\/\/127\.0\.0\.1:\d+)/);if(m){clearTimeout(timer);resolve(m[1])}});server.once('error',reject)});
  const client=await request.newContext({baseURL:base});const api=async(route,method='GET',data)=>{const response=await client.fetch('/api/v1'+route,{method,data,headers:{'Idempotency-Key':require('node:crypto').randomUUID()}});assert(response.ok(),await response.text());return response.json()};
  const work=(await api('/workspaces'))[0],session=await api('/sessions','POST',{workspaceId:work.id,title:'耗时检查'});
  browser=await chromium.launch({headless:true});const page=await browser.newPage({viewport:{width:1440,height:1000},locale:'zh-CN',bypassCSP:true}),errors=[],queries=[],requests=[];
  page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>requests.push({url:r.url(),method:r.method()}));
  await page.addInitScript(()=>{localStorage.setItem('rundesk-language','zh');sessionStorage.setItem('rundesk-setup-dismissed','1');window.EventSource=class{close(){}};});
  const step=(id,durationMs,type='mcpToolCall')=>({id,runId:'run-complete',type,title:id,status:'completed',durationMs,point:false,eventIds:[10,11],preview:{},previewOnly:true,clock:'host_journal',association:'observed',startObserved:true,endObserved:true,missing:[],issues:[],previewTruncated:false,evidenceTruncated:false});
  const steps=[...Array.from({length:21},(_,i)=>step('quick-'+i,10)),step('slowest-off-first-page',4000,'commandExecution'),step('overlap-mcp',3000),{...step('missing-end',undefined),status:'unknown',endObserved:false,missing:['end'],issues:['missing_end']}];
  const timing=run=>({runs:run==='run-open'?[{runId:run,status:'running',start:'2026-11-04T00:00:00Z',observedElapsedMs:2000,clock:'host_journal',startMethod:'run/input',startObserved:true,endObserved:false,complete:false,issues:['missing_end']}]:[{runId:'run-complete',status:'completed',start:'2026-11-04T00:00:00Z',end:'2026-11-04T00:00:05Z',durationMs:5000,clock:'host_journal',startMethod:'run/input',endMethod:'run/state',startObserved:true,endObserved:true,complete:true,issues:[]}],byType:run==='run-open'?[]:[{type:'commandExecution',stepCount:1,completeDurationSteps:1,withoutCompleteDuration:0,workDurationMs:4000},{type:'mcpToolCall',stepCount:23,completeDurationSteps:22,withoutCompleteDuration:1,workDurationMs:3210}],slowestSteps:run==='run-open'?[]:[steps[21],steps[22]],knownWorkDurationMs:run==='run-open'?0:7210,completeDurationSteps:run==='run-open'?0:23,withoutCompleteDuration:1,pointSteps:0});
  await page.route('**/api/v1/sessions/'+session.id+'/debug/**',async route=>{
   const u=new URL(route.request().url()),q=u.searchParams,kind=q.get('kind');
   if(u.pathname.endsWith('/capabilities'))return route.fulfill({json:{backend:'codex',queries:['runs','steps','step','statistics','transcript']}});
   if(!u.pathname.endsWith('/query'))return route.continue();
   queries.push(Object.fromEntries(q));const run=q.get('runId')||'';
   if(kind==='events')return route.fulfill({json:{data:{through:99,events:[]}}});
   if(kind==='runs')return route.fulfill({json:{data:{runs:[{id:'run-complete',question:'完成的一轮',status:'completed'},{id:'run-open',question:'尚未结束的一轮',status:'running'}],nextOffset:2,total:2,hasMore:false}}});
   if(kind==='statistics')return route.fulfill({json:{data:{steps:q.get('type')?1:steps.length,attentionSteps:1,withoutCompleteDuration:q.get('type')?0:1,timing:q.get('type')?{...timing(run),knownWorkDurationMs:4000}:timing(run)}}});
   if(kind==='transcript')return route.fulfill({json:{data:{entries:[],offset:0,nextOffset:0,total:0,hasMore:false,coverage:'fixture'}}});
   if(kind==='steps'||kind==='issues'){
    const sort=q.get('sort')||'recorded',rows=run==='run-open'?[]:steps.filter(s=>!q.get('type')||s.type===q.get('type'));
    if(sort!=='recorded')rows.sort((a,b)=>a.durationMs==null?b.durationMs==null?0:1:b.durationMs==null?-1:sort==='duration_asc'?a.durationMs-b.durationMs:b.durationMs-a.durationMs);
    const offset=Number(q.get('offset')||0),limit=Number(q.get('limit')||20),end=Math.min(rows.length,offset+limit);
    return route.fulfill({json:{data:{steps:rows.slice(offset,end),nextOffset:end,total:rows.length,hasMore:end<rows.length}}});
   }
   if(kind==='step')return route.fulfill({json:{data:steps.find(s=>s.id===q.get('stepId'))}});
   throw Error('unexpected trace query '+kind);
  });
  await page.goto(base+'/#session/'+session.id);await page.waitForFunction(id=>state.session?.id===id,session.id);
  await page.locator('#trace-button').click();const dialog=page.locator('#debug-inspector');await dialog.locator('[data-panel=steps]').click();await dialog.getByRole('combobox',{name:'调试轮次'}).selectOption('run-complete');await dialog.locator('[data-timing-wall]').waitFor();
  const wall=await dialog.locator('[data-timing-wall]').innerText(),workText=await dialog.locator('[data-timing-work]').innerText();assert(/5(?:\.0)?\s*s|5(?:\.0)?\s*秒|5000/.test(wall),'known wall interval not displayed: '+wall);assert(/7\.21|7\.2|7210/.test(workText),'work sum not displayed distinctly: '+workText);assert.notEqual(wall,workText);
  assert.equal(await dialog.locator('.debug-step-list [data-step-id="slowest-off-first-page"]').count(),0,'fixture slow step unexpectedly on recorded first page');
  await dialog.getByRole('combobox',{name:'步骤排序'}).selectOption('duration_desc');await page.waitForFunction(()=>document.querySelector('.debug-step-list [data-step-id]')?.dataset.stepId==='slowest-off-first-page');
  const latest=queries.filter(q=>q.kind==='steps').at(-1);assert.equal(latest.sort,'duration_desc');assert.equal(latest.offset,'0');assert.equal(latest.runId,'run-complete');assert.equal(latest.through,'99');
  await dialog.locator('[data-debug-panel=steps]').getByRole('button',{name:'下一页',exact:true}).click();await dialog.locator('.debug-step-list [data-step-id="missing-end"]').waitFor();assert((await dialog.locator('.debug-step-list [data-step-id="missing-end"]').innerText()).includes('未知'));
  assert.equal(queries.filter(q=>q.kind==='steps').at(-1).sort,'duration_desc');
  await dialog.getByRole('combobox',{name:'步骤类型'}).selectOption('commandExecution');await page.waitForFunction(()=>document.querySelector('.debug-step-list')?.querySelectorAll('[data-step-id]').length===1);assert.equal(await dialog.locator('[data-timing-work]').innerText(),workText,'type filter replaced whole-run work statistics');const filtered=queries.filter(q=>q.kind==='steps').at(-1);assert.equal(filtered.type,'commandExecution');assert.equal(filtered.offset,'0');assert(queries.some(q=>q.kind==='statistics'&&q.type==='commandExecution')&&queries.some(q=>q.kind==='statistics'&&q.runId==='run-complete'&&!q.type),'filtered and whole-run statistics were not queried separately');
  await dialog.getByRole('combobox',{name:'步骤类型'}).selectOption('');await dialog.locator('.debug-slowest-step[data-step-id="slowest-off-first-page"]').click();await dialog.locator('.debug-step-detail').getByRole('heading',{name:'slowest-off-first-page',exact:true}).waitFor();const inspected=queries.filter(q=>q.kind==='step').at(-1);assert.equal(inspected.stepId,'slowest-off-first-page');assert.equal(inspected.runId,'run-complete');assert.equal(inspected.through,'99');assert((await dialog.locator('.debug-timing-clock').innerText()).includes('RunDesk 事件日志'));assert((await dialog.locator('.debug-timing-clock').innerText()).includes('run/input → run/state'));
  await dialog.getByRole('combobox',{name:'调试轮次'}).selectOption('run-open');await page.waitForFunction(()=>document.querySelector('[data-timing-wall]')?.textContent.includes('未知')||document.querySelector('[data-timing-wall]')?.textContent.includes('未结束'));
  const openWall=await dialog.locator('[data-timing-wall]').innerText();assert(!/5(?:\.0)?\s*s|5000/.test(openWall),'completed wall time leaked into open run');
  await page.setViewportSize({width:390,height:844});assert(await dialog.evaluate(n=>n.scrollWidth<=n.clientWidth+1),'mobile timing overflow');await dialog.evaluate(n=>n.scrollTop=0);const heading=await dialog.locator('.dialog-head').boundingBox();assert(heading.y>=0&&heading.y+heading.height<=844,'mobile timing header is unreachable');if(out)await page.screenshot({path:path.join(out,'trace-timing-mobile.png')});
  assert(!requests.some(r=>r.method!=='GET'&&/turns|actions|tasks|model-test/.test(r.url)),'timing inspection executed work');assert.deepEqual(errors,[]);
  const result={passed:true,scope:'Actual isolated Go demo service and shipped browser assets; read-only trace API fixtures, no model/Docker/worker execution.',checks:['wall duration distinct from parallel work sum','global-duration sort request resets first page and retains fixed run/cursor','unknown durations remain unknown at end of sorted pages','type filter preserves whole-run timing and resets paging','slowest step opens scoped evidence and host clock provenance remains visible','open run cannot inherit completed wall time','mobile timing layout and reachable header','inspection has no execution side effects']};if(out)fs.writeFileSync(path.join(out,'trace-timing-browser.json'),JSON.stringify(result,null,2));console.log(JSON.stringify(result));await client.dispose();
 }finally{if(browser)await browser.close();if(server.pid&&server.exitCode===null)await new Promise(r=>{server.once('exit',r);server.kill('SIGTERM')});fs.rmSync(dir,{recursive:true,force:true})}
})().catch(e=>{console.error(e);process.exitCode=1});
