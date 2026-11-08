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
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import stat
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


def context_manifest(payload):
    """Compare exported content and execute permissions without ZIP metadata."""
    with zipfile.ZipFile(io.BytesIO(payload)) as archive:
        files = {}
        for entry in archive.infolist():
            assert not entry.is_dir() and not stat.S_ISLNK(entry.external_attr >> 16)
            assert entry.filename not in files
            files[entry.filename] = {"sha256": sha256(archive.read(entry)), "bytes": entry.file_size,
                                     "mode": oct((entry.external_attr >> 16) & 0o7777)}
        return files


def verify_context(payload, profile):
    with zipfile.ZipFile(io.BytesIO(payload)) as archive:
        names = archive.namelist()
        assert archive.testzip() is None
        assert len(names) == len(set(names))
        assert "Dockerfile" in names and "profile.lock.json" in names and "SHA256SUMS" in names
        assert "recipe.json" not in names and not any(name.startswith(("examples/", "variants/")) for name in names)
        lock = json.loads(archive.read("profile.lock.json"))
        assert lock["profile"]["id"] == profile and lock["codexVersion"] == CODEX_VERSION
        assert "sharedFiles" not in lock, "frozen context must be self contained"
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
        return {"files": len(names), "sha256": sha256(payload), "fileManifest": context_manifest(payload)}


def exercise_shared_inputs(stage, base, token, exports, frozen_path, original_frozen, report):
    """Mutate only disposable packaged resources; require a fully portable ZIP."""
    recipes = stage / "recipes"
    profile = recipes / "news2douyin"
    descriptor_file = profile / "recipe.json"
    original_descriptor = descriptor_file.read_bytes()
    descriptor = json.loads(original_descriptor)
    target = "tools/install-codex.sh"
    assert target in descriptor["sharedFiles"]
    shared = recipes / "common" / descriptor["sharedFiles"][target]
    local = profile / target
    assert shared.is_file() and not local.exists()
    export_path = f"/build-center/recipes/builtin-news2douyin/context.zip?codexVersion={CODEX_VERSION}"

    def rejected(reason):
        status, _ = request(base, token, export_path, expected=None)
        assert status >= 400, "unsafe or missing shared input was accepted: " + reason

    def descriptor_case(value, reason):
        try:
            descriptor_file.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")
            rejected(reason)
        finally:
            descriptor_file.write_bytes(original_descriptor)

    original_shared = shared.read_bytes()
    local.parent.mkdir(parents=True, exist_ok=True)
    marker = b"\n# portable-local-override-fixture\n"
    try:
        local.write_bytes(original_shared + marker)
        local.chmod(0o755)
        overridden = request(base, token, export_path)[1]
        with zipfile.ZipFile(io.BytesIO(overridden)) as archive:
            assert archive.read(target) == original_shared + marker
            assert archive.getinfo(target).external_attr >> 16 & 0o111
        hidden = shared.with_name(shared.name + ".portable-original")
        shared.rename(hidden)
        try:
            rejected("missing shared source, even with local override")
            assert request(base, token, frozen_path)[1] == original_frozen
            shared.symlink_to(hidden)
            try:
                rejected("shared source symlink")
            finally:
                shared.unlink()
        finally:
            hidden.rename(shared)
    finally:
        local.unlink()
    local.symlink_to(shared)
    try:
        rejected("local override symlink")
    finally:
        local.unlink()
    for source in ("../news2douyin/Dockerfile", "/etc/passwd", "tools//install-codex.sh"):
        changed = json.loads(original_descriptor)
        changed["sharedFiles"][target] = source
        descriptor_case(changed, "invalid source " + source)
    for unsafe in ("../outside", "profile.lock.json", "tools/../outside"):
        changed = json.loads(original_descriptor)
        changed["sharedFiles"][unsafe] = descriptor["sharedFiles"][target]
        descriptor_case(changed, "invalid target " + unsafe)
    report["checks"].append("shared sources materialize with local overrides; missing inputs, traversal, metadata injection and symlinks fail")

    # Exercise legacy self-contained authoring through the same real service.
    # Materialize all profiles because builtin discovery validates the catalog.
    backup = stage.parent / "recipes-authoring-backup"
    shutil.copytree(recipes, backup)
    try:
        for name in sorted(PROFILES):
            directory = recipes / name
            metadata = directory / "recipe.json"
            value = json.loads(metadata.read_bytes())
            for destination, origin in value.pop("sharedFiles", {}).items():
                source = recipes / "common" / origin
                paths = source.rglob("*") if source.is_dir() else [source]
                for path in paths:
                    if not path.is_file():
                        continue
                    relative = path.relative_to(source) if source.is_dir() else Path()
                    output = directory / destination / relative
                    if not output.exists():
                        output.parent.mkdir(parents=True, exist_ok=True)
                        shutil.copy2(path, output)
            metadata.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")
        shutil.rmtree(recipes / "common")
        for name in sorted(PROFILES):
            payload = request(base, token, f"/build-center/recipes/builtin-{name}/context.zip?codexVersion={CODEX_VERSION}")[1]
            assert context_manifest(payload) == context_manifest(exports[name]), "legacy authoring changed exported content: " + name
    finally:
        shutil.rmtree(recipes)
        backup.rename(recipes)
    report["checks"].append("all six fully self-contained legacy recipes export identical files and permissions without common")


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


