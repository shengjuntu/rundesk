#!/usr/bin/env python3
"""Check source hygiene; --release checks the exact tree before source packaging.

Normal developer checks ignore known local output directories. Release checks
reject them, generated recipe metadata, caches and runtime payloads. The optional
checksum check is for a completed release tree, not an editable checkout.
"""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import stat

ROOT = Path(__file__).resolve().parents[1]
LOCAL_ROOTS = {".git", "bin", "build", "dist", "data", ".cache", "coverage", "htmlcov"}
CACHE_DIRS = {"__pycache__", "node_modules", ".pytest_cache", ".mypy_cache", ".ruff_cache", ".next", ".parcel-cache"}
PROTECTED_ROOTS = {"recipes", "config", "third_party", "internal"}
TEMP_SUFFIXES = {".tmp", ".temp", ".bak", ".orig", ".rej", ".swp", ".swo"}
NATIVE_SUFFIXES = {".exe", ".dll", ".so", ".dylib", ".o", ".a", ".class"}
GENERATED_NAMES = {"package-manifest.json", "PACKAGE-INCOMPLETE.txt", "context.zip", "build.log", "image-id", "downloads.json"}


def source_version(root: Path) -> str:
    match = re.search(r'const Version = "([0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?)"',
                      (root / "internal/app/api_v1.go").read_text(encoding="utf-8"))
    if not match:
        raise ValueError("RunDesk source version was not found")
    return match.group(1)


def validation_name(version: str) -> str:
    major, minor, patch = version.split("-", 1)[0].split(".")
    return f"release-{int(major)}{int(minor):02d}{patch if int(patch) else ''}.json"


def validation_issues(root: Path, version: str, require_passed: bool = False) -> list[str]:
    """One current summary is kept in source; old raw records live in prior releases."""
    directory = root / "docs/validation"
    summary = directory / "current" / validation_name(version)
    allowed = {"README.md", "current/" + summary.name}
    errors = []
    if directory.is_symlink() or (directory / "current").is_symlink():
        return ["docs/validation/: validation directories must not be symbolic links"]
    if not (directory / "README.md").is_file():
        errors.append("docs/validation/README.md: validation scope/provenance guide is missing")
    if directory.is_dir():
        for path in directory.rglob("*"):
            rel = path.relative_to(directory).as_posix()
            if path.is_symlink():
                errors.append(f"docs/validation/{rel}: validation links are not source records")
            elif path.is_dir():
                if rel != "current":
                    errors.append(f"docs/validation/{rel}/: historical or generated record directory is not active source")
            elif rel not in allowed:
                errors.append(f"docs/validation/{rel}: retain only README.md and current/{summary.name}")
    if summary.is_file():
        try:
            report = json.loads(summary.read_text(encoding="utf-8"))
            if not isinstance(report, dict) or report.get("version") != version:
                errors.append(f"docs/validation/current/{summary.name}: report version must match {version}")
            elif require_passed and report.get("status") != "passed":
                errors.append(f"docs/validation/current/{summary.name}: release validation is not passed")
        except (OSError, UnicodeError, ValueError) as error:
            errors.append(f"docs/validation/current/{summary.name}: invalid JSON report ({error})")
    elif require_passed:
        errors.append(f"docs/validation/current/{summary.name}: current release report is missing")
    return errors


def fixture_path(parts: tuple[str, ...]) -> bool:
    return "testdata" in parts or len(parts) >= 2 and parts[:2] == ("tests", "fixtures")


def native_payload(path: Path) -> bool:
    with path.open("rb") as stream:
        head = stream.read(64)
        if head[:4] in (b"\x7fELF", b"\xfe\xed\xfa\xce", b"\xce\xfa\xed\xfe", b"\xfe\xed\xfa\xcf", b"\xcf\xfa\xed\xfe", b"\xca\xfe\xba\xbe"):
            return True
        if head[:2] == b"MZ" and len(head) == 64:
            offset = int.from_bytes(head[60:64], "little")
            if 64 <= offset <= 1 << 20:
                stream.seek(offset)
                return stream.read(4) == b"PE\0\0"
    return False


def digest(path: Path) -> str:
    value = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            value.update(block)
    return value.hexdigest()


