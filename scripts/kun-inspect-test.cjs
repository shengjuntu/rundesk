// Node regression for event association and read-only inspector behavior.
// This is not browser/layout acceptance. No third-party DOM dependency needed.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
class Element{
 constructor(tag,attrs={},...children){this.tag=tag;this.attrs=attrs;this.children=[];this.disabled=false;this.append(...children);}
 append(...children){this.children.push(...children.flat());}
 replaceChildren(...children){this.children=[];this.append(...children);}
 set textContent(value){this.children=[value];}
 get textContent(){return this.children.map(c=>c instanceof Element?c.textContent:String(c??'')).join('');}
}
const el=(...args)=>new Element(...args),button=(label,fn)=>{const b=el('button',{},label);b.click=fn;return b;};
const pending=[];const api=url=>new Promise((resolve,reject)=>pending.push({url,resolve,reject}));
const context=vm.createContext({el,button,api,console});
vm.runInContext(fs.readFileSync(path.join(__dirname,'../internal/web/kun-inspect.js'),'utf8'),context);
const event=(sequence,runId,kind,data)=>({id:sequence,method:'kun/'+kind,data:{sequence,runId,sessionId:'session',revision:sequence,type:'kun/'+kind,data}});
const records=[
 event(1,'a','model.started',{step:1,request:{model:'fixture',messages:[{role:'user',content:'<script>literal input</script>'}],tools:[]}}),
 event(2,'a','tool.completed',{step:1,call:{id:'same',function:{name:'write_file',arguments:'{}'}},status:'rejected',isError:true,output:'not dispatched'}),
 event(3,'a','model.completed',{step:1,message:{content:'new answer'},usage:{total_tokens:9}}),
 event(4,'b','model.started',{step:1,request:{model:'other'}}),
 event(5,'a','tool.started',{step:2,call:{id:'same',function:{name:'write_file',arguments:'{}'}}}),
 event(6,'a','mcp.request',{server:'one',exchangeId:1,method:'tools/list'}),
 event(7,'a','mcp.response',{server:'one',exchangeId:1,method:'tools/list',result:{tools:[]}}),
 event(8,'b','mcp.request',{server:'one',exchangeId:1,method:'tools/list'}),
 event(9,'a','mcp.request',{server:'two',exchangeId:1,method:'tools/list'}),
];
const rows=context.kunCallRows(records);
assert.equal(rows.length,7,'run/step/server collisions were incorrectly merged');
assert.equal(rows[0].start.data.sequence,1);assert.equal(rows[0].end.data.sequence,3);
const early=context.kunRenderCall(rows[0],1).textContent;
assert(!early.includes('new answer'),'fixed request snapshot leaked a later response');assert(early.includes('结果未记录'));
assert(context.kunRenderCall(rows[0],3).textContent.includes('new answer'));
assert(context.kunRenderCall(rows[1],2).textContent.includes('未保留 / 未派发'),'validation rejection falsely implied dispatch');
assert.equal(context.kunCallStatus(rows[3]),'结果未记录');
const snapshot={sequence:1,state:{runId:'a',revision:1,messages:[{role:'assistant',content:'wrong state message'}],skills:[]}};
const actual=context.kunRenderContext(records[0],snapshot,null);
assert(actual.textContent.includes('实际模型请求'));assert(actual.textContent.includes('<script>literal input</script>'));
const treeWalk=(node,predicate)=>{if(predicate(node))return node;for(const child of node.children||[]){if(child instanceof Element){const result=treeWalk(child,predicate);if(result)return result;}}};
assert(!treeWalk(actual,n=>n.tag==='script'),'untrusted message created markup');
const messages=treeWalk(actual,n=>n.attrs.class==='kun-context-messages');
assert(!messages.textContent.includes('wrong state message'),'actual request replaced by snapshot messages');
assert(context.kunRenderContext(records[2],snapshot,null).textContent.includes('状态上下文'));
(async()=>{
 let selected=records[0];
 const view=context.kunDiffView({sid:'session',getSelected:()=>selected});view.update();
 const set=treeWalk(view.node,n=>n.tag==='button'&&n.textContent==='设为比较起点');
 const compare=treeWalk(view.node,n=>n.tag==='button'&&n.textContent==='比较到所选快照');
 set.click();selected=records[2];view.update();const first=compare.click();
 assert.equal(pending[0].url,'/sessions/session/debug/query?kind=diff&fromSequence=1&sequence=3');
 view.update();assert(compare.disabled,'refresh enabled a duplicate comparison while busy');
 // A delayed comparison must not overwrite a newly selected baseline.
 selected=records[3];set.click();pending[0].resolve({data:{from:{sequence:1},to:{sequence:3},changes:[]}});await first;
 const result=treeWalk(view.node,n=>n.attrs.class==='kun-diff-result');assert.equal(result.textContent,'');
 selected=records[3];const second=compare.click();pending[1].resolve({data:{from:{sequence:4,runId:'b',revision:4},to:{sequence:4,runId:'b',revision:4},changes:[],truncated:false}});await second;
 assert(view.node.textContent.includes('脱敏后的状态没有差异'));assert(!compare.disabled);
 console.log('Kun inspector regressions passed: association, snapshot cutoff, literal content, actual context, bounded read-only comparison, stale response.');
})().catch(e=>{console.error(e);process.exitCode=1;});
