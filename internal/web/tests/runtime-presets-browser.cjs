/* Run with NODE_PATH pointing at Playwright and RUNDESK_BINARY at a freshly built RunDesk.
 * Uses isolated demo data and a fixture image catalog; never runs Docker or a model. */
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {spawn}=require('node:child_process');
const {chromium,request}=require('playwright');
(async()=>{
 const dir=fs.mkdtempSync(path.join(require('node:os').tmpdir(),'rundesk-presets-browser-'));
 const output=process.env.RUNDESK_BROWSER_OUTPUT;if(output)fs.mkdirSync(output,{recursive:true});
 const binary=process.env.RUNDESK_BINARY||path.resolve(__dirname,'../../../bin/rundesk');
 const server=spawn(binary,['--demo','--listen','127.0.0.1:0','--data',dir],{stdio:['ignore','ignore','pipe']});
 let browser;
 try{
  const base=await new Promise((resolve,reject)=>{let log='';const timer=setTimeout(()=>reject(Error('Server did not start: '+log)),15000);server.stderr.on('data',d=>{log+=d;const url=log.match(/RunDesk (http:\/\/127\.0\.0\.1:\d+)/)?.[1];if(url){clearTimeout(timer);resolve(url)}});server.once('error',e=>{clearTimeout(timer);reject(e)});server.once('exit',code=>{clearTimeout(timer);reject(Error('server exited '+code+': '+log))})});
  const client=await request.newContext({baseURL:base});
  const api=async(route,method='GET',data)=>{const r=await client.fetch('/api/v1'+route,{method,data,headers:{'Idempotency-Key':require('node:crypto').randomUUID()}});assert(r.ok(),method+' '+route+': '+r.status()+' '+await r.text());return r.json()};
  const agent=await api('/instances','POST',{name:'VideoAPP 默认'}),ws=await api('/workspaces','POST',{name:'VideoAPP 默认'});
  await api('/applications/video','PUT',{name:'视频制作',description:'选择适合任务的制作环境。',instanceId:agent.id,workspaceId:ws.id});
  await api('/workspaces','POST',{name:'旧工作区',path:path.join(dir,'legacy','rundest','project')});
  fs.rmSync(path.join(dir,'legacy','rundest'),{recursive:true});
  browser=await chromium.launch({headless:true});const page=await browser.newPage({viewport:{width:1440,height:1000},locale:'zh-CN'});
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.addInitScript(()=>{localStorage.setItem('rundesk-language','zh');sessionStorage.setItem('rundesk-setup-dismissed','1')});
  await page.route('**/api/v1/build-center/images',route=>route.fulfill({json:{items:[{imageId:'sha256:hyperframes',references:['rundesk/hyperframes:1.0.1'],sources:[{name:'HyperFrames',profileId:'hyperframes'}],availability:'available'},{imageId:'sha256:openmontage',references:['rundesk/openmontage:1.0.0'],sources:[{name:'OpenMontage',profileId:'openmontage'}],availability:'available'}]}}));
  await page.goto(base+'/#application/video/settings');
  await page.locator('.runtime-add-preset').waitFor();
  for(const [key,description] of [['hyperframes','制作新闻动画和信息视频'],['openmontage','剪辑素材，制作短视频']]){
   await page.locator('.runtime-add-preset').click();await page.locator('#preset-name').selectOption('sha256:'+key);await page.locator('#preset-description').fill(description);await page.locator('#preset-submit').click();
   await page.locator('.runtime-preset-dialog').waitFor({state:'detached'});
  }
  assert.equal(await page.locator('.runtime-preset-card').count(),2);
  const presets=await api('/applications/video/runtime-presets');assert.equal(presets.length,2);assert.equal(new Set(presets.map(p=>p.instanceId)).size,2);assert.equal(new Set(presets.map(p=>p.workspaceId)).size,2);
  assert(!fs.existsSync(path.join(dir,'legacy','rundest')),'stale historical directory was recreated');
  const original=(await api('/applications')).find(a=>a.appId==='video');assert.equal(original.instanceId,agent.id);assert.equal(original.workspaceId,ws.id);
  if(output)await page.screenshot({path:path.join(output,'presets-desktop.png'),fullPage:true});
  await page.locator('.runtime-add-preset').click();await page.locator('#preset-name').selectOption('sha256:hyperframes');
  if(output)await page.screenshot({path:path.join(output,'preset-dialog-desktop.png')});
  for(const [width,height] of [[1440,800],[800,600],[390,720],[360,640]]){
   await page.setViewportSize({width,height});
   const submit=await page.locator('#preset-submit').boundingBox();assert(submit.y>=0&&submit.y+submit.height<=height,'submit clipped at '+width+'x'+height);
   assert(await page.locator('.runtime-preset-dialog').evaluate(n=>n.scrollWidth<=n.clientWidth),'dialog horizontal overflow');
   await page.locator('.runtime-preset-technical').evaluate(n=>n.open=true);
   const expanded=await page.locator('#preset-submit').boundingBox();assert(expanded.y+expanded.height<=height,'expanded details hid submit');
  }
  if(output)await page.screenshot({path:path.join(output,'preset-dialog-mobile.png')});
  await page.route('**/api/v1/workspaces',async route=>{if(route.request().method()==='POST'){await page.unroute('**/api/v1/workspaces');await route.fulfill({status:409,json:{error:'预设工作目录不存在或不可访问，请检查 RunDesk 数据目录及磁盘挂载后重试',code:'runtime_preset_workspace_invalid'}})}else await route.continue()});
  await page.locator('#preset-submit').click();await page.getByRole('button',{name:'重试添加',exact:true}).waitFor();
  const submit=await page.locator('#preset-submit').boundingBox();assert(submit.y+submit.height<=640,'error hid retry button');
  if(output)await page.screenshot({path:path.join(output,'preset-error-mobile.png')});
  const beforeRetry=(await api('/instances')).length;await page.locator('#preset-submit').click();await page.locator('.runtime-preset-dialog').waitFor({state:'detached'});assert.equal((await api('/instances')).length,beforeRetry,'retry created duplicate instance');
  assert.equal((await api('/applications/video/runtime-presets')).length,3);await page.reload();await page.locator('.runtime-preset-card').first().waitFor();assert.equal(await page.locator('.runtime-preset-card').count(),3);
  assert.deepEqual(errors,[]);
  const result={passed:true,checks:['real Go API with missing historical directory','two presets added through browser without replacing default','third preset after failed workspace request reused Agent','bindings survive page reload','visible submit on desktop tablet and mobile with expanded details and error'],scope:'real compiled service in isolated demo data; only image catalog mocked, no Docker container or model invoked'};
  if(output)fs.writeFileSync(path.join(output,'preset-browser-result.json'),JSON.stringify(result,null,2));
  console.log(JSON.stringify(result));
  await client.dispose();
 }finally{if(browser)await browser.close();if(server.pid&&server.exitCode===null){await new Promise(resolve=>{server.once('exit',resolve);server.kill('SIGTERM')})}fs.rmSync(dir,{recursive:true,force:true})}
})().catch(e=>{console.error(e);process.exitCode=1});