def verify_incomplete_package_rejected(binary, temp, report):
    fixture = temp / "incomplete-source"
    for name in ("recipes", "config", "LICENSES"):
        shutil.copytree(ROOT / name, fixture / name)
    for name in ("LICENSE", "THIRD_PARTY_NOTICES.md", "internal/app/api_v1.go", "third_party/kun-sdk/LICENSE"):
        destination = fixture / name
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(ROOT / name, destination)
    descriptor = json.loads((fixture / "recipes/news2douyin/recipe.json").read_text())
    missing = fixture / "recipes/common" / descriptor["sharedFiles"]["tools/install-codex.sh"]
    missing.unlink()
    spec = importlib.util.spec_from_file_location("portable_package_release", ROOT / "scripts/package-release.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    destination = temp / "must-not-be-packaged"
    try:
        module.stage_release(binary, destination, resource_root=fixture)
    except (FileNotFoundError, ValueError) as error:
        assert "install-codex.sh" in str(error), str(error)
    else:
        raise AssertionError("packager accepted a missing declared shared source")
    assert not destination.exists()
    shutil.rmtree(fixture)
    report["checks"].append("packaging rejects missing declared shared sources before creating the destination")


def exercise(binary, results, report):
    token = secrets.token_hex(24)
    with tempfile.TemporaryDirectory(prefix="rundesk portable qa-") as directory:
        temp = Path(directory)
        stage = temp / "package"
        assert ROOT not in stage.parents
        verify_incomplete_package_rejected(binary, temp, report)
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
            exercise_shared_inputs(stage, base, token, exports, frozen_path, original_frozen, report)
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
        common = stage / "recipes/common"
        unavailable_common = stage.parent / "common-unavailable"
        common.rename(unavailable_common)
        try:
            with service(stage, data, token, env, results / "server-launcher.log", launcher=True) as base:
                assert request(base, token, frozen_path)[1] == original_frozen
                status, _ = request(base, token, "/build-center/recipes", expected=None)
                assert status >= 400, "missing common inputs were silently ignored after restart"
                unavailable_common.rename(common)
                assert read_json(base, token, "/build-center/recipes")["items"]
        finally:
            if unavailable_common.exists():
                unavailable_common.rename(common)
        report["checks"].append("packaged launcher handles spaces without Python; frozen custom recipes survive restart with common unavailable")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--baseline", type=Path, help="compare all Go-exported files and modes with a prior exports.json")
    parser.add_argument("--allow-baseline-change", action="append", default=[], metavar="PROFILE:PATH",
                        help="explicit reviewed byte change allowed against baseline; modes and path set must remain unchanged")
    args = parser.parse_args()
    results = args.output_dir.resolve()
    if results == ROOT or ROOT in results.parents:
        parser.error("results must be outside the source checkout")
    results.mkdir(parents=True, exist_ok=True)
    report = {"passed": False, "checks": [], "scope": "Actual packaged native Go demo server outside the checkout; recipe metadata/export/import only. No real Docker, model or worker execution."}
    try:
        exercise(args.binary.resolve(strict=True), results, report)
        if args.baseline:
            previous = json.loads(args.baseline.read_text())["go"]
            assert set(previous) == PROFILES
            allowed = {profile: set() for profile in PROFILES}
            for entry in args.allow_baseline_change:
                profile, path = entry.split(":", 1)
                assert profile in PROFILES and path in previous[profile]["files"], "invalid baseline exception: " + entry
                allowed[profile].add(path)
            reviewed = {}
            for profile, exported in report["contexts"].items():
                before, after = previous[profile]["files"], exported["fileManifest"]
                assert before.keys() == after.keys(), "baseline path set changed: " + profile
                changed = {path for path in before if before[path] != after[path]}
                assert changed <= allowed[profile], "unreviewed baseline changes: " + profile + ": " + ", ".join(sorted(changed - allowed[profile]))
                for path in changed:
                    assert before[path]["mode"] == after[path]["mode"], "baseline permission changed: " + profile + ":" + path
                    reviewed[profile + ":" + path] = {"before": before[path], "after": after[path]}
            report["reviewedBaselineChanges"] = reviewed
            report["checks"].append("all six Go exports preserve baseline paths, permissions and bytes except explicitly reviewed file changes")
            report["baselineSHA256"] = sha256(args.baseline.read_bytes())
        report["passed"] = True
    except Exception as error:
        report["error"] = str(error)
        traceback.print_exc()
    (results / "portable-package.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(report, ensure_ascii=False))
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
