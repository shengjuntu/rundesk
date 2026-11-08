/* Isolated real Go demo service + scoped setup/build fixtures. Requires
 * Playwright Chromium and RUNDESK_BINARY. No model, worker or Docker runs. */
"use strict";
const {spawn}=require("node:child_process"),fs=require("node:fs"),path=require("node:path"),os=require("node:os"),assert=require("node:assert/strict");
const {chromium}=require("playwright");
(async()=>{
 const dir=fs.mkdtempSync(path.join(os.tmpdir(),"rundesk-setup-docker-qa-")),out=process.env.RUNDESK_BROWSER_OUTPUT||path.join(dir,"results");fs.mkdirSync(out,{recursive:true});
 const server=spawn(process.env.RUNDESK_BINARY||path.resolve(__dirname,"../../../bin/rundesk"),["--demo","--listen","127.0.0.1:0","--data",dir],{stdio:["ignore","ignore","pipe"]});let browser;
 try{
  const base=await new Promise((resolve,reject)=>{let log="";const timer=setTimeout(()=>reject(Error(log||"server startup timeout")),20000);server.stderr.on("data",d=>{log+=d;const m=log.match(/RunDesk (http:\/\/127\.0\.0\.1:\d+)/);if(m){clearTimeout(timer);resolve(m[1]);}});server.on("error",reject);});
  browser=await chromium.launch({headless:true,...(process.env.RUNDESK_BROWSER_EXECUTABLE?{executablePath:process.env.RUNDESK_BROWSER_EXECUTABLE}:{})});const page=await browser.newPage({viewport:{width:1440,height:1080},locale:"zh-CN"}),errors=[],writes=[],reads=[],manual=[];
  page.on("pageerror",e=>errors.push(e.message));await page.addInitScript(()=>{localStorage.setItem("rundesk-language","zh");sessionStorage.setItem("rundesk-setup-dismissed","1");});
  const source=(id,name)=>({id,name,status:"failed",scope:"recipe",recipeId:"builtin-news",error:"原始构建失败："+id});
  const a=source("source-a","新闻视频环境 A"),b=source("source-b","人物研究环境 B"),c=source("source-c","手动修复环境 C");
  const attempt=(verified=false)=>({plan:{requestId:"candidate-plan",goal:"调整依赖打包",hypothesis:"重复包含资源",preserve:["预置工具"],checks:["offline","smoke"]},buildId:"candidate-b",status:verified?"succeeded":"running",imageId:verified?"sha256:"+"f".repeat(64):"",contextSha256:"context-b",verificationIds:verified?["offline-b","smoke-b"]:[],offline:verified,smoke:verified});
  let builds=[a,b,c,{id:"candidate-b",name:"B 修复候选",scope:"recipe",recipeId:"custom-news",status:"running",ownerRunId:"repair-b",rebuildOf:"source-b"},{id:"test-b",name:"镜像检查记录",scope:"image-test",source:"local",reference:"local:test",sourceBuildId:"source-b",status:"failed",error:"独立镜像检查"}];
  let runs=[{id:"repair-a",sourceBuildId:"source-a",status:"running",sessionId:"session-a",maintenance:{phase:"building",maxAttempts:3,attempts:[]}},{id:"repair-b",sourceBuildId:"source-b",status:"succeeded",sessionId:"session-b",maintenance:{phase:"verifying",maxAttempts:3,attempts:[attempt()]}}];
  runs.push({id:"repair-missing",sourceBuildId:"source-missing",status:"running",sessionId:"session-missing",maintenance:{phase:"planning",maxAttempts:3,attempts:[]}});
  const autos=[{id:"auto-a",kind:"image-build",sourceId:"source-a",enabled:true,status:"submitted",repairId:"repair-a",sessionId:"session-a"},{id:"auto-missing",kind:"image-build",sourceId:"source-missing",enabled:true,status:"submitted",repairId:"repair-missing",sessionId:"session-missing"}];let failManual=true,buildReads=0;
  const recipe={id:"builtin-news",profileId:"news",kind:"builtin",name:"新闻视频环境",version:"1.1.2",recipeSha:"sha",revision:1};
  await page.route("**/api/v1/**",async route=>{
   const req=route.request(),u=new URL(req.url()),p=u.pathname.replace("/api/v1",""),method=req.method();if(method!=="GET")writes.push({path:p,method});else reads.push(p+u.search);
   const json=x=>route.fulfill({json:x});
   if(p==="/setup/kun")return json({ready:true,configured:true,available:true,instanceId:"setup-kun",workspaceId:"kun-work",kunPath:"/tools/kun",endpoint:"http://127.0.0.1:8000/v1",model:"fixture"});
   if(["/setup/codex-installs","/setup/repairs","/setup/experiences"].includes(p))return json({items:[]});
   if(p==="/setup/auto-repairs")return json({items:autos});
   if(p==="/setup/discovery")return json({platform:{os:"linux",arch:"amd64",cpuCount:8},tools:[],gpus:[],services:[],containers:[]});
   if(p==="/setup/check")return json({ok:true});
   if(p==="/build-center/sources")return json({schemaVersion:1,revision:"sources-1",defaultGroup:"international",types:[],groups:[{id:"international",name:"国际源",sources:[]}]});
   if(p==="/build-center/recipes")return json({items:[recipe]});
   if(p==="/build-center/recipes/builtin-news/sources")return json({types:[]});
   if(p==="/build-center/builds"){buildReads++;return json({items:builds});}
   if(p==="/setup/public-images")return json({schemaVersion:1,revision:"public-1",items:[]});
   if(p==="/setup/runtime-profiles")return json({platform:"linux/amd64",supported:true,items:[{id:"news",name:"新闻视频环境"}]});
   if(p==="/build-center/assistant-runs"&&method==="GET"){const id=u.searchParams.get("sourceBuildId");assert(id,"assistant lookup must stay scoped");return json({items:runs.filter(r=>r.sourceBuildId===id),hasMore:false});}
   if(p.startsWith("/build-center/assistant-runs/")){const row=runs.find(r=>p.endsWith("/"+r.id));assert(row);return json(row);}
   if(p.startsWith("/build-center/builds/")&&p.endsWith("/log"))return json({text:"本轮构建日志："+p.split("/")[3]+"\n"+Array.from({length:100},(_,i)=>"日志行 "+i).join("\n"),next:32});
   if(p==="/build-center/assistant-runs"&&method==="POST"){
    const data=req.postDataJSON();manual.push(data);assert.equal(data.sourceBuildId,"source-c");assert.match(data.requestId,/^[a-f0-9]{24}$/);
    if(failManual){failManual=false;return route.fulfill({status:503,json:{error:"fixture: response unknown",code:"build_operation_unconfirmed",retryable:true}});}
    const row={id:"repair-c",sourceBuildId:"source-c",status:"queued",sessionId:"session-c",maintenance:{phase:"planning",maxAttempts:3,attempts:[]}};runs.push(row);return json(row);
   }
   if(p==="/setup/runtime-images"&&method==="POST"){const data=req.postDataJSON();const row={id:"download-failed",name:"下载失败环境",scope:"setup",profileId:"news",codexVersion:"0.157.1",source:"pull",reference:data.reference,status:"failed",error:"registry unreachable"};builds.unshift(row);return json(row);}
   return route.continue();
  });
  await page.goto(base+"/");await page.locator("body:not(.rd-starting)").waitFor();await page.locator("#system-setup").click();await page.locator('[data-setup-step="docker"]').click();
  await page.locator('[data-repair-id="repair-b"]').waitFor();assert.equal(await page.locator("#onboarding-docker-mode").evaluate(n=>n.tagName),"FIELDSET");
  const mode=value=>page.locator('[name="onboarding-docker-mode"][value="'+value+'"]'),card=id=>page.locator('[data-build-id="'+id+'"]');
  assert(await card("source-missing").locator('[data-repair-id="repair-missing"]').count(),"repair for an absent source vanished");assert.notEqual(await card("source-missing").getAttribute("data-preparation-state"),"ready");
  assert(await mode("pull").isChecked());assert(!await page.locator("#onboarding-repairs").count());assert(!await page.locator("#onboarding-docker-version").count());
  assert.equal(await card("source-a").locator('a[href*="session-a"]').count(),1);assert.equal(await card("source-a").locator('a[href*="session-b"]').count(),0);
  assert.equal(await card("source-b").locator('[data-build-attempt="candidate-b"]').count(),1);assert.equal(await card("source-b").locator('[data-build-attempt="test-b"]').count(),0);
  assert(!await page.locator('[data-runtime-retry="test-b"], [data-build-repair="test-b"]').count(),"an image check received preparation retry/repair actions");
  const independentCheckLog=page.locator('[data-ob-focus="log-button:test-b"]');assert(await independentCheckLog.isVisible(),"an unmatched image check was hidden with its log");await independentCheckLog.click();await page.locator('[data-build-log="test-b"]').waitFor();assert((await page.locator('[data-build-log="test-b"]').innerText()).includes("test-b"));
  assert.equal(await card("source-b").getAttribute("data-preparation-state"),"repairing");assert(!writes.some(r=>/assistant-runs|runtime-images|\/build$|model-test/.test(r.path)),"reading preparation progress started execution");
  assert(await page.locator('[data-build-repair="source-a"]').isDisabled(),"active automatic repair must prevent another submission");
  runs=runs.map(r=>r.id!=="repair-a"?r:{...r,status:"failed",maintenance:{phase:"blocked",maxAttempts:3,attempts:[],stopReason:"需要继续处理"}});
  await page.waitForFunction(()=>document.querySelector('[data-build-id="source-a"]')?.dataset.preparationState==="failed");assert(!await card("source-a").locator(".ob-job-top > .ob-status").first().innerText().then(s=>s.includes("Kun 正在修复")),"submitted handoff concealed a known blocked repair");assert(await page.locator('[data-build-repair="source-a"]').isEnabled(),"terminal blocked repair must allow an explicit new attempt");
  await mode("pull").focus();await page.keyboard.press("ArrowRight");assert(await mode("build").isChecked());await page.locator("#onboarding-docker-version").waitFor();
  await mode("pull").check();await page.locator("#onboarding-runtime-reference").fill("registry.example/news:broken");await mode("local").check();await page.locator("#onboarding-runtime-reference").fill("local-news:ready");await mode("pull").check();assert.equal(await page.locator("#onboarding-runtime-reference").inputValue(),"registry.example/news:broken");
  for(const [width,height,label] of [[1440,1080,"desktop"],[800,1000,"tablet"],[390,844,"mobile"]]){
   await page.setViewportSize({width,height});await page.locator("#onboarding-docker-mode").scrollIntoViewIfNeeded();assert(await page.evaluate(()=>document.documentElement.scrollWidth<=document.documentElement.clientWidth),label+" horizontal overflow");assert(await page.locator("#setup-onboarding").evaluate(n=>n.scrollWidth<=n.clientWidth),label+" wizard overflow");await page.screenshot({path:path.join(out,"docker-methods-"+label+".png")});
   await card("source-b").scrollIntoViewIfNeeded();assert(await card("source-b").getByRole("link",{name:/修复|轨迹|记录/}).first().isVisible(),label+" repair link not reachable");
  }
  await page.setViewportSize({width:1440,height:1080});await page.locator('[data-build-repair="source-c"]').click();await card("source-c").getByText("fixture: response unknown",{exact:false}).waitFor();await page.locator('[data-build-repair="source-c"]').click();await card("source-c").locator('[data-repair-id="repair-c"]').waitFor();assert.equal(manual.length,2);assert.equal(manual[0].requestId,manual[1].requestId);assert(await page.locator('[data-build-repair="source-c"]').isDisabled());
  await card("source-b").getByRole("button",{name:"查看日志",exact:true}).click();await card("source-b").locator('[data-build-log="source-b"]').waitFor();
  const details=card("source-b").locator("details").first();if(await details.count())await details.evaluate(n=>n.open=true);await card("source-b").locator('[data-build-log="source-b"]').evaluate(n=>n.scrollTop=80);const readAt=buildReads;
  await page.waitForFunction(()=>document.querySelector('[data-build-log="source-b"]')?.textContent.includes("构建日志"));await page.waitForTimeout(2400);assert(buildReads>readAt,"background progress did not refresh");assert(await card("source-b").locator('[data-build-log="source-b"]').isVisible());assert.equal(await card("source-b").locator('[data-build-log="source-b"]').evaluate(n=>n.scrollTop),80,"poll lost log reading position");if(await details.count())assert(await details.evaluate(n=>n.open),"poll closed details");
  await page.locator("#onboarding-dismiss").click();await page.locator("#system-setup").click();await page.locator('[data-setup-step="docker"]').click();await card("source-c").locator('[data-repair-id="repair-c"]').waitFor();assert(await card("source-a").locator('[data-repair-id="repair-a"]').count());
  runs=runs.map(r=>r.id!=="repair-b"?r:{...r,maintenance:{phase:"verified",maxAttempts:3,verifiedBuildId:"candidate-b",attempts:[attempt(true)]}});builds=builds.map(v=>v.id!=="candidate-b"?v:{...v,status:"succeeded",imageId:"sha256:"+"f".repeat(64),contextSha256:"context-b"});
  await page.waitForFunction(()=>document.querySelector('[data-build-id="source-b"]')?.dataset.preparationState==="ready");assert((await card("source-b").innerText()).includes("原始构建失败：source-b"),"initial source failure evidence was hidden after repair");
  await mode("pull").check();await page.locator("#onboarding-runtime-reference").fill("registry.example/news:broken");await page.locator("#onboarding-prepare-image").click();await card("download-failed").waitFor();assert(!await card("download-failed").locator("[data-build-repair]").count());const before=writes.length;await page.locator('[data-runtime-retry="download-failed"]').click();assert(await mode("pull").isChecked());assert.equal(writes.length,before);assert(!writes.some(r=>r.path==="/setup/auto-repairs"&&r.method==="POST"));
  await card("source-b").locator("details").evaluateAll(nodes=>nodes.forEach(node=>node.open=false));
  for(const [width,height,label] of [[1440,1080,"desktop"],[390,844,"mobile"]]){await page.setViewportSize({width,height});await card("source-b").scrollIntoViewIfNeeded();await card("source-b").screenshot({path:path.join(out,"docker-repair-"+label+".png")});}
  assert.equal(await page.locator("#onboarding-build-history").evaluate(root=>{let count=0;const walker=document.createTreeWalker(root,NodeFilter.SHOW_TEXT);while(walker.nextNode())if(walker.currentNode.nodeValue==="null")count++;return count;}),0,"optional logs rendered null placeholders");
  assert.deepEqual(errors,[]);
  const result={passed:true,scope:"Actual isolated Go demo service and shipped assets, scoped preparation/repair API fixtures; no Docker, model or worker execution.",checks:["distinct method cards with keyboard-native radio navigation","selected form and per-method drafts","same-card auto/manual repair and candidate associations","no cross-source or image-test misassociation","ambiguous manual response retains request identity and disables duplicate repair","logs and disclosure persist through polling","repair history restores after wizard reopening","maintenance verification required for readiness","download retry never implicitly builds or registers repair","desktop tablet mobile geometry and repair entry visibility","progress inspection is read-only"],errors};fs.writeFileSync(path.join(out,"setup-docker-flow-browser.json"),JSON.stringify(result,null,2)+"\n");console.log(JSON.stringify(result));
 }finally{if(browser)await browser.close();if(server.pid&&server.exitCode===null)await new Promise(r=>{server.once("exit",r);server.kill("SIGTERM");});fs.rmSync(dir,{recursive:true,force:true});}
})().catch(e=>{console.error(e);process.exitCode=1;});
