#!/usr/bin/env python3
"""Check native video-use offline, without installing or contacting generation/ASR."""
import argparse
import hashlib
import importlib.metadata
import json
import os
from pathlib import Path
import subprocess
import sys

COMMIT = "43cfc566833548093455c9d4a46bd69913b7e552"
SOURCE_HASHES = {
    "LICENSE": "5afa3d97bf1e6f998d587fa101e3fe0d0c416840b7fe77bcd6ae97690a334631",
    "SKILL.md": "d3bbeb19bc0e81e197d74806fba15e0a5f5694a0cdf33f0a2348bffe8472a27b",
    "helpers/render.py": "6bbde45ad5929c7c94fa068996f81e0c44b55a9c9dedcf716fc24c071a5c8dd7",
    "helpers/transcribe.py": "5b61a5f92db9baf6e7f7f3ee88d0afaed8485e5f7b537a38e90f87ce8c76e58a",
    "helpers/pack_transcripts.py": "f9e419def5f0a014d5e1fd16fdad801013ae068854c1d474c3492297e2304f4b",
    "helpers/timeline_view.py": "69aee88e4204f86127740cca9de6a6eaa75a558df1bb07745dd62f69a3c2e9cf"
}


def command(argv):
    try:
        result = subprocess.run(argv, capture_output=True, text=True, timeout=30)
        return {"ok": result.returncode == 0, "exitCode": result.returncode, "output": (result.stdout + result.stderr)[-1500:]}
    except (OSError, subprocess.TimeoutExpired) as error:
        return {"ok": False, "error": str(error)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, default=Path("/opt/upstreams/video-use"))
    parser.add_argument("--requirements", type=Path, default=Path("/opt/rundesk-upstream/requirements.lock"))
    args = parser.parse_args()
    checks = []
    for relative, expected in SOURCE_HASHES.items():
        path = args.source / relative
        try:
            actual = hashlib.sha256(path.read_bytes()).hexdigest()
            checks.append({"name": relative, "ok": actual == expected, "sha256": actual})
        except OSError as error:
            checks.append({"name": relative, "ok": False, "error": str(error)})
    try:
        requirements = args.requirements.read_text().splitlines()
        for line in requirements:
            if not line or line.startswith("#"):
                continue
            name, expected = line.split("==", 1)
            try:
                actual = importlib.metadata.version(name)
                checks.append({"name": "python:" + name, "ok": actual == expected, "version": actual, "expected": expected})
            except importlib.metadata.PackageNotFoundError:
                checks.append({"name": "python:" + name, "ok": False, "expected": expected, "error": "not installed"})
    except (OSError, ValueError) as error:
        checks.append({"name": "python-lock", "ok": False, "error": str(error)})
    for argv in (["ffmpeg", "-version"], ["ffprobe", "-version"],
                 [sys.executable, str(args.source / "helpers/render.py"), "--help"],
                 [sys.executable, str(args.source / "helpers/pack_transcripts.py"), "--help"],
                 [sys.executable, str(args.source / "helpers/timeline_view.py"), "--help"]):
        checks.append({"name": " ".join(argv), **command(argv)})
    result = {"schemaVersion": 1, "profileId": "video-use", "upstreamCommit": COMMIT,
              "ready": all(row["ok"] for row in checks), "checks": checks,
              "videoAppConfigured": all(os.environ.get(name) for name in ("VIDEO_APP_MCP_URL", "VIDEO_APP_TOKEN", "VIDEO_APP_PROJECT_ID")),
              "scope": "offline source integrity, pinned dependencies and native helper startup only",
              "modelTested": False, "generationTested": False, "asrTested": False}
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if result["ready"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
