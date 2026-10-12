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
for route, method in [('/backends', 'get'), ('/setup/environment', 'get'), ('/setup/codex-install', 'post')]:
    operation = spec['paths'][route][method]
    assert operation['x-administrator-only'] is True, route
install = spec['paths']['/setup/codex-install']['post']
assert any(p['name'] == 'Idempotency-Key' and p['required'] for p in install['parameters'])
assert not any(p.name.startswith(('RELEASE-', 'KUN-DESIGN-', 'CAPABILITY-STATUS-')) for p in (root / 'docs').glob('*.md'))
if errors:
    raise SystemExit('\n'.join(errors))
print(json.dumps({'passed': True, 'version': version, 'documents': len(files), 'paths': len(spec['paths']), 'checks': ['local links', 'version', 'startup admin scope', 'install idempotency', 'no historical release duplicates']}))
