#!/usr/bin/env python3
"""Copy or verify a pinned, engine-free Kun SDK for unpublished local releases."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil

ROOT = Path(__file__).resolve().parents[1]
TARGET = ROOT / 'third_party/kun-sdk'


def inventory(root):
    return {p.relative_to(root).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in sorted(root.rglob('*')) if p.is_file()
            and not any(x in {'__pycache__', '.git', 'node_modules'} for x in p.parts)
            and p.suffix != '.pyc' and p.name != 'SOURCE.json'}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, help='Independent Kun sdk directory; required to update or compare upstream bytes')
    parser.add_argument('--check', action='store_true', help='Verify manifest and optional exact upstream contents without writing')
    args = parser.parse_args()
    if args.check:
        manifest = json.loads((TARGET/'SOURCE.json').read_text())
        actual = inventory(TARGET)
        if actual != manifest['files']:
            raise SystemExit('Kun SDK snapshot differs from recorded contents')
        if args.source and inventory(args.source.resolve()) != actual:
            raise SystemExit('Kun SDK snapshot differs from independent source')
        print('Kun SDK snapshot matches manifest' + (' and independent source.' if args.source else '.'))
        return
    if not args.source:
        parser.error('--source is required to update the snapshot')
    source = args.source.resolve()
    if (source/'go.mod').read_text().splitlines()[0] != 'module github.com/your-org/kun/sdk':
        raise SystemExit('Expected independent Kun SDK module')
    source_files = inventory(source)
    if any(p.startswith(('internal/engine/', 'cmd/kun/')) for p in source_files):
        raise SystemExit('Execution engine must not enter the host SDK')
    TARGET.mkdir(parents=True, exist_ok=True)
    # Remove only files from the previously generated SDK manifest.
    old = json.loads((TARGET/'SOURCE.json').read_text()).get('files', {}) if (TARGET/'SOURCE.json').exists() else {}
    for rel in old.keys() - source_files.keys():
        p = TARGET/rel
        if p.is_file():
            p.unlink()
    for rel in source_files:
        destination = TARGET/rel
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source/rel, destination)
    manifest = {'module': 'github.com/your-org/kun/sdk', 'version': '0.17.0',
                'source': 'Kun standalone release, sdk/', 'files': source_files}
    (TARGET/'SOURCE.json').write_text(json.dumps(manifest, indent=2, sort_keys=True)+'\n')
    print('Copied engine-free Kun SDK snapshot.')


if __name__ == '__main__':
    main()
