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
let modelTools=false,dockerOK=false,failInstallResponse=true,failAutoResponse=true,failRuntimeResponse=true;
const publicImages={schemaVersion:1,revision:"public-1",items:[]};
const runtimeProfiles={platform:"linux/amd64",supported:true,items:recipes.map(value=>({id:value.profileId,name:value.name}))};
const calls=[],db={kun:{configured:false,available:false,ready:false,revision:0,kunPath:"/tools/kun"},installs:[{id:"history-old",status:"failed",input:{version:"0.157.1"},result:{ok:false,error:"historical failure"}}],repairs:[],builds:[],experiences:[],autoRows:[]};
db.experiences=[{id:"verified-case",kind:"image-build",status:"verified",summary:"agent-reach 打包修复",changes:"移除重复 force-include，并内置 vendor/agent-reach。",scope:{os:"linux",arch:"amd64",version:"cf70302e"},failure:{id:"failed-build",error:"Duplicate wheel archive path"},success:{id:"success-build",status:"succeeded"},validation:{installVersionVerified:false,offline:true,smoke:true},patch:{available:true,changedFiles:2,files:[{path:"Dockerfile",change:"modified"},{path:"vendor/agent-reach/pyproject.toml",change:"modified"}],dockerfileDiff:"+COPY vendor/ /opt/vendor/\n+ENV API_KEY=must-hide-this\n"+"x".repeat(13000)}},{id:"candidate-case",kind:"codex-install",status:"candidate",summary:"下载通道候选修复",changes:"更换 npm 镜像，待版本核验。",scope:{os:"linux",arch:"amd64",version:"0.157.1"},failure:{id:"failed-install",error:"timeout"},success:{id:"candidate-install"},validation:{installVersionVerified:false,offline:false,smoke:false}}];
const fail=(message,status=500)=>Object.assign(Error(message),{status});
w.api=async(route,options={})=>{
  const method=options.method||"GET",body=options.body;calls.push({route,method,body:body&&copy(body)});
  if(route==="/setup")return copy(setup);
  if(method==="GET"){
    if(route==="/setup/kun")return copy(db.kun);
    if(route==="/setup/public-images")return copy(publicImages);
    if(route==="/setup/runtime-profiles")return copy(runtimeProfiles);
    if(route==="/build-center/images")return {items:db.builds.filter(value=>value.imageId).map(value=>({key:"image-"+value.id,imageId:value.imageId}))};
    if(route==="/setup/codex-installs")return {items:copy(db.installs)};
    if(route==="/setup/repairs")return {items:copy(db.repairs)};
    if(route==="/setup/experiences")return {items:copy(db.experiences)};
    if(route==="/setup/auto-repairs")return {items:copy(db.autoRows)};
    if(route.startsWith("/build-center/runtime-options?"))return {platform:"linux/amd64",version:"0.157.1",nativeAvailable:true,reason:"verified fixture package"};
    if(route==="/build-center/sources")return copy(sources);
    if(route==="/build-center/recipes")return {items:copy(recipes)};
    if(route==="/build-center/builds")return {items:copy(db.builds)};
    if(/\/build-center\/recipes\/[^/]+\/sources/.test(route))return {types:[{type:"npm",detected:true,supported:true,arguments:["RD_NPM_REGISTRY"]}]};
    if(route.startsWith("/build-center/builds/")&&route.includes("/log?"))return {text:"error: upstream build failed\n",next:29,status:"failed"};
    if(route.startsWith("/build-center/assistant-runs/"))return copy(db.repairs.find(value=>route.endsWith(value.id)));
  }
  if(route==="/setup/discovery")return {scannedAt:"2026-10-27T00:00:00Z",platform:{os:"linux",arch:"amd64",cpuCount:16,memoryBytes:68719476736},gpus:[{vendor:"NVIDIA",name:"A100",memoryMiB:40960,driver:"550"}],tools:[{id:"kun",path:"/tools/kun",status:"ready",version:"kun 0.17.0"},{id:"codex",path:"codex",status:"unavailable"},{id:"docker",path:"/usr/bin/docker",status:"ready",version:"Docker 28"}],services:[{endpoint:"http://127.0.0.1:8000/v1",source:"loopback",status:"listed",authRequired:false,inferenceTested:false,models:[{id:"Qwen3-Coder"}]}],warnings:[]};
  if(route==="/setup/check")return {ok:dockerOK,error:dockerOK?"":"Docker daemon unavailable"};
  if(method==="POST"&&route==="/setup/runtime-images"){
    assert.match(body.requestId,/^[a-f0-9]{24}$/);assert(["pull","local"].includes(body.source));assert(!body.codexRuntime,"runtime installation settings must not leak into image acquisition");
    let job=db.builds.find(value=>value.requestId===body.requestId);
    if(!job){const failed=body.reference.endsWith(":broken");job={...copy(body),id:"runtime-"+(db.builds.filter(value=>value.scope==="setup").length+1),name:body.profileId+" · "+body.source,scope:"setup",status:failed?"failed":"succeeded",offlineReady:!failed,...(failed?{error:"registry connection failed"}:{imageId:"sha256:"+(db.builds.filter(value=>value.scope==="setup").length+1).toString(16).repeat(64)})};db.builds.unshift(job);}
    if(failRuntimeResponse){failRuntimeResponse=false;throw fail("pull response lost");}return copy(job);
  }
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
    assert.deepEqual(copy(body.codexRuntime),{mode:"base",reference:"rundesk-codex:0.157.1"});
    assert.equal(body.buildSources.group,"china");assert.equal(body.buildSources.overrides.npm.registry,route.includes("news2douyin")?"https://company.example/npm/":"https://registry.npmmirror.com/");
    const value={id:"build-"+(db.builds.filter(value=>!value.scope).length+1),name:route.includes("news2douyin")?"news2douyin":"people-search",status:"failed",error:"compile failed",recipeId:route.split("/")[3],requestId:body.requestId};db.builds.unshift(value);return copy(value);
  }
  if(method==="POST"&&route==="/build-center/assistant-runs"){
    assert.equal(body.instanceId,"setup-kun");assert(body.sourceBuildId);const value={id:"build-repair",sourceBuildId:body.sourceBuildId,status:"queued",sessionId:"build-repair-session"};db.repairs.push(value);return copy(value);
  }
  if(method==="POST"&&route==="/setup/complete"){assert.equal(body.revision,setup.settings.revision);assert.equal(body.completed,true);setup.settings.completed=true;setup.settings.revision++;return {settings:copy(setup.settings)};}
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
  assert(w.document.body.classList.contains("rd-onboarding"));
  assert.deepEqual($$("[data-setup-step]").map(node=>node.dataset.setupStep),["llm","codex","docker"],"startup must have exactly the three requested steps");
  assert.equal($('[aria-current="step"]').dataset.setupStep,"llm");
  assert($("#onboarding-endpoint"),"LLM discovery and configuration must share the first step");
  assert(!$("#onboarding-complete"),"completion belongs to the final Docker step");
  assert($('[data-tool="codex"]').textContent.includes("未发现"));
  assert(calls.some(row=>row.route==="/setup/discovery"));assert(!calls.some(row=>row.route==="/setup/model-test"),"discovery must not infer automatically");
  assert.equal(calls.filter(row=>row.route==="/setup/codex-installs"&&row.method==="POST").length,0);
  assert.equal($$("[data-preset-id]:checked").length,0,"presets must be opt in");
  await click('[data-service-endpoint]');
  assert.equal($('[aria-current="step"]').dataset.setupStep,"llm","selecting a discovered service must remain in the first step");
  assert.equal($("#onboarding-endpoint").value,"http://127.0.0.1:8000/v1");assert.equal($("#onboarding-model").value,"Qwen3-Coder");
  await click("#onboarding-enable-kun");await until(()=>$("#onboarding-error").textContent.includes("No tool call"));
  assert(!db.kun.ready,"inference alone must not enable repairs");
  modelTools=true;await click("#onboarding-enable-kun");await until(()=>db.kun.ready);assert.equal(w.RunDeskOnboarding.preferredInstance,"setup-kun");
  await click("[data-ob-next]");assert.equal($('[aria-current="step"]').dataset.setupStep,"codex","the next action must go directly from LLM to Codex");assert.equal($("#onboarding-install-group").value,"china");assert($("#onboarding-install-auto-repair").checked);assert(!$("#onboarding-install-auto-repair").disabled);
  await click("#onboarding-install-codex");await until(()=>$("#onboarding-error").textContent.includes("response lost"));
  await click("#onboarding-install-codex");await until(()=>!!$('[data-install-id="install-1"]'));
  const installs=calls.filter(row=>row.route==="/setup/codex-installs"&&row.method==="POST");assert.equal(installs.length,2);assert.equal(installs[0].body.requestId,installs[1].body.requestId,"lost response must preserve the exact request identity");assert.equal(db.installs.length,2);
  await until(()=>$$("#onboarding-repairs button").some(value=>value.textContent==="重试注册自动修复"));$$("#onboarding-repairs button").find(value=>value.textContent==="重试注册自动修复").click();await until(()=>!!$('[data-auto-repair="auto-install-1"]'));
  const registrations=calls.filter(row=>row.route==="/setup/auto-repairs"&&row.method==="POST");assert.equal(registrations.length,2);assert.equal(registrations[0].body.requestId,registrations[1].body.requestId);assert.equal(db.autoRows.length,1);assert($('[data-install-repair="install-1"]').disabled,"manual duplicate repair must be disabled after handoff");
  await click('[data-install-repair="history-old"]');await until(()=>!!$('a[href*="repair-session"]'));assert($('a[href*="repair-session"]').target==="_blank");
  await click("[data-ob-next]");assert.equal($('[aria-current="step"]').dataset.setupStep,"docker","the next action must go directly from Codex to Docker");
  assert($("#onboarding-complete"),"Docker must offer completion without a separate fourth stage");
  assert(!$("[data-ob-next]"),"no fourth step may follow Docker");
  await until(()=>!!$("#onboarding-runtime-profile"));
  assert.equal($("#onboarding-docker-mode").value,"pull","prebuilt download must be the default");assert(!$("#onboarding-codex-runtime-mode"),"download mode must not show build runtime controls");
  assert($("#onboarding-public-catalog-status").textContent.includes("还没有"));
  assert.equal($("#onboarding-runtime-reference").value,"","an empty catalog must not invent a usable published reference");
  assert($("#onboarding-prepare-image").disabled,"Docker unavailable must disable image downloads");
  dockerOK=true;await click("#onboarding-check-docker");await until(()=>!$("#onboarding-prepare-image").disabled);
  await click("#onboarding-prepare-image");assert($("#onboarding-error").textContent.includes("真实镜像地址"));
  const manual=$("#onboarding-runtime-reference");manual.value="registry.example/team/news:broken";manual.dispatchEvent(new w.Event("input"));
  const autoBefore=db.autoRows.length;
  await click("#onboarding-prepare-image");await until(()=>$("#onboarding-error").textContent.includes("pull response lost"));
  await click("#onboarding-prepare-image");await until(()=>!!$('[data-build-id="runtime-1"]'));
  const downloads=calls.filter(value=>value.route==="/setup/runtime-images"&&value.method==="POST");assert.equal(downloads.length,2);assert.equal(downloads[0].body.requestId,downloads[1].body.requestId);
  assert.equal(db.builds.filter(value=>value.scope==="setup").length,1,"a lost pull response must preserve request identity");
  assert.equal(db.autoRows.length,autoBefore,"pulls must not receive automatic build repair authorization");
  assert(!$('[data-build-repair="runtime-1"]'),"failed pulls must not offer build repair");
  const buildCallsBefore=calls.filter(value=>value.method==="POST"&&value.route.endsWith("/build")).length;
  await click('[data-runtime-retry="runtime-1"]');assert.equal($("#onboarding-docker-mode").value,"pull");assert.equal($("#onboarding-runtime-reference").value,"registry.example/team/news:broken");
  assert.equal(calls.filter(value=>value.method==="POST"&&value.route.endsWith("/build")).length,buildCallsBefore,"retrying a download must never silently switch to building");
  const digest="sha256:"+"b".repeat(64),mirrorDigest=digest;
  publicImages.items=[{id:"news-release",profileId:"news2douyin",profileVersion:"1.1.2",codexVersion:"0.157.1",platform:"linux/amd64",sources:[{id:"ghcr",name:"GHCR",reference:"ghcr.io/team/news@"+digest},{id:"mirror",name:"Mirror",reference:"registry.example/team/news@"+mirrorDigest}]},{id:"wrong-platform",profileId:"news2douyin",profileVersion:"1.1.2",codexVersion:"0.157.1",platform:"linux/arm64",sources:[{id:"wrong",name:"Wrong",reference:"ghcr.io/team/news@"+digest}]},{id:"old-preset",profileId:"news2douyin",profileVersion:"1.0.0",codexVersion:"0.157.1",platform:"linux/amd64",sources:[{id:"old",name:"Old",reference:"ghcr.io/team/news@"+digest}]}];
  await click("#onboarding-refresh-public-images");await until(()=>$("#onboarding-public-image").options.length===2);
  assert.equal($("#onboarding-runtime-reference").value,"registry.example/team/news:broken","refreshing must preserve a manually entered reference");
  $("#onboarding-public-image").value="news-release";$("#onboarding-public-image").dispatchEvent(new w.Event("change"));
  assert($("#onboarding-runtime-reference").readOnly);assert($("#onboarding-runtime-version").readOnly);assert($("#onboarding-runtime-reference").value.endsWith(digest));
  $("#onboarding-public-source").value="mirror";$("#onboarding-public-source").dispatchEvent(new w.Event("change"));
  await click("#onboarding-prepare-image");await until(()=>!!$('[data-build-id="runtime-2"]'));
  const selected=calls.filter(value=>value.route==="/setup/runtime-images"&&value.method==="POST").at(-1).body;
  assert.equal(selected.publicImageId,"news-release");assert.equal(selected.publicSourceId,"mirror");assert.equal(selected.catalogRevision,"public-1");assert(selected.reference.endsWith(mirrorDigest));assert.equal(db.autoRows.length,autoBefore);
  assert($('[data-runtime-open-image="runtime-2"]'),"successful downloads should lead to image inspection and instance creation");
  $("#onboarding-docker-mode").value="local";$("#onboarding-docker-mode").dispatchEvent(new w.Event("change"));
  assert(!$("#onboarding-public-image"));$("#onboarding-runtime-reference").value="local-news:ready";$("#onboarding-runtime-reference").dispatchEvent(new w.Event("input"));
  await click("#onboarding-prepare-image");await until(()=>!!$('[data-build-id="runtime-3"]'));
  assert.equal(calls.filter(value=>value.route==="/setup/runtime-images"&&value.method==="POST").at(-1).body.source,"local");
  $("#onboarding-docker-mode").value="build";$("#onboarding-docker-mode").dispatchEvent(new w.Event("change"));
  await until(()=>$$("[data-preset-id]").length===2);assert(!$("#onboarding-build-selected").disabled);
  assert.equal($("#onboarding-codex-runtime-mode").value,"auto");await click("#onboarding-codex-runtime-check");await until(()=>$("#onboarding-codex-runtime-status").textContent.includes("发现匹配"));
  $("#onboarding-codex-runtime-mode").value="base";$("#onboarding-codex-runtime-mode").dispatchEvent(new w.Event("change"));$("#onboarding-codex-runtime-reference").value="rundesk-codex:0.157.1";$("#onboarding-codex-runtime-reference").dispatchEvent(new w.Event("input"));
  const first=$('[data-preset-id="builtin-news2douyin"]');first.checked=true;first.dispatchEvent(new w.Event("change"));
  let custom=$('[data-ob-source-recipe="builtin-news2douyin"] [data-source-field="registry"]');custom.value="https://company.example/npm/";custom.dispatchEvent(new w.Event("input"));
  const second=$('[data-preset-id="builtin-people-search"]');second.checked=true;second.dispatchEvent(new w.Event("change"));
  assert.equal($('[data-ob-source-recipe="builtin-news2douyin"] [data-source-field="registry"]').value,"https://company.example/npm/","selecting another preset must preserve edited sources");
  await nav("llm");await nav("docker");assert.equal($('[data-ob-source-recipe="builtin-news2douyin"] [data-source-field="registry"]').value,"https://company.example/npm/","navigation must preserve edited sources");
  assert.equal($("#onboarding-codex-runtime-mode").value,"base");assert.equal($("#onboarding-codex-runtime-reference").value,"rundesk-codex:0.157.1","runtime source draft must survive navigation");
  const ids=$$("#onboarding-preset-sources [id]").map(value=>value.id);assert.equal(ids.length,new Set(ids).size,"multiple source forms must not duplicate IDs");
  await click("#onboarding-build-selected");await until(()=>$$('[data-build-id^="build-"]').length===2);assert.equal(db.builds.filter(value=>!value.scope).length,2);
  await until(()=>!!$('a[href*="auto-session-build-1"]'));assert($('[data-build-repair="build-1"]').disabled);assert.equal(db.autoRows.length,3,"each newly submitted source gets one backend authorization");
  const viewLog=$$('[data-build-id="build-1"] button').find(value=>value.textContent==="查看日志");viewLog.click();await until(()=>!!$('[data-build-log="build-1"]'));assert($('[data-build-log="build-1"]').textContent.includes("upstream"));
  const verified=$('[data-experience-details="verified-case"]'),candidate=$('[data-experience-id="candidate-case"]');
  assert(!verified.open,"experience evidence should be collapsed by default");verified.querySelector("summary").click();assert(verified.open);
  assert(verified.textContent.includes("Duplicate wheel archive path"));assert(verified.textContent.includes("移除重复 force-include"));assert(verified.textContent.includes("vendor/agent-reach/pyproject.toml"));assert(verified.textContent.includes("已通过"));
  const diff=$('[data-experience-diff="verified-case"]').textContent;assert(!diff.includes("must-hide-this"));assert(diff.includes("[redacted]"));assert(diff.length<12100);assert(diff.includes("展示内容已截断"));
  assert(verified.querySelector('a[href="/api/v1/build-center/builds/failed-build/log?after=0"]'));
  assert(candidate.textContent.includes("候选经验"));assert(candidate.textContent.includes("未验证"));assert(candidate.textContent.includes("尚未通过所需的完整验证"));assert(!candidate.querySelector('[data-state="ready"]'),"candidate must not be presented as passed");
  await click('[data-runtime-open-image="runtime-2"]');assert.equal(w.location.hash,"#build-center/images/image-runtime-2");assert.equal(await pending,true);assert(!$('[id="setup-onboarding"]'));assert(setup.settings.completed);
  // A completed setup is not reopened automatically; demo and explicit trace
  // links are also not captured by initialization.
  await w.RunDeskSetup.maybeOpen();assert(!$("#setup-onboarding"));
  setup.settings.completed=false;state.demo=true;await w.RunDeskSetup.maybeOpen();assert(!$("#setup-onboarding"));state.demo=false;
  w.history.replaceState(null,"","/?session=repair-session");await w.RunDeskSetup.maybeOpen();assert(!$("#setup-onboarding"));
  // Existing users can reopen setup manually; an empty recipe catalog must
  // not strand them on the last page now that completion lives there.
  recipes.length=0;w.history.replaceState(null,"","/");
  const resumed=w.RunDeskOnboarding.open({setup});await until(()=>!!$("#onboarding-endpoint"));
  assert.equal($('[aria-current="step"]').dataset.setupStep,"llm");assert.equal($$("[data-setup-step]").length,3);
  await nav("docker");assert($("#onboarding-complete"));assert(!$("#onboarding-complete").disabled);assert(!$("[data-ob-next]"));
  await click("#onboarding-dismiss");assert.equal(await resumed,false);
  console.log(JSON.stringify({version:"0.57.0",passed:true,checks:["real api() supplies all initialization idempotency headers","real api() preserves header after transport failure","initial full-page setup","exactly three steps: LLM, Codex, Docker","LLM discovery and configuration share the first step","selecting a discovered service stays in LLM configuration","next actions follow LLM to Codex to Docker","completion and repair evidence belong to Docker with no fourth step","discovery does not infer or install","inference and tools both required","task-scoped Kun configuration","native installation defaults and mirror selection","lost-response receipt reuse","backend automatic handoff with bounded task scope","automatic registration receipt retry","historical failures remain opt in","manual duplicate repair prevented","Docker availability gating","multiple opt-in preset builds","source edits survive preset selection and navigation","isolated editable source forms","Kun build repair","build log inspection","collapsed repair evidence exposes issues, changes and validation","bounded redacted Dockerfile diff and source record links","candidate experience remains unverified","optional components and completion","completed/demo/deep-link entry","manual reopening starts at the LLM step","empty Docker catalog does not block completion","prebuilt downloads are the default","empty public catalog does not invent published images","Docker readiness gates downloads","download request receipts survive lost responses","pull failures never trigger local builds or Kun build repair","published images filtered by preset version and platform","manual references survive catalog refresh","catalog image source and revision are pinned on submission","local image checks remain separate from downloads","successful acquisition completes setup and routes to image management","build runtime controls do not leak into image acquisition","server cache result is displayed without inventing readiness","runtime source drafts survive navigation","every selected builtin receives the chosen runtime source"]}));
})().catch(error=>{console.error(error);process.exitCode=1;});
