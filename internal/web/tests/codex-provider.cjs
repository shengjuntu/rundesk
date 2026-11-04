'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const {JSDOM}=require('jsdom');
const dom=new JSDOM('<!doctype html><html lang="zh-CN"><body></body></html>',{url:'http://localhost/',runScripts:'outside-only'}),w=dom.window,ctx=dom.getInternalVMContext();
w.el=(tag,attrs={},...children)=>{const n=w.document.createElement(tag);for(const [k,v] of Object.entries(attrs))if(k==='class')n.className=v;else if(k.startsWith('on'))n[k]=v;else if(v!==undefined&&v!==false){if(k in n)n[k]=v;else n.setAttribute(k,v);}for(const c of children.flat())if(c!=null)n.append(c.nodeType?c:String(c));return n;};
w.button=(text,fn,cls)=>w.el('button',{type:'button',class:cls,onclick:fn},text);
const requests=[],copy=x=>JSON.parse(JSON.stringify(x));
let defaults={configured:true,revision:2,baseUrl:'http://host.docker.internal:18000/v1',model:'local-coder',apiKeyEnv:'LOCAL_LLM_KEY',wireAPI:'responses'},current={configured:false,revision:0,scope:'environment',state:'unmanaged'};
w.api=async(url,options={})=>{const method=options.method||'GET';requests.push({url,method,body:copy(options.body||{})});
 if(url==='/setup/codex-provider'){if(method==='PUT'){assert.equal(options.body.revision,defaults.revision);defaults={...defaults,...options.body,revision:defaults.revision+1};}return copy(defaults);}
 if(url==='/instances/video-agent/codex-provider?workspaceId=project-a'){if(method==='PUT'){assert.equal(options.body.revision,current.revision);current={...current,...options.body,configured:true,revision:current.revision+1,state:'configured'};}return copy(current);}
 if(url==='/instances/video-agent/codex-provider/test?workspaceId=project-a')return {scope:'docker-container',status:'ready',inferenceOK:true,toolsOK:true};
 throw Error('Unexpected API '+method+' '+url);
};
vm.runInContext(fs.readFileSync(path.join(__dirname,'../codex-provider.js'),'utf8'),ctx);
const $=s=>w.document.querySelector(s),until=async fn=>{for(let n=0;n<30;n++){if(fn())return;await new Promise(r=>setImmediate(r));}throw Error('condition timeout')};
(async()=>{
 const host=w.RunDeskCodexProvider.settings({instanceId:'video-agent',workspaceId:'project-a'});w.document.body.append(host);await until(()=>$('#codex-environment-provider-url'));
 assert.equal(requests.filter(r=>r.method!=='GET').length,0,'opening settings wrote configuration');
 $('#codex-environment-provider-url').value='http://host.docker.internal:19000/v1';$('#codex-environment-provider-model').value='changed-model';$('#codex-environment-provider-key').value='MODEL_CREDENTIAL';
 await [...host.querySelectorAll('button')].find(b=>b.textContent==='保存当前环境').onclick();
 const write=requests.find(r=>r.method==='PUT');assert.equal(write.url,'/instances/video-agent/codex-provider?workspaceId=project-a');assert.deepEqual(write.body,{baseUrl:'http://host.docker.internal:19000/v1',model:'changed-model',apiKeyEnv:'MODEL_CREDENTIAL',revision:0});
 await [...host.querySelectorAll('button')].find(b=>b.textContent==='测试已保存的配置').onclick();assert(host.textContent.includes('验证位置：Docker 容器'));assert(host.textContent.includes('工具调用：✓'));
 const form=w.el('form');w.document.body.append(form);const fields=await w.RunDeskCodexProvider.launchFields(form);assert.deepEqual(copy(fields.value()),{providerMode:'inherit-default',providerRevision:2});$('#center-launch-provider').value='existing';assert.deepEqual(copy(fields.value()),{providerMode:'existing'});form.remove();
 const retry=w.el('form');w.document.body.append(retry);const frozen=await w.RunDeskCodexProvider.launchFields(retry,{saved:{providerMode:'inherit-default',providerRevision:1}});assert($('#center-launch-provider').disabled);assert.deepEqual(copy(frozen.value()),{providerMode:'inherit-default',providerRevision:1});assert(retry.textContent.includes('原请求'));retry.remove();
 defaults.configured=false;const existing=w.el('form');w.document.body.append(existing);const existingFields=await w.RunDeskCodexProvider.launchFields(existing);assert.deepEqual(copy(existingFields.value()),{providerMode:'existing'});
 assert(!requests.some(r=>/turns|tasks|environment.*action|\/models/.test(r.url)),'settings invoked daily work or a model implicitly');
 console.log(JSON.stringify({passed:true,checks:['exact instance/workspace write and config revision','explicit saved runtime test and Docker scope','default inheritance and existing-image choice','frozen retry provider revision','opening settings is read-only'],scope:'shipped UI with API fixtures; no actual model or Docker'}));
 dom.window.close();
})().catch(e=>{console.error(e);dom.window.close();process.exitCode=1});
