# SPDX-License-Identifier: AGPL-3.0-only
# RunDesk contributors. Deployment changes to the pinned OpenMontage source.
"""Bind native render tools to installed runtimes without runtime downloads."""
import json
from pathlib import Path
import shutil
import sys


def replace_once(text, old, new):
    if text.count(old) != 1:
        raise ValueError("Pinned source differs from the reviewed runtime patch")
    return text.replace(old, new, 1)


def patch(source: Path):
    changed = []

    def save(rel, text):
        path = source / rel
        backup = source / "RUNDESK-ORIGINALS" / rel
        backup.parent.mkdir(parents=True, exist_ok=True)
        if backup.exists():
            raise ValueError("Runtime patch already applied")
        shutil.copy2(path, backup)
        path.write_text("# Modified for RunDesk deployment; see RUNDESK-ORIGINALS and patch_runtime.py.\n" + text)
        changed.append(rel)

    rel = "tools/video/hyperframes_compose.py"
    text = (source / rel).read_text()
    start = text.index("    @classmethod\n    def _resolve_npm_package(cls)")
    end = text.index("    @classmethod\n    def _probe_cli(cls)", start)
    replacement = '''    @classmethod
    def _resolve_npm_package(cls) -> dict[str, str]:
        """Verify the locally installed pinned package without network access."""
        if cls._npm_resolve_cache is None:
            try:
                package = Path("/opt/hyperframes/node_modules/hyperframes/package.json")
                version = json.loads(package.read_text())["version"]
                cls._npm_resolve_cache = ({"version": version} if version == "0.8.87"
                                          else {"error": "Unexpected installed HyperFrames version"})
            except (OSError, ValueError, KeyError):
                cls._npm_resolve_cache = {"error": "Pinned HyperFrames package is not installed"}
        return cls._npm_resolve_cache

'''
    text = text[:start] + replacement + text[end:]
    text = replace_once(text, 'npx = shutil.which("npx")', 'npx = shutil.which("hyperframes")')
    text = replace_once(text, '[npx, "--yes", cls._NPM_PACKAGE, "doctor", "--json"]', '[npx, "doctor", "--json"]')
    text = replace_once(text, 'cmd = ["npx", "--yes", "hyperframes", *args]', 'cmd = ["/usr/local/bin/hyperframes", *args]')
    save(rel, text)

    rel = "tools/video/video_compose.py"
    text = (source / rel).read_text()
    text = replace_once(text, 'cmd = ["npx", "remotion", "render", str(effective_entry), str(comp_id), str(output_path)]', 'cmd = ["npx", "remotion", "render", str(effective_entry), str(comp_id), str(output_path), "--browser-executable=/usr/bin/chromium"]')
    text = replace_once(text, '                f"--props={props_path}",\n            ]', '                f"--props={props_path}",\n                "--browser-executable=/usr/bin/chromium",\n            ]')
    save(rel, text)

    rel = "tools/video/remotion_caption_burn.py"
    text = (source / rel).read_text()
    text = replace_once(text, '        self.run_command(render_cmd, cwd=str(root))', '        render_cmd.append("--browser-executable=/usr/bin/chromium")\n        self.run_command(render_cmd, cwd=str(root))')
    save(rel, text)
    (source / "RUNDESK-RUNTIME-PATCHES.json").write_text(json.dumps({"schemaVersion": 1, "files": changed, "nativePipelinesUnchanged": True}, indent=2) + "\n")
    return changed


if __name__ == "__main__":
    patch(Path(sys.argv[1]))
