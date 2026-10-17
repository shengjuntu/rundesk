// Browser regression for the shipped session-capture control with an explicit HTTP API fixture.
// This tests UI state/requests, not Go persistence or a real provider connection.
const {chromium}=require(process.env.PLAYWRIGHT_PATH||'playwright');
const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
(async()=>{
 const root=path.resolve(__dirname,'..'),errors=[],requests=[],checks=[],shots=process.env.UI_SCREENSHOT_DIR;
 const clone=x=>JSON.parse(JSON.stringify(x)),sessions=new Map([
  ['s1',{id:'s1',title:'运行中的 Codex 会话',runtimeKind:'codex',status:'running',runId:'run-1',observeHTTP:false,observation:{state:'disabled',appliedEnabled:false,pending:false,message:'此会话尚未启用采集。'}}],
  ['s2',{id:'s2',title:'另一个会话',runtimeKind:'codex',status:'idle',observeHTTP:false,observation:{state:'disabled',appliedEnabled:false,pending:false,message:'此会话尚未启用采集。'}}],
  ['kun1',{id:'kun1',title:'Kun 会话',runtimeKind:'kun',status:'idle',observeHTTP:false,observation:{state:'unavailable',appliedEnabled:false,pending:false,message:'Kun 不使用 Codex HTTP 采集。'}}]
 ]);
 let patchMode='normal',releasePatch,server,browser;
 const menu=fs.readFileSync(path.join(root,'internal/web/index.html'),'utf8').match(/<details id="session-menu"[\s\S]*?<\/details>/)[0].replace('session-menu hidden','session-menu');
 const html=`<!doctype html><html lang="zh"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/style.css"><link rel="stylesheet" href="/kun.css"><link rel="stylesheet" href="/debug-trace.css"></head><body><button id="trace-button">调试工作台</button>${menu}<textarea id="prompt"></textarea><script>
 window.errors=[];window.rdText=x=>x;window.$=s=>document.querySelector(s);window.toast=e=>errors.push(String(e));window.safe=fn=>Promise.resolve().then(fn).catch(e=>errors.push(e.message));
 window.el=(tag,attrs={},...children)=>{const n=document.createElement(tag);for(const[k,v]of Object.entries(attrs)){if(k==='class')n.className=v;else if(k.startsWith('on'))n.addEventListener(k.slice(2),v);else if(v!==false&&v!=null)n.setAttribute(k,v===true?'':v);}for(const c of children.flat())if(c!=null)n.append(c instanceof Node?c:document.createTextNode(String(c)));return n;};
 window.button=(text,fn,cls='')=>el('button',{type:'button',class:cls,onclick:()=>safe(fn)},text);
 window.api=async(url,options={})=>{const response=await fetch('/api/v1'+url,{method:options.method||'GET',headers:{'Content-Type':'application/json'},body:options.body?JSON.stringify(options.body):undefined});const value=await response.json();if(!response.ok)throw Error(value.error||'HTTP '+response.status);return value;};
 window.state={session:null,sessions:[]};window.refreshSessions=async()=>{};window.scheduleRender=()=>{};
 window.selectSession=async id=>{window.RunDeskDebug?.close();state.session=await api('/sessions/'+id);state.sessions=[state.session];};
 </script><script src="/debug-trace.js"></script><script src="/trace.js"></script><script>selectSession(new URLSearchParams(location.search).get('sid')||'s1').then(()=>window.ready=true);</script></body></html>`;
 const send=(res,value,status=200)=>{res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(value));};
 server=http.createServer(async(req,res)=>{
  try{
   const u=new URL(req.url,'http://fixture');if(!u.pathname.startsWith('/api/v1/')){if(u.pathname==='/'){res.writeHead(200,{'Content-Type':'text/html'});res.end(html);return;}const name=u.pathname.slice(1);if(!['style.css','kun.css','debug-trace.css','debug-trace.js','trace.js'].includes(name)){res.writeHead(404);res.end();return;}res.writeHead(200,{'Content-Type':name.endsWith('.css')?'text/css':'text/javascript'});res.end(fs.readFileSync(path.join(root,'internal/web',name)));return;}
   const api=u.pathname.slice('/api/v1'.length),sid=api.split('/')[2],session=sessions.get(sid);let body='';for await(const chunk of req)body+=chunk;
   requests.push({method:req.method,path:api,body:body?JSON.parse(body):undefined});
   if(/^\/sessions\/[^/]+$/.test(api)){
    if(!session){send(res,{error:'unknown session'},404);return;}
    if(req.method==='GET'){send(res,clone(session));return;}
    if(req.method==='PATCH'){
     const mode=patchMode;patchMode='normal';const p=JSON.parse(body);assert.deepEqual(Object.keys(p),['observeHTTP']);assert.equal(typeof p.observeHTTP,'boolean');
     if(mode==='reject'){send(res,{error:'保存失败-fixture'},409);return;}
     if(mode==='invalid'){send(res,{...clone(session),id:'wrong-session',observeHTTP:p.observeHTTP});return;}
     session.observeHTTP=p.observeHTTP;session.observation.pending=session.observation.appliedEnabled!==p.observeHTTP;session.observation.state=session.observation.pending?'pending':p.observeHTTP?'configured':'disabled';session.observation.message='偏好已保存，当前轮不受影响。';
     const answer=clone(session);if(mode==='hold'){releasePatch=()=>send(res,answer);return;}send(res,answer);return;
    }
   }
   if(api.endsWith('/debug/capabilities')){send(res,{backend:session.runtimeKind,queries:['steps','llm_requests']});return;}
   if(api.endsWith('/debug/query')){const kind=u.searchParams.get('kind');if(kind==='events'){send(res,{data:{through:20,events:[]}});return;}if(kind==='runs'){send(res,{data:{runs:[{id:'run-1',question:'当前任务',status:'running'}],total:1,nextOffset:1,hasMore:false}});return;}if(kind==='statistics'){send(res,{data:{steps:0,attentionSteps:0,withoutCompleteDuration:0}});return;}if(kind==='steps'){send(res,{data:{steps:[],total:0,nextOffset:0,hasMore:false}});return;}if(kind==='llm_requests'){send(res,{data:{requests:[],total:0,nextOffset:0,hasMore:false}});return;}}
   send(res,{error:'Unexpected fixture endpoint '+api},404);
  }catch(e){send(res,{error:e.message},500);}
 });
 try{
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const base='http://127.0.0.1:'+server.address().port;
  browser=await chromium.launch({executablePath:process.env.CHROMIUM_PATH,headless:true,args:['--no-sandbox']});const page=await browser.newPage({viewport:{width:1440,height:1000}});page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base);await page.waitForFunction(()=>window.ready);
  const menuOpen=async()=>{await page.locator('#session-menu summary').click();await page.locator('#session-http-observation').click();await page.locator('#debug-inspector').waitFor();await page.waitForFunction(()=>document.querySelector('.debug-capture-settings').getAttribute('aria-busy')==='false');};
  await menuOpen();let d=page.locator('#debug-inspector'),toggle=d.getByRole('checkbox',{name:'为此会话启用 HTTP 采集'});
  assert.equal(await d.getAttribute('data-panel'),'http');assert.equal(await page.locator('#session-menu').evaluate(n=>n.open),false);assert.equal(await toggle.isChecked(),false);assert(await toggle.isEnabled());assert((await d.locator('.debug-capture-settings').innerText()).includes('已保存设置：关闭'));assert.equal(requests.filter(r=>r.method==='PATCH').length,0);checks.push('session menu discovers the one HTTP-page switch; default remains off');
  await toggle.check();await d.getByText('设置已保存，下一轮提交时应用；不会中断当前轮。',{exact:true}).waitFor();assert(await toggle.isChecked());assert.equal(sessions.get('s1').observation.appliedEnabled,false);assert((await d.locator('.debug-capture-settings').innerText()).includes('开启设置尚未生效'));assert(!(await d.locator('.debug-capture-state').innerText()).includes('已有采集记录'));checks.push('enabling saves a preference without claiming current capture or issuing a turn');
  // Simulate the backend applying the saved preference at the next turn, then reload the browser.
  sessions.get('s1').observation={state:'configured',appliedEnabled:true,pending:false,message:'代理已配置，尚未记录真实请求。'};
  await page.reload();await page.waitForFunction(()=>window.ready);await menuOpen();d=page.locator('#debug-inspector');toggle=d.getByRole('checkbox',{name:'为此会话启用 HTTP 采集'});assert(await toggle.isChecked());assert((await d.locator('.debug-capture-settings').innerText()).includes('已应用直通代理配置'));assert(!(await d.locator('.debug-capture-state').innerText()).includes('已有采集记录'));checks.push('browser reload reads the persisted preference and separately reported applied state');
  await toggle.uncheck();await d.getByText('设置已保存，下一轮提交时应用；不会中断当前轮。',{exact:true}).waitFor();assert.equal(await toggle.isChecked(),false);assert((await d.locator('.debug-capture-settings').innerText()).includes('当前轮仍沿用代理配置'));assert.equal(sessions.get('s1').observation.appliedEnabled,true);checks.push('disabling during a proxied turn does not label the current connection direct');
  patchMode='reject';await toggle.click();await d.getByText(/未确认保存：保存失败-fixture/).waitFor();assert.equal(await toggle.isChecked(),false);assert(await toggle.isEnabled());checks.push('rejected save rolls the checkbox back to the last confirmed value');
  patchMode='invalid';await toggle.click();await d.getByText(/未确认保存：会话设置响应与目标不符/).waitFor();assert.equal(await toggle.isChecked(),false);assert.equal(await page.evaluate(()=>state.session.id),'s1');checks.push('a mismatched session response cannot overwrite local settings');
  await page.route('**/api/v1/sessions/s1',async route=>{await route.fetch();await route.abort('failed');},{times:1});await toggle.click();await d.getByText(/未确认保存：.*已恢复上次确认的显示/).waitFor();assert.equal(await toggle.isChecked(),false);assert.equal(sessions.get('s1').observeHTTP,true);await d.getByRole('button',{name:'核对采集状态',exact:true}).click();await d.getByText('已核对服务端保存的设置与连接状态。',{exact:true}).waitFor();assert(await toggle.isChecked());checks.push('a lost response is explicitly uncertain and a GET reconciles the actual saved value');
  patchMode='hold';await toggle.uncheck();await d.getByText('正在保存此会话设置…',{exact:true}).waitFor();assert(await toggle.isDisabled());assert(await d.getByRole('button',{name:'核对采集状态',exact:true}).isDisabled());
  await page.evaluate(()=>selectSession('s2'));await d.waitFor({state:'detached'});await menuOpen();d=page.locator('#debug-inspector');toggle=d.getByRole('checkbox',{name:'为此会话启用 HTTP 采集'});assert.equal(await toggle.isChecked(),false);assert(releasePatch);releasePatch();await page.waitForTimeout(100);assert.equal(await page.evaluate(()=>state.session.id),'s2');assert.equal(await page.evaluate(()=>state.session.observeHTTP),false);assert.equal(await toggle.isChecked(),false);assert(!(await d.locator('.debug-capture-settings').innerText()).includes('当前轮仍沿用代理配置'));checks.push('saving is serialized and a response arriving after session switch is ignored');
  await page.evaluate(()=>selectSession('kun1'));await d.waitFor({state:'detached'});await menuOpen();d=page.locator('#debug-inspector');assert.equal(await d.getByRole('checkbox',{name:'为此会话启用 HTTP 采集'}).count(),0);assert((await d.locator('.debug-capture-settings').innerText()).includes('Kun 不使用 Codex HTTP 采集开关'));checks.push('Kun explains the unsupported setting without an enable control');
  await page.evaluate(()=>selectSession('s1'));await d.waitFor({state:'detached'});await menuOpen();d=page.locator('#debug-inspector');toggle=d.getByRole('checkbox',{name:'为此会话启用 HTTP 采集'});assert.equal(await toggle.isChecked(),false);assert((await d.locator('.debug-capture-settings').innerText()).includes('当前轮仍沿用代理配置'));
  if(shots){fs.mkdirSync(shots,{recursive:true});await page.screenshot({path:path.join(shots,'capture-settings-desktop.png')});}await page.setViewportSize({width:390,height:844});assert(await d.evaluate(n=>n.scrollWidth<=n.clientWidth+1),'mobile workbench overflow');assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'mobile page overflow');if(shots)await page.screenshot({path:path.join(shots,'capture-settings-mobile.png')});checks.push('closed/reopened sessions read server state and the control fits mobile');
  const writes=requests.filter(r=>r.method!=='GET');assert.equal(writes.length,6);assert(writes.every(r=>r.method==='PATCH'&&r.path==='/sessions/s1'&&Object.keys(r.body).length===1));assert.deepEqual(errors,[]);assert.deepEqual(await page.evaluate(()=>window.errors),[]);
  console.log(JSON.stringify({passed:true,scope:'real Chromium + explicit HTTP session API fixture; Go persistence and provider transport tested separately',checks,pageErrors:errors}));
 }finally{await browser?.close();server.closeAllConnections();await new Promise(resolve=>server.close(resolve));}
})().catch(e=>{console.error(e);process.exitCode=1});
