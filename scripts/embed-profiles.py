#!/usr/bin/env python3
"""Legacy non-destructive recipe validator/exporter; canonical files live in recipes/."""
import argparse
from pathlib import Path
import shutil
import tempfile
import recipes


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="Validate canonical files without modifying them")
    parser.add_argument("--output-dir", type=Path, help="Export complete contexts outside the canonical source tree")
    parser.add_argument("--codex-version", default="0.0.0", help="Exact Codex release; 0.0.0 exports templates only")
    args = parser.parse_args()
    profiles = [recipes.load_profile(name) for name in recipes.PROFILE_IDS]
    generic = recipes.ROOT / "generic"
    for name in ("Dockerfile", ".dockerignore", "build.sh", "seed/config.toml", "tools/install-codex.sh", "tools/configure-apt.sh"):
        if not (generic / name).is_file() or (generic / name).is_symlink():
            raise SystemExit("Missing or unsafe generic recipe: " + name)
    if args.check and args.output_dir:
        parser.error("--check cannot write --output-dir")
    if args.output_dir:
        output = args.output_dir.absolute()
        source = recipes.ROOT.resolve()
        effective = output.resolve(strict=False)
        if output.exists() or output.is_symlink() or effective == source or source in effective.parents or effective in source.parents:
            parser.error("Choose a new output directory outside recipes/")
        output.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.TemporaryDirectory(prefix="rundesk-recipe-export-", dir=output.parent) as tmp:
            staging = Path(tmp) / "recipes"; staging.mkdir()
            for profile in profiles:
                target = staging / profile["id"]; target.mkdir()
                selection = recipes.resolve(profile)
                recipes.render(profile, selection, args.codex_version, recipes.default_base_image(profile, selection), target)
            shutil.copytree(generic, staging / "generic")
            staging.rename(output)
    print("Canonical recipes validated; source files were not modified.")


if __name__ == "__main__":
    main()
