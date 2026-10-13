// Exercise the real approval-card handlers; no browser layout claim.
const {readFileSync}=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
class Node {
 constructor(tag,attrs,children){this.tag=tag;Object.assign(this,attrs);this.children=children;this.value=tag==='textarea'?children.join(''):'';}
 append(...children){this.children.push(...children);}
 querySelectorAll(tag){return this.children.flatMap(c=>c instanceof Node?[...(c.tag===tag?[c]:[]),...c.querySelectorAll(tag)]:[]);}
}
const requests=[],context=vm.createContext({URL,JSON,Object,Array,
 rdText:x=>x,json:JSON.stringify,el:(tag,attrs,...children)=>new Node(tag,attrs,children),
 button:(label,click)=>new Node('button',{label,click},[]),api:async(path,options)=>requests.push({path,...options}),
 refreshApprovals:async()=>{},refreshSessions:async()=>{},refreshRuntime:async()=>{}});
vm.runInContext(readFileSync('internal/web/reliability.js','utf8'),context);
context.refreshRuntime=async()=>{};
function card(params){return context.approvalCard({id:'approval-1',request:{method:'mcpServer/elicitation/request',params}},'session-1');}
(async()=>{
 const approval=card({_meta:{codex_approval_kind:'mcp_tool_call',persist:['session','always']},mode:'form',requestedSchema:{type:'object',properties:{}}});
 assert.equal(approval.querySelectorAll('textarea').length,0);
 await approval.querySelectorAll('button').find(n=>n.label==='允许本次').click();
 assert.deepEqual(JSON.parse(JSON.stringify(requests.pop().body.content)),{});
 const form=card({mode:'form',requestedSchema:{type:'object',properties:{reason:{type:'string'}}}});
 assert.equal(form.querySelectorAll('textarea').length,1);
 form.querySelectorAll('textarea')[0].value='{"reason":"user input"}';
 await form.querySelectorAll('button').find(n=>n.label==='允许本次').click();
 assert.equal(requests.pop().body.content.reason,'user input');
 await form.querySelectorAll('button').find(n=>n.label==='拒绝本次').click();
 assert.equal(requests.pop().body.content,null);
 console.log('PASS: empty MCP tool approval needs no JSON; accept sends {}; real forms preserved; decline sends null');
})().catch(e=>{console.error(e);process.exitCode=1;});
