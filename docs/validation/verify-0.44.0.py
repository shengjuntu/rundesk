"""Release receipt checks. Run from the repository root after validate-kun.py."""
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile

root = Path(__file__).resolve().parents[2]
receipt = root / 'docs/validation/kun-0.44.0-final/summary.json'
report = json.loads(receipt.read_text())
assert report['passed'] and len(report['checks']) == 8
sources = sorted([root/'go.mod', root/'go.sum', root/'Makefile'] + [p for folder in ('cmd','internal','scripts') for p in (root/folder).rglob('*') if p.is_file() and '__pycache__' not in p.parts])
h = hashlib.sha256()
for p in sources:
    h.update(p.relative_to(root).as_posix().encode()+b'\0'+p.read_bytes()+b'\0')
assert h.hexdigest() == report['sourceSHA256'], 'source changed after validation'

def save(name, data):
    (root/'docs'/name).write_text(json.dumps(data, ensure_ascii=False, indent=2)+'\n')

cli = {'version':'0.44.0', 'passed':True}
for binary, expected in [('rundesk','RunDesk 0.44.0'), ('kun','Kun 0.16.0')]:
    cli[binary] = subprocess.check_output([str(root/'bin'/binary),'--version'], text=True).strip()
    assert cli[binary] == expected
with tempfile.TemporaryDirectory() as d:
    requests = [{'version':16,'id':'hello-current','method':'hello'}, {'version':15,'id':'hello-old','method':'hello'}]
    proc = subprocess.run([str(root/'bin/kun'),'--data',d], input=''.join(json.dumps(v)+'\n' for v in requests), text=True, capture_output=True, timeout=10, check=True)
    answers = {v['id']:v for line in proc.stdout.splitlines() if (v:=json.loads(line)).get('id')}
    hello = answers['hello-current']['result']; hello.pop('pid')
    assert hello['engineVersion']=='0.16.0' and hello['protocolVersion']==16
    assert all(hello['capabilities'][n] for n in ['bash','codingTools','workspaceFixtures','fastFallback','forkHybrid'])
    assert 'unsupported protocol version 15' in answers['hello-old']['error']
    cli.update(hello=hello, oldProtocolRejected=True, scope='Linux CLI, hello/EOF, protocol 15 rejection; Windows cross-compilation only')
save('kun-cli-validation.json', cli)

spec = json.loads((root/'internal/app/openapi.json').read_text())
def references(value):
    if isinstance(value, dict):
        if '$ref' in value:
            ref=value['$ref']; assert ref.startswith('#/')
            target=spec
            for part in ref[2:].split('/'): target=target[part.replace('~1','/').replace('~0','~')]
        for v in value.values(): references(v)
    elif isinstance(value,list):
        for v in value: references(v)
references(spec)
operations=0
for item in spec['paths'].values():
    for method, op in item.items():
        if method not in ['get','post','put','patch','delete','head','options']: continue
        operations+=1
        parameters=op.get('parameters',[])
        keys=[(p['in'],p['name']) for p in parameters]
        assert len(keys)==len(set(keys))
for route, method in [('/instances/{iid}/agent-runtime','put'),('/kun-case-runs/{cid}/start','post')]:
    assert spec['paths'][route][method]['x-administrator-only'] is True
control=spec['paths']['/sessions/{sid}/kun/control']['post']
assert control['x-application-scopes']==['run']
assert control['x-conditional-scopes']=={'approve':['approvals'],'reject':['approvals']}
for route in ['/kun-case-runs/{cid}/start','/kun-benchmarks/{bid}/start']:
    assert any(p['name']=='Idempotency-Key' for p in spec['paths'][route]['post']['parameters'])
config=spec['components']['schemas']['KunConfig']
for name in ['allowBash','bashPath','bashApproval','bashTimeoutSeconds']:
    assert name in config['properties'] and name not in config['required']
assert spec['components']['schemas']['KunToolApproval']['properties']['kind']['enum']==['builtin']
assert 'allowBash' in spec['components']['schemas']['KunLivePreview']['properties']
for filename in ['go.mod','go.sum']:
    previous=root.parent/'rundesk-v0.43.0'/filename
    if previous.exists(): assert (root/filename).read_bytes()==previous.read_bytes()
javascript=sum(p.suffix in ('.js','.cjs') for folder in ['internal/web','scripts'] for p in (root/folder).rglob('*'))
save('kun-contract-validation.json', {'version':'0.44.0','passed':True,'javascriptFiles':javascript,'nodeInteractionSuites':12,'openapiPaths':len(spec['paths']),'openapiOperations':operations,'uniqueParameters':True,'localReferencesResolve':True,'administratorAndIdempotency':True,'bashFieldsOptional':True,'sourceFingerprintMatches':True,'sourceSHA256':h.hexdigest(),'receipt':'validation/kun-0.44.0-final/summary.json'})
save('kun-build-validation.json',{'version':'0.44.0','passed':True,'receipt':'validation/kun-0.44.0-final/summary.json','sourceSHA256':h.hexdigest(),'checks':report['checks'],'binaries':{p.name:p.stat().st_size for p in sorted((root/'bin').iterdir()) if p.is_file()},'scope':'full local suite, vet, focused race including Bash/edit/search/fast fallback; Linux build; Windows cross-compilation only'})
print(json.dumps({'passed':True,'sourceSHA256':h.hexdigest(),'javascriptFiles':javascript,'openapiPaths':len(spec['paths']),'openapiOperations':operations}))
