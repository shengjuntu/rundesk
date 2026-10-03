#!/usr/bin/env python3
"""Real Codex App Server MCP handshake/tool calls, no model inference or credentials."""
import argparse,json,os,queue,sqlite3,subprocess,tempfile,threading,time
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--codex',default='/opt/codex/bin/codex');p.add_argument('--binary',default='bin/rundesk');a=p.parse_args()
checks=[]
with tempfile.TemporaryDirectory(prefix='rundesk-native-trace-') as td:
 root=Path(td);(root/'codex').mkdir();(root/'work').mkdir();db=root/'state.db';c=sqlite3.connect(db);c.executescript('CREATE TABLE objects(kind TEXT,id TEXT,data BLOB,PRIMARY KEY(kind,id)); CREATE TABLE events(id INTEGER PRIMARY KEY,session TEXT,time TEXT,direction TEXT,method TEXT,data BLOB);');c.execute('INSERT INTO objects VALUES(?,?,?)',('session','source',json.dumps({'id':'source'})))
 for i,method,data in [(1,'run/input',{'runId':'r','input':{'text':'Check a public profile'}}),(2,'item/started',{'params':{'turnId':'t','item':{'id':'tool','type':'mcpToolCall','server':'social','tool':'query','arguments':{'query':'a profile'}}}}),(3,'item/completed',{'params':{'turnId':'t','item':{'id':'tool','type':'mcpToolCall','status':'completed','result':{'results':[]}}}}),(4,'turn/completed',{'params':{'turn':{'id':'t','status':'completed'}}})]:c.execute('INSERT INTO events VALUES(?,?,?,?,?,?)',(i,'source',f'2026-10-03T10:00:0{i}Z','in',method,json.dumps(data)))
 c.commit();c.close();before=db.read_bytes()
 env=dict(os.environ,CODEX_HOME=str(root/'codex'));env.pop('CODEX_SQLITE_HOME',None)
 proc=subprocess.Popen([a.codex,'app-server'],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,env=env);messages=queue.Queue();errors=[]
 threading.Thread(target=lambda:[messages.put(json.loads(line)) for line in proc.stdout if line.strip()],daemon=True).start();threading.Thread(target=lambda:[errors.append(line) for line in proc.stderr],daemon=True).start();counter=0
 def rpc(method,params):
  global counter
  counter+=1;rid=counter;proc.stdin.write(json.dumps({'id':rid,'method':method,'params':params})+'\n');proc.stdin.flush();deadline=time.monotonic()+35
  while time.monotonic()<deadline:
   try:msg=messages.get(timeout=max(.1,deadline-time.monotonic()))
   except queue.Empty:break
   if msg.get('id')==rid:
    if 'error'in msg:raise RuntimeError(json.dumps(msg['error']))
    return msg['result']
  raise RuntimeError('Timed out: '+method+'\n'+''.join(errors[-15:]))
 try:
  rpc('initialize',{'clientInfo':{'name':'rundesk-native-trace-test','version':'0.8.0'},'capabilities':{'experimentalApi':True}});proc.stdin.write('{"method":"initialized"}\n');proc.stdin.flush()
  config={'mcp_servers.rundesk_trace':{'command':str(Path(a.binary).resolve()),'args':['__trace_mcp',str(db),'source','4'],'enabled':True,'required':True,'startup_timeout_sec':15,'tool_timeout_sec':30}}
  thread=rpc('thread/start',{'cwd':str(root/'work'),'sandbox':'read-only','approvalPolicy':'on-request','config':config})['thread']['id'];checks.append('native thread/start accepts per-thread MCP override without writing config.toml')
  rpc('config/mcpServer/reload',{});checks.append('the normal pre-turn MCP reload retains the thread-scoped trace server')
  status=rpc('mcpServerStatus/list',{'threadId':thread,'detail':'full'});servers=status.get('data',[]);found=next(x for x in servers if x.get('name')=='rundesk_trace');assert len(found.get('tools',{}))==5,found;checks.append('real Codex discovers all five read-only trace tools over stdio')
  res=rpc('mcpServer/tool/call',{'threadId':thread,'server':'rundesk_trace','tool':'trace_find_steps','arguments':{'runId':'r','type':'mcpToolCall'}});assert 'results' in json.dumps(res) and 'step-2' in json.dumps(res),res;checks.append('Codex routes a typed tool call to the source snapshot and returns retained arguments and empty results')
  res=rpc('mcpServer/tool/call',{'threadId':thread,'server':'rundesk_trace','tool':'trace_read_event','arguments':{'eventId':5}});assert 'outside the source snapshot' in json.dumps(res),res;checks.append('out-of-snapshot event access fails through the real MCP client')
  assert not (root/'codex'/'config.toml').exists();assert db.read_bytes()==before;checks.append('source database bytes and persistent Codex configuration remain unchanged')
  normal=rpc('thread/start',{'cwd':str(root/'work'),'sandbox':'read-only','approvalPolicy':'on-request'})['thread']['id'];status=rpc('mcpServerStatus/list',{'threadId':normal});assert not any(x.get('name')=='rundesk_trace' for x in status.get('data',[]));checks.append('a separate ordinary thread does not inherit the analysis MCP server')
  report={'codex':subprocess.check_output([a.codex,'--version'],text=True).strip(),'mode':'real App Server and MCP; no inference turn sent','passed':len(checks),'checks':checks};Path('docs/native-trace-validation-0.8.0.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n');print(json.dumps(report,ensure_ascii=False))
 finally:
  proc.terminate()
  try:proc.wait(timeout=8)
  except subprocess.TimeoutExpired:proc.kill();proc.wait()
