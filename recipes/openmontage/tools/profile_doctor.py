#!/usr/bin/env python3
"""Offline checks for independent upstream environments; never calls generation."""
import argparse
import json
from pathlib import Path
import subprocess
import sys


def run(argv, timeout=90):
    try:
        p = subprocess.run(argv, capture_output=True, text=True, timeout=timeout)
        return {"ok": p.returncode == 0, "exitCode": p.returncode, "output": (p.stdout + p.stderr)[-8000:]}
    except (OSError, subprocess.TimeoutExpired) as error:
        return {"ok": False, "error": str(error)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--json", action="store_true")
    parser.add_argument("--manifest", default="/opt/rundesk-profile/profile.lock.json")
    args = parser.parse_args()
    config = json.loads(Path(args.manifest).read_text())
    profile, upstream = config["profile"], config["upstream"]
    root = Path("/opt/upstreams") / profile["id"]
    checks = []
    for argv in (["codex", "--version"], ["codex", "app-server", "--help"], ["node", "--version"], ["python3", "--version"]):
        row = {"name": " ".join(argv), **run(argv)}
        if argv == ["codex", "--version"] and row["ok"]:
            row["ok"] = config["codexVersion"] in row["output"].split()
        checks.append(row)
    for rel in upstream["requiredFiles"]:
        checks.append({"name": "upstream-file:" + rel, "ok": (root / rel).is_file()})
    for row in upstream["skillDirectories"]:
        path = Path("/etc/codex/skills") / row["name"]
        expected = root if row["source"] == "." else root / row["source"]
        checks.append({"name": "native-skill:" + row["name"], "ok": path.is_symlink() and path.resolve() == expected.resolve() and (path / "SKILL.md").is_file()})
    checks.append({"name": "integration-skill", "ok": (Path("/etc/codex/skills") / profile["skill"] / "SKILL.md").is_file()})
    checks.append({"name": "video-app-media-bridge", **run([sys.executable, "/opt/rundesk-profile/tools/video_app_media.py", "--version"])})
    checks.append({"name": "native-upstream-runtime", **run([sys.executable, "/opt/rundesk-upstream/doctor.py"], timeout=360)})
    result = {"schemaVersion": 1, "profileId": profile["id"], "upstreamCommit": upstream["commit"], "ready": all(x["ok"] for x in checks), "checks": checks, "modelTested": False, "networkTested": False, "scope": "offline native tools, source, skills and bridge; no MCP service handshake or generation"}
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if result["ready"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
