"use strict";
// Compatibility entry points for conversation links and saved #trace URLs.
// All views now share the DebugService projection and one workbench instance.
window.RunDeskTraceUI={
 open:()=>{if(!state.session){toast(rdText("请先选择会话"));return;}sessionStorage.setItem("rundesk-trace-session",state.session.id);history.replaceState(null,"",location.pathname+location.search+"#trace");return RunDeskDebug.open(state.session);},
 close:()=>RunDeskDebug.close(),
 isOpen:()=>RunDeskDebug.isOpen(),
 syncComposer:()=>{},
 updateLive:()=>{}, // A fixed history advances only through the explicit refresh action.
 openOrigin:async origin=>{await selectSession(origin.sessionId);return RunDeskDebug.open(state.session,{runId:origin.runId,through:origin.through});},
 sessionChanged:()=>{if(RunDeskDebug.isOpen()&&RunDeskDebug.sessionId()!==state.session?.id)RunDeskDebug.close();if(location.hash==="#trace"&&state.session)RunDeskTraceUI.open();}
};
$("#trace-button").textContent=rdText("轨迹");
$("#trace-button").onclick=RunDeskTraceUI.open;

// The session menu opens the same control, so preference state has one editor.
const captureMenuEntry=document.querySelector("#session-http-observation");
if(captureMenuEntry)captureMenuEntry.onclick=()=>{
 if(!state.session){toast(rdText("请先选择会话"));return;}
 const menu=document.querySelector("#session-menu");if(menu)menu.open=false;
 return RunDeskDebug.open(state.session,{panel:"http"});
};