def manifest_issues(root: Path, files: dict[str, Path]) -> list[str]:
    manifest = root / "SHA256SUMS"
    if manifest.is_symlink() or not manifest.is_file():
        return ["SHA256SUMS: final source manifest is missing or linked"]
    expected = {}
    errors = []
    try:
        for number, line in enumerate(manifest.read_text(encoding="utf-8").splitlines(), 1):
            pair = line.split("  ", 1)
            if len(pair) != 2 or not re.fullmatch(r"[a-f0-9]{64}", pair[0]):
                errors.append(f"SHA256SUMS:{number}: invalid checksum entry")
                continue
            sha, name = pair
            relative = PurePosixPath(name)
            if (not name or relative.is_absolute() or ".." in relative.parts or relative.as_posix() != name
                    or any(char in name for char in "\\:\x00\r\n") or name == "SHA256SUMS" or name in expected):
                errors.append(f"SHA256SUMS:{number}: unsafe, duplicate or self-referencing path")
                continue
            expected[name] = sha
        actual = set(files) - {"SHA256SUMS"}
        for name in sorted(expected.keys() - actual):
            errors.append(f"SHA256SUMS: listed file is missing: {name}")
        for name in sorted(actual - expected.keys()):
            errors.append(f"SHA256SUMS: file is not listed: {name}")
        for name in sorted(actual & expected.keys()):
            if digest(files[name]) != expected[name]:
                errors.append(f"SHA256SUMS: checksum mismatch: {name}")
    except (OSError, UnicodeError) as error:
        errors.append(f"SHA256SUMS: could not verify manifest ({error})")
    return errors


def inspect_source(root: Path, *, release: bool = False, verify_manifest: bool = False) -> dict:
    root = root.resolve()
    release = release or verify_manifest
    errors, ignored, files = [], [], {}
    try:
        version = source_version(root)
    except (OSError, UnicodeError, ValueError) as error:
        return {"passed": False, "errors": [str(error)], "release": release, "files": 0}

    def walk(directory: Path):
        for path in sorted(directory.iterdir()):
            relative = path.relative_to(root)
            name = relative.as_posix()
            parts = relative.parts
            protected = parts[0] in PROTECTED_ROOTS
            if len(parts) == 1 and path.name in LOCAL_ROOTS:
                if release:
                    errors.append(f"{name}: local build/runtime/version-control output is not source")
                else:
                    ignored.append(name)
                continue
            if path.is_symlink():
                errors.append(f"{name}: symbolic links are not allowed in a source release")
                continue
            mode = path.stat().st_mode
            if stat.S_ISDIR(mode):
                if path.name in CACHE_DIRS:
                    if release or protected:
                        errors.append(f"{name}: generated dependency/cache directory is not source")
                    else:
                        ignored.append(name)
                    continue
                if len(parts) == 1 and path.name == "examples":
                    errors.append("examples/: retired source tree must not be restored")
                    continue
                walk(path)
                continue
            if not stat.S_ISREG(mode):
                errors.append(f"{name}: special files are not allowed")
                continue
            local_file = path.suffix in {".pyc", ".pyo"} or path.name in {".env", "server.lock"} or re.search(r"\.(?:db|sqlite|sqlite3)(?:-(?:wal|shm))?$", path.name)
            if local_file:
                if release or protected:
                    errors.append(f"{name}: generated cache, credential or runtime state is not source")
                else:
                    ignored.append(name)
                continue
            if path.suffix in TEMP_SUFFIXES or path.name.endswith("~"):
                errors.append(f"{name}: editor/temporary backup is not source")
            fixture = fixture_path(parts) and parts[0] not in {"recipes", "config"}
            if not fixture and (path.name in GENERATED_NAMES or path.suffix.lower() in NATIVE_SUFFIXES or native_payload(path)):
                errors.append(f"{name}: generated package/build output or native binary is not source")
            if len(parts) >= 3 and parts[0] == "recipes":
                if len(parts) == 3 and path.name in {"profile.lock.json", "SHA256SUMS"}:
                    errors.append(f"{name}: recipe metadata/checksums must be generated in a build snapshot")
                if len(parts) == 4 and parts[2:] == ("upstream", "upstream.json"):
                    errors.append(f"{name}: derived upstream metadata must be generated from recipe.json")
            files[name] = path

    try:
        walk(root)
        errors.extend(validation_issues(root, version, require_passed=release))
        if verify_manifest:
            errors.extend(manifest_issues(root, files))
    except (OSError, UnicodeError, ValueError) as error:
        errors.append(f"Could not inspect source tree: {error}")
    return {"passed": not errors, "version": version, "release": release,
            "manifestVerified": verify_manifest and not errors, "files": len(files),
            "ignoredLocalOutputs": ignored, "errors": errors}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT, help="Source checkout or final staged source tree")
    parser.add_argument("--release", action="store_true", help="Reject all local/generated output and require passed release validation")
    parser.add_argument("--verify-manifest", action="store_true", help="Verify complete SHA256SUMS coverage and hashes; implies --release")
    args = parser.parse_args()
    result = inspect_source(args.root, release=args.release, verify_manifest=args.verify_manifest)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
