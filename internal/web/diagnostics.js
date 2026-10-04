/* Diagnostics record observations, never retry submissions or execute response content. */
"use strict";
window.RunDeskDiagnostics = (() => {
  const KEY="rundesk-diagnostics-v1", LIMIT=50, BODY_LIMIT=32768;
  const node=(tag,attrs={},...children)=>{const n=document.createElement(tag);for(const [k,v] of Object.entries(attrs)){if(k==="class")n.className=v;else n.setAttribute(k,v);}for(const c of children.flat())if(c!=null)n.append(c instanceof Node?c:document.createTextNode(String(c)));return n;};
  const action=(label,fn,attrs={})=>{const b=node("button",{type:"button",...attrs},label);b.onclick=fn;return b;};
  const secretKey=k=>/^(authorization|cookie|setcookie|token|accesstoken|refreshtoken|idtoken|apikey|password|secret|clientsecret|env|httpheaders)$/i.test(k.replace(/[_-]/g,""));
  function cleanText(s) {
    return String(s??"").replace(/(Bearer\s+)[^\s"',;]+/gi,"$1[redacted]")
      .replace(/((?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|secret|authorization|cookie)["']?\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s&,;"'<>}\]]+)/gi,(_,prefix,value)=>prefix+(value.startsWith('"')?'"[redacted]"':value.startsWith("'")?"'[redacted]'":"[redacted]"))
      .replace(/(https?:\/\/)[^/\s:@]+:[^@\s/]+@/gi,"$1[redacted]@");
  }
  function clean(v,depth=0) {
    if(depth>18)return "[nested data omitted]";
    if(typeof v==="string")return cleanText(v);
    if(Array.isArray(v))return v.map(x=>clean(x,depth+1));
    if(v&&typeof v==="object")return Object.fromEntries(Object.entries(v).map(([k,x])=>[k,secretKey(k)?"[redacted]":clean(x,depth+1)]));
    return v;
  }
  const bounded=v=>{const s=typeof v==="string"?cleanText(v):JSON.stringify(clean(v),null,2);return s.length>BODY_LIMIT?s.slice(0,BODY_LIMIT)+"\n[诊断内容已截断]":s;};
  function safeURL(value) {
    try {const u=new URL(value,location.origin);u.username="";u.password="";u.hash="";for(const k of [...u.searchParams.keys()]){if(secretKey(k)||!/^(instanceId|scope|preview|stream|after|through|limit|name|replace|path)$/.test(k))u.searchParams.set(k,"[omitted]");}return u.origin===location.origin?u.pathname+u.search:u.origin+u.pathname+u.search;}catch{return cleanText(value).slice(0,1500);}
  }
  let records=[],selected=null,returnFocus=null;
  try{const saved=JSON.parse(sessionStorage.getItem(KEY)||"[]");if(Array.isArray(saved))records=saved.filter(r=>r&&typeof r.id==="string"&&typeof r.message==="string").slice(0,LIMIT).map(clean);}catch{}
  const dialog=node("dialog",{id:"diagnostics-dialog","aria-labelledby":"diagnostics-title"});
  const list=node("nav",{id:"diagnostics-list","aria-label":"错误记录"}),detail=node("section",{id:"diagnostics-detail"}),notice=node("p",{id:"diagnostics-copy-status",role:"status"});
  const close=action("关闭",()=>dialog.close(),{"aria-label":"关闭错误记录"});
  dialog.append(node("div",{class:"diagnostics-head"},node("div",{},node("h2",{id:"diagnostics-title"},"错误记录"),node("p",{},"本标签页最近 50 组错误 · 刷新后保留")),close),node("div",{class:"diagnostics-layout"},list,detail),node("div",{class:"diagnostics-footer"},notice,action("清空记录",()=>clear(),{id:"diagnostics-clear"})));
  document.body.append(dialog);
  const originLabel=r=>({http:"页面请求",network:"网络连接",protocol:"响应格式",runtime:"Codex 运行",stream:"事件连接"}[r.kind]||r.kind);
  const time=s=>new Date(s).toLocaleString();
  function persist(){try{sessionStorage.setItem(KEY,JSON.stringify(records));}catch{notice.textContent="浏览器无法保存记录，本次页面内仍可查看。";}}
  function sync(){for(const b of document.querySelectorAll("[data-diagnostics-open]")){b.textContent="错误记录"+(records.length?` ${records.length}`:"");b.classList.toggle("has-errors",records.length>0);b.onclick=e=>{e.preventDefault();open();};}}
  function render(){
    sync();if(!dialog.open)return;
    if(!records.some(r=>r.id===selected))selected=records[0]?.id;
    list.replaceChildren(...records.map(r=>action("",()=>{selected=r.id;render();},{class:"diagnostics-item"+(r.id===selected?" selected":""),"aria-current":r.id===selected?"true":"false"})).map((b,i)=>{const r=records[i];b.append(node("strong",{},`${r.status?"HTTP "+r.status:originLabel(r)}${r.count>1?" · "+r.count+" 次":""}`),node("span",{},r.message.slice(0,125)),node("small",{},time(r.lastSeen)));return b;}));
    const r=records.find(r=>r.id===selected);detail.replaceChildren();
    if(!r){detail.append(node("p",{class:"help"},"尚无错误记录。页面请求失败或收到运行错误时，详情会保存在这里。"));return;}
    detail.append(node("p",{class:"diagnostics-origin"},originLabel(r)),node("h3",{},r.message),action("复制诊断信息",async()=>{const content=JSON.stringify(r,null,2);try{await navigator.clipboard.writeText(content);notice.textContent="诊断信息已复制。";}catch{const area=node("textarea",{readonly:"",rows:"8","aria-label":"可手动复制的诊断信息"},content);detail.append(area);area.select();notice.textContent="自动复制不可用，请手动复制已选中的内容。";}},{id:"diagnostics-copy"}));
    const fields=node("dl",{class:"diagnostics-fields"});
    for(const [label,value] of [["来源",originLabel(r)],["HTTP 状态",r.status?`${r.status}${r.statusText?" "+r.statusText:""}`:"未获取到 HTTP 状态"],["请求 / 事件",[r.method,r.url||r.eventMethod].filter(Boolean).join(" ")],["错误代码",r.code],["请求编号",r.requestId],["原响应编号",r.responseRequestId&&r.responseRequestId!==r.requestId?r.responseRequestId:null],["会话",r.sessionId],["轮次",r.turnId||r.runId],["事件编号",r.eventId],["应用配置",r.instanceId],["项目",r.workspaceId],["首次出现",time(r.time)],["最近出现",time(r.lastSeen)],["请求耗时",r.elapsedMs==null?null:`${r.elapsedMs} ms`],["响应类型",r.contentType],["上次响应重放",r.replayed],["服务端允许重试",typeof r.retryable==="boolean"?(r.retryable?"是（不会自动重试）":"否"):null]])if(value!=null&&value!=="")fields.append(node("dt",{},label),node("dd",{},value));
    const primary=node("dl",{class:"diagnostics-fields"});
    for(const dt of [...fields.querySelectorAll("dt")])if(["HTTP 状态","请求 / 事件","请求编号","原响应编号","事件编号"].includes(dt.textContent)){const dd=dt.nextElementSibling;primary.append(dt,dd);}
    detail.append(primary);
    if(r.note)detail.append(node("p",{class:"help"},r.note));
    if(r.response)detail.append(node("h4",{},r.kind==="runtime"?"运行错误详情":"响应详情"),node("pre",{id:"diagnostics-response"},r.response));
    detail.append(node("details",{class:"diagnostics-metadata"},node("summary",{},"会话、时间和其他定位信息"),fields));
    if(r.truncated)detail.append(node("p",{class:"help"},"响应较大，保留前 32 KiB；后续内容未读取。"));
    if(r.requestIds?.length>1)detail.append(node("details",{},node("summary",{},"最近请求编号"),node("pre",{},r.requestIds.join("\n"))));
    detail.append(node("p",{class:"help"},"未记录请求正文、认证头或上传内容。常见密钥字段已遮盖；响应文本仍可能包含业务信息。"));
  }
  function open(id){selected=id||records[0]?.id;notice.textContent="";if(!dialog.open){returnFocus=document.activeElement;dialog.showModal();}render();}
  dialog.addEventListener("close",()=>{if(returnFocus?.isConnected)returnFocus.focus({preventScroll:true});});
  function clear(){records=[];selected=null;sessionStorage.removeItem(KEY);document.querySelectorAll(".diagnostics-inline").forEach(n=>n.remove());render();}
  function record(input){
    const now=new Date().toISOString(),r=clean({...input,id:crypto.randomUUID?.()||"d-"+Date.now()+Math.random(),time:input.time||now,lastSeen:now,count:1});
    r.message=String(r.message||"请求失败").slice(0,4000);r.response=r.response?bounded(r.response):"";
    if(r.eventId && records.some(x=>x.kind===r.kind&&x.sessionId===r.sessionId&&(x.eventId===r.eventId||x.eventIds?.includes(r.eventId))))return records.find(x=>x.kind===r.kind&&x.sessionId===r.sessionId&&(x.eventId===r.eventId||x.eventIds?.includes(r.eventId)));
    if(r.eventId)r.eventIds=[r.eventId];
    const previous=records.find(x=>x.kind===r.kind&&x.method===r.method&&x.url===r.url&&x.sessionId===r.sessionId&&x.status===r.status&&x.code===r.code&&x.message===r.message&&Date.now()-Date.parse(x.lastSeen)<60000);
    if(previous){r.id=previous.id;r.time=previous.time;r.count=previous.count+1;if(r.eventId)r.eventIds=[...new Set([...(previous.eventIds||[previous.eventId]),r.eventId].filter(Boolean))].slice(-200);r.requestIds=[...new Set([...(previous.requestIds||[]),previous.requestId,r.requestId].filter(Boolean))].slice(-5);records=records.filter(x=>x!==previous);}
    records.unshift(r);records=records.slice(0,LIMIT);persist();render();
    const modal=[...document.querySelectorAll("dialog[open]")].filter(d=>d!==dialog).at(-1);
    if(modal){let inline=modal.querySelector(".diagnostics-inline");if(!inline){inline=node("div",{class:"diagnostics-inline",role:"status"});modal.append(inline);}inline.replaceChildren(node("span",{},r.message.slice(0,140)),action("查看错误详情",()=>open(r.id)));}
    return r;
  }
  function failure(message,info){const r=record({...info,message});const error=Error(r.message);error.diagnosticId=r.id;error.code=r.code;error.requestId=r.requestId;error.status=r.status;return error;}
  async function readErrorBody(response){
    if(!response.body)return {text:"",truncated:false};
    const reader=response.body.getReader(),decoder=new TextDecoder();let text="",size=0,truncated=false;
    try{while(true){const next=await reader.read();if(next.done)break;const take=Math.min(next.value.length,BODY_LIMIT-size);text+=decoder.decode(next.value.subarray(0,take),{stream:true});size+=take;if(next.value.length>take||size>=BODY_LIMIT){truncated=true;await reader.cancel();break;}}text+=decoder.decode();return {text,truncated};}finally{reader.releaseLock();}
  }
  async function request(url,options={},context={},format="json"){
    const started=performance.now(),base={kind:"http",method:(options.method||"GET").toUpperCase(),url:safeURL(url),...context};let response;
    try{response=await fetch(url,options);}catch(e){if(e.name==="AbortError")throw e;throw failure("请求未收到 HTTP 响应："+e.message,{...base,kind:"network",elapsedMs:Math.round(performance.now()-started),note:"可能是网络中断、服务不可达或浏览器拦截；浏览器未提供服务端状态。操作是否已执行需要核对，未自动重发。"});}
    const meta={...base,status:response.status,statusText:response.statusText,requestId:response.headers.get("X-Request-ID")||"",contentType:response.headers.get("Content-Type")||"",replayed:response.headers.get("Idempotency-Replayed")||""};
    let raw,data,truncated=false;
    try{if(response.ok){raw=await response.text();}else{const body=await readErrorBody(response);raw=body.text;truncated=body.truncated;}}catch(e){if(e.name==="AbortError")throw e;throw failure("响应读取中断："+e.message,{...meta,kind:"network",elapsedMs:Math.round(performance.now()-started)});}
    try{data=JSON.parse(raw);}catch{}
    const isObject=data!==null&&typeof data==="object"&&!Array.isArray(data);
    if(!response.ok || format==="json"&&data===undefined){
      if(response.status===401)document.querySelector("#login")?.classList.remove("hidden");
      const info={...meta,kind:response.ok?"protocol":"http",elapsedMs:Math.round(performance.now()-started),code:isObject?data.code:undefined,responseRequestId:isObject?data.requestId:undefined,retryable:isObject?data.retryable:undefined,response:data===undefined?raw:bounded(data),truncated};
      if(!info.requestId)info.requestId=info.responseRequestId||"";
      const summary=isObject?(typeof data.error==="string"?data.error:data.error?.message||data.message):"";
      const message=response.ok?`HTTP ${response.status}：预期 JSON，但收到其他内容`:`HTTP ${response.status}：${summary|| (raw.trim()?"服务端返回非标准错误响应":"服务端未提供错误内容")}`;
      // The initial authentication challenge is normal; bad login and all other errors remain visible.
      if(response.status===401&&new URL(url,location.origin).pathname==="/api/v1/meta"){const error=Error(message);error.status=401;throw error;}
      throw failure(message,info);
    }
    return format==="text"?raw:data;
  }
  function captureEvent(event,session){
    const p=event.data?.params||{};let value;
    if(event.method==="error")value=p;
    else if(event.method==="turn/completed"&&p.turn?.error)value=p.turn;
    else if(event.method==="run/state"&&event.data?.error)value=event.data;
    else return;
    const problem=value.error||value, message=typeof problem==="string"?problem:problem.message||JSON.stringify(problem);
    return record({kind:"runtime",message,eventMethod:event.method,eventId:event.id,time:event.time,sessionId:session.id,instanceId:session.instanceId,workspaceId:session.workspaceId,runId:event.data?.runId,turnId:p.turnId||p.turn?.id,response:bounded(value),note:"这是 Codex 运行事件，不代表页面到 RunDesk 的 HTTP 请求返回了同样的状态。"});
  }
  function recent(message){return records.find(r=>r.message===message&&Date.now()-Date.parse(r.lastSeen)<10000);}
  document.addEventListener("DOMContentLoaded",sync);
  return {request,record,captureEvent,open,clear,sync,recent};
})();
