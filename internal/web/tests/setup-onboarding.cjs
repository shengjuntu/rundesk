/* Exercise the shipped initialization UI with service responses. No real
 * model, package installation or Docker daemon is used by this test. */
"use strict";
const assert=require("node:assert/strict"),fs=require("node:fs"),path=require("node:path"),vm=require("node:vm");
const {JSDOM}=require("jsdom"),{webcrypto}=require("node:crypto"),{TextEncoder}=require("node:util");
const root=path.resolve(__dirname,".."),copy=value=>JSON.parse(JSON.stringify(value));
const dom=new JSDOM(fs.readFileSync(path.join(root,"index.html"),"utf8"),{url:"http://localhost/",runScripts:"outside-only"}),w=dom.window,ctx=dom.getInternalVMContext();
Object.defineProperty(w,"crypto",{value:webcrypto});w.TextEncoder=TextEncoder;
w.rdText=value=>value;w.rdFormat=(value,...args)=>value.replace(/\$\{(\d+)\}/g,(_,n)=>args[+n]);
const timers=new Map();let timer=0;w.setTimeout=(fn,delay)=>{timers.set(++timer,{fn,delay});return timer;};w.clearTimeout=id=>timers.delete(id);w.setInterval=()=>0;
const app=fs.readFileSync(path.join(root,"app.js"),"utf8");vm.runInContext(app.slice(0,app.indexOf("async function boot()")),ctx);
const realAPI=w.api;
const state=vm.runInContext("state",ctx),$=selector=>w.document.querySelector(selector),$$=selector=>[...w.document.querySelectorAll(selector)];
const setup={settings:{completed:false,revision:0,workspaceId:"work"},codex:"codex",instance:{id:"default",revision:1,agentRuntime:{kind:"codex"}},firstRun:true};
state.instances=[copy(setup.instance)];state.workspaces=[{id:"work",name:"工作区",path:"/work"}];$("#workspace").append(new w.Option("工作区","work"));$("#instance").append(new w.Option("Codex","default"));
const sources={schemaVersion:1,revision:"catalog-1",defaultGroup:"china",types:[{id:"npm",name:"npm",fields:[{id:"registry",label:"npm registry",argument:"RD_NPM_REGISTRY"}]}],groups:[{id:"international",name:"国际源",sources:[{id:"npm-us",type:"npm",name:"npm official",values:{registry:"https://registry.npmjs.org/"}}]},{id:"china",name:"中国镜像",sources:[{id:"npm-cn",type:"npm",name:"npm mirror",values:{registry:"https://registry.npmmirror.com/"}}]}]};
const recipes=["news2douyin","people-search"].map(id=>({id:"builtin-"+id,name:id,profileId:id,kind:"builtin",revision:1,recipeSha:"sha-"+id,version:"1.1.2",description:"可复用工具环境"}));
let modelTools=false,dockerOK=false,failInstallResponse=true,failAutoResponse=true;
const calls=[],db={kun:{configured:false,available:false,ready:false,revision:0,kunPath:"/tools/kun"},installs:[{id:"history-old",status:"failed",input:{version:"0.157.1"},result:{ok:false,error:"historical failure"}}],repairs:[],builds:[],experiences:[],autoRows:[]};
db.experiences=[{id:"verified-case",kind:"image-build",status:"verified",summary:"agent-reach 打包修复",changes:"移除重复 force-include，并内置 vendor/agent-reach。",scope:{os:"linux",arch:"amd64",version:"cf70302e"},failure:{id:"failed-build",error:"Duplicate wheel archive path"},success:{id:"success-build",status:"succeeded"},validation:{installVersionVerified:false,offline:true,smoke:true},patch:{available:true,changedFiles:2,files:[{path:"Dockerfile",change:"modified"},{path:"vendor/agent-reach/pyproject.toml",change:"modified"}],dockerfileDiff:"+COPY vendor/ /opt/vendor/\n+ENV API_KEY=must-hide-this\n"+"x".repeat(13000)}},{id:"candidate-case",kind:"codex-install",status:"candidate",summary:"下载通道候选修复",changes:"更换 npm 镜像，待版本核验。",scope:{os:"linux",arch:"amd64",version:"0.157.1"},failure:{id:"failed-install",error:"timeout"},success:{id:"candidate-install"},validation:{installVersionVerified:false,offline:false,smoke:false}}];
const fail=(message,status=500)=>Object.assign(Error(message),{status});
w.api=async(route,options={})=>{
  const method=options.method||"GET",body=options.body;calls.push({route,method,body:body&&copy(body)});
  if(route==="/setup")return copy(setup);
  if(method==="GET"){
    if(route==="/setup/kun")return copy(db.kun);
    if(route==="/setup/codex-installs")return {items:copy(db.installs)};
    if(route==="/setup/repairs")return {items:copy(db.repairs)};
    if(route==="/setup/experiences")return {items:copy(db.experiences)};
    if(route==="/setup/auto-repairs")return {items:copy(db.autoRows)};
    if(route==="/build-center/sources")return copy(sources);
    if(route==="/build-center/recipes")return {items:copy(recipes)};
    if(route==="/build-center/builds")return {items:copy(db.builds)};
    if(/\/build-center\/recipes\/[^/]+\/sources/.test(route))return {types:[{type:"npm",detected:true,supported:true,arguments:["RD_NPM_REGISTRY"]}]};
    if(route.startsWith("/build-center/builds/")&&route.includes("/log?"))return {text:"error: upstream build failed\n",next:29,status:"failed"};
    if(route.startsWith("/build-center/assistant-runs/"))return copy(db.repairs.find(value=>route.endsWith(value.id)));
  }
  if(route==="/setup/discovery")return {scannedAt:"2026-10-27T00:00:00Z",platform:{os:"linux",arch:"amd64",cpuCount:16,memoryBytes:68719476736},gpus:[{vendor:"NVIDIA",name:"A100",memoryMiB:40960,driver:"550"}],tools:[{id:"kun",path:"/tools/kun",status:"ready",version:"kun 0.17.0"},{id:"codex",path:"codex",status:"unavailable"},{id:"docker",path:"/usr/bin/docker",status:"ready",version:"Docker 28"}],services:[{endpoint:"http://127.0.0.1:8000/v1",source:"loopback",status:"listed",authRequired:false,inferenceTested:false,models:[{id:"Qwen3-Coder"}]}],warnings:[]};
  if(route==="/setup/check")return {ok:dockerOK,error:dockerOK?"":"Docker daemon unavailable"};
  if(method==="POST"&&route==="/setup/model-test")return {endpoint:body.endpoint,model:body.model,inferenceOK:true,toolsOK:modelTools,status:modelTools?"ready":"inference_only",error:modelTools?"":"No tool call returned"};
  if(method==="POST"&&route==="/setup/kun"){
    assert(!body.agentRuntime&&!body.instanceId,"setup must not overwrite the default instance");
    db.kun={...db.kun,...body,configured:true,available:true,ready:modelTools,instanceId:"setup-kun",workspaceId:"kun-work",revision:db.kun.revision+1};return copy(db.kun);
  }
  if(method==="POST"&&route==="/setup/codex-installs"){
    assert.match(body.requestId,/^[a-f0-9]{24}$/);assert.equal(body.method,"auto");assert.equal(body.registry,"https://registry.npmmirror.com/");
    let job=db.installs.find(value=>value.input.requestId===body.requestId);
    if(!job){job={id:"install-1",status:"failed",input:copy(body),result:{version:body.version,ok:false,error:"download timeout",log:"download log"}};db.installs.push(job);}
    if(failInstallResponse){failInstallResponse=false;throw fail("response lost");}return copy(job);
  }
  if(method==="POST"&&route==="/setup/repairs"){
    assert.equal(body.installId,"history-old");assert.match(body.requestId,/^[a-f0-9]{24}$/);
    const value={id:"repair-1",sourceInstallId:body.installId,status:"queued",sessionId:"repair-session",taskId:"repair-task"};db.repairs=[value];return copy(value);
  }
  if(method==="POST"&&route==="/setup/auto-repairs"){
    assert.match(body.requestId,/^[a-f0-9]{24}$/);assert.notEqual(body.sourceId,"history-old","historical failures must never be enrolled automatically");
    let value=db.autoRows.find(row=>row.kind===body.kind&&row.sourceId===body.sourceId);
    if(!value){value={id:"auto-"+body.sourceId,kind:body.kind,sourceId:body.sourceId,enabled:true,status:"submitted",repairId:"auto-task-"+body.sourceId,sessionId:"auto-session-"+body.sourceId};db.autoRows.push(value);}
    if(failAutoResponse){failAutoResponse=false;throw fail("handoff response lost");}return copy(value);
  }
  if(method==="POST"&&route.startsWith("/build-center/recipes/")&&route.endsWith("/build")){
    assert.equal(body.buildSources.group,"china");assert.equal(body.buildSources.overrides.npm.registry,route.includes("news2douyin")?"https://company.example/npm/":"https://registry.npmmirror.com/");
    const value={id:"build-"+(db.builds.length+1),name:route.includes("news2douyin")?"news2douyin":"people-search",status:"failed",error:"compile failed",recipeId:route.split("/")[3],requestId:body.requestId};db.builds.unshift(value);return copy(value);
  }
  if(method==="POST"&&route==="/build-center/assistant-runs"){
    assert.equal(body.instanceId,"setup-kun");assert(body.sourceBuildId);const value={id:"build-repair",sourceBuildId:body.sourceBuildId,status:"queued",sessionId:"build-repair-session"};db.repairs.push(value);return copy(value);
  }
  if(method==="POST"&&route==="/setup/complete"){assert.equal(body.revision,0);assert.equal(body.completed,true);setup.settings.completed=true;setup.settings.revision++;return {settings:copy(setup.settings)};}
  throw Error("Unexpected request "+method+" "+route);
};
w.setProductPage=()=>{};w.stashProductDraft=()=>{};
for(const file of ["build-center.js","setup-onboarding.js","setup.js"])vm.runInContext(fs.readFileSync(path.join(root,file),"utf8"),ctx);
const until=async fn=>{for(let n=0;n<300;n++){if(fn())return;await new Promise(resolve=>setImmediate(resolve));}assert(fn(),"UI did not settle");};
const click=async selector=>{$(selector).click();await new Promise(resolve=>setImmediate(resolve));};
const nav=step=>click('[data-setup-step="'+step+'"]');
(async()=>{
  // Exercise the real shipped api() rather than the UI fixture. Every new
  // initialization POST needs the server's persistent idempotency header.
  const realRequests=[];let lose=true;
  w.RunDeskDiagnostics={request:async(url,options)=>{realRequests.push({url,options});assert.match(options.headers["Idempotency-Key"],/^[a-f0-9]{32}$/);assert.equal(typeof options.body,"string");if(url.endsWith("/setup/kun")&&lose){lose=false;throw fail("network result unknown");}return {ok:true};}};
  for(const endpoint of ["codex-install","codex-installs","runtime-images","kun","repairs","auto-repairs","complete","model-test","experiences","discovery"]){
    const route="/setup/"+endpoint,payload={probe:endpoint};
    if(endpoint==="kun")await assert.rejects(realAPI(route,{method:"POST",body:payload}),/network result unknown/);
    await realAPI(route,{method:"POST",body:payload});
  }
  const retryHeaders=realRequests.filter(row=>row.url.endsWith("/setup/kun")).map(row=>row.options.headers["Idempotency-Key"]);assert.equal(retryHeaders.length,2);assert.equal(retryHeaders[0],retryHeaders[1],"the real api() must preserve its header after transport failure");
  const pending=w.RunDeskOnboarding.open({initial:true,setup});await until(()=>!!$('[data-service-endpoint]'));
  assert(w.document.body.classList.contains("rd-onboarding"));assert($('[data-tool="codex"]').textContent.includes("未发现"));
  assert(calls.some(row=>row.route==="/setup/discovery"));assert(!calls.some(row=>row.route==="/setup/model-test"),"discovery must not infer automatically");
  assert.equal(calls.filter(row=>row.route==="/setup/codex-installs"&&row.method==="POST").length,0);
  assert.equal($$("[data-preset-id]:checked").length,0,"presets must be opt in");
  await click('[data-service-endpoint]');assert.equal($("#onboarding-endpoint").value,"http://127.0.0.1:8000/v1");assert.equal($("#onboarding-model").value,"Qwen3-Coder");
  await click("#onboarding-enable-kun");await until(()=>$("#onboarding-error").textContent.includes("No tool call"));
  assert(!db.kun.ready,"inference alone must not enable repairs");
  modelTools=true;await click("#onboarding-enable-kun");await until(()=>db.kun.ready);assert.equal(w.RunDeskOnboarding.preferredInstance,"setup-kun");
  await nav("codex");assert.equal($("#onboarding-install-group").value,"china");assert($("#onboarding-install-auto-repair").checked);assert(!$("#onboarding-install-auto-repair").disabled);
  await click("#onboarding-install-codex");await until(()=>$("#onboarding-error").textContent.includes("response lost"));
  await click("#onboarding-install-codex");await until(()=>!!$('[data-install-id="install-1"]'));
  const installs=calls.filter(row=>row.route==="/setup/codex-installs"&&row.method==="POST");assert.equal(installs.length,2);assert.equal(installs[0].body.requestId,installs[1].body.requestId,"lost response must preserve the exact request identity");assert.equal(db.installs.length,2);
  await until(()=>$$("#onboarding-repairs button").some(value=>value.textContent==="重试注册自动修复"));$$("#onboarding-repairs button").find(value=>value.textContent==="重试注册自动修复").click();await until(()=>!!$('[data-auto-repair="auto-install-1"]'));
  const registrations=calls.filter(row=>row.route==="/setup/auto-repairs"&&row.method==="POST");assert.equal(registrations.length,2);assert.equal(registrations[0].body.requestId,registrations[1].body.requestId);assert.equal(db.autoRows.length,1);assert($('[data-install-repair="install-1"]').disabled,"manual duplicate repair must be disabled after handoff");
  await click('[data-install-repair="history-old"]');await until(()=>!!$('a[href*="repair-session"]'));assert($('a[href*="repair-session"]').target==="_blank");
  await nav("docker");await until(()=>$$("[data-preset-id]").length===2);assert($("#onboarding-build-selected").disabled,"Docker unavailable must disable builds");
  dockerOK=true;await click("#onboarding-check-docker");await until(()=>!$("#onboarding-build-selected").disabled);
  const first=$('[data-preset-id="builtin-news2douyin"]');first.checked=true;first.dispatchEvent(new w.Event("change"));
  let custom=$('[data-ob-source-recipe="builtin-news2douyin"] [data-source-field="registry"]');custom.value="https://company.example/npm/";custom.dispatchEvent(new w.Event("input"));
  const second=$('[data-preset-id="builtin-people-search"]');second.checked=true;second.dispatchEvent(new w.Event("change"));
  assert.equal($('[data-ob-source-recipe="builtin-news2douyin"] [data-source-field="registry"]').value,"https://company.example/npm/","selecting another preset must preserve edited sources");
  await nav("kun");await nav("docker");assert.equal($('[data-ob-source-recipe="builtin-news2douyin"] [data-source-field="registry"]').value,"https://company.example/npm/","navigation must preserve edited sources");
  const ids=$$("#onboarding-preset-sources [id]").map(value=>value.id);assert.equal(ids.length,new Set(ids).size,"multiple source forms must not duplicate IDs");
  await click("#onboarding-build-selected");await until(()=>$$('[data-build-id]').length===2);assert.equal(db.builds.length,2);
  await until(()=>!!$('a[href*="auto-session-build-1"]'));assert($('[data-build-repair="build-1"]').disabled);assert.equal(db.autoRows.length,3,"each newly submitted source gets one backend authorization");
  const viewLog=$$('[data-build-id="build-1"] button').find(value=>value.textContent==="查看日志");viewLog.click();await until(()=>!!$('[data-build-log="build-1"]'));assert($('[data-build-log="build-1"]').textContent.includes("upstream"));
  await nav("finish");
  const verified=$('[data-experience-details="verified-case"]'),candidate=$('[data-experience-id="candidate-case"]');
  assert(!verified.open,"experience evidence should be collapsed by default");verified.querySelector("summary").click();assert(verified.open);
  assert(verified.textContent.includes("Duplicate wheel archive path"));assert(verified.textContent.includes("移除重复 force-include"));assert(verified.textContent.includes("vendor/agent-reach/pyproject.toml"));assert(verified.textContent.includes("已通过"));
  const diff=$('[data-experience-diff="verified-case"]').textContent;assert(!diff.includes("must-hide-this"));assert(diff.includes("[redacted]"));assert(diff.length<12100);assert(diff.includes("展示内容已截断"));
  assert(verified.querySelector('a[href="/api/v1/build-center/builds/failed-build/log?after=0"]'));
  assert(candidate.textContent.includes("候选经验"));assert(candidate.textContent.includes("未验证"));assert(candidate.textContent.includes("尚未通过所需的完整验证"));assert(!candidate.querySelector('[data-state="ready"]'),"candidate must not be presented as passed");
  await click("#onboarding-complete");assert.equal(await pending,true);assert(!$('[id="setup-onboarding"]'));assert(setup.settings.completed);
  // A completed setup is not reopened automatically; demo and explicit trace
  // links are also not captured by initialization.
  await w.RunDeskSetup.maybeOpen();assert(!$("#setup-onboarding"));
  setup.settings.completed=false;state.demo=true;await w.RunDeskSetup.maybeOpen();assert(!$("#setup-onboarding"));state.demo=false;
  w.history.replaceState(null,"","/?session=repair-session");await w.RunDeskSetup.maybeOpen();assert(!$("#setup-onboarding"));
  console.log(JSON.stringify({passed:true,checks:["real api() supplies all initialization idempotency headers","real api() preserves header after transport failure","initial full-page setup","discovery does not infer or install","inference and tools both required","task-scoped Kun configuration","native installation defaults and mirror selection","lost-response receipt reuse","backend automatic handoff with bounded task scope","automatic registration receipt retry","historical failures remain opt in","manual duplicate repair prevented","Docker availability gating","multiple opt-in preset builds","source edits survive preset selection and navigation","isolated editable source forms","Kun build repair","build log inspection","collapsed repair evidence exposes issues, changes and validation","bounded redacted Dockerfile diff and source record links","candidate experience remains unverified","optional components and completion","completed/demo/deep-link entry"]}));
})().catch(error=>{console.error(error);process.exitCode=1;});
