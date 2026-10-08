"use strict";
// Native DOM only: reply and protocol text never becomes executable markup.
const conversationIcons = {
  settings: ['M9 3h6l1 3 3 1 3 5-3 5-3 1-1 3H9l-1-3-3-1-3-5 3-5 3-1z','M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0'],
  copy: ['M9 9h11v11H9z', 'M15 5V3H3v12h2'],
  up: ['M7 10v11H3V10z', 'M7 10l5-8c2 0 3 2 2 5l-1 3h6a2 2 0 0 1 2 2l-2 7a2 2 0 0 1-2 2H7'],
  down: ['M7 14V3H3v11z', 'M7 14l5 8c2 0 3-2 2-5l-1-3h6a2 2 0 0 0 2-2l-2-7a2 2 0 0 0-2-2H7'],
  note: ['M3 3h18v14H9l-6 4z','M7 7h10M7 11h7'],
  share: ['M12 15V3m-4 4 4-4 4 4', 'M5 12v8h14v-8'],
  volume: ['M11 4 5 9H2v6h3l6 5z', 'M15 8a6 6 0 0 1 0 8m3-11a10 10 0 0 1 0 14'],
  pause: ['M8 4v16M16 4v16'], play: ['m8 4 12 8-12 8z'], stop: ['M5 5h14v14H5z'],
  close: ['m6 6 12 12M18 6 6 18'], menu: ['M4 6h16M4 12h16M4 18h16'],
  commandExecution: ['m4 6 6 6-6 6M13 18h7'],
  reasoning: ['M9 18h6m-5 3h4', 'M8 15c0-2-3-3-3-7a7 7 0 0 1 14 0c0 4-3 5-3 7z'],
  fileChange: ['M14 2H5v20h14V7zM14 2v6h5', 'M8 13h8m-8 4h5'],
  mcpToolCall: ['M7 2v5m10-5v5M5 7h14v5a7 7 0 0 1-14 0zM12 19v3'],
  webSearch: ['M20 20l-5-5', 'M16 9a7 7 0 1 1-14 0 7 7 0 0 1 14 0'],
  plan: ['M9 5h12M9 12h12M9 19h12M2 4l2 2 3-3M2 11l2 2 3-3M2 18l2 2 3-3'],
  contextCompaction: ['M3 3h18v18H3zM7 12h10m-7-3 3 3-3 3'],
  tool: ['M8 3h8l5 9-5 9H8l-5-9zM9 12h6'],
  completed: ['m5 12 4 4L19 6'], failed: ['M12 3 2 21h20zM12 9v5m0 3v1'],
  declined: ['m5 5 14 14M19 5 5 19'], inProgress: ['M20 12a8 8 0 1 1-8-8'],
};
function conversationIcon(name) {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  for (const [key,value] of Object.entries({viewBox:"0 0 24 24",width:"18",height:"18",fill:"none",stroke:"currentColor","stroke-width":"1.6","stroke-linecap":"round","stroke-linejoin":"round","aria-hidden":"true",focusable:"false"})) svg.setAttribute(key,value);
  for (const d of conversationIcons[name] || conversationIcons.tool) {
    const path = document.createElementNS(svg.namespaceURI,"path"); path.setAttribute("d",d); svg.append(path);
  }
  return svg;
}
function replyButton(name, label, fn) {
  return el("button", {type:"button",class:"reply-action","aria-label":label,title:label,onclick:()=>safe(fn)},conversationIcon(name));
}
function toolSummary(summary,item) {
  const title = {commandExecution:rdText("命令执行"),reasoning:rdText("推理摘要"),fileChange:rdText("文件修改"),mcpToolCall:rdText("MCP 工具"),webSearch:rdText("网页搜索"),plan:rdText("计划"),contextCompaction:rdText("上下文压缩")}[item.type] || item.type;
  const status = item.status || (item._eventId ? "completed" : "inProgress");
  const statusText = {inProgress:rdText("进行中"),completed:rdText("已完成"),failed:rdText("失败"),declined:rdText("已拒绝")}[status] || status;
  let label = item.command || (item.type==="mcpToolCall" ? [item.server,item.tool].filter(Boolean).join(" / ") : "");
  if(item.type==="webSearch") label=item.query||item.action?.query||"";
  if(item.type==="fileChange") label=(item.changes||[]).map(c=>c.path).filter(Boolean).join("、");
  if(item.type==="reasoning") label=(item.summary||[]).map(p=>typeof p==="string"?p:p.text||"").join(" ");
  label=String(label).replace(/\s+/g," ").slice(0,100);
  const description=[title,label,statusText,rdText("展开或收起详情")].filter(Boolean).join(" · ");
  summary.title=description; summary.setAttribute("aria-label",description);
  if(!summary.firstElementChild) summary.append(el("span",{class:"tool-kind"},conversationIcon(item.type)),el("span",{class:"tool-label"}),el("span",{class:"tool-state"}));
  summary.children[1].textContent=label;
  const badge=summary.lastElementChild;
  if(badge.dataset.status!==status) {badge.dataset.status=status;badge.replaceChildren(conversationIcon(status));}
  badge.title=statusText;
}

