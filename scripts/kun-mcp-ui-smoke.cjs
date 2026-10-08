// Local fixture validation; Playwright and Chromium are test-only dependencies.
const {chromium}=require(process.env.PLAYWRIGHT_PATH||'playwright');
const {spawn}=require('node:child_process'),fs=require('node:fs'),os=require('node:os'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
(async()=>{
 const root=path.resolve(__dirname,'..'),temp=fs.mkdtempSync(path.join(os.tmpdir(),'kun-mcp-ui-'));
 const pictures=path.join(root,'docs/screenshots/0.34.0');fs.mkdirSync(pictures,{recursive:true});
 let server,browser,toolCalls=0,modelCalls=0,listCalls=0;const errors=[];
 const fixture=http.createServer(async(req,res)=>{
  if(req.method==='DELETE'){res.writeHead(204).end();return}
  let raw='';for await(const b of req)raw+=b;const q=JSON.parse(raw);
  if(q.id===undefined){res.writeHead(202).end();return}
  let result={};if(q.method==='initialize')result={protocolVersion:'2025-11-25',capabilities:{tools:{}}};
  if(q.method==='tools/list'){listCalls++;result={tools:[{name:'lookup',description:'读取本地测试桩的天气信息，不连接外部服务。',inputSchema:{type:'object',properties:{city:{type:'string'}},required:['city']}}]}}
  if(q.method==='tools/call'){toolCalls++;assert.equal(q.params.name,'lookup');result={content:[{type:'text',text:'Shanghai 24°C; local MCP fixture'}]}}
  res.setHeader('Content-Type','application/json');res.end(JSON.stringify({jsonrpc:'2.0',id:q.id,result}));
 });
 const model=http.createServer(async(req,res)=>{
  let raw='';for await(const b of req)raw+=b;const q=JSON.parse(raw);modelCalls++;
  let message={role:'assistant',content:'MCP 调用完成：上海 24°C。'},finish_reason='stop';
  if(modelCalls%2===1){const name=q.tools.find(t=>t.function.name.startsWith('mcp_')).function.name;message={role:'assistant',content:'',tool_calls:[{id:'call-'+modelCalls,type:'function',function:{name,arguments:JSON.stringify({city:'Shanghai'})}}]};finish_reason='tool_calls'}
  res.setHeader('Content-Type','application/json');res.end(JSON.stringify({choices:[{message,finish_reason}],usage:{total_tokens:120}}));
 });
 try{
  await new Promise(r=>fixture.listen(0,'127.0.0.1',r));await new Promise(r=>model.listen(0,'127.0.0.1',r));
  const port=38731,base=`http://127.0.0.1:${port}`;
  server=spawn(root+'/bin/rundesk',['--data',temp+'/data','--listen',`127.0.0.1:${port}`],{env:{...process.env,RUNDESK_TOKEN:'',CODEX_HOME:temp+'/codex-home'},stdio:['ignore','ignore','pipe']});
  const api=async(p,method='GET',body)=>{const r=await fetch(base+'/api/v1'+p,{method,headers:{'Content-Type':'application/json'},body:body?JSON.stringify(body):undefined});assert(r.ok,await r.clone().text());return r.json()};
  for(let n=0;n<100;n++){try{await api('/meta');break}catch{}await new Promise(r=>setTimeout(r,80))}
  browser=await chromium.launch({executablePath:process.env.CHROMIUM_PATH,headless:true,args:['--no-sandbox']});
  const page=await browser.newPage({viewport:{width:1440,height:1000}});page.setDefaultTimeout(12000);page.on('pageerror',e=>errors.push(e.message));
  await page.addInitScript(()=>{localStorage.setItem('rundesk-language','zh');sessionStorage.setItem('rundesk-setup-dismissed','1')});await page.goto(base);
  await page.locator('#settings-button').click();await page.locator('[data-settings-tab=kun]').click();
  const form=page.locator('.kun-config');await form.locator('select').selectOption('kun');
  await form.locator('label').filter({hasText:'基础地址'}).locator('input').fill(`http://127.0.0.1:${model.address().port}/v1`);
  await form.locator('label').filter({hasText:'模型名称'}).locator('input').fill('kun-mcp-fixture');assert.equal(await form.locator(':invalid').count(),0,'initial engine defaults are invalid');await form.locator('button[type=submit]').click();
  await page.waitForFunction(()=>document.querySelector('.kun-config [role=status]')?.textContent.includes('已保存'));
  await page.locator('[data-settings-tab=mcp]').click();
  const editor=page.locator('details.configuration-editor');if(await editor.count())await editor.locator('summary').click();
  await page.locator('#mcp-name').fill('weather');await page.locator('#mcp-type').selectOption('http');await page.locator('#mcp-url').fill(`http://127.0.0.1:${fixture.address().port}/mcp`);
  const save=async()=>{const [res]=await Promise.all([page.waitForResponse(r=>r.url().includes('/mcp/weather')&&r.request().method()==='PUT'),page.getByRole('button',{name:'保存配置',exact:true}).click()]);assert(res.ok(),await res.text());await page.locator('#mcp-name').waitFor({state:'attached'})};
  const edit=async()=>page.locator('.skill-row').filter({hasText:'weather'}).getByRole('button',{name:'编辑',exact:true}).click();
  await save();await edit();await page.getByRole('button',{name:'读取已保存服务的工具列表',exact:true}).click();
  await page.locator('[data-mcp-tool=lookup]').waitFor();assert.equal(toolCalls,0);assert.equal(listCalls,1);
  await page.locator('[data-mcp-tool=lookup]').selectOption('prompt');await save();
  await page.locator('#capability-page .back-link').click();await page.locator('#new-session').click();
  await page.locator('#prompt').fill('查询上海天气。');await page.locator('#send').click();
  await page.locator('.kun-approval').waitFor();assert.equal(toolCalls,0,'tool executed before approval');
  await page.screenshot({path:pictures+'/kun-mcp-approval.png'});
  await page.locator('.kun-approval').getByRole('button',{name:'允许本次',exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('#messages')?.textContent.includes('MCP 调用完成'));assert.equal(toolCalls,1);
  await page.locator('#kun-debug-open').click();const dialog=page.locator('#kun-devtools');
  await dialog.getByRole('button',{name:'Application · MCP',exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('#kun-devtools .kun-application')?.textContent.includes('lookup'));
  assert((await dialog.locator('.kun-application').innerText()).includes('每次调用前询问'));await page.screenshot({path:pictures+'/kun-mcp-application.png'});
  await dialog.getByRole('button',{name:'Network · 调用',exact:true}).click();await dialog.locator('.kun-call-list button').filter({hasText:'tools/call · 已返回'}).click();
  assert((await dialog.locator('.kun-detail').innerText()).includes('Shanghai 24°C'));await page.screenshot({path:pictures+'/kun-mcp-network.png'});
  await dialog.getByRole('button',{name:'Elements · 上下文',exact:true}).click();await dialog.locator('.kun-call-list button').first().click();
  await page.waitForFunction(()=>document.querySelector('#kun-devtools .kun-detail')?.textContent.includes('"mcpTools"'));
  await dialog.getByRole('button',{name:'关闭',exact:true}).click();
  await page.locator('#settings-button').click();if(await page.locator('#settings').isVisible())await page.locator('[data-settings-tab=mcp]').click();await edit();await page.locator('[data-mcp-tool=lookup]').selectOption('approve');await save();await edit();
  await page.locator('[data-mcp-tool=lookup]').scrollIntoViewIfNeeded();await page.screenshot({path:pictures+'/kun-mcp-policy.png'});
  await page.locator('#capability-page .back-link').click();await page.locator('#prompt').fill('再查一次。');await page.locator('#send').click();
  await page.waitForFunction(()=>[...document.querySelectorAll('#messages *')].filter(e=>e.children.length===0&&e.textContent.includes('MCP 调用完成')).length>=2);
  assert.equal(toolCalls,2);assert.equal(modelCalls,4);assert.equal(await page.locator('.kun-approval').count(),0);
  const sessions=await api('/sessions');const sid=sessions.find(s=>s.runtimeKind==='kun').id;const events=await api(`/sessions/${sid}/events?limit=1000`);
  assert.equal(events.filter(e=>e.method==='kun/approval.requested').length,1,'always-allow asked again');
  await page.reload();await page.locator('#kun-debug-open').waitFor();await page.setViewportSize({width:390,height:844});await page.locator('#kun-debug-open').click();
  await page.locator('#kun-devtools').getByRole('button',{name:'Application · MCP',exact:true}).click();assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
  await page.screenshot({path:pictures+'/kun-mcp-mobile.png'});assert.deepEqual(errors,[]);
  const report={passed:true,modelCalls,toolCalls,listCalls,pageErrors:errors,checks:['Kun config UI','MCP discovery without tool execution','prompt holds execution','approve once','request/result inspection','tool definitions snapshot','always-allow next turn without approval','reload','390px viewport']};
  fs.writeFileSync(path.join(root,'docs/kun-mcp-ui-validation.json'),JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report));
 }finally{await browser?.close();if(server&&server.exitCode===null&&server.signalCode===null){server.kill();await new Promise(r=>server.once('exit',r))}for(const s of [fixture,model]){s.closeAllConnections();s.close()}fs.rmSync(temp,{recursive:true,force:true})}
})().catch(e=>{console.error(e);process.exitCode=1});
