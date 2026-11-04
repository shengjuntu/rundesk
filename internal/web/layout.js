"use strict";
// The application navigation and conversation outline have separate lifetimes.
// Page navigation closes a mobile drawer, never changes the desktop preference.
window.RunDeskLayout=(()=>{
 const $=selector=>document.querySelector(selector);
 const collapsedKey='rundesk-history-collapsed',widthKey='rundesk-history-width';
 const read=key=>{try{return localStorage.getItem(key);}catch{return null;}};
 const write=(key,value)=>{try{localStorage.setItem(key,value);}catch{}};
 const tr=(zh,en)=>document.documentElement.lang==='en'?en:zh;
 let initialized=false,view='',drawerOpen=false,width=250;
 let preference=read(collapsedKey);
 const mobile=()=>window.innerWidth<=600;
 const conversation=()=>view==='conversation';
 const collapsed=()=>preference===null?window.innerWidth<900:preference==='1';
 function sync(){
  const sidebar=$('#history-sidebar'),toggle=$('#sidebar-toggle'),main=$('main'),rail=$('.global-rail');
  if(!sidebar||!toggle)return;
  if(!conversation()||!mobile())drawerOpen=false;
  const visible=conversation()&&(mobile()?drawerOpen:!collapsed());
  document.body.classList.toggle('history-collapsed',!mobile()&&collapsed());
  document.body.classList.toggle('sidebar-open',mobile()&&drawerOpen);
  sidebar.inert=!visible;sidebar.setAttribute('aria-hidden',String(!visible));
  toggle.setAttribute('aria-expanded',String(visible));
  toggle.setAttribute('aria-label',visible?tr('收起工作区与会话','Collapse workspaces and conversations'):tr('展开工作区与会话','Show workspaces and conversations'));
  toggle.title=toggle.getAttribute('aria-label');
  if(mobile()&&drawerOpen){sidebar.setAttribute('role','dialog');sidebar.setAttribute('aria-modal','true');}
  else{sidebar.removeAttribute('role');sidebar.removeAttribute('aria-modal');}
  if(main)main.inert=mobile()&&drawerOpen;
  if(rail)rail.inert=mobile()&&drawerOpen;
 }
 function close(restoreFocus=false){drawerOpen=false;sync();if(restoreFocus&&conversation())$('#sidebar-toggle')?.focus();}
 function setView(next){view=next;drawerOpen=false;sync();}
 function setCollapsed(next){preference=next?'1':'0';write(collapsedKey,preference);sync();if(next)$('#sidebar-toggle')?.focus();}
 function applyWidth(value){
  width=Math.max(220,Math.min(360,Number(value)||250));
  document.documentElement.style.setProperty('--history-width',width+'px');write(widthKey,String(width));
  const handle=$('#history-resize');if(handle){handle.setAttribute('aria-valuemin','220');handle.setAttribute('aria-valuemax','360');handle.setAttribute('aria-valuenow',String(width));}
 }
 function focusable(){return [...$('#history-sidebar').querySelectorAll('button:not(:disabled),a[href],input:not(:disabled),select:not(:disabled),textarea:not(:disabled),[tabindex]:not([tabindex="-1"])')].filter(node=>!node.closest('[hidden]')&&node.getClientRects().length);}
 function init(){
  if(initialized)return;initialized=true;
  view=document.body.dataset.view||view||'conversation';
  applyWidth(read(widthKey));
  const handle=$('#history-resize');
  if(handle){
   handle.onpointerdown=e=>{if(mobile()||!conversation()||collapsed())return;handle.setPointerCapture(e.pointerId);handle.onpointermove=e=>applyWidth(e.clientX-$('.global-rail').getBoundingClientRect().width);};
   handle.onpointerup=()=>handle.onpointermove=null;handle.onlostpointercapture=()=>handle.onpointermove=null;
   handle.onkeydown=e=>{if(e.key==='ArrowLeft'||e.key==='ArrowRight'){e.preventDefault();applyWidth(width+(e.key==='ArrowLeft'?-20:20));}};
  }
  $('#history-collapse')?.addEventListener('click',()=>setCollapsed(true));
  $('#sidebar-toggle')?.addEventListener('click',()=>{
   if(!conversation())return;
   if(!mobile()){setCollapsed(!collapsed());return;}
   drawerOpen=!drawerOpen;sync();if(drawerOpen)$('#sidebar-close')?.focus();
  });
  $('#sidebar-close')?.addEventListener('click',()=>close(true));
  $('#sidebar-backdrop')?.addEventListener('click',()=>close(true));
  $('#system-setup')?.addEventListener('click',()=>{close(false);RunDeskSetup.open();});
  document.addEventListener('keydown',e=>{
   if(!mobile()||!drawerOpen||document.querySelector('dialog[open]'))return;
   if(e.key==='Escape'){e.preventDefault();close(true);return;}
   if(e.key==='Tab'){
    const nodes=focusable(),first=nodes[0],last=nodes[nodes.length-1];
    if(!first){e.preventDefault();$('#sidebar-close')?.focus();return;}
    if(e.shiftKey&&(document.activeElement===first||!$('#history-sidebar').contains(document.activeElement))){e.preventDefault();last.focus();}
    else if(!e.shiftKey&&(document.activeElement===last||!$('#history-sidebar').contains(document.activeElement))){e.preventDefault();first.focus();}
   }
  });
  window.addEventListener('resize',()=>{const hadDrawer=drawerOpen;if(!mobile())drawerOpen=false;sync();if(hadDrawer&&!drawerOpen&&conversation())$('#sidebar-toggle')?.focus();});
  new MutationObserver(sync).observe(document.documentElement,{attributes:true,attributeFilter:['lang']});
  sync();
 }
 window.addEventListener('DOMContentLoaded',init);
 return {init,setView,close};
})();
