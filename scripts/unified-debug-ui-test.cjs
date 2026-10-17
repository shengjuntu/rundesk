// Real Chromium with an explicit read-only DebugService fixture. No provider calls.
const {chromium}=require(process.env.PLAYWRIGHT_PATH||'playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{
 const root=path.resolve(__dirname,'..'),errors=[];let browser;const shots=process.env.UI_SCREENSHOT_DIR;if(shots)fs.mkdirSync(shots,{recursive:true});
 try{
  browser=await chromium.launch({executablePath:process.env.CHROMIUM_PATH,headless:true,args:['--no-sandbox']});
  const page=await browser.newPage({viewport:{width:1440,height:1000}});page.on('pageerror',e=>errors.push(e.message));
  await page.route('http://rundesk.test/**',route=>route.fulfill({contentType:'text/html',body:'<!doctype html><html><body><button id="trace-button">轨迹</button><textarea id="prompt"></textarea></body></html>'}));
  await page.goto('http://rundesk.test/');
  for(const file of ['style.css','kun.css','debug-trace.css'])await page.addStyleTag({content:fs.readFileSync(path.join(root,'internal/web',file),'utf8')});
  await page.evaluate(()=>{
   window.requests=[];window.errors=[];window.rdText=x=>x;window.$=s=>document.querySelector(s);window.toast=e=>window.errors.push(String(e));window.safe=fn=>Promise.resolve().then(fn).catch(e=>window.errors.push(e.message));
   window.el=(tag,attrs={},...children)=>{const n=document.createElement(tag);for(const[k,v]of Object.entries(attrs)){if(k==='class')n.className=v;else if(k.startsWith('on'))n.addEventListener(k.slice(2),v);else if(v!==false&&v!=null)n.setAttribute(k,v===true?'':v);}for(const c of children.flat())if(c!=null)n.append(c instanceof Node?c:document.createTextNode(String(c)));return n;};
   window.button=(text,fn,cls='')=>el('button',{type:'button',class:cls,onclick:()=>safe(fn)},text);
   window.state={session:{id:'s1',title:'结构化调试',runtimeKind:'kun',observation:{state:'configured',message:'已接入直通代理'}}};
   window.refreshSessions=async()=>{};window.selectSession=async()=>{};window.scheduleRender=()=>{};
   const before='{ "method":"item/started", "params":{"item":{"arguments":{"exact":9007199254740993,"nested":{"text":"'+ '中'.repeat(17000)+'"}}}}}';
   const after='{"method":"item/completed","params":{"item":{"result":{"answer":"完整结果 <img src=x onerror=alert(1)>"}}}}';
   const events=[{id:10,method:'kun/model.started',data:{sequence:1,runId:'run-1',data:{step:1,request:{model:'fixture',messages:[],tools:[]}}}},{id:11,method:'kun/model.completed',data:{sequence:2,runId:'run-1',data:{step:1,message:{content:'完成'},usage:{total_tokens:3}}}},{id:100,method:'kun/model.started',data:{sequence:3,runId:'run-2',data:{step:2,request:{model:'future'}}}}];
   const current={status:'paused',phase:'before_model',runId:'run-2',revision:7,step:2,debug:{pause:{phase:'before_model'}},actions:{},modules:{},budget:{}};
   window.api=async(url,opts={})=>{
    requests.push({url,method:opts.method||'GET'});const u=new URL(url,'http://rundesk.test');
    if(u.pathname==='/sessions/s1')return state.session;
    if(u.pathname.endsWith('/debug/capabilities'))return {backend:'kun',queries:['runs','steps','step','event','llm_requests','llm_request']};
    if(u.pathname.endsWith('/events'))return events.filter(e=>e.id>Number(u.searchParams.get('after')||0));
    if(u.pathname.endsWith('/kun/state'))return current;
    if(u.pathname.includes('/kun/snapshots/'))return {state:{...current,runId:'run-1',revision:3,step:1,messages:[],modules:{},budget:{}}};
    if(u.pathname.endsWith('/debug/query')){
     const q=u.searchParams,kind=q.get('kind'),offset=Number(q.get('offset')||0),limit=Number(q.get('limit')||20),run=q.get('runId');
     if(kind==='events')return {data:{through:99,events:[]}};
     if(kind==='runs')return {data:{runs:[{id:'run-1',question:'原始请求',status:'completed'},{id:'run-2',question:'后续运行',status:'running'}],nextOffset:2,hasMore:false,total:2}};
     if(kind==='statistics')return {data:{steps:1,attentionSteps:0,withoutCompleteDuration:0}};
     const step={id:'canonical-step',runId:'run-1',type:'mcpToolCall',title:'结构化工具',status:'completed',eventIds:[10,11],workerSequences:[1,2],durationMs:100,issues:[],startObserved:true,endObserved:true,association:'observed',clock:'host',preview:{arguments:'preview only'},previewTruncated:true};
     if(kind==='steps'||kind==='issues')return {data:{steps:[step],nextOffset:1,total:1,hasMore:false}};
     if(kind==='step')return {data:step};
     if(kind==='event'){const raw=q.get('eventId')==='10'?before:after,chars=[...raw],end=Math.min(chars.length,offset+limit);return {data:{text:chars.slice(offset,end).join(''),totalCharacters:chars.length,nextOffset:end,hasMore:end<chars.length}};}
     const record={eventId:50,id:'http-1',runId:'run-1',method:'POST',url:'https://api.example.test/v1/responses',statusCode:200,durationMs:50,complete:true,requestBytes:25,responseBytes:45};
     const wsRecord={...record,eventId:51,id:'ws-1',method:'GET',statusCode:101,runId:'',websocket:true};
     if(kind==='llm_requests'){const rows=run?[record]:[record,wsRecord];return {data:{requests:rows,nextOffset:rows.length,hasMore:false,total:rows.length}};}
     if(kind==='llm_request'){
      const part=q.get('section'),isWS=q.get('eventId')==='51';if(part==='metadata')return {data:{...(isWS?wsRecord:record),...(isWS?{websocket:{encoding:'raw frames'}}:{}),request:{headers:{Authorization:['[redacted]'],'Content-Type':['application/json']}},response:{headers:{'Content-Type':['text/event-stream']}}}};
      if(part.endsWith('_messages'))return {data:{messages:[{index:0,type:'text',payloadBytes:18050,complete:true,compressed:false},{index:1,type:'binary',payloadBytes:2,complete:true,compressed:true}],nextOffset:2,hasMore:false,decode:{compressionUnsupported:true,complete:true}}};
      if(part.endsWith('_payload')){if(q.get('messageIndex')==null)throw Error('Missing message index');const compressed=q.get('messageIndex')==='1',raw=JSON.stringify({type:'response.create',model:'ws-original-model',input:'汉'.repeat(6000)+'尾部',tools:[{name:'structured_tool',parameters:{type:'object',properties:{query:{type:'string'}}}}]}),bytes=compressed?new Uint8Array([1,255]):new TextEncoder().encode(raw),end=Math.min(bytes.length,offset+limit),chunk=bytes.slice(offset,end);return {data:{text:new TextDecoder().decode(chunk),bodyBase64:btoa(String.fromCharCode(...chunk)),offset,nextOffset:end,totalBytes:bytes.length,hasMore:end<bytes.length,complete:true,compressed}};}
      if(part.endsWith('_chunks'))return {data:{chunks:[{sequence:1,offset:0,bytes:12},{sequence:2,offset:12,bytes:33}],nextOffset:2,hasMore:false}};
      const raw=part==='request'?JSON.stringify({model:'original-model',input:'汉'.repeat(6000)+'尾部',tools:[{parameters:{type:'object'}}]}):'data: {"delta":"one"}\n\ndata: {"delta":"two"}\n\n';const bytes=new TextEncoder().encode(raw),end=Math.min(bytes.length,offset+limit),chunk=bytes.slice(offset,end);return {data:{text:new TextDecoder().decode(chunk),bodyBase64:btoa(String.fromCharCode(...chunk)),offset,nextOffset:end,totalBytes:bytes.length,hasMore:end<bytes.length,complete:true}};
     }
    }
    throw Error('Unexpected fixture request '+url);
   };
  });
  for(const file of ['kun-inspect.js','kun-panels.js','kun-debug.js','kun.js','debug-trace.js','trace.js'])await page.addScriptTag({content:fs.readFileSync(path.join(root,'internal/web',file),'utf8')});
  await page.locator('#trace-button').click();const d=page.locator('#debug-inspector');await d.locator('.debug-step').first().waitFor();
  await d.getByRole('combobox',{name:'调试轮次'}).selectOption('run-1');await d.locator('.debug-step').first().click();
  await d.getByRole('button',{name:'读取完整结构（所有来源事件）',exact:true}).click();await d.getByText('已完整读取 2 个来源事件（已脱敏）。',{exact:true}).waitFor();
  const evidence=await d.locator('.debug-evidence').innerText();assert(evidence.includes('9007199254740993'),'large integer rounded');assert(evidence.includes('完整结果'),'completion event missing');assert(evidence.includes('中'.repeat(17000)),'chunked arguments truncated');assert.equal(await d.locator('img').count(),0);
  await d.locator('.debug-workbench-tabs [data-panel=http]').click();await d.locator('.debug-http-list .debug-step').first().click();await d.getByText('原始字节 Base64（精确分页）',{exact:true}).waitFor();
  assert((await d.locator('.debug-http-detail').innerText()).includes('original-model'));assert((await d.locator('.debug-capture-state').innerText()).includes('已配置，待核对请求'));
  await d.getByRole('button',{name:'读取完整已保存正文',exact:true}).click();await d.getByRole('button',{name:'下载已保存原始字节',exact:true}).waitFor();const whole=await d.locator('.debug-http-detail pre').last().textContent();assert(whole.includes('汉'.repeat(6000)+'尾部'),'byte pages did not reconstruct complete Unicode body');assert(!whole.includes('�'),'UTF-8 body split corrupted characters');
  await d.getByRole('combobox',{name:'HTTP 采集内容'}).selectOption('response');await page.waitForFunction(()=>document.querySelector('.debug-http-detail').textContent.includes('"delta":"two"'));
  assert((await d.locator('.debug-http-detail').innerText()).indexOf('"delta":"one"')<(await d.locator('.debug-http-detail').innerText()).indexOf('"delta":"two"'));
  if(shots)await page.screenshot({path:path.join(shots,'unified-http.png')});
  await d.getByRole('combobox',{name:'调试轮次'}).selectOption('');await d.locator('.debug-http-list .debug-step').nth(1).waitFor();await d.locator('.debug-http-list .debug-step').nth(1).click();await d.getByRole('combobox',{name:'WebSocket 消息'}).waitFor();await page.waitForFunction(()=>document.querySelector('.debug-http-detail').textContent.includes('ws-original-model'));await d.getByRole('button',{name:'读取完整消息负载',exact:true}).click();await d.getByRole('button',{name:'下载消息 Payload 字节',exact:true}).waitFor();const wsBody=await d.locator('.debug-http-detail pre').last().textContent();assert(wsBody.includes('structured_tool'));assert(wsBody.includes('汉'.repeat(6000)+'尾部'));assert(!wsBody.includes('�'));if(shots)await page.screenshot({path:path.join(shots,'unified-websocket.png')});
  await d.getByRole('combobox',{name:'WebSocket 消息'}).selectOption('1');await page.waitForFunction(()=>document.querySelector('.debug-http-detail').textContent.includes('压缩消息尚未解压'));assert(!(await d.locator('.debug-http-detail pre').last().textContent()).includes('ws-original-model'));await d.getByRole('combobox',{name:'调试轮次'}).selectOption('run-1');
  await d.locator('.debug-workbench-tabs [data-panel=kun]').click();await d.locator('#kun-devtools .kun-call-list button').first().waitFor();assert.equal(await page.locator('dialog[open]').count(),1);assert.equal(await d.locator('#kun-devtools .kun-call-list button').count(),1,'future run leaked into fixed scope');
  await d.locator('#kun-devtools [data-panel=sources]').click();assert(await d.locator('#kun-devtools').getByRole('button',{name:'继续',exact:true}).isDisabled(),'historical selection can control another run');
  await page.evaluate(()=>RunDeskKun.open());assert.equal(await page.locator('dialog[open]').count(),1,'legacy entry opened a second dialog');
  await d.locator('.debug-workbench-tabs [data-panel=analysis]').click();assert(await d.getByRole('button',{name:'创建诊断并发送问题',exact:true}).isDisabled());await d.getByRole('textbox',{name:'诊断问题'}).fill('请检查参数');assert(await d.getByRole('button',{name:'创建诊断并发送问题',exact:true}).isEnabled());
  if(shots)await page.screenshot({path:path.join(shots,'unified-analysis.png')});
  await page.setViewportSize({width:390,height:844});if(shots)await page.screenshot({path:path.join(shots,'unified-mobile.png')});assert(await d.evaluate(n=>n.scrollWidth<=n.clientWidth+1),'mobile workbench overflow');
  const req=await page.evaluate(()=>requests);assert(req.every(r=>r.method==='GET'),'inspection had side effects');const fixed=req.filter(r=>/kind=(step|steps|statistics|llm_request)/.test(r.url));assert(fixed.every(r=>new URL(r.url,'http://rundesk.test').searchParams.get('through')==='99'),'views use different source cutoffs');
  assert.deepEqual(errors,[]);assert.deepEqual(await page.evaluate(()=>window.errors),[]);
  await d.getByRole('button',{name:'返回对话',exact:true}).click();await d.waitFor({state:'detached'});assert.equal(await page.locator('#kun-devtools').count(),0,'mounted inspector leaked after close');
  console.log(JSON.stringify({passed:true,scope:'real Chromium with explicit DebugService fixture; no model calls',checks:['canonical server projection','all start/end evidence with Unicode pagination and exact integers','HTTP byte reconstruction across UTF-8 boundaries and ordered SSE response','decoded WebSocket message selection and exact payload bytes with unsupported-compression labels','capture state labels','shared run and fixed through','single mounted Kun workbench','historical control protection','read-only inspection','mobile layout']}));
 }finally{await browser?.close();}
})().catch(e=>{console.error(e);process.exitCode=1});
