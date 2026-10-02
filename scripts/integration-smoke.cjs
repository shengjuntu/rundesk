// RunDesk v0.6 browser/API integration. Uses the explicit Codex protocol simulator.
const {chromium}=require(process.env.PLAYWRIGHT_PATH||"playwright");
const {spawn}=require("node:child_process");
const fs=require("node:fs"),os=require("node:os"),path=require("node:path"),assert=require("node:assert/strict");
(async()=>{
 const temp=fs.mkdtempSync(path.join(os.tmpdir(),"rundesk-v6-"));
 const port=33000+Math.floor(Math.random()*12000),base=`http://127.0.0.1:${port}`,token="integration-test-token-123456789012";
 const server=spawn(path.resolve(process.env.RUNDESK_BINARY||"bin/rundesk"),["--demo","--data",temp,"--listen",`127.0.0.1:${port}`],{env:{...process.env,RUNDESK_TOKEN:token},stdio:["ignore","ignore","pipe"]});
 let browser,logs="";server.stderr.on("data",b=>logs+=b);
 const checks=[];const ok=name=>checks.push(name);
 async function api(p,{method="GET",body,key,legacy=false}={}) {
  const r=await fetch(base+(legacy?"/api":"/api/v1")+p,{method,headers:{Authorization:"Bearer "+token,"Content-Type":"application/json",...(key?{"Idempotency-Key":key}:{})},body:body===undefined?undefined:JSON.stringify(body)});
  const data=await r.json();if(!r.ok)throw Error(`${r.status}: ${JSON.stringify(data)}`);return data;
 }
 try {
  for(let n=0;n<100;n++){try{await api("/meta");break;}catch{}await new Promise(r=>setTimeout(r,100));}
  const work=(await api("/workspaces"))[0];
  const inst=await api("/instances",{method:"POST",key:"create-news-assistant",body:{name:"新闻研究助手",description:"给应用提供新闻背景研究能力",defaultModel:"demo-fixture"}});
  const cp=suffix=>`/workspaces/${work.id}${suffix}?instanceId=${inst.id}`;
  await api(cp("/skills/research-guide")+"&scope=instance",{method:"PUT",legacy:true,body:{content:"---\nname: research-guide\ndescription: 新闻研究与证据整理\n---\nRead and save evidence."}});
  const mcp=await api(cp("/mcp"),{legacy:true});
  await api(cp("/mcp/news-research"),{method:"PUT",legacy:true,body:{version:mcp.version,config:{command:"demo-not-executed",enabled:false}}});
  ok("legacy_instance_skill_mcp_contract");
  const payload={instanceId:inst.id,workspaceId:work.id,title:"政策背景研究",source:{kind:"application",appId:"news2douyin",taskId:"research-case-42"}};
  const session=await api("/sessions",{method:"POST",key:"create-research-session",body:payload});
  const replay=await api("/sessions",{method:"POST",key:"create-research-session",body:payload});assert.equal(replay.id,session.id);
  assert.equal((await api("/sessions?appId=news2douyin&taskId=research-case-42")).length,1);ok("application_source_and_creation_receipt");
  browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH||undefined,args:["--no-sandbox","--disable-dev-shm-usage","--disable-gpu"]});
  const page=await browser.newPage({viewport:{width:1440,height:1000}});const errors=[];page.on("pageerror",e=>errors.push(e.message));
  await page.goto(base);await page.locator("#token").fill(token);await page.locator("#login-form button").click();await page.locator("#login").waitFor({state:"hidden"});
  await page.locator(`#instance option[value="${inst.id}"]`).waitFor({state:"attached"});await page.locator("#instance").selectOption(inst.id);
  await page.locator("#session-title").filter({hasText:"政策背景研究"}).waitFor();assert.match(await page.locator("#capability-strip").innerText(),/news2douyin/);ok("session_source_and_model_strip");
  await page.locator("#view-capabilities").click();await page.locator("#capabilities-content .capability-row").first().waitFor();
  const text=await page.locator("#capabilities-content").innerText();assert.match(text,/research-guide/);assert.match(text,/news-research/);assert.match(text,/已禁用/);assert.match(text,/research-case-42/);ok("session_capabilities_and_scope");
  await page.getByRole("button",{name:"配置所属实例",exact:true}).click();await page.locator(".configuration-tile").first().waitFor();
  assert.match(await page.locator("#settings-context").innerText(),/新闻研究助手/);await page.locator("#scan-instance-capabilities").click();await page.locator("#settings-content .capability-row").first().waitFor();
  const output=path.resolve(process.env.OUTPUT_DIR||"docs/screenshots/v0.6");fs.mkdirSync(output,{recursive:true});await page.screenshot({path:path.join(output,"instance-overview.png"),fullPage:true});ok("unified_instance_overview");
  await page.locator('.configuration-tile').filter({hasText:"模型与认证"}).click();await page.getByRole("button",{name:"读取可用模型",exact:true}).click();await page.locator("#instance-model").fill("next-session-model");await page.getByRole("button",{name:"保存实例",exact:true}).click();await page.waitForFunction(()=>document.querySelector("#instance-model")?.value==="next-session-model");
  await page.locator("#close-settings").click();assert.equal(await page.locator("#model").inputValue(),"demo-fixture");ok("instance_default_preserves_existing_session_model");
  // Drop the first acknowledgement after the server accepts it. Retrying from a
  // running session must replay the original start, never steer the text twice.
  let drop=true;await page.route("**/api/v1/sessions/*/turns",async route=>{if(drop){drop=false;await route.fetch();await route.abort("failed");}else await route.continue();});
  await page.locator("#prompt").fill("审批：测试任务提交回执恢复");await page.locator("#send").click();await page.locator(".approval").waitFor();
  await page.waitForFunction(()=>document.querySelector("#send-feedback").textContent.includes("未确认"));
  await page.locator("#send").click();await page.waitForFunction(()=>document.querySelector("#prompt").value==="");
  const events=await api(`/sessions/${session.id}/events`);assert.equal(events.filter(e=>e.method==="run/input").length,1);assert.equal(events.filter(e=>e.method==="run/steer").length,0);ok("lost_start_response_retries_without_duplicate_or_steer");
  const record=(await api("/sessions/"+session.id));await page.locator("#stop").click();await page.waitForFunction(()=>document.querySelector("#run-status").textContent==="已停止");
  const stopped=await api("/sessions/"+session.id);assert.equal(stopped.runId,record.runId);ok("stop_targets_expected_run");
  await page.locator("#view-capabilities").click();await page.locator("#capabilities-content .capability-row").first().waitFor();assert.match(await page.locator("#capabilities-content").innerText(),/上次提交与实际运行/);assert.match(await page.locator("#capabilities-content").innerText(),/demo-fixture/);
  await page.screenshot({path:path.join(output,"session-capabilities.png"),fullPage:true});ok("historical_runtime_separate_from_current_configuration");
  await page.locator("#close-capabilities").click();await page.reload();await page.locator("#view-capabilities").waitFor();assert.equal(await page.locator("#instance").inputValue(),inst.id);ok("reload_preserves_session_binding");
  await page.setViewportSize({width:390,height:844});await page.locator("#view-capabilities").click();await page.locator("#capabilities-content .capability-row").first().waitFor();assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth),true);await page.screenshot({path:path.join(output,"session-mobile.png"),fullPage:true});ok("mobile_capabilities_no_horizontal_overflow");
  assert.deepEqual(errors,[]);ok("no_javascript_exceptions");
  const spec=await api("/openapi.json");assert.equal(spec.openapi,"3.1.0");assert(spec.paths["/requests/{key}"]);ok("openapi_served_with_authentication");
  fs.writeFileSync(path.join(output,"integration-report.json"),JSON.stringify({passed:checks.length,checks,browser:await browser.version(),fixture:"Codex protocol simulator; no real model credentials used"},null,2));console.log(JSON.stringify({passed:checks.length,checks}));
 }catch(e){console.error(logs);throw e;}finally{if(browser)await browser.close();server.kill("SIGTERM");await new Promise(r=>{server.once("exit",r);setTimeout(r,4000)});fs.rmSync(temp,{recursive:true,force:true});}
})().catch(e=>{console.error(e);process.exitCode=1});
