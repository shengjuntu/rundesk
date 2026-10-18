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
    if profile["id"] == "video-build":
        vendor = Path("/opt/video-use")
        expected = config["dependencies"]["videoUse"]["commit"]
        adapter = run([sys.executable, "-I", str(Path(__file__).with_name("video_use_adapter.py")), "--version"])
        try:
            identity = json.loads(adapter.get("output", ""))
            adapter["ok"] = adapter["ok"] and identity == {"schemaVersion": 1, "adapterVersion": 2, "upstreamCommit": expected}
        except (ValueError, TypeError):
            adapter["ok"] = False
        checks.append({"name": "video-use-adapter-contract", **adapter})
        try:
            pin = json.loads((vendor / "RUNDESK_PIN.json").read_text())
            checks.append({"name": "video-use-source-pin", "ok": pin["commit"] == expected and (vendor / "LICENSE").is_file() and (vendor / "helpers/render.py").is_file(), "commit": pin.get("commit")})
        except (OSError, ValueError, KeyError) as e:
            checks.append({"name": "video-use-source-pin", "ok": False, "error": str(e)})
        for package, expected in config["dependencies"]["videoUse"]["python"].items():
            try:
                installed = importlib.metadata.version(package)
                checks.append({"name": "video-use " + package, "ok": installed == expected, "version": installed, "expected": expected})
            except importlib.metadata.PackageNotFoundError:
                checks.append({"name": "video-use " + package, "ok": False, "error": "not installed"})
        with tempfile.TemporaryDirectory(prefix="rundesk-video-use-doctor-") as tmp:
            command = [sys.executable, str(Path(__file__).with_name("video_use_smoke.py")), "--out", str(Path(tmp) / "run")]
            try:
                p = subprocess.run(command, capture_output=True, text=True, timeout=120)
                checks.append({"name": "video-use-script-media-render", "ok": p.returncode == 0, "output": (p.stdout + p.stderr)[-4000:]})
            except (OSError, subprocess.TimeoutExpired) as e:
                checks.append({"name": "video-use-script-media-render", "ok": False, "error": str(e)})
    skill = Path("/etc/codex/skills") / profile["skill"] / "SKILL.md"
    checks.append({"name": "admin-skill", "ok": skill.is_file(), "path": str(skill)})
    try:
        home = Path(os.environ["HOME"])
        with tempfile.TemporaryDirectory(prefix="rundesk-profile-probe-", dir=home):
            pass
        checks.append({"name": "writable-home", "ok": True})
    except (OSError, KeyError) as e:
        checks.append({"name": "writable-home", "ok": False, "error": str(e)})
    if selection["agentReach"]:
        for argv in (["agent-reach", "--version"], ["gh", "--version"], ["bird", "--version"], ["mcporter", "--version"]):
            checks.append({"name": " ".join(argv), **run(argv)})
        for package, expected in config["dependencies"]["npm"].items():
            path = Path('/opt/agent-tools/node_modules') / package / 'package.json'
            installed = json.loads(path.read_text()).get('version') if path.is_file() else None
            checks.append({"name": package + " package", "ok": installed == expected, "version": installed, "expected": expected})
        checks.append({"name": "agent-reach-skill", "ok": Path('/etc/codex/skills/agent-reach/SKILL.md').is_file()})
        # This only reads configuration; 'mcporter list' would contact servers.
        checks.append({"name": "exa-cli-configuration", **run(["mcporter", "config", "get", "exa", "--json"])})
    result = {"schemaVersion": 1, "profileId": profile["id"], "ready": all(c["ok"] for c in checks), "checks": checks,
              "scope": "offline executable, dependencies, skill path and writable-home checks", "modelTested": False, "networkTested": False,
              "notes": ["Does not prove Codex skill discovery, MCP handshake, model downloads, auth or live search quality."]}
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if result["ready"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
