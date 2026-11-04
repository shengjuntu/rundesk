/* Sidebar preference and modal keyboard behavior; shipped JS in a DOM fixture. */
'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const {JSDOM}=require('jsdom'),root=path.resolve(__dirname,'..');
function fixture({width=1440,collapsed=null,storedWidth=null}={}){
 const dom=new JSDOM(fs.readFileSync(path.join(root,'index.html'),'utf8'),{url:'http://localhost/',runScripts:'outside-only'}),w=dom.window;
 Object.defineProperty(w,'innerWidth',{value:width,writable:true});w.document.body.dataset.view='conversation';
 if(collapsed!==null)w.localStorage.setItem('rundesk-history-collapsed',collapsed);
 if(storedWidth!==null)w.localStorage.setItem('rundesk-history-width',storedWidth);
 w.HTMLElement.prototype.getClientRects=function(){return this.closest('[hidden]')?[]:[{}]};w.RunDeskSetup={open(){}};
 vm.runInContext(fs.readFileSync(path.join(root,'layout.js'),'utf8'),dom.getInternalVMContext());w.RunDeskLayout.init();
 const $=q=>w.document.querySelector(q),key=(name,shift=false)=>w.document.dispatchEvent(new w.KeyboardEvent('keydown',{key:name,shiftKey:shift,bubbles:true,cancelable:true}));
 return {dom,w,$,key,layout:w.RunDeskLayout};
}
(()=>{
 const {dom,w,$,layout}=fixture({storedWidth:'420'});
 assert.equal($('#history-resize').getAttribute('aria-valuenow'),'360');assert.equal($('#sidebar-toggle').getAttribute('aria-expanded'),'true');
 $('#history-collapse').click();assert(w.document.body.classList.contains('history-collapsed'));assert($('#history-sidebar').inert);assert.equal(w.localStorage.getItem('rundesk-history-collapsed'),'1');assert.equal(w.document.activeElement.id,'sidebar-toggle');
 layout.setView('build-center');assert.equal($('#history-sidebar').getAttribute('aria-hidden'),'true');layout.setView('conversation');assert.equal($('#sidebar-toggle').getAttribute('aria-expanded'),'false');
 $('#sidebar-toggle').click();assert.equal(w.localStorage.getItem('rundesk-history-collapsed'),'0');assert.equal($('#sidebar-toggle').getAttribute('aria-expanded'),'true');
 layout.setView('instances');layout.setView('conversation');assert.equal($('#sidebar-toggle').getAttribute('aria-expanded'),'true','navigation changed outline preference');
 $('#history-resize').dispatchEvent(new w.KeyboardEvent('keydown',{key:'ArrowLeft',bubbles:true}));assert.equal($('#history-resize').getAttribute('aria-valuenow'),'340');
 dom.window.close();
 const restored=fixture({collapsed:'1'});assert.equal(restored.$('#sidebar-toggle').getAttribute('aria-expanded'),'false');restored.dom.window.close();
 const tablet=fixture({width:800});assert.equal(tablet.$('#sidebar-toggle').getAttribute('aria-expanded'),'false','tablet defaults cramped workspace panel open');tablet.dom.window.close();
})()
;(()=>{
 const {dom,w,$,key,layout}=fixture({width:390,collapsed:'0'});
 assert.equal($('#sidebar-toggle').getAttribute('aria-expanded'),'false');$('#sidebar-toggle').click();
 assert(w.document.body.classList.contains('sidebar-open'));assert($('main').inert);assert($('.global-rail').inert);assert.equal($('#history-sidebar').getAttribute('aria-modal'),'true');assert.equal(w.document.activeElement.id,'sidebar-close');
 key('Tab',true);assert.equal(w.document.activeElement.id,'logout','reverse Tab escaped drawer');key('Tab');assert.equal(w.document.activeElement.id,'sidebar-close','forward Tab escaped drawer');
 key('Escape');assert(!w.document.body.classList.contains('sidebar-open'));assert(!$('main').inert);assert(!$('.global-rail').inert);assert.equal(w.document.activeElement.id,'sidebar-toggle');assert.equal(w.localStorage.getItem('rundesk-history-collapsed'),'0','mobile close overwrote desktop preference');
 $('#sidebar-toggle').click();$('#sidebar-backdrop').click();assert.equal(w.document.activeElement.id,'sidebar-toggle');
 $('#sidebar-toggle').click();layout.setView('build-center');assert(!$('main').inert);assert.equal($('#history-sidebar').getAttribute('aria-hidden'),'true');assert(!w.document.body.classList.contains('sidebar-open'));
 layout.setView('conversation');w.innerWidth=1440;w.dispatchEvent(new w.Event('resize'));assert.equal($('#sidebar-toggle').getAttribute('aria-expanded'),'true','desktop preference not restored after mobile');
 dom.window.close();
})()
;console.log(JSON.stringify({passed:true,checks:['desktop width bounds and persisted collapse preference','nonconversation hides/inerts outline and return restores preference','tablet defaults collapsed','mobile focus trap, escape and backdrop restore focus','mobile navigation clears inert without changing desktop preference'],scope:'shipped layout JavaScript in JSDOM; geometry validated separately in browser'}));
