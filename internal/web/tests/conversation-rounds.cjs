'use strict';
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const {JSDOM}=require('jsdom'),{webcrypto}=require('node:crypto'),{TextEncoder}=require('node:util');
const root=path.resolve(__dirname,'..'),dom=new JSDOM(fs.readFileSync(path.join(root,'index.html'),'utf8'),{url:'http://localhost/',runScripts:'outside-only'}),w=dom.window,ctx=dom.getInternalVMContext();
Object.defineProperty(w,'crypto',{value:webcrypto});w.TextEncoder=TextEncoder;w.setInterval=()=>0;w.matchMedia=()=>({matches:false,addEventListener(){}});w.rdText=s=>s;w.rdFormat=(s,...a)=>s.replace(/\$\{(\d+)\}/g,(_,i)=>a[+i]);w.RunDeskTraceUI={open(){}};
const load=name=>vm.runInContext(fs.readFileSync(path.join(root,name),'utf8').replace('document.addEventListener("DOMContentLoaded", () => safe(boot));',''),ctx,{filename:name});
load('conversation.js');load('conversation-rounds.js');load('app.js');
const state=vm.runInContext('state',ctx),$=s=>w.document.querySelector(s),$$=s=>[...w.document.querySelectorAll(s)],r=w.RunDeskConversationRounds;
let next=0;
const host=(method,data)=>({id:++next,direction:'internal',method,data});
const native=(method,turn,extra)=>({id:++next,direction:'in',method,data:{params:{turnId:turn,...extra}}});
const input=(run,text)=>host('run/input',{runId:run,input:{text}});
const alias=(run,turn)=>host('run/turn',{runId:run,turnId:turn});
const done=run=>host('run/state',{runId:run,status:'completed'});
function show(events,session={id:'rounds-session',runId:'r2',turnId:'t2',status:'running'}){state.session=session;state.events=events;w.renderMessages();}
const turn=(id,type,text)=>({item:{id,type,text,status:'completed'}});
const events=[input('r1','第一轮：检查项目\n更多要求'),alias('r1','t1'),native('item/completed','t1',turn('a1','agentMessage','第一轮答案')),done('r1'),input('r2','第二轮'),alias('r2','t2'),native('item/started','t2',{item:{id:'tool2',type:'commandExecution',command:'echo test',status:'inProgress'}})];
show(events);
assert.equal($$('.conversation-round').length,2);assert(!$('.conversation-round[data-run-id="r1"]').open);assert($('.conversation-round[data-run-id="r2"]').open);assert.equal($('.conversation-round[data-run-id="r1"] .conversation-round-title').textContent,'第一轮：检查项目');
const first=$('.conversation-round[data-run-id="r1"]'),reply=first.querySelector('.message.assistant');first.querySelector('summary').click();assert(first.open);assert(JSON.parse(w.localStorage.getItem('rundesk-round-disclosure:rounds-session')).r1);
const tool=$('.conversation-round[data-run-id="r2"] .tool');tool.open=true;tool.querySelector('summary').focus();
events.push(native('item/commandExecution/outputDelta','t2',{itemId:'tool2',delta:'new output'}));show(events);assert.equal(first.querySelector('.message.assistant'),reply,'reply DOM was replaced');assert.equal($('.conversation-round[data-run-id="r2"] .tool'),tool,'tool DOM was replaced');assert(tool.open);assert.equal(w.document.activeElement,tool.querySelector('summary'),'streaming stole focus');assert(first.open,'streaming reset manual round expansion');
events.push(done('r2'));show(events,{id:'rounds-session',runId:'r2',turnId:'t2',status:'completed'});assert($('.conversation-round[data-run-id="r2"]').open,'completion hid an actively read tool');
first.querySelector('summary').click();assert(!first.open);load('conversation-rounds.js');show(events);assert(!$('.conversation-round[data-run-id="r1"]').open,'reload forgot saved collapse');$('.conversation-round[data-run-id="r1"] summary').click();load('conversation-rounds.js');show(events);assert($('.conversation-round[data-run-id="r1"]').open,'reload forgot saved expansion');
// Unknown turns stay separate; a later authoritative host alias can safely move
// the same mounted tool. Conflicting aliases undo ownership instead of guessing.
const foreign=native('item/completed','foreign',{item:{id:'foreign-tool',type:'commandExecution',command:'unknown'}});events.push(input('r3','第三轮'),foreign);show(events);const foreignNode=$('.conversation-unassigned .tool');assert(foreignNode);
events.push(alias('r3','foreign'));show(events);assert(!$('.conversation-unassigned'));assert.equal($('.conversation-round[data-run-id="r3"] .tool:last-child'),foreignNode);
events.push(alias('r1','foreign'));show(events);assert.equal($('.conversation-unassigned .tool'),foreignNode,'conflicting source was assigned to a chosen run');
const spoof=[input('safe','safe title'),native('item/completed','fake',turn('fake-item','agentMessage','unassigned reply')),{id:++next,direction:'in',method:'run/turn',data:{runId:'safe',turnId:'fake'}}];show(spoof,{id:'spoof-session',runId:'',turnId:'',status:'idle'});assert($('.conversation-unassigned .message.assistant'),'incoming alias was trusted');
const legacy=[input('legacy','Old journal'),native('turn/started','old',{turn:{id:'old',status:'inProgress'}}),native('item/completed','old',turn('old-answer','agentMessage','Old answer')),done('legacy')];const legacyProjection=r.project(legacy,{});assert.equal(legacyProjection.origins.get(legacy[2].id).runId,'legacy');
const ambiguous=[input('one','one'),alias('one','native-a'),alias('one','native-b'),native('item/completed','native-a',turn('a','agentMessage','A')),native('item/completed','native-b',turn('b','agentMessage','B'))];const ambiguousProjection=r.project(ambiguous,{});assert(!ambiguousProjection.origins.has(ambiguous[3].id));assert(!ambiguousProjection.origins.has(ambiguous[4].id));
show([{id:++next,direction:'in',method:'run/input',data:{params:{opaque:true}}}],{id:'malformed'});assert($('.conversation-unassigned .run-error'));
w.document.documentElement.lang='en';show([input('verbatim','已完成'),done('verbatim')],{id:'english'});assert.equal($('.conversation-round-title').textContent,'已完成');assert.equal($('.conversation-round-status').textContent,'Completed');w.document.documentElement.lang='zh-CN';
const shared=[input('same-id','Shared identifier'),done('same-id')];show(shared,{id:'session-one'});$('.conversation-round-summary').click();show(shared,{id:'session-two'});assert(!$('.conversation-round').open,'disclosure leaked between sessions');$('.conversation-round-summary').click();assert(JSON.parse(w.localStorage.getItem('rundesk-round-disclosure:session-two'))['same-id']);assert(JSON.parse(w.localStorage.getItem('rundesk-round-disclosure:session-one'))['same-id']);
const fail=[input('failed','失败轮'),host('run/state',{runId:'failed',status:'failed',error:'fixture'})];show(fail,{id:'failed-session'});assert($('.conversation-round').open);assert($('.conversation-round-status').textContent.includes('失败'));
const bound=[input('large','大量步骤'),alias('large','large-turn')];for(let n=0;n<220;n++)bound.push(native('item/completed','large-turn',turn('a'+n,'agentMessage','answer'+n)));bound.push(done('large'));show(bound,{id:'bounded-session'});assert.equal($$('[data-message-key]').length,200);assert($('.conversation-history-limit'));assert($('.conversation-round-title').textContent.includes('大量步骤'));
console.log(JSON.stringify({passed:true,checks:['completed whole-round fold with user title','active and failed status retained','manual disclosure survives updates and reload','mounted answer/tool identity and reading focus retained','late authoritative aliases and conflicting/unknown records','incoming alias ignored','conservative legacy association','200-card history bound disclosed'],scope:'shipped model and renderMessages in JSDOM; no model or container'}));
dom.window.close();
