#!/usr/bin/env python3
"""Docker CLI fixture for UI tests. No actual container/daemon/model is used."""
import fcntl, hashlib, json, os, pathlib, sys
root=pathlib.Path(os.environ['DOCKER_CONFIG']);root.mkdir(parents=True,exist_ok=True)
args=sys.argv[1:]
if args[:1]==['--host']:args=args[2:]
with (root/'calls.jsonl').open('a') as f:f.write(json.dumps(args)+'\n')
if (root/'offline').exists():print('Fixture: Docker daemon unavailable',file=sys.stderr);sys.exit(1)
lock=(root/'lock').open('a+');fcntl.flock(lock,fcntl.LOCK_EX)
file=root/'state.json';state=json.loads(file.read_text()) if file.exists() else {}
def finish(value=None):
 file.write_text(json.dumps(state));lock.close()
 if value is not None:print(json.dumps(value) if not isinstance(value,str) else value)
 sys.exit(0)
if args[0]=='version':finish({'Version':'fixture-only','Os':'linux','Arch':'amd64'})
if args[0]=='info':finish(['name=seccomp'])
if args[:2]==['image','inspect']:
 ref=args[2];meta_path=root/'images.json';meta=json.loads(meta_path.read_text()) if meta_path.exists() else {}
 if ref in meta.get('missing',[]):print('Fixture image missing: '+ref,file=sys.stderr);sys.exit(1)
 image=ref if ref.startswith('sha256:') else meta.get('tags',{}).get(ref,'sha256:'+hashlib.sha256(ref.encode()).hexdigest())
 labels={'org.opencontainers.image.version':'fixture-'+image[-4:],'org.opencontainers.image.source':'https://example.test/news-source','org.opencontainers.image.revision':image[-12:],'io.rundesk.codex.version':'fixture-only','io.rundesk.capabilities.tools':'Python 3, Git','io.rundesk.capabilities.skills':'news-research','io.rundesk.capabilities.mcp':'search-provider','io.rundesk.build.dockerfile':'examples/docker/Dockerfile'}
 finish([{'Id':image,'Os':'linux','Architecture':'amd64','Size':230686720,'Created':'2026-10-01T12:00:00Z','RepoDigests':['registry.example/news@sha256:'+image[7:]],'Config':{'Labels':labels,'Env':['API_KEY=hidden-fixture']}}])
if args[:2]==['container','ls']:
 name=next((s[7:-1] for s in args if s.startswith('name=^/')),None);running='status=running' in args
 finish('\n'.join(c['Id'] for n,c in state.items() if (not name or n==name) and (not running or c['State']['Status']=='running')))
if args[:2]==['container','create']:
 name=args[args.index('--name')+1];labels={};env={};mounts={}
 for a,b in zip(args,args[1:]):
  if a=='--label':k,v=b.split('=',1);labels[k]=v
  if a=='--env':k,v=b.split('=',1);env[k]=v
  if a=='--mount':parts=dict(p.split('=',1) for p in b.split(',') if '=' in p);mounts[parts['dst']]=parts['src']
 state[name]={'Id':'fixture-'+name,'Image':args[-2],'Config':{'Labels':labels},'State':{'Status':'created','ExitCode':0,'OOMKilled':False},'env':env,'mounts':mounts,'workdir':args[args.index('--workdir')+1]};finish(state[name]['Id'])
if args[:2]==['container','inspect']:finish([state[args[2]]])
if args[:2]==['container','start']:state[args[2]]['State']['Status']='running';finish(args[2])
if args[:2]==['container','stop']:state[args[-1]]['State']['Status']='exited';finish(args[-1])
if args[:2]==['container','rm']:del state[args[2]];finish(args[2])
if args[0]=='exec':
 name=next(a for a in args if a in state);c=state[name];command=args[args.index(name)+1:];lock.close();os.environ.update(c['env']);os.chdir(c['workdir']);helper=c['mounts']['/opt/rundesk/bin/rundesk']
 if command[1]=='__container_probe':sys.exit(0)
 if command[1]=='__container_seed':os.execv(helper,[helper,'__container_seed',command[2]])
 if command[1]=='__container_exec':os.execv(helper,[helper,'__container_exec',command[2],helper,'__demo_agent'])
raise SystemExit('Unsupported fixture call: '+repr(args))
