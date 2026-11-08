#!/usr/bin/env python3
"""Prepare a writable native checkout inside the persistent RunDesk workspace."""
import fcntl
import hashlib
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile
import time

COMMIT = "c8066452325cb57c8fc1ba8c2a71b6e8842e6b65"


def installed_identity(profile_lock=Path("/opt/rundesk-profile/profile.lock.json"), overlay=Path("/opt/rundesk-upstream")) -> str:
    """Hash the installed recipe and its declared small deployment sources.

    Dependency locks are declared build files, so changing a provider, lock,
    recipe version or Codex pin selects a new copy even at the same upstream
    commit. Never traverse the installed, potentially large node_modules tree.
    """
    raw = profile_lock.read_bytes()
    value = json.loads(raw)
    upstream = value["upstream"]
    names = upstream["buildFiles"]
    if value["profile"]["id"] != "openmontage" or upstream["commit"] != COMMIT or not isinstance(names, list) or not all(isinstance(x, str) for x in names) or len(set(names)) != len(names):
        raise ValueError("Installed OpenMontage build identity is invalid")
    digest = hashlib.sha256()
    digest.update(len(raw).to_bytes(8, "big"))
    digest.update(raw)
    for name in sorted(names):
        rel = Path(name)
        path = overlay / rel
        if rel.is_absolute() or ".." in rel.parts or rel.as_posix() != name or path.is_symlink() or not path.is_file():
            raise ValueError("Installed OpenMontage build file is invalid")
        label = name.encode()
        content = path.read_bytes()
        digest.update(len(label).to_bytes(8, "big"))
        digest.update(label)
        digest.update(len(content).to_bytes(8, "big"))
        digest.update(content)
    return digest.hexdigest()


def prepare(source: Path, workspace: Path, *, identity=None) -> Path:
    identity = installed_identity() if identity is None else identity
    if not isinstance(identity, str) or len(identity) != 64 or any(c not in "0123456789abcdef" for c in identity):
        raise ValueError("Installed OpenMontage identity is invalid")
    parent = workspace / ".rundesk" / "upstreams"
    parent.mkdir(parents=True, exist_ok=True)
    cache_key = "openmontage-" + COMMIT + "-" + identity
    target = parent / cache_key
    expected_marker = {"commit": COMMIT, "source": str(source), "installationSHA256": identity}
    marker_name = "RUNDESK_WORKSPACE_PIN.json"
    with (parent / (cache_key + ".lock")).open("a+") as lock:
        deadline = time.monotonic() + 180
        while True:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError:
                if time.monotonic() >= deadline:
                    raise RuntimeError("Native workspace preparation is busy; retry the same launch later")
                time.sleep(0.05)
        if target.exists() or target.is_symlink():
            if target.is_symlink() or not target.is_dir():
                raise RuntimeError("Native workspace target is not an owned directory")
            marker = json.loads((target / marker_name).read_text())
            if marker != expected_marker:
                raise RuntimeError("Native workspace identity does not match this installed source")
            return target
        staging = Path(tempfile.mkdtemp(prefix=".openmontage-stage-", dir=parent))
        try:
            # Copy the installed modules as real files: native tools write into
            # composer/projects/public and node_modules caches on a read-only-root
            # Docker host. Symlinking the composer back into /opt is insufficient.
            shutil.copytree(source, staging / "source", symlinks=True)
            (staging / "source" / marker_name).write_text(json.dumps(expected_marker))
            (staging / "source").rename(target)
        finally:
            shutil.rmtree(staging, ignore_errors=True)
        return target


def main():
    workspace = Path.cwd().resolve()
    source = Path("/opt/upstreams/openmontage")
    try:
        target = prepare(source, workspace)
    except (OSError, ValueError, RuntimeError) as exc:
        print("OpenMontage workspace preparation failed: " + str(exc), file=sys.stderr)
        return 2
    env = dict(os.environ)
    env["PYTHONPATH"] = str(target) + (os.pathsep + env["PYTHONPATH"] if env.get("PYTHONPATH") else "")
    env.setdefault("OPENMONTAGE_PROJECTS_DIR", str(workspace / "projects"))
    env["OPENMONTAGE_RUNTIME_ROOT"] = str(target)
    os.execvpe(sys.executable, [sys.executable, *sys.argv[1:]], env)


if __name__ == "__main__":
    raise SystemExit(main())
