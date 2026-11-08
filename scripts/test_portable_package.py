#!/usr/bin/env python3
"""Exercise an actual compiled RunDesk from a package outside the source tree.

Run after building a native Linux executable:
  python3 scripts/test_portable_package.py --binary /path/to/rundesk \
    --output-dir /path/to/results

The package is temporary. This launches only an isolated --demo server and
performs recipe reads/imports; it never builds Docker images or runs a model.
"""
from __future__ import annotations

import argparse
from contextlib import contextmanager
import hashlib
import http.client
import io
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import sys
import tempfile
import time
import traceback
from urllib.parse import urlsplit
import zipfile


ROOT = Path(__file__).resolve().parents[1]
PROFILES = {"news2douyin", "people-search", "video-build", "hyperframes", "video-use", "openmontage"}
CODEX_VERSION = "0.157.1"


def sha256(payload):
    return hashlib.sha256(payload).hexdigest()


@contextmanager
def service(stage, data, token, env, log, launcher=False):
    command = [str(stage / ("start-rundesk.sh" if launcher else "rundesk")),
               "--demo", "--listen", "127.0.0.1:0", "--data", str(data)]
    with log.open("w", encoding="utf-8") as stream:
        process = subprocess.Popen(command, cwd=stage, env=env, stdout=stream, stderr=subprocess.STDOUT)
        try:
            deadline = time.monotonic() + 20
            while time.monotonic() < deadline:
                if process.poll() is not None:
                    raise RuntimeError("Packaged server exited before startup: " + log.read_text())
                match = re.search(r"RunDesk (http://127\.0\.0\.1:\d+)", log.read_text())
                if match:
                    yield match[1]
                    return
                time.sleep(.05)
            raise RuntimeError("Packaged server did not become ready")
        finally:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
            text = log.read_text().replace(token, "[test-token]").replace(str(stage.parent), "[temporary-package]")
            log.write_text(text)


def request(base, token, path, method="GET", body=None, content_type=None, expected=200):
    address = urlsplit(base)
    assert address.hostname == "127.0.0.1"
    connection = http.client.HTTPConnection(address.hostname, address.port, timeout=30)
    headers = {"Authorization": "Bearer " + token}
    if content_type:
        headers["Content-Type"] = content_type
    try:
        connection.request(method, "/api/v1" + path, body=body, headers=headers)
        response = connection.getresponse()
        payload = response.read()
        if expected is not None:
            assert response.status == expected, f"{method} {path}: HTTP {response.status}: {payload[:1600]!r}"
        return response.status, payload
    finally:
        connection.close()


def read_json(base, token, path):
    return json.loads(request(base, token, path)[1])


def verify_context(payload, profile):
    with zipfile.ZipFile(io.BytesIO(payload)) as archive:
        names = archive.namelist()
        assert archive.testzip() is None
        assert len(names) == len(set(names))
        assert "Dockerfile" in names and "profile.lock.json" in names and "SHA256SUMS" in names
        assert "recipe.json" not in names and not any(name.startswith(("examples/", "variants/")) for name in names)
        lock = json.loads(archive.read("profile.lock.json"))
        assert lock["profile"]["id"] == profile and lock["codexVersion"] == CODEX_VERSION
        dockerfile = archive.read("Dockerfile").decode()
        assert "ARG CODEX_VERSION=" + CODEX_VERSION in dockerfile
        assert 'LABEL io.rundesk.codex.version="' + CODEX_VERSION + '"' in dockerfile
        manifest = {}
        for line in archive.read("SHA256SUMS").decode().splitlines():
            digest, relative = line.split("  ", 1)
            assert relative not in manifest and not relative.startswith("/") and ".." not in Path(relative).parts
            manifest[relative] = digest
        assert set(manifest) == set(names) - {"SHA256SUMS"}
        for name, digest in manifest.items():
            assert sha256(archive.read(name)) == digest, name
        if profile in {"hyperframes", "video-use", "openmontage"}:
            upstream = lock["upstream"]
            assert json.loads(archive.read("upstream/upstream.json")) == upstream
            if "sourceArchive" in upstream:
                assert sha256(archive.read(upstream["sourceArchive"])) == upstream["sourceSHA256"]
        if profile in {"news2douyin", "people-search"}:
            assert "/opt/rundesk-profile/vendor/agent-reach" in archive.read("requirements.txt").decode()
            assert "COPY vendor/ /opt/rundesk-profile/vendor/" in dockerfile
            assert "force-include" not in archive.read("vendor/agent-reach/pyproject.toml").decode()
            assert "vendor/agent-reach/LICENSE" in names
            assert 'verify_codex "$binary"' in archive.read("tools/install-codex.sh").decode()
        return {"files": len(names), "sha256": sha256(payload)}


