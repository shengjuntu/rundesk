// Real host + Kun subprocess + Chromium; local model fixture, no real provider.
const {chromium}=require(process.env.PLAYWRIGHT_PATH||'playwright');
const {spawn}=require('node:child_process'),http=require('node:http'),fs=require('node:fs'),path=require('node:path'),os=require('node:os'),assert=require('node:assert/strict'),crypto=require('node:crypto');
(async()=>{
 const root=path.resolve(__dirname,'..'),temp=fs.mkdtempSync(path.join(os.tmpdir(),'rd-diagnosis-')),pictures=path.join(root,'docs/screenshots/0.29.0');fs.mkdirSync(pictures,{recursive:true});
 const base='http://127.0.0.1:38735',errors=[],requests=[],checks=[],modelRequests=[];let browser,host,model,source,through,cited;
 const pause=ms=>new Promise(r=>setTimeout(r,ms));
 const api=async(p,body,method)=>{const response=await fetch(base+'/api/v1'+p,{method:method||(body?'POST':'GET'),headers:{'Content-Type':'application/json','Idempotency-Key':crypto.randomUUID()},body:body?JSON.stringify(body):undefined});assert(response.ok,await response.clone().text());return response.json()};
 const waitState=async(id,status)=>{for(let n=0;n<150;n++){const s=await api('/sessions/'+id);if(s.status===status)return s;assert(!['failed','interrupted'].includes(s.status),JSON.stringify(s));await pause(100)}throw Error('session did not reach '+status)};
 try{
  model=http.createServer(async(req,res)=>{
   let body='';for await(const part of req)body+=part;const data=JSON.parse(body);modelRequests.push(data);
   assert.equal(data.tools.length,7);assert(data.tools.every(t=>t.function.name.startsWith('trace_')));assert(!JSON.stringify(data).includes('BUSINESS-SYSTEM'));
   let message,finish_reason='tool_calls';
   const call=(name,args)=>({role:'assistant',tool_calls:[{id:'call-'+modelRequests.length,type:'function',function:{name,arguments:JSON.stringify(args)}}]});
   if(modelRequests.length===1)message=call('trace_statistics',{runId:source.runId});
   else if(modelRequests.length===2)message=call('trace_propose',{runId:source.runId,proposal:{kind:'steer',reason:'来源记录显示模型调用前暂停；这是固定历史中的事实。',text:'请先核对断点，再决定是否继续。<img src=x onerror="window.diagnosticXSS=true">',evidenceIds:[cited]}});
   else{finish_reason='stop';message={role:'assistant',content:'已记录事实：来源在模型调用前暂停。推断：可能是预设断点。建议尚未执行，请审核来源事件。'}}
   res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify({choices:[{message,finish_reason}],usage:{prompt_tokens:10,completion_tokens:5,total_tokens:15}}));
  });await new Promise(r=>model.listen(0,'127.0.0.1',r));
  host=spawn(root+'/bin/rundesk',['--data',temp+'/data','--listen','127.0.0.1:38735'],{env:{...process.env,RUNDESK_TOKEN:'',CODEX_HOME:temp+'/codex-home'},stdio:['ignore','ignore','pipe']});
  for(let n=0;n<100;n++){try{await api('/meta');break}catch{}await pause(100)}
  const i=(await api('/instances'))[0],workspace=(await api('/workspaces'))[0];
  await api('/instances/'+i.id+'/agent-runtime',{revision:i.revision,config:{kind:'kun',endpoint:'http://127.0.0.1:'+model.address().port+'/v1',model:'diagnostic-fixture',allowWrite:true,pauseBeforeModel:true,maxSteps:5,timeoutSeconds:10,systemPrompt:'BUSINESS-SYSTEM'}},'PUT');
  source=await api('/sessions',{workspaceId:workspace.id,title:'独立诊断 UI 验证'});await api('/sessions/'+source.id+'/turns',{text:'等待人工检查的业务任务'});source=await waitState(source.id,'waiting');
  const original=await api('/sessions/'+source.id+'/kun/state'),events=await api('/sessions/'+source.id+'/debug/query?kind=events');through=events.data.through;cited=events.data.events[0].id;assert.equal(modelRequests.length,0);
  browser=await chromium.launch({executablePath:process.env.CHROMIUM_PATH,headless:true,args:['--no-sandbox']});const page=await browser.newPage({viewport:{width:1440,height:1000},locale:'zh-CN'});page.on('pageerror',e=>errors.push(e.message));page.on('response',r=>{if(r.status()>=400)console.error('HTTP_ERROR',r.status(),r.url())});page.on('request',r=>requests.push({url:r.url(),method:r.method()}));
  await page.addInitScript(()=>{localStorage.setItem('rundesk-language','zh');sessionStorage.setItem('rundesk-setup-dismissed','1')});await page.goto(base);await page.waitForFunction(()=>window.RunDeskDebug&&typeof selectSession==='function');await page.evaluate(id=>selectSession(id),source.id);
  await page.locator('#debug-inspect-open').click();let d=page.locator('#debug-inspector');await d.locator('.debug-step').first().waitFor();await d.locator('.debug-diagnosis summary').click();assert(await d.getByRole('button',{name:'创建诊断并发送问题'}).isDisabled());
  await d.getByRole('combobox',{name:'调试轮次'}).selectOption(source.runId);await d.getByRole('textbox',{name:'诊断问题'}).fill('为什么暂停？引用证据并给出待审核建议。');await page.screenshot({path:pictures+'/diagnosis-create.png'});
  await d.getByRole('button',{name:'创建诊断并发送问题'}).click();await d.waitFor({state:'detached'});await page.locator('.diagnostic-suggestion').waitFor();
  const analysis=await page.evaluate(()=>state.session);assert.notEqual(analysis.id,source.id);assert.equal(analysis.traceOrigin.through,through);assert.equal(analysis.runtimeKind,'kun');await waitState(analysis.id,'completed');
  assert(await page.locator('#attach').isDisabled());assert(await page.locator('#skill-picker').isDisabled());const card=page.locator('.diagnostic-suggestion');assert((await card.innerText()).includes('尚未执行'));assert.equal(await card.locator('img').count(),0);assert.equal(await page.evaluate(()=>window.diagnosticXSS),undefined);assert.equal(await card.locator('button').count(),1);checks.push('explicit creation starts a separate Kun diagnostic with fixed cursor and exclusive trace tools');
  await page.screenshot({path:pictures+'/diagnosis-suggestion.png'});
  const after=await api('/sessions/'+source.id+'/kun/state');assert.equal(after.revision,original.revision);assert.equal(after.status,'paused');assert.equal(after.step,original.step);assert.equal(after.budget.reportedTokens,original.budget.reportedTokens);assert.equal((await api('/sessions/'+source.id+'/debug/query?kind=events')).data.through,through);checks.push('source remains paused with identical revision and events; model usage belongs to diagnostic');
  await card.getByRole('button',{name:'来源事件 #'+cited}).click();d=page.locator('#debug-inspector');await d.locator('pre').filter({hasText:'等待人工检查的业务任务'}).waitFor();assert((await d.innerText()).includes('固定宿主历史 ≤ #'+through));await d.getByRole('button',{name:'关闭',exact:true}).click();checks.push('suggestion citations open original evidence at the frozen source cursor');
  await page.reload();await page.waitForFunction(()=>window.RunDeskDebug&&typeof selectSession==='function');await page.evaluate(id=>selectSession(id),analysis.id);await page.locator('.diagnostic-suggestion').waitFor();checks.push('diagnostic source banner and reviewable suggestion survive reload');
  await page.setViewportSize({width:390,height:844});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'mobile overflow');await page.screenshot({path:pictures+'/diagnosis-mobile.png'});
  await page.locator('#prompt').fill('继续解释，保持同一来源范围');const submitted=page.waitForResponse(r=>r.url()===base+'/api/v1/sessions/'+analysis.id+'/turns'&&r.request().method()==='POST');await page.locator('#send').click();assert.equal((await submitted).status(),202);await waitState(analysis.id,'completed');assert.equal(modelRequests.length,4);const final=await api('/sessions/'+analysis.id+'/kun/state');assert.equal(final.diagnostic.through,through);assert.equal(final.budget.reportedTokens,15);checks.push('follow-up reuses the fixed source; 390px layout and literal text are safe');
  assert(!requests.some(r=>r.method==='POST'&&r.url.includes('/sessions/'+source.id+'/')),'UI controlled or messaged source');assert.deepEqual(errors,[]);
  const report={version:'0.29.0',passed:true,scope:'real Chromium + RunDesk + Kun worker + local model fixture; no real provider',modelCalls:modelRequests.length,checks,pageErrors:errors};fs.writeFileSync(root+'/docs/kun-diagnostic-ui-validation.json',JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report));
 }finally{
  await browser?.close();if(host&&host.exitCode===null){const exited=new Promise(r=>host.once('exit',r));host.kill();await Promise.race([exited,pause(5000)]);if(host.exitCode===null)host.kill('SIGKILL')}
  if(model)await new Promise(r=>model.close(r));fs.rmSync(temp,{recursive:true,force:true});
 }
})().catch(e=>{console.error(e);process.exitCode=1});