const replyState = {ratings:new Map(), ready:false, busy:new Set(), dialog:null};
function resetReplyActions() {
  stopReplySpeech();
  replyState.ratings.clear();replyState.ready=false;replyState.busy.clear();
  replyState.dialog?.close();
  closeConversationSidebar();
}
async function loadReplyFeedback(id,selection=state.selection) {
  const values=await api(`/sessions/${id}/feedback`);
  if(state.selection!==selection || state.session?.id!==id) return;
  replyState.ratings=new Map(values.map(v=>[v.eventId,v]));replyState.ready=true;
  refreshReplyActions();
}
function renderAssistant(node,item) {
  node._reply={...item,sessionId:state.session.id};
  if(!node.firstElementChild) node.append(markdown(item.text||"…"));
  else if(node._replyText!==item.text) node.firstElementChild.replaceWith(markdown(item.text||"…"));
  node._replyText=item.text;
  if(item._eventId) node.dataset.eventId=item._eventId;
  if(item._eventId && !node.querySelector(".reply-actions")) {
    node.dataset.eventId=item._eventId;
    const toolbar=el("div",{class:"reply-actions",role:"group","aria-label":rdText("回复操作")});
    const copy=replyButton("copy",rdText("复制回复"),()=>copyReplyText(node._reply.text||""));copy.dataset.action="copy";
    const up=replyButton("up",rdText("有帮助"),()=>rateReply(node,"up"));up.dataset.rating="up";
    const down=replyButton("down",rdText("有待改进"),()=>rateReply(node,"down"));down.dataset.rating="down";
    const note=replyButton("note",rdText("查看评价说明"),()=>editReplyFeedback(node._reply));note.dataset.action="note";note.hidden=true;
    const share=replyButton("share",rdText("分享回复"),()=>shareReply(node._reply));share.dataset.action="share";
    const read=replyButton("volume",rdText("朗读回复"),()=>toggleReplySpeech(node));read.dataset.action="read";
    const stop=replyButton("stop",rdText("停止朗读"),stopReplySpeech);stop.dataset.action="stop";stop.hidden=true;
    toolbar.append(copy,up,down,note,share,read,stop,el("span",{class:"speech-status",role:"status"}));
    node.append(toolbar);
  }
  updateReplyActions(node);
}
function updateReplyActions(node) {
  const item=node._reply;if(!item) return;
  for(const btn of node.querySelectorAll("[data-rating]")) {
    const selected=replyState.ratings.get(item._eventId)?.rating===btn.dataset.rating;
    btn.setAttribute("aria-pressed",String(selected));btn.disabled=replyState.busy.has(item._eventId);
    const label=btn.dataset.rating==="up"?rdText("有帮助"):rdText("有待改进");
    btn.setAttribute("aria-label",selected?rdFormat("取消${0}评价",label):label);btn.title=btn.getAttribute("aria-label");
  }
  const note=node.querySelector('[data-action="note"]');if(note)note.hidden=!replyState.ratings.get(item._eventId)?.comment;
  const read=node.querySelector('[data-action="read"]');if(!read) return;
  const supported=!!window.speechSynthesis && !!window.SpeechSynthesisUtterance;
  const current=speechReply.node===node;
  const label=!supported?rdText("此浏览器不支持朗读"):current?(speechReply.paused?rdText("继续朗读"):rdText("暂停朗读")):rdText("朗读回复");
  read.disabled=!supported;read.title=label+(supported?rdText(" · 浏览器语音，跳过代码块"):"");read.setAttribute("aria-label",label);
  const icon=current?(speechReply.paused?"play":"pause"):"volume";
  if(read.dataset.icon!==icon){read.replaceChildren(conversationIcon(icon));read.dataset.icon=icon;}
  node.querySelector('[data-action="stop"]').hidden=!current;
  node.querySelector(".speech-status").textContent=current?(speechReply.paused?rdText("已暂停"):speechReply.started?rdText("正在朗读"):rdText("准备朗读…")):"";
}
function refreshReplyActions(){document.querySelectorAll("#messages .assistant").forEach(updateReplyActions);}
async function copyReplyText(text) {
  try {
    if(!navigator.clipboard?.writeText) throw Error("unsupported");
    await navigator.clipboard.writeText(text);toast(rdText("已复制"));
  } catch {
    const dialog=replyDialog(rdText("复制内容"));
    const field=el("textarea",{class:"copy-fallback",readonly:true,"aria-label":rdText("待复制的内容")});field.value=text;
    dialog.append(el("p",{class:"help"},rdText("浏览器未允许自动复制，请选中后使用系统的复制功能。")),field);
    dialog.showModal();field.focus();field.select();
  }
}
function replyDialog(title) {
  replyState.dialog?.close();
  const dialog=el("dialog",{class:"reply-dialog","aria-labelledby":"reply-dialog-title"});
  dialog.append(el("div",{class:"dialog-head"},el("h2",{id:"reply-dialog-title"},title),replyButton("close",rdText("关闭"),()=>dialog.close())));
  dialog.addEventListener("close",()=>{dialog.remove();if(replyState.dialog===dialog) replyState.dialog=null;});
  document.body.append(dialog);replyState.dialog=dialog;return dialog;
}
async function rateReply(node,rating) {
  const item=node._reply, selection=state.selection;
  if(!replyState.ready) {await loadReplyFeedback(item.sessionId,selection);if(state.selection!==selection)return;}
  if(replyState.ratings.get(item._eventId)?.rating===rating) return saveReplyFeedback(item,"none","");
  if(rating==="up") return saveReplyFeedback(item,"up","");
  return editReplyFeedback(item, rating);
}
function editReplyFeedback(item, rating=replyState.ratings.get(item._eventId)?.rating||"down") {
  const dialog=replyDialog(rdText("这条回复可以怎样改进？"));
  const field=el("textarea",{rows:4,maxlength:2000,placeholder:rdText("说明原因（选填）"),"aria-label":rdText("评价说明")});
  field.value=replyState.ratings.get(item._eventId)?.comment||"";
  const save=button(rdText("保存评价"),async()=>{
    save.disabled=true;
    try{await saveReplyFeedback(item,rating,field.value);dialog.close();} finally{save.disabled=false;}
  },"primary");
  dialog.append(field,el("p",{class:"help"},rdText("评价保存在此 RunDesk 中，供你回看。")),el("div",{class:"reply-dialog-actions"},button(rdText("取消"),()=>dialog.close()),save));
  dialog.showModal();field.focus();
}
async function saveReplyFeedback(item,rating,comment) {
  if(replyState.busy.has(item._eventId)) return;
  const selection=state.selection;
  replyState.busy.add(item._eventId);refreshReplyActions();
  try {
    const value=await api(`/sessions/${item.sessionId}/messages/${item._eventId}/feedback`,{method:"PUT",body:{rating,comment}});
    if(state.selection!==selection) return;
    replyState.ratings.set(item._eventId,value);toast(rating==="none"?rdText("已取消评价"):rdText("评价已保存"));
  } finally {
    if(state.selection===selection){replyState.busy.delete(item._eventId);refreshReplyActions();}
  }
}
function shareReply(item) {
  // Preview is exactly the selected reply, without prompts, logs or account data.
  const text=item.text||"", dialog=replyDialog(rdText("分享回复"));
  const preview=markdown(text);preview.classList.add("share-preview");
  const actions=el("div",{class:"reply-dialog-actions"},button(rdText("复制全文"),()=>copyReplyText(text)),button(rdText("下载 Markdown"),()=>{
    const url=URL.createObjectURL(new Blob([text],{type:"text/markdown;charset=utf-8"}));
    const link=el("a",{href:url,download:`rundesk-reply-${item._eventId}.md`});document.body.append(link);link.click();link.remove();setTimeout(()=>URL.revokeObjectURL(url),1000);
  }));
  const data={title:rdText("RunDesk 回复"),text};
  if(navigator.share && (!navigator.canShare || navigator.canShare(data))) actions.append(button(rdText("系统分享"),async()=>{
    try {await navigator.share(data);} catch(e) {if(e.name!=="AbortError") throw Error(rdText("系统分享未完成，请使用复制或下载。"));}
  },"primary"));
  dialog.append(el("p",{class:"help"},rdText("预览本次分享的内容：")),preview,actions);dialog.showModal();
}
const speechReply={node:null,paused:false,started:false,generation:0,utterance:null,timer:null};
function stopReplySpeech() {
  speechReply.generation++;clearTimeout(speechReply.timer);
  speechReply.node=null;speechReply.paused=false;speechReply.started=false;speechReply.utterance=null;
  window.speechSynthesis?.cancel();refreshReplyActions();
}
function toggleReplySpeech(node) {
  const synth=window.speechSynthesis;if(!synth||!window.SpeechSynthesisUtterance)return;
  if(speechReply.node===node) {
    if(speechReply.paused) synth.resume();else synth.pause();
    speechReply.paused=!speechReply.paused;refreshReplyActions();return;
  }
  stopReplySpeech();
  // Read rendered prose; code fences are available via their own copy controls.
  const body=node.firstElementChild.cloneNode(true);body.querySelectorAll(".code-block").forEach(n=>n.remove());
  body.querySelectorAll("br").forEach(n=>n.replaceWith("\n"));
  const text=[...body.children].map(n=>n.textContent).join("\n").trim();
  if(!text){toast(rdText("这条回复没有可朗读的正文"));return;}
  const chunks=text.match(/[^。！？.!?\n]{1,180}[。！？.!?\n]?|[。！？.!?\n]/gu)||[];
  const generation=speechReply.generation;speechReply.node=node;
  const next=()=>{
    if(generation!==speechReply.generation)return;
    const chunk=chunks.shift();if(!chunk){stopReplySpeech();return;}
    const utterance=new SpeechSynthesisUtterance(chunk);utterance.lang=/[\u3400-\u9fff]/u.test(text)?"zh-CN":navigator.language;
    speechReply.utterance=utterance;
    utterance.onstart=()=>{if(generation!==speechReply.generation)return;clearTimeout(speechReply.timer);speechReply.started=true;refreshReplyActions();};
    utterance.onend=()=>{if(generation!==speechReply.generation)return;clearTimeout(speechReply.timer);next();};
    utterance.onerror=()=>{if(generation!==speechReply.generation)return;stopReplySpeech();toast(rdText("浏览器未能朗读，请检查系统语音是否可用。"));};
    synth.speak(utterance);
    speechReply.timer=setTimeout(()=>{if(generation===speechReply.generation&&!speechReply.paused){stopReplySpeech();toast(rdText("朗读未能启动，请检查系统语音是否可用。"));}},15000);
  };
  synth.resume();next();refreshReplyActions();
}
window.addEventListener("pagehide",stopReplySpeech);
function closeConversationSidebar(){
  document.body.classList.remove("sidebar-open");document.querySelector("#sidebar-toggle")?.setAttribute("aria-expanded","false");
  const main=document.querySelector("main");if(main)main.inert=false;
}
window.addEventListener("DOMContentLoaded",()=>{
  document.querySelector("#manage-instances").replaceChildren(conversationIcon("settings"));
  document.querySelector("#manage-instances").setAttribute("aria-label",rdText("管理实例"));
  const toggle=document.querySelector("#sidebar-toggle");toggle?.append(conversationIcon("menu"));
  toggle?.addEventListener("click",()=>{if(innerWidth>600){document.body.classList.toggle("history-collapsed");return;}const opened=document.body.classList.toggle("sidebar-open");toggle.setAttribute("aria-expanded",String(opened));document.querySelector("main").inert=opened;if(opened)document.querySelector("#sidebar-close").focus();});
  document.querySelector("#sidebar-close")?.addEventListener("click",()=>{closeConversationSidebar();toggle.focus();});
  document.querySelector("#sidebar-backdrop")?.addEventListener("click",closeConversationSidebar);
  matchMedia("(max-width:600px)").addEventListener("change",closeConversationSidebar);
  document.addEventListener("keydown",e=>{if(e.key==="Escape"&&!document.querySelector("dialog[open]")&&document.body.classList.contains("sidebar-open")){closeConversationSidebar();toggle.focus();}});
});
