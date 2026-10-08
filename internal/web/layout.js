"use strict";
window.addEventListener('DOMContentLoaded',()=>{
 const handle=document.querySelector('#history-resize');if(!handle)return;
 const apply=v=>{v=Math.max(220,Math.min(420,Number(v)||270));document.documentElement.style.setProperty('--history-width',v+'px');localStorage.setItem('rundesk-history-width',String(v));};
 apply(localStorage.getItem('rundesk-history-width'));
 handle.onpointerdown=e=>{handle.setPointerCapture(e.pointerId);handle.onpointermove=e=>apply(e.clientX-document.querySelector('.global-rail').getBoundingClientRect().width);};handle.onpointerup=()=>handle.onpointermove=null;handle.onlostpointercapture=()=>handle.onpointermove=null;
 handle.onkeydown=e=>{if(['ArrowLeft','ArrowRight'].includes(e.key)){e.preventDefault();apply(Number(localStorage.getItem('rundesk-history-width'))+(e.key==='ArrowLeft'?-20:20));}};
 document.querySelector('#history-collapse').onclick=()=>document.body.classList.add('history-collapsed');
 document.querySelector('#system-setup').onclick=()=>RunDeskSetup.open();
});
