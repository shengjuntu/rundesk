// Real setup view with a DOM fixture: interaction contracts, not visual layout.
const assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm'),path=require('node:path');
class Element{
 constructor(tag,attrs={},...children){this.tagName=tag.toUpperCase();this.attrs=attrs;Object.assign(this,attrs);this.children=[];this.listeners={};this.dataset={};this.value=attrs.value??'';this.append(...children);}
 append(...children){for(const c of children.flat().filter(n=>n!=null)){if(c instanceof Element)c.parent=this;this.children.push(c);}}
 replaceChildren(...children){this.children=[];this.append(...children);}
 set textContent(v){this.children=[v]}get textContent(){return this.children.map(n=>n instanceof Element?n.textContent:String(n??'')).join('')}
 addEventListener(type,fn){this.listeners[type]=fn}emit(type){return this.listeners[type]?.({preventDefault(){}})}
 showModal(){this.open=true}close(){this.open=false;this.emit('close')}remove(){if(this.parent)this.parent.children=this.parent.children.filter(x=>x!==this)}
}
const el=(...a)=>new Element(...a),button=(label,click)=>{const b=el('button',{},label);b.click=click;return b};
const all=n=>[n,...n.children.filter(c=>c instanceof Element).flatMap(all)],get=(n,id)=>all(n).find(x=>x.id===id),byText=(n,s)=>all(n).find(x=>x.tagName==='BUTTON'&&x.textContent===s);
const root=path.resolve(__dirname,'..'),clone=x=>JSON.parse(JSON.stringify(x));
function fixture(){
 const storage=new Map(),body=el('body'),select=el('select',{value:'work'}),requests=[];
 let inst={id:'default',revision:2,defaultModel:'',agentRuntime:{kind:'codex',maxSteps:12,allowBash:false,harness:{loopPolicy:'plan-act-v1'},budget:{maxToolCalls:8}},execution:{mode:'local'}};
 let setup={settings:{revision:0,completed:false,workspaceId:'work'},instance:inst,codex:'absent-codex',user:'fixture',data:'/tmp/fixture',firstRun:true};
 let environment={ready:false,demo:false,checks:[{id:'codex',required:true,status:'unavailable',detail:'missing'}]},pendingInstall=null;
 const state={workspaces:[{id:'work',name:'Fixture'}],instances:[inst]};
 const api=async(url,options={})=>{requests.push({url,options:clone(options)});if(url==='/setup'&&options.method==='PUT'){assert.equal(options.body.instanceRevision,inst.revision);inst={...inst,revision:inst.revision+1,agentRuntime:options.body.agentRuntime};state.instances=[inst];setup={...setup,settings:{...setup.settings,revision:setup.settings.revision+1,completed:options.body.completed},instance:inst};environment={ready:true,demo:false,checks:[{id:'kun',required:true,status:'ready',detail:'fixture'}]};return {settings:setup.settings};}if(url==='/setup')return clone(setup);if(url.startsWith('/setup/environment'))return clone(environment);if(url==='/instances')return clone(state.instances);if(url==='/setup/codex-install')return new Promise(resolve=>pendingInstall=resolve);throw Error('unexpected '+url)};
 const context=vm.createContext({window:{},document:{body,documentElement:{lang:'zh'}},el,button,api,state,rdText:s=>s,json:JSON.stringify,ws:()=>state.workspaces[0],$:()=>select,updateWorkspaceLabel(){},toast(){},encodeURIComponent,sessionStorage:{getItem:k=>storage.get(k),setItem:(k,v)=>storage.set(k,v)},localStorage:{setItem(){}},prompt(){throw Error('unexpected prompt')}});
 vm.runInContext(fs.readFileSync(root+'/internal/web/setup.js','utf8'),context);
 return {context,body,requests,state,storage,resolveInstall:value=>pendingInstall(value)};
}
(async()=>{
 const f=fixture();await f.context.window.RunDeskSetup.maybeOpen();let dialog=f.body.children[0];assert(dialog.open,'missing dependency did not open wizard');
 assert(f.requests.some(x=>x.url.startsWith('/setup/environment')),'startup did not check environment');assert(!f.requests.some(x=>x.options.method==='POST'),'startup ran an action');
 const selector=get(dialog,'setup-runtime');selector.value='kun';selector.emit('change');assert(get(dialog,'setup-install').hidden,'Kun still shows Codex installation');
 get(dialog,'setup-model').value='local-model';get(dialog,'setup-endpoint').value='http://localhost:9000/v1';
 await byText(dialog,'保存配置').click();const save=f.requests.find(x=>x.options.method==='PUT').options.body;
 assert.equal(save.agentRuntime.kind,'kun');assert.equal(save.agentRuntime.model,'local-model');assert.equal(save.agentRuntime.maxSteps,12);assert.equal(save.agentRuntime.harness.loopPolicy,'plan-act-v1');assert.equal(save.agentRuntime.budget.maxToolCalls,8);assert.equal(save.codex,'absent-codex');
 await byText(dialog,'完成设置').click();assert.equal(f.requests.filter(x=>x.options.method==='PUT').at(-1).options.body.instanceRevision,3);assert.equal(f.body.children.length,0);
 const g=fixture();await g.context.window.RunDeskSetup.open();dialog=g.body.children[0];
 await byText(dialog,'安装指定版本').click();assert(!g.requests.some(x=>x.url==='/setup/codex-install'),'unchecked installation ran');
 get(dialog,'setup-install-version').value='1.2.3';get(dialog,'setup-install-confirm').checked=true;
 const install=byText(dialog,'安装指定版本').click();await Promise.resolve();assert(byText(dialog,'安装指定版本').disabled);await byText(dialog,'完成设置').click();assert(!g.requests.some(x=>x.options.method==='PUT'),'saved during installation');
 g.resolveInstall({ok:true,codex:'/managed/codex',log:'fixture log',error:''});await install;assert.equal(get(dialog,'setup-codex').value,'/managed/codex');assert(!g.requests.some(x=>x.options.method==='PUT'),'installation selected itself');
 await byText(dialog,'关闭').click();await g.context.window.RunDeskSetup.maybeOpen();assert.equal(g.body.children.length,0,'dismissed wizard reopened');
 const proof={version:fs.readFileSync(root+'/internal/app/api_v1.go','utf8').match(/const Version = "([^"]+)"/)[1],passed:true,scope:'real setup JS in Node DOM fixture; browser layout not tested',checks:['startup prerequisite probe without installation/model calls','Kun selection hides optional Codex install','saved configuration preserves advanced Kun settings and carries both revisions','unchecked installation is inert','save blocked during install','installed executable requires explicit save','dismissal respected']};
 fs.mkdirSync(root+'/docs/validation/current',{recursive:true});fs.writeFileSync(root+'/docs/validation/current/setup-ui.json',JSON.stringify(proof,null,2)+'\n');console.log(JSON.stringify(proof));
})().catch(e=>{console.error(e);process.exitCode=1});
