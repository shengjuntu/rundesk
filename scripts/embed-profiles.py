#!/usr/bin/env python3
"""Generate the built-in contexts from the same compiler used by the CLI."""
import argparse
import io
from pathlib import Path
import sys
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'examples'))
import profiles


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    for name in profiles.PROFILE_IDS:
        with tempfile.TemporaryDirectory() as tmp:
            context = Path(tmp)
            profile = profiles.load_profile(name)
            profiles.render(profile, profiles.resolve(profile), '0.0.0', 'node:22-bookworm-slim', context)
            output = io.BytesIO()
            with zipfile.ZipFile(output, 'w', zipfile.ZIP_DEFLATED) as archive:
                for file in sorted(p for p in context.rglob('*') if p.is_file()):
                    entry = zipfile.ZipInfo(file.relative_to(context).as_posix(), (2020, 1, 1, 0, 0, 0))
                    entry.compress_type = zipfile.ZIP_DEFLATED
                    entry.create_system = 3
                    entry.external_attr = (0o100755 if file.stat().st_mode & 0o111 else 0o100644) << 16
                    archive.writestr(entry, file.read_bytes())
            path = ROOT / 'examples/profiletemplates' / (name + '.zip')
            if args.check:
                if not path.exists() or path.read_bytes() != output.getvalue():
                    raise SystemExit('Stale built-in profile: ' + name + '; run scripts/embed-profiles.py')
            else:
                path.parent.mkdir(exist_ok=True)
                path.write_bytes(output.getvalue())
    print('Built-in profiles match the CLI compiler.' if args.check else 'Built-in profiles generated.')


if __name__ == '__main__':
    main()
