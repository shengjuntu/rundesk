// Exercise real initialization UI code; this is a DOM contract test, not browser layout.
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
class E{
 constructor(tag,attrs={},...children){this.tagName=tag.toUpperCase();Object.assign(this,attrs);this.children=[];this.listeners={};this.value=attrs.value||'';this.append(...children)}
 append(...children){this.children.push(...children.flat().filter(x=>x!=null))}
 replaceChildren(...children){this.children=[];this.append(...children)}
 addEventListener(name,fn){(this.listeners[name]??=[]).push(fn)}
 emit(name){for(const fn of this.listeners[name]||[])fn()}
 set textContent(v){this.children=[v]}get textContent(){return this.children.map(x=>x instanceof E?x.textContent:String(x)).join('')}
}
const all=e=>[e,...e.children.filter(x=>x instanceof E).flatMap(all)],get=(e,id)=>all(e).find(x=>x.id===id),find=(e,text)=>all(e).find(x=>x.tagName==='BUTTON'&&x.textContent===text);
const requests=[],timers=new Map();let jobs=[],timerID=0;
const api=async(url,options={})=>{
 requests.push({url,options});
 if(url==='/setup/runtime-profiles')return {supported:true,items:[{id:'news2douyin',name:'新闻研究',reference:'rundesk-news2douyin:exa-agent-reach'}]};
 if(url==='/instances/default/builds')return structuredClone(jobs);
 if(url==='/setup/runtime-images'){assert.equal(options.body.profileId,'news2douyin');assert.equal(options.body.codexVersion,'0.144.6');const v={id:'fixture',name:'news',scope:'setup',source:options.body.source,profileId:'news2douyin',codexVersion:'0.144.6',reference:options.body.reference,status:'queued'};jobs=[v];return structuredClone(v)}
 if(url.includes('/log?after='))return {text:'',next:0};
 if(url.endsWith('/cancel')){jobs[0].status='canceled';return structuredClone(jobs[0])}
 throw Error('unexpected '+url);
};
const context=vm.createContext({window:{},document:{documentElement:{lang:'zh'}},api,el:(...a)=>new E(...a),button:(label,click)=>{const b=new E('button',{},label);b.click=click;return b},setTimeout:(fn)=>{timers.set(++timerID,fn);return timerID},clearTimeout:id=>timers.delete(id)});
vm.runInContext(fs.readFileSync(path.join(__dirname,'../internal/web/setup-images.js'),'utf8'),context);
const flush=async()=>{for(let n=0;n<8;n++)await Promise.resolve()};
(async()=>{
 const dialog=new E('dialog'),panel=context.window.RunDeskRuntimeSetup.panel(dialog);await flush();
 assert(!requests.some(x=>x.options.method==='POST'),'initialization performed an installation');
 assert.equal(get(panel,'runtime-image-reference').value,'rundesk-news2douyin:exa-agent-reach');
 get(panel,'runtime-image-version').value='0.144.6';await find(panel,'准备镜像').click();assert(find(panel,'取消准备'));
 assert(timers.size>0);await find(panel,'取消准备').click();assert(!find(panel,'取消准备'));assert(find(panel,'用原配置重试'));
 await find(panel,'用原配置重试').click();await flush();assert.equal(jobs[0].status,'queued');
 dialog.emit('close');assert.equal(timers.size,0);assert.equal(jobs[0].status,'queued','closing wizard canceled background work');
 jobs[0].status='succeeded';jobs[0].imageId='sha256:fixture';jobs[0].offlineReady=true;
 const reopened=new E('dialog'),next=context.window.RunDeskRuntimeSetup.panel(reopened);await flush();assert(next.textContent.includes('公网搜索和真实研究尚未验证'));assert(next.textContent.includes('sha256:fixture'));
 reopened.emit('close');console.log('Setup image UI: load without install, background submit, cancel, retry, close, reopen and readiness distinction passed.');
})().catch(e=>{console.error(e);process.exitCode=1});
