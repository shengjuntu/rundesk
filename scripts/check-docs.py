#!/usr/bin/env python3
"""Check current documentation links and the generated startup/API contract."""
import json
from pathlib import Path
import re

root = Path(__file__).resolve().parents[1]
errors = []
files = [root / 'README.md', root / 'README.zh-CN.md', *sorted((root / 'docs').rglob('*.md')), *sorted((root / 'examples').rglob('*.md'))]
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
for route, method in [
    ('/backends', 'get'), ('/setup/environment', 'get'),
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
