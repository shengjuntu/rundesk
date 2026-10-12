#!/usr/bin/env python3
"""Offline runtime checks. Never installs packages, logs in or calls a model."""
import argparse
import importlib.metadata
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile


def run(argv):
    try:
        p = subprocess.run(argv, capture_output=True, text=True, timeout=20)
        return {"ok": p.returncode == 0, "exitCode": p.returncode, "output": (p.stdout + p.stderr)[:2000]}
    except (OSError, subprocess.TimeoutExpired) as e:
        return {"ok": False, "error": str(e)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--json", action="store_true")
    parser.add_argument("--manifest", default="/opt/rundesk-profile/profile.lock.json")
    args = parser.parse_args()
    config = json.loads(Path(args.manifest).read_text())
    profile = config["profile"]; selection = config["selection"]
    checks = []
    for argv in (["codex", "--version"], ["codex", "app-server", "--help"], ["node", "--version"], ["python3", "--version"]):
        row = {"name": " ".join(argv), **run(argv)}
        if argv == ["codex", "--version"] and row["ok"]:
            row["ok"] = config["codexVersion"] in row["output"].split()
        checks.append(row)
    for package in profile["pythonPackages"] + (["agent-reach"] if selection["agentReach"] else []):
        try:
            installed = importlib.metadata.version(package)
            expected = config["dependencies"]["python"].get(package) or config["dependencies"]["agentReach"]["version"]
            checks.append({"name": package, "ok": installed == expected, "version": installed, "expected": expected})
        except importlib.metadata.PackageNotFoundError:
            checks.append({"name": package, "ok": False, "error": "not installed"})
    if "ffmpeg" in profile["aptPackages"]:
        for argv in (["ffmpeg", "-version"], ["ffprobe", "-version"], ["fc-match", "Noto Sans CJK SC"]):
            checks.append({"name": argv[0], **run(argv)})
    skill = Path("/etc/codex/skills") / profile["skill"] / "SKILL.md"
    checks.append({"name": "admin-skill", "ok": skill.is_file(), "path": str(skill)})
    try:
        home = Path(os.environ["HOME"])
        with tempfile.TemporaryDirectory(prefix="rundesk-profile-probe-", dir=home):
            pass
        checks.append({"name": "writable-home", "ok": True})
    except (OSError, KeyError) as e:
        checks.append({"name": "writable-home", "ok": False, "error": str(e)})
    if selection["searchBackend"] == "wigolo":
        checks.append({"name": "wigolo-launcher", "ok": shutil.which("rundesk-wigolo") is not None})
        checks.append({"name": "wigolo-startup", **run(["rundesk-wigolo", "--version"])})
        package = Path("/opt/wigolo/node_modules/wigolo/package.json")
        version = json.loads(package.read_text())["version"] if package.is_file() else None
        checks.append({"name": "wigolo-package", "ok": version == config["dependencies"]["wigolo"]["version"], "version": version})
        checks.append({"name": "browser-launch", **run(["node", "--input-type=module", "-e", "import {chromium} from '/opt/wigolo/node_modules/playwright/index.mjs'; const b=await chromium.launch({headless:true}); const p=await b.newPage(); await p.setContent('<title>RunDesk probe</title>'); if(await p.title()!=='RunDesk probe')process.exitCode=1; await b.close();"])})
    result = {"schemaVersion": 1, "profileId": profile["id"], "ready": all(c["ok"] for c in checks), "checks": checks,
              "scope": "offline executable, dependencies, skill path and writable-home checks", "modelTested": False, "networkTested": False,
              "notes": ["Does not prove Codex skill discovery, MCP handshake, model downloads, auth or live search quality."]}
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if result["ready"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
