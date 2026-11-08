#!/usr/bin/env python3
"""Read-only upstream HyperFrames checks; no network, models or render."""
import json
from pathlib import Path
import shutil
import subprocess
import sys


def main():
    checks = []

    def check(name, passed, detail=""):
        checks.append({"name": name, "passed": bool(passed), "detail": detail})

    for name in ("node", "hyperframes", "chromium", "ffmpeg", "ffprobe"):
        check("command:" + name, shutil.which(name) is not None)
    for package, expected in (("hyperframes", "0.8.87"), ("gsap", "3.15.0")):
        path = Path("/opt/hyperframes/node_modules") / package / "package.json"
        try:
            actual = json.loads(path.read_text())["version"]
            check("package:" + package, actual == expected, actual)
        except (OSError, ValueError, KeyError):
            check("package:" + package, False, "missing or invalid package metadata")
    source = Path("/opt/upstreams/hyperframes")
    check("upstream:license", (source / "LICENSE").is_file())
    for path in sorted((source / "skills").glob("*/SKILL.md")):
        check("skill:" + path.parent.name, (Path("/etc/codex/skills") / path.parent.name / "SKILL.md").is_file())
    check("upstream:skill-count", len(list((source / "skills").glob("*/SKILL.md"))) == 21)
    check("integration:skill", Path("/etc/codex/skills/rundesk-hyperframes/SKILL.md").is_file())
    if shutil.which("node"):
        result = subprocess.run(["node", "-e", 'process.stdout.write(process.versions.node)'], text=True, capture_output=True, timeout=10)
        actual = result.stdout.strip()
        check("node:minimum", result.returncode == 0 and actual.split(".")[0].isdigit() and int(actual.split(".")[0]) >= 22, actual)
    passed = all(c["passed"] for c in checks)
    print(json.dumps({"passed": passed, "profile": "hyperframes", "upstreamCommit": "82ae73a03156f782e48d631907db08232aeed817", "checks": checks, "scope": "offline installation; no Docker build, MCP/model call, media generation or video render"}))
    return 0 if passed else 1


if __name__ == "__main__":
    sys.exit(main())
