const assert=require('node:assert/strict');
const {Model}=require('../internal/web/trace-model.js');
const {inspect,replyRows,links}=require('../internal/web/trace-process.js');
let id=0;const event=(method,data)=>({id:++id,time:new Date(1700000000000+id).toISOString(),method,data});
const m=new Model().ingest([
 event('run/input',{runId:'r',input:{text:'Find a public profile',skills:[{name:'social-research'}]}}),
 event('turn/started',{params:{turn:{id:'t'}}}),
 event('item/started',{params:{turnId:'t',item:{id:'lookup',type:'mcpToolCall',status:'inProgress',server:'search',tool:'query',arguments:{query:'a person',url:'https://input-only.example/'}}}}),
 event('item/completed',{params:{turnId:'t',item:{id:'lookup',type:'mcpToolCall',result:{results:[]}}}}),
 event('item/completed',{params:{turnId:'t',item:{id:'error',type:'mcpToolCall',result:{isError:true,content:[{text:'HTTP 403; could not verify https://example.org/profiles/one'}]}}}}),
 event('item/completed',{params:{turnId:'t',item:{id:'note',type:'agentMessage',phase:'commentary',text:'Searching is complete.'}}}),
 event('turn/completed',{params:{turn:{id:'t',status:'completed'}}})
]);
const lookup=m.rows.get('item-t-lookup'), error=m.rows.get('item-t-error');
assert.equal(lookup.status,'completed');assert.equal(inspect(lookup).empty,true);
assert.match(inspect(lookup).input,/a person/);assert.equal(error.status,'failed');assert.match(inspect(error).error,/403/);
assert.equal(replyRows(m.list()).length,0,'commentary is not presented as a final answer');
assert.equal(links(m.list()).length,1);assert.equal(links(m.list())[0].url,'https://example.org/profiles/one');
assert(!links(m.list()).some(r=>r.url.includes('input-only')));
m.ingest([event('item/completed',{params:{turnId:'t',item:{id:'reply',type:'agentMessage',text:'No verified profile.'}}})]);
assert.equal(replyRows(m.list())[0].detail.text,'No verified profile.');
m.ingest([event('item/completed',{params:{turnId:'t',item:{id:'final',type:'agentMessage',phase:'final_answer',text:'Identity remains unverified.'}}})]);
assert.equal(replyRows(m.list())[0].detail.text,'Identity remains unverified.');
assert.equal(m.runMap.get('r').status,'completed','a tool problem does not invent a failed whole run');
assert.match(m.list().find(r=>r.title==='显式选中 Skills').note,/不代表已经执行/);
assert.equal(links([{track:'tools',type:'mcpToolCall',detail:{result:{url:'https://user:secret@example.org/a'}}}]).length,0);
console.log('PASS: retained inputs, completion status, empty results, MCP errors, phase-aware replies, output-only source links, credential URLs and skill attribution');
