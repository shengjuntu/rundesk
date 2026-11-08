#!/usr/bin/env python3
"""Check current documentation links and the generated startup/API contract."""
import json
from pathlib import Path
import re

root = Path(__file__).resolve().parents[1]
errors = []
files = [root / 'README.md', root / 'README.zh-CN.md', root / 'THIRD_PARTY_NOTICES.md', *[file for directory in ('docs', 'recipes', 'clients', 'deploy') for file in sorted((root / directory).rglob('*.md'))]]
if (root / 'examples').exists():
    errors.append('examples/: obsolete source tree must not be shipped')
for file in files:
    for target in re.findall(r'\]\(([^)]+)\)', file.read_text()):
        if '://' in target or target.startswith(('#', 'mailto:')):
            continue
        target = target.split('#')[0]
        if target and not (file.parent / target).exists():
            errors.append(f'{file.relative_to(root)}: missing {target}')
version = re.search(r'const Version = "([^"]+)"', (root / 'internal/app/api_v1.go').read_text()).group(1)
spec = json.loads((root / 'internal/app/openapi.json').read_text())
assert spec['info']['version'] == version
for name in ['README.md', 'README.zh-CN.md', 'docs/README.md', 'docs/STATUS.md', 'docs/WEBUI.md']:
    text = (root / name).read_text()
    assert re.search(r'RunDesk '+re.escape(version)+r'\b', text), f'{name}: current version missing'
assert re.search(r'^## '+re.escape(version)+r'[:：]', (root/'docs/UPGRADING.md').read_text(), re.M), 'current upgrade notes missing'
maintenance = spec['components']['schemas']
assert maintenance['BuildAssistantRun']['properties']['maintenance']['$ref'].endswith('/BuildMaintenanceView')
assert 'protocolVersion' in maintenance['BuildAssistantRun']['properties']
assert 'sourceInstanceId' in maintenance['TraceOrigin']['properties']
assert 'sourceWorkspaceId' in maintenance['TraceOrigin']['properties']
assert 'purpose' in maintenance['Workspace']['properties']
assert maintenance['DebugStatistics']['properties']['timing']['$ref'].endswith('/DebugTimingSummary')
assert {'startMethod','endMethod'} <= set(maintenance['DebugRunTiming']['properties'])
for path in ['/sessions/{sid}/debug/query','/member/{grantId}/sessions/{sid}/debug/query']:
    sorts=[p for p in spec['paths'][path]['get']['parameters'] if p['name']=='sort']
    assert len(sorts)==1 and sorts[0]['schema']['enum']==['recorded','duration_desc','duration_asc'], path
assert 'docker' in maintenance['SetupDiscoveryService']['properties']['source']['enum']
assert 'containers' in maintenance['SetupDiscovery']['properties']
assert 'recipeName' in maintenance['BuildCenterImage']['properties']['sources']['items']['properties']
assert 'providerMode' in spec['paths']['/build-center/images/{key}/launch']['post']['requestBody']['content']['application/json']['schema']['properties']
assistant_list = spec['paths']['/build-center/assistant-runs']['get']
assert assistant_list['responses']['200']['content']['application/json']['schema']['$ref'].endswith('/BuildAssistantRunList')
assistant_queries = [p for p in assistant_list['parameters'] if p['in'] == 'query']
assert len(assistant_queries) == 1 and assistant_queries[0]['name'] == 'sourceBuildId' and assistant_queries[0]['required'] is True
assert assistant_queries[0]['schema'] == {'type': 'string', 'pattern': '^[a-f0-9]{24}$', 'minLength': 24, 'maxLength': 24}
assert maintenance['BuildAssistantRunList']['required'] == ['items', 'hasMore']
assert maintenance['BuildAssistantRunList']['properties']['items']['maxItems'] == 20
for route, method in [
    ('/backends', 'get'), ('/setup/environment', 'get'),
    ('/setup/codex-provider', 'get'), ('/setup/codex-provider', 'put'),
    ('/instances/{iid}/codex-provider', 'get'), ('/instances/{iid}/codex-provider', 'put'),
    ('/instances/{iid}/codex-provider/test', 'post'),
    ('/setup/discovery', 'get'), ('/setup/discovery', 'post'),
    ('/setup/model-test', 'post'), ('/setup/kun', 'get'), ('/setup/kun', 'post'),
    ('/setup/codex-install', 'post'), ('/setup/codex-installs', 'get'),
    ('/setup/codex-installs', 'post'), ('/setup/codex-installs/{cid}', 'get'),
    ('/setup/repairs', 'get'), ('/setup/repairs', 'post'),
    ('/setup/repairs/{sid}', 'get'), ('/setup/experiences', 'get'),
    ('/setup/experiences', 'post'), ('/setup/complete', 'post'),
    ('/setup/auto-repairs', 'get'), ('/setup/auto-repairs', 'post'),
    ('/setup/public-images', 'get'), ('/setup/public-images', 'put'),
    ('/build-center/sources/defaults', 'get'), ('/build-center/runtime-options', 'get'),
    ('/build-center/assistant-runs', 'get'),
    ('/build-center/publications', 'get'), ('/build-center/publications', 'post'),
    ('/build-center/publications/{id}', 'get'),
    ('/build-center/publications/{id}/log', 'get'),
    ('/build-center/publications/{id}/cancel', 'post'),
    ('/build-center/publications/{id}/check-public', 'post'),
    ('/build-center/images/{key}/publication-readiness', 'get'),
]:
    operation = spec['paths'][route][method]
    assert operation['x-administrator-only'] is True, route
for route in ['/setup/codex-install', '/setup/model-test', '/setup/kun',
              '/setup/codex-installs', '/setup/repairs', '/setup/experiences',
              '/setup/complete', '/setup/auto-repairs', '/build-center/publications',
              '/build-center/publications/{id}/cancel',
              '/build-center/publications/{id}/check-public']:
    operation = spec['paths'][route]['post']
    assert any(p['name'] == 'Idempotency-Key' and p['required'] for p in operation['parameters']), route
for route in ['/setup/codex-provider', '/instances/{iid}/codex-provider']:
    assert any(p['name'] == 'Idempotency-Key' and p['required'] for p in spec['paths'][route]['put']['parameters']), route
task_mcp = spec['paths']['/setup/agent-mcp']['post']
assert task_mcp['x-setup-task-capability-only'] is True
assert task_mcp['security'] == [{'SetupTaskCapability': []}]
def check_refs(value):
    if isinstance(value, dict):
        if '$ref' in value:
            assert value['$ref'].startswith('#/components/schemas/')
            assert value['$ref'].split('/')[-1] in spec['components']['schemas'], value['$ref']
        for child in value.values():
            check_refs(child)
    elif isinstance(value, list):
        for child in value:
            check_refs(child)
check_refs(spec)
assert not any(p.name.startswith(('RELEASE-', 'KUN-DESIGN-', 'CAPABILITY-STATUS-')) for p in (root / 'docs').glob('*.md'))
if errors:
    raise SystemExit('\n'.join(errors))
print(json.dumps({'passed': True, 'version': version, 'documents': len(files), 'paths': len(spec['paths']), 'checks': ['local links', 'version', 'startup admin scope', 'startup idempotency', 'task MCP scope', 'schema references', 'no historical release duplicates']}))