def import_recipe(base, token, payload):
    boundary = "rundesk-portable-" + secrets.token_hex(12)
    manifest = {"schemaVersion": 1, "name": "Portable frozen fixture", "version": "1.0.0",
                "description": "Isolated package test; no execution.", "dockerfile": "Dockerfile"}
    chunks = []
    for name, value in (("manifest", json.dumps(manifest)), ("requestId", secrets.token_hex(12))):
        chunks.append((f'--{boundary}\r\nContent-Disposition: form-data; name="{name}"\r\n\r\n{value}\r\n').encode())
    chunks.append((f'--{boundary}\r\nContent-Disposition: form-data; name="context"; filename="context.zip"\r\nContent-Type: application/zip\r\n\r\n').encode())
    chunks.extend([payload, (f"\r\n--{boundary}--\r\n").encode()])
    return json.loads(request(base, token, "/build-center/recipes", "POST", b"".join(chunks),
                              "multipart/form-data; boundary=" + boundary)[1])


def exercise(binary, results, report):
    token = secrets.token_hex(24)
    with tempfile.TemporaryDirectory(prefix="rundesk portable qa-") as directory:
        temp = Path(directory)
        stage = temp / "package"
        assert ROOT not in stage.parents
        command = [sys.executable, str(ROOT / "scripts/package-release.py"), "--binary", str(binary), "--output-dir", str(stage)]
        packaged = subprocess.run(command, text=True, capture_output=True)
        assert packaged.returncode == 0, packaged.stdout + packaged.stderr
        package = json.loads((stage / "package-manifest.json").read_text())
        assert package["platform"] == "linux" and not (stage / "examples").exists()
        assert not (stage / "scripts").exists() and not (stage / "clients").exists()
        for name, metadata in package["files"].items():
            content = (stage / name).read_bytes()
            assert len(content) == metadata["bytes"] and sha256(content) == metadata["sha256"], name
        for name in ("LICENSE", "THIRD_PARTY_NOTICES.md", "LICENSES/Pi-MIT.txt", "LICENSES/PiG-MIT.txt"):
            assert (stage / name).read_bytes() == (ROOT / name).read_bytes(), name
        assert (stage / "LICENSES/kun-sdk-LICENSE.txt").read_bytes() == (ROOT / "third_party/kun-sdk/LICENSE").read_bytes()
        version = subprocess.check_output([str(stage / "rundesk"), "--version"], text=True).strip()
        assert version == "RunDesk " + package["sourceVersion"]
        report["version"] = package["sourceVersion"]
        report["binarySHA256"] = sha256((stage / "rundesk").read_bytes())
        report["checks"].append("staged native executable, all resource hashes, preserved license notices and source version agree")
        minimal = temp / "minimal-path"
        minimal.mkdir()
        dirname = shutil.which("dirname")
        assert dirname
        (minimal / "dirname").symlink_to(dirname)
        env = {"PATH": str(minimal), "RUNDESK_TOKEN": token, "XDG_CONFIG_HOME": str(temp / "settings")}
        # Preserve the process home identity; --demo places all Codex state
        # under the explicit temporary data directory before it is used.
        if "HOME" in os.environ:
            env["HOME"] = os.environ["HOME"]
        assert all(shutil.which(name, path=env["PATH"]) is None for name in ("python", "python3", "node", "npm", "go", "docker"))
        assert "RUNDESK_RECIPES_DIR" not in env
        data = temp / "runtime-data"
        with service(stage, data, token, env, results / "server-direct.log") as base:
            before = {kind: read_json(base, token, "/" + kind) for kind in ("tasks", "sessions", "build-center/builds")}
            recipes = read_json(base, token, "/build-center/recipes")["items"]
            assert {row["profileId"] for row in recipes if row["kind"] == "builtin"} == PROFILES
            exports = {}
            report["contexts"] = {}
            for profile in sorted(PROFILES):
                exports[profile] = request(base, token, f"/build-center/recipes/builtin-{profile}/context.zip?codexVersion={CODEX_VERSION}")[1]
                report["contexts"][profile] = verify_context(exports[profile], profile)
            report["checks"].append("all six recipe exports work without examples, source ancestry, Python, Node or Docker on PATH")
            generic = request(base, token, f"/setup/docker-template?version={CODEX_VERSION}")[1]
            with zipfile.ZipFile(io.BytesIO(generic)) as archive:
                assert "Dockerfile" in archive.namelist() and "seed/config.toml" in archive.namelist()
                assert "ARG CODEX_VERSION=" + CODEX_VERSION in archive.read("Dockerfile").decode()
            report["checks"].append("generic template export reads packaged external resources")
            frozen = import_recipe(base, token, exports["news2douyin"])
            frozen_path = f"/build-center/recipes/{frozen['id']}/context.zip"
            original_frozen = request(base, token, frozen_path)[1]
            marker = "# portable-external-edit-fixture\n"
            dockerfile = stage / "recipes/news2douyin/Dockerfile"
            dockerfile.write_bytes(dockerfile.read_bytes() + marker.encode())
            updated = request(base, token, f"/build-center/recipes/builtin-news2douyin/context.zip?codexVersion={CODEX_VERSION}")[1]
            with zipfile.ZipFile(io.BytesIO(updated)) as archive:
                assert marker.encode() in archive.read("Dockerfile")
            assert updated != exports["news2douyin"] and request(base, token, frozen_path)[1] == original_frozen
            report["checks"].append("external Dockerfile edits affect new exports while imported context bytes stay frozen")
            available = stage / "recipes"
            missing = stage / "recipes.unavailable"
            available.rename(missing)
            try:
                status, _ = request(base, token, "/build-center/recipes", expected=None)
                assert status >= 400, "source checkout fallback masked missing packaged recipes"
            finally:
                missing.rename(available)
            assert read_json(base, token, "/build-center/recipes")["items"]
            report["checks"].append("missing packaged recipes fail visibly rather than falling back into a checkout")
            assert {kind: read_json(base, token, "/" + kind) for kind in before} == before
            report["checks"].append("recipe reads/imports create no tasks, sessions or image builds")
        with service(stage, data, token, env, results / "server-launcher.log", launcher=True) as base:
            assert request(base, token, frozen_path)[1] == original_frozen
            assert read_json(base, token, "/build-center/recipes")["items"]
        report["checks"].append("packaged launcher handles a path with spaces without Python and restart retains frozen custom recipes")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output-dir", type=Path, required=True)
    args = parser.parse_args()
    results = args.output_dir.resolve()
    if results == ROOT or ROOT in results.parents:
        parser.error("results must be outside the source checkout")
    results.mkdir(parents=True, exist_ok=True)
    report = {"passed": False, "checks": [], "scope": "Actual packaged native Go demo server outside the checkout; recipe metadata/export/import only. No real Docker, model or worker execution."}
    try:
        exercise(args.binary.resolve(strict=True), results, report)
        report["passed"] = True
    except Exception as error:
        report["error"] = str(error)
        traceback.print_exc()
    (results / "portable-package.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(report, ensure_ascii=False))
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
