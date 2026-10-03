// Real RunDesk persistence and Chromium UI. Speech and native share use explicit
// browser API stubs: this verifies control flow, not audible OS speech/sharing.
const {chromium}=require(process.env.PLAYWRIGHT_PATH||"playwright");
const {spawn}=require("node:child_process");
const fs=require("node:fs"),os=require("node:os"),path=require("node:path"),assert=require("node:assert/strict");
(async()=>{
 const temp=fs.mkdtempSync(path.join(os.tmpdir(),"rundesk-replies-"));
 const port=33000+Math.floor(Math.random()*12000),base=`http://127.0.0.1:${port}`,token="reply-test-token-123456789012";
 const server=spawn(path.resolve(process.env.RUNDESK_BINARY||"bin/rundesk"),["--demo","--data",temp,"--listen",`127.0.0.1:${port}`],{env:{...process.env,RUNDESK_TOKEN:token},stdio:["ignore","ignore","pipe"]});
 let browser,logs="";server.stderr.on("data",b=>logs+=b);
 const checks=[],ok=name=>checks.push(name);
 async function api(p,{method="GET",body,key}={}) {
  const r=await fetch(base+"/api/v1"+p,{method,headers:{Authorization:"Bearer "+token,"Content-Type":"application/json",...(key?{"Idempotency-Key":key}:{})},body:body===undefined?undefined:JSON.stringify(body)});
  const data=await r.json();if(!r.ok)throw Error(`${r.status}: ${JSON.stringify(data)}`);return data;
 }
 try {
  for(let n=0;n<100;n++){try{await api("/meta");break;}catch{}await new Promise(r=>setTimeout(r,100));}
  const work=(await api("/workspaces"))[0];
  const session=await api("/sessions",{method:"POST",key:"reply-session-test",body:{workspaceId:work.id,title:"新闻背景研究 · 交互演示"}});
  browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_PATH||undefined,args:["--no-sandbox","--disable-dev-shm-usage","--disable-gpu"]});
  const context=await browser.newContext({viewport:{width:1440,height:1000},permissions:["clipboard-read","clipboard-write"]});
  await context.addInitScript(()=>{
   window.speechCalls=[];
   Object.defineProperty(window,"speechSynthesis",{configurable:true,value:{
    cancel(){speechCalls.push("cancel");},pause(){speechCalls.push("pause");},resume(){speechCalls.push("resume");},
    speak(u){speechCalls.push("speak");window.testUtterance=u;setTimeout(()=>u.onstart?.(),0);}
   }});
   Object.defineProperty(window,"SpeechSynthesisUtterance",{configurable:true,value:class{constructor(text){this.text=text;}}});
   Object.defineProperty(navigator,"share",{configurable:true,value:async data=>{window.sharedReply=data;}});
   Object.defineProperty(navigator,"canShare",{configurable:true,value:()=>true});
  });
  const page=await context.newPage(),errors=[];page.on("pageerror",e=>errors.push(e.message));
  await page.goto(base);await page.locator("#token").fill(token);await page.locator("#login-form button").click();await page.locator("#login").waitFor({state:"hidden"});
  await page.getByRole("button",{name:"新闻背景研究 · 交互演示",exact:true}).click();
  await page.locator("#prompt").fill("检查新闻研究流程，演示工具轨迹。");await page.locator("#send").click();
  await page.locator(".assistant .reply-actions").waitFor();await page.waitForFunction(()=>document.querySelector("#run-status").textContent==="已完成");
  const answer=page.locator(".assistant").last();
  const eventId=Number(await answer.getAttribute("data-event-id"));assert(eventId>0);
  const original=await api(`/sessions/${session.id}/messages/${eventId}`);
  await answer.getByRole("button",{name:"复制回复",exact:true}).click();assert.equal(await page.evaluate(()=>navigator.clipboard.readText()),original.text);ok("copy_exact_reply_markdown");
  // Code fence copy preserves literal markup, never executes HTML.
  await page.evaluate(()=>{window.codeFixture='echo "<script>window.injected=true</script>"\n';const n=markdown('```sh\n'+codeFixture+'```');n.id="code-copy-test";document.querySelector(".assistant .body").append(n);});
  await page.locator("#code-copy-test").getByRole("button",{name:"复制代码"}).click();assert.equal(await page.evaluate(()=>navigator.clipboard.readText()),await page.evaluate(()=>codeFixture));assert.equal(await page.evaluate(()=>window.injected),undefined);await page.locator("#code-copy-test").evaluate(n=>n.remove());ok("copy_code_literal_and_html_safe");
  await answer.getByRole("button",{name:"有帮助",exact:true}).click();await answer.getByRole("button",{name:"取消有帮助评价",exact:true}).waitFor();
  assert.equal((await api(`/sessions/${session.id}/feedback`))[0].rating,"up");
  await page.reload();await page.getByRole("button",{name:"取消有帮助评价",exact:true}).waitFor();ok("feedback_persists_after_reload");
  await answer.getByRole("button",{name:"有待改进",exact:true}).click();await page.getByRole("textbox",{name:"评价说明"}).fill("需要补充事件背景和来源。");await page.getByRole("button",{name:"保存评价",exact:true}).click();await answer.getByRole("button",{name:"取消有待改进评价",exact:true}).waitFor();
  assert.equal((await api(`/sessions/${session.id}/feedback`))[0].comment,"需要补充事件背景和来源。");ok("downvote_reason_saved");
  await page.reload();await answer.getByRole("button",{name:"查看评价说明",exact:true}).click();assert.equal(await page.getByRole("textbox",{name:"评价说明"}).inputValue(),"需要补充事件背景和来源。");await page.getByRole("button",{name:"取消",exact:true}).click();ok("saved_feedback_reason_can_be_reviewed");
  await answer.getByRole("button",{name:"取消有待改进评价",exact:true}).click();await answer.getByRole("button",{name:"有待改进",exact:true}).waitFor();assert.equal((await api(`/sessions/${session.id}/feedback`))[0].rating,"none");ok("feedback_toggle_clears_comment");
  await page.route("**/messages/*/feedback",route=>route.abort("failed"));await answer.getByRole("button",{name:"有帮助",exact:true}).click();await page.waitForFunction(()=>!document.querySelector('[data-rating="up"]').disabled);assert.equal(await answer.locator('[data-rating="up"]').getAttribute("aria-pressed"),"false");await page.unroute("**/messages/*/feedback");ok("failed_save_does_not_fake_success");
  await answer.getByRole("button",{name:"分享回复",exact:true}).click();assert.equal(await page.locator(".share-preview").innerText(),await answer.locator(":scope > .body").innerText());
  const download=page.waitForEvent("download");await page.getByRole("button",{name:"下载 Markdown",exact:true}).click();const file=await download;assert.equal(fs.readFileSync(await file.path(),"utf8"),original.text);ok("preview_and_download_selected_reply_only");
  await page.getByRole("button",{name:"系统分享",exact:true}).click();assert.deepEqual(await page.evaluate(()=>window.sharedReply),{title:"RunDesk 回复",text:original.text});await page.locator(".reply-dialog").getByRole("button",{name:"关闭",exact:true}).click();ok("native_share_receives_only_preview_text_stub");
  await answer.getByRole("button",{name:"朗读回复",exact:true}).click();await page.getByText("正在朗读",{exact:true}).waitFor();
  await answer.getByRole("button",{name:"暂停朗读",exact:true}).click();await page.getByText("已暂停",{exact:true}).waitFor();
  await answer.getByRole("button",{name:"继续朗读",exact:true}).click();await answer.getByRole("button",{name:"停止朗读",exact:true}).click();assert(await page.evaluate(()=>speechCalls.includes("pause")&&speechCalls.includes("resume")&&speechCalls.includes("speak")));ok("speech_start_pause_resume_stop_stub");
  await answer.getByRole("button",{name:"朗读回复",exact:true}).click();await page.evaluate(()=>window.testUtterance.onerror({error:"voice-unavailable"}));await page.locator("#toast").filter({hasText:"浏览器未能朗读"}).waitFor();assert.equal(await answer.getByRole("button",{name:"停止朗读",exact:true}).count(),0);ok("speech_error_restores_idle");
  await answer.getByRole("button",{name:"朗读回复",exact:true}).click();const finished=await page.evaluate(()=>{let count=0;while(speechReply.node&&count<30){window.testUtterance.onend();count++;}return {count,idle:speechReply.node===null};});assert(finished.count>1&&finished.idle);ok("long_speech_advances_chunks_and_finishes_stub");
  await page.evaluate(()=>{window.savedClipboard=navigator.clipboard.writeText.bind(navigator.clipboard);navigator.clipboard.writeText=async()=>{throw Error("denied");};});await answer.getByRole("button",{name:"复制回复",exact:true}).click();assert.equal(await page.getByRole("textbox",{name:"待复制的内容"}).inputValue(),original.text);await page.locator(".reply-dialog").getByRole("button",{name:"关闭",exact:true}).click();await page.evaluate(()=>navigator.clipboard.writeText=window.savedClipboard);ok("clipboard_denied_manual_copy_fallback");
  const summary=page.locator(".tool > summary").first();assert.equal(await summary.evaluate(n=>getComputedStyle(n).listStyleType),"none");assert(await summary.locator("svg").count());assert(!/推理摘要|命令执行|MCP 工具/.test(await summary.innerText()));await summary.focus();await page.keyboard.press("Enter");assert.equal(await summary.evaluate(n=>n.parentElement.open),true);ok("icon_tools_keep_keyboard_disclosure");
  const output=path.resolve("docs/screenshots/0.7.0/replies");fs.mkdirSync(output,{recursive:true});await summary.press("Enter");await page.locator("#toast").evaluate(n=>n.classList.add("hidden"));await page.locator("#prompt").focus();await page.locator("#messages").evaluate(n=>n.scrollTop=n.scrollHeight);await page.screenshot({path:path.join(output,"conversation-desktop.png"),fullPage:true});
  await answer.getByRole("button",{name:"分享回复",exact:true}).click();await page.screenshot({path:path.join(output,"reply-share.png"),fullPage:true});await page.locator(".reply-dialog").getByRole("button",{name:"关闭",exact:true}).click();
  await page.setViewportSize({width:390,height:844});await page.locator("#messages").evaluate(n=>n.scrollTop=n.scrollHeight);await page.screenshot({path:path.join(output,"conversation-mobile.png"),fullPage:true});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  await page.getByRole("button",{name:"会话列表",exact:true}).click();await page.locator("#session-list").waitFor({state:"visible"});assert.equal(await page.evaluate(()=>document.querySelector("main").inert),true);await page.screenshot({path:path.join(output,"history-mobile.png"),fullPage:true});await page.keyboard.press("Escape");assert.equal(await page.evaluate(()=>document.body.classList.contains("sidebar-open")),false);ok("mobile_history_drawer_and_escape");
  await answer.getByRole("button",{name:"朗读回复",exact:true}).click();
  await page.getByRole("button",{name:"会话列表",exact:true}).click();await page.locator("#new-session").click();await page.waitForFunction(()=>state.session===null);assert.equal(await page.evaluate(()=>speechReply.node),null);assert.equal(await page.evaluate(()=>document.body.classList.contains("sidebar-open")),false);ok("switch_session_stops_speech_and_closes_drawer");
  await page.setViewportSize({width:1440,height:1000});await page.getByRole("button",{name:"新闻背景研究 · 交互演示",exact:true}).click();await answer.locator(".reply-actions").waitFor();await page.evaluate(()=>{Object.defineProperty(window,"speechSynthesis",{configurable:true,value:undefined});refreshReplyActions();});assert.equal(await answer.getByRole("button",{name:"此浏览器不支持朗读"}).isDisabled(),true);ok("unsupported_speech_disabled");
  assert.deepEqual(errors,[]);ok("no_javascript_exceptions");
  const report={passed:checks.length,checks,browser:await browser.version(),fixture:"Codex protocol simulator; OS speech and native share stubbed"};fs.writeFileSync(path.join(output,"reply-actions-report.json"),JSON.stringify(report,null,2)+"\n");console.log(JSON.stringify(report));
 }catch(e){console.error(logs);if(browser){for(const c of browser.contexts())for(const p of c.pages())console.error(await p.locator("body").innerText());}throw e;}finally{if(browser)await browser.close();server.kill("SIGTERM");await new Promise(r=>{server.once("exit",r);setTimeout(r,4000)});fs.rmSync(temp,{recursive:true,force:true});}
})().catch(e=>{console.error(e);process.exitCode=1});
