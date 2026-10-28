#!/usr/bin/env python3
"""Generate/check external recipe directories (legacy command name; no Go embedding).

This developer command refreshes distribution defaults under recipes/. Runtime
builds read those files directly and never invoke this generator. Keep customized
recipes in a separate RUNDESK_RECIPES_DIR before refreshing release defaults.
"""
import argparse
from pathlib import Path
import shutil
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'examples'))
import profiles


def files(directory):
    return {p.relative_to(directory).as_posix(): p.read_bytes() for p in directory.rglob('*') if p.is_file()}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    parser.add_argument('--output-dir', type=Path, default=ROOT / 'recipes')
    args = parser.parse_args()
    for name in profiles.PROFILE_IDS:
        with tempfile.TemporaryDirectory() as tmp:
            context = Path(tmp)
            profile = profiles.load_profile(name)
            profiles.render(profile, profiles.resolve(profile), '0.0.0', 'node:22-bookworm-slim', context)
            path = args.output_dir / name
            if args.check:
                if not path.is_dir() or files(path) != files(context):
                    raise SystemExit('Stale external recipe: ' + name + '; run scripts/embed-profiles.py')
            else:
                path.parent.mkdir(parents=True, exist_ok=True)
                if path.exists():
                    shutil.rmtree(path)
                shutil.copytree(context, path)
    # The generic download template uses the same external-files deployment.
    # It is not another application profile and is not listed in the catalog.
    generic = args.output_dir / 'generic'
    expected = {}
    for name in ('Dockerfile', '.dockerignore', 'README.md', 'build.sh', 'seed/config.toml'):
        expected[name] = (ROOT / 'examples/docker' / name).read_bytes()
    expected['tools/configure-apt.sh'] = (ROOT / 'examples/_shared/configure-apt.sh').read_bytes()
    if args.check:
        if not generic.is_dir() or files(generic) != expected:
            raise SystemExit('Stale external generic template; run scripts/embed-profiles.py')
    else:
        if generic.exists():
            shutil.rmtree(generic)
        for name, content in expected.items():
            target = generic / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(content)
            target.chmod(0o755 if name.endswith('.sh') else 0o644)
    print('External recipe directories match the CLI compiler.' if args.check else 'External recipe directories generated.')


if __name__ == '__main__':
    main()
